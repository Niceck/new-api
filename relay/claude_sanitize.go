package relay

import (
	"bytes"
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
)

// Pass-through bodies are forwarded verbatim, so structurally invalid thinking
// blocks accumulated by clients (e.g. an empty "thinking" left over from an
// interrupted upstream stream) reach the upstream unfiltered and fail
// Anthropic's schema validation with "each thinking block must contain
// thinking". Dropping just those blocks keeps the rest of the conversation
// intact and lets the request through.

// sanitizeClaudePassThroughBody reads the stored pass-through body and strips
// invalid thinking blocks. It returns the sanitized body and how many blocks
// were repaired; repaired == 0 means the body must be forwarded from storage
// untouched.
func sanitizeClaudePassThroughBody(storage common.BodyStorage) ([]byte, int, error) {
	raw, err := storage.Bytes()
	if err != nil {
		return nil, 0, err
	}
	sanitized, repaired := sanitizeClaudeThinkingBlocks(raw)
	return sanitized, repaired, nil
}

// sanitizeClaudeThinkingBlocks drops content blocks of type "thinking" whose
// "thinking" field is missing, null, non-string or empty, and blocks of type
// "redacted_thinking" with the same defect on "data". A message whose content
// would become empty keeps a minimal text placeholder so role alternation
// survives. Anything that fails to parse is returned verbatim: the sanitizer
// must never become a failure point itself (fail-open).
//
// Untouched values are carried as json.RawMessage, so every byte outside the
// repaired messages (numbers, ordering inside blocks, unknown fields) is
// preserved exactly; only object key order at the rewritten levels may change.
func sanitizeClaudeThinkingBlocks(body []byte) ([]byte, int) {
	if !bytes.Contains(body, []byte(`"thinking"`)) && !bytes.Contains(body, []byte(`"redacted_thinking"`)) {
		return body, 0
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return body, 0
	}
	rawMessages, ok := root["messages"]
	if !ok {
		return body, 0
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(rawMessages, &messages); err != nil {
		return body, 0
	}

	repaired := 0
	for i, rawMsg := range messages {
		newMsg, dropped := sanitizeClaudeMessageContent(rawMsg)
		if dropped == 0 {
			continue
		}
		repaired += dropped
		messages[i] = newMsg
	}
	if repaired == 0 {
		return body, 0
	}

	newMessages, err := json.Marshal(messages)
	if err != nil {
		return body, 0
	}
	root["messages"] = newMessages
	newBody, err := json.Marshal(root)
	if err != nil {
		return body, 0
	}
	return newBody, repaired
}

// sanitizeClaudeMessageContent returns the message with invalid thinking
// blocks removed and the number of blocks dropped. String-form contents and
// unparsable messages are returned unchanged.
func sanitizeClaudeMessageContent(rawMsg json.RawMessage) (json.RawMessage, int) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return rawMsg, 0
	}
	rawContent, ok := msg["content"]
	if !ok {
		return rawMsg, 0
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(rawContent, &blocks); err != nil {
		return rawMsg, 0
	}

	kept := make([]json.RawMessage, 0, len(blocks))
	for _, block := range blocks {
		if isInvalidThinkingBlock(block) {
			continue
		}
		kept = append(kept, block)
	}
	dropped := len(blocks) - len(kept)
	if dropped == 0 {
		return rawMsg, 0
	}
	if len(kept) == 0 {
		// An empty content array is itself rejected upstream; keep a minimal
		// placeholder so the message and the role alternation survive.
		kept = append(kept, json.RawMessage(`{"type":"text","text":"…"}`))
	}

	newContent, err := json.Marshal(kept)
	if err != nil {
		return rawMsg, 0
	}
	msg["content"] = newContent
	newMsg, err := json.Marshal(msg)
	if err != nil {
		return rawMsg, 0
	}
	return newMsg, dropped
}

// isInvalidThinkingBlock reports whether the block is a thinking-family block
// that would fail Anthropic's schema validation ("each thinking block must
// contain thinking", or the redacted_thinking/data counterpart).
func isInvalidThinkingBlock(rawBlock json.RawMessage) bool {
	var block struct {
		Type     string          `json:"type"`
		Thinking json.RawMessage `json:"thinking"`
		Data     json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rawBlock, &block); err != nil {
		return false
	}
	switch block.Type {
	case "thinking":
		return !isNonEmptyJSONString(block.Thinking)
	case "redacted_thinking":
		return !isNonEmptyJSONString(block.Data)
	default:
		return false
	}
}

// isNonEmptyJSONString reports whether raw holds a JSON string with content.
// A missing field (len 0), JSON null, a non-string value or "" all fail.
func isNonEmptyJSONString(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return s != ""
}
