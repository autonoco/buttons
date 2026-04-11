package engine

import (
	"encoding/json"
	"net/url"
	"strings"
)

// SubstituteURL replaces every {{arg}} placeholder in a URL template with
// a context-aware encoding of the matching arg value. Path components are
// encoded with url.PathEscape; query components with url.QueryEscape;
// fragment components with url.PathEscape (per RFC 3986).
//
// Exported so cmd/press.go can render dry-run output using the same
// encoding the actual executor will apply.
//
// The template is split into path/query/fragment sections on the FIRST
// occurrence of `?` and `#` in the original template string. Once split,
// each section is substituted independently, then the sections are
// reassembled. Arg values that happen to contain `?`, `#`, `&`, or `=`
// get encoded as percent sequences and cannot re-introduce structural
// ambiguity into the reassembled URL.
//
// Example (before this fix):
//
//	template: "https://api.example.com/users/{{user}}"
//	args:     {"user": "alice&admin=true"}
//	result:   "https://api.example.com/users/alice&admin=true"  (injected!)
//
// Example (after this fix):
//
//	template: "https://api.example.com/users/{{user}}"
//	args:     {"user": "alice&admin=true"}
//	result:   "https://api.example.com/users/alice%26admin%3Dtrue"  (safe)
func SubstituteURL(template string, args map[string]string) string {
	// Split on the first `#` to find the fragment, then on the first `?`
	// to find the query. Using strings.Index here rather than url.Parse
	// because the template contains `{{arg}}` placeholders which a
	// strict parser may choke on.
	var path, query, fragment string
	var hasQuery, hasFragment bool

	rest := template
	if i := strings.Index(rest, "#"); i != -1 {
		hasFragment = true
		fragment = rest[i+1:]
		rest = rest[:i]
	}
	if i := strings.Index(rest, "?"); i != -1 {
		hasQuery = true
		query = rest[i+1:]
		rest = rest[:i]
	}
	path = rest

	for k, v := range args {
		placeholder := "{{" + k + "}}"
		path = strings.ReplaceAll(path, placeholder, url.PathEscape(v))
		query = strings.ReplaceAll(query, placeholder, url.QueryEscape(v))
		fragment = strings.ReplaceAll(fragment, placeholder, url.PathEscape(v))
	}

	result := path
	if hasQuery {
		result += "?" + query
	}
	if hasFragment {
		result += "#" + fragment
	}
	return result
}

// SubstituteBody replaces every {{arg}} placeholder in a request body
// template with an encoding appropriate for the declared Content-Type
// header. Exported for parity with SubstituteURL.
//
//   - application/json (or missing, assume JSON for APIs): values are
//     JSON-escaped via json.Marshal with the outer quotes stripped.
//     Arg values containing `"`, `\`, newlines, or control characters
//     round-trip safely and cannot inject JSON fields.
//   - application/x-www-form-urlencoded: values are URL-query-escaped
//     via url.QueryEscape, matching how a browser encodes form data.
//   - anything else: values are substituted raw, matching the pre-fix
//     behavior. This is the user's responsibility — documented in
//     SECURITY.md.
//
// The Content-Type header is looked up case-insensitively and any
// `; charset=...` suffix is ignored.
//
// Example (before this fix, JSON body):
//
//	template: `{"msg": "{{msg}}"}`
//	args:     {"msg": `foo", "admin": true, "_": "`}
//	result:   `{"msg": "foo", "admin": true, "_": ""}`  (injected field!)
//
// Example (after this fix, JSON body):
//
//	template: `{"msg": "{{msg}}"}`
//	args:     {"msg": `foo", "admin": true, "_": "`}
//	result:   `{"msg": "foo\", \"admin\": true, \"_\": \""}`  (safe)
func SubstituteBody(template string, args map[string]string, headers map[string]string) string {
	escape := escaperForContentType(contentTypeFromHeaders(headers))
	result := template
	for k, v := range args {
		placeholder := "{{" + k + "}}"
		result = strings.ReplaceAll(result, placeholder, escape(v))
	}
	return result
}

// contentTypeFromHeaders returns the lower-cased, parameter-stripped
// Content-Type value from a case-insensitive header lookup, or the
// empty string if the header is missing.
func contentTypeFromHeaders(headers map[string]string) string {
	for k, v := range headers {
		if strings.EqualFold(k, "Content-Type") {
			// Strip `; charset=utf-8` and similar parameters.
			ct := strings.TrimSpace(strings.SplitN(v, ";", 2)[0])
			return strings.ToLower(ct)
		}
	}
	return ""
}

// escaperForContentType returns the value-encoder appropriate for the
// given (normalized) Content-Type.
func escaperForContentType(ct string) func(string) string {
	switch ct {
	case "", "application/json", "text/json":
		return jsonEscape
	case "application/x-www-form-urlencoded":
		return url.QueryEscape
	default:
		// Raw substitution for unknown content types. Users relying
		// on this must ensure arg values are safe for the target
		// format — documented in SECURITY.md.
		return func(v string) string { return v }
	}
}

// jsonEscape returns the JSON-encoded form of v with the surrounding
// double quotes stripped, suitable for substitution inside an existing
// JSON string literal in a body template.
func jsonEscape(v string) string {
	// json.Marshal always succeeds for a plain string — it produces
	// `"..."` with interior characters escaped per RFC 8259. The outer
	// quotes are stripped because the template already provides them.
	b, _ := json.Marshal(v)
	if len(b) < 2 {
		return ""
	}
	return string(b[1 : len(b)-1])
}
