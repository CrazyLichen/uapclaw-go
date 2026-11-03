package runtime

import (
	"os"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPayloadExtras_ToolCall 防 D5 回归：工具调用记录必须携带 tool_call 嵌套对象。
//
// 对齐 Python interface.py:1063（extra = payload 去掉 event_type/content）。
func TestPayloadExtras_ToolCall(t *testing.T) {
	payload := map[string]any{
		"event_type": "chat.tool_call",
		"tool_call":  map[string]any{"name": "read_file", "arguments": "{}", "tool_call_id": "call_x"},
	}
	extra := payloadExtras(payload)

	tc, ok := extra["tool_call"].(map[string]any)
	if !ok {
		t.Fatalf("extra 应含 tool_call 嵌套对象，实际: %+v", extra)
	}
	if tc["tool_call_id"] != "call_x" {
		t.Fatalf("tool_call_id 丢失: %+v", tc)
	}
	if _, exists := extra["event_type"]; exists {
		t.Fatal("event_type 不应进入 extra")
	}
}

// TestPayloadExtras_ToolResult 防 D5 回归：工具结果字段平铺到顶层。
func TestPayloadExtras_ToolResult(t *testing.T) {
	payload := map[string]any{
		"event_type":   "chat.tool_result",
		"content":      "",
		"result":       "文件内容",
		"tool_name":    "read_file",
		"tool_call_id": "call_x",
		"success":      true,
	}
	extra := payloadExtras(payload)
	for _, key := range []string{"result", "tool_name", "tool_call_id", "success"} {
		if _, ok := extra[key]; !ok {
			t.Fatalf("extra 缺少 %s: %+v", key, extra)
		}
	}
	if _, exists := extra["content"]; exists {
		t.Fatal("content 不应进入 extra")
	}
}

// TestPayloadExtras_TeamMessageKeepsEvent 防 team 记录形状回归：
// 既保留嵌套 event，又展平其子字段，且不覆盖 type/timestamp/content。
func TestPayloadExtras_TeamMessageKeepsEvent(t *testing.T) {
	payload := map[string]any{
		"event_type": "team.message",
		"event": map[string]any{
			"type":      "member_added",
			"timestamp": 1.5,
			"content":   "hi",
			"member_id": "m1",
		},
	}
	extra := payloadExtras(payload)
	extra = mergeTeamEventExtras(extra, payload)

	ev, ok := extra["event"].(map[string]any)
	if !ok {
		t.Fatalf("extra 应保留嵌套 event，实际: %+v", extra)
	}
	if ev["member_id"] != "m1" {
		t.Fatalf("嵌套 event 内容被破坏: %+v", ev)
	}
	if extra["member_id"] != "m1" {
		t.Fatalf("event 子字段应展平到顶层: %+v", extra)
	}
	for _, key := range []string{"type", "timestamp", "content"} {
		if _, exists := extra[key]; exists {
			t.Fatalf("展平不得写入 %s: %+v", key, extra)
		}
	}
}

// TestExtractChunkContent_ErrorFallback 防 D6 回归：content 为空时取 error。
//
// 对齐 Python interface.py:1085（content or error or ""）。
func TestExtractChunkContent_ErrorFallback(t *testing.T) {
	if got := extractChunkContent(map[string]any{"error": "模型调用失败"}); got != "模型调用失败" {
		t.Fatalf("应回退到 error 字段，实际 %q", got)
	}
	if got := extractChunkContent(map[string]any{"content": "正文", "error": "忽略"}); got != "正文" {
		t.Fatalf("content 优先，实际 %q", got)
	}
	if got := extractChunkContent(map[string]any{}); got != "" {
		t.Fatalf("空 payload 应返回空串，实际 %q", got)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestAppendHistoryRecord_ChannelMetadata 防 D7 回归：user 记录必须把
// channel_metadata 落到会话元数据（Python interface.py:920 传 request.metadata）。
//
// 说明：本用例锁定 AppendHistoryRecord 的落盘/元数据联动；调用点是否传参见
// ProcessMessageStream 的 user 记录（本次改为 request.Metadata）。
func TestAppendHistoryRecord_ChannelMetadata(t *testing.T) {
	// 用 UAPCLAW_DATA_DIR + ResetCache 隔离数据根（与本包既有测试一致的路径覆盖方式；
	// workspace.SetUserHome 不足以覆盖 AgentSessionsDir 的解析结果）
	origDataDir, hadDataDir := os.LookupEnv(pathutil.EnvDataDir)
	t.Cleanup(func() {
		session.FlushMetadataQueue()
		session.FlushHistoryQueue()
		if hadDataDir {
			_ = os.Setenv(pathutil.EnvDataDir, origDataDir)
		} else {
			_ = os.Unsetenv(pathutil.EnvDataDir)
		}
		pathutil.ResetCache()
	})
	if err := os.Setenv(pathutil.EnvDataDir, t.TempDir()); err != nil {
		t.Fatalf("设置数据目录失败: %v", err)
	}
	pathutil.ResetCache()

	sid := "sess-meta-test"
	session.AppendHistoryRecord(sid, "req-1", "web", "user", "你好", 1.0,
		"", nil, map[string]any{"chat_id": "c1"}, "agent")
	session.FlushHistoryQueue()

	records, err := session.ReadHistoryRecords(sid)
	if err != nil {
		t.Fatalf("读取历史失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("应写入 1 条记录，实际 %d: %+v", len(records), records)
	}
	if records[0]["content"] != "你好" {
		t.Fatalf("content 不符: %+v", records[0])
	}

	meta := session.ReadSessionMetadataWithCache(sid)
	if meta == nil {
		t.Fatal("会话元数据应已创建")
	}
	cm, ok := meta["channel_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("元数据应含 channel_metadata，实际: %+v", meta)
	}
	if cm["chat_id"] != "c1" {
		t.Fatalf("channel_metadata 内容不符: %+v", cm)
	}
}
