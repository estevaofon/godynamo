package gui

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
)

// Backend is the set of DynamoDB operations the bridge needs (reads + writes).
// *dynamo.Client satisfies this interface; tests supply a fake.
type Backend interface {
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error)
	ScanTable(ctx context.Context, name string, limit int32,
		startKey map[string]types.AttributeValue,
		filterExpr string, names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error)
	QueryTable(ctx context.Context, input dynamo.QueryInput) (*dynamo.QueryResult, error)
	PutItem(ctx context.Context, tableName string, item map[string]types.AttributeValue) error
	DeleteItem(ctx context.Context, tableName string, key map[string]types.AttributeValue) error
	CreateTable(ctx context.Context, input dynamo.CreateTableInput) error
}

// Compile-time check that the production client satisfies Backend.
var _ Backend = (*dynamo.Client)(nil)
