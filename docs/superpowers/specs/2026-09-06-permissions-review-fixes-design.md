# 10.6.5 Permissions 审查修复设计

## 背景

10.6.5 Permissions Rail 全量实现已完成（两个提交 `dddb0f54` + `7f8d3ace`），合并 origin/main 审查修复后（`b365376d`）编译和测试均通过。审查发现 3 个需修复的问题。

## 修复项

### 1. `processMessageStreamImpl` 未调用 `NewPermissionContextFromRequest`

**问题**：`processMessageImpl`（L713-720）正确调用了 `NewPermissionContextFromRequest(req.ChannelID, req.Metadata)` 从 metadata 构建权限上下文，但 `processMessageStreamImpl`（L862-867）只使用了 `req.PermissionContext`，跳过了从 metadata 构建。

**后果**：流式请求如果 `req.PermissionContext` 为 nil（metadata 中有 `avatar_mode=true` 但请求对象未预填），权限上下文丢失，数字分身场景的 owner_scopes 检查不生效。

**修复**：`processMessageStreamImpl` L862-867 完全对齐 `processMessageImpl` L713-720：

```go
// 修复前（L862-867）
ctx = schema.WithToolPermissionChannelID(ctx, req.ChannelID)
if req.PermissionContext != nil {
    ctx = schema.WithPermissionContextValue(ctx, req.PermissionContext)
}

// 修复后（对齐 processMessageImpl）
ctx = schema.WithToolPermissionChannelID(ctx, req.ChannelID)
permCtx := schema.NewPermissionContextFromRequest(req.ChannelID, req.Metadata)
if permCtx != nil {
    ctx = schema.WithPermissionContextValue(ctx, permCtx)
} else if req.PermissionContext != nil {
    ctx = schema.WithPermissionContextValue(ctx, req.PermissionContext)
}
```

**影响文件**：`internal/swarm/server/adapter/deep_adapter.go`

### 2. `llm any` → `*llm.Model`

**问题**：permissions 模块中 5 处 `llm any` 参数/字段，用 `any` 规避了不存在的循环依赖。7.8 审查已将 memory 模块的 `llm any` 修复为 `*llm.Model`，permissions 应保持一致。

**循环依赖验证**：
- `security` 包已依赖 `foundation/llm/schema`（models.go）
- `llm/schema` 不依赖 `llm` 主包
- `llm` 主包不依赖 `harness/*`
- **结论：`security` → `llm` 主包无循环依赖**

**5 处替换**：

| 文件 | 行 | 当前 | 替换为 |
|------|----|------|--------|
| `security/permission_engine.go:26` | 字段 | `llm any` | `llm *llm.Model` |
| `security/permission_engine.go:46` | 构造参数 | `NewPermissionEngine(config, llm any, ...)` | `NewPermissionEngine(config, llm *llm.Model, ...)` |
| `security/permission_engine.go:86` | 方法参数 | `UpdateLLM(llm any, ...)` | `UpdateLLM(llm *llm.Model, ...)` |
| `security/models.go:145` | 函数类型 | `PermissionRailConstructor(..., llm any, ...)` | `PermissionRailConstructor(..., llm *llm.Model, ...)` |
| `rails/security/tool_security_rail.go:83` | 构造参数 | `NewPermissionInterruptRail(..., llm any, ...)` | `NewPermissionInterruptRail(..., llm *llm.Model, ...)` |

**新增 import**：
- `security/permission_engine.go`：添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`
- `security/models.go`：已有 `llmschema`，添加 `llm` 主包
- `rails/security/tool_security_rail.go`：添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`

**影响文件**：
- `internal/agentcore/harness/security/permission_engine.go`
- `internal/agentcore/harness/security/models.go`
- `internal/agentcore/harness/rails/security/tool_security_rail.go`
- `internal/agentcore/harness/security/factory.go`（构造函数签名需同步）
- `internal/agentcore/harness/deep_agent.go`（调用方传入 `d.model` 已是 `*llm.Model`，无需改）
- `internal/swarm/server/adapter/deep_adapter_rails.go`（构造闭包签名需同步）
- `internal/swarm/server/adapter/code_adapter.go`（构造闭包签名需同步）

### 3. `permissions_config_rpc.go` 保持 `map[string]any`

**决策**：保持 `map[string]any` 操作风格，对齐 Python dict 操作。`ApprovalOverrideEntry` 和 `PermissionsSection` 在 `DeepAgentConfig` 体系（`harness/schema/config.go` + `deep_agent.go`）中有真实使用，保留不动。

**无需修改**。

## 实施步骤

1. 修复 `processMessageStreamImpl` 权限上下文注入（1 处）
2. `llm any` → `*llm.Model`（5 处签名 + 3 处 import + 调用方签名同步）
3. 编译验证 + 测试
