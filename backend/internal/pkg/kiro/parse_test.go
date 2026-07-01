package kiro

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"
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
