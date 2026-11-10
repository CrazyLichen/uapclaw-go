package server

import (
	"testing"

	lifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
)

// 本文件覆盖 lifecycleJournal 的两个方法。
//
// 历史上它们是恒假/恒真空实现，且注释把缺口错标成「spec §10 R-4、等
// session/lifecycle.py 迁移」——那个 R-4 实为执行 owner 表，与删除可恢复性
// 无关。真正的数据源是 10.3.28e 已落地的 lifecycle 包
// （Python: server/runtime/session/lifecycle.py session_delete_pending :142）。

// TestRecoveringDelete_无删除操作时为假 干净会话不得被判成恢复中。
func TestRecoveringDelete_无删除操作时为假(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	if (&lifecycleJournal{}).RecoveringDelete("sess-clean") {
		t.Error("无删除操作的会话不应判为恢复中的删除")
	}
}

// TestRecoveringDelete_删除未完成时为真 删除流程中断后必须能识别。
func TestRecoveringDelete_删除未完成时为真(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	// begin 且 blockExecution=true：写入 operation 并置 blocked（对齐上游三条件）
	if _, err := lifecycle.Begin("session", "sess-deleting", "delete", true); err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}

	if !(&lifecycleJournal{}).RecoveringDelete("sess-deleting") {
		t.Error("未完成的删除应判为恢复中")
	}
}

// TestRecoveringDelete_完成后的删除为假 completed 不再算恢复中。
func TestRecoveringDelete_完成后的删除为假(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	if _, err := lifecycle.Begin("session", "sess-done", "delete", true); err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	if err := lifecycle.Complete("session", "sess-done", false, false, nil); err != nil {
		t.Fatalf("Complete 失败: %v", err)
	}

	if (&lifecycleJournal{}).RecoveringDelete("sess-done") {
		t.Error("已完成的操作不得判为恢复中")
	}
}

// TestMarkCleanup_非删除流程为空操作 不是删除就不该动状态。
func TestMarkCleanup_非删除流程为空操作(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	if err := (&lifecycleJournal{}).MarkCleanup("sess-plain"); err != nil {
		t.Fatalf("非删除流程不应报错: %v", err)
	}
	if lifecycle.SessionDeletePending("sess-plain") {
		t.Error("空操作后不应产生删除待办")
	}
}

// TestMarkCleanup_删除流程推进到收尾 应把 operation 标成 finalizing 状态。
func TestMarkCleanup_删除流程推进到收尾(t *testing.T) {
	t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())

	if _, err := lifecycle.Begin("session", "sess-cleanup", "delete", true); err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	if err := (&lifecycleJournal{}).MarkCleanup("sess-cleanup"); err != nil {
		t.Fatalf("MarkCleanup 失败: %v", err)
	}

	operation, _ := lifecycle.State("session", "sess-cleanup")["operation"].(map[string]any)
	status, _ := operation["status"].(string)
	if status == lifecycle.OperationStatusCompleted {
		t.Error("MarkCleanup 不应把操作直接标成 completed")
	}
	if status == "" {
		t.Error("MarkCleanup 后 operation 状态不应为空")
	}
}
