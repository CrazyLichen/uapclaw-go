# ResponsePromptRail 实现设计

## 概述

实现 `ResponsePromptRail`——在每次 LLM 调用前向 SystemPromptBuilder 注入 `"response"` PromptSection，告知 LLM 如何区分用户消息与系统消息（cron/heartbeat/notify）。

Python 参考：`jiuwenswarm/agents/harness/common/rails/response_prompt_rail.py`（36 行）

## 当前状态

| 组件 | 状态 |
|------|------|
| `prompt.BuildResponseSection(language)` | ✅ 已实现（`internal/swarm/agents/harness/common/prompt/prompt_builder.go`） |
| `ResponsePromptRail` 结构体 | ☐ 未实现 |
| `DeepAdapter.buildResponsePromptRail()` | ⤵️ 返回 nil（`deep_adapter_rails.go:631-634`） |
| `CodeAdapter` 注册调用 | ✅ 骨架已就位（`code_adapter.go:862`） |

## 设计

### 结构体

```go
type ResponsePromptRail struct {
    rails.DeepAgentRail
    builder saprompt.SystemPromptBuilderInterface  // Python: self.system_prompt_builder
}
```

### 方法

| 方法 | 行为 | Python 对齐 |
|------|------|-------------|
| `NewResponsePromptRail()` | 无参构造，priority=5 | `ResponsePromptRail()` |
| `Init(ctx, agent)` | 从 `agent.SystemPromptBuilder()` 获取引用 | `init(agent)` |
| `Uninit(agent)` | `builder.RemoveSection("response")` + 置 nil | `uninit(agent)` |
| `BeforeModelCall(ctx, cbc)` | 读 `builder.Language()` fallback `"cn"` → 调 `prompt.BuildResponseSection(language)` → `builder.AddSection(section)` | `before_model_call(ctx)` |
| `GetCallbacks()` | 注册 BeforeModelCall 回调 | — |

### 语言来源

仅从 `builder.Language()` 读取，fallback `"cn"`。对齐 Python：

```python
section = _response_prompt(self.system_prompt_builder.language or "cn")
```

### 回填清单

1. **`DeepAdapter.buildResponsePromptRail()`**（`deep_adapter_rails.go:631-634`）
   - 改为：`return rails.NewResponsePromptRail()`
   - 日志：成功 Info / 失败 Warn

2. **`CodeAdapter`**（`code_adapter.go:862`）
   - 已就位，无需改动

3. **`IMPLEMENTATION_PLAN.md`**（第 708 行）
   - 将 `ResponsePrompt` 改为 `ResponsePrompt✅`

4. **`rails/doc.go`**
   - 文件目录添加 `response_prompt_rail.go` 条目

### 测试

- `TestNewResponsePromptRail` — 构造 + priority 验证
- `TestResponsePromptRail_Init` — Init 获取 builder 引用
- `TestResponsePromptRail_Uninit` — Uninit 清除 section + 释放 builder
- `TestResponsePromptRail_BeforeModelCall` — 注入 response section（cn/en）
- `TestResponsePromptRail_BeforeModelCall_NoBuilder` — builder 为 nil 时无操作
- `TestResponsePromptRail_GetCallbacks` — 回调映射包含 BeforeModelCall

使用 fakeBaseAgent + fakeSystemPromptBuilder mock，无外部依赖。

## 文件变更

| 文件 | 操作 |
|------|------|
| `internal/swarm/agents/harness/common/rails/response_prompt_rail.go` | 新增 |
| `internal/swarm/agents/harness/common/rails/response_prompt_rail_test.go` | 新增 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | 修改（回填 buildResponsePromptRail） |
| `internal/swarm/agents/harness/common/rails/doc.go` | 修改（添加文件条目） |
| `IMPLEMENTATION_PLAN.md` | 修改（状态更新） |
