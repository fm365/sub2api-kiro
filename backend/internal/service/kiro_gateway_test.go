package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type kiroHTTPUpstreamRecorder struct {
	callCount int
	bodies    [][]byte
	requests  []*http.Request
	responses []*http.Response
	errors    []error
}

func (r *kiroHTTPUpstreamRecorder) addResponse(statusCode int, header http.Header, body string) {
	if header == nil {
		header = http.Header{}
	}
	r.responses = append(r.responses, &http.Response{
		StatusCode: statusCode,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	})
}

func (r *kiroHTTPUpstreamRecorder) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	return r.DoWithTLS(req, proxyURL, accountID, accountConcurrency, nil)
}

func (r *kiroHTTPUpstreamRecorder) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	r.callCount++
	r.requests = append(r.requests, req)
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(body))
	r.bodies = append(r.bodies, body)

	if len(r.errors) > 0 {
		err := r.errors[0]
		r.errors = r.errors[1:]
		return nil, err
	}
	if len(r.responses) > 0 {
		resp := r.responses[0]
		r.responses = r.responses[1:]
		return resp, nil
	}
	return nil, errors.New("no response or error configured")
}

func TestNormalizeKiroUpstreamStatusTreatsCapacity400AsRateLimit(t *testing.T) {
	body := []byte(`{"__type":"com.amazon.aws.codewhisperer#ThrottlingException","message":"I am experiencing high traffic, please try again shortly.","reason":"INSUFFICIENT_MODEL_CAPACITY"}`)
	if got := normalizeKiroUpstreamStatus(http.StatusBadRequest, body); got != http.StatusTooManyRequests {
		t.Fatalf("normalizeKiroUpstreamStatus = %d, want %d", got, http.StatusTooManyRequests)
	}
}

func TestNormalizeKiroUpstreamStatusKeepsValidation400(t *testing.T) {
	body := []byte(`{"__type":"com.amazon.aws.codewhisperer#ValidationException","message":"Improperly formed request."}`)
	if got := normalizeKiroUpstreamStatus(http.StatusBadRequest, body); got != http.StatusBadRequest {
		t.Fatalf("normalizeKiroUpstreamStatus = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestKiroRequestFromClaudeCodeBodyKeepsClaudeCodeFields(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"stream":true,
		"max_tokens":4096,
		"temperature":0.2,
		"metadata":{"user_id":"claude-code-session-1"},
		"system":[{"type":"text","text":"You are Claude Code.\n<system-reminder>large repo context</system-reminder>","cache_control":{"type":"ephemeral"}}],
		"tools":[{"name":"Bash","description":"Run shell commands","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"inspect repo"}]},
			{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"pwd"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"/tmp/repo"},{"type":"text","text":"continue"}]}
		]
	}`)

	req, err := kiroRequestFromAnthropicBody(body)
	if err != nil {
		t.Fatalf("kiroRequestFromAnthropicBody error: %v", err)
	}
	if req.Model != "claude-sonnet-4-5" || !req.Stream || req.MaxTokens != 4096 {
		t.Fatalf("unexpected request metadata: %#v", req)
	}
	if req.Temperature == nil || *req.Temperature != 0.2 {
		t.Fatalf("temperature = %#v, want 0.2", req.Temperature)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "Bash" || !strings.Contains(string(req.Tools[0].InputSchema), `"command"`) {
		t.Fatalf("tools not preserved: %#v", req.Tools)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages len = %d, want 3", len(req.Messages))
	}

	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: kiro.DefaultRegion}, nil)
	upstreamBody, modelID := client.BuildRequestBody(req)
	if modelID == "" {
		t.Fatalf("modelID is empty")
	}
	payload, err := json.Marshal(upstreamBody)
	if err != nil {
		t.Fatalf("marshal upstream body: %v", err)
	}
	payloadText := string(payload)
	for _, want := range []string{
		"conversationState",
		"You are Claude Code.",
		"system-reminder",
		"inspect repo",
		"userInputMessageContext",
		"toolSpecification",
		"Bash",
		"command",
	} {
		if !strings.Contains(payloadText, want) {
			t.Fatalf("upstream payload missing %q: %s", want, payloadText)
		}
	}
	for _, notWant := range []string{"Called Bash (toolu_1)", "Tool result (toolu_1)", "[Called", "[Tool result"} {
		if strings.Contains(payloadText, notWant) {
			t.Fatalf("upstream payload should not textify tool block %q: %s", notWant, payloadText)
		}
	}
	if got := gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.toolUses.0.toolUseId").String(); got != "toolu_1" {
		t.Fatalf("assistant toolUseId = %q, want toolu_1. payload=%s", got, payloadText)
	}
	if got := gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.toolUses.0.name").String(); got != "Bash" {
		t.Fatalf("assistant toolUse name = %q, want Bash. payload=%s", got, payloadText)
	}
	if got := gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.toolUses.0.input.command").String(); got != "pwd" {
		t.Fatalf("assistant toolUse input.command = %q, want pwd. payload=%s", got, payloadText)
	}
	if got := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.toolUseId").String(); got != "toolu_1" {
		t.Fatalf("current toolResult toolUseId = %q, want toolu_1. payload=%s", got, payloadText)
	}
	if got := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.status").String(); got != "success" {
		t.Fatalf("current toolResult status = %q, want success. payload=%s", got, payloadText)
	}
	if got := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.json.text").String(); got != "/tmp/repo" {
		t.Fatalf("current toolResult content text = %q, want /tmp/repo. payload=%s", got, payloadText)
	}
	if got := gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools.0.toolSpecification.inputSchema.json.type").String(); got != "object" {
		t.Fatalf("tool inputSchema.json.type = %q, want object. payload=%s", got, payloadText)
	}
}

func TestHandleKiroClaudeStreamEmitsClaudeCodeToolUseEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	streamBody := strings.Join([]string{
		`{"name":"Bash","toolUseId":"toolu_1","input":{"command":"pwd"}}`,
		`{"input":"{\"cwd\":\"/tmp/repo\"}"}`,
		`{"stop":true}`,
		`{"content":"done"}`,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(streamBody)),
	}

	usage, firstTokenMs, clientDisconnect, err := (&GatewayService{}).handleKiroClaudeStream(c.Request.Context(), c, resp, "claude-sonnet-4-5", testKiroStartTime(), ClaudeUsage{})
	if err != nil {
		t.Fatalf("handleKiroClaudeStream error: %v", err)
	}
	if clientDisconnect {
		t.Fatalf("clientDisconnect = true, want false")
	}
	if firstTokenMs == nil {
		t.Fatalf("firstTokenMs is nil")
	}
	if usage.OutputTokens == 0 {
		t.Fatalf("usage.OutputTokens = 0, want > 0")
	}

	body := w.Body.String()
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		`"type":"tool_use"`,
		`"id":"toolu_1"`,
		`"name":"Bash"`,
		`"type":"input_json_delta"`,
		`"partial_json":"{\"command\":\"pwd\",\"cwd\":\"/tmp/repo\"}"`,
		`"type":"text_delta"`,
		`"text":"done"`,
		`"stop_reason":"tool_use"`,
		"event: message_stop",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream output missing %q: %s", want, body)
		}
	}
}

func TestForwardKiroWebPortalBuildsCBORRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &kiroHTTPUpstreamRecorder{}
	upstream.addResponse(http.StatusOK, http.Header{"Content-Type": []string{"application/json"}}, `{"content":"pong"}`)

	svc := &GatewayService{httpUpstream: upstream}
	account := &Account{
		ID:       1,
		Platform: PlatformKiro,
		Extra:    map[string]any{"kiro_web_portal": true},
		Credentials: map[string]any{
			"access_token":   "fake-token",
			"profile_arn":    "arn:aws:codewhisperer:us-east-1:123:profile/example",
			"csrf_token":     "csrf",
			"user_id":        "user-1",
			"visitor_id":     "visitor-1",
			"web_session_id": "session-1",
			"web_space_id":   "space-1",
			"web_agent_mode": "VIBE",
			"model_mapping": map[string]any{
				"claude-opus-4-7": "claude-opus-4.7",
			},
		},
	}
	parsed, err := ParseGatewayRequest([]byte(`{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`), "")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result, err := svc.forwardKiro(context.Background(), c, account, parsed, testKiroStartTime())

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, upstream.callCount)
	require.Equal(t, kiro.WebPortalStreamURL, upstream.requests[0].URL.String())
	require.Equal(t, "application/cbor", upstream.requests[0].Header.Get("Content-Type"))
	require.Equal(t, "rpc-v2-cbor", upstream.requests[0].Header.Get("Smithy-Protocol"))
	require.Equal(t, "csrf", upstream.requests[0].Header.Get("x-csrf-token"))
}

func TestForwardKiroStripToolsOnFailRetriesWithoutTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &kiroHTTPUpstreamRecorder{}
	upstream.addResponse(http.StatusBadRequest, nil, `{"message":"validation error"}`)
	upstream.addResponse(http.StatusOK, nil, `{"content":"pong"}`)

	svc := &GatewayService{httpUpstream: upstream}
	account := &Account{
		ID:       1,
		Platform: PlatformKiro,
		Extra:    map[string]any{"kiro_strip_tools_on_fail": true},
		Credentials: map[string]any{
			"access_token": "fake-token",
			"region":       kiro.DefaultRegion,
			"model_mapping": map[string]any{
				"claude-opus-4-7": "claude-opus-4.7",
			},
		},
	}
	parsed, err := ParseGatewayRequest([]byte(`{
		"model":"claude-opus-4-7",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[{"name":"Bash","description":"Run shell commands","input_schema":{"type":"object"}}]
	}`), "")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result, err := svc.forwardKiro(context.Background(), c, account, parsed, testKiroStartTime())

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "true", w.Header().Get("X-Kiro-Tools-Stripped"))
	require.Contains(t, w.Body.String(), `"text":"pong"`)
	require.Equal(t, 2, upstream.callCount)
	require.True(t, gjson.GetBytes(upstream.bodies[0], "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools").Exists())
	require.False(t, gjson.GetBytes(upstream.bodies[1], "conversationState.currentMessage.userInputMessage.userInputMessageContext.tools").Exists())
}

func TestForwardKiroStripToolsOnFailDisabledByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &kiroHTTPUpstreamRecorder{}
	upstream.addResponse(http.StatusBadRequest, nil, `{"message":"validation error"}`)

	svc := &GatewayService{httpUpstream: upstream}
	account := &Account{
		ID:       1,
		Platform: PlatformKiro,
		Credentials: map[string]any{
			"access_token":  "fake-token",
			"region":        kiro.DefaultRegion,
			"model_mapping": map[string]any{"claude-opus-4-7": "claude-opus-4.7"},
		},
	}
	parsed, err := ParseGatewayRequest([]byte(`{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}],"tools":[{"name":"Bash","input_schema":{"type":"object"}}]}`), "")
	require.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	_, err = svc.forwardKiro(context.Background(), c, account, parsed, testKiroStartTime())

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Empty(t, w.Header().Get("X-Kiro-Tools-Stripped"))
	require.Equal(t, 1, upstream.callCount)
}

func testKiroStartTime() time.Time { return time.Now().Add(-10 * time.Millisecond) }

func TestKiroOpsCapturePreservesRequestBodyAndRecordsHTTPError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	upstreamReq := httptest.NewRequest(http.MethodPost, "https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse", strings.NewReader(`{"conversationState":{"currentMessage":{}}}`))

	captureKiroUpstreamRequestBody(c, upstreamReq)
	bodyAfterCapture, err := io.ReadAll(upstreamReq.Body)
	if err != nil {
		t.Fatalf("read request body after capture: %v", err)
	}
	if string(bodyAfterCapture) != `{"conversationState":{"currentMessage":{}}}` {
		t.Fatalf("request body after capture = %q", string(bodyAfterCapture))
	}
	if upstreamReq.ContentLength != int64(len(bodyAfterCapture)) {
		t.Fatalf("ContentLength = %d, want %d", upstreamReq.ContentLength, len(bodyAfterCapture))
	}

	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     http.Header{},
	}
	resp.Header.Set("x-amzn-requestid", "kiro-rid-1")
	recordKiroUpstreamHTTPError(c, &GatewayService{}, &Account{ID: 123, Name: "kiro-account"}, upstreamReq, resp, http.StatusBadRequest, []byte(`{"message":"Improperly formed request."}`))

	if got, ok := c.Get(OpsUpstreamStatusCodeKey); !ok || got.(int) != http.StatusBadRequest {
		t.Fatalf("ops upstream status = %#v ok=%v, want %d", got, ok, http.StatusBadRequest)
	}
	if got, ok := c.Get(OpsUpstreamErrorMessageKey); !ok || !strings.Contains(got.(string), "Improperly formed request") {
		t.Fatalf("ops upstream message = %#v ok=%v", got, ok)
	}
	v, ok := c.Get(OpsUpstreamErrorsKey)
	if !ok {
		t.Fatalf("missing %s", OpsUpstreamErrorsKey)
	}
	events, ok := v.([]*OpsUpstreamErrorEvent)
	if !ok || len(events) != 1 {
		t.Fatalf("events = %#v, want one OpsUpstreamErrorEvent", v)
	}
	event := events[0]
	if event.Platform != PlatformKiro || event.AccountID != 123 || event.AccountName != "kiro-account" || event.UpstreamRequestID != "kiro-rid-1" || event.Kind != "http_error" {
		t.Fatalf("unexpected event metadata: %#v", event)
	}
	if !strings.Contains(event.UpstreamURL, "codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse") {
		t.Fatalf("unexpected upstream url: %q", event.UpstreamURL)
	}
	if !strings.Contains(event.UpstreamRequestBody, "conversationState") {
		t.Fatalf("missing upstream request body in event: %#v", event)
	}
}

func TestKiroBlocksToAnthropicContentMapsNonStreamingToolUse(t *testing.T) {
	blocks := kiroBlocksToAnthropicContent(kiro.Response{Blocks: []kiro.Block{
		{Type: "text", Text: "Let me inspect."},
		{Type: "tool_use", ID: "toolu_1", Name: "Bash", Input: `{"command":"pwd"}`},
		{Type: "text", Text: "Done."},
	}})
	if len(blocks) != 3 {
		t.Fatalf("blocks len = %d, want 3: %#v", len(blocks), blocks)
	}
	if blocks[0].Type != "text" || blocks[0].Text != "Let me inspect." {
		t.Fatalf("unexpected text block: %#v", blocks[0])
	}
	if blocks[1].Type != "tool_use" || blocks[1].ID != "toolu_1" || blocks[1].Name != "Bash" {
		t.Fatalf("unexpected tool block: %#v", blocks[1])
	}
	if string(blocks[1].Input) != `{"command":"pwd"}` {
		t.Fatalf("tool input = %s, want command JSON", string(blocks[1].Input))
	}
	if blocks[2].Type != "text" || blocks[2].Text != "Done." {
		t.Fatalf("unexpected final text block: %#v", blocks[2])
	}
}

func TestKiroParseStreamUsageAndCacheFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	streamBody := strings.Join([]string{
		`{"usage":{"input_tokens":120,"cache_creation_input_tokens":40,"cache_read_input_tokens":80,"cache_creation":{"ephemeral_5m_input_tokens":15,"ephemeral_1h_input_tokens":25}}}`,
		`{"content":"done"}`,
		`{"usage":{"output_tokens":9}}`,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(streamBody))}

	usage, _, clientDisconnect, err := (&GatewayService{}).handleKiroClaudeStream(c.Request.Context(), c, resp, "claude-opus-4-6", testKiroStartTime(), ClaudeUsage{})
	require.NoError(t, err)
	require.False(t, clientDisconnect)
	require.Equal(t, 120, usage.InputTokens)
	require.Equal(t, 9, usage.OutputTokens)
	require.Equal(t, 40, usage.CacheCreationInputTokens)
	require.Equal(t, 80, usage.CacheReadInputTokens)
	require.Equal(t, 15, usage.CacheCreation5mTokens)
	require.Equal(t, 25, usage.CacheCreation1hTokens)

	body := w.Body.String()
	require.Contains(t, body, `"cache_creation_input_tokens":40`)
	require.Contains(t, body, `"cache_read_input_tokens":80`)
	require.Contains(t, body, `"input_tokens":120`)
	require.Contains(t, body, `"output_tokens":9`)
}

func TestKiroRequestUsageEstimateSeparatesCacheCreationTokens(t *testing.T) {
	parsed, err := ParseGatewayRequest([]byte(`{
		"model":"claude-opus-4-6",
		"system":[{"type":"text","text":"cached system prompt","cache_control":{"type":"ephemeral"}}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hello world"}]}]
	}`), PlatformKiro)
	require.NoError(t, err)

	usage := estimateKiroRequestUsage(parsed)
	require.Greater(t, usage.CacheCreationInputTokens, 0)
	require.Greater(t, usage.InputTokens, 0)
	require.Equal(t, usage.CacheCreationInputTokens, usage.CacheCreation5mTokens)
	require.Equal(t, 0, usage.CacheReadInputTokens)
}
