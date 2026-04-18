package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/engine"
)

// logsSinkBuffer is the streaming channel depth. Generous — the
// engine's tee is non-blocking (drops on backpressure), so a wide
// buffer makes bursty scripts rare to drop during the microsecond
// gap between emit and Bubble Tea's receive.
const logsSinkBuffer = 256

// logLineMsg carries a single streamed line from the child into the
// model's Update. Wrapping LogLine (rather than receiving it
// directly) keeps Bubble Tea's msg-type discrimination clean.
type logLineMsg struct {
	line engine.LogLine
}

// logsChannelClosedMsg fires once the sink is closed by streamPress
// — after Execute returns and the goroutine ran close(sink). Tells
// the Update loop to stop re-arming waitForLine.
type logsChannelClosedMsg struct{}

// logsDoneMsg carries the final Result plus any startup error. The
// press is definitively over when this arrives; after it we flip
// off the spinner and let the user scroll / dismiss at leisure.
type logsDoneMsg struct {
	result *engine.Result
}

// streamPress runs engine.Execute on a goroutine, tees output into
// sink (via the engine's LineSink), and returns logsDoneMsg when the
// press completes. close(sink) on return so waitForLine terminates.
func streamPress(
	ctx context.Context,
	btn *button.Button,
	args, batteries map[string]string,
	sink chan engine.LogLine,
	codePath string,
) tea.Cmd {
	return func() tea.Msg {
		// chan engine.LogLine is bi-directional; engine.Execute wants
		// a LineSink (chan<- LogLine type-alias), which implicit
		// conversion handles. The close happens on the bi-dir side
		// we own — engine.Execute never closes its input.
		result := engine.Execute(ctx, btn, args, batteries, sink, codePath)
		close(sink)
		return logsDoneMsg{result: result}
	}
}

// waitForLine returns a Cmd that blocks on one receive from the sink.
// On receive, emits logLineMsg and the Update loop re-arms waitForLine
// for the next line. On close (streamPress done), emits
// logsChannelClosedMsg and the Update loop stops re-arming.
func waitForLine(sink <-chan engine.LogLine) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-sink
		if !ok {
			return logsChannelClosedMsg{}
		}
		return logLineMsg{line: line}
	}
}

// logsTimeoutContext builds the context used for the press. Same
// timeout-seconds semantics as cmd/press uses, so behavior stays
// identical whether you ran the press via CLI or logs TUI.
func logsTimeoutContext(btn *button.Button) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(btn.TimeoutSeconds)*time.Second)
}
