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
	"github.com/ugorji/go/codec"
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

	systemPrompt := contentTextOnly(req.System)
	if systemPrompt != "" {
		if len(messages) > 0 && messages[0].Role == "user" {
			firstParts := parseContentParts(messages[0].Content)
			firstParts.text = joinNonEmpty("\n\n", systemPrompt, firstParts.text)
			history = append(history, userInputMessageFromParts(firstParts, modelID))
			start = 1
		} else {
			history = append(history, userInputMessage(systemPrompt, modelID))
		}
	}

	for i := start; i < len(messages)-1; i++ {
		msg := messages[i]
		parts := parseContentParts(msg.Content)
		if msg.Role == "assistant" {
			history = append(history, assistantResponseMessageFromParts(parts))
			continue
		}
		history = append(history, userInputMessageFromParts(parts, modelID))
	}

	currentParts := parsedContentParts{text: "Continue"}
	if len(messages) > 0 {
		current := messages[len(messages)-1]
		currentParts = parseContentParts(current.Content)
		if current.Role == "assistant" {
			history = append(history, assistantResponseMessageFromParts(currentParts))
			currentParts = parsedContentParts{text: "Continue"}
		} else if len(history) > 0 {
			if _, ok := history[len(history)-1].(map[string]any)["assistantResponseMessage"]; !ok {
				history = append(history, map[string]any{"assistantResponseMessage": map[string]any{"content": "Continue"}})
			}
		}
	}
	if strings.TrimSpace(currentParts.text) == "" && len(currentParts.toolResults) == 0 {
		currentParts.text = "Continue"
	}

	currentUserInput := map[string]any{
		"content": currentParts.text,
		"modelId": modelID,
		"origin":  OriginAIEditor,
	}
	context := userInputMessageContext(req.Tools, currentParts.toolResults)
	if len(context) > 0 {
		currentUserInput["userInputMessageContext"] = context
	}

	state := map[string]any{
		"chatTriggerType": ChatTriggerManual,
		"conversationId":  conversationID,
		"currentMessage": map[string]any{
			"userInputMessage": currentUserInput,
		},
	}
	if len(history) > 0 {
		state["history"] = history
	}
	body := map[string]any{"conversationState": state}
	if c.creds.AuthMethod == AuthMethodSocial && c.creds.ProfileARN != "" {
		body["profileArn"] = c.creds.ProfileARN
	}
	return body, modelID
}

func (c *Client) BuildWebPortalStreamRequest(ctx context.Context, req Request) (*http.Request, string, error) {
	modelID := MapModel(req.Model)
	content := currentMessageText(req.Messages)
	if strings.TrimSpace(content) == "" {
		content = "Continue"
	}
	sessionID := firstNonEmpty(c.creds.WebSessionID, c.creds.UUID, uuid.NewString())
	spaceID := firstNonEmpty(c.creds.WebSpaceID, sessionID)
	agentMode := firstNonEmpty(c.creds.WebAgentMode, "VIBE")
	body := map[string]any{
		"spaceId":   spaceID,
		"sessionId": sessionID,
		"contentBlocks": []any{
			map[string]any{"text": map[string]any{"text": content}},
		},
		"modelId":    modelID,
		"csrfToken":  c.creds.CSRFToken,
		"agentMode":  agentMode,
		"profileArn": c.creds.ProfileARN,
	}
	var payload bytes.Buffer
	var handle codec.CborHandle
	if err := codec.NewEncoder(&payload, &handle).Encode(body); err != nil {
		return nil, "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, WebPortalStreamURL, bytes.NewReader(payload.Bytes()))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Accept", "application/cbor")
	httpReq.Header.Set("Content-Type", "application/cbor")
	httpReq.Header.Set("Smithy-Protocol", "rpc-v2-cbor")
	httpReq.Header.Set("Authorization", "Bearer "+c.creds.AccessToken)
	httpReq.Header.Set("amz-sdk-invocation-id", uuid.NewString())
	httpReq.Header.Set("amz-sdk-request", "attempt=1; max=1")
	httpReq.Header.Set("x-amz-user-agent", "aws-sdk-js/1.0.0 ua/2.1 os/"+runtime.GOOS+" lang/js md/browser#sub2api m/N,M,E")
	httpReq.Header.Set("Origin", "https://app.kiro.dev")
	httpReq.Header.Set("Referer", "https://app.kiro.dev/session/"+sessionID)
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")
	if c.creds.CSRFToken != "" {
		httpReq.Header.Set("x-csrf-token", c.creds.CSRFToken)
	}
	if c.creds.UserID != "" {
		httpReq.Header.Set("x-kiro-userid", c.creds.UserID)
	}
	if c.creds.VisitorID != "" {
		httpReq.Header.Set("x-kiro-visitorid", c.creds.VisitorID)
	}
	if c.creds.WebCookie != "" {
		httpReq.Header.Set("Cookie", c.creds.WebCookie)
	}
	return httpReq, modelID, nil
}

func currentMessageText(messages []Message) string {
	if len(messages) == 0 {
		return ""
	}
	return contentText(messages[len(messages)-1].Content)
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
	for k, values := range c.headers(req.Stream) {
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
	for k, values := range c.headers(false) {
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

func (c *Client) headers(stream bool) http.Header {
	machineID := c.machineID()
	osName := runtime.GOOS + "#" + runtime.GOARCH
	nodeVersion := "20.0.0"
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("amz-sdk-request", "attempt=1; max=1")
	h.Set("x-amzn-kiro-agent-mode", "vibe")
	h.Set("x-amz-user-agent", fmt.Sprintf("aws-sdk-js/1.0.0 KiroIDE-%s-%s", KiroIDEVersion, machineID))
	h.Set("user-agent", fmt.Sprintf("aws-sdk-js/1.0.0 ua/2.1 os/%s lang/js md/nodejs#%s api/codewhispererruntime#1.0.0 m/E KiroIDE-%s-%s", osName, nodeVersion, KiroIDEVersion, machineID))

	if stream {
		h.Set("Accept", "application/vnd.amazon.eventstream")
		h.Set("Connection", "keep-alive")
	} else {
		h.Set("Accept", "application/json")
		h.Set("Connection", "close")
	}
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

type parsedContentParts struct {
	text        string
	toolUses    []any
	toolResults []any
}

func assistantResponseMessageFromParts(parts parsedContentParts) map[string]any {
	msg := map[string]any{"content": parts.text}
	if len(parts.toolUses) > 0 {
		msg["toolUses"] = parts.toolUses
	}
	return map[string]any{"assistantResponseMessage": msg}
}

func userInputMessageFromParts(parts parsedContentParts, modelID string) map[string]any {
	msg := map[string]any{
		"content": parts.text,
		"modelId": modelID,
		"origin":  OriginAIEditor,
	}
	context := userInputMessageContext(nil, parts.toolResults)
	if len(context) > 0 {
		msg["userInputMessageContext"] = context
	}
	return map[string]any{"userInputMessage": msg}
}

func userInputMessageContext(tools []Tool, toolResults []any) map[string]any {
	context := map[string]any{}
	if len(tools) > 0 {
		for k, v := range toolsContext(tools) {
			context[k] = v
		}
	}
	if len(toolResults) > 0 {
		context["toolResults"] = toolResults
	}
	return context
}

func contentTextOnly(v any) string {
	return parseContentParts(v).text
}

func parseContentParts(v any) parsedContentParts {
	switch t := v.(type) {
	case nil:
		return parsedContentParts{}
	case string:
		return parsedContentParts{text: t}
	case json.RawMessage:
		return rawContentParts(t)
	case []byte:
		return rawContentParts(t)
	default:
		b, _ := json.Marshal(t)
		return rawContentParts(b)
	}
}

func rawContentParts(raw []byte) parsedContentParts {
	if len(raw) == 0 {
		return parsedContentParts{}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return parsedContentParts{text: s}
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		return parsedContentParts{text: string(raw)}
	}

	out := parsedContentParts{}
	var texts []string
	for _, part := range parts {
		typ, _ := part["type"].(string)
		switch typ {
		case "text":
			if text, _ := part["text"].(string); text != "" {
				texts = append(texts, text)
			}
		case "tool_use":
			name, _ := part["name"].(string)
			id, _ := part["id"].(string)
			if name == "" || id == "" {
				continue
			}
			input, ok := part["input"]
			if !ok || input == nil {
				input = map[string]any{}
			}
			out.toolUses = append(out.toolUses, map[string]any{
				"toolUseId": id,
				"name":      name,
				"input":     input,
			})
		case "tool_result":
			id, _ := part["tool_use_id"].(string)
			if id == "" {
				continue
			}
			status := "success"
			if isError, _ := part["is_error"].(bool); isError {
				status = "error"
			}
			out.toolResults = append(out.toolResults, map[string]any{
				"toolUseId": id,
				"content":   toolResultContent(part["content"]),
				"status":    status,
			})
		}
	}
	out.text = strings.Join(texts, "\n")
	return out
}

func toolResultContent(content any) []any {
	if content == nil {
		return []any{map[string]any{"json": map[string]any{"text": ""}}}
	}
	if s, ok := content.(string); ok {
		return []any{map[string]any{"json": map[string]any{"text": s}}}
	}
	items, ok := content.([]any)
	if !ok {
		return []any{map[string]any{"json": content}}
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		if block, ok := item.(map[string]any); ok {
			if typ, _ := block["type"].(string); typ == "text" {
				if text, _ := block["text"].(string); text != "" {
					out = append(out, map[string]any{"json": map[string]any{"text": text}})
					continue
				}
			}
		}
		out = append(out, map[string]any{"json": item})
	}
	if len(out) == 0 {
		return []any{map[string]any{"json": map[string]any{"text": ""}}}
	}
	return out
}

func joinNonEmpty(sep string, vals ...string) string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, sep)
}

func toolsContext(tools []Tool) map[string]any {
	out := make([]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"toolSpecification": map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": normalizeToolInputSchema(tool.InputSchema),
		}})
	}
	return map[string]any{"tools": out}
}

func normalizeToolInputSchema(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{"json": map[string]any{}}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return map[string]any{"json": map[string]any{}}
	}
	if wrapped, ok := decoded.(map[string]any); ok {
		if _, hasJSON := wrapped["json"]; hasJSON {
			return wrapped
		}
	}
	return map[string]any{"json": decoded}
}

func mergeAdjacentMessages(in []Message) []Message {
	out := make([]Message, 0, len(in))
	for _, msg := range in {
		if len(out) == 0 || out[len(out)-1].Role != msg.Role {
			out = append(out, msg)
			continue
		}
		prevParts := parseContentParts(out[len(out)-1].Content)
		nextParts := parseContentParts(msg.Content)
		if len(prevParts.toolUses) > 0 || len(prevParts.toolResults) > 0 || len(nextParts.toolUses) > 0 || len(nextParts.toolResults) > 0 {
			out = append(out, msg)
			continue
		}
		if prevParts.text == "" {
			out[len(out)-1].Content = nextParts.text
		} else if nextParts.text != "" {
			out[len(out)-1].Content = prevParts.text + "\n" + nextParts.text
		}
	}
	return out
}

func contentText(v any) string {
	return contentTextOnly(v)
}

func rawContentText(raw []byte) string {
	return rawContentParts(raw).text
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
