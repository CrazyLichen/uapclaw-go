package rails

import (
	"context"
	"strconv"
	"strings"
	"testing"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/tool_discovery"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
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
	_ string,
) []agentschema.ExecuteResult {
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

// progressiveFakeAttachmentManager 测试用附件管理器。
type progressiveFakeAttachmentManager struct {
	// sections 已写入附件（按 section 名覆盖）
	sections map[string]*prompts.Attachment
	// snapshot 是否已有历史快照
	snapshot bool
}

// newProgressiveFakeAttachmentManager 创建测试附件管理器。
func newProgressiveFakeAttachmentManager() *progressiveFakeAttachmentManager {
	return &progressiveFakeAttachmentManager{sections: make(map[string]*prompts.Attachment)}
}

// AddSection 写入附件。
func (f *progressiveFakeAttachmentManager) AddSection(
	sessionID, section, content string,
	kind prompts.AttachmentKind,
	source string,
	opts ...prompts.AttachmentOption,
) (*prompts.Attachment, error) {
	item := &prompts.Attachment{
		SessionID: sessionID,
		Section:   section,
		Content:   content,
		Kind:      kind,
		Source:    source,
		Metadata:  make(map[string]any),
	}
	for _, opt := range opts {
		opt(item)
	}
	f.sections[section] = item
	return item, nil
}

// Find 查询附件。
func (f *progressiveFakeAttachmentManager) Find(_, section, _ string) *prompts.Attachment {
	return f.sections[section]
}

// HasHistorySnapshot 返回是否有历史快照。
func (f *progressiveFakeAttachmentManager) HasHistorySnapshot(_ string) bool { return f.snapshot }

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
	attachments := newProgressiveFakeAttachmentManager()

	err := rail.syncDeferredToolAttachment(context.Background(), session, attachments, map[string]string{"a": "工具A"})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(attachments.sections) != 0 {
		t.Fatalf("首轮未变化不应写附件，got %d", len(attachments.sections))
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
	attachments := newProgressiveFakeAttachmentManager()
	attachments.snapshot = true

	rail.ensureInitialDeferredCatalog(session, manager)
	if err := rail.syncDeferredToolAttachment(context.Background(), session, attachments, map[string]string{"a": "工具A"}); err != nil {
		t.Fatal(err)
	}
	if err := rail.syncDeferredToolAttachment(context.Background(), session, attachments,
		map[string]string{"a": "工具A", "b": "工具B"}); err != nil {
		t.Fatal(err)
	}

	item := attachments.sections[deferredToolAttachmentSection]
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
	attachments := newProgressiveFakeAttachmentManager()
	attachments.snapshot = true

	catalog := map[string]string{"tool_0": "工具0"}
	rail.ensureInitialDeferredCatalog(session, manager)
	if err := rail.syncDeferredToolAttachment(context.Background(), session, attachments, catalog); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= fullSnapshotInterval; i++ {
		catalog["tool_"+strconv.Itoa(i)] = "工具"
		if err := rail.syncDeferredToolAttachment(context.Background(), session, attachments, catalog); err != nil {
			t.Fatal(err)
		}
	}
	if mode := attachments.sections[deferredToolAttachmentSection].Metadata["catalog_mode"]; mode != "snapshot" {
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

// progressiveFakeInitAgent 测试用 BaseAgent（只提供 AbilityManager/Card）。
type progressiveFakeInitAgent struct {
	agentinterfaces.BaseAgent
	// abilityManager 能力管理器
	abilityManager agentinterfaces.AbilityManagerInterface
}

// AbilityManager 返回能力管理器。
func (f *progressiveFakeInitAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.abilityManager
}

// Card 返回 Agent 卡片。
func (f *progressiveFakeInitAgent) Card() *agentschema.AgentCard { return nil }
