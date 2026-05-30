package models

import (
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func TestAttributeValueToInterface(t *testing.T) {
	cases := []struct {
		name string
		in   types.AttributeValue
		want interface{}
	}{
		{"string", &types.AttributeValueMemberS{Value: "hi"}, "hi"},
		{"int", &types.AttributeValueMemberN{Value: "42"}, int64(42)},
		{"float", &types.AttributeValueMemberN{Value: "4.5"}, 4.5},
		{"bool", &types.AttributeValueMemberBOOL{Value: true}, true},
		{"null", &types.AttributeValueMemberNULL{Value: true}, nil},
		{"stringset", &types.AttributeValueMemberSS{Value: []string{"a", "b"}}, []string{"a", "b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := AttributeValueToInterface(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v want %#v", got, c.want)
			}
		})
	}
}

func TestAttributeValueToInterfaceNested(t *testing.T) {
	in := &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
		"name": &types.AttributeValueMemberS{Value: "x"},
		"tags": &types.AttributeValueMemberL{Value: []types.AttributeValue{
			&types.AttributeValueMemberN{Value: "1"},
		}},
	}}
	got := AttributeValueToInterface(in).(map[string]interface{})
	if got["name"] != "x" {
		t.Fatalf("name=%v", got["name"])
	}
	if !reflect.DeepEqual(got["tags"], []interface{}{int64(1)}) {
		t.Fatalf("tags=%#v", got["tags"])
	}
}

// Bug fix #2: large integers must NOT lose precision (ParseFloat would round
// 2^53+1 down to 2^53). Fails before the fix, passes after.
func TestAttributeValueToInterfaceLargeIntPrecision(t *testing.T) {
	in := &types.AttributeValueMemberN{Value: "9007199254740993"} // 2^53 + 1
	got := AttributeValueToInterface(in)
	if got != int64(9007199254740993) {
		t.Fatalf("large int lost precision: got %#v want int64(9007199254740993)", got)
	}
}

func TestInterfaceToAttributeValue(t *testing.T) {
	cases := []struct {
		name string
		in   interface{}
		want types.AttributeValue
	}{
		{"string", "hi", &types.AttributeValueMemberS{Value: "hi"}},
		{"int", 7, &types.AttributeValueMemberN{Value: "7"}},
		{"int64", int64(7), &types.AttributeValueMemberN{Value: "7"}},
		{"float", 4.5, &types.AttributeValueMemberN{Value: "4.5"}},
		{"bool", true, &types.AttributeValueMemberBOOL{Value: true}},
		{"nil", nil, &types.AttributeValueMemberNULL{Value: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := InterfaceToAttributeValue(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %#v want %#v", got, c.want)
			}
		})
	}
}

func TestRoundTripItemJSON(t *testing.T) {
	item := map[string]types.AttributeValue{
		"id":     &types.AttributeValueMemberS{Value: "abc"},
		"age":    &types.AttributeValueMemberN{Value: "30"},
		"price":  &types.AttributeValueMemberN{Value: "9.99"},
		"active": &types.AttributeValueMemberBOOL{Value: true},
	}
	jsonStr, err := ItemToJSON(item, false)
	if err != nil {
		t.Fatalf("ItemToJSON: %v", err)
	}
	back, err := JSONToItem(jsonStr)
	if err != nil {
		t.Fatalf("JSONToItem: %v", err)
	}
	if !reflect.DeepEqual(back, item) {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", back, item)
	}
}

func TestGetAttributeType(t *testing.T) {
	cases := []struct {
		in   types.AttributeValue
		want string
	}{
		{&types.AttributeValueMemberS{}, "S"},
		{&types.AttributeValueMemberN{}, "N"},
		{&types.AttributeValueMemberBOOL{}, "BOOL"},
		{&types.AttributeValueMemberNULL{}, "NULL"},
		{&types.AttributeValueMemberL{}, "L"},
		{&types.AttributeValueMemberM{}, "M"},
	}
	for _, c := range cases {
		if got := GetAttributeType(c.in); got != c.want {
			t.Errorf("got %q want %q", got, c.want)
		}
	}
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		name   string
		in     types.AttributeValue
		maxLen int
		want   string
	}{
		{"null", &types.AttributeValueMemberNULL{Value: true}, 0, "null"},
		{"short no trunc", &types.AttributeValueMemberS{Value: "hi"}, 10, "hi"},
		{"ascii trunc", &types.AttributeValueMemberS{Value: "abcdefghij"}, 8, "abcde..."},
		// Bug fix #1: rune-aware truncation must not split a multibyte rune.
		{"multibyte trunc", &types.AttributeValueMemberS{Value: "ααααα"}, 4, "α..."},
		// Bug fix #1: small maxLen must not panic (was str[:maxLen-3]).
		{"maxLen 1", &types.AttributeValueMemberS{Value: "hello"}, 1, "h"},
		{"maxLen 2", &types.AttributeValueMemberS{Value: "hello"}, 2, "he"},
		{"maxLen 3", &types.AttributeValueMemberS{Value: "hello"}, 3, "hel"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatValue(c.in, c.maxLen); got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestNewItem(t *testing.T) {
	raw := map[string]types.AttributeValue{
		"id": &types.AttributeValueMemberS{Value: "1"},
		"n":  &types.AttributeValueMemberN{Value: "5"},
	}
	item := NewItem(raw)
	if item.Attributes["id"] != "1" || item.Attributes["n"] != int64(5) {
		t.Fatalf("attrs=%#v", item.Attributes)
	}
	if !reflect.DeepEqual(item.Raw, raw) {
		t.Fatal("raw not preserved")
	}
}

func TestAttributeValueToInterfaceBinaryAndSets(t *testing.T) {
	if got := AttributeValueToInterface(&types.AttributeValueMemberB{Value: []byte{1, 2}}); !reflect.DeepEqual(got, []byte{1, 2}) {
		t.Errorf("B: got %#v", got)
	}
	if got := AttributeValueToInterface(&types.AttributeValueMemberNS{Value: []string{"1", "2"}}); !reflect.DeepEqual(got, []float64{1, 2}) {
		t.Errorf("NS: got %#v", got)
	}
	if got := AttributeValueToInterface(&types.AttributeValueMemberBS{Value: [][]byte{{1}}}); !reflect.DeepEqual(got, [][]byte{{1}}) {
		t.Errorf("BS: got %#v", got)
	}
}

func TestInterfaceToAttributeValueListMapBytes(t *testing.T) {
	got := InterfaceToAttributeValue([]interface{}{"a", int64(1)})
	if l, ok := got.(*types.AttributeValueMemberL); !ok || len(l.Value) != 2 {
		t.Fatalf("list: %#v", got)
	}
	got = InterfaceToAttributeValue(map[string]interface{}{"k": "v"})
	if m, ok := got.(*types.AttributeValueMemberM); !ok || len(m.Value) != 1 {
		t.Fatalf("map: %#v", got)
	}
	got = InterfaceToAttributeValue([]byte{1, 2})
	if b, ok := got.(*types.AttributeValueMemberB); !ok || len(b.Value) != 2 {
		t.Fatalf("bytes: %#v", got)
	}
}

func TestJSONToItemInvalid(t *testing.T) {
	if _, err := JSONToItem("{not json"); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestItemToJSONIndented(t *testing.T) {
	item := map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "1"}}
	got, err := ItemToJSON(item, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("indented output should contain a newline: %q", got)
	}
}

func TestFormatValueNonStringMarshals(t *testing.T) {
	if got := FormatValue(&types.AttributeValueMemberN{Value: "42"}, 0); got != "42" {
		t.Fatalf("number format: %q", got)
	}
	if got := FormatValue(&types.AttributeValueMemberBOOL{Value: true}, 0); got != "true" {
		t.Fatalf("bool format: %q", got)
	}
}
