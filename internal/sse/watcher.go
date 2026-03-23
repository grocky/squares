package sse

import (
	"context"
	"log"
	"time"

	"github.com/grocky/squares/internal/dynamo"
)

// WatchSyncState polls DynamoDB for changes to the sync timestamp and
// broadcasts a "sync" event to all SSE clients when a new sync is detected.
// This replaces the Lambda → HTTP broadcast call, avoiding the need for
// Lambda egress or a public endpoint reachable from within AWS.
//
// Call this in a goroutine; it exits when ctx is cancelled.
func WatchSyncState(ctx context.Context, repo *dynamo.Repo, hub *Hub, poolID string, interval time.Duration) {
	log.Printf("SSE sync watcher started (pool=%s interval=%s)", poolID, interval)

	var lastSeen time.Time
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("SSE sync watcher stopped")
			return
		case <-ticker.C:
			t, err := repo.GetSyncState(ctx, poolID)
			if err != nil {
				log.Printf("sync watcher: error reading sync state: %v", err)
				continue
			}
			if t.IsZero() || !t.After(lastSeen) {
				continue
			}
			lastSeen = t
			hub.SetLastSyncTime(t)

			log.Printf("sync watcher: new sync detected at %s — broadcasting", t.Format(time.RFC3339))
			hub.Broadcast("sync")
		}
	}
}
