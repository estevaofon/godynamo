package dynamo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// AWS Regions list
var AWSRegions = []string{
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"af-south-1",
	"ap-east-1",
	"ap-south-1",
	"ap-south-2",
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-northeast-3",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-southeast-3",
	"ap-southeast-4",
	"ca-central-1",
	"eu-central-1",
	"eu-central-2",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-south-1",
	"eu-south-2",
	"eu-north-1",
	"il-central-1",
	"me-south-1",
	"me-central-1",
	"sa-east-1",
}

// RegionInfo contains information about a region with tables
type RegionInfo struct {
	Region     string
	TableCount int
	Tables     []string
}

// DiscoverRegionsWithTables scans all regions and returns those with DynamoDB tables
func DiscoverRegionsWithTables(ctx context.Context, useLocal bool, endpoint string) ([]RegionInfo, error) {
	if useLocal {
		// For local DynamoDB, just return a single "local" region
		cfg, err := config.LoadDefaultConfig(ctx,
			config.WithRegion("us-east-1"),
			config.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider("local", "local", ""),
			),
		)
		if err != nil {
			return nil, err
		}

		client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})

		tables, err := client.ListTables(ctx, &dynamodb.ListTablesInput{})
		if err != nil {
			return nil, err
		}

		return []RegionInfo{{
			Region:     "local",
			TableCount: len(tables.TableNames),
			Tables:     tables.TableNames,
		}}, nil
	}

	var results []RegionInfo
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrent requests
	sem := make(chan struct{}, 10)

	for _, region := range AWSRegions {
		wg.Add(1)
		go func(r string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(r))
			if err != nil {
				return
			}

			client := dynamodb.NewFromConfig(cfg)

			// Quick check - just get the first page
			tables, err := client.ListTables(ctx, &dynamodb.ListTablesInput{
				Limit: aws.Int32(100),
			})
			if err != nil {
				return
			}

			if len(tables.TableNames) > 0 {
				mu.Lock()
				results = append(results, RegionInfo{
					Region:     r,
					TableCount: len(tables.TableNames),
					Tables:     tables.TableNames,
				})
				mu.Unlock()
			}
		}(region)
	}

	wg.Wait()

	return results, nil
}

// Client wraps the DynamoDB client with helper methods
type Client struct {
	db       *dynamodb.Client
	endpoint string
	region   string
}

// ConnectionConfig holds connection settings
type ConnectionConfig struct {
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	UseLocal  bool
}

// NewClient creates a new DynamoDB client
func NewClient(cfg ConnectionConfig) (*Client, error) {
	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithRegion(cfg.Region))

	if cfg.UseLocal {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(context.TODO(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	var dbOpts []func(*dynamodb.Options)
	if cfg.Endpoint != "" {
		dbOpts = append(dbOpts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	client := dynamodb.NewFromConfig(awsCfg, dbOpts...)

	return &Client{
		db:       client,
		endpoint: cfg.Endpoint,
		region:   cfg.Region,
	}, nil
}

// ListTables returns all table names
func (c *Client) ListTables(ctx context.Context) ([]string, error) {
	var tables []string
	var lastEvaluatedTableName *string

	for {
		output, err := c.db.ListTables(ctx, &dynamodb.ListTablesInput{
			ExclusiveStartTableName: lastEvaluatedTableName,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list tables: %w", err)
		}

		tables = append(tables, output.TableNames...)

		if output.LastEvaluatedTableName == nil {
			break
		}
		lastEvaluatedTableName = output.LastEvaluatedTableName
	}

	return tables, nil
}

// TableInfo contains table metadata
type TableInfo struct {
	Name           string
	Status         string
	ItemCount      int64
	SizeBytes      int64
	PartitionKey   string
	PartitionType  string
	SortKey        string
	SortKeyType    string
	GSIs           []IndexInfo
	LSIs           []IndexInfo
	RawJSON        string // Full JSON response from DescribeTable
}

// IndexInfo contains index metadata
type IndexInfo struct {
	Name         string
	PartitionKey string
	SortKey      string
	Status       string
}

// DescribeTable returns table metadata
func (c *Client) DescribeTable(ctx context.Context, tableName string) (*TableInfo, error) {
	output, err := c.db.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}

	// Generate raw JSON from the Table response
	rawJSON, _ := json.MarshalIndent(output.Table, "", "  ")

	info := &TableInfo{
		Name:      *output.Table.TableName,
		Status:    string(output.Table.TableStatus),
		ItemCount: *output.Table.ItemCount,
		SizeBytes: *output.Table.TableSizeBytes,
		RawJSON:   string(rawJSON),
	}

	// Get key schema
	for _, key := range output.Table.KeySchema {
		keyType := ""
		for _, attr := range output.Table.AttributeDefinitions {
			if *attr.AttributeName == *key.AttributeName {
				keyType = string(attr.AttributeType)
				break
			}
		}

		if key.KeyType == types.KeyTypeHash {
			info.PartitionKey = *key.AttributeName
			info.PartitionType = keyType
		} else if key.KeyType == types.KeyTypeRange {
			info.SortKey = *key.AttributeName
			info.SortKeyType = keyType
		}
	}

	// Get GSIs
	for _, gsi := range output.Table.GlobalSecondaryIndexes {
		idx := IndexInfo{
			Name:   *gsi.IndexName,
			Status: string(gsi.IndexStatus),
		}
		for _, key := range gsi.KeySchema {
			if key.KeyType == types.KeyTypeHash {
				idx.PartitionKey = *key.AttributeName
			} else if key.KeyType == types.KeyTypeRange {
				idx.SortKey = *key.AttributeName
			}
		}
		info.GSIs = append(info.GSIs, idx)
	}

	// Get LSIs
	for _, lsi := range output.Table.LocalSecondaryIndexes {
		idx := IndexInfo{
			Name: *lsi.IndexName,
		}
		for _, key := range lsi.KeySchema {
			if key.KeyType == types.KeyTypeHash {
				idx.PartitionKey = *key.AttributeName
			} else if key.KeyType == types.KeyTypeRange {
				idx.SortKey = *key.AttributeName
			}
		}
		info.LSIs = append(info.LSIs, idx)
	}

	return info, nil
}

// ScanResult contains scan output
type ScanResult struct {
	Items            []map[string]types.AttributeValue
	LastEvaluatedKey map[string]types.AttributeValue
	Count            int32
	ScannedCount     int32
}

// ScanTable performs a scan operation
func (c *Client) ScanTable(ctx context.Context, tableName string, limit int32, startKey map[string]types.AttributeValue, filterExpression string, expressionNames map[string]string, expressionValues map[string]interface{}) (*ScanResult, error) {
	input := &dynamodb.ScanInput{
		TableName: aws.String(tableName),
		Limit:     aws.Int32(limit),
	}

	if startKey != nil {
		input.ExclusiveStartKey = startKey
	}

	if filterExpression != "" {
		input.FilterExpression = aws.String(filterExpression)
		
		if len(expressionNames) > 0 {
			input.ExpressionAttributeNames = expressionNames
		}
		
		if len(expressionValues) > 0 {
			attrValues := make(map[string]types.AttributeValue)
			for k, v := range expressionValues {
				attrValues[k] = interfaceToAttributeValue(v)
			}
			input.ExpressionAttributeValues = attrValues
		}
	}

	output, err := c.db.Scan(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to scan table: %w", err)
	}

	return &ScanResult{
		Items:            output.Items,
		LastEvaluatedKey: output.LastEvaluatedKey,
		Count:            output.Count,
		ScannedCount:     output.ScannedCount,
	}, nil
}

// ContinuousScanResult contains results from a continuous scan operation
type ContinuousScanResult struct {
	Items            []map[string]types.AttributeValue
	LastEvaluatedKey map[string]types.AttributeValue
	TotalScanned     int64
	HasMore          bool
	TimedOut         bool
}

// ScanTableContinuous performs a continuous scan until targetCount items are found or table is exhausted
// It will scan in batches and accumulate results until the target is reached
// The scan can be cancelled via context
func (c *Client) ScanTableContinuous(ctx context.Context, tableName string, targetCount int, startKey map[string]types.AttributeValue, filterExpression string, expressionNames map[string]string, expressionValues map[string]interface{}) (*ContinuousScanResult, error) {
	var allItems []map[string]types.AttributeValue
	var lastKey map[string]types.AttributeValue = startKey
	var totalScanned int64 = 0
	batchSize := int32(500) // Scan in larger batches for efficiency

	// Convert expression values once
	var attrValues map[string]types.AttributeValue
	if len(expressionValues) > 0 {
		attrValues = make(map[string]types.AttributeValue)
		for k, v := range expressionValues {
			attrValues[k] = interfaceToAttributeValue(v)
		}
	}

	for {
		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return &ContinuousScanResult{
				Items:            allItems,
				LastEvaluatedKey: lastKey,
				TotalScanned:     totalScanned,
				HasMore:          lastKey != nil,
				TimedOut:         true,
			}, nil
		default:
		}

		input := &dynamodb.ScanInput{
			TableName: aws.String(tableName),
			Limit:     aws.Int32(batchSize),
		}

		if lastKey != nil {
			input.ExclusiveStartKey = lastKey
		}

		if filterExpression != "" {
			input.FilterExpression = aws.String(filterExpression)
			if len(expressionNames) > 0 {
				input.ExpressionAttributeNames = expressionNames
			}
			if attrValues != nil {
				input.ExpressionAttributeValues = attrValues
			}
		}

		output, err := c.db.Scan(ctx, input)
		if err != nil {
			// If context was cancelled, return what we have
			if ctx.Err() != nil {
				return &ContinuousScanResult{
					Items:            allItems,
					LastEvaluatedKey: lastKey,
					TotalScanned:     totalScanned,
					HasMore:          true,
					TimedOut:         true,
				}, nil
			}
			return nil, fmt.Errorf("failed to scan table: %w", err)
		}

		allItems = append(allItems, output.Items...)
		totalScanned += int64(output.ScannedCount)
		lastKey = output.LastEvaluatedKey

		// Check if we have enough items or if we've reached the end
		if len(allItems) >= targetCount || lastKey == nil {
			break
		}
	}

	return &ContinuousScanResult{
		Items:            allItems,
		LastEvaluatedKey: lastKey,
		TotalScanned:     totalScanned,
		HasMore:          lastKey != nil,
		TimedOut:         false,
	}, nil
}

// interfaceToAttributeValue converts a Go interface to DynamoDB AttributeValue
func interfaceToAttributeValue(v interface{}) types.AttributeValue {
	switch val := v.(type) {
	case string:
		return &types.AttributeValueMemberS{Value: val}
	case int:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", val)}
	case int64:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", val)}
	case float64:
		return &types.AttributeValueMemberN{Value: fmt.Sprintf("%v", val)}
	case bool:
		return &types.AttributeValueMemberBOOL{Value: val}
	default:
		return &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", val)}
	}
}

// QueryInput contains query parameters
type QueryInput struct {
	TableName                string
	IndexName                string
	KeyConditionExpression   string
	FilterExpression         string
	ExpressionAttributeNames map[string]string
	ExpressionValues         map[string]interface{}
	Limit                    int32
	ScanIndexForward         bool
	StartKey                 map[string]types.AttributeValue
}

// QueryResult contains query output
type QueryResult struct {
	Items            []map[string]types.AttributeValue
	LastEvaluatedKey map[string]types.AttributeValue
	Count            int32
	ScannedCount     int32
}

// QueryTable performs a query operation
func (c *Client) QueryTable(ctx context.Context, input QueryInput) (*QueryResult, error) {
	queryInput := &dynamodb.QueryInput{
		TableName:              aws.String(input.TableName),
		KeyConditionExpression: aws.String(input.KeyConditionExpression),
		ScanIndexForward:       aws.Bool(input.ScanIndexForward),
	}

	// Convert expression values
	if len(input.ExpressionValues) > 0 {
		attrValues := make(map[string]types.AttributeValue)
		for k, v := range input.ExpressionValues {
			attrValues[k] = interfaceToAttributeValue(v)
		}
		queryInput.ExpressionAttributeValues = attrValues
	}

	if input.IndexName != "" {
		queryInput.IndexName = aws.String(input.IndexName)
	}

	if input.FilterExpression != "" {
		queryInput.FilterExpression = aws.String(input.FilterExpression)
	}

	if input.ExpressionAttributeNames != nil {
		queryInput.ExpressionAttributeNames = input.ExpressionAttributeNames
	}

	if input.Limit > 0 {
		queryInput.Limit = aws.Int32(input.Limit)
	}

	if input.StartKey != nil {
		queryInput.ExclusiveStartKey = input.StartKey
	}

	output, err := c.db.Query(ctx, queryInput)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}

	return &QueryResult{
		Items:            output.Items,
		LastEvaluatedKey: output.LastEvaluatedKey,
		Count:            output.Count,
		ScannedCount:     output.ScannedCount,
	}, nil
}

// PutItem creates or updates an item
func (c *Client) PutItem(ctx context.Context, tableName string, item map[string]types.AttributeValue) error {
	_, err := c.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("failed to put item: %w", err)
	}
	return nil
}

// DeleteItem removes an item
func (c *Client) DeleteItem(ctx context.Context, tableName string, key map[string]types.AttributeValue) error {
	_, err := c.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(tableName),
		Key:       key,
	})
	if err != nil {
		return fmt.Errorf("failed to delete item: %w", err)
	}
	return nil
}

// CreateTableInput contains table creation parameters
type CreateTableInput struct {
	TableName     string
	PartitionKey  string
	PartitionType string
	SortKey       string
	SortKeyType   string
	ReadCapacity  int64
	WriteCapacity int64
	BillingMode   string
}

// CreateTable creates a new table
func (c *Client) CreateTable(ctx context.Context, input CreateTableInput) error {
	keySchema := []types.KeySchemaElement{
		{
			AttributeName: aws.String(input.PartitionKey),
			KeyType:       types.KeyTypeHash,
		},
	}

	attrDefs := []types.AttributeDefinition{
		{
			AttributeName: aws.String(input.PartitionKey),
			AttributeType: types.ScalarAttributeType(input.PartitionType),
		},
	}

	if input.SortKey != "" {
		keySchema = append(keySchema, types.KeySchemaElement{
			AttributeName: aws.String(input.SortKey),
			KeyType:       types.KeyTypeRange,
		})
		attrDefs = append(attrDefs, types.AttributeDefinition{
			AttributeName: aws.String(input.SortKey),
			AttributeType: types.ScalarAttributeType(input.SortKeyType),
		})
	}

	createInput := &dynamodb.CreateTableInput{
		TableName:            aws.String(input.TableName),
		KeySchema:            keySchema,
		AttributeDefinitions: attrDefs,
	}

	if input.BillingMode == "PAY_PER_REQUEST" {
		createInput.BillingMode = types.BillingModePayPerRequest
	} else {
		createInput.BillingMode = types.BillingModeProvisioned
		createInput.ProvisionedThroughput = &types.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(input.ReadCapacity),
			WriteCapacityUnits: aws.Int64(input.WriteCapacity),
		}
	}

	_, err := c.db.CreateTable(ctx, createInput)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// GetItem retrieves a single item
func (c *Client) GetItem(ctx context.Context, tableName string, key map[string]types.AttributeValue) (map[string]types.AttributeValue, error) {
	output, err := c.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(tableName),
		Key:       key,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}
	return output.Item, nil
}

