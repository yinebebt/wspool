package wspool

import (
	"github.com/gorilla/websocket"
	"log"
	"time"
)

func main() {
	dialer := websocket.Dialer{}
	config := Config{
		MaxConn:           4,
		MinConn:           1,
		HealthCheckPeriod: time.Minute,
		Dialer:            &dialer,
		URL:               "ws://localhost:6060/channel",
	}
	// Create a new WebSocket pool from config.
	p, err := New(config)
	if err != nil {
		log.Fatalf("Failed to create WebSocket pool: %v", err)
	}

	c, err := p.Acquire()
	if err != nil {
		log.Fatalf("Failed to get connection from pool: %v", err)
	}

	defer c.Release()

	err = c.SendMessage("hi")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}

	_, err = c.ReadMessage()
	if err != nil {
		log.Fatalf("Failed to read message: %v", err)
	}
	//todo: handle response
}
