// Stream support for Execute.
//
// A caller that wants to watch a press as it runs — notably the
// forthcoming `buttons logs <name>` TUI — passes a LineSink to
// Execute. Every line the child process writes to stdout / stderr
// arrives on the channel tagged with a Severity and a wall-clock
// timestamp. The sink is optional: passing nil keeps the pre-streaming
// behavior intact (buffered Stdout / Stderr on Result, no channel).
//
// Back-pressure policy
//
// The tee emits to the sink via a non-blocking select. If the consumer
// can't keep up, lines are dropped — the buffered `Result.Stdout` /
// `Result.Stderr` remain authoritative (that's where history.Record
// pulls from). Dropping a preview line is the right trade versus
// blocking the press itself on a slow UI. Use a wide channel (buf >=
// 64) if you want to see every line.
package engine

import (
	"bytes"
	"io"
	"sync"
	"time"
)

// Severity classifies a log line's source. Consumers (TUI log viewer,
// future log filters) colorize and group by this.
type Severity int

const (
	// SeverityInfo is emitted by the engine itself, not the child —
	// used for "starting press X" / "exit Y" wrapping. Reserved for
	// future wrapping lines; the shell path doesn't emit any today.
	SeverityInfo Severity = iota
	// SeverityStdout is anything the child wrote to its stdout.
	SeverityStdout
	// SeverityStderr is anything the child wrote to its stderr.
	SeverityStderr
	// SeverityWarn is reserved for future heuristic or explicit warn
	// tagging (e.g. lines starting with `warn:` or matching a warn
	// pattern). Not emitted automatically in this version.
	SeverityWarn
)

// String returns a stable lowercase name — handy for logging and for
// JSON serialization if we ever expose a stream endpoint.
func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityStdout:
		return "stdout"
	case SeverityStderr:
		return "stderr"
	case SeverityWarn:
		return "warn"
	default:
		return "unknown"
	}
}

// LogLine is one classified chunk of output. Text never includes the
// trailing newline — consumers shouldn't have to strip.
type LogLine struct {
	Ts   time.Time
	Sev  Severity
	Text string
}

// LineSink is the channel type Execute's caller provides. Send-only
// from the engine's perspective so a caller can't accidentally receive
// on their own sink.
type LineSink = chan<- LogLine

// lineTee wraps an underlying io.Writer (the primary capture buffer)
// and, as a side effect, splits incoming bytes on newline and emits
// each completed line to a LineSink tagged with a fixed severity.
// Partial lines are accumulated across Write calls until a Flush or
// the next newline-terminated chunk.
type lineTee struct {
	w    io.Writer // primary capture — source of truth for Result.Stdout/Stderr
	sink LineSink  // optional streaming consumer
	sev  Severity

	mu  sync.Mutex
	buf []byte // in-flight partial line
}

// newLineTee returns a tee writer. If sink is nil, the tee only writes
// to w — zero overhead on the streaming side.
func newLineTee(w io.Writer, sink LineSink, sev Severity) *lineTee {
	return &lineTee{w: w, sink: sink, sev: sev}
}

// Write captures to the primary buffer first (so Result.Stdout is
// always complete regardless of what happens to the sink), then splits
// into lines and emits each to the sink.
func (t *lineTee) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if t.sink == nil {
		return n, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p[:n]...)
	for {
		i := bytes.IndexByte(t.buf, '\n')
		if i < 0 {
			break
		}
		line := string(t.buf[:i])
		t.buf = t.buf[i+1:]
		t.emit(line)
	}
	return n, err
}

// Flush emits any trailing partial line (no newline terminator) still
// sitting in the buffer. Call when the child process exits to catch
// the last log message that scripts without a trailing newline would
// otherwise drop.
func (t *lineTee) Flush() {
	if t.sink == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) > 0 {
		t.emit(string(t.buf))
		t.buf = t.buf[:0]
	}
}

// emit sends a line to the sink. Non-blocking: if the channel is full
// the line is dropped. The primary buffer still holds it, so Result
// and history are unaffected; only the real-time stream loses it.
func (t *lineTee) emit(text string) {
	line := LogLine{Ts: time.Now(), Sev: t.sev, Text: text}
	select {
	case t.sink <- line:
	default:
		// Dropped; see back-pressure policy in the package comment.
	}
}
