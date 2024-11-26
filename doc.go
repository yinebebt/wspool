// Package wspool provides a WebSocket client with a connection pool.
/*
 # Overview

 wspool is designed to address challenges associated with managing WebSocket connections in services
 that frequently interact with WebSocket APIs. It allows for dynamic connection pooling,resource optimization,
 and reuse of existing WebSocket connections.

 This package is useful in scenarios where:

   - A single WebSocket connection is insufficient to handle concurrent requests due to high traffic.
   - Creating a new WebSocket connection for every request introduces excessive overhead.

 wspool offers a balanced solution by maintaining a pool of reusable connections
 and dynamically scaling the pool based on usage patterns.

 # Features/Design Consideration

 - **Dynamic Connection Management**: Automatically adjusts the number of active WebSocket connections
   based on the current load.
 - **Connection Pooling**: Maintains a pool of reusable connections, reducing the cost of establishing new connections.
 - **Resource Optimization**: Closes idle connections when traffic decreases, freeing up system resources.
 - **Thread-Safety**: The pool implementation is thread-safe, allowing multiple goroutines to
   use it concurrently.
 - **Flexible Usage**: Can be used either as a pooled client or as a standalone client.

 # Example-Usage

	import (
		"log"
		"github.com/yinebebt/wspool"
		"github.com/gorilla/websocket"
	)

	func main() {
		dialer := websocket.Dialer{}
		config := wspool.Config{
			MaxConn:           4,
			MinConn:           1,
			HealthCheckPeriod: time.Minute,
			Dialer:            &dialer,
			URL: "ws://localhost:6060/channel",
		}
		// Create a new WebSocket pool from config.
		p, err := wspool.New(config)
		if err != nil {
			log.Fatalf("Failed to create WebSocket pool: %v", err)
		}

		// Acquire a connection from the pool.
		c, err := p.Acquire()
		if err != nil {
			log.Fatalf("Failed to get connection from pool: %v", err)
		}

	    defer c.Release()   // Release the connection to the pool.

		// send a message.
		err = c.SendMessage("hi")
		if err != nil {
			log.Fatalf("Failed to send message: %v", err)
		}
		// read response
		res,err := c.ReadMessage()
		// handle response and error
	}

# Inspiration

 The initial design and features of wspool are inspired by [pgxpool](https://github.com/jackc/pgx/v5/pgxpool),
 connection pool for pgx.
*/
package wspool
