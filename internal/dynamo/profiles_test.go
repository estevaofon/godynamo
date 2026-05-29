package dynamo

import (
	"reflect"
	"strings"
	"testing"
)

func TestListProfilesFromReaderParsesSections(t *testing.T) {
	in := "[default]\naws_access_key_id = A\n\n[work]\naws_access_key_id = B\n[ personal ]\n"
	names, def := ListProfilesFromReader(strings.NewReader(in))
	if def != "default" {
		t.Fatalf("want default, got %q", def)
	}
	want := []string{"default", "work", "personal"} // file order
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("want %v, got %v", want, names)
	}
}

func TestListProfilesFromReaderNoDefault(t *testing.T) {
	names, def := ListProfilesFromReader(strings.NewReader("[work]\n[home]\n"))
	if def != "" {
		t.Fatalf("want empty default, got %q", def)
	}
	if !reflect.DeepEqual(names, []string{"work", "home"}) {
		t.Fatalf("got %v", names)
	}
}

func TestListProfilesFromReaderIgnoresNoise(t *testing.T) {
	names, def := ListProfilesFromReader(strings.NewReader("# comment\n\n  \nkey = val\n"))
	if len(names) != 0 || def != "" {
		t.Fatalf("want empty, got %v / %q", names, def)
	}
}

func TestOrderProfilesDefaultFirstThenSorted(t *testing.T) {
	got := orderProfiles([]string{"work", "default", "alpha"}, "default")
	want := []string{"default", "alpha", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}
