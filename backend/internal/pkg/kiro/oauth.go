package kiro

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	ssooidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
	"github.com/google/uuid"
)

const (
	DeviceGrantType  = "urn:ietf:params:oauth:grant-type:device_code"
	RefreshGrantType = "refresh_token"
	deviceSessionTTL = 15 * time.Minute
)

type DeviceSessionStatus string

const (
	DeviceSessionPending  DeviceSessionStatus = "pending"
	DeviceSessionComplete DeviceSessionStatus = "completed"
	DeviceSessionError    DeviceSessionStatus = "error"
	DeviceSessionExpired  DeviceSessionStatus = "expired"
)

type DeviceSession struct {
	ID                      string
	Type                    string
	Provider                string
	Region                  string
	StartURL                string
	ClientID                string
	ClientSecret            string
	ClientSecretExpiresAt   int64
	CodeVerifier            string
	RedirectURI             string
	State                   string
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	Interval                time.Duration
	ExpiresAt               time.Time
	CreatedAt               time.Time
	Status                  DeviceSessionStatus
	Error                   string
	Credentials             *Credentials
	ProxyURL                string
}

type DeviceSessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*DeviceSession
}

func NewDeviceSessionStore() *DeviceSessionStore {
	return &DeviceSessionStore{sessions: make(map[string]*DeviceSession)}
}

func (s *DeviceSessionStore) Set(session *DeviceSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(time.Now())
	s.sessions[session.ID] = session
}

func (s *DeviceSessionStore) Get(id string) (*DeviceSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	return cloneDeviceSession(session), true
}

func (s *DeviceSessionStore) FindByState(state string) (*DeviceSession, bool) {
	state = strings.TrimSpace(state)
	if state == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, session := range s.sessions {
		if session.State == state {
			return cloneDeviceSession(session), true
		}
	}
	return nil, false
}

func (s *DeviceSessionStore) Update(id string, fn func(*DeviceSession)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return false
	}
	fn(session)
	return true
}

func (s *DeviceSessionStore) cleanupLocked(now time.Time) {
	for id, session := range s.sessions {
		if now.Sub(session.CreatedAt) > deviceSessionTTL {
			delete(s.sessions, id)
		}
	}
}

func cloneDeviceSession(session *DeviceSession) *DeviceSession {
	copy := *session
	if session.Credentials != nil {
		creds := *session.Credentials
		copy.Credentials = &creds
	}
	return &copy
}

func newOIDCHTTPClient(proxyURL string) (*http.Client, error) {
	return httpclient.GetClient(httpclient.Options{
		ProxyURL:              proxyURL,
		Timeout:               30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	})
}

func oidcBaseURL(region string) string {
	if strings.TrimSpace(region) == "" {
		region = DefaultRegion
	}
	return fmt.Sprintf("https://oidc.%s.amazonaws.com", region)
}

func StartDeviceAuthorization(ctx context.Context, authType, region, startURL, proxyURL string) (*DeviceSession, error) {
	authType = normalizeDeviceAuthType(authType)
	if strings.TrimSpace(region) == "" {
		region = DefaultRegion
	}
	if authType == AuthMethodBuilderID {
		startURL = BuilderIDStartURL
	} else {
		if err := validateStartURL(startURL); err != nil {
			return nil, err
		}
	}

	client, err := newOIDCHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}

	reg, err := registerOIDCClient(ctx, client, authType, region, startURL)
	if err != nil {
		return nil, err
	}
	if reg.ClientID == "" || reg.ClientSecret == "" {
		return nil, errors.New("kiro oidc client registration returned empty client credentials")
	}

	auth, err := startOIDCDeviceAuthorization(ctx, client, region, reg.ClientID, reg.ClientSecret, startURL)
	if err != nil {
		return nil, err
	}
	if auth.DeviceCode == "" || auth.UserCode == "" || auth.VerificationURI == "" {
		return nil, errors.New("kiro device authorization returned incomplete response")
	}

	expiresIn := auth.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 600
	}
	interval := auth.Interval
	if interval <= 0 {
		interval = 5
	}
	complete := ""
	if auth.VerificationURIComplete != "" {
		complete = auth.VerificationURIComplete
	}
	return &DeviceSession{
		ID:                      uuid.NewString(),
		Type:                    authType,
		Region:                  region,
		StartURL:                startURL,
		ClientID:                reg.ClientID,
		ClientSecret:            reg.ClientSecret,
		ClientSecretExpiresAt:   reg.ClientSecretExpiresAt,
		DeviceCode:              auth.DeviceCode,
		UserCode:                auth.UserCode,
		VerificationURI:         auth.VerificationURI,
		VerificationURIComplete: complete,
		Interval:                time.Duration(interval) * time.Second,
		ExpiresAt:               time.Now().Add(time.Duration(expiresIn) * time.Second),
		CreatedAt:               time.Now(),
		Status:                  DeviceSessionPending,
		ProxyURL:                proxyURL,
	}, nil
}

func StartSocialAuthorization(ctx context.Context, provider, region, redirectURI, proxyURL string) (*DeviceSession, error) {
	provider = normalizeSocialProvider(provider)
	if strings.TrimSpace(region) == "" {
		region = DefaultRegion
	}
	redirectURI = strings.TrimSpace(redirectURI)
	if err := validateRedirectURI(redirectURI); err != nil {
		return nil, err
	}
	codeVerifier, err := randomBase64URL(32)
	if err != nil {
		return nil, err
	}
	state, err := randomBase64URL(16)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(h[:])
	authURL := fmt.Sprintf("%s/login?idp=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s&prompt=select_account",
		fmt.Sprintf(AuthServiceEndpointTemplate, region),
		url.QueryEscape(provider),
		url.QueryEscape(redirectURI),
		url.QueryEscape(codeChallenge),
		url.QueryEscape(state),
	)
	return &DeviceSession{
		ID:              uuid.NewString(),
		Type:            AuthMethodSocial,
		Provider:        provider,
		Region:          region,
		StartURL:        BuilderIDStartURL,
		CodeVerifier:    codeVerifier,
		RedirectURI:     redirectURI,
		State:           state,
		VerificationURI: authURL,
		CreatedAt:       time.Now(),
		ExpiresAt:       time.Now().Add(10 * time.Minute),
		Status:          DeviceSessionPending,
		ProxyURL:        proxyURL,
	}, nil
}

func CompleteSocialAuthorization(ctx context.Context, session *DeviceSession, callbackOrCode string) (*Credentials, DeviceSessionStatus, error) {
	if session == nil {
		return nil, DeviceSessionError, errors.New("kiro social session is nil")
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, DeviceSessionExpired, errors.New("kiro social authorization expired")
	}
	code, state, err := parseCallbackCode(callbackOrCode)
	if err != nil {
		return nil, DeviceSessionError, err
	}
	if state != "" && session.State != "" && state != session.State {
		return nil, DeviceSessionError, errors.New("kiro social authorization state mismatch")
	}
	client, err := newOIDCHTTPClient(session.ProxyURL)
	if err != nil {
		return nil, DeviceSessionError, err
	}
	body, err := postOIDCJSON(ctx, client, fmt.Sprintf(AuthServiceEndpointTemplate, session.Region)+"/oauth/token", map[string]string{
		"code":          code,
		"code_verifier": session.CodeVerifier,
		"redirect_uri":  session.RedirectURI,
	})
	if err != nil {
		return nil, DeviceSessionError, err
	}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileARN   string `json:"profileArn"`
		ExpiresAt    string `json:"expiresAt"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, DeviceSessionError, fmt.Errorf("parse kiro social token response: %w", err)
	}
	if strings.TrimSpace(out.AccessToken) == "" && strings.TrimSpace(out.RefreshToken) == "" {
		return nil, DeviceSessionError, errors.New("kiro social token response missing token")
	}
	expiresAt := out.ExpiresAt
	if expiresAt == "" && out.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return &Credentials{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ProfileARN:   out.ProfileARN,
		ExpiresAt:    expiresAt,
		AuthMethod:   AuthMethodSocial,
		Provider:     session.Provider,
		Region:       session.Region,
		StartURL:     BuilderIDStartURL,
		SSORegion:    session.Region,
	}, DeviceSessionComplete, nil
}

func PollDeviceToken(ctx context.Context, session *DeviceSession) (*Credentials, DeviceSessionStatus, error) {
	if session == nil {
		return nil, DeviceSessionError, errors.New("kiro device session is nil")
	}
	if time.Now().After(session.ExpiresAt) {
		return nil, DeviceSessionExpired, errors.New("kiro device authorization expired")
	}
	client, err := newOIDCHTTPClient(session.ProxyURL)
	if err != nil {
		return nil, DeviceSessionError, err
	}
	token, status, err := pollOIDCDeviceToken(ctx, client, session)
	if err != nil {
		return nil, status, err
	}
	if status == DeviceSessionPending {
		return nil, DeviceSessionPending, nil
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, DeviceSessionError, errors.New("kiro device token response missing access token")
	}
	expiresIn := token.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	creds := &Credentials{
		AccessToken:           token.AccessToken,
		RefreshToken:          token.RefreshToken,
		ClientID:              session.ClientID,
		ClientSecret:          session.ClientSecret,
		ClientSecretExpiresAt: session.ClientSecretExpiresAt,
		AuthMethod:            session.Type,
		ExpiresAt:             time.Now().Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339),
		Region:                session.Region,
		StartURL:              session.StartURL,
		SSORegion:             session.Region,
	}
	return creds, DeviceSessionComplete, nil
}

type DetectedToken struct {
	Source                     string      `json:"source"`
	FilePath                   string      `json:"file_path"`
	FileName                   string      `json:"file_name"`
	IsExpired                  bool        `json:"is_expired"`
	IsUsable                   bool        `json:"is_usable"`
	HasClientCredentials       bool        `json:"has_client_credentials"`
	ClientCredentialsSource    string      `json:"client_credentials_source,omitempty"`
	ClientCredentialsExpiresAt any         `json:"client_credentials_expires_at,omitempty"`
	Credentials                Credentials `json:"credentials"`
}

func ScanLocalTokens() ([]DetectedToken, []string) {
	var tokens []DetectedToken
	var errs []string
	for _, root := range tokenSearchRoots() {
		found, err := scanTokenDirectory(root.source, root.path, 3)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", root.path, err))
			continue
		}
		tokens = append(tokens, found...)
	}
	return tokens, errs
}

type tokenRoot struct {
	source string
	path   string
}

func tokenSearchRoots() []tokenRoot {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	roots := []tokenRoot{
		{source: "AWS SSO", path: filepath.Join(home, ".aws", "sso", "cache")},
		{source: "AWS SSO", path: filepath.Join(home, ".aws", "cli", "cache")},
	}
	switch {
	case fileExists(filepath.Join(home, "Library")):
		roots = append(roots,
			tokenRoot{source: "Kiro IDE", path: filepath.Join(home, "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent")},
			tokenRoot{source: "Kiro CLI", path: filepath.Join(home, "Library", "Application Support", "kiro-cli")},
		)
	case fileExists(filepath.Join(home, "AppData")):
		roots = append(roots, tokenRoot{source: "Kiro IDE", path: filepath.Join(home, "AppData", "Roaming", "Kiro", "User", "globalStorage", "kiro.kiroagent")})
	default:
		roots = append(roots, tokenRoot{source: "Kiro", path: filepath.Join(home, ".kiro")})
	}
	return roots
}

func scanTokenDirectory(source, dir string, depth int) ([]DetectedToken, error) {
	if depth < 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []DetectedToken
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			nested, _ := scanTokenDirectory(source, full, depth-1)
			out = append(out, nested...)
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 1024*1024 {
			continue
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			continue
		}
		creds := CredentialsFromMap(data)
		if creds.AccessToken == "" && creds.RefreshToken == "" {
			continue
		}
		clientIDHash := stringFromAny(data["clientIdHash"])
		hasClientCreds := true
		clientCredSource := ""
		var clientCredExpires any
		if clientIDHash != "" && (creds.AuthMethod == "" || creds.AuthMethod == AuthMethodBuilderID || creds.AuthMethod == AuthMethodIdentityCenter) {
			clientCreds, credSource, expires, ok := readClientCredentials(filepath.Dir(full), clientIDHash)
			hasClientCreds = ok
			if ok {
				if creds.ClientID == "" {
					creds.ClientID = clientCreds.ClientID
				}
				if creds.ClientSecret == "" {
					creds.ClientSecret = clientCreds.ClientSecret
				}
				if creds.ClientSecretExpiresAt == 0 {
					creds.ClientSecretExpiresAt = clientCreds.ClientSecretExpiresAt
				}
				clientCredSource = credSource
				clientCredExpires = expires
			}
		}
		if creds.AuthMethod == "" {
			if creds.ClientID != "" && creds.ClientSecret != "" {
				creds.AuthMethod = AuthMethodBuilderID
			} else {
				creds.AuthMethod = AuthMethodSocial
			}
		}
		if creds.Region == "" {
			creds.Region = DefaultRegion
		}
		expired := isTokenExpired(creds.ExpiresAt)
		usable := !expired || (creds.RefreshToken != "" && (creds.AuthMethod == AuthMethodSocial || hasClientCreds))
		out = append(out, DetectedToken{
			Source:                     source,
			FilePath:                   full,
			FileName:                   entry.Name(),
			IsExpired:                  expired,
			IsUsable:                   usable,
			HasClientCredentials:       hasClientCreds,
			ClientCredentialsSource:    clientCredSource,
			ClientCredentialsExpiresAt: clientCredExpires,
			Credentials:                creds,
		})
	}
	return out, nil
}

func CredentialsFromMap(data map[string]any) Credentials {
	return Credentials{
		UUID:                  firstAnyString(data, "uuid"),
		AccessToken:           firstAnyString(data, "accessToken", "access_token"),
		RefreshToken:          firstAnyString(data, "refreshToken", "refresh_token"),
		ClientID:              firstAnyString(data, "clientId", "client_id"),
		ClientSecret:          firstAnyString(data, "clientSecret", "client_secret"),
		ClientSecretExpiresAt: firstAnyInt64(data, "clientSecretExpiresAt", "client_secret_expires_at", "clientCredentialsExpiresAt"),
		AuthMethod:            normalizeCredentialAuthMethod(firstAnyString(data, "authMethod", "auth_method")),
		ExpiresAt:             firstAnyString(data, "expiresAt", "expires_at", "expiration"),
		ProfileARN:            firstAnyString(data, "profileArn", "profile_arn"),
		Region:                firstNonEmpty(firstAnyString(data, "region"), firstAnyString(data, "ssoRegion", "sso_region")),
		Provider:              firstAnyString(data, "provider"),
		StartURL:              firstAnyString(data, "startUrl", "start_url"),
		SSORegion:             firstAnyString(data, "ssoRegion", "sso_region"),
	}
}

type oidcClientRegistration struct {
	ClientID              string
	ClientSecret          string
	ClientSecretExpiresAt int64
}

type oidcDeviceAuthorization struct {
	DeviceCode              string
	UserCode                string
	VerificationURI         string
	VerificationURIComplete string
	ExpiresIn               int32
	Interval                int32
}

type oidcTokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int32
}

func registerOIDCClient(ctx context.Context, client *http.Client, authType, region, startURL string) (oidcClientRegistration, error) {
	out, err := newSSOOIDCClient(region, client).RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: aws.String("KiroIDE"),
		ClientType: aws.String("public"),
		Scopes:     OAuthScopes,
		GrantTypes: []string{DeviceGrantType, RefreshGrantType},
	})
	if err != nil {
		return oidcClientRegistration{}, err
	}
	return oidcClientRegistration{
		ClientID:              aws.ToString(out.ClientId),
		ClientSecret:          aws.ToString(out.ClientSecret),
		ClientSecretExpiresAt: out.ClientSecretExpiresAt,
	}, nil
}

func startOIDCDeviceAuthorization(ctx context.Context, client *http.Client, region, clientID, clientSecret, startURL string) (oidcDeviceAuthorization, error) {
	out, err := newSSOOIDCClient(region, client).StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     aws.String(clientID),
		ClientSecret: aws.String(clientSecret),
		StartUrl:     aws.String(startURL),
	})
	if err != nil {
		return oidcDeviceAuthorization{}, err
	}
	return oidcDeviceAuthorization{
		DeviceCode:              aws.ToString(out.DeviceCode),
		UserCode:                aws.ToString(out.UserCode),
		VerificationURI:         aws.ToString(out.VerificationUri),
		VerificationURIComplete: aws.ToString(out.VerificationUriComplete),
		ExpiresIn:               out.ExpiresIn,
		Interval:                out.Interval,
	}, nil
}

func pollOIDCDeviceToken(ctx context.Context, client *http.Client, session *DeviceSession) (oidcTokenResponse, DeviceSessionStatus, error) {
	out, err := newSSOOIDCClient(session.Region, client).CreateToken(ctx, &ssooidc.CreateTokenInput{
		ClientId:     aws.String(session.ClientID),
		ClientSecret: aws.String(session.ClientSecret),
		DeviceCode:   aws.String(session.DeviceCode),
		GrantType:    aws.String(DeviceGrantType),
	})
	if err != nil {
		var pending *ssooidctypes.AuthorizationPendingException
		if errors.As(err, &pending) {
			return oidcTokenResponse{}, DeviceSessionPending, nil
		}
		var slowDown *ssooidctypes.SlowDownException
		if errors.As(err, &slowDown) {
			session.Interval += 5 * time.Second
			return oidcTokenResponse{}, DeviceSessionPending, nil
		}
		var expired *ssooidctypes.ExpiredTokenException
		if errors.As(err, &expired) {
			return oidcTokenResponse{}, DeviceSessionExpired, fmt.Errorf("kiro device authorization expired")
		}
		var denied *ssooidctypes.AccessDeniedException
		if errors.As(err, &denied) {
			return oidcTokenResponse{}, DeviceSessionError, fmt.Errorf("kiro device authorization denied")
		}
		return oidcTokenResponse{}, DeviceSessionError, fmt.Errorf("kiro device token request failed: %w", err)
	}
	return oidcTokenResponse{
		AccessToken:  aws.ToString(out.AccessToken),
		RefreshToken: aws.ToString(out.RefreshToken),
		ExpiresIn:    out.ExpiresIn,
	}, DeviceSessionComplete, nil
}

func newSSOOIDCClient(region string, client *http.Client) *ssooidc.Client {
	if strings.TrimSpace(region) == "" {
		region = DefaultRegion
	}
	return ssooidc.NewFromConfig(aws.Config{
		Region:     region,
		HTTPClient: client,
	})
}

func postOIDCJSON(ctx context.Context, client *http.Client, endpoint string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("kiro oidc request returned %d: %s", resp.StatusCode, truncate(body, 512))
	}
	return body, nil
}

func normalizeDeviceAuthType(authType string) string {
	switch strings.ToLower(strings.TrimSpace(authType)) {
	case "idc", "identity-center", "identity_center":
		return AuthMethodIdentityCenter
	default:
		return AuthMethodBuilderID
	}
}

func normalizeSocialProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "github":
		return "Github"
	case "cognito":
		return "Cognito"
	default:
		return "Google"
	}
}

func normalizeCredentialAuthMethod(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "idc", "identity-center", "identity_center":
		return AuthMethodIdentityCenter
	case "builder-id", "builder_id", "oidc":
		return AuthMethodBuilderID
	case "social":
		return AuthMethodSocial
	default:
		return strings.TrimSpace(method)
	}
}

func validateRedirectURI(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid redirect URI")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("redirect URI must be http or https")
	}
	return nil
}

func validateStartURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("invalid IAM Identity Center start URL")
	}
	if !strings.Contains(u.Path, "/start") {
		return fmt.Errorf("invalid IAM Identity Center start URL: path must include /start")
	}
	return nil
}

func randomBase64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func parseCallbackCode(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("authorization code is required")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", err
		}
		if callbackErr := strings.TrimSpace(u.Query().Get("error")); callbackErr != "" {
			desc := strings.TrimSpace(u.Query().Get("error_description"))
			if desc != "" {
				return "", "", fmt.Errorf("authorization callback returned %s: %s", callbackErr, desc)
			}
			return "", "", fmt.Errorf("authorization callback returned %s", callbackErr)
		}
		code := strings.TrimSpace(u.Query().Get("code"))
		if code == "" {
			return "", "", fmt.Errorf("callback URL missing code")
		}
		return code, strings.TrimSpace(u.Query().Get("state")), nil
	}
	return raw, "", nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readClientCredentials(dirPath, clientIDHash string) (Credentials, string, any, bool) {
	path := filepath.Join(dirPath, clientIDHash+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, "", nil, false
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return Credentials{}, "", nil, false
	}
	creds := CredentialsFromMap(data)
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return Credentials{}, "", nil, false
	}
	expires := data["expiresAt"]
	if expires == nil {
		expires = data["clientSecretExpiresAt"]
	}
	return creds, path, expires, true
}

func isTokenExpired(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return time.Now().After(t)
	}
	return false
}

func firstAnyString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := stringFromAny(data[key]); v != "" {
			return v
		}
	}
	return ""
}

func firstAnyInt64(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := data[key].(type) {
		case int64:
			if v != 0 {
				return v
			}
		case int:
			if v != 0 {
				return int64(v)
			}
		case float64:
			if v != 0 {
				return int64(v)
			}
		case json.Number:
			if n, err := v.Int64(); err == nil && n != 0 {
				return n
			}
		case string:
			if parsed, err := time.Parse(time.RFC3339, v); err == nil {
				return parsed.Unix()
			}
		}
	}
	return 0
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return ""
	}
}
