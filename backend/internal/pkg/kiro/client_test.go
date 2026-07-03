package kiro_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

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
	require.Equal(t, "CLAUDE_SONNET_4_5_20250929_V1_0", upstreamModel)
	require.Contains(t, httpReq.URL.String(), "/generateAssistantResponse")
	require.Equal(t, "application/vnd.amazon.eventstream", httpReq.Header.Get("Accept"))
	require.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))
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
	require.Equal(t, "CLAUDE_SONNET_4_5_20250929_V1_0", upstreamModel)
	require.Contains(t, httpReq.URL.String(), "/generateAssistantResponse")
	require.Equal(t, "application/json", httpReq.Header.Get("Accept"))
	require.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))
	require.Equal(t, "close", strings.ToLower(httpReq.Header.Get("Connection")))
	require.True(t, strings.HasPrefix(httpReq.Header.Get("Authorization"), "Bearer "))
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
	require.Equal(t, "/tmp/repo\ntotal 1", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.json.text").String())
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
	require.Equal(t, "/tmp/repo", gjson.GetBytes(payload, "conversationState.currentMessage.userInputMessage.userInputMessageContext.toolResults.0.content.0.json.text").String())
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
