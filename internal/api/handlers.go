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
		"add": func(a, b int) int {
			return a + b
		},
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
	r.Get("/pools/{poolID}/games", h.handleGames)
	r.Post("/pools/{poolID}/sync", h.handleSync)
	r.Post("/pools/{poolID}/squares", h.handleAssignSquares)
	r.Post("/pools/{poolID}/axes", h.handleAssignAxes)

	r.Put("/pools/{poolID}", h.handleUpdatePool)
	r.Put("/pools/{poolID}/squares/{row}/{col}", h.handleUpdateSquare)
	r.Put("/pools/{poolID}/rounds/{roundNum}/axis/{type}", h.handleUpdateRoundAxis)
	r.Put("/pools/{poolID}/rounds/{roundNum}/config", h.handleUpdateRoundConfig)
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
	IsRocky   bool
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
	if err != nil || v < 0 || v > 6 {
		return 0
	}
	return v
}

func (h *Handler) handlePoolDashboard(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	roundFilter := parseRoundFilter(r)
	data, err := h.buildDashboardData(r.Context(), poolID, roundFilter)
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
	roundFilter := parseRoundFilter(r)
	data, err := h.buildDashboardData(r.Context(), poolID, roundFilter)
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
	roundConfigs, _ := h.repo.GetAllRoundConfigs(r.Context(), poolID)
	data := dashboardData{
		Pool:         pool,
		Editing:      r.URL.Query().Get("editing") == "true",
		RoundConfigs: roundConfigs,
	}
	if err := h.templates.ExecuteTemplate(w, "header", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	roundFilter := parseRoundFilter(r)
	data, err := h.buildDashboardData(r.Context(), poolID, roundFilter)
	if err != nil {
		http.Error(w, "failed to load leaderboard", http.StatusInternalServerError)
		return
	}
	if err := h.templates.ExecuteTemplate(w, "leaderboard.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) handleGames(w http.ResponseWriter, r *http.Request) {
	poolID := chi.URLParam(r, "poolID")
	roundFilter := parseRoundFilter(r)
	data, err := h.buildDashboardData(r.Context(), poolID, roundFilter)
	if err != nil {
		http.Error(w, "failed to load games", http.StatusInternalServerError)
		return
	}
	if err := h.templates.ExecuteTemplate(w, "games.html", data); err != nil {
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

	squares, err := h.repo.GetAllSquares(ctx, poolID)
	if err != nil {
		http.Error(w, "failed to get squares", http.StatusInternalServerError)
		return
	}
	squareMap := make(map[[2]int]models.Square)
	for _, sq := range squares {
		squareMap[[2]int{sq.Row, sq.Col}] = sq
	}

	// Build round config map for payout amounts
	roundConfigs, err := h.repo.GetAllRoundConfigs(ctx, poolID)
	if err != nil {
		http.Error(w, "failed to get round configs", http.StatusInternalServerError)
		return
	}
	rcMap := make(map[int]models.RoundConfig)
	for _, rc := range roundConfigs {
		rcMap[rc.RoundNum] = rc
	}

	for _, game := range games {
		if game.Status != "final" {
			continue
		}
		roundNum := game.RoundNum
		if roundNum < 1 || roundNum > 6 {
			roundNum = 1
		}

		winnerAxis, err := h.repo.GetRoundAxis(ctx, poolID, roundNum, "winner")
		if err != nil {
			log.Printf("no winner axis for round %d: %v", roundNum, err)
			continue
		}
		loserAxis, err := h.repo.GetRoundAxis(ctx, poolID, roundNum, "loser")
		if err != nil {
			log.Printf("no loser axis for round %d: %v", roundNum, err)
			continue
		}

		row, col := scorer.FindWinningSquare(game, winnerAxis, loserAxis)
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

		payoutAmount := 0.0
		if rc, ok := rcMap[roundNum]; ok {
			payoutAmount = rc.PayoutAmount
		}

		payout := models.Payout{
			PoolID:      poolID,
			GameID:      game.EspnID,
			Row:         row,
			Col:         col,
			OwnerName:   sq.OwnerName,
			Amount:      payoutAmount,
			WinnerScore: game.WinnerScore,
			LoserScore:  game.LoserScore,
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

	data, err := h.buildDashboardData(r.Context(), poolID, 0)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}
	data.Editing = true
	if err := h.templates.ExecuteTemplate(w, "grid.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
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

	data, err := h.buildDashboardData(r.Context(), poolID, 0)
	if err != nil {
		http.Error(w, "failed to load grid", http.StatusInternalServerError)
		return
	}
	data.Editing = true
	if err := h.templates.ExecuteTemplate(w, "grid.html", data); err != nil {
		log.Printf("template error: %v", err)
	}
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
	if err := h.templates.ExecuteTemplate(w, "header", data); err != nil {
		log.Printf("template error: %v", err)
	}
}

func (h *Handler) buildDashboardData(ctx context.Context, poolID string, roundFilter int) (dashboardData, error) {
	pool, err := h.repo.GetPool(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}

	var data dashboardData
	data.Pool = pool
	data.RoundFilter = roundFilter

	// Load round configs
	roundConfigs, _ := h.repo.GetAllRoundConfigs(ctx, poolID)
	data.RoundConfigs = roundConfigs

	// Load axes for all rounds
	var roundAxes []roundAxisPair
	rcMap := make(map[int]string)
	for _, rc := range roundConfigs {
		rcMap[rc.RoundNum] = rc.Name
	}

	for roundNum := 1; roundNum <= 6; roundNum++ {
		winnerAxis, wErr := h.repo.GetRoundAxis(ctx, poolID, roundNum, "winner")
		loserAxis, lErr := h.repo.GetRoundAxis(ctx, poolID, roundNum, "loser")
		if wErr == nil && lErr == nil {
			name := rcMap[roundNum]
			if name == "" {
				name = fmt.Sprintf("Round %d", roundNum)
			}
			roundAxes = append(roundAxes, roundAxisPair{
				RoundNum:   roundNum,
				RoundName:  name,
				WinnerAxis: winnerAxis,
				LoserAxis:  loserAxis,
			})
		}
	}
	data.RoundAxes = roundAxes

	// Use the filtered round's axes for grid display, or round 1 as default
	displayRound := roundFilter
	if displayRound == 0 {
		displayRound = 1
	}
	for _, ra := range roundAxes {
		if ra.RoundNum == displayRound {
			data.WinnerAxis = ra.WinnerAxis
			data.LoserAxis = ra.LoserAxis
			data.HasAxes = true
			break
		}
	}
	if !data.HasAxes && len(roundAxes) > 0 {
		data.WinnerAxis = roundAxes[0].WinnerAxis
		data.LoserAxis = roundAxes[0].LoserAxis
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

	// Build game → round number map so we can look up current payout amounts
	allGames, err := h.repo.GetAllGames(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	gameRoundMap := make(map[string]int)
	for _, g := range allGames {
		gameRoundMap[g.EspnID] = g.RoundNum
	}

	// Filter games by round if a filter is set
	if roundFilter > 0 {
		var filtered []models.Game
		for _, g := range allGames {
			if g.RoundNum == roundFilter {
				filtered = append(filtered, g)
			}
		}
		data.Games = filtered
	} else {
		data.Games = allGames
	}

	// Build a set of game IDs in the filtered round for payout filtering
	filteredGameIDs := make(map[string]bool)
	if roundFilter > 0 {
		for _, g := range data.Games {
			filteredGameIDs[g.EspnID] = true
		}
	}

	// Build round config payout map
	rcPayoutMap := make(map[int]float64)
	for _, rc := range roundConfigs {
		rcPayoutMap[rc.RoundNum] = rc.PayoutAmount
	}

	allPayouts, err := h.repo.GetAllPayouts(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	// Override stored payout amount with current round config amount
	for i, p := range allPayouts {
		if roundNum, ok := gameRoundMap[p.GameID]; ok {
			if currentAmount, ok := rcPayoutMap[roundNum]; ok {
				allPayouts[i].Amount = currentAmount
			}
		}
	}

	// Filter payouts by round if a filter is set
	var payouts []models.Payout
	if roundFilter > 0 {
		for _, p := range allPayouts {
			if filteredGameIDs[p.GameID] {
				payouts = append(payouts, p)
			}
		}
	} else {
		payouts = allPayouts
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

	return data, nil
}
