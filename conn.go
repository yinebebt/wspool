package wspool

import (
	"errors"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

// WsConn is an acquired conn from a Pool.
type WsConn struct {
	c          *websocket.Conn
	p          *Pool
	mu         sync.Mutex
	createdAt  time.Time
	lastUsedAt time.Time
}

// Send sends a message over the WebSocket connection.
func (w *WsConn) Send(message string) error {
	if w.c == nil {
		return errors.New("connection is nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lastUsedAt = time.Now()
	return w.c.WriteMessage(websocket.TextMessage, []byte(message))
}

// SendJSON sends a JSON-encoded message over the WebSocket connection.
func (w *WsConn) SendJSON(v interface{}) error {
	if w.c == nil {
		return errors.New("connection is nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lastUsedAt = time.Now()
	return w.c.WriteJSON(v)
}

// Close closes w and removes it from the pool.
func (w *WsConn) Close() error {
	if w.c == nil {
		return errors.New("connection is nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	err := w.c.Close()
	w.c = nil
	return err
}

// Release returns w to the pool it was acquired from.
func (w *WsConn) Release() {
	if w.c == nil || w.p == nil {
		return
	}
	w.p.release(w)
	w.c = nil
}
