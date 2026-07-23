package message_handler

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

type ChannelControlState struct {
	// SessionID 当前会话标识
	SessionID string
	// Mode 当前渠道模式
	Mode ChannelMode
}

// ──────────────────────────── 枚举 ────────────────────────────

type ChannelMode int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ChannelModeAgentPlan agent.plan 模式
	ChannelModeAgentPlan ChannelMode = iota
	// ChannelModeAgentFast agent.fast 模式
	ChannelModeAgentFast
	// ChannelModeCodePlan code.plan 模式
	ChannelModeCodePlan
	// ChannelModeCodeNormal code.normal 模式
	ChannelModeCodeNormal
	// ChannelModeCodeTeam code.team 模式
	ChannelModeCodeTeam
	// ChannelModeTeam team 模式
	ChannelModeTeam
)

// ──────────────────────────── 全局变量 ────────────────────────────

var channelModeStrings = map[ChannelMode]string{
	ChannelModeAgentPlan:  "agent.plan",
	ChannelModeAgentFast:  "agent.fast",
	ChannelModeCodePlan:   "code.plan",
	ChannelModeCodeNormal: "code.normal",
	ChannelModeCodeTeam:   "code.team",
	ChannelModeTeam:       "team",
}

var channelModeLookup map[string]ChannelMode

var controlChannelTypes map[string]bool

var _ sync.RWMutex //nolint:unused // 保留 import 引用

// ──────────────────────────── 导出函数 ────────────────────────────

func ChannelModeString(m ChannelMode) string {
	if s, ok := channelModeStrings[m]; ok {
		return s
	}
	return "agent.plan"
}

func ParseChannelMode(s string) ChannelMode {
	s = strings.TrimSpace(strings.ToLower(s))
	if m, ok := channelModeLookup[s]; ok {
		return m
	}
	return ChannelModeAgentPlan
}

func IsValidChannelMode(s string) bool {
	_, ok := channelModeLookup[strings.TrimSpace(strings.ToLower(s))]
	return ok
}

func (mh *MessageHandler) ApplyChannelState(msg *schema.Message) {
	channelType := mh.resolveControlChannelType(msg)
	if !controlChannelTypes[string(channelType)] {
		return
	}
	state := mh.GetOrCreateChannelState(msg)

	if state.SessionID != "" {
		msg.SessionID = state.SessionID
	}

	// 将 mode 写入 params，后续 E2A / Agent 侧从 params["mode"] 读取
	if len(msg.Params) == 0 {
		msg.Params = json.RawMessage(`{}`)
	}

	// 解析现有 params，设置 mode（setdefault 语义）
	var paramsMap map[string]any
	if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
		paramsMap = make(map[string]any)
	}
	if _, exists := paramsMap["mode"]; !exists {
		paramsMap["mode"] = ChannelModeString(state.Mode)
	}
	if updated, err := json.Marshal(paramsMap); err == nil {
		msg.Params = json.RawMessage(updated)
	}
}

// GetOrCreateChannelState 获取或创建消息对应 channel 状态。
//
// 对齐 Python _get_or_create_channel_state (L278-299)：
// 使用复合键 channelID:sessionID（对齐 Python _get_channel_state_key）。
// TODO(#11.7): SessionMap 集成（等 11.7 回填）。
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

func GenerateChannelSessionID(channelID string) string {
	ts := fmt.Sprintf("%x", time.Now().UnixMilli())
	suffix := generateRandomHex(3) // 3 字节 = 6 个十六进制字符
	return fmt.Sprintf("%s_%s_%s", channelID, ts, suffix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveControlChannelType 解析消息对应的受控渠道类型。
//
// 对齐 Python: _resolve_control_channel_type — 优先 msg.provider，fallback msg.channel_id。
// Python 不查 ChannelManager，直接从消息字段推断。
func (mh *MessageHandler) resolveControlChannelType(msg *schema.Message) channel_manager.ChannelType {
	// 优先 msg.Provider（对齐 Python: msg.provider）
	if msg.Provider != "" {
		return channel_manager.ChannelType(msg.Provider)
	}
	// fallback: 从 channelID 推断（对齐 Python: msg.channel_id）
	return inferChannelTypeFromID(msg.ChannelID)
}

// getChannelDefaultState 从默认配置创建渠道状态。
//
// 对齐 Python _get_channel_default_state (L247-270)：
// 如果 getConfigRaw 不为 nil，从 config 中读取 channels[channelID] 的 default_session_id 和 default_mode；
// 否则默认 mode 为 agent.plan，默认 session_id 为新生成的 ID。
func (mh *MessageHandler) getChannelDefaultState(channelID string) *ChannelControlState {
	if mh.getConfigRaw != nil {
		if config := mh.getConfigRaw(); config != nil {
			if channels, ok := config["channels"].(map[string]any); ok {
				if chConfig, ok := channels[channelID].(map[string]any); ok {
					sid := ""
					if s, ok := chConfig["default_session_id"].(string); ok && s != "" {
						sid = s
					}
					mode := ChannelModeAgentPlan
					if m, ok := chConfig["default_mode"].(string); ok && m != "" {
						mode = ParseChannelMode(m)
					}
					if sid == "" {
						sid = GenerateChannelSessionID(channelID)
					}
					return &ChannelControlState{
						SessionID: sid,
						Mode:      mode,
					}
				}
			}
		}
	}

	sid := GenerateChannelSessionID(channelID)
	return &ChannelControlState{
		SessionID: sid,
		Mode:      ChannelModeAgentPlan,
	}
}

func getChannelStateKey(channelID, sessionID string) string {
	if sessionID != "" {
		return fmt.Sprintf("%s:%s", channelID, sessionID)
	}
	return channelID
}

// saveChannelStateToConfig 保存渠道状态到 config。
//
// 对齐 Python _save_channel_state_to_config (L301-312)：
// 调用 updateChannelInConfig 注入 default_session_id 和 default_mode。
// 注：Python 中此方法当前未被调用（dead code），但补定义以对齐。
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
		"default_session_id": state.SessionID,
		"default_mode":       ChannelModeString(state.Mode),
	})
}

// inferChannelTypeFromID 从 channelID 推断渠道类型
func inferChannelTypeFromID(channelID string) channel_manager.ChannelType {
	lower := strings.ToLower(channelID)
	prefixes := map[string]channel_manager.ChannelType{
		"web":      channel_manager.ChannelTypeWeb,
		"feishu":   channel_manager.ChannelTypeFeishu,
		"xiaoyi":   channel_manager.ChannelTypeXiaoyi,
		"dingtalk": channel_manager.ChannelTypeDingTalk,
		"telegram": channel_manager.ChannelTypeTelegram,
		"discord":  channel_manager.ChannelTypeDiscord,
		"whatsapp": channel_manager.ChannelTypeWhatsApp,
		"wecom":    channel_manager.ChannelTypeWeCom,
		"wechat":   channel_manager.ChannelTypeWeChat,
		"tui":      channel_manager.ChannelTypeCLI,
		"acp":      channel_manager.ChannelTypeACP,
	}
	for prefix, ct := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return ct
		}
	}
	return channel_manager.ChannelTypeWeb
}

func generateRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// fallback：使用时间戳
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n*2]
	}
	return fmt.Sprintf("%x", b)
}

func init() {
	// 构建 ChannelMode 查找表
	channelModeLookup = make(map[string]ChannelMode, len(channelModeStrings))
	for m, s := range channelModeStrings {
		channelModeLookup[s] = m
	}

	// 构建受控渠道类型集合
	// 对齐 Python _control_channel_types：feishu, xiaoyi, dingtalk, whatsapp, wecom, wechat
	controlChannelTypes = map[string]bool{
		string(channel_manager.ChannelTypeFeishu):   true,
		string(channel_manager.ChannelTypeXiaoyi):   true,
		string(channel_manager.ChannelTypeDingTalk): true,
		string(channel_manager.ChannelTypeWhatsApp): true,
		string(channel_manager.ChannelTypeWeCom):    true,
		string(channel_manager.ChannelTypeWeChat):   true,
	}
}
