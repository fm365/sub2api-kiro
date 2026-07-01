package kiro_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
	"github.com/stretchr/testify/require"
)

func TestBuildHTTPRequest_StreamUsesSendMessageStreaming(t *testing.T) {
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
	require.Contains(t, httpReq.URL.String(), "/SendMessageStreaming")
	require.Equal(t, "application/vnd.amazon.eventstream", httpReq.Header.Get("Accept"))
	require.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))
	require.Equal(t, "keep-alive", strings.ToLower(httpReq.Header.Get("Connection")))
	require.Equal(t, "Bearer token", httpReq.Header.Get("Authorization"))
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
	require.Equal(t, "Bearer token", httpReq.Header.Get("Authorization"))
}
