# AvatarPromptRail + Forbidden Memory 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 10.6.4 AvatarPromptRail + 10.6.17 Forbidden Memory，对齐 Python 的 `avatar_rail.py` 和 `forbidden.py`，包括 PermissionContext 扩展和 OwnerScopesPermissionContext 迁移。

**Architecture:** 在 `swarm/agents/harness/common/rails/` 新增 `avatar_rail.go`，在 `swarm/agents/harness/common/memory/` 新增 `forbidden.go`，扩展 `swarm/schema/permission.go` 的 `PermissionContext` 加 3 字段，将 `OwnerScopesPermissionContext` 从 `adapter/` 迁到 `rails/permissions/`。

**Tech Stack:** Go 1.22+, 标准库 context/testing, 项目内 agentcore/harness/rails 基类, config 包读取 YAML

---

## 文件变更清单

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 修改 | `internal/swarm/schema/permission.go` | PermissionContext 加 3 字段 |
| 修改 | `internal/swarm/schema/permission_test.go` | 新字段测试 |
| 新建 | `internal/swarm/agents/harness/common/rails/permissions/doc.go` | 包文档 |
| 新建 | `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go` | OwnerScopesPermissionContext（从 adapter 迁入） |
| 删除 | `internal/swarm/server/adapter/owner_scopes_permission.go` | 已迁移 |
| 新建 | `internal/swarm/agents/harness/common/memory/doc.go` | 包文档 |
| 新建 | `internal/swarm/agents/harness/common/memory/forbidden.go` | Forbidden Memory 实现 |
| 新建 | `internal/swarm/agents/harness/common/memory/forbidden_test.go` | Forbidden Memory 测试 |
| 新建 | `internal/swarm/agents/harness/common/rails/avatar_rail.go` | AvatarPromptRail 实现 |
| 新建 | `internal/swarm/agents/harness/common/rails/avatar_rail_test.go` | AvatarPromptRail 测试 |
| 修改 | `internal/swarm/agents/harness/common/rails/doc.go` | 更新文件目录 |
| 修改 | `internal/swarm/server/adapter/deep_adapter_rails.go` | buildAvatarRail 真实构建 + 移除 ⤵️ |
| 修改 | `internal/swarm/server/adapter/deep_adapter.go` | 移除 avatarRail 字段 ⤵️ 标记 |
| 删除 | `internal/swarm/server/rails/` | 整个目录删除 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 10.6.3-10 + 10.6.17 状态更新 |

---

### Task 1: 扩展 schema.PermissionContext

**Files:**
- Modify: `internal/swarm/schema/permission.go`
- Modify: `internal/swarm/schema/permission_test.go`

- [ ] **Step 1: 在 PermissionContext 结构体中新增 3 个字段**

在 `permission.go` 的 `PermissionContext` 结构体中，在 `WebUserID` 字段后添加：

```go
// EnableMemory 是否启用记忆（默认 true）
EnableMemory bool `json:"enable_memory"`
// AvatarPrincipalName 数字分身主体名称
AvatarPrincipalName string `json:"avatar_principal_name"`
// AvatarMode 是否为群聊消息
AvatarMode bool `json:"avatar_mode"`
```

- [ ] **Step 2: 修改 NewPermissionContext 默认值**

在 `NewPermissionContext` 中设置 `EnableMemory` 默认值为 `true`：

```go
func NewPermissionContext(opts ...PermissionContextOption) *PermissionContext {
	pc := &PermissionContext{
		EnableMemory: true,
	}
	for _, opt := range opts {
		opt(pc)
	}
	return pc
}
```

- [ ] **Step 3: 在 NewPermissionContextFromDict 中解析 3 个新字段**

在 `NewPermissionContextFromDict` 函数中，在 `web_user_id` 解析块后添加：

```go
if v, ok := data["enable_memory"]; ok {
	if b, ok := v.(bool); ok {
		pc.EnableMemory = b
	} else {
		// 默认 true，仅显式 false 才关闭
		pc.EnableMemory = true
	}
} else {
	pc.EnableMemory = true
}
if v, ok := data["avatar_principal_name"]; ok {
	if s, ok := v.(string); ok {
		pc.AvatarPrincipalName = s
	}
}
if v, ok := data["avatar_mode"]; ok {
	if b, ok := v.(bool); ok {
		pc.AvatarMode = b
	}
}
```

- [ ] **Step 4: 新增 3 个 WithPermission* 选项函数**

在现有 `WithPermissionWebUserID` 后添加：

```go
// WithPermissionEnableMemory 设置是否启用记忆的选项。
func WithPermissionEnableMemory(v bool) PermissionContextOption {
	return func(pc *PermissionContext) { pc.EnableMemory = v }
}

// WithPermissionAvatarPrincipalName 设置数字分身主体名称的选项。
func WithPermissionAvatarPrincipalName(name string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.AvatarPrincipalName = name }
}

// WithPermissionAvatarMode 设置群聊消息标志的选项。
func WithPermissionAvatarMode(v bool) PermissionContextOption {
	return func(pc *PermissionContext) { pc.AvatarMode = v }
}
```

- [ ] **Step 5: 更新 ToDict 方法**

在 `ToDict()` 返回的 map 中添加 3 个新字段：

```go
"enable_memory":        p.EnableMemory,
"avatar_principal_name": p.AvatarPrincipalName,
"avatar_mode":          p.AvatarMode,
```

- [ ] **Step 6: 更新测试文件**

在 `permission_test.go` 中：

1. `TestNewPermissionContext` — 验证 `EnableMemory` 默认为 `true`，`AvatarPrincipalName` 默认为 `""`，`AvatarMode` 默认为 `false`
2. `TestNewPermissionContext_使用Option` — 使用新的 3 个 Option 并验证
3. `TestNewPermissionContextFromDict` — data map 中加入 3 个新字段并验证解析
4. `TestNewPermissionContextFromDict_缺失字段用零值` — 验证缺失 `enable_memory` 时默认 `true`
5. `TestPermissionContext_ToDictFromDict往返` — 验证 3 个新字段往返一致
6. `TestPermissionContext_JSON往返` — original 结构体加入 3 个新字段并验证

- [ ] **Step 7: 运行测试确认通过**

```bash
go test ./internal/swarm/schema/... -v -run TestPermissionContext
```

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/schema/permission.go internal/swarm/schema/permission_test.go
git commit -m "feat(schema): 扩展 PermissionContext 新增 EnableMemory/AvatarPrincipalName/AvatarMode 字段"
```

---

### Task 2: 迁移 OwnerScopesPermissionContext

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/doc.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`
- Delete: `internal/swarm/server/adapter/owner_scopes_permission.go`

- [ ] **Step 1: 创建 permissions 目录和 doc.go**

```bash
mkdir -p internal/swarm/agents/harness/common/rails/permissions
```

创建 `internal/swarm/agents/harness/common/rails/permissions/doc.go`：

```go
// Package permissions 提供数字分身场景下的权限上下文与工具权限检查。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/，
// 包含 OwnerScopesPermissionContext 和相关辅助函数。
//
// 文件目录：
//
//	permissions/
//	├── doc.go            # 包文档
//	└── owner_scopes.go   # OwnerScopesPermissionContext
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/
package permissions
```

- [ ] **Step 2: 创建 owner_scopes.go**

将 `internal/swarm/server/adapter/owner_scopes_permission.go` 的内容（结构体、方法、构造函数）原样复制到 `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`，仅修改 `package` 声明为 `permissions`。

完整文件内容（对齐 Python `owner_scopes.py`）：

```go
package permissions

// ──────────────────────────── 结构体 ────────────────────────────

// OwnerScopesPermissionContext 数字分身场景下的权限上下文。
// 对齐 Python: owner_scopes.PermissionContext
// 不放入 schema/agent.py，不序列化到 AgentRequest；
// 仅从 metadata 构建 → Context 注入 → 匹配。
type OwnerScopesPermissionContext struct {
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// GroupDigitalAvatar 是否为数字分身场景
	GroupDigitalAvatar bool `json:"group_digital_avatar"`
	// PrincipalUserID 权限 owner
	PrincipalUserID string `json:"principal_user_id"`
	// TriggeringUserID 触发者
	TriggeringUserID string `json:"triggering_user_id"`
	// EnableMemory 是否启用记忆
	EnableMemory bool `json:"enable_memory"`
	// AvatarPrincipalName 数字分身主体名称
	AvatarPrincipalName string `json:"avatar_principal_name"`
	// AvatarMode 是否为群聊消息
	AvatarMode bool `json:"avatar_mode"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOwnerScopesPermissionContextFromDict 从字典创建权限上下文
func NewOwnerScopesPermissionContextFromDict(data map[string]any) *OwnerScopesPermissionContext {
	pc := &OwnerScopesPermissionContext{EnableMemory: true}
	if v, ok := data["channel_id"].(string); ok {
		pc.ChannelID = v
	}
	if v, ok := data["group_digital_avatar"].(bool); ok {
		pc.GroupDigitalAvatar = v
	}
	if v, ok := data["principal_user_id"].(string); ok {
		pc.PrincipalUserID = v
	}
	if v, ok := data["triggering_user_id"].(string); ok {
		pc.TriggeringUserID = v
	}
	if v, ok := data["enable_memory"].(bool); ok {
		pc.EnableMemory = v
	}
	if v, ok := data["avatar_principal_name"].(string); ok {
		pc.AvatarPrincipalName = v
	}
	if v, ok := data["avatar_mode"].(bool); ok {
		pc.AvatarMode = v
	}
	return pc
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Scene 返回权限场景类型
func (p *OwnerScopesPermissionContext) Scene() string {
	if p.GroupDigitalAvatar {
		return "group_digital_avatar"
	}
	if p.ChannelID == "web" {
		return "web"
	}
	return "normal_im"
}

// OwnerScopeKey 返回 (channel_id, principal_user_id)
func (p *OwnerScopesPermissionContext) OwnerScopeKey() [2]string {
	return [2]string{p.ChannelID, p.PrincipalUserID}
}
```

- [ ] **Step 3: 删除旧文件**

```bash
rm internal/swarm/server/adapter/owner_scopes_permission.go
```

- [ ] **Step 4: 更新 adapter 中对 OwnerScopesPermissionContext 的引用（如有）**

搜索 adapter 包内是否引用 `OwnerScopesPermissionContext`。如果 adapter 内部代码使用了它，添加 import：

```go
commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
commperms "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
```

并将 `OwnerScopesPermissionContext` 替换为 `commperms.OwnerScopesPermissionContext`，`NewOwnerScopesPermissionContextFromDict` 替换为 `commperms.NewOwnerScopesPermissionContextFromDict`。

经查 adapter 包内无外部引用（只在定义文件自身），所以无需修改其他文件。

- [ ] **Step 5: 运行编译确认无错误**

```bash
go build ./internal/swarm/...
```

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/
git rm internal/swarm/server/adapter/owner_scopes_permission.go
git commit -m "refactor: 迁移 OwnerScopesPermissionContext 到 swarm/agents/harness/common/rails/permissions/"
```

---

### Task 3: 删除空的 swarm/server/rails/ 目录

**Files:**
- Delete: `internal/swarm/server/rails/`

- [ ] **Step 1: 确认目录只剩 doc.go**

```bash
ls -la internal/swarm/server/rails/
```

预期：仅有 `doc.go`

- [ ] **Step 2: 删除整个目录**

```bash
rm -rf internal/swarm/server/rails/
```

- [ ] **Step 3: 搜索并修复对 server/rails 包的 import 引用**

```bash
grep -rn "swarm/server/rails" internal/swarm/
```

如果有引用，更新为 `swarm/agents/harness/common/rails`。

- [ ] **Step 4: 运行编译确认**

```bash
go build ./internal/swarm/...
```

- [ ] **Step 5: Commit**

```bash
git rm -r internal/swarm/server/rails/
git commit -m "chore: 删除空的 swarm/server/rails/ 目录（已迁移到 swarm/agents/harness/common/rails/）"
```

---

### Task 4: 实现 Forbidden Memory

**Files:**
- Create: `internal/swarm/agents/harness/common/memory/doc.go`
- Create: `internal/swarm/agents/harness/common/memory/forbidden.go`
- Create: `internal/swarm/agents/harness/common/memory/forbidden_test.go`

- [ ] **Step 1: 创建 memory 目录和 doc.go**

```bash
mkdir -p internal/swarm/agents/harness/common/memory
```

创建 `internal/swarm/agents/harness/common/memory/doc.go`：

```go
// Package memory 提供 Swarm 侧的记忆相关功能。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/memory/，
// 包含记忆禁止配置（forbidden）等子模块。
//
// 文件目录：
//
//	memory/
//	├── doc.go          # 包文档
//	└── forbidden.go    # 记忆禁止配置与提示词生成
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/memory/
package memory
```

- [ ] **Step 2: 编写 forbidden_test.go 的失败测试**

创建 `internal/swarm/agents/harness/common/memory/forbidden_test.go`：

```go
package memory

import (
	"os"
	"testing"
)

// TestGetForbiddenMemoryPrompt_禁用返回空 验证 enabled=false 时返回空串
func TestGetForbiddenMemoryPrompt_禁用返回空(t *testing.T) {
	prompt := GetForbiddenMemoryPrompt("cn")
	// 未配置时默认 enabled=false，应返回空串
	if prompt != "" {
		t.Errorf("未配置时 GetForbiddenMemoryPrompt 应返回空串，实际: %q", prompt)
	}
}

// TestGetForbiddenMemoryPrompt_中文输出 验证中文提示词包含关键内容
func TestGetForbiddenMemoryPrompt_中文输出(t *testing.T) {
	// 设置配置文件路径
	os.Setenv("JIUWEN_CONFIG_PATH", "testdata/forbidden_enabled.yaml")
	defer os.Unsetenv("JIUWEN_CONFIG_PATH")

	prompt := GetForbiddenMemoryPrompt("cn")
	if prompt == "" {
		t.Fatal("中文提示词不应为空")
	}
	if !contains(prompt, "记忆限制规则") {
		t.Error("中文提示词应包含 '记忆限制规则'")
	}
	if !contains(prompt, "密码") {
		t.Error("中文提示词应包含 pattern '密码'")
	}
	if !contains(prompt, "experience_learn") {
		t.Error("中文提示词应包含执行要求中的 experience_learn")
	}
	if !contains(prompt, "write_memory") {
		t.Error("中文提示词应包含执行要求中的 write_memory")
	}
}

// TestGetForbiddenMemoryPrompt_英文输出 验证英文提示词包含关键内容
func TestGetForbiddenMemoryPrompt_英文输出(t *testing.T) {
	os.Setenv("JIUWEN_CONFIG_PATH", "testdata/forbidden_enabled.yaml")
	defer os.Unsetenv("JIUWEN_CONFIG_PATH")

	prompt := GetForbiddenMemoryPrompt("en")
	if prompt == "" {
		t.Fatal("英文提示词不应为空")
	}
	if !contains(prompt, "Memory Restriction Rules") {
		t.Error("英文提示词应包含 'Memory Restriction Rules'")
	}
	if !contains(prompt, "passwords") {
		t.Error("英文提示词应包含 pattern 'passwords'")
	}
}

// TestGetForbiddenMemoryPrompt_无Patterns 验证无 patterns 时不输出列表
func TestGetForbiddenMemoryPrompt_无Patterns(t *testing.T) {
	os.Setenv("JIUWEN_CONFIG_PATH", "testdata/forbidden_no_patterns.yaml")
	defer os.Unsetenv("JIUWEN_CONFIG_PATH")

	prompt := GetForbiddenMemoryPrompt("cn")
	if prompt == "" {
		t.Fatal("有 enabled=true 但无 patterns 时提示词不应为空")
	}
	if contains(prompt, "禁止记忆的敏感信息类型包括") {
		t.Error("无 patterns 时不应输出 pattern 列表")
	}
	if !contains(prompt, "记忆限制规则") {
		t.Error("无 patterns 时仍应包含标题")
	}
}

// TestGetMemoryForbiddenConfig_配置缺失 验证无配置文件时返回默认值
func TestGetMemoryForbiddenConfig_配置缺失(t *testing.T) {
	os.Unsetenv("JIUWEN_CONFIG_PATH")
	cfg := getMemoryForbiddenConfig()
	if cfg.Enabled {
		t.Error("无配置时 Enabled 应为 false")
	}
	if len(cfg.Patterns) != 0 {
		t.Error("无配置时 Patterns 应为空")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: 创建测试数据文件**

```bash
mkdir -p internal/swarm/agents/harness/common/memory/testdata
```

创建 `internal/swarm/agents/harness/common/memory/testdata/forbidden_enabled.yaml`：

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

创建 `internal/swarm/agents/harness/common/memory/testdata/forbidden_no_patterns.yaml`：

```yaml
memory:
  forbidden_memory_definition:
    enabled: true
    description:
      zh: "以下内容禁止记忆：敏感信息"
      en: "The following content is forbidden to remember: sensitive information"
```

- [ ] **Step 4: 运行测试确认失败**

```bash
go test ./internal/swarm/agents/harness/common/memory/... -v -run TestGetForbidden
```

预期：编译失败（forbidden.go 不存在）

- [ ] **Step 5: 编写 forbidden.go 实现**

创建 `internal/swarm/agents/harness/common/memory/forbidden.go`：

```go
package memory

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryForbiddenConfig 记忆禁止配置。
// 对齐 Python: _get_memory_forbidden_config() 返回的 dict
type MemoryForbiddenConfig struct {
	// Enabled 是否启用禁止记忆规则
	Enabled bool `json:"enabled"`
	// Patterns 禁止记忆的敏感信息类型列表
	Patterns []string `json:"patterns"`
	// Description 多语言描述："zh"/"en" → 描述文本
	Description map[string]string `json:"description"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var forbiddenLogComponent = logger.ComponentChannel

// ──────────────────────────── 导出函数 ────────────────────────────

// GetForbiddenMemoryPrompt 格式化禁止记忆提示词。enabled=false 时返回空串。
// 对齐 Python: get_forbidden_memory_prompt(language)
func GetForbiddenMemoryPrompt(language string) string {
	cfg := getMemoryForbiddenConfig()

	if !cfg.Enabled {
		return ""
	}

	descText := ""
	if cfg.Description != nil {
		// 优先使用请求语言，回退到 "zh"
		if v, ok := cfg.Description[language]; ok && v != "" {
			descText = v
		} else if v, ok := cfg.Description["zh"]; ok {
			descText = v
		}
	}

	if language == "zh" || language == "cn" {
		return buildForbiddenPromptCN(descText, cfg.Patterns)
	}
	return buildForbiddenPromptEN(descText, cfg.Patterns)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getMemoryForbiddenConfig 从 config 读取 memory.forbidden_memory_definition。
// 对齐 Python: _get_memory_forbidden_config()
func getMemoryForbiddenConfig() *MemoryForbiddenConfig {
	cfg, err := config.New("")
	if err != nil {
		logger.Warn(forbiddenLogComponent).Err(err).Msg("加载配置失败")
		return &MemoryForbiddenConfig{Enabled: false}
	}
	configBase, err := cfg.Load()
	if err != nil {
		logger.Warn(forbiddenLogComponent).Err(err).Msg("读取配置失败")
		return &MemoryForbiddenConfig{Enabled: false}
	}

	memoryRaw, ok := configBase["memory"]
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}
	memoryMap, ok := memoryRaw.(map[string]any)
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}
	forbiddenRaw, ok := memoryMap["forbidden_memory_definition"]
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}
	forbiddenMap, ok := forbiddenRaw.(map[string]any)
	if !ok {
		return &MemoryForbiddenConfig{Enabled: false}
	}

	result := &MemoryForbiddenConfig{}

	if v, ok := forbiddenMap["enabled"].(bool); ok {
		result.Enabled = v
	}
	if patternsRaw, ok := forbiddenMap["patterns"]; ok {
		if arr, ok := patternsRaw.([]any); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result.Patterns = append(result.Patterns, s)
				}
			}
		}
	}
	if descRaw, ok := forbiddenMap["description"]; ok {
		if m, ok := descRaw.(map[string]any); ok {
			result.Description = make(map[string]string, len(m))
			for k, v := range m {
				if s, ok := v.(string); ok {
					result.Description[k] = s
				}
			}
		}
	}

	return result
}

// buildForbiddenPromptCN 构建中文禁止记忆提示词
// 对齐 Python: get_forbidden_memory_prompt("zh") 的格式化输出
func buildForbiddenPromptCN(descText string, patterns []string) string {
	parts := []string{"### 记忆限制规则", ""}
	if descText != "" {
		parts = append(parts, descText, "")
	}
	if len(patterns) > 0 {
		parts = append(parts, "**禁止记忆的敏感信息类型包括：**", "")
		for i, p := range patterns {
			parts = append(parts, fmt.Sprintf("%d. `%s`", i+1, p))
		}
		parts = append(parts, "")
	}
	parts = append(parts, "**执行要求：**")
	parts = append(parts, "- 在调用 `experience_learn` 或 `write_memory` 存储记忆前，必须检查内容是否包含上述敏感信息")
	parts = append(parts, "- 如果检测到敏感信息，必须对其进行脱敏处理（如替换为 ***）或拒绝存储")
	parts = append(parts, "- 用户明确要求的密码、密钥等敏感信息不得存入记忆系统")
	parts = append(parts, "")
	return strings.Join(parts, "\n")
}

// buildForbiddenPromptEN 构建英文禁止记忆提示词
// 对齐 Python: get_forbidden_memory_prompt("en") 的格式化输出
func buildForbiddenPromptEN(descText string, patterns []string) string {
	parts := []string{"### Memory Restriction Rules", ""}
	if descText != "" {
		parts = append(parts, descText, "")
	}
	if len(patterns) > 0 {
		parts = append(parts, "**Types of sensitive information forbidden to remember:**", "")
		for i, p := range patterns {
			parts = append(parts, fmt.Sprintf("%d. `%s`", i+1, p))
		}
		parts = append(parts, "")
	}
	parts = append(parts, "**Requirements:**")
	parts = append(parts, "- Before calling `experience_learn` or `write_memory` to store memories, you must check if the content contains the above sensitive information")
	parts = append(parts, "- If sensitive information is detected, it must be desensitized (e.g., replaced with ***) or storage must be refused")
	parts = append(parts, "- Sensitive information such as passwords and keys explicitly provided by the user must not be stored in the memory system")
	parts = append(parts, "")
	return strings.Join(parts, "\n")
}
```

- [ ] **Step 6: 运行测试确认通过**

```bash
go test ./internal/swarm/agents/harness/common/memory/... -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/swarm/agents/harness/common/memory/
git commit -m "feat(memory): 实现 GetForbiddenMemoryPrompt 记忆禁止配置与提示词生成（10.6.17）"
```

---

### Task 5: 实现 AvatarPromptRail

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/avatar_rail.go`
- Create: `internal/swarm/agents/harness/common/rails/avatar_rail_test.go`

- [ ] **Step 1: 编写 avatar_rail_test.go 的失败测试**

创建 `internal/swarm/agents/harness/common/rails/avatar_rail_test.go`，包含核心测试用例：

```go
package rails

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeBaseAgent 用于测试的 mock agent
type fakeBaseAgentForAvatar struct {
	builder *saprompt.SystemPromptBuilder
}

func (f *fakeBaseAgentForAvatar) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.builder
}

func newTestContextWithPerm(perm *sschema.PermissionContext) context.Context {
	ctx := context.Background()
	if perm != nil {
		ctx = sschema.WithPermissionContextValue(ctx, perm)
	}
	return ctx
}

// ──────────────────────────── BeforeModelCall 测试 ────────────────────────────

// TestAvatarPromptRail_BeforeModelCall_无PermissionContext 验证 perm_ctx 为 nil 时只注入 forbidden_memory
func TestAvatarPromptRail_BeforeModelCall_无PermissionContext(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{builder: builder}
	cbc := agentinterfaces.NewAgentCallbackContext(context.Background(), agent, nil, nil, nil, nil, nil)

	err := rail.BeforeModelCall(context.Background(), cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	// 无 PermissionContext 时不应注入 avatar_identity
	if builder.HasSection("avatar_identity") {
		t.Error("无 PermissionContext 时不应注入 avatar_identity")
	}
}

// TestAvatarPromptRail_BeforeModelCall_数字分身模式 验证群聊数字分身注入 4 种 section
func TestAvatarPromptRail_BeforeModelCall_数字分身模式(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{builder: builder}

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionAvatarPrincipalName("张三"),
		sschema.WithPermissionEnableMemory(true),
	)
	ctx := newTestContextWithPerm(perm)
	cbc := agentinterfaces.NewAgentCallbackContext(ctx, agent, nil, nil, nil, nil, nil)

	err := rail.BeforeModelCall(ctx, cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	if !builder.HasSection("avatar_identity") {
		t.Error("数字分身模式应注入 avatar_identity")
	}
	if !builder.HasSection("group_chat_memory_notice") {
		t.Error("数字分身模式应注入 group_chat_memory_notice")
	}
	if !builder.HasSection("interaction_guidance") {
		t.Error("数字分身模式应注入 interaction_guidance")
	}
	// enable_memory=true 时不注入 memory_fully_disabled
	if builder.HasSection("memory_fully_disabled") {
		t.Error("enable_memory=true 时不应注入 memory_fully_disabled")
	}
}

// TestAvatarPromptRail_BeforeModelCall_记忆完全禁用 验证 enable_memory=false 时注入 memory_fully_disabled
func TestAvatarPromptRail_BeforeModelCall_记忆完全禁用(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{builder: builder}

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionAvatarPrincipalName("张三"),
		sschema.WithPermissionEnableMemory(false),
	)
	ctx := newTestContextWithPerm(perm)
	cbc := agentinterfaces.NewAgentCallbackContext(ctx, agent, nil, nil, nil, nil, nil)

	err := rail.BeforeModelCall(ctx, cbc)
	if err != nil {
		t.Fatalf("BeforeModelCall 返回错误: %v", err)
	}

	if !builder.HasSection("memory_fully_disabled") {
		t.Error("enable_memory=false + 数字分身时应注入 memory_fully_disabled")
	}
}

// TestAvatarPromptRail_BeforeModelCall_清除旧注入 验证连续调用时清除上次注入的 section
func TestAvatarPromptRail_BeforeModelCall_清除旧注入(t *testing.T) {
	rail := NewAvatarPromptRail()
	builder := saprompt.NewSystemPromptBuilder()
	agent := &fakeBaseAgentForAvatar{builder: builder}

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionAvatarPrincipalName("张三"),
	)
	ctx := newTestContextWithPerm(perm)

	// 第一次调用：注入 avatar_identity
	cbc1 := agentinterfaces.NewAgentCallbackContext(ctx, agent, nil, nil, nil, nil, nil)
	rail.BeforeModelCall(ctx, cbc1)
	if !builder.HasSection("avatar_identity") {
		t.Fatal("第一次调用后应存在 avatar_identity")
	}

	// 第二次调用：无 PermissionContext → 应清除 avatar_identity
	cbc2 := agentinterfaces.NewAgentCallbackContext(context.Background(), agent, nil, nil, nil, nil, nil)
	rail.BeforeModelCall(context.Background(), cbc2)
	if builder.HasSection("avatar_identity") {
		t.Error("第二次调用后 avatar_identity 应被清除")
	}
}

// ──────────────────────────── BeforeToolCall 测试 ────────────────────────────

// TestAvatarPromptRail_BeforeToolCall_群聊禁写 验证群聊模式拦截 write_memory
func TestAvatarPromptRail_BeforeToolCall_群聊禁写(t *testing.T) {
	rail := NewAvatarPromptRail()

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionEnableMemory(true),
	)
	ctx := newTestContextWithPerm(perm)

	for _, toolName := range []string{"write_memory", "edit_memory"} {
		toolInputs := &agentinterfaces.ToolCallInputs{ToolName: toolName}
		cbc := agentinterfaces.NewAgentCallbackContext(ctx, nil, nil, toolInputs, nil, nil, nil)

		rail.BeforeToolCall(ctx, cbc)

		if cbc.Extra()["_skip_tool"] != true {
			t.Errorf("群聊模式应拦截 %s", toolName)
		}
	}
}

// TestAvatarPromptRail_BeforeToolCall_记忆完全禁用 验证拦截所有 5 个记忆工具
func TestAvatarPromptRail_BeforeToolCall_记忆完全禁用(t *testing.T) {
	rail := NewAvatarPromptRail()

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionEnableMemory(false),
	)
	ctx := newTestContextWithPerm(perm)

	for _, toolName := range []string{"write_memory", "edit_memory", "read_memory", "memory_search", "memory_get"} {
		toolInputs := &agentinterfaces.ToolCallInputs{ToolName: toolName}
		cbc := agentinterfaces.NewAgentCallbackContext(ctx, nil, nil, toolInputs, nil, nil, nil)

		rail.BeforeToolCall(ctx, cbc)

		if cbc.Extra()["_skip_tool"] != true {
			t.Errorf("记忆完全禁用应拦截 %s", toolName)
		}
	}
}

// TestAvatarPromptRail_BeforeToolCall_正常放行 验证非记忆工具不被拦截
func TestAvatarPromptRail_BeforeToolCall_正常放行(t *testing.T) {
	rail := NewAvatarPromptRail()

	perm := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
		sschema.WithPermissionEnableMemory(true),
	)
	ctx := newTestContextWithPerm(perm)

	toolInputs := &agentinterfaces.ToolCallInputs{ToolName: "search"}
	cbc := agentinterfaces.NewAgentCallbackContext(ctx, nil, nil, toolInputs, nil, nil, nil)

	rail.BeforeToolCall(ctx, cbc)

	if cbc.Extra()["_skip_tool"] == true {
		t.Error("非记忆工具不应被拦截")
	}
}

// ──────────────────────────── 提示词内容测试 ────────────────────────────

// TestBuildAvatarPrompt_中文 验证中文身份提示词内容
func TestBuildAvatarPrompt_中文(t *testing.T) {
	prompt := buildAvatarPrompt("张三", "cn")
	if prompt == "" {
		t.Fatal("中文身份提示词不应为空")
	}
	if !contains(prompt, "数字分身模式") {
		t.Error("中文提示词应包含 '数字分身模式'")
	}
	if !contains(prompt, "张三") {
		t.Error("中文提示词应包含主体名称 '张三'")
	}
	if !contains(prompt, "第一人称视角") {
		t.Error("中文提示词应包含 '第一人称视角'")
	}
	if !contains(prompt, "不暴露身份") {
		t.Error("中文提示词应包含 '不暴露身份'")
	}
}

// TestBuildAvatarPrompt_英文 验证英文身份提示词内容
func TestBuildAvatarPrompt_英文(t *testing.T) {
	prompt := buildAvatarPrompt("Alice", "en")
	if prompt == "" {
		t.Fatal("英文身份提示词不应为空")
	}
	if !contains(prompt, "Digital Avatar Mode") {
		t.Error("英文提示词应包含 'Digital Avatar Mode'")
	}
	if !contains(prompt, "Alice") {
		t.Error("英文提示词应包含主体名称 'Alice'")
	}
}

// TestBuildInteractionPrompt_中文 验证追问指引内容
func TestBuildInteractionPrompt_中文(t *testing.T) {
	prompt := buildInteractionPrompt("cn")
	if prompt == "" {
		t.Fatal("中文追问指引不应为空")
	}
	if !contains(prompt, "多轮交互指引") {
		t.Error("中文指引应包含 '多轮交互指引'")
	}
	if !contains(prompt, "群聊追问@") {
		t.Error("中文指引应包含 '群聊追问@'")
	}
	if !contains(prompt, "私聊追问") {
		t.Error("中文指引应包含 '私聊追问'")
	}
}
```

> **注意**: 上述测试中 `agentinterfaces.NewAgentCallbackContext` 的参数签名需与实际代码对齐，实现时需根据 `callback.go` 中 `AgentCallbackContext` 的构造方式调整。

- [ ] **Step 2: 运行测试确认失败**

```bash
go test ./internal/swarm/agents/harness/common/rails/... -v -run TestAvatarPrompt
```

预期：编译失败（avatar_rail.go 不存在）

- [ ] **Step 3: 编写 avatar_rail.go 实现**

创建 `internal/swarm/agents/harness/common/rails/avatar_rail.go`：

```go
package rails

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AvatarPromptRail 数字分身 Rail — 处理所有 per-request 的 avatar 逻辑。
// 对齐 Python: AvatarPromptRail(DeepAgentRail)
//
// 职责:
// 1. BeforeModelCall: 根据 PermissionContext 动态注入/移除 avatar 相关 PromptSection
// 2. BeforeToolCall: 拦截群聊记忆禁写 + enable_memory=False 场景
type AvatarPromptRail struct {
	rails.DeepAgentRail
	// injectedSections 已注入的 PromptSection 名称集合
	injectedSections map[string]struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// avatarPromptPriority 数字分身提示词基础优先级
	// 对齐 Python: _AVATAR_PROMPT_PRIORITY = 110
	avatarPromptPriority = 110

	// avatarPromptRailPriority AvatarPromptRail 优先级
	// 对齐 Python: AvatarPromptRail.priority = 85
	avatarPromptRailPriority = 85
)

// ──────────────────────────── 全局变量 ────────────────────────────

// memoryWriteTools 记忆写入工具集合
// 对齐 Python: _MEMORY_WRITE_TOOLS = frozenset({"write_memory", "edit_memory"})
var memoryWriteTools = map[string]struct{}{
	"write_memory": {},
	"edit_memory":  {},
}

// memoryAllTools 所有记忆工具集合（记忆完全禁用时全部拦截）
var memoryAllTools = map[string]struct{}{
	"write_memory":  {},
	"edit_memory":   {},
	"read_memory":   {},
	"memory_search": {},
	"memory_get":    {},
}

// avatarLogComponent 日志组件标识
var avatarLogComponent = logger.ComponentAgentServer

// 编译时验证 AvatarPromptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*AvatarPromptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAvatarPromptRail 创建 AvatarPromptRail 实例。
// 对齐 Python: AvatarPromptRail.__init__()
func NewAvatarPromptRail() *AvatarPromptRail {
	r := &AvatarPromptRail{
		injectedSections: make(map[string]struct{}),
	}
	r.WithPriority(avatarPromptRailPriority)
	return r
}

// BeforeModelCall 模型调用前动态注入/移除 avatar 相关 PromptSection。
// 对齐 Python: AvatarPromptRail.before_model_call()
func (r *AvatarPromptRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 获取 SystemPromptBuilder
	builder := cbc.Agent().SystemPromptBuilder()
	if builder == nil {
		return nil
	}

	// 清除上次注入的 sections
	for name := range r.injectedSections {
		builder.RemoveSection(name)
	}
	r.injectedSections = make(map[string]struct{})

	// 读取语言
	language := builder.Language()
	if language == "" {
		language = "cn"
	}

	// 1. 注入 forbidden_memory（优先级 113）
	r.injectForbiddenMemory(builder, language)

	// 2. 从 context 获取 PermissionContext
	permCtx := sschema.PermissionContextFromCtx(ctx)
	if permCtx == nil {
		return nil
	}

	// 3. 判断数字分身模式
	isGroupDigitalAvatar := permCtx.GroupDigitalAvatar && permCtx.AvatarMode

	// 4. 数字分身身份提示词
	if isGroupDigitalAvatar {
		displayName := permCtx.AvatarPrincipalName
		if displayName == "" {
			displayName = permCtx.PrincipalUserID
		}
		content := buildAvatarPrompt(displayName, language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "avatar_identity",
			Content:  map[string]string{language: content},
			Priority: avatarPromptPriority,
		})
		r.injectedSections["avatar_identity"] = struct{}{}
	}

	// 5. 群聊记忆禁写通知
	if isGroupDigitalAvatar {
		notice := buildGroupChatMemoryNotice(language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "group_chat_memory_notice",
			Content:  map[string]string{language: notice},
			Priority: avatarPromptPriority + 1,
		})
		r.injectedSections["group_chat_memory_notice"] = struct{}{}
	}

	// 6. 记忆完全禁用
	shouldDisableMemory := !permCtx.EnableMemory && permCtx.GroupDigitalAvatar && permCtx.AvatarMode
	if shouldDisableMemory {
		content := buildMemoryFullyDisabledPrompt(language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "memory_fully_disabled",
			Content:  map[string]string{language: content},
			Priority: avatarPromptPriority + 2,
		})
		r.injectedSections["memory_fully_disabled"] = struct{}{}
	}

	// 7. 多轮交互指引
	if isGroupDigitalAvatar {
		content := buildInteractionPrompt(language)
		builder.AddSection(saprompt.PromptSection{
			Name:     "interaction_guidance",
			Content:  map[string]string{language: content},
			Priority: avatarPromptPriority + 4,
		})
		r.injectedSections["interaction_guidance"] = struct{}{}
	}

	return nil
}

// BeforeToolCall 工具调用前拦截记忆工具。
// 对齐 Python: AvatarPromptRail.before_tool_call()
func (r *AvatarPromptRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	permCtx := sschema.PermissionContextFromCtx(ctx)
	if permCtx == nil {
		return nil
	}

	shouldDisableMemory := !permCtx.EnableMemory && permCtx.GroupDigitalAvatar && permCtx.AvatarMode

	// 记忆完全禁用 — 拒绝所有记忆工具
	if shouldDisableMemory {
		if _, exists := memoryAllTools[toolInputs.ToolName]; exists {
			r.rejectTool(cbc, toolInputs, "[PERMISSION_DENIED] 记忆系统已禁用，禁止访问")
			return nil
		}
	}

	// 群聊数字分身 — 只拒绝写入
	isGroupDigitalAvatar := permCtx.GroupDigitalAvatar && permCtx.AvatarMode
	if isGroupDigitalAvatar {
		if _, exists := memoryWriteTools[toolInputs.ToolName]; exists {
			r.rejectTool(cbc, toolInputs, "[PERMISSION_DENIED] 群聊模式下禁止写入/编辑记忆文件")
			return nil
		}
	}

	return nil
}

// GetCallbacks 覆写基类回调映射，注册 BeforeModelCall + BeforeToolCall。
func (r *AvatarPromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]agentinterfaces.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// injectForbiddenMemory 注入 forbidden_memory PromptSection。
func (r *AvatarPromptRail) injectForbiddenMemory(builder saprompt.SystemPromptBuilderInterface, language string) {
	forbidden := commmem.GetForbiddenMemoryPrompt(language)
	if forbidden == "" {
		return
	}
	builder.AddSection(saprompt.PromptSection{
		Name:     "forbidden_memory",
		Content:  map[string]string{language: forbidden},
		Priority: avatarPromptPriority + 3,
	})
	r.injectedSections["forbidden_memory"] = struct{}{}
}

// rejectTool 跳过工具执行，设置拒绝消息。
// 对齐 Python: AvatarPromptRail._reject_tool()
func (r *AvatarPromptRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, toolInputs *agentinterfaces.ToolCallInputs, message string) {
	toolCallID := ""
	if toolInputs.ToolCall != nil {
		toolCallID = toolInputs.ToolCall.ID
	}
	cbc.Extra()["_skip_tool"] = true
	toolInputs.ToolResult = message
	toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, message)
	logger.Info(avatarLogComponent).
		Str("tool_name", toolInputs.ToolName).
		Str("message", message).
		Msg("AvatarPromptRail 拦截工具")
}
```

- [ ] **Step 4: 编写 4 个提示词构建函数（buildAvatarPrompt / buildGroupChatMemoryNotice / buildMemoryFullyDisabledPrompt / buildInteractionPrompt）**

在同一文件 `avatar_rail.go` 的非导出函数区块添加（文本严格对齐 Python `avatar_rail.py`）：

`buildAvatarPrompt` — 对齐 Python `_build_avatar_prompt`:
```go
func buildAvatarPrompt(principalName string, language string) string {
	if language == "cn" {
		var identity, perspective, boundary string
		if principalName != "" {
			identity = "你当前正在群聊场景中作为 **" + principalName + "** 的数字分身发言。"
			perspective = "1. **第一人称视角**：始终以 " + principalName + " 的身份和口吻回复，使用\"我\"而非\"他/她\"。"
			boundary = "2. **承诺边界**：不要代替 " + principalName + " 做出超出已知信息范围的承诺或决定。如果上下文不足，坦诚说明需要确认，而非编造事实。"
		} else {
			identity = "你当前正在群聊场景中作为用户本人的数字分身发言。"
			perspective = "1. **第一人称视角**：始终以用户本人的身份和口吻回复，使用\"我\"而非\"他/她\"。"
			boundary = "2. **承诺边界**：不要代替用户做出超出已知信息范围的承诺或决定。如果上下文不足，坦诚说明需要确认，而非编造事实。"
		}
		return "---\n\n# 数字分身模式\n\n" + identity + "以下规则**仅约束你在群聊中的对外回复方式**，不影响你的任何内部能力。\n\n## 回复风格约束\n\n" + perspective + "\n" + boundary + "\n3. **自然简洁**：回复风格应贴近真人在群聊中的表达习惯，简短、自然，避免过度正式或冗长。\n4. **不暴露身份**：不要提及自己是 AI、数字分身或机器人。\n\n## 能力不受影响\n\n数字分身模式**不限制**你的任何 agent 能力，以下功能照常执行：\n- **工具调用**：所有工具（文件操作、搜索、代码执行等）正常使用\n- **待办管理**：todo_create / todo_complete / todo_insert 等正常使用\n- **定时任务**：可以正常创建并执行 cron 任务和 heartbeat 任务\n- **技能调用**：所有已注册技能正常使用\n"
	}
	// 英文
	var identity, perspective, boundary string
	if principalName != "" {
		identity = "You are currently acting as **" + principalName + "**'s digital avatar in a group chat."
		perspective = "1. **First-person perspective**: Always reply as " + principalName + ", using \"I\" instead of \"he/she\"."
		boundary = "2. **Commitment boundary**: Do not make commitments or decisions beyond known information on behalf of " + principalName + "."
	} else {
		identity = "You are currently acting as the user's digital avatar in a group chat."
		perspective = "1. **First-person perspective**: Always reply as the user, using \"I\" instead of \"he/she\"."
		boundary = "2. **Commitment boundary**: Do not make commitments or decisions beyond known information on behalf of the user."
	}
	return "---\n\n# Digital Avatar Mode\n\n" + identity + " The rules below **only constrain your outward reply style** in group chat.\n\n## Reply Style Constraints\n\n" + perspective + "\n" + boundary + "\n3. **Natural and concise**: Reply style should resemble a real person's expression in group chat.\n4. **Do not reveal identity**: Never mention that you are an AI, digital avatar, or bot.\n"
}
```

`buildGroupChatMemoryNotice` — 对齐 Python 内联逻辑:
```go
func buildGroupChatMemoryNotice(language string) string {
	if language == "cn" {
		return "\n[群聊模式：禁止调用 write_memory/edit_memory]\n"
	}
	return "\n[Group chat mode: write_memory/edit_memory calls are prohibited]\n"
}
```

`buildMemoryFullyDisabledPrompt` — 对齐 Python `_build_memory_fully_disabled_prompt`:
```go
func buildMemoryFullyDisabledPrompt(language string) string {
	if language == "cn" {
		return "## 记忆系统 - 已完全禁用\n\n**记忆系统当前已完全禁用。**\n\n- **禁止** 使用任何记忆工具：\n  - 写入工具：write_memory、edit_memory\n  - 读取工具：read_memory、memory_search、memory_get\n- 如果用户询问历史信息或要求记住某些内容，回复：\"记忆系统当前已禁用，我无法访问历史记录或保存新信息。\"\n"
	}
	return "## Memory System - Fully Disabled\n\n**The memory system is currently fully disabled.**\n\n- **Do NOT** use any memory tools:\n  - Write tools: write_memory, edit_memory\n  - Read tools: read_memory, memory_search, memory_get\n- If the user asks about historical information or requests to remember something, reply: \"The memory system is currently disabled. I cannot access historical records or save new information.\"\n"
}
```

`buildInteractionPrompt` — 对齐 Python `_build_interaction_prompt`（完整中英双语文本，严格复制 Python 源码）:
```go
func buildInteractionPrompt(language string) string {
	if language == "cn" {
		return "## 多轮交互指引\n\n在以下情况，你必须通过追问来明确需求，不要自行假设或跳过：\n\n### 何时必须追问\n1. **缺少关键参数**：任务需要具体参数但用户未提供（如订会议室但没说楼层、时间）\n2. **需求模糊或宽泛**：用户请求范围太大或方向不明确，直接执行可能偏离意图（如\"帮我写个报告\"\"做个调研\"\"整理一下\"）\n3. **存在多种理解**：请求可以有多种解读方式，不同理解会导致完全不同的执行结果\n4. **需要确认授权**：需要 principal（你代替的人）确认或授权才能执行\n\n### 群聊追问\n如果缺少的信息可以由群聊中的某位用户提供，在回复开头加上 `[群聊追问@用户名]`：\n- 例：`[群聊追问@张三] 请问需要预约哪个楼层的会议室？`\n- 系统会自动在群聊中 @张三 并追踪回复\n\n如果缺少的信息由发送请求的人自己补充即可，在回复开头加上 `[群聊追问]`（不带@）：\n- 例：`[群聊追问] 请问会议主题是什么？`\n- 例：`[群聊追问] 你说的调研报告是关于哪个方向的？需要覆盖哪些内容？`\n- 系统会自动追踪发送者的回复\n\n### 私聊追问\n如果需要 principal（你代替的人）确认或授权，在回复开头加上 `[私聊追问]`：\n- 例：`[私聊追问] 张三要订会议室，你确认吗？`\n- 系统会自动私聊 principal 并在群聊中发送简短确认\n\n### 注意事项\n- 需求模糊时**必须追问**，不要自行猜测用户意图后直接执行，否则很可能白做\n- 追问时给出具体选项或方向提示，帮助用户快速回复（如\"是A方向还是B方向？\"而非\"你要什么？\"）\n- 追问前缀必须放在回复的最开头\n- 收到追问的回答后，继续完成任务即可，不需要再加前缀\n- 收到追问回答后，只针对当前追问的任务继续处理，不要与之前的其他任务混淆\n- 如果群聊历史中存在多个不同的任务，务必根据追问上下文区分，只处理当前任务\n"
	}
	return "## Multi-turn Interaction Guidance\n\nYou MUST follow up to clarify requirements in these situations — do NOT assume or skip:\n\n### When You Must Follow Up\n1. **Missing key parameters**: The task requires specific parameters the user hasn't provided (e.g., booking a room without specifying floor or time)\n2. **Vague or broad requests**: The request is too broad or unclear — executing directly may miss the user's intent (e.g., \"write a report\", \"do some research\", \"organize this\")\n3. **Ambiguous interpretation**: The request could be understood in multiple ways, leading to very different outcomes\n4. **Need confirmation**: You need the principal (the person you represent) to confirm or authorize\n\n### Group Follow-up\nIf the missing information can be provided by someone in the group chat, prefix your reply with `[群聊追问@Username]`:\n- Example: `[群聊追问@张三] Which floor meeting room do you need?`\n- The system will automatically @mention the user and track their reply\n\nIf the sender can provide the missing information themselves, prefix your reply with `[群聊追问]` (without @):\n- Example: `[群聊追问] What is the meeting topic?`\n- Example: `[群聊追问] What direction should the research report cover? What topics should it include?`\n- The system will automatically track the sender's reply\n\n### DM Follow-up\nIf you need the principal (the person you represent) to confirm or authorize, prefix your reply with `[私聊追问]`:\n- Example: `[私聊追问] 张三 wants to book a meeting room, do you confirm?`\n- The system will automatically DM the principal and send a brief acknowledgment in the group\n\n### Notes\n- When the request is vague, you **MUST follow up** — do NOT guess the user's intent and execute, or you'll likely waste effort\n- When following up, provide specific options or directional hints to help the user reply quickly (e.g., \"Direction A or Direction B?\" rather than \"What do you want?\")\n- The follow-up prefix must be at the very beginning of your reply\n- After receiving the answer, continue completing the task without any prefix\n- After receiving the answer, only process the current task from the follow-up, do not mix with previous tasks\n- If the group chat history contains multiple different tasks, distinguish them based on the follow-up context and only handle the current one\n"
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
go test ./internal/swarm/agents/harness/common/rails/... -v -run TestAvatarPrompt
```

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/avatar_rail.go internal/swarm/agents/harness/common/rails/avatar_rail_test.go
git commit -m "feat(rails): 实现 AvatarPromptRail 数字分身 Rail（10.6.4）"
```

---

### Task 6: 回填接入 + 目录清理

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/agents/harness/common/rails/doc.go`

- [ ] **Step 1: 修改 buildAvatarRail() 为真实构建**

在 `deep_adapter_rails.go` 中：

1. 添加 import：
```go
commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
```

2. 替换 `buildAvatarRail()` 方法：

旧代码（L311-317）：
```go
// buildAvatarRail 构建头像护栏。
// ⤵️ 10.6.3-10: AvatarRail
// 对齐 Python: _build_avatar_rail() (line 2146-2155)
func (d *DeepAdapter) buildAvatarRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 实现 AvatarRail
	return nil
}
```

新代码：
```go
// buildAvatarRail 构建数字分身护栏。
// 对齐 Python: _build_avatar_rail() (line 2146-2155)
func (d *DeepAdapter) buildAvatarRail() sainterfaces.AgentRail {
	rail := commrails.NewAvatarPromptRail()
	logger.Info(logComponent).Msg("AvatarPromptRail create success")
	return rail
}
```

- [ ] **Step 2: 移除 deep_adapter.go 中的 ⤵️ 标记**

在 `deep_adapter.go` L160-162：

旧：
```go
// avatarRail 头像护栏
// ⤵️ 10.6.3-10: AvatarRail
avatarRail sainterfaces.AgentRail
```

新：
```go
// avatarRail 数字分身护栏
avatarRail sainterfaces.AgentRail
```

- [ ] **Step 3: 更新 rails/doc.go 文件目录**

更新 `internal/swarm/agents/harness/common/rails/doc.go`：

```go
// Package rails 提供 Swarm 侧的 Rail 扩展实现（对齐 Python common/rails）。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/ 下的 Rail 实现，
// 在 agentcore 的通用 Rail 基础上增加 Swarm 专属逻辑。
//
// 文件目录：
//
//	rails/
//	├── doc.go                        # 包文档
//	├── structured_ask_user_rail.go   # StructuredAskUserRail + StructuredAskUserPayload
//	├── structured_ask_user_tool.go   # StructuredAskUserTool + 扩展 schema
//	├── avatar_rail.go                # AvatarPromptRail 数字分身 Rail
//	└── permissions/
//	    ├── doc.go                    # 包文档
//	    └── owner_scopes.go           # OwnerScopesPermissionContext 权限上下文
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/
package rails
```

- [ ] **Step 4: 运行编译和测试确认**

```bash
go build ./internal/swarm/...
go test ./internal/swarm/... -v -run "TestAvatar|TestPermission"
```

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter.go internal/swarm/agents/harness/common/rails/doc.go
git commit -m "feat(adapter): 回填 buildAvatarRail 真实构建 + 移除 ⤵️ 标记"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.6.3-10 行状态**

找到 `10.6.3-10` 行，将 `AskUser✅` 改为 `AskUser✅/Avatar✅`：

旧：
```
| 10.6.3-10 | 🔄 | Swarm Rails | AskUser✅/Avatar/Permissions/Interrupt/ProjectMemory/ResponsePrompt/RuntimePrompt/StreamEvent | `jiuwenswarm/agents/harness/common/rails/` |
```

新：
```
| 10.6.3-10 | 🔄 | Swarm Rails | AskUser✅/Avatar✅/Permissions/Interrupt/ProjectMemory/ResponsePrompt/RuntimePrompt/StreamEvent | `jiuwenswarm/agents/harness/common/rails/` |
```

- [ ] **Step 2: 更新 10.6.17 行状态**

找到 `10.6.17` 行，将 `☐` 改为 `✅`：

旧：
```
| 10.6.13-18 | ☐ | Swarm Memory | Config/Dreaming/Embeddings/External/Forbidden/RPC | `jiuwenswarm/agents/harness/common/memory/` |
```

注意：10.6.13-18 是合并行，Forbidden 只是其中 10.6.17 一个。应该将 `Forbidden` 标记为完成：

```
| 10.6.13-18 | 🔄 | Swarm Memory | Config/Dreaming/Embeddings/External/Forbidden✅/RPC | `jiuwenswarm/agents/harness/common/memory/` |
```

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 10.6.4 Avatar✅ + 10.6.17 Forbidden✅ 状态"
```
