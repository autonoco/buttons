// Package trigger stores and runs button triggers (#272): cron schedules,
// file watchers, and webhook endpoints. Triggers live on the button spec
// (button.Trigger); the Engine runs them under `buttons serve`.
package trigger

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/autonoco/buttons/internal/button"
	cron "github.com/robfig/cron/v3"
)

// Trigger kinds.
const (
	KindCron    = "cron"
	KindWatch   = "watch"
	KindWebhook = "webhook"
)

// Add validates and appends a trigger to a button, persisting the spec. The
// generated id is returned for later `trigger rm`.
func Add(svc *button.Service, buttonName string, tr button.Trigger) (*button.Trigger, error) {
	btn, err := svc.Get(buttonName)
	if err != nil {
		return nil, err
	}
	if err := Validate(tr); err != nil {
		return nil, err
	}
	// Reject a duplicate webhook path on the same button (would collide at mount).
	if tr.Kind == KindWebhook {
		for _, ex := range btn.Triggers {
			if ex.Kind == KindWebhook && ex.Path == tr.Path {
				return nil, fmt.Errorf("button %q already has a webhook trigger at %s", buttonName, tr.Path)
			}
		}
	}
	tr.ID = newID()
	tr.CreatedAt = time.Now()
	btn.Triggers = append(btn.Triggers, tr)
	if err := svc.Update(btn); err != nil {
		return nil, err
	}
	return &tr, nil
}

// Validate checks a trigger's shape per kind (cron expression parses, paths set).
func Validate(tr button.Trigger) error {
	switch tr.Kind {
	case KindCron:
		if strings.TrimSpace(tr.Schedule) == "" {
			return fmt.Errorf("cron trigger needs a --schedule")
		}
		if _, err := cron.ParseStandard(tr.Schedule); err != nil {
			return fmt.Errorf("invalid cron schedule %q: %w", tr.Schedule, err)
		}
	case KindWatch:
		if strings.TrimSpace(tr.Path) == "" {
			return fmt.Errorf("watch trigger needs a --path")
		}
	case KindWebhook:
		if !strings.HasPrefix(tr.Path, "/") {
			return fmt.Errorf("webhook trigger needs a --webhook-path starting with '/'")
		}
	default:
		return fmt.Errorf("unknown trigger kind %q (want cron|watch|webhook)", tr.Kind)
	}
	return nil
}

// List returns a button's triggers.
func List(svc *button.Service, buttonName string) ([]button.Trigger, error) {
	btn, err := svc.Get(buttonName)
	if err != nil {
		return nil, err
	}
	return btn.Triggers, nil
}

// Bound is a (button, trigger) pair from a full scan.
type Bound struct {
	Button  string         `json:"button"`
	Trigger button.Trigger `json:"trigger"`
}

// ListAll returns every trigger across every button.
func ListAll(svc *button.Service) ([]Bound, error) {
	buttons, err := svc.List()
	if err != nil {
		return nil, err
	}
	out := []Bound{}
	for _, b := range buttons {
		for _, t := range b.Triggers {
			out = append(out, Bound{Button: b.Name, Trigger: t})
		}
	}
	return out, nil
}

// Remove deletes a trigger by id from a button.
func Remove(svc *button.Service, buttonName, id string) error {
	btn, err := svc.Get(buttonName)
	if err != nil {
		return err
	}
	kept := make([]button.Trigger, 0, len(btn.Triggers))
	found := false
	for _, t := range btn.Triggers {
		if t.ID == id {
			found = true
			continue
		}
		kept = append(kept, t)
	}
	if !found {
		return fmt.Errorf("no trigger %q on button %q", id, buttonName)
	}
	btn.Triggers = kept
	return svc.Update(btn)
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
