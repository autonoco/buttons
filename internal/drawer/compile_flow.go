package drawer

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CompileOptions controls which provider button set CompileFlow wires.
type CompileOptions struct {
	// Provider is "local" (default) or "github". Empty uses d.Flow.Provider, then "local".
	Provider string
}

// PrepareForExecute returns a drawer the existing executor can run.
// Action drawers pass through unchanged. Flow drawers compile in memory.
func PrepareForExecute(d *Drawer) (*Drawer, error) {
	return PrepareForExecuteOpts(d, CompileOptions{})
}

// PrepareForExecuteOpts is PrepareForExecute with explicit compile options.
func PrepareForExecuteOpts(d *Drawer, opts CompileOptions) (*Drawer, error) {
	if d == nil {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "drawer is nil"}
	}
	if d.DrawerKind != DrawerKindFlow {
		return d, nil
	}
	return CompileFlow(d, opts)
}

// CompileFlow turns a flow-kind drawer into an in-memory action drawer whose
// Steps are the provider-list → for_each(claim→perform→validate→apply) →
// ensure-trigger pipeline. The result must never be saved: MarshalJSON would
// strip steps from a flow entity, and this design never persists a compile.
func CompileFlow(d *Drawer, opts CompileOptions) (*Drawer, error) {
	if d == nil || d.DrawerKind != DrawerKindFlow {
		return nil, &ServiceError{Code: "DRAWER_KIND_MISMATCH", Message: "CompileFlow requires a flow drawer"}
	}
	if d.Flow == nil {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: "flow drawer requires a flow definition"}
	}

	report := ValidationReport{}
	validateFlowDefinition(d, &report)
	if len(report.Errors) > 0 {
		msg := report.Errors[0].Message
		if report.Errors[0].Remediation != "" {
			msg = msg + "; " + report.Errors[0].Remediation
		}
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("flow %q: %s", d.Name, msg)}
	}

	provider := strings.TrimSpace(opts.Provider)
	if provider == "" {
		provider = strings.TrimSpace(d.Flow.Provider)
	}
	if provider == "" {
		provider = "local"
	}
	if provider != "local" && provider != "github" {
		return nil, &ServiceError{Code: "VALIDATION_ERROR", Message: fmt.Sprintf("unknown flow provider %q (want local or github)", provider)}
	}

	prefix := "flow-" + provider
	parallelism := 1
	for _, stage := range d.Flow.Stages {
		if stage.Concurrency > parallelism {
			parallelism = stage.Concurrency
		}
	}
	if d.Flow.Limits != nil && d.Flow.Limits.MaxActiveTasks > 0 && parallelism > d.Flow.Limits.MaxActiveTasks {
		parallelism = d.Flow.Limits.MaxActiveTasks
	}

	systemPrompt := composePerformPrompt(d.Flow)
	transitionsJSON := stageTransitionsJSON(d.Flow.Stages)
	gatesJSON := stageGatesJSON(d.Flow.Stages)
	evidenceJSON := stageEvidenceJSON(d.Flow.Stages)

	compiled := &Drawer{
		SchemaVersion: SchemaVersion,
		Name:          d.Name,
		DrawerKind:    DrawerKindAction,
		Description:   d.Description,
		Version:       d.Version,
		Inputs:        d.Inputs,
		Triggers:      d.Triggers,
		CreatedAt:     d.CreatedAt,
		UpdatedAt:     d.UpdatedAt,
		Steps: []Step{
			{
				ID:     "provider-list",
				Kind:   "button",
				Button: prefix + "-provider-list",
				Args: map[string]any{
					"board": d.Name,
				},
			},
			{
				ID:            "items",
				Kind:          "for_each",
				Over:          "provider-list.output.items",
				As:            "item",
				OnItemFailure: "continue",
				Parallelism:   parallelism,
				Steps: []Step{
					{
						ID:   "actionable",
						Kind: "switch",
						Cases: []Case{{
							ID:   "yes",
							When: "has(item.actionable) && item.actionable",
							Steps: []Step{
								{
									ID:     "claim",
									Kind:   "button",
									Button: prefix + "-claim",
									Args: map[string]any{
										"board":   d.Name,
										"task":    "${item}",
										"task_id": "${item.id}",
									},
								},
								{
									ID:   "claimed",
									Kind: "switch",
									Cases: []Case{{
										ID:   "won",
										When: "has(claim.output.claimed) && claim.output.claimed",
										Steps: []Step{
											{
												ID:     "perform",
												Kind:   "button",
												Button: prefix + "-perform",
												Args: map[string]any{
													"board":         d.Name,
													"task":          "${item}",
													"system_prompt": systemPrompt,
													"transitions":   transitionsJSON,
												},
											},
											{
												ID:     "validate",
												Kind:   "button",
												Button: prefix + "-validate",
												Args: map[string]any{
													"board":       d.Name,
													"task":        "${item}",
													"verdict":     "${perform.output}",
													"transitions": transitionsJSON,
													"evidence":    evidenceJSON,
												},
											},
											{
												ID:     "apply",
												Kind:   "button",
												Button: prefix + "-apply",
												Args: map[string]any{
													"board":    d.Name,
													"task":     "${item}",
													"verdict":  "${validate.output}",
													"gates":    gatesJSON,
													"provider": provider,
												},
											},
										},
									}},
								},
							},
						}},
					},
				},
			},
			{
				ID:     "ensure-trigger",
				Kind:   "button",
				Button: prefix + "-ensure-trigger",
				Args: map[string]any{
					"board":    d.Name,
					"provider": provider,
				},
			},
		},
	}
	return compiled, nil
}

func composePerformPrompt(flow *FlowDefinition) string {
	var parts []string
	if flow.Manager.SystemPrompt != "" {
		parts = append(parts, flow.Manager.SystemPrompt)
	}
	for _, stage := range flow.Stages {
		if stage.SystemPrompt != "" {
			parts = append(parts, fmt.Sprintf("[%s] %s", stage.ID, stage.SystemPrompt))
		}
		if stage.OnFeedback != "" {
			parts = append(parts, fmt.Sprintf("[%s on_feedback] %s", stage.ID, stage.OnFeedback))
		}
		if stage.Review != "" {
			parts = append(parts, fmt.Sprintf("[%s review] %s", stage.ID, stage.Review))
		}
		if stage.Role != "" && flow.Roles != nil {
			if role, ok := flow.Roles[stage.Role]; ok && role.SystemPrompt != "" {
				parts = append(parts, fmt.Sprintf("[%s role:%s] %s", stage.ID, stage.Role, role.SystemPrompt))
			}
		}
	}
	if len(parts) == 0 {
		return "You are a Buttons Flow agent. Respond with a JSON verdict: {\"verdict\":\"advance|retry|hold|delegate|escalate\",\"to_stage\":\"...\",\"summary\":\"...\"}."
	}
	parts = append(parts, "Respond with JSON: {\"verdict\":\"advance|retry|hold|delegate|escalate\",\"to_stage\":\"...\",\"summary\":\"...\"}.")
	return strings.Join(parts, "\n\n")
}

func stageTransitionsJSON(stages []FlowStage) string {
	m := make(map[string][]string, len(stages))
	for _, s := range stages {
		m[s.ID] = s.Transitions
	}
	return mustJSONString(m)
}

func stageGatesJSON(stages []FlowStage) string {
	m := make(map[string]any, len(stages))
	for _, s := range stages {
		if s.Gate != nil && s.Gate.RequiresHumanApproval {
			m[s.ID] = map[string]any{
				"requires_human_approval": true,
				"approvers":               s.Gate.Approvers,
			}
		}
	}
	return mustJSONString(m)
}

func stageEvidenceJSON(stages []FlowStage) string {
	m := make(map[string]any, len(stages))
	for _, s := range stages {
		if s.Evidence != nil && len(s.Evidence.Required) > 0 {
			m[s.ID] = s.Evidence.Required
		}
	}
	return mustJSONString(m)
}

func mustJSONString(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
