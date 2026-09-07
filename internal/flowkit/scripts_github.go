package flowkit

// GitHub-backed pipeline buttons. Prefer `gh` CLI; fall back messages are JSON.

const githubProviderListCode = `#!/bin/sh
set -e
REPO="${BUTTONS_ARG_REPO:-}"
BOARD="${BUTTONS_ARG_BOARD}"
LABEL="flow:${BOARD}"
if [ -z "$REPO" ]; then REPO="${BUTTONS_FLOW_REPO:-}"; fi
if [ -z "$REPO" ]; then
  echo '{"items":[],"error":"repo required (BUTTONS_ARG_REPO or BUTTONS_FLOW_REPO)"}'
  exit 0
fi
python3 - <<'PY'
import json, os, subprocess, time
from datetime import datetime, timezone

repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
board = os.environ["BUTTONS_ARG_BOARD"]
label = f"flow:{board}"
cmd = ["gh", "issue", "list", "--repo", repo, "--label", label, "--state", "open", "--json", "number,title,body,labels,assignees,updatedAt"]
proc = subprocess.run(cmd, capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"items": [], "error": proc.stderr.strip() or "gh failed"}))
    raise SystemExit(0)
issues = json.loads(proc.stdout or "[]")
now = time.time()
items = []
for issue in issues:
    labels = [l.get("name","") for l in (issue.get("labels") or [])]
    status = "intake"
    for lab in labels:
        if lab.startswith("status:"):
            status = lab.split(":",1)[1]
            break
    assignees = issue.get("assignees") or []
    claimed_by = assignees[0].get("login") if assignees else ""
    claimed_at = issue.get("updatedAt") or ""
    timeout = 3600
    stale = False
    if claimed_by and claimed_at:
        try:
            ts = datetime.fromisoformat(claimed_at.replace("Z","+00:00")).timestamp()
            stale = (now - ts) > timeout
        except Exception:
            stale = True
    actionable = status != "done" and (not claimed_by or stale) and ("flow.pending_approval" not in labels)
    items.append({
        "id": str(issue["number"]),
        "title": issue.get("title") or "",
        "body": issue.get("body") or "",
        "status": status,
        "props": {"status": status, "flow.claimed_by": claimed_by, "flow.claimed_at": claimed_at},
        "tags": labels,
        "claimed_by": claimed_by,
        "claimed_at": claimed_at,
        "timeout_seconds": timeout,
        "actionable": actionable,
        "repo": repo,
    })
print(json.dumps({"items": items}))
PY
`

const githubClaimCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys, time

repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
raw = os.environ.get("BUTTONS_ARG_TASK","{}")
task = json.loads(raw) if raw.strip().startswith("{") else {"id": raw}
tid = str(task.get("id") or "")
if not tid:
    print(json.dumps({"claimed": False, "reason": "missing_id"}))
    raise SystemExit(0)
holder = os.environ.get("BUTTONS_FLOW_HOLDER") or os.environ.get("GITHUB_ACTOR") or "buttons-agent"
view = subprocess.run(["gh","issue","view",tid,"--repo",repo,"--json","assignees,labels"], capture_output=True, text=True)
if view.returncode != 0:
    print(json.dumps({"claimed": False, "reason": "view_failed", "error": view.stderr.strip()}))
    raise SystemExit(0)
data = json.loads(view.stdout)
assignees = [a.get("login") for a in (data.get("assignees") or [])]
others = [a for a in assignees if a != holder]
if others:
    print(json.dumps({"claimed": False, "reason": "already_claimed", "claimed_by": others[0]}))
    raise SystemExit(0)
edit = subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--add-assignee",holder,"--add-label","Agent Claimed"], capture_output=True, text=True)
if edit.returncode != 0:
    print(json.dumps({"claimed": False, "reason": "edit_failed", "error": edit.stderr.strip()}))
    raise SystemExit(0)
time.sleep(float(os.environ.get("BUTTONS_FLOW_CLAIM_WAIT","1")))
view2 = subprocess.run(["gh","issue","view",tid,"--repo",repo,"--json","assignees"], capture_output=True, text=True)
data2 = json.loads(view2.stdout or "{}")
assignees2 = [a.get("login") for a in (data2.get("assignees") or [])]
if assignees2 != [holder]:
    print(json.dumps({"claimed": False, "reason": "lost_race"}))
    raise SystemExit(0)
print(json.dumps({"claimed": True, "claimed_by": holder, "id": tid}))
PY
`

const githubApplyCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys

repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
raw_task = os.environ.get("BUTTONS_ARG_TASK","{}")
task = json.loads(raw_task) if raw_task.strip().startswith("{") else {"id": raw_task}
tid = str(task.get("id") or "")
verdict = json.loads(os.environ.get("BUTTONS_ARG_VERDICT") or "{}")
gates = json.loads(os.environ.get("BUTTONS_ARG_GATES") or "{}")
if not tid:
    print(json.dumps({"applied": False, "reason": "missing_id"}))
    raise SystemExit(0)
if not verdict.get("ok", True):
    proc = subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",f"needs_attention: {verdict.get('reason')}"], capture_output=True, text=True)
    if proc.returncode != 0:
        print(json.dumps({"ok": False, "error": proc.stderr.strip() or "comment failed"})); sys.exit(1)
    print(json.dumps({"applied": False, "reason": verdict.get("reason")}))
    raise SystemExit(0)
v = verdict.get("verdict")
from_stage = verdict.get("from_stage") or (task.get("props") or {}).get("status")
to_stage = verdict.get("to_stage") or from_stage
gate = gates.get(from_stage) or {}
if v == "advance" and gate.get("requires_human_approval"):
    proc = subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--add-label","flow.pending_approval"], capture_output=True, text=True)
    if proc.returncode != 0:
        print(json.dumps({"ok": False, "error": proc.stderr.strip() or "label failed"})); sys.exit(1)
    print(json.dumps({"applied": True, "pending_approval": to_stage, "id": tid}))
    raise SystemExit(0)
if v == "advance":
    if from_stage:
        subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-label",f"status:{from_stage}"], capture_output=True, text=True)
    proc = subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--add-label",f"status:{to_stage}"], capture_output=True, text=True)
    if proc.returncode != 0:
        print(json.dumps({"ok": False, "error": proc.stderr.strip() or "status update failed"})); sys.exit(1)
    if to_stage == "done":
        subprocess.run(["gh","issue","close",tid,"--repo",repo], capture_output=True, text=True)
if verdict.get("summary"):
    subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",verdict["summary"]], capture_output=True, text=True)
holder = os.environ.get("BUTTONS_FLOW_HOLDER") or os.environ.get("GITHUB_ACTOR") or "buttons-agent"
proc = subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-assignee",holder], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip() or "release claim failed"})); sys.exit(1)
print(json.dumps({"applied": True, "status": to_stage if v=="advance" else from_stage, "id": tid, "verdict": v}))
PY
`

const githubTaskAddCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
board = os.environ["BUTTONS_ARG_BOARD"]
title = os.environ.get("BUTTONS_ARG_TITLE") or ""
body = os.environ.get("BUTTONS_ARG_BODY") or ""
status = os.environ.get("BUTTONS_ARG_STATUS") or "intake"
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); raise SystemExit(1)
cmd = ["gh","issue","create","--repo",repo,"--title",title,"--body",body or title,
       "--label",f"flow:{board}","--label",f"status:{status}"]
proc = subprocess.run(cmd, capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip()})); raise SystemExit(1)
url = proc.stdout.strip()
num = url.rstrip("/").split("/")[-1]
print(json.dumps({"id": num, "title": title, "body": body, "status": status, "url": url, "props": {"status": status}}))
PY
`

const githubTaskListCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
board = os.environ["BUTTONS_ARG_BOARD"]
filt = os.environ.get("BUTTONS_ARG_FILTER") or ""
want_status = None
for part in filt.split(","):
    if part.strip().startswith("status="):
        want_status = part.split("=",1)[1].strip()
label = f"flow:{board}"
proc = subprocess.run(["gh","issue","list","--repo",repo,"--label",label,"--state","all","--json","number,title,body,labels,state"], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip() or "issue list failed"})); sys.exit(1)
issues = json.loads(proc.stdout or "[]")
items = []
for issue in issues:
    labels = [l.get("name","") for l in (issue.get("labels") or [])]
    status = "intake"
    for lab in labels:
        if lab.startswith("status:"):
            status = lab.split(":",1)[1]
    if want_status and status != want_status:
        continue
    items.append({"id": str(issue["number"]), "title": issue.get("title"), "status": status, "props": {"status": status}, "tags": labels})
print(json.dumps({"items": items, "count": len(items)}))
PY
`

const githubTaskReadCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
tid = os.environ["BUTTONS_ARG_ID"]
proc = subprocess.run(["gh","issue","view",tid,"--repo",repo,"--json","number,title,body,labels,assignees,state"], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip()})); raise SystemExit(1)
issue = json.loads(proc.stdout)
labels = [l.get("name","") for l in (issue.get("labels") or [])]
status = "intake"
for lab in labels:
    if lab.startswith("status:"):
        status = lab.split(":",1)[1]
print(json.dumps({"id": str(issue["number"]), "title": issue.get("title"), "body": issue.get("body"), "status": status, "props": {"status": status}, "tags": labels}))
PY
`

const githubTaskUpdateCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
tid = os.environ["BUTTONS_ARG_ID"]
patch = json.loads(os.environ.get("BUTTONS_ARG_PATCH") or "{}")
def run_gh(cmd, label):
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        print(json.dumps({"ok": False, "error": proc.stderr.strip() or f"{label} failed"})); sys.exit(1)
if "title" in patch:
    run_gh(["gh","issue","edit",tid,"--repo",repo,"--title",str(patch["title"])], "title update")
if "body" in patch:
    run_gh(["gh","issue","edit",tid,"--repo",repo,"--body",str(patch["body"])], "body update")
status = patch.get("status") or (patch.get("props") or {}).get("status")
if status:
    view = subprocess.run(["gh","issue","view",tid,"--repo",repo,"--json","labels"], capture_output=True, text=True)
    if view.returncode != 0:
        print(json.dumps({"ok": False, "error": view.stderr.strip() or "label read failed"})); sys.exit(1)
    labels = [l.get("name","") for l in (json.loads(view.stdout or "{}").get("labels") or [])]
    for lab in labels:
        if lab.startswith("status:"):
            run_gh(["gh","issue","edit",tid,"--repo",repo,"--remove-label",lab], "status clear")
    run_gh(["gh","issue","edit",tid,"--repo",repo,"--add-label",f"status:{status}"], "status update")
print(json.dumps({"ok": True, "id": tid, "patch": patch}))
PY
`

const githubTaskRmCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
tid = os.environ["BUTTONS_ARG_ID"]
proc = subprocess.run(["gh","issue","close",tid,"--repo",repo,"--comment","removed via buttons flow task rm"], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip() or "close failed"})); sys.exit(1)
print(json.dumps({"ok": True, "id": tid}))
PY
`

const githubTaskCommentCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
tid = os.environ["BUTTONS_ARG_ID"]
body = os.environ.get("BUTTONS_ARG_BODY") or ""
proc = subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",body], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip() or "comment failed"})); sys.exit(1)
print(json.dumps({"ok": True, "id": tid}))
PY
`

const githubApproveCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
tid = os.environ["BUTTONS_ARG_ID"]
proc = subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-label","flow.pending_approval","--add-label","approved"], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip() or "approve failed"})); sys.exit(1)
print(json.dumps({"ok": True, "id": tid, "approved": True}))
PY
`

const githubRejectCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess, sys
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
if not repo:
    print(json.dumps({"ok": False, "error": "repo required"})); sys.exit(1)
tid = os.environ["BUTTONS_ARG_ID"]
reason = os.environ.get("BUTTONS_ARG_REASON") or "rejected"
proc = subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-label","flow.pending_approval"], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip() or "reject failed"})); sys.exit(1)
proc = subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",f"rejected: {reason}"], capture_output=True, text=True)
if proc.returncode != 0:
    print(json.dumps({"ok": False, "error": proc.stderr.strip() or "comment failed"})); sys.exit(1)
print(json.dumps({"ok": True, "id": tid, "rejected": True}))
PY
`
