package api

import (
	"context"
	"testing"
	"time"

	"github.com/grocky/squares/internal/models"
)

func TestCacheHitAvoidsRepoCall(t *testing.T) {
	repo := newMockRepo()
	repo.pool = models.Pool{ID: "p1", Name: "Test"}
	repo.roundConfigs = []models.RoundConfig{{PoolID: "p1", RoundNum: 1, Name: "Round of 64"}}
	repo.axes = []models.Axis{
		{PoolID: "p1", RoundNum: 1, Type: "winner", Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{PoolID: "p1", RoundNum: 1, Type: "loser", Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
	}
	h := newTestHandler(repo)

	// First call — cache miss, fetches from repo
	pool, configs, axes := h.loadPoolMetadata(context.Background(), "p1", true)
	if pool.ID != "p1" {
		t.Fatalf("pool.ID = %q, want p1", pool.ID)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %d, want 1", len(configs))
	}
	if len(axes) != 2 {
		t.Fatalf("axes = %d, want 2", len(axes))
	}
	if repo.called("GetPool") != 1 {
		t.Errorf("GetPool called %d times on miss, want 1", repo.called("GetPool"))
	}

	// Second call — cache hit, should NOT call repo again
	pool2, configs2, axes2 := h.loadPoolMetadata(context.Background(), "p1", true)
	if pool2.ID != "p1" {
		t.Fatalf("pool2.ID = %q, want p1", pool2.ID)
	}
	if len(configs2) != 1 {
		t.Fatalf("configs2 = %d, want 1", len(configs2))
	}
	if len(axes2) != 2 {
		t.Fatalf("axes2 = %d, want 2", len(axes2))
	}
	if repo.called("GetPool") != 1 {
		t.Errorf("GetPool called %d times after cache hit, want 1 (no new call)", repo.called("GetPool"))
	}
	if repo.called("GetAllRoundConfigs") != 1 {
		t.Errorf("GetAllRoundConfigs called %d times after cache hit, want 1", repo.called("GetAllRoundConfigs"))
	}
	if repo.called("GetAllRoundAxes") != 1 {
		t.Errorf("GetAllRoundAxes called %d times after cache hit, want 1", repo.called("GetAllRoundAxes"))
	}
}

func TestCacheMissFetchesAndPopulates(t *testing.T) {
	repo := newMockRepo()
	repo.pool = models.Pool{ID: "p1", Name: "Test"}
	h := newTestHandler(repo)

	// Verify cache is empty
	if _, ok := h.cache.get("p1"); ok {
		t.Fatal("cache should be empty initially")
	}

	// Load with cache enabled — triggers fetch and store
	h.loadPoolMetadata(context.Background(), "p1", true)

	// Verify cache is now populated
	entry, ok := h.cache.get("p1")
	if !ok {
		t.Fatal("cache should be populated after loadPoolMetadata")
	}
	if entry.pool.ID != "p1" {
		t.Errorf("cached pool.ID = %q, want p1", entry.pool.ID)
	}
}

func TestExpiredCacheEntryReRefetches(t *testing.T) {
	repo := newMockRepo()
	repo.pool = models.Pool{ID: "p1", Name: "Test"}
	h := &Handler{repo: repo, cache: newPoolCache(0)} // TTL=0 means always expired

	// First call — miss
	h.loadPoolMetadata(context.Background(), "p1", true)
	if repo.called("GetPool") != 1 {
		t.Fatalf("GetPool called %d times, want 1", repo.called("GetPool"))
	}

	// Second call — entry exists but expired, should re-fetch
	h.loadPoolMetadata(context.Background(), "p1", true)
	if repo.called("GetPool") != 2 {
		t.Errorf("GetPool called %d times after expiry, want 2", repo.called("GetPool"))
	}
}

func TestInvalidationCausesRefetch(t *testing.T) {
	repo := newMockRepo()
	repo.pool = models.Pool{ID: "p1", Name: "Test"}
	h := newTestHandler(repo)

	// Populate cache
	h.loadPoolMetadata(context.Background(), "p1", true)
	if repo.called("GetPool") != 1 {
		t.Fatalf("GetPool called %d times, want 1", repo.called("GetPool"))
	}

	// Invalidate
	h.cache.invalidate("p1")

	// Next call should re-fetch
	h.loadPoolMetadata(context.Background(), "p1", true)
	if repo.called("GetPool") != 2 {
		t.Errorf("GetPool called %d times after invalidation, want 2", repo.called("GetPool"))
	}
}

func TestAdminDashboardBypassesCache(t *testing.T) {
	repo := newMockRepo()
	repo.pool = models.Pool{ID: "p1", Name: "Test"}
	h := newTestHandler(repo)

	// Pre-populate cache
	h.loadPoolMetadata(context.Background(), "p1", true)
	if repo.called("GetPool") != 1 {
		t.Fatalf("GetPool called %d times, want 1", repo.called("GetPool"))
	}

	// Admin dashboard uses useCache=false — should always hit repo
	h.loadFullDashboard(context.Background(), "p1", 0, false)
	if repo.called("GetPool") != 2 {
		t.Errorf("GetPool called %d times, want 2 (admin bypasses cache)", repo.called("GetPool"))
	}
	if repo.called("GetAllRoundConfigs") != 2 {
		t.Errorf("GetAllRoundConfigs called %d times, want 2 (admin bypasses cache)", repo.called("GetAllRoundConfigs"))
	}
	if repo.called("GetAllRoundAxes") != 2 {
		t.Errorf("GetAllRoundAxes called %d times, want 2 (admin bypasses cache)", repo.called("GetAllRoundAxes"))
	}
}

func TestCacheGetSetInvalidate(t *testing.T) {
	c := newPoolCache(time.Minute)

	// Empty cache
	if _, ok := c.get("x"); ok {
		t.Error("get on empty cache should return false")
	}

	// Set and get
	entry := poolCacheEntry{
		pool: models.Pool{ID: "x", Name: "Test"},
	}
	c.set("x", entry)

	got, ok := c.get("x")
	if !ok {
		t.Fatal("get after set should return true")
	}
	if got.pool.ID != "x" {
		t.Errorf("pool.ID = %q, want x", got.pool.ID)
	}

	// Invalidate
	c.invalidate("x")
	if _, ok := c.get("x"); ok {
		t.Error("get after invalidate should return false")
	}
}
