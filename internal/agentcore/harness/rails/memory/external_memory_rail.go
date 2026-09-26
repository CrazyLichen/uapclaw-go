package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	ext "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/external"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// providerTool 将 MemoryProvider 的 ToolSchema 桥接为 tool.Tool 接口。
//
// 每个 providerTool 实例对应 Provider 提供的一个工具，Invoke 委托给
// provider.HandleToolCall()，Stream 不支持。
// 对齐 Python: LocalFunction(card=tool_card, func=_tool_func) 注册模式。
type providerTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// provider 外部记忆提供者
	provider ext.MemoryProvider
	// toolName 原始工具名称（Provider Schema 中的 name）
	toolName string
}

// ExternalMemoryRail 外部记忆护栏，桥接 MemoryProvider 到 Agent 生命周期。
//
// 生命周期:
//   1. Init: 注册 Provider 工具 + 注入 system_prompt_block
//   2. BeforeInvoke: 调 provider.Initialize()
//   3. BeforeModelCall: 调 provider.Prefetch() 注入记忆上下文
//   4. AfterInvoke: 调 provider.SyncTurn()（序列化 + 熔断器）
//   5. Uninit: 注销工具 + provider.Shutdown()
//
// Python: ExternalMemoryRail (openjiuwen/harness/rails/memory/external_memory_rail.py)
type ExternalMemoryRail struct {
	rails.DeepAgentRail
	// provider 外部记忆提供者
	provider ext.MemoryProvider
	// userID 用户标识
	userID string
	// scopeID 作用域标识
	scopeID string
	// sessionID 会话标识
	sessionID string
	// initialized 是否已初始化
	initialized bool
	// ownedToolNames 本 Rail 注册到 ability_manager 的工具名称集合
	ownedToolNames map[string]struct{}
	// ownedToolIDs 本 Rail 注册到 resource_mgr 的工具 ID 集合
	ownedToolIDs map[string]struct{}
	// systemPromptBuilder 系统提示词构建器引用
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// prefetchCache 预取缓存（指针区分空串和未缓存）
	prefetchCache *string
	// prefetchInvokeID 预取缓存对应的 invoke ID
	prefetchInvokeID uintptr
	// syncMu 保护 syncTurn 串行
	syncMu sync.Mutex
	// syncDone 上一次 sync 完成信号
	syncDone chan struct{}
	// syncConsecutiveFailures syncTurn 连续失败次数
	syncConsecutiveFailures int
	// syncBreakerUntil 熔断器冷却截止时间
	syncBreakerUntil time.Time
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// externalMemoryRailPriority 外部记忆护栏优先级
	// Python: ExternalMemoryRail.priority = 75
	externalMemoryRailPriority = 75
	// externalMemoryPrefetchTimeout 预取超时时间
	// Python: ExternalMemoryRail.PREFETCH_TIMEOUT = 5.0
	externalMemoryPrefetchTimeout = 5 * time.Second
	// externalMemorySyncBreakerThreshold 熔断器连续失败阈值
	// Python: _SYNC_BREAKER_THRESHOLD = 5
	externalMemorySyncBreakerThreshold = 5
	// externalMemorySyncBreakerCooldown 熔断器冷却时间
	// Python: _SYNC_BREAKER_COOLDOWN = 120.0
	externalMemorySyncBreakerCooldown = 120 * time.Second
	// externalMemoryShutdownTimeout 关闭超时
	// Python: _shutdown_with_timeout timeout=10.0
	externalMemoryShutdownTimeout = 10 * time.Second
	// externalMemoryPrefetchSection 预取结果 section 名称
	// Python: EXTERNAL_MEMORY_PREFETCH_SECTION = "external_memory_prefetch"
	externalMemoryPrefetchSection = "external_memory_prefetch"
	// externalMemorySyncWaitTimeout 等待上一次 SyncTurn 完成的超时
	// Python: await asyncio.wait_for(asyncio.shield(self._sync_task), timeout=5.0)
	externalMemorySyncWaitTimeout = 5 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 ExternalMemoryRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ExternalMemoryRail)(nil)

var extMemoryLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExternalMemoryRail 创建 ExternalMemoryRail 实例。
// Python: ExternalMemoryRail.__init__(provider, user_id, scope_id, session_id)
func NewExternalMemoryRail(
	provider ext.MemoryProvider,
	userID, scopeID, sessionID string,
) *ExternalMemoryRail {
	r := &ExternalMemoryRail{
		DeepAgentRail:  *rails.NewDeepAgentRail(),
		provider:       provider,
		userID:         userID,
		scopeID:        scopeID,
		sessionID:      sessionID,
		ownedToolNames: make(map[string]struct{}),
		ownedToolIDs:   make(map[string]struct{}),
		syncDone:       make(chan struct{}, 1),
	}
	// 初始标记 syncDone 已完成
	r.syncDone <- struct{}{}
	r.WithPriority(externalMemoryRailPriority)
	return r
}

// Init 注册 Provider 工具 + 注入 system_prompt_block。
// Python: ExternalMemoryRail.init(agent)
func (r *ExternalMemoryRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 注册 Provider 工具
	r.registerProviderTools(agent)

	// 注入 Provider 的静态系统提示词块
	// Python: prompt_block = self._provider.system_prompt_block()
	//         if prompt_block: section = build_external_memory_section(prompt_block, language=lang)
	if r.systemPromptBuilder != nil {
		promptBlock := r.provider.SystemPromptBlock()
		if promptBlock != "" {
			lang := r.systemPromptBuilder.Language()
			section := sections.BuildExternalMemorySection(promptBlock, lang)
			if section != nil {
				r.systemPromptBuilder.AddSection(*section)
			}
		}
	}

	return nil
}

// Uninit 注销工具 + 关闭 Provider。
// Python: ExternalMemoryRail.uninit(agent)
func (r *ExternalMemoryRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// 1. 从 ability_manager 移除工具
	// Python: for tool_name in list(self._owned_tool_names): agent.ability_manager.remove(tool_name)
	am := agent.AbilityManager()
	if am != nil {
		for name := range r.ownedToolNames {
			func(name string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_uninit").
							Str("tool_name", name).
							Msgf("从 ability_manager 移除工具失败: %v", rec)
					}
				}()
				am.Remove(name)
			}(name)
		}
	}

	// 2. 从 resource_mgr 移除工具
	// Python: for tool_id in list(self._owned_tool_ids): Runner.resource_mgr.remove_tool(tool_id)
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		for toolID := range r.ownedToolIDs {
			func(toolID string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_uninit").
							Str("tool_id", toolID).
							Msgf("从 resource_mgr 移除工具失败: %v", rec)
					}
				}()
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}(toolID)
		}
	}

	// 3. 清理状态
	r.ownedToolNames = make(map[string]struct{})
	r.ownedToolIDs = make(map[string]struct{})
	r.initialized = false

	// 4. 从 systemPromptBuilder 移除 section
	// Python: self.system_prompt_builder.remove_section(SectionName.EXTERNAL_MEMORY)
	//         self.system_prompt_builder.remove_section(EXTERNAL_MEMORY_PREFETCH_SECTION)
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionExternalMemory)
		r.systemPromptBuilder.RemoveSection(externalMemoryPrefetchSection)
		r.systemPromptBuilder = nil
	}

	// 5. 关闭 Provider（带超时，对齐 Python: await asyncio.wait_for(self._provider.shutdown(), timeout=10.0)）
	shutdownCtx, cancel := context.WithTimeout(context.Background(), externalMemoryShutdownTimeout)
	defer cancel()
	if err := r.provider.Shutdown(shutdownCtx); err != nil {
		logger.Warn(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_uninit").
			Err(err).
			Msg("Provider shutdown 失败")
	}

	return nil
}

// BeforeInvoke 调 provider.Initialize()。
// Python: ExternalMemoryRail.before_invoke(ctx)
func (r *ExternalMemoryRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 清空预取缓存
	// Python: self._prefetch_cache = None; self._prefetch_invoke_id = id(ctx)
	r.prefetchCache = nil
	r.prefetchInvokeID = 0

	// Python: if not self._initialized:
	//             await self._provider.initialize(user_id=..., scope_id=..., session_id=...)
	if !r.initialized {
		if err := r.provider.Initialize(ctx,
			ext.WithUserID(r.userID),
			ext.WithScopeID(r.scopeID),
			ext.WithSessionID(r.sessionID),
		); err != nil {
			logger.Error(extMemoryLogComponent).
				Str("event_type", "external_memory_rail_before_invoke").
				Str("provider", r.provider.Name()).
				Err(err).
				Msg("Provider 初始化失败")
			return nil // 不阻断，降级运行
		}
		r.initialized = true
		logger.Info(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_before_invoke").
			Str("provider", r.provider.Name()).
			Msg("Provider 初始化成功")
	}

	return nil
}

// BeforeModelCall 调 provider.Prefetch() 并注入记忆上下文到系统提示词。
// Python: ExternalMemoryRail.before_model_call(ctx)
func (r *ExternalMemoryRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: if not self._initialized or self.system_prompt_builder is None: return
	if !r.initialized || r.systemPromptBuilder == nil {
		return nil
	}

	// 移除旧的预取 section
	// Python: self.system_prompt_builder.remove_section(EXTERNAL_MEMORY_PREFETCH_SECTION)
	r.systemPromptBuilder.RemoveSection(externalMemoryPrefetchSection)

	// 检查预取缓存
	// Python: invoke_id = id(ctx)
	//         if self._prefetch_invoke_id == invoke_id and self._prefetch_cache is not None:
	invokeID := cbcRefID(cbc)
	if r.prefetchInvokeID == invokeID && r.prefetchCache != nil {
		rawContext := *r.prefetchCache
		r.injectMemoryContext(rawContext)
		return nil
	}

	// 解析用户查询
	// Python: query = self._resolve_user_text_for_memory(ctx)
	query := resolveUserTextForMemory(cbc)
	if query == "" {
		return nil
	}

	// 带超时的预取
	// Python: raw_context = await asyncio.wait_for(
	//             self._provider.prefetch(query, user_id=..., scope_id=...),
	//             timeout=self.PREFETCH_TIMEOUT)
	prefetchCtx, cancel := context.WithTimeout(ctx, externalMemoryPrefetchTimeout)
	defer cancel()

	rawContext, err := r.provider.Prefetch(prefetchCtx, query,
		ext.WithUserID(r.userID),
		ext.WithScopeID(r.scopeID),
	)
	if err != nil {
		if ctx.Err() == nil && prefetchCtx.Err() != nil {
			// prefetch 自身超时
			logger.Warn(extMemoryLogComponent).
				Str("event_type", "external_memory_rail_prefetch_timeout").
				Str("provider", r.provider.Name()).
				Msg("Prefetch 超时")
		} else {
			logger.Error(extMemoryLogComponent).
				Str("event_type", "external_memory_rail_prefetch_failed").
				Str("provider", r.provider.Name()).
				Err(err).
				Msg("Prefetch 失败")
		}
		return nil
	}

	// 缓存结果
	// Python: self._prefetch_cache = raw_context; self._prefetch_invoke_id = invoke_id
	r.prefetchCache = &rawContext
	r.prefetchInvokeID = invokeID

	r.injectMemoryContext(rawContext)
	return nil
}

// AfterInvoke 调 provider.SyncTurn()（序列化 + 熔断器保护）。
// Python: ExternalMemoryRail.after_invoke(ctx)
func (r *ExternalMemoryRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: if not self._initialized: return
	if !r.initialized {
		return nil
	}

	// 跳过后台运行
	// Python: if self._is_background_run(ctx): return
	if isBackgroundRun(cbc) {
		return nil
	}

	// 熔断器检查
	// Python: if self._sync_consecutive_failures >= _SYNC_BREAKER_THRESHOLD:
	//             if time.monotonic() < self._sync_breaker_until: return
	//             self._sync_consecutive_failures = 0
	if r.syncConsecutiveFailures >= externalMemorySyncBreakerThreshold {
		if time.Now().Before(r.syncBreakerUntil) {
			return nil // 熔断中
		}
		r.syncConsecutiveFailures = 0 // 冷却完毕，重置
	}

	// 解析用户查询和助手输出
	// Python: query = self._resolve_user_text_for_memory(ctx)
	//         output = self._extract_assistant_output(ctx)
	query := resolveUserTextForMemory(cbc)
	output := extractAssistantOutput(cbc)
	if query == "" || output == "" {
		return nil
	}

	// 序列化：等待上一次 sync 完成
	// Python: if self._sync_task and not self._sync_task.done():
	//             await asyncio.wait_for(asyncio.shield(self._sync_task), timeout=5.0)
	r.syncMu.Lock()
	select {
	case <-r.syncDone:
		// 上一次已完成
	case <-time.After(externalMemorySyncWaitTimeout):
		logger.Warn(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_sync_wait_timeout").
			Msg("等待上一次 SyncTurn 超时 5s，继续执行")
	}

	// 启动新的 sync goroutine
	// Python: self._sync_task = asyncio.create_task(_serialized_sync())
	done := make(chan struct{})
	r.syncDone = done
	r.syncMu.Unlock()

	go func() {
		defer close(done)
		err := r.provider.SyncTurn(ctx, query, output,
			ext.WithUserID(r.userID),
			ext.WithScopeID(r.scopeID),
			ext.WithSessionID(r.sessionID),
		)
		if err != nil {
			r.syncMu.Lock()
			r.syncConsecutiveFailures++
			if r.syncConsecutiveFailures >= externalMemorySyncBreakerThreshold {
				r.syncBreakerUntil = time.Now().Add(externalMemorySyncBreakerCooldown)
				logger.Warn(extMemoryLogComponent).
					Str("event_type", "external_memory_rail_sync_breaker_open").
					Int("consecutive_failures", r.syncConsecutiveFailures).
					Dur("cooldown", externalMemorySyncBreakerCooldown).
					Err(err).
					Msg("SyncTurn 熔断器开启")
			} else {
				logger.Warn(extMemoryLogComponent).
					Str("event_type", "external_memory_rail_sync_failed").
					Int("consecutive_failures", r.syncConsecutiveFailures).
					Err(err).
					Msg("SyncTurn 失败")
			}
			r.syncMu.Unlock()
		} else {
			r.syncMu.Lock()
			r.syncConsecutiveFailures = 0
			r.syncMu.Unlock()
		}
	}()

	return nil
}

// GetCallbacks 覆盖基类回调映射。
// Python: ExternalMemoryRail 隐式覆盖 before_invoke/before_model_call/after_invoke
func (r *ExternalMemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterInvoke] = func(ctx context.Context, railCtx any) error {
		return r.AfterInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Card 返回工具卡片。
func (pt *providerTool) Card() *tool.ToolCard { return pt.card }

// Invoke 执行工具调用，委托给 provider.HandleToolCall()。
// 对齐 Python: _tool_func(captured_name=captured_name, captured_provider=captured_provider, **kwargs)
func (pt *providerTool) Invoke(ctx context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	resultStr, err := pt.provider.HandleToolCall(ctx, pt.toolName, inputs)
	if err != nil {
		return nil, err
	}
	// 尝试 JSON 解析，对齐 Python: result = json.loads(result_str)
	var result map[string]any
	if err := json.Unmarshal([]byte(resultStr), &result); err == nil {
		return result, nil
	}
	// 解析失败返回包装结果，对齐 Python: {"result": result_str}
	return map[string]any{"result": resultStr}, nil
}

// Stream 不支持流式调用。
func (pt *providerTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// newProviderTool 从 ToolSchema 创建 providerTool。
func newProviderTool(provider ext.MemoryProvider, schema ext.ToolSchema) *providerTool {
	// Python: tool_id = f"external_memory_{self._provider.name}_{tool_name}"
	toolID := fmt.Sprintf("external_memory_%s_%s", provider.Name(), schema.Name)
	card := &tool.ToolCard{
		BaseCard: cschema.BaseCard{
			ID:          toolID,
			Name:        schema.Name,
			Description: schema.Description,
		},
		InputParams: schemaParamsToParamSlice(schema.Parameters),
	}
	return &providerTool{
		card:     card,
		provider: provider,
		toolName: schema.Name,
	}
}

// schemaParamsToParamSlice 将 ToolSchema 的 Parameters (map[string]any) 转换为 []*schema.Param。
// 从 parameters.properties 中提取字段定义。
func schemaParamsToParamSlice(params map[string]any) []*cschema.Param {
	if params == nil {
		return nil
	}
	props, _ := params["properties"].(map[string]any)
	if props == nil {
		return nil
	}
	requiredMap := make(map[string]bool)
	if req, ok := params["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				requiredMap[s] = true
			}
		}
	}

	var result []*cschema.Param
	for name, def := range props {
		p := &cschema.Param{
			Name:     name,
			Required: requiredMap[name],
		}
		if defMap, ok := def.(map[string]any); ok {
			if desc, ok := defMap["description"].(string); ok {
				p.Description = desc
			}
			if typ, ok := defMap["type"].(string); ok {
				p.Type = paramTypeFromString(typ)
			}
		}
		result = append(result, p)
	}
	return result
}

// paramTypeFromString 将字符串类型名转换为 cschema.ParamType。
func paramTypeFromString(s string) cschema.ParamType {
	switch s {
	case "string":
		return cschema.ParamTypeString
	case "boolean":
		return cschema.ParamTypeBoolean
	case "integer":
		return cschema.ParamTypeInteger
	case "number":
		return cschema.ParamTypeNumber
	case "array":
		return cschema.ParamTypeArray
	case "object":
		return cschema.ParamTypeObject
	default:
		return cschema.ParamTypeString
	}
}

// registerProviderTools 将 Provider 的工具 Schema 注册到 Agent。
// Python: _register_provider_tools(agent)
func (r *ExternalMemoryRail) registerProviderTools(agent agentinterfaces.BaseAgent) {
	am := agent.AbilityManager()
	if am == nil {
		logger.Warn(extMemoryLogComponent).
			Str("event_type", "external_memory_rail_register_tools").
			Msg("Agent 无 ability_manager，跳过工具注册")
		return
	}

	// Python: schemas = self._provider.get_tool_schemas()
	schemas := r.provider.GetToolSchemas()
	resourceMgr := runner.GetResourceMgr()

	for _, schema := range schemas {
		toolName := schema.Name
		// Python: if not tool_name: continue
		if toolName == "" {
			continue
		}

		pt := newProviderTool(r.provider, schema)

		// 注册到 resource_mgr
		// Python: existing = Runner.resource_mgr.get_tool(tool_id)
		//         if existing is None: add_result = Runner.resource_mgr.add_tool(local_func)
		if resourceMgr != nil {
			func(pt *providerTool) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_register_tools").
							Str("tool_id", pt.Card().ID).
							Msgf("注册工具到 resource_mgr 失败: %v", rec)
					}
				}()
				existing, err := resourceMgr.GetTool([]string{pt.Card().ID})
				if err != nil || len(existing) == 0 {
					if addErr := resourceMgr.AddTool(pt); addErr != nil {
						logger.Warn(extMemoryLogComponent).
							Str("event_type", "external_memory_rail_register_tools").
							Str("tool_id", pt.Card().ID).
							Err(addErr).
							Msg("注册工具到 resource_mgr 失败")
					} else {
						r.ownedToolIDs[pt.Card().ID] = struct{}{}
					}
				}
			}(pt)
		}

		// 注册到 ability_manager
		// Python: result = agent.ability_manager.add(tool_card)
		//         if result.added: self._owned_tool_names.add(tool_name)
		func(pt *providerTool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(extMemoryLogComponent).
						Str("event_type", "external_memory_rail_register_tools").
						Str("tool_name", pt.Card().Name).
						Msgf("注册工具到 ability_manager 失败: %v", rec)
				}
			}()
			result := am.Add(pt.Card())
			if result.Added {
				r.ownedToolNames[pt.Card().Name] = struct{}{}
				logger.Info(extMemoryLogComponent).
					Str("event_type", "external_memory_rail_register_tools").
					Str("tool_name", pt.Card().Name).
					Msg("注册工具成功")
			}
		}(pt)
	}
}

// injectMemoryContext 将记忆上下文注入系统提示词。
func (r *ExternalMemoryRail) injectMemoryContext(rawContext string) {
	if rawContext == "" || r.systemPromptBuilder == nil {
		return
	}
	// Python: fenced = self._build_memory_context_block(raw_context)
	//         section = PromptSection(name=EXTERNAL_MEMORY_PREFETCH_SECTION, content={lang: fenced}, priority=55)
	fenced := buildMemoryContextBlock(rawContext)
	lang := r.systemPromptBuilder.Language()
	section := saprompt.PromptSection{
		Name:     externalMemoryPrefetchSection,
		Content:  map[string]string{lang: fenced},
		Priority: 55,
	}
	r.systemPromptBuilder.AddSection(section)
}

// resolveUserTextForMemory 从回调上下文中解析用户文本。
// Python: _resolve_user_text_for_memory(ctx)
//
// 优先级（对齐 Python 实现）：
//  1. InvokeInputs.Query.PlainText()
//  2. ModelCallInputs.Messages 中最后一条 user message content
//  3. 空字符串（记录 warn 日志）
func resolveUserTextForMemory(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs := cbc.Inputs()

	// Python: # Prioritize the query field
	//         if hasattr(ctx.inputs, "query"):
	//             q = ctx.inputs.query
	//             if isinstance(q, str) and q.strip(): return q.strip()
	if invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs); ok {
		if invokeInputs.Query != nil {
			if text := invokeInputs.Query.PlainText(); strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		}
	}

	// BaseMessage 是接口，方法为 GetRole()/GetContent()
	// Python: for msg in reversed(messages):
	//             if role == "user" and content.strip(): return content.strip()
	if modelInputs, ok := inputs.(*agentinterfaces.ModelCallInputs); ok {
		for i := len(modelInputs.Messages) - 1; i >= 0; i-- {
			msg := modelInputs.Messages[i]
			if msg.GetRole() == schema.RoleTypeUser {
				text := msg.GetContent().Text()
				if strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
			}
		}
	}

	// Python: logger.warning("[ExternalMemoryRail] Cannot resolve user text for memory. ...")
	logger.Warn(extMemoryLogComponent).
		Str("event_type", "external_memory_rail_resolve_user_text").
		Str("input_kind", inputs.EventKind()).
		Msg("无法从回调上下文解析用户文本")

	return ""
}

// extractAssistantOutput 从回调上下文中提取助手输出。
// Python: _extract_assistant_output(ctx)
//
// 从 InvokeInputs.Result 中尝试多个 key: output, message, content, text, response
// 对齐 Python 的 output_keys 列表和嵌套 message.content 处理。
func extractAssistantOutput(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs := cbc.Inputs()
	invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs)
	if !ok {
		return ""
	}
	// Python: if not hasattr(ctx.inputs, "result"): return ""
	if invokeInputs.Result == nil {
		return ""
	}

	// Python: output_keys = ["output", "message", "content", "text", "response"]
	outputKeys := []string{"output", "message", "content", "text", "response"}
	for _, key := range outputKeys {
		if value, exists := invokeInputs.Result[key]; exists {
			if str, ok := value.(string); ok && strings.TrimSpace(str) != "" {
				return strings.TrimSpace(str)
			}
			// Python: # message may be nested structure
			//         if isinstance(value, dict) and "content" in value:
			if nested, ok := value.(map[string]any); ok {
				if content, exists := nested["content"]; exists {
					if str, ok := content.(string); ok && strings.TrimSpace(str) != "" {
						return strings.TrimSpace(str)
					}
				}
			}
		}
	}

	return ""
}

// isBackgroundRun 判断是否为后台运行（心跳/cron）。
// Python: _is_background_run(ctx)
//
// 对齐 Python:
//
//	for method_name in ("is_heartbeat", "is_cron"):
//	    method = getattr(inputs, method_name, None)
//	    if callable(method) and method(): return True
//	run_kind = getattr(inputs, "run_kind", None)
//	if run_kind in (RunKind.HEARTBEAT, RunKind.CRON): return True
func isBackgroundRun(cbc *agentinterfaces.AgentCallbackContext) bool {
	inputs := cbc.Inputs()
	invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs)
	if !ok {
		return false
	}
	return invokeInputs.IsHeartbeat() || invokeInputs.IsCron()
}

// buildMemoryContextBlock 构建 <memory-context> 包裹块。
// Python: _build_memory_context_block(raw_context)
//
// 对齐 Python 返回格式：
//
//	<memory-context>
//	[System note: recalled memory context from long-term memory, NOT new user input.]
//
//	{raw_context}
//	</memory-context>
func buildMemoryContextBlock(rawContext string) string {
	return "<memory-context>\n" +
		"[System note: recalled memory context from long-term memory, NOT new user input.]\n\n" +
		rawContext + "\n" +
		"</memory-context>"
}

// cbcRefID 获取回调上下文的唯一标识（模拟 Python id(ctx)）。
func cbcRefID(cbc *agentinterfaces.AgentCallbackContext) uintptr {
	return reflect.ValueOf(cbc).Pointer()
}
