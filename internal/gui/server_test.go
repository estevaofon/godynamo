package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
)

type fakeBackend struct {
	tables   []string
	info     *dynamo.TableInfo
	scan     *dynamo.ScanResult
	scanErr  error
	query    *dynamo.QueryResult
	queryErr error
	putItem   map[string]types.AttributeValue
	putErr    error
	deleteKey map[string]types.AttributeValue
	deleteErr error
	createIn  dynamo.CreateTableInput
	createErr error
}

func (f *fakeBackend) ListTables(ctx context.Context) ([]string, error) { return f.tables, nil }

func (f *fakeBackend) DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error) {
	return f.info, nil
}

func (f *fakeBackend) ScanTable(ctx context.Context, name string, limit int32,
	startKey map[string]types.AttributeValue, filterExpr string,
	names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error) {
	return f.scan, f.scanErr
}

func (f *fakeBackend) QueryTable(ctx context.Context, input dynamo.QueryInput) (*dynamo.QueryResult, error) {
	return f.query, f.queryErr
}

func (f *fakeBackend) PutItem(ctx context.Context, tableName string, item map[string]types.AttributeValue) error {
	f.putItem = item
	return f.putErr
}

func (f *fakeBackend) DeleteItem(ctx context.Context, tableName string, key map[string]types.AttributeValue) error {
	f.deleteKey = key
	return f.deleteErr
}

func (f *fakeBackend) CreateTable(ctx context.Context, input dynamo.CreateTableInput) error {
	f.createIn = input
	return f.createErr
}

func newTestServer(b Backend) *server {
	s := newServer("test-token")
	s.setBackend(b)
	return s
}

func do(s *server, method, target string, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	return rec
}

func TestRequiresToken(t *testing.T) {
	s := newTestServer(&fakeBackend{tables: []string{"a"}})
	r := httptest.NewRequest(http.MethodGet, "/tables", nil) // no Authorization header
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	s := newServer("test-token")
	r := httptest.NewRequest(http.MethodOptions, "/connect", nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing CORS allow-origin header")
	}
}

func TestListTablesSorted(t *testing.T) {
	s := newTestServer(&fakeBackend{tables: []string{"b", "a"}})
	rec := do(s, http.MethodGet, "/tables", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tables []string `json:"tables"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Tables) != 2 || resp.Tables[0] != "a" || resp.Tables[1] != "b" {
		t.Fatalf("want [a b], got %v", resp.Tables)
	}
}

func TestConnectStoresBackend(t *testing.T) {
	s := newServer("test-token")
	s.connectFn = func(req connectRequest) (Backend, error) {
		return &fakeBackend{tables: []string{"t2", "t1"}}, nil
	}
	rec := do(s, http.MethodPost, "/connect", `{"mode":"local","endpoint":"http://localhost:8000"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Tables []string `json:"tables"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Tables) != 2 || resp.Tables[0] != "t1" {
		t.Fatalf("want sorted tables, got %v", resp.Tables)
	}
	if _, ok := s.getBackend(); !ok {
		t.Fatal("backend not stored after connect")
	}
}

func TestConnectValidatesMode(t *testing.T) {
	s := newServer("test-token")
	rec := do(s, http.MethodPost, "/connect", `{"mode":"bogus"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestSchemaHandler(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{
		Name: "mytable", PartitionKey: "id", SortKey: "sk", RawJSON: `{"TableName":"mytable"}`,
	}})
	rec := do(s, http.MethodGet, "/tables/mytable/schema", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp struct {
		Info    map[string]interface{} `json:"info"`
		RawJSON string                 `json:"rawJSON"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Info["PartitionKey"] != "id" {
		t.Fatalf("want PartitionKey=id, got %v", resp.Info["PartitionKey"])
	}
	if resp.RawJSON == "" {
		t.Fatal("want rawJSON")
	}
}

func TestScanHandlerConvertsItemsAndCursor(t *testing.T) {
	s := newTestServer(&fakeBackend{
		scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{
				{
					"id": &types.AttributeValueMemberS{Value: "x"},
					"n":  &types.AttributeValueMemberN{Value: "5"},
				},
			},
			Count: 1,
			LastEvaluatedKey: map[string]types.AttributeValue{
				"id": &types.AttributeValueMemberS{Value: "x"},
			},
		},
	})
	rec := do(s, http.MethodGet, "/tables/mytable/scan?limit=10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items  []map[string]interface{} `json:"items"`
		Cursor string                   `json:"cursor"`
		Count  int                      `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0]["id"] != "x" {
		t.Fatalf("want id=x, got %v", resp.Items[0]["id"])
	}
	if resp.Items[0]["n"] != float64(5) {
		t.Fatalf("want n=5, got %v (%T)", resp.Items[0]["n"], resp.Items[0]["n"])
	}
	if resp.Cursor == "" {
		t.Fatal("want non-empty cursor")
	}
}

func TestScanNotConnected(t *testing.T) {
	s := newServer("test-token") // no backend set
	rec := do(s, http.MethodGet, "/tables/x/scan", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
}

func TestScanBackendError(t *testing.T) {
	s := newTestServer(&fakeBackend{scanErr: errors.New("dynamo timeout")})
	rec := do(s, http.MethodGet, "/tables/x/scan", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}
}

func TestQueryModeForPartitionKeyEquals(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{Name: "t", PartitionKey: "id"},
		query: &dynamo.QueryResult{
			Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
			Count: 1,
		},
	})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[{"name":"id","op":"eq","value":"1"}],"limit":10}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string                   `json:"mode"`
		Items []map[string]interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "query" {
		t.Fatalf("want mode query, got %q", resp.Mode)
	}
	if len(resp.Items) != 1 || resp.Items[0]["id"] != "1" {
		t.Fatalf("items=%v", resp.Items)
	}
}

func TestQueryFallsBackToScanForNonKey(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{Name: "t", PartitionKey: "id"},
		scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{{"status": &types.AttributeValueMemberS{Value: "active"}}},
			Count: 1,
		},
	})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[{"name":"status","op":"eq","value":"active"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode string `json:"mode"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "scan" {
		t.Fatalf("want mode scan, got %q", resp.Mode)
	}
}

func TestQueryUnknownOperator(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[{"name":"id","op":"bogus","value":"1"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestQueryNotConnected(t *testing.T) {
	s := newServer("test-token")
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
}

func TestQueryNoEffectiveFilter(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestPutItem(t *testing.T) {
	f := &fakeBackend{}
	s := newTestServer(f)
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"{\"id\":\"1\",\"name\":\"Alice\"}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if f.putItem["id"] == nil {
		t.Fatalf("expected item passed to PutItem, got %v", f.putItem)
	}
}

func TestPutItemInvalidJSON(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"not json"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestDeleteItemDerivesKey(t *testing.T) {
	f := &fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id", SortKey: "sk"}}
	s := newTestServer(f)
	rec := do(s, http.MethodDelete, "/tables/t/item", `{"json":"{\"id\":\"1\",\"sk\":\"a\",\"extra\":\"x\"}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(f.deleteKey) != 2 {
		t.Fatalf("expected key with pk+sk only, got %v", f.deleteKey)
	}
	if f.deleteKey["id"] == nil || f.deleteKey["sk"] == nil {
		t.Fatalf("key missing pk/sk: %v", f.deleteKey)
	}
}

func TestDeleteItemMissingKey(t *testing.T) {
	f := &fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}}
	s := newTestServer(f)
	rec := do(s, http.MethodDelete, "/tables/t/item", `{"json":"{\"other\":\"x\"}"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestCreateTable(t *testing.T) {
	f := &fakeBackend{}
	s := newTestServer(f)
	rec := do(s, http.MethodPost, "/tables", `{"name":"NewT","pk":"id","pkType":"S","billingMode":"PAY_PER_REQUEST"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if f.createIn.TableName != "NewT" || f.createIn.PartitionKey != "id" {
		t.Fatalf("createIn=%+v", f.createIn)
	}
}

func TestCreateTableValidates(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables", `{"pk":"id"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestWriteNotConnected(t *testing.T) {
	s := newServer("test-token")
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"{}"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d", rec.Code)
	}
}
