package engine

import "testing"

func TestSubstituteURL_Path(t *testing.T) {
	tests := []struct {
		name     string
		template string
		args     map[string]string
		want     string
	}{
		{
			name:     "plain path substitution",
			template: "https://api.example.com/users/{{user}}",
			args:     map[string]string{"user": "alice"},
			want:     "https://api.example.com/users/alice",
		},
		{
			name:     "path with special characters is escaped",
			template: "https://api.example.com/users/{{user}}",
			args:     map[string]string{"user": "alice bob"},
			want:     "https://api.example.com/users/alice%20bob",
		},
		{
			// The attacker wants to break out of the path segment into
			// a query string. PathEscape encodes `?` as %3F so the
			// backend parser sees the whole thing as a single path
			// component. Note: `&` and `=` are valid sub-delims in a
			// path segment per RFC 3986 and are intentionally NOT
			// escaped by PathEscape — well-behaved backends don't
			// misparse them there.
			name:     "query-escape attempt from path arg is prevented",
			template: "https://api.example.com/users/{{user}}",
			args:     map[string]string{"user": "alice?secret=stolen"},
			want:     "https://api.example.com/users/alice%3Fsecret=stolen",
		},
		{
			// Same idea for fragments — `#` marks the fragment start in
			// URL parsing, so unescaped it would let an attacker peel
			// off a fragment from what should be a path segment.
			name:     "fragment-escape attempt from path arg is prevented",
			template: "https://api.example.com/users/{{user}}",
			args:     map[string]string{"user": "alice#evil"},
			want:     "https://api.example.com/users/alice%23evil",
		},
		{
			name:     "slash in path arg is escaped to prevent path escape",
			template: "https://api.example.com/files/{{name}}",
			args:     map[string]string{"name": "../etc/passwd"},
			want:     "https://api.example.com/files/..%2Fetc%2Fpasswd",
		},
		{
			name:     "unicode in path arg survives",
			template: "https://api.example.com/users/{{user}}",
			args:     map[string]string{"user": "日本語"},
			want:     "https://api.example.com/users/%E6%97%A5%E6%9C%AC%E8%AA%9E",
		},
	}
	runSubstituteURLTests(t, tests)
}

func TestSubstituteURL_Query(t *testing.T) {
	tests := []struct {
		name     string
		template string
		args     map[string]string
		want     string
	}{
		{
			name:     "plain query substitution",
			template: "https://wttr.in/?format={{fmt}}",
			args:     map[string]string{"fmt": "j1"},
			want:     "https://wttr.in/?format=j1",
		},
		{
			name:     "query injection via query arg is prevented",
			template: "https://api.example.com/search?q={{query}}",
			args:     map[string]string{"query": "foo&secret=stolen"},
			want:     "https://api.example.com/search?q=foo%26secret%3Dstolen",
		},
		{
			name:     "equals in query arg is escaped",
			template: "https://api.example.com/search?q={{query}}",
			args:     map[string]string{"query": "a=b"},
			want:     "https://api.example.com/search?q=a%3Db",
		},
		{
			name:     "fragment marker in query arg is escaped",
			template: "https://api.example.com/search?q={{query}}",
			args:     map[string]string{"query": "foo#section"},
			want:     "https://api.example.com/search?q=foo%23section",
		},
		{
			name:     "multiple query args all escaped independently",
			template: "https://api.example.com/search?q={{q}}&limit={{n}}",
			args:     map[string]string{"q": "hello world", "n": "10"},
			want:     "https://api.example.com/search?q=hello+world&limit=10",
		},
	}
	runSubstituteURLTests(t, tests)
}

func TestSubstituteURL_Fragment(t *testing.T) {
	tests := []struct {
		name     string
		template string
		args     map[string]string
		want     string
	}{
		{
			name:     "plain fragment substitution",
			template: "https://docs.example.com/#{{section}}",
			args:     map[string]string{"section": "intro"},
			want:     "https://docs.example.com/#intro",
		},
		{
			name:     "fragment with special chars escaped",
			template: "https://docs.example.com/#{{section}}",
			args:     map[string]string{"section": "a/b c"},
			want:     "https://docs.example.com/#a%2Fb%20c",
		},
	}
	runSubstituteURLTests(t, tests)
}

func TestSubstituteURL_Mixed(t *testing.T) {
	tests := []struct {
		name     string
		template string
		args     map[string]string
		want     string
	}{
		{
			name:     "path + query + fragment all substituted with correct encoders",
			template: "https://api.example.com/users/{{user}}/posts?filter={{filter}}#{{anchor}}",
			args: map[string]string{
				"user":   "alice bob",
				"filter": "x&y",
				"anchor": "top section",
			},
			want: "https://api.example.com/users/alice%20bob/posts?filter=x%26y#top%20section",
		},
		{
			name:     "same arg in path and query gets different encoding",
			template: "https://api.example.com/{{thing}}?search={{thing}}",
			args:     map[string]string{"thing": "a b"},
			want:     "https://api.example.com/a%20b?search=a+b",
		},
		{
			name:     "no args leaves template unchanged",
			template: "https://api.example.com/users/alice",
			args:     map[string]string{},
			want:     "https://api.example.com/users/alice",
		},
	}
	runSubstituteURLTests(t, tests)
}

func runSubstituteURLTests(t *testing.T, tests []struct {
	name     string
	template string
	args     map[string]string
	want     string
}) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubstituteURL(tt.template, tt.args)
			if got != tt.want {
				t.Errorf("SubstituteURL() got:\n  %s\nwant:\n  %s", got, tt.want)
			}
		})
	}
}

func TestSubstituteBody_JSON(t *testing.T) {
	headers := map[string]string{"Content-Type": "application/json"}

	tests := []struct {
		name     string
		template string
		args     map[string]string
		want     string
	}{
		{
			name:     "plain string value",
			template: `{"msg": "{{msg}}"}`,
			args:     map[string]string{"msg": "hello"},
			want:     `{"msg": "hello"}`,
		},
		{
			name:     "quote in value is escaped",
			template: `{"msg": "{{msg}}"}`,
			args:     map[string]string{"msg": `she said "hi"`},
			want:     `{"msg": "she said \"hi\""}`,
		},
		{
			name:     "backslash in value is escaped",
			template: `{"path": "{{p}}"}`,
			args:     map[string]string{"p": `C:\Users`},
			want:     `{"path": "C:\\Users"}`,
		},
		{
			name:     "newline in value is escaped",
			template: `{"text": "{{t}}"}`,
			args:     map[string]string{"t": "line1\nline2"},
			want:     `{"text": "line1\nline2"}`,
		},
		{
			name:     "JSON injection attempt is neutralized",
			template: `{"msg": "{{msg}}"}`,
			args:     map[string]string{"msg": `foo", "admin": true, "_": "`},
			want:     `{"msg": "foo\", \"admin\": true, \"_\": \""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SubstituteBody(tt.template, tt.args, headers)
			if got != tt.want {
				t.Errorf("SubstituteBody() got:\n  %s\nwant:\n  %s", got, tt.want)
			}
		})
	}
}

func TestSubstituteBody_JSONDefaultWhenContentTypeMissing(t *testing.T) {
	// An HTTP button with no explicit Content-Type should still get JSON
	// escaping — APIs default to JSON in our target audience.
	got := SubstituteBody(
		`{"msg": "{{msg}}"}`,
		map[string]string{"msg": `foo"bar`},
		map[string]string{},
	)
	want := `{"msg": "foo\"bar"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteBody_JSONContentTypeCaseInsensitiveWithCharset(t *testing.T) {
	got := SubstituteBody(
		`{"msg": "{{msg}}"}`,
		map[string]string{"msg": `foo"bar`},
		map[string]string{"content-type": "Application/JSON; charset=utf-8"},
	)
	want := `{"msg": "foo\"bar"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteBody_FormURLEncoded(t *testing.T) {
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	got := SubstituteBody(
		`user={{user}}&action={{action}}`,
		map[string]string{"user": "alice bob", "action": "delete&force=true"},
		headers,
	)
	want := `user=alice+bob&action=delete%26force%3Dtrue`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteBody_UnknownContentTypeIsRaw(t *testing.T) {
	// Unknown content types get raw substitution. This is documented and
	// the user is responsible for escaping values appropriately.
	headers := map[string]string{"Content-Type": "text/xml"}
	got := SubstituteBody(
		`<value>{{v}}</value>`,
		map[string]string{"v": `</value><injected/>`},
		headers,
	)
	want := `<value></value><injected/></value>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteBody_NoArgsNoChange(t *testing.T) {
	headers := map[string]string{"Content-Type": "application/json"}
	got := SubstituteBody(`{"fixed": "value"}`, map[string]string{}, headers)
	if got != `{"fixed": "value"}` {
		t.Errorf("got %q, want unchanged", got)
	}
}
