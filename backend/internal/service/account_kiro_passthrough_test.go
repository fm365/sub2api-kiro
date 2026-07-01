package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccount_IsKiroPassthroughEnabled(t *testing.T) {
	tests := []struct {
		name string
		acct *Account
		want bool
	}{
		{
			name: "enabled for kiro account",
			acct: &Account{Platform: PlatformKiro, Extra: map[string]any{"kiro_passthrough": true}},
			want: true,
		},
		{
			name: "disabled when missing",
			acct: &Account{Platform: PlatformKiro, Extra: map[string]any{}},
			want: false,
		},
		{
			name: "disabled for non bool",
			acct: &Account{Platform: PlatformKiro, Extra: map[string]any{"kiro_passthrough": "true"}},
			want: false,
		},
		{
			name: "disabled for non kiro",
			acct: &Account{Platform: PlatformOpenAI, Extra: map[string]any{"kiro_passthrough": true}},
			want: false,
		},
		{
			name: "disabled for nil",
			acct: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.acct.IsKiroPassthroughEnabled())
		})
	}
}

func TestKiroRequestFromPassthroughKeepsFields(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"stream":true,
		"max_tokens":1024,
		"temperature":0.2,
		"system":"system reminder",
		"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],
		"tools":[{"name":"Read","description":"read file","input_schema":{"type":"object"}}]
	}`)

	req, err := kiroRequestFromPassthrough(body)

	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4-5", req.Model)
	require.True(t, req.Stream)
	require.Equal(t, 1024, req.MaxTokens)
	require.NotNil(t, req.Temperature)
	require.Equal(t, 0.2, *req.Temperature)
	require.JSONEq(t, `"system reminder"`, string(req.System))
	require.Len(t, req.Messages, 1)
	require.Equal(t, "user", req.Messages[0].Role)
	raw, err := json.Marshal(req.Messages[0].Content)
	require.NoError(t, err)
	require.JSONEq(t, `[{"type":"text","text":"hi"}]`, string(raw))
	require.Len(t, req.Tools, 1)
	require.Equal(t, "Read", req.Tools[0].Name)
	require.JSONEq(t, `{"type":"object"}`, string(req.Tools[0].InputSchema))
}

func TestAccount_IsKiroStripToolsOnFailEnabled(t *testing.T) {
	tests := []struct {
		name string
		acct *Account
		want bool
	}{
		{
			name: "enabled for kiro account",
			acct: &Account{Platform: PlatformKiro, Extra: map[string]any{"kiro_strip_tools_on_fail": true}},
			want: true,
		},
		{
			name: "disabled by default",
			acct: &Account{Platform: PlatformKiro, Extra: map[string]any{}},
			want: false,
		},
		{
			name: "disabled for non bool",
			acct: &Account{Platform: PlatformKiro, Extra: map[string]any{"kiro_strip_tools_on_fail": "true"}},
			want: false,
		},
		{
			name: "disabled for non kiro",
			acct: &Account{Platform: PlatformOpenAI, Extra: map[string]any{"kiro_strip_tools_on_fail": true}},
			want: false,
		},
		{
			name: "disabled for nil",
			acct: nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.acct.IsKiroStripToolsOnFailEnabled())
		})
	}
}
