package security

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestUserConfig_默认敏感模式 默认 settings.is_sensitive=True（Python DEFAULT）
func TestUserConfig_默认敏感模式(t *testing.T) {
	resetUserConfigForTest(t)
	uc := getUserConfigForTest("")
	if !uc.IsSensitive {
		t.Fatalf("默认 IsSensitive = false, 期望 true（Python DEFAULT is_sensitive=True）")
	}
}

// TestUserConfig_配置文件关闭敏感模式 加载 INI 配置后覆盖默认值
func TestUserConfig_配置文件关闭敏感模式(t *testing.T) {
	resetUserConfigForTest(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "user_config.ini")
	content := "[settings]\nis_sensitive = False\nsensitive_paths = /tmp/a,/tmp/b\n"
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	uc := getUserConfigForTest(cfg)
	if uc.IsSensitive {
		t.Fatalf("IsSensitive = true, 期望 false")
	}
	paths := uc.GetSensitivePathsList()
	if len(paths) != 2 || paths[0] != "/tmp/a" {
		t.Fatalf("sensitive_paths 解析 = %v", paths)
	}
}

// TestUserConfig_损坏配置回落默认 配置解析异常 → 落默认 settings
func TestUserConfig_损坏配置回落默认(t *testing.T) {
	resetUserConfigForTest(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "bad.ini")
	_ = os.WriteFile(cfg, []byte("not ini\x00\x01garbage"), 0o600)
	uc := getUserConfigForTest(cfg)
	if !uc.IsSensitive {
		t.Fatalf("损坏配置应回落默认 true")
	}
	if len(uc.GetSensitivePathsList()) != len(defaultSensitivePaths) {
		t.Fatalf("损坏配置敏感路径应回落默认清单")
	}
}

// TestUserConfig_环境变量优先 IS_SENSITIVE=false 直接短路返回 False（user_config.py:62）
func TestUserConfig_环境变量优先(t *testing.T) {
	resetUserConfigForTest(t)
	t.Setenv("IS_SENSITIVE", "false")
	// Python is_sensitive() 类方法 → Go 包级 IsSensitive()（env 短路属类方法，不在实例字段）
	if IsSensitive() {
		t.Fatal("IS_SENSITIVE=false 应直接短路返回 False（user_config.py:62-64）")
	}
}

// TestUserConfig_敏感路径默认清单 空配置回落 DEFAULT_SENSITIVE_PATHS
func TestUserConfig_敏感路径默认清单(t *testing.T) {
	resetUserConfigForTest(t)
	uc := getUserConfigForTest("")
	paths := uc.GetSensitivePathsList()
	if len(paths) != len(defaultSensitivePaths) || paths[0] != "/etc/passwd" {
		t.Fatalf("默认敏感路径 = %v", paths)
	}
	// 返回副本：修改不影响内部状态
	paths[0] = "changed"
	if uc.GetSensitivePathsList()[0] == "changed" {
		t.Fatal("GetSensitivePathsList 应返回副本（Python .copy()）")
	}
}

// TestUserConfig_SetIsSensitive 运行期翻转
func TestUserConfig_SetIsSensitive(t *testing.T) {
	resetUserConfigForTest(t)
	uc := getUserConfigForTest("")
	uc.SetIsSensitive(false)
	if uc.IsSensitive {
		t.Fatal("SetIsSensitive(false) 后应为 false")
	}
	uc.SetIsSensitive(true)
	if !uc.IsSensitive {
		t.Fatal("SetIsSensitive(true) 后应为 true")
	}
}

// ──────────────────────────── 测试助手 ────────────────────────────

// resetUserConfigForTest 重置单例与环境（仅测试用）
func resetUserConfigForTest(t *testing.T) {
	t.Helper()
	t.Setenv("IS_SENSITIVE", "")
	userConfigInstance = nil
	userConfigOnce = sync.Once{}
	userConfigPath = ""
	userConfigPathSet = false
}

// getUserConfigForTest 构造独立实例（绕过单例，便于多场景断言）
func getUserConfigForTest(configPath string) *UserConfig {
	return NewUserConfig(configPath)
}
