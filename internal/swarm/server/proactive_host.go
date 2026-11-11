package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	recommendation "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// proactiveHost AgentServer 侧的 MainAgentHost 实现：把 server 的运行时能力
// （AgentManager / SendPush / 流任务表）适配给 recommendation 包。
//
// 存在理由是依赖倒置：recommendation 是 harness 层，不能直接 import server；
// 由 server 在启动时把自身能力注入（与 Start 里 SetSendFilePushFunc 等接线同款）。
//
// Python: proactive_adapter.py 的 server 参数（get_agent_manager + send_push +
// resolve_adapter 三处运行时依赖）
type proactiveHost struct {
	// server 反向持有 AgentServer，用于取 AgentManager / SendPush / 流任务表
	server *AgentServer
}

// ──────────────────────────── 导出函数 ────────────────────────────

// initProactiveEngine 装配并注入主动推荐引擎。
//
// 在 AgentServer.Start 里调用，对齐 Python
// server/worker/capabilities.py:73 prepare_proactive →
// await init_proactive_engine(server, proactive_config)。
//
// 配置取自 config.yaml 的 proactive_recommendation 段。
// 装配失败只告警，不阻断服务启动（Python 侧同样 signals.degraded 后继续）。
func (s *AgentServer) initProactiveEngine(ctx context.Context) {
	proactiveConfig := readProactiveRecommendationConfig(s.config)
	engine := recommendation.InitProactiveEngine(ctx, &proactiveHost{server: s}, proactiveConfig)
	if engine == nil {
		return
	}
	s.SetProactiveEngine(engine)
}

// ──────────────────────────── 全局变量 ────────────────────────────

// errProactiveHostUnavailable proactiveHost 未持有 server 时返回此错误。
var errProactiveHostUnavailable = errors.New("proactive host unavailable")

// 编译期断言：运行时 agent 直接满足 recommendation 的窄接口，
// 无需包装层（签名一致，见 runtime/interface.go ProcessMessageStream）。
var _ recommendation.MainAgentHandle = (*runtime.UapClawSwarm)(nil)

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetAgentNoWait 按 channel 取已存在的 agent；不存在返回 nil（不自动创建）。
//
// Python: manager.get_agent_nowait(cid)（proactive_adapter.py:174）
//
// 不自动创建是硬要求：tick 不应替用户建 agent——自动建时 cache_key
// （mode:sub_mode:project_dir）与用户对话实际用的可能不一致，会建出第二个 agent，
// 导致推荐进的不是用户对话用的 context。
func (h *proactiveHost) GetAgentNoWait(channelID, mode, projectDir, subMode string) recommendation.MainAgentHandle {
	if h.server == nil {
		return nil
	}
	manager := h.server.AgentManager()
	if manager == nil {
		return nil
	}
	// *runtime.UapClawSwarm 的 ProcessMessageStream 签名与 MainAgentHandle 一致，
	// 直接作为接口值返回（编译期由 var _ 断言兜住）。
	agent := manager.GetAgentNoWait(channelID, mode, projectDir, subMode)
	if agent == nil {
		return nil
	}
	return agent
}

// SendPush 推一个 chunk 到前端。
//
// Python: server.send_push(msg)（proactive_adapter.py:229 _push_chunks 内调用）
func (h *proactiveHost) SendPush(ctx context.Context, payload map[string]any) error {
	if h.server == nil {
		return errProactiveHostUnavailable
	}
	return h.server.SendPush(ctx, payload)
}

// IsSessionExecuting 目标会话是否正在跑（避让检查）。
//
// Python: adapter.is_deep_agent_executing_for_session(session_id)
// （proactive_adapter.py:189 —— 同 session 不支持并发 stream，忙则跳过本 tick）
//
// Go 侧的等价物是 AgentServer 的流任务登记表：handleStreamImpl 进入时
// registerStreamTask，收尾 releaseStreamTask。表内有该 session 即说明正在跑。
func (h *proactiveHost) IsSessionExecuting(sessionID string) bool {
	if h.server == nil || sessionID == "" {
		return false
	}
	h.server.sessionStreamTasksMu.Lock()
	defer h.server.sessionStreamTasksMu.Unlock()
	_, executing := h.server.sessionStreamTasks[sessionID]
	return executing
}

// readProactiveRecommendationConfig 从 config.yaml 读 proactive_recommendation 段。
//
// Python: config.get("proactive_recommendation", {})（capabilities.py:70）
func readProactiveRecommendationConfig(cfg *config.Config) map[string]any {
	if cfg == nil {
		return map[string]any{}
	}
	// 走 Load() 拿到归一化后的完整配置（与 handle_extensions 等既有读法一致）
	full, err := cfg.Load()
	if err != nil || full == nil {
		return map[string]any{}
	}
	if section, ok := full["proactive_recommendation"].(map[string]any); ok {
		return section
	}
	return map[string]any{}
}

// tryRecordImplicitFeedback best-effort 采集隐式反馈：用户在收到推荐后的文本回复。
//
// 主动推荐送达后，用户若直接用文本回复（「简洁点」「不需要」…）而不是点卡片上的
// 赞/踩按钮，这条回复就是隐式反馈。把它关联到该会话最近一条推荐，交由
// RecordImplicitFeedback 入 buffer，供下次 tick 梯度更新。
//
// 与 Runtime activity participant 同款 best-effort：任何异常只 log debug，
// 绝不阻断主 chat 流。只在 chat.send 且来源不是 proactive 自己触发的推荐指令时
// 才介入（source=proactive_recommendation 是系统主动塞给主 agent 的指令，
// 不是用户说的话）。
//
// Python: agent_ws_server.py:3456 _try_record_implicit_feedback
func (s *AgentServer) tryRecordImplicitFeedback(request *schema.AgentRequest) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Debug(logComponent).Any("panic", recovered).
				Msg("implicit feedback hook panicked; preserving chat")
		}
	}()

	params := decodeParamsQuietly(request.Params)
	// proactive 自己触发主 agent 的指令带 source=proactive_recommendation，不是用户输入
	if source, _ := params["source"].(string); source == recommendation.ProactiveSourceSource {
		return
	}

	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	if sessionID == "" {
		if fromParams, _ := params["session_id"].(string); fromParams != "" {
			sessionID = fromParams
		}
	}
	if sessionID == "" {
		return
	}

	// Python: query = _request_query_text(request) —— 只取纯文本 query，
	// 结构化事件（非字符串 query）不参与隐式反馈
	query, _ := params["query"].(string)
	if query == "" {
		return
	}

	// Python: find_latest_recommendation(session_id, max_age_seconds=0)
	// max_age=0 砍掉时间窗：不靠时间硬挡「无关反馈」——是否相关、是否产生梯度
	// 交给梯度更新器的模型判断。配合 RecordFeedback 的「同 rec_id 只采紧跟第一条」，
	// 每条推荐只关联它之后紧跟的第一条用户回复。
	latest := recommendation.FindLatestRecommendation(sessionID, 0)
	if latest == nil {
		return
	}
	recID, _ := latest["id"].(string)
	if recID == "" {
		return
	}

	// 已对该 rec_id 给过显式反馈（赞/踩）的话，RecordFeedback 的去重逻辑会自动
	// 丢弃隐式补充——无需在此预判。
	recommendation.RecordImplicitFeedback(recID, query)
}

// decodeParamsQuietly 解析 params，失败返回空 map（隐式反馈对参数不敏感）。
func decodeParamsQuietly(raw json.RawMessage) map[string]any {
	params := make(map[string]any)
	if len(raw) == 0 {
		return params
	}
	_ = json.Unmarshal(raw, &params)
	return params
}
