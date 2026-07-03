package kiro

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/ugorji/go/codec"
)

func TestParseNonStreamingResponse_EventStream(t *testing.T) {
	body := append(kiroTestEventStreamFrame("assistantResponseEvent", []byte(`{"content":"hi","modelId":"claude-opus-4.6"}`)),
		kiroTestEventStreamFrame("assistantResponseEvent", []byte(`{"content":" there","modelId":"claude-opus-4.6"}`))...)

	resp := ParseNonStreamingResponse(body)
	if resp.Content != "hi there" {
		t.Fatalf("content = %q, want %q", resp.Content, "hi there")
	}
}

func TestParseEventStreamBytes_TextFallback(t *testing.T) {
	events := ParseEventStreamBytes([]byte(`noise {"content":"hello","modelId":"claude-opus-4.6"}`))
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Content != "hello" {
		t.Fatalf("content = %q, want hello", events[0].Content)
	}
}

func kiroTestEventStreamFrame(eventType string, payload []byte) []byte {
	var headers bytes.Buffer
	writeKiroTestHeader(&headers, ":event-type", eventType)
	writeKiroTestHeader(&headers, ":content-type", "application/json")
	writeKiroTestHeader(&headers, ":message-type", "event")

	headersBytes := headers.Bytes()
	totalLen := uint32(12 + len(headersBytes) + len(payload) + 4)
	headersLen := uint32(len(headersBytes))

	var prelude bytes.Buffer
	_ = binary.Write(&prelude, binary.BigEndian, totalLen)
	_ = binary.Write(&prelude, binary.BigEndian, headersLen)
	preludeBytes := prelude.Bytes()
	preludeCRC := crc32.Checksum(preludeBytes, crc32.MakeTable(crc32.IEEE))

	var frame bytes.Buffer
	_, _ = frame.Write(preludeBytes)
	_ = binary.Write(&frame, binary.BigEndian, preludeCRC)
	_, _ = frame.Write(headersBytes)
	_, _ = frame.Write(payload)
	messageCRC := crc32.Checksum(frame.Bytes(), crc32.MakeTable(crc32.IEEE))
	_ = binary.Write(&frame, binary.BigEndian, messageCRC)
	return frame.Bytes()
}

func writeKiroTestHeader(buf *bytes.Buffer, name, value string) {
	_ = buf.WriteByte(byte(len(name)))
	_, _ = buf.WriteString(name)
	_ = buf.WriteByte(7)
	_ = binary.Write(buf, binary.BigEndian, uint16(len(value)))
	_, _ = buf.WriteString(value)
}

func TestParseNonStreamingResponse_EventStreamToolUseBlocks(t *testing.T) {
	body := append(kiroTestEventStreamFrame("assistantResponseEvent", []byte(`{"content":"Let me inspect."}`)),
		kiroTestEventStreamFrame("assistantResponseEvent", []byte(`{"name":"Bash","toolUseId":"toolu_1","input":{"command":"pwd"}}`))...)
	body = append(body, kiroTestEventStreamFrame("assistantResponseEvent", []byte(`{"input":"{\"cwd\":\"/tmp/repo\"}"}`))...)
	body = append(body, kiroTestEventStreamFrame("assistantResponseEvent", []byte(`{"stop":true}`))...)
	body = append(body, kiroTestEventStreamFrame("assistantResponseEvent", []byte(`{"content":"Done."}`))...)

	resp := ParseNonStreamingResponse(body)
	if resp.StopReason != "tool_use" {
		t.Fatalf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if resp.Content != "Let me inspect.Done." {
		t.Fatalf("Content = %q, want text-only content", resp.Content)
	}
	if len(resp.Blocks) != 3 {
		t.Fatalf("Blocks len = %d, want 3: %#v", len(resp.Blocks), resp.Blocks)
	}
	if resp.Blocks[0].Type != "text" || resp.Blocks[0].Text != "Let me inspect." {
		t.Fatalf("unexpected first block: %#v", resp.Blocks[0])
	}
	tool := resp.Blocks[1]
	if tool.Type != "tool_use" || tool.ID != "toolu_1" || tool.Name != "Bash" || tool.Input != `{"command":"pwd","cwd":"/tmp/repo"}` {
		t.Fatalf("unexpected tool block: %#v", tool)
	}
	if resp.Blocks[2].Type != "text" || resp.Blocks[2].Text != "Done." {
		t.Fatalf("unexpected last block: %#v", resp.Blocks[2])
	}
}

func TestParseNonStreamingResponse_WebPortalCBOREventStream(t *testing.T) {
	body := append(kiroTestWebPortalFrame("agent_message_chunk", map[string]any{
		"text":    "Still",
		"content": map[string]any{"type": "text", "text": "Still"},
	}), kiroTestWebPortalFrame("agent_message_chunk", map[string]any{
		"text":    " here!",
		"content": map[string]any{"type": "text", "text": " here!"},
	})...)

	resp := ParseNonStreamingResponse(body)
	if resp.Content != "Still here!" {
		t.Fatalf("content = %q, want %q", resp.Content, "Still here!")
	}
	if len(resp.Blocks) != 1 || resp.Blocks[0].Text != "Still here!" {
		t.Fatalf("unexpected blocks: %#v", resp.Blocks)
	}
}

func kiroTestWebPortalFrame(eventType string, payload any) []byte {
	payloadJSON, _ := jsonMarshalForTest(payload)
	var cborPayload bytes.Buffer
	var handle codec.CborHandle
	_ = codec.NewEncoder(&cborPayload, &handle).Encode(map[string]any{
		"eventType": eventType,
		"payload":   string(payloadJSON),
	})
	return kiroTestEventStreamFrame(eventType, cborPayload.Bytes())
}

func jsonMarshalForTest(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	err := enc.Encode(v)
	return bytes.TrimSpace(buf.Bytes()), err
}

func TestParseNonStreamingResponse_WebPortalCBORToolUse(t *testing.T) {
	body := append(kiroTestWebPortalFrame("tool_use_start", map[string]any{
		"name":      "Bash",
		"toolUseId": "toolu_test_1",
		"input":     map[string]any{"command": "pwd"},
	}), kiroTestWebPortalFrame("tool_use_input_chunk", map[string]any{
		"input": map[string]any{"cwd": "/tmp/repo"},
	})...)
	body = append(body, kiroTestWebPortalFrame("tool_use_stop", map[string]any{
		"stop": true,
	})...)
	body = append(body, kiroTestWebPortalFrame("agent_message_chunk", map[string]any{
		"text": "Done.",
	})...)

	resp := ParseNonStreamingResponse(body)
	if resp.StopReason != "tool_use" {
		t.Fatalf("StopReason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.Blocks) != 2 {
		t.Fatalf("Blocks len = %d, want 2: %#v", len(resp.Blocks), resp.Blocks)
	}
	tool := resp.Blocks[0]
	if tool.Type != "tool_use" || tool.ID != "toolu_test_1" || tool.Name != "Bash" {
		t.Fatalf("unexpected tool block: %#v", tool)
	}
	if !strings.Contains(tool.Input, "command") && !strings.Contains(tool.Input, "cwd") {
		t.Fatalf("unexpected tool input: %q", tool.Input)
	}

}

func TestParseEventStreamBytes_UsageEvent(t *testing.T) {
	events := ParseEventStreamBytes([]byte(`noise {"usage":{"input_tokens":120,"output_tokens":9,"cache_creation_input_tokens":40,"cache_read_input_tokens":80,"cache_creation":{"ephemeral_5m_input_tokens":15,"ephemeral_1h_input_tokens":25}}}`))
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Type != "usage" || events[0].Usage == nil {
		t.Fatalf("event = %#v, want usage event", events[0])
	}
	if events[0].Usage.InputTokens != 120 || events[0].Usage.OutputTokens != 9 || events[0].Usage.CacheCreationInputTokens != 40 || events[0].Usage.CacheReadInputTokens != 80 {
		t.Fatalf("usage = %#v", events[0].Usage)
	}
	if events[0].Usage.CacheCreation5mTokens != 15 || events[0].Usage.CacheCreation1hTokens != 25 {
		t.Fatalf("cache creation breakdown = %#v", events[0].Usage)
	}
}
