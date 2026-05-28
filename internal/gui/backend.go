package gui

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/godynamo/internal/dynamo"
)

// Backend is the narrow set of read-only DynamoDB operations the bridge needs.
// *dynamo.Client satisfies this interface; tests supply a fake.
type Backend interface {
	ListTables(ctx context.Context) ([]string, error)
	DescribeTable(ctx context.Context, name string) (*dynamo.TableInfo, error)
	ScanTable(ctx context.Context, name string, limit int32,
		startKey map[string]types.AttributeValue,
		filterExpr string, names map[string]string, values map[string]interface{}) (*dynamo.ScanResult, error)
}

// Compile-time check that the production client satisfies Backend.
var _ Backend = (*dynamo.Client)(nil)
