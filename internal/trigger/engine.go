package trigger

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/autonoco/buttons/internal/button"
	"github.com/autonoco/buttons/internal/runner"
	cron "github.com/robfig/cron/v3"
)

// Engine runs cron + watch triggers in-process and produces webhook routes for
// the serve listener to mount. Construct with NewEngine, Start, then Stop.
type Engine struct {
	svc  *button.Service
	cron *cron.Cron
	wg   sync.WaitGroup
	log  func(string, ...any)
}

// NewEngine builds an Engine. log may be nil (then logging is dropped).
func NewEngine(svc *button.Service, log func(string, ...any)) *Engine {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Engine{svc: svc, log: log}
}

// WebhookRoute is one webhook trigger to mount on the HTTP listener.
type WebhookRoute struct {
	Path   string
	Button string
	Token  string
	Args   map[string]string
}

// Start launches cron schedulers and file watchers for every configured
// trigger and returns the webhook routes the caller must mount. Cron/watch run
// until ctx is cancelled; call Stop to drain.
func (e *Engine) Start(ctx context.Context) ([]WebhookRoute, error) {
	all, err := ListAll(e.svc)
	if err != nil {
		return nil, err
	}
	e.cron = cron.New()
	webhooks := []WebhookRoute{}
	cronN, watchN := 0, 0

	for _, b := range all {
		switch b.Trigger.Kind {
		case KindCron:
			name, args := b.Button, b.Trigger.Args
			if _, err := e.cron.AddFunc(b.Trigger.Schedule, func() { e.fire(name, args, "cron") }); err != nil {
				e.log("skip cron trigger on %s: %v", name, err)
				continue
			}
			cronN++
		case KindWatch:
			e.wg.Add(1)
			go func(name, path string, args map[string]string) {
				defer e.wg.Done()
				e.watch(ctx, name, path, args)
			}(b.Button, b.Trigger.Path, b.Trigger.Args)
			watchN++
		case KindWebhook:
			webhooks = append(webhooks, WebhookRoute{
				Path: b.Trigger.Path, Button: b.Button, Token: b.Trigger.Token, Args: b.Trigger.Args,
			})
		}
	}

	if cronN > 0 {
		e.cron.Start()
	}
	e.log("triggers loaded: %d cron, %d watch, %d webhook", cronN, watchN, len(webhooks))
	return webhooks, nil
}

// Stop halts the cron scheduler and waits for watchers + running jobs to finish.
func (e *Engine) Stop() {
	if e.cron != nil {
		<-e.cron.Stop().Done() // wait out any in-flight cron jobs
	}
	e.wg.Wait()
}

// fire presses the button. The button's own timeout applies via runner.
func (e *Engine) fire(name string, args map[string]string, source string) {
	res, err := runner.Press(context.Background(), name, args, runner.Options{RecordHistory: true})
	if err != nil {
		e.log("[%s] %s press error: %v", source, name, err)
		return
	}
	e.log("[%s] %s → %s (%dms)", source, name, res.Status, res.DurationMs)
}

// watch polls a file's mtime/size every 500ms and fires on change. Polling
// (vs fsnotify) is robust across editors that rename-replace on save, and
// keeps the watch dependency-free; the 500ms interval is the debounce window.
func (e *Engine) watch(ctx context.Context, name, path string, args map[string]string) {
	const interval = 500 * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastMod time.Time
	var lastSize int64
	primed := false

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fi, err := os.Stat(path)
			if err != nil {
				continue // file missing/unreadable right now; keep watching
			}
			mod, size := fi.ModTime(), fi.Size()
			if !primed {
				lastMod, lastSize, primed = mod, size, true
				continue
			}
			if !mod.Equal(lastMod) || size != lastSize {
				lastMod, lastSize = mod, size
				e.fire(name, args, "watch:"+path)
			}
		}
	}
}

// WebhookHandler returns the HTTP handler for one webhook route. A POST presses
// the button (token-gated when set) and responds 202 immediately so the sender
// doesn't block on the press.
func (e *Engine) WebhookHandler(route WebhookRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method_not_allowed"})
			return
		}
		if route.Token != "" && !tokenOK(r, route.Token) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		// Body isn't mapped to args in v1 (the trigger's configured args are
		// used); drain it bounded so the sender's write completes.
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		go e.fire(route.Button, route.Args, "webhook:"+route.Path)
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "button": route.Button})
	}
}

func tokenOK(r *http.Request, want string) bool {
	return constEq(r.Header.Get("X-Buttons-Token"), want) || constEq(r.URL.Query().Get("token"), want)
}

func constEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
