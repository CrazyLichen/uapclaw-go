package prompts

import (
	"context"
	"testing"
)

// TestHasHistorySnapshot 验证快照存在性判定与会话隔离。
func TestHasHistorySnapshot(t *testing.T) {
	manager := NewManager()
	history := &fakeHistory{session: "sess-1"}
	if manager.HasHistorySnapshot(history, "sess-1") {
		t.Fatal("首轮应无快照")
	}
	_, _ = manager.AddSection("sess-1", "deferred", "内容", AttachmentTool, "src")
	if err := manager.Sync(context.Background(), history); err != nil {
		t.Fatalf("sync 失败: %v", err)
	}
	if !manager.HasHistorySnapshot(history, "sess-1") {
		t.Fatal("快照写入后应为 true")
	}
	if manager.HasHistorySnapshot(history, "sess-2") {
		t.Fatal("其他会话不应受影响")
	}
	if manager.HasHistorySnapshot(nil, "sess-1") {
		t.Fatal("nil 上下文应返回 false")
	}
}

// TestFind_按section与来源查询 验证附件恢复入口。
func TestFind_按section与来源查询(t *testing.T) {
	manager := NewManager()
	if got := manager.Find("s1", "deferred", "src"); got != nil {
		t.Fatal("无附件时应返回 nil")
	}
	_, _ = manager.AddSection("s1", "deferred", "内容", AttachmentTool, "src", WithAttachmentPriority(75))
	got := manager.Find("s1", "deferred", "src")
	if got == nil || got.Content != "内容" {
		t.Fatalf("应查到附件，got %+v", got)
	}
	if other := manager.Find("s1", "deferred", "other"); other != nil {
		t.Fatal("来源不匹配应返回 nil")
	}
}
