# 启动流程差异修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 uapclaw app 启动流程与 Python 的 4 个差异：提取 utils/path 消除循环依赖、ConfigFile 基于 ConfigDir、ParseCustomHeaders 返回具体类型、补齐日志

**Architecture:** 将 workspace/paths.go 中的纯路径计算逻辑提取到 utils/path 子包（零依赖），workspace 变为薄代理+日志层，logger/config 删除各自的硬编码路径函数改用 path 包

**Tech Stack:** Go, sync.Once 缓存, filepath 路径操作

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `internal/common/utils/path/doc.go` | Create | 包文档 |
| `internal/common/utils/path/paths.go` | Create | 纯路径计算（从 workspace/paths.go 迁移，去掉 logger 调用） |
| `internal/common/utils/path/paths_test.go` | Create | 路径计算测试（从 workspace/paths_test.go 迁移） |
| `internal/common/workspace/paths.go` | Modify | 改为调用 path 包 + 补日志，常量改为引用 path 包 |
| `internal/common/workspace/paths_test.go` | Modify | 适配代理模式 |
| `internal/common/workspace/doc.go` | Modify | 更新文件目录 |
| `internal/common/logger/logger.go` | Modify | 删除 resolveConfigFilePath()，改用 path.ConfigFile() |
| `internal/common/logger/logger_test.go` | Modify | 删除 TestResolveConfigFilePath，适配 path |
| `internal/common/config/config.go` | Modify | 删除 resolveConfigPath()，改用 path.ConfigFile() |
| `internal/common/config/config_test.go` | Modify | 适配新路径解析 |
| `internal/common/config/doc.go` | Modify | 更新文件目录 |
| `internal/common/config/normalize.go` | Modify | ParseCustomHeaders 返回 map[string]any + 补 Warn 日志 |
| `internal/common/config/normalize_test.go` | Modify | 适配返回类型变更 |

---

### Task 1: 创建 utils/path 包 — doc.go + paths.go

**Files:**
- Create: `internal/common/utils/path/doc.go`
- Create: `internal/common/utils/path/paths.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package path 提供纯路径计算函数，无日志依赖。
//
// 本包从 workspace 包提取路径解析逻辑，作为 workspace、logger、config 的共同依赖，
// 消除 logger↔workspace 循环依赖和代码重复。
//
// 所有函数为纯计算（不含 logger 调用），workspace 在调用后自行补日志。
// 缓存使用 sync.Once 保证幂等，与 Python _resolve_paths() 的 _initialized 全局变量行为对齐。
//
// 文件目录：
//
//	path/
//	├── doc.go      # 包文档
//	├── paths.go    # 路径解析与回退逻辑、18个路径辅助函数
//	└── paths_test.go # 路径计算测试
//
// 对应 Python 代码：jiuwenswarm/common/utils.py（路径管理、_resolve_paths、prepare_workspace）
package path
```

- [ ] **Step 2: 创建 paths.go**

将 `workspace/paths.go` 中的全部逻辑迁移过来，关键修改：
1. package 改为 `path`
2. 删除 `logger` import 和所有 `logger.*` 调用
3. `getResolvedPaths()` 中 `ResourcesDir()` 找不到时不打 Warn 日志，仅设置降级值
4. 新增 `ResetCache()` 导出函数（供测试重置缓存）
5. `ConfigFile()` 改为 `ConfigDir() + "/config.yaml"`（D2 修改）
6. `EnvFile()` 改为 `ConfigDir() + "/.env"`（D2 修改）
7. 常量从 workspace 迁移：`EnvHome`、`EnvDataDir`、`EnvResourcesDir`、`DefaultDir`、`DefaultInstancesDir`
8. 结构体 `ResolvedPaths` 从 workspace 迁移

完整代码：

```go
package path

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResolvedPaths 缓存已解析的 ConfigDir 和 AgentWorkspaceDir。
//
// 对应 Python: jiuwenswarm/common/utils.py 中的 _config_dir / _workspace_dir 全局变量。
// 未初始化时（~/.uapclaw/config/ 不存在）回退到 resources 目录，
// 与 Python _resolve_paths() 行为对齐。
type ResolvedPaths struct {
	// ConfigDir 配置目录
	ConfigDir string
	// WorkspaceDir Agent 工作空间目录
	WorkspaceDir string
	// Fallback true 表示 ConfigDir 来自 resources 回退而非用户目录
	Fallback bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// EnvHome 自定义用户主目录环境变量。
	// 对应 Python: JIUWENSWARM_HOME
	EnvHome = "UAPCLAW_HOME"

	// EnvDataDir 自定义数据根目录环境变量。
	// 对应 Python: JIUWENSWARM_DATA_DIR
	EnvDataDir = "UAPCLAW_DATA_DIR"

	// EnvResourcesDir 自定义资源目录环境变量。
	// Go 版特有，Python 通过 __file__ 反推包位置，Go 用环境变量覆盖。
	EnvResourcesDir = "UAPCLAW_RESOURCES_DIR"

	// DefaultDir 默认工作区目录名。
	// 对应 Python: ".jiuwenswarm"
	DefaultDir = ".uapclaw"

	// DefaultInstancesDir 默认命名实例根目录名。
	// 对应 Python: ".jiuwenswarm-instances"
	DefaultInstancesDir = ".uapclaw-instances"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// userHomeOnce 保证 UserHomeDir 只解析一次。
	userHomeOnce sync.Once
	userHomeVal  string

	// workspaceOnce 保证 WorkspaceDir 只解析一次。
	workspaceOnce sync.Once
	workspaceVal  string

	// resolveOnce 保证 resolvePaths 只执行一次。
	resolveOnce   sync.Once
	resolvedPaths *ResolvedPaths
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UserHomeDir 获取用户主目录。
//
// 优先级（与 Python get_user_home() 对齐）：
//  1. 缓存值（首次调用后缓存）
//  2. UAPCLAW_HOME 环境变量
//  3. os.UserHomeDir()
func UserHomeDir() string {
	userHomeOnce.Do(func() {
		if env := os.Getenv(EnvHome); env != "" {
			userHomeVal = env
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			// 降级到当前目录
			userHomeVal = "."
			return
		}
		userHomeVal = home
	})
	return userHomeVal
}

// SetUserHome 设置自定义主目录并重置所有缓存。
//
// 调用后 WorkspaceDir()、ConfigDir()、AgentWorkspaceDir() 都会重新解析。
// 对应 Python: set_user_home(path)
func SetUserHome(p string) {
	userHomeVal = p
	// 重置所有缓存
	userHomeOnce = sync.Once{}
	workspaceOnce = sync.Once{}
	resolveOnce = sync.Once{}
	resolvedPaths = nil
}

// ResetCache 重置所有路径缓存（供测试使用）。
func ResetCache() {
	userHomeOnce = sync.Once{}
	userHomeVal = ""
	workspaceOnce = sync.Once{}
	workspaceVal = ""
	resolveOnce = sync.Once{}
	resolvedPaths = nil
}

// WorkspaceDir 获取数据根目录（~/.uapclaw/）。
//
// 此函数不受未初始化回退影响，始终返回用户目录下的路径。
// 对应 Python: get_user_workspace_dir()
//
// 优先级：
//  1. 缓存值
//  2. UAPCLAW_DATA_DIR 环境变量（用于多实例隔离）
//  3. UserHomeDir() / ".uapclaw"
func WorkspaceDir() string {
	workspaceOnce.Do(func() {
		if env := os.Getenv(EnvDataDir); env != "" {
			workspaceVal = env
			return
		}
		workspaceVal = filepath.Join(UserHomeDir(), DefaultDir)
	})
	return workspaceVal
}

// ConfigDir 获取配置目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/config/，
// 未初始化时回退到 ResourcesDir()。
// 对应 Python: get_config_dir()
func ConfigDir() string {
	paths := getResolvedPaths()
	return paths.ConfigDir
}

// AgentWorkspaceDir 获取 Agent 工作空间目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/agent/workspace/，
// 未初始化时回退到 ResourcesDir()/agent/workspace。
// 对应 Python: get_workspace_dir()
func AgentWorkspaceDir() string {
	paths := getResolvedPaths()
	return paths.WorkspaceDir
}

// IsInitialized 检查工作区是否已初始化。
//
// 判断依据：WorkspaceDir()/config/ 目录是否存在。
// 对应 Python: _resolve_paths() 中 user_config_dir.exists() 判断
func IsInitialized() bool {
	configDir := filepath.Join(WorkspaceDir(), "config")
	return dirExists(configDir)
}

// ResourcesDir 获取外部资源目录。
//
// 三级回退（Go 版特有，Python 通过 __file__ 反推包位置）：
//  1. UAPCLAW_RESOURCES_DIR 环境变量
//  2. 可执行文件同目录的 resources/
//  3. 当前工作目录的 resources/
//
// 找不到时返回错误。
func ResourcesDir() (string, error) {
	// 优先使用环境变量
	if env := os.Getenv(EnvResourcesDir); env != "" {
		if dirExists(env) {
			return env, nil
		}
		return "", fmt.Errorf("UAPCLAW_RESOURCES_DIR 指向的目录不存在: %s", env)
	}

	// 尝试可执行文件同目录
	execPath, err := os.Executable()
	if err == nil {
		resDir := filepath.Join(filepath.Dir(execPath), "resources")
		if dirExists(resDir) {
			return resDir, nil
		}
	}

	// 尝试当前工作目录
	if cwd, err := os.Getwd(); err == nil {
		resDir := filepath.Join(cwd, "resources")
		if dirExists(resDir) {
			return resDir, nil
		}
	}

	return "", fmt.Errorf("找不到 resources 目录，请设置 UAPCLAW_RESOURCES_DIR 环境变量或确认 resources/ 目录存在于可执行文件同目录或当前目录")
}

// ConfigFile 返回配置文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_file()。
func ConfigFile() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// EnvFile 返回环境变量文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_dir() 同级 .env。
func EnvFile() string {
	return filepath.Join(ConfigDir(), ".env")
}

// 以下路径辅助函数始终基于 WorkspaceDir() 派生，不受回退影响。
// 对应 Python: 各 get_xxx_dir() 函数

// AgentRootDir 返回 Agent 根目录：WorkspaceDir()/agent
func AgentRootDir() string {
	return filepath.Join(WorkspaceDir(), "agent")
}

// AgentMemoryDir 返回 Agent 记忆目录：WorkspaceDir()/agent/workspace/memory
func AgentMemoryDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "memory")
}

// AgentSkillsDir 返回 Agent 技能目录：WorkspaceDir()/agent/workspace/skills
func AgentSkillsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "skills")
}

// AgentSessionsDir 返回 Agent 会话目录：WorkspaceDir()/agent/sessions
func AgentSessionsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "sessions")
}

// AgentInteractionsDir 返回 Agent 交互目录：WorkspaceDir()/agent/workspace/interactions
func AgentInteractionsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "interactions")
}

// CheckpointDir 返回检查点目录：WorkspaceDir()/agent/.checkpoint
func CheckpointDir() string {
	return filepath.Join(WorkspaceDir(), "agent", ".checkpoint")
}

// LogsDir 返回日志目录：WorkspaceDir()/agent/.logs
func LogsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", ".logs")
}

// DeepAgentTodoDir 返回 DeepAgent 待办目录：WorkspaceDir()/agent/workspace/todo
func DeepAgentTodoDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "todo")
}

// DeepAgentMessagesDir 返回 DeepAgent 消息目录：WorkspaceDir()/agent/workspace/messages
func DeepAgentMessagesDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "messages")
}

// DeepAgentAgentsDir 返回 DeepAgent 子 Agent 目录：WorkspaceDir()/agent/workspace/agents
func DeepAgentAgentsDir() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "agents")
}

// DeepAgentHeartbeatPath 返回 DeepAgent 心跳文件路径：WorkspaceDir()/agent/workspace/HEARTBEAT.md
func DeepAgentHeartbeatPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "HEARTBEAT.md")
}

// DeepAgentAgentMDPath 返回 DeepAgent Agent 描述文件路径：WorkspaceDir()/agent/workspace/AGENT.md
func DeepAgentAgentMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "AGENT.md")
}

// DeepAgentSoulMDPath 返回 DeepAgent Soul 文件路径：WorkspaceDir()/agent/workspace/SOUL.md
func DeepAgentSoulMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "SOUL.md")
}

// DeepAgentIdentityMDPath 返回 DeepAgent Identity 文件路径：WorkspaceDir()/agent/workspace/IDENTITY.md
func DeepAgentIdentityMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "IDENTITY.md")
}

// DeepAgentUserMDPath 返回 DeepAgent User 文件路径：WorkspaceDir()/agent/workspace/USER.md
func DeepAgentUserMDPath() string {
	return filepath.Join(WorkspaceDir(), "agent", "workspace", "USER.md")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getResolvedPaths 获取已解析的路径（带回退逻辑）。
//
// 对应 Python: _resolve_paths()，核心逻辑：
//   - ~/.uapclaw/config/ 存在 → 已初始化模式，指向用户目录
//   - 不存在 → 未初始化模式，回退到 resources 目录
//
// 纯计算，不含日志调用。workspace 在调用后根据 Fallback 字段补日志。
func getResolvedPaths() *ResolvedPaths {
	resolveOnce.Do(func() {
		workspaceDir := WorkspaceDir()
		userConfigDir := filepath.Join(workspaceDir, "config")
		userWorkspaceDir := filepath.Join(workspaceDir, "agent", "workspace")

		if dirExists(userConfigDir) {
			// 已初始化：指向用户目录
			resolvedPaths = &ResolvedPaths{
				ConfigDir:    userConfigDir,
				WorkspaceDir: userWorkspaceDir,
				Fallback:     false,
			}
		} else {
			// 未初始化：回退到 resources 目录
			resDir, err := ResourcesDir()
			if err != nil {
				// resources 也找不到，降级为用户目录（后续操作因目录不存在自然报错）
				resolvedPaths = &ResolvedPaths{
					ConfigDir:    userConfigDir,
					WorkspaceDir: userWorkspaceDir,
					Fallback:     true,
				}
				return
			}
			resolvedPaths = &ResolvedPaths{
				ConfigDir:    resDir,
				WorkspaceDir: filepath.Join(resDir, "agent", "workspace"),
				Fallback:     true,
			}
		}
	})
	return resolvedPaths
}

// dirExists 检查目录是否存在。
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}
```

关键设计决策：
- 新增 `Fallback` 字段让 workspace 知道是否走了回退路径，自行补日志
- `getResolvedPaths()` 不含 logger 调用，纯计算
- `ConfigFile()` 和 `EnvFile()` 基于 `ConfigDir()`（D2 修改）

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/common/utils/path/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/common/utils/path/doc.go internal/common/utils/path/paths.go
git commit -m "feat(path): 新建 utils/path 包，提取纯路径计算逻辑（无 logger 依赖）"
```

---

### Task 2: 创建 utils/path 测试

**Files:**
- Create: `internal/common/utils/path/paths_test.go`

- [ ] **Step 1: 创建 paths_test.go**

从 `workspace/paths_test.go` 迁移测试逻辑，关键修改：
1. package 改为 `path`
2. 所有缓存重置改用 `ResetCache()`
3. `ConfigFile()` 测试基于 `ConfigDir()` 验证
4. 新增 `TestConfigFile_基于ConfigDir` 和 `TestEnvFile_基于ConfigDir`

```go
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
	SetUserHome(newHome)

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
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/common/utils/path/... -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/common/utils/path/paths_test.go
git commit -m "test(path): 添加 utils/path 包测试"
```

---

### Task 3: workspace 改为调用 path 包

**Files:**
- Modify: `internal/common/workspace/paths.go`
- Modify: `internal/common/workspace/doc.go`

- [ ] **Step 1: 重写 workspace/paths.go 为薄代理层**

关键修改：
1. 删除所有路径计算逻辑和缓存变量（已迁移到 path 包）
2. 所有函数改为调用 `path.Xxx()`
3. 常量改为从 path 包重导出（`const EnvHome = path.EnvHome` 等）
4. `ConfigDir()` 和 `AgentWorkspaceDir()` 调用 path 后根据 `Fallback` 字段补日志
5. 删除 `ResolvedPaths` 结构体（改用 `path.ResolvedPaths`）
6. 删除 `getResolvedPaths()` 和 `dirExists()` 函数

```go
package workspace

import (
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

// 重导出 path 包常量，保持 workspace 外部 API 不变。
const (
	// EnvHome 自定义用户主目录环境变量。
	EnvHome = path.EnvHome
	// EnvDataDir 自定义数据根目录环境变量。
	EnvDataDir = path.EnvDataDir
	// EnvResourcesDir 自定义资源目录环境变量。
	EnvResourcesDir = path.EnvResourcesDir
	// DefaultDir 默认工作区目录名。
	DefaultDir = path.DefaultDir
	// DefaultInstancesDir 默认命名实例根目录名。
	DefaultInstancesDir = path.DefaultInstancesDir
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UserHomeDir 获取用户主目录。
func UserHomeDir() string {
	return path.UserHomeDir()
}

// SetUserHome 设置自定义主目录并重置所有缓存。
func SetUserHome(p string) {
	path.SetUserHome(p)
}

// WorkspaceDir 获取数据根目录（~/.uapclaw/）。
func WorkspaceDir() string {
	return path.WorkspaceDir()
}

// ConfigDir 获取配置目录。
//
// 有未初始化回退：已初始化时返回 ~/.uapclaw/config/，
// 未初始化时回退到 ResourcesDir()。
// 对应 Python: get_config_dir()
func ConfigDir() string {
	dir := path.ConfigDir()
	// 根据 path 包的回退状态补日志
	logFallbackIfNeeded()
	return dir
}

// AgentWorkspaceDir 获取 Agent 工作空间目录。
func AgentWorkspaceDir() string {
	dir := path.AgentWorkspaceDir()
	logFallbackIfNeeded()
	return dir
}

// IsInitialized 检查工作区是否已初始化。
func IsInitialized() bool {
	return path.IsInitialized()
}

// ResourcesDir 获取外部资源目录。
func ResourcesDir() (string, error) {
	return path.ResourcesDir()
}

// ConfigFile 返回配置文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_file()。
func ConfigFile() string {
	return path.ConfigFile()
}

// EnvFile 返回环境变量文件路径。
//
// 基于 ConfigDir()（有 resources 回退），对齐 Python get_config_dir() 同级 .env。
func EnvFile() string {
	return path.EnvFile()
}

// AgentRootDir 返回 Agent 根目录：WorkspaceDir()/agent
func AgentRootDir() string {
	return path.AgentRootDir()
}

// AgentMemoryDir 返回 Agent 记忆目录：WorkspaceDir()/agent/workspace/memory
func AgentMemoryDir() string {
	return path.AgentMemoryDir()
}

// AgentSkillsDir 返回 Agent 技能目录：WorkspaceDir()/agent/workspace/skills
func AgentSkillsDir() string {
	return path.AgentSkillsDir()
}

// AgentSessionsDir 返回 Agent 会话目录：WorkspaceDir()/agent/sessions
func AgentSessionsDir() string {
	return path.AgentSessionsDir()
}

// AgentInteractionsDir 返回 Agent 交互目录：WorkspaceDir()/agent/workspace/interactions
func AgentInteractionsDir() string {
	return path.AgentInteractionsDir()
}

// CheckpointDir 返回检查点目录：WorkspaceDir()/agent/.checkpoint
func CheckpointDir() string {
	return path.CheckpointDir()
}

// LogsDir 返回日志目录：WorkspaceDir()/agent/.logs
func LogsDir() string {
	return path.LogsDir()
}

// DeepAgentTodoDir 返回 DeepAgent 待办目录：WorkspaceDir()/agent/workspace/todo
func DeepAgentTodoDir() string {
	return path.DeepAgentTodoDir()
}

// DeepAgentMessagesDir 返回 DeepAgent 消息目录：WorkspaceDir()/agent/workspace/messages
func DeepAgentMessagesDir() string {
	return path.DeepAgentMessagesDir()
}

// DeepAgentAgentsDir 返回 DeepAgent 子 Agent 目录：WorkspaceDir()/agent/workspace/agents
func DeepAgentAgentsDir() string {
	return path.DeepAgentAgentsDir()
}

// DeepAgentHeartbeatPath 返回 DeepAgent 心跳文件路径：WorkspaceDir()/agent/workspace/HEARTBEAT.md
func DeepAgentHeartbeatPath() string {
	return path.DeepAgentHeartbeatPath()
}

// DeepAgentAgentMDPath 返回 DeepAgent Agent 描述文件路径：WorkspaceDir()/agent/workspace/AGENT.md
func DeepAgentAgentMDPath() string {
	return path.DeepAgentAgentMDPath()
}

// DeepAgentSoulMDPath 返回 DeepAgent Soul 文件路径：WorkspaceDir()/agent/workspace/SOUL.md
func DeepAgentSoulMDPath() string {
	return path.DeepAgentSoulMDPath()
}

// DeepAgentIdentityMDPath 返回 DeepAgent Identity 文件路径：WorkspaceDir()/agent/workspace/IDENTITY.md
func DeepAgentIdentityMDPath() string {
	return path.DeepAgentIdentityMDPath()
}

// DeepAgentUserMDPath 返回 DeepAgent User 文件路径：WorkspaceDir()/agent/workspace/USER.md
func DeepAgentUserMDPath() string {
	return path.DeepAgentUserMDPath()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// logFallbackIfNeeded 检查 path 包的回退状态并补日志。
//
// path 包是纯计算不含日志，workspace 负责在回退时输出日志。
var fallbackLogged bool

func logFallbackIfNeeded() {
	if fallbackLogged {
		return
	}
	// 通过比较 ConfigDir 和 WorkspaceDir/config 判断是否回退
	configDir := path.ConfigDir()
	userConfigDir := filepath.Join(path.WorkspaceDir(), "config")
	if configDir != userConfigDir {
		logger.Info(logComponent).Str("config_dir", configDir).Msg("工作区未初始化，回退到 resources 目录")
		fallbackLogged = true
	}
}
```

注意：`logFallbackIfNeeded` 用 `fallbackLogged` 防止重复打日志，这是简化实现。更精确的实现可以用 path 包的 `ResolvedPaths.Fallback` 字段，但需要 path 包导出获取函数或 workspace 直接调用非导出函数（不可行）。当前通过比较路径来判断是否回退。

- [ ] **Step 2: 更新 workspace/doc.go 文件目录**

在 doc.go 的文件目录中添加 path 子包说明，更新 paths.go 的职责描述。

- [ ] **Step 3: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/common/workspace/...`
Expected: PASS

- [ ] **Step 4: 运行 workspace 测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/common/workspace/... -v -count=1`
Expected: PASS（可能需要微调测试中的缓存重置方式，改用 path.ResetCache()）

- [ ] **Step 5: Commit**

```bash
git add internal/common/workspace/paths.go internal/common/workspace/doc.go
git commit -m "refactor(workspace): paths.go 改为调用 utils/path 包，消除内部路径计算逻辑"
```

---

### Task 4: workspace 测试适配

**Files:**
- Modify: `internal/common/workspace/paths_test.go`

- [ ] **Step 1: 适配 paths_test.go**

workspace 测试中的缓存重置改用 `path.ResetCache()`。由于 workspace 的函数现在是代理调用，测试逻辑基本不变，但需要确保：
1. 每个测试前调用 `path.ResetCache()` 重置缓存
2. `ConfigFile()` 测试验证基于 `ConfigDir()` 的行为
3. `EnvFile()` 测试验证基于 `ConfigDir()` 的行为

具体修改：在每个测试的 `defer` 或 setup 中，将原来的 `SetUserHome` 重置改为同时调用 `path.ResetCache()`。

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/common/workspace/... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/common/workspace/paths_test.go
git commit -m "test(workspace): 适配 paths_test.go 使用 path.ResetCache()"
```

---

### Task 5: logger 删除 resolveConfigFilePath，改用 path.ConfigFile

**Files:**
- Modify: `internal/common/logger/logger.go`
- Modify: `internal/common/logger/logger_test.go`

- [ ] **Step 1: 修改 logger.go**

1. 删除 `resolveConfigFilePath()` 函数（L388-L403）
2. `loadLoggingConfigFromYAML()` 中 `configPath := resolveConfigFilePath()` 改为 `configPath := path.ConfigFile()`
3. import 新增 `"github.com/uapclaw/uapclaw-go/internal/common/utils/path"`
4. 删除 `"path/filepath"` import（如果不再使用）

修改 `loadLoggingConfigFromYAML()`：

```go
// loadLoggingConfigFromYAML 读取 config.yaml 中的 logging 段。
//
// 不做 ${VAR:-default} 解析，仅取字面量值。
// 通过 utils/path 包获取配置文件路径，对齐 Python: get_config_file()。
// 对应 Python: _load_logging_config_from_yaml()
func loadLoggingConfigFromYAML() *config.LoggingConfig {
	configPath := path.ConfigFile()
	if configPath == "" {
		return nil
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var raw map[string]any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil
	}

	loggingVal, ok := raw["logging"]
	if !ok {
		return nil
	}

	bytes, err := yaml.Marshal(loggingVal)
	if err != nil {
		return nil
	}

	var cfg config.LoggingConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return nil
	}

	return &cfg
}
```

- [ ] **Step 2: 修改 logger_test.go**

1. 删除 `TestResolveConfigFilePath` 测试函数（L469-L486）
2. `TestWithConfigFile` 中 `t.Setenv("UAPCLAW_CONFIG_DIR", ...)` 改为 `t.Setenv("UAPCLAW_DATA_DIR", ...)` + 创建 config 子目录（对齐 path.ConfigFile 逻辑）
3. `TestWithConfigFile_文件不存在` 类似调整

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/common/logger/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/common/logger/logger.go internal/common/logger/logger_test.go
git commit -m "refactor(logger): 删除 resolveConfigFilePath()，改用 path.ConfigFile() 消除代码重复"
```

---

### Task 6: config 删除 resolveConfigPath，改用 path.ConfigFile

**Files:**
- Modify: `internal/common/config/config.go`
- Modify: `internal/common/config/config_test.go`
- Modify: `internal/common/config/doc.go`

- [ ] **Step 1: 修改 config.go**

1. 删除 `resolveConfigPath()` 函数（L246-L264）
2. 删除 `EnvConfigDir` 常量（不再需要，path 包不使用 `UAPCLAW_CONFIG_DIR`）
3. `New()` 中 `path == ""` 时改为使用 `path.ConfigFile()`：

```go
func New(p string, opts ...Option) (*Config, error) {
	cfg := &Config{}

	// 应用选项
	for _, opt := range opts {
		opt(cfg)
	}

	// 解析配置文件路径
	if p == "" {
		p = path.ConfigFile()
	}
	cfg.path = p

	// 初始化空数据
	cfg.raw = make(map[string]any)
	cfg.data = make(map[string]any)

	return cfg, nil
}
```

4. import 新增 `"github.com/uapclaw/uapclaw-go/internal/common/utils/path"`
5. 删除 `"os"` import 中的 `os.Getenv` 使用（如果不再需要）

- [ ] **Step 2: 修改 config_test.go**

`New("")` 调用在测试中会使用 `path.ConfigFile()`，需要确保测试环境中 `UAPCLAW_DATA_DIR` 正确指向测试目录。检查 `testdata/` 测试是否需要调整。

- [ ] **Step 3: 更新 doc.go 文件目录**

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/common/config/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/common/config/config.go internal/common/config/config_test.go internal/common/config/doc.go
git commit -m "refactor(config): 删除 resolveConfigPath()，改用 path.ConfigFile() 消除代码重复"
```

---

### Task 7: D3+D4 — ParseCustomHeaders 返回 map[string]any + 补 Warn 日志

**Files:**
- Modify: `internal/common/config/normalize.go`
- Modify: `internal/common/config/normalize_test.go`

- [ ] **Step 1: 修改 normalize.go**

1. `ParseCustomHeaders` 返回类型从 `any` 改为 `map[string]any`
2. 添加 `logger` import 和 `logger.Warn` 日志
3. JSON 解析失败时补 Warn 日志（对齐 Python）

```go
package config

import (
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeConfig 配置后处理，将需要结构化的字段解析为原生类型。
//
// 对应 Python: _normalize_config(config)
//  1. models.*.model_client_config.custom_headers — JSON 字符串 → map
//  2. react.model_client_config.custom_headers — JSON 字符串 → map
//  3. channels.web.send_file_allowed — 默认值 true
func NormalizeConfig(data map[string]any) {
	if data == nil {
		return
	}

	// 1. 解析 models 中各条目的 custom_headers
	models, ok := data["models"].(map[string]any)
	if ok {
		for _, entry := range models {
			entryMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			mcc, ok := entryMap["model_client_config"].(map[string]any)
			if !ok {
				continue
			}
			if raw, exists := mcc["custom_headers"]; exists {
				mcc["custom_headers"] = ParseCustomHeaders(raw)
			}
		}
	}

	// 2. 解析 react.model_client_config.custom_headers
	react, ok := data["react"].(map[string]any)
	if ok {
		if mcc, ok := react["model_client_config"].(map[string]any); ok {
			if raw, exists := mcc["custom_headers"]; exists {
				mcc["custom_headers"] = ParseCustomHeaders(raw)
			}
		}
	}

	// 3. 设置 channels.web.send_file_allowed 默认值
	channels, ok := data["channels"].(map[string]any)
	if ok {
		web, ok := channels["web"].(map[string]any)
		if ok {
			if _, exists := web["send_file_allowed"]; !exists {
				web["send_file_allowed"] = true
			}
		} else {
			channels["web"] = map[string]any{"send_file_allowed": true}
		}
	}
}

// ParseCustomHeaders 解析 custom_headers 配置，支持 JSON 字符串格式。
//
// 如果输入已经是 map 类型则原样返回；如果是 JSON 字符串则解析为 map；
// 其他类型或解析失败返回 nil。
// 对应 Python: _parse_custom_headers(value)
func ParseCustomHeaders(value any) map[string]any {
	if value == nil {
		return nil
	}

	// 已经是 map，直接返回
	if m, ok := value.(map[string]any); ok {
		return m
	}

	// 字符串类型，尝试 JSON 解析
	s, ok := value.(string)
	if !ok {
		return nil
	}
	if s == "" {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		// 对齐 Python: logger.warning(f"custom_headers JSON parse failed: {e}")
		logger.Warn(logger.ComponentCommon).
			Str("value", truncateString(s, 100)).
			Err(err).
			Msg("custom_headers JSON 解析失败")
		return nil
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncateString 截断字符串到指定长度。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
```

- [ ] **Step 2: 修改 normalize_test.go**

1. `TestParseCustomHeaders` 中 `result.(map[string]any)` 类型断言去掉（返回值已是 `map[string]any`）
2. `TestNormalizeConfig` 中 `mcc["custom_headers"].(map[string]any)` 类型断言去掉
3. `TestNormalizeConfig_与Load集成` 中同理

具体修改 `TestParseCustomHeaders` 中 "JSON字符串解析为map" 用例：

```go
t.Run("JSON字符串解析为map", func(t *testing.T) {
	input := `{"X-Trace-Id": "abc", "X-Request": "req"}`
	result := ParseCustomHeaders(input)
	// 返回值已是 map[string]any，无需断言
	assert.Equal(t, "abc", result["X-Trace-Id"])
	assert.Equal(t, "req", result["X-Request"])
})
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/common/config/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/common/config/normalize.go internal/common/config/normalize_test.go
git commit -m "refactor(config): ParseCustomHeaders 返回 map[string]any，补 Warn 日志对齐 Python"
```

---

### Task 8: 全量编译 + 测试验证

**Files:** 无新文件

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: PASS

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./... -count=1`
Expected: PASS

- [ ] **Step 3: 覆盖率检查**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/common/utils/path/... ./internal/common/workspace/... ./internal/common/logger/... ./internal/common/config/...`
Expected: 各包覆盖率 ≥ 85%

- [ ] **Step 4: Commit（如有微调）**

如果有测试适配微调，在此提交。
