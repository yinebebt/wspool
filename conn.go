package wspool

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WsConn is an acquired conn from a Pool.
type WsConn struct {
	c          *websocket.Conn
	p          *Pool
	mu         sync.Mutex
	createdAt  time.Time
	lastUsedAt time.Time
}

// SendMessage sends a text message over the WebSocket connection.
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

// SendBinary sends a binary message over the WebSocket connection.
func (w *WsConn) SendBinary(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return errors.New("connection is nil")
	}
	w.lastUsedAt = time.Now()
	return w.c.WriteMessage(websocket.BinaryMessage, data)
}

// ReadMessage reads a text message from the WebSocket connection.
func (w *WsConn) ReadMessage() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return nil, errors.New("connection is nil")
	}
	mt, data, err := w.c.ReadMessage()
	if err != nil {
		return nil, err
	}
	if mt != websocket.TextMessage {
		return nil, fmt.Errorf("expected text frame, got %d", mt)
	}
	w.lastUsedAt = time.Now()
	return data, nil
}

// ReadBinary reads a binary message from the WebSocket connection.
func (w *WsConn) ReadBinary() ([]byte, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return nil, errors.New("connection is nil")
	}
	mt, data, err := w.c.ReadMessage()
	if err != nil {
		return nil, err
	}
	if mt != websocket.BinaryMessage {
		return nil, fmt.Errorf("expected binary frame, got %d", mt)
	}
	w.lastUsedAt = time.Now()
	return data, nil
}

// ReadJSON reads a JSON-encoded message from the WebSocket connection into v.
func (w *WsConn) ReadJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return errors.New("connection is nil")
	}
	if err := w.c.ReadJSON(v); err != nil {
		return err
	}
	w.lastUsedAt = time.Now()
	return nil
}

// ping sends a WebSocket ping frame to verify the connection is alive.
// On failure the underlying socket is closed. Updates lastUsedAt on success.
// Must be called without p.lock held: ping acquires w.mu, and the lock
// ordering rule is p.lock → w.mu — never the reverse.
func (w *WsConn) ping() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.c == nil {
		return false
	}
	if err := w.c.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
		w.c.Close()
		w.c = nil
		return false
	}
	w.lastUsedAt = time.Now()
	return true
}

// disconnect closes the underlying socket without touching pool state.
// Pool methods use this when they already hold p.lock and manage activeConnections themselves.
func (w *WsConn) disconnect() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.c != nil {
		w.c.Close()
		w.c = nil
	}
}

// Close closes the underlying connection and removes it from the pool.
// w.mu is released before p.lock is acquired to preserve lock ordering:
// pool internals always acquire p.lock then w.mu (via disconnect),
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
