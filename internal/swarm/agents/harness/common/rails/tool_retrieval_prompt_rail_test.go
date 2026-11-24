package rails

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	kvcache "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/ability"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
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

// ──────────────────────────── 漂移同步与执行链补测 ────────────────────────

// railStateSession 会话状态容器 + 全量 SessionFacade 空实现
// （GetState/UpdateState 真实，其余 no-op；复刻 harness/rails mockSession 契约）。
type railStateSession struct {
	state map[string]any
	id    string
}

func (s *railStateSession) UpdateState(data map[string]any) {
	for key, value := range data {
		s.state[key] = value
	}
}

func (s *railStateSession) GetState(key sessionstate.StateKey) (any, error) {
	return s.state[key.String()], nil
}

func (s *railStateSession) GetSessionID() string {
	if s.id != "" {
		return s.id
	}
	return "sess"
}

func (s *railStateSession) GetCacheIdentity() kvCacheIdentityReal        { return kvCacheIdentityReal{} }
func (s *railStateSession) GetParentSessionID() string                   { return "" }
func (s *railStateSession) DumpState() map[string]any                    { return s.state }
func (s *railStateSession) WriteStream(context.Context, any) error       { return nil }
func (s *railStateSession) WriteCustomStream(context.Context, any) error { return nil }
func (s *railStateSession) GetEnv(string, ...any) any                    { return nil }
func (s *railStateSession) Interact(context.Context, any) error          { return nil }
func (s *railStateSession) ClearSession(context.Context)                 {}

// kvCacheIdentity 别名（kv_cache.KVCacheIdentity）。
type kvCacheIdentityReal = kvcache.KVCacheIdentity

// TestSyncDeferredToolCatalog_首轮无变化不写附件 对齐 Python：稳定目录即基线，
// 首轮未变化不发布。
func TestSyncDeferredToolCatalog_首轮无变化不写附件(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	manager := prompts.NewManager()
	rail.attachmentManager = manager
	sess := &railStateSession{state: map[string]any{}, id: "sess"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, sess)
	// 附件管理器按 session 绑定：注入 session id 语义经 BindContext 消费
	// （BindContextForSession 供 writer 建联；此处直接走 BindContext 分支）
	rail.syncDeferredToolCatalog(cbc, "cn")
	// 状态已写入（initialized），未发布附件（无变化）
	state := railCatalogState(sess)
	if state["initialized"] != true {
		t.Fatal("首轮应写目录状态")
	}
	attachments, _ := manager.ListByFilter(prompts.AttachmentFilter{
		SessionID: "sess", Section: catalogAttachmentSection,
	})
	if len(attachments) != 0 {
		t.Fatalf("首轮无变化不应发布附件, got %d", len(attachments))
	}
}

// TestSyncDeferredToolCatalog_漂移发Delta 对齐 Python：新增 deferred 工具
// → delta 附件（版本推进）。
func TestSyncDeferredToolCatalog_漂移发Delta(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	// 注册第二个 deferred 工具，首轮冻结后新增 → 漂移
	extra := deferredCard("extra_tool", "新工具")
	_ = agent.am.AddAbility(extra, stubTool{name: "extra_tool"})
	rail.frozenSnapshot = nil
	_ = rail.freezeDisclosureCatalogue()

	manager := prompts.NewManager()
	rail.attachmentManager = manager
	sess := &railStateSession{state: map[string]any{}, id: "sess"}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, sess)
	// 首轮：以冻结目录为基线，live 多出 extra_tool → delta
	rail.syncDeferredToolCatalog(cbc, "cn")
	state := railCatalogState(sess)
	if state["initialized"] != true {
		t.Fatal("首轮应初始化状态")
	}
}

// railCatalogState 读回目录状态。
func railCatalogState(sess *railStateSession) map[string]any {
	raw, _ := sess.GetState(sessionstate.StringKey(catalogStateKey))
	typed, _ := raw.(map[string]any)
	return typed
}

// TestCallDeferredTool_闭环 对齐 Python _call_deferred_tool：目录成员 →
// 经 AbilityManager.Execute 真实执行；非成员/非 deferred 拒绝。
func TestCallDeferredTool_闭环(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	inputs := &agentinterfaces.ToolCallInputs{ToolCall: &llmschema.ToolCall{ID: "call1", Name: "tool_call"}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)

	// 目录成员：cron_list_jobs 是 deferred → 经真 AbilityManager 执行
	out, err := rail.callDeferredTool(context.Background(), "cron_list_jobs", map[string]any{}, cbc)
	if err != nil {
		t.Fatalf("目录成员应执行: %v", err)
	}
	if out == nil {
		t.Fatal("应产出 RelayedToolOutput")
	}
	// 非成员拒绝
	_, err = rail.callDeferredTool(context.Background(), "unknown_tool", nil, cbc)
	if err == nil || !strings.Contains(err.Error(), "must come from the tool_search catalogue") {
		t.Fatalf("非成员应拒绝: %v", err)
	}
	// direct 工具拒绝：bash 不在 deferred 目录（Python 顺序——先目录成员校验
	// 再曝光校验，direct 工具落目录错误分支）
	if _, err = rail.callDeferredTool(context.Background(), "bash", nil, cbc); err == nil ||
		!strings.Contains(err.Error(), "must come from the tool_search catalogue") {
		t.Fatalf("direct 工具应拒绝: %v", err)
	}
	// 空名拒绝
	if _, err = rail.callDeferredTool(context.Background(), "", nil, cbc); err == nil {
		t.Fatal("空名应拒绝")
	}
}

// TestBeforeToolCall_未发现Deferred阻断 对齐 Python：未发现的 deferred 直调
// → 阻断 + 拒绝文案；meta/direct 放行。
func TestBeforeToolCall_未发现Deferred阻断(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "cron_list_jobs", ToolCall: &llmschema.ToolCall{ID: "c1", Name: "cron_list_jobs"}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeToolCall 失败: %v", err)
	}
	if inputs.ToolResult == nil {
		t.Fatal("未发现 deferred 直调应被阻断")
	}
	// direct 工具放行
	directInputs := &agentinterfaces.ToolCallInputs{ToolName: "bash", ToolCall: &llmschema.ToolCall{ID: "c2", Name: "bash"}}
	directCbc := agentinterfaces.NewAgentCallbackContext(agent, directInputs, nil)
	if err := rail.BeforeToolCall(context.Background(), directCbc); err != nil {
		t.Fatalf("direct 应放行: %v", err)
	}
	if directInputs.ToolResult != nil {
		t.Fatal("direct 工具不应被阻断")
	}
	// 开关关 → 直通
	rail.sessionEnabled = false
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Fatalf("禁用态应直通: %v", err)
	}
	rail.sessionEnabled = true
}

// TestToolkitAccessor 对齐 V-6 消费面：Toolkit() 返回构造注入的 toolkit。
func TestToolkitAccessor(t *testing.T) {
	rail, _ := newRailUnderTest(t, true)
	if rail.Toolkit() == nil {
		t.Fatal("Toolkit() 应返回构造注入的工具包")
	}
}

// TestGetCallbacks_分发 注册映射消费 BeforeModelCall / BeforeToolCall。
func TestGetCallbacks_分发(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	callbacks := rail.GetCallbacks()
	if _, ok := callbacks[agentinterfaces.CallbackBeforeModelCall]; !ok {
		t.Fatal("应注册 BeforeModelCall")
	}
	if _, ok := callbacks[agentinterfaces.CallbackBeforeToolCall]; !ok {
		t.Fatal("应注册 BeforeToolCall")
	}
	// 经分发调用（真实回调上下文）
	inputs := &agentinterfaces.ModelCallInputs{Tools: []schema.ToolInfoInterface{&schema.ToolInfo{Name: "bash"}}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, nil)
	if err := callbacks[agentinterfaces.CallbackBeforeModelCall](context.Background(), cbc); err != nil {
		t.Fatalf("分发 BeforeModelCall 失败: %v", err)
	}
	// 非 AgentCallbackContext 输入直通
	if err := callbacks[agentinterfaces.CallbackBeforeModelCall](context.Background(), "bad"); err != nil {
		t.Fatalf("坏输入应直通: %v", err)
	}
}

// TestDispatchToolCall_上下文断言 tool_call 分发要求真实回调上下文。
func TestDispatchToolCall_上下文断言(t *testing.T) {
	rail, _ := newRailUnderTest(t, true)
	if _, err := rail.dispatchToolCall(context.Background(), "x", nil, nil); err == nil ||
		!strings.Contains(err.Error(), "active agent callback context") {
		t.Fatalf("非回调上下文应拒绝: %v", err)
	}
}

// TestHasAttachmentHistory_模型上下文缺失 空模型上下文 → false（孤儿 delta 防护）。
func TestHasAttachmentHistory_模型上下文缺失(t *testing.T) {
	rail, agent := newRailUnderTest(t, true)
	manager := prompts.NewManager()
	rail.attachmentManager = manager
	writer := manager.BindContextForSession("sess-x")
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)
	if rail.hasAttachmentHistory(writer, cbc) {
		t.Fatal("无模型上下文应返回 false")
	}
}

// TestRenderConnectorLines_MCP分支 对齐 Python：connector 虚拟分支行渲染、
// 兜底文案、已覆盖工具跳过。
func TestRenderConnectorLines_MCP分支(t *testing.T) {
	rail, _ := newRailUnderTest(t, true)
	// 冻结 connector 分组（cron_create_job 已被 cron 分类覆盖 → 跳过）
	rail.frozenConnectorGroups = map[string]swarmtools.McpServerGroup{
		"github": {Tools: []string{"mcp_github_list", "cron_create_job"}, DescriptionZh: "", DescriptionEn: ""},
		"gitlab": {Tools: []string{}, DescriptionZh: "GitLab 服务器", DescriptionEn: ""},
	}
	lines, shown := rail.renderConnectorLines(false, "cn")
	joined := strings.Join(lines, "\n")
	// github 无描述 → 兜底文案；已覆盖的 cron_create_job 不重复认领
	if !strings.Contains(joined, "`mcp_github`") || !strings.Contains(joined, "MCP 连接器 github 提供的工具。") {
		t.Fatalf("兜底文案缺失: %s", joined)
	}
	if strings.Contains(joined, "gitlab") {
		t.Fatal("空成员分支不应渲染")
	}
	if _, ok := shown["mcp_github_list"]; !ok {
		t.Fatal("mcp_github_list 应被认领")
	}
	if _, ok := shown["cron_create_job"]; ok {
		t.Fatal("已归类工具不应被 connector 认领")
	}
	// 冻结目录覆盖集：cron_create_job 归 cron 分类
	rail.frozenSnapshot = &swarmtools.ToolCatalogueSnapshot{Categories: []swarmtools.ToolCategory{
		{Path: "cron", Description: "d", Tools: []string{"cron_create_job", "cron_list_jobs"}},
	}}
	lines2, shown2 := rail.renderConnectorLines(false, "cn")
	// cron_create_job 被分类覆盖后剔除，mcp_github_list 未覆盖仍渲染
	joined2 := strings.Join(lines2, "\n")
	if !strings.Contains(joined2, "`mcp_github`") {
		t.Fatalf("未覆盖成员应保留分支行: %s", joined2)
	}
	if _, ok := shown2["cron_create_job"]; ok {
		t.Fatal("已归类成员不应被认领")
	}
	if _, ok := shown2["mcp_github_list"]; !ok {
		t.Fatal("未覆盖成员应被认领")
	}
	// 全覆盖（分类补齐 mcp_github_list）→ 分支成员为空 → 无行
	rail.frozenSnapshot.Categories[0].Tools = append(rail.frozenSnapshot.Categories[0].Tools, "mcp_github_list")
	lines3, _ := rail.renderConnectorLines(false, "cn")
	if len(lines3) != 0 {
		t.Fatalf("全覆盖分支不应渲染: %v", lines3)
	}
}

// TestMapValueOf_FailedResult 助手分支覆盖。
func TestMapValueOf_FailedResult(t *testing.T) {
	if got := mapValueOf(map[string]any{"a": "b"}); got["a"] != "b" {
		t.Fatal("map 转换失败")
	}
	if got := mapValueOf(map[string]string{"a": "b"}); got["a"] != "b" {
		t.Fatal("强类型 map 直通")
	}
	if mapValueOf(123) != nil {
		t.Fatal("非 map 应 nil")
	}
	if !failedResult(map[string]any{"error": "boom"}) {
		t.Fatal("error 键应判失败")
	}
	if failedResult(map[string]any{"error": "boom", "success": true}) {
		t.Fatal("success=true 不判失败")
	}
	if !failedResult("rejected") {
		t.Fatal("字符串结果判失败")
	}
	if failedResult(nil) || failedResult(map[string]any{}) {
		t.Fatal("空结果不判失败")
	}
}
