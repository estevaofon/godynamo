package gui

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
	"github.com/godynamo/internal/models"
	"github.com/godynamo/internal/query"
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
	h         http.Handler
}

func newServer(token string) *server {
	s := &server{token: token, connectFn: defaultConnect}
	s.h = s.buildHandler()
	return s
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

func (s *server) handler() http.Handler { return s.h }

func (s *server) buildHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /connect", s.handleConnect)
	mux.HandleFunc("GET /tables", s.handleListTables)
	mux.HandleFunc("GET /tables/{name}/schema", s.handleSchema)
	mux.HandleFunc("GET /tables/{name}/scan", s.handleScan)
	mux.HandleFunc("POST /tables/{name}/query", s.handleQuery)
	mux.HandleFunc("POST /tables/{name}/item", s.handlePutItem)
	mux.HandleFunc("DELETE /tables/{name}/item", s.handleDeleteItem)
	mux.HandleFunc("POST /tables", s.handleCreateTable)
	return s.withMiddleware(mux)
}

func (s *server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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

func (s *server) setBackend(b Backend) {
	s.mu.Lock()
	s.backend = b
	s.mu.Unlock()
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
	tables = append([]string(nil), tables...)
	sort.Strings(tables)

	s.setBackend(backend)

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
	tables = append([]string(nil), tables...)
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

type queryRequest struct {
	Conditions []queryCondition `json:"conditions"`
	Limit      int32            `json:"limit"`
	Cursor     string           `json:"cursor"`
}

type queryCondition struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

var queryOperators = map[string]query.Operator{
	"eq":           query.OpEquals,
	"ne":           query.OpNotEquals,
	"gt":           query.OpGreaterThan,
	"lt":           query.OpLessThan,
	"ge":           query.OpGreaterOrEqual,
	"le":           query.OpLessOrEqual,
	"contains":     query.OpContains,
	"not_contains": query.OpNotContains,
	"begins_with":  query.OpBeginsWith,
	"exists":       query.OpExists,
	"not_exists":   query.OpNotExists,
}

func (s *server) handleQuery(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	conds := make([]query.Condition, 0, len(req.Conditions))
	for _, c := range req.Conditions {
		op, known := queryOperators[c.Op]
		if !known {
			writeError(w, http.StatusBadRequest, "unknown operator: "+c.Op)
			return
		}
		conds = append(conds, query.Condition{Name: c.Name, Operator: op, Value: c.Value})
	}

	limit := int32(500)
	if req.Limit > 0 && req.Limit <= 1000 {
		limit = req.Limit
	}

	startKey, err := decodeCursor(req.Cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	expr, names, values := query.BuildExpression(conds)
	if expr == "" {
		// The /query endpoint requires a real filter; an empty expression would
		// degrade to a full-table scan. Unfiltered browsing uses GET /scan.
		writeError(w, http.StatusBadRequest, "query requires at least one complete condition (attribute, operator, and value)")
		return
	}

	// DescribeTable is called per query to plan Query-vs-Scan. The schema is
	// stable for the session, so this small per-request cost is acceptable for a
	// loopback dev tool (caching can come with the region-switch phase).
	info, err := backend.DescribeTable(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	plan := query.BuildPlan(info, expr, names, values)

	var (
		rawItems     []map[string]types.AttributeValue
		lastKey      map[string]types.AttributeValue
		count        int32
		scannedCount int32
		mode         string
	)

	if plan.Mode == query.ModeQuery {
		mode = "query"
		res, qerr := backend.QueryTable(r.Context(), dynamo.QueryInput{
			TableName:                name,
			IndexName:                plan.IndexName,
			KeyConditionExpression:   plan.KeyConditionExpression,
			FilterExpression:         plan.FilterExpression,
			ExpressionAttributeNames: plan.Names,
			ExpressionValues:         plan.Values,
			Limit:                    limit,
			ScanIndexForward:         true,
			StartKey:                 startKey,
		})
		if qerr != nil {
			writeError(w, http.StatusBadGateway, qerr.Error())
			return
		}
		rawItems, lastKey, count, scannedCount = res.Items, res.LastEvaluatedKey, res.Count, res.ScannedCount
	} else {
		mode = "scan"
		res, serr := backend.ScanTable(r.Context(), name, limit, startKey, plan.FilterExpression, plan.Names, plan.Values)
		if serr != nil {
			writeError(w, http.StatusBadGateway, serr.Error())
			return
		}
		rawItems, lastKey, count, scannedCount = res.Items, res.LastEvaluatedKey, res.Count, res.ScannedCount
	}

	cursor, err := encodeCursor(lastKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]map[string]interface{}, len(rawItems))
	for i, item := range rawItems {
		converted := make(map[string]interface{}, len(item))
		for k, v := range item {
			converted[k] = models.AttributeValueToInterface(v)
		}
		items[i] = converted
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"mode":         mode,
		"items":        items,
		"cursor":       cursor,
		"count":        count,
		"scannedCount": scannedCount,
	})
}

type itemRequest struct {
	JSON string `json:"json"`
}

type createTableRequest struct {
	Name        string `json:"name"`
	PK          string `json:"pk"`
	PKType      string `json:"pkType"`
	SK          string `json:"sk"`
	SKType      string `json:"skType"`
	BillingMode string `json:"billingMode"`
	RCU         int64  `json:"rcu"`
	WCU         int64  `json:"wcu"`
}

func (s *server) handlePutItem(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := models.JSONToItem(req.JSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := backend.PutItem(r.Context(), name, item); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}
	name := r.PathValue("name")

	var req itemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	item, err := models.JSONToItem(req.JSON)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := backend.DescribeTable(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if info == nil || info.PartitionKey == "" {
		writeError(w, http.StatusBadGateway, "table metadata unavailable or missing a partition key")
		return
	}

	key := make(map[string]types.AttributeValue)
	if info.PartitionKey != "" {
		v, present := item[info.PartitionKey]
		if !present {
			writeError(w, http.StatusBadRequest, "item is missing the partition key: "+info.PartitionKey)
			return
		}
		key[info.PartitionKey] = v
	}
	if info.SortKey != "" {
		v, present := item[info.SortKey]
		if !present {
			writeError(w, http.StatusBadRequest, "item is missing the sort key: "+info.SortKey)
			return
		}
		key[info.SortKey] = v
	}

	if err := backend.DeleteItem(r.Context(), name, key); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	backend, ok := s.getBackend()
	if !ok {
		writeError(w, http.StatusConflict, "not connected")
		return
	}

	var req createTableRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.PK == "" || req.PKType == "" {
		writeError(w, http.StatusBadRequest, "table name, partition key, and partition key type are required")
		return
	}

	input := dynamo.CreateTableInput{
		TableName:     req.Name,
		PartitionKey:  req.PK,
		PartitionType: strings.ToUpper(req.PKType),
		SortKey:       req.SK,
		SortKeyType:   strings.ToUpper(req.SKType),
		BillingMode:   req.BillingMode,
		ReadCapacity:  req.RCU,
		WriteCapacity: req.WCU,
	}
	if err := backend.CreateTable(r.Context(), input); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
