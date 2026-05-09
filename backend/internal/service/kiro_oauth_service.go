package service

import (
	"context"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const kiroSocialCallbackListenHostEnv = "KIRO_SOCIAL_CALLBACK_LISTEN_HOST"

type KiroOAuthService struct {
	sessionStore *kiro.DeviceSessionStore
	proxyRepo    ProxyRepository

	callbackMu       sync.Mutex
	callbackServer   *http.Server
	callbackListener net.Listener
	callbackAddr     string
}

func NewKiroOAuthService(proxyRepo ProxyRepository) *KiroOAuthService {
	return &KiroOAuthService{
		sessionStore: kiro.NewDeviceSessionStore(),
		proxyRepo:    proxyRepo,
	}
}

type KiroDeviceAuthInput struct {
	AuthType string
	Region   string
	StartURL string
	ProxyID  *int64
}

type KiroSocialAuthInput struct {
	Provider    string
	Region      string
	RedirectURI string
	ProxyID     *int64
}

type KiroCompleteSocialInput struct {
	SessionID      string
	CallbackOrCode string
}

type KiroDeviceAuthResult struct {
	SessionID       string `json:"session_id"`
	AuthURL         string `json:"auth_url"`
	UserCode        string `json:"user_code"`
	DeviceCode      string `json:"device_code"`
	ExpiresAt       string `json:"expires_at"`
	IntervalSeconds int    `json:"interval_seconds"`
	AuthMethod      string `json:"auth_method"`
	Region          string `json:"region"`
	StartURL        string `json:"start_url,omitempty"`
}

type KiroSessionStatusResult struct {
	SessionID   string         `json:"session_id"`
	Status      string         `json:"status"`
	Error       string         `json:"error,omitempty"`
	UserCode    string         `json:"user_code,omitempty"`
	AuthURL     string         `json:"auth_url,omitempty"`
	Credentials map[string]any `json:"credentials,omitempty"`
}

type KiroScanTokensResult struct {
	Tokens []kiro.DetectedToken `json:"tokens"`
	Errors []string             `json:"errors,omitempty"`
}

func (s *KiroOAuthService) StartDeviceAuth(ctx context.Context, input KiroDeviceAuthInput) (*KiroDeviceAuthResult, error) {
	proxyURL, err := s.proxyURL(ctx, input.ProxyID)
	if err != nil {
		return nil, err
	}
	session, err := kiro.StartDeviceAuthorization(ctx, input.AuthType, input.Region, input.StartURL, proxyURL)
	if err != nil {
		return nil, err
	}
	s.sessionStore.Set(session)
	return &KiroDeviceAuthResult{
		SessionID:       session.ID,
		AuthURL:         firstNonEmpty(session.VerificationURIComplete, session.VerificationURI),
		UserCode:        session.UserCode,
		DeviceCode:      session.DeviceCode,
		ExpiresAt:       session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		IntervalSeconds: int(session.Interval.Seconds()),
		AuthMethod:      session.Type,
		Region:          session.Region,
		StartURL:        session.StartURL,
	}, nil
}

func (s *KiroOAuthService) StartSocialAuth(ctx context.Context, input KiroSocialAuthInput) (*KiroDeviceAuthResult, error) {
	proxyURL, err := s.proxyURL(ctx, input.ProxyID)
	if err != nil {
		return nil, err
	}
	session, err := kiro.StartSocialAuthorization(ctx, input.Provider, input.Region, input.RedirectURI, proxyURL)
	if err != nil {
		return nil, err
	}
	s.sessionStore.Set(session)
	if isLoopbackCallback(session.RedirectURI) {
		if err := s.ensureSocialCallbackServer(session.RedirectURI); err != nil {
			logger.LegacyPrintf("service.kiro_oauth", "Kiro social callback listener unavailable for %s: %v; manual callback paste remains available", session.RedirectURI, err)
		}
	}
	return &KiroDeviceAuthResult{
		SessionID:       session.ID,
		AuthURL:         session.VerificationURI,
		ExpiresAt:       session.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z"),
		IntervalSeconds: int(session.Interval.Seconds()),
		AuthMethod:      session.Type,
		Region:          session.Region,
		StartURL:        session.StartURL,
	}, nil
}

func (s *KiroOAuthService) CompleteSocialAuth(ctx context.Context, input KiroCompleteSocialInput) (*KiroSessionStatusResult, error) {
	sessionID := strings.TrimSpace(input.SessionID)
	session, ok := s.sessionStore.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session 不存在或已过期")
	}
	if session.Type != kiro.AuthMethodSocial {
		return nil, fmt.Errorf("session 不是 Kiro Social Auth")
	}
	creds, status, err := kiro.CompleteSocialAuthorization(ctx, session, input.CallbackOrCode)
	s.sessionStore.Update(sessionID, func(current *kiro.DeviceSession) {
		current.Status = status
		if err != nil && status != kiro.DeviceSessionPending {
			current.Error = err.Error()
		}
		if creds != nil {
			current.Credentials = creds
		}
	})
	return s.GetSessionStatus(ctx, sessionID)
}

func (s *KiroOAuthService) handleSocialCallback(w http.ResponseWriter, r *http.Request) {
	if r.URL == nil || r.URL.Path != "/oauth/callback" {
		http.NotFound(w, r)
		return
	}

	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		s.writeSocialCallbackPage(w, http.StatusBadRequest, "Kiro authorization failed", "The callback did not include a state value. Return to Sub2API and paste the callback URL manually.")
		return
	}

	session, ok := s.sessionStore.FindByState(state)
	if !ok || session.Type != kiro.AuthMethodSocial {
		s.writeSocialCallbackPage(w, http.StatusBadRequest, "Kiro authorization failed", "This authorization session was not found or has expired. Start Kiro social login again in Sub2API.")
		return
	}

	creds, status, err := kiro.CompleteSocialAuthorization(r.Context(), session, callbackURLForRequest(r))
	s.sessionStore.Update(session.ID, func(current *kiro.DeviceSession) {
		current.Status = status
		if err != nil && status != kiro.DeviceSessionPending {
			current.Error = err.Error()
		}
		if creds != nil {
			current.Credentials = creds
		}
	})
	if err != nil {
		s.writeSocialCallbackPage(w, http.StatusBadRequest, "Kiro authorization failed", err.Error())
		return
	}
	s.writeSocialCallbackPage(w, http.StatusOK, "Kiro authorization complete", "You can close this tab and return to Sub2API. The account will be created automatically.")
}

func (s *KiroOAuthService) GetSessionStatus(ctx context.Context, sessionID string) (*KiroSessionStatusResult, error) {
	sessionID = strings.TrimSpace(sessionID)
	session, ok := s.sessionStore.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session 不存在或已过期")
	}

	if session.Status == kiro.DeviceSessionPending && session.Type != kiro.AuthMethodSocial {
		creds, status, err := kiro.PollDeviceToken(ctx, session)
		s.sessionStore.Update(sessionID, func(current *kiro.DeviceSession) {
			current.Status = status
			if err != nil && status != kiro.DeviceSessionPending {
				current.Error = err.Error()
			}
			if creds != nil {
				current.Credentials = creds
			}
		})
		session, _ = s.sessionStore.Get(sessionID)
	}

	result := &KiroSessionStatusResult{
		SessionID: session.ID,
		Status:    string(session.Status),
		Error:     session.Error,
		UserCode:  session.UserCode,
		AuthURL:   firstNonEmpty(session.VerificationURIComplete, session.VerificationURI),
	}
	if session.Status == kiro.DeviceSessionComplete && session.Credentials != nil {
		result.Credentials = s.BuildAccountCredentials(*session.Credentials)
	}
	return result, nil
}

func (s *KiroOAuthService) CancelSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id 不能为空")
	}
	ok := s.sessionStore.Update(sessionID, func(current *kiro.DeviceSession) {
		current.Status = kiro.DeviceSessionError
		current.Error = "cancelled"
	})
	if !ok {
		return fmt.Errorf("session 不存在或已过期")
	}
	return nil
}

func (s *KiroOAuthService) ScanTokens() KiroScanTokensResult {
	tokens, errs := kiro.ScanLocalTokens()
	return KiroScanTokensResult{Tokens: tokens, Errors: errs}
}

func (s *KiroOAuthService) BuildAccountCredentials(creds kiro.Credentials) map[string]any {
	out := map[string]any{
		"access_token":  creds.AccessToken,
		"refresh_token": creds.RefreshToken,
		"auth_method":   creds.AuthMethod,
		"region":        firstNonEmpty(creds.Region, kiro.DefaultRegion),
	}
	if strings.TrimSpace(creds.ExpiresAt) != "" {
		out["expires_at"] = creds.ExpiresAt
	}
	if strings.TrimSpace(creds.ProfileARN) != "" {
		out["profile_arn"] = creds.ProfileARN
	}
	if strings.TrimSpace(creds.ClientID) != "" {
		out["client_id"] = creds.ClientID
	}
	if strings.TrimSpace(creds.ClientSecret) != "" {
		out["client_secret"] = creds.ClientSecret
	}
	if creds.ClientSecretExpiresAt > 0 {
		out["client_secret_expires_at"] = creds.ClientSecretExpiresAt
	}
	if strings.TrimSpace(creds.Provider) != "" {
		out["provider"] = creds.Provider
	}
	if strings.TrimSpace(creds.StartURL) != "" {
		out["start_url"] = creds.StartURL
	}
	if strings.TrimSpace(creds.SSORegion) != "" {
		out["sso_region"] = creds.SSORegion
	}
	return out
}

func (s *KiroOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		return "", err
	}
	if proxy == nil {
		return "", nil
	}
	return proxy.URL(), nil
}

func (s *KiroOAuthService) ensureSocialCallbackServer(redirectURI string) error {
	addr, err := callbackListenAddr(redirectURI)
	if err != nil {
		return err
	}

	s.callbackMu.Lock()
	defer s.callbackMu.Unlock()
	if s.callbackServer != nil && s.callbackAddr == addr {
		return nil
	}
	if s.callbackServer != nil {
		return fmt.Errorf("kiro social callback listener is already running on %s", s.callbackAddr)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/callback", s.handleSocialCallback)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.callbackServer = server
	s.callbackListener = listener
	s.callbackAddr = addr
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.LegacyPrintf("service.kiro_oauth", "Kiro social callback listener stopped: %v", err)
			s.callbackMu.Lock()
			if s.callbackServer == server {
				s.callbackServer = nil
				s.callbackListener = nil
				s.callbackAddr = ""
			}
			s.callbackMu.Unlock()
		}
	}()
	logger.LegacyPrintf("service.kiro_oauth", "Kiro social callback listener started on %s", addr)
	return nil
}

func (s *KiroOAuthService) writeSocialCallbackPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	color := "#16a34a"
	if status >= 400 {
		color = "#dc2626"
	}
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>%s</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
</head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;margin:0;min-height:100vh;display:grid;place-items:center;background:#f8fafc;color:#111827;">
  <main style="max-width:560px;padding:32px;text-align:center;">
    <h1 style="color:%s;font-size:28px;margin:0 0 12px;">%s</h1>
    <p style="font-size:16px;line-height:1.6;margin:0;">%s</p>
    <script>setTimeout(function(){ window.close(); }, 2500);</script>
  </main>
</body>
</html>`, html.EscapeString(title), color, html.EscapeString(title), html.EscapeString(message))
}

func callbackURLForRequest(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return scheme + "://" + host + r.URL.RequestURI()
}

func callbackListenAddr(raw string) (string, error) {
	host, port, err := callbackHostPort(raw)
	if err != nil {
		return "", err
	}
	if bindHost := strings.TrimSpace(os.Getenv(kiroSocialCallbackListenHostEnv)); bindHost != "" {
		host = bindHost
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

func callbackHostPort(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	if u == nil || u.Scheme == "" || u.Host == "" {
		return "", "", fmt.Errorf("invalid callback URI")
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		if strings.Contains(err.Error(), "missing port in address") {
			host = u.Hostname()
			port = u.Port()
		} else {
			return "", "", err
		}
	}
	if port == "" {
		return "", "", fmt.Errorf("callback URI must include a port")
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if host == "localhost" {
		host = "127.0.0.1"
	}
	return host, port, nil
}

func isLoopbackCallback(raw string) bool {
	host, _, err := callbackHostPort(raw)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
