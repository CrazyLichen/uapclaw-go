package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// TestUserHomeDir_默认 测试默认用户主目录
func TestUserHomeDir_默认(t *testing.T) {
	_ = os.Unsetenv(EnvHome)
	path.ResetCache()

	home := UserHomeDir()
	if home == "" {
		t.Error("UserHomeDir 不应返回空字符串")
	}
	if home == "." {
		t.Log("UserHomeDir 降级到当前目录")
	}
}

// TestUserHomeDir_环境变量 测试 UAPCLAW_HOME 环境变量
func TestUserHomeDir_环境变量(t *testing.T) {
	customHome := "/tmp/uapclaw-test-home"
	_ = os.MkdirAll(customHome, 0o755)
	defer func() { _ = os.RemoveAll(customHome) }()

	_ = os.Setenv(EnvHome, customHome)
	defer func() { _ = os.Unsetenv(EnvHome) }()
	path.ResetCache()

	home := UserHomeDir()
	if home != customHome {
		t.Errorf("UserHomeDir 期望 %q，实际 %q", customHome, home)
	}
}

// TestWorkspaceDir_默认 测试默认工作区目录
func TestWorkspaceDir_默认(t *testing.T) {
	_ = os.Unsetenv(EnvDataDir)
	_ = os.Unsetenv(EnvHome)
	path.ResetCache()

	ws := WorkspaceDir()
	if !strings.HasSuffix(ws, DefaultDir) {
		t.Errorf("WorkspaceDir 应以 %q 结尾，实际 %q", DefaultDir, ws)
	}
}

// TestWorkspaceDir_环境变量 测试 UAPCLAW_DATA_DIR 环境变量
func TestWorkspaceDir_环境变量(t *testing.T) {
	customDir := "/tmp/uapclaw-test-workspace"
	_ = os.MkdirAll(customDir, 0o755)
	defer func() { _ = os.RemoveAll(customDir) }()

	_ = os.Setenv(EnvDataDir, customDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	path.ResetCache()

	ws := WorkspaceDir()
	if ws != customDir {
		t.Errorf("WorkspaceDir 期望 %q，实际 %q", customDir, ws)
	}
}

// TestConfigDir_已初始化 测试已初始化时 ConfigDir 返回用户目录
func TestConfigDir_已初始化(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	// 模拟已初始化：创建 config 目录
	_ = os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)
	path.ResetCache()

	configDir := ConfigDir()
	expected := filepath.Join(tmpDir, "config")
	if configDir != expected {
		t.Errorf("ConfigDir 期望 %q，实际 %q", expected, configDir)
	}
}

// TestConfigDir_未初始化_回退到resources 测试未初始化时回退到 resources
func TestConfigDir_未初始化_回退到resources(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建 resources 目录
	resDir := filepath.Join(tmpDir, "resources")
	_ = os.MkdirAll(filepath.Join(resDir, "agent", "workspace"), 0o755)
	_ = os.WriteFile(filepath.Join(resDir, "config.yaml"), []byte("test"), 0o644)

	_ = os.Setenv(EnvDataDir, filepath.Join(t.TempDir(), "nonexistent"))
	_ = os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		_ = os.Unsetenv(EnvDataDir)
		_ = os.Unsetenv(EnvResourcesDir)
	}()
	path.ResetCache()

	configDir := ConfigDir()
	if configDir != resDir {
		t.Errorf("ConfigDir 未初始化时应回退到 resources %q，实际 %q", resDir, configDir)
	}
}

// TestAgentWorkspaceDir_已初始化 测试已初始化时的 Agent 工作空间路径
func TestAgentWorkspaceDir_已初始化(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	_ = os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)
	path.ResetCache()

	wsDir := AgentWorkspaceDir()
	expected := filepath.Join(tmpDir, "agent", "workspace")
	if wsDir != expected {
		t.Errorf("AgentWorkspaceDir 期望 %q，实际 %q", expected, wsDir)
	}
}

// TestIsInitialized 测试初始化状态判断
func TestIsInitialized(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	path.ResetCache()

	// 未初始化
	if IsInitialized() {
		t.Error("工作区不应已初始化")
	}

	// 创建 config 目录模拟已初始化
	_ = os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)
	path.ResetCache()

	if !IsInitialized() {
		t.Error("创建 config 目录后应已初始化")
	}
}

// TestResourcesDir_环境变量 测试 ResourcesDir 环境变量优先级
func TestResourcesDir_环境变量(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "myresources")
	_ = os.MkdirAll(resDir, 0o755)

	_ = os.Setenv(EnvResourcesDir, resDir)
	defer func() { _ = os.Unsetenv(EnvResourcesDir) }()

	dir, err := ResourcesDir()
	if err != nil {
		t.Fatalf("ResourcesDir 失败: %v", err)
	}
	if dir != resDir {
		t.Errorf("ResourcesDir 期望 %q，实际 %q", resDir, dir)
	}
}

// TestResourcesDir_不存在 测试 ResourcesDir 找不到时报错
func TestResourcesDir_不存在(t *testing.T) {
	_ = os.Unsetenv(EnvResourcesDir)

	_, err := ResourcesDir()
	if err == nil {
		t.Error("ResourcesDir 找不到时应返回错误")
	}
}

// TestPathHelpers 测试路径辅助函数
func TestPathHelpers(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	// 创建 config 目录使 ConfigDir 不回退
	_ = os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)
	path.ResetCache()

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		// ConfigFile/EnvFile 基于 ConfigDir()
		{"ConfigFile", ConfigFile(), filepath.Join(tmpDir, "config", "config.yaml")},
		{"EnvFile", EnvFile(), filepath.Join(tmpDir, "config", ".env")},
		// 其他路径基于 WorkspaceDir()
		{"AgentRootDir", AgentRootDir(), filepath.Join(tmpDir, "agent")},
		{"AgentMemoryDir", AgentMemoryDir(), filepath.Join(tmpDir, "agent", "workspace", "memory")},
		{"AgentSkillsDir", AgentSkillsDir(), filepath.Join(tmpDir, "agent", "workspace", "skills")},
		{"AgentSessionsDir", AgentSessionsDir(), filepath.Join(tmpDir, "agent", "sessions")},
		{"CheckpointDir", CheckpointDir(), filepath.Join(tmpDir, "agent", ".checkpoint")},
		{"LogsDir", LogsDir(), filepath.Join(tmpDir, "agent", ".logs")},
		{"DeepAgentTodoDir", DeepAgentTodoDir(), filepath.Join(tmpDir, "agent", "workspace", "todo")},
		{"DeepAgentMessagesDir", DeepAgentMessagesDir(), filepath.Join(tmpDir, "agent", "workspace", "messages")},
		{"DeepAgentAgentsDir", DeepAgentAgentsDir(), filepath.Join(tmpDir, "agent", "workspace", "agents")},
		{"DeepAgentHeartbeatPath", DeepAgentHeartbeatPath(), filepath.Join(tmpDir, "agent", "workspace", "HEARTBEAT.md")},
		{"DeepAgentAgentMDPath", DeepAgentAgentMDPath(), filepath.Join(tmpDir, "agent", "workspace", "AGENT.md")},
		{"DeepAgentSoulMDPath", DeepAgentSoulMDPath(), filepath.Join(tmpDir, "agent", "workspace", "SOUL.md")},
		{"DeepAgentIdentityMDPath", DeepAgentIdentityMDPath(), filepath.Join(tmpDir, "agent", "workspace", "IDENTITY.md")},
		{"DeepAgentUserMDPath", DeepAgentUserMDPath(), filepath.Join(tmpDir, "agent", "workspace", "USER.md")},
		{"AgentInteractionsDir", AgentInteractionsDir(), filepath.Join(tmpDir, "agent", "workspace", "interactions")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("期望 %q，实际 %q", tt.expected, tt.got)
			}
		})
	}
}

// TestSetUserHome_重置缓存 测试 SetUserHome 正确重置所有缓存
func TestSetUserHome_重置缓存(t *testing.T) {
	ws1 := WorkspaceDir()

	// 设置新 home
	newHome := "/tmp/uapclaw-reset-test"
	_ = os.MkdirAll(newHome, 0o755)
	defer func() { _ = os.RemoveAll(newHome) }()

	_ = os.Setenv(EnvHome, newHome)
	defer func() { _ = os.Unsetenv(EnvHome) }()
	SetUserHome("")

	ws2 := WorkspaceDir()
	// 两次调用结果应该不同（因为 home 变了）
	if ws1 == ws2 {
		t.Errorf("SetUserHome 后 WorkspaceDir 应该变化，但都是 %q", ws1)
	}
}

// TestGetResolvedPaths_未初始化无resources 测试未初始化且无 resources 时的降级
func TestGetResolvedPaths_未初始化无resources(t *testing.T) {
	tmpDir := t.TempDir()

	_ = os.Setenv(EnvDataDir, filepath.Join(tmpDir, "data"))
	_ = os.Setenv(EnvResourcesDir, filepath.Join(tmpDir, "nonexistent"))
	defer func() {
		_ = os.Unsetenv(EnvDataDir)
		_ = os.Unsetenv(EnvResourcesDir)
	}()
	path.ResetCache()

	// 未初始化且无 resources 时应降级到用户目录
	configDir := ConfigDir()
	if configDir == "" {
		t.Error("ConfigDir 不应为空")
	}
	// 应降级到 WorkspaceDir() 下的 config 目录
	expected := filepath.Join(filepath.Join(tmpDir, "data"), "config")
	if configDir != expected {
		t.Errorf("降级路径期望 %q，实际 %q", expected, configDir)
	}
}

// TestResourcesDir_环境变量不存在 测试 ResourcesDir 环境变量指向不存在的目录
func TestResourcesDir_环境变量不存在(t *testing.T) {
	_ = os.Setenv(EnvResourcesDir, "/nonexistent/path")
	defer func() { _ = os.Unsetenv(EnvResourcesDir) }()

	_, err := ResourcesDir()
	if err == nil {
		t.Error("环境变量指向不存在的目录应返回错误")
	}
}

// TestResourcesDir_可执行文件同目录 测试从可执行文件同目录查找 resources
func TestResourcesDir_可执行文件同目录(t *testing.T) {
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	resDir := filepath.Join(execDir, "resources")
	_ = os.MkdirAll(resDir, 0o755)
	defer func() { _ = os.RemoveAll(resDir) }()

	_ = os.Unsetenv(EnvResourcesDir)

	dir, err := ResourcesDir()
	if err != nil {
		t.Logf("ResourcesDir 在可执行文件目录查找失败: %v", err)
	} else if dir != resDir {
		t.Errorf("ResourcesDir 期望 %q，实际 %q", resDir, dir)
	}
}

// TestConfigFile_基于ConfigDir 测试 ConfigFile 基于 ConfigDir
func TestConfigFile_基于ConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	_ = os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)
	path.ResetCache()

	cf := ConfigFile()
	expected := filepath.Join(ConfigDir(), "config.yaml")
	if cf != expected {
		t.Errorf("ConfigFile 期望 %q，实际 %q", expected, cf)
	}
}

// TestEnvFile_基于ConfigDir 测试 EnvFile 基于 ConfigDir
func TestEnvFile_基于ConfigDir(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv(EnvDataDir, tmpDir)
	defer func() { _ = os.Unsetenv(EnvDataDir) }()
	_ = os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)
	path.ResetCache()

	ef := EnvFile()
	expected := filepath.Join(ConfigDir(), ".env")
	if ef != expected {
		t.Errorf("EnvFile 期望 %q，实际 %q", expected, ef)
	}
}
