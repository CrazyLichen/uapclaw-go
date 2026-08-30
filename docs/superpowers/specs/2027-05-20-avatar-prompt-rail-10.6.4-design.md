# 10.6.4 AvatarPromptRail + 10.6.17 Forbidden Memory 实现设计

## 概述

实现 10.6.4 小节的 AvatarPromptRail 和 10.6.17 小节的 Forbidden Memory，对齐 Python 的 `jiuwenswarm/agents/harness/common/rails/avatar_rail.py` 和 `jiuwenswarm/agents/harness/common/memory/forbidden.py`。

AvatarPromptRail 是数字分身场景的守护 Rail，在每次 LLM 调用前根据 PermissionContext 动态注入身份提示词和记忆权限约束，并在工具调用前拦截违规的记忆操作。

Forbidden Memory 提供 `GetForbiddenMemoryPrompt()` 函数，从 config.yaml 读取禁止记忆配置并格式化为提示词，供 AvatarPromptRail 注入系统提示词。

## 在 Agent 会话中的流程位置与作用

```
用户消息 → Gateway → E2A → AgentServer → DeepAdapter.ProcessMessage()
  ├─ 步骤 10-11: WithPermissionContextValue(ctx, req.PermissionContext)  ← 从 metadata 注入权限上下文
  ├─ buildAgentRails()
  │    ├─ 步骤 15: buildAvatarRail()  ← ★ 10.6.4 位置（当前 return nil → 改为真实构建）
  │    └─ ...
  ├─ DeepAgent.Run()
  │    ├─ ReAct Loop:
  │    │    ├─ BeforeModelCall()       ← AvatarPromptRail.BeforeModelCall: 注入/移除 PromptSection
  │    │    │    ├─ forbidden_memory        (优先级 113) — GetForbiddenMemoryPrompt()
  │    │    │    ├─ avatar_identity         (优先级 110) — 数字分身身份提示词
  │    │    │    ├─ group_chat_memory_notice(优先级 111) — 群聊禁止写记忆通知
  │    │    │    ├─ memory_fully_disabled   (优先级 112) — 记忆完全禁用提示词
  │    │    │    └─ interaction_guidance    (优先级 114) — 多轮交互追问指引
  │    │    ├─ LLM 调用
  │    │    ├─ BeforeToolCall()       ← AvatarPromptRail.BeforeToolCall: 拦截记忆工具
  │    │    │    ├─ 记忆完全禁用 → 拒绝所有 5 个记忆工具
  │    │    │    └─ 群聊数字分身 → 拒绝 write_memory/edit_memory
  │    │    └─ ...
  │    └─ ...
  └─ cleanup
```

**AvatarPromptRail 作用**：数字分身场景下的"身份约束+权限守卫"——确保 LLM 以用户本人身份回复、不在群聊中暴露 AI 身份、按配置控制记忆读写权限。

**Forbidden Memory 作用**：将敏感信息禁止记忆规则（密码、密钥、身份证号等）注入系统提示词，防止 LLM 将敏感信息存入记忆系统。

## Python vs Go 对齐分析

### Python 层级结构

```
jiuwenswarm/agents/harness/common/
  memory/forbidden.py               → get_forbidden_memory_prompt()
  rails/avatar_rail.py              → AvatarPromptRail(DeepAgentRail)
  rails/permissions/owner_scopes.py → PermissionContext + TOOL_PERMISSION_CONTEXT
  rails/permissions/tool_permission_context.py → TOOL_PERMISSION_CHANNEL_ID
```

### Go 当前结构

```
swarm/schema/permission.go                      → PermissionContext + WithPermissionContextValue  ✅ (但缺 3 字段)
swarm/server/adapter/owner_scopes_permission.go  → OwnerScopesPermissionContext                    ✅ (需迁移)
agentcore/harness/rails/base.go                  → DeepAgentRail                                    ✅ 对齐
agentcore/single_agent/prompts/builder.go        → PromptSection + SystemPromptBuilder              ✅ 对齐
agentcore/harness/rails/interrupt/interrupt_base.go → skipTool 模式                                ✅ 对齐
```

### Go 缺失部分

| 能力 | Python | Go 当前 |
|------|--------|---------|
| AvatarPromptRail | ✅ | ❌ (buildAvatarRail() return nil) |
| get_forbidden_memory_prompt | ✅ | ❌ |
| PermissionContext.EnableMemory 字段 | ✅ | ❌ (schema.PermissionContext 无此字段) |
| PermissionContext.AvatarPrincipalName 字段 | ✅ | ❌ |
| PermissionContext.AvatarMode 字段 | ✅ | ❌ |
| 数字分身身份提示词 | ✅ | ❌ |
| 群聊追问指引提示词 | ✅ | ❌ |
| 记忆禁用提示词 | ✅ | ❌ |
| 记忆工具拦截 | ✅ | ❌ |
| OwnerScopesPermissionContext 在 permissions 包 | ✅ | ❌ (在 adapter 包) |

## 设计

### 段 1：基础设施层 — PermissionContext 扩展 + OwnerScopesPermissionContext 迁移

#### 1.1 扩展 `schema.PermissionContext`

**文件**: `swarm/schema/permission.go`

新增 3 个字段：

```go
type PermissionContext struct {
    PrincipalUserID    string `json:"principal_user_id"`
    TriggeringUserID   string `json:"triggering_user_id"`
    ChannelID          string `json:"channel_id"`
    GroupDigitalAvatar bool   `json:"group_digital_avatar"`
    WebUserID          string `json:"web_user_id"`
    EnableMemory       bool   `json:"enable_memory"`        // 新增：默认 true
    AvatarPrincipalName string `json:"avatar_principal_name"` // 新增
    AvatarMode         bool   `json:"avatar_mode"`           // 新增：是否为群聊消息
}
```

同步更新：
- `NewPermissionContext()` — `EnableMemory` 默认 `true`
- `NewPermissionContextFromDict()` — 解析 `enable_memory`(bool)、`avatar_principal_name`(string)、`avatar_mode`(bool)
- `ToDict()` — 输出 3 个新字段
- 新增 3 个选项函数：`WithPermissionEnableMemory`、`WithPermissionAvatarPrincipalName`、`WithPermissionAvatarMode`
- 测试补充

#### 1.2 迁移 `OwnerScopesPermissionContext`

**从**: `swarm/server/adapter/owner_scopes_permission.go`
**到**: `swarm/agents/harness/common/rails/permissions/owner_scopes.go`

新建 `swarm/agents/harness/common/rails/permissions/` 目录：
- `doc.go` — 包文档
- `owner_scopes.go` — OwnerScopesPermissionContext（从 adapter 搬入，内容不变）

OwnerScopesPermissionContext 无外部包引用，迁移只需删除原文件、新建目标文件、更新 adapter 内部 import（如有）。

#### 1.3 清理

- 删除 `swarm/server/adapter/owner_scopes_permission.go`
- 删除 `swarm/server/rails/` 目录（只剩空 doc.go，StructuredAskUserRail 已迁移）

### 段 2：Forbidden Memory

#### 2.1 包位置

```
swarm/agents/harness/common/memory/
├── doc.go           # 包文档
└── forbidden.go     # MemoryForbiddenConfig + GetForbiddenMemoryPrompt
```

对齐 Python `jiuwenswarm/agents/harness/common/memory/forbidden.py`

#### 2.2 核心实现

```go
// MemoryForbiddenConfig 记忆禁止配置
type MemoryForbiddenConfig struct {
    Enabled     bool              `json:"enabled"`
    Patterns    []string          `json:"patterns"`
    Description map[string]string `json:"description"` // "zh"/"en" → 描述文本
}

// getMemoryForbiddenConfig 从 config 读取 memory.forbidden_memory_definition
// 对齐 Python: _get_memory_forbidden_config()
func getMemoryForbiddenConfig() *MemoryForbiddenConfig

// GetForbiddenMemoryPrompt 格式化禁止记忆提示词。enabled=false 返回空串。
// 对齐 Python: get_forbidden_memory_prompt(language)
func GetForbiddenMemoryPrompt(language string) string
```

#### 2.3 配置读取

使用 `config.New("")` → `cfg.Load()` 读取 `memory.forbidden_memory_definition` 节点，和 DeepAdapter.initInstance 中读取 config 的方式一致。

#### 2.4 提示词模板

严格复制 Python 的中/英双语文本，包含：
- 中文：`### 记忆限制规则` + 描述 + patterns 列表 + 执行要求
- 英文：`### Memory Restriction Rules` + 对应内容

#### 2.5 测试

- `TestGetMemoryForbiddenConfig_正常读取`
- `TestGetMemoryForbiddenConfig_配置缺失`
- `TestGetForbiddenMemoryPrompt_禁用返回空`
- `TestGetForbiddenMemoryPrompt_中文输出`
- `TestGetForbiddenMemoryPrompt_英文输出`

### 段 3：AvatarPromptRail

#### 3.1 包位置

```
swarm/agents/harness/common/rails/
├── avatar_rail.go       # AvatarPromptRail 实现
└── avatar_rail_test.go  # 测试
```

对齐 Python `jiuwenswarm/agents/harness/common/rails/avatar_rail.py`

#### 3.2 结构体

```go
// AvatarPromptRail 数字分身 Rail — 处理所有 per-request 的 avatar 逻辑。
// 对齐 Python: AvatarPromptRail(DeepAgentRail)
type AvatarPromptRail struct {
    rails.DeepAgentRail
    // injectedSections 已注入的 PromptSection 名称集合
    injectedSections map[string]struct{}
}
```

优先级：`85`（对齐 Python `priority: int = 85`）

#### 3.3 BeforeModelCall 钩子

每次 LLM 调用前执行，动态注入 PromptSection：

1. 获取 SystemPromptBuilder（从 `cbc.Agent().SystemPromptBuilder()`）
2. 清除上次注入的 sections（遍历 `injectedSections` → `RemoveSection`）
3. 读取 language（`builder.Language()`，默认 `"cn"`）
4. 注入 `forbidden_memory`（优先级 113）— 调 `GetForbiddenMemoryPrompt(language)`
5. 从 ctx 获取 PermissionContext（`schema.PermissionContextFromCtx(ctx)`）
6. 若 perm_ctx == nil → return
7. 若 `GroupDigitalAvatar && AvatarMode` → 注入 `avatar_identity`（优先级 110）
8. 若 `GroupDigitalAvatar && AvatarMode` → 注入 `group_chat_memory_notice`（优先级 111）
9. 若 `!EnableMemory && GroupDigitalAvatar && AvatarMode` → 注入 `memory_fully_disabled`（优先级 112）
10. 若 `GroupDigitalAvatar && AvatarMode` → 注入 `interaction_guidance`（优先级 114）

5 种 PromptSection 的内容和 Python 完全一致（中/英双语），封装为 4 个私有构建函数：

| Go 函数 | Python 函数 | 输出 |
|---------|------------|------|
| `buildAvatarPrompt(principalName, language)` | `_build_avatar_prompt` | 数字分身身份提示词 |
| `buildGroupChatMemoryNotice(language)` | 内联逻辑 | 禁写记忆通知 |
| `buildMemoryFullyDisabledPrompt(language)` | `_build_memory_fully_disabled_prompt` | 记忆完全禁用提示词 |
| `buildInteractionPrompt(language)` | `_build_interaction_prompt` | 多轮交互追问指引 |

#### 3.4 BeforeToolCall 钩子

1. 获取 toolName（从 `cbc.Inputs().(*ToolCallInputs).ToolName`）
2. 从 ctx 获取 PermissionContext（`schema.PermissionContextFromCtx(ctx)`）
3. 若 perm_ctx == nil → return
4. `should_disable_memory = !EnableMemory && GroupDigitalAvatar && AvatarMode`
   → 禁止所有记忆工具: `write_memory`, `edit_memory`, `read_memory`, `memory_search`, `memory_get`
   → `rejectTool("[PERMISSION_DENIED] 记忆系统已禁用，禁止访问")`
5. `is_group_digital_avatar = GroupDigitalAvatar && AvatarMode`
   → 禁止写入: `write_memory`, `edit_memory`
   → `rejectTool("[PERMISSION_DENIED] 群聊模式下禁止写入/编辑记忆文件")`

#### 3.5 rejectTool 方法

对齐 Python `_reject_tool` + Go 现有 `BaseInterruptRail.skipTool` 模式：

```go
func (r *AvatarPromptRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, message string) {
    toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
    if !ok { return }
    toolCallID := ""
    if toolInputs.ToolCall != nil { toolCallID = toolInputs.ToolCall.ID }
    cbc.Extra()["_skip_tool"] = true
    toolInputs.ToolResult = message
    toolInputs.ToolMsg = llmschema.NewToolMessage(toolCallID, message)
}
```

#### 3.6 GetCallbacks 覆写

注册 `BeforeModelCall` + `BeforeToolCall` 两个回调事件。

#### 3.7 回填接入

**文件**: `swarm/server/adapter/deep_adapter_rails.go`

```go
func (d *DeepAdapter) buildAvatarRail() sainterfaces.AgentRail {
    rail := commrails.NewAvatarPromptRail()
    logger.Info(logComponent).Msg("AvatarPromptRail create success")
    return rail
}
```

- 移除 `⤵️ 10.6.3-10: 实现 AvatarRail` 注释
- `buildDynamicRail()` 中 `"buildAvatarRail"` case 保持不变

**文件**: `swarm/server/adapter/deep_adapter.go`

- `avatarRail sainterfaces.AgentRail` 字段移除 `⤵️ 10.6.3-10: AvatarRail` 注释

#### 3.8 测试

| 测试用例 | 覆盖场景 |
|---------|---------|
| `TestAvatarPromptRail_BeforeModelCall_无PermissionContext` | perm_ctx 为 nil 时跳过 |
| `TestAvatarPromptRail_BeforeModelCall_数字分身模式` | 注入 4 种 section（identity + notice + interaction + forbidden） |
| `TestAvatarPromptRail_BeforeModelCall_记忆完全禁用` | 注入 memory_fully_disabled |
| `TestAvatarPromptRail_BeforeModelCall_非数字分身` | 只注入 forbidden_memory |
| `TestAvatarPromptRail_BeforeModelCall_清除旧注入` | 连续两次调用，第一次注入的 section 被清除 |
| `TestAvatarPromptRail_BeforeToolCall_群聊禁写` | 拦截 write_memory/edit_memory |
| `TestAvatarPromptRail_BeforeToolCall_记忆完全禁用` | 拦截所有 5 个记忆工具 |
| `TestAvatarPromptRail_BeforeToolCall_正常放行` | 非记忆工具不拦截 |
| `TestBuildAvatarPrompt_中文` | 提示词内容验证 |
| `TestBuildAvatarPrompt_英文` | 提示词内容验证 |
| `TestBuildInteractionPrompt_中文` | 追问指引内容验证 |

### 段 4：回填接入 + 目录清理 + IMPLEMENTATION_PLAN 更新

#### 4.1 doc.go 更新

**`swarm/agents/harness/common/rails/doc.go`**：

```
rails/
├── doc.go                        # 包文档
├── structured_ask_user_rail.go   # StructuredAskUserRail
├── structured_ask_user_tool.go   # StructuredAskUserTool
├── avatar_rail.go                # AvatarPromptRail
└── permissions/
    ├── doc.go                    # 包文档
    └── owner_scopes.go           # OwnerScopesPermissionContext
```

**新建 `swarm/agents/harness/common/memory/doc.go`**

#### 4.2 清理

- 删除 `swarm/server/adapter/owner_scopes_permission.go`
- 删除 `swarm/server/rails/` 目录

#### 4.3 IMPLEMENTATION_PLAN.md 状态更新

| 变更 | 说明 |
|------|------|
| 10.6.3-10 行 | AskUser✅ → AskUser✅/Avatar✅ |
| 10.6.17 行 | ☐ → ✅ |

## 文件变更汇总

| 操作 | 文件路径 | 说明 |
|------|---------|------|
| 修改 | `swarm/schema/permission.go` | 新增 3 字段 + FromDict + 选项函数 |
| 修改 | `swarm/schema/permission_test.go` | 补充新字段测试 |
| 新建 | `swarm/agents/harness/common/rails/permissions/doc.go` | 包文档 |
| 新建 | `swarm/agents/harness/common/rails/permissions/owner_scopes.go` | 从 adapter 迁入 |
| 删除 | `swarm/server/adapter/owner_scopes_permission.go` | 已迁移 |
| 新建 | `swarm/agents/harness/common/memory/doc.go` | 包文档 |
| 新建 | `swarm/agents/harness/common/memory/forbidden.go` | Forbidden Memory 实现 |
| 新建 | `swarm/agents/harness/common/memory/forbidden_test.go` | Forbidden Memory 测试 |
| 新建 | `swarm/agents/harness/common/rails/avatar_rail.go` | AvatarPromptRail 实现 |
| 新建 | `swarm/agents/harness/common/rails/avatar_rail_test.go` | AvatarPromptRail 测试 |
| 修改 | `swarm/agents/harness/common/rails/doc.go` | 更新文件目录 |
| 修改 | `swarm/server/adapter/deep_adapter_rails.go` | buildAvatarRail 真实构建 + 移除 ⤵️ |
| 修改 | `swarm/server/adapter/deep_adapter.go` | 移除 ⤵️ 标记 |
| 删除 | `swarm/server/rails/` | 整个目录删除 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 10.6.3-10 + 10.6.17 状态更新 |
