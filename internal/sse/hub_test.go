package sse

import (
	"strings"
	"testing"
	"time"
)

func TestNewHub(t *testing.T) {
	h := NewHub()
	if h.ClientCount() != 0 {
		t.Errorf("new hub should have 0 clients, got %d", h.ClientCount())
	}
}

func TestHub_BroadcastToClients(t *testing.T) {
	h := NewHub()

	ch := make(chan string, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	h.Broadcast("sync")

	select {
	case msg := <-ch:
		if !strings.Contains(msg, "event: sync") {
			t.Errorf("broadcast message should contain event name, got: %q", msg)
		}
		if !strings.Contains(msg, "data:") {
			t.Errorf("broadcast message should contain data line, got: %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broadcast")
	}
}

func TestHub_BroadcastSkipsSlowClients(t *testing.T) {
	h := NewHub()

	// Channel with no buffer — will be "slow"
	slowCh := make(chan string)
	// Channel with buffer — should receive
	fastCh := make(chan string, 16)

	h.mu.Lock()
	h.clients[slowCh] = struct{}{}
	h.clients[fastCh] = struct{}{}
	h.mu.Unlock()

	h.Broadcast("test")

	select {
	case msg := <-fastCh:
		if !strings.Contains(msg, "event: test") {
			t.Errorf("fast client should receive broadcast, got: %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fast client broadcast")
	}

	// Slow client should not have received (non-blocking send skipped it)
	select {
	case <-slowCh:
		t.Error("slow client should not have received broadcast")
	default:
		// expected
	}
}

func TestHub_ClientCount(t *testing.T) {
	h := NewHub()

	ch1 := make(chan string, 1)
	ch2 := make(chan string, 1)

	h.mu.Lock()
	h.clients[ch1] = struct{}{}
	h.clients[ch2] = struct{}{}
	h.mu.Unlock()

	if h.ClientCount() != 2 {
		t.Errorf("ClientCount() = %d, want 2", h.ClientCount())
	}

	h.mu.Lock()
	delete(h.clients, ch1)
	h.mu.Unlock()

	if h.ClientCount() != 1 {
		t.Errorf("ClientCount() = %d, want 1", h.ClientCount())
	}
}

func TestHub_Shutdown(t *testing.T) {
	h := NewHub()

	ch := make(chan string, 16)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	h.Shutdown()

	// done channel should be closed
	select {
	case <-h.done:
		// expected
	default:
		t.Error("done channel should be closed after Shutdown")
	}

	// Client should have received a reconnect event
	select {
	case msg := <-ch:
		if !strings.Contains(msg, "event: reconnect") {
			t.Errorf("shutdown should send reconnect event, got: %q", msg)
		}
		if !strings.Contains(msg, "retry: 500") {
			t.Errorf("shutdown should include retry directive, got: %q", msg)
		}
	default:
		t.Error("client should have received reconnect event")
	}
}

func TestHub_ShutdownNoPanic_NoClients(t *testing.T) {
	h := NewHub()
	// Should not panic with no clients
	h.Shutdown()

	select {
	case <-h.done:
	default:
		t.Error("done should be closed")
	}
}

func TestHub_BroadcastAfterShutdown_NoPanic(t *testing.T) {
	h := NewHub()
	h.Shutdown()
	// Broadcast after shutdown should not panic (no clients to send to)
	h.Broadcast("test")
}
