# 10.6.5 Permissions Rail + SecurityRail 回填设计

## 背景

10.6.5 是 IMPLEMENTATION_PLAN.md 中 10.6.3-10（Swarm Rails）分组的核心子项。agentcore 层的 9.19 Security Rail 已全部实现（PermissionEngine / TieredPolicy / ShellAST / ExternalDirectoryChecker / Patterns / Suggestions / PermissionInterruptRail / BaseSecurityRail / SafetyPromptRail），但 swarm 侧 adapter 层的胶水代码尚未回填，导致运行时无权限护栏生效。

本设计覆盖 10.6.5 Permissions 全量回填 + SecurityRail（SafetyPromptRail）一行回填。

## 流程位置

PermissionInterruptRail 在 Agent 会话中的拦截位置：

```
用户请求入口 (DeepAdapter.processMessageImpl / CodeAdapter)
    │
    ├─ ① 设置权限上下文 context (TOOL_PERMISSION_CHANNEL_ID + PermissionContext)
    │
    ▼
Agent 执行 (Runner / ReAct Loop)
    │
    ├─ ② PermissionInterruptRail.BeforeToolCall [priority=90, 最先拦截]
    │   ├─ 宿主场景钩子 (数字分身/owner_scopes) 优先
    │   ├─ PermissionEngine.CheckPermission → 分层策略评估
    │   │   ├─ EvaluateTieredPolicy (整工具deny→内置规则→用户规则→approval_overrides→基线→默认)
    │   │   └─ ExternalDirectoryChecker (外部路径)
    │   ├─ auto_confirm 检查
    │   ├─ 宿主确认钩子 (ACP 频道) 或 内置中断
    │   └─ 用户确认后持久化
    │
    ├─ ③ SafetyPromptRail.BeforeModelCall [priority=85, 注入安全提示词]
    │
    ├─ ④ AvatarPromptRail.BeforeToolCall [priority=85, 记忆工具拦截]
    │
    ▼
工具实际执行
    │
    ▼
    ⑤ 请求结束：context 自动释放（Go 无需手动 cleanup）
```

## 实现步骤（6 步，按依赖顺序）

### 步骤 1：owner_scopes 补充

**文件**: `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`

在已有 `OwnerScopesPermissionContext` 基础上，补充 Python 端的 3 个核心函数：

| 函数 | 对齐 Python | 说明 |
|------|------------|------|
| `CheckAvatarPermission(toolName string, toolArgs map[string]any, channelID string, sessionID string) (string, error)` | `check_avatar_permission()` | 综合判断 allow/deny。ASK 自动降级为 DENY（数字分身不支持交互审批） |
| `ResolveOwnerScopeLevel(scopeCfg map[string]any, toolName string, toolArgs map[string]any) (string, error)` | `_resolve_owner_scope_level()` | 三级优先匹配：patterns > wildcard(`*`) > defaults |
| `MatchArgs(pattern string, args map[string]any, matchType string) (bool, error)` | `_match_args()` | 复用 agentcore security 包的 MatchWildcard / PathMatcher / CommandMatcher |

**CheckAvatarPermission 逻辑**（对齐 Python `owner_scopes.py`）：
1. 从 `GetConfig()["permissions"]` 读取权限配置
2. 按 `owner_scopes.<channel>.<user>` 查找 scope 配置
3. 调用 `ResolveOwnerScopeLevel` 获取 owner 级别
4. 调用 `PermissionEngine.EvaluateGlobalPolicyDirectly` 获取全局级别
5. 取两者中更严结果
6. ASK 自动降级为 DENY（数字分身场景不支持交互审批）

**ResolveOwnerScopeLevel 逻辑**（三级优先）：
```
1. scopeCfg.tools.<toolName>.patterns.<pattern> → 精确 pattern 匹配
2. scopeCfg.tools.<toolName>.* → 工具级通配
3. scopeCfg.defaults.* → 默认级别
4. 无匹配 → 返回 ""（空字符串，调用方按无处理）
```

**MatchArgs 逻辑**：
- `matchType == "path"` → `PathMatcher.Match(pattern, value)`
- `matchType == "command"` → `CommandMatcher.Match(pattern, value)`
- `matchType == "url"` → `URLMatcher.Match(pattern, value)`
- 其他 → `MatchWildcard(value, pattern)`

**依赖**: agentcore 的 `harnesssecurity` 包（PermissionEngine / PathMatcher 等），以及 `internal/common/config` 包（GetConfig）

### 步骤 2：buildPermissionRail 回填（Deep + Code）

**文件**: `internal/swarm/server/adapter/deep_adapter_rails.go` + `code_adapter.go`

对齐 Python `interrupt_helpers.py:build_permission_rail()`。

**DeepAdapter.buildPermissionRail** 核心逻辑：

```go
func (d *DeepAdapter) buildPermissionRail(configBase map[string]any) sainterfaces.AgentRail {
    permissionConfig := extractMap(configBase, "permissions")
    if permissionConfig == nil || !extractBool(permissionConfig, "enabled") {
        return nil
    }

    toolNames := collectOptionalToolTags(permissionConfig)
    llm := d.model  // DeepAdapter 已有 _model 字段
    modelName := extractModelName(configBase)

    // 构建 ToolPermissionHost 7 个回调
    host := &security.ToolPermissionHost{
        GetPermissionsSnapshot:   d.getPermissionsSnapshot,
        PersistAllowRule:         d.persistAllowRule,
        ResolveWorkspaceDir:      d.resolveWorkspaceDir,
        PermissionYAMLPath:       d.getPermissionYAMLPath(),
        ToolPermissionChecksActive: func() bool { return true },
        RequestPermissionConfirmation: d.requestPermissionConfirmation,
        PermissionSceneHook:      d.permissionSceneHook,
    }

    workspaceRoot := ""
    if dir, err := workspace.GetWorkspaceDir(); err == nil {
        workspaceRoot = dir
    }

    return security.BuildPermissionInterruptRail(
        permissionConfig, nil, host, workspaceRoot, llm, modelName,
        secrail.NewPermissionInterruptRail,  // PermissionRailConstructor
    )
}
```

**CodeAdapter.buildPermissionRail** 逻辑相同，但额外传入 `llm` 和 `modelName` 参数（CodeAdapter 已有这些字段）。

**7 个 Host 回调实现**：

| 回调 | 实现方式 | 对齐 Python |
|------|---------|------------|
| `GetPermissionsSnapshot` | `config.GetConfig()["permissions"]` | `get_config().get("permissions")` |
| `PersistAllowRule` | 读取 YAML → 替换 permissions 节 → 写回 | `_persist_allow_rule` |
| `ResolveWorkspaceDir` | `workspace.GetWorkspaceDir()` | `get_workspace_dir()` |
| `PermissionYAMLPath` | `config.GetConfigFile()` | `get_config_file()` |
| `ToolPermissionChecksActive` | `func() bool { return true }` | 暂无开关需求 |
| `RequestPermissionConfirmation` | 读取 context channelID，ACP 走 JSON-RPC，非 ACP 返回 `ConfirmActionInterrupt` | `_request_permission_confirmation` |
| `PermissionSceneHook` | 从 context 取 PermissionContext，数字分身调 CheckAvatarPermission，owner_scopes 调 ResolveOwnerScopeLevel | `_permission_scene_hook` |

**RequestPermissionConfirmation 细节**（对齐 Python）：
- 从 context 读取 channelID
- `channelID != "acp"` → 返回 `ConfirmActionInterrupt`（走内置中断流程）
- `channelID == "acp"` → 通过 ACP output manager 发送 `session/request_permission` JSON-RPC
- 解析响应：`allow-once` / `allow-always` / `reject-once` / `cancelled`

**PermissionSceneHook 细节**（对齐 Python）：
- 从 `input.Ctx` context 中读取 `PermissionContext`（`sschema.PermissionContextFromCtx`）
- `permCtx == nil` → 返回 nil（继续走引擎）
- `permCtx.Scene == "group_digital_avatar"`:
  - `userInput != nil` → reject（数字分身不支持交互审批）
  - 否则调 `CheckAvatarPermission` → allow/approve 或 reject
- 其他 owner_scopes 场景：读 config owner_scopes → `ResolveOwnerScopeLevel` → approve/reject

**collectOptionalToolTags 辅助函数**：
从 `permissions.tools` 和 `permissions.rules` 中收集工具名标签列表，仅作展示/日志用（PermissionInterruptRail 对所有工具拦截，不限 toolNames 子集）。

### 步骤 3：_update_permission_rail 热重载

**文件**: `internal/swarm/server/adapter/deep_adapter.go`

对齐 Python `_update_permission_rail()`：

```go
func (d *DeepAdapter) updatePermissionRail(configBase map[string]any) {
    permissionConfig := extractMap(configBase, "permissions")

    if d.permissionRail != nil {
        // 已有 rail → 原地更新配置
        if rail, ok := d.permissionRail.(*secrail.PermissionInterruptRail); ok {
            rail.UpdateConfig(permissionConfig)
            logger.Info(logComponent).Msg("permissionRail 配置热更新完成")
        }
        return
    }

    // 首次启用 → 创建新 rail
    if permissionConfig != nil && extractBool(permissionConfig, "enabled") {
        rail := d.buildPermissionRail(configBase)
        if rail != nil {
            d.permissionRail = rail
            logger.Info(logComponent).Msg("permissionRail 首次创建完成（热重载触发）")
        }
    }
}
```

在配置热更新路径中调用（对齐 Python `_get_current_agent_rails` 中 `self._update_permission_rail(config_base)`）。

### 步骤 4：请求级 Context 注入

**文件**: `internal/swarm/server/adapter/deep_adapter.go` + `code_adapter.go`

对齐 Python `TOOL_PERMISSION_CHANNEL_ID.set()` + `setup_permission_context()`。

Go 端已实现 context 注入工具（`internal/swarm/schema/permission.go`）：
- `sschema.WithToolPermissionChannelID(ctx, channelID)` 
- `sschema.WithPermissionContextValue(ctx, permCtx)`

在 `processMessageImpl` / `processMessageStreamImpl` 入口处：

```go
func (d *DeepAdapter) processMessageImpl(ctx context.Context, ...) {
    // 注入权限上下文
    ctx = sschema.WithToolPermissionChannelID(ctx, channelID)
    permCtx := sschema.NewPermissionContextFromRequest(request)  // 从 request.metadata 构建
    if permCtx != nil {
        ctx = sschema.WithPermissionContextValue(ctx, permCtx)
    }

    // ... 执行 agent
    // 无需 defer cleanup，Go context 自动释放
}
```

**与 Python 的差异**：Python 使用 ContextVar + reset token 模式，需要 `defer cleanup_permission_context(token)`；Go 使用 context.Context 注入，随请求自动释放，更简洁。

**NewPermissionContextFromRequest**（新辅助函数，在 `swarm/schema/permission.go` 中）：
从 `request.Metadata` 构建 `PermissionContext`，对齐 Python `setup_permission_context(request)` 的字段映射逻辑。

### 步骤 5：permissions_config_rpc

**新文件**: `internal/swarm/agents/harness/common/rails/permissions/permissions_config_rpc.go`

对齐 Python `permissions_config_rpc.py`，实现 10 个 PERMISSIONS_* RPC 方法分发。

```go
// permissionsCfgMethods 权限配置 RPC 方法集合
var permissionsCfgMethods = map[schema.ReqMethod]bool{
    schema.ReqMethodPermissionsToolsGet:            true,
    schema.ReqMethodPermissionsToolsSet:            true,
    schema.ReqMethodPermissionsToolsUpdate:         true,
    schema.ReqMethodPermissionsToolsDelete:         true,
    schema.ReqMethodPermissionsRulesGet:            true,
    schema.ReqMethodPermissionsRulesCreate:         true,
    schema.ReqMethodPermissionsRulesUpdate:         true,
    schema.ReqMethodPermissionsRulesDelete:         true,
    schema.ReqMethodPermissionsApprovalOverridesGet:  true,
    schema.ReqMethodPermissionsApprovalOverridesDelete: true,
}

// DispatchPermissionsConfigRequest 执行一条权限配置 RPC
func DispatchPermissionsConfigRequest(request schema.AgentRequest) schema.AgentResponse {
    // 按 req_method 分发到对应 config 操作
}
```

每个 RPC 方法底层调用 Go 端已有的 config CRUD 函数。需确认以下 config 层函数是否已存在（如不存在需补充）：
- `GetPermissionsTools()`
- `ReplacePermissionsToolsInConfig(tools)`
- `UpdatePermissionsToolInConfig(tool, level)`
- `DeletePermissionsToolInConfig(tool)`
- `GetPermissionsRules()`
- `CreatePermissionsRuleInConfig(rule)`
- `UpdatePermissionsRuleInConfig(id, patch)`
- `DeletePermissionsRuleInConfig(id)`
- `GetPermissionsApprovalOverrides()`
- `DeletePermissionsApprovalOverrideInConfig(id)`

**注册位置**: 在 `AgentWebSocketServer` 的 RPC 分发表中注册这 10 个方法，对齐 Python 中 `register_method` 的注册方式。

### 步骤 6：SecurityRail 回填 + 状态更新 + 标记清理

**6a. SecurityRail 回填**

`deep_adapter_rails.go` 中 `buildSecurityRail`：

```go
func (d *DeepAdapter) buildSecurityRail(configBase map[string]any) sainterfaces.AgentRail {
    rail := secrail.NewSafetyPromptRail()
    logger.Info(logComponent).Msg("SafetyPromptRail 创建成功")
    return rail
}
```

`code_adapter.go` 中同理。

**6b. IMPLEMENTATION_PLAN.md 状态更新**

- 10.6.3-10 行：Permissions 标记为 ✅
- 9.19 Security Rail 确认为 ✅（已由之前提交完成）

**6c. 回填标记清理**

清理所有 `⤵️ 10.6.3-10: PermissionInterruptRail` 和 `⤵️ 10.6.3-10: SecurityRail` 标记：
- `deep_adapter_rails.go` — buildPermissionRail / buildSecurityRail
- `deep_adapter.go` — updatePermissionRail / 字段注释
- `code_adapter.go` — buildPermissionRail / buildCodeAgentRails
- `agentcore/harness/deep_agent.go` — 9.11 回填标记（PermissionInterruptRail 在 agentcore 层已通过 9.19 实现）
- `agentcore/harness/schema/config.go` — PermissionHost 类型占位

## 文件变更清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `swarm/agents/harness/common/rails/permissions/owner_scopes.go` | 修改 | 补充 CheckAvatarPermission / ResolveOwnerScopeLevel / MatchArgs |
| `swarm/agents/harness/common/rails/permissions/owner_scopes_test.go` | 修改 | 补充 3 个函数的测试 |
| `swarm/agents/harness/common/rails/permissions/permissions_config_rpc.go` | 新增 | 10 个 RPC 方法分发 |
| `swarm/agents/harness/common/rails/permissions/permissions_config_rpc_test.go` | 新增 | RPC 分发测试 |
| `swarm/agents/harness/common/rails/permissions/doc.go` | 修改 | 更新文件目录 |
| `swarm/schema/permission.go` | 修改 | 补充 NewPermissionContextFromRequest |
| `swarm/schema/permission_test.go` | 修改 | 补充测试 |
| `swarm/server/adapter/deep_adapter_rails.go` | 修改 | buildPermissionRail + buildSecurityRail 实现 |
| `swarm/server/adapter/deep_adapter.go` | 修改 | updatePermissionRail + 请求级 context 注入 |
| `swarm/server/adapter/code_adapter.go` | 修改 | buildPermissionRail + 请求级 context 注入 |
| `agentcore/harness/deep_agent.go` | 修改 | 清理 9.11 回填标记 |
| `agentcore/harness/schema/config.go` | 修改 | PermissionHost 类型占位确认/回填 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 状态更新 |

## 测试策略

- **步骤 1**: `owner_scopes_test.go` — CheckAvatarPermission / ResolveOwnerScopeLevel / MatchArgs 单元测试，mock config
- **步骤 2**: `deep_adapter_rails_test.go` — buildPermissionRail 的 enabled=false/true 分支、host 回调注入验证
- **步骤 3**: `deep_adapter_test.go` — updatePermissionRail 的已有 rail 更新 / 首次创建分支
- **步骤 4**: `permission_test.go` — NewPermissionContextFromRequest 字段映射测试
- **步骤 5**: `permissions_config_rpc_test.go` — 10 个 RPC 分发 + 错误处理
- **步骤 6**: SecurityRail 回填验证 + 标记清理确认
