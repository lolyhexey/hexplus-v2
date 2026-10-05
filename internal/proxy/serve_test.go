package proxy

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

// Stopping a proxy waited for every live session: a client that had
// connected and sent nothing held the stop for the 60 s header timeout, and
// a tunnelled session held it until systemd killed the unit.
func TestServeReturnsPromptlyWithALiveSession(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	h, err := NewHandler(Config{Name: "t", Port: port, DefaultHost: "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()

	var c net.Conn
	for i := 0; i < 50; i++ { // wait for the listener
		if c, err = net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatalf("proxy did not listen: %v", err)
	}
	defer c.Close()
	time.Sleep(100 * time.Millisecond) // the session is now in handleConn

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return within 3 s while a client was connected")
	}
}
