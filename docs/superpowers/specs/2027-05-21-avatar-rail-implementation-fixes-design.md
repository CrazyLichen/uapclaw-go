# 10.6.4 AvatarPromptRail 实现差异修复设计

## 背景

10.6.4 AvatarPromptRail + 10.6.17 Forbidden Memory 实现已完成并提交（7 个 commit），但审查发现 5 处计划与实现的差异，其中 3 处需要修复。

## 差异清单与修复决策

### 差异 1&2：GetCallbacks 返回类型 + NewAgentCallbackContext 参数签名（无需修复）

- **计划**：`PerAgentCallbackFunc` 在 `agentinterfaces` 包，`NewAgentCallbackContext` 7 参数
- **实际**：`PerAgentCallbackFunc` 在 `cb`（callback）包，`NewAgentCallbackContext` 3 参数
- **决策**：正确偏差，框架实际 API 如此，无需修改

### 差异 3：Forbidden Memory 集成测试缺失（需修复）

- **现状**：`forbidden_test.go` 只测了纯函数 `buildForbiddenPromptCN/EN`，`getMemoryForbiddenConfig()` → `GetForbiddenMemoryPrompt()` 的端到端流程无测试
- **原因**：`config.New("")` 全局单例，单元测试无法注入 testdata 路径
- **决策**：补写 `//go:build integration` 集成测试，依赖 testdata YAML，日常 `go test` 不执行

### 差异 4：OwnerScopesPermissionContext.Scene() 缺 strip（需修复）

- **现状**：Go 用 `p.ChannelID == "web"`，Python 用 `self.channel_id.strip() == "web"`
- **决策**：加 `strings.TrimSpace(p.ChannelID) == "web"` 对齐 Python，补充带空格的测试用例

### 差异 5：forbidden.go 日志组件不正确（需修复）

- **现状**：`forbidden.go` 用 `ComponentChannel`
- **决策**：改为 `ComponentAgentServer`，与 `avatar_rail.go` 统一，符合 `agents/harness/common/` 模块定位

## 修复方案

### 修复 1：补写 Forbidden Memory 集成测试

**新建文件**：`internal/swarm/agents/harness/common/memory/forbidden_integration_test.go`

```go
//go:build integration

package memory
```

**测试用例**：
1. `TestGetForbiddenMemoryPrompt_中文输出_integration` — 配置 enabled=true + patterns，验证返回中文提示词含 patterns 列表
2. `TestGetForbiddenMemoryPrompt_英文输出_integration` — 配置 enabled=true + patterns，验证返回英文提示词
3. `TestGetForbiddenMemoryPrompt_未启用_integration` — 配置 enabled=false，验证返回空串

**依赖**：已有 `testdata/forbidden_enabled.yaml` 和 `testdata/forbidden_no_patterns.yaml`，可能需要新增 `testdata/forbidden_disabled.yaml`

**注意**：集成测试通过环境变量 `JIUWEN_CONFIG_PATH` 指向 testdata，需确认 config 包是否支持此环境变量

### 修复 2：OwnerScopes Scene() 加 TrimSpace

**修改文件**：`internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`

```go
// 修改前
if p.ChannelID == "web" {

// 修改后
if strings.TrimSpace(p.ChannelID) == "web" {
```

**补充测试**：`owner_scopes_test.go` 新增 `TestOwnerScopesPermissionContext_Scene_空格channelID` 验证 `" web "` 仍匹配 web 场景

### 修复 3：forbidden.go 日志组件

**修改文件**：`internal/swarm/agents/harness/common/memory/forbidden.go`

```go
// 修改前
var forbiddenLogComponent = logger.ComponentChannel

// 修改后
var forbiddenLogComponent = logger.ComponentAgentServer
```

## 文件变更清单

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新建 | `internal/swarm/agents/harness/common/memory/forbidden_integration_test.go` | Forbidden Memory 集成测试 |
| 修改 | `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go` | Scene() 加 TrimSpace |
| 新建 | `internal/swarm/agents/harness/common/rails/permissions/owner_scopes_test.go` | OwnerScopes 补充测试 |
| 修改 | `internal/swarm/agents/harness/common/memory/forbidden.go` | 日志组件改为 ComponentAgentServer |
