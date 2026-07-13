package drawer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/autonoco/buttons/internal/button"
)

func validFlowDrawerJSON() string {
	return `{
		"schema_version": 2,
		"name": "software-delivery",
		"drawer_kind": "flow",
		"description": "Move a ticket through delivery.",
		"version": "1",
		"flow": {
			"initial_stage": "intake",
			"manager": {
				"agent": "activation.manager",
				"system_prompt": "Supervise this flow.",
				"heartbeat_seconds": 300
			},
			"limits": {
				"max_active_tasks": 50,
				"max_runtime_seconds": 86400,
				"max_attempts_per_stage": 3
			},
			"stages": [
				{
					"id": "intake",
					"title": "Intake",
					"system_prompt": "Clarify the request.",
					"manager": {
						"agent": "agent:tenant-a:intake-manager",
						"system_prompt": "Review intake quality.",
						"heartbeat_seconds": 180
					},
					"worker": {"agent": "activation.worker"},
					"session_policy": {
						"manager": "continue_stage",
						"worker": "new_attempt"
					},
					"triggers": [
						{"kind": "heartbeat", "every_seconds": 180},
						{"kind": "event", "event_type": "comment.added"}
					],
					"transitions": ["research", "blocked"],
					"completion": {"requires_summary": true},
					"retry": {
						"limit": 2,
						"backoff": "exponential",
						"initial_seconds": 30
					},
					"timeout_seconds": 1800,
					"concurrency": 5
				},
				{
					"id": "research",
					"title": "Research",
					"transitions": ["done"]
				},
				{
					"id": "blocked",
					"title": "Blocked",
					"transitions": ["intake"]
				},
				{
					"id": "done",
					"title": "Done"
				}
			]
		},
		"created_at": "2026-07-12T00:00:00Z",
		"updated_at": "2026-07-12T00:00:00Z"
	}`
}

func decodeDrawerJSON(t *testing.T, raw string) *Drawer {
	t.Helper()
	var got Drawer
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode drawer: %v", err)
	}
	return &got
}

func mutateFlowDrawerJSON(t *testing.T, mutate func(map[string]any)) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(validFlowDrawerJSON()), &value); err != nil {
		t.Fatal(err)
	}
	mutate(value)
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func requireFlowValidationError(t *testing.T, raw, contains string) {
	t.Helper()
	report := Validate(decodeDrawerJSON(t, raw), button.NewService())
	if report.OK {
		t.Fatalf("Validate() unexpectedly succeeded; want error containing %q", contains)
	}
	for _, issue := range report.Errors {
		if strings.Contains(issue.Message, contains) {
			return
		}
	}
	t.Fatalf("errors = %#v; want one containing %q", report.Errors, contains)
}

func TestValidateFlowDrawerAcceptsValidDefinition(t *testing.T) {
	report := Validate(decodeDrawerJSON(t, validFlowDrawerJSON()), button.NewService())
	if !report.OK {
		t.Fatalf("valid flow rejected: %#v", report.Errors)
	}
}

func TestValidateFlowDrawerStructuralInvariants(t *testing.T) {
	t.Run("non-empty stages", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			value["flow"].(map[string]any)["stages"] = []any{}
		})
		requireFlowValidationError(t, raw, "at least one stage")
	})

	t.Run("unique stage ids", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[1].(map[string]any)["id"] = "intake"
		})
		requireFlowValidationError(t, raw, "duplicate stage id")
	})

	t.Run("initial stage exists", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			value["flow"].(map[string]any)["initial_stage"] = "missing"
		})
		requireFlowValidationError(t, raw, "initial stage")
	})

	t.Run("transition target exists", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[0].(map[string]any)["transitions"] = []any{"missing"}
		})
		requireFlowValidationError(t, raw, "unknown transition target")
	})

	t.Run("manager reference syntax", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			value["flow"].(map[string]any)["manager"].(map[string]any)["agent"] = "../../manager"
		})
		requireFlowValidationError(t, raw, "invalid manager agent reference")
	})

	t.Run("worker reference syntax", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[0].(map[string]any)["worker"].(map[string]any)["agent"] = "not a ref"
		})
		requireFlowValidationError(t, raw, "invalid worker agent reference")
	})
}

func TestValidateFlowDrawerPolicyInvariants(t *testing.T) {
	t.Run("session policy", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[0].(map[string]any)["session_policy"].(map[string]any)["manager"] = "forever"
		})
		requireFlowValidationError(t, raw, "invalid manager session policy")
	})

	t.Run("trigger kind", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[0].(map[string]any)["triggers"] = []any{map[string]any{"kind": "magic"}}
		})
		requireFlowValidationError(t, raw, "invalid stage trigger kind")
	})

	t.Run("heartbeat duration", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[0].(map[string]any)["triggers"] = []any{map[string]any{"kind": "heartbeat", "every_seconds": 0}}
		})
		requireFlowValidationError(t, raw, "positive every_seconds")
	})

	t.Run("timeout", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[0].(map[string]any)["timeout_seconds"] = -1
		})
		requireFlowValidationError(t, raw, "timeout_seconds must be positive")
	})

	t.Run("concurrency", func(t *testing.T) {
		raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
			stages := value["flow"].(map[string]any)["stages"].([]any)
			stages[0].(map[string]any)["concurrency"] = -1
		})
		requireFlowValidationError(t, raw, "concurrency must be positive")
	})
}

func TestValidateFlowDrawerRejectsMixedActionFields(t *testing.T) {
	raw := mutateFlowDrawerJSON(t, func(value map[string]any) {
		value["steps"] = []any{map[string]any{"id": "build", "button": "build"}}
	})
	requireFlowValidationError(t, raw, "flow drawer cannot define steps")
}

func TestValidateLegacyV1ActionDrawerRemainsValid(t *testing.T) {
	d := decodeDrawerJSON(t, `{
		"schema_version": 1,
		"name": "legacy",
		"steps": [],
		"created_at": "2026-07-12T00:00:00Z",
		"updated_at": "2026-07-12T00:00:00Z"
	}`)
	report := Validate(d, button.NewService())
	if !report.OK {
		t.Fatalf("legacy v1 action drawer rejected: %#v", report.Errors)
	}
}

func TestDrawerSchemaDescribesLegacyActionAndFlowVariants(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(SchemaJSON, &schema); err != nil {
		t.Fatalf("embedded schema is invalid JSON: %v", err)
	}
	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 3 {
		t.Fatalf("schema oneOf = %#v; want legacy, action, and flow variants", schema["oneOf"])
	}
	encoded := string(SchemaJSON)
	for _, want := range []string{`"drawer_kind"`, `"action"`, `"flow"`, `"FlowDefinition"`, `"FlowStage"`} {
		if !strings.Contains(encoded, want) {
			t.Errorf("schema missing %s", want)
		}
	}
}
