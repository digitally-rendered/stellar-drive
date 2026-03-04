package stringutil

import (
	"strings"
	"unicode"
)

// ToSnakeCase converts a PascalCase or camelCase string to snake_case.
// Sequences of uppercase letters are treated as a single word, so "HTTPStatus"
// becomes "http_status" rather than "h_t_t_p_status".
//
// Examples:
//
//	ToSnakeCase("firstName")  // "first_name"
//	ToSnakeCase("HTTPStatus") // "http_status"
//	ToSnakeCase("UserID")     // "user_id"
func ToSnakeCase(s string) string {
	if s == "" {
		return ""
	}

	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 4)

	for i, r := range runes {
		if !unicode.IsUpper(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}

		// Determine whether to insert an underscore before this uppercase rune.
		// Insert when:
		//   (a) not at the start, AND
		//   (b) either the previous rune was lowercase/digit,
		//       OR the next rune is lowercase (end of an acronym run, e.g. "HTTPStatus" → "HTTP"/"Status").
		if i > 0 {
			prev := runes[i-1]
			nextIsLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || nextIsLower {
				b.WriteRune('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}

	return b.String()
}

// ToPascalCase converts a snake_case string to PascalCase.
//
// Examples:
//
//	ToPascalCase("first_name") // "FirstName"
//	ToPascalCase("user_id")    // "UserId"
func ToPascalCase(s string) string {
	return joinWords(splitSnake(s), true)
}

// ToCamelCase converts a snake_case string to camelCase.
//
// Examples:
//
//	ToCamelCase("first_name") // "firstName"
//	ToCamelCase("user_id")    // "userId"
func ToCamelCase(s string) string {
	words := splitSnake(s)
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, w := range words {
		if i == 0 {
			b.WriteString(strings.ToLower(w))
		} else {
			b.WriteString(capitalise(w))
		}
	}
	return b.String()
}

// ToKebabCase converts a snake_case or PascalCase/camelCase string to kebab-case.
//
// Examples:
//
//	ToKebabCase("first_name") // "first-name"
//	ToKebabCase("HTTPStatus") // "http-status"
func ToKebabCase(s string) string {
	// First normalise to snake_case, then swap underscores for hyphens.
	snake := ToSnakeCase(strings.ReplaceAll(s, "_", "X_")) // preserve existing underscores
	// The approach above is fragile; instead handle both forms directly.
	snake = toSnakeFromAny(s)
	return strings.ReplaceAll(snake, "_", "-")
}

// toSnakeFromAny handles inputs that may already contain underscores (snake_case)
// or may be PascalCase/camelCase. It normalises to snake_case in both cases.
func toSnakeFromAny(s string) string {
	// If the string contains underscores it is likely already snake_case; just
	// lower-case it. If it has uppercase letters, treat it as camel/Pascal.
	if strings.ContainsAny(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return ToSnakeCase(s)
	}
	return strings.ToLower(s)
}

// Pluralize returns a simple English plural of the given noun.
// Rules applied (in order):
//  1. Words ending in "s", "x", "z", "ch", "sh" → append "es"
//  2. Words ending in consonant + "y"            → replace "y" with "ies"
//  3. Words ending in "f" or "fe"                → replace with "ves"
//  4. Everything else                            → append "s"
//
// Examples:
//
//	Pluralize("cat")    // "cats"
//	Pluralize("church") // "churches"
//	Pluralize("baby")   // "babies"
//	Pluralize("leaf")   // "leaves"
//	Pluralize("class")  // "classes"
func Pluralize(s string) string {
	if s == "" {
		return ""
	}

	lower := strings.ToLower(s)

	switch {
	case strings.HasSuffix(lower, "fe"):
		return s[:len(s)-2] + "ves"
	case strings.HasSuffix(lower, "lf"):
		// e.g. "half" → "halves"
		return s[:len(s)-1] + "ves"
	case strings.HasSuffix(lower, "f") && !strings.HasSuffix(lower, "ff"):
		return s[:len(s)-1] + "ves"
	case strings.HasSuffix(lower, "s") ||
		strings.HasSuffix(lower, "x") ||
		strings.HasSuffix(lower, "z") ||
		strings.HasSuffix(lower, "ch") ||
		strings.HasSuffix(lower, "sh"):
		return s + "es"
	case strings.HasSuffix(lower, "y"):
		// consonant + y → ies; vowel + y → ys
		if len(s) >= 2 && isVowel(rune(lower[len(lower)-2])) {
			return s + "s"
		}
		return s[:len(s)-1] + "ies"
	default:
		return s + "s"
	}
}

// splitSnake splits a snake_case string into its component words.
func splitSnake(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "_")
	words := parts[:0]
	for _, p := range parts {
		if p != "" {
			words = append(words, p)
		}
	}
	return words
}

// joinWords concatenates words, capitalising the first letter of each.
// When capitaliseFirst is true the very first word is also capitalised.
func joinWords(words []string, capitaliseFirst bool) string {
	if len(words) == 0 {
		return ""
	}
	var b strings.Builder
	for i, w := range words {
		if i == 0 && !capitaliseFirst {
			b.WriteString(strings.ToLower(w))
		} else {
			b.WriteString(capitalise(w))
		}
	}
	return b.String()
}

// capitalise upper-cases the first rune of s and lower-cases the rest.
func capitalise(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	for i := 1; i < len(runes); i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
