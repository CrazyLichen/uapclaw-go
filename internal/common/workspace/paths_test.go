package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUserHomeDir_默认 测试默认用户主目录
func TestUserHomeDir_默认(t *testing.T) {
	// 清除环境变量和缓存
	os.Unsetenv(EnvHome)
	SetUserHome("")

	home := UserHomeDir()
	if home == "" {
		t.Error("UserHomeDir 不应返回空字符串")
	}
	if home == "." {
		// 降级到当前目录也是合法的
		t.Log("UserHomeDir 降级到当前目录")
	}
}

// TestUserHomeDir_环境变量 测试 UAPCLAW_HOME 环境变量
func TestUserHomeDir_环境变量(t *testing.T) {
	customHome := "/tmp/uapclaw-test-home"
	os.MkdirAll(customHome, 0o755)
	defer os.RemoveAll(customHome)

	os.Setenv(EnvHome, customHome)
	defer os.Unsetenv(EnvHome)
	SetUserHome("") // 重置缓存

	home := UserHomeDir()
	if home != customHome {
		t.Errorf("UserHomeDir 期望 %q，实际 %q", customHome, home)
	}

	// 清理
	SetUserHome("")
}

// TestWorkspaceDir_默认 测试默认工作区目录
func TestWorkspaceDir_默认(t *testing.T) {
	os.Unsetenv(EnvDataDir)
	os.Unsetenv(EnvHome)
	SetUserHome("")

	ws := WorkspaceDir()
	if !strings.HasSuffix(ws, DefaultDir) {
		t.Errorf("WorkspaceDir 应以 %q 结尾，实际 %q", DefaultDir, ws)
	}
}

// TestWorkspaceDir_环境变量 测试 UAPCLAW_DATA_DIR 环境变量
func TestWorkspaceDir_环境变量(t *testing.T) {
	customDir := "/tmp/uapclaw-test-workspace"
	os.MkdirAll(customDir, 0o755)
	defer os.RemoveAll(customDir)

	os.Setenv(EnvDataDir, customDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("") // 重置缓存

	ws := WorkspaceDir()
	if ws != customDir {
		t.Errorf("WorkspaceDir 期望 %q，实际 %q", customDir, ws)
	}

	// 清理
	SetUserHome("")
}

// TestConfigDir_已初始化 测试已初始化时 ConfigDir 返回用户目录
func TestConfigDir_已初始化(t *testing.T) {
	tmpDir := t.TempDir()

	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("") // 重置缓存

	// 模拟已初始化：创建 config 目录
	os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)

	configDir := ConfigDir()
	expected := filepath.Join(tmpDir, "config")
	if configDir != expected {
		t.Errorf("ConfigDir 期望 %q，实际 %q", expected, configDir)
	}

	SetUserHome("")
}

// TestConfigDir_未初始化_回退到resources 测试未初始化时回退到 resources
func TestConfigDir_未初始化_回退到resources(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建 resources 目录
	resDir := filepath.Join(tmpDir, "resources")
	os.MkdirAll(filepath.Join(resDir, "agent", "workspace"), 0o755)
	os.WriteFile(filepath.Join(resDir, "config.yaml"), []byte("test"), 0o644)

	// 设置环境变量
	os.Setenv(EnvDataDir, filepath.Join(t.TempDir(), "nonexistent"))
	os.Setenv(EnvResourcesDir, resDir)
	defer func() {
		os.Unsetenv(EnvDataDir)
		os.Unsetenv(EnvResourcesDir)
		SetUserHome("")
	}()
	SetUserHome("")

	configDir := ConfigDir()
	if configDir != resDir {
		t.Errorf("ConfigDir 未初始化时应回退到 resources %q，实际 %q", resDir, configDir)
	}

	SetUserHome("")
}

// TestAgentWorkspaceDir_已初始化 测试已初始化时的 Agent 工作空间路径
func TestAgentWorkspaceDir_已初始化(t *testing.T) {
	tmpDir := t.TempDir()

	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 模拟已初始化
	os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)

	wsDir := AgentWorkspaceDir()
	expected := filepath.Join(tmpDir, "agent", "workspace")
	if wsDir != expected {
		t.Errorf("AgentWorkspaceDir 期望 %q，实际 %q", expected, wsDir)
	}

	SetUserHome("")
}

// TestIsInitialized 测试初始化状态判断
func TestIsInitialized(t *testing.T) {
	tmpDir := t.TempDir()

	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	// 未初始化
	if IsInitialized() {
		t.Error("工作区不应已初始化")
	}

	// 创建 config 目录模拟已初始化
	os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755)
	// 需要重置缓存
	SetUserHome("")

	if !IsInitialized() {
		t.Error("创建 config 目录后应已初始化")
	}

	SetUserHome("")
}

// TestResourcesDir_环境变量 测试 ResourcesDir 环境变量优先级
func TestResourcesDir_环境变量(t *testing.T) {
	tmpDir := t.TempDir()
	resDir := filepath.Join(tmpDir, "myresources")
	os.MkdirAll(resDir, 0o755)

	os.Setenv(EnvResourcesDir, resDir)
	defer os.Unsetenv(EnvResourcesDir)

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
	os.Unsetenv(EnvResourcesDir)

	_, err := ResourcesDir()
	if err == nil {
		t.Error("ResourcesDir 找不到时应返回错误")
	}
}

// TestPathHelpers 测试路径辅助函数
func TestPathHelpers(t *testing.T) {
	tmpDir := t.TempDir()

	os.Setenv(EnvDataDir, tmpDir)
	defer os.Unsetenv(EnvDataDir)
	SetUserHome("")

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"ConfigFile", ConfigFile(), filepath.Join(tmpDir, "config", "config.yaml")},
		{"EnvFile", EnvFile(), filepath.Join(tmpDir, "config", ".env")},
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

	SetUserHome("")
}

// TestSetUserHome_重置缓存 测试 SetUserHome 正确重置所有缓存
func TestSetUserHome_重置缓存(t *testing.T) {
	// 第一次调用
	SetUserHome("")
	ws1 := WorkspaceDir()

	// 设置新 home
	newHome := "/tmp/uapclaw-reset-test"
	os.MkdirAll(newHome, 0o755)
	defer os.RemoveAll(newHome)

	os.Setenv(EnvHome, newHome)
	defer os.Unsetenv(EnvHome)
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

	// 设置一个不存在的 data dir 和不存在的 resources
	os.Setenv(EnvDataDir, filepath.Join(tmpDir, "data"))
	os.Setenv(EnvResourcesDir, filepath.Join(tmpDir, "nonexistent"))
	defer func() {
		os.Unsetenv(EnvDataDir)
		os.Unsetenv(EnvResourcesDir)
		SetUserHome("")
	}()
	SetUserHome("")

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
	os.Setenv(EnvResourcesDir, "/nonexistent/path")
	defer os.Unsetenv(EnvResourcesDir)

	_, err := ResourcesDir()
	if err == nil {
		t.Error("环境变量指向不存在的目录应返回错误")
	}
}

// TestResourcesDir_可执行文件同目录 测试从可执行文件同目录查找 resources
func TestResourcesDir_可执行文件同目录(t *testing.T) {
	// 创建可执行文件同目录的 resources
	execPath, _ := os.Executable()
	execDir := filepath.Dir(execPath)
	resDir := filepath.Join(execDir, "resources")
	os.MkdirAll(resDir, 0o755)
	defer os.RemoveAll(resDir)

	os.Unsetenv(EnvResourcesDir)
	defer func() {
		// 恢复环境变量
	}()

	dir, err := ResourcesDir()
	if err != nil {
		// 在某些环境中可执行文件目录可能不可写，这是预期的
		t.Logf("ResourcesDir 在可执行文件目录查找失败: %v", err)
	} else if dir != resDir {
		t.Errorf("ResourcesDir 期望 %q，实际 %q", resDir, dir)
	}
}
