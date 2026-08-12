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
import json, os, subprocess, time

repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
raw = os.environ.get("BUTTONS_ARG_TASK","{}")
task = json.loads(raw) if raw.strip().startswith("{") else {"id": raw}
tid = str(task.get("id") or "")
if not repo or not tid:
    print(json.dumps({"claimed": False, "reason": "missing_repo_or_id"}))
    raise SystemExit(0)
holder = os.environ.get("BUTTONS_FLOW_HOLDER") or os.environ.get("GITHUB_ACTOR") or "buttons-agent"
view = subprocess.run(["gh","issue","view",tid,"--repo",repo,"--json","assignees,labels"], capture_output=True, text=True)
if view.returncode != 0:
    print(json.dumps({"claimed": False, "reason": "view_failed", "error": view.stderr.strip()}))
    raise SystemExit(0)
data = json.loads(view.stdout)
assignees = data.get("assignees") or []
if assignees and assignees[0].get("login") != holder:
    print(json.dumps({"claimed": False, "reason": "already_claimed", "claimed_by": assignees[0].get("login")}))
    raise SystemExit(0)
edit = subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--add-assignee",holder,"--add-label","Agent Claimed"], capture_output=True, text=True)
if edit.returncode != 0:
    print(json.dumps({"claimed": False, "reason": "edit_failed", "error": edit.stderr.strip()}))
    raise SystemExit(0)
time.sleep(float(os.environ.get("BUTTONS_FLOW_CLAIM_WAIT","1")))
view2 = subprocess.run(["gh","issue","view",tid,"--repo",repo,"--json","assignees"], capture_output=True, text=True)
data2 = json.loads(view2.stdout or "{}")
assignees2 = data2.get("assignees") or []
if not assignees2 or assignees2[0].get("login") != holder:
    print(json.dumps({"claimed": False, "reason": "lost_race"}))
    raise SystemExit(0)
print(json.dumps({"claimed": True, "claimed_by": holder, "id": tid}))
PY
`

const githubApplyCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess

repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
raw_task = os.environ.get("BUTTONS_ARG_TASK","{}")
task = json.loads(raw_task) if raw_task.strip().startswith("{") else {"id": raw_task}
tid = str(task.get("id") or "")
verdict = json.loads(os.environ.get("BUTTONS_ARG_VERDICT") or "{}")
gates = json.loads(os.environ.get("BUTTONS_ARG_GATES") or "{}")
if not repo or not tid:
    print(json.dumps({"applied": False, "reason": "missing_repo_or_id"}))
    raise SystemExit(0)
if not verdict.get("ok", True):
    subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",f"needs_attention: {verdict.get('reason')}"], check=False)
    print(json.dumps({"applied": False, "reason": verdict.get("reason")}))
    raise SystemExit(0)
v = verdict.get("verdict")
from_stage = verdict.get("from_stage") or (task.get("props") or {}).get("status")
to_stage = verdict.get("to_stage") or from_stage
gate = gates.get(from_stage) or {}
if v == "advance" and gate.get("requires_human_approval"):
    subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--add-label","flow.pending_approval"], check=False)
    print(json.dumps({"applied": True, "pending_approval": to_stage, "id": tid}))
    raise SystemExit(0)
if v == "advance":
    # swap status label
    if from_stage:
        subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-label",f"status:{from_stage}"], check=False)
    subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--add-label",f"status:{to_stage}"], check=False)
    if to_stage == "done":
        subprocess.run(["gh","issue","close",tid,"--repo",repo], check=False)
if verdict.get("summary"):
    subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",verdict["summary"]], check=False)
# clear assignee to release claim
subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-assignee","@me"], check=False)
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
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
board = os.environ["BUTTONS_ARG_BOARD"]
filt = os.environ.get("BUTTONS_ARG_FILTER") or ""
want_status = None
for part in filt.split(","):
    if part.strip().startswith("status="):
        want_status = part.split("=",1)[1].strip()
label = f"flow:{board}"
proc = subprocess.run(["gh","issue","list","--repo",repo,"--label",label,"--state","all","--json","number,title,body,labels,state"], capture_output=True, text=True)
issues = json.loads(proc.stdout or "[]") if proc.returncode == 0 else []
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
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
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
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
tid = os.environ["BUTTONS_ARG_ID"]
patch = json.loads(os.environ.get("BUTTONS_ARG_PATCH") or "{}")
if "title" in patch:
    subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--title",str(patch["title"])], check=False)
if "body" in patch:
    subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--body",str(patch["body"])], check=False)
status = patch.get("status") or (patch.get("props") or {}).get("status")
if status:
    subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--add-label",f"status:{status}"], check=False)
print(json.dumps({"ok": True, "id": tid, "patch": patch}))
PY
`

const githubTaskRmCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
tid = os.environ["BUTTONS_ARG_ID"]
subprocess.run(["gh","issue","close",tid,"--repo",repo,"--comment","removed via buttons flow task rm"], check=False)
print(json.dumps({"ok": True, "id": tid}))
PY
`

const githubTaskCommentCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
tid = os.environ["BUTTONS_ARG_ID"]
body = os.environ.get("BUTTONS_ARG_BODY") or ""
subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",body], check=True)
print(json.dumps({"ok": True, "id": tid}))
PY
`

const githubApproveCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
tid = os.environ["BUTTONS_ARG_ID"]
subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-label","flow.pending_approval","--add-label","approved"], check=False)
print(json.dumps({"ok": True, "id": tid, "approved": True}))
PY
`

const githubRejectCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, subprocess
repo = os.environ.get("BUTTONS_ARG_REPO") or os.environ.get("BUTTONS_FLOW_REPO")
tid = os.environ["BUTTONS_ARG_ID"]
reason = os.environ.get("BUTTONS_ARG_REASON") or "rejected"
subprocess.run(["gh","issue","edit",tid,"--repo",repo,"--remove-label","flow.pending_approval"], check=False)
subprocess.run(["gh","issue","comment",tid,"--repo",repo,"--body",f"rejected: {reason}"], check=False)
print(json.dumps({"ok": True, "id": tid, "rejected": True}))
PY
`
