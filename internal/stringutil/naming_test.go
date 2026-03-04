package stringutil_test

import (
	"testing"

	"github.com/digitally-rendered/stellar-drive/internal/stringutil"
	"github.com/stretchr/testify/assert"
)

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "already lowercase", input: "hello", want: "hello"},
		{name: "camelCase simple", input: "firstName", want: "first_name"},
		{name: "camelCase multiple", input: "myVariableName", want: "my_variable_name"},
		{name: "PascalCase simple", input: "FirstName", want: "first_name"},
		{name: "PascalCase multiple", input: "UserProfileData", want: "user_profile_data"},
		{name: "acronym prefix", input: "HTTPStatus", want: "http_status"},
		{name: "acronym suffix", input: "UserID", want: "user_id"},
		{name: "all caps single", input: "URL", want: "url"},
		{name: "acronym mid-word", input: "parseHTTPResponse", want: "parse_http_response"},
		{name: "single char", input: "A", want: "a"},
		{name: "digits preserved", input: "base64Encode", want: "base64_encode"},
		{name: "leading digit", input: "v2Endpoint", want: "v2_endpoint"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stringutil.ToSnakeCase(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "single word", input: "hello", want: "Hello"},
		{name: "two words", input: "first_name", want: "FirstName"},
		{name: "multiple words", input: "my_variable_name", want: "MyVariableName"},
		{name: "leading underscore ignored", input: "_foo", want: "Foo"},
		{name: "trailing underscore ignored", input: "foo_", want: "Foo"},
		{name: "consecutive underscores", input: "foo__bar", want: "FooBar"},
		{name: "user_id", input: "user_id", want: "UserId"},
		{name: "already title", input: "hello_world", want: "HelloWorld"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stringutil.ToPascalCase(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "single word", input: "hello", want: "hello"},
		{name: "two words", input: "first_name", want: "firstName"},
		{name: "multiple words", input: "my_variable_name", want: "myVariableName"},
		{name: "leading underscore ignored", input: "_foo", want: "foo"},
		{name: "user_id", input: "user_id", want: "userId"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stringutil.ToCamelCase(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestToKebabCase(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		{name: "snake_case input", input: "first_name", want: "first-name"},
		{name: "PascalCase input", input: "FirstName", want: "first-name"},
		{name: "camelCase input", input: "firstName", want: "first-name"},
		{name: "acronym", input: "HTTPStatus", want: "http-status"},
		{name: "single word lower", input: "hello", want: "hello"},
		{name: "multiple snake words", input: "my_variable_name", want: "my-variable-name"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stringutil.ToKebabCase(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: ""},
		// default +s
		{name: "regular noun", input: "cat", want: "cats"},
		{name: "dog", input: "dog", want: "dogs"},
		// s/x/z/ch/sh → +es
		{name: "ends in s", input: "class", want: "classes"},
		{name: "ends in x", input: "box", want: "boxes"},
		{name: "ends in z", input: "buzz", want: "buzzes"},
		{name: "ends in ch", input: "church", want: "churches"},
		{name: "ends in sh", input: "dish", want: "dishes"},
		// consonant+y → ies
		{name: "baby", input: "baby", want: "babies"},
		{name: "city", input: "city", want: "cities"},
		// vowel+y → ys
		{name: "day", input: "day", want: "days"},
		{name: "key", input: "key", want: "keys"},
		// f/fe → ves
		{name: "leaf", input: "leaf", want: "leaves"},
		{name: "knife", input: "knife", want: "knives"},
		{name: "half", input: "half", want: "halves"},
		// ff stays
		{name: "staff", input: "staff", want: "staffs"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stringutil.Pluralize(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}
