// Package rails 渐进披露族 prompt rail。
//
// 本文件对应 Python tool_retrieval_prompt_rail.py（1,086 行）：
// ToolRetrievalPromptRail——持有渐进工具披露的提示词目录与工具过滤。
// 三职责（每轮）：1) 向系统提示词注入披露节（分类目录，来源
// ToolRetrievalToolkit.frozen_catalogue——对 live deferred 注册表冻结的
// 静态分类表）；2) 保持冻结目录诚实：冻结后注册/移除的工具以 delta/
// snapshot 附件上报（复刻 openjiuwen ProgressiveToolRail 的附件同步契约）；
// 3) 过滤 inputs.tools，只让 direct 工具（加 tool_search/tool_call）到达模型。
//
// 与 9.11a ProgressiveToolRail 的关系（单一所有者）：两者注册同名元工具
// tool_search/tool_call，启用本 rail 时由 DeepAdapter 从 pending rails 剔除
// 内置 rail；本 rail 构造失败返回 nil 时 DeepAdapter 保留内置 rail 兜底。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/tool_retrieval_prompt_rail.py
package rails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	tool_discovery "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/tool_discovery"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolRetrievalPromptRail 渐进式工具披露的提示词与过滤持有者。
// Python: ToolRetrievalPromptRail（:147-176）。
type ToolRetrievalPromptRail struct {
	// DeepAgentRail 基座（GetCallbacks/生命周期契约）
	rails.DeepAgentRail
	// toolkit 检索工具包（DeepAdapter 懒创建注入）
	toolkit *swarmtools.ToolRetrievalToolkit
	// configBase 应用配置
	configBase map[string]any
	// sessionEnabled 会话级开关（is_tool_retrieval_enabled）
	sessionEnabled bool
	// agent 会话内 agent 引用
	agent agentinterfaces.BaseAgent
	// systemPromptBuilder 系统提示词构建器
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// frozenSnapshot 冻结目录快照
	frozenSnapshot *swarmtools.ToolCatalogueSnapshot
	// frozenToolNames 冻结时的 deferred 工具名集合
	frozenToolNames map[string]struct{}
	// frozenConnectorGroups 冻结的 MCP 连接器分组（server → 组）
	frozenConnectorGroups map[string]swarmtools.McpServerGroup
	// catalogueFinalized 首次模型调用前是否已重冻结
	catalogueFinalized bool
	// ownedToolCards 本 rail 注册进 AbilityManager 的卡片（tool_call 信封）；
	// uninit 只摘这些（别的所有者重新注册的名字不动）
	ownedToolCards map[string]*tool.ToolCard
	// attachmentManager 提示词附件管理器
	attachmentManager *prompts.Manager
}

// McpServerGroupAlias 兼容别名（供外部类型引用）。
type McpServerGroupAlias = swarmtools.McpServerGroup

// ──────────────────────────── 常量 ────────────────────────────

const (
	// railPriority 优先级（Python: priority = 80）
	railPriority = 80
	// sectionName 披露节名称（Python: SECTION_NAME = "tool_retrieval"）
	sectionName = "tool_retrieval"
	// sectionPriority 披露节优先级（Python: SECTION_PRIORITY = 75）
	disclosureSectionPriority = 75
	// toolSearchToolName tool_search 元工具名（Python: _TOOL_SEARCH_TOOL_NAME）
	toolSearchToolName = "tool_search"
	// toolCallToolName tool_call 信封名（Python: _TOOL_CALL_TOOL_NAME）
	toolCallToolName = "tool_call"
	// baselineMetaAnchor 元工具在广告数组中的锚点（Python: _BASELINE_META_ANCHOR）
	baselineMetaAnchor = "task_tool"
	// catalogSnapshotInterval 连续 delta 上限，达到后改发全量快照
	// （Python: _CATALOG_SNAPSHOT_INTERVAL）
	catalogSnapshotInterval = 10
	// maxEntryDescChars 目录行描述压缩上限（Python: _MAX_ENTRY_DESC_CHARS）
	maxEntryDescChars = 120
	// catalogStateKey 会话状态键（Python: _CATALOG_STATE_KEY）
	catalogStateKey = "__tool_retrieval_deferred_catalog__"
	// catalogAttachmentSection 附件节名（Python: _CATALOG_ATTACHMENT_SECTION）
	catalogAttachmentSection = "tool_retrieval_deferred_tools"
	// catalogAttachmentSource 附件来源（Python: _CATALOG_ATTACHMENT_SOURCE）
	catalogAttachmentSource = "tool_retrieval_prompt_rail"
	// catalogMetadataTools 附件 metadata 键：目录描述集
	catalogMetadataTools = "catalog_tools"
	// catalogMetadataFingerprints 附件 metadata 键：目录指纹集
	catalogMetadataFingerprints = "catalog_fingerprints"
	// nestedToolCallKey 嵌套工具调用标记（复刻 9.11a 契约）
	nestedToolCallKey = "_nested_tool_call"
)

// baselineMetaOrder 广告数组基线槽位序：search 先于执行（Python: _BASELINE_META_ORDER）。
var baselineMetaOrder = []string{toolSearchToolName, toolCallToolName}

// disclosureIntroZh 中文披露导语（Python: _DISCLOSURE_INTRO_CN 逐字节）。
const disclosureIntroZh = "当前启用了渐进式工具加载。工具分为两类：\n" +
	"- direct 工具：在顶层 tools 中，可以直接调用。\n" +
	"- deferred 工具：已注册且可用，但不在顶层 tools 中，需先通过 `tool_search` 发现。\n" +
	"\n" +
	"## tool_search 使用规则\n" +
	"1. 先判断用户真正想完成的操作；用户未点名执行方式且 direct 工具明确支持时，" +
	"可直接调用。\n" +
	"2. 用户点名了具体工具、技术或执行方式（如“用 PowerShell”“用终端”）时，以用户" +
	"点名为准：即使 direct 工具（如 bash）技术上也能完成，也必须使用对应的 deferred " +
	"工具；目录中没有时，向用户说明后选用最接近的 direct 工具完成。\n" +
	"3. 目录每行已标注类型，按标注导航：`[category]` 行是分类，要看其下工具名单，调用 " +
	"`tool_search`（operation=list，category=该行名称）；`[tool]` 行已直列工具名与简述，" +
	"直接调用 `tool_search`（operation=get_schema，tool=名称），不要 list。分类名就在行" +
	"上，永远不要 list 根分类。" +
	"不能猜测 deferred 工具名称。list 只返回名称与简短" +
	"描述，不含 schema。\n" +
	"4. 确定工具后，用 get_schema（tool=准确工具名）获取该工具的 " +
	"{name, description, schema}，schema 即执行参数结构。\n" +
	"5. get_schema 之后，下一轮通过固定的 `tool_call` 执行：把准确工具名称放入 `name`，" +
	"把符合 schema 的参数放入 `args`。不要直接调用 deferred 工具名称。\n" +
	"6. `tool_search` 只用于发现工具，永不执行工具；不要用 `bash` 或其他 direct 工具" +
	"替代或执行搜索到的工具。\n" +
	"7. 内置工具的 keyword 支持中英文能力词，MCP 按提供方原始描述检索。" +
	"不要重复相近检索、逐个枚举无关分类或猜测工具名称。\n" +
	"\n" +
	"下方目录为 session 初始快照；会话中的新增、修改、删除以「deferred 工具目录更新」" +
	"附件通知，新增工具的名称同样直列，直接 get_schema。"

// disclosureIntroEn 英文披露导语（Python: _DISCLOSURE_INTRO_EN 逐字节）。
const disclosureIntroEn = "Progressive tool loading is enabled. Tools fall into two classes:\n" +
	"- direct tools: listed in the top-level tools; call them directly.\n" +
	"- deferred tools: registered and available but not in the top-level tools; " +
	"discover them via `tool_search` first.\n" +
	"\n" +
	"## tool_search usage rules\n" +
	"1. Identify the operation the user actually wants; if a direct tool clearly " +
	"supports it and the user named no specific tool or execution method, call " +
	"that tool directly.\n" +
	"2. When the user explicitly names a tool, technology, or execution method " +
	"(e.g. \"use PowerShell\", \"use the terminal\"), that naming takes " +
	"precedence: even if a direct tool (such as bash) could technically do it, " +
	"you must use the matching deferred tool; if the catalogue has no match, " +
	"say so and use the closest direct tool for the job.\n" +
	"3. Every catalogue line carries a type mark — navigate by it: a " +
	"`[category]` line is a category; to see its tool names, call " +
	"`tool_search` (operation=list, category=that name); a `[tool]` line " +
	"already lists the tool name with a short description — call " +
	"`tool_search` (operation=get_schema, tool=the name) directly, do not " +
	"list. The category name is on the line; never list the root category. " +
	"Do not guess deferred tool names. list returns names and short " +
	"descriptions only, no schemas.\n" +
	"4. Once you know the tool, use get_schema (tool=the exact tool name) to " +
	"get its {name, description, schema}; the schema is the execution " +
	"parameter structure.\n" +
	"5. After get_schema, execute through the fixed `tool_call` on the next " +
	"turn: put the exact tool name in `name` and the schema-conformant " +
	"parameters in `args`. Do not call the deferred tool name directly.\n" +
	"6. `tool_search` only discovers tools and never executes them; do not " +
	"use `bash` or other direct tools to substitute for or execute discovered " +
	"tools.\n" +
	"7. Built-in tools' keyword accepts Chinese and English capability terms; " +
	"MCP follows the provider's original description. Do not repeat similar " +
	"searches, enumerate unrelated categories one by one, or guess tool " +
	"names.\n" +
	"\n" +
	"The catalogue below is the session's initial snapshot; additions, " +
	"modifications and removals during the session are notified via the " +
	"\"deferred tool catalogue update\" attachment, with new tool names " +
	"listed directly — call get_schema on them directly."

// ──────────────────────────── 构造 ────────────────────────────

// NewToolRetrievalPromptRail 构造 rail。Python: __init__（:154-176）。
func NewToolRetrievalPromptRail(
	toolkit *swarmtools.ToolRetrievalToolkit,
	configBase map[string]any,
) *ToolRetrievalPromptRail {
	r := &ToolRetrievalPromptRail{
		DeepAgentRail:         *rails.NewDeepAgentRail(),
		toolkit:               toolkit,
		configBase:            configBase,
		sessionEnabled:        config.ToolRetrievalEnabled(configBase),
		frozenToolNames:       map[string]struct{}{},
		frozenConnectorGroups: map[string]swarmtools.McpServerGroup{},
		ownedToolCards:        map[string]*tool.ToolCard{},
	}
	// V-4：受信任构造函数标记（对齐 NewProgressiveToolRail；readonly 绑定
	// 链据此判定工具可免 LLM 审查）。
	r.MarkTrusted()
	return r
}

// ──────────────────────────── 生命周期 ────────────────────────────

// Init 会话启动时冻结披露目录并注册执行信封。
// Python: init（:178-199）——init 在 ensure_initialized 期间运行（AbilityManager
// 已解析曝光），先刷新拿 live deferred 集（工具包构造期快照常为空）。
func (r *ToolRetrievalPromptRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.agent = agent
	if agent != nil {
		r.systemPromptBuilder = agent.SystemPromptBuilder()
		r.attachmentManager = prompts.ManagerFor(agent)
	}
	// 冻结失败不阻断会话（Python try/except debug 日志）。
	if err := r.freezeDisclosureCatalogue(); err != nil {
		logger.Debug(logComponent).
			Str("event_type", "tool_retrieval_freeze_failed").
			Err(err).
			Msg("[ToolRetrievalPromptRail] snapshot freeze failed")
	}
	r.registerExecutionEnvelope(agent)
	return nil
}

// Uninit 只移除本 rail 注册的卡片并清理状态。
// Python: uninit（:380-407）——跳过其他所有者重新注册的名字（摘除会注销
// 其 live 实例）。
func (r *ToolRetrievalPromptRail) Uninit(agent agentinterfaces.BaseAgent) error {
	abilityManager := abilityManagerOfAgent(agent)
	for toolName, toolCard := range r.ownedToolCards {
		if abilityManager != nil {
			// Ability 即卡片具体形态；跳过其他所有者重新注册的名字。
			if current, ok := abilityManager.Get(toolName).(*tool.ToolCard); ok && current == toolCard {
				_ = abilityManager.Remove(toolName)
			}
		}
	}
	r.ownedToolCards = map[string]*tool.ToolCard{}

	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sectionName)
	}
	r.systemPromptBuilder = nil
	r.agent = nil
	r.frozenSnapshot = nil
	r.frozenToolNames = map[string]struct{}{}
	r.frozenConnectorGroups = map[string]swarmtools.McpServerGroup{}
	r.catalogueFinalized = false
	return nil
}

// Toolkit 返回检索工具包（V-6 绑定身份校验消费；Python 经
// rail._toolkit 私有字段访问，Go 以导出访问器等价替代）。
func (r *ToolRetrievalPromptRail) Toolkit() *swarmtools.ToolRetrievalToolkit {
	return r.toolkit
}

// GetCallbacks 注册 BeforeModelCall / BeforeToolCall 钩子。
func (r *ToolRetrievalPromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
		if !ok {
			return nil
		}
		return r.BeforeModelCall(ctx, cbc)
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
		if !ok {
			return nil
		}
		return r.BeforeToolCall(ctx, cbc)
	}
	return callbacks
}

// ──────────────────────────── 冻结与信封 ────────────────────────────

// freezeDisclosureCatalogue 从 live 注册表（重）冻结披露目录。
// Python: _freeze_disclosure_catalogue（:201-233）——init 一次 + 首次
// before_model_call 再一次：MCP 连接器工具异步注册，可能晚于
// ensure_initialized 落地，静态目录必须取首次渲染前的最后时机。
func (r *ToolRetrievalPromptRail) freezeDisclosureCatalogue() error {
	toolkit := r.toolkit
	if toolkit == nil {
		return nil
	}
	toolkit.RefreshSnapshot()
	snapshot := toolkit.FrozenCatalogue()
	r.frozenSnapshot = &snapshot
	r.frozenToolNames = map[string]struct{}{}
	for name := range toolkit.DeferredToolNames() {
		r.frozenToolNames[name] = struct{}{}
	}
	// 冻结失败回落空分组（Python try/except debug）。
	r.frozenConnectorGroups = toolkit.McpConnectorGroups()
	logger.Info(logComponent).
		Str("event_type", "tool_retrieval_freeze_catalogue").
		Int("tools", len(r.frozenToolNames)).
		Int("categories", len(snapshot.Categories)).
		Int("connectors", len(r.frozenConnectorGroups)).
		Msg("[ToolRetrievalPromptRail] freeze catalogue")
	return nil
}

// registerExecutionEnvelope 注册广告的 tool_call 包装器。
// Python: _register_execution_envelope（:236-277）——对未广告的 deferred 工具
// 直调没有 API 级 schema 锚定，嵌套对象参数在自由转写中被双重序列化成
// JSON 字符串；deferred 工具不进广告列表，改由固定信封（args: object 槽
// 锚定载荷）承载——与 openjiuwen ProgressiveToolRail 相同契约。
func (r *ToolRetrievalPromptRail) registerExecutionEnvelope(agent agentinterfaces.BaseAgent) {
	abilityManager := abilityManagerOfAgent(agent)
	if abilityManager == nil {
		return
	}
	language := "cn"
	if r.systemPromptBuilder != nil {
		language = r.systemPromptBuilder.Language()
	}
	envelope := tool_discovery.NewToolCallTool(language, "")
	envelope.SetDispatch(r.dispatchToolCall)
	card := envelope.Card()
	card.Exposure = tool.ToolExposureDirect
	card.SetExposureDeclared(true)
	result := abilityManager.AddAbility(card, envelope)
	if result.Added {
		r.ownedToolCards[card.Name] = card
		logger.Info(logComponent).
			Str("event_type", "tool_retrieval_envelope_registered").
			Msg("[ToolRetrievalPromptRail] tool_call envelope registered")
	} else {
		logger.Info(logComponent).
			Str("event_type", "tool_retrieval_envelope_not_added").
			Msg("[ToolRetrievalPromptRail] tool_call envelope not added")
	}
}

// dispatchToolCall 分发 tool_call（回调上下文由工具侧注入）。
func (r *ToolRetrievalPromptRail) dispatchToolCall(
	ctx context.Context,
	name string,
	args map[string]any,
	callbackContext tool.ToolCallbackContext,
) (any, error) {
	cbc, ok := callbackContext.(*agentinterfaces.AgentCallbackContext)
	if !ok {
		return nil, errors.New("tool_call requires an active agent callback context")
	}
	return r.callDeferredTool(ctx, name, args, cbc)
}

// callDeferredTool 经 AbilityManager 执行一个目录内 deferred 工具。
// Python: _call_deferred_tool（:279-378）——镜像 openjiuwen
// ProgressiveToolRail._call_discovered_tool：解析目标、对照 live deferred
// 目录授权、走常规 AbilityManager 执行路径（目标工具 rail、权限、超时、
// 错误处理全部保持激活）。
func (r *ToolRetrievalPromptRail) callDeferredTool(
	ctx context.Context,
	name string,
	args map[string]any,
	cbc *agentinterfaces.AgentCallbackContext,
) (*tool_discovery.RelayedToolOutput, error) {
	targetName := strings.TrimSpace(name)
	if targetName == "" {
		return nil, errors.New("tool_call requires a non-empty tool name.")
	}
	toolkit := r.toolkit
	if toolkit == nil {
		return nil, errors.New("Deferred tool '" + targetName + "' must come from the tool_search catalogue before it can be called.")
	}
	if _, live := toolkit.DeferredToolNames()[targetName]; !live {
		return nil, errors.New("Deferred tool '" + targetName + "' must come from the tool_search catalogue before it can be called.")
	}

	agent := cbc.Agent()
	if agent == nil {
		agent = r.agent
	}
	abilityManager := abilityManagerOfAgent(agent)
	var card *tool.ToolCard
	if abilityManager != nil {
		card = lookupCardByName(abilityManager, targetName)
	}
	if card == nil {
		return nil, errors.New("Deferred tool '" + targetName + "' is no longer registered.")
	}
	if card.EffectiveExposure() != tool.ToolExposureDeferred {
		return nil, errors.New("Tool '" + targetName + "' is not a deferred catalogue tool.")
	}

	outerCallID := "tool_call"
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok && inputs.ToolCall != nil && inputs.ToolCall.ID != "" {
		outerCallID = inputs.ToolCall.ID
	}
	arguments, err := json.Marshal(args)
	if err != nil {
		arguments = []byte("{}")
	}
	targetCall := &llmschema.ToolCall{
		ID:        outerCallID + ":target",
		Type:      "function",
		Name:      targetName,
		Arguments: string(arguments),
	}

	cbc.SetExtra(nestedToolCallKey, true)
	defer cbc.DeleteExtra(nestedToolCallKey)

	results := abilityManager.Execute(ctx, cbc, []*llmschema.ToolCall{targetCall}, cbc.Session(), "", true)
	if len(results) == 0 || results[0].ToolMsg == nil {
		return nil, errors.New("Deferred tool '" + targetName + "' returned no execution result.")
	}
	result := results[0]
	rendered := result.ToolMsg.Content.Text()
	// 拦截路径（rail 拒绝/执行错误）的 Result 形态是 map{"error": msg}
	// 或 string——对齐 Python target_result.success is False 分支。
	if failedResult(result.Result) {
		rawError := rendered
		return nil, fmt.Errorf(
			"%s — deferred call to '%s' failed; if the args did not match, re-fetch the exact schema with tool_search (operation=get_schema, tool='%s') and resend: args must mirror that schema's parameters object, with object-typed fields as JSON object literals, never JSON strings.",
			rawError, targetName, targetName)
	}
	payload := map[string]any{"name": targetName, "result": result.Result}
	return tool_discovery.NewRelayedToolOutput(true, rendered, payload, nil), nil
}

// ──────────────────────────── 每轮钩子 ────────────────────────────

// BeforeModelCall 注入披露节 + 同步漂移附件 + 过滤 inputs.tools。
// Python: before_model_call（:409-449）。
func (r *ToolRetrievalPromptRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if !r.sessionEnabled {
		return nil
	}
	if cbc.Agent() != nil && r.systemPromptBuilder == nil {
		r.systemPromptBuilder = cbc.Agent().SystemPromptBuilder()
	}

	if !r.catalogueFinalized {
		// 首次渲染前的最后一刻重冻结：MCP 连接器工具在 ensure_initialized
		// 之后注册，必须进静态目录而非只在 tool_search 结果里。
		r.catalogueFinalized = true
		if err := r.freezeDisclosureCatalogue(); err != nil {
			logger.Debug(logComponent).
				Str("event_type", "tool_retrieval_refreeze_failed").
				Err(err).
				Msg("[ToolRetrievalPromptRail] first-render re-freeze failed")
		}
	}

	language := "cn"
	if r.systemPromptBuilder != nil {
		language = r.systemPromptBuilder.Language()
		english := strings.HasPrefix(strings.ToLower(language), "en")
		appendix := r.buildDisclosureAppendix(english)
		r.systemPromptBuilder.RemoveSection(sectionName)
		r.systemPromptBuilder.AddSection(saprompt.PromptSection{
			Name:     sectionName,
			Content:  map[string]string{language: appendix},
			Priority: disclosureSectionPriority,
		})
	}

	r.syncDeferredToolCatalog(cbc, language)
	r.filterDeferredFromInputs(cbc)
	return nil
}

// BeforeToolCall 放行 direct 与嵌套标记的 deferred 调用。
// Python 无独立 before_tool_call（信封经嵌套标记绕过 9.11a 门卫），此处
// 与 9.11a 门卫语义一致：meta 工具放行，嵌套标记 + 目录成员放行。
func (r *ToolRetrievalPromptRail) BeforeToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if !r.sessionEnabled {
		return nil
	}
	toolName := toolNameOfContext(cbc)
	if toolName == "" || toolName == toolSearchToolName || toolName == toolCallToolName {
		return nil
	}
	card := lookupCardByName(abilityManagerOfAgent(cbc.Agent()), toolName)
	if card == nil {
		return nil
	}
	if card.EffectiveExposure() == tool.ToolExposureDirect {
		return nil
	}
	if nested, _ := cbc.GetExtra(nestedToolCallKey); nested == true {
		return nil
	}
	// 未发现的 deferred 工具直调 → 阻断（与 9.11a 同文案语义）。
	cbc.SetExtra("_skip_tool", true)
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	inputs.ToolResult = map[string]any{
		"error": "Deferred tool '" + toolName + "' must be discovered via tool_search before it can be called.",
	}
	return nil
}

// ──────────────────────────── 披露节渲染 ────────────────────────────

// buildDisclosureAppendix 渲染渐进披露节：分类 + 名称。
// Python: _build_disclosure_appendix（:453-540）——只展示分类目录，永不平铺
// 全量 dump。每分类行只带描述——工具名经 tool_search 发现。MCP 连接器工具
// 每 connector 一行虚拟分支；静态表未路由到的工具以 name+描述进未归类块
// （裸名无路由信号：模型得逐个 get_schema 试探）。列表 init 时冻结；
// 会话中途漂移经 delta 附件上报，不重写本节。
func (r *ToolRetrievalPromptRail) buildDisclosureAppendix(english bool) string {
	language := "cn"
	if english {
		language = "en"
	}
	lines := []string{"## Deferred Tools"}
	if english {
		lines[0] = "## Deferred Tools"
		lines = append(lines, "", disclosureIntroEn)
	} else {
		lines[0] = "## Deferred 工具（渐进式披露）"
		lines = append(lines, "", disclosureIntroZh)
	}

	snapshot := r.frozenSnapshot
	if snapshot == nil {
		return strings.Join(lines, "\n")
	}

	if len(snapshot.Categories) > 0 {
		heading := "Categories (virtual groups):"
		if !english {
			heading = "分类（虚拟目录）："
		}
		lines = append(lines, "", heading)
		for _, category := range snapshot.Categories {
			lines = append(lines, fmt.Sprintf("- [category] `%s`: %s",
				compactText(category.Path), compactText(category.DescriptionFor(language))))
		}
	}
	connectorLines, connectorNames := r.renderConnectorLines(english, language)
	lines = append(lines, connectorLines...)
	uncovered := r.uncoveredToolNames(connectorNames)
	if len(uncovered) > 0 {
		heading := "Direct get_schema tools:"
		if !english {
			heading = "直接 get_schema 工具："
		}
		lines = append(lines, "", heading)
		for _, entry := range r.uncoveredFlatEntries(uncovered) {
			lines = append(lines, fmt.Sprintf("- [tool] `%s`: %s",
				compactText(entry.Name), compactDescription(entry.Description)))
		}
	}
	if len(snapshot.Categories) == 0 && len(connectorLines) == 0 && len(uncovered) == 0 {
		if english {
			lines = append(lines, "", "No available tools.")
		} else {
			lines = append(lines, "", "当前没有可用工具。")
		}
	}
	return strings.Join(lines, "\n")
}

// renderConnectorLines 每个 MCP connector 一行目录：其永久虚拟分支。
// Python: _render_connector_lines（:542-576）——连接器工具动态、永不进静态
// 表；分支归组到 `mcp_{server}`（tool_search 的 list 可解析），描述取
// manifest 本地化描述（否则通用兜底）。已归属分类的工具防御性跳过，
// 每个 connector 的工具只被认领一次。
func (r *ToolRetrievalPromptRail) renderConnectorLines(english bool, language string) ([]string, map[string]struct{}) {
	lines := []string{}
	shown := map[string]struct{}{}
	servers := make([]string, 0, len(r.frozenConnectorGroups))
	for server := range r.frozenConnectorGroups {
		servers = append(servers, server)
	}
	sort.Strings(servers)
	for _, server := range servers {
		group := r.frozenConnectorGroups[server]
		names := []string{}
		for _, name := range group.Tools {
			if _, covered := coveredByCategories(r.frozenSnapshot)[name]; covered {
				continue
			}
			if _, shownAlready := shown[name]; shownAlready {
				continue
			}
			names = append(names, name)
		}
		if len(names) == 0 {
			continue
		}
		description := group.DescriptionFor(language)
		if description == "" {
			if english {
				description = fmt.Sprintf("Tools from MCP connector %s.", server)
			} else {
				description = fmt.Sprintf("MCP 连接器 %s 提供的工具。", server)
			}
		}
		lines = append(lines, fmt.Sprintf("- [category] `mcp_%s`: %s", server, compactText(description)))
		for _, name := range names {
			shown[name] = struct{}{}
		}
	}
	return lines, shown
}

// coveredByCategories 收集快照分类覆盖的工具名。
func coveredByCategories(snapshot *swarmtools.ToolCatalogueSnapshot) map[string]struct{} {
	covered := map[string]struct{}{}
	if snapshot == nil {
		return covered
	}
	for _, category := range snapshot.Categories {
		for _, name := range category.Tools {
			covered[name] = struct{}{}
		}
	}
	return covered
}

// uncoveredToolNames 冻结名中无分类/连接器行覆盖的集合。
// Python: _uncovered_tool_names（:556-568）。
func (r *ToolRetrievalPromptRail) uncoveredToolNames(extraCovered map[string]struct{}) []string {
	covered := coveredByCategories(r.frozenSnapshot)
	for name := range extraCovered {
		covered[name] = struct{}{}
	}
	uncovered := []string{}
	for name := range r.frozenToolNames {
		if _, ok := covered[name]; !ok {
			uncovered = append(uncovered, name)
		}
	}
	sort.Slice(uncovered, func(i, j int) bool { return strings.ToLower(uncovered[i]) < strings.ToLower(uncovered[j]) })
	return uncovered
}

// uncoveredFlatEntries 未覆盖工具名的冻结 name+描述条目。
// Python: _uncovered_flat_entries（:570-583）——冻结扁平清单（与目录同一
// 冻结时机捕获）提供压缩描述；未覆盖顺序保持确定性。
func (r *ToolRetrievalPromptRail) uncoveredFlatEntries(uncovered []string) []swarmtools.FlatToolEntry {
	snapshot := r.frozenSnapshot
	descriptions := map[string]string{}
	if snapshot != nil {
		for _, entry := range snapshot.FlatEntries {
			descriptions[entry.Name] = entry.Description
		}
	}
	out := make([]swarmtools.FlatToolEntry, 0, len(uncovered))
	for _, name := range uncovered {
		out = append(out, swarmtools.FlatToolEntry{Name: name, Description: descriptions[name]})
	}
	return out
}

// ──────────────────────────── 工具过滤 ────────────────────────────

// filterDeferredFromInputs inputs.tools 过滤为 direct ∪ 元工具。
// Python: _filter_deferred_from_inputs（:585-601）——DIRECT 工具加元工具
// （tool_call 以 exposure=DIRECT 注册故已在 direct_names）；deferred 工具
// 隐藏于广告列表，模型经广告的 tool_call 信封执行。
func (r *ToolRetrievalPromptRail) filterDeferredFromInputs(cbc *agentinterfaces.AgentCallbackContext) {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || inputs.Tools == nil {
		return
	}
	directNames := directToolNamesOf(r.agent)
	if directNames == nil {
		directNames = directToolNamesOf(cbc.Agent())
	}
	directNames[toolSearchToolName] = struct{}{}
	filtered := make([]schema.ToolInfoInterface, 0, len(inputs.Tools))
	for _, info := range inputs.Tools {
		name := info.GetName()
		if _, ok := directNames[name]; ok {
			filtered = append(filtered, info)
		}
	}
	inputs.Tools = restoreBaselineMetaSlot(filtered)
}

// directToolNamesOf AbilityManager 中 exposure=DIRECT 的工具名集合。
// Python: _get_direct_tool_names（:603-622）。
func directToolNamesOf(agent agentinterfaces.BaseAgent) map[string]struct{} {
	abilityManager := abilityManagerOfAgent(agent)
	if abilityManager == nil {
		return map[string]struct{}{}
	}
	names := map[string]struct{}{}
	for _, ability := range abilityManager.List() {
		card, ok := ability.(*tool.ToolCard)
		if !ok {
			continue
		}
		name := card.Name
		if name != "" && card.EffectiveExposure() == tool.ToolExposureDirect {
			names[name] = struct{}{}
		}
	}
	return names
}

// restoreBaselineMetaSlot tool_search/tool_call 回到基线广告位。
// Python: _restore_baseline_meta_slot（:626-659）——注册顺序把 tool_search
// 停在早段共享工具中、tool_call 靠尾；广告数组仍须读作内置基线
// （…, task_tool, tool_search, tool_call, todo_*），过滤后的 reslot 把两个
// 元工具放在 task_tool 锚点之后——无锚点则按调用序放尾。无元工具的列表
// 原对象透传；重复元工具名只保留首次注册。
func restoreBaselineMetaSlot(tools []schema.ToolInfoInterface) []schema.ToolInfoInterface {
	metaObjects := map[string]schema.ToolInfoInterface{}
	rest := make([]schema.ToolInfoInterface, 0, len(tools))
	for _, info := range tools {
		name := info.GetName()
		isMeta := false
		for _, metaName := range baselineMetaOrder {
			if name == metaName {
				isMeta = true
				break
			}
		}
		if isMeta {
			if _, exists := metaObjects[name]; !exists {
				metaObjects[name] = info
			}
			continue
		}
		rest = append(rest, info)
	}
	if len(metaObjects) == 0 {
		return tools
	}
	metas := []schema.ToolInfoInterface{}
	for _, metaName := range baselineMetaOrder {
		if info, ok := metaObjects[metaName]; ok {
			metas = append(metas, info)
		}
	}
	for index, info := range rest {
		if info.GetName() == baselineMetaAnchor {
			cut := index + 1
			merged := make([]schema.ToolInfoInterface, 0, len(rest)+len(metas))
			merged = append(merged, rest[:cut]...)
			merged = append(merged, metas...)
			merged = append(merged, rest[cut:]...)
			return merged
		}
	}
	rest = append(rest, metas...)
	return rest
}

// ──────────────────────────── 漂移同步（复刻 ProgressiveToolRail 契约） ────

// syncDeferredToolCatalog 发布 deferred 目录变化（added/updated/removed）。
// Python: _sync_deferred_tool_catalog（:662-934）——冻结的提示词节是会话
// 初始目录，永不重写。每轮把 live deferred 注册表与上次物化目录对比；
// 漂移以 delta 附件上报，语言变化、上下文压缩历史丢失或连续 delta 达
// _CATALOG_SNAPSHOT_INTERVAL 时切换全量 snapshot——与 openjiuwen
// ProgressiveToolRail._sync_deferred_tool_attachment 同契约。
func (r *ToolRetrievalPromptRail) syncDeferredToolCatalog(cbc *agentinterfaces.AgentCallbackContext, language string) {
	sess := cbc.Session()
	manager := r.attachmentManager
	if manager == nil && cbc.Agent() != nil {
		if m, ok := cbc.Agent().(interface{ PromptAttachmentManager() *prompts.Manager }); ok {
			manager = m.PromptAttachmentManager()
		}
	}
	if sess == nil || manager == nil {
		return
	}
	writer := manager.BindContext(cbc)
	if writer.SessionID() == "" {
		return
	}

	currentTools, currentFingerprints := r.collectLiveCatalog()
	catalogState := catalogStateOfSession(sess)

	initialTools := r.initialStateToolsOf(catalogState, currentTools)
	initialFingerprints := initialFingerprintsOf(catalogState, currentFingerprints)
	previousTools := previousStateToolsOf(catalogState)
	previousFingerprints := previousFingerprintsOf(catalogState)
	initialized := catalogState["initialized"] == true
	previousLanguage := stringOfState(catalogState["language"])
	version := intOfState(catalogState["version"], 1)
	deltaCount := intOfState(catalogState["delta_count"], 0)

	// 会话状态持久，但热重载或 adapter 重建可能给 rail 一个全新 Session
	// 而附件管理器仍持有最近物化目录。恢复该目录为 diff 基线；否则
	// removal/addition 会以新版本每轮重复推送（或漏推）。
	if existing := firstCatalogAttachment(writer); existing != nil {
		if recovered, ok := normalizeCatalogMap(mapValueOf(existing.Metadata[catalogMetadataTools])); ok {
			previousTools = recovered
			initialized = true
			if fingerprints, ok := normalizeCatalogMap(mapValueOf(existing.Metadata[catalogMetadataFingerprints])); ok {
				previousFingerprints = fingerprints
			}
			if value, ok := existing.Metadata["catalog_language"].(string); ok && value != "" {
				previousLanguage = value
			}
			if value, ok := existing.Metadata["catalog_version"].(int); ok && value > version {
				version = value
			}
			if value, ok := existing.Metadata["delta_count"].(int); ok && value != 0 {
				deltaCount = value
			}
		}
	}

	baselineTools := previousTools
	baselineFingerprints := previousFingerprints
	if !initialized {
		baselineTools = initialTools
		baselineFingerprints = initialFingerprints
	}
	added := map[string]string{}
	updated := map[string]string{}
	for name, description := range currentTools {
		existing, ok := baselineTools[name]
		if !ok {
			added[name] = description
			continue
		}
		if existing != description {
			updated[name] = description
			continue
		}
		// 指纹只在双方都有时比较：冻结基线有描述无指纹，任一侧指纹失败
		// 不得读作变化。
		baselineFp, hasBaseline := baselineFingerprints[name]
		currentFp, hasCurrent := currentFingerprints[name]
		if hasBaseline && hasCurrent && baselineFp != currentFp {
			updated[name] = description
		}
	}
	removed := []string{}
	for name := range baselineTools {
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

	var content string
	var mode string
	var nextDeltaCount int

	if !initialized {
		// 冻结提示词节就是基线。启动与首次模型调用之间注册有变，则把该
		// 差异作为首条附件上报；否则不发布。
		if !changed {
			r.writeCatalogState(sess, catalogState, version, 0, language,
				initialTools, initialFingerprints, currentTools, currentFingerprints)
			return
		}
		version = nextVersion
		nextDeltaCount = 1
		mode = "delta"
		content = sections.RenderDeferredToolCatalogDelta(added, updated, removed, language, version)
	} else {
		forceSnapshot := !r.hasAttachmentHistory(writer, cbc) ||
			previousLanguage != language ||
			deltaCount >= catalogSnapshotInterval
		if !forceSnapshot && !changed {
			return
		}
		version = nextVersion
		if forceSnapshot {
			content = sections.RenderDeferredToolCatalogSnapshot(currentTools, language, version)
			mode = "snapshot"
			nextDeltaCount = 0
		} else {
			content = sections.RenderDeferredToolCatalogDelta(added, updated, removed, language, version)
			mode = "delta"
			nextDeltaCount = deltaCount + 1
		}
	}

	if _, err := writer.AddSection(
		catalogAttachmentSection,
		content,
		prompts.AttachmentTool,
		catalogAttachmentSource,
		prompts.WithAttachmentPriority(disclosureSectionPriority),
		prompts.WithAttachmentContentKind("text/markdown"),
		prompts.WithAttachmentMetadata(map[string]any{
			"catalog_version":           version,
			"catalog_mode":              mode,
			"catalog_language":          language,
			"delta_count":               nextDeltaCount,
			catalogMetadataTools:        currentTools,
			catalogMetadataFingerprints: currentFingerprints,
		}),
	); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "tool_retrieval_catalog_attachment").
			Err(err).
			Msg("[ToolRetrievalPromptRail] deferred-catalogue attachment failed")
		return
	}

	r.writeCatalogState(sess, catalogState, version, nextDeltaCount, language,
		initialTools, initialFingerprints, currentTools, currentFingerprints)
}

// frozenCatalogDescriptions 冻结目录转 name → 压缩描述。
// Python: _frozen_catalog_descriptions（:936-948）——同步的初始基线必须是
// 模型在提示词节里实际看到的目录（工具包 live 记录冻结而来），不是首次
// 同步时注册表恰好持有的集合。
func (r *ToolRetrievalPromptRail) frozenCatalogDescriptions() map[string]string {
	snapshot := r.frozenSnapshot
	out := map[string]string{}
	if snapshot == nil {
		return out
	}
	for _, entry := range snapshot.FlatEntries {
		out[entry.Name] = compactDescription(entry.Description)
	}
	return out
}

// collectLiveCatalog 读 live deferred 目录与 schema 指纹。
// Python: _collect_live_catalog（:951-982）——描述压缩到提示词预算（附件
// 列目录节同款短形）；指纹来自 tool_discovery.ToolFingerprint，原地 schema
// 编辑不改压缩描述也计入 update。
func (r *ToolRetrievalPromptRail) collectLiveCatalog() (map[string]string, map[string]string) {
	toolkit := r.toolkit
	if toolkit == nil {
		return map[string]string{}, map[string]string{}
	}
	infos := toolkit.DeferredToolInfos()
	descriptions := map[string]string{}
	for name, info := range infos {
		if strings.TrimSpace(name) == "" {
			continue
		}
		descriptions[name] = compactDescription(swarmtools.DiscoveryDescription(info, toolkit.Language()))
	}
	fingerprints := map[string]string{}
	for name, info := range infos {
		fingerprints[name] = tool_discovery.ToolFingerprint(info)
	}
	return sortCatalog(descriptions), sortCatalog(fingerprints)
}

// writeCatalogState 写回目录状态。Python: _write_catalog_state（:985-1010）。
func (r *ToolRetrievalPromptRail) writeCatalogState(
	sess sessioninterfaces.SessionFacade,
	state map[string]any,
	version, deltaCount int,
	language string,
	initialTools, initialFingerprints, tools, fingerprints map[string]string,
) {
	if sess == nil {
		return
	}
	merged := map[string]any{}
	for key, value := range state {
		merged[key] = value
	}
	merged["initialized"] = true
	merged["version"] = version
	merged["delta_count"] = deltaCount
	merged["language"] = language
	merged["initial_tools"] = initialTools
	merged[catalogMetadataTools+"_initial"] = initialFingerprints
	merged["tools"] = tools
	merged["fingerprints"] = fingerprints
	sess.UpdateState(map[string]any{catalogStateKey: merged})
}

// initialStateToolsOf 初始目录（缺省用当前播种；冻结目录非空时优先）。
// Python :716-721 缺省播种优先冻结目录——注册竞速报为 addition 而非吞进基线。
func (r *ToolRetrievalPromptRail) initialStateToolsOf(state map[string]any, current map[string]string) map[string]string {
	if normalized, ok := normalizeCatalogMap(mapValueOf(state["initial_tools"])); ok {
		return normalized
	}
	if frozen := r.frozenCatalogDescriptions(); len(frozen) > 0 {
		return frozen
	}
	normalized, _ := normalizeCatalogMap(current)
	return normalized
}

// initialFingerprintsOf 初始指纹（缺省用当前播种）。
func initialFingerprintsOf(state map[string]any, current map[string]string) map[string]string {
	if normalized, ok := normalizeCatalogMap(mapValueOf(state["initial_fingerprints"])); ok {
		return normalized
	}
	normalized, _ := normalizeCatalogMap(current)
	return normalized
}

// previousStateToolsOf 上次已发布目录。
func previousStateToolsOf(state map[string]any) map[string]string {
	if normalized, ok := normalizeCatalogMap(mapValueOf(state["tools"])); ok {
		return normalized
	}
	return map[string]string{}
}

// previousFingerprintsOf 上次已发布指纹。
func previousFingerprintsOf(state map[string]any) map[string]string {
	if normalized, ok := normalizeCatalogMap(mapValueOf(state["fingerprints"])); ok {
		return normalized
	}
	return map[string]string{}
}

// firstCatalogAttachment 读当前物化的 deferred 目录附件。
// Python: _existing_catalog_attachment（:1013-1033）。
func firstCatalogAttachment(w *prompts.ContextWriter) *prompts.Attachment {
	if w == nil || w.Manager() == nil || w.SessionID() == "" {
		return nil
	}
	items, err := w.Manager().ListByFilter(prompts.AttachmentFilter{
		SessionID: w.SessionID(),
		Section:   catalogAttachmentSection,
		Source:    catalogAttachmentSource,
	})
	if err != nil || len(items) == 0 {
		return nil
	}
	return items[0]
}

// hasAttachmentHistory 上下文是否仍持有附件快照。
// Python: _has_attachment_history（:1036-1058）——上下文压缩或重建后历史
// 快照丢失；下一次更新必须是全量 snapshot，否则模型收到孤儿 delta。
func (r *ToolRetrievalPromptRail) hasAttachmentHistory(w *prompts.ContextWriter, cbc *agentinterfaces.AgentCallbackContext) bool {
	if w == nil || w.Manager() == nil {
		return false
	}
	modelCtx := cbc.ModelContext()
	if modelCtx == nil {
		return false
	}
	return w.Manager().HasHistorySnapshot(modelCtx, w.SessionID())
}

// ──────────────────────────── 状态/文本助手 ────────────────────────────

// catalogStateOfSession 读取会话目录状态。
func catalogStateOfSession(sess sessioninterfaces.SessionFacade) map[string]any {
	raw, err := sess.GetState(sessionStateKey(catalogStateKey))
	if err != nil {
		return map[string]any{}
	}
	typed, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return typed
}

// normalizeCatalogMap 归一化目录：None=不可恢复（缺省播种路径）；空字典是
// 有效目录（空 deferred 注册表），不得与缺失状态混淆。
// Python: _normalize_catalog（:1090-1106）。
func normalizeCatalogMap(catalog map[string]string) (map[string]string, bool) {
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

// mapValueOf 任意值转字符串目录。
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

// sortCatalog 排序目录（确定输出序）。
func sortCatalog(catalog map[string]string) map[string]string {
	out := make(map[string]string, len(catalog))
	for name, description := range catalog {
		out[name] = description
	}
	return out
}

// intOfState 读整数值（缺省回落）。
func intOfState(value any, fallback int) int {
	if typed, ok := value.(int); ok {
		return typed
	}
	return fallback
}

// stringOfState 读字符串值。
func stringOfState(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

// compactText 空白折叠 + 反引号转义。Python: _compact_text（:1060-1062）。
func compactText(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "`", "\\`")), " ")
}

// compactDescription 描述压缩到提示词预算。
// Python: _compact_description（:1109-1130）——保留第一句或截到
// maxEntryDescChars（powershell/cron 的长文档不得主导提示词）。
func compactDescription(description string) string {
	compact := compactText(description)
	runes := []rune(compact)
	if len(runes) <= maxEntryDescChars {
		return compact
	}
	// Python 按 str 字符索引；Go 按 rune 处理（"。" 等 3 字节符不可按字节切）。
	sentenceEndings := "。.！!？?"
	firstEnding := -1
	for index, char := range runes {
		if strings.ContainsRune(sentenceEndings, char) && index > 0 {
			firstEnding = index
			break
		}
	}
	if 0 < firstEnding && firstEnding < maxEntryDescChars {
		return string(runes[:firstEnding+1])
	}
	return strings.TrimRight(string(runes[:maxEntryDescChars-1]), " \t\n") + "…"
}

// ──────────────────────────── 非导出助手（上下文/管理器访问） ────────────────

// abilityManagerOfAgent 从 agent 取能力管理器（nil 安全）。
func abilityManagerOfAgent(agent agentinterfaces.BaseAgent) agentinterfaces.AbilityManagerInterface {
	if agent == nil {
		return nil
	}
	return agent.AbilityManager()
}

// lookupCardByName 从注册表按名取工具卡片（Ability 即卡片的具体形态）。
func lookupCardByName(abilityManager agentinterfaces.AbilityManagerInterface, name string) *tool.ToolCard {
	if abilityManager == nil {
		return nil
	}
	card, _ := abilityManager.Get(name).(*tool.ToolCard)
	return card
}

// toolNameOfContext 读取回调上下文里的工具名。
func toolNameOfContext(cbc *agentinterfaces.AgentCallbackContext) string {
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

// failedResult 判定执行结果是否为拦截/失败路径。
func failedResult(result any) bool {
	switch v := result.(type) {
	case map[string]any:
		if errText, ok := v["error"]; ok && errText != nil {
			if success, ok := v["success"]; !ok || success != true {
				return true
			}
		}
	case string:
		return true
	}
	return false
}

// sessionStateKey 会话状态键包装。
func sessionStateKey(path string) sessionstate.StateKey {
	return sessionstate.StringKey(path)
}
