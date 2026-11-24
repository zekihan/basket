package basket

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func normalizeTitle(title string) string {
	lower := strings.ToLowerSpecial(unicode.TurkishCase, norm.NFC.String(title))
	folded := strings.Map(func(char rune) rune {
		switch {
		case char == 'ı':
			return 'i'
		case unicode.IsMark(char) || unicode.IsPunct(char):
			return -1
		default:
			return char
		}
	}, norm.NFD.String(lower))

	return strings.Join(strings.Fields(folded), " ")
}

func similarTitles(first, second string) bool {
	if first == second {
		return true
	}

	left := strings.Fields(first)
	right := strings.Fields(second)

	hasLongOverlap := slices.ContainsFunc(left, func(token string) bool {
		return utf8.RuneCountInString(token) >= 3 && slices.Contains(right, token)
	})

	return hasLongOverlap && (containsTokens(left, right) || containsTokens(right, left))
}

func containsTokens(haystack, needles []string) bool {
	return !slices.ContainsFunc(needles, func(token string) bool {
		return !slices.Contains(haystack, token)
	})
}

func matchesQuery(title string, tokens []string) bool {
	words := strings.Fields(normalizeTitle(title))

	for _, token := range tokens {
		if !slices.ContainsFunc(words, func(word string) bool {
			return strings.HasPrefix(word, token)
		}) {
			return false
		}
	}

	return true
}
