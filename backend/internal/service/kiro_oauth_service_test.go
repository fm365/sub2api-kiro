package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
)

func TestKiroOAuthSocialCallbackUpdatesSessionOnCallbackError(t *testing.T) {
	t.Parallel()

	svc := NewKiroOAuthService(nil)
	session := &kiro.DeviceSession{
		ID:          "session-1",
		Type:        kiro.AuthMethodSocial,
		State:       "state-1",
		RedirectURI: "http://127.0.0.1:49153/oauth/callback",
		Status:      kiro.DeviceSessionPending,
		ExpiresAt:   time.Now().Add(time.Minute),
	}
	svc.sessionStore.Set(session)

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:49153/oauth/callback?error=access_denied&error_description=Denied&state=state-1", nil)
	rr := httptest.NewRecorder()

	svc.handleSocialCallback(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	updated, ok := svc.sessionStore.Get(session.ID)
	if !ok {
		t.Fatal("session missing")
	}
	if updated.Status != kiro.DeviceSessionError {
		t.Fatalf("session status = %q, want %q", updated.Status, kiro.DeviceSessionError)
	}
	if !strings.Contains(updated.Error, "access_denied") {
		t.Fatalf("session error = %q, want access_denied", updated.Error)
	}
}

func TestKiroOAuthCallbackListenAddrHonorsDockerBindHost(t *testing.T) {
	t.Setenv(kiroSocialCallbackListenHostEnv, "0.0.0.0")

	addr, err := callbackListenAddr("http://127.0.0.1:49153/oauth/callback")
	if err != nil {
		t.Fatalf("callbackListenAddr() error = %v", err)
	}
	if addr != "0.0.0.0:49153" {
		t.Fatalf("addr = %q, want 0.0.0.0:49153", addr)
	}
}
