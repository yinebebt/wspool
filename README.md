# wspool

WebSocket client with a connection pool

## Overview

wspool is designed to address challenges associated with managing WebSocket connections in services
that frequently interact with WebSocket APIs. It allows for dynamic connection pooling,resource optimization,
and reuse of existing WebSocket connections.

This package is useful in scenarios where:

- A single WebSocket connection is insufficient to handle concurrent requests due to high traffic.
- Creating a new WebSocket connection for every request introduces excessive overhead.

**wspool** offers a balanced solution by maintaining a pool of reusable connections
and dynamically scaling the pool based on usage patterns.

## Design Consideration

- **Dynamic Connection Management**: Automatically adjusts the number of active WebSocket connections
  based on the current load.
- **Connection Pooling**: Maintains a pool of reusable connections, reducing the cost of establishing new connections.
- **Resource Optimization**: Closes idle connections when traffic decreases, freeing up system resources.
- **Thread-Safety**: The pool implementation is thread-safe, allowing multiple goroutines to
  use it concurrently.
- **Flexible Usage**: Can be used either as a pooled client or as a standalone client.

## Example Usage

```go
package main

import (
    "log"
    "time"

    "github.com/gorilla/websocket"
    "github.com/yinebebt/wspool"
)

func main() {
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
        log.Fatalf("create pool: %v", err)
    }
    defer pool.Close()

    // Acquire a connection from the pool.
    conn, err := pool.Acquire()
    if err != nil {
        log.Fatalf("acquire: %v", err)
    }
    defer conn.Release() // returns the connection to the pool when done

    if err := conn.SendMessage("hello"); err != nil {
        log.Fatalf("send: %v", err)
    }

    msg, err := conn.ReadMessage()
    if err != nil {
        log.Fatalf("read: %v", err)
    }
    log.Printf("response: %s", msg)
}
```

## Inspiration

The initial design and features of wspool are inspired by [pgxpool](https://github.com/jackc/pgx/v5/pgxpool),
connection pool for pgx.
