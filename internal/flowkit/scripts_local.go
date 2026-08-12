package flowkit

// Local pipeline button bodies. Prefer python3 for JSON so we don't
// depend on jq being installed.

const localProviderListCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, time
from pathlib import Path

board = os.environ["BUTTONS_ARG_BOARD"]
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
tasks_dir = Path(home) / "flows" / board / "tasks"
tasks_dir.mkdir(parents=True, exist_ok=True)
now = time.time()
items = []
for path in sorted(tasks_dir.glob("*.json")):
    task = json.loads(path.read_text())
    props = task.setdefault("props", {})
    status = props.get("status") or task.get("status") or "intake"
    claimed_by = props.get("flow.claimed_by") or ""
    claimed_at = props.get("flow.claimed_at") or ""
    timeout = int(task.get("timeout_seconds") or props.get("timeout_seconds") or 3600)
    stale = False
    if claimed_by and claimed_at:
        try:
            # RFC3339
            from datetime import datetime, timezone
            ts = datetime.fromisoformat(claimed_at.replace("Z", "+00:00")).timestamp()
            stale = (now - ts) > timeout
        except Exception:
            stale = True
    actionable = status != "done" and (not claimed_by or stale)
    if props.get("flow.pending_approval"):
        actionable = False
    items.append({
        "id": task.get("id") or path.stem,
        "title": task.get("title", ""),
        "body": task.get("body", ""),
        "status": status,
        "props": props,
        "tags": task.get("tags") or [],
        "claimed_by": claimed_by,
        "claimed_at": claimed_at,
        "timeout_seconds": timeout,
        "actionable": actionable,
        "comments": task.get("comments") or [],
    })
print(json.dumps({"items": items}))
PY
`

const localClaimCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, time, tempfile
from pathlib import Path
from datetime import datetime, timezone

def atomic_write(p, text):
    fd, tmp = tempfile.mkstemp(dir=str(p.parent), suffix=".tmp")
    try:
        os.write(fd, text.encode()); os.fchmod(fd, 0o600); os.close(fd)
        os.replace(tmp, str(p))
    except BaseException:
        os.close(fd) if not os.get_inheritable(fd) else None
        os.unlink(tmp)
        raise

board = os.environ["BUTTONS_ARG_BOARD"]
raw = os.environ["BUTTONS_ARG_TASK"]
task_in = json.loads(raw) if raw.strip().startswith("{") else {"id": raw}
tid = str(task_in.get("id") or "")
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if not path.exists():
    print(json.dumps({"claimed": False, "reason": "not_found"}))
    raise SystemExit(0)
task = json.loads(path.read_text())
props = task.setdefault("props", {})
claimed_by = props.get("flow.claimed_by") or ""
claimed_at = props.get("flow.claimed_at") or ""
timeout = int(task.get("timeout_seconds") or props.get("timeout_seconds") or 3600)
stale = False
if claimed_by and claimed_at:
    try:
        ts = datetime.fromisoformat(claimed_at.replace("Z", "+00:00")).timestamp()
        stale = (time.time() - ts) > timeout
    except Exception:
        stale = True
holder = os.environ.get("BUTTONS_FLOW_HOLDER") or f"agent-{os.getpid()}"
if claimed_by and not stale and claimed_by != holder:
    print(json.dumps({"claimed": False, "reason": "already_claimed", "claimed_by": claimed_by}))
    raise SystemExit(0)
now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
props["flow.claimed_by"] = holder
props["flow.claimed_at"] = now
props["flow.claim_label"] = "Agent Claimed"
atomic_write(path, json.dumps(task, indent=2) + "\n")
time.sleep(float(os.environ.get("BUTTONS_FLOW_CLAIM_WAIT", "1")))
task2 = json.loads(path.read_text())
props2 = task2.get("props") or {}
if props2.get("flow.claimed_by") != holder:
    print(json.dumps({"claimed": False, "reason": "lost_race", "claimed_by": props2.get("flow.claimed_by")}))
    raise SystemExit(0)
print(json.dumps({"claimed": True, "claimed_by": holder, "claimed_at": now, "id": tid}))
PY
`

const localPerformCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os

raw = os.environ.get("BUTTONS_ARG_TASK", "{}")
task = json.loads(raw) if raw.strip().startswith("{") else {"id": raw}
status = (task.get("props") or {}).get("status") or task.get("status") or "intake"
transitions = {}
try:
    transitions = json.loads(os.environ.get("BUTTONS_ARG_TRANSITIONS") or "{}")
except Exception:
    transitions = {}
cands = transitions.get(status) or []
# Deterministic local perform: advance to first transition when available.
if cands:
    out = {"verdict": "advance", "to_stage": cands[0], "summary": f"advanced from {status} to {cands[0]}", "from_stage": status}
else:
    out = {"verdict": "hold", "to_stage": status, "summary": f"no transitions from {status}", "from_stage": status}
# Allow override via env for tests.
override = os.environ.get("BUTTONS_FLOW_VERDICT")
if override:
    out = json.loads(override)
print(json.dumps(out))
PY
`

const localValidateCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os

raw_task = os.environ.get("BUTTONS_ARG_TASK", "{}")
task = json.loads(raw_task) if raw_task.strip().startswith("{") else {"id": raw_task}
raw_verdict = os.environ.get("BUTTONS_ARG_VERDICT", "{}")
verdict = json.loads(raw_verdict) if raw_verdict.strip().startswith("{") else {}
transitions = json.loads(os.environ.get("BUTTONS_ARG_TRANSITIONS") or "{}")
evidence = json.loads(os.environ.get("BUTTONS_ARG_EVIDENCE") or "{}")
status = (task.get("props") or {}).get("status") or task.get("status") or "intake"
v = verdict.get("verdict") or ""
allowed = {"advance", "retry", "hold", "delegate", "escalate"}
ok = v in allowed
reason = ""
if not ok:
    reason = f"invalid verdict {v!r}"
to_stage = verdict.get("to_stage") or status
if ok and v == "advance":
    cands = transitions.get(status) or []
    if to_stage not in cands:
        ok = False
        reason = f"to_stage {to_stage!r} not in transitions {cands}"
    req = evidence.get(status) or []
    for field in req:
        if not verdict.get(field):
            ok = False
            reason = f"missing evidence field {field}"
            break
out = dict(verdict)
out["ok"] = ok
out["from_stage"] = status
out["to_stage"] = to_stage
if reason:
    out["reason"] = reason
print(json.dumps(out))
PY
`

const localApplyCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, tempfile
from pathlib import Path
from datetime import datetime, timezone

def atomic_write(p, text):
    fd, tmp = tempfile.mkstemp(dir=str(p.parent), suffix=".tmp")
    try:
        os.write(fd, text.encode()); os.fchmod(fd, 0o600); os.close(fd)
        os.replace(tmp, str(p))
    except BaseException:
        os.close(fd) if not os.get_inheritable(fd) else None
        os.unlink(tmp)
        raise

board = os.environ["BUTTONS_ARG_BOARD"]
raw_task = os.environ.get("BUTTONS_ARG_TASK", "{}")
task_in = json.loads(raw_task) if raw_task.strip().startswith("{") else {"id": raw_task}
tid = str(task_in.get("id") or "")
verdict = json.loads(os.environ.get("BUTTONS_ARG_VERDICT") or "{}")
gates = json.loads(os.environ.get("BUTTONS_ARG_GATES") or "{}")
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if not path.exists():
    print(json.dumps({"applied": False, "reason": "not_found"}))
    raise SystemExit(0)
task = json.loads(path.read_text())
props = task.setdefault("props", {})
for k, v in (task_in.get("props") or {}).items():
    if k not in props:
        props[k] = v
if not verdict.get("ok", True):
    props["needs_attention"] = verdict.get("reason") or "validate_failed"
    atomic_write(path, json.dumps(task, indent=2) + "\n")
    print(json.dumps({"applied": False, "reason": props["needs_attention"], "id": tid}))
    raise SystemExit(0)
v = verdict.get("verdict")
from_stage = verdict.get("from_stage") or props.get("status")
to_stage = verdict.get("to_stage") or from_stage
gate = gates.get(from_stage) or {}
if v == "advance" and gate.get("requires_human_approval"):
    approved_stage = props.get("flow.approved_stage")
    if approved_stage != from_stage:
        props["flow.pending_approval"] = to_stage
        props.pop("flow.claimed_by", None)
        props.pop("flow.claimed_at", None)
        atomic_write(path, json.dumps(task, indent=2) + "\n")
        print(json.dumps({"applied": True, "pending_approval": to_stage, "id": tid}))
        raise SystemExit(0)
    props.pop("flow.approved_stage", None)
if v == "advance":
    props["status"] = to_stage
    task["status"] = to_stage
props.pop("flow.claimed_by", None)
props.pop("flow.claimed_at", None)
props.pop("flow.pending_approval", None)
if verdict.get("summary"):
    comments = task.setdefault("comments", [])
    comments.append({"body": verdict["summary"], "at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")})
atomic_write(path, json.dumps(task, indent=2) + "\n")
print(json.dumps({"applied": True, "status": props.get("status"), "id": tid, "verdict": v}))
PY
`

const localEnsureTriggerCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os
from pathlib import Path

board = os.environ["BUTTONS_ARG_BOARD"]
provider = os.environ.get("BUTTONS_ARG_PROVIDER") or "local"
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
sched = Path(home) / "flows" / board / "schedule.json"
sched.parent.mkdir(parents=True, exist_ok=True)
if not sched.exists():
    sched.write_text(json.dumps({"board": board, "provider": provider, "kind": "poll", "every_seconds": 60}, indent=2) + "\n")
print(json.dumps({"ok": True, "board": board, "schedule": str(sched), "provider": provider}))
PY
`

const localTaskAddCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, uuid, tempfile
from pathlib import Path
from datetime import datetime, timezone

def atomic_write(p, text):
    fd, tmp = tempfile.mkstemp(dir=str(p.parent), suffix=".tmp")
    try:
        os.write(fd, text.encode()); os.fchmod(fd, 0o600); os.close(fd)
        os.replace(tmp, str(p))
    except BaseException:
        os.close(fd) if not os.get_inheritable(fd) else None
        os.unlink(tmp)
        raise

board = os.environ["BUTTONS_ARG_BOARD"]
title = os.environ.get("BUTTONS_ARG_TITLE") or ""
body = os.environ.get("BUTTONS_ARG_BODY") or ""
status = os.environ.get("BUTTONS_ARG_STATUS") or ""
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
if not status:
    drawer = Path(home) / "drawers" / board / "drawer.json"
    status = "intake"
    if drawer.exists():
        d = json.loads(drawer.read_text())
        status = ((d.get("flow") or {}).get("initial_stage")) or status
tid = uuid.uuid4().hex[:12]
task = {
    "id": tid,
    "title": title,
    "body": body,
    "tags": [],
    "comments": [],
    "props": {"status": status},
    "status": status,
    "created_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
}
tasks = Path(home) / "flows" / board / "tasks"
tasks.mkdir(parents=True, exist_ok=True)
atomic_write(tasks / f"{tid}.json", json.dumps(task, indent=2) + "\n")
print(json.dumps(task))
PY
`

const localTaskListCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os
from pathlib import Path

board = os.environ["BUTTONS_ARG_BOARD"]
filt = os.environ.get("BUTTONS_ARG_FILTER") or ""
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
tasks_dir = Path(home) / "flows" / board / "tasks"
tasks_dir.mkdir(parents=True, exist_ok=True)
want = {}
for part in filt.split(","):
    part = part.strip()
    if "=" in part:
        k, v = part.split("=", 1)
        want[k.strip()] = v.strip()
items = []
for path in sorted(tasks_dir.glob("*.json")):
    task = json.loads(path.read_text())
    props = task.get("props") or {}
    status = props.get("status") or task.get("status")
    ok = True
    for k, v in want.items():
        if k == "status" and status != v:
            ok = False
        elif k != "status" and str(props.get(k, task.get(k, ""))) != v:
            ok = False
    if ok:
        items.append(task)
print(json.dumps({"items": items, "count": len(items)}))
PY
`

const localTaskReadCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, sys
from pathlib import Path
board = os.environ["BUTTONS_ARG_BOARD"]
tid = os.environ["BUTTONS_ARG_ID"]
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if not path.exists():
    print(json.dumps({"ok": False, "error": "not_found"}))
    sys.exit(1)
print(path.read_text())
PY
`

const localTaskUpdateCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, sys, tempfile
from pathlib import Path
board = os.environ["BUTTONS_ARG_BOARD"]
tid = os.environ["BUTTONS_ARG_ID"]
patch = json.loads(os.environ.get("BUTTONS_ARG_PATCH") or "{}")
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if not path.exists():
    print(json.dumps({"ok": False, "error": "not_found"}))
    sys.exit(1)
def atomic_write(p, text):
    fd, tmp = tempfile.mkstemp(dir=str(p.parent), suffix=".tmp")
    try:
        os.write(fd, text.encode()); os.fchmod(fd, 0o600); os.close(fd)
        os.replace(tmp, str(p))
    except BaseException:
        os.close(fd) if not os.get_inheritable(fd) else None
        os.unlink(tmp)
        raise
task = json.loads(path.read_text())
props = task.setdefault("props", {})
for k, v in patch.items():
    if k == "props" and isinstance(v, dict):
        props.update(v)
    elif k == "tags" and isinstance(v, list):
        task["tags"] = v
    elif k == "status":
        props["status"] = v
        task["status"] = v
    else:
        task[k] = v
atomic_write(path, json.dumps(task, indent=2) + "\n")
print(json.dumps(task))
PY
`

const localTaskRmCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os
from pathlib import Path
board = os.environ["BUTTONS_ARG_BOARD"]
tid = os.environ["BUTTONS_ARG_ID"]
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if path.exists():
    path.unlink()
print(json.dumps({"ok": True, "id": tid}))
PY
`

const localTaskCommentCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, sys, tempfile
from pathlib import Path
from datetime import datetime, timezone
board = os.environ["BUTTONS_ARG_BOARD"]
tid = os.environ["BUTTONS_ARG_ID"]
body = os.environ.get("BUTTONS_ARG_BODY") or ""
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if not path.exists():
    print(json.dumps({"ok": False, "error": "not_found"}))
    sys.exit(1)
def atomic_write(p, text):
    fd, tmp = tempfile.mkstemp(dir=str(p.parent), suffix=".tmp")
    try:
        os.write(fd, text.encode()); os.fchmod(fd, 0o600); os.close(fd)
        os.replace(tmp, str(p))
    except BaseException:
        os.close(fd) if not os.get_inheritable(fd) else None
        os.unlink(tmp)
        raise
task = json.loads(path.read_text())
comments = task.setdefault("comments", [])
comments.append({"body": body, "at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")})
atomic_write(path, json.dumps(task, indent=2) + "\n")
print(json.dumps(task))
PY
`

const localApproveCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, sys, tempfile
from pathlib import Path
board = os.environ["BUTTONS_ARG_BOARD"]
tid = os.environ["BUTTONS_ARG_ID"]
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if not path.exists():
    print(json.dumps({"ok": False, "error": "not_found"}))
    sys.exit(1)
def atomic_write(p, text):
    fd, tmp = tempfile.mkstemp(dir=str(p.parent), suffix=".tmp")
    try:
        os.write(fd, text.encode()); os.fchmod(fd, 0o600); os.close(fd)
        os.replace(tmp, str(p))
    except BaseException:
        os.close(fd) if not os.get_inheritable(fd) else None
        os.unlink(tmp)
        raise
task = json.loads(path.read_text())
props = task.setdefault("props", {})
pending = props.pop("flow.pending_approval", None)
from_stage = props.get("status") or task.get("status") or "intake"
props["flow.approved_stage"] = from_stage
if pending:
    props["status"] = pending
    task["status"] = pending
atomic_write(path, json.dumps(task, indent=2) + "\n")
print(json.dumps({"ok": True, "id": tid, "status": props.get("status"), "approved_to": pending}))
PY
`

const localRejectCode = `#!/bin/sh
set -e
python3 - <<'PY'
import json, os, sys, tempfile
from pathlib import Path
from datetime import datetime, timezone
board = os.environ["BUTTONS_ARG_BOARD"]
tid = os.environ["BUTTONS_ARG_ID"]
reason = os.environ.get("BUTTONS_ARG_REASON") or "rejected"
home = os.environ.get("BUTTONS_HOME") or str(Path.home() / ".buttons")
path = Path(home) / "flows" / board / "tasks" / f"{tid}.json"
if not path.exists():
    print(json.dumps({"ok": False, "error": "not_found"}))
    sys.exit(1)
def atomic_write(p, text):
    fd, tmp = tempfile.mkstemp(dir=str(p.parent), suffix=".tmp")
    try:
        os.write(fd, text.encode()); os.fchmod(fd, 0o600); os.close(fd)
        os.replace(tmp, str(p))
    except BaseException:
        os.close(fd) if not os.get_inheritable(fd) else None
        os.unlink(tmp)
        raise
task = json.loads(path.read_text())
props = task.setdefault("props", {})
props.pop("flow.pending_approval", None)
props["needs_attention"] = reason
comments = task.setdefault("comments", [])
comments.append({"body": f"rejected: {reason}", "at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")})
atomic_write(path, json.dumps(task, indent=2) + "\n")
print(json.dumps({"ok": True, "id": tid, "rejected": True}))
PY
`
