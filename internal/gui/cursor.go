package gui

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// encodeCursor serializes a DynamoDB LastEvaluatedKey into an opaque base64 token.
// DynamoDB key attributes are only S, N, or B, so only those are handled.
// An empty/nil key yields an empty string (meaning "no more pages").
func encodeCursor(key map[string]types.AttributeValue) (string, error) {
	if len(key) == 0 {
		return "", nil
	}
	wire := make(map[string]map[string]string, len(key))
	for name, av := range key {
		switch v := av.(type) {
		case *types.AttributeValueMemberS:
			wire[name] = map[string]string{"S": v.Value}
		case *types.AttributeValueMemberN:
			wire[name] = map[string]string{"N": v.Value}
		case *types.AttributeValueMemberB:
			wire[name] = map[string]string{"B": base64.StdEncoding.EncodeToString(v.Value)}
		default:
			return "", fmt.Errorf("unsupported key attribute type for %q", name)
		}
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// decodeCursor reverses encodeCursor. An empty string yields a nil key.
func decodeCursor(cursor string) (map[string]types.AttributeValue, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	var wire map[string]map[string]string
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	key := make(map[string]types.AttributeValue, len(wire))
	for name, typed := range wire {
		if v, ok := typed["S"]; ok {
			key[name] = &types.AttributeValueMemberS{Value: v}
		} else if v, ok := typed["N"]; ok {
			key[name] = &types.AttributeValueMemberN{Value: v}
		} else if v, ok := typed["B"]; ok {
			b, decErr := base64.StdEncoding.DecodeString(v)
			if decErr != nil {
				return nil, fmt.Errorf("invalid cursor binary for %q: %w", name, decErr)
			}
			key[name] = &types.AttributeValueMemberB{Value: b}
		} else {
			return nil, fmt.Errorf("unsupported cursor attribute for %q", name)
		}
	}
	return key, nil
}
