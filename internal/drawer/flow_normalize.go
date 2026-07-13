package drawer

import (
	"encoding/json"
	"fmt"
)

// NormalizedFlowDefinition is the immutable execution definition emitted next
// to a published flow Drawer artifact. Authoring timestamps and action-only
// fields are excluded so consumers can hash and register the actual policy.
type NormalizedFlowDefinition struct {
	SchemaVersion int             `json:"schema_version"`
	Name          string          `json:"name"`
	DrawerKind    string          `json:"drawer_kind"`
	Description   string          `json:"description,omitempty"`
	Version       string          `json:"version"`
	Flow          *FlowDefinition `json:"flow"`
}

func NormalizeFlowDefinition(d *Drawer) ([]byte, error) {
	if d == nil || d.DrawerKind != DrawerKindFlow || d.Flow == nil {
		return nil, fmt.Errorf("normalized flow definition requires a flow drawer")
	}
	report := Validate(d, nil)
	if !report.OK {
		return nil, fmt.Errorf("flow drawer is invalid: %s", report.Errors[0].Message)
	}
	return json.Marshal(NormalizedFlowDefinition{
		SchemaVersion: d.SchemaVersion,
		Name:          d.Name,
		DrawerKind:    d.DrawerKind,
		Description:   d.Description,
		Version:       d.Version,
		Flow:          d.Flow,
	})
}
