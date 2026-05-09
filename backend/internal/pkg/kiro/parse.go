package kiro

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

func ParseNonStreamingResponse(body []byte) Response {
	if events := ParseEventStreamBytes(body); len(events) > 0 {
		var parts []string
		for _, event := range events {
			if event.Type == "content" && event.Content != "" {
				parts = append(parts, event.Content)
			}
		}
		text := strings.TrimSpace(strings.Join(parts, ""))
		return Response{
			Content:    text,
			StopReason: "end_turn",
			Usage: Usage{
				InputTokens:  estimateTokens(text) / 2,
				OutputTokens: estimateTokens(text),
			},
		}
	}
	text := strings.TrimSpace(extractContentFromArbitraryJSON(body))
	return Response{
		Content:    text,
		StopReason: "end_turn",
		Usage: Usage{
			InputTokens:  estimateTokens(text) / 2,
			OutputTokens: estimateTokens(text),
		},
	}
}

func ParseEventStreamBuffer(buffer string) (events []StreamEvent, remaining string) {
	remaining = buffer
	searchStart := 0
	for {
		start := nextJSONStart(remaining, searchStart)
		if start < 0 {
			break
		}
		end := matchingObjectEnd(remaining, start)
		if end < 0 {
			remaining = remaining[start:]
			return events, remaining
		}
		raw := remaining[start : end+1]
		if event, ok := parseEventObject(raw); ok {
			events = append(events, event)
		}
		searchStart = end + 1
		if searchStart >= len(remaining) {
			remaining = ""
			return events, remaining
		}
	}
	if searchStart > 0 && searchStart < len(remaining) {
		remaining = remaining[searchStart:]
	} else if searchStart >= len(remaining) {
		remaining = ""
	}
	return events, remaining
}

type EventStreamDecoder struct {
	reader *bufio.Reader
}

func NewEventStreamDecoder(r io.Reader) *EventStreamDecoder {
	return &EventStreamDecoder{reader: bufio.NewReaderSize(r, 64*1024)}
}

func (d *EventStreamDecoder) Decode() (StreamEvent, error) {
	for {
		payload, err := d.decodePayload()
		if err != nil {
			return StreamEvent{}, err
		}
		if event, ok := parseEventObject(string(payload)); ok {
			return event, nil
		}
	}
}

func (d *EventStreamDecoder) DecodeAll() ([]StreamEvent, error) {
	var events []StreamEvent
	for {
		event, err := d.Decode()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return events, nil
			}
			return events, err
		}
		events = append(events, event)
	}
}

func (d *EventStreamDecoder) decodePayload() ([]byte, error) {
	for {
		prelude := make([]byte, 12)
		if _, err := io.ReadFull(d.reader, prelude); err != nil {
			return nil, err
		}

		preludeCRC := eventStreamReadUint32(prelude[8:12])
		if crc32.Checksum(prelude[0:8], eventStreamCRCTable) != preludeCRC {
			return nil, fmt.Errorf("eventstream prelude CRC mismatch")
		}

		totalLength := eventStreamReadUint32(prelude[0:4])
		headersLength := eventStreamReadUint32(prelude[4:8])
		if totalLength < 16 {
			return nil, fmt.Errorf("invalid eventstream frame: total_length=%d", totalLength)
		}
		if headersLength > totalLength-16 {
			return nil, fmt.Errorf("invalid eventstream frame: headers_length=%d total_length=%d", headersLength, totalLength)
		}

		data := make([]byte, int(totalLength)-12)
		if _, err := io.ReadFull(d.reader, data); err != nil {
			return nil, err
		}

		messageCRC := eventStreamReadUint32(data[len(data)-4:])
		h := crc32.New(eventStreamCRCTable)
		_, _ = h.Write(prelude)
		_, _ = h.Write(data[:len(data)-4])
		if h.Sum32() != messageCRC {
			return nil, fmt.Errorf("eventstream message CRC mismatch")
		}

		headers := data[:headersLength]
		payload := data[headersLength : len(data)-4]
		messageType := eventStreamHeaderValue(headers, ":message-type")
		if messageType == "exception" || messageType == "error" {
			return nil, fmt.Errorf("kiro eventstream error: %s", string(payload))
		}
		if exceptionType := eventStreamHeaderValue(headers, ":exception-type"); exceptionType != "" {
			return nil, fmt.Errorf("kiro eventstream exception: %s: %s", exceptionType, string(payload))
		}
		if len(payload) == 0 {
			continue
		}
		return payload, nil
	}
}

func ParseEventStreamBytes(body []byte) []StreamEvent {
	if events, err := NewEventStreamDecoder(bytes.NewReader(body)).DecodeAll(); err == nil && len(events) > 0 {
		return events
	}
	events, _ := ParseEventStreamBuffer(string(body))
	return events
}

func nextJSONStart(s string, from int) int {
	candidates := []string{
		`{"content":`,
		`{"name":`,
		`{"input":`,
		`{"stop":`,
		`{"contextUsagePercentage":`,
	}
	best := -1
	for _, candidate := range candidates {
		if idx := strings.Index(s[from:], candidate); idx >= 0 {
			pos := from + idx
			if best < 0 || pos < best {
				best = pos
			}
		}
	}
	return best
}

func matchingObjectEnd(s string, start int) int {
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escape {
			escape = false
			continue
		}
		if ch == '\\' && inString {
			escape = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseEventObject(raw string) (StreamEvent, bool) {
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return StreamEvent{}, false
	}
	if v, ok := obj["content"].(string); ok {
		if _, followup := obj["followupPrompt"]; !followup {
			return StreamEvent{Type: "content", Content: v}, true
		}
	}
	if name, ok := obj["name"].(string); ok {
		toolID, _ := obj["toolUseId"].(string)
		input := ""
		if rawInput, exists := obj["input"]; exists {
			b, _ := json.Marshal(rawInput)
			input = string(b)
			if s, ok := rawInput.(string); ok {
				input = s
			}
		}
		stop, _ := obj["stop"].(bool)
		return StreamEvent{Type: "toolUse", ToolUse: &ToolUse{ToolUseID: toolID, Name: name, Input: input, Stop: stop}}, true
	}
	if v, ok := obj["input"]; ok {
		if _, hasName := obj["name"]; !hasName {
			input := ""
			if s, ok := v.(string); ok {
				input = s
			} else {
				b, _ := json.Marshal(v)
				input = string(b)
			}
			return StreamEvent{Type: "toolUseInput", Input: input}, true
		}
	}
	if v, ok := obj["stop"].(bool); ok {
		return StreamEvent{Type: "toolUseStop", Stop: v}, true
	}
	if v, ok := obj["contextUsagePercentage"].(float64); ok {
		return StreamEvent{Type: "contextUsage", Percentage: v}, true
	}
	return StreamEvent{}, false
}

func eventStreamHeaderValue(headers []byte, targetName string) string {
	pos := 0
	for pos < len(headers) {
		nameLen := int(headers[pos])
		pos++
		if pos+nameLen > len(headers) {
			break
		}
		name := string(headers[pos : pos+nameLen])
		pos += nameLen
		if pos >= len(headers) {
			break
		}
		valueType := headers[pos]
		pos++
		switch valueType {
		case 0, 1:
			if name == targetName {
				if valueType == 0 {
					return "true"
				}
				return "false"
			}
		case 2:
			if pos+1 > len(headers) {
				return ""
			}
			pos++
		case 3:
			if pos+2 > len(headers) {
				return ""
			}
			pos += 2
		case 4:
			if pos+4 > len(headers) {
				return ""
			}
			pos += 4
		case 5, 8:
			if pos+8 > len(headers) {
				return ""
			}
			pos += 8
		case 6, 7:
			if pos+2 > len(headers) {
				return ""
			}
			valueLen := int(eventStreamReadUint16(headers[pos : pos+2]))
			pos += 2
			if pos+valueLen > len(headers) {
				return ""
			}
			value := string(headers[pos : pos+valueLen])
			pos += valueLen
			if name == targetName {
				return value
			}
		case 9:
			if pos+16 > len(headers) {
				return ""
			}
			pos += 16
		default:
			return ""
		}
	}
	return ""
}

var eventStreamCRCTable = crc32.MakeTable(crc32.IEEE)

func eventStreamReadUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func eventStreamReadUint16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

func extractContentFromArbitraryJSON(body []byte) string {
	var obj any
	if err := json.Unmarshal(body, &obj); err != nil {
		return string(body)
	}
	return walkContent(obj)
}

func walkContent(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if content, ok := t["content"].(string); ok {
			return content
		}
		preferred := []string{"assistantResponseMessage", "message", "output", "result"}
		for _, key := range preferred {
			if child, ok := t[key]; ok {
				if text := walkContent(child); text != "" {
					return text
				}
			}
		}
		for _, child := range t {
			if text := walkContent(child); text != "" {
				return text
			}
		}
	case []any:
		var parts []string
		for _, child := range t {
			if text := walkContent(child); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len([]rune(text)) + 3) / 4
}
