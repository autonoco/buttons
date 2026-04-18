package button

import "testing"

func TestParseArgDef(t *testing.T) {
	tests := []struct {
		raw      string
		wantName string
		wantType string
		wantReq  bool
		wantErr  bool
	}{
		{"url:string:required", "url", "string", true, false},
		{"count:int:optional", "count", "int", false, false},
		{"verbose:bool:required", "verbose", "bool", true, false},
		{"url:string", "", "", false, true},           // missing segment
		{"url::required", "", "", false, true},         // empty type
		{":string:required", "", "", false, true},      // empty name
		{"url:float:required", "", "", false, true},    // invalid type
		{"url:string:maybe", "", "", false, true},      // invalid required flag
		{"a:b:c:d", "", "", false, true},               // too many segments
		{"url:STRING:REQUIRED", "url", "string", true, false}, // case insensitive
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := ParseArgDef(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseArgDef(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if err != nil {
				if se, ok := err.(*ServiceError); !ok || se.Code != "VALIDATION_ERROR" {
					t.Errorf("expected VALIDATION_ERROR, got %v", err)
				}
				return
			}
			if got.Name != tt.wantName || got.Type != tt.wantType || got.Required != tt.wantReq {
				t.Errorf("ParseArgDef(%q) = %+v, want {%s %s %v}", tt.raw, got, tt.wantName, tt.wantType, tt.wantReq)
			}
		})
	}
}

func TestParseArgDefs_DuplicateName(t *testing.T) {
	_, err := ParseArgDefs([]string{"url:string:required", "url:int:optional"})
	if err == nil {
		t.Fatal("expected error for duplicate arg names")
	}
	se, ok := err.(*ServiceError)
	if !ok || se.Code != "VALIDATION_ERROR" {
		t.Errorf("expected VALIDATION_ERROR, got %v", err)
	}
}

func TestParseArgDefs_Valid(t *testing.T) {
	defs, err := ParseArgDefs([]string{"url:string:required", "count:int:optional"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}
}

func TestParseArgDef_EnumValid(t *testing.T) {
	def, err := ParseArgDef("env:enum:required:staging|prod|canary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Type != "enum" {
		t.Errorf("type = %q, want enum", def.Type)
	}
	if !def.Required {
		t.Error("required should be true")
	}
	if len(def.Values) != 3 {
		t.Fatalf("values = %v, want 3 entries", def.Values)
	}
	for i, want := range []string{"staging", "prod", "canary"} {
		if def.Values[i] != want {
			t.Errorf("values[%d] = %q, want %q", i, def.Values[i], want)
		}
	}
}

func TestParseArgDef_EnumRejectsBadShapes(t *testing.T) {
	cases := []struct {
		raw    string
		reason string
	}{
		{"env:enum:required", "missing values segment"},
		{"env:enum:required:only-one", "single-value enum is useless"},
		{"env:enum:required:a||b", "empty entry in the list"},
		{"env:enum:required:a|a", "duplicate enum value"},
		{"name:string:required:a|b", "non-enum types don't accept a 4th segment"},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			if _, err := ParseArgDef(tc.raw); err == nil {
				t.Errorf("ParseArgDef(%q) should fail: %s", tc.raw, tc.reason)
			}
		})
	}
}

func TestParsePressArgs_EnumMembership(t *testing.T) {
	defs := []ArgDef{{
		Name:     "env",
		Type:     "enum",
		Required: true,
		Values:   []string{"staging", "prod", "canary"},
	}}

	// Valid value — passes.
	out, err := ParsePressArgs([]string{"env=staging"}, defs)
	if err != nil {
		t.Fatalf("staging should be accepted: %v", err)
	}
	if out["env"] != "staging" {
		t.Errorf("value = %q, want staging", out["env"])
	}

	// Invalid value — rejected with a helpful message listing the set.
	if _, err := ParsePressArgs([]string{"env=dev"}, defs); err == nil {
		t.Error("dev should be rejected (not in enum)")
	}
}
