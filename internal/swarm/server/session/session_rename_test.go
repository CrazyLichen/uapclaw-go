package session

import (
	"testing"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// setupRenameTest 设置测试环境。
func setupRenameTest(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	path.ResetCache()
	ClearAllSessionMetadataCache()
	FlushMetadataQueue()
	t.Cleanup(func() {
		FlushMetadataQueue()
		ClearAllSessionMetadataCache()
		path.ResetCache()
	})
}

// TestApplySessionRename_查询标题 验证 title=nil 查询模式
func TestApplySessionRename_查询标题(t *testing.T) {
	setupRenameTest(t)

	// 先初始化一个会话
	InitSessionMetadata("sess_query", "web", "", "当前标题", "agent", "")

	result, err := ApplySessionRename("sess_query", nil, "web")
	if err != nil {
		t.Fatalf("ApplySessionRename 返回错误: %v", err)
	}
	if result["title"] != "当前标题" {
		t.Errorf("title = %v, want 当前标题", result["title"])
	}
	if result["previous_title"] != "当前标题" {
		t.Errorf("previous_title = %v, want 当前标题", result["previous_title"])
	}
	FlushMetadataQueue()
}

// TestApplySessionRename_设置标题 验证 title=非空 设置模式
func TestApplySessionRename_设置标题(t *testing.T) {
	setupRenameTest(t)

	InitSessionMetadata("sess_set", "web", "", "旧标题", "agent", "")

	newTitle := "新标题"
	result, err := ApplySessionRename("sess_set", &newTitle, "web")
	if err != nil {
		t.Fatalf("ApplySessionRename 返回错误: %v", err)
	}
	if result["title"] != "新标题" {
		t.Errorf("title = %v, want 新标题", result["title"])
	}
	if result["previous_title"] != "旧标题" {
		t.Errorf("previous_title = %v, want 旧标题", result["previous_title"])
	}
	FlushMetadataQueue()
}

// TestApplySessionRename_清除标题 验证 title=空串 清除模式
func TestApplySessionRename_清除标题(t *testing.T) {
	setupRenameTest(t)

	InitSessionMetadata("sess_clear", "web", "", "要清除的标题", "agent", "")

	emptyTitle := ""
	result, err := ApplySessionRename("sess_clear", &emptyTitle, "web")
	if err != nil {
		t.Fatalf("ApplySessionRename 返回错误: %v", err)
	}
	if result["title"] != "" {
		t.Errorf("title = %v, want 空串（已清除）", result["title"])
	}
	FlushMetadataQueue()
}

// TestApplySessionRename_标题截断 验证超过 200 字符截断
func TestApplySessionRename_标题截断(t *testing.T) {
	setupRenameTest(t)

	InitSessionMetadata("sess_trunc", "web", "", "", "agent", "")

	longTitle := "这是一个非常长的标题超过了两百个字符的限制所以应该被截断处理这是一个非常长的标题超过了两百个字符的限制所以应该被截断处理这是一个非常长的标题超过了两百个字符的限制所以应该被截断处理"
	result, err := ApplySessionRename("sess_trunc", &longTitle, "web")
	if err != nil {
		t.Fatalf("ApplySessionRename 返回错误: %v", err)
	}
	title, _ := result["title"].(string)
	if utf8.RuneCountInString(title) > renameTitleMaxLen {
		t.Errorf("title 字符数 = %d, want <= %d", utf8.RuneCountInString(title), renameTitleMaxLen)
	}
	FlushMetadataQueue()
}

// TestApplySessionRename_空SessionID 验证空 session_id 返回错误 payload
func TestApplySessionRename_空SessionID(t *testing.T) {
	setupRenameTest(t)

	result, err := ApplySessionRename("", PtrStr("标题"), "web")
	if err != nil {
		t.Fatalf("ApplySessionRename 返回错误: %v", err)
	}
	if result["code"] != "BAD_REQUEST" {
		t.Errorf("code = %v, want BAD_REQUEST", result["code"])
	}
}

// TestApplySessionRename_初始化新会话 验证 metadata 不存在时自动初始化
func TestApplySessionRename_初始化新会话(t *testing.T) {
	setupRenameTest(t)

	newTitle := "全新会话"
	result, err := ApplySessionRename("sess_new_rename", &newTitle, "web_init")
	if err != nil {
		t.Fatalf("ApplySessionRename 返回错误: %v", err)
	}
	if result["title"] != "全新会话" {
		t.Errorf("title = %v, want 全新会话", result["title"])
	}
	FlushMetadataQueue()
}
