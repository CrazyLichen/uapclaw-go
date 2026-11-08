package permissions

import (
	"os"
	"path/filepath"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// isolateConfig 把 agent 配置目录重定向到临时目录，避免污染真实用户配置。
// 返回临时根目录（配置写在 <root>/config/config.yaml）。
//
// 两个隔离手段必须同时用：
//   - workspace.SetUserHome(root)：直接改写主目录并重置缓存，是决定性的；
//   - UAPCLAW_DATA_DIR：WorkspaceDir 的第二优先级来源，兜住并行用例。
//
// 仅设环境变量不够：上一个用例 t.Cleanup 里的 ResetCache 会在下一个用例
// t.Setenv 生效前先按旧环境重算一次路径，导致写回 ~/.uapclaw/config/config.yaml。
func isolateConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(pathutil.EnvDataDir, root)
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}
	prevHome := workspace.UserHomeDir()
	workspace.SetUserHome(root)
	t.Cleanup(func() {
		workspace.SetUserHome(prevHome)
	})
	return root
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestValidateToolsMap 验证 tools 映射校验的各分支。
func TestValidateToolsMap(t *testing.T) {
	t.Run("非对象报错", func(t *testing.T) {
		if _, err := validateToolsMap("not-a-map"); err == nil {
			t.Fatal("非 map 应报错")
		}
	})

	t.Run("空工具名报错", func(t *testing.T) {
		if _, err := validateToolsMap(map[string]any{"  ": "allow"}); err == nil {
			t.Fatal("空工具名应报错")
		}
	})

	t.Run("合法级别通过", func(t *testing.T) {
		got, err := validateToolsMap(map[string]any{"bash": "allow"})
		if err != nil {
			t.Fatalf("应通过: %v", err)
		}
		if got["bash"] != "allow" {
			t.Fatalf("bash = %v, 期望 allow", got["bash"])
		}
	})

	t.Run("级别大小写归一", func(t *testing.T) {
		got, err := validateToolsMap(map[string]any{"bash": "  ALLOW  "})
		if err != nil {
			t.Fatalf("应通过: %v", err)
		}
		if got["bash"] != "allow" {
			t.Fatalf("应归一为 allow，实际 %v", got["bash"])
		}
	})

	t.Run("非法级别报错", func(t *testing.T) {
		if _, err := validateToolsMap(map[string]any{"bash": "bogus"}); err == nil {
			t.Fatal("非法级别应报错")
		}
	})

	t.Run("星号对象形态通过", func(t *testing.T) {
		got, err := validateToolsMap(map[string]any{"bash": map[string]any{"*": "deny"}})
		if err != nil {
			t.Fatalf("应通过: %v", err)
		}
		if got["bash"] != "deny" {
			t.Fatalf("bash = %v, 期望 deny", got["bash"])
		}
	})

	t.Run("星号对象非法级别报错", func(t *testing.T) {
		_, err := validateToolsMap(map[string]any{"bash": map[string]any{"*": "bogus"}})
		if err == nil {
			t.Fatal("非法级别应报错")
		}
	})

	t.Run("对象缺少星号键报错", func(t *testing.T) {
		_, err := validateToolsMap(map[string]any{"bash": map[string]any{"other": "allow"}})
		if err == nil {
			t.Fatal("对象缺 * 键应报错")
		}
	})
}

// TestToolNameFromParams 验证工具名提取优先 tool 再 name。
func TestToolNameFromParams(t *testing.T) {
	if got := toolNameFromParams(map[string]any{"tool": "bash"}); got != "bash" {
		t.Fatalf("应取 tool，实际 %q", got)
	}
	if got := toolNameFromParams(map[string]any{"name": "read_file"}); got != "read_file" {
		t.Fatalf("应取 name，实际 %q", got)
	}
	if got := toolNameFromParams(map[string]any{"tool": "  "}); got != "" {
		t.Fatalf("空白应归一为空串，实际 %q", got)
	}
	if got := toolNameFromParams(map[string]any{}); got != "" {
		t.Fatalf("空 params 应返回空串，实际 %q", got)
	}
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestReplacePermissionsToolsInConfig 验证整体替换 tools 并落盘。
func TestReplacePermissionsToolsInConfig(t *testing.T) {
	isolateConfig(t)

	if err := ReplacePermissionsToolsInConfig(map[string]any{"bash": "allow"}); err != nil {
		t.Fatalf("替换失败: %v", err)
	}
	data := readYAMLData()
	perm, _ := data["permissions"].(map[string]any)
	tools, _ := perm["tools"].(map[string]any)
	if tools["bash"] != "allow" {
		t.Fatalf("落盘后 tools.bash = %v, 期望 allow", tools["bash"])
	}

	t.Run("非法输入报错且不写盘", func(t *testing.T) {
		if err := ReplacePermissionsToolsInConfig("bad"); err == nil {
			t.Fatal("非法 tools 应报错")
		}
	})
}

// TestUpdatePermissionsToolInConfig 验证单条工具合并写回。
func TestUpdatePermissionsToolInConfig(t *testing.T) {
	isolateConfig(t)

	if _, err := UpdatePermissionsToolInConfig("bash", "ask"); err != nil {
		t.Fatalf("首次更新失败: %v", err)
	}
	// 再更新另一条，应合并而非覆盖
	if _, err := UpdatePermissionsToolInConfig("read_file", "deny"); err != nil {
		t.Fatalf("第二次更新失败: %v", err)
	}

	data := readYAMLData()
	perm, _ := data["permissions"].(map[string]any)
	tools, _ := perm["tools"].(map[string]any)
	if tools["bash"] != "ask" || tools["read_file"] != "deny" {
		t.Fatalf("合并结果不符: %v", tools)
	}

	t.Run("空工具名报错", func(t *testing.T) {
		if _, err := UpdatePermissionsToolInConfig("   ", "allow"); err == nil {
			t.Fatal("空工具名应报错")
		}
	})
}

// TestUpdatePermissionsOwnerScopesInConfig 验证 owner_scopes 与 deny 消息写回。
func TestUpdatePermissionsOwnerScopesInConfig(t *testing.T) {
	isolateConfig(t)

	ok, err := UpdatePermissionsOwnerScopesInConfig(map[string]any{
		"owner_scopes":          map[string]any{"a": "b"},
		"deny_guidance_message": "no",
	})
	if err != nil || !ok {
		t.Fatalf("更新失败: ok=%v err=%v", ok, err)
	}

	got := GetPermissionsOwnerScopes()
	if got["deny_guidance_message"] != "no" {
		t.Fatalf("deny_guidance_message = %v, 期望 no", got["deny_guidance_message"])
	}
}

// TestGetPermissionsOwnerScopes_空配置 验证无配置时返回空结构而非 nil。
func TestGetPermissionsOwnerScopes_空配置(t *testing.T) {
	isolateConfig(t)
	got := GetPermissionsOwnerScopes()
	if got == nil {
		t.Fatal("应返回非 nil")
	}
	if got["owner_scopes"] == nil {
		t.Fatal("owner_scopes 不应为 nil")
	}
	if got["deny_guidance_message"] != "" {
		t.Fatalf("deny_guidance_message 应为空串，实际 %v", got["deny_guidance_message"])
	}
}
