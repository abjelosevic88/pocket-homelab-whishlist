package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-wishlist/internal/store"
)

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(120 * time.Second))

	r.Get("/pocket-ask.js", s.core.AskWidget("wishlist"))
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		// A shared link: no login. The visitor's browser keeps a secret
		// (X-Giver) so they can change or drop their own reservations.
		r.Get("/s/{token}", s.handleLinkList)
		r.Get("/s/{token}/wishes/{id}/photo", s.handleLinkPhoto)
		r.Post("/s/{token}/wishes/{id}/reserve", s.handleLinkReserve)
		r.Delete("/s/{token}/wishes/{id}/reserve", s.handleLinkRelease)
		r.Post("/s/{token}/wishes/{id}/bought", s.handleLinkBought)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			r.Get("/dashboard", s.handleDashboard)
			r.Get("/home", s.handleHome)
			r.Get("/currencies", s.handleCurrencies)
			r.Get("/tags", s.handleTags)
			r.Get("/occasions", s.handleOccasions)
			r.Get("/day/{date}", s.handleDay)
			r.Post("/preview", s.handlePreview)
			r.Post("/hunt", s.handleNewHunt)
			r.Get("/ai/status", s.handleAIStatus)
			r.Get("/ai/models", s.handleAIModels)

			r.Get("/lists", s.handleListLists)
			r.Post("/lists", s.handleCreateList)
			r.Put("/lists/reorder", s.handleReorderLists)
			r.Get("/lists/{id}", s.handleGetList)
			r.Put("/lists/{id}", s.handleUpdateList)
			r.Delete("/lists/{id}", s.handleDeleteList)
			r.Put("/lists/{id}/order", s.handleReorderWishes)
			r.Get("/lists/{id}/shares", s.handleListShares)
			r.Post("/lists/{id}/shares", s.handleCreateShare)
			r.Delete("/lists/{id}/shares/{sid}", s.handleDeleteShare)

			r.Get("/wishes", s.handleListWishes)
			r.Post("/wishes", s.handleCreateWish)
			r.Get("/wishes/{id}", s.handleGetWish)
			r.Put("/wishes/{id}", s.handleUpdateWish)
			r.Delete("/wishes/{id}", s.handleDeleteWish)
			r.Post("/wishes/{id}/bought", s.handleBought)
			r.Post("/wishes/{id}/status", s.handleSetStatus)
			r.Post("/wishes/{id}/check", s.handleCheckNow)
			r.Get("/wishes/{id}/prices", s.handlePrices)
			r.Post("/wishes/{id}/hunt", s.handleHunt)
			r.Get("/wishes/{id}/offers", s.handleOffers)
			r.Post("/wishes/{id}/offers/{oid}/use", s.handleUseOffer)
			r.Post("/wishes/{id}/offers/{oid}/dismiss", s.handleDismissOffer)
			r.Get("/wishes/{id}/photo", s.handlePhoto)
			r.Post("/wishes/{id}/photo", s.handleUploadPhoto)
			r.Delete("/wishes/{id}/photo", s.handleDeletePhoto)

			// lists other people on the box shared with me
			r.Get("/shared", s.handleSharedLists)
			r.Get("/shared/{id}", s.handleSharedList)
			r.Get("/shared/{id}/wishes/{wid}/photo", s.handleSharedPhoto)
			r.Post("/shared/{id}/wishes/{wid}/reserve", s.handleSharedReserve)
			r.Delete("/shared/{id}/wishes/{wid}/reserve", s.handleSharedRelease)
			r.Post("/shared/{id}/wishes/{wid}/bought", s.handleSharedBought)
			r.Get("/reservations", s.handleMyReservations)

			r.Post("/integrations/test", s.handleIntegrationTest)
			r.Get("/contacts/search", s.handleContactSearch)
			r.Get("/money/accounts", s.handleMoneyAccounts)
			r.Get("/money/categories", s.handleMoneyCategories)
			r.Get("/split/groups", s.handleSplitGroups)
			r.Get("/inventory/locations", s.handleInventoryLocations)
			r.Post("/notify/test", s.handleNotifyTest)
			r.Post("/checks/run", s.handleRunChecks)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.json", s.handleExportJSON)
			r.Post("/import", s.handleImportJSON)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict), errors.Is(err, core.ErrOperationConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	case errors.Is(err, store.ErrFullyReserved):
		writeErr(w, http.StatusConflict, "someone already reserved this")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_wishlist_info Build info.\n# TYPE pocket_wishlist_info gauge\npocket_wishlist_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_wishlist_uptime_seconds Process uptime.\n# TYPE pocket_wishlist_uptime_seconds gauge\npocket_wishlist_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_wishlist_http_requests_total HTTP requests served.\n# TYPE pocket_wishlist_http_requests_total counter\npocket_wishlist_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_wishlist_http_errors_total HTTP 5xx responses.\n# TYPE pocket_wishlist_http_errors_total counter\npocket_wishlist_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_wishlist_rows Rows per table.\n# TYPE pocket_wishlist_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_wishlist_rows{table=%q} %d\n", k, v)
	}
	fmt.Fprintf(w, "# HELP pocket_wishlist_price_checks_total Price checks run since start.\n# TYPE pocket_wishlist_price_checks_total counter\npocket_wishlist_price_checks_total %d\n", priceChecks.Load())
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

// ---------- files ----------

// saveFile writes bytes under FILES_DIR/<sub>/ with a random name and returns the relative path.
func (s *Server) saveFile(sub, ext string, data []byte) (string, error) {
	dir := filepath.Join(s.cfg.FilesDir, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext = strings.ToLower(filepath.Ext("x" + ext))
	if len(ext) > 8 {
		ext = ""
	}
	name := store.RandomToken(12) + ext
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		return "", err
	}
	return sub + "/" + name, nil
}

func (s *Server) filePath(rel string) string {
	rel = filepath.Clean("/" + rel)
	return filepath.Join(s.cfg.FilesDir, rel)
}

func (s *Server) removeFile(rel string) {
	if rel == "" {
		return
	}
	_ = os.Remove(s.filePath(rel))
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, rel string) {
	if rel == "" {
		writeErr(w, http.StatusNotFound, "no file")
		return
	}
	p := s.filePath(rel)
	if _, err := os.Stat(p); err != nil {
		writeErr(w, http.StatusNotFound, "file missing")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// pictures come from shops and uploads: an SVG or a mislabelled file
	// opened directly must not run as a page of this origin
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, p)
}

// readUpload reads one multipart file field (or the raw body) up to the limit.
func (s *Server) readUpload(r *http.Request, field string) ([]byte, string, string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, s.cfg.MaxUploadBytes)
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(s.cfg.MaxUploadBytes); err != nil {
			return nil, "", "", err
		}
		f, hdr, err := r.FormFile(field)
		if err != nil {
			return nil, "", "", err
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		if err != nil {
			return nil, "", "", err
		}
		return b, hdr.Filename, hdr.Header.Get("Content-Type"), nil
	}
	b, err := io.ReadAll(r.Body)
	return b, r.URL.Query().Get("name"), ct, err
}
