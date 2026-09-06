package team_workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// newToolTestTranslator 创建用于测试的 ToolTranslator 闭包。
// 模拟 locales.MakeTranslator 的行为，避免循环依赖。
func newToolTestTranslator() ToolTranslator {
	strings := map[string]string{
		"workspace_meta":        "工作空间元数据工具（测试）",
		"workspace_meta.action": "操作类型：lock（获取文件锁）、unlock（释放文件锁）、locks（列出所有活跃锁）、history（查看文件版本历史）",
		"workspace_meta.path":   "目标文件的相对路径（lock/unlock/history 时必填）",
	}
	return func(tool string, key ...string) string {
		if len(key) == 0 {
			if desc, ok := strings[tool]; ok {
				return desc
			}
			return tool
		}
		dictKey := tool + "." + key[0]
		if val, ok := strings[dictKey]; ok {
			return val
		}
		return dictKey
	}
}

// newToolTestManager 创建用于 WorkspaceMetaTool 测试的 TeamWorkspaceManager。
// 使用 manager_test.go 中的 newTestManager，避免重复定义。
func newToolTestManager(t *testing.T) *TeamWorkspaceManager {
	t.Helper()
	ws := newTestManager(t, true)
	if err := ws.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}
	setupGitConfig(t, ws.WorkspacePath())
	return ws
}

// TestNewWorkspaceMetaTool 测试创建 WorkspaceMetaTool
func TestNewWorkspaceMetaTool(t *testing.T) {
	ws := newToolTestManager(t)
	tr := newToolTestTranslator()

	t.Run("基本创建", func(t *testing.T) {
		tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")
		if tool == nil {
			t.Fatal("NewWorkspaceMetaTool 返回 nil")
		}
		if tool.Card() == nil {
			t.Fatal("Card() 返回 nil")
		}
		if tool.Card().Name != toolNameWorkspaceMeta {
			t.Errorf("工具名称 = %q, 期望 %q", tool.Card().Name, toolNameWorkspaceMeta)
		}
		if tool.memberName != "member1" {
			t.Errorf("memberName = %q, 期望 %q", tool.memberName, "member1")
		}
		if tool.displayName != "成员一" {
			t.Errorf("displayName = %q, 期望 %q", tool.displayName, "成员一")
		}
	})

	t.Run("InputParams数量", func(t *testing.T) {
		tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")
		params := tool.Card().InputParams
		if len(params) != 2 {
			t.Fatalf("InputParams 数量 = %d, 期望 2", len(params))
		}
		if params[0].Name != "action" {
			t.Errorf("第一个参数名 = %q, 期望 %q", params[0].Name, "action")
		}
		if !params[0].Required {
			t.Error("action 参数应为 Required")
		}
		if params[1].Name != "path" {
			t.Errorf("第二个参数名 = %q, 期望 %q", params[1].Name, "path")
		}
		if params[1].Required {
			t.Error("path 参数应为非 Required")
		}
	})

	t.Run("action枚举值", func(t *testing.T) {
		tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")
		params := tool.Card().InputParams
		actionParam := params[0]
		if len(actionParam.Enum) != 4 {
			t.Fatalf("action 枚举值数量 = %d, 期望 4", len(actionParam.Enum))
		}
		expectedEnum := []string{actionLock, actionUnlock, actionLocks, actionHistory}
		for i, exp := range expectedEnum {
			if actionParam.Enum[i] != exp {
				t.Errorf("枚举值[%d] = %v, 期望 %q", i, actionParam.Enum[i], exp)
			}
		}
	})
}

// TestWorkspaceMetaTool_Invoke_Lock 测试 lock 操作
func TestWorkspaceMetaTool_Invoke_Lock(t *testing.T) {
	ws := newToolTestManager(t)
	tr := newToolTestTranslator()
	tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")
	ctx := context.Background()

	t.Run("成功获取锁", func(t *testing.T) {
		result, err := tool.Invoke(ctx, map[string]any{
			"action": "lock",
			"path":   ".team/artifacts/code/main.go",
		})
		if err != nil {
			t.Fatalf("Invoke 返回错误: %v", err)
		}
		if result["success"] != true {
			t.Errorf("success = %v, 期望 true", result["success"])
		}
		data, _ := result["data"].(map[string]any)
		if data["locked"] != ".team/artifacts/code/main.go" {
			t.Errorf("locked = %v, 期望 .team/artifacts/code/main.go", data["locked"])
		}
	})

	t.Run("缺少path参数", func(t *testing.T) {
		result, _ := tool.Invoke(ctx, map[string]any{
			"action": "lock",
		})
		if result["success"] != false {
			t.Errorf("success = %v, 期望 false", result["success"])
		}
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			t.Error("期望返回错误信息")
		}
	})

	t.Run("锁冲突", func(t *testing.T) {
		// 先用 member2 获取锁
		acquired, err := ws.AcquireLock(ctx, ".team/artifacts/code/conflict.go", "member2", "成员二")
		if err != nil || !acquired {
			t.Fatalf("AcquireLock 准备失败: acquired=%v, err=%v", acquired, err)
		}

		result, _ := tool.Invoke(ctx, map[string]any{
			"action": "lock",
			"path":   ".team/artifacts/code/conflict.go",
		})
		if result["success"] != false {
			t.Errorf("success = %v, 期望 false", result["success"])
		}
		errMsg, _ := result["error"].(string)
		if errMsg == "" {
			t.Error("期望返回锁冲突错误信息")
		}
	})
}

// TestWorkspaceMetaTool_Invoke_Unlock 测试 unlock 操作
func TestWorkspaceMetaTool_Invoke_Unlock(t *testing.T) {
	ws := newToolTestManager(t)
	tr := newToolTestTranslator()
	tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")
	ctx := context.Background()

	t.Run("成功释放锁", func(t *testing.T) {
		// 先获取锁
		acquired, err := ws.AcquireLock(ctx, ".team/artifacts/code/main.go", "member1", "成员一")
		if err != nil || !acquired {
			t.Fatalf("AcquireLock 准备失败: acquired=%v, err=%v", acquired, err)
		}

		result, err := tool.Invoke(ctx, map[string]any{
			"action": "unlock",
			"path":   ".team/artifacts/code/main.go",
		})
		if err != nil {
			t.Fatalf("Invoke 返回错误: %v", err)
		}
		if result["success"] != true {
			t.Errorf("success = %v, 期望 true", result["success"])
		}
		data, _ := result["data"].(map[string]any)
		if data["released"] != true {
			t.Errorf("released = %v, 期望 true", data["released"])
		}
	})

	t.Run("释放他人锁返回false", func(t *testing.T) {
		// member2 持有锁
		acquired, err := ws.AcquireLock(ctx, ".team/artifacts/code/other.go", "member2", "成员二")
		if err != nil || !acquired {
			t.Fatalf("AcquireLock 准备失败: acquired=%v, err=%v", acquired, err)
		}

		result, _ := tool.Invoke(ctx, map[string]any{
			"action": "unlock",
			"path":   ".team/artifacts/code/other.go",
		})
		if result["success"] != true {
			t.Errorf("success = %v, 期望 true（解锁操作始终成功返回）", result["success"])
		}
		data, _ := result["data"].(map[string]any)
		if data["released"] != false {
			t.Errorf("released = %v, 期望 false（不是自己的锁）", data["released"])
		}
	})

	t.Run("缺少path参数", func(t *testing.T) {
		result, _ := tool.Invoke(ctx, map[string]any{
			"action": "unlock",
		})
		if result["success"] != false {
			t.Errorf("success = %v, 期望 false", result["success"])
		}
	})
}

// TestWorkspaceMetaTool_Invoke_Locks 测试 locks 操作
func TestWorkspaceMetaTool_Invoke_Locks(t *testing.T) {
	ws := newToolTestManager(t)
	tr := newToolTestTranslator()
	tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")
	ctx := context.Background()

	t.Run("无锁时返回空列表", func(t *testing.T) {
		result, err := tool.Invoke(ctx, map[string]any{
			"action": "locks",
		})
		if err != nil {
			t.Fatalf("Invoke 返回错误: %v", err)
		}
		if result["success"] != true {
			t.Errorf("success = %v, 期望 true", result["success"])
		}
		data, _ := result["data"].(map[string]any)
		locks, _ := data["locks"].([]map[string]any)
		if len(locks) != 0 {
			t.Errorf("锁数量 = %d, 期望 0", len(locks))
		}
	})

	t.Run("有锁时返回锁列表", func(t *testing.T) {
		acquired, err := ws.AcquireLock(ctx, ".team/artifacts/code/a.go", "member1", "成员一")
		if err != nil || !acquired {
			t.Fatalf("AcquireLock 准备失败: acquired=%v, err=%v", acquired, err)
		}
		acquired, err = ws.AcquireLock(ctx, ".team/artifacts/code/b.go", "member2", "成员二")
		if err != nil || !acquired {
			t.Fatalf("AcquireLock 准备失败: acquired=%v, err=%v", acquired, err)
		}

		result, _ := tool.Invoke(ctx, map[string]any{
			"action": "locks",
		})
		if result["success"] != true {
			t.Errorf("success = %v, 期望 true", result["success"])
		}
		data, _ := result["data"].(map[string]any)
		locks, _ := data["locks"].([]map[string]any)
		if len(locks) != 2 {
			t.Errorf("锁数量 = %d, 期望 2", len(locks))
		}
		// 验证锁条目包含必要字段
		for i, lock := range locks {
			if _, ok := lock["file_path"]; !ok {
				t.Errorf("锁[%d] 缺少 file_path 字段", i)
			}
			if _, ok := lock["holder_id"]; !ok {
				t.Errorf("锁[%d] 缺少 holder_id 字段", i)
			}
			if _, ok := lock["holder_name"]; !ok {
				t.Errorf("锁[%d] 缺少 holder_name 字段", i)
			}
		}
	})
}

// TestWorkspaceMetaTool_Invoke_History 测试 history 操作
func TestWorkspaceMetaTool_Invoke_History(t *testing.T) {
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, "ws_team1")
	ws := NewTeamWorkspaceManager(NewTeamWorkspaceConfig(), wsDir, "team1", WorkspaceModeLocal)
	if err := ws.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize 失败: %v", err)
	}
	setupGitConfig(t, wsDir)
	// 创建一个文件并提交
	testFile := filepath.Join(wsDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	if _, err := ws.AutoCommit(context.Background(), "test.txt", "member1"); err != nil {
		t.Fatalf("AutoCommit 失败: %v", err)
	}

	tr := newToolTestTranslator()
	tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")
	ctx := context.Background()

	t.Run("成功获取历史", func(t *testing.T) {
		result, err := tool.Invoke(ctx, map[string]any{
			"action": "history",
			"path":   "test.txt",
		})
		if err != nil {
			t.Fatalf("Invoke 返回错误: %v", err)
		}
		if result["success"] != true {
			t.Errorf("success = %v, 期望 true", result["success"])
		}
		data, _ := result["data"].(map[string]any)
		history, _ := data["history"].([]map[string]any)
		if len(history) == 0 {
			t.Error("历史记录为空，期望至少一条")
		}
		// 验证历史条目包含必要字段
		for i, entry := range history {
			if _, ok := entry["commit"]; !ok {
				t.Errorf("历史条目[%d] 缺少 commit 字段", i)
			}
			if _, ok := entry["author"]; !ok {
				t.Errorf("历史条目[%d] 缺少 author 字段", i)
			}
		}
	})

	t.Run("缺少path参数", func(t *testing.T) {
		result, _ := tool.Invoke(ctx, map[string]any{
			"action": "history",
		})
		if result["success"] != false {
			t.Errorf("success = %v, 期望 false", result["success"])
		}
	})
}

// TestWorkspaceMetaTool_Invoke_未知操作 测试未知操作
func TestWorkspaceMetaTool_Invoke_未知操作(t *testing.T) {
	ws := newToolTestManager(t)
	tr := newToolTestTranslator()
	tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")

	result, err := tool.Invoke(context.Background(), map[string]any{
		"action": "invalid_action",
	})
	if err != nil {
		t.Fatalf("Invoke 不应返回硬错误: %v", err)
	}
	if result["success"] != false {
		t.Errorf("success = %v, 期望 false", result["success"])
	}
	errMsg, _ := result["error"].(string)
	if errMsg == "" {
		t.Error("期望返回未知操作错误信息")
	}
}

// TestWorkspaceMetaTool_Stream 测试 Stream 不支持
func TestWorkspaceMetaTool_Stream(t *testing.T) {
	ws := newToolTestManager(t)
	tr := newToolTestTranslator()
	tool := NewWorkspaceMetaTool(ws, tr, "member1", "成员一")

	_, err := tool.Stream(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("Stream 应返回错误")
	}
}

// TestLockToMap 测试 WorkspaceFileLock 到 map 的转换
func TestLockToMap(t *testing.T) {
	lock := NewWorkspaceFileLock(".team/test.go", "member1", "成员一")
	m, err := lockToMap(lock)
	if err != nil {
		t.Fatalf("lockToMap 返回错误: %v", err)
	}
	if m["file_path"] != ".team/test.go" {
		t.Errorf("file_path = %v, 期望 .team/test.go", m["file_path"])
	}
	if m["holder_id"] != "member1" {
		t.Errorf("holder_id = %v, 期望 member1", m["holder_id"])
	}
	if m["holder_name"] != "成员一" {
		t.Errorf("holder_name = %v, 期望 成员一", m["holder_name"])
	}
}

// TestHistoryEntryToMap 测试 HistoryEntry 到 map 的转换
func TestHistoryEntryToMap(t *testing.T) {
	entry := HistoryEntry{
		Commit:  "abc123",
		Author:  "member1",
		Date:    "2024-01-01",
		Message: "initial commit",
	}
	m, err := historyEntryToMap(entry)
	if err != nil {
		t.Fatalf("historyEntryToMap 返回错误: %v", err)
	}
	if m["commit"] != "abc123" {
		t.Errorf("commit = %v, 期望 abc123", m["commit"])
	}
	if m["author"] != "member1" {
		t.Errorf("author = %v, 期望 member1", m["author"])
	}
	if m["message"] != "initial commit" {
		t.Errorf("message = %v, 期望 initial commit", m["message"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
