package storage

import "strings"

type searchTerm struct {
	value     string
	phoneOnly bool
}

func likePattern(value string) string {
	return "%" + escapeLike(strings.TrimSpace(value)) + "%"
}

func escapeLike(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\\', '%', '_':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func searchTerms(query string) []searchTerm {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	terms := []searchTerm{{value: query}}
	digits := digitSearchTerm(query)
	if digits != "" && digits != query {
		terms = append(terms, searchTerm{value: digits, phoneOnly: true})
	}
	return terms
}

func digitSearchTerm(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
