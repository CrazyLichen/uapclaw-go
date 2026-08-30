# 10.6.4 实现差异修复 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 10.6.4 AvatarPromptRail 实现审查发现的 3 处差异：补写 Forbidden Memory 集成测试、OwnerScopes Scene() 加 TrimSpace、forbidden.go 日志组件纠正

**Architecture:** 3 个独立修复，每个改动 1-2 个文件。集成测试用 `//go:build integration` 标签隔离，日常 `go test` 不执行

**Tech Stack:** Go 1.22+, 标准库 testing/strings, 项目内 config 包 + pathutil

---

## 文件变更清单

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 修改 | `internal/swarm/agents/harness/common/memory/forbidden.go` | 日志组件改为 ComponentAgentServer |
| 新建 | `internal/swarm/agents/harness/common/memory/forbidden_integration_test.go` | Forbidden Memory 集成测试 |
| 修改 | `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go` | Scene() 加 TrimSpace |
| 新建 | `internal/swarm/agents/harness/common/rails/permissions/owner_scopes_test.go` | OwnerScopes 完整测试 |

---

### Task 1: forbidden.go 日志组件改为 ComponentAgentServer

**Files:**
- Modify: `internal/swarm/agents/harness/common/memory/forbidden.go:30`

- [ ] **Step 1: 修改日志组件**

将 `forbidden.go` 第 30 行：
```go
var forbiddenLogComponent = logger.ComponentChannel
```
改为：
```go
var forbiddenLogComponent = logger.ComponentAgentServer
```

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go vet ./internal/swarm/agents/harness/common/memory/...`
Expected: 无输出（无错误）

- [ ] **Step 3: 运行现有测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/memory/...`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/memory/forbidden.go
git commit -m "fix(memory): 日志组件从 ComponentChannel 改为 ComponentAgentServer 对齐 agents/harness 模块定位"
```

---

### Task 2: OwnerScopes Scene() 加 TrimSpace + 补写完整测试

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go:70`
- Create: `internal/swarm/agents/harness/common/rails/permissions/owner_scopes_test.go`

- [ ] **Step 1: 写 owner_scopes_test.go 测试文件（含 TrimSpace 测试用例）**

```go
package permissions

import (
	"strings"
	"testing"
)

// ──────────────────────────── NewOwnerScopesPermissionContextFromDict 测试 ────────────────────────────

// TestNewOwnerScopesPermissionContextFromDict_完整字段 验证所有字段正确解析
func TestNewOwnerScopesPermissionContextFromDict_完整字段(t *testing.T) {
	data := map[string]any{
		"channel_id":            "feishu",
		"group_digital_avatar":  true,
		"principal_user_id":     "user-1",
		"triggering_user_id":    "sender-1",
		"enable_memory":         false,
		"avatar_principal_name": "张三",
		"avatar_mode":           true,
	}
	pc := NewOwnerScopesPermissionContextFromDict(data)
	if pc.ChannelID != "feishu" {
		t.Errorf("ChannelID = %q, 期望 \"feishu\"", pc.ChannelID)
	}
	if !pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 应为 true")
	}
	if pc.PrincipalUserID != "user-1" {
		t.Errorf("PrincipalUserID = %q, 期望 \"user-1\"", pc.PrincipalUserID)
	}
	if pc.TriggeringUserID != "sender-1" {
		t.Errorf("TriggeringUserID = %q, 期望 \"sender-1\"", pc.TriggeringUserID)
	}
	if pc.EnableMemory {
		t.Error("EnableMemory 应为 false")
	}
	if pc.AvatarPrincipalName != "张三" {
		t.Errorf("AvatarPrincipalName = %q, 期望 \"张三\"", pc.AvatarPrincipalName)
	}
	if !pc.AvatarMode {
		t.Error("AvatarMode 应为 true")
	}
}

// TestNewOwnerScopesPermissionContextFromDict_默认值 验证缺失字段使用零值 + EnableMemory 默认 true
func TestNewOwnerScopesPermissionContextFromDict_默认值(t *testing.T) {
	data := map[string]any{
		"channel_id": "feishu",
	}
	pc := NewOwnerScopesPermissionContextFromDict(data)
	if pc.ChannelID != "feishu" {
		t.Errorf("ChannelID = %q, 期望 \"feishu\"", pc.ChannelID)
	}
	if pc.GroupDigitalAvatar {
		t.Error("GroupDigitalAvatar 缺失时应为 false")
	}
	if !pc.EnableMemory {
		t.Error("EnableMemory 缺失时应为 true（默认值）")
	}
	if pc.AvatarMode {
		t.Error("AvatarMode 缺失时应为 false")
	}
}

// TestNewOwnerScopesPermissionContextFromDict_空字典 验证空字典返回默认值
func TestNewOwnerScopesPermissionContextFromDict_空字典(t *testing.T) {
	pc := NewOwnerScopesPermissionContextFromDict(map[string]any{})
	if !pc.EnableMemory {
		t.Error("空字典时 EnableMemory 应为 true（默认值）")
	}
}

// ──────────────────────────── Scene 测试 ────────────────────────────

// TestOwnerScopesPermissionContext_Scene_数字分身优先 验证 GroupDigitalAvatar 优先于 web
func TestOwnerScopesPermissionContext_Scene_数字分身优先(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          "web",
		GroupDigitalAvatar: true,
	}
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("当 group_digital_avatar=true 且 channel_id=web 时，Scene() = %q, 期望 \"group_digital_avatar\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_web 验证非数字分身时 web 场景
func TestOwnerScopesPermissionContext_Scene_web(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          "web",
		GroupDigitalAvatar: false,
	}
	if got := pc.Scene(); got != "web" {
		t.Errorf("Scene() = %q, 期望 \"web\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_普通IM 验证默认场景
func TestOwnerScopesPermissionContext_Scene_普通IM(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          "feishu",
		GroupDigitalAvatar: false,
	}
	if got := pc.Scene(); got != "normal_im" {
		t.Errorf("Scene() = %q, 期望 \"normal_im\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_空格channelID 验证 TrimSpace 对齐 Python strip()
func TestOwnerScopesPermissionContext_Scene_空格channelID(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          " web ",
		GroupDigitalAvatar: false,
	}
	if got := pc.Scene(); got != "web" {
		t.Errorf("带空格的 channel_id=\" web \" 时 Scene() = %q, 期望 \"web\"", got)
	}
}

// TestOwnerScopesPermissionContext_Scene_空格channelID数字分身仍优先 验证空格不影响数字分身优先级
func TestOwnerScopesPermissionContext_Scene_空格channelID数字分身仍优先(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:          " web ",
		GroupDigitalAvatar: true,
	}
	if got := pc.Scene(); got != "group_digital_avatar" {
		t.Errorf("带空格 channel_id + group_digital_avatar=true 时 Scene() = %q, 期望 \"group_digital_avatar\"", got)
	}
}

// ──────────────────────────── OwnerScopeKey 测试 ────────────────────────────

// TestOwnerScopesPermissionContext_OwnerScopeKey 验证返回 [channel_id, principal_user_id]
func TestOwnerScopesPermissionContext_OwnerScopeKey(t *testing.T) {
	pc := &OwnerScopesPermissionContext{
		ChannelID:      "feishu",
		PrincipalUserID: "user-1",
	}
	key := pc.OwnerScopeKey()
	if key[0] != "feishu" {
		t.Errorf("OwnerScopeKey()[0] = %q, 期望 \"feishu\"", key[0])
	}
	if key[1] != "user-1" {
		t.Errorf("OwnerScopeKey()[1] = %q, 期望 \"user-1\"", key[1])
	}
}
```

- [ ] **Step 2: 运行测试确认空格场景用例失败（Scene 尚未加 TrimSpace）**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -run TestOwnerScopesPermissionContext_Scene_空格 -v`
Expected: FAIL（`" web "` 返回 `"normal_im"` 而非 `"web"`）

- [ ] **Step 3: 修改 owner_scopes.go Scene() 加 TrimSpace**

将第 70 行：
```go
	if p.ChannelID == "web" {
```
改为：
```go
	if strings.TrimSpace(p.ChannelID) == "web" {
```

同时确保 `import` 中包含 `"strings"`（当前文件没有 import 块，需新增）：

```go
package permissions

import (
	"strings"
)
```

- [ ] **Step 4: 运行全部测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -v`
Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go internal/swarm/agents/harness/common/rails/permissions/owner_scopes_test.go
git commit -m "fix(permissions): OwnerScopes Scene() 加 TrimSpace 对齐 Python strip() + 补写完整测试"
```

---

### Task 3: 补写 Forbidden Memory 集成测试

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/forbidden_integration_test.go`

**前提**：config 包的路径解析使用 `sync.Once` 缓存。集成测试需要通过 `UAPCLAW_DATA_DIR` 环境变量 + `pathutil.ResetCache()` 让 `config.New("")` 指向 testdata 目录下的配置文件。testdata 目录下需要 `config/config.yaml` 结构（因为 `ConfigDir()` 返回 `<data_dir>/config/`，`ConfigFile()` 返回 `<data_dir>/config/config.yaml`）。

- [ ] **Step 1: 创建集成测试目录结构**

testdata 目录下创建两个场景的 config 子目录（config 包的 `ConfigDir()` 返回 `<data_dir>/config/`，`ConfigFile()` 返回 `<data_dir>/config/config.yaml`）：

```bash
mkdir -p internal/swarm/agents/harness/common/memory/testdata/forbidden_enabled/config
mkdir -p internal/swarm/agents/harness/common/memory/testdata/forbidden_no_patterns/config
```

- [ ] **Step 2: 创建 testdata/forbidden_enabled/config/config.yaml**

```yaml
memory:
  forbidden_memory_definition:
    enabled: true
    patterns:
      - 密码
      - API密钥
      - Secret
      - Token
      - 信用卡号
    description:
      zh: "以下内容禁止记忆：密码、API密钥等敏感信息"
      en: "The following content is forbidden to remember: passwords, API keys, etc."
```

注意：原有的 `testdata/forbidden_enabled.yaml` 和 `testdata/forbidden_no_patterns.yaml` 是顶层文件，集成测试不使用它们（config 包要求 `config/config.yaml` 路径结构）。可以保留原文件（纯函数测试可能未来使用），也可以删除以避免混淆。

- [ ] **Step 3: 写 forbidden_integration_test.go**

```go
//go:build integration

package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// TestGetForbiddenMemoryPrompt_中文输出_integration 验证从真实配置文件读取并生成中文提示词
// 运行方式: go test -tags=integration ./internal/swarm/agents/harness/common/memory/...
func TestGetForbiddenMemoryPrompt_中文输出_integration(t *testing.T) {
	setupTestConfig(t, "forbidden_enabled")
	defer restoreConfig()

	prompt := GetForbiddenMemoryPrompt("cn")
	if prompt == "" {
		t.Fatal("配置 enabled=true 时应返回非空中文提示词")
	}
	if !strings.Contains(prompt, "记忆限制规则") {
		t.Error("中文提示词应包含 '记忆限制规则'")
	}
	if !strings.Contains(prompt, "密码") {
		t.Error("中文提示词应包含 pattern '密码'")
	}
	if !strings.Contains(prompt, "API密钥") {
		t.Error("中文提示词应包含 pattern 'API密钥'")
	}
	if !strings.Contains(prompt, "experience_learn") {
		t.Error("中文提示词应包含执行要求中的 experience_learn")
	}
}

// TestGetForbiddenMemoryPrompt_英文输出_integration 验证从真实配置文件读取并生成英文提示词
// 运行方式: go test -tags=integration ./internal/swarm/agents/harness/common/memory/...
func TestGetForbiddenMemoryPrompt_英文输出_integration(t *testing.T) {
	setupTestConfig(t, "forbidden_enabled")
	defer restoreConfig()

	prompt := GetForbiddenMemoryPrompt("en")
	if prompt == "" {
		t.Fatal("配置 enabled=true 时应返回非空英文提示词")
	}
	if !strings.Contains(prompt, "Memory Restriction Rules") {
		t.Error("英文提示词应包含 'Memory Restriction Rules'")
	}
	if !strings.Contains(prompt, "passwords") {
		t.Error("英文提示词应包含 pattern 'passwords'")
	}
}

// TestGetForbiddenMemoryPrompt_无Patterns_integration 验证无 patterns 时不输出列表
// 运行方式: go test -tags=integration ./internal/swarm/agents/harness/common/memory/...
func TestGetForbiddenMemoryPrompt_无Patterns_integration(t *testing.T) {
	setupTestConfig(t, "forbidden_no_patterns")
	defer restoreConfig()

	prompt := GetForbiddenMemoryPrompt("cn")
	if prompt == "" {
		t.Fatal("配置 enabled=true 时应返回非空提示词")
	}
	if strings.Contains(prompt, "禁止记忆的敏感信息类型包括") {
		t.Error("无 patterns 时不应输出列表标题")
	}
	if !strings.Contains(prompt, "执行要求") {
		t.Error("无 patterns 时仍应包含执行要求")
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// originalDataDir 保存原始 UAPCLAW_DATA_DIR 环境变量
var originalDataDir string

// setupTestConfig 设置 UAPCLAW_DATA_DIR 指向 testdata 下的指定子目录，
// 并重置 pathutil 缓存让 config.New("") 读取测试配置。
func setupTestConfig(t *testing.T, scenario string) {
	t.Helper()
	// 保存原始环境变量
	originalDataDir = os.Getenv("UAPCLAW_DATA_DIR")

	// testdata 下每个场景有自己的 config/ 子目录
	testdataDir := filepath.Join("testdata", scenario)
	absPath, err := filepath.Abs(testdataDir)
	if err != nil {
		t.Fatalf("获取测试目录绝对路径失败: %v", err)
	}

	// 确认配置文件存在
	configFile := filepath.Join(absPath, "config", "config.yaml")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Fatalf("测试配置文件不存在: %s", configFile)
	}

	// 设置环境变量 + 重置缓存
	os.Setenv("UAPCLAW_DATA_DIR", absPath)
	pathutil.ResetCache()
}

// restoreConfig 恢复原始环境变量和 pathutil 缓存
func restoreConfig() {
	if originalDataDir == "" {
		os.Unsetenv("UAPCLAW_DATA_DIR")
	} else {
		os.Setenv("UAPCLAW_DATA_DIR", originalDataDir)
	}
	pathutil.ResetCache()
}
```

- [ ] **Step 4: 创建 testdata/forbidden_no_patterns/config/config.yaml**

```yaml
memory:
  forbidden_memory_definition:
    enabled: true
    description:
      zh: "以下内容禁止记忆：敏感信息"
      en: "The following content is forbidden to remember: sensitive information"
```

- [ ] **Step 5: 运行集成测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test -tags=integration ./internal/swarm/agents/harness/common/memory/... -v`
Expected: ALL PASS

- [ ] **Step 6: 确认日常 go test 不执行集成测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/memory/... -v`
Expected: 只运行 `forbidden_test.go` 中的纯函数测试，不运行 `forbidden_integration_test.go` 中的集成测试

- [ ] **Step 7: 确认 permissions 包测试也能通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/... -v`
Expected: ALL PASS

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/agents/harness/common/memory/forbidden_integration_test.go internal/swarm/agents/harness/common/memory/testdata/forbidden_enabled/config/ internal/swarm/agents/harness/common/memory/testdata/forbidden_no_patterns/config/
git commit -m "test(memory): 补写 Forbidden Memory 集成测试（//go:build integration）"
```
