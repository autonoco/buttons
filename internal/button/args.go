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
}

func ParseArgDef(raw string) (ArgDef, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 3 {
		return ArgDef{}, &ServiceError{
			Code:    "VALIDATION_ERROR",
			Message: fmt.Sprintf("arg %q must have format name:type:required|optional", raw),
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
			Message: fmt.Sprintf("arg %q has invalid type %q (allowed: string, int, bool)", raw, typ),
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

	return ArgDef{Name: name, Type: typ, Required: required}, nil
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
		if err := validateArgType(d.Name, d.Type, val); err != nil {
			return nil, err
		}
	}

	return supplied, nil
}

func validateArgType(name, typ, value string) error {
	switch typ {
	case "int":
		if _, err := strconv.Atoi(value); err != nil {
			return &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg '%s' expects int, got %q", name, value),
			}
		}
	case "bool":
		switch strings.ToLower(value) {
		case "true", "false", "1", "0":
		default:
			return &ServiceError{
				Code:    "VALIDATION_ERROR",
				Message: fmt.Sprintf("arg '%s' expects bool (true/false/1/0), got %q", name, value),
			}
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
