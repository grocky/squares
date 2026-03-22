package sse

import (
	"strings"
	"testing"
	"time"
)

// WatchSyncState takes a concrete *dynamo.Repo — it cannot be tested with a
// mock without an interface change. The Hub broadcast path is covered in
// hub_test.go. This file tests the integration of Broadcast with a registered
// client to cover the codepath WatchSyncState would trigger.

func TestHub_BroadcastSync(t *testing.T) {
	hub := NewHub()
	ch := make(chan string, 16)
	hub.mu.Lock()
	hub.clients[ch] = struct{}{}
	hub.mu.Unlock()

	hub.Broadcast("sync")

	select {
	case msg := <-ch:
		if !strings.Contains(msg, "event: sync") {
			t.Errorf("expected sync event, got: %q", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out")
	}
}
