package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type WakeEvent struct {
	Type   string `json:"type"`
	Desk   string `json:"desk,omitempty"`
	Button struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Kind    string `json:"kind"`
	} `json:"button,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

type WakeHandler struct {
	Token   string
	Update  func(context.Context) error
	Options Options
}

func (h WakeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.Token != "" && bearerToken(r.Header.Get("Authorization")) != h.Token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var event WakeEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if event.Type != "button.version_published" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	update := h.Update
	if update == nil {
		update = func(ctx context.Context) error {
			_, err := Apply(ctx, h.Options)
			return err
		}
	}
	if err := update(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func bearerToken(header string) string {
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}
