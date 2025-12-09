package ui

import (
	"sort"
	"strings"
	"unicode"
)

// FuzzyMatch represents a fuzzy match result
type FuzzyMatch struct {
	Text       string
	Score      int
	MatchedIdx []int
}

// FuzzyFind performs fuzzy matching on a list of strings
func FuzzyFind(pattern string, items []string) []FuzzyMatch {
	if pattern == "" {
		// Return all items with score 0
		results := make([]FuzzyMatch, len(items))
		for i, item := range items {
			results[i] = FuzzyMatch{Text: item, Score: 0}
		}
		return results
	}

	pattern = strings.ToLower(pattern)
	var results []FuzzyMatch

	for _, item := range items {
		score, matchedIdx := fuzzyScore(pattern, strings.ToLower(item))
		if score > 0 {
			results = append(results, FuzzyMatch{
				Text:       item,
				Score:      score,
				MatchedIdx: matchedIdx,
			})
		}
	}

	// Sort by score (higher is better)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// fuzzyScore calculates a fuzzy match score
// Returns score (0 if no match) and matched character indices
func fuzzyScore(pattern, text string) (int, []int) {
	if len(pattern) == 0 {
		return 1, nil
	}
	if len(text) == 0 {
		return 0, nil
	}

	// Quick check: all pattern chars must exist in text
	patternRunes := []rune(pattern)
	textRunes := []rune(text)
	
	patternIdx := 0
	var matchedIdx []int
	score := 0
	lastMatchIdx := -1
	consecutiveBonus := 0

	for textIdx := 0; textIdx < len(textRunes) && patternIdx < len(patternRunes); textIdx++ {
		if textRunes[textIdx] == patternRunes[patternIdx] {
			matchedIdx = append(matchedIdx, textIdx)
			
			// Base score for matching
			score += 10

			// Bonus for consecutive matches
			if lastMatchIdx == textIdx-1 {
				consecutiveBonus++
				score += consecutiveBonus * 5
			} else {
				consecutiveBonus = 0
			}

			// Bonus for matching at start
			if textIdx == 0 {
				score += 25
			}

			// Bonus for matching after separator (_, -, space)
			if textIdx > 0 {
				prevChar := textRunes[textIdx-1]
				if prevChar == '_' || prevChar == '-' || prevChar == ' ' || prevChar == '.' {
					score += 20
				}
				// Bonus for camelCase match
				if unicode.IsLower(prevChar) && unicode.IsUpper(rune(text[textIdx])) {
					score += 15
				}
			}

			lastMatchIdx = textIdx
			patternIdx++
		}
	}

	// All pattern characters must be matched
	if patternIdx < len(patternRunes) {
		return 0, nil
	}

	// Bonus for shorter strings (more relevant)
	score += 100 - len(text)

	// Bonus for exact prefix match
	if strings.HasPrefix(text, pattern) {
		score += 50
	}

	// Bonus for exact match
	if text == pattern {
		score += 100
	}

	return score, matchedIdx
}

// HighlightMatches returns a string with matched characters highlighted
func HighlightMatches(text string, matchedIdx []int, normalStyle, matchStyle func(string) string) string {
	if len(matchedIdx) == 0 {
		return normalStyle(text)
	}

	runes := []rune(text)
	matchSet := make(map[int]bool)
	for _, idx := range matchedIdx {
		matchSet[idx] = true
	}

	var result strings.Builder
	inMatch := false
	var currentRun strings.Builder

	for i, r := range runes {
		isMatch := matchSet[i]
		
		if isMatch != inMatch {
			// Flush current run
			if currentRun.Len() > 0 {
				if inMatch {
					result.WriteString(matchStyle(currentRun.String()))
				} else {
					result.WriteString(normalStyle(currentRun.String()))
				}
				currentRun.Reset()
			}
			inMatch = isMatch
		}
		currentRun.WriteRune(r)
	}

	// Flush remaining
	if currentRun.Len() > 0 {
		if inMatch {
			result.WriteString(matchStyle(currentRun.String()))
		} else {
			result.WriteString(normalStyle(currentRun.String()))
		}
	}

	return result.String()
}


