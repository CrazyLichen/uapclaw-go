# 9.25 ResearchAgent 实现设计

## 概述

回填 9.25 ResearchAgent（研究子 Agent），包含两个工厂函数和 adapter 层集成。

## 流程位置

```
用户输入 → DeepAgent.Invoke()
  → TaskLoopController / TaskTool
    → DeepAgent.CreateSubagent("research_agent", subSessionID)
      → 查找 SubAgentConfig (factory_name="research_agent")
      → 构建 SubagentCreateParams
      → switch "research_agent" → CreateResearchAgent()
        → CreateDeepAgent() 工厂
      → 子 DeepAgent 实例
    → 子 DeepAgent.Invoke()  ← ReAct 循环最多 15 轮
```

## 作用

- 研究调查子 Agent，接收研究主题，开展调研，只返回最终结果
- 无自定义工具，通过 `SysOperationRail` 注入文件系统/Shell 工具
- 最简单的子 Agent（无子代理嵌套、无 AgentModeRail、无 CodingMemoryRail）

## max_iterations 语义

`max_iterations = 15` 指 **15 次 ReAct 循环迭代**，不是 15 次工具调用。

每次迭代：调用 LLM → 如果返回 tool_calls 则执行所有工具 → 如果纯文本则退出。

一次迭代可包含多个工具调用（LLM 一次响应多个 tool_calls），实际工具调用数 = 15 × N。

## Python 对照

| Python | Go |
|--------|-----|
| `RESEARCH_AGENT_FACTORY_NAME` | `ResearchAgentFactoryName` |
| `DEFAULT_RESEARCH_AGENT_SYSTEM_PROMPT` (CN/EN dict) | `defaultResearchAgentSystemPrompt` (map) |
| `DEFAULT_RESEARCH_AGENT_DESCRIPTION` (CN/EN dict) | `defaultResearchAgentDescription` (map) |
| `build_research_agent_config(model, language=..., ...)` | `BuildResearchAgentConfig(model, params)` |
| `create_research_agent(model, card=..., ...)` | `CreateResearchAgent(ctx, params)` |

## 改动点

### 1. `subagents/research_agent.go` — 回填 + 新增

#### 常量

```go
const ResearchAgentFactoryName = "research_agent"
```

#### 全局变量

```go
var (
    defaultResearchAgentSystemPrompt = map[string]string{
        "cn": "你是研究助理，负责围绕用户输入的主题开展调研，仅需返回最终研究结果。",
        "en": "You are a research assistant responsible for conducting research around the topic provided by the user.Only return the final research results.",
    }
    defaultResearchAgentDescription = map[string]string{
        "cn": "专注于研究调查任务，当用户想要调查某问题时，可使用该代理执行研究工作。每次只给这位研究员一个主题。",
        "en": "Focuses on research and investigation tasks. \nWhen users want to investigate a specific issue, this agent can be used to execute research work. \nProvide only one topic to this researcher at a time.",
    }
)
```

提示词一比一复刻 Python 原文，不做自行翻译。

#### BuildResearchAgentConfig

```go
func BuildResearchAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig
```

- 签名对齐 Python：adapter 层先从 `map[string]any` 解析出 `SubagentCreateParams`，再传入
- 内部逻辑：
  1. `language := hprompts.ResolveLanguage(params.Language)`
  2. 构建 `SubAgentConfig`，填充默认值：
     - `AgentCard`: `NewAgentCard(WithName("research_agent"), WithDescription(按语言选))`
     - `SystemPrompt`: 按语言选默认提示词
     - `Model`: 传入的 model
     - `MaxIterations`: params.MaxIterations，0 时默认 15
     - `EnableTaskLoop`: params.EnableTaskLoop，默认 false
     - `FactoryName`: `"research_agent"`
     - `Language`: resolvedLanguage
     - `Workspace`: params.Workspace
  3. Rails 不在此设置，交给 `CreateResearchAgent` 的 full override 规则

#### CreateResearchAgent

```go
func CreateResearchAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error)
```

- 签名对齐 Python 的 `create_research_agent(model, **kwargs)`，用 `SubagentCreateParams` 替代 Python 关键字参数
- 内部逻辑：
  1. `language := hprompts.ResolveLanguage(params.Language)`
  2. Full override rule：`params.Rails == nil` 时注入 `[NewSysOperationRail()]`
  3. 默认 AgentCard：`params.Card == nil` 时创建默认
  4. 默认 SystemPrompt：`params.SystemPrompt == ""` 时用默认
  5. 默认 MaxIterations：`params.MaxIterations == 0` 时默认 15
  6. 转换为 `CreateDeepAgentParams` → 调用 `CreateDeepAgent()`

### 2. `subagents/research_agent_test.go` — 新建测试

| 测试用例 | 覆盖内容 |
|---------|---------|
| `TestBuildResearchAgentConfig_默认配置` | 验证所有默认值（FactoryName、MaxIterations、EnableTaskLoop） |
| `TestBuildResearchAgentConfig_CN提示词` | language="cn" 时使用中文提示词 |
| `TestBuildResearchAgentConfig_EN提示词` | language="en" 时使用英文提示词 |
| `TestBuildResearchAgentConfig_自定义MaxIterations` | params.MaxIterations 覆盖默认 15 |
| `TestCreateResearchAgent_默认Rails` | 未传 rails 时自动注入 SysOperationRail |
| `TestCreateResearchAgent_用户覆盖Rails` | 传了 rails 时使用用户的（full override） |
| `TestCreateResearchAgent_默认Card和Prompt` | 未传时使用默认值 |
| `TestCreateResearchAgent_用户覆盖Card和Prompt` | 传了时使用用户的 |
| `TestCreateResearchAgent_默认MaxIterations` | 未传时默认 15 |

### 3. `harness/deep_agent.go` — 回填 CreateSubagent 分支

```go
case "research_agent":
    researchParams := d.buildSubagentCreateParams(kwargs, subSessionID)
    return CreateResearchAgent(ctx, researchParams)
```

将原来返回 stub 错误改为调用 `CreateResearchAgent`。

### 4. `harness/deep_agent_test.go` — 更新已有测试

- `TestDeepAgent_CreateSubagent_工厂分派`：`research_agent` 从返回 stub 错误改为正常创建
- `TestDeepAgent_CreateSubagent_工厂未实现`：从 stubFactories 列表中移除 `"research_agent"`

### 5. adapter 层 `deep_adapter_config.go` — 改动调用方

```go
// 之前：
cfg := subagents.BuildResearchAgentConfig(d.model, config, configBase)

// 之后：
params := d.buildResearchSubagentParams(config, configBase)  // 从 map 解析 SubagentCreateParams
cfg := subagents.BuildResearchAgentConfig(d.model, params)
```

新增 `buildResearchSubagentParams(config, configBase)` 方法：
- 从 configBase 提取 `preferred_language` → 调 `hprompts.ResolveLanguage()` 得 language
- 从 config 提取 `workspace` → workspace
- 从 subagents config 提取 `max_iterations` → maxIterations（回退到 config 的 max_iterations，默认 15）
- 构建 `SubagentCreateParams` 返回

对齐 Python adapter:
```python
build_research_agent_config(
    model,
    workspace=workspace,
    language=resolved_language,
    max_iterations=parse_int(
        research_agent_cfg.get("max_iterations"),
        react_cfg.get("max_iterations", 15),
    ),
)
```

### 6. `IMPLEMENTATION_PLAN.md` — 状态更新

9.25: 🔄 → ✅

## 依赖状态

| 依赖 | 状态 | 阻塞？ |
|------|------|--------|
| SubAgentConfig | ✅ 已实现 | 否 |
| SubagentCreateParams | ✅ 已实现 | 否 |
| SysOperationRail | ✅ 已实现 | 否 |
| CreateDeepAgent 工厂 | ✅ 已实现 | 否 |
| hprompts.ResolveLanguage | ✅ 已实现 | 否 |
| 9.38-49 Harness 工具集 | ☐ | 否（ResearchAgent 不需要自定义工具） |
