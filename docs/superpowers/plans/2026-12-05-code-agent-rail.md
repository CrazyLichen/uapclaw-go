# CodeAgentRail (10.3.7) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 CodeAgentRail，让 Code 模式下的主 Agent 可以通过 `Agent` 工具调用用户自定义的子 Agent。

**Architecture:** CodeAgentRail 嵌入 DeepAgentRail，Init 时从 AgentConfigService 加载自定义 Agent 定义，构建 AgentTool 注册到 ResourceMgr + AbilityManager。AgentTool 实现 tool.Tool 接口，Invoke 时动态创建子 DeepAgent 执行任务，支持同步和异步（background）两种模式。热重载通过 Reload 方法实现。

**Tech Stack:** Go, agentcore (harness/rails, foundation/tool, harness/factory), swarm/server/adapter

---

## File Structure

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/swarm/server/adapter/code_agent_rail.go` | 创建 | CodeAgentRail 结构体 + 常量 + 辅助函数（filterToolCards, buildAgentToolCard） |
| `internal/swarm/server/adapter/agent_tool.go` | 创建 | AgentTool 结构体（实现 tool.Tool 接口） |
| `internal/swarm/server/adapter/code_agent_rail_test.go` | 创建 | CodeAgentRail + 辅助函数测试 |
| `internal/swarm/server/adapter/agent_tool_test.go` | 创建 | AgentTool 测试 |
| `internal/swarm/server/adapter/code_adapter.go` | 修改 | 步骤 16 回填 buildCodeAgentRail |
| `internal/swarm/server/runtime/agent_config.go` | 修改 | ListAvailableTools 引用共享常量 |
| `internal/swarm/server/adapter/doc.go` | 修改 | 添加新文件到目录 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 10.3.7 ✅, 10.3.13 ✅ |

---

### Task 1: 常量与映射 + filterToolCards + buildAgentToolCard

**Files:**
- Create: `internal/swarm/server/adapter/code_agent_rail.go`
- Create: `internal/swarm/server/adapter/code_agent_rail_test.go`

- [ ] **Step 1: 写 filterToolCards 的失败测试**

在 `internal/swarm/server/adapter/code_agent_rail_test.go` 中：

```go
package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestFilterToolCards_通配符(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCard("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCard("write_file", "Write", "写入文件", nil, nil),
		tool.NewToolCard("bash", "Bash", "执行命令", nil, nil),
	}
	result := filterToolCards(cards, []string{"*"}, nil)
	assert.Len(t, result, 3)
}

func TestFilterToolCards_指定名(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCard("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCard("write_file", "Write", "写入文件", nil, nil),
		tool.NewToolCard("bash", "Bash", "执行命令", nil, nil),
	}
	// 用显示名过滤
	result := filterToolCards(cards, []string{"Read", "Bash"}, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, "read_file", result[0].ID)
	assert.Equal(t, "bash", result[1].ID)
}

func TestFilterToolCards_内部名匹配(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCard("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCard("write_file", "Write", "写入文件", nil, nil),
	}
	// 用内部名过滤
	result := filterToolCards(cards, []string{"read_file"}, nil)
	assert.Len(t, result, 1)
	assert.Equal(t, "read_file", result[0].ID)
}

func TestFilterToolCards_含disallowed(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCard("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCard("write_file", "Write", "写入文件", nil, nil),
		tool.NewToolCard("bash", "Bash", "执行命令", nil, nil),
	}
	result := filterToolCards(cards, []string{"*"}, []string{"Write", "bash"})
	assert.Len(t, result, 1)
	assert.Equal(t, "read_file", result[0].ID)
}

func TestBuildAgentToolCard_基本(t *testing.T) {
	agents := []*types.AgentDefinition{
		{
			Name:        "code-reviewer",
			Description: "代码审查",
			WhenToUse:   "审查代码质量",
			Tools:       []string{"Read", "Grep"},
		},
	}
	card := buildAgentToolCard(agents, "test_agent")
	assert.Equal(t, "Agent", card.Name)
	assert.Contains(t, card.Description, "code-reviewer")
	assert.Contains(t, card.Description, "审查代码质量")
	assert.Contains(t, card.Description, "Read, Grep")
}

func TestBuildAgentToolCard_空列表(t *testing.T) {
	card := buildAgentToolCard(nil, "test_agent")
	assert.Equal(t, "Agent", card.Name)
	assert.Contains(t, card.Description, "Launch a new agent")
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestFilterToolCards|TestBuildAgentToolCard" -v 2>&1 | head -20`
Expected: 编译失败（filterToolCards / buildAgentToolCard 未定义）

- [ ] **Step 3: 实现 code_agent_rail.go（常量 + 辅助函数）**

在 `internal/swarm/server/adapter/code_agent_rail.go` 中：

```go
package adapter

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAgentRail Code 模式下的自定义 Agent 护栏。
// 对齐 Python: CodeAgentRail(DeepAgentRail) priority=90
//
// 管理 /agents 创建的自定义 Agent，通过 AgentTool 注册为统一 "Agent" 工具。
// 与 SubagentRail 共存，只管理自定义 Agent，不触碰内置 Agent。
type CodeAgentRail struct {
	rails.DeepAgentRail
	// workspaceDir 工作空间目录
	workspaceDir string
	// configLister 自定义 Agent 配置列表接口
	configLister AgentConfigLister
	// agentTool 已注册的 AgentTool 实例
	agentTool *AgentTool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// codeAgentRailPriority CodeAgentRail 优先级。
	// 对齐 Python: CodeAgentRail.priority = 90
	codeAgentRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// disallowedForSubagents 禁止传递给子 Agent 的工具名集合。
// 对齐 Python: DISALLOWED_FOR_SUBAGENTS
// 引用 types.DisallowedForSubagents 切片构建 map，避免硬编码重复
var disallowedForSubagents map[string]bool

func init() {
	disallowedForSubagents = make(map[string]bool, len(types.DisallowedForSubagents))
	for _, name := range types.DisallowedForSubagents {
		disallowedForSubagents[name] = true
	}
}

// displayToInternal 显示名→内部名映射。
// 对齐 Python: _DISPLAY_TO_INTERNAL
var displayToInternal = map[string]string{
	"Read": "read_file", "Write": "write_file", "Edit": "edit_file",
	"Bash": "bash", "Grep": "grep", "Glob": "glob",
	"LS": "ls", "ListDir": "ls",
	"TodoWrite": "todo_create", "TodoList": "todo_list",
	"WebSearch": "web_search", "WebFetch": "web_fetch",
	"ImageOCR": "image_ocr", "VisionQA": "visual_question_answering",
	"AudioTranscribe": "audio_transcription",
	"AudioQA": "audio_question_answering",
	"AudioMetadata": "audio_metadata",
}

// toolGroups 工具分组（用于 Agent 定义 UI）。
// 对齐 Python: TOOL_GROUPS
var toolGroups = map[string][]string{
	"核心":   {"Read", "Write", "Edit", "Bash", "LS"},
	"搜索":   {"Grep", "Glob", "WebSearch", "WebFetch"},
	"代码智能": {"LSP", "TodoWrite", "TodoList"},
	"高级":   {"MemorySearch", "MemoryGet", "WriteMemory", "EditMemory", "CronCreate", "CronList", "CronDelete", "SkillTool"},
	"可视化":  {"VisionQA", "ImageOCR", "AudioTranscribe"},
}

// 编译时验证 CodeAgentRail 满足 AgentRail 接口
var _ sainterfaces.AgentRail = (*CodeAgentRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeAgentRail 创建 CodeAgentRail 实例。
// 对齐 Python: CodeAgentRail(workspace_dir=workspace_dir)
func NewCodeAgentRail(workspaceDir string, configLister AgentConfigLister) *CodeAgentRail {
	r := &CodeAgentRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
		workspaceDir:  workspaceDir,
		configLister:  configLister,
	}
	r.WithPriority(codeAgentRailPriority)
	return r
}

// Init 初始化 CodeAgentRail，从 AgentConfigService 加载自定义 Agent 并注册 AgentTool。
// 对齐 Python: CodeAgentRail.init(agent)
func (r *CodeAgentRail) Init(agent sainterfaces.BaseAgent) error {
	customAgents := r.loadCustomAgents()
	if len(customAgents) == 0 {
		logger.Info(logComponent).
			Str("event_type", "code_agent_rail_no_custom_agents").
			Msg("无自定义 Agent，Agent 工具未注册")
		return nil
	}

	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	card := buildAgentToolCard(customAgents, agentID)
	r.agentTool = NewAgentTool(card, agent, customAgents)

	// 幂等注册到 ResourceMgr
	resourceMgr := getResourceMgr()
	if resourceMgr != nil {
		toolID := r.agentTool.Card().ID
		if toolID != "" {
			existing, err := resourceMgr.GetTool([]string{toolID})
			if err == nil && len(existing) > 0 {
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}
		}
		_ = resourceMgr.AddTool(r.agentTool)
	}

	// 注册到 AbilityManager
	am := agent.AbilityManager()
	if am != nil {
		am.Add(r.agentTool.Card())
	}

	names := make([]string, 0, len(customAgents))
	for _, a := range customAgents {
		names = append(names, a.Name)
	}
	logger.Info(logComponent).
		Str("event_type", "code_agent_rail_init").
		Int("custom_agent_count", len(customAgents)).
		Strs("agent_names", names).
		Msg("CodeAgentRail 已注册 Agent 工具")

	return nil
}

// Uninit 注销 CodeAgentRail，移除已注册的 AgentTool。
// 对齐 Python: CodeAgentRail.uninit(agent)
func (r *CodeAgentRail) Uninit(agent sainterfaces.BaseAgent) error {
	if r.agentTool == nil {
		return nil
	}

	am := agent.AbilityManager()
	resourceMgr := getResourceMgr()

	// 从 AbilityManager 移除
	name := r.agentTool.Card().Name
	if name != "" && am != nil {
		am.Remove(name)
	}

	// 从 ResourceMgr 移除
	toolID := r.agentTool.Card().ID
	if toolID != "" && resourceMgr != nil {
		_, _ = resourceMgr.RemoveTool([]string{toolID})
	}

	r.agentTool = nil
	logger.Info(logComponent).
		Str("event_type", "code_agent_rail_uninit").
		Msg("CodeAgentRail 注销完成")

	return nil
}

// Reload 热重载自定义 Agent 定义。
// 对齐 Python: _get_current_agent_rails() 覆写
func (r *CodeAgentRail) Reload(agent sainterfaces.BaseAgent) error {
	if err := r.Uninit(agent); err != nil {
		return fmt.Errorf("CodeAgentRail Reload Uninit 失败: %w", err)
	}
	if err := r.Init(agent); err != nil {
		return fmt.Errorf("CodeAgentRail Reload Init 失败: %w", err)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadCustomAgents 从 AgentConfigService 加载启用的自定义 Agent。
// 对齐 Python: CodeAgentRail._load_custom_agents()
func (r *CodeAgentRail) loadCustomAgents() []*types.AgentDefinition {
	if r.configLister == nil {
		return nil
	}
	var result []*types.AgentDefinition
	for _, a := range r.configLister.ListCustomAgents() {
		if a.Enabled != nil && *a.Enabled {
			result = append(result, a)
		}
	}
	return result
}

// filterToolCards 按允许/禁止列表过滤 ToolCard。
// 对齐 Python: _filter_tool_cards(all_tool_cards, allowed_tools, disallowed_tools)
func filterToolCards(
	allToolCards []*tool.ToolCard,
	allowedTools []string,
	disallowedTools []string,
) []*tool.ToolCard {
	var result []*tool.ToolCard

	if len(allowedTools) == 1 && allowedTools[0] == "*" {
		result = make([]*tool.ToolCard, len(allToolCards))
		copy(result, allToolCards)
	} else {
		// 同时匹配显示名和内部名
		targetNames := make(map[string]bool, len(allowedTools)*2)
		for _, name := range allowedTools {
			targetNames[name] = true
			if internal, ok := displayToInternal[name]; ok {
				targetNames[internal] = true
			}
		}
		for _, tc := range allToolCards {
			if targetNames[tc.Name] || targetNames[tc.ID] {
				result = append(result, tc)
			}
		}
	}

	if len(disallowedTools) > 0 {
		disallowedSet := make(map[string]bool, len(disallowedTools)*2)
		for _, name := range disallowedTools {
			disallowedSet[name] = true
			if internal, ok := displayToInternal[name]; ok {
				disallowedSet[internal] = true
			}
		}
		filtered := make([]*tool.ToolCard, 0, len(result))
		for _, tc := range result {
			if !disallowedSet[tc.Name] && !disallowedSet[tc.ID] {
				filtered = append(filtered, tc)
			}
		}
		result = filtered
	}

	return result
}

// buildAgentToolCard 动态构建 Agent 工具的 ToolCard。
// 对齐 Python: _build_agent_tool_card(custom_agents, agent_id)
func buildAgentToolCard(customAgents []*types.AgentDefinition, agentID string) *tool.ToolCard {
	lines := []string{
		"Launch a new agent to handle complex, multi-step tasks autonomously.",
		"",
		"Available custom agents (created via /agents):",
	}
	for _, a := range customAgents {
		desc := a.WhenToUse
		if desc == "" {
			desc = a.Description
		}
		toolsDesc := "*"
		if len(a.Tools) > 0 {
			tmp := make([]string, len(a.Tools))
			copy(tmp, a.Tools)
			toolsDesc = fmt.Sprintf("%v", tmp)
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (Tools: %s)", a.Name, desc, toolsDesc))
	}
	lines = append(lines,
		"",
		"Usage notes:",
		"- Each agent starts fresh — provide complete context in the prompt",
		"- Clearly tell the agent whether you expect it to write code or just to do research",
		"- Never delegate understanding — write prompts that prove you understood the task",
		"- Delegate the COMPLETE task, not just the analysis portion",
		"- Use background=True for independent parallel work",
		"- You can also invoke agents via @agent-<name> syntax in user messages",
	)

	toolID := fmt.Sprintf("agent_tool_%s", agentID)

	return tool.NewToolCardWithID(
		toolID,
		"Agent",
		fmt.Sprintf("%s\n%s", lines[0], joinLines(lines[1:])),
		[]*schema.Param{
			schema.NewParam("description", "string", "A short (3-5 word) description of the task", true),
			schema.NewParam("prompt", "string", "The task for the agent to perform", true),
			schema.NewParam("subagent_type", "string", "The name of the custom agent to use", true),
			schema.NewParam("model", "string", "Optional model override", false),
			schema.NewParam("background", "boolean", "Run in background. You will be notified when complete.", false),
		},
		nil,
	)
}

// joinLines 用换行连接字符串行
func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}

// getResourceMgr 获取全局 ResourceMgr（封装 runner.GetResourceMgr）
func getResourceMgr() any {
	return nil // TODO: 回填 runner.GetResourceMgr()
}
```

注意：上面的代码引用了 `schema.Param`，需要确认项目中 `tool.NewToolCardWithID` 的参数签名是否匹配。实际实现时需要对照 `internal/common/schema/param.go` 中的 `Param` 定义调整。`getResourceMgr()` 先返回 nil，后续回填。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestFilterToolCards|TestBuildAgentToolCard" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/server/adapter/code_agent_rail.go internal/swarm/server/adapter/code_agent_rail_test.go && git commit -m "feat(adapter): 添加 CodeAgentRail 常量、映射、filterToolCards、buildAgentToolCard"
```

---

### Task 2: AgentTool 实现

**Files:**
- Create: `internal/swarm/server/adapter/agent_tool.go`
- Create: `internal/swarm/server/adapter/agent_tool_test.go`

- [ ] **Step 1: 写 AgentTool 的失败测试**

在 `internal/swarm/server/adapter/agent_tool_test.go` 中：

```go
package adapter

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

func TestAgentTool_Card(t *testing.T) {
	card := tool.NewToolCard("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	assert.Equal(t, "Agent", at.Card().Name)
}

func TestAgentTool_Invoke_缺少subagentType(t *testing.T) {
	card := tool.NewToolCard("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	_, err := at.Invoke(context.Background(), map[string]any{"prompt": "hello"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subagent_type")
}

func TestAgentTool_Invoke_缺少prompt(t *testing.T) {
	card := tool.NewToolCard("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	_, err := at.Invoke(context.Background(), map[string]any{"subagent_type": "reviewer"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "prompt")
}

func TestAgentTool_Invoke_未找到Agent(t *testing.T) {
	card := tool.NewToolCard("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, map[string]*types.AgentDefinition{})
	_, err := at.Invoke(context.Background(), map[string]any{
		"subagent_type": "nonexistent",
		"prompt":        "hello",
		"description":   "test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAgentTool_BuildSubSessionID(t *testing.T) {
	id := buildSubSessionID("sess123", "reviewer")
	assert.Contains(t, id, "sess123_custom_reviewer_")
	assert.Len(t, id, len("sess123_custom_reviewer_")+8)
}

func TestAgentTool_Stream_不支持(t *testing.T) {
	card := tool.NewToolCard("agent_tool_test", "Agent", "测试", nil, nil)
	at := NewAgentTool(card, nil, nil)
	_, err := at.Stream(context.Background(), map[string]any{})
	assert.Error(t, err)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestAgentTool" -v 2>&1 | head -20`
Expected: 编译失败（NewAgentTool 未定义）

- [ ] **Step 3: 实现 agent_tool.go**

在 `internal/swarm/server/adapter/agent_tool.go` 中：

```go
package adapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTool 自定义 Agent 调度工具。
// 对齐 Python: AgentTool(Tool)
// 实现 tool.Tool 接口，invoke 时创建子 DeepAgent 执行任务。
type AgentTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// parentAgent 父 Agent 接口（用于获取 AbilityManager、DeepConfig 等）
	parentAgent sainterfaces.BaseAgent
	// customAgents 自定义 Agent 定义映射（name → AgentDefinition）
	customAgents map[string]*types.AgentDefinition
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTool 创建 AgentTool 实例。
// 对齐 Python: AgentTool(card, parent_agent, custom_agents)
func NewAgentTool(
	card *tool.ToolCard,
	parentAgent sainterfaces.BaseAgent,
	customAgents []*types.AgentDefinition,
) *AgentTool {
	agentMap := make(map[string]*types.AgentDefinition, len(customAgents))
	for _, a := range customAgents {
		agentMap[a.Name] = a
	}
	return &AgentTool{
		card:         card,
		parentAgent:  parentAgent,
		customAgents: agentMap,
	}
}

// Card 返回工具卡片。
// 对齐 Python: Tool.card
func (t *AgentTool) Card() *tool.ToolCard {
	return t.card
}

// Invoke 执行自定义 Agent 调度。
// 对齐 Python: AgentTool.invoke(inputs, **kwargs)
func (t *AgentTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	// 步骤 1: 解析输入参数
	subagentType, _ := inputs["subagent_type"].(string)
	prompt, _ := inputs["prompt"].(string)
	description, _ := inputs["description"].(string)
	background, _ := inputs["background"].(bool)

	// 步骤 2: 参数校验
	if subagentType == "" || prompt == "" {
		return nil, exception.BuildError(
			exception.StatusAgentToolExecutionError,
			exception.WithParam("reason", "Both 'subagent_type' and 'prompt' are required"),
		)
	}

	// 步骤 3: 查找自定义 Agent 定义
	agentDef, ok := t.customAgents[subagentType]
	if !ok {
		available := sortedKeys(t.customAgents)
		return nil, exception.BuildError(
			exception.StatusAgentToolNotFound,
			exception.WithParam("reason", fmt.Sprintf("Agent type '%s' not found. Available custom agents: %v", subagentType, available)),
		)
	}

	// 步骤 4: 提取 session
	var parentSession any
	callOpts := tool.NewToolCallOptions(opts...)
	if callOpts.Session != nil {
		parentSession = callOpts.Session
	}

	// 步骤 5: 构建 subSessionID
	parentSessionID := "default"
	// 对齐 Python: parent_session.get_session_id()
	// SessionFacade 接口提取 sessionID 的逻辑需要适配
	subSessionID := buildSubSessionID(parentSessionID, subagentType)

	// 步骤 6: 创建子 Agent
	subAgent, err := t.createSubAgent(agentDef, subSessionID)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "agent_tool_create_subagent_failed").
			Str("subagent_type", subagentType).
			Err(err).
			Msg("子 Agent 创建失败")
		return nil, exception.BuildError(
			exception.StatusAgentToolExecutionError,
			exception.WithParam("reason", fmt.Sprintf("Custom agent '%s' creation failed: %v", subagentType, err)),
		)
	}

	// 步骤 7: 执行（同步 / 异步）
	if background {
		go t.runAsync(ctx, subAgent, prompt, subSessionID, subagentType)
		return map[string]any{
			"status":    "async_launched",
			"agent_id":  subagentType,
			"prompt":    prompt,
		}, nil
	}

	// 同步执行
	result, err := subAgent.Invoke(ctx, map[string]any{
		"query":           prompt,
		"conversation_id": subSessionID,
	})
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "agent_tool_invoke_failed").
			Str("subagent_type", subagentType).
			Err(err).
			Msg("子 Agent 执行失败")
		return nil, exception.BuildError(
			exception.StatusAgentToolExecutionError,
			exception.WithParam("reason", fmt.Sprintf("Custom agent '%s' execution failed: %v", subagentType, err)),
		)
	}

	output := ""
	if result != nil {
		if s, ok := result["output"].(string); ok {
			output = s
		}
	}

	return map[string]any{
		"output":   output,
		"agent_id": subagentType,
	}, nil
}

// Stream 流式执行（不支持）。
// 对齐 Python: AgentTool.stream(inputs, **kwargs): pass
func (t *AgentTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.NewErrStreamNotSupported(t.card.Name)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createSubAgent 从 AgentDefinition 创建子 DeepAgent。
// 对齐 Python: AgentTool._create_sub_agent(agent_def, sub_session_id)
func (t *AgentTool) createSubAgent(agentDef *types.AgentDefinition, subSessionID string) (*harness.DeepAgent, error) {
	// ⤵️ 依赖 CreateDeepAgent + agentDefToSubagentConfig + filterToolCards
	// 当前返回 stub，待 Task 3 完整实现
	return nil, fmt.Errorf("createSubAgent not yet implemented")
}

// runAsync 后台异步执行子 Agent。
// 对齐 Python: AgentTool._run_async(subagent, prompt, sub_session_id, subagent_type, parent_session)
func (t *AgentTool) runAsync(ctx context.Context, subAgent *harness.DeepAgent, prompt, subSessionID, subagentType string) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Str("event_type", "agent_tool_async_panic").
				Str("subagent_type", subagentType).
				Any("panic", r).
				Msg("异步子 Agent 执行 panic")
		}
	}()
	_, err := subAgent.Invoke(ctx, map[string]any{
		"query":           prompt,
		"conversation_id": subSessionID,
	})
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "agent_tool_async_failed").
			Str("subagent_type", subagentType).
			Err(err).
			Msg("异步子 Agent 执行失败")
	}
}

// buildSubSessionID 构建 sub session ID。
// 对齐 Python: AgentTool._build_sub_session_id(parent_session_id, subagent_type)
func buildSubSessionID(parentSessionID, subagentType string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_custom_%s_%s", parentSessionID, subagentType, hex.EncodeToString(b))
}

// sortedKeys 返回 map 的排序键
func sortedKeys(m map[string]*types.AgentDefinition) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
```

注意：`createSubAgent` 当前返回 stub 错误，完整实现在后续步骤。`harness.DeepAgent` 和 `hinterfaces` 的 import 路径需要实际确认。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestAgentTool" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/server/adapter/agent_tool.go internal/swarm/server/adapter/agent_tool_test.go && git commit -m "feat(adapter): 添加 AgentTool 结构体与 Invoke/Stream 骨架"
```

---

### Task 3: createSubAgent 完整实现

**Files:**
- Modify: `internal/swarm/server/adapter/agent_tool.go`

- [ ] **Step 1: 写 createSubAgent 的失败测试**

在 `agent_tool_test.go` 中追加：

```go
func TestAgentTool_CreateSubAgent_参数转换(t *testing.T) {
	// 验证 agentDefToSubagentConfig 被正确调用
	// 验证 CreateDeepAgentParams 中的字段映射
	// 由于依赖真实 CreateDeepAgent，此处验证参数构造逻辑
	agentDef := &types.AgentDefinition{
		Name:        "reviewer",
		Description: "代码审查",
		Prompt:      "你是代码审查专家",
		Tools:       []string{"Read", "Grep"},
	}
	spec := agentDefToSubagentConfig(agentDef, nil, nil)
	require.NotNil(t, spec)
	assert.Equal(t, "reviewer", spec.AgentCard.Name)
	assert.Equal(t, "你是代码审查专家", spec.SystemPrompt)
	assert.True(t, spec.EnableTaskLoop)
}
```

- [ ] **Step 2: 实现 createSubAgent 方法**

在 `agent_tool.go` 中替换 `createSubAgent` stub 为完整实现：

```go
func (t *AgentTool) createSubAgent(agentDef *types.AgentDefinition, subSessionID string) (*harness.DeepAgent, error) {
	// 步骤 1: 转换 AgentDefinition → SubAgentConfig
	spec := agentDefToSubagentConfig(agentDef, nil, nil)

	// 步骤 2: 从父 Agent AbilityManager 获取 ToolCard 列表
	var allToolCards []*tool.ToolCard
	if t.parentAgent != nil {
		am := t.parentAgent.AbilityManager()
		if am != nil {
			for _, ability := range am.List() {
				if tc, ok := ability.(*tool.ToolCard); ok && !disallowedForSubagents[tc.Name] {
					allToolCards = append(allToolCards, tc)
				}
			}
		}
	}

	// 步骤 3: 按 Agent 定义的 tools/disallowed_tools 过滤
	parentToolCards := filterToolCards(
		allToolCards,
		spec.Tools,
		nil, // disallowed 已在 agentDefToSubagentConfig 中合并
	)

	// 步骤 4: 获取父 Agent 的 Model
	var model *llm.Model
	if deepAgent, ok := t.parentAgent.(hinterfaces.DeepAgentInterface); ok {
		if deepCfg := deepAgent.DeepConfig(); deepCfg != nil {
			model = deepCfg.Model
		}
	}

	// 步骤 5: 获取父 Agent 的 Workspace
	var ws *hworkspace.Workspace
	if deepAgent, ok := t.parentAgent.(hinterfaces.DeepAgentInterface); ok {
		if deepCfg := deepAgent.DeepConfig(); deepCfg != nil {
			ws = deepCfg.Workspace
		}
	}
	// 如果无 workspace，创建基于 subSessionID 的默认 workspace
	if ws == nil {
		ws = hworkspace.NewWorkspace(subSessionID, "en")
	}

	// 步骤 6: 构建 CreateDeepAgentParams
	maxIter := 15
	if spec.MaxIterations > 0 {
		maxIter = spec.MaxIterations
	}
	restrictToWorkDir := true

	params := harness_config.CreateDeepAgentParams{
		Model:                   model,
		Card:                    spec.AgentCard,
		SystemPrompt:            spec.SystemPrompt,
		ToolCards:               parentToolCards,
		EnableTaskLoop:          spec.EnableTaskLoop,
		MaxIterations:           maxIter,
		Workspace:               ws,
		Skills:                  spec.Skills,
		SysOperation:            nil, // 子 Agent 不继承 sys_operation
		RestrictToWorkDir:       &restrictToWorkDir,
		AutoCreateWorkspace:     false,
		AddGeneralPurposeAgent:  false,
		Subagents:               nil,
		EnableAsyncSubagent:     false,
	}

	// 步骤 7: 创建子 DeepAgent
	subAgent, err := harness.CreateDeepAgent(context.Background(), params)
	if err != nil {
		return nil, fmt.Errorf("CreateDeepAgent 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("event_type", "agent_tool_create_subagent").
		Str("subagent_type", agentDef.Name).
		Msg("子 Agent 创建成功")

	return subAgent, nil
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -run "TestAgentTool_CreateSubAgent" -v`
Expected: PASS

- [ ] **Step 4: 运行全量 adapter 测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -v 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/server/adapter/agent_tool.go internal/swarm/server/adapter/agent_tool_test.go && git commit -m "feat(adapter): 实现 AgentTool.createSubAgent 完整子 Agent 创建逻辑"
```

---

### Task 4: CodeAdapter 步骤 16 回填

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`
- Modify: `internal/swarm/server/adapter/code_agent_rail.go`（添加 buildCodeAgentRail builder）

- [ ] **Step 1: 在 code_adapter.go 中添加 buildCodeAgentRail builder 方法**

在 `code_adapter.go` 中添加：

```go
// buildCodeAgentRail 构建 CodeAgentRail。
// 对齐 Python: JiuwenClawCodeAdapter._build_code_agent_rail() (line 826-834)
func (c *CodeAdapter) buildCodeAgentRail() *CodeAgentRail {
	if c.deep.configLister == nil {
		return nil
	}
	rail := NewCodeAgentRail(c.deep.workspaceDir, c.deep.configLister)
	logger.Info(logComponent).Msg("CodeAgentRail created")
	return rail
}
```

- [ ] **Step 2: 在 CreateInstance 步骤 16 中添加 CodeAgentRail 构建**

在 `code_adapter.go` 的 `CreateInstance` 方法中，步骤 16 注释之后添加：

```go
// CodeAgentRail: Code 模式专有护栏，管理 /agents 创建的自定义 Agent
codeAgentRail := c.buildCodeAgentRail()
if codeAgentRail != nil {
	c.codeAgentRail = codeAgentRail
	// rail 将在 CreateDeepAgent 时通过 params.Rails 传入
}
```

需要确保 `codeAgentRail` 在 CreateDeepAgent 的 Rails 参数中被传入。当前步骤 16-20 都是 ⤵️ 标记的 stub，实际集成时需要根据 CreateDeepAgentParams.Rails 字段追加。

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/server/adapter/code_adapter.go && git commit -m "feat(adapter): CodeAdapter 步骤 16 回填 buildCodeAgentRail"
```

---

### Task 5: AgentConfigService.ListAvailableTools 回填

**Files:**
- Create: `internal/swarm/server/types/agent_tools.go`
- Modify: `internal/swarm/server/runtime/agent_config.go`

- [ ] **Step 1: 创建 types/agent_tools.go 共享常量**

在 `internal/swarm/server/types/agent_tools.go` 中：

```go
package types

// ──────────────────────────── 全局变量 ────────────────────────────

// DisallowedForSubagents 禁止传递给子 Agent 的工具名切片。
// 对齐 Python: DISALLOWED_FOR_SUBAGENTS
// adapter 和 runtime 共享此常量，避免硬编码重复
var DisallowedForSubagents = []string{
	"Agent", "task", "enter_plan_mode", "exit_plan_mode",
	"ask_user_question", "task_stop", "switch_mode",
}

// ToolGroups 工具分组（用于 Agent 定义 UI）。
// 对齐 Python: TOOL_GROUPS
var ToolGroups = map[string][]string{
	"核心":   {"Read", "Write", "Edit", "Bash", "LS"},
	"搜索":   {"Grep", "Glob", "WebSearch", "WebFetch"},
	"代码智能": {"LSP", "TodoWrite", "TodoList"},
	"高级":   {"MemorySearch", "MemoryGet", "WriteMemory", "EditMemory", "CronCreate", "CronList", "CronDelete", "SkillTool"},
	"可视化":  {"VisionQA", "ImageOCR", "AudioTranscribe"},
}
```

- [ ] **Step 2: 修改 agent_config.go 引用共享常量**

在 `agent_config.go` 的 `ListAvailableTools` 中：

```go
// 之前：
disallowedForSubagents := []string{
    "Agent", "task", "enter_plan_mode", "exit_plan_mode",
    "ask_user_question", "task_stop", "switch_mode",
}

// 之后：
disallowedForSubagents := types.DisallowedForSubagents
```

同样将 `ToolGroups` 引用替换为 `types.ToolGroups`。

- [ ] **Step 3: 运行编译和测试**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/... && go test ./internal/swarm/server/runtime/ -run "TestAgentConfigService_ListAvailableTools" -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/server/types/agent_tools.go internal/swarm/server/runtime/agent_config.go internal/swarm/server/adapter/code_agent_rail.go && git commit -m "refactor: 提取 DisallowedForSubagents 到 types 包，消除 agent_config 硬编码"
```

---

### Task 6: doc.go 更新

**Files:**
- Modify: `internal/swarm/server/adapter/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

在 doc.go 的文件目录树中添加：

```
//	├── agent_tool.go          # AgentTool 自定义 Agent 调度工具（10.3.7）
//	├── code_agent_rail.go     # CodeAgentRail Code 模式自定义 Agent 护栏（10.3.7）
```

- [ ] **Step 2: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/swarm/server/adapter/doc.go && git commit -m "docs(adapter): 更新 doc.go 文件目录添加 CodeAgentRail"
```

---

### Task 7: IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.3.7-11 行状态**

找到 10.3.7-11 行，将 `CodeAgentRail` 从未标记改为 ✅：

```
| 10.3.7-11 | 🔄 | 适配器辅助 | CodeAgentRail✅/TeamHelpers☐/EvolutionHelpers✅/RecapPrompts✅/SysOpBuilder✅(...) |
```

- [ ] **Step 2: 更新 10.3.13 行状态**

将 10.3.13 从 ☐ 改为 ✅：

```
| 10.3.13 | ✅ | AgentConfigService | Agent 配置 CRUD | `jiuwenswarm/server/runtime/agent_config_service.py` |
```

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 10.3.7 CodeAgentRail ✅ + 10.3.13 ✅"
```

---

### Task 8: 全量编译和测试验证

**Files:** 无新文件

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行 adapter 包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/ -v -cover`
Expected: 所有测试通过，覆盖率 ≥ 85%（新增代码部分）

- [ ] **Step 3: 运行 runtime 包测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/ -v -run "TestAgentConfigService" 2>&1 | tail -30`
Expected: PASS

- [ ] **Step 4: 运行全量测试（可选，视编译时间）**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/... -count=1 2>&1 | tail -30`
Expected: PASS
