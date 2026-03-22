# Squares Refactor Plan

## Context

The squares app stores game data (ESPN scores) pool-scoped, causing redundant DynamoDB writes when multiple pools exist. The `buildDashboardData()` function is called from 7 handler sites and executes up to 17 DynamoDB ops per call — 51 ops on each SSE sync refresh cycle. This plan decouples global game data from pool-scoped data and introduces targeted data loaders to minimise query overhead.

---

## Step 0 — Test coverage for all internal packages

**Goal:** Establish a test baseline before any structural changes. All subsequent steps must maintain passing tests.

**Scope:**
- `internal/espn` — `roundFromHeadline`, `normalizeStatus`, `dateRange`, `currentRoundEndDate`, `FetchGames` (mock HTTP)
- `internal/scorer` — `FindWinningSquare` with axis/score combinations
- `internal/syncer` — `Sync` with mock repo + ESPN client; verify payout creation, deduplication, sync state write
- `internal/sse` — `Hub`: Broadcast, Shutdown, channel ownership (no double-close), watcher poll logic
- `internal/api` — `currentRound` helper, `parseRoundFilter`
- `internal/version` — `Get` with/without ldflag commit set

**Requirements:**
- Use `testing` + `testify/assert` (already a dep if present, otherwise add it)
- Mock external dependencies (DynamoDB, HTTP) — no live calls in tests
- `go test ./...` must pass with 0 failures before committing
- Commit message: `test: add baseline test coverage for all internal packages`

---

## Step 1 — Global game storage

**Goal:** Decouple game data from pool partitions so the cron writes once globally regardless of pool count.

### Data model change

Move games from `PK=POOL#<id> / SK=GAME#<espnID>` to a dedicated global partition:

```
PK=GAMES / SK=GAME#<espnID>
```

Payouts remain pool-scoped (`PK=POOL#<id> / SK=PAYOUT#...`) and reference games by `EspnID`.

### Changes required

**`internal/models/models.go`**
- Remove `PoolID` field from `Game` struct (it's now global)

**`internal/dynamo/repo.go`**
- Add `PutGameGlobal(ctx, game)` — writes to `PK=GAMES`
- Add `GetAllGamesGlobal(ctx)` — queries `PK=GAMES / begins_with(SK, "GAME#")`
- Keep old `PutGame` / `GetAllGames` as deprecated shims that delegate to global methods (for rollback safety); remove in a follow-up
- Add `GetAllRoundAxes(ctx, poolID)` — single Query on `begins_with(SK, "AXIS#")` replacing the 12 serial GetItem calls

**`internal/espn/client.go`**
- `SyncGames` no longer takes `poolID`; writes to global partition

**`internal/syncer/syncer.go`**
- `Sync(ctx, poolID)` calls `espnClient.SyncGames(ctx)` (no poolID)
- Reads games from global partition for payout computation
- `PutSyncState` remains pool-scoped (each pool can track its own sync)

**`cmd/cron/main.go`**
- No change needed (syncer interface unchanged)

### Tests required
- `GetAllGamesGlobal` returns games from `PK=GAMES`
- `PutGameGlobal` writes correct PK/SK
- `Sync` with mock global game store computes payouts correctly
- Verify old pool-scoped game reads return empty (migration boundary)

**Commit:** `refactor: move game storage to global partition, decouple from pool`

---

## Step 2 — Batch axis fetch

**Goal:** Replace 12 serial `GetRoundAxis` GetItem calls in `buildDashboardData` with a single Query.

### Changes required

**`internal/dynamo/repo.go`**
- Add `GetAllRoundAxes(ctx, poolID) ([]models.Axis, error)` — Query `PK=POOL#<id>` with `begins_with(SK, "AXIS#")`
- Ensure SK format is `AXIS#<roundNum>#<type>` (verify existing format, update if needed)

**`internal/api/handlers.go`**
- Replace the `for roundNum := 1; roundNum <= 6` loop in `buildDashboardData` with a single `GetAllRoundAxes` call
- Build the `roundAxes` slice from the batch result

### Tests required
- `GetAllRoundAxes` returns correct axes from a mock DynamoDB response
- `buildDashboardData` axis section calls `GetAllRoundAxes` exactly once (verify via mock call count)

**Commit:** `perf: batch axis fetch — replace 12 serial GetItem calls with single Query`

---

## Step 3 — Targeted partial data loaders

**Goal:** Replace all 7 `buildDashboardData` calls with purpose-built loaders that only fetch what each handler actually needs.

### Query budget per handler (after refactor)

| Handler | Loader | DynamoDB ops |
|---------|--------|-------------|
| `handlePoolDashboard` | `loadFullDashboard` | ~5 (pool + configs + axes batch + squares + payouts + games) |
| `handleAdminDashboard` | `loadFullDashboard` | ~5 |
| `handleGrid` | `loadGridData(poolID, round)` | 3 (axes batch + squares + payouts filtered) |
| `handleLeaderboard` | `loadLeaderboardData(poolID)` | 1 (payouts only) |
| `handleGames` | `loadGamesData(round)` | 1 (global games filtered by round) |
| `handleUpdateSquare` | `loadGridData(poolID, round)` | 3 |
| `handleUpdateRoundAxis` | `loadGridData(poolID, round)` | 3 |
| **SSE sync (×3 partials)** | mixed | **5 total** (was 51) |

### New loader functions (in `internal/api/loaders.go`)

```go
// Full page load — used by handlePoolDashboard, handleAdminDashboard
func (h *Handler) loadFullDashboard(ctx, poolID, roundFilter) (dashboardData, error)

// Grid partial — axes + squares + payouts for selected round
func (h *Handler) loadGridData(ctx, poolID, roundFilter) (dashboardData, error)

// Leaderboard partial — payouts only (all rounds)
func (h *Handler) loadLeaderboardData(ctx, poolID) (dashboardData, error)

// Games partial — global games filtered by round
func (h *Handler) loadGamesData(ctx, round) (dashboardData, error)
```

### Changes required
- Create `internal/api/loaders.go` with the four loader functions
- Update each handler to call the appropriate loader instead of `buildDashboardData`
- Delete `buildDashboardData` once all call sites are migrated
- `dashboardData` struct may need trimming — leaderboard loader doesn't need grid fields, etc. Consider splitting or using a common base + extensions

### Tests required
- Each loader calls only the expected repo methods (verify via mock)
- `handleLeaderboard` does NOT call `GetAllSquares` or `GetAllRoundAxes`
- `handleGames` does NOT call `GetAllPayouts` or `GetAllSquares`
- Round filter correctly scopes game/payout results in each loader

**Commit:** `perf: replace buildDashboardData with targeted partial loaders`

---

## Step 4 — Server-side pool cache

**Goal:** Cache pool metadata (pool record, round configs, axes) in-memory since these change only on admin writes — eliminating their DB reads on every SSE refresh.

### Design

```go
type poolCacheEntry struct {
    pool         models.Pool
    roundConfigs []models.RoundConfig
    roundAxes    []models.Axis
    cachedAt     time.Time
}

type poolCache struct {
    mu      sync.RWMutex
    entries map[string]poolCacheEntry
    ttl     time.Duration // default: 60s
}
```

- Cache is populated on first load per pool
- Invalidated (deleted from map) on any admin write: `handleUpdatePool`, `handleUpdateRoundConfig`, `handleUpdateRoundAxis`
- TTL provides safety net — stale cache auto-expires even if invalidation is missed
- Cache lives on `Handler` struct

### Changes required
- Add `poolCache` type to `internal/api/cache.go`
- Inject into `Handler` struct, initialised in `NewHandler`
- Loaders check cache before repo calls for pool/config/axes data
- Admin write handlers call `h.cache.Invalidate(poolID)` after successful writes
- Cache is not used in admin dashboard (always fresh reads for admin)

### Tests required
- Cache hit avoids repo call
- Cache miss fetches and stores
- Invalidation on admin write causes next load to re-fetch
- TTL expiry causes re-fetch

**Commit:** `feat: add server-side pool metadata cache to reduce DynamoDB reads`

---

## General Requirements (all steps)

- `go test ./...` must pass before every commit
- `go build ./...` must pass before every commit
- `go vet ./...` must pass before every commit
- No regressions to existing functionality
- Each commit must be atomic and self-contained (no broken intermediate states)
- Use conventional commit format: `type: description`
