// Package flowkit installs the shared Buttons Flow pipeline buttons and
// helpers used by CompileFlow (provider-list, claim, perform, validate,
// apply, ensure-trigger, and task CRUD).
package flowkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
)

const claimWaitSeconds = 1

// EnsureButtons installs (or refreshes) the pipeline buttons for provider
// ("local" or "github"). Idempotent.
func EnsureButtons(provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		provider = "local"
	}
	switch provider {
	case "local":
		return ensureLocalButtons()
	case "github":
		return ensureGitHubButtons()
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
}

func ensureLocalButtons() error {
	specs := []buttonSpec{
		{name: "flow-local-provider-list", args: boardArg(), code: localProviderListCode, timeout: 30},
		{name: "flow-local-claim", args: append(boardTaskArgs(), arg("task_id", "string", false)), code: localClaimCode, timeout: 30, queue: &button.QueueConfig{Name: "flow-local-claim", Concurrency: 1, Key: "${inputs.task_id}"}},
		{name: "flow-local-perform", args: append(boardTaskArgs(), arg("system_prompt", "string", false), arg("transitions", "string", false)), code: localPerformCode, timeout: 120},
		{name: "flow-local-validate", args: append(boardTaskArgs(), arg("verdict", "string", true), arg("transitions", "string", false), arg("evidence", "string", false)), code: localValidateCode, timeout: 30},
		{name: "flow-local-apply", args: append(boardTaskArgs(), arg("verdict", "string", true), arg("gates", "string", false), arg("provider", "string", false)), code: localApplyCode, timeout: 30},
		{name: "flow-local-ensure-trigger", args: []button.ArgDef{arg("board", "string", true), arg("provider", "string", false)}, code: localEnsureTriggerCode, timeout: 30},
		{name: "flow-local-task-add", args: []button.ArgDef{arg("board", "string", true), arg("title", "string", true), arg("body", "string", false), arg("status", "string", false)}, code: localTaskAddCode, timeout: 30},
		{name: "flow-local-task-list", args: []button.ArgDef{arg("board", "string", true), arg("filter", "string", false)}, code: localTaskListCode, timeout: 30},
		{name: "flow-local-task-read", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true)}, code: localTaskReadCode, timeout: 30},
		{name: "flow-local-task-update", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("patch", "string", true)}, code: localTaskUpdateCode, timeout: 30},
		{name: "flow-local-task-rm", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true)}, code: localTaskRmCode, timeout: 30},
		{name: "flow-local-task-claim", args: append(boardTaskArgs(), arg("task_id", "string", false)), code: localClaimCode, timeout: 30, queue: &button.QueueConfig{Name: "flow-local-claim", Concurrency: 1, Key: "${inputs.task_id}"}},
		{name: "flow-local-task-comment", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("body", "string", true)}, code: localTaskCommentCode, timeout: 30},
		{name: "flow-local-approve", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true)}, code: localApproveCode, timeout: 30},
		{name: "flow-local-reject", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("reason", "string", false)}, code: localRejectCode, timeout: 30},
	}
	return installSpecs(specs)
}

func ensureGitHubButtons() error {
	specs := []buttonSpec{
		{name: "flow-github-provider-list", args: append(boardArg(), arg("repo", "string", false)), code: githubProviderListCode, timeout: 60},
		{name: "flow-github-claim", args: append(boardTaskArgs(), arg("repo", "string", false), arg("task_id", "string", false)), code: githubClaimCode, timeout: 60, queue: &button.QueueConfig{Name: "flow-github-claim", Concurrency: 1, Key: "${inputs.task_id}"}},
		{name: "flow-github-perform", args: append(boardTaskArgs(), arg("system_prompt", "string", false), arg("transitions", "string", false)), code: localPerformCode, timeout: 120},
		{name: "flow-github-validate", args: append(boardTaskArgs(), arg("verdict", "string", true), arg("transitions", "string", false), arg("evidence", "string", false)), code: localValidateCode, timeout: 30},
		{name: "flow-github-apply", args: append(boardTaskArgs(), arg("verdict", "string", true), arg("gates", "string", false), arg("provider", "string", false), arg("repo", "string", false)), code: githubApplyCode, timeout: 60},
		{name: "flow-github-ensure-trigger", args: []button.ArgDef{arg("board", "string", true), arg("provider", "string", false)}, code: localEnsureTriggerCode, timeout: 30},
		{name: "flow-github-task-add", args: []button.ArgDef{arg("board", "string", true), arg("title", "string", true), arg("body", "string", false), arg("status", "string", false), arg("repo", "string", false)}, code: githubTaskAddCode, timeout: 60},
		{name: "flow-github-task-list", args: []button.ArgDef{arg("board", "string", true), arg("filter", "string", false), arg("repo", "string", false)}, code: githubTaskListCode, timeout: 60},
		{name: "flow-github-task-read", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("repo", "string", false)}, code: githubTaskReadCode, timeout: 60},
		{name: "flow-github-task-update", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("patch", "string", true), arg("repo", "string", false)}, code: githubTaskUpdateCode, timeout: 60},
		{name: "flow-github-task-rm", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("repo", "string", false)}, code: githubTaskRmCode, timeout: 60},
		{name: "flow-github-task-claim", args: append(boardTaskArgs(), arg("repo", "string", false)), code: githubClaimCode, timeout: 60},
		{name: "flow-github-task-comment", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("body", "string", true), arg("repo", "string", false)}, code: githubTaskCommentCode, timeout: 60},
		{name: "flow-github-approve", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("repo", "string", false)}, code: githubApproveCode, timeout: 60},
		{name: "flow-github-reject", args: []button.ArgDef{arg("board", "string", true), arg("id", "string", true), arg("reason", "string", false), arg("repo", "string", false)}, code: githubRejectCode, timeout: 60},
	}
	return installSpecs(specs)
}

type buttonSpec struct {
	name    string
	args    []button.ArgDef
	code    string
	timeout int
	queue   *button.QueueConfig
}

func installSpecs(specs []buttonSpec) error {
	svc := button.NewService()
	for _, sp := range specs {
		if err := installOne(svc, sp); err != nil {
			return err
		}
	}
	return nil
}

func installOne(svc *button.Service, sp buttonSpec) error {
	exists, err := buttonExists(sp.name)
	if err != nil {
		return err
	}
	if exists {
		_ = svc.Remove(sp.name)
	}
	_, err = svc.Create(button.CreateOpts{
		Name:           sp.name,
		Runtime:        "shell",
		Code:           sp.code,
		TimeoutSeconds: sp.timeout,
		Args:           sp.args,
		Description:    "Buttons Flow pipeline button (" + sp.name + ")",
	})
	if err != nil {
		return err
	}
	if sp.queue != nil {
		if err := patchQueue(sp.name, sp.queue); err != nil {
			return err
		}
	}
	return nil
}

func buttonExists(name string) (bool, error) {
	dir, err := config.ButtonDir(name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(dir, "button.json"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func patchQueue(name string, q *button.QueueConfig) error {
	dir, err := config.ButtonDir(name)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "button.json")
	data, err := os.ReadFile(path) // #nosec G304
	if err != nil {
		return err
	}
	var b button.Button
	if err := json.Unmarshal(data, &b); err != nil {
		return err
	}
	b.Queue = q
	b.UpdatedAt = time.Now().UTC()
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o600)
}

func boardArg() []button.ArgDef { return []button.ArgDef{arg("board", "string", true)} }

func boardTaskArgs() []button.ArgDef {
	return []button.ArgDef{arg("board", "string", true), arg("task", "string", true)}
}

func arg(name, typ string, required bool) button.ArgDef {
	return button.ArgDef{Name: name, Type: typ, Required: required}
}

// TasksDir returns ~/.buttons/flows/<board>/tasks.
func TasksDir(board string) (string, error) {
	boardDir, err := config.FlowBoardDir(board)
	if err != nil {
		return "", err
	}
	return filepath.Join(boardDir, "tasks"), nil
}

// EnsureTasksDir creates the local task store for a board.
func EnsureTasksDir(board string) error {
	dir, err := TasksDir(board)
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o700)
}
