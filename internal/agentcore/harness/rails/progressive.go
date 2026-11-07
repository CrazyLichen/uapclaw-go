package rails

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/tool_discovery"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// deferredToolDocument BM25 索引的文档单元：可调用 schema + 仅检索用文本。
//
// Python: _DeferredToolDocument
type deferredToolDocument struct {
	// info 模型可见工具 schema
	info cschema.ToolInfoInterface
	// extraSearchText 仅参与检索的额外文本（本地化 display + aliases）
	extraSearchText string
	// displayDescription 目录摘要使用的本地化描述
	displayDescription string
}

// ProgressiveToolRail 渐进式工具发现与延迟工具目录 Rail。
//
// 大工具集场景下，deferred 工具不进顶层 tools，只把目录摘要写进系统提示词；
// 模型用 tool_search 发现（返回完整 schema），用固定 tool_call 执行。
//
// Python: openjiuwen/harness/rails/progressive_tool_rail.py (ProgressiveToolRail)
type ProgressiveToolRail struct {
	DeepAgentRail
	// mu 保护索引替换、缓存与卡片映射
	mu sync.Mutex
	// config DeepAgent 运行时配置
	config *schema.DeepAgentConfig
	// metaToolNames 元工具名称集合（tool_search/tool_call）
	metaToolNames map[string]struct{}
	// ownedToolCards 本 Rail 注册且仍持有的卡片（name → card）
	ownedToolCards map[string]*tool.ToolCard
	// cachedAllToolInfos 全量工具清单缓存
	cachedAllToolInfos []cschema.ToolInfoInterface
	// toolSearchIndex BM25 索引（不可变实例，变化时整体替换）
	toolSearchIndex *tool_discovery.BM25ToolIndex
	// toolSearchIndexRevision 建索引时的注册版本
	toolSearchIndexRevision int
	// toolSearchRegistry 注册表引用（rail 自持，供索引与目录读取）
	toolSearchRegistry agentinterfaces.AbilityManagerInterface
}

// promptAttachmentProvider 提供会话级动态提示词管理器。
//
// 由 harness.DeepAgent 实现（PromptAttachments）。
type promptAttachmentProvider interface {
	// PromptAttachments 返回动态提示词管理器
	PromptAttachments() *prompts.Manager
}

// attachmentManager 目录附件所需的最小能力。
//
// Python: PromptAttachmentManager（add_section / list_by_filter / has_history_snapshot）
type attachmentManager interface {
	// AddSection 写入或覆盖附件
	AddSection(sessionID, section, content string, kind prompts.AttachmentKind, source string, opts ...prompts.AttachmentOption) (*prompts.Attachment, error)
	// Find 查询指定 section/source 的附件（无则 nil）
	Find(sessionID, section, source string) *prompts.Attachment
	// HasHistorySnapshot 判断会话历史是否已有完整快照
	HasHistorySnapshot(sessionID string) bool
}

// managerAdapter 把 prompts.Manager 适配为 rail 需要的附件能力。
type managerAdapter struct {
	// manager 附件管理器
	manager *prompts.Manager
	// history 会话历史上下文（用于快照判定）
	history prompts.HistoryContext
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// progressiveToolRailPriority Rail 优先级
	progressiveToolRailPriority = 90

	// discoveredToolsKey 会话状态键：已授权工具名
	discoveredToolsKey = "__progressive_discovered_tool_names__"
	// discoveredToolFingerprintsKey 会话状态键：授权时的 schema 指纹
	discoveredToolFingerprintsKey = "__progressive_discovered_tool_fingerprints__"
	// discoveryTraceKey 会话状态键：发现轨迹
	discoveryTraceKey = "__progressive_tool_discovery_trace__"
	// deferredToolCatalogStateKey 会话状态键：目录快照与版本
	deferredToolCatalogStateKey = "__progressive_deferred_tool_catalog__"

	// deferredToolAttachmentSection 目录附件的 section 名
	deferredToolAttachmentSection = "progressive_deferred_tools"
	// deferredToolAttachmentSource 目录附件的来源标识
	deferredToolAttachmentSource = "progressive_tool_rail"
	// deferredToolCatalogMetadataKey 附件 metadata：目录内容
	deferredToolCatalogMetadataKey = "catalog_tools"
	// deferredToolCatalogFingerprintsMetadataKey 附件 metadata：目录指纹
	deferredToolCatalogFingerprintsMetadataKey = "catalog_fingerprints"
	// deferredToolRegistryRevisionMetadataKey 附件 metadata：注册版本
	deferredToolRegistryRevisionMetadataKey = "registry_revision"
	// fullSnapshotInterval 连续 delta 上限，达到后改发全量快照
	fullSnapshotInterval = 10
	// nestedToolCallKey 嵌套调用标记（写在回调上下文 Extra 上）
	nestedToolCallKey = "__progressive_nested_tool_call__"
	// progressiveDiscoveryProperty 卡片扩展属性键：发现元数据
	progressiveDiscoveryProperty = "progressive_discovery"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// _ 编译时验证 ProgressiveToolRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ProgressiveToolRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProgressiveToolRail 创建 ProgressiveToolRail 实例。
//
// Python: ProgressiveToolRail.__init__(config)
func NewProgressiveToolRail(config *schema.DeepAgentConfig) *ProgressiveToolRail {
	rail := &ProgressiveToolRail{
		DeepAgentRail:  *NewDeepAgentRail(),
		config:         config,
		metaToolNames:  make(map[string]struct{}),
		ownedToolCards: make(map[string]*tool.ToolCard),
	}
	rail.WithPriority(progressiveToolRailPriority)
	return rail
}

// Init 注册两个元工具（显式 direct），保存注册表引用，不建索引。
//
// Python: ProgressiveToolRail.init
func (r *ProgressiveToolRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	language := r.catalogLanguage()
	agentID := ""
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}

	searchTool := tool_discovery.NewToolSearchTool(r.searchTools, language, agentID, r.toolSearchLimit())
	callTool := tool_discovery.NewToolCallTool(language, agentID)
	callTool.SetDispatch(r.dispatchToolCall)

	metaNames := map[string]struct{}{}
	owned := map[string]*tool.ToolCard{}

	abilityManager := agent.AbilityManager()
	r.mu.Lock()
	r.toolSearchRegistry = abilityManager
	r.mu.Unlock()

	for _, card := range []*tool.ToolCard{searchTool.Card(), callTool.Card()} {
		// 元工具始终直接可调用，防止被渐进策略误藏
		card.Exposure = tool.ToolExposureDirect
		card.SetExposureDeclared(true)
		metaNames[card.Name] = struct{}{}
		if abilityManager == nil {
			continue
		}
		if result := abilityManager.Add(card); result.Added {
			owned[card.Name] = card
		}
	}

	r.mu.Lock()
	r.metaToolNames = metaNames
	r.ownedToolCards = owned
	r.mu.Unlock()

	r.registerToolResources(searchTool, callTool)
	return nil
}

// FinalizeStartupAsync 先物化惰性 schema，再建首份 BM25 索引。
//
// 必须等所有 startup rail 注册完工具后调用。
//
// Python: ProgressiveToolRail.finalize_startup_async
func (r *ProgressiveToolRail) FinalizeStartupAsync(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	abilityManager := agent.AbilityManager()
	if abilityManager == nil {
		return nil
	}
	r.mu.Lock()
	r.toolSearchRegistry = abilityManager
	r.mu.Unlock()

	if _, err := abilityManager.ListToolInfo(ctx, nil); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "progressive_finalize_startup").
			Err(err).
			Msg("failed to materialize startup tool schemas")
	}
	r.rebuildToolSearchIndex(r.collectDeferredToolDocuments(abilityManager))
	return nil
}

// Uninit 按 ownership 移除元工具并清空缓存。
//
// Python: ProgressiveToolRail.uninit
func (r *ProgressiveToolRail) Uninit(agent agentinterfaces.BaseAgent) error {
	abilityManager := agent.AbilityManager()
	if abilityManager != nil {
		r.mu.Lock()
		owned := make(map[string]*tool.ToolCard, len(r.ownedToolCards))
		for name, card := range r.ownedToolCards {
			owned[name] = card
		}
		r.mu.Unlock()
		for name, card := range owned {
			current := abilityManager.Get(name)
			// 名称已被他人重新注册时不得卸载对方的工具
			if current != nil && current.AbilityID() != card.ID {
				continue
			}
			abilityManager.Remove(name)
		}
	}

	r.mu.Lock()
	r.ownedToolCards = nil
	r.metaToolNames = nil
	r.cachedAllToolInfos = nil
	r.toolSearchIndex = nil
	r.toolSearchIndexRevision = 0
	r.toolSearchRegistry = nil
	r.mu.Unlock()
	return nil
}

// BeforeInvoke 缓存全量工具清单并冻结会话初始目录。
//
// Python: before_invoke
func (r *ProgressiveToolRail) BeforeInvoke(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	abilityManager := r.abilityManagerFrom(cbc)
	if abilityManager == nil {
		return nil
	}
	infos, err := abilityManager.ListToolInfo(context.Background(), nil)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "progressive_before_invoke").
			Err(err).
			Msg("failed to list tool infos")
	}
	r.mu.Lock()
	r.cachedAllToolInfos = infos
	r.mu.Unlock()
	r.ensureInitialDeferredCatalog(cbc.Session(), abilityManager)
	return nil
}

// BeforeModelCall 注入规则节、同步目录附件、过滤顶层工具。
//
// Python: before_model_call
func (r *ProgressiveToolRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	abilityManager := r.abilityManagerFrom(cbc)
	if abilityManager == nil {
		return nil
	}
	builder := cbc.Agent().SystemPromptBuilder()
	if builder == nil {
		return errors.New("ProgressiveToolRail requires agent.system_prompt_builder")
	}
	initial := r.ensureInitialDeferredCatalog(cbc.Session(), abilityManager)
	builder.AddSection(sections.BuildProgressiveToolRulesSection(initial))

	if err := r.syncDeferredToolAttachment(
		ctx,
		cbc.Session(),
		r.attachments(cbc),
		r.collectDeferredToolDescriptions(abilityManager),
	); err != nil {
		return err
	}

	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok {
		return nil
	}
	return r.filterModelCallTools(inputs, abilityManager)
}

// BeforeToolCall 只放行 direct 与「嵌套标记 + 已授权」的调用。
//
// Python: before_tool_call
func (r *ProgressiveToolRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	toolName := toolNameOf(cbc)
	if toolName == "" || r.isMetaTool(toolName) {
		return nil
	}

	session := cbc.Session()
	nested, _ := cbc.GetExtra(nestedToolCallKey)
	card := r.lookupCard(cbc, toolName)
	if card == nil {
		if nested == true && containsString(r.getDiscoveredTools(session), toolName) && r.isCachedTool(toolName) {
			return nil
		}
		rejectToolCall(cbc, "Unknown tool '"+toolName+"'.")
		return nil
	}
	if card.EffectiveExposure() == tool.ToolExposureDirect {
		return nil
	}
	if nested == true && containsString(r.getDiscoveredTools(session), toolName) {
		return nil
	}
	rejectToolCall(cbc, "Deferred tool '"+toolName+"' must be found with tool_search before it can be called.")
	return nil
}

// GetCallbacks 注册 BeforeInvoke / BeforeModelCall / BeforeToolCall。
//
// Python: ProgressiveToolRail 隐式覆盖对应钩子
func (r *ProgressiveToolRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectDeferredToolDocuments 构造 deferred 目录文档（含仅检索用的额外文本）。
//
// Python: _list_registered_deferred_tool_documents
func (r *ProgressiveToolRail) collectDeferredToolDocuments(abilityManager agentinterfaces.AbilityManagerInterface) []deferredToolDocument {
	if abilityManager == nil {
		return nil
	}
	language := r.catalogLanguage()
	documents := make([]deferredToolDocument, 0)
	for _, ability := range abilityManager.List() {
		card, ok := ability.(*tool.ToolCard)
		if !ok {
			continue
		}
		if r.isMetaTool(card.Name) {
			continue
		}
		if card.EffectiveExposure() != tool.ToolExposureDeferred {
			continue
		}
		extra, display := resolveProgressiveDiscovery(card, language)
		description := display
		if description == "" {
			description = card.Description
		}
		documents = append(documents, deferredToolDocument{
			info:               card.ToolInfo(),
			extraSearchText:    extra,
			displayDescription: description,
		})
	}
	return documents
}

// collectDeferredToolDescriptions 返回按名排序的 deferred 目录摘要。
//
// Python: _collect_deferred_tool_descriptions
func (r *ProgressiveToolRail) collectDeferredToolDescriptions(abilityManager agentinterfaces.AbilityManagerInterface) map[string]string {
	descriptions := make(map[string]string)
	for _, document := range r.collectDeferredToolDocuments(abilityManager) {
		if name := document.info.GetName(); name != "" {
			descriptions[name] = document.displayDescription
		}
	}
	return descriptions
}

// collectDeferredFingerprints 读取当前目录指纹（按名排序）。
//
// Python: _collect_deferred_tool_fingerprints
func (r *ProgressiveToolRail) collectDeferredFingerprints() map[string]string {
	fingerprints := make(map[string]string)
	for _, document := range r.collectDeferredToolDocuments(r.abilityManager()) {
		if name := document.info.GetName(); name != "" {
			fingerprints[name] = tool_discovery.ToolFingerprint(document.info)
		}
	}
	return fingerprints
}

// rebuildToolSearchIndex 重建 BM25 索引并记录注册版本。
//
// Python: _rebuild_tool_search_index
func (r *ProgressiveToolRail) rebuildToolSearchIndex(documents []deferredToolDocument) {
	infos := make([]cschema.ToolInfoInterface, 0, len(documents))
	extras := make([]string, 0, len(documents))
	for _, document := range documents {
		infos = append(infos, document.info)
		extras = append(extras, document.extraSearchText)
	}
	index := tool_discovery.BuildBM25ToolIndex(infos, extras)

	r.mu.Lock()
	r.toolSearchIndex = index
	r.toolSearchIndexRevision = registryRevisionOf(r.toolSearchRegistry)
	r.mu.Unlock()
}

// ensureToolSearchIndex 仅在目录或注册版本变化时重建。
//
// Python: _ensure_tool_search_index
func (r *ProgressiveToolRail) ensureToolSearchIndex() {
	abilityManager := r.abilityManager()
	documents := r.collectDeferredToolDocuments(abilityManager)
	infos := make([]cschema.ToolInfoInterface, 0, len(documents))
	extras := make([]string, 0, len(documents))
	for _, document := range documents {
		infos = append(infos, document.info)
		extras = append(extras, document.extraSearchText)
	}
	fingerprint := tool_discovery.Fingerprint(infos, extras)

	r.mu.Lock()
	reusable := r.toolSearchIndex != nil &&
		r.toolSearchIndex.SourceFingerprint() == fingerprint &&
		r.toolSearchIndexRevision == registryRevisionOf(abilityManager)
	r.mu.Unlock()
	if reusable {
		return
	}
	r.rebuildToolSearchIndex(documents)
}

// searchTools 检索并授权命中工具。
//
// Python: _search_tools
func (r *ProgressiveToolRail) searchTools(
	_ context.Context,
	query string,
	limit int,
	sess sessioninterfaces.SessionFacade,
) ([]map[string]any, error) {
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		return nil, nil
	}
	r.ensureToolSearchIndex()

	r.mu.Lock()
	index := r.toolSearchIndex
	r.mu.Unlock()

	var matched []cschema.ToolInfoInterface
	if index != nil {
		matched = index.Search(normalized, limit)
	}

	names := make([]string, 0, len(matched))
	fingerprints := make(map[string]string, len(matched))
	for _, info := range matched {
		if info.GetName() == "" {
			continue
		}
		names = append(names, info.GetName())
		fingerprints[info.GetName()] = tool_discovery.ToolFingerprint(info)
	}
	r.authorizeDiscoveredTools(sess, names, fingerprints)
	r.appendTrace(sess, map[string]any{
		"action":  "tool_search",
		"query":   normalized,
		"limit":   limit,
		"matched": names,
	})

	results := make([]map[string]any, 0, len(matched))
	for _, info := range matched {
		results = append(results, map[string]any{
			"name":        info.GetName(),
			"description": info.GetDescription(),
			"parameters":  info.GetParameters(),
		})
	}
	return results, nil
}

// callDiscoveredTool 经 AbilityManager 嵌套执行一个已发现的 deferred 工具。
//
// 五道校验：非空 → 已授权 → 仍注册 → 仍为 deferred → 指纹未变。
//
// Python: _call_discovered_tool
func (r *ProgressiveToolRail) callDiscoveredTool(
	ctx context.Context,
	name string,
	args map[string]any,
	sess sessioninterfaces.SessionFacade,
	cbc *agentinterfaces.AgentCallbackContext,
) (*tool_discovery.RelayedToolOutput, error) {
	target := strings.TrimSpace(name)
	if target == "" {
		return nil, errors.New("tool_call requires a non-empty tool name.")
	}
	if !containsString(r.getDiscoveredTools(sess), target) {
		return rejectedToolOutput("Deferred tool '" + target + "' must be returned by tool_search before it can be called."), nil
	}
	card := r.lookupCard(cbc, target)
	if card == nil {
		return rejectedToolOutput("Deferred tool '" + target + "' is no longer registered."), nil
	}
	if card.EffectiveExposure() != tool.ToolExposureDeferred {
		return rejectedToolOutput("Tool '" + target + "' is not a deferred search result."), nil
	}
	if fingerprints := r.getDiscoveredToolFingerprints(sess); fingerprints != nil {
		expected := fingerprints[target]
		if expected == "" {
			return rejectedToolOutput("Deferred tool '" + target + "' must be searched again before it can be called."), nil
		}
		current := tool_discovery.ToolFingerprint(card.ToolInfo())
		if current != expected {
			r.invalidateDiscoveredTools(sess, map[string]struct{}{target: {}})
			return rejectedToolOutput("Deferred tool '" + target + "' has changed; search it again before calling it."), nil
		}
	}
	if cbc == nil {
		return nil, errors.New("tool_call requires an active agent callback context")
	}
	r.appendTrace(sess, map[string]any{"action": "tool_call", "name": target})

	outerID := "tool_call"
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok && inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		outerID = inputs.ToolCall.ID
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		arguments = []byte("{}")
	}
	targetCall := &llmschema.ToolCall{
		ID:        outerID + ":target",
		Type:      "function",
		Name:      target,
		Arguments: string(arguments),
	}

	cbc.SetExtra(nestedToolCallKey, true)
	defer cbc.DeleteExtra(nestedToolCallKey)

	results := r.abilityManagerFrom(cbc).Execute(ctx, cbc, []*llmschema.ToolCall{targetCall}, sess, "", true)
	if len(results) == 0 || results[0].ToolMsg == nil {
		return rejectedToolOutput("Deferred tool '" + target + "' returned no execution result."), nil
	}
	result := results[0]
	rendered := result.ToolMsg.Content.Text()
	payload := map[string]any{"name": target, "result": result.Result}
	return tool_discovery.NewRelayedToolOutput(true, rendered, payload, nil), nil
}

// dispatchToolCall 分发 tool_call（回调上下文由工具侧注入）。
func (r *ProgressiveToolRail) dispatchToolCall(
	ctx context.Context,
	name string,
	args map[string]any,
	callbackContext tool.ToolCallbackContext,
) (any, error) {
	cbc, ok := callbackContext.(*agentinterfaces.AgentCallbackContext)
	if !ok {
		return nil, errors.New("tool_call requires an active agent callback context")
	}
	return r.callDiscoveredTool(ctx, name, args, cbc.Session(), cbc)
}

// rejectedToolOutput 构造失败结果（供模型读取的拒绝文案）。
func rejectedToolOutput(message string) *tool_discovery.RelayedToolOutput {
	return tool_discovery.NewRelayedToolOutput(false, "", nil, errors.New(message))
}

// filterModelCallTools 把模型可见工具收敛为 meta ∪ direct。
//
// Python: before_model_call 的工具过滤段
func (r *ProgressiveToolRail) filterModelCallTools(
	inputs *agentinterfaces.ModelCallInputs,
	abilityManager agentinterfaces.AbilityManagerInterface,
) error {
	if inputs == nil || inputs.Tools == nil {
		return nil
	}
	filtered := make([]cschema.ToolInfoInterface, 0, len(inputs.Tools))
	for _, info := range inputs.Tools {
		name := info.GetName()
		if name == "" {
			continue
		}
		if r.isMetaTool(name) {
			filtered = append(filtered, info)
			continue
		}
		if card := lookupCardByName(abilityManager, name); card != nil && card.EffectiveExposure() == tool.ToolExposureDirect {
			filtered = append(filtered, info)
		}
	}
	inputs.Tools = filtered
	return nil
}

// rejectToolCall 阻断执行并向模型返回可读结果。
//
// Python: _reject_tool_call
func rejectToolCall(cbc *agentinterfaces.AgentCallbackContext, message string) {
	cbc.SetExtra("_skip_tool", true)
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return
	}
	callID := "unknown-tool-call"
	if inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		callID = inputs.ToolCall.ID
	}
	inputs.ToolResult = map[string]any{"error": message}
	inputs.ToolMsg = llmschema.NewToolMessage(callID, message)
}

// toolNameOf 读取回调上下文里的工具名。
func toolNameOf(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil {
		return ""
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return ""
	}
	if inputs.ToolName != "" {
		return inputs.ToolName
	}
	if inputs.ToolCall != nil {
		return inputs.ToolCall.Name
	}
	return ""
}

// registerToolResources 把元工具实例注册到进程级 ResourceMgr。
func (r *ProgressiveToolRail) registerToolResources(tools ...tool.Tool) {
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr == nil {
		return
	}
	for _, candidate := range tools {
		card := candidate.Card()
		if existing, err := resourceMgr.GetTool([]string{card.ID}); err == nil && len(existing) > 0 {
			continue
		}
		if err := resourceMgr.AddTool(candidate); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "progressive_rail_init").
				Str("tool_id", card.ID).
				Err(err).
				Msg("failed to register tool to resource_mgr")
		}
	}
}

// getDiscoveredTools 读取已授权工具名。
//
// Python: _get_discovered_tools
func (r *ProgressiveToolRail) getDiscoveredTools(sess sessioninterfaces.SessionFacade) []string {
	if sess == nil {
		return nil
	}
	raw, err := sess.GetState(state.StringKey(discoveredToolsKey))
	if err != nil {
		return nil
	}
	values, ok := raw.([]string)
	if !ok {
		return nil
	}
	return values
}

// getDiscoveredToolFingerprints 读取授权指纹；无记录返回 nil（对齐上游 Optional）。
//
// Python: _get_discovered_tool_fingerprints
func (r *ProgressiveToolRail) getDiscoveredToolFingerprints(sess sessioninterfaces.SessionFacade) map[string]string {
	if sess == nil {
		return nil
	}
	raw, err := sess.GetState(state.StringKey(discoveredToolFingerprintsKey))
	if err != nil {
		return nil
	}
	typed, ok := raw.(map[string]string)
	if !ok {
		return nil
	}
	return typed
}

// setDiscoveredTools 写入授权名单与指纹（只保留名单内的指纹）。
//
// Python: _set_discovered_tools
func (r *ProgressiveToolRail) setDiscoveredTools(sess sessioninterfaces.SessionFacade, names []string, fingerprints map[string]string) {
	if sess == nil {
		return
	}
	deduped := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		deduped = append(deduped, trimmed)
	}
	merged := r.getDiscoveredToolFingerprints(sess)
	if merged == nil {
		merged = map[string]string{}
	}
	for name, fingerprint := range fingerprints {
		if name != "" && fingerprint != "" {
			merged[name] = fingerprint
		}
	}
	kept := make(map[string]string, len(deduped))
	for _, name := range deduped {
		if fingerprint, ok := merged[name]; ok {
			kept[name] = fingerprint
		}
	}
	sess.UpdateState(map[string]any{
		discoveredToolsKey:            deduped,
		discoveredToolFingerprintsKey: kept,
	})
}

// authorizeDiscoveredTools 把命中工具并入授权名单。
//
// Python: _authorize_discovered_tools
func (r *ProgressiveToolRail) authorizeDiscoveredTools(sess sessioninterfaces.SessionFacade, names []string, fingerprints map[string]string) {
	if sess == nil {
		return
	}
	r.setDiscoveredTools(sess, append(r.getDiscoveredTools(sess), names...), fingerprints)
}

// invalidateDiscoveredTools 失效指定工具的授权。
//
// Python: _invalidate_discovered_tools
func (r *ProgressiveToolRail) invalidateDiscoveredTools(sess sessioninterfaces.SessionFacade, names map[string]struct{}) {
	if sess == nil || len(names) == 0 {
		return
	}
	current := r.getDiscoveredTools(sess)
	retained := make([]string, 0, len(current))
	for _, name := range current {
		if _, drop := names[name]; drop {
			continue
		}
		retained = append(retained, name)
	}
	if len(retained) != len(current) {
		r.setDiscoveredTools(sess, retained, nil)
	}
}

// appendTrace 追加发现轨迹。
//
// Python: _append_trace
func (r *ProgressiveToolRail) appendTrace(sess sessioninterfaces.SessionFacade, event map[string]any) {
	if sess == nil {
		return
	}
	trace := []map[string]any{}
	if raw, err := sess.GetState(state.StringKey(discoveryTraceKey)); err == nil {
		if existing, ok := raw.([]map[string]any); ok {
			trace = existing
		}
	}
	trace = append(trace, event)
	sess.UpdateState(map[string]any{discoveryTraceKey: trace})
}

// ensureInitialDeferredCatalog 读取或冻结会话初始目录。
//
// Python: _ensure_initial_deferred_catalog
func (r *ProgressiveToolRail) ensureInitialDeferredCatalog(
	sess sessioninterfaces.SessionFacade,
	abilityManager agentinterfaces.AbilityManagerInterface,
) map[string]string {
	current := r.collectDeferredToolDescriptions(abilityManager)
	if sess == nil {
		return current
	}
	stateMap := catalogStateOf(sess)
	if initial, ok := normalizeDeferredCatalog(mapValueOf(stateMap["initial_tools"])); ok {
		return initial
	}
	initial := current
	if stateMap["initialized"] == true {
		if previous, ok := normalizeDeferredCatalog(mapValueOf(stateMap["tools"])); ok {
			initial = previous
		}
	}
	stateMap["initial_tools"] = initial
	sess.UpdateState(map[string]any{deferredToolCatalogStateKey: stateMap})
	return initial
}

// abilityManager 返回 rail 持有的注册表引用。
func (r *ProgressiveToolRail) abilityManager() agentinterfaces.AbilityManagerInterface {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolSearchRegistry
}

// abilityManagerFrom 从回调上下文取注册表，缺失时回落到 rail 引用。
func (r *ProgressiveToolRail) abilityManagerFrom(cbc *agentinterfaces.AgentCallbackContext) agentinterfaces.AbilityManagerInterface {
	if cbc != nil && cbc.Agent() != nil {
		if abilityManager := cbc.Agent().AbilityManager(); abilityManager != nil {
			return abilityManager
		}
	}
	return r.abilityManager()
}

// lookupCard 按名取工具卡片。
func (r *ProgressiveToolRail) lookupCard(cbc *agentinterfaces.AgentCallbackContext, name string) *tool.ToolCard {
	return lookupCardByName(r.abilityManagerFrom(cbc), name)
}

// lookupCardByName 从注册表按名取工具卡片。
func lookupCardByName(abilityManager agentinterfaces.AbilityManagerInterface, name string) *tool.ToolCard {
	if abilityManager == nil {
		return nil
	}
	card, _ := abilityManager.Get(name).(*tool.ToolCard)
	return card
}

// isMetaTool 判断是否为元工具。
func (r *ProgressiveToolRail) isMetaTool(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.metaToolNames[name]
	return ok
}

// isCachedTool 判断工具在缓存清单中。
func (r *ProgressiveToolRail) isCachedTool(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, info := range r.cachedAllToolInfos {
		if info.GetName() == name {
			return true
		}
	}
	return false
}

// attachments 取会话级动态提示词管理器。
func (r *ProgressiveToolRail) attachments(cbc *agentinterfaces.AgentCallbackContext) attachmentManager {
	if cbc == nil || cbc.Agent() == nil {
		return nil
	}
	provider, ok := cbc.Agent().(promptAttachmentProvider)
	if !ok {
		return nil
	}
	manager := provider.PromptAttachments()
	if manager == nil {
		return nil
	}
	return &managerAdapter{manager: manager, history: cbc.ModelContext()}
}

// catalogLanguage 解析目录语言（cn/en）。
//
// Python: _resolve_catalog_language
func (r *ProgressiveToolRail) catalogLanguage() string {
	language := ""
	if r.config != nil {
		language = strings.ToLower(strings.TrimSpace(r.config.EffectiveLanguage()))
	}
	if language == "en" {
		return "en"
	}
	return "cn"
}

// toolSearchLimit 返回 tool_search 默认条数（clamp 1..20）。
func (r *ProgressiveToolRail) toolSearchLimit() int {
	limit := 0
	if r.config != nil {
		limit = r.config.ToolSearchLimit
	}
	if limit <= 0 {
		limit = tool_discovery.DefaultToolSearchLimit
	}
	return tool_discovery.NormalizeToolSearchLimit(limit)
}

// registryRevisionOf 读取注册表版本（无能力时返回 0）。
func registryRevisionOf(abilityManager agentinterfaces.AbilityManagerInterface) int {
	if provider, ok := abilityManager.(agentinterfaces.RegistryRevisionProvider); ok {
		return provider.RegistryRevision()
	}
	return 0
}

// resolveProgressiveDiscovery 解析卡片上的本地化描述与检索别名。
//
// Python: _resolve_progressive_discovery
func resolveProgressiveDiscovery(card *tool.ToolCard, language string) (string, string) {
	if card.Properties == nil {
		return "", ""
	}
	raw, ok := card.Properties[progressiveDiscoveryProperty].(map[string]any)
	if !ok {
		return "", ""
	}
	parts := make([]string, 0, 4)
	displayDescription := ""
	if display, ok := raw["display"].(map[string]any); ok {
		keys := make([]string, 0, len(display))
		for key := range display {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if text, ok := display[key].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		displayDescription = localizedDisplay(display, language)
	}
	if aliases, ok := raw["aliases"].([]any); ok {
		for _, alias := range aliases {
			if text, ok := alias.(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
	}
	return strings.Join(parts, " "), displayDescription
}

// localizedDisplay 按语言候选取本地化描述。
func localizedDisplay(display map[string]any, language string) string {
	candidates := []string{language}
	switch language {
	case "cn":
		candidates = []string{"cn", "zh"}
	case "zh":
		candidates = []string{"zh", "cn"}
	}
	for _, candidate := range candidates {
		if text, ok := display[candidate].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// containsString 判断切片是否包含目标字符串。
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// AddSection 写入或覆盖附件。
func (a *managerAdapter) AddSection(sessionID, section, content string, kind prompts.AttachmentKind, source string, opts ...prompts.AttachmentOption) (*prompts.Attachment, error) {
	return a.manager.AddSection(sessionID, section, content, kind, source, opts...)
}

// Find 查询指定 section/source 的附件。
func (a *managerAdapter) Find(sessionID, section, source string) *prompts.Attachment {
	return a.manager.Find(sessionID, section, source)
}

// HasHistorySnapshot 判断会话历史是否已有完整快照。
func (a *managerAdapter) HasHistorySnapshot(sessionID string) bool {
	if a.history == nil {
		return false
	}
	return a.manager.HasHistorySnapshot(a.history, sessionID)
}

// syncDeferredToolAttachment 计算目录差异并写入附件（delta / snapshot）。
//
// 首轮未变化不写；无快照、语言变化或连续 delta 达上限时改发全量快照。
//
// Python: _sync_deferred_tool_attachment
func (r *ProgressiveToolRail) syncDeferredToolAttachment(
	_ context.Context,
	sess sessioninterfaces.SessionFacade,
	manager attachmentManager,
	current map[string]string,
) error {
	if sess == nil || manager == nil {
		return nil
	}
	sessionID := sess.GetSessionID()
	if sessionID == "" {
		return nil
	}
	language := r.catalogLanguage()
	currentTools, _ := normalizeDeferredCatalog(current)
	currentFingerprints := r.collectDeferredFingerprints()

	catalogState := catalogStateOf(sess)
	initialTools := initialStateTools(catalogState, currentTools)
	previousTools := previousStateTools(catalogState)
	initialized := catalogState["initialized"] == true
	previousLanguage := stringOf(catalogState["language"])
	version := intOf(catalogState["version"], 1)
	deltaCount := intOf(catalogState["delta_count"], 0)

	// 热重载/新 Session：从附件 metadata 恢复上次目录，避免重复推送同一差异
	if existing := manager.Find(sessionID, deferredToolAttachmentSection, deferredToolAttachmentSource); existing != nil {
		if recovered, ok := normalizeDeferredCatalog(mapValueOf(existing.Metadata[deferredToolCatalogMetadataKey])); ok {
			previousTools = recovered
			initialized = true
			if fingerprints, ok := normalizeDeferredCatalog(mapValueOf(existing.Metadata[deferredToolCatalogFingerprintsMetadataKey])); ok {
				_ = fingerprints
			}
			if value, ok := existing.Metadata["catalog_language"].(string); ok && value != "" {
				previousLanguage = value
			}
			if value, ok := existing.Metadata["catalog_version"].(int); ok && value > version {
				version = value
			}
			if value, ok := existing.Metadata["delta_count"].(int); ok {
				deltaCount = value
			}
		}
	}

	baseline := initialTools
	if initialized {
		baseline = previousTools
	}
	added := make(map[string]string)
	updated := make(map[string]string)
	for name, description := range currentTools {
		existing, ok := baseline[name]
		if !ok {
			added[name] = description
			continue
		}
		if existing != description {
			updated[name] = description
		}
	}
	removed := make([]string, 0)
	for name := range baseline {
		if _, ok := currentTools[name]; !ok {
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	changed := len(added) > 0 || len(updated) > 0 || len(removed) > 0
	nextVersion := version
	if changed {
		nextVersion = version + 1
	}

	// 变更即失效对应授权：旧搜索结果不得复活已修改/删除的工具
	invalidated := make(map[string]struct{}, len(updated)+len(removed))
	for name := range updated {
		invalidated[name] = struct{}{}
	}
	for _, name := range removed {
		invalidated[name] = struct{}{}
	}
	if len(invalidated) > 0 {
		r.invalidateDiscoveredTools(sess, invalidated)
	}

	if !initialized {
		// 稳定提示词里的初始目录就是基线；首轮未变化则不写附件
		if !changed {
			r.saveCatalogState(sess, initialTools, currentTools, currentFingerprints, version, 0, language)
			return nil
		}
		version = nextVersion
		content := sections.RenderDeferredToolCatalogDelta(added, updated, removed, language, version)
		if err := r.writeCatalogAttachment(manager, sessionID, language, version, "delta", content, currentTools, currentFingerprints, 1); err != nil {
			return err
		}
		r.saveCatalogState(sess, initialTools, currentTools, currentFingerprints, version, 1, language)
		return nil
	}

	forceSnapshot := !manager.HasHistorySnapshot(sessionID) ||
		previousLanguage != language ||
		deltaCount >= fullSnapshotInterval
	if !forceSnapshot && !changed {
		return nil
	}

	version = nextVersion
	mode := "delta"
	nextDeltaCount := deltaCount + 1
	content := sections.RenderDeferredToolCatalogDelta(added, updated, removed, language, version)
	if forceSnapshot {
		mode = "snapshot"
		nextDeltaCount = 0
		content = sections.RenderDeferredToolCatalogSnapshot(currentTools, language, version)
	}
	if err := r.writeCatalogAttachment(manager, sessionID, language, version, mode, content, currentTools, currentFingerprints, nextDeltaCount); err != nil {
		return err
	}
	r.saveCatalogState(sess, initialTools, currentTools, currentFingerprints, version, nextDeltaCount, language)
	return nil
}

// saveCatalogState 写回目录状态。
//
// Python: _sync_deferred_tool_attachment 末尾的 session.update_state
func (r *ProgressiveToolRail) saveCatalogState(
	sess sessioninterfaces.SessionFacade,
	initialTools map[string]string,
	currentTools map[string]string,
	fingerprints map[string]string,
	version int,
	deltaCount int,
	language string,
) {
	if sess == nil {
		return
	}
	sess.UpdateState(map[string]any{deferredToolCatalogStateKey: map[string]any{
		"initialized":                           true,
		"version":                               version,
		"delta_count":                           deltaCount,
		"language":                              language,
		"initial_tools":                         initialTools,
		"tools":                                 currentTools,
		"fingerprints":                          fingerprints,
		deferredToolRegistryRevisionMetadataKey: registryRevisionOf(r.abilityManager()),
	}})
}

// writeCatalogAttachment 写入目录附件（priority 75）。
//
// Python: add_section(section=progressive_deferred_tools, kind=TOOL, priority=75)
func (r *ProgressiveToolRail) writeCatalogAttachment(
	manager attachmentManager,
	sessionID string,
	language string,
	version int,
	mode string,
	content string,
	currentTools map[string]string,
	fingerprints map[string]string,
	deltaCount int,
) error {
	_, err := manager.AddSection(
		sessionID,
		deferredToolAttachmentSection,
		content,
		prompts.AttachmentTool,
		deferredToolAttachmentSource,
		prompts.WithAttachmentPriority(75),
		prompts.WithAttachmentContentKind("text/markdown"),
		prompts.WithAttachmentMetadata(map[string]any{
			"catalog_version":                          version,
			"catalog_mode":                             mode,
			"catalog_language":                         language,
			"delta_count":                              deltaCount,
			deferredToolCatalogMetadataKey:             currentTools,
			deferredToolCatalogFingerprintsMetadataKey: fingerprints,
			deferredToolRegistryRevisionMetadataKey:    registryRevisionOf(r.abilityManager()),
		}),
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "progressive_catalog_attachment").
			Err(err).
			Msg("failed to update deferred-tool attachment")
	}
	return err
}

// catalogStateOf 读取会话里的目录状态。
func catalogStateOf(sess sessioninterfaces.SessionFacade) map[string]any {
	raw, err := sess.GetState(state.StringKey(deferredToolCatalogStateKey))
	if err != nil {
		return map[string]any{}
	}
	typed, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return typed
}

// initialStateTools 读取初始目录，缺失时用当前目录播种。
func initialStateTools(catalogState map[string]any, current map[string]string) map[string]string {
	if normalized, ok := normalizeDeferredCatalog(mapValueOf(catalogState["initial_tools"])); ok {
		return normalized
	}
	normalized, _ := normalizeDeferredCatalog(current)
	return normalized
}

// previousStateTools 读取上次已发布的目录。
func previousStateTools(catalogState map[string]any) map[string]string {
	if normalized, ok := normalizeDeferredCatalog(mapValueOf(catalogState["tools"])); ok {
		return normalized
	}
	return map[string]string{}
}

// normalizeDeferredCatalog 归一化目录：丢弃空名，空目录也视为有效目录。
//
// Python: _normalize_deferred_catalog（None 与空字典语义不同）
func normalizeDeferredCatalog(catalog map[string]string) (map[string]string, bool) {
	if catalog == nil {
		return nil, false
	}
	normalized := make(map[string]string, len(catalog))
	for name, description := range catalog {
		if strings.TrimSpace(name) == "" {
			continue
		}
		normalized[name] = description
	}
	return normalized, true
}

// mapValueOf 把附件/会话状态里的任意值转为字符串目录。
func mapValueOf(value any) map[string]string {
	if converted, ok := value.(map[string]string); ok {
		return converted
	}
	if generic, ok := value.(map[string]any); ok {
		converted := make(map[string]string, len(generic))
		for key, item := range generic {
			if text, ok := item.(string); ok {
				converted[key] = text
			}
		}
		return converted
	}
	return nil
}

// intOf 读取整数值（缺省回落 fallback）。
func intOf(value any, fallback int) int {
	if typed, ok := value.(int); ok {
		return typed
	}
	return fallback
}

// stringOf 读取字符串值。
func stringOf(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}
