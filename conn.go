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
	return data, nil
}

// ReadJson reads a response as Json from the WebSocket connection and store in v.
func (w *WsConn) ReadJson(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.c.ReadJSON(v)
}

// Close closes w and removes it from the pool.
func (w *WsConn) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return errors.New("connection is nil")
	}
	err := w.c.Close()
	w.c = nil
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
