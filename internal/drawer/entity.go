package drawer

import "time"

type Drawer struct {
	SchemaVersion int       `json:"schema_version"`
	Name          string    `json:"name"`
	OnFailure     string    `json:"on_failure"`
	Steps         []Step    `json:"steps"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Step struct {
	Button   string            `json:"button"`
	Bindings map[string]string `json:"bindings,omitempty"`
}
