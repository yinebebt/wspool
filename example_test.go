package wspool_test

import (
	"context"
	"log"
	"time"

	"github.com/gorilla/websocket"
	"github.com/yinebebt/wspool"
)

// ExampleNew demonstrates pool creation and shutdown.
func ExampleNew() {
	pool, err := wspool.New(wspool.Config{
		Dialer:            &websocket.Dialer{},
		URL:               "ws://localhost:6060/ws",
		MinConn:           2,
		MaxConn:           10,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   5 * time.Minute,
		HealthCheckPeriod: 30 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
}

// ExamplePool_Acquire demonstrates the acquire-use-release pattern
// for both text and JSON messages.
func ExamplePool_Acquire() {
	pool, err := wspool.New(wspool.Config{
		Dialer:            &websocket.Dialer{},
		URL:               "ws://localhost:6060/ws",
		MaxConn:           10,
		HealthCheckPeriod: time.Minute,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Release()

	if err := conn.SendMessage("hello"); err != nil {
		log.Fatal(err)
	}
	msg, err := conn.ReadMessage()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("text: %s", msg)

	type Event struct {
		Name string `json:"name"`
	}
	if err := conn.SendJSON(Event{Name: "ping"}); err != nil {
		log.Fatal(err)
	}
	var resp Event
	if err := conn.ReadJSON(&resp); err != nil {
		log.Fatal(err)
	}
	log.Printf("json: %s", resp.Name)
}
