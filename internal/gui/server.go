package gui

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/models"
)

type connectRequest struct {
	Mode     string `json:"mode"` // "aws" or "local"
	Region   string `json:"region"`
	Endpoint string `json:"endpoint"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type server struct {
	token     string
	mu        sync.RWMutex
	backend   Backend // nil until /connect succeeds
	connectFn func(req connectRequest) (Backend, error)
}

func newServer(token string) *server {
	return &server{token: token, connectFn: defaultConnect}
}

// defaultConnect builds a real *dynamo.Client from the request.
func defaultConnect(req connectRequest) (Backend, error) {
	cfg := dynamo.ConnectionConfig{
		Region:   req.Region,
		Endpoint: req.Endpoint,
		UseLocal: req.Mode == "local",
	}
	if req.Mode == "local" {
		if cfg.Region == "" {
			cfg.Region = "us-east-1"
		}
		cfg.AccessKey = "local"
		cfg.SecretKey = "local"
	}
	return dynamo.NewClient(cfg)
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connect", s.handleConnect)
	mux.HandleFunc("GET /tables", s.handleListTables)
	mux.HandleFunc("GET /tables/{name}/schema", s.handleSchema)
	mux.HandleFunc("GET /tables/{name}/scan", s.handleScan)
	return s.withMiddleware(mux)
}

func (s *server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(h[len(prefix):]), []byte(s.token)) == 1
}

func (s *server) getBackend() (Backend, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.backend, s.backend != nil
}

func (s *server) handleConnect(w http.ResponseWriter, r *http.Request) {
	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Mode {
	case "aws":
		if req.Region == "" {
			writeError(w, http.StatusBadRequest, "region is required for aws mode")
			return
		}
	case "local":
		if req.Endpoint == "" {
			writeError(w, http.StatusBadRequest, "endpoint is required for local mode")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, `mode must be "aws" or "local"`)
		return
	}

	backend, err := s.connectFn(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to connect: "+err.Error())
		return
	}
	tables, err := backend.ListTables(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "connected but failed to list tables: "+err.Error())
		return
	}
	sort.Strings(tables)

	s.mu.Lock()
	s.backend = backend
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, map[string]interface{}{"tables": tables})
}

func (s *server) handleListTables(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	tables, err := backend.ListTables(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	sort.Strings(tables)
	writeJSON(w, http.StatusOK, map[string]interface{}{"tables": tables})
}

func (s *server) handleSchema(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	info, err := backend.DescribeTable(r.Context(), r.PathValue("name"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"info":    info,
		"rawJSON": info.RawJSON,
	})
}

func (s *server) handleScan(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	limit := int32(500)
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = int32(parsed)
		}
	}

	startKey, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := backend.ScanTable(r.Context(), name, limit, startKey, "", nil, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	cursor, err := encodeCursor(result.LastEvaluatedKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]interface{}, len(result.Items))
	for i, item := range result.Items {
		converted := make(map[string]interface{}, len(item))
		for k, v := range item {
			converted[k] = models.AttributeValueToInterface(v)
		}
		items[i] = converted
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items":  items,
		"cursor": cursor,
		"count":  result.Count,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
