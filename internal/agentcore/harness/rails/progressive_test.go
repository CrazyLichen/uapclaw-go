package rails

import (
	"context"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	"strconv"
	"strings"
	"testing"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	tokeniface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/tool_discovery"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// progressiveFakeAbilityManager 测试用能力管理器（只实现 rail 需要的能力）。
type progressiveFakeAbilityManager struct {
	// cards 工具卡片注册表
	cards map[string]*tool.ToolCard
	// revision 注册版本
	revision int
	// executeResults Execute 的固定返回
	executeResults []agentschema.ExecuteResult
	// executeCalls Execute 调用次数
	executeCalls int
}

// AddAbility 空实现（接口扩张后补齐，规则 10）。
func (f *progressiveFakeAbilityManager) AddAbility(card *tool.ToolCard, _ tool.Tool) agentschema.AddAbilityResult {
	if card == nil {
		return agentschema.AddAbilityResult{Added: false, Reason: "nil_card"}
	}
	return agentschema.AddAbilityResult{Name: card.Name, Added: true}
}

// newProgressiveFakeAbilityManager 创建测试用能力管理器。
func newProgressiveFakeAbilityManager() *progressiveFakeAbilityManager {
	return &progressiveFakeAbilityManager{cards: make(map[string]*tool.ToolCard)}
}

// Add 注册能力。
func (f *progressiveFakeAbilityManager) Add(ability cschema.Ability) agentschema.AddAbilityResult {
	card, ok := ability.(*tool.ToolCard)
	if !ok {
		return agentschema.AddAbilityResult{Name: ability.AbilityName(), Added: false}
	}
	if _, exists := f.cards[card.Name]; exists {
		return agentschema.AddAbilityResult{Name: card.Name, Added: false}
	}
	f.cards[card.Name] = card
	f.revision++
	return agentschema.AddAbilityResult{Name: card.Name, Added: true}
}

// AddMany 批量注册。
func (f *progressiveFakeAbilityManager) AddMany(abilities []cschema.Ability) []agentschema.AddAbilityResult {
	results := make([]agentschema.AddAbilityResult, 0, len(abilities))
	for _, ability := range abilities {
		results = append(results, f.Add(ability))
	}
	return results
}

// Remove 移除能力。
func (f *progressiveFakeAbilityManager) Remove(name string) cschema.Ability {
	card, ok := f.cards[name]
	if !ok {
		return nil
	}
	delete(f.cards, name)
	return card
}

// RemoveMany 批量移除。
func (f *progressiveFakeAbilityManager) RemoveMany(names []string) []cschema.Ability {
	results := make([]cschema.Ability, 0, len(names))
	for _, name := range names {
		results = append(results, f.Remove(name))
	}
	return results
}

// Get 按名查询。
func (f *progressiveFakeAbilityManager) Get(name string) cschema.Ability {
	if card, ok := f.cards[name]; ok {
		return card
	}
	return nil
}

// List 列出全部能力。
func (f *progressiveFakeAbilityManager) List() []cschema.Ability {
	abilities := make([]cschema.Ability, 0, len(f.cards))
	for _, card := range f.cards {
		abilities = append(abilities, card)
	}
	return abilities
}

// ListToolInfo 列出工具 schema。
func (f *progressiveFakeAbilityManager) ListToolInfo(_ context.Context, names []string, _ ...string) ([]cschema.ToolInfoInterface, error) {
	infos := make([]cschema.ToolInfoInterface, 0, len(f.cards))
	for _, card := range f.cards {
		infos = append(infos, card.ToolInfo())
	}
	return infos, nil
}

// Execute 执行工具调用（返回固定结果）。
func (f *progressiveFakeAbilityManager) Execute(
	_ context.Context,
	_ *agentinterfaces.AgentCallbackContext,
	_ []*llmschema.ToolCall,
	_ sessioninterfaces.SessionFacade,
	_ string, parallelToolCalls bool) []agentschema.ExecuteResult {
	f.executeCalls++
	return f.executeResults
}

// SetContextEngine 空实现。
func (f *progressiveFakeAbilityManager) SetContextEngine(_ ceinterface.ContextEngine) {}

// ReorderTools 空实现。
func (f *progressiveFakeAbilityManager) ReorderTools(_ []string) {}

// RegistryRevision 返回注册版本。
func (f *progressiveFakeAbilityManager) RegistryRevision() int { return f.revision }

// progressiveFakeSession 测试用会话门面。
type progressiveFakeSession struct {
	// id 会话 id
	id string
	// stateMap 状态表
	stateMap map[string]any
}

// newProgressiveFakeSession 创建测试会话。
func newProgressiveFakeSession(id string) *progressiveFakeSession {
	return &progressiveFakeSession{id: id, stateMap: make(map[string]any)}
}

// GetSessionID 返回会话 id。
func (f *progressiveFakeSession) GetSessionID() string { return f.id }

// UpdateState 合并写入状态。
func (f *progressiveFakeSession) UpdateState(data map[string]any) {
	for key, value := range data {
		f.stateMap[key] = value
	}
}

// GetState 读取状态。
func (f *progressiveFakeSession) GetState(key state.StateKey) (any, error) {
	return f.stateMap[key.String()], nil
}

// DumpState 导出状态。
func (f *progressiveFakeSession) DumpState() map[string]any { return f.stateMap }

// WriteStream 空实现。
func (f *progressiveFakeSession) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 空实现。
func (f *progressiveFakeSession) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 返回 nil。
func (f *progressiveFakeSession) GetEnv(_ string, _ ...any) any { return nil }

// Interact 空实现。
func (f *progressiveFakeSession) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 空实现。
func (f *progressiveFakeSession) ClearSession(_ context.Context) {}

// newRail 创建测试用 rail。
func newProgressiveRail(progressive bool) *ProgressiveToolRail {
	return NewProgressiveToolRail(&schema.DeepAgentConfig{ProgressiveToolEnabled: progressive})
}

// newDeferredCard 构造 deferred 卡片。
func newProgressiveDeferredCard(name, description string) *tool.ToolCard {
	card := &tool.ToolCard{}
	card.Name = name
	card.Description = description
	card.Exposure = tool.ToolExposureDeferred
	card.SetExposureDeclared(true)
	return card
}

// TestCollectDeferredToolDescriptions_只取deferred且排除元工具 验证目录收集规则。
func TestCollectDeferredToolDescriptions_只取deferred且排除元工具(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()

	directCard := &tool.ToolCard{}
	directCard.Name = "read_file"
	directCard.Exposure = tool.ToolExposureDirect
	manager.cards["read_file"] = directCard

	metaCard := &tool.ToolCard{}
	metaCard.Name = "tool_search"
	metaCard.Exposure = tool.ToolExposureDirect
	manager.cards["tool_search"] = metaCard

	manager.cards["xlsx_read"] = newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	rail.metaToolNames = map[string]struct{}{"tool_search": {}, "tool_call": {}}

	got := rail.collectDeferredToolDescriptions(manager)
	if len(got) != 1 || got["xlsx_read"] != "读取 Excel" {
		t.Fatalf("只应收录 deferred 且非元工具，got %v", got)
	}
}

// TestResolveProgressiveDiscovery_本地化与别名 验证 display 与 aliases 解析。
func TestResolveProgressiveDiscovery_本地化与别名(t *testing.T) {
	card := newProgressiveDeferredCard("xlsx_read", "fallback")
	card.Properties = map[string]any{
		progressiveDiscoveryProperty: map[string]any{
			"display": map[string]any{"cn": "读取表格", "en": "read spreadsheet"},
			"aliases": []any{"excel", "xlsx"},
		},
	}
	extra, display := resolveProgressiveDiscovery(card, "cn")
	if display != "读取表格" {
		t.Fatalf("中文 display 应命中，got %s", display)
	}
	for _, want := range []string{"read spreadsheet", "读取表格", "excel", "xlsx"} {
		if !strings.Contains(extra, want) {
			t.Fatalf("额外检索文本缺 %s：%s", want, extra)
		}
	}
	if _, fallback := resolveProgressiveDiscovery(card, "jp"); fallback != "" {
		t.Fatalf("未知语言不应命中本地化，got %s", fallback)
	}
}

// TestEnsureToolSearchIndex_指纹变化触发重建 验证双判据复用。
func TestEnsureToolSearchIndex_指纹变化触发重建(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	card := newProgressiveDeferredCard("t1", "旧描述")
	manager.cards["t1"] = card
	rail.toolSearchRegistry = manager

	rail.ensureToolSearchIndex()
	first := rail.toolSearchIndex
	rail.ensureToolSearchIndex()
	if rail.toolSearchIndex != first {
		t.Fatal("未变化时不应重建")
	}

	card.Description = "新描述"
	rail.ensureToolSearchIndex()
	if rail.toolSearchIndex == first {
		t.Fatal("描述变化后应重建")
	}
}

// TestSearchTools_授权写回会话 验证检索结果与授权落库。
func TestSearchTools_授权写回会话(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	manager.cards["xlsx_read"] = newProgressiveDeferredCard("xlsx_read", "读取 Excel 表格")
	rail.toolSearchRegistry = manager

	session := newProgressiveFakeSession("s1")
	results, err := rail.searchTools(context.Background(), "excel", 5, session)
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("应命中 1 个工具，got %d", len(results))
	}
	names := rail.getDiscoveredTools(session)
	if len(names) != 1 || names[0] != "xlsx_read" {
		t.Fatalf("授权名单应写入会话，got %v", names)
	}
	if rail.getDiscoveredToolFingerprints(session)["xlsx_read"] == "" {
		t.Fatal("授权指纹应写入会话")
	}
	trace, _ := rail.getTrace(session)
	if len(trace) != 1 || trace[0]["action"] != "tool_search" {
		t.Fatalf("应记录搜索轨迹，got %v", trace)
	}
}

// TestSetDiscoveredTools_合并与裁剪 验证授权表合并去重与指纹裁剪。
func TestSetDiscoveredTools_合并与裁剪(t *testing.T) {
	rail := newProgressiveRail(true)
	session := newProgressiveFakeSession("s1")
	rail.setDiscoveredTools(session, []string{"a", "a", "b"}, map[string]string{"a": "fp-a", "b": "fp-b", "c": "fp-c"})
	names := rail.getDiscoveredTools(session)
	if len(names) != 2 {
		t.Fatalf("应去重为 2 项，got %v", names)
	}
	fingerprints := rail.getDiscoveredToolFingerprints(session)
	if len(fingerprints) != 2 || fingerprints["c"] != "" {
		t.Fatalf("指纹应裁剪到名单内，got %v", fingerprints)
	}

	rail.invalidateDiscoveredTools(session, map[string]struct{}{"a": {}})
	if len(rail.getDiscoveredTools(session)) != 1 {
		t.Fatalf("失效后应只剩 1 项，got %v", rail.getDiscoveredTools(session))
	}
}

// TestSyncDeferredToolAttachment_首轮未变不发附件 验证首轮静默。
func TestSyncDeferredToolAttachment_首轮未变不发附件(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	manager.cards["a"] = newProgressiveDeferredCard("a", "工具A")
	rail.toolSearchRegistry = manager
	session := newProgressiveFakeSession("s1")
	attachments := prompts.NewManager()
	writer := attachments.BindContextForSession("s1")

	err := rail.syncDeferredToolAttachment(context.Background(), session, writer, nil, map[string]string{"a": "工具A"})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	written, _ := attachments.ListByFilter(prompts.AttachmentFilter{SessionID: "s1", Section: deferredToolAttachmentSection})
	if len(written) != 0 {
		t.Fatalf("首轮未变化不应写附件，got %d", len(written))
	}
}

// TestSyncDeferredToolAttachment_新增发delta 验证目录变化推送。
func TestSyncDeferredToolAttachment_新增发delta(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	manager.cards["a"] = newProgressiveDeferredCard("a", "工具A")
	manager.cards["b"] = newProgressiveDeferredCard("b", "工具B")
	rail.toolSearchRegistry = manager
	session := newProgressiveFakeSession("s1")
	attachments := prompts.NewManager()
	writer := attachments.BindContextForSession("s1")
	// Python: has_history_snapshot 为真才走 delta（否则强制全量快照）
	history := newSnapshotHistory("s1")

	rail.ensureInitialDeferredCatalog(session, manager)
	if err := rail.syncDeferredToolAttachment(context.Background(), session, writer, history, map[string]string{"a": "工具A"}); err != nil {
		t.Fatal(err)
	}
	if err := rail.syncDeferredToolAttachment(context.Background(), session, writer, history, map[string]string{"a": "工具A", "b": "工具B"}); err != nil {
		t.Fatal(err)
	}

	items, _ := attachments.ListByFilter(prompts.AttachmentFilter{SessionID: "s1", Section: deferredToolAttachmentSection})
	if len(items) == 0 {
		t.Fatal("应写入附件")
	}
	item := items[0]
	if item == nil {
		t.Fatal("应写入附件")
	}
	if !strings.Contains(item.Content, "新增") || !strings.Contains(item.Content, "b") {
		t.Fatalf("delta 应含新增段，got %s", item.Content)
	}
	if item.Metadata["catalog_mode"] != "delta" {
		t.Fatalf("mode 应为 delta，got %v", item.Metadata["catalog_mode"])
	}
}

// TestSyncDeferredToolAttachment_连续十次后发快照 验证快照兜底。
func TestSyncDeferredToolAttachment_连续十次后发快照(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	rail.toolSearchRegistry = manager
	session := newProgressiveFakeSession("s1")
	attachments := prompts.NewManager()
	writer := attachments.BindContextForSession("s1")

	catalog := map[string]string{"tool_0": "工具0"}
	rail.ensureInitialDeferredCatalog(session, manager)
	if err := rail.syncDeferredToolAttachment(context.Background(), session, writer, nil, catalog); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= fullSnapshotInterval; i++ {
		catalog["tool_"+strconv.Itoa(i)] = "工具"
		if err := rail.syncDeferredToolAttachment(context.Background(), session, writer, nil, catalog); err != nil {
			t.Fatal(err)
		}
	}
	items, _ := attachments.ListByFilter(prompts.AttachmentFilter{SessionID: "s1", Section: deferredToolAttachmentSection})
	if len(items) == 0 {
		t.Fatal("应写入附件")
	}
	if mode := items[0].Metadata["catalog_mode"]; mode != "snapshot" {
		t.Fatalf("连续 delta 达上限后应发快照，got %v", mode)
	}
}

// TestCallDiscoveredTool_未授权拒绝 验证校验顺序第一位。
func TestCallDiscoveredTool_未授权拒绝(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	manager.cards["xlsx_read"] = newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	rail.toolSearchRegistry = manager
	session := newProgressiveFakeSession("s1")

	result, err := rail.callDiscoveredTool(context.Background(), "xlsx_read", nil, session, nil)
	if err != nil {
		t.Fatalf("拒绝应作为结果返回: %v", err)
	}
	if result.Success() {
		t.Fatal("未授权应失败")
	}
	if !strings.Contains(result.Err().Error(), "must be returned by tool_search") {
		t.Fatalf("文案不符: %v", result.Err())
	}
}

// TestCallDiscoveredTool_指纹变化要求重搜 验证变更即失效。
func TestCallDiscoveredTool_指纹变化要求重搜(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	manager.cards["xlsx_read"] = newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	rail.toolSearchRegistry = manager
	session := newProgressiveFakeSession("s1")
	rail.setDiscoveredTools(session, []string{"xlsx_read"}, map[string]string{"xlsx_read": "stale"})

	result, err := rail.callDiscoveredTool(context.Background(), "xlsx_read", nil, session, nil)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !strings.Contains(result.Err().Error(), "has changed; search it again") {
		t.Fatalf("文案不符: %v", result.Err())
	}
	if len(rail.getDiscoveredTools(session)) != 0 {
		t.Fatal("授权应被失效")
	}
}

// TestCallDiscoveredTool_成功转发 验证嵌套执行与结果转发。
func TestCallDiscoveredTool_成功转发(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	card := newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	manager.cards["xlsx_read"] = card
	manager.executeResults = []agentschema.ExecuteResult{{
		Result:  map[string]any{"sheet": "S1"},
		ToolMsg: llmschema.NewToolMessage("call-1:target", "渲染结果"),
	}}
	rail.toolSearchRegistry = manager
	session := newProgressiveFakeSession("s1")
	rail.setDiscoveredTools(session, []string{"xlsx_read"},
		map[string]string{"xlsx_read": toolFingerprintOf(card)})

	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{
		ToolCall: llmschema.NewToolCall("call-1", "tool_call", `{}`),
		ToolName: "tool_call",
	}, session)
	result, err := rail.callDiscoveredTool(context.Background(), "xlsx_read", map[string]any{"sheet": "S1"}, session, cbc)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Success() {
		t.Fatalf("应成功，got %v", result.Err())
	}
	if manager.executeCalls != 1 {
		t.Fatalf("应嵌套执行一次，got %d", manager.executeCalls)
	}
	if result.RenderedText() != "渲染结果" {
		t.Fatalf("应转发目标渲染文本，got %s", result.RenderedText())
	}
}

// TestFilterModelCallTools_只留meta与direct 验证顶层过滤。
func TestFilterModelCallTools_只留meta与direct(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	directCard := &tool.ToolCard{}
	directCard.Name = "read_file"
	directCard.Exposure = tool.ToolExposureDirect
	manager.cards["read_file"] = directCard
	manager.cards["xlsx_read"] = newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	rail.metaToolNames = map[string]struct{}{"tool_search": {}, "tool_call": {}}

	inputs := &agentinterfaces.ModelCallInputs{Tools: []cschema.ToolInfoInterface{
		directCard.ToolInfo(),
		manager.cards["xlsx_read"].ToolInfo(),
	}}
	if err := rail.filterModelCallTools(inputs, manager); err != nil {
		t.Fatal(err)
	}
	if len(inputs.Tools) != 1 || inputs.Tools[0].GetName() != "read_file" {
		t.Fatalf("只应保留 direct，got %v", inputs.Tools)
	}
}

// TestInit_元工具显式direct 验证元工具注册与曝光锁定。
func TestInit_元工具显式direct(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	agent := &progressiveFakeInitAgent{abilityManager: manager}
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	for _, name := range []string{"tool_search", "tool_call"} {
		card, ok := manager.cards[name]
		if !ok {
			t.Fatalf("应注册元工具 %s", name)
		}
		if card.EffectiveExposure() != tool.ToolExposureDirect {
			t.Fatalf("元工具须 direct，got %s", card.EffectiveExposure())
		}
		if declared, ok := card.GetExposureDeclared(); !ok || !declared {
			t.Fatalf("元工具应锁定曝光声明")
		}
	}
	if len(rail.metaToolNames) != 2 {
		t.Fatalf("元工具名集合应为 2，got %v", rail.metaToolNames)
	}
}

// TestUninit_不移除他人注册 验证 ownership 判定。
func TestUninit_不移除他人注册(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	agent := &progressiveFakeInitAgent{abilityManager: manager}
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatal(err)
	}
	// 模拟他人接管同名工具
	replacement := &tool.ToolCard{}
	replacement.Name = "tool_search"
	replacement.ID = "other-owner"
	manager.cards["tool_search"] = replacement

	if err := rail.Uninit(agent); err != nil {
		t.Fatal(err)
	}
	if _, exists := manager.cards["tool_search"]; !exists {
		t.Fatal("他人接管的同名工具不得被移除")
	}
	if _, exists := manager.cards["tool_call"]; exists {
		t.Fatal("自有元工具应被移除")
	}
}

// getTrace 读取会话里的发现轨迹（测试助手）。
func (r *ProgressiveToolRail) getTrace(sess sessioninterfaces.SessionFacade) ([]map[string]any, error) {
	raw, err := sess.GetState(state.StringKey(discoveryTraceKey))
	if err != nil {
		return nil, err
	}
	trace, _ := raw.([]map[string]any)
	return trace, nil
}

// toolFingerprintOf 计算卡片指纹（测试助手）。
func toolFingerprintOf(card *tool.ToolCard) string {
	return tool_discovery.ToolFingerprint(card.ToolInfo())
}

// progressiveFakeInitAgent 测试用 BaseAgent（提供 AbilityManager/Card/Builder/附件）。
type progressiveFakeInitAgent struct {
	agentinterfaces.BaseAgent
	// abilityManager 能力管理器
	abilityManager agentinterfaces.AbilityManagerInterface
	// builder 系统提示词构建器
	builder *saprompt.SystemPromptBuilder
	// attachments 动态提示词管理器
	attachments *prompts.Manager
}

// SystemPromptBuilder 返回构建器。
func (f *progressiveFakeInitAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	if f.builder == nil {
		return nil
	}
	return f.builder
}

// PromptAttachments 返回动态提示词管理器。
func (f *progressiveFakeInitAgent) PromptAttachments() *prompts.Manager {
	return f.attachments
}

// AbilityManager 返回能力管理器。
func (f *progressiveFakeInitAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.abilityManager
}

// Card 返回 Agent 卡片。
func (f *progressiveFakeInitAgent) Card() *agentschema.AgentCard { return nil }

// TestLifecycleHooks_注入规则与过滤 验证 BeforeInvoke/BeforeModelCall 的完整链路。
func TestLifecycleHooks_注入规则与过滤(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	deferredCard := newProgressiveDeferredCard("xlsx_read", "读取 Excel 表格")
	directCard := &tool.ToolCard{}
	directCard.Name = "read_file"
	directCard.Exposure = tool.ToolExposureDirect
	manager.cards["xlsx_read"] = deferredCard
	manager.cards["read_file"] = directCard

	agent := &progressiveFakeInitAgent{
		abilityManager: manager,
		builder:        saprompt.NewSystemPromptBuilder(),
		attachments:    prompts.NewManager(),
	}
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	// 元工具占用了 tool_search/tool_call，deferred 工具仍需存在
	manager.cards["xlsx_read"] = deferredCard

	session := newProgressiveFakeSession("s1")
	inputs := &agentinterfaces.ModelCallInputs{Tools: []cschema.ToolInfoInterface{
		deferredCard.ToolInfo(),
		directCard.ToolInfo(),
	}}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, inputs, session)

	if err := rail.BeforeInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeInvoke 失败: %v", err)
	}
	if len(rail.cachedAllToolInfos) == 0 {
		t.Fatal("BeforeInvoke 应缓存全量工具清单")
	}
	if err := rail.BeforeModelCall(context.Background(), cbc); err != nil {
		t.Fatalf("BeforeModelCall 失败: %v", err)
	}
	if !agent.builder.HasSection(sections.SectionProgressiveToolRules) {
		t.Fatal("应注入渐进式工具规则节")
	}
	if len(inputs.Tools) != 1 || inputs.Tools[0].GetName() != "read_file" {
		t.Fatalf("顶层工具应只留 direct，got %v", inputs.Tools)
	}
	if rail.attachmentManager == nil {
		t.Fatal("应能从 agent 取得附件管理器")
	}
	if writer := rail.attachmentManager.BindContext(cbc); writer.SessionID() == "" {
		t.Fatal("回调上下文应能解析出会话")
	}
	if writer := rail.attachmentManager.BindContext(nil); writer.SessionID() != "" {
		t.Fatal("nil 回调上下文不应解析出会话")
	}
}

// TestBeforeToolCall_四分支 验证门卫的四个分支。
func TestBeforeToolCall_四分支(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	directCard := &tool.ToolCard{}
	directCard.Name = "read_file"
	directCard.Exposure = tool.ToolExposureDirect
	manager.cards["read_file"] = directCard
	manager.cards["xlsx_read"] = newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	rail.metaToolNames = map[string]struct{}{"tool_search": {}, "tool_call": {}}
	rail.toolSearchRegistry = manager

	session := newProgressiveFakeSession("s1")
	agent := &progressiveFakeInitAgent{abilityManager: manager}

	// ① direct 放行
	if err := rail.BeforeToolCall(context.Background(), toolCallCtx(agent, session, "read_file")); err != nil {
		t.Fatalf("direct 应放行: %v", err)
	}
	// ② 元工具放行
	if err := rail.BeforeToolCall(context.Background(), toolCallCtx(agent, session, "tool_search")); err != nil {
		t.Fatalf("元工具应放行: %v", err)
	}
	// ③ deferred 无嵌套标记 → 拒绝
	rejected := toolCallCtx(agent, session, "xlsx_read")
	if err := rail.BeforeToolCall(context.Background(), rejected); err != nil {
		t.Fatalf("拒绝不应返回 error: %v", err)
	}
	if skipped, _ := rejected.GetExtra("_skip_tool"); skipped != true {
		t.Fatal("deferred 直调应设置 _skip_tool")
	}
	// ④ 有嵌套标记 + 已授权 → 放行
	rail.setDiscoveredTools(session, []string{"xlsx_read"}, nil)
	nested := toolCallCtx(agent, session, "xlsx_read")
	nested.SetExtra(nestedToolCallKey, true)
	if err := rail.BeforeToolCall(context.Background(), nested); err != nil {
		t.Fatalf("嵌套已授权应放行: %v", err)
	}
	// ⑤ 未知工具 → 拒绝
	unknown := toolCallCtx(agent, session, "no_such_tool")
	if err := rail.BeforeToolCall(context.Background(), unknown); err != nil {
		t.Fatalf("拒绝不应返回 error: %v", err)
	}
	if skipped, _ := unknown.GetExtra("_skip_tool"); skipped != true {
		t.Fatal("未知工具应设置 _skip_tool")
	}
	if toolNameOf(nil) != "" {
		t.Fatal("nil 回调上下文应返回空工具名")
	}
	if !rail.isCachedTool("") == false {
		_ = rail.isCachedTool("read_file")
	}
}

// TestFinalizeStartupAsync_建首份索引 验证启动收尾。
func TestFinalizeStartupAsync_建首份索引(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	manager.cards["xlsx_read"] = newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	agent := &progressiveFakeInitAgent{abilityManager: manager}

	if err := rail.FinalizeStartupAsync(context.Background(), agent); err != nil {
		t.Fatalf("收尾失败: %v", err)
	}
	if rail.toolSearchIndex == nil {
		t.Fatal("启动收尾应建立 BM25 索引")
	}
	if rail.toolSearchIndex.DocumentCount() != 1 {
		t.Fatalf("索引应含 1 个 deferred 工具，got %d", rail.toolSearchIndex.DocumentCount())
	}
}

// TestGetCallbacks_注册三事件 验证回调映射。
func TestGetCallbacks_注册三事件(t *testing.T) {
	rail := newProgressiveRail(true)
	callbacks := rail.GetCallbacks()
	for _, event := range []agentinterfaces.AgentCallbackEvent{
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackBeforeModelCall,
		agentinterfaces.CallbackBeforeToolCall,
	} {
		if _, ok := callbacks[event]; !ok {
			t.Fatalf("缺少回调 %s", event)
		}
	}
}

// TestDispatchToolCall_上下文校验 验证 tool_call 分发入口。
func TestDispatchToolCall_上下文校验(t *testing.T) {
	rail := newProgressiveRail(true)
	if _, err := rail.dispatchToolCall(context.Background(), "x", nil, nil); err == nil {
		t.Fatal("缺少回调上下文应报错")
	}
}

// TestCallDiscoveredTool_非deferred工具拒绝 验证曝光校验分支。
func TestCallDiscoveredTool_非deferred工具拒绝(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	directCard := &tool.ToolCard{}
	directCard.Name = "read_file"
	directCard.Exposure = tool.ToolExposureDirect
	manager.cards["read_file"] = directCard
	rail.toolSearchRegistry = manager

	session := newProgressiveFakeSession("s1")
	rail.setDiscoveredTools(session, []string{"read_file"}, nil)
	agent := &progressiveFakeInitAgent{abilityManager: manager}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{}, session)

	result, err := rail.callDiscoveredTool(context.Background(), "read_file", nil, session, cbc)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !strings.Contains(result.Err().Error(), "not a deferred search result") {
		t.Fatalf("文案不符: %v", result.Err())
	}
}

// TestCallDiscoveredTool_无执行结果 验证 Execute 空结果分支。
func TestCallDiscoveredTool_无执行结果(t *testing.T) {
	rail := newProgressiveRail(true)
	manager := newProgressiveFakeAbilityManager()
	card := newProgressiveDeferredCard("xlsx_read", "读取 Excel")
	manager.cards["xlsx_read"] = card
	rail.toolSearchRegistry = manager

	session := newProgressiveFakeSession("s1")
	rail.setDiscoveredTools(session, []string{"xlsx_read"},
		map[string]string{"xlsx_read": toolFingerprintOf(card)})
	agent := &progressiveFakeInitAgent{abilityManager: manager}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{}, session)

	result, err := rail.callDiscoveredTool(context.Background(), "xlsx_read", nil, session, cbc)
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if !strings.Contains(result.Err().Error(), "returned no execution result") {
		t.Fatalf("文案不符: %v", result.Err())
	}
}

// toolCallCtx 构造工具调用回调上下文（测试助手）。
func toolCallCtx(agent agentinterfaces.BaseAgent, session sessioninterfaces.SessionFacade, toolName string) *agentinterfaces.AgentCallbackContext {
	cbc := agentinterfaces.NewAgentCallbackContext(agent, &agentinterfaces.ToolCallInputs{
		ToolCall: llmschema.NewToolCall("call-"+toolName, toolName, `{}`),
		ToolName: toolName,
	}, session)
	return cbc
}

// TeardownTools 测试替身空实现（对齐 AbilityManagerInterface 扩张）。
func (f *progressiveFakeAbilityManager) TeardownTools() {}

// GetCacheIdentity 返回零值缓存身份（测试替身，不参与 KV 亲和）。
// Python: SessionFacade.get_cache_identity 的测试替身实现。
func (f *progressiveFakeSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 返回空父会话（测试替身）。
func (f *progressiveFakeSession) GetParentSessionID() string { return "" }

// snapshotHistory 声明「已有历史快照」的 ModelContext 桩。
// 只用于让 HasHistorySnapshot 返回 true（强制走 delta 而非全量快照）。
type snapshotHistory struct {
	// sessionID 会话标识
	sessionID string
	// messages 上下文消息
	messages []llmschema.BaseMessage
}

// newSnapshotHistory 构造带 snapshot 标记的上下文桩。
func newSnapshotHistory(sessionID string) *snapshotHistory {
	msg := &llmschema.UserMessage{}
	msg.SetMetadata(map[string]any{
		prompts.MetadataKeyAttachmentHistory: true,
		prompts.MetadataKeySessionID:         sessionID,
		prompts.MetadataKeyMode:              prompts.ModeSnapshot,
	})
	return &snapshotHistory{sessionID: sessionID, messages: []llmschema.BaseMessage{msg}}
}

// Len 返回消息数量。
func (h *snapshotHistory) Len() int { return len(h.messages) }

// GetMessages 返回消息列表。
func (h *snapshotHistory) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return h.messages, nil
}

// SetMessages 替换消息列表。
func (h *snapshotHistory) SetMessages(messages []llmschema.BaseMessage, _ bool) {
	h.messages = messages
}

// PopMessages 弹出消息。
func (h *snapshotHistory) PopMessages(_ int, _ bool) []llmschema.BaseMessage {
	popped := h.messages
	h.messages = nil
	return popped
}

// ClearMessages 清空消息。
func (h *snapshotHistory) ClearMessages(_ context.Context, _ bool, _ ...ceinterface.Option) error {
	h.messages = nil
	return nil
}

// AddMessages 追加消息。
func (h *snapshotHistory) AddMessages(_ context.Context, messages []llmschema.BaseMessage, _ ...ceinterface.Option) ([]llmschema.BaseMessage, error) {
	h.messages = append(h.messages, messages...)
	return h.messages, nil
}

// GetContextWindow 空实现。
func (h *snapshotHistory) GetContextWindow(_ context.Context, _ []llmschema.BaseMessage, _ []cschema.ToolInfoInterface, _ int, _ int, _ ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return nil, nil
}

// Statistic 空实现。
func (h *snapshotHistory) Statistic() *ceinterface.ContextStats { return nil }

// SessionID 返回会话标识。
func (h *snapshotHistory) SessionID() string { return h.sessionID }

// ContextID 返回上下文标识。
func (h *snapshotHistory) ContextID() string { return h.sessionID }

// TokenCounter 空实现。
func (h *snapshotHistory) TokenCounter() tokeniface.TokenCounter { return nil }

// ReloaderTool 空实现。
func (h *snapshotHistory) ReloaderTool() tool.Tool { return nil }

// WorkspaceDir 空实现。
func (h *snapshotHistory) WorkspaceDir() string { return "" }

// SetSessionRef 空实现。
func (h *snapshotHistory) SetSessionRef(_ sessioninterfaces.SessionFacade) {}

// GetSessionRef 空实现。
func (h *snapshotHistory) GetSessionRef() sessioninterfaces.SessionFacade { return nil }

// OffloadMessages 空实现。
func (h *snapshotHistory) OffloadMessages(_ string, _ []llmschema.BaseMessage) {}

// CompressContext 空实现。
func (h *snapshotHistory) CompressContext(_ context.Context, _ ...ceinterface.CompressContextOption) (*ceinterface.CompressContextResult, error) {
	return nil, nil
}

// SaveState 空实现。
func (h *snapshotHistory) SaveState() map[string]any { return nil }

// LoadState 空实现。
func (h *snapshotHistory) LoadState(_ map[string]any) {}

// CreateContext 空实现。
func (h *snapshotHistory) CreateContext(_ context.Context, _ string, _ sessioninterfaces.SessionFacade, _ ...ceinterface.CreateContextOption) (ceinterface.ModelContext, error) {
	return h, nil
}

// GetContext 空实现。
func (h *snapshotHistory) GetContext(_ string, _ string) ceinterface.ModelContext { return nil }

// UpdateModelContext 空实现。
func (h *snapshotHistory) UpdateModelContext(_ string, _ int) {}

// RecoverFromModelContextOverflow 空实现。
func (h *snapshotHistory) RecoverFromModelContextOverflow(_ context.Context, _ sessioninterfaces.SessionFacade, _ string, _ bool, _ int) bool {
	return false
}

// 编译期验证。
var _ ceinterface.ModelContext = (*snapshotHistory)(nil)

// TestSyncDeferredToolAttachment_DeltaCount零值不覆盖 验证附件 delta_count=0 不清掉累积计数。
func TestSyncDeferredToolAttachment_DeltaCount零值不覆盖(t *testing.T) {
	rail := newProgressiveRail(true)
	abilityManager := newProgressiveFakeAbilityManager()
	abilityManager.cards["a"] = newProgressiveDeferredCard("a", "工具A")
	rail.toolSearchRegistry = abilityManager
	attachments := prompts.NewManager()
	_, err := attachments.AddSection("s1", deferredToolAttachmentSection, "prev", prompts.AttachmentTool,
		deferredToolAttachmentSource, prompts.WithAttachmentMetadata(map[string]any{
			deferredToolCatalogMetadataKey:             map[string]string{"a": "工具A"},
			deferredToolCatalogFingerprintsMetadataKey: map[string]string{"a": "f1"},
			"catalog_version":                          3,
			"delta_count":                              0,
			"catalog_language":                         "cn",
		}))
	if err != nil {
		t.Fatal(err)
	}
	session := newProgressiveFakeSession("s1")
	session.stateMap = map[string]any{
		deferredToolCatalogStateKey: map[string]any{
			"initialized":  true,
			"version":      3,
			"delta_count":  7,
			"language":     "cn",
			"tools":        map[string]any{"a": "工具A"},
			"fingerprints": map[string]any{"a": "f1"},
		},
	}
	// 沿用旧基线但新增一个工具，让本轮真的发 delta 并走 deltaCount+1
	if err := rail.syncDeferredToolAttachment(context.Background(), session,
		attachments.BindContextForSession("s1"), newSnapshotHistory("s1"),
		map[string]string{"a": "工具A", "b": "工具B"}); err != nil {
		t.Fatal(err)
	}

	state, _ := session.stateMap[deferredToolCatalogStateKey].(map[string]any)
	if state == nil {
		t.Fatal("应写回目录状态")
	}
	if got := state["delta_count"]; got != 8 {
		t.Fatalf("delta_count 应从 session 的 7 递增到 8（附件里的 0 不得覆盖），got %v", got)
	}
}

// TestSyncDeferredToolAttachment_指纹恢复参与基线 验证 description 不变但指纹变化判 updated。
func TestSyncDeferredToolAttachment_指纹恢复参与基线(t *testing.T) {
	rail := newProgressiveRail(true)
	abilityManager := newProgressiveFakeAbilityManager()
	abilityManager.cards["a"] = newProgressiveDeferredCard("a", "工具A")
	rail.toolSearchRegistry = abilityManager
	attachments := prompts.NewManager()
	_, err := attachments.AddSection("s2", deferredToolAttachmentSection, "prev", prompts.AttachmentTool,
		deferredToolAttachmentSource, prompts.WithAttachmentMetadata(map[string]any{
			deferredToolCatalogMetadataKey:             map[string]string{"a": "工具A"},
			deferredToolCatalogFingerprintsMetadataKey: map[string]string{"a": "old-fingerprint"},
			"catalog_version":                          2,
			"catalog_language":                         "cn",
		}))
	if err != nil {
		t.Fatal(err)
	}
	session := newProgressiveFakeSession("s2")
	session.stateMap = map[string]any{
		deferredToolCatalogStateKey: map[string]any{
			"initialized": true,
			"version":     2,
			"language":    "cn",
		},
	}

	if err := rail.syncDeferredToolAttachment(context.Background(), session,
		attachments.BindContextForSession("s2"), newSnapshotHistory("s2"), map[string]string{"a": "工具A"}); err != nil {
		t.Fatal(err)
	}

	items, _ := attachments.ListByFilter(prompts.AttachmentFilter{SessionID: "s2", Section: deferredToolAttachmentSection})
	if len(items) != 1 {
		t.Fatalf("应只保留一条附件，got %d", len(items))
	}
	if mode := items[0].Metadata["catalog_mode"]; mode != "delta" {
		t.Fatalf("指纹变化应判为 delta，got %v", mode)
	}
}

// TestSyncDeferredToolAttachment_初始指纹播种 验证无状态时写入 initial_fingerprints。
func TestSyncDeferredToolAttachment_初始指纹播种(t *testing.T) {
	rail := newProgressiveRail(true)
	abilityManager := newProgressiveFakeAbilityManager()
	abilityManager.cards["a"] = newProgressiveDeferredCard("a", "工具A")
	abilityManager.cards["b"] = newProgressiveDeferredCard("b", "工具B")
	rail.toolSearchRegistry = abilityManager
	attachments := prompts.NewManager()
	session := newProgressiveFakeSession("s3")

	rail.ensureInitialDeferredCatalog(session, abilityManager)
	// 首轮注册新增 b，触发一次写入以落状态
	if err := rail.syncDeferredToolAttachment(context.Background(), session,
		attachments.BindContextForSession("s3"), newSnapshotHistory("s3"),
		map[string]string{"a": "工具A", "b": "工具B"}); err != nil {
		t.Fatal(err)
	}

	state, ok := session.stateMap[deferredToolCatalogStateKey].(map[string]any)
	if !ok {
		t.Fatal("应写回目录状态")
	}
	if _, ok := state[deferredToolCatalogInitialFingerprintsStateKey]; !ok {
		t.Fatal("应写入初始指纹键")
	}
	if _, ok := state[deferredToolCatalogFingerprintsStateKey]; !ok {
		t.Fatal("应写入当前指纹键")
	}
}
