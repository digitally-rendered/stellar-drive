package schema

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// emailRegexp is a simple RFC-5322-ish e-mail address pattern.
var emailRegexp = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// ValidateEnvelope validates the envelope structure: name, version, and schema
// must be present and non-empty/nil.
func ValidateEnvelope(envelope *SchemaEnvelope) error {
	if envelope == nil {
		return fmt.Errorf("envelope is nil")
	}
	if strings.TrimSpace(envelope.Name) == "" {
		return fmt.Errorf("envelope: name is required")
	}
	if strings.TrimSpace(envelope.Version) == "" {
		return fmt.Errorf("envelope %q: version is required", envelope.Name)
	}
	if envelope.Schema == nil {
		return fmt.Errorf("envelope %q: schema is required", envelope.Name)
	}
	return nil
}

// ValidateData validates data against a schema's JSON Schema definition.
// Checks required fields, type compatibility, enum membership, string formats,
// and numeric range constraints.
func ValidateData(def *SchemaDefinition, data map[string]any) error {
	if def == nil {
		return fmt.Errorf("schema definition is nil")
	}
	if data == nil {
		data = map[string]any{}
	}

	var errs []string

	// Required fields.
	for _, req := range def.RequiredFields {
		v, present := data[req]
		if !present || v == nil {
			errs = append(errs, fmt.Sprintf("field %q is required", req))
		}
	}

	// Per-field validation.
	for _, field := range def.Fields {
		value, present := data[field.Name]
		if !present || value == nil {
			// Already caught by required check above.
			continue
		}
		if fieldErrs := validateField(field, value, def.RawSchema); len(fieldErrs) > 0 {
			errs = append(errs, fieldErrs...)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// validateField validates a single value against a FieldDefinition.
func validateField(field FieldDefinition, value any, rawSchema map[string]any) []string {
	var errs []string

	// Enum check first — applies regardless of type.
	if len(field.Enum) > 0 {
		if !enumContains(field.Enum, value) {
			errs = append(errs, fmt.Sprintf("field %q: value %v is not one of %v", field.Name, value, field.Enum))
		}
	}

	switch field.JSONType {
	case "string":
		s, ok := value.(string)
		if !ok {
			errs = append(errs, fmt.Sprintf("field %q: expected string, got %T", field.Name, value))
			break
		}
		errs = append(errs, validateStringFormat(field, s)...)
		errs = append(errs, validateStringConstraints(field, s, rawSchema)...)

	case "number":
		errs = append(errs, validateNumeric(field, value, false, rawSchema)...)

	case "integer":
		errs = append(errs, validateNumeric(field, value, true, rawSchema)...)

	case "boolean":
		if _, ok := value.(bool); !ok {
			errs = append(errs, fmt.Sprintf("field %q: expected boolean, got %T", field.Name, value))
		}

	case "array":
		if _, ok := value.([]any); !ok {
			errs = append(errs, fmt.Sprintf("field %q: expected array, got %T", field.Name, value))
		}

	case "object":
		if _, ok := value.(map[string]any); !ok {
			errs = append(errs, fmt.Sprintf("field %q: expected object, got %T", field.Name, value))
		}
	}

	return errs
}

// validateStringFormat checks format-specific constraints for string fields.
func validateStringFormat(field FieldDefinition, s string) []string {
	switch field.Format {
	case "email":
		if !emailRegexp.MatchString(s) {
			return []string{fmt.Sprintf("field %q: %q is not a valid email address", field.Name, s)}
		}
	case "date-time":
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02T15:04:05Z",
			"2006-01-02",
		}
		parsed := false
		for _, f := range formats {
			if _, err := time.Parse(f, s); err == nil {
				parsed = true
				break
			}
		}
		if !parsed {
			return []string{fmt.Sprintf("field %q: %q is not a valid date-time", field.Name, s)}
		}
	case "uri":
		if !strings.Contains(s, "://") {
			return []string{fmt.Sprintf("field %q: %q is not a valid URI", field.Name, s)}
		}
	}
	return nil
}

// validateStringConstraints checks minLength / maxLength / pattern from the raw schema.
func validateStringConstraints(field FieldDefinition, s string, rawSchema map[string]any) []string {
	prop := rawProp(field.Name, rawSchema)
	if prop == nil {
		return nil
	}

	var errs []string

	if minLen, ok := numericValue(prop["minLength"]); ok {
		if float64(len(s)) < minLen {
			errs = append(errs, fmt.Sprintf("field %q: length %d is less than minLength %v", field.Name, len(s), minLen))
		}
	}
	if maxLen, ok := numericValue(prop["maxLength"]); ok {
		if float64(len(s)) > maxLen {
			errs = append(errs, fmt.Sprintf("field %q: length %d exceeds maxLength %v", field.Name, len(s), maxLen))
		}
	}
	if pattern, ok := prop["pattern"].(string); ok && pattern != "" {
		re, err := regexp.Compile(pattern)
		if err == nil && !re.MatchString(s) {
			errs = append(errs, fmt.Sprintf("field %q: %q does not match pattern %q", field.Name, s, pattern))
		}
	}

	return errs
}

// validateNumeric checks type coercion and minimum/maximum for number/integer.
func validateNumeric(field FieldDefinition, value any, mustBeInteger bool, rawSchema map[string]any) []string {
	var f float64
	switch v := value.(type) {
	case float64:
		f = v
	case float32:
		f = float64(v)
	case int:
		f = float64(v)
	case int32:
		f = float64(v)
	case int64:
		f = float64(v)
	case json2Number:
		// encoding/json number type (if used with UseNumber).
		pf, err := v.Float64()
		if err != nil {
			return []string{fmt.Sprintf("field %q: cannot parse number: %v", field.Name, err)}
		}
		f = pf
	default:
		return []string{fmt.Sprintf("field %q: expected %s, got %T", field.Name, field.JSONType, value)}
	}

	var errs []string

	if mustBeInteger && f != math.Trunc(f) {
		errs = append(errs, fmt.Sprintf("field %q: expected integer, got fractional number %v", field.Name, f))
	}

	prop := rawProp(field.Name, rawSchema)
	if prop == nil {
		return errs
	}

	if min, ok := numericValue(prop["minimum"]); ok && f < min {
		errs = append(errs, fmt.Sprintf("field %q: value %v is less than minimum %v", field.Name, f, min))
	}
	if max, ok := numericValue(prop["maximum"]); ok && f > max {
		errs = append(errs, fmt.Sprintf("field %q: value %v exceeds maximum %v", field.Name, f, max))
	}
	if exMin, ok := numericValue(prop["exclusiveMinimum"]); ok && f <= exMin {
		errs = append(errs, fmt.Sprintf("field %q: value %v must be > %v", field.Name, f, exMin))
	}
	if exMax, ok := numericValue(prop["exclusiveMaximum"]); ok && f >= exMax {
		errs = append(errs, fmt.Sprintf("field %q: value %v must be < %v", field.Name, f, exMax))
	}

	return errs
}

// json2Number is an alias so we can switch on encoding/json.Number without
// importing encoding/json here (the type assertion works on the interface value).
type json2Number interface {
	Float64() (float64, error)
}

// rawProp retrieves the raw property map for a named field from a JSON Schema.
func rawProp(name string, rawSchema map[string]any) map[string]any {
	if rawSchema == nil {
		return nil
	}
	props, ok := rawSchema["properties"]
	if !ok {
		return nil
	}
	propsMap, ok := props.(map[string]any)
	if !ok {
		return nil
	}
	p, ok := propsMap[name]
	if !ok {
		return nil
	}
	pm, _ := p.(map[string]any)
	return pm
}

// numericValue coerces an interface{} to float64, returning false when not a number.
func numericValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// enumContains reports whether value is present in the enum slice.
func enumContains(enum []any, value any) bool {
	for _, e := range enum {
		if fmt.Sprintf("%v", e) == fmt.Sprintf("%v", value) {
			return true
		}
	}
	return false
}
