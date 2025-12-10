package models

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Item represents a DynamoDB item in a displayable format
type Item struct {
	Raw        map[string]types.AttributeValue
	Attributes map[string]interface{}
}

// NewItem creates an Item from DynamoDB AttributeValue map
func NewItem(raw map[string]types.AttributeValue) Item {
	attrs := make(map[string]interface{})
	for k, v := range raw {
		attrs[k] = AttributeValueToInterface(v)
	}
	return Item{
		Raw:        raw,
		Attributes: attrs,
	}
}

// AttributeValueToInterface converts an AttributeValue to a Go interface{}
func AttributeValueToInterface(av types.AttributeValue) interface{} {
	switch v := av.(type) {
	case *types.AttributeValueMemberS:
		return v.Value
	case *types.AttributeValueMemberN:
		if f, err := strconv.ParseFloat(v.Value, 64); err == nil {
			if f == float64(int64(f)) {
				return int64(f)
			}
			return f
		}
		return v.Value
	case *types.AttributeValueMemberB:
		return v.Value
	case *types.AttributeValueMemberBOOL:
		return v.Value
	case *types.AttributeValueMemberNULL:
		return nil
	case *types.AttributeValueMemberSS:
		return v.Value
	case *types.AttributeValueMemberNS:
		nums := make([]float64, len(v.Value))
		for i, n := range v.Value {
			if f, err := strconv.ParseFloat(n, 64); err == nil {
				nums[i] = f
			}
		}
		return nums
	case *types.AttributeValueMemberBS:
		return v.Value
	case *types.AttributeValueMemberL:
		list := make([]interface{}, len(v.Value))
		for i, item := range v.Value {
			list[i] = AttributeValueToInterface(item)
		}
		return list
	case *types.AttributeValueMemberM:
		m := make(map[string]interface{})
		for k, item := range v.Value {
			m[k] = AttributeValueToInterface(item)
		}
		return m
	default:
		return nil
	}
}

// InterfaceToAttributeValue converts a Go interface{} to an AttributeValue
func InterfaceToAttributeValue(v interface{}) types.AttributeValue {
	switch val := v.(type) {
	case string:
		return &types.AttributeValueMemberS{Value: val}
	case int:
		return &types.AttributeValueMemberN{Value: strconv.Itoa(val)}
	case int64:
		return &types.AttributeValueMemberN{Value: strconv.FormatInt(val, 10)}
	case float64:
		return &types.AttributeValueMemberN{Value: strconv.FormatFloat(val, 'f', -1, 64)}
	case bool:
		return &types.AttributeValueMemberBOOL{Value: val}
	case nil:
		return &types.AttributeValueMemberNULL{Value: true}
	case []interface{}:
		list := make([]types.AttributeValue, len(val))
		for i, item := range val {
			list[i] = InterfaceToAttributeValue(item)
		}
		return &types.AttributeValueMemberL{Value: list}
	case map[string]interface{}:
		m := make(map[string]types.AttributeValue)
		for k, item := range val {
			m[k] = InterfaceToAttributeValue(item)
		}
		return &types.AttributeValueMemberM{Value: m}
	case []byte:
		return &types.AttributeValueMemberB{Value: val}
	default:
		return &types.AttributeValueMemberS{Value: fmt.Sprintf("%v", val)}
	}
}

// JSONToItem converts a JSON string to a DynamoDB item
func JSONToItem(jsonStr string) (map[string]types.AttributeValue, error) {
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	item := make(map[string]types.AttributeValue)
	for k, v := range data {
		item[k] = InterfaceToAttributeValue(v)
	}

	return item, nil
}

// ItemToJSON converts a DynamoDB item to JSON string
func ItemToJSON(item map[string]types.AttributeValue, indent bool) (string, error) {
	data := make(map[string]interface{})
	for k, v := range item {
		data[k] = AttributeValueToInterface(v)
	}

	var jsonBytes []byte
	var err error

	if indent {
		jsonBytes, err = json.MarshalIndent(data, "", "  ")
	} else {
		jsonBytes, err = json.Marshal(data)
	}

	if err != nil {
		return "", fmt.Errorf("failed to marshal item: %w", err)
	}

	return string(jsonBytes), nil
}

// GetAttributeType returns the DynamoDB type of an AttributeValue
func GetAttributeType(av types.AttributeValue) string {
	switch av.(type) {
	case *types.AttributeValueMemberS:
		return "S"
	case *types.AttributeValueMemberN:
		return "N"
	case *types.AttributeValueMemberB:
		return "B"
	case *types.AttributeValueMemberBOOL:
		return "BOOL"
	case *types.AttributeValueMemberNULL:
		return "NULL"
	case *types.AttributeValueMemberSS:
		return "SS"
	case *types.AttributeValueMemberNS:
		return "NS"
	case *types.AttributeValueMemberBS:
		return "BS"
	case *types.AttributeValueMemberL:
		return "L"
	case *types.AttributeValueMemberM:
		return "M"
	default:
		return "?"
	}
}

// FormatValue returns a string representation of an AttributeValue
func FormatValue(av types.AttributeValue, maxLen int) string {
	val := AttributeValueToInterface(av)
	
	var str string
	switch v := val.(type) {
	case string:
		str = v
	case nil:
		str = "null"
	default:
		jsonBytes, _ := json.Marshal(v)
		str = string(jsonBytes)
	}

	if maxLen > 0 && len(str) > maxLen {
		return str[:maxLen-3] + "..."
	}
	return str
}

// Connection represents saved connection settings
type Connection struct {
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
	UseLocal  bool   `json:"use_local"`
}

// AppState represents the current application state
type AppState int

const (
	StateConnecting AppState = iota
	StateTableList
	StateTableView
	StateItemView
	StateCreateItem
	StateEditItem
	StateCreateTable
	StateQuery
	StateSettings
)





