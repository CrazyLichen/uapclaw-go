package path

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserHomeDir_默认(t *testing.T) {
	ResetCache()
	_ = os.Unsetenv(EnvHome)
	ResetCache()

	home := UserHomeDir()
	if home == "" {
		t.Error("UserHomeDir 不应返回空")
	}
}

func TestUserHomeDir_环境变量(t *testing.T) {
	customHome := "/tmp/uapclaw-test-home"
	_ = os.Setenv(EnvHome, customHome)
	defer func() { _ = os.Unsetenv(EnvHome) }()
	ResetCache()

	home := UserHomeDir()
	if home != customHome {
		t.Errorf("期望 %s，实际 %s", customHome, home)
	}
}

func TestWorkspaceDir_默认(t *testing.T) {
	ResetCache()
	_ = os.Unsetenv(EnvDataDir)
	_ = os.Unsetenv(EnvHome)
	ResetCache()

	ws := WorkspaceDir()
	if !strings.HasSuffix(ws, DefaultDir) {
		t.Errorf("WorkspaceDir 应以 %q 结尾，实际 %q", DefaultDir, ws)
	}
}

func TestWorkspaceDir_环境变量(t *testing.T) {
	customDir := "/tmp/uapclaw-test-data"
	_ = os.Setenv(EnvDataDir, customDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	ResetCache()

	ws := WorkspaceDir()
	if ws != customDir {
		t.Errorf("期望 %s，实际 %s", customDir, ws)
	}
}

func TestConfigDir_已初始化(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	// 创建 config 目录
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	ResetCache()

	configDir := ConfigDir()
	expected := filepath.Join(tmpDir, "config")
	if configDir != expected {
		t.Errorf("期望 %s，实际 %s", expected, configDir)
	}
}

func TestConfigDir_未初始化_回退到resources(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv(EnvDataDir, filepath.Join(tmpDir, "nonexistent"))
	_ = os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		_ = os.Unsetenv(EnvDataDir)
		_ = os.Unsetenv(EnvResourcesDir)
	}()
	ResetCache()

	configDir := ConfigDir()
	if configDir != resDir {
		t.Errorf("期望回退到 %s，实际 %s", resDir, configDir)
	}
}

func TestConfigFile_基于ConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	ResetCache()

	cf := ConfigFile()
	expected := filepath.Join(ConfigDir(), "config.yaml")
	if cf != expected {
		t.Errorf("期望 %s，实际 %s", expected, cf)
	}
}

func TestEnvFile_基于ConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	ResetCache()

	ef := EnvFile()
	expected := filepath.Join(ConfigDir(), ".env")
	if ef != expected {
		t.Errorf("期望 %s，实际 %s", expected, ef)
	}
}

func TestIsInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	ResetCache()

	// 未初始化
	if IsInitialized() {
		t.Error("临时目录不应已初始化")
	}

	// 创建 config 目录
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	ResetCache()

	if !IsInitialized() {
		t.Error("有 config 目录后应已初始化")
	}
}

func TestResourcesDir_环境变量(t *testing.T) {
	resDir := t.TempDir()
	_ = os.Setenv(EnvResourcesDir, resDir)
	defer func() { _ = os.Unsetenv(EnvResourcesDir) }()

	dir, err := ResourcesDir()
	if err != nil {
		t.Fatalf("ResourcesDir 失败: %v", err)
	}
	if dir != resDir {
		t.Errorf("期望 %s，实际 %s", resDir, dir)
	}
}

func TestResourcesDir_不存在(t *testing.T) {
	_ = os.Unsetenv(EnvResourcesDir)

	_, err := ResourcesDir()
	if err == nil {
		t.Error("不存在的 resources 目录应返回错误")
	}
}

func TestSetUserHome_重置缓存(t *testing.T) {
	newHome := "/tmp/uapclaw-new-home"
	_ = os.MkdirAll(newHome, 0o755)
	defer func() { _ = os.RemoveAll(newHome) }()

	// 先设置 env var，再 SetUserHome 触发缓存重置
	_ = os.Setenv(EnvHome, newHome)
	defer func() { _ = os.Unsetenv(EnvHome) }()
	SetUserHome("")

	home := UserHomeDir()
	if home != newHome {
		t.Errorf("期望 %s，实际 %s", newHome, home)
	}
}

func TestResetCache(t *testing.T) {
	_ = os.Setenv(EnvHome, "/tmp/test-reset")
	defer func() { _ = os.Unsetenv(EnvHome) }()

	// 先触发缓存
	_ = UserHomeDir()
	// 重置
	ResetCache()
	// 再次获取应重新计算
	home := UserHomeDir()
	if home != "/tmp/test-reset" {
		t.Errorf("期望 /tmp/test-reset，实际 %s", home)
	}
}

func TestGetResolvedPaths_未初始化无resources(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv(EnvDataDir, filepath.Join(tmpDir, "data"))
	_ = os.Setenv(EnvResourcesDir, filepath.Join(tmpDir, "nonexistent"))
	defer func() {
		_ = os.Unsetenv(EnvDataDir)
		_ = os.Unsetenv(EnvResourcesDir)
	}()
	ResetCache()

	// data/config 不存在，resources 也不存在 → 降级为用户目录
	configDir := ConfigDir()
	if configDir == "" {
		t.Error("降级时 ConfigDir 不应为空")
	}
}

func TestResolvedPaths_Fallback字段(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "resources")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Setenv(EnvDataDir, filepath.Join(tmpDir, "nonexistent"))
	_ = os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		_ = os.Unsetenv(EnvDataDir)
		_ = os.Unsetenv(EnvResourcesDir)
	}()
	ResetCache()

	// 未初始化，回退到 resources → Fallback=true
	paths := getResolvedPaths()
	if !paths.Fallback {
		t.Error("回退场景 Fallback 应为 true")
	}
}
