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
	mu         sync.Mutex // protects c, lastUsedAt, released
	rMu        sync.Mutex // serializes read operations
	wMu        sync.Mutex // serializes write operations
	createdAt  time.Time
	lastUsedAt time.Time
	released   bool
}

// SendMessage sends a message over the WebSocket connection.
func (w *WsConn) SendMessage(message string) error {
	w.wMu.Lock()
	defer w.wMu.Unlock()

	w.mu.Lock()
	c := w.c
	w.mu.Unlock()

	if c == nil {
		return errors.New("connection is closed")
	}

	err := c.WriteMessage(websocket.TextMessage, []byte(message))

	w.mu.Lock()
	w.lastUsedAt = time.Now()
	w.mu.Unlock()

	return err
}

// SendJSON sends a JSON-encoded message over the WebSocket connection.
func (w *WsConn) SendJSON(v interface{}) error {
	w.wMu.Lock()
	defer w.wMu.Unlock()

	w.mu.Lock()
	c := w.c
	w.mu.Unlock()

	if c == nil {
		return errors.New("connection is closed")
	}

	err := c.WriteJSON(v)

	w.mu.Lock()
	w.lastUsedAt = time.Now()
	w.mu.Unlock()

	return err
}

// ReadMessage reads a response from the WebSocket connection.
func (w *WsConn) ReadMessage() ([]byte, error) {
	w.rMu.Lock()
	defer w.rMu.Unlock()

	w.mu.Lock()
	c := w.c
	w.mu.Unlock()

	if c == nil {
		return nil, errors.New("connection is closed")
	}

	_, data, err := c.ReadMessage()
	if err != nil {
		return nil, err
	}

	w.mu.Lock()
	w.lastUsedAt = time.Now()
	w.mu.Unlock()

	return data, nil
}

// ReadJSON reads a JSON response from the WebSocket connection and stores in v.
func (w *WsConn) ReadJSON(v interface{}) error {
	w.rMu.Lock()
	defer w.rMu.Unlock()

	w.mu.Lock()
	c := w.c
	w.mu.Unlock()

	if c == nil {
		return errors.New("connection is closed")
	}

	if err := c.ReadJSON(v); err != nil {
		return err
	}

	w.mu.Lock()
	w.lastUsedAt = time.Now()
	w.mu.Unlock()

	return nil
}

// Close closes w's underlying connection. Returns an error if the connection
// was already released back to the pool or already closed.
func (w *WsConn) Close() error {
	w.mu.Lock()
	if w.c == nil {
		w.mu.Unlock()
		return errors.New("connection is closed")
	}
	if w.released {
		w.mu.Unlock()
		return errors.New("connection was released back to pool")
	}
	c := w.c
	w.c = nil
	w.mu.Unlock()

	err := c.Close()

	if w.p != nil {
		w.p.lock.Lock()
		w.p.activeConnections--
		w.p.lock.Unlock()
	}
	return err
}

// close is an internal method used by the pool when it already holds the pool lock.
func (w *WsConn) close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c != nil {
		w.c.Close()
		w.c = nil
	}
}

// Release returns w to the pool it was acquired from.
func (w *WsConn) Release() {
	w.mu.Lock()
	if w.c == nil || w.p == nil || w.released {
		w.mu.Unlock()
		return
	}
	w.released = true
	w.mu.Unlock()

	w.p.release(w)
}
