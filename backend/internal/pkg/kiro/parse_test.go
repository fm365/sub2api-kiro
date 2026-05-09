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
