//go:build kiro_file_experiment

package service

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/xiangking/sub2api-kiro/backend/internal/data/ent"
	"github.com/xiangking/sub2api-kiro/backend/internal/data/ent/schema"
	"github.com/xiangking/sub2api-kiro/backend/internal/pkg/anthropic"
	"github.com/xiangking/sub2api-kiro/backend/internal/pkg/kiro"
)

// MockKiroClientForFileTest is a mock client that simulates Kiro's response for a file analysis scenario.
type MockKiroClientForFileTest struct{}

func (m *MockKiroClientForFileTest) SendMessageStreaming(ctx context.Context, req *kiro.SendMessageStreamingRequest, writer io.Writer) (*kiro.SendMessageStreamingResponse, error) {
	// Simulate a series of events that might occur during file analysis,
	// including the problematic ones we suspect are being missed.
	events := []interface{}{
		anthropic.Event{Event: "message_start", Message: anthropic.Message{Usage: anthropic.Usage{InputTokens: 6000}}},
		anthropic.Event{Event: "content_block_start", Index: 0, ContentBlock: anthropic.ContentBlock{Type: "text", Text: ""}},
		anthropic.Event{Event: "ping"},
		// Simulate a toolUse event, a likely candidate for missing tokens
		anthropic.Event{
			Event: "content_block_start",
			Index: 1,
			ContentBlock: anthropic.ContentBlock{
				Type: "tool_use",
				ID:   "toolu_test123",
				Name: "document_summary",
			},
		},
		anthropic.Event{
			Event: "content_block_delta",
			Index: 1,
			Delta: anthropic.Delta{Type: "input_json_delta", PartialJson: `{"summary_text": "This is a summary`},
			Usage: anthropic.Usage{CompletionTokens: 20}, // Token usage inside a delta
		},
		anthropic.Event{
			Event: "content_block_delta",
			Index: 1,
			Delta: anthropic.Delta{Type: "input_json_delta", PartialJson: ` of the document."}`},
			Usage: anthropic.Usage{CompletionTokens: 15},
		},
		anthropic.Event{
			Event: "content_block_stop",
			Index: 1,
			Usage: anthropic.Usage{CompletionTokens: 5}, // Tokens in stop event
		},
		// Simulate a final usage event
		anthropic.Event{
			Event: "message_delta",
			Delta: anthropic.MessageDelta{StopReason: "tool_use"},
			Usage: anthropic.MessageDeltaUsage{
				InputTokens:   6000,
				OutputTokens:  50, // Final total
				CacheCreation: 0,
				CacheRead:     0,
				ContextUsage:  anthropic.ContextUsage{InputTokens: 5, OutputTokens: 10},
			},
		},
		anthropic.Event{Event: "message_stop", StopReason: "tool_use"},
	}

	for _, event := range events {
		data, _ := json.Marshal(event)
		io.WriteString(writer, "data: "+string(data)+"\n\n")
		// Simulate a small delay between events
		time.Sleep(10 * time.Millisecond)
	}
	// Simulate the end of the stream
	io.WriteString(writer, "data: [DONE]\n\n")

	return &kiro.SendMessageStreamingResponse{}, nil
}

func TestHandleKiroClaudeStream_FileAnalysisTokenCounting(t *testing.T) {
	// 1. Setup
	ctx := context.Background()
	account := &ent.Account{
		ID:         99,
		Name:       "file-test-account",
		Platform:   "kiro",
		Type:       "oauth",
		Status:     schema.AccountStatusNormal,
		IsEditable: true,
	}
	chatReq := &anthropic.ChatRequest{
		Model: "claude-opus-4.7",
		Messages: []anthropic.MessagePart{
			{Role: "user", Content: "Analyze this long document..."},
		},
		Stream: true,
	}

	// Create a pipe to capture the output of handleKiroClaudeStream
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()

	// Use the mock client
	service := &KiroGatewayService{
		kiroClient: &MockKiroClientForFileTest{},
		log:        NewZapLoggerTo(os.Stdout), // Log to stdout for visibility
	}

	var finalUsage anthropic.Usage

	// 2. Run the function in a goroutine
	go func() {
		defer pw.Close()
		resp, err := service.handleKiroClaudeStream(ctx, account, chatReq, pw)
		assert.NoError(t, err)
		if resp != nil && resp.Usage != nil {
			finalUsage = *resp.Usage
		}
	}()

	// 3. Read from the pipe and decode the events to verify they are passed through
	decoder := json.NewDecoder(pr)
	eventCount := 0
	for {
		var event anthropic.StreamResponse
		if err := decoder.Decode(&event); err == io.EOF {
			break
		}
		// In a real test, we might inspect each event. Here we just consume them.
		eventCount++
	}

	// 4. Assertions
	t.Logf("Final calculated usage: %+v", finalUsage)
	assert.True(t, eventCount > 0, "Expected at least one event to be streamed")
	assert.Equal(t, 6000, finalUsage.InputTokens, "Input tokens should be correctly aggregated")
	// This is the key assertion: check if output tokens are now correctly counted
	assert.Equal(t, 50, finalUsage.OutputTokens, "Output tokens should be correctly aggregated from all relevant events")
}
