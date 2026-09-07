package flowkit_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/autonoco/buttons/internal/config"
	"github.com/autonoco/buttons/internal/flowkit"
)

func TestLocalClaimRaceAndStaleness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BUTTONS_HOME", home)
	t.Setenv("BUTTONS_FLOW_CLAIM_WAIT", "0.05")

	if err := flowkit.EnsureButtons("local"); err != nil {
		t.Fatalf("EnsureButtons: %v", err)
	}
	board := "race-board"
	if err := flowkit.EnsureTasksDir(board); err != nil {
		t.Fatal(err)
	}
	tid := "task1"
	boardDir, err := config.FlowBoardDir(board)
	if err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(boardDir, "tasks", tid+".json")
	task := map[string]any{
		"id": tid, "title": "t", "status": "intake",
		"props": map[string]any{"status": "intake"},
		"timeout_seconds": 1,
	}
	raw, _ := json.MarshalIndent(task, "", "  ")
	if err := os.WriteFile(taskPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	btnDir := filepath.Join(home, "buttons", "flow-local-claim")
	code := filepath.Join(btnDir, "main.sh")

	var wg sync.WaitGroup
	results := make(chan map[string]any, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			holder := "agent-a"
			if n == 1 {
				holder = "agent-b"
			}
			cmd := exec.Command("/bin/sh", code)
			cmd.Env = append(os.Environ(),
				"BUTTONS_HOME="+home,
				"BUTTONS_ARG_BOARD="+board,
				"BUTTONS_ARG_TASK="+string(mustJSON(map[string]any{"id": tid})),
				"BUTTONS_FLOW_HOLDER="+holder,
				"BUTTONS_FLOW_CLAIM_WAIT=0.05",
			)
			out, err := cmd.CombinedOutput()
			var got map[string]any
			_ = json.Unmarshal(out, &got)
			if err != nil && got == nil {
				t.Errorf("claim %d: %v out=%s", n, err, out)
			}
			results <- got
		}(i)
	}
	wg.Wait()
	close(results)
	claimed := 0
	for r := range results {
		if r != nil && r["claimed"] == true {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("expected exactly one winner, got %d", claimed)
	}

	// Staleness: mark claim old and ensure provider-list marks actionable.
	old := time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339)
	task["props"] = map[string]any{
		"status": "intake", "flow.claimed_by": "stale-agent", "flow.claimed_at": old,
	}
	raw, _ = json.MarshalIndent(task, "", "  ")
	if err := os.WriteFile(taskPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	listCode := filepath.Join(home, "buttons", "flow-local-provider-list", "main.sh")
	cmd := exec.Command("/bin/sh", listCode)
	cmd.Env = append(os.Environ(), "BUTTONS_HOME="+home, "BUTTONS_ARG_BOARD="+board)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider-list: %v %s", err, out)
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		t.Fatalf("parse list: %v %s", err, out)
	}
	if len(listed.Items) != 1 || listed.Items[0]["actionable"] != true {
		t.Fatalf("expected stale claim to be actionable, got %#v", listed.Items)
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
