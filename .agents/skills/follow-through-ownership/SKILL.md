---
name: "follow-through-ownership"
description: "Turn recurring reports and monitoring signals into verified internal follow-up and closure."
---

# Follow-through ownership

Use after standups, recaps, tracker reviews, alerts, scheduled research, and monitoring passes when evidence reveals an internal action, dependency, blocker, contradiction, or overdue commitment.

## Workflow

1. Extract each actionable signal:
   - evidence and source link
   - required outcome
   - accountable owner
   - urgency or checkpoint
   - completion proof
2. Confirm it is still open. Check recent messages, trackers, and prior coordination for closure or a duplicate nudge.
3. Check authority:
   - proceed only with internal, reversible, narrowly scoped coordination already authorized by the source workflow or user
   - require explicit approval for external outreach, consequential writes, calendar booking without an agreed time, permission changes, purchases, or unrelated work
4. Prepare before contacting:
   - why it matters
   - exact ask
   - proposed next step or bounded options
   - source material needed to respond
5. Contact the smallest relevant internal audience. Name one accountable owner; do not broadcast a vague “someone should follow up.”
6. Record the receipt in the source workflow state or tracker:
   - message/thread ID
   - owner
   - next checkpoint
   - completion criteria
   - status: open, waiting, blocked, or completed
7. Surface unresolved items in the next scheduled report. When evidence verifies completion, report the receipt once and close the loop.

## Judgment rules

- A report is not complete when it identifies an actionable internal dependency but leaves the next move ownerless.
- Do not infer progress, decisions, owners, deadlines, or authorization.
- Prefer one useful message with prepared context over repeated reminders.
- Escalate only when the owner is ambiguous, the blocker crosses teams, the deadline materially changed, or the same condition remains unresolved through its checkpoint.
- Silence is not completion; require evidence.
- Do not create a ticket, meeting, document, or ceremony unless the user or governing workflow authorizes it.

## Cron integration

Scheduled jobs that produce standups, recaps, tracker reviews, alerts, or research digests should invoke this skill as their final phase:

1. Generate and send the primary report.
2. Run `follow-through-ownership` over the report's collaboration, blocker, overdue, contradiction, and next-action items.
3. Send authorized internal coordination.
4. Persist receipts and checkpoints.
5. Return the cron's required silent sentinel when direct delivery already occurred.

Avoid a separate high-frequency follow-up cron when the source cron can own the loop. Add a checkpoint cron only when an open item has a real deadline and the source workflow will not run before it.
