package flowkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Run the embedded scripts against a fake gh; no GitHub requests are made.
func runGitHubTaskScript(t *testing.T, code, patch, fail, stderr string) (map[string]any, [][]string, error) {
	t.Helper()
	dir := t.TempDir()
	fake := `#!/usr/bin/env python3
import json, os, sys
args = sys.argv[1:]
with open(os.environ["TEST_GH_CALLS"], "a") as f:
    f.write(json.dumps(args) + "\n")
if os.environ.get("TEST_GH_FAIL") in args:
    print(os.environ["TEST_GH_STDERR"], file=sys.stderr)
    sys.exit(1)
if args[:2] == ["issue", "view"]:
    print(json.dumps({"labels": [{"name": s} for s in ["flow:research", "status:intake", "bug", "status:review"]]}))
elif args[:2] == ["issue", "list"]:
    print(json.dumps([
        {"number": 1, "title": "Research", "labels": [{"name": "status:review"}], "state": "OPEN"},
        {"number": 2, "title": "Deck", "labels": [], "state": "CLOSED"}
    ]))
else:
    print("mutation succeeded")
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TEST_GH_CALLS", filepath.Join(dir, "calls"))
	t.Setenv("TEST_GH_FAIL", fail)
	t.Setenv("TEST_GH_STDERR", stderr)
	t.Setenv("BUTTONS_ARG_REPO", "example/research")
	t.Setenv("BUTTONS_ARG_BOARD", "research")
	t.Setenv("BUTTONS_ARG_ID", "1")
	t.Setenv("BUTTONS_ARG_PATCH", patch)
	cmd := exec.Command("/bin/sh", "-c", code)
	out, runErr := cmd.CombinedOutput()
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON response: %v; output=%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var call []string
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
	}
	return result, calls, runErr
}

func TestGitHubTaskFailures(t *testing.T) {
	for _, tc := range []struct {
		name, code, fail, fallback string
		calls                      int
	}{
		{"list", githubTaskListCode, "list", "issue list failed", 1},
		{"title", githubTaskUpdateCode, "--title", "title update failed", 1},
		{"body", githubTaskUpdateCode, "--body", "body update failed", 2},
		{"labels", githubTaskUpdateCode, "view", "label read failed", 3},
		{"remove status", githubTaskUpdateCode, "--remove-label", "status clear failed", 4},
		{"remove second status", githubTaskUpdateCode, "status:review", "status clear failed", 5},
		{"add status", githubTaskUpdateCode, "--add-label", "status update failed", 6},
		{"close", githubTaskRmCode, "close", "close failed", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, stderr := range []string{"  denied by GitHub  ", ""} {
				got, calls, err := runGitHubTaskScript(t, tc.code, `{"title":"New title","body":"New body","status":"done"}`, tc.fail, stderr)
				want := strings.TrimSpace(stderr)
				if want == "" {
					want = tc.fallback
				}
				if err == nil || got["ok"] != false || got["error"] != want {
					t.Fatalf("failure response=%v, err=%v; want %q", got, err, want)
				}
				if len(calls) != tc.calls {
					t.Fatalf("expected to stop after %d calls, got %v", tc.calls, calls)
				}
			}
		})
	}
}

func TestGitHubTaskUpdateReplacesAllStatusLabels(t *testing.T) {
	for _, patch := range []string{`{"status":"done"}`, `{"props":{"status":"done"}}`} {
		got, calls, err := runGitHubTaskScript(t, githubTaskUpdateCode, patch, "", "")
		if err != nil || got["ok"] != true || got["id"] != "1" {
			t.Fatalf("update response=%v, err=%v", got, err)
		}
		var wantPatch map[string]any
		if err := json.Unmarshal([]byte(patch), &wantPatch); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got["patch"], wantPatch) {
			t.Fatalf("patch response=%v, want %v", got["patch"], wantPatch)
		}
		want := [][]string{
			{"issue", "view", "1", "--repo", "example/research", "--json", "labels"},
			{"issue", "edit", "1", "--repo", "example/research", "--remove-label", "status:intake"},
			{"issue", "edit", "1", "--repo", "example/research", "--remove-label", "status:review"},
			{"issue", "edit", "1", "--repo", "example/research", "--add-label", "status:done"},
		}
		if !reflect.DeepEqual(calls, want) {
			t.Fatalf("status update calls=%v, want %v", calls, want)
		}
	}
}

func TestGitHubTaskListPreservesFiltering(t *testing.T) {
	for _, filter := range []string{"", "status=review"} {
		t.Setenv("BUTTONS_ARG_FILTER", filter)
		got, _, err := runGitHubTaskScript(t, githubTaskListCode, "{}", "", "")
		if err != nil {
			t.Fatal(err)
		}
		wantCount := 2
		if filter != "" {
			wantCount = 1
		}
		items := got["items"].([]any)
		if len(items) != wantCount || got["count"] != float64(wantCount) {
			t.Fatalf("list response=%v", got)
		}
		item := items[0].(map[string]any)
		if item["id"] != "1" || item["status"] != "review" || item["title"] != "Research" {
			t.Fatalf("unexpected item=%v", item)
		}
	}
}

func TestGitHubTaskMutationsSucceed(t *testing.T) {
	for _, code := range []string{githubTaskUpdateCode, githubTaskRmCode} {
		got, _, err := runGitHubTaskScript(t, code, `{"title":"Title","body":"Body","status":"done"}`, "", "")
		if err != nil || got["ok"] != true || got["id"] != "1" {
			t.Fatalf("mutation response=%v, err=%v", got, err)
		}
	}
}
