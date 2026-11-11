package message_handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelControlState 渠道控制状态
type ChannelControlState struct {
	// mu 保护 SessionID 与 Mode 的并发读写。
	// 该对象被 forwardLoop（写）与 HandleMessage/slash 处理（读写）共享，
	// Python 单线程无此问题，Go 必须显式同步。
	mu sync.RWMutex
	// SessionID 当前会话标识
	SessionID string
	// Mode 当前渠道模式
	Mode ChannelMode
}

// GetSessionID 返回当前会话标识（并发安全）。
func (s *ChannelControlState) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SessionID
}

// SetSessionID 更新会话标识（并发安全）。
func (s *ChannelControlState) SetSessionID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SessionID = id
}

// GetMode 返回当前模式（并发安全）。
func (s *ChannelControlState) GetMode() ChannelMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Mode
}

// SetMode 更新模式（并发安全）。
func (s *ChannelControlState) SetMode(mode ChannelMode) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Mode = mode
}

// Snapshot 原子返回会话标识与模式。
func (s *ChannelControlState) Snapshot() (string, ChannelMode) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SessionID, s.Mode
}

// ──────────────────────────── 枚举 ────────────────────────────

// ChannelMode 渠道模式枚举。
// 对齐 Python `gateway/message_handler/message_handler.py:205-225`
// `class ChannelMode(str, Enum)`（18 个成员，旧成员保留以兼容历史持久化反解析）。
// Go 侧用字符串枚举而非 int：一是成员值与 Python `.value` 完全一致（无需映射表），
// 二是零值为 `""` 而不是"plan"，杜绝"零值即计划模式"这一类缺陷。
type ChannelMode string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// externalSessionChannelA2A 需要做外部会话别名映射的通道 ID
	externalSessionChannelA2A = "a2a"
	// externalSessionMetadataKey 外部原始会话 ID 在 metadata 中的键
	externalSessionMetadataKey = "external_session_id"
	// defaultExternalSessionMode 外部消息未带 mode 时的默认模式
	defaultExternalSessionMode = "agent"
)

const (
	// ChannelModeAgent 代理模式（历史值；plan/fast 已合并到 agent）
	ChannelModeAgent ChannelMode = "agent"
	// ChannelModeAgentPlan 代理规划模式（历史值，仅用于反解析旧持久化状态）
	ChannelModeAgentPlan ChannelMode = "agent.plan"
	// ChannelModeAgentFast 代理快速模式（历史值）
	ChannelModeAgentFast ChannelMode = "agent.fast"
	// ChannelModeCodePlan 代码规划模式（历史值）
	ChannelModeCodePlan ChannelMode = "code.plan"
	// ChannelModeCodeNormal 代码常态模式（历史值）
	ChannelModeCodeNormal ChannelMode = "code.normal"
	// ChannelModeCodeTeam 代码团队模式（历史值）
	ChannelModeCodeTeam ChannelMode = "code.team"
	// ChannelModeTeam 团队模式
	ChannelModeTeam ChannelMode = "team"
	// ChannelModeTeamPlan 团队规划模式别名（与 TeamPlanNormal 同值，对齐 Python 双成员）
	ChannelModeTeamPlan ChannelMode = "team.plan.normal"
	// ChannelModeTeamPlanNormal 团队规划模式（历史值）
	ChannelModeTeamPlanNormal ChannelMode = "team.plan.normal"
	// ChannelModeTeamPlanCode 团队代码规划模式（历史值）
	ChannelModeTeamPlanCode ChannelMode = "team.plan.code"

	// ChannelModeAgentWorkNormal 代理工作常态（新三段 canonical）
	ChannelModeAgentWorkNormal ChannelMode = "agent.work.normal"
	// ChannelModeAgentWorkPlan 代理工作规划（新三段 canonical）
	ChannelModeAgentWorkPlan ChannelMode = "agent.work.plan"
	// ChannelModeAgentCodeNormal 代理代码常态（新三段 canonical）
	ChannelModeAgentCodeNormal ChannelMode = "agent.code.normal"
	// ChannelModeAgentCodePlan 代理代码规划（新三段 canonical）
	ChannelModeAgentCodePlan ChannelMode = "agent.code.plan"
	// ChannelModeTeamWorkNormal 团队工作常态（新三段 canonical）
	ChannelModeTeamWorkNormal ChannelMode = "team.work.normal"
	// ChannelModeTeamWorkPlan 团队工作规划（新三段 canonical）
	ChannelModeTeamWorkPlan ChannelMode = "team.work.plan"
	// ChannelModeTeamCodeNormal 团队代码常态（新三段 canonical）
	ChannelModeTeamCodeNormal ChannelMode = "team.code.normal"
	// ChannelModeTeamCodePlan 团队代码规划（新三段 canonical）
	ChannelModeTeamCodePlan ChannelMode = "team.code.plan"
)

// channelModeMembers ChannelMode 的已声明成员集合（对齐 Python `ChannelMode(value)` 构造校验）。
// Python 在 channel_mode_from_str 里 `try: return ChannelMode(new_mode_str) except ValueError`，
// 未声明的 canonical（如 team.design）会构造失败并兜底 AGENT——Go 用本集合等价表达。
var channelModeMembers = map[ChannelMode]bool{
	ChannelModeAgent:      true,
	ChannelModeAgentPlan:  true,
	ChannelModeAgentFast:  true,
	ChannelModeCodePlan:   true,
	ChannelModeCodeNormal: true,
	ChannelModeCodeTeam:   true,
	ChannelModeTeam:       true,
	// ChannelModeTeamPlan 与 ChannelModeTeamPlanNormal 同值，map 只登记一次
	ChannelModeTeamPlanNormal:  true,
	ChannelModeTeamPlanCode:    true,
	ChannelModeAgentWorkNormal: true,
	ChannelModeAgentWorkPlan:   true,
	ChannelModeAgentCodeNormal: true,
	ChannelModeAgentCodePlan:   true,
	ChannelModeTeamWorkNormal:  true,
	ChannelModeTeamWorkPlan:    true,
	ChannelModeTeamCodeNormal:  true,
	ChannelModeTeamCodePlan:    true,
}

// agentWorkModes `/switch` 判据：agent 族（plan 与 fast 已合并）。
// Python: _SWITCH_AGENT_WORK_MODES（message_handler.py:272-274）
var agentWorkModes = map[string]bool{
	string(ChannelModeAgentWorkNormal): true,
	string(ChannelModeAgentWorkPlan):   true,
}

// codeModes `/switch` 判据：code 族三种模式。
// Python: _SWITCH_CODE_MODES（message_handler.py:275-277）
var codeModes = map[string]bool{
	string(ChannelModeAgentCodePlan):   true,
	string(ChannelModeAgentCodeNormal): true,
	string(ChannelModeTeamCodeNormal):  true,
}

// ──────────────────────────── 全局变量 ────────────────────────────

// controlChannelTypes 控制类渠道类型集合
var controlChannelTypes map[string]bool

// sessionMapChannelTypes 使用 SessionMap 的通道族集合（精确或 base: 前缀匹配）
// Python: _session_map_channel_types = frozenset({"feishu_enterprise"})
var sessionMapChannelTypes map[string]bool

// stickyChannelTypes 无正式会话时需要"记住状态键"的通道族。
// Python: _sticky_channel_state_types = frozenset({ChannelType.XIAOYI.value})
var stickyChannelTypes map[string]bool

// knownTeamModeInputs 已知 Team 模式清单（Python: _KNOWN_TEAM_MODE_INPUTS）。
// 由 validModeInputs 过滤 is_team_mode 后排序派生，避免两处手抄漂移。
var knownTeamModeInputs []string

// unknownTeamModeNotice 未知 team.* 模式的拒绝文案。
// Python: _UNKNOWN_TEAM_MODE_NOTICE（message_handler.py:108-111）
const unknownTeamModeNotice = "不支持的模式 '%s'：以 team 开头的 mode 必须是已知的 Team 模式（%s）。请修正 mode 后重试。"

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyChannelState 把渠道控制状态（session_id / mode）应用到入站消息。
// Python: _apply_channel_state (L2654-2694)。步骤顺序逐条对齐：
//  1. 非受控渠道直接返回
//  2. team 成员消息守卫（metadata.member_name 存在时强制 mode=team 并返回）
//  3. 取/建渠道状态
//  4. 三个互斥分支：SessionMap 命中 / state 有 sid / 都没有（记住状态键并清空入站 sid）
//  5. params.setdefault("mode", state.mode.value)
func (mh *MessageHandler) ApplyChannelState(msg *schema.Message) {
	// 1. 受控渠道判定
	channelType := mh.resolveControlChannelType(msg)
	if !controlChannelTypes[string(channelType)] {
		return
	}

	// 2. team 成员守卫：session/mode 已由 team 路由决定，不覆盖
	if memberName, ok := msg.Metadata["member_name"].(string); ok && strings.TrimSpace(memberName) != "" {
		params := decodeMessageParams(msg)
		params["mode"] = string(ChannelModeTeam)
		encodeMessageParams(msg, params)
		return
	}

	// 3. 获取或创建状态
	state := mh.GetOrCreateChannelState(msg)

	// 4. session_id 三分支
	// Python: 企业通道族走 SessionMap；其它受控通道按 state（elif）；都没有时记键并清空
	if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(msg.ChannelID) {
		if sid := mh.sessionMap.GetSessionID(p, c, b, u, false); sid != "" {
			state.SetSessionID(sid)
			msg.SessionID = sid
		} else {
			msg.SessionID = ""
		}
	} else if state.GetSessionID() != "" {
		msg.SessionID = state.GetSessionID()
	} else {
		// Python: _remember_alloc_state_key(msg, cid); msg.session_id = None
		mh.rememberAllocStateKey(msg, msg.ChannelID)
		msg.SessionID = ""
	}

	// 5. 将 mode 写入 params（setdefault 语义）
	params := decodeMessageParams(msg)
	if _, exists := params["mode"]; !exists {
		params["mode"] = string(state.GetMode())
	}
	encodeMessageParams(msg, params)
}

// GetOrCreateChannelState 获取或创建消息对应 channel 状态。
// Python: _get_or_create_channel_state (L921-938)：
// 使用复合键 channelID:sessionID（对齐 Python _get_channel_state_key）。
// 首建时若企业通道身份齐备，SessionMap 覆盖 config 默认 session_id（Python L933-935）。
func (mh *MessageHandler) GetOrCreateChannelState(msg *schema.Message) *ChannelControlState {
	ch := msg.ChannelID
	key := getChannelStateKey(ch, msg.SessionID)

	mh.statesMu.RLock()
	state, exists := mh.channelStates[key]
	mh.statesMu.RUnlock()

	if exists {
		return state
	}

	// 创建默认状态（从 config 读取）
	state = mh.getChannelDefaultState(ch)

	// 企业通道身份齐备时，SessionMap 覆盖默认 session_id
	if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(ch) {
		state.SetSessionID(mh.sessionMap.GetSessionID(p, c, b, u, false))
	}

	mh.statesMu.Lock()
	// 双重检查，防止并发创建
	if existing, ok := mh.channelStates[key]; ok {
		mh.statesMu.Unlock()
		return existing
	}
	mh.channelStates[key] = state
	mh.statesMu.Unlock()

	return state
}

// EnsureInboundSession 受控通道入站无 session 时为其分配真实会话并写回状态。
// Python: _ensure_inbound_session (L1012-1026)。在 _apply_channel_state 之后调用
// （Python: L4966-4967），返回 error 时调用方停止本轮转发（Python 抛 RuntimeError）。
func (mh *MessageHandler) EnsureInboundSession(ctx context.Context, msg *schema.Message) error {
	channelType := mh.resolveControlChannelType(msg)
	if !controlChannelTypes[string(channelType)] {
		return nil
	}
	if strings.TrimSpace(msg.SessionID) != "" {
		return nil
	}

	// Python: key = self._pending_alloc_state_key.pop(msg.id, "")
	mh.statesMu.Lock()
	key := mh.pendingAllocStateKey[msg.ID]
	delete(mh.pendingAllocStateKey, msg.ID)
	state := mh.channelStates[key]
	mh.statesMu.Unlock()

	if state == nil {
		state = mh.GetOrCreateChannelState(msg)
	}
	sid, err := mh.allocateChannelSession(ctx, msg, state, false)
	if err != nil {
		return err
	}
	msg.SessionID = sid
	// 兼容旧行为：同一次分配也留一份 channel 级状态（cancel/disconnect 的 mode
	// 兜底按 channel_id 键读取）。
	mh.statesMu.Lock()
	if _, ok := mh.channelStates[string(channelType)]; !ok {
		mh.channelStates[string(channelType)] = state
	}
	mh.statesMu.Unlock()
	return nil
}

// resolveExternalChannelSession 把 A2A 协议会话 ID 映射到 AgentServer 会话。
// Python: _resolve_external_channel_session (message_handler.py:1028-1061)。
// 仅处理 a2a 通道（ssh 是 AgentOS 纯中继，不分配本地会话）：以 (channel_id, 外部
// session_id) 为键查进程级别名表，未命中时按外部消息 params.mode（缺省 agent）分配
// 本地会话并记录。随后把外部原始 ID 留在 metadata.external_session_id，并把
// msg.session_id 换成本地会话 ID，保证同一段外部对话始终落在同一个本地会话。
// 锁语义对齐上游：分配在锁内完成（上游是全局 asyncio.Lock 内 await 分配），并发首轮
// 只会创建一个会话；a2a 首轮分配本身罕见，串行化的代价可接受。
func (mh *MessageHandler) resolveExternalChannelSession(ctx context.Context, msg *schema.Message) {
	channelID := strings.TrimSpace(msg.ChannelID)
	externalID := strings.TrimSpace(msg.SessionID)
	if channelID != externalSessionChannelA2A || externalID == "" {
		return
	}

	key := externalSessionAliasKey{channelID: channelID, externalID: externalID}
	mh.externalAliasMu.Lock()
	resolved, cached := mh.externalSessionAliases[key]
	if !cached {
		rawMode := defaultExternalSessionMode
		if params := decodeMessageParams(msg); params != nil {
			if v := strings.TrimSpace(strScalar(params["mode"])); v != "" {
				rawMode = v
			}
		}
		state := &ChannelControlState{Mode: channelModeFromStr(rawMode)}
		sessionID, err := mh.allocateChannelSession(ctx, msg, state, false)
		if err != nil {
			mh.externalAliasMu.Unlock()
			logger.Warn(logComponent).
				Err(err).
				Str("msg_id", msg.ID).
				Str("channel_id", channelID).
				Str("external_session_id", externalID).
				Msg("allocate session for external channel failed")
			return
		}
		resolved = sessionID
		mh.externalSessionAliases[key] = resolved
	}
	mh.externalAliasMu.Unlock()

	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any, 1)
	}
	if _, has := msg.Metadata[externalSessionMetadataKey]; !has {
		msg.Metadata[externalSessionMetadataKey] = externalID
	}
	msg.SessionID = resolved
}

// IsUnknownTeamModeSend 判断是否为"mode 以 team 开头但不是已知 Team 模式"的 chat.send。
// Python: _is_unknown_team_mode_send (L742-753)——只收紧 team.*，其余未知 mode 保持宽松路径。
func (mh *MessageHandler) IsUnknownTeamModeSend(msg *schema.Message) bool {
	if msg.ReqMethod != schema.ReqMethodChatSend {
		return false
	}
	params := decodeMessageParams(msg)
	mode := strings.ToLower(strings.TrimSpace(strScalar(params["mode"])))
	if mode == "" || strings.SplitN(mode, ".", 2)[0] != "team" {
		return false
	}
	return !schema.IsTeamMode(mode)
}

// RejectUnknownTeamModeSend 拒绝未知 team.* 模式并回 chat.error。
// Python: _reject_unknown_team_mode_send (L755-785)——避免网关按普通 chat 转发、
// AgentServer 按 team 执行导致客户端永久等待。
func (mh *MessageHandler) RejectUnknownTeamModeSend(msg *schema.Message) {
	params := decodeMessageParams(msg)
	mode := strScalar(params["mode"])
	payload := map[string]any{
		"event_type":  string(schema.EventTypeChatError),
		"error":       fmt.Sprintf(unknownTeamModeNotice, mode, strings.Join(knownTeamModeInputs, ", ")),
		"code":        "UNKNOWN_TEAM_MODE",
		"is_complete": true,
	}
	out := &schema.Message{
		ID:              msg.ID,
		Type:            schema.MessageTypeEvent,
		ChannelID:       msg.ChannelID,
		SessionID:       msg.SessionID,
		Params:          json.RawMessage(`{}`),
		Timestamp:       schema.NowTimestamp(),
		OK:              false,
		Payload:         payload,
		EventType:       schema.EventTypeChatError,
		Metadata:        msg.Metadata,
		EnableStreaming: false,
	}
	mh.PublishRobotMessages(out)
	logger.Warn(logComponent).
		Str("channel_id", msg.ChannelID).
		Str("session_id", msg.SessionID).
		Str("mode", mode).
		Msg("rejected unknown team.* chat.send")
}

// GenerateChannelSessionID 生成渠道会话标识。
// 注意：0.2.5 起网关不再自造会话 id（改由 AgentServer 的 session.create 分配），
// 本函数仅保留给历史调用方（如无 AgentServer 的本地场景）。
func GenerateChannelSessionID(channelID string) string {
	ts := fmt.Sprintf("%x", time.Now().UnixMilli())
	suffix := generateRandomHex(3) // 3 字节 = 6 个十六进制字符
	return fmt.Sprintf("%s_%s_%s", channelID, ts, suffix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// channelModeFromStr 把任意 mode 字符串归一到 ChannelMode。
// Python: message_handler.py:233-263 channel_mode_from_str：
//  1. deprecate_mode 先把旧 canonical 静默映射到新 canonical（Go 等价物 schema.DeprecateMode）
//  2. 命中 NEW_CANONICAL_MODES → 直接构造
//  3. 未命中 → 兜底 ChannelMode.AGENT，并按"是否发生归一"分别记 Debug 日志
//
// 与 Python 的差异仅在日志级别映射：Python 的 `except ValueError` 分支在 Go 的
// 字符串枚举下不可达（值即字符串，不存在构造失败），故 Warn 只在兜底时记录。
func channelModeFromStr(modeStr string) ChannelMode {
	newModeStr := schema.DeprecateMode(modeStr)
	if schema.IsNewCanonicalMode(newModeStr) {
		if channelModeMembers[ChannelMode(newModeStr)] {
			logger.Debug(logComponent).
				Str("raw", modeStr).
				Str("mode", newModeStr).
				Msg("channel_mode_from_str: hit new canonical mode")
			return ChannelMode(newModeStr)
		}
		// Python: except ValueError（canonical 命中集合但不是枚举成员，如 team.design）
		logger.Warn(logComponent).
			Str("raw", modeStr).
			Str("mode", newModeStr).
			Msg("channel_mode_from_str: canonical not a ChannelMode member, fallback AGENT")
		return ChannelModeAgent
	}
	raw := strings.TrimSpace(strings.ToLower(modeStr))
	if newModeStr != raw {
		logger.Debug(logComponent).
			Str("raw", modeStr).
			Str("canonical", newModeStr).
			Msg("channel_mode_from_str: legacy canonical normalized, fallback AGENT")
	} else {
		logger.Debug(logComponent).
			Str("raw", modeStr).
			Msg("channel_mode_from_str: not in NEW_CANONICAL_MODES, fallback AGENT")
	}
	return ChannelModeAgent
}

// resolveControlChannelType 解析消息对应的受控渠道类型。
// Python: _resolve_control_channel_type — 优先 msg.provider，fallback msg.channel_id。
// Python 不查 ChannelManager，直接从消息字段推断。
func (mh *MessageHandler) resolveControlChannelType(msg *schema.Message) channel_manager.ChannelType {
	// 优先 msg.Provider（对齐 Python: msg.provider）
	if msg.Provider != "" {
		return channel_manager.ChannelType(msg.Provider)
	}
	// M-06: fallback 原样返回 msg.ChannelID（对齐 Python: return str(msg.channel_id)）。
	// 原实现走 inferChannelTypeFromID 前缀映射，未知前缀静默归为 web——
	// 过度受控：Python 的 fallback 语义是"原样透传"，由 controlChannelTypes
	// 集合判定是否受控，未知值不应被改写。
	return channel_manager.ChannelType(msg.ChannelID)
}

// getChannelDefaultState 从 config 读取渠道默认会话与模式。
// Python: _get_channel_default_state (L889-907)，步骤逐条对齐：
// cfg → channels → ch_cfg → default_session_id → default_mode（缺省 "agent"）→
// channel_mode_from_str。**不在此处生成 session_id**：0.2.5 起由
// EnsureInboundSession → allocateChannelSession 分配（见 spec §7 D1）。
func (mh *MessageHandler) getChannelDefaultState(channelID string) *ChannelControlState {
	config := map[string]any{}
	if mh.getConfigRaw != nil {
		if cfg := mh.getConfigRaw(); cfg != nil {
			config = cfg
		}
	}
	channels, _ := config["channels"].(map[string]any)
	chConfig, _ := channels[channelID].(map[string]any)
	sidRaw, _ := chConfig["default_session_id"].(string)
	sid := strings.TrimSpace(sidRaw)
	modeRaw, _ := chConfig["default_mode"].(string)
	if strings.TrimSpace(modeRaw) == "" {
		modeRaw = "agent"
	}
	mode := channelModeFromStr(strings.ToLower(strings.TrimSpace(modeRaw)))
	return &ChannelControlState{
		SessionID: sid,
		Mode:      mode,
	}
}

// allocateChannelSession 为渠道分配并持久化一个 AgentServer 侧真实会话。
// Python: _allocate_channel_session (L951-994)：经 E2A `session.create` 建会话，
// 回包 session_id 写入 state 与企业通道 SessionMap。
func (mh *MessageHandler) allocateChannelSession(ctx context.Context, msg *schema.Message, state *ChannelControlState, persistSession bool) (string, error) {
	channelType := mh.resolveControlChannelType(msg)
	mode := string(state.GetMode())
	params := decodeMessageParams(msg)
	createParams := map[string]any{
		"create_token": generateRandomHex(16),
		"mode":         mode,
		"is_swarm":     schema.IsTeamMode(mode),
	}
	if persistSession {
		createParams["persist_session"] = true
	}
	for _, name := range []string{"project_id", "project_dir", "work_mode", "model_name"} {
		if v, ok := params[name]; ok && v != nil {
			createParams[name] = v
		}
	}
	requestID := fmt.Sprintf("session-create-%x-%s", time.Now().UnixMilli(), generateRandomHex(3))
	env := e2a.E2AFromAgentFields(requestID,
		e2a.WithFieldChannelID(string(channelType)),
		e2a.WithFieldReqMethod(string(schema.ReqMethodSessionCreate)),
		e2a.WithFieldParams(createParams),
		e2a.WithFieldIsStream(false),
		e2a.WithFieldTimestamp(float64(time.Now().UnixNano())/1e9),
	)
	resp, err := mh.agentClient.SendRequest(ctx, env)
	if err != nil {
		return "", fmt.Errorf("session.create failed: %w", err)
	}
	payload := map[string]any{}
	if resp != nil && resp.Payload != nil {
		payload = resp.Payload
	}
	if resp == nil || !resp.OK {
		return "", fmt.Errorf("session.create failed: %v", payload["error"])
	}
	sid := strings.TrimSpace(strScalar(payload["session_id"]))
	if sid == "" {
		sid = strings.TrimSpace(strScalar(payload["sessionId"]))
	}
	if sid == "" {
		return "", fmt.Errorf("session.create returned empty session_id")
	}
	state.SetSessionID(sid)
	if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(string(channelType)) {
		if err := mh.sessionMap.SetSessionID(p, c, b, u, sid); err != nil {
			return "", err
		}
	}
	return sid, nil
}

// rememberAllocStateKey 受控通道无正式会话时记录本次状态键，供分配后写回。
// Python: _remember_alloc_state_key (L996-1010)——小艺入站 session_id 是手机侧
// 逻辑会话 id，分配真实会话后若键退化成 channel 级，下次入站永远命中不到 →
// 每条 query 都新建 session、丢上下文。
func (mh *MessageHandler) rememberAllocStateKey(msg *schema.Message, channelID string) {
	cid := strings.TrimSpace(channelID)
	if !stickyChannelTypes[cid] {
		return
	}
	key := getChannelStateKey(cid, msg.SessionID)
	mh.statesMu.Lock()
	mh.pendingAllocStateKey[msg.ID] = key
	mh.statesMu.Unlock()
}

// getChannelStateKey 拼出「渠道 + 会话」的存储键。
func getChannelStateKey(channelID, sessionID string) string {
	if sessionID != "" {
		return fmt.Sprintf("%s:%s", channelID, sessionID)
	}
	return channelID
}

// saveChannelStateToConfig 保存渠道状态到 config。
// Python: _save_channel_state_to_config (L940-950)：
// 调用 updateChannelInConfig 注入 default_session_id 和 default_mode。
func (mh *MessageHandler) saveChannelStateToConfig(channelID string) {
	if mh.updateChannelInConfig == nil {
		return
	}

	mh.statesMu.RLock()
	state, exists := mh.channelStates[channelID]
	mh.statesMu.RUnlock()

	if !exists {
		return
	}

	mh.updateChannelInConfig(channelID, map[string]any{
		"default_session_id": state.GetSessionID(),
		"default_mode":       string(state.GetMode()),
	})
}

// decodeMessageParams 解析消息 params 为可写 map（nil/非法时返回空 map）。
func decodeMessageParams(msg *schema.Message) map[string]any {
	params := map[string]any{}
	if msg == nil || len(msg.Params) == 0 {
		return params
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return map[string]any{}
	}
	if params == nil {
		params = map[string]any{}
	}
	return params
}

// encodeMessageParams 把 params 写回消息。
func encodeMessageParams(msg *schema.Message, params map[string]any) {
	if msg == nil {
		return
	}
	updated, err := json.Marshal(params)
	if err != nil {
		return
	}
	msg.Params = json.RawMessage(updated)
}

// extractIdentityTuple 提取消息身份四元组。
// Python: _extract_identity_tuple (L321-329)：
// 四字段 TrimSpace 后任一为空返回 ok=false（对齐 Python all(identity_parts)）
func extractIdentityTuple(msg *schema.Message) (provider, chatID, botID, userID string, ok bool) {
	provider = strings.TrimSpace(msg.Provider)
	chatID = strings.TrimSpace(msg.ChatID)
	botID = strings.TrimSpace(msg.BotID)
	userID = strings.TrimSpace(msg.UserID)
	if provider == "" || chatID == "" || botID == "" || userID == "" {
		return "", "", "", "", false
	}
	return provider, chatID, botID, userID, true
}

// channelIDMatchesSessionMapTypes channel_id 是否属于 SessionMap 通道族。
// Python: _channel_id_matches_session_map_types (L331-337)：
// 精确匹配或 "base:" 前缀
func channelIDMatchesSessionMapTypes(channelID string) bool {
	cid := strings.TrimSpace(channelID)
	for base := range sessionMapChannelTypes {
		if cid == base || strings.HasPrefix(cid, base+":") {
			return true
		}
	}
	return false
}

// generateRandomHex 生成指定长度的随机十六进制串，用作临时标识。
func generateRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// fallback：使用时间戳
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return fmt.Sprintf("%x", b)
}

func init() {
	// 构建受控渠道类型集合
	// Python: _control_channel_types：feishu, xiaoyi, dingtalk, whatsapp, wecom, wechat
	controlChannelTypes = map[string]bool{
		string(channel_manager.ChannelTypeFeishu):   true,
		string(channel_manager.ChannelTypeXiaoyi):   true,
		string(channel_manager.ChannelTypeDingTalk): true,
		string(channel_manager.ChannelTypeWhatsApp): true,
		string(channel_manager.ChannelTypeWeCom):    true,
		string(channel_manager.ChannelTypeWeChat):   true,
	}

	// 构建企业通道会话映射族集合
	// Python: _session_map_channel_types = frozenset({"feishu_enterprise"})
	sessionMapChannelTypes = map[string]bool{
		"feishu_enterprise": true,
	}

	// 构建 sticky 通道集合
	// Python: _sticky_channel_state_types = frozenset({ChannelType.XIAOYI.value})
	stickyChannelTypes = map[string]bool{
		string(channel_manager.ChannelTypeXiaoyi): true,
	}

	// 派生已知 Team 模式清单（Python: sorted(m for m in _VALID_MODE_INPUTS if is_team_mode(m))）
	for mode := range validModeInputs {
		if schema.IsTeamMode(mode) {
			knownTeamModeInputs = append(knownTeamModeInputs, mode)
		}
	}
	sort.Strings(knownTeamModeInputs)
}
