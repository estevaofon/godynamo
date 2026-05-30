package ui

import (
	"strings"
	"testing"
)

func TestFuzzyFindEmptyPatternReturnsAll(t *testing.T) {
	items := []string{"alpha", "beta"}
	got := FuzzyFind("", items)
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
}

func TestFuzzyFindNoMatchExcluded(t *testing.T) {
	got := FuzzyFind("xyz", []string{"alpha", "beta"})
	if len(got) != 0 {
		t.Fatalf("want 0 matches, got %d (%v)", len(got), got)
	}
}

func TestFuzzyFindRanksExactAndPrefixHigher(t *testing.T) {
	items := []string{"u_status", "status", "status_history"}
	got := FuzzyFind("status", items)
	if len(got) == 0 {
		t.Fatal("expected matches")
	}
	if got[0].Text != "status" {
		t.Fatalf("want 'status' first, got %q (full: %v)", got[0].Text, got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Fatalf("results not sorted by score: %v", got)
		}
	}
}

func TestFuzzyFindSubsequenceMatches(t *testing.T) {
	got := FuzzyFind("ac", []string{"abc"})
	if len(got) != 1 || got[0].Text != "abc" {
		t.Fatalf("want subsequence match on 'abc', got %v", got)
	}
}

func TestHighlightMatches(t *testing.T) {
	bold := func(s string) string { return "[" + s + "]" }
	plain := func(s string) string { return s }
	got := HighlightMatches("abc", []int{0, 2}, plain, bold)
	if got != "[a]b[c]" {
		t.Fatalf("got %q want %q", got, "[a]b[c]")
	}
}

func TestHighlightMatchesNoMatches(t *testing.T) {
	plain := func(s string) string { return "<" + s + ">" }
	got := HighlightMatches("abc", nil, plain, strings.ToUpper)
	if got != "<abc>" {
		t.Fatalf("got %q", got)
	}
}
