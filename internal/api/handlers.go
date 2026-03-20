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
	"sort"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/espn"
	"github.com/grocky/squares/internal/models"
	"github.com/grocky/squares/internal/scorer"
)

type Handler struct {
	repo       *dynamo.Repo
	espnClient *espn.Client
	templates  *template.Template
}

func NewHandler(repo *dynamo.Repo, espnClient *espn.Client, templateFS fs.FS) *Handler {
	funcMap := template.FuncMap{
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"printf": fmt.Sprintf,
	}
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html"))
	return &Handler{
		repo:       repo,
		espnClient: espnClient,
		templates:  tmpl,
	}
}

func (h *Handler) Routes() *chi.Mux {
	r := chi.NewRouter()
	r.Use(RecoveryMiddleware)
	r.Use(LoggingMiddleware)

	r.Get("/", h.handleIndex)
	r.Post("/pools", h.handleCreatePool)
	r.Get("/pools/{poolID}", h.handlePoolDashboard)
	r.Get("/pools/{poolID}/grid", h.handleGrid)
	r.Get("/pools/{poolID}/leaderboard", h.handleLeaderboard)
	r.Post("/pools/{poolID}/sync", h.handleSync)
	r.Post("/pools/{poolID}/squares", h.handleAssignSquares)
	r.Post("/pools/{poolID}/axes", h.handleAssignAxes)

	r.Put("/pools/{poolID}", h.handleUpdatePool)
	r.Put("/pools/{poolID}/squares/{row}/{col}", h.handleUpdateSquare)
	r.Put("/pools/{poolID}/axis/{type}", h.handleUpdateAxis)
	r.Get("/pools/{poolID}/header", h.handleHeader)

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
	payoutStr := r.FormValue("payoutAmount")
	var payout float64
	fmt.Sscanf(payoutStr, "%f", &payout)

	pool := models.Pool{
		ID:           fmt.Sprintf("%d", time.Now().UnixNano()),
		Name:         name,
		PayoutAmount: payout,
		Status:       "active",
		CreatedAt:    time.Now().UTC(),
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
	IsRocky   bool
}

type dashboardData struct {
	Pool        models.Pool
	RowAxis     models.Axis
	ColAxis     models.Axis
	Grid        [10][10]gridCell
	Payouts     []models.Payout
	Leaderboard []leaderEntry
	Games       []models.Game
	HasAxes     bool
	Editing     bool
}

type leaderEntry struct {
	Name   string
	Total  float64
	Wins   int
}

func (h *Handler) handlePoolDashboard(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	data, err := h.buildDashboardData(r.Context(), poolID)
	if err != nil {
		log.Printf("error building dashboard: %v", err)
		http.Error(w, "failed to load pool", http.StatusInternalServerError)
		return
	}
	data.Editing = r.URL.Query().Get("editing") == "true"
	if err := h.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleGrid(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	data, err := h.buildDashboardData(r.Context(), poolID)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}
	data.Editing = r.URL.Query().Get("editing") == "true"
	if err := h.templates.ExecuteTemplate(w, "grid.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleHeader(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	pool, err := h.repo.GetPool(r.Context(), poolID)
	if err != nil {
		http.Error(w, "pool not found", http.StatusNotFound)
		return
	}
	data := dashboardData{
		Pool:    pool,
		Editing: r.URL.Query().Get("editing") == "true",
	}
	if err := h.templates.ExecuteTemplate(w, "header", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	data, err := h.buildDashboardData(r.Context(), poolID)
	if err != nil {
		http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
		return
	}
	if err := h.templates.ExecuteTemplate(w, "leaderboard.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleSync(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	ctx := r.Context()

	games, err := h.espnClient.SyncGames(ctx, poolID)
	if err != nil {
		log.Printf("sync error: %v", err)
		http.Error(w, "sync failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	pool, err := h.repo.GetPool(ctx, poolID)
	if err != nil {
		http.Error(w, "pool not found", http.StatusNotFound)
		return
	}

	rowAxis, err := h.repo.GetAxis(ctx, poolID, "row")
	if err != nil {
		http.Error(w, "axes not assigned", http.StatusBadRequest)
		return
	}
	colAxis, err := h.repo.GetAxis(ctx, poolID, "col")
	if err != nil {
		http.Error(w, "axes not assigned", http.StatusBadRequest)
		return
	}

	squares, err := h.repo.GetAllSquares(ctx, poolID)
	if err != nil {
		http.Error(w, "failed to get squares", http.StatusInternalServerError)
		return
	}
	squareMap := make(map[[2]int]models.Square)
	for _, sq := range squares {
		squareMap[[2]int{sq.Row, sq.Col}] = sq
	}

	for _, game := range games {
		if game.Status != "final" {
			continue
		}
		row, col := scorer.FindWinningSquare(game, rowAxis, colAxis)
		if row < 0 || col < 0 {
			continue
		}

		exists, err := h.repo.PayoutExists(ctx, poolID, game.EspnID, row, col)
		if err != nil {
			log.Printf("error checking payout: %v", err)
			continue
		}
		if exists {
			continue
		}

		sq, ok := squareMap[[2]int{row, col}]
		if !ok {
			continue
		}

		payout := models.Payout{
			PoolID:    poolID,
			GameID:    game.EspnID,
			Row:       row,
			Col:       col,
			OwnerName: sq.OwnerName,
			Amount:    pool.PayoutAmount,
			HomeScore: game.HomeScore,
			AwayScore: game.AwayScore,
		}
		if err := h.repo.PutPayout(ctx, payout); err != nil {
			log.Printf("error creating payout: %v", err)
		}
	}

	http.Redirect(w, r, "/pools/"+poolID, http.StatusFound)
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

	// Idempotent: check if axes already exist
	if _, err := h.repo.GetAxis(ctx, poolID, "row"); err == nil {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true,"message":"axes already assigned"}`)
		return
	}

	// Seed from pool ID for reproducibility
	var seed int64
	for _, c := range poolID {
		seed = seed*31 + int64(c)
	}
	rng := rand.New(rand.NewSource(seed))

	rowDigits := rng.Perm(10)
	colDigits := rng.Perm(10)

	rowAxis := models.Axis{PoolID: poolID, Type: "row", Digits: rowDigits}
	colAxis := models.Axis{PoolID: poolID, Type: "col", Digits: colDigits}

	if err := h.repo.PutAxis(ctx, rowAxis); err != nil {
		http.Error(w, "failed to save row axis", http.StatusInternalServerError)
		return
	}
	if err := h.repo.PutAxis(ctx, colAxis); err != nil {
		http.Error(w, "failed to save col axis", http.StatusInternalServerError)
		return
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
		Name         *string  `json:"name"`
		PayoutAmount *float64 `json:"payoutAmount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name != nil {
		pool.Name = *req.Name
	}
	if req.PayoutAmount != nil {
		pool.PayoutAmount = *req.PayoutAmount
	}

	if err := h.repo.PutPool(ctx, pool); err != nil {
		http.Error(w, "failed to update pool", http.StatusInternalServerError)
		return
	}

	data := dashboardData{Pool: pool, Editing: true}
	if err := h.templates.ExecuteTemplate(w, "header", data); err != nil {
		log.Printf("template error: %v", err)
	}
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

	data, err := h.buildDashboardData(r.Context(), poolID)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}
	data.Editing = true
	if err := h.templates.ExecuteTemplate(w, "grid.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleUpdateAxis(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	axisType := chi.URLParam(r, "type")
	if axisType != "row" && axisType != "col" {
		http.Error(w, "type must be 'row' or 'col'", http.StatusBadRequest)
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
		PoolID: poolID,
		Type:   axisType,
		Digits: req.Digits,
	}
	if err := h.repo.PutAxis(r.Context(), axis); err != nil {
		http.Error(w, "failed to update axis", http.StatusInternalServerError)
		return
	}

	data, err := h.buildDashboardData(r.Context(), poolID)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}
	data.Editing = true
	if err := h.templates.ExecuteTemplate(w, "grid.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) buildDashboardData(ctx context.Context, poolID string) (dashboardData, error) {
	pool, err := h.repo.GetPool(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}

	var data dashboardData
	data.Pool = pool

	rowAxis, rowErr := h.repo.GetAxis(ctx, poolID, "row")
	colAxis, colErr := h.repo.GetAxis(ctx, poolID, "col")
	if rowErr == nil && colErr == nil {
		data.RowAxis = rowAxis
		data.ColAxis = colAxis
		data.HasAxes = true
	}

	squares, err := h.repo.GetAllSquares(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	for _, sq := range squares {
		if sq.Row >= 0 && sq.Row < 10 && sq.Col >= 0 && sq.Col < 10 {
			data.Grid[sq.Row][sq.Col] = gridCell{
				OwnerName: sq.OwnerName,
				IsRocky:   sq.OwnerName == "Rocky",
			}
		}
	}

	payouts, err := h.repo.GetAllPayouts(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	data.Payouts = payouts
	for _, p := range payouts {
		if p.Row >= 0 && p.Row < 10 && p.Col >= 0 && p.Col < 10 {
			cell := &data.Grid[p.Row][p.Col]
			cell.IsWinner = true
			cell.Amount += p.Amount
		}
	}

	// Build leaderboard
	totals := make(map[string]*leaderEntry)
	for _, p := range payouts {
		e, ok := totals[p.OwnerName]
		if !ok {
			e = &leaderEntry{Name: p.OwnerName}
			totals[p.OwnerName] = e
		}
		e.Total += p.Amount
		e.Wins++
	}
	for _, e := range totals {
		data.Leaderboard = append(data.Leaderboard, *e)
	}
	sort.Slice(data.Leaderboard, func(i, j int) bool {
		return data.Leaderboard[i].Total > data.Leaderboard[j].Total
	})

	games, err := h.repo.GetAllGames(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	data.Games = games

	return data, nil
}
