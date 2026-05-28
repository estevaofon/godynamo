package gui

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestCursorRoundTrip(t *testing.T) {
	key := map[string]types.AttributeValue{
		"pk": &types.AttributeValueMemberS{Value: "user#1"},
		"sk": &types.AttributeValueMemberN{Value: "42"},
	}
	enc, err := encodeCursor(key)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc == "" {
		t.Fatal("expected non-empty cursor")
	}
	dec, err := decodeCursor(enc)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	s, ok := dec["pk"].(*types.AttributeValueMemberS)
	if !ok || s.Value != "user#1" {
		t.Fatalf("pk mismatch: %#v", dec["pk"])
	}
	n, ok := dec["sk"].(*types.AttributeValueMemberN)
	if !ok || n.Value != "42" {
		t.Fatalf("sk mismatch: %#v", dec["sk"])
	}
}

func TestEmptyCursor(t *testing.T) {
	enc, err := encodeCursor(nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if enc != "" {
		t.Fatalf("want empty, got %q", enc)
	}
	dec, err := decodeCursor("")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dec != nil {
		t.Fatalf("want nil, got %#v", dec)
	}
}

func TestDecodeInvalidCursor(t *testing.T) {
	if _, err := decodeCursor("!!!not-base64!!!"); err == nil {
		t.Fatal("expected error for invalid cursor")
	}
}

func TestDecodeCorruptedJSON(t *testing.T) {
	tok := base64.StdEncoding.EncodeToString([]byte("not-json"))
	if _, err := decodeCursor(tok); err == nil {
		t.Fatal("expected error for non-JSON payload")
	}
}

func TestDecodeUnknownTypeTag(t *testing.T) {
	raw, _ := json.Marshal(map[string]map[string]string{"pk": {"X": "val"}})
	tok := base64.StdEncoding.EncodeToString(raw)
	if _, err := decodeCursor(tok); err == nil {
		t.Fatal("expected error for unknown type tag")
	}
}
