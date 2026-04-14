package wspool

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// newEchoServer starts a local WebSocket echo server and returns its ws:// URL.
func newEchoServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				break
			}
			if err = conn.WriteMessage(mt, msg); err != nil {
				break
			}
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

// newPool creates a pool pointed at url with test-safe defaults and registers cleanup.
func newPool(t *testing.T, url string, cfg Config) *Pool {
	t.Helper()
	cfg.Dialer = websocket.DefaultDialer
	cfg.URL = url
	if cfg.HealthCheckPeriod == 0 {
		cfg.HealthCheckPeriod = time.Hour
	}
	p, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// idleCount returns the number of idle connections currently held by the pool.
func idleCount(p *Pool) int {
	p.lock.Lock()
	defer p.lock.Unlock()
	return len(p.conns)
}

func TestNew_InvalidConfig(t *testing.T) {
	url := newEchoServer(t)
	cases := []struct {
		name string
		cfg  Config
	}{
		{"missing dialer", Config{URL: url, MaxConn: 1, HealthCheckPeriod: time.Second}},
		{"missing URL", Config{Dialer: websocket.DefaultDialer, MaxConn: 1, HealthCheckPeriod: time.Second}},
		{"zero HealthCheckPeriod", Config{Dialer: websocket.DefaultDialer, URL: url, MaxConn: 1}},
		{"zero MaxConn", Config{Dialer: websocket.DefaultDialer, URL: url, HealthCheckPeriod: time.Second}},
		{"MinConn > MaxConn", Config{Dialer: websocket.DefaultDialer, URL: url, MinConn: 5, MaxConn: 2, HealthCheckPeriod: time.Second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.cfg); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestAcquire_RespectsMaxConnAndCounter(t *testing.T) {
	url := newEchoServer(t)
	const max = 2
	p := newPool(t, url, Config{MaxConn: max})

	c1, err := p.Acquire()
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	c2, err := p.Acquire()
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}

	p.lock.Lock()
	if got := p.activeConnections; got != max {
		t.Errorf("activeConnections = %d, want %d", got, max)
	}
	p.lock.Unlock()

	if _, err := p.Acquire(); err == nil {
		t.Fatal("expected error when pool is exhausted")
	}

	c1.Release()
	c2.Release()
}

func TestAcquire_UpdatesLastUsedAt(t *testing.T) {
	url := newEchoServer(t)
	p := newPool(t, url, Config{MinConn: 1, MaxConn: 2})

	p.lock.Lock()
	before := p.conns[0].lastUsedAt
	p.lock.Unlock()

	time.Sleep(5 * time.Millisecond)

	conn, err := p.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	if !conn.lastUsedAt.After(before) {
		t.Error("lastUsedAt was not refreshed on Acquire")
	}
}

func TestRelease_ReturnsToPool(t *testing.T) {
	url := newEchoServer(t)
	p := newPool(t, url, Config{MaxConn: 2})

	conn, err := p.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	conn.Release()

	if got := idleCount(p); got != 1 {
		t.Errorf("idle count after Release = %d, want 1", got)
	}
}

func TestSendReceive(t *testing.T) {
	url := newEchoServer(t)
	p := newPool(t, url, Config{MaxConn: 1})

	conn, err := p.Acquire()
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer conn.Release()

	t.Run("text", func(t *testing.T) {
		const want = "hello"
		if err := conn.SendMessage(want); err != nil {
			t.Fatalf("SendMessage: %v", err)
		}
		got, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage: %v", err)
		}
		if string(got) != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("json", func(t *testing.T) {
		type msg struct {
			Key string `json:"key"`
		}
		if err := conn.SendJSON(msg{"value"}); err != nil {
			t.Fatalf("SendJSON: %v", err)
		}
		var got msg
		if err := conn.ReadJson(&got); err != nil {
			t.Fatalf("ReadJson: %v", err)
		}
		if got.Key != "value" {
			t.Errorf("got %+v, want key=value", got)
		}
	})
}

func TestClose_Idempotent(t *testing.T) {
	url := newEchoServer(t)
	p := newPool(t, url, Config{MinConn: 2, MaxConn: 4})
	p.Close()
	p.Close() // must not panic
	if p.conns != nil {
		t.Errorf("conns not nil after Close, len=%d", len(p.conns))
	}
}

func TestHealthCheck_Eviction(t *testing.T) {
	url := newEchoServer(t)

	t.Run("idle timeout", func(t *testing.T) {
		p, err := New(Config{
			Dialer:            websocket.DefaultDialer,
			URL:               url,
			MaxConn:           3,
			MaxConnIdleTime:   50 * time.Millisecond,
			HealthCheckPeriod: 20 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer p.Close()

		// Acquire all at once so the health check cannot evict between iterations.
		conns := make([]*WsConn, 3)
		for i := range conns {
			if conns[i], err = p.Acquire(); err != nil {
				t.Fatalf("Acquire #%d: %v", i+1, err)
			}
		}
		for _, c := range conns {
			c.Release()
		}
		time.Sleep(200 * time.Millisecond)
		if got := idleCount(p); got != 0 {
			t.Errorf("idle = %d after idle timeout, want 0", got)
		}
	})

	t.Run("lifetime expiry", func(t *testing.T) {
		p, err := New(Config{
			Dialer:            websocket.DefaultDialer,
			URL:               url,
			MaxConn:           2,
			MaxConnLifetime:   50 * time.Millisecond,
			HealthCheckPeriod: 20 * time.Millisecond,
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		defer p.Close()

		c, err := p.Acquire()
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		c.Release()
		time.Sleep(200 * time.Millisecond)
		if got := idleCount(p); got != 0 {
			t.Errorf("idle = %d after lifetime expiry, want 0", got)
		}
	})
}
