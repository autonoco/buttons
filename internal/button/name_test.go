package button

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Button", "my-button"},
		{"hello_world", "hello-world"},
		{"valid-name-123", "valid-name-123"},
		{"MyButton", "mybutton"},
		{"UPPER CASE", "upper-case"},
		{"  spaces  ", "spaces"},
		{"foo--bar", "foo-bar"},
		{"foo/bar", "foobar"},
		{"../evil", "evil"},
		{"hello.world", "helloworld"},
		{"a_b_c", "a-b-c"},
		{"---leading-trailing---", "leading-trailing"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"valid-name", false},
		{"a", false},
		{"abc-123", false},
		{"", true},
		{"../evil", true},
		{"foo/bar", true},
		{"foo\\bar", true},
		{"a..b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
			if err != nil {
				if se, ok := err.(*ServiceError); !ok || se.Code != "VALIDATION_ERROR" {
					t.Errorf("expected ServiceError with VALIDATION_ERROR, got %v", err)
				}
			}
		})
	}
}
