package button

import (
	"fmt"
	"strconv"
	"strings"
)

var allowedTypes = map[string]bool{
	"string": true,
	"int":    true,
	"bool":   true,
	"enum":   true,
}

// ParseArgDef parses a single --arg spec. Accepted shapes:
//
//	name:type:required|optional                      (string, int, bool)
//	name:enum:required|optional:value1|value2|value3  (enum with choices)
//
// The enum form uses the same outer colon separator as the simple
// types but adds a fourth segment: a pipe-separated list of allowed
// values. Examples:
//
//	env:enum:required:staging|prod|canary
//	log-level:enum:optional:debug|info|warn|error
func ParseArgDef(raw string) (ArgDef, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 3 && len(parts) != 4 {
		return ArgDef{}, &ServiceError{
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("arg %q must have format name:type:required|optional (enum adds a 4th :value1|value2 segment)", raw),
		}
	}

	name := strings.TrimSpace(parts[0])
	typ := strings.TrimSpace(strings.ToLower(parts[1]))
	reqFlag := strings.TrimSpace(strings.ToLower(parts[2]))

	if name == "" {
		return ArgDef{}, &ServiceError{
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("arg %q has empty name", raw),
		}
	}

	if typ == "" || !allowedTypes[typ] {
		return ArgDef{}, &ServiceError{
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("arg %q has invalid type %q (allowed: string, int, bool, enum)", raw, typ),
		}
	}

	var required bool
	switch reqFlag {
	case "required":
		required = true
	case "optional":
		required = false
	default:
		return ArgDef{}, &ServiceError{
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("arg %q must end with 'required' or 'optional', got %q", raw, reqFlag),
		}
	}

	// Enum-only: 4th segment required and must list ≥ 2 values.
	// One-value enum is technically legal but useless — we reject it
	// early so a typo like `env:enum:required:staging` (missing pipe)
	// surfaces as a clear error instead of a degenerate enum.
	var values []string
	if typ == "enum" {
		if len(parts) != 4 {
			return ArgDef{}, &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg %q: enum type requires a 4th segment with pipe-separated values (e.g. name:enum:required:a|b|c)", raw),
			}
		}
		rawValues := strings.Split(parts[3], "|")
		seen := make(map[string]bool, len(rawValues))
		for _, v := range rawValues {
			v = strings.TrimSpace(v)
			if v == "" {
				return ArgDef{}, &ServiceError{
					Code:    "VALIDATION_ERROR",
					Message: fmt.Sprintf("arg %q: enum value set has empty entry", raw),
				}
			}
			if seen[v] {
				return ArgDef{}, &ServiceError{
					Code:    "VALIDATION_ERROR",
					Message: fmt.Sprintf("arg %q: duplicate enum value %q", raw, v),
				}
			}
			seen[v] = true
			values = append(values, v)
		}
		if len(values) < 2 {
			return ArgDef{}, &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg %q: enum needs at least 2 values, got %d", raw, len(values)),
			}
		}
	} else if len(parts) == 4 {
		// Non-enum type with a 4th segment is almost certainly a
		// mistake (typo'd :enum, or mis-copied example). Reject.
		return ArgDef{}, &ServiceError{
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("arg %q: only enum types accept a 4th :value1|value2 segment", raw),
		}
	}

	return ArgDef{Name: name, Type: typ, Required: required, Values: values}, nil
}

// ParsePressArgs parses key=value pairs and validates them against the button's arg definitions.
func ParsePressArgs(raws []string, defs []ArgDef) (map[string]string, error) {
	supplied := make(map[string]string, len(raws))
	for _, raw := range raws {
		idx := strings.Index(raw, "=")
		if idx < 0 {
			return nil, &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg %q must have format key=value", raw),
			}
		}
		key := raw[:idx]
		value := raw[idx+1:]
		if key == "" {
			return nil, &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg %q has empty key", raw),
			}
		}
		if strings.ContainsAny(key, "=\x00") {
			return nil, &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg key %q contains invalid characters", key),
			}
		}
		supplied[key] = value
	}

	defsByName := make(map[string]ArgDef, len(defs))
	for _, d := range defs {
		defsByName[d.Name] = d
	}

	// Check required args are present and validate types
	for _, d := range defs {
		val, ok := supplied[d.Name]
		if !ok {
			if d.Required {
				return nil, &ServiceError{
					Code:    "MISSING_ARG",
					Message: fmt.Sprintf("required argument '%s' (%s) not provided", d.Name, d.Type),
				}
			}
			continue
		}
		if err := validateArgValue(d, val); err != nil {
			return nil, err
		}
	}

	return supplied, nil
}

// validateArgValue checks a supplied press-time value against a
// declared ArgDef. Dispatches on Type; enum also checks membership in
// the Values set.
func validateArgValue(d ArgDef, value string) error {
	switch d.Type {
	case "int":
		if _, err := strconv.Atoi(value); err != nil {
			return &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg '%s' expects int, got %q", d.Name, value),
			}
		}
	case "bool":
		switch strings.ToLower(value) {
		case "true", "false", "1", "0":
		default:
			return &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg '%s' expects bool (true/false/1/0), got %q", d.Name, value),
			}
		}
	case "enum":
		for _, v := range d.Values {
			if v == value {
				return nil
			}
		}
		return &ServiceError{
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("arg '%s' must be one of %s; got %q", d.Name, strings.Join(d.Values, ", "), value),
		}
	}
	return nil
}

func ParseArgDefs(raws []string) ([]ArgDef, error) {
	seen := make(map[string]bool, len(raws))
	defs := make([]ArgDef, 0, len(raws))

	for _, raw := range raws {
		def, err := ParseArgDef(raw)
		if err != nil {
			return nil, err
		}
		if seen[def.Name] {
			return nil, &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("duplicate arg name %q", def.Name),
			}
		}
		seen[def.Name] = true
		defs = append(defs, def)
	}

	return defs, nil
}
