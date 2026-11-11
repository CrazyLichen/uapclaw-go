package recommendation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MainAgentHost 触发主 agent 所需的运行时依赖。
//
// **依赖倒置方向**：这是 DeepAgent/recommendation 作为**使用方**去依赖 server 侧能力，
// 不是别人调用 DeepAgent（规则 10.2 允许的窄接口场景）。
// 由 server 侧实现并注入，避免 recommendation 包反向依赖 server 实现。
//
// Python: proactive_adapter.py 的 server 参数（get_agent_manager +
// RuntimeHostPushTransport 两个运行时依赖）
type MainAgentHost interface {
	// GetAgentNoWait 按 channel 取已存在的 agent；不存在返回 nil（不自动创建）。
	// tick 不应替用户建 agent——自动建时 cache_key 可能与用户对话用的不一致，
	// 会建出第二个 agent，导致推荐进的不是用户对话的 context。
	GetAgentNoWait(channelID, mode, projectDir, subMode string) MainAgentHandle
	// SendPush 推一个 chunk 到前端。
	// 签名与 AgentServer.SendPush 一致（ctx + payload → error），失败返回 error。
	SendPush(ctx context.Context, payload map[string]any) error
	// IsSessionExecuting 目标会话是否正在跑（避让检查，同 session 不支持并发 stream）。
	IsSessionExecuting(sessionID string) bool
}

// MainAgentHandle 主 agent 的窄接口面（只暴露触发推荐需要的方法）。
type MainAgentHandle interface {
	// ProcessMessageStream 跑一轮并返回 chunk 通道。
	// opts 是调用方专属钩子（on_agent_ready / _agent_execution），本接口只透传不使用；
	// 加可变参数是为了保持与 ChatAgentPort 的方法签名一致（20.2 D-7）。
	ProcessMessageStream(ctx context.Context, request *swarmschema.AgentRequest, opts ...swruntime.ExecuteOption) (<-chan *swarmschema.AgentResponseChunk, error)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ProactiveSourceSource 推荐指令的来源标记。
	//
	// process_message_stream 写 user history 时据此跳过——否则刷新页面会看到
	// 「[主动推荐指令] xxx」这条用户没说过的消息。
	// 隐式反馈采集也据此排除（系统指令不是用户说的话）。
	//
	// 导出是因为 server 侧的隐式反馈采集要读同一协议值——
	// 前端按此字符串做分支，两侧必须同源，不得各写一份字面量。
	//
	// Python: proactive_adapter.py:196 "source": "proactive_recommendation"
	ProactiveSourceSource = "proactive_recommendation"
	// notificationSource 系统通知的来源标记（前端据此走顶部弹窗，不进会话历史）
	// Python: proactive_adapter.py:418 "source": "proactive_notification"
	notificationSource = "proactive_notification"
	// proactiveAgentName proactive agent 的标识
	// Python: proactive_adapter.py:104 AgentCard(name="proactive_agent", id="proactive_agent")
	proactiveAgentName = "proactive_agent"
	// proactiveAgentSystemPrompt proactive agent 的系统提示词
	// Python: proactive_adapter.py:106 system_prompt="你是用户洞察与推荐助手。严格输出 JSON 对象。"
	proactiveAgentSystemPrompt = "你是用户洞察与推荐助手。严格输出 JSON 对象。"
	// defaultAgentChannel 无 channel 时的默认值
	// Python: proactive_adapter.py:163 cid = channel_id or "web"
	defaultAgentChannel = "web"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// inflightPushMu 保护 inflightPushKeys
	inflightPushMu sync.Mutex
	// inflightPushKeys 正在推送的 "channel:session" 集合，防并发重复推
	//
	// Python: proactive_adapter.py:220 _proactive_push_inflight
	inflightPushKeys = make(map[string]struct{})
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildProactiveAgent 构造推荐专用的轻量 agent。
//
// 无 tools、无 rails、关闭 task loop、单轮、输出 JSON。
// 替代裸 model.Invoke —— 走 agent 框架的 invoke 链路（rails / 模型选择 / 观测）。
//
// 返回 nil 表示无模型配置或构造失败（引擎会跳过本次 tick）。
//
// Python: proactive_adapter.py:85 build_proactive_agent
func BuildProactiveAgent(ctx context.Context) *harness.DeepAgent {
	model := getProactiveModel()
	if model == nil {
		logger.Warn(logComponent).Msg("proactive agent: no model configured")
		return nil
	}
	agent, err := harness.CreateDeepAgent(ctx, harness_config.CreateDeepAgentParams{
		Model:                  model,
		Card:                   agentschema.NewAgentCard(agentschema.WithAgentName(proactiveAgentName)),
		SystemPrompt:           proactiveAgentSystemPrompt,
		EnableTaskLoop:         false,
		MaxIterations:          1,
		AddGeneralPurposeAgent: false,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("proactive agent build failed")
		return nil
	}
	return agent
}

// TriggerMainAgentOnHost 驱动主 agent 跑一轮，把推荐话术流式推到前端。
//
// 避让：目标 session 正在跑 stream 时跳过（同 session 不支持并发 stream）。
// 触发后主 agent 自己生成话术 → 进 context engine → stream 推前端。
//
// **fire-and-forget**：主 agent 跑一轮被丢到后台 goroutine，本函数立即返回 true
// （表示「已触发」，让调用方 cron 秒回不超时）。request.OnDelivered 回调在后台
// **真正跑完**（主 agent 输出流尽、有正文、未出 chat.error、推送未失败）后才被调用
// ——用于在「推荐确实送达」时再做计数/持久化，避免后台失败却已计数。
//
// Python: proactive_adapter.py:120 trigger_main_agent
func TriggerMainAgentOnHost(
	ctx context.Context,
	host MainAgentHost,
	request *ProactiveTriggerRequest,
) bool {
	if host == nil || request == nil || request.Decision == nil {
		return false
	}

	cid := request.ChannelID
	if cid == "" {
		cid = defaultAgentChannel
	}

	// 用 get_agent_nowait（不自动创建）：tick 不应替用户建 agent——
	// 自动建时 cache_key（mode:sub_mode:project_dir）与用户对话实际用的可能不一致，
	// 会建出第二个 agent，导致推荐进的不是用户对话用的 context。
	// agent 不在内存 = 用户尚未在该 channel 发过消息（无活跃 context 可投递）→
	// 跳过本次 tick，等用户用过一次、agent 建好后下个 tick 自然拿到。
	agent := host.GetAgentNoWait(cid, "agent", "", "")
	if agent == nil {
		logger.Info(logComponent).Str("channel", cid).
			Msg("trigger: no agent for channel (user hasn't used it yet), skipping")
		return false
	}

	// 避让：目标 session 正忙 → 跳过本次 tick
	if host.IsSessionExecuting(request.SessionID) {
		logger.Info(logComponent).Str("session", truncateRunes(request.SessionID, 20)).
			Msg("trigger: session busy, skipping")
		return false
	}

	agentRequest, err := buildProactiveAgentRequest(request, cid)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("trigger: build agent request failed")
		return false
	}

	// fire-and-forget：主 agent 跑一轮 + 流式推前端可能很慢（LLM + 工具调用 +
	// post_run 持久化），串行等会把调用方（proactive.tick → cron）一起拖到超时。
	// 丢后台 goroutine，立即返回 true 表示「已触发」。
	inflightKey := cid + ":" + request.SessionID
	if !acquireInflightPush(inflightKey) {
		logger.Info(logComponent).Str("inflight", inflightKey).
			Msg("trigger: previous push still running, skipping")
		return false
	}

	go func() {
		defer releaseInflightPush(inflightKey)
		pushProactiveChunks(ctx, host, agent, agentRequest, request, cid)
	}()

	logger.Info(logComponent).Str("inflight", inflightKey).Msg("trigger: fired background push")
	return true
}

// InitProactiveEngine 组装引擎 + 注入专用 agent 构造器 + 三个回调，返回可用引擎。
//
// 启动时调用，把所有 proactive 适配逻辑集中在此。
// 返回 nil 表示装配失败（只告警，不影响主流程）。
//
// Python: proactive_adapter.py:393 init_proactive_engine
func InitProactiveEngine(ctx context.Context, host MainAgentHost, cfg map[string]any) *ProactiveEngine {
	engine := NewProactiveEngine(cfg)

	// 专用 agent：只做决策（无 tools、无 task_loop、单轮、输出 JSON）
	engine.SetProactiveAgentBuilder(func() *harness.DeepAgent {
		return BuildProactiveAgent(ctx)
	})

	// 检查 agent 是否活跃——在调 LLM 之前检查，避免 agent 被 evict 后白调 LLM
	engine.SetCheckAgentAvailableCallback(func(targetChannel string) bool {
		cid := targetChannel
		if cid == "" {
			cid = defaultAgentChannel
		}
		if host == nil {
			return false
		}
		return host.GetAgentNoWait(cid, "agent", "", "") != nil
	})

	// 推送通知回调——直接推文本到前端，不经过主 agent（不进 context）。
	// 用于「今日推荐已达上限」等系统提醒。
	engine.SetSendNotificationCallback(func(targetChannel, message string) error {
		cid := targetChannel
		if cid == "" {
			cid = defaultAgentChannel
		}
		if host == nil {
			return fmt.Errorf("no host")
		}
		return host.SendPush(context.Background(), map[string]any{
			"request_id": fmt.Sprintf("proactive_notification_%d", time.Now().UnixMilli()),
			"channel_id": cid,
			"payload": map[string]any{
				"content":    message,
				"event_type": "chat.final",
				"role":       "assistant",
				"source":     notificationSource,
			},
		})
	})

	// 触发主 agent 回调：tick 决策后，TriggerMainAgent 已把决策包成指令式 query
	// 并填进 request，这里透传给 TriggerMainAgentOnHost 跑主 agent。
	engine.SetTriggerMainAgentCallback(func(ctx context.Context, request *ProactiveTriggerRequest) (bool, error) {
		return TriggerMainAgentOnHost(ctx, host, request), nil
	})

	return engine
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pushProactiveChunks 后台消费主 agent 的流式输出并推前端。
//
// 送达判定收紧：循环跑完且未产 chat.error 且确实拿到话术正文（final/delta）
// 且推送未失败。缺任何一条 = 未真正送达（LLM 失败/0 token/推送异常），
// 不计 delivered → 不写 history，避免空记录。
//
// Python: proactive_adapter.py:229 _push_chunks
func pushProactiveChunks(
	ctx context.Context,
	host MainAgentHost,
	agent MainAgentHandle,
	agentRequest *swarmschema.AgentRequest,
	request *ProactiveTriggerRequest,
	cid string,
) {
	var (
		// finalContent chat.final 的完整文本（优先采用）
		finalContent string
		// deltaParts chat.delta 增量（final 缺失时兜底拼接）
		deltaParts []string
		// hadChatError 本轮是否产出失败 chunk
		//
		// LLM 调用失败（429 budget 等）时流里会带 chat.error 而非正文，0 token 产出。
		// 若仅判「循环未抛异常」会把这种失败也当送达 → 写进 history（content 空）
		// 但用户从没收到卡片。故必须识别 chat.error 为未送达。
		hadChatError bool
		// markingClosed 话术轮已交付，其后 chunk 不再打标、不再透传
		markingClosed bool
		// sawToolCallSinceLastFinal 自上一个 final 以来是否出现过 tool_call
		//
		// 主 agent 收到推荐指令后可能先跑工具（如 ls/diff），工具轮结束会吐一个
		// 总结 final（"命令执行完毕…"），它只是进度正文，不是推荐话术。
		// 靠这个标记区分工具轮 final 与话术轮 final。
		sawToolCallSinceLastFinal bool
		// pushFailed 推送是否失败过
		pushFailed bool
	)

	stream, err := agent.ProcessMessageStream(ctx, agentRequest)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("trigger: process_message_stream failed")
		return
	}

	for chunk := range stream {
		if chunk == nil {
			continue
		}
		chunkPayload := make(map[string]any, len(chunk.Payload))
		for k, v := range chunk.Payload {
			chunkPayload[k] = v
		}

		// event_type 字段名也可能写作 event，兼容两种
		evt := stringField(chunkPayload, "event_type")
		if evt == "" {
			evt = stringField(chunkPayload, "event")
		}
		isChatError := strings.HasSuffix(evt, ".error")
		if isChatError {
			logger.Warn(logComponent).Str("rec_id", request.RecID).
				Str("target", request.Decision.Target).
				Str("error", stringField(chunkPayload, "error")).
				Msg("main agent emitted chat.error during push")
			// 仅卡片送达前的错误才算「未送达」；卡片已送达后延续轮的错误不翻转 delivered
			if !markingClosed {
				hadChatError = true
			}
		}

		if markingClosed {
			// 卡片已送达。其后的 chunk 是主 agent 的自我延续（补充文本轮、工具轮、
			// processing_status 收尾），不属于推荐交付物，一律不再透传前端：
			// 无标记文本 final/delta 会在卡片下渲染成独立普通气泡；无标记 reasoning
			// 会并进会话 reasoningSegments；无标记 processing_status 会误关用户当前轮
			// 的 processing 状态。流仍继续消费（history/context 照常落盘），仅推送被抑制。
			continue
		}

		if strings.HasSuffix(evt, ".tool_call") {
			sawToolCallSinceLastFinal = true
		}
		content := stringField(chunkPayload, "content")
		isNonemptyAssistantFinal := strings.HasSuffix(evt, ".final") && content != ""

		// 工具轮总结 final（该轮出现过 tool_call）：进度正文，不是推荐话术。
		// 抑制不透传前端、不打标，窗口保持打开等待话术轮——
		// 否则工具轮总结会抢占卡片锚点（卡片 content 变成「命令执行完毕…」）。
		if isNonemptyAssistantFinal && sawToolCallSinceLastFinal {
			sawToolCallSinceLastFinal = false
			continue
		}

		if !isChatError {
			// 注入 source/proactive_type：主 agent 的 chunk 是普通对话格式，不带主动推荐
			// 标记。前端靠 payload.source==='proactive_recommendation' 识别卡片、
			// payload.proactive_type 选颜色，缺这俩会退化成普通白色气泡。
			//
			// chat.error 不打标：错误 chunk 带 source 会被前端当推荐卡片渲染，
			// 应让用户看到错误提示而非异常卡片。错误 chunk 仍透传前端（无 source）。
			if _, exists := chunkPayload["source"]; !exists {
				chunkPayload["source"] = ProactiveSourceSource
			}
			if _, exists := chunkPayload["proactive_type"]; !exists {
				chunkPayload["proactive_type"] = request.Decision.Type
			}
			if _, exists := chunkPayload["proactive_target"]; !exists {
				chunkPayload["proactive_target"] = request.Decision.Target
			}
			if request.RecID != "" {
				chunkPayload["proactive_rec_id"] = request.RecID
			}
			// 话术轮的非空 final = 推荐正文已发完，其后不再打标
			if isNonemptyAssistantFinal {
				markingClosed = true
			}
		}

		// 累积话术文本
		if isNonemptyAssistantFinal {
			// 覆盖式：保留最后一个非空 final 的内容作为交付物
			finalContent = content
		} else if strings.HasSuffix(evt, ".delta") && !markingClosed {
			deltaParts = append(deltaParts, content)
		}

		pushPayload := map[string]any{
			"request_id":  chunk.RequestID,
			"channel_id":  cid,
			"session_id":  request.SessionID,
			"payload":     chunkPayload,
			"is_complete": chunk.IsComplete,
		}
		if pushPayload["request_id"] == "" {
			pushPayload["request_id"] = agentRequest.RequestID
		}
		if pushErr := host.SendPush(ctx, pushPayload); pushErr != nil {
			pushFailed = true
			logger.Debug(logComponent).Err(pushErr).Msg("send_push failed")
		}
	}

	// 送达判定
	phrasingText := finalContent
	if phrasingText == "" {
		phrasingText = strings.Join(deltaParts, "")
	}
	delivered := phrasingText != "" && !hadChatError && !pushFailed

	if delivered && request.OnDelivered != nil {
		safeInvokeOnDelivered(request.OnDelivered, phrasingText)
	} else if hadChatError {
		logger.Info(logComponent).Msg("not delivered (main agent chat.error, no phrasing text)")
	}
}

// buildProactiveAgentRequest 构造给主 agent 的推荐指令请求。
//
// Python: proactive_adapter.py:181-207 AgentRequest(...)
func buildProactiveAgentRequest(request *ProactiveTriggerRequest, cid string) (*swarmschema.AgentRequest, error) {
	params := map[string]any{
		"query": request.Query,
		"mode":  "agent",
		// source 标记这是系统触发的推荐指令，不是用户说的话
		"source": ProactiveSourceSource,
		// proactive_type/target 给 assistant 写 history 时透传用
		"proactive_type":   request.Decision.Type,
		"proactive_target": request.Decision.Target,
	}
	// 透传 rec_id：写 assistant history 时一并持久化，刷新页面后前端靠
	// payload.proactive_rec_id 还原赞/踩按钮，否则历史消息上按钮不出现。
	if request.RecID != "" {
		params["proactive_rec_id"] = request.RecID
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	sessionID := request.SessionID
	return &swarmschema.AgentRequest{
		RequestID: fmt.Sprintf("proactive_%s_%d", request.Decision.Type, time.Now().UnixMilli()),
		ChannelID: cid,
		SessionID: &sessionID,
		Params:    rawParams,
		IsStream:  true,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
	}, nil
}

// getProactiveModel 构造推荐决策用的模型（temperature=0，关闭 thinking）。
//
// 关闭 thinking：proactive 决策是轻量单轮 JSON 产出（skill 选择/痛点判断），
// 不需要思维链，开了反而拖慢 tick、占 token。
//
// Python: proactive_actions.py:235 _get_model
func getProactiveModel() *llm.Model {
	entries := config.GetDefaultModels(nil)
	if len(entries) == 0 {
		return nil
	}
	entry := entries[0]
	mcc, _ := entry["model_client_config"].(map[string]any)
	if mcc == nil {
		mcc = map[string]any{}
	}
	modelName, _ := mcc["model_name"].(string)

	// mcc 的 map → ModelClientConfig（消除后续类型断言）
	var clientConfig schema.ModelClientConfig
	if mccData, err := json.Marshal(mcc); err == nil {
		if err := json.Unmarshal(mccData, &clientConfig); err != nil {
			logger.Warn(logComponent).Err(err).Msg("ModelClientConfig deserialization failed")
		}
	}
	if clientConfig.ClientProvider == "" {
		clientConfig.ClientProvider = "OpenAI"
	}
	if clientConfig.Timeout <= 0 {
		clientConfig.Timeout = 60.0
	}
	if clientConfig.MaxRetries <= 0 {
		clientConfig.MaxRetries = 3
	}

	// extra_body 经 ModelRequestConfig.Extra 透传（对应 Python model_config extra="allow"）
	modelConfig := schema.NewModelRequestConfig(
		schema.WithModelName(modelName),
		schema.WithTemperatureValue(0.0),
	)
	if modelConfig.Extra == nil {
		modelConfig.Extra = make(map[string]any, 2)
	}
	modelConfig.Extra["extra_body"] = map[string]any{
		"thinking":             map[string]any{"type": "disabled"},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	}

	model, err := llm.NewModel(&clientConfig, modelConfig)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("proactive model build failed")
		return nil
	}
	return model
}

// acquireInflightPush 登记在飞推送，已有同名则返回 false。
func acquireInflightPush(key string) bool {
	inflightPushMu.Lock()
	defer inflightPushMu.Unlock()
	if _, exists := inflightPushKeys[key]; exists {
		return false
	}
	inflightPushKeys[key] = struct{}{}
	return true
}

// releaseInflightPush 注销在飞推送。
func releaseInflightPush(key string) {
	inflightPushMu.Lock()
	defer inflightPushMu.Unlock()
	delete(inflightPushKeys, key)
}

// safeInvokeOnDelivered 调用送达回调，异常只告警（不阻断后台清理）。
func safeInvokeOnDelivered(callback func(string), generated string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Warn(logComponent).Any("panic", recovered).Msg("on_delivered callback panicked")
		}
	}()
	callback(generated)
}
