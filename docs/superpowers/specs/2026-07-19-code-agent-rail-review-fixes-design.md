# CodeAgentRail 审查修复设计

> 修复 CodeAgentRail 实现审查中发现的 4 项问题，对齐 Python 行为并消除历史 `any` 遗留。

## 1. 修复项总览

| # | 问题 | 严重性 | 影响 |
|---|------|--------|------|
| 1 | `createSubAgent` 中 parentAgent 类型断言失败时静默零值 | 高 | 子 Agent 以零值 Model/Workspace 创建，难以排查 |
| 2 | `modelCache` 未从 DeepConfig 获取传给 `agentDefToSubagentConfig` | 中 | 自定义 agent 的 `model` 字段无效 |
| 3 | `SubAgentConfig.Tools` 类型为 `[]*tool.ToolCard`，应改为 `[]string` 对齐 Python | 中 | spec.Tools 被赋值但从未使用，createSubAgent 绕过 spec 直接用 agentDef.Tools |
| 4 | `tool.ToolCallOptions.Session` 为 `any`，应改为 `SessionFacade` | 低 | 历史遗留 any，已确认无循环依赖 |

## 2. 修复 #1：createSubAgent 防御性检查

### 2.1 问题

`agent_tool.go` 的 `createSubAgent` 方法中，parentAgent 类型断言 `t.parentAgent.(hinterfaces.DeepAgentInterface)` 使用 `ok` 模式，断言失败时 `model`、`ws`、`language` 全部为零值，子 Agent 以默认值静默创建，不报错也不记录日志。

### 2.2 Python 对齐

Python 中 `self._parent_agent` 类型为 `DeepAgent`，不需要类型断言。Go 因为 `parentAgent` 是 `sainterfaces.BaseAgent` 接口（AgentRail 约束），需要断言为 `DeepAgentInterface` 才能获取 DeepConfig。

### 2.3 修复方案

断言失败时记录 Warn 日志并返回 error，而非静默零值：

```go
deepAgent, ok := t.parentAgent.(hinterfaces.DeepAgentInterface)
if !ok {
    logger.Warn(logComponent).
        Str("event_type", "agent_tool_parent_not_deep").
        Str("subagent_type", agentDef.Name).
        Msg("父 Agent 未实现 DeepAgentInterface，无法创建子 Agent")
    return nil, fmt.Errorf("parent agent 未实现 DeepAgentInterface，无法创建子 Agent")
}
deepCfg := deepAgent.DeepConfig()
if deepCfg == nil {
    logger.Warn(logComponent).
        Str("event_type", "agent_tool_deep_config_nil").
        Str("subagent_type", agentDef.Name).
        Msg("父 Agent 的 DeepConfig 为 nil")
    return nil, fmt.Errorf("parent agent 的 DeepConfig 为 nil，无法创建子 Agent")
}
```

## 3. 修复 #2：modelCache 传递

### 3.1 问题

`createSubAgent` 中 `var modelCache map[string]*llm.Model` 为 nil，传给 `agentDefToSubagentConfig` 时不起作用。如果用户在自定义 agent 定义中指定 `model: sonnet`，Go 会忽略，始终使用父 agent 的默认 model。

### 3.2 Python 对齐

```python
# code_agent_rail.py L198
getattr(self._parent_agent, "_model_cache", None)
```

Python 从父 agent 的 `_model_cache` 属性获取，该属性在 `DeepAdapter._create_model()` 中从 `config.yaml` 的 `models.defaults` 构建。

`_agent_def_to_subagent_config` 中的使用：
```python
if agent_def.model and isinstance(model_cache, dict):
    resolved_model = model_cache.get(agent_def.model, model)
```

### 3.3 修复方案

从 DeepConfig 获取 modelCache（需确认 DeepConfig 是否有 ModelCache 字段），传给 `agentDefToSubagentConfig`：

```go
var modelCache map[string]*llm.Model
if deepCfg != nil && deepCfg.ModelCache != nil {
    modelCache = deepCfg.ModelCache
}
spec := agentDefToSubagentConfig(agentDef, model, modelCache)
```

**前提**：需确认 Go 的 DeepConfig 中是否有 ModelCache 字段。如果没有，需要新增。

## 4. 修复 #3：SubAgentConfig.Tools 改为 []string

### 4.1 问题

Go 的 `SubAgentConfig.Tools` 类型为 `[]*tool.ToolCard`，但 Python 的 `SubAgentConfig.tools` 实际存的是 `List[str]`。

Python 中 `spec.tools`（`List[str]`）的唯一用途是传给 `_filter_tool_cards(allowed_tools=spec.tools)`，过滤后的 `parent_tool_cards`（`List[ToolCard]`）才是真正传给 `create_deep_agent` 的工具集。

Go 当前绕过 `spec.Tools`，直接用 `agentDef.Tools` + inline disallowed 过滤，功能等价但不符合 Python 的数据流。

### 4.2 Python 数据流

```
AgentDefinition.tools: List[str]
        │
        ▼  _agent_def_to_subagent_config()
        │  合并 disallowed_tools → spec.tools 已减去 disallowed
        │
SubAgentConfig.tools: List[str]  (已减去 disallowed)
        │
        ▼  _create_sub_agent()
        │
        ├─ spec.tools → _filter_tool_cards(allowed_tools=spec.tools)
        │                    → parent_tool_cards: List[ToolCard]
        │
        └─ create_kwargs["tools"] = parent_tool_cards
```

### 4.3 修复方案

1. 将 `SubAgentConfig.Tools` 从 `[]*tool.ToolCard` 改为 `[]string`
2. `agentDefToSubagentConfig` 中合并 disallowed_tools 逻辑已在 Python 中存在，Go 需对齐
3. `createSubAgent` 中用 `spec.Tools` 传给 `filterToolCards`，删除 inline disallowed 过滤
4. `CreateDeepAgentParams.ToolCards` 仍为 `[]*tool.ToolCard`，传入 `parentToolCards`

### 4.4 影响范围

需要确认 `SubAgentConfig.Tools` 的其他消费方（如 `SubagentRail`），确保 `[]string` 改动不破坏现有功能。

## 5. 修复 #4：ToolCallOptions.Session 类型化

### 5.1 问题

`tool.ToolCallOptions.Session` 定义为 `any`，导致消费方需要类型断言。

旧注释 `// 使用 any 避免循环依赖：tool → session → controller → schema → tool` 是历史遗留，当前架构中这个循环已不存在。

### 5.2 循环依赖验证

```
foundation/tool 当前依赖：runner/callback, common/exception, common/schema
session/interfaces 当前依赖：session/config, session/state, session/stream, session/tracer

两个依赖树无交集 → foundation/tool → session/interfaces 是单向依赖，无循环
```

### 5.3 修复方案

```go
// 之前：
type ToolCallOptions struct {
    Session any
    // ...
}

// 之后：
type ToolCallOptions struct {
    Session sessioninterfaces.SessionFacade
    // ...
}
```

影响：所有使用 `callOpts.Session` 的 Invoke 方法可以去掉类型断言，直接用 `SessionFacade` 接口方法。

## 6. 实现步骤

| 步骤 | 内容 | 文件 |
|------|------|------|
| 1 | 确认 DeepConfig 是否有 ModelCache 字段 | `harness/harness_config/` |
| 2 | 确认 SubAgentConfig.Tools 消费方 | `harness/rails/subagent/` |
| 3 | 修复 #4：ToolCallOptions.Session 类型化 | `foundation/tool/base.go` + 所有消费方 |
| 4 | 修复 #1：createSubAgent 防御性检查 | `adapter/agent_tool.go` |
| 5 | 修复 #2：modelCache 传递 | `adapter/agent_tool.go` + DeepConfig |
| 6 | 修复 #3：SubAgentConfig.Tools 改为 []string | `harness/schema/` + `adapter/agent_tool.go` + `adapter/deep_adapter_config.go` |
| 7 | 编译 + 测试验证 | 全量 |
