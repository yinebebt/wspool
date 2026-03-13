package wspool

import (
	"errors"
	"github.com/gorilla/websocket"
	"sync"
	"time"
)

var errConnClosed = errors.New("connection is closed")

// pooledConn is the internal representation of a pooled WebSocket connection.
type pooledConn struct {
	c          *websocket.Conn
	rMu        sync.Mutex // serializes read operations
	wMu        sync.Mutex // serializes write operations
	createdAt  time.Time
	lastUsedAt time.Time
}

// close closes the underlying WebSocket connection.
// It acquires both I/O mutexes to ensure no in-flight operations race with the close.
func (pc *pooledConn) close() {
	pc.wMu.Lock()
	pc.rMu.Lock()
	if pc.c != nil {
		pc.c.Close()
		pc.c = nil
	}
	pc.rMu.Unlock()
	pc.wMu.Unlock()
}

// WsConn is an acquired connection handle from a Pool.
// Each Acquire returns a new handle; releasing or closing the handle
// detaches it from the underlying connection, making stale aliases inert.
type WsConn struct {
	mu sync.Mutex // protects pc
	pc *pooledConn
	p  *Pool
}

// SendMessage sends a message over the WebSocket connection.
func (w *WsConn) SendMessage(message string) error {
	w.mu.Lock()
	pc := w.pc
	w.mu.Unlock()

	if pc == nil {
		return errConnClosed
	}

	pc.wMu.Lock()
	defer pc.wMu.Unlock()

	if pc.c == nil {
		return errConnClosed
	}

	err := pc.c.WriteMessage(websocket.TextMessage, []byte(message))
	if err == nil {
		pc.lastUsedAt = time.Now()
	}

	return err
}

// SendJSON sends a JSON-encoded message over the WebSocket connection.
func (w *WsConn) SendJSON(v interface{}) error {
	w.mu.Lock()
	pc := w.pc
	w.mu.Unlock()

	if pc == nil {
		return errConnClosed
	}

	pc.wMu.Lock()
	defer pc.wMu.Unlock()

	if pc.c == nil {
		return errConnClosed
	}

	err := pc.c.WriteJSON(v)
	if err == nil {
		pc.lastUsedAt = time.Now()
	}

	return err
}

// ReadMessage reads a response from the WebSocket connection.
func (w *WsConn) ReadMessage() ([]byte, error) {
	w.mu.Lock()
	pc := w.pc
	w.mu.Unlock()

	if pc == nil {
		return nil, errConnClosed
	}

	pc.rMu.Lock()
	defer pc.rMu.Unlock()

	if pc.c == nil {
		return nil, errConnClosed
	}

	_, data, err := pc.c.ReadMessage()
	if err != nil {
		return nil, err
	}

	pc.lastUsedAt = time.Now()
	return data, nil
}

// ReadJSON reads a JSON response from the WebSocket connection and stores in v.
func (w *WsConn) ReadJSON(v interface{}) error {
	w.mu.Lock()
	pc := w.pc
	w.mu.Unlock()

	if pc == nil {
		return errConnClosed
	}

	pc.rMu.Lock()
	defer pc.rMu.Unlock()

	if pc.c == nil {
		return errConnClosed
	}

	if err := pc.c.ReadJSON(v); err != nil {
		return err
	}

	pc.lastUsedAt = time.Now()
	return nil
}

// Close closes the underlying connection and removes it from the pool.
// Returns an error if the handle was already released or closed.
func (w *WsConn) Close() error {
	w.mu.Lock()
	pc := w.pc
	if pc == nil {
		w.mu.Unlock()
		return errors.New("connection is closed or released")
	}
	w.pc = nil
	w.mu.Unlock()

	// Acquire both I/O mutexes to wait for in-flight operations to complete.
	pc.wMu.Lock()
	pc.rMu.Lock()
	c := pc.c
	pc.c = nil
	pc.rMu.Unlock()
	pc.wMu.Unlock()

	var err error
	if c != nil {
		err = c.Close()
	}

	if w.p != nil {
		w.p.lock.Lock()
		w.p.activeConnections--
		w.p.lock.Unlock()
	}
	return err
}

// Release returns the underlying connection to the pool.
// The handle becomes inert after this call.
func (w *WsConn) Release() {
	w.mu.Lock()
	pc := w.pc
	if pc == nil || w.p == nil {
		w.mu.Unlock()
		return
	}
	w.pc = nil
	w.mu.Unlock()

	w.p.release(pc)
}
