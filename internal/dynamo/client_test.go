package dynamo

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// fakeAPI implements dynamoAPI with canned outputs — NEVER touches AWS.
// list/scan outputs are returned in sequence to exercise pagination loops.
type fakeAPI struct {
	listOuts  []*dynamodb.ListTablesOutput
	listCalls int
	describe  *dynamodb.DescribeTableOutput
	scanOuts  []*dynamodb.ScanOutput
	scanCalls int
	scanErr   error
	query     *dynamodb.QueryOutput
	getOut    *dynamodb.GetItemOutput
	putErr    error
	delErr    error
	createErr error

	lastScan   *dynamodb.ScanInput
	lastQuery  *dynamodb.QueryInput
	lastCreate *dynamodb.CreateTableInput
	lastPut    *dynamodb.PutItemInput
	lastDelete *dynamodb.DeleteItemInput
}

func (f *fakeAPI) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	out := f.listOuts[f.listCalls]
	f.listCalls++
	return out, nil
}
func (f *fakeAPI) DescribeTable(_ context.Context, _ *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return f.describe, nil
}
func (f *fakeAPI) Scan(_ context.Context, in *dynamodb.ScanInput, _ ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	f.lastScan = in
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	out := f.scanOuts[f.scanCalls]
	f.scanCalls++
	return out, nil
}
func (f *fakeAPI) Query(_ context.Context, in *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	f.lastQuery = in
	return f.query, nil
}
func (f *fakeAPI) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.lastPut = in
	return &dynamodb.PutItemOutput{}, f.putErr
}
func (f *fakeAPI) DeleteItem(_ context.Context, in *dynamodb.DeleteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error) {
	f.lastDelete = in
	return &dynamodb.DeleteItemOutput{}, f.delErr
}
func (f *fakeAPI) CreateTable(_ context.Context, in *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	f.lastCreate = in
	return &dynamodb.CreateTableOutput{}, f.createErr
}
func (f *fakeAPI) GetItem(_ context.Context, _ *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return f.getOut, nil
}

func newTestClient(f *fakeAPI) *Client {
	return &Client{db: f, region: "us-east-1"}
}

func TestListTablesPaginates(t *testing.T) {
	f := &fakeAPI{listOuts: []*dynamodb.ListTablesOutput{
		{TableNames: []string{"a", "b"}, LastEvaluatedTableName: aws.String("b")},
		{TableNames: []string{"c"}},
	}}
	got, err := newTestClient(f).ListTables(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v want [a b c]", got)
	}
	if f.listCalls != 2 {
		t.Fatalf("expected 2 paginated calls, got %d", f.listCalls)
	}
}

func TestDescribeTableParsesSchema(t *testing.T) {
	f := &fakeAPI{describe: &dynamodb.DescribeTableOutput{Table: &types.TableDescription{
		TableName:      aws.String("Users"),
		TableStatus:    types.TableStatusActive,
		ItemCount:      aws.Int64(10),
		TableSizeBytes: aws.Int64(2048),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeN},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndexDescription{
			{IndexName: aws.String("gsi1"), IndexStatus: types.IndexStatusActive,
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("gpk"), KeyType: types.KeyTypeHash},
				}},
		},
	}}}
	info, err := newTestClient(f).DescribeTable(context.Background(), "Users")
	if err != nil {
		t.Fatal(err)
	}
	if info.PartitionKey != "pk" || info.PartitionType != "S" {
		t.Errorf("partition: %q/%q", info.PartitionKey, info.PartitionType)
	}
	if info.SortKey != "sk" || info.SortKeyType != "N" {
		t.Errorf("sort: %q/%q", info.SortKey, info.SortKeyType)
	}
	if len(info.GSIs) != 1 || info.GSIs[0].Name != "gsi1" || info.GSIs[0].PartitionKey != "gpk" {
		t.Errorf("gsi: %+v", info.GSIs)
	}
	if info.ItemCount != 10 || info.SizeBytes != 2048 {
		t.Errorf("counts: %d/%d", info.ItemCount, info.SizeBytes)
	}
}

func TestScanTablePassesFilterAndConvertsValues(t *testing.T) {
	f := &fakeAPI{scanOuts: []*dynamodb.ScanOutput{{
		Items: []map[string]types.AttributeValue{
			{"id": &types.AttributeValueMemberS{Value: "1"}},
		},
		Count:        1,
		ScannedCount: 5,
	}}}
	res, err := newTestClient(f).ScanTable(context.Background(), "T", 100, nil,
		"#a = :v", map[string]string{"#a": "name"}, map[string]interface{}{":v": "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Count != 1 || res.ScannedCount != 5 || len(res.Items) != 1 {
		t.Fatalf("result=%+v", res)
	}
	if aws.ToString(f.lastScan.FilterExpression) != "#a = :v" {
		t.Errorf("filter not passed: %v", f.lastScan.FilterExpression)
	}
	v, ok := f.lastScan.ExpressionAttributeValues[":v"].(*types.AttributeValueMemberS)
	if !ok || v.Value != "alice" {
		t.Errorf("value not converted: %#v", f.lastScan.ExpressionAttributeValues[":v"])
	}
}

func TestScanTableContinuousAccumulatesAcrossPages(t *testing.T) {
	f := &fakeAPI{scanOuts: []*dynamodb.ScanOutput{
		{Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "1"}}},
			ScannedCount: 3, LastEvaluatedKey: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "1"}}},
		{Items: []map[string]types.AttributeValue{{"id": &types.AttributeValueMemberS{Value: "2"}}},
			ScannedCount: 4},
	}}
	res, err := newTestClient(f).ScanTableContinuous(context.Background(), "T", 10, nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("want 2 accumulated items, got %d", len(res.Items))
	}
	if res.TotalScanned != 7 {
		t.Fatalf("TotalScanned=%d want 7", res.TotalScanned)
	}
	if res.HasMore || res.TimedOut {
		t.Fatalf("expected exhausted clean: hasMore=%v timedOut=%v", res.HasMore, res.TimedOut)
	}
}

func TestScanTableContinuousCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeAPI{}
	res, err := newTestClient(f).ScanTableContinuous(ctx, "T", 10, nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatal("cancelled context should set TimedOut=true")
	}
	if f.scanCalls != 0 {
		t.Fatalf("cancelled context must not call Scan, got %d calls", f.scanCalls)
	}
}

func TestQueryTablePassesIndexAndLimit(t *testing.T) {
	f := &fakeAPI{query: &dynamodb.QueryOutput{Count: 2}}
	_, err := newTestClient(f).QueryTable(context.Background(), QueryInput{
		TableName:              "T",
		IndexName:              "gsi1",
		KeyConditionExpression: "#a = :v",
		ExpressionValues:       map[string]interface{}{":v": 5},
		Limit:                  25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if aws.ToString(f.lastQuery.IndexName) != "gsi1" {
		t.Errorf("index not passed: %v", f.lastQuery.IndexName)
	}
	if aws.ToInt32(f.lastQuery.Limit) != 25 {
		t.Errorf("limit not passed: %v", f.lastQuery.Limit)
	}
	n, ok := f.lastQuery.ExpressionAttributeValues[":v"].(*types.AttributeValueMemberN)
	if !ok || n.Value != "5" {
		t.Errorf("value not converted: %#v", f.lastQuery.ExpressionAttributeValues[":v"])
	}
}

func TestCreateTableBillingModes(t *testing.T) {
	t.Run("pay per request", func(t *testing.T) {
		f := &fakeAPI{}
		err := newTestClient(f).CreateTable(context.Background(), CreateTableInput{
			TableName: "T", PartitionKey: "pk", PartitionType: "S", BillingMode: "PAY_PER_REQUEST",
		})
		if err != nil {
			t.Fatal(err)
		}
		if f.lastCreate.BillingMode != types.BillingModePayPerRequest {
			t.Errorf("billing=%v", f.lastCreate.BillingMode)
		}
		if f.lastCreate.ProvisionedThroughput != nil {
			t.Error("PAY_PER_REQUEST must not set provisioned throughput")
		}
	})
	t.Run("provisioned with sort key", func(t *testing.T) {
		f := &fakeAPI{}
		err := newTestClient(f).CreateTable(context.Background(), CreateTableInput{
			TableName: "T", PartitionKey: "pk", PartitionType: "S",
			SortKey: "sk", SortKeyType: "N", BillingMode: "PROVISIONED",
			ReadCapacity: 5, WriteCapacity: 7,
		})
		if err != nil {
			t.Fatal(err)
		}
		if f.lastCreate.BillingMode != types.BillingModeProvisioned {
			t.Errorf("billing=%v", f.lastCreate.BillingMode)
		}
		if aws.ToInt64(f.lastCreate.ProvisionedThroughput.ReadCapacityUnits) != 5 {
			t.Errorf("read cap=%v", f.lastCreate.ProvisionedThroughput.ReadCapacityUnits)
		}
		if len(f.lastCreate.KeySchema) != 2 {
			t.Errorf("expected pk+sk schema, got %d", len(f.lastCreate.KeySchema))
		}
	})
}

func TestPutAndDeletePropagateErrors(t *testing.T) {
	f := &fakeAPI{putErr: errors.New("boom")}
	if err := newTestClient(f).PutItem(context.Background(), "T", nil); err == nil {
		t.Fatal("PutItem should propagate the error")
	}
	f2 := &fakeAPI{delErr: errors.New("boom")}
	if err := newTestClient(f2).DeleteItem(context.Background(), "T", nil); err == nil {
		t.Fatal("DeleteItem should propagate the error")
	}
}

func TestGetItemReturnsItem(t *testing.T) {
	f := &fakeAPI{getOut: &dynamodb.GetItemOutput{Item: map[string]types.AttributeValue{
		"id": &types.AttributeValueMemberS{Value: "1"},
	}}}
	got, err := newTestClient(f).GetItem(context.Background(), "T", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got["id"].(*types.AttributeValueMemberS).Value != "1" {
		t.Fatalf("got %#v", got)
	}
}

func TestInterfaceToAttributeValueConversions(t *testing.T) {
	cases := []struct {
		in   interface{}
		want string
	}{
		{"s", "S"}, {7, "N"}, {int64(9), "N"}, {3.14, "N"}, {true, "BOOL"},
	}
	for _, c := range cases {
		got := interfaceToAttributeValue(c.in)
		if tag := memberTag(got); tag != c.want {
			t.Errorf("%v: got %s want %s", c.in, tag, c.want)
		}
	}
}

func memberTag(av types.AttributeValue) string {
	switch av.(type) {
	case *types.AttributeValueMemberS:
		return "S"
	case *types.AttributeValueMemberN:
		return "N"
	case *types.AttributeValueMemberBOOL:
		return "BOOL"
	default:
		return "?"
	}
}
