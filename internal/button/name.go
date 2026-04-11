package button

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	nonAlphanumHyphen = regexp.MustCompile(`[^a-z0-9-]`)
	multiHyphen       = regexp.MustCompile(`-{2,}`)
)

func Slugify(input string) string {
	s := strings.ToLower(input)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = nonAlphanumHyphen.ReplaceAllString(s, "")
	s = multiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

var reservedNames = map[string]bool{
	"create": true, "press": true, "list": true, "rm": true,
	"board": true, "drawer": true, "batteries": true, "smash": true,
	"store": true, "history": true, "help": true, "version": true,
	"mcp": true, "serve": true,
}

func IsReservedName(name string) bool {
	return reservedNames[strings.ToLower(name)]
}

func ValidateName(name string) error {
	if name == "" {
		return &ServiceError{Code: "VALIDATION_ERROR", Message: "button name cannot be empty"}
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("invalid button name: %q", name)}
	}
	if len(name) > 128 {
		return &ServiceError{Code: "VALIDATION_ERROR", Message: "button name exceeds 128 characters"}
	}
	if IsReservedName(Slugify(name)) {
		return &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("button name %q is reserved", name)}
	}
	return nil
}
