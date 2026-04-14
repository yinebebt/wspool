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

// SendMessage sends a message over the WebSocket connection.
func (w *WsConn) SendMessage(message string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return errors.New("connection is nil")
	}
	w.lastUsedAt = time.Now()
	return w.c.WriteMessage(websocket.TextMessage, []byte(message))
}

// SendJSON sends a JSON-encoded message over the WebSocket connection.
func (w *WsConn) SendJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return errors.New("connection is nil")
	}
	w.lastUsedAt = time.Now()
	return w.c.WriteJSON(v)
}

// ReadMessage reads a response from the WebSocket connection.
func (w *WsConn) ReadMessage() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, data, err := w.c.ReadMessage()
	if err != nil {
		return nil, err
	}
	w.lastUsedAt = time.Now()
	return data, nil
}

// ReadJson reads a response as Json from the WebSocket connection and store in v.
func (w *WsConn) ReadJson(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.c.ReadJSON(v); err != nil {
		return err
	}
	w.lastUsedAt = time.Now()
	return nil
}

// closeInternal closes the underlying WebSocket connection without touching pool state.
// Pool methods use this when they already hold p.lock to avoid re-entrant locking.
func (w *WsConn) closeInternal() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.c != nil {
		w.c.Close()
		w.c = nil
	}
}

// Close closes the underlying connection and removes it from the pool.
// w.mu is released before p.lock is acquired to preserve lock ordering:
// pool internals always acquire p.lock then w.mu (via closeInternal),
// never the reverse.
func (w *WsConn) Close() error {
	w.mu.Lock()
	if w.c == nil {
		w.mu.Unlock()
		return errors.New("connection is nil")
	}
	err := w.c.Close()
	w.c = nil
	w.mu.Unlock()

	if w.p != nil {
		w.p.lock.Lock()
		w.p.activeConnections--
		w.p.lock.Unlock()
	}
	return err
}

// Release returns w to the pool it was acquired from.
// The caller must not use w after calling Release.
func (w *WsConn) Release() {
	if w.p == nil {
		return
	}
	w.p.release(w)
}
