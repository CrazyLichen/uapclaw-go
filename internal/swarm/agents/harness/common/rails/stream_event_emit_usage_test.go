package rails

import (
	"testing"

	usage_snapshot "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/usage"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextUsageSnapshotPayload_带齐前端必填字段 验证载荷是 v1 快照的 map 形态
//
// 前端 parseContextUsageSnapshot 缺任一必填字段就整条丢弃；结构体 payload
// 又会被 ParseStreamChunk 的 map 断言兜成空载荷，因此这里逐字段核对 json 名。
func TestContextUsageSnapshotPayload_带齐前端必填字段(t *testing.T) {
	limit := 200000
	inputTokens := 2800
	snapshot := usage_snapshot.BuildSnapshot(usage_snapshot.SnapshotInput{
		Phase:              usage_snapshot.PhasePostCall,
		RequestID:          "sess-1-123",
		SessionID:          "sess-1",
		ProductSessionID:   "sess-1",
		LimitTokens:        limit,
		SystemPromptTokens: 1000,
		ToolsTokens:        200,
		MessagesTokens:     300,
		Usage: &llmschema.UsageMetadata{
			InputTokens:  2800,
			OutputTokens: 100,
			TotalTokens:  2900,
		},
	})

	payload, err := contextUsageSnapshotPayload(snapshot)
	if err != nil {
		t.Fatalf("payload 构造失败: %v", err)
	}

	required := []string{
		"event_type", "schema_version", "phase", "request_id",
		"product_session_id", "session_id", "depth", "context_window", "parts",
	}
	for _, key := range required {
		if _, ok := payload[key]; !ok {
			t.Fatalf("载荷缺少前端必填字段 %q，实际 keys=%v", key, keysOf(payload))
		}
	}

	if payload["event_type"] != "context.usage" {
		t.Fatalf("event_type 应为 context.usage，实际 %v", payload["event_type"])
	}
	if payload["schema_version"] != "context-usage.v1" {
		t.Fatalf("schema_version 应为 context-usage.v1，实际 %v", payload["schema_version"])
	}
	if payload["phase"] != "post_call" {
		t.Fatalf("phase 应为 post_call，实际 %v", payload["phase"])
	}

	window, ok := payload["context_window"].(map[string]any)
	if !ok {
		t.Fatalf("context_window 应为对象，实际 %T", payload["context_window"])
	}
	if window["limit_tokens"] != float64(limit) {
		t.Fatalf("context_window.limit_tokens 应为 %d，实际 %v", limit, window["limit_tokens"])
	}
	// provider 上报输入侧 → 分子取 provider_usage
	if window["input_tokens"] != float64(inputTokens) {
		t.Fatalf("context_window.input_tokens 应为 %d，实际 %v", inputTokens, window["input_tokens"])
	}
	if window["tokens_source"] != "provider_usage" {
		t.Fatalf("tokens_source 应为 provider_usage，实际 %v", window["tokens_source"])
	}

	parts, ok := payload["parts"].(map[string]any)
	if !ok {
		t.Fatalf("parts 应为对象，实际 %T", payload["parts"])
	}
	for _, category := range []string{"system_prompt", "tools", "skills", "messages"} {
		if _, ok := parts[category]; !ok {
			t.Fatalf("parts 缺少分类 %q，实际 keys=%v", category, keysOf(parts))
		}
	}
}

// TestContextUsageSnapshotPayload_无provider用量时降级 验证本地估算口径
func TestContextUsageSnapshotPayload_无provider用量时降级(t *testing.T) {
	snapshot := usage_snapshot.BuildSnapshot(usage_snapshot.SnapshotInput{
		Phase:              usage_snapshot.PhasePostCompact,
		RequestID:          "sess-2-456",
		SessionID:          "sess-2",
		SystemPromptTokens: 1000,
		ToolsTokens:        200,
		MessagesTokens:     300,
	})

	payload, err := contextUsageSnapshotPayload(snapshot)
	if err != nil {
		t.Fatalf("payload 构造失败: %v", err)
	}
	window, ok := payload["context_window"].(map[string]any)
	if !ok {
		t.Fatalf("context_window 应为对象，实际 %T", payload["context_window"])
	}
	if window["tokens_source"] != "local_estimate" {
		t.Fatalf("无 provider 用量时应为 local_estimate，实际 %v", window["tokens_source"])
	}
	if window["input_tokens"] != float64(1500) {
		t.Fatalf("本地估算总量应为 1500，实际 %v", window["input_tokens"])
	}
	if payload["phase"] != "post_compact" {
		t.Fatalf("phase 应保留 post_compact，实际 %v", payload["phase"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// keysOf 返回 map 的键集合，仅用于失败信息。
func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
