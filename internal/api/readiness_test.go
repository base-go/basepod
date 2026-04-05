package api

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestWaitForLocalPortSucceedsWhenListenerStarts(t *testing.T) {
	t.Parallel()

	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	port := reserved.Addr().(*net.TCPAddr).Port
	if err := reserved.Close(); err != nil {
		t.Fatalf("failed to release reserved port: %v", err)
	}

	ready := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return
		}
		defer ln.Close()
		close(ready)
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := waitForLocalPort(ctx, port); err != nil {
		t.Fatalf("waitForLocalPort returned error: %v", err)
	}

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("listener never started")
	}
}

func TestWaitForLocalPortTimesOut(t *testing.T) {
	t.Parallel()

	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve port: %v", err)
	}
	port := reserved.Addr().(*net.TCPAddr).Port
	if err := reserved.Close(); err != nil {
		t.Fatalf("failed to release reserved port: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if err := waitForLocalPort(ctx, port); err == nil {
		t.Fatal("expected waitForLocalPort to time out")
	}
}
