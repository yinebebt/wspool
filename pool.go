package wspool

import (
	"errors"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

// Pool allows connection reuse
type Pool struct {
	conns             []*WsConn
	config            *Config
	lock              sync.Mutex
	activeConnections int32
	closeOnce         sync.Once
	closeChan         chan struct{}
}

// Config is a struct for creating a pool.
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

	p := &Pool{
		config:            &config,
		conns:             make([]*WsConn, 0, config.MinConn),
		activeConnections: 0,
		closeChan:         make(chan struct{}),
	}

	// Initialize minimum connections
	for i := int32(0); i < config.MinConn; i++ {
		conn, err := p.newConnection()
		if err != nil {
			return nil, err
		}
		p.conns = append(p.conns, conn)
	}

	// Start the health check routine
	go p.startHealthCheck()

	return p, nil
}

// newConnection creates a new WebSocket connection wrapped in WsConn.
func (p *Pool) newConnection() (*WsConn, error) {
	conn, _, err := p.config.Dialer.Dial(p.config.URL, nil)
	if err != nil {
		return nil, err
	}
	p.activeConnections++

	return &WsConn{
		c:          conn,
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
	}, nil
}

// Acquire returns a connection from the pool.
func (p *Pool) Acquire() (*WsConn, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	// If there are idle connections, reuse them
	if len(p.conns) > 0 {
		conn := p.conns[len(p.conns)-1]
		p.conns = p.conns[:len(p.conns)-1]
		return conn, nil
	}

	// If pool size allows, create a new connection
	if p.activeConnections < p.config.MaxConn {
		conn, err := p.newConnection()
		if err != nil {
			return nil, err
		}
		p.activeConnections++
		return conn, nil
	}

	// Otherwise, no connection is available
	return nil, errors.New("no available connections")
}

// release returns a connection to the pool.
func (p *Pool) release(conn *WsConn) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if int32(len(p.conns)) < p.config.MaxConn {
		p.conns = append(p.conns, conn)
	} else {
		conn.Close()
	}

	p.maintainPoolSize()
}

// Close closes all connections in the pool.
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.closeChan)
		p.lock.Lock()
		defer p.lock.Unlock()

		for _, conn := range p.conns {
			conn.Close()
		}
		p.conns = nil
	})
}

// maintainPoolSize ensures the pool respects min and max connection constraints.
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
		conn.Close()
	}
}

// isIdleOrExpired checks if a connection is idle or exceeds its lifetime.
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

			var activeConnections []*WsConn
			now := time.Now()
			for _, conn := range p.conns {
				if p.isIdleOrExpired(conn, now) {
					conn.Close()
					continue
				}
				activeConnections = append(activeConnections, conn)
			}
			p.conns = activeConnections

			p.maintainPoolSize()
			p.lock.Unlock()

		case <-p.closeChan:
			return
		}
	}
}
