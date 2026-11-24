package rails

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/ability"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ──────────────────────────── 测试基建 ────────────────────────────
// agent 复用包内既有 fakeBaseAgent（structured_ask_user_rail_test.go）：
// 真实 AbilityManager（ability.NewAbilityManager）+ 真实 SystemPromptBuilder。
// ──────────────────────────── 构造辅助 ────────────────────────────

// deferredCard 构造 deferred 曝光的工具卡。
func deferredCard(name, description string) *tool.ToolCard {
	card := tool.NewToolCardWithID(name, name, description, nil, nil)
	card.Exposure = tool.ToolExposureDeferred
	card.SetExposureDeclared(true)
	return card
}

// directCard 构造 direct 曝光的工具卡。
func directCard(name, description string) *tool.ToolCard {
	card := tool.NewToolCardWithID(name, name, description, nil, nil)
	card.Exposure = tool.ToolExposureDirect
	card.SetExposureDeclared(true)
	return card
}

// newRailUnderTest 真实 AbilityManager + 真实 builder + 冻结目录的 rail。
func newRailUnderTest(t *testing.T, enabled bool) (*ToolRetrievalPromptRail, *fakeBaseAgent) {
	t.Helper()
	agent := &fakeBaseAgent{
		am: ability.NewAbilityManager(nil),
		sb: saprompt.NewSystemPromptBuilder(),
	}
	// 注册 deferred 工具（cron 类两件 + 单成员 read_pdf）+ 一件 direct。
	_ = agent.am.AddAbility(deferredCard("cron_create_job", "创建定时任务"), stubTool{name: "cron_create_job"})
	_ = agent.am.AddAbility(deferredCard("cron_list_jobs", "列出定时任务"), stubTool{name: "cron_list_jobs"})
	_ = agent.am.AddAbility(deferredCard("read_pdf", "读取 PDF 文本"), stubTool{name: "read_pdf"})
	_ = agent.am.AddAbility(directCard("bash", "执行命令"), stubTool{name: "bash"})

	provider := func() []schema.ToolInfoInterface {
		infos := []schema.ToolInfoInterface{}
		for _, abilityCard := range agent.am.List() {
			if card, ok := abilityCard.(*tool.ToolCard); ok && card.EffectiveExposure() == tool.ToolExposureDeferred {
				infos = append(infos, card.ToolInfo())
			}
		}
		return infos
	}
	toolkit := swarmtools.NewToolRetrievalToolkit(provider, "sess", nil, nil, "cn")
	configBase := map[string]any{}
	if enabled {
		configBase = map[string]any{"progressive_tool_enabled": true, "progressive_tool_optimize": true}
	}
	rail := NewToolRetrievalPromptRail(toolkit, configBase)
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	rail.agent = agent
	return rail, agent
}

// stubTool 最小可执行工具（真 tool.Tool 实现，仅回显）。
type stubTool struct{ name string }

func (s stubTool) Card() *tool.ToolCard { return nil }
func (s stubTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"success": true, "echo": inputs}, nil
}
func (s stubTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}
func (s stubTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// ──────────────────────────── 用例 ────────────────────────────

// TestRailInit_冻结目录 对齐 Python init：冻结 live deferred 集 + 注册信封。
func TestRailInit_冻结目录(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	if len(rail.frozenToolNames) != 3 {
		t.Fatalf("应冻结 3 个 deferred 工具, got %d", len(rail.frozenToolNames))
	}
	if _, ok := rail.ownedToolCards[toolCallToolName]; !ok {
		t.Fatal("tool_call 信封未注册")
	}
	// 信封以 direct 曝光注册（防渐进策略误藏）
	card := agent.am.Get(toolCallToolName)
	envelopeCard, ok := card.(*tool.ToolCard)
	if !ok || envelopeCard.EffectiveExposure() != tool.ToolExposureDirect {
		t.Fatalf("信封应 direct 曝光: %+v", envelopeCard)
	}
}

// TestRailBeforeModelCall_禁用直通 对齐 Python：开关关 → 不注入不过滤。
func TestRailBeforeModelCall_禁用直通(t *testing.T) {
	rail, agent := newRailUnderTest(t, false)
	if err := rail.BeforeModelCall(context.Background(), agentinterfaces.NewAgentCallbackContext(agent, nil, nil)); err != nil {
		t.Fatalf("禁用态应直通: %v", err)
	}
}

// TestBeforeModelCall_目录节注入 对齐 Python：section 注入含 intro 与
// 分类行（cron 成员 2 ≥ 2 保留；read_pdf 单成员折叠进未归类块）。
func TestBeforeModelCall_目录节注入(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)
	if err := rail.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 失败: %v", err)
	}
	// 真实 builder 具体类型才有 Build；接口断言取回（非替身，构造即真实现）。
	builder, ok := agent.sb.(*saprompt.SystemPromptBuilder)
	if !ok {
		t.Fatal("builder 应为真实 SystemPromptBuilder")
	}
	rendered := builder.Build()
	for _, want := range []string{"## Deferred 工具（渐进式披露）", "[category] `cron`", "[tool] `read_pdf`", "tool_search 使用规则"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("披露节缺 %q:\n%s", want, rendered)
		}
	}
	// 单成员 documents 分类被折叠：不应出现 documents 分类行
	if strings.Contains(rendered, "[category] `documents`") {
		t.Fatalf("单成员分类应折叠:\n%s", rendered)
	}
}

// TestBeforeModelCall_过滤InputsTools 对齐 Python：inputs.tools 缩为
// direct ∪ 元工具，deferred 隐藏；meta reslot 到基线槽位。
func TestBeforeModelCall_过滤InputsTools(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	// 预注册 tool_search（模拟 DeepAdapter/信封注入）
	_ = agent.am.AddAbility(directCard(toolSearchToolName, "搜索"), stubTool{name: toolSearchToolName})
	_ = agent.am.AddAbility(directCard(baselineMetaAnchor, "任务工具"), stubTool{name: baselineMetaAnchor})

	inputs := &agentinterfaces.ModelCallInputs{
		Tools: []schema.ToolInfoInterface{
			&schema.ToolInfo{Name: "bash"},
			&schema.ToolInfo{Name: "cron_create_job"},
			&schema.ToolInfo{Name: toolSearchToolName},
			&schema.ToolInfo{Name: baselineMetaAnchor},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)
	if err := rail.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 失败: %v", err)
	}
	names := []string{}
	for _, info := range inputs.Tools {
		names = append(names, info.GetName())
	}
	joined := strings.Join(names, ",")
	if strings.Contains(joined, "cron_create_job") {
		t.Fatalf("deferred 工具应被过滤: %v", names)
	}
	// meta reslot：tool_search 应在 task_tool 之后
	searchIdx, anchorIdx := -1, -1
	for i, name := range names {
		if name == toolSearchToolName {
			searchIdx = i
		}
		if name == baselineMetaAnchor {
			anchorIdx = i
		}
	}
	if anchorIdx < 0 || searchIdx != anchorIdx+1 {
		t.Fatalf("tool_search 应回位到 task_tool 之后: %v", names)
	}
}

// TestCompactDescription_截断 对齐 Python _compact_description：第一句优先，
// 否则截到 120 字符 + 省略号。
func TestCompactDescription_截断(t *testing.T) {
	long := strings.Repeat("描述", 100)
	got := compactDescription(long)
	if n := len([]rune(got)); n != maxEntryDescChars {
		t.Fatalf("截断应 %d 字符, got %d", maxEntryDescChars, n)
	}
	// 第一句（<120 字符）优先整句保留
	sentence := strings.Repeat("a", 50) + "。" + strings.Repeat("b", 200)
	if got := compactDescription(sentence); !strings.HasSuffix(got, "。") {
		t.Fatalf("第一句优先保留: %q", got)
	}
	// 第一句超限时仍走截断
	longSentence := strings.Repeat("a", 200) + "。" + strings.Repeat("b", 200)
	if got := compactDescription(longSentence); !strings.HasSuffix(got, "…") {
		t.Fatalf("超限第一句应走截断: %q", got)
	}
}

// TestUninit_只摘自有卡片 对齐 Python uninit：其他所有者重新注册的名字不动。
func TestUninit_只摘自有卡片(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	envelopeCard := rail.ownedToolCards[toolCallToolName]
	if envelopeCard == nil {
		t.Fatal("信封应已注册")
	}
	// 模拟其他所有者重注册同名卡
	other := directCard(toolCallToolName, "其他所有者")
	_ = agent.am.Remove(toolCallToolName)
	_ = agent.am.AddAbility(other, stubTool{name: toolCallToolName})
	if err := rail.Uninit(agent); err != nil {
		t.Fatalf("Uninit 失败: %v", err)
	}
	// 其他所有者的卡仍在
	if agent.am.Get(toolCallToolName) == nil {
		t.Fatal("他人重注册的卡不应被摘除")
	}
	// 状态清空
	if rail.frozenSnapshot != nil || len(rail.frozenToolNames) != 0 || rail.catalogueFinalized {
		t.Fatal("Uninit 后状态应清空")
	}
}
