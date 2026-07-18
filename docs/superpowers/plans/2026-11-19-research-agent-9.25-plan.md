# ResearchAgent 9.25 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 回填 9.25 ResearchAgent，实现 BuildResearchAgentConfig + CreateResearchAgent 两个工厂函数，并集成到 adapter 层和 DeepAgent.CreateSubagent 分支。

**Architecture:** ResearchAgent 是最简单的子 Agent——无自定义工具，通过 SysOperationRail 注入文件系统/Shell 工具。两个工厂函数统一用 `*SubagentCreateParams` 作入参（对齐 Python 具名参数风格），adapter 层负责从 `map[string]any` 解析出 `SubagentCreateParams`。

**Tech Stack:** Go, 现有 harness 包基础设施（SubAgentConfig, SubagentCreateParams, CreateDeepAgent, SysOperationRail, ResolveLanguage）

---

### Task 1: 回填 research_agent.go 常量、全局变量和 BuildResearchAgentConfig

**Files:**
- Modify: `internal/agentcore/harness/subagents/research_agent.go`

- [ ] **Step 1: 写 research_agent.go 完整实现**

将现有骨架代码替换为完整实现：

```go
package subagents

import (
	"context"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"

	harness "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
)

// ──────────────────────────── 常量 ────────────────────────────

// ResearchAgentFactoryName research 子代理工厂名称
// 对齐 Python: RESEARCH_AGENT_FACTORY_NAME
const ResearchAgentFactoryName = "research_agent"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultResearchAgentSystemPrompt 默认系统提示词
	// 对齐 Python: DEFAULT_RESEARCH_AGENT_SYSTEM_PROMPT
	defaultResearchAgentSystemPrompt = map[string]string{
		"cn": "你是研究助理，负责围绕用户输入的主题开展调研，仅需返回最终研究结果。",
		"en": "You are a research assistant responsible for conducting research around the topic provided by the user.Only return the final research results.",
	}
	// defaultResearchAgentDescription 默认描述
	// 对齐 Python: DEFAULT_RESEARCH_AGENT_DESCRIPTION
	defaultResearchAgentDescription = map[string]string{
		"cn": "专注于研究调查任务，当用户想要调查某问题时，可使用该代理执行研究工作。每次只给这位研究员一个主题。",
		"en": "Focuses on research and investigation tasks. \nWhen users want to investigate a specific issue, this agent can be used to execute research work. \nProvide only one topic to this researcher at a time.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildResearchAgentConfig 构建 research 子代理配置（延迟实例化）。
// 对齐 Python: build_research_agent_config(model, language=..., workspace=..., ...)
func BuildResearchAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig {
	language := hprompts.ResolveLanguage(params.Language)

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := defaultResearchAgentDescription[language]
		if desc == "" {
			desc = defaultResearchAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(ResearchAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时使用默认
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		prompt := defaultResearchAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultResearchAgentSystemPrompt["cn"]
		}
		cfg.SystemPrompt = prompt
	}

	cfg.Tools = params.Tools
	cfg.ToolInstances = params.ToolInstances
	cfg.Mcps = params.Mcps
	cfg.Model = model
	cfg.Rails = params.Rails
	cfg.Skills = params.Skills
	cfg.Backend = params.Backend
	cfg.Workspace = params.Workspace
	cfg.SysOperation = params.SysOperation
	cfg.Language = language
	cfg.PromptMode = params.PromptMode
	cfg.EnableTaskLoop = params.EnableTaskLoop

	// MaxIterations：用户未提供（0）时默认 15
	cfg.MaxIterations = params.MaxIterations
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 15
	}

	cfg.FactoryName = ResearchAgentFactoryName
	cfg.FactoryKwargs = nil
	cfg.EnablePlanMode = params.EnablePlanMode
	cfg.RestrictToWorkDir = params.RestrictToWorkDir

	return cfg
}

// CreateResearchAgent 创建并配置 ResearchAgent DeepAgent 实例。
// 对齐 Python: create_research_agent(model, card=..., system_prompt=..., ...)
//
// 预定义 ResearchAgent 配备 SysOperationRail，用户可自由覆盖配置。
// Full override rule：如果用户传了 rails，则使用用户的，否则默认注入 [SysOperationRail()]。
func CreateResearchAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*harness.DeepAgent, error) {
	language := hprompts.ResolveLanguage(params.Language)

	// Full override rule：用户传了 rails 就用用户的，否则默认注入 SysOperationRail
	// 对齐 Python: final_rails = rails if rails is not None else [SysOperationRail()]
	finalRails := params.Rails
	if finalRails == nil {
		finalRails = []sainterfaces.AgentRail{rails.NewSysOperationRail()}
	}

	// 默认 AgentCard
	card := params.Card
	if card == nil {
		desc := defaultResearchAgentDescription[language]
		if desc == "" {
			desc = defaultResearchAgentDescription["cn"]
		}
		card = agentschema.NewAgentCard(
			agentschema.WithAgentName(ResearchAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// 默认 SystemPrompt
	systemPrompt := params.SystemPrompt
	if systemPrompt == "" {
		prompt := defaultResearchAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultResearchAgentSystemPrompt["cn"]
		}
		systemPrompt = prompt
	}

	// 默认 MaxIterations
	maxIterations := params.MaxIterations
	if maxIterations == 0 {
		maxIterations = 15
	}

	// 转换为 CreateDeepAgentParams 并调用工厂
	return harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Model:             params.Model,
		Card:              card,
		SystemPrompt:      systemPrompt,
		ToolCards:         params.Tools,
		ToolInstances:     params.ToolInstances,
		Mcps:              params.Mcps,
		Rails:             finalRails,
		EnableTaskLoop:    params.EnableTaskLoop,
		MaxIterations:     maxIterations,
		Workspace:         params.Workspace,
		Skills:            params.Skills,
		Backend:           params.Backend,
		SysOperation:      params.SysOperation,
		Language:          language,
		PromptMode:        params.PromptMode,
		EnableTaskPlanning: params.EnablePlanMode,
		RestrictToWorkDir: params.RestrictToWorkDir,
	})
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/subagents/...`
Expected: 编译通过，无错误

注意：如果出现循环依赖（subagents → harness → subagents），需要调整 import。`CreateResearchAgent` 中引用了 `harness.DeepAgent` 和 `harness.CreateDeepAgent`，如果 subagents 包和 harness 包在同一层级且 harness 已导入 subagents，可能需要把 `CreateResearchAgent` 移到 harness 包中，或将返回类型改为接口 `hinterfaces.DeepAgentInterface`。

- [ ] **Step 3: 如有循环依赖，调整方案**

如果编译报循环依赖，将 `CreateResearchAgent` 移到 `harness` 包中（而非 subagents 包），因为 `CreateDeepAgent` 和 `DeepAgent` 都在 harness 包。subagents 包只保留 `BuildResearchAgentConfig`。

调整后的文件分布：
- `subagents/research_agent.go` — 只保留 `BuildResearchAgentConfig` + 常量 + 全局变量
- `harness/research_agent_factory.go` — 新文件，放 `CreateResearchAgent`

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/subagents/research_agent.go
git commit -m "feat(9.25): 回填 BuildResearchAgentConfig + 新增 CreateResearchAgent"
```

---

### Task 2: 回填 DeepAgent.CreateSubagent 中 research_agent 分支

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go` (L624-625)

- [ ] **Step 1: 修改 CreateSubagent 的 research_agent case**

将 L624-625 的 stub 错误改为调用 CreateResearchAgent：

原来：
```go
case "research_agent":
    return nil, fmt.Errorf("research_agent 工厂尚未实现，⤵️ 9.25 回填")
```

改为：
```go
case "research_agent":
    return CreateResearchAgent(ctx, kwargs)
```

注意：`kwargs` 已经是 `*hschema.SubagentCreateParams` 类型，由 `buildSubagentCreateKwargs` 从 `SubAgentConfig` 构建而来，其中包含 Card、SystemPrompt、Rails、MaxIterations 等全部字段。`CreateResearchAgent` 内部会做默认值填充和 full override 规则处理。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/...`
Expected: 编译通过

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/deep_agent.go
git commit -m "feat(9.25): 回填 CreateSubagent research_agent 分支"
```

---

### Task 3: 更新 adapter 层调用 BuildResearchAgentConfig

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go` (L106-111)

- [ ] **Step 1: 新增 buildResearchSubagentParams 方法**

在 `deep_adapter_config.go` 中新增非导出方法，从 `map[string]any` 解析出 `SubagentCreateParams`：

```go
// buildResearchSubagentParams 从配置映射构建 research 子代理的 SubagentCreateParams。
// 对齐 Python: _build_configured_subagents 中对 build_research_agent_config 的调用
func (d *DeepAdapter) buildResearchSubagentParams(config map[string]any, configBase map[string]any) *hschema.SubagentCreateParams {
	resolvedLanguage := d.resolveRuntimeLanguage()

	// 从 config 提取 workspace
	workspace := ""
	if v, ok := config["workspace"]; ok {
		if s, ok := v.(string); ok {
			workspace = s
		}
	}
	if workspace == "" {
		workspace = d.workspaceDir()
	}

	// 从 subagents config 提取 max_iterations，回退到 config 的 max_iterations，默认 15
	maxIterations := 0
	subagentsCfg, _ := config["subagents"].(map[string]any)
	if subagentsCfg != nil {
		if researchCfg, ok := subagentsCfg["research_agent"]; ok {
			if m, ok := researchCfg.(map[string]any); ok {
				if v, ok := m["max_iterations"]; ok {
					switch n := v.(type) {
					case int:
						maxIterations = n
					case float64:
						maxIterations = int(n)
					}
				}
			}
		}
	}
	if maxIterations == 0 {
		if v, ok := config["max_iterations"]; ok {
			switch n := v.(type) {
			case int:
				maxIterations = n
			case float64:
				maxIterations = int(n)
			}
		}
	}
	if maxIterations == 0 {
		maxIterations = 15
	}

	return &hschema.SubagentCreateParams{
		Language:      resolvedLanguage,
		MaxIterations: maxIterations,
	}
}
```

- [ ] **Step 2: 修改 buildConfiguredSubagents 中的调用**

将 L106-111：
```go
if d.isSubagentExplicitlyEnabled(subagentsCfg, "research_agent") {
    cfg := subagents.BuildResearchAgentConfig(d.model, config, configBase)
    if cfg != nil {
        specs = append(specs, cfg)
    }
}
```

改为：
```go
if d.isSubagentExplicitlyEnabled(subagentsCfg, "research_agent") {
    params := d.buildResearchSubagentParams(config, configBase)
    cfg := subagents.BuildResearchAgentConfig(d.model, params)
    if cfg != nil {
        specs = append(specs, cfg)
    }
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_config.go
git commit -m "feat(9.25): adapter 层解析 map→SubagentCreateParams 传入 BuildResearchAgentConfig"
```

---

### Task 4: 新建 research_agent_test.go 测试文件

**Files:**
- Create: `internal/agentcore/harness/subagents/research_agent_test.go`
- 可能还需要在 `internal/agentcore/harness/` 下补充 CreateResearchAgent 测试（取决于 Task 1 的循环依赖方案）

- [ ] **Step 1: 写 BuildResearchAgentConfig 测试**

```go
package subagents

import (
	"testing"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildResearchAgentConfig_默认配置 测试所有默认值
func TestBuildResearchAgentConfig_默认配置(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{}

	cfg := BuildResearchAgentConfig(model, params)

	require.NotNil(t, cfg)
	assert.Equal(t, "research_agent", cfg.AgentCard.GetName())
	assert.Equal(t, "research_agent", cfg.FactoryName)
	assert.Equal(t, 15, cfg.MaxIterations)
	assert.False(t, cfg.EnableTaskLoop)
	assert.True(t, cfg.RestrictToWorkDir)
	assert.Equal(t, model, cfg.Model)
}

// TestBuildResearchAgentConfig_CN提示词 测试中文提示词
func TestBuildResearchAgentConfig_CN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "cn"}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "研究助理")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "研究调查")
}

// TestBuildResearchAgentConfig_EN提示词 测试英文提示词
func TestBuildResearchAgentConfig_EN提示词(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{Language: "en"}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Contains(t, cfg.SystemPrompt, "research assistant")
	assert.Contains(t, cfg.AgentCard.GetDescription(), "research and investigation")
}

// TestBuildResearchAgentConfig_自定义MaxIterations 测试覆盖默认值
func TestBuildResearchAgentConfig_自定义MaxIterations(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{MaxIterations: 25}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Equal(t, 25, cfg.MaxIterations)
}

// TestBuildResearchAgentConfig_用户覆盖Card 测试用户提供的 AgentCard
func TestBuildResearchAgentConfig_用户覆盖Card(t *testing.T) {
	model := &llm.Model{}
	customCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("custom_researcher"),
		agentschema.WithAgentDescription("自定义研究助手"),
	)
	params := &hschema.SubagentCreateParams{Card: customCard}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Equal(t, "custom_researcher", cfg.AgentCard.GetName())
	assert.Equal(t, "自定义研究助手", cfg.AgentCard.GetDescription())
}

// TestBuildResearchAgentConfig_用户覆盖SystemPrompt 测试用户提供的系统提示词
func TestBuildResearchAgentConfig_用户覆盖SystemPrompt(t *testing.T) {
	model := &llm.Model{}
	params := &hschema.SubagentCreateParams{SystemPrompt: "自定义提示词"}

	cfg := BuildResearchAgentConfig(model, params)

	assert.Equal(t, "自定义提示词", cfg.SystemPrompt)
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/subagents/... -v -run TestBuildResearchAgentConfig`
Expected: 全部 PASS

- [ ] **Step 3: 写 CreateResearchAgent 测试**

根据 Task 1 的循环依赖方案，测试放在对应包中。如果 `CreateResearchAgent` 在 subagents 包：

```go
// TestCreateResearchAgent_默认Rails 测试未传 rails 时自动注入 SysOperationRail
func TestCreateResearchAgent_默认Rails(t *testing.T) {
	// 需要 mock model 和 resource manager，此测试标记为需要 harness 初始化
	// 用 build tag 隔离或 skip
}

// TestCreateResearchAgent_用户覆盖Rails 测试传了 rails 时使用用户的
func TestCreateResearchAgent_用户覆盖Rails(t *testing.T) {
	// 同上
}
```

注意：`CreateResearchAgent` 内部调用 `CreateDeepAgent`，需要全局 Runner/ResourceMgr 已初始化。此类集成测试用 `//go:build integration` 标签隔离。单元测试层面，主要测试 `BuildResearchAgentConfig` 的默认值逻辑。

- [ ] **Step 4: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/subagents/... -v`
Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/subagents/research_agent_test.go
git commit -m "test(9.25): 新增 BuildResearchAgentConfig 单元测试"
```

---

### Task 5: 更新 DeepAgent 测试中 research_agent 的 stub 断言

**Files:**
- Modify: `internal/agentcore/harness/deep_agent_test.go`

- [ ] **Step 1: 修改 TestDeepAgent_CreateSubagent_工厂分派**

原来 `research_agent` 在 `factories` 列表中，期望返回 stub 错误。现在 `research_agent` 已实现，应从 stub 列表中移除。

将 L 中的 `stubFactories` 列表移除 `"research_agent"`：

```go
stubFactories := []string{
    "browser_agent", "code_agent",
    "mobile_gui_agent",
}
```

同时更新 `TestDeepAgent_CreateSubagent_工厂分派` 中的 `factories` 列表，将 `research_agent` 改为验证正常创建而非报错。

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/... -v -run TestDeepAgent_CreateSubagent`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/deep_agent_test.go
git commit -m "test(9.25): 更新 CreateSubagent 测试，research_agent 不再返回 stub 错误"
```

---

### Task 6: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.25 状态**

将 `| 9.25 | 🔄 | ResearchAgent |` 改为 `| 9.25 | ✅ | ResearchAgent |`

同时更新描述文字，移除 `⤵️ 9.38-49` 标记，补充实际实现内容。

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(9.25): 更新实现计划状态为已完成"
```

---

### Task 7: 全量编译和测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译通过

- [ ] **Step 2: 运行相关包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/... ./internal/agentcore/harness/subagents/... ./internal/swarm/server/adapter/... -v -count=1`
Expected: 全部 PASS

- [ ] **Step 3: Final commit（如有未提交的改动）**

```bash
git add -A
git commit -m "chore(9.25): 全量验证通过"
```
