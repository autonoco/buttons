package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWakeHandlerRunsUpdate(t *testing.T) {
	called := make(chan struct{}, 1)
	handler := WakeHandler{
		Token: "secret",
		Update: func(context.Context) error {
			called <- struct{}{}
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/wake", strings.NewReader(`{"type":"button.version_published"}`))
	req.Header.Set("Authorization", "Bearer secret")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", res.Code)
	}
	select {
	case <-called:
	default:
		t.Fatal("expected wake handler to run update")
	}
}

func TestWakeHandlerRejectsBadToken(t *testing.T) {
	handler := WakeHandler{Token: "secret", Update: func(context.Context) error {
		t.Fatal("update should not run")
		return nil
	}}

	req := httptest.NewRequest(http.MethodPost, "/wake", strings.NewReader(`{"type":"button.version_published"}`))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.Code)
	}
}

func TestWakeHandlerRequiresTokenByDefault(t *testing.T) {
	handler := WakeHandler{Update: func(context.Context) error {
		t.Fatal("update should not run")
		return nil
	}}

	req := httptest.NewRequest(http.MethodPost, "/wake", strings.NewReader(`{"type":"button.version_published"}`))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.Code)
	}
}

func TestWakeHandlerAllowUnauthenticatedIsExplicit(t *testing.T) {
	called := make(chan struct{}, 1)
	handler := WakeHandler{
		AllowUnauthenticated: true,
		Update: func(context.Context) error {
			called <- struct{}{}
			return nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/wake", strings.NewReader(`{"type":"button.version_published"}`))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)

	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", res.Code)
	}
	select {
	case <-called:
	default:
		t.Fatal("expected wake handler to run update")
	}
}
