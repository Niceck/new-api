package relay

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizeClaudeThinkingBlocksUntouched(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "no thinking anywhere",
			body: `{"model":"claude-opus-5","messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "valid thinking block with signature",
			body: `{"model":"claude-opus-5","thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reasoning...","signature":"sig"},{"type":"text","text":"answer"}]}]}`,
		},
		{
			name: "valid redacted_thinking block",
			body: `{"messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"opaque"},{"type":"text","text":"a"}]}]}`,
		},
		{
			name: "string-form content mentioning thinking",
			body: `{"messages":[{"role":"user","content":"tell me about \"thinking\" blocks"}]}`,
		},
		{
			name: "top-level thinking param only",
			body: `{"model":"m","thinking":{"type":"enabled","budget_tokens":2048},"messages":[{"role":"user","content":[{"type":"text","text":"q"}]}]}`,
		},
		{
			name: "invalid JSON is passed through",
			body: `{"messages":[{"role":"user","content":[{"type":"thinking"`,
		},
		{
			name: "no messages field",
			body: `{"model":"m","thinking":{"type":"enabled"}}`,
		},
		{
			name: "unparsable block left alone",
			body: `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"ok","signature":"s"},"loose string block"]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, repaired := sanitizeClaudeThinkingBlocks([]byte(tc.body))
			if repaired != 0 {
				t.Fatalf("repaired = %d, want 0", repaired)
			}
			if string(got) != tc.body {
				t.Fatalf("body changed:\n got: %s\nwant: %s", got, tc.body)
			}
		})
	}
}

func TestSanitizeClaudeThinkingBlocksDropsInvalid(t *testing.T) {
	cases := []struct {
		name         string
		body         string
		wantRepaired int
		wantGone     []string // substrings that must not survive
		wantKept     []string // substrings that must survive
	}{
		{
			name:         "empty thinking string",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""},{"type":"text","text":"keep me"}]}]}`,
			wantRepaired: 1,
			wantGone:     []string{`"type":"thinking"`},
			wantKept:     []string{"keep me"},
		},
		{
			name:         "missing thinking field",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"thinking","signature":"sig-only"},{"type":"text","text":"keep me"}]}]}`,
			wantRepaired: 1,
			wantGone:     []string{"sig-only"},
			wantKept:     []string{"keep me"},
		},
		{
			name:         "null thinking",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":null},{"type":"text","text":"keep me"}]}]}`,
			wantRepaired: 1,
			wantGone:     []string{`"type":"thinking"`},
			wantKept:     []string{"keep me"},
		},
		{
			name:         "non-string thinking",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":{"nested":1}},{"type":"text","text":"keep me"}]}]}`,
			wantRepaired: 1,
			wantGone:     []string{"nested"},
			wantKept:     []string{"keep me"},
		},
		{
			name:         "empty redacted_thinking data",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":""},{"type":"text","text":"keep me"}]}]}`,
			wantRepaired: 1,
			wantGone:     []string{"redacted_thinking"},
			wantKept:     []string{"keep me"},
		},
		{
			name:         "empty thinking next to tool_use",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""},{"type":"tool_use","id":"tu_1","name":"bash","input":{"cmd":"ls"}}]}]}`,
			wantRepaired: 1,
			wantGone:     []string{`"type":"thinking"`},
			wantKept:     []string{"tool_use", "tu_1"},
		},
		{
			name:         "multiple bad blocks across messages",
			body:         `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""},{"type":"text","text":"a"}]},{"role":"user","content":[{"type":"text","text":"q"}]},{"role":"assistant","content":[{"type":"thinking","thinking":null},{"type":"text","text":"b"}]}]}`,
			wantRepaired: 2,
			wantGone:     []string{`"type":"thinking"`},
			wantKept:     []string{`"a"`, `"q"`, `"b"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, repaired := sanitizeClaudeThinkingBlocks([]byte(tc.body))
			if repaired != tc.wantRepaired {
				t.Fatalf("repaired = %d, want %d\nbody: %s", repaired, tc.wantRepaired, got)
			}
			for _, s := range tc.wantGone {
				if strings.Contains(string(got), s) {
					t.Fatalf("substring %q should be gone:\n%s", s, got)
				}
			}
			for _, s := range tc.wantKept {
				if !strings.Contains(string(got), s) {
					t.Fatalf("substring %q should survive:\n%s", s, got)
				}
			}
			if !json.Valid(got) {
				t.Fatalf("sanitized body is not valid JSON: %s", got)
			}
		})
	}
}

func TestSanitizeClaudeThinkingBlocksPlaceholderOnEmptiedMessage(t *testing.T) {
	body := `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""}]},{"role":"user","content":[{"type":"text","text":"next"}]}]}`
	got, repaired := sanitizeClaudeThinkingBlocks([]byte(body))
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("unmarshal sanitized body: %v", err)
	}
	if len(parsed.Messages) != 2 {
		t.Fatalf("message count = %d, want 2 (role alternation must survive)", len(parsed.Messages))
	}
	first := parsed.Messages[0]
	if first.Role != "assistant" || len(first.Content) != 1 || first.Content[0].Type != "text" || first.Content[0].Text == "" {
		t.Fatalf("emptied message should hold a text placeholder, got: %+v", first)
	}
}

func TestSanitizeClaudeThinkingBlocksPreservesSurroundingBytes(t *testing.T) {
	// Large integer and unknown fields must survive byte-exact even when a
	// sibling message is rewritten (raw-message surgery, no float64 round-trip).
	body := `{"model":"claude-opus-5","big_id":9007199254740993,"metadata":{"user_id":"u-1"},"messages":[{"role":"user","content":[{"type":"text","text":"q"}],"x_custom":{"n":123456789012345678}},{"role":"assistant","content":[{"type":"thinking","thinking":""},{"type":"text","text":"a"}]}]}`
	got, repaired := sanitizeClaudeThinkingBlocks([]byte(body))
	if repaired != 1 {
		t.Fatalf("repaired = %d, want 1", repaired)
	}
	for _, s := range []string{"9007199254740993", "123456789012345678", `"metadata":{"user_id":"u-1"}`, `"x_custom":{"n":123456789012345678}`} {
		if !strings.Contains(string(got), s) {
			t.Fatalf("byte-exact survivor %q missing from:\n%s", s, got)
		}
	}
}

func TestSanitizeClaudeThinkingBlocksOversizedBodyFailOpen(t *testing.T) {
	// Bodies beyond maxSanitizeBodySize must be forwarded untouched even when
	// they contain repairable blocks: the sanitizer must not multiply the
	// resident memory of huge requests (codex audit P1-4).
	padding := strings.Repeat("x", maxSanitizeBodySize)
	body := `{"pad":"` + padding + `","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""},{"type":"text","text":"a"}]}]}`
	got, repaired := sanitizeClaudeThinkingBlocks([]byte(body))
	if repaired != 0 {
		t.Fatalf("repaired = %d, want 0 (oversized body must fail open)", repaired)
	}
	if string(got) != body {
		t.Fatalf("oversized body must be returned verbatim")
	}
}

func TestSanitizeClaudeThinkingBlocksToolResultNotRecursed(t *testing.T) {
	// Deliberately non-recursive: only messages[].content[] top-level blocks
	// are inspected. A thinking-shaped block nested inside tool_result.content
	// is left for the upstream to judge (it is not the schema error this
	// sanitizer repairs), so a body whose only defect sits there is untouched.
	body := `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"thinking","thinking":""}]}]}]}`
	got, repaired := sanitizeClaudeThinkingBlocks([]byte(body))
	if repaired != 0 {
		t.Fatalf("repaired = %d, want 0 (tool_result interiors are out of scope)", repaired)
	}
	if string(got) != body {
		t.Fatalf("body with only nested defects must be returned verbatim:\n%s", got)
	}
}
