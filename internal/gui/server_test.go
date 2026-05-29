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
	s.activeProfile = "test"
	s.connectFn = func(profile, region string) (Backend, error) { return b, nil }
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
	r := httptest.NewRequest(http.MethodGet, "/profiles", nil) // no Authorization header
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	s := newServer("test-token")
	r := httptest.NewRequest(http.MethodOptions, "/discover", nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatal("missing CORS allow-origin header")
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "DELETE") {
		t.Fatalf("CORS methods must allow DELETE, got %q", rec.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestSchemaHandler(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{
		Name: "mytable", PartitionKey: "id", SortKey: "sk", RawJSON: `{"TableName":"mytable"}`,
	}})
	rec := do(s, http.MethodGet, "/tables/mytable/schema?region=us-east-1", "")
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
	rec := do(s, http.MethodGet, "/tables/mytable/scan?limit=10&region=us-east-1", "")
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

func TestScanMissingRegion(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodGet, "/tables/x/scan", "") // no region param
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestScanBackendError(t *testing.T) {
	s := newTestServer(&fakeBackend{scanErr: errors.New("dynamo timeout")})
	rec := do(s, http.MethodGet, "/tables/x/scan?region=us-east-1", "")
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
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1", `{"conditions":[{"name":"id","op":"eq","value":"1"}],"limit":10}`)
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
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1", `{"conditions":[{"name":"status","op":"eq","value":"active"}]}`)
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
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1", `{"conditions":[{"name":"id","op":"bogus","value":"1"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestQueryMissingRegion(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}})
	rec := do(s, http.MethodPost, "/tables/t/query", `{"conditions":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestQueryNoEffectiveFilter(t *testing.T) {
	s := newTestServer(&fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id"}})
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1", `{"conditions":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestPutItem(t *testing.T) {
	f := &fakeBackend{}
	s := newTestServer(f)
	rec := do(s, http.MethodPost, "/tables/t/item?region=us-east-1", `{"json":"{\"id\":\"1\",\"name\":\"Alice\"}"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if f.putItem["id"] == nil {
		t.Fatalf("expected item passed to PutItem, got %v", f.putItem)
	}
}

func TestPutItemInvalidJSON(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables/t/item?region=us-east-1", `{"json":"not json"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestDeleteItemDerivesKey(t *testing.T) {
	f := &fakeBackend{info: &dynamo.TableInfo{PartitionKey: "id", SortKey: "sk"}}
	s := newTestServer(f)
	rec := do(s, http.MethodDelete, "/tables/t/item?region=us-east-1", `{"json":"{\"id\":\"1\",\"sk\":\"a\",\"extra\":\"x\"}"}`)
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
	rec := do(s, http.MethodDelete, "/tables/t/item?region=us-east-1", `{"json":"{\"other\":\"x\"}"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestCreateTable(t *testing.T) {
	f := &fakeBackend{}
	s := newTestServer(f)
	rec := do(s, http.MethodPost, "/tables?region=us-east-1", `{"name":"NewT","pk":"id","pkType":"S","billingMode":"PAY_PER_REQUEST"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if f.createIn.TableName != "NewT" || f.createIn.PartitionKey != "id" {
		t.Fatalf("createIn=%+v", f.createIn)
	}
}

func TestCreateTableValidates(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables?region=us-east-1", `{"pk":"id"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestPutItemMissingRegion(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables/t/item", `{"json":"{}"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestCreateTableRequiresPKType(t *testing.T) {
	s := newTestServer(&fakeBackend{})
	rec := do(s, http.MethodPost, "/tables?region=us-east-1", `{"name":"T","pk":"id"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestQueryForceScanIgnoresIndexableEquality(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{Name: "t", PartitionKey: "id"},
		scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
			Count: 1,
		},
	})
	// id = 1 would normally Query the table; strategy:scan must force a Scan.
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1",
		`{"conditions":[{"name":"id","op":"eq","value":"1"}],"strategy":{"mode":"scan"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string `json:"mode"`
		Index string `json:"index"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "scan" {
		t.Fatalf("want mode scan, got %q", resp.Mode)
	}
	if resp.Index != "" {
		t.Fatalf("want empty index for scan, got %q", resp.Index)
	}
}

func TestQueryForceIndexUsesGSI(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{
			Name: "t", PartitionKey: "id",
			GSIs: []dynamo.IndexInfo{{Name: "by-email", PartitionKey: "email"}},
		},
		query: &dynamo.QueryResult{
			Items: []map[string]types.AttributeValue{{"email": &types.AttributeValueMemberS{Value: "a@b.com"}}},
			Count: 1,
		},
	})
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1",
		`{"conditions":[{"name":"email","op":"eq","value":"a@b.com"}],"strategy":{"mode":"query","index":"by-email"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string `json:"mode"`
		Index string `json:"index"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "query" {
		t.Fatalf("want mode query, got %q", resp.Mode)
	}
	if resp.Index != "by-email" {
		t.Fatalf("want index by-email, got %q", resp.Index)
	}
}

func TestQueryForceIndexWithoutEqualityIs400(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{
			Name: "t", PartitionKey: "id",
			GSIs: []dynamo.IndexInfo{{Name: "by-email", PartitionKey: "email"}},
		},
	})
	// begins_with on email is not an equality, so forcing the index must 400.
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1",
		`{"conditions":[{"name":"email","op":"begins_with","value":"a"}],"strategy":{"mode":"query","index":"by-email"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestQueryAutoReturnsIndexName(t *testing.T) {
	s := newTestServer(&fakeBackend{
		info: &dynamo.TableInfo{
			Name: "t", PartitionKey: "id",
			GSIs: []dynamo.IndexInfo{{Name: "by-email", PartitionKey: "email"}},
		},
		query: &dynamo.QueryResult{
			Items: []map[string]types.AttributeValue{{"email": &types.AttributeValueMemberS{Value: "a@b.com"}}},
			Count: 1,
		},
	})
	// No strategy -> auto; the planner picks the GSI; the response must report it.
	rec := do(s, http.MethodPost, "/tables/t/query?region=us-east-1",
		`{"conditions":[{"name":"email","op":"eq","value":"a@b.com"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Mode  string `json:"mode"`
		Index string `json:"index"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Mode != "query" || resp.Index != "by-email" {
		t.Fatalf("want query/by-email, got %q/%q", resp.Mode, resp.Index)
	}
}

func TestProfilesHandler(t *testing.T) {
	s := newServer("test-token")
	s.profilesFn = func() ([]string, string, error) {
		return []string{"default", "work"}, "default", nil
	}
	rec := do(s, http.MethodGet, "/profiles", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Profiles []string `json:"profiles"`
		Default  string   `json:"default"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Default != "default" || len(resp.Profiles) != 2 || resp.Profiles[0] != "default" {
		t.Fatalf("got %+v", resp)
	}
}

func TestDiscoverReturnsRegionsAndResetsCache(t *testing.T) {
	s := newServer("test-token")
	s.discoverFn = func(ctx context.Context, profile string) ([]string, error) {
		return []string{"sa-east-1", "us-east-1"}, nil
	}
	s.connectFn = func(profile, region string) (Backend, error) {
		return &fakeBackend{tables: []string{region + "-tableB", region + "-tableA"}}, nil
	}
	rec := do(s, http.MethodPost, "/discover", `{"profile":"work"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Profile string `json:"profile"`
		Regions []struct {
			Region string   `json:"region"`
			Tables []string `json:"tables"`
		} `json:"regions"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Profile != "work" {
		t.Fatalf("want profile work, got %q", resp.Profile)
	}
	if len(resp.Regions) != 2 || resp.Regions[0].Region != "sa-east-1" {
		t.Fatalf("regions not sorted/returned: %+v", resp.Regions)
	}
	// tables sorted within a region
	if resp.Regions[0].Tables[0] != "sa-east-1-tableA" {
		t.Fatalf("tables not sorted: %+v", resp.Regions[0].Tables)
	}
	if s.activeProfile != "work" {
		t.Fatalf("active profile not set: %q", s.activeProfile)
	}
}

func TestRegionRoutingCachesPerRegion(t *testing.T) {
	calls := map[string]int{}
	s := newServer("test-token")
	s.activeProfile = "work"
	s.connectFn = func(profile, region string) (Backend, error) {
		calls[region]++
		return &fakeBackend{scan: &dynamo.ScanResult{
			Items: []map[string]types.AttributeValue{
				{"r": &types.AttributeValueMemberS{Value: region}},
			},
			Count: 1,
		}}, nil
	}
	// First scan in sa-east-1 builds + caches that client.
	rec := do(s, http.MethodGet, "/tables/t/scan?region=sa-east-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]interface{} `json:"items"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Items) != 1 || resp.Items[0]["r"] != "sa-east-1" {
		t.Fatalf("routed to wrong region: %+v", resp.Items)
	}
	// Second scan in the same region reuses the cached client.
	_ = do(s, http.MethodGet, "/tables/t/scan?region=sa-east-1", "")
	if calls["sa-east-1"] != 1 {
		t.Fatalf("want connectFn called once for sa-east-1, got %d", calls["sa-east-1"])
	}
}
