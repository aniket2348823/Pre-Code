// Package main provides a tiny health check binary for Docker distroless images.
// It performs a TCP dial to the API server and exits with code 0 (healthy) or 1 (unhealthy).
package main

import (
	"net"
	"os"
	"time"
)

func main() {
	addr := "127.0.0.1:8080"
	if env := os.Getenv("HEALTHCHECK_ADDR"); env != "" {
		addr = env
	}

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		// Exit non-zero without leaking error details (internal IP/port)
		os.Exit(1)
	}
	conn.Close()
}
