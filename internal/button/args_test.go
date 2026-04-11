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
