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

// ChannelControlState 渠道控制状态，跟踪 session_id 和 mode
type ChannelControlState struct {
	// SessionID 当前会话标识
	SessionID string
	// Mode 当前渠道模式
	Mode ChannelMode
}

// ──────────────────────────── 枚举 ────────────────────────────

// ChannelMode 渠道模式枚举
type ChannelMode int

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

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// channelModeStrings ChannelMode 到字符串的映射
var channelModeStrings = map[ChannelMode]string{
	ChannelModeAgentPlan:  "agent.plan",
	ChannelModeAgentFast:  "agent.fast",
	ChannelModeCodePlan:   "code.plan",
	ChannelModeCodeNormal: "code.normal",
	ChannelModeCodeTeam:   "code.team",
	ChannelModeTeam:       "team",
}

// channelModeLookup 字符串到 ChannelMode 的查找表
var channelModeLookup map[string]ChannelMode

// controlChannelTypes 受控渠道类型集合（slash 命令仅在受控渠道上生效）
var controlChannelTypes map[string]bool

// ──────────────────────────── 导出函数 ────────────────────────────

// ChannelModeString 返回 ChannelMode 的字符串值
func ChannelModeString(m ChannelMode) string {
	if s, ok := channelModeStrings[m]; ok {
		return s
	}
	return "agent.plan"
}

// ParseChannelMode 从字符串解析 ChannelMode，不合法返回 ChannelModeAgentPlan
func ParseChannelMode(s string) ChannelMode {
	s = strings.TrimSpace(strings.ToLower(s))
	if m, ok := channelModeLookup[s]; ok {
		return m
	}
	return ChannelModeAgentPlan
}

// IsValidChannelMode 判断字符串是否为合法 ChannelMode 值
func IsValidChannelMode(s string) bool {
	_, ok := channelModeLookup[strings.TrimSpace(strings.ToLower(s))]
	return ok
}

// ApplyChannelState 将当前 Channel 的控制状态应用到消息上（session_id / mode）
//
// 对齐 Python _apply_channel_state：
//  1. 检查渠道类型是否受控
//  2. 获取或创建渠道状态
//  3. 注入 session_id（如果 state 有 sessionID 则覆盖 msg.SessionID）
//  4. 注入 mode 到 params["mode"]（setdefault 语义）
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

// GetOrCreateChannelState 获取或创建消息对应 channel 状态
//
// 对齐 Python _get_or_create_channel_state：使用 channel_id 作为 key。
func (mh *MessageHandler) GetOrCreateChannelState(msg *schema.Message) *ChannelControlState {
	ch := msg.ChannelID
	key := ch

	mh.statesMu.RLock()
	state, exists := mh.channelStates[key]
	mh.statesMu.RUnlock()

	if exists {
		return state
	}

	// 创建默认状态
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

// GenerateChannelSessionID 为指定 channel 生成新的 session_id
//
// 对齐 Python _generate_channel_session_id：
// 格式：{channelID}_{hex_timestamp}_{6_random_hex}
func GenerateChannelSessionID(channelID string) string {
	ts := fmt.Sprintf("%x", time.Now().UnixMilli())
	suffix := generateRandomHex(3) // 3 字节 = 6 个十六进制字符
	return fmt.Sprintf("%s_%s_%s", channelID, ts, suffix)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveControlChannelType 解析消息对应的受控渠道类型
func (mh *MessageHandler) resolveControlChannelType(msg *schema.Message) channel_manager.ChannelType {
	chID := msg.ChannelID
	if chID == "" {
		return ""
	}

	// 尝试从 ChannelManager 中查找渠道类型
	if mh.channelMgr != nil {
		if ch := mh.channelMgr.GetChannel(chID); ch != nil {
			return ch.ChannelType()
		}
	}

	// fallback：从 channelID 前缀推断
	return inferChannelTypeFromID(chID)
}

// getChannelDefaultState 从默认配置创建渠道状态
//
// 对齐 Python _get_channel_default_state：默认 mode 为 agent.plan，
// 默认 session_id 为新生成的 ID。
func (mh *MessageHandler) getChannelDefaultState(channelID string) *ChannelControlState {
	sid := GenerateChannelSessionID(channelID)
	return &ChannelControlState{
		SessionID: sid,
		Mode:      ChannelModeAgentPlan,
	}
}

// getChannelStateKey 生成 channel 状态的复合键
func getChannelStateKey(channelID, sessionID string) string {
	if sessionID != "" {
		return fmt.Sprintf("%s:%s", channelID, sessionID)
	}
	return channelID
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

// generateRandomHex 生成 n 字节的随机 hex 字符串
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

// 确保 sync 在 import 中可用（channelStates 使用 statesMu sync.RWMutex）
var _ sync.RWMutex //nolint:unused // 保留 import 引用
