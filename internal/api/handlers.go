package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/grocky/squares/internal/espn"
	"github.com/grocky/squares/internal/models"
	"github.com/grocky/squares/internal/sse"
	"github.com/grocky/squares/internal/syncer"
)

// repository defines the data access methods used by handlers and loaders.
type repository interface {
	GetPool(ctx context.Context, poolID string) (models.Pool, error)
	PutPool(ctx context.Context, pool models.Pool) error
	GetAllRoundConfigs(ctx context.Context, poolID string) ([]models.RoundConfig, error)
	GetRoundConfig(ctx context.Context, poolID string, roundNum int) (models.RoundConfig, error)
	PutRoundConfig(ctx context.Context, rc models.RoundConfig) error
	GetAllRoundAxes(ctx context.Context, poolID string) ([]models.Axis, error)
	GetRoundAxis(ctx context.Context, poolID string, roundNum int, axisType string) (models.Axis, error)
	PutRoundAxis(ctx context.Context, axis models.Axis) error
	GetAllSquares(ctx context.Context, poolID string) ([]models.Square, error)
	PutSquare(ctx context.Context, sq models.Square) error
	GetAllGamesGlobal(ctx context.Context) ([]models.Game, error)
	GetAllPayouts(ctx context.Context, poolID string) ([]models.Payout, error)
}

type Handler struct {
	repo       repository
	espnClient *espn.Client
	templates  *template.Template
	syncer     *syncer.Syncer
	hub        *sse.Hub
	version    string
}

type HandlerConfig struct {
	Repo       repository
	EspnClient *espn.Client
	TemplateFS fs.FS
	Syncer     *syncer.Syncer
	Hub        *sse.Hub
	Version    string
}

func NewHandler(config HandlerConfig) *Handler {
	funcMap := template.FuncMap{
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"printf": fmt.Sprintf,
		"formatTime": func(t time.Time, layout string) string {
			if t.IsZero() {
				return ""
			}
			return t.In(time.FixedZone("EDT", -4*60*60)).Format(layout)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"contains": strings.Contains,
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(config.TemplateFS, "templates/*.html"))
	return &Handler{
		repo:       config.Repo,
		espnClient: config.EspnClient,
		templates:  tmpl,
		syncer:     config.Syncer,
		hub:        config.Hub,
		version:    config.Version,
	}
}

func (h *Handler) Routes() *chi.Mux {
	r := chi.NewRouter()
	r.Use(RecoveryMiddleware)
	r.Use(LoggingMiddleware)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	r.Get("/", h.handleIndex)
	r.Post("/pools", h.handleCreatePool)
	r.Get("/pools/{poolID}", h.handlePoolDashboard)
	r.Get("/pools/{poolID}/grid", h.handleGrid)
	r.Get("/pools/{poolID}/leaderboard", h.handleLeaderboard)
	r.Get("/pools/{poolID}/games", h.handleGames)
	r.Post("/pools/{poolID}/sync", h.handleSync)
	r.Get("/pools/{poolID}/events", h.hub.Handler())
	r.Post("/pools/{poolID}/squares", h.handleAssignSquares)
	r.Post("/pools/{poolID}/axes", h.handleAssignAxes)

	r.Put("/pools/{poolID}", h.handleUpdatePool)
	r.Put("/pools/{poolID}/squares/{row}/{col}", h.handleUpdateSquare)
	r.Put("/pools/{poolID}/rounds/{roundNum}/axis/{type}", h.handleUpdateRoundAxis)
	r.Put("/pools/{poolID}/rounds/{roundNum}/config", h.handleUpdateRoundConfig)
	r.Get("/pools/{poolID}/header", h.handleHeader)

	// Admin login/logout (no auth required)
	r.Get("/admin/login", h.handleAdminLogin)
	r.Post("/admin/login", h.handleAdminLoginPost)
	r.Get("/admin/logout", h.handleAdminLogout)

	// Admin area (auth required)
	r.Group(func(r chi.Router) {
		r.Use(AdminAuthMiddleware)
		r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/admin/pools/main", http.StatusFound)
		})
		r.Get("/admin/pools/{poolID}", h.handleAdminDashboard)
	})

	return r
}

func (h *Handler) handleIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/pools/main", http.StatusFound)
}

func (h *Handler) handleCreatePool(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")

	pool := models.Pool{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:      name,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	if err := h.repo.PutPool(r.Context(), pool); err != nil {
		http.Error(w, "failed to create pool", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/pools/"+pool.ID, http.StatusFound)
}

type gridCell struct {
	OwnerName string
	IsWinner  bool
	Amount    float64
}

type roundAxisPair struct {
	RoundNum   int
	RoundName  string
	WinnerAxis models.Axis
	LoserAxis  models.Axis
}

type dashboardData struct {
	Pool         models.Pool
	WinnerAxis   models.Axis
	LoserAxis    models.Axis
	Grid         [10][10]gridCell
	Payouts      []models.Payout
	Leaderboard  []leaderEntry
	Games        []models.Game
	HasAxes      bool
	Editing      bool
	RoundConfigs []models.RoundConfig
	RoundAxes    []roundAxisPair
	RoundFilter  int // 0 = all rounds, 1-6 = specific round
}

type leaderEntry struct {
	Name  string
	Total float64
	Wins  int
}

func parseRoundFilter(r *http.Request) int {
	v, err := strconv.Atoi(r.URL.Query().Get("round"))
	if err != nil || v < 1 || v > 6 {
		return 0 // caller should substitute currentRound
	}
	return v
}

// currentRound returns the round number for the current view:
// 1. The highest round that has an in_progress game
// 2. Any round with games scheduled/played today
// 3. The highest round that has at least one final game
// 4. Falls back to 1
func currentRound(games []models.Game) int {
	today := time.Now().UTC().Truncate(24 * time.Hour)

	// Prefer the highest round with an in-progress game
	inProgress := 0
	for _, g := range games {
		if g.Status == "in_progress" && g.RoundNum > inProgress {
			inProgress = g.RoundNum
		}
	}
	if inProgress > 0 {
		return inProgress
	}

	// Next: prefer the highest round with games today
	todayRound := 0
	for _, g := range games {
		if !g.StartTime.IsZero() && g.StartTime.UTC().Truncate(24*time.Hour).Equal(today) && g.RoundNum > todayRound {
			todayRound = g.RoundNum
		}
	}
	if todayRound > 0 {
		return todayRound
	}

	// Fall back to highest round with completed games
	latest := 1
	for _, g := range games {
		if g.Status == "final" && g.RoundNum > latest {
			latest = g.RoundNum
		}
	}
	return latest
}

func (h *Handler) renderTemplate(w http.ResponseWriter, tmpl string, data interface{}) {
	data = struct {
		Version string
		Data    interface{}
	}{
		Version: h.version,
		Data:    data,
	}

	if err := h.templates.ExecuteTemplate(w, tmpl, data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handlePoolDashboard(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	roundFilter := parseRoundFilter(r)
	if roundFilter == 0 {
		allGames, _ := h.repo.GetAllGamesGlobal(r.Context())
		roundFilter = currentRound(allGames)
	}
	data, err := h.loadFullDashboard(r.Context(), poolID, roundFilter)
	if err != nil {
		log.Printf("error building dashboard: %v", err)
		http.Error(w, "failed to load pool", http.StatusInternalServerError)
		return
	}

	h.renderTemplate(w, "index.html", data)
}

func (h *Handler) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	data, err := h.loadFullDashboard(r.Context(), poolID, 0)
	if err != nil {
		log.Printf("error building admin dashboard: %v", err)
		http.Error(w, "failed to load pool", http.StatusInternalServerError)
		return
	}
	data.Editing = true

	h.renderTemplate(w, "admin", data)
}

func (h *Handler) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	showError := r.URL.Query().Get("error") == "1"
	if err := h.templates.ExecuteTemplate(w, "admin_login", showError); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleAdminLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	adminToken := os.Getenv("ADMIN_TOKEN")

	if adminToken == "" || token != adminToken {
		http.Redirect(w, r, "/admin/login?error=1", http.StatusFound)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    adminSessionValue(adminToken),
		Path:     "/",
		MaxAge:   adminCookieMaxAge,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/admin/pools/main", http.StatusFound)
}

func (h *Handler) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:   adminCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *Handler) handleGrid(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	roundFilter := parseRoundFilter(r)
	data, err := h.loadGridData(r.Context(), poolID, roundFilter)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}

	h.renderTemplate(w, "grid.html", data)
}

func (h *Handler) handleHeader(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	pool, err := h.repo.GetPool(r.Context(), poolID)
	if err != nil {
		http.Error(w, "pool not found", http.StatusNotFound)
		return
	}
	roundConfigs, _ := h.repo.GetAllRoundConfigs(r.Context(), poolID)
	data := dashboardData{
		Pool:         pool,
		RoundConfigs: roundConfigs,
	}

	h.renderTemplate(w, "header", data)
}

func (h *Handler) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	data, err := h.loadLeaderboardData(r.Context(), poolID)
	if err != nil {
		http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
		return
	}

	h.renderTemplate(w, "leaderboard.html", data)
}

func (h *Handler) handleGames(w http.ResponseWriter, r *http.Request) {
	roundFilter := parseRoundFilter(r)
	data, err := h.loadGamesData(r.Context(), roundFilter)
	if err != nil {
		http.Error(w, "failed to load games", http.StatusInternalServerError)
		return
	}

	h.renderTemplate(w, "games.html", data)
}

func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")

	if err := h.syncer.Sync(r.Context(), poolID); err != nil {
		log.Printf("sync error: %v", err)
		http.Error(w, "sync failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	h.hub.Broadcast("sync")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

type squareAssignment struct {
	Row       int    `json:"row"`
	Col       int    `json:"col"`
	OwnerName string `json:"ownerName"`
}

func (h *Handler) handleAssignSquares(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	var assignments []squareAssignment
	if err := json.NewDecoder(r.Body).Decode(&assignments); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for _, a := range assignments {
		sq := models.Square{
			PoolID:    poolID,
			Row:       a.Row,
			Col:       a.Col,
			OwnerName: a.OwnerName,
		}
		if err := h.repo.PutSquare(r.Context(), sq); err != nil {
			http.Error(w, "failed to assign square", http.StatusInternalServerError)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"ok":true,"count":%d}`, len(assignments))
}

func (h *Handler) handleAssignAxes(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	ctx := r.Context()

	// Idempotent: check if round 1 winner axis already exists
	if _, err := h.repo.GetRoundAxis(ctx, poolID, 1, "winner"); err == nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true,"message":"axes already assigned"}`)
		return
	}

	var seed int64
	for _, c := range poolID {
		seed = seed*31 + int64(c)
	}
	rng := rand.New(rand.NewSource(seed))

	// Create axes for all 6 rounds
	for roundNum := 1; roundNum <= 6; roundNum++ {
		winnerDigits := rng.Perm(10)
		loserDigits := rng.Perm(10)

		winnerAxis := models.Axis{PoolID: poolID, RoundNum: roundNum, Type: "winner", Digits: winnerDigits}
		loserAxis := models.Axis{PoolID: poolID, RoundNum: roundNum, Type: "loser", Digits: loserDigits}

		if err := h.repo.PutRoundAxis(ctx, winnerAxis); err != nil {
			http.Error(w, fmt.Sprintf("failed to save winner axis round %d", roundNum), http.StatusInternalServerError)
			return
		}
		if err := h.repo.PutRoundAxis(ctx, loserAxis); err != nil {
			http.Error(w, fmt.Sprintf("failed to save loser axis round %d", roundNum), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"ok":true}`)
}

func (h *Handler) handleUpdatePool(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	ctx := r.Context()

	pool, err := h.repo.GetPool(ctx, poolID)
	if err != nil {
		http.Error(w, "pool not found", http.StatusNotFound)
		return
	}

	var req struct {
		Name *string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		pool.Name = *req.Name
	}

	if err := h.repo.PutPool(ctx, pool); err != nil {
		http.Error(w, "failed to update pool", http.StatusInternalServerError)
		return
	}

	roundConfigs, _ := h.repo.GetAllRoundConfigs(ctx, poolID)
	data := dashboardData{Pool: pool, Editing: true, RoundConfigs: roundConfigs}

	h.renderTemplate(w, "header", data)
}

func (h *Handler) handleUpdateSquare(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	row, err := strconv.Atoi(chi.URLParam(r, "row"))
	if err != nil || row < 0 || row > 9 {
		http.Error(w, "invalid row", http.StatusBadRequest)
		return
	}
	col, err := strconv.Atoi(chi.URLParam(r, "col"))
	if err != nil || col < 0 || col > 9 {
		http.Error(w, "invalid col", http.StatusBadRequest)
		return
	}

	var req struct {
		OwnerName string `json:"ownerName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	sq := models.Square{
		PoolID:    poolID,
		Row:       row,
		Col:       col,
		OwnerName: req.OwnerName,
	}
	if err := h.repo.PutSquare(r.Context(), sq); err != nil {
		http.Error(w, "failed to update square", http.StatusInternalServerError)
		return
	}

	data, err := h.loadGridData(r.Context(), poolID, 0)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}
	data.Editing = true

	h.renderTemplate(w, "grid.html", data)
}

func (h *Handler) handleUpdateRoundAxis(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	roundNum, err := strconv.Atoi(chi.URLParam(r, "roundNum"))
	if err != nil || roundNum < 1 || roundNum > 6 {
		http.Error(w, "roundNum must be 1-6", http.StatusBadRequest)
		return
	}
	axisType := chi.URLParam(r, "type")
	if axisType != "winner" && axisType != "loser" {
		http.Error(w, "type must be 'winner' or 'loser'", http.StatusBadRequest)
		return
	}

	var req struct {
		Digits []int `json:"digits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if len(req.Digits) != 10 {
		http.Error(w, "digits must be an array of 10 values", http.StatusBadRequest)
		return
	}

	axis := models.Axis{
		PoolID:   poolID,
		RoundNum: roundNum,
		Type:     axisType,
		Digits:   req.Digits,
	}
	if err := h.repo.PutRoundAxis(r.Context(), axis); err != nil {
		http.Error(w, "failed to update axis", http.StatusInternalServerError)
		return
	}

	data, err := h.loadGridData(r.Context(), poolID, 0)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}
	data.Editing = true

	h.renderTemplate(w, "grid.html", data)
}

func (h *Handler) handleUpdateRoundConfig(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	roundNum, err := strconv.Atoi(chi.URLParam(r, "roundNum"))
	if err != nil || roundNum < 1 || roundNum > 6 {
		http.Error(w, "roundNum must be 1-6", http.StatusBadRequest)
		return
	}

	var req struct {
		Name         *string  `json:"name"`
		PayoutAmount *float64 `json:"payoutAmount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	rc, err := h.repo.GetRoundConfig(r.Context(), poolID, roundNum)
	if err != nil {
		http.Error(w, "round config not found", http.StatusNotFound)
		return
	}

	if req.Name != nil {
		rc.Name = *req.Name
	}
	if req.PayoutAmount != nil {
		rc.PayoutAmount = *req.PayoutAmount
	}

	if err := h.repo.PutRoundConfig(r.Context(), rc); err != nil {
		http.Error(w, "failed to update round config", http.StatusInternalServerError)
		return
	}

	roundConfigs, _ := h.repo.GetAllRoundConfigs(r.Context(), poolID)
	pool, _ := h.repo.GetPool(r.Context(), poolID)
	data := dashboardData{Pool: pool, Editing: true, RoundConfigs: roundConfigs}

	h.renderTemplate(w, "header", data)
}

