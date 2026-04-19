package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/config"
)

var tailFollow bool

var tailCmd = &cobra.Command{
	Use:   "tail <button-or-path>",
	Short: "Follow the progress JSONL of a press",
	Long: `Tail a press's structured progress stream.

Argument can be either:
  • a button name — tails the LATEST press's progress file
  • an absolute path to a .progress.jsonl file

Scripts write to $BUTTONS_PROGRESS_PATH (set automatically by the
engine). Typical event shape:

    {"ts":"...","event":"progress","pct":0.25,"msg":"downloaded 5/20"}
    {"ts":"...","event":"log","level":"info","msg":"..."}

Examples:
  buttons tail publish-npm              # latest press of publish-npm
  buttons tail publish-npm -f           # follow forever
  buttons tail /tmp/run.progress.jsonl  # specific file`,
	Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		path, err := resolveTailPath(target)
		if err != nil {
			return handleServiceError(err)
		}
		return streamFile(path, tailFollow)
	},
}

func init() {
	tailCmd.Flags().BoolVarP(&tailFollow, "follow", "f", false, "keep tailing as new lines arrive")
	rootCmd.AddCommand(tailCmd)
}

// resolveTailPath decides whether the argument is a file path or a
// button name. Absolute or relative paths that exist on disk are
// used verbatim; anything else is treated as a button name whose
// most recent progress file we should find.
func resolveTailPath(target string) (string, error) {
	if strings.ContainsAny(target, "/\\") || strings.HasSuffix(target, ".jsonl") {
		abs, err := filepath.Abs(target)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", err
		}
		return abs, nil
	}

	svc := button.NewService()
	pressedDir, err := svc.PressedDir(target)
	if err != nil {
		return "", err
	}
	return latestProgressFile(pressedDir)
}

// latestProgressFile returns the most recent *.progress.jsonl in dir
// (sorted by filename, which embeds an ISO-ish timestamp).
func latestProgressFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	candidates := []string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".progress.jsonl") {
			candidates = append(candidates, e.Name())
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no progress files yet — press the button first")
	}
	sort.Strings(candidates)
	return filepath.Join(dir, candidates[len(candidates)-1]), nil
}

// streamFile prints the file's current contents to stdout, then
// optionally follows by polling for appends. Poll-based (not inotify)
// so it stays portable across macOS / Linux / Windows.
func streamFile(path string, follow bool) error {
	// #nosec G304 -- path is either user-supplied (explicit), or
	// produced by latestProgressFile which scans a dir rooted in
	// ButtonsDir + button name; no traversal.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReader(f)
	if err := drain(r); err != nil {
		return err
	}
	if !follow {
		return nil
	}
	// Follow mode: poll every 200ms for appended bytes.
	for {
		time.Sleep(200 * time.Millisecond)
		if err := drain(r); err != nil {
			return err
		}
	}
}

func drain(r *bufio.Reader) error {
	for {
		line, err := r.ReadString('\n')
		if line != "" {
			fmt.Print(line)
		}
		if err != nil {
			// EOF is expected and fine; anything else is a real
			// error we want to surface.
			if err.Error() == "EOF" {
				return nil
			}
			return err
		}
	}
}

// referenced so config stays imported even if future edits remove
// the only indirect reference.
var _ = config.DataDir
