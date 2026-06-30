package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
	"github.com/gin-gonic/gin"
)

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
		"Called Bash (toolu_1)",
		"Tool result (toolu_1)",
		"userInputMessageContext",
		"toolSpecification",
		"Bash",
		"command",
	} {
		if !strings.Contains(payloadText, want) {
			t.Fatalf("upstream payload missing %q: %s", want, payloadText)
		}
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

	usage, firstTokenMs, clientDisconnect, err := (&GatewayService{}).handleKiroClaudeStream(c.Request.Context(), c, resp, "claude-sonnet-4-5", testKiroStartTime())
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
