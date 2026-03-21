package sse

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type Hub struct {
	mu      sync.RWMutex
	clients map[chan string]struct{}
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan string]struct{}),
	}
}

// ClientCount returns the number of currently connected SSE clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Broadcast sends an event to all connected clients.
func (h *Hub) Broadcast(event string) {
	data := fmt.Sprintf("event: %s\ndata: {\"time\":\"%s\"}\n\n", event, time.Now().UTC().Format(time.RFC3339))
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
			// skip slow clients
		}
	}
}

// Handler returns an HTTP handler that streams SSE events to clients.
func (h *Hub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := make(chan string, 16)
		h.mu.Lock()
		h.clients[ch] = struct{}{}
		h.mu.Unlock()

		defer func() {
			h.mu.Lock()
			delete(h.clients, ch)
			h.mu.Unlock()
			close(ch)
		}()

		// Send initial connected event
		fmt.Fprintf(w, "event: connected\ndata: {\"time\":\"%s\"}\n\n", time.Now().UTC().Format(time.RFC3339))
		flusher.Flush()

		log.Printf("SSE client connected (total=%d ip=%s)", h.ClientCount(), r.RemoteAddr)

		for {
			select {
			case <-r.Context().Done():
				log.Printf("SSE client disconnected (total=%d)", h.ClientCount())
				return
			case msg := <-ch:
				fmt.Fprint(w, msg)
				flusher.Flush()
			}
		}
	}
}
