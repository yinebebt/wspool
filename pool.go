package wspool

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Pool manages a pool of reusable WebSocket connections.
type Pool struct {
	conns             []*WsConn
	config            *Config
	lock              sync.Mutex
	activeConnections int32
	closed            bool
	waiters           []chan *WsConn
	closeOnce         sync.Once
	closeChan         chan struct{}
}

// Stats holds a snapshot of pool health at the time of the call.
type Stats struct {
	// IdleConns is the number of connections sitting idle in the pool.
	IdleConns int32
	// ActiveConns is the total number of open connections (idle + acquired).
	ActiveConns int32
	// MaxConns is the configured upper bound.
	MaxConns int32
}

// Config specifies the configuration for a Pool.
type Config struct {
	// MaxConnLifetime is the duration since creation after which a connection will be automatically closed.
	MaxConnLifetime time.Duration

	// MaxConnIdleTime is the duration after which an idle connection will be automatically closed by the health check.
	MaxConnIdleTime time.Duration

	// MaxConn is the maximum size of the pool.
	MaxConn int32

	// MinConn is the minimum size of the pool.
	MinConn int32

	// HealthCheckPeriod is the duration between checks of the health of idle connections.
	HealthCheckPeriod time.Duration
	Dialer            *websocket.Dialer
	URL               string
}

// New creates a new Pool with the specified configuration.
func New(config Config) (*Pool, error) {
	if config.Dialer == nil || config.URL == "" {
		return nil, errors.New("dialer and URL must be provided")
	}
	if config.HealthCheckPeriod <= 0 {
		return nil, errors.New("HealthCheckPeriod must be greater than zero")
	}
	if config.MaxConn <= 0 {
		return nil, errors.New("MaxConn must be greater than zero")
	}
	if config.MinConn < 0 || config.MinConn > config.MaxConn {
		return nil, errors.New("MinConn must be between 0 and MaxConn")
	}

	p := &Pool{
		config:    &config,
		conns:     make([]*WsConn, 0, config.MinConn),
		closeChan: make(chan struct{}),
	}

	// Initialize minimum connections; close any already-created ones on failure.
	for i := int32(0); i < config.MinConn; i++ {
		conn, err := p.newConnection()
		if err != nil {
			for _, c := range p.conns {
				c.disconnect()
				p.activeConnections--
			}
			return nil, err
		}
		p.conns = append(p.conns, conn)
	}

	go p.startHealthCheck()

	return p, nil
}

// newConnection dials a new WebSocket connection and wraps it in a WsConn.
func (p *Pool) newConnection() (*WsConn, error) {
	conn, _, err := p.config.Dialer.Dial(p.config.URL, nil)
	if err != nil {
		return nil, err
	}
	p.activeConnections++

	return &WsConn{
		p:          p,
		c:          conn,
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
	}, nil
}

// Acquire returns a connection from the pool, blocking until one is available
// or ctx is cancelled. Idle connections are verified with a ping before being
// returned; dead ones are discarded and the loop retries.
func (p *Pool) Acquire(ctx context.Context) (*WsConn, error) {
	for {
		p.lock.Lock()

		if p.closed {
			p.lock.Unlock()
			return nil, errors.New("pool is closed")
		}

		// Reuse an idle connection.
		if len(p.conns) > 0 {
			conn := p.conns[len(p.conns)-1]
			p.conns = p.conns[:len(p.conns)-1]
			p.lock.Unlock()

			if !conn.ping() {
				// Connection is dead; discard and retry.
				p.lock.Lock()
				p.activeConnections--
				p.lock.Unlock()
				continue
			}
			return conn, nil
		}

		// Create a new connection if capacity allows.
		if p.activeConnections < p.config.MaxConn {
			conn, err := p.newConnection()
			p.lock.Unlock()
			if err != nil {
				return nil, err
			}
			return conn, nil
		}

		// Pool is at capacity — register as a waiter and block.
		ch := make(chan *WsConn, 1)
		p.waiters = append(p.waiters, ch)
		p.lock.Unlock()

		select {
		case conn := <-ch:
			if !conn.ping() {
				p.lock.Lock()
				p.activeConnections--
				p.lock.Unlock()
				continue
			}
			return conn, nil
		case <-ctx.Done():
			p.removeWaiter(ch)
			return nil, ctx.Err()
		}
	}
}

// removeWaiter removes ch from the waiters list and returns any connection
// that arrived just before the context was cancelled back to the pool.
func (p *Pool) removeWaiter(ch chan *WsConn) {
	p.lock.Lock()
	for i, w := range p.waiters {
		if w == ch {
			p.waiters = append(p.waiters[:i], p.waiters[i+1:]...)
			break
		}
	}
	p.lock.Unlock()

	// Drain: a connection may have been sent between ctx cancellation and
	// waiter removal. If so, return it to the pool.
	select {
	case conn := <-ch:
		p.release(conn)
	default:
	}
}

// release returns a connection to the pool.
func (p *Pool) release(conn *WsConn) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.closed {
		conn.disconnect()
		p.activeConnections--
		return
	}

	// Hand the connection directly to a waiter if one is waiting.
	// maintainPoolSize is not called here: the connection remains active
	// (owned by the waiter), so pool size is unchanged.
	if len(p.waiters) > 0 {
		ch := p.waiters[0]
		p.waiters = p.waiters[1:]
		ch <- conn
		return
	}

	if int32(len(p.conns)) < p.config.MaxConn {
		p.conns = append(p.conns, conn)
	} else {
		conn.disconnect()
		p.activeConnections--
	}

	p.maintainPoolSize()
}

// Close closes all connections in the pool.
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.closeChan)
		p.lock.Lock()
		defer p.lock.Unlock()

		p.closed = true
		for _, conn := range p.conns {
			conn.disconnect()
			p.activeConnections--
		}
		p.conns = nil
	})
}

// Stats returns a snapshot of the current pool state.
func (p *Pool) Stats() Stats {
	p.lock.Lock()
	defer p.lock.Unlock()
	return Stats{
		IdleConns:   int32(len(p.conns)),
		ActiveConns: p.activeConnections,
		MaxConns:    p.config.MaxConn,
	}
}

// maintainPoolSize ensures the idle pool stays between MinConn and MaxConn.
func (p *Pool) maintainPoolSize() {
	for int32(len(p.conns)) < p.config.MinConn {
		conn, err := p.newConnection()
		if err != nil {
			break
		}
		p.conns = append(p.conns, conn)
	}

	for int32(len(p.conns)) > p.config.MaxConn {
		conn := p.conns[len(p.conns)-1]
		p.conns = p.conns[:len(p.conns)-1]
		conn.disconnect()
		p.activeConnections--
	}
}

// isIdleOrExpired reports whether a connection should be evicted.
func (p *Pool) isIdleOrExpired(conn *WsConn, now time.Time) bool {
	if p.config.MaxConnLifetime > 0 && now.Sub(conn.createdAt) > p.config.MaxConnLifetime {
		return true
	}
	if p.config.MaxConnIdleTime > 0 && now.Sub(conn.lastUsedAt) > p.config.MaxConnIdleTime {
		return true
	}
	return false
}

func (p *Pool) startHealthCheck() {
	ticker := time.NewTicker(p.config.HealthCheckPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.lock.Lock()

			var healthy []*WsConn
			now := time.Now()
			for _, conn := range p.conns {
				if p.isIdleOrExpired(conn, now) {
					conn.disconnect()
					p.activeConnections--
					continue
				}
				healthy = append(healthy, conn)
			}
			p.conns = healthy

			p.maintainPoolSize()
			p.lock.Unlock()

		case <-p.closeChan:
			return
		}
	}
}
