package wspool

import (
	"errors"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

// Pool allows connection reuse
type Pool struct {
	conns             []*pooledConn
	config            *Config
	lock              sync.Mutex
	activeConnections int32
	closed            bool
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
	if config.MaxConn <= 0 {
		return nil, errors.New("MaxConn must be greater than 0")
	}
	if config.MinConn < 0 || config.MinConn > config.MaxConn {
		return nil, errors.New("MinConn must be between 0 and MaxConn")
	}

	p := &Pool{
		config:            &config,
		conns:             make([]*pooledConn, 0, config.MinConn),
		activeConnections: 0,
		closeChan:         make(chan struct{}),
	}

	// Initialize minimum connections
	for i := int32(0); i < config.MinConn; i++ {
		pc, err := p.newConnection()
		if err != nil {
			// Close already created connections on failure
			for _, c := range p.conns {
				c.close()
			}
			return nil, err
		}
		p.conns = append(p.conns, pc)
	}

	// Start the health check routine
	if config.HealthCheckPeriod > 0 {
		go p.startHealthCheck()
	}

	return p, nil
}

// newConnection creates a new WebSocket connection wrapped in pooledConn.
func (p *Pool) newConnection() (*pooledConn, error) {
	conn, _, err := p.config.Dialer.Dial(p.config.URL, nil)
	if err != nil {
		return nil, err
	}
	p.activeConnections++

	return &pooledConn{
		c:          conn,
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
	}, nil
}

// Acquire returns a connection from the pool.
func (p *Pool) Acquire() (*WsConn, error) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.closed {
		return nil, errors.New("pool is closed")
	}

	// If there are idle connections, reuse them (skip stale ones)
	now := time.Now()
	for len(p.conns) > 0 {
		pc := p.conns[len(p.conns)-1]
		p.conns = p.conns[:len(p.conns)-1]
		if p.isIdleOrExpired(pc, now) {
			pc.close()
			p.activeConnections--
			continue
		}
		return &WsConn{pc: pc, p: p}, nil
	}

	// If pool size allows, create a new connection
	if p.activeConnections < p.config.MaxConn {
		pc, err := p.newConnection()
		if err != nil {
			return nil, err
		}
		return &WsConn{pc: pc, p: p}, nil
	}

	// Otherwise, no connection is available
	return nil, errors.New("no available connections")
}

// release returns a connection to the pool.
func (p *Pool) release(pc *pooledConn) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.closed {
		pc.close()
		p.activeConnections--
		return
	}

	if int32(len(p.conns)) < p.config.MinConn {
		pc.lastUsedAt = time.Now()
		p.conns = append(p.conns, pc)
	} else {
		pc.close()
		p.activeConnections--
	}
}

// Close closes all connections in the pool.
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.closeChan)
		p.lock.Lock()
		defer p.lock.Unlock()

		p.closed = true
		for _, pc := range p.conns {
			pc.close()
			p.activeConnections--
		}
		p.conns = nil
	})
}

// maintainPoolSize ensures the pool respects min and max connection constraints.
func (p *Pool) maintainPoolSize() {
	for int32(len(p.conns)) < p.config.MinConn && p.activeConnections < p.config.MaxConn {
		pc, err := p.newConnection()
		if err != nil {
			break
		}
		p.conns = append(p.conns, pc)
	}

	for int32(len(p.conns)) > p.config.MaxConn {
		pc := p.conns[len(p.conns)-1]
		p.conns = p.conns[:len(p.conns)-1]
		pc.close()
		p.activeConnections--
	}
}

// isIdleOrExpired checks if a connection is idle or exceeds its lifetime.
func (p *Pool) isIdleOrExpired(pc *pooledConn, now time.Time) bool {
	if p.config.MaxConnLifetime > 0 && now.Sub(pc.createdAt) > p.config.MaxConnLifetime {
		return true
	}

	if p.config.MaxConnIdleTime > 0 && now.Sub(pc.lastUsedAt) > p.config.MaxConnIdleTime {
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

			if p.closed {
				p.lock.Unlock()
				return
			}

			var healthy []*pooledConn
			now := time.Now()
			for _, pc := range p.conns {
				if p.isIdleOrExpired(pc, now) {
					pc.close()
					p.activeConnections--
					continue
				}
				healthy = append(healthy, pc)
			}
			p.conns = healthy

			p.maintainPoolSize()
			p.lock.Unlock()

		case <-p.closeChan:
			return
		}
	}
}
