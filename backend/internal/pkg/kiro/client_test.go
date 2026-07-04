package kiro_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestListAvailableModels_UsesManagementAPI(t *testing.T) {
	var capturedPath string
	var capturedQuery url.Values
	var capturedHeaders http.Header
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedQuery = r.URL.Query()
		capturedHeaders = r.Header.Clone()
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		require.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = w.Write([]byte(`{"defaultModel":{"modelId":"auto"},"models":[{"modelId":"claude-opus-4.8","modelName":"claude-opus-4.8","description":"Claude Opus 4.8 model","rateMultiplier":2.2,"rateUnit":"Credit","supportedInputTypes":["TEXT","IMAGE"],"tokenLimits":{"maxInputTokens":1000000,"maxOutputTokens":128000},"promptCaching":{"supportsPromptCaching":true,"minimumTokensPerCacheCheckpoint":1024,"maximumCacheCheckpointsPerRequest":4}}]}`))
	}))
	defer server.Close()

	// The management URL template is a const, so route the well-known host through
	// the test server transport by replacing the request URL in RoundTrip.
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1", ProfileARN: "arn:test"}, &http.Client{Transport: rewriteHostTransport{target: server.URL}})

	models, _, err := client.ListAvailableModels(context.Background())
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Equal(t, "auto", models.DefaultModel.ModelID)
	require.Len(t, models.Models, 1)
	require.Equal(t, "claude-opus-4.8", models.Models[0].ModelID)
	require.Equal(t, 1000000, models.Models[0].TokenLimits.MaxInputTokens)
	require.True(t, models.Models[0].PromptCaching.SupportsPromptCaching)

	require.Equal(t, "/", capturedPath)
	require.Equal(t, "KIRO_CLI", capturedQuery.Get("origin"))
	require.Equal(t, "arn:test", capturedQuery.Get("profileArn"))
	require.Equal(t, "KIRO_CLI", capturedBody["origin"])
	require.Equal(t, "arn:test", capturedBody["profileArn"])
	require.Equal(t, "application/x-amz-json-1.0", capturedHeaders.Get("Content-Type"))
	require.Equal(t, "AmazonCodeWhispererService.ListAvailableModels", capturedHeaders.Get("x-amz-target"))
	require.Equal(t, "Bearer token", capturedHeaders.Get("Authorization"))
	require.Equal(t, "attempt=1; max=3", capturedHeaders.Get("amz-sdk-request"))
}

type rewriteHostTransport struct{ target string }

func (t rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := url.Parse(t.target)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = target.Scheme
	req.URL.Host = target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestBuildHTTPRequest_StreamUsesGenerateAssistantResponse(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)

	httpReq, upstreamModel, err := client.BuildHTTPRequest(context.Background(), kiro.Request{
		Model:  "claude-sonnet-4-5",
		Stream: true,
		Messages: []kiro.Message{{
			Role:    "user",
			Content: "hi",
		}},
	})

	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4.5", upstreamModel)
	require.Equal(t, "runtime.us-east-1.kiro.dev", httpReq.URL.Host)
	require.Equal(t, "AmazonCodeWhispererStreamingService.GenerateAssistantResponse", httpReq.Header.Get("x-amz-target"))
	require.Equal(t, "application/vnd.amazon.eventstream", httpReq.Header.Get("Accept"))
	require.Equal(t, "application/x-amz-json-1.0", httpReq.Header.Get("Content-Type"))
	require.Equal(t, "keep-alive", strings.ToLower(httpReq.Header.Get("Connection")))
	require.True(t, strings.HasPrefix(httpReq.Header.Get("Authorization"), "Bearer "))
}

func TestBuildHTTPRequest_NonStreamUsesGenerateAssistantResponse(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)

	httpReq, upstreamModel, err := client.BuildHTTPRequest(context.Background(), kiro.Request{
		Model:  "claude-sonnet-4-5",
		Stream: false,
		Messages: []kiro.Message{{
			Role:    "user",
			Content: "hi",
		}},
	})

	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4.5", upstreamModel)
	require.Equal(t, "runtime.us-east-1.kiro.dev", httpReq.URL.Host)
	require.Equal(t, "AmazonCodeWhispererStreamingService.GenerateAssistantResponse", httpReq.Header.Get("x-amz-target"))
	require.Equal(t, "application/json", httpReq.Header.Get("Accept"))
	require.Equal(t, "application/x-amz-json-1.0", httpReq.Header.Get("Content-Type"))
	require.Equal(t, "close", strings.ToLower(httpReq.Header.Get("Connection")))
	require.True(t, strings.HasPrefix(httpReq.Header.Get("Authorization"), "Bearer "))
}

func TestBuildRequestBody_UsesKiroCLIOriginAndAgentTaskType(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)
	body, _ := client.BuildRequestBody(kiro.Request{
		Model: "claude-opus-4-7",
		Messages: []kiro.Message{{
			Role:    "user",
			Content: "hi",
		}},
	})

	payload, err := json.Marshal(body)
	require.NoError(t, err)
	require.Equal(t, "vibe", gjson.GetBytes(payload, "conversationState.agentTaskType").String())
	require.Equal(t, "KIRO_CLI", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.origin").String())
	require.False(t, gjson.GetBytes(payload, "additionalModelRequestFields").Exists())
	require.NotContains(t, string(payload), "AI_EDITOR")
}

func TestBuildRequestBody_IncludesKiroCLIEnvState(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)
	body, _ := client.BuildRequestBody(kiro.Request{
		Model: "claude-opus-4-7",
		Messages: []kiro.Message{{
			Role:    "user",
			Content: "hi",
		}},
		Tools: []kiro.Tool{{
			Name:        "read",
			Description: "Read files",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	})

	payload, err := json.Marshal(body)
	require.NoError(t, err)
	require.NotEmpty(t, gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.envState.currentWorkingDirectory").String())
	require.NotEmpty(t, gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.envState.operatingSystem").String())
}

func TestBuildRequestBody_ToolInputSchemaWrappedAsJSONDocument(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)
	body, _ := client.BuildRequestBody(kiro.Request{
		Model: "claude-sonnet-4-5",
		Messages: []kiro.Message{{
			Role:    "user",
			Content: "hi",
		}},
		Tools: []kiro.Tool{{
			Name:        "Bash",
			Description: "Run shell",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
		}},
	})

	conversationState := body["conversationState"].(map[string]any)
	currentMessage := conversationState["currentMessage"].(map[string]any)
	userInput := currentMessage["userInputMessage"].(map[string]any)
	context := userInput["userInputMessageContext"].(map[string]any)
	tools := context["tools"].([]any)
	require.Len(t, tools, 1)

	spec := tools[0].(map[string]any)["toolSpecification"].(map[string]any)
	inputSchema := spec["inputSchema"].(map[string]any)
	require.Contains(t, inputSchema, "json")

	jsonDoc := inputSchema["json"].(map[string]any)
	require.Equal(t, "object", jsonDoc["type"])
	properties := jsonDoc["properties"].(map[string]any)
	require.Contains(t, properties, "command")
}

func TestBuildRequestBody_DoesNotTextifyHistoricalToolBlocks(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)
	body, _ := client.BuildRequestBody(kiro.Request{
		Model: "claude-opus-4-7-thinking",
		Messages: []kiro.Message{
			{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"inspect workspace"}]`)},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_1","name":"mcp__workspace__bash","input":{"command":"pwd && ls -la"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_1","content":"/tmp/repo\ntotal 1"},{"type":"text","text":"continue"}]`)},
		},
	})
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	payloadText := string(payload)
	require.NotContains(t, payloadText, "[Called ")
	require.NotContains(t, payloadText, "[Tool result")
	require.Equal(t, "toolu_1", gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.toolUses.0.toolUseId").String())
	require.Equal(t, "mcp__workspace__bash", gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.toolUses.0.name").String())
	require.Equal(t, "pwd && ls -la", gjson.GetBytes(payload, "conversationState.history.1.assistantResponseMessage.toolUses.0.input.command").String())
	require.Equal(t, "continue", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String())
	require.Equal(t, "toolu_1", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.toolUseId").String())
	require.Equal(t, "success", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.status").String())
	require.Equal(t, "/tmp/repo\ntotal 1", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.text").String())
}

func TestBuildRequestBody_ToolResultOnlyCurrentMessageKeepsEmptyContent(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)
	body, _ := client.BuildRequestBody(kiro.Request{
		Model: "claude-opus-4-7-thinking",
		Messages: []kiro.Message{
			{Role: "user", Content: "call shell"},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_2","name":"mcp__workspace__bash","input":{"command":"pwd"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_2","content":[{"type":"text","text":"/tmp/repo"}],"is_error":false}]`)},
		},
	})
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	require.Equal(t, "", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.content").String())
	require.Equal(t, "toolu_2", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.toolUseId").String())
	require.Equal(t, "success", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.status").String())
	require.Equal(t, "/tmp/repo", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.text").String())
}

func TestBuildRequestBody_ToolResultErrorStatus(t *testing.T) {
	client := kiro.NewClient(kiro.Credentials{AccessToken: "token", Region: "us-east-1"}, nil)
	body, _ := client.BuildRequestBody(kiro.Request{
		Model: "claude-opus-4-7-thinking",
		Messages: []kiro.Message{
			{Role: "user", Content: "call shell"},
			{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"toolu_err","name":"mcp__workspace__bash","input":{"command":"false"}}]`)},
			{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_err","content":{"exit_status":"exit status: 1","stderr":"boom"},"is_error":true}]`)},
		},
	})
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	require.Equal(t, "error", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.status").String())
	require.Equal(t, "exit status: 1", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.json.exit_status").String())
	require.Equal(t, "boom", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.json.stderr").String())
}
