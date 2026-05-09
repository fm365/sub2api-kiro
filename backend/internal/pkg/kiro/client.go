package kiro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Client struct {
	httpClient *http.Client
	creds      Credentials
}

type RefreshResult struct {
	Credentials Credentials
	Refreshed   bool
}

func NewClient(creds Credentials, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if strings.TrimSpace(creds.Region) == "" {
		creds.Region = DefaultRegion
	}
	if strings.TrimSpace(creds.AuthMethod) == "" {
		creds.AuthMethod = AuthMethodSocial
	}
	return &Client{httpClient: httpClient, creds: creds}
}

func (c *Client) Credentials() Credentials {
	return c.creds
}

func (c *Client) EnsureAccessToken(ctx context.Context) (RefreshResult, error) {
	if strings.TrimSpace(c.creds.AccessToken) != "" && !isExpiryNear(c.creds.ExpiresAt, 10*time.Minute) {
		return RefreshResult{Credentials: c.creds}, nil
	}
	if strings.TrimSpace(c.creds.RefreshToken) == "" {
		if strings.TrimSpace(c.creds.AccessToken) == "" {
			return RefreshResult{}, errors.New("kiro access_token not found in credentials")
		}
		return RefreshResult{Credentials: c.creds}, nil
	}
	if c.creds.AuthMethod == AuthMethodSocial {
		return c.refreshSocial(ctx)
	}
	return c.refreshWithSSOOIDC(ctx)
}

func (c *Client) ForceRefreshAccessToken(ctx context.Context) (RefreshResult, error) {
	if strings.TrimSpace(c.creds.RefreshToken) == "" {
		return RefreshResult{}, errors.New("kiro refresh_token not found in credentials")
	}
	if c.creds.AuthMethod == AuthMethodSocial {
		return c.refreshSocial(ctx)
	}
	return c.refreshWithSSOOIDC(ctx)
}

func (c *Client) refreshSocial(ctx context.Context) (RefreshResult, error) {
	body, _ := json.Marshal(map[string]string{"refreshToken": c.creds.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf(RefreshURLTemplate, c.creds.Region), bytes.NewReader(body))
	if err != nil {
		return RefreshResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RefreshResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		return RefreshResult{}, fmt.Errorf("kiro social token refresh failed: status=%d body=%s", resp.StatusCode, truncate(respBody, 512))
	}

	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ProfileARN   string `json:"profileArn"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return RefreshResult{}, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return RefreshResult{}, errors.New("kiro social token refresh returned empty accessToken")
	}
	c.creds.AccessToken = out.AccessToken
	if out.RefreshToken != "" {
		c.creds.RefreshToken = out.RefreshToken
	}
	if out.ProfileARN != "" {
		c.creds.ProfileARN = out.ProfileARN
	}
	if out.ExpiresIn > 0 {
		c.creds.ExpiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return RefreshResult{Credentials: c.creds, Refreshed: true}, nil
}

func (c *Client) refreshWithSSOOIDC(ctx context.Context) (RefreshResult, error) {
	if c.creds.ClientSecretExpiresAt > 0 && time.Unix(c.creds.ClientSecretExpiresAt, 0).Before(time.Now()) {
		return RefreshResult{}, errors.New("kiro oidc client credentials expired; re-authenticate the account")
	}
	if c.creds.ClientID == "" || c.creds.ClientSecret == "" {
		return RefreshResult{}, errors.New("kiro client_id and client_secret are required for oidc refresh")
	}
	body, _ := json.Marshal(map[string]string{
		"grantType":    "refresh_token",
		"clientId":     c.creds.ClientID,
		"clientSecret": c.creds.ClientSecret,
		"refreshToken": c.creds.RefreshToken,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("https://oidc.%s.amazonaws.com/token", c.creds.Region), bytes.NewReader(body))
	if err != nil {
		return RefreshResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return RefreshResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		return RefreshResult{}, fmt.Errorf("kiro oidc token refresh failed: status=%d body=%s", resp.StatusCode, truncate(respBody, 512))
	}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int64  `json:"expiresIn"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return RefreshResult{}, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return RefreshResult{}, errors.New("kiro oidc refresh returned empty accessToken")
	}
	c.creds.AccessToken = out.AccessToken
	if out.RefreshToken != "" {
		c.creds.RefreshToken = out.RefreshToken
	}
	expiresIn := int64(3600)
	if out.ExpiresIn > 0 {
		expiresIn = out.ExpiresIn
	}
	c.creds.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
	return RefreshResult{Credentials: c.creds, Refreshed: true}, nil
}

func (c *Client) BuildRequestBody(req Request) (map[string]any, string) {
	conversationID := uuid.NewString()
	modelID := MapModel(req.Model)
	messages := mergeAdjacentMessages(req.Messages)
	history := make([]any, 0, len(messages))
	start := 0

	systemPrompt := contentText(req.System)
	if systemPrompt != "" {
		if len(messages) > 0 && messages[0].Role == "user" {
			first := contentText(messages[0].Content)
			history = append(history, userInputMessage(systemPrompt+"\n\n"+first, modelID))
			start = 1
		} else {
			history = append(history, userInputMessage(systemPrompt, modelID))
		}
	}

	for i := start; i < len(messages)-1; i++ {
		msg := messages[i]
		if msg.Role == "assistant" {
			history = append(history, map[string]any{"assistantResponseMessage": map[string]any{"content": contentText(msg.Content)}})
			continue
		}
		history = append(history, userInputMessage(contentText(msg.Content), modelID))
	}

	currentContent := "Continue"
	if len(messages) > 0 {
		current := messages[len(messages)-1]
		currentContent = contentText(current.Content)
		if current.Role == "assistant" {
			history = append(history, map[string]any{"assistantResponseMessage": map[string]any{"content": currentContent}})
			currentContent = "Continue"
		} else if len(history) > 0 {
			if _, ok := history[len(history)-1].(map[string]any)["assistantResponseMessage"]; !ok {
				history = append(history, map[string]any{"assistantResponseMessage": map[string]any{"content": "Continue"}})
			}
		}
	}
	if strings.TrimSpace(currentContent) == "" {
		currentContent = "Continue"
	}

	state := map[string]any{
		"chatTriggerType": ChatTriggerManual,
		"conversationId":  conversationID,
		"currentMessage": map[string]any{
			"userInputMessage": map[string]any{
				"content": currentContent,
				"modelId": modelID,
				"origin":  OriginAIEditor,
			},
		},
	}
	if len(history) > 0 {
		state["history"] = history
	}
	if len(req.Tools) > 0 {
		state["currentMessage"].(map[string]any)["userInputMessage"].(map[string]any)["userInputMessageContext"] = toolsContext(req.Tools)
	}
	body := map[string]any{"conversationState": state}
	if c.creds.AuthMethod == AuthMethodSocial && c.creds.ProfileARN != "" {
		body["profileArn"] = c.creds.ProfileARN
	}
	return body, modelID
}

func (c *Client) BuildHTTPRequest(ctx context.Context, req Request) (*http.Request, string, error) {
	body, modelID := c.BuildRequestBody(req)
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	url := fmt.Sprintf(CodeWhispererURLTmpl, c.creds.Region)
	if strings.HasPrefix(req.Model, "amazonq") {
		url = fmt.Sprintf(AmazonQURLTmpl, c.creds.Region)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, "", err
	}
	for k, values := range c.headers() {
		for _, v := range values {
			httpReq.Header.Add(k, v)
		}
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	httpReq.Header.Set("amz-sdk-invocation-id", uuid.NewString())
	return httpReq, modelID, nil
}

func (c *Client) GetUsageLimits(ctx context.Context) (*UsageLimits, RefreshResult, error) {
	refresh, err := c.EnsureAccessToken(ctx)
	if err != nil {
		return nil, refresh, err
	}
	if refresh.Refreshed {
		c.creds = refresh.Credentials
	}

	usage, err := c.getUsageLimitsOnce(ctx)
	if err == nil {
		return usage, refresh, nil
	}
	if !strings.Contains(err.Error(), "status=403") {
		return nil, refresh, err
	}

	retryRefresh, refreshErr := c.ForceRefreshAccessToken(ctx)
	if refreshErr != nil {
		return nil, refresh, refreshErr
	}
	usage, retryErr := c.getUsageLimitsOnce(ctx)
	if retryErr != nil {
		return nil, retryRefresh, retryErr
	}
	return usage, retryRefresh, nil
}

func (c *Client) getUsageLimitsOnce(ctx context.Context) (*UsageLimits, error) {
	params := url.Values{}
	params.Set("isEmailRequired", "true")
	params.Set("origin", OriginAIEditor)
	params.Set("resourceType", "AGENTIC_REQUEST")
	if c.creds.AuthMethod == AuthMethodSocial && strings.TrimSpace(c.creds.ProfileARN) != "" {
		params.Set("profileArn", c.creds.ProfileARN)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(UsageLimitsURLTemplate, c.creds.Region)+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	for k, values := range c.headers() {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	req.Header.Set("amz-sdk-invocation-id", uuid.NewString())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("kiro getUsageLimits failed: status=%d body=%s", resp.StatusCode, truncate(body, 512))
	}
	return FormatUsageLimits(body)
}

func (c *Client) headers() http.Header {
	machineID := c.machineID()
	osName := runtime.GOOS + "#" + runtime.GOARCH
	nodeVersion := "20.0.0"
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Accept", "application/json")
	h.Set("amz-sdk-request", "attempt=1; max=1")
	h.Set("x-amzn-kiro-agent-mode", "vibe")
	h.Set("x-amz-user-agent", fmt.Sprintf("aws-sdk-js/1.0.0 KiroIDE-%s-%s", KiroIDEVersion, machineID))
	h.Set("user-agent", fmt.Sprintf("aws-sdk-js/1.0.0 ua/2.1 os/%s lang/js md/nodejs#%s api/codewhispererruntime#1.0.0 m/E KiroIDE-%s-%s", osName, nodeVersion, KiroIDEVersion, machineID))
	h.Set("Connection", "close")
	return h
}

func (c *Client) machineID() string {
	key := firstNonEmpty(c.creds.UUID, c.creds.ProfileARN, c.creds.ClientID, os.Getenv("HOSTNAME"), "KIRO_DEFAULT_MACHINE")
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func isExpiryNear(raw string, skew time.Duration) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return true
	}
	return time.Until(t) <= skew
}

func userInputMessage(content, modelID string) map[string]any {
	return map[string]any{"userInputMessage": map[string]any{
		"content": content,
		"modelId": modelID,
		"origin":  OriginAIEditor,
	}}
}

func toolsContext(tools []Tool) map[string]any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		schema := json.RawMessage(`{}`)
		if len(tool.InputSchema) > 0 {
			schema = tool.InputSchema
		}
		out = append(out, map[string]any{"toolSpecification": map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": map[string]any{"json": json.RawMessage(schema)},
		}})
	}
	return map[string]any{"tools": out}
}

func mergeAdjacentMessages(in []Message) []Message {
	out := make([]Message, 0, len(in))
	for _, msg := range in {
		if len(out) == 0 || out[len(out)-1].Role != msg.Role {
			out = append(out, msg)
			continue
		}
		prev := contentText(out[len(out)-1].Content)
		next := contentText(msg.Content)
		if prev == "" {
			out[len(out)-1].Content = next
		} else if next != "" {
			out[len(out)-1].Content = prev + "\n" + next
		}
	}
	return out
}

func contentText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.RawMessage:
		return rawContentText(t)
	case []byte:
		return rawContentText(t)
	default:
		b, _ := json.Marshal(t)
		return rawContentText(b)
	}
}

func rawContentText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, part := range parts {
			switch part["type"] {
			case "text":
				if text, _ := part["text"].(string); text != "" {
					texts = append(texts, text)
				}
			case "tool_use":
				name, _ := part["name"].(string)
				id, _ := part["id"].(string)
				input, _ := json.Marshal(part["input"])
				if id != "" {
					texts = append(texts, fmt.Sprintf("[Called %s (%s) with args: %s]", name, id, input))
				} else {
					texts = append(texts, fmt.Sprintf("[Called %s with args: %s]", name, input))
				}
			case "tool_result":
				id, _ := part["tool_use_id"].(string)
				content, _ := json.Marshal(part["content"])
				texts = append(texts, fmt.Sprintf("[Tool result (%s): %s]", id, contentText(json.RawMessage(content))))
			}
		}
		return strings.Join(texts, "\n")
	}
	return string(raw)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}
