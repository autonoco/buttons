package engine

import (
	"bytes"
	"strings"
	"testing"
)

func collect(sink chan LogLine) []LogLine {
	var out []LogLine
	for l := range sink {
		out = append(out, l)
	}
	return out
}

func TestLineTee_SplitsOnNewline(t *testing.T) {
	var buf bytes.Buffer
	sink := make(chan LogLine, 10)
	tee := newLineTee(&buf, sink, SeverityStdout)

	if _, err := tee.Write([]byte("one\ntwo\nthree\n")); err != nil {
		t.Fatal(err)
	}
	close(sink)

	lines := collect(sink)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %+v", len(lines), lines)
	}
	for i, want := range []string{"one", "two", "three"} {
		if lines[i].Text != want {
			t.Errorf("line[%d] = %q, want %q", i, lines[i].Text, want)
		}
		if lines[i].Sev != SeverityStdout {
			t.Errorf("line[%d] severity = %v, want stdout", i, lines[i].Sev)
		}
	}
	// Primary capture must be verbatim, including newlines.
	if got := buf.String(); got != "one\ntwo\nthree\n" {
		t.Errorf("primary buf = %q", got)
	}
}

func TestLineTee_PartialLineHeldUntilFlush(t *testing.T) {
	var buf bytes.Buffer
	sink := make(chan LogLine, 10)
	tee := newLineTee(&buf, sink, SeverityStderr)

	if _, err := tee.Write([]byte("hello, ")); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	// No newline yet — nothing emitted.
	select {
	case l := <-sink:
		t.Fatalf("unexpected emit before newline/flush: %+v", l)
	default:
	}
	tee.Flush()
	close(sink)

	lines := collect(sink)
	if len(lines) != 1 || lines[0].Text != "hello, world" {
		t.Fatalf("want one line 'hello, world'; got %+v", lines)
	}
	if lines[0].Sev != SeverityStderr {
		t.Errorf("severity = %v, want stderr", lines[0].Sev)
	}
}

func TestLineTee_SplitAcrossWrites(t *testing.T) {
	var buf bytes.Buffer
	sink := make(chan LogLine, 10)
	tee := newLineTee(&buf, sink, SeverityStdout)

	// Split a logical line across two writes.
	if _, err := tee.Write([]byte("first-")); err != nil {
		t.Fatal(err)
	}
	if _, err := tee.Write([]byte("half\nsecond\n")); err != nil {
		t.Fatal(err)
	}
	close(sink)

	lines := collect(sink)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0].Text != "first-half" {
		t.Errorf("line[0] = %q, want first-half", lines[0].Text)
	}
	if lines[1].Text != "second" {
		t.Errorf("line[1] = %q, want second", lines[1].Text)
	}
}

func TestLineTee_NilSinkIsPassthrough(t *testing.T) {
	var buf bytes.Buffer
	tee := newLineTee(&buf, nil, SeverityStdout)

	if _, err := tee.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	tee.Flush() // should not panic
	if got := buf.String(); got != "hello\n" {
		t.Errorf("primary buf = %q", got)
	}
}

func TestLineTee_DropsWhenSinkFull(t *testing.T) {
	var buf bytes.Buffer
	// Unbuffered sink that nobody's reading → every emit should drop.
	sink := make(chan LogLine)
	tee := newLineTee(&buf, sink, SeverityStdout)

	payload := strings.Repeat("line\n", 50)
	if _, err := tee.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	// Primary capture is still complete. Sink receives nothing because
	// drops are the policy when back-pressured.
	if got := buf.String(); got != payload {
		t.Errorf("primary buf lost bytes: %d written, %d captured", len(payload), len(got))
	}
	select {
	case l := <-sink:
		t.Fatalf("unexpected emit on stalled sink: %+v", l)
	default:
	}
}

func TestSeverityString(t *testing.T) {
	cases := map[Severity]string{
		SeverityInfo:   "info",
		SeverityStdout: "stdout",
		SeverityStderr: "stderr",
		SeverityWarn:   "warn",
		Severity(99):   "unknown",
	}
	for sev, want := range cases {
		if got := sev.String(); got != want {
			t.Errorf("Severity(%d).String() = %q, want %q", sev, got, want)
		}
	}
}
