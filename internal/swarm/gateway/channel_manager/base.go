package channel_manager

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelMetadata Channel 元数据，标识一条通道实例的来源与归属。
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/base.py (ChannelMetadata)
type ChannelMetadata struct {
	// ChannelID 渠道实例唯一标识
	ChannelID string `json:"channel_id"`
	// Source 来源平台标识（如 feishu/dingtalk/web）
	Source string `json:"source"`
	// UserID 用户标识（可选）
	UserID string `json:"user_id,omitempty"`
	// Extra 扩展字段（可选）
	Extra map[string]any `json:"extra,omitempty"`
}

// ──────────────────────────── 接口 ────────────────────────────

// BaseChannel Channel 实现的抽象接口。
//
// 每个 Channel 都应实现此接口以集成到 Gateway 消息总线中。
// 方法对齐 Python BaseChannel ABC：Config/Start/Stop/Send/OnMessage/IsRunning。
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/base.py (BaseChannel)
type BaseChannel interface {
	// Config 返回当前 Channel 配置（任意类型，具体由各实现定义）
	Config() any
	// Start 启动 Channel 并开始监听消息（长期运行的协程，需监听传入消息并通过 OnMessage 转发到总线）
	Start(ctx context.Context) error
	// Stop 停止 Channel 并清理资源
	Stop(ctx context.Context) error
	// Send 通过 Channel 发送消息到外部平台
	Send(ctx context.Context, msg *schema.Message) error
	// OnMessage 注册入站消息回调（Channel 收到外部平台消息时调用）
	OnMessage(callback func(*schema.Message))
	// IsRunning 返回 Channel 是否正在运行
	IsRunning() bool
	// ChannelID 返回 Channel 唯一标识
	ChannelID() string
	// ChannelType 返回 Channel 类型
	ChannelType() ChannelType
}

// ──────────────────────────── 枚举 ────────────────────────────

// ChannelType Channel 类型枚举，标识不同的 IM 平台渠道。
//
// 对应 Python: jiuwenswarm/gateway/channel_manager/base.py (ChannelType)
type ChannelType string

const (
	// ChannelTypeACP ACP 协议通道
	ChannelTypeACP ChannelType = "acp"
	// ChannelTypeWeb Web 通道
	ChannelTypeWeb ChannelType = "web"
	// ChannelTypeFeishu 飞书通道
	ChannelTypeFeishu ChannelType = "feishu"
	// ChannelTypeXiaoyi 小艺通道
	ChannelTypeXiaoyi ChannelType = "xiaoyi"
	// ChannelTypeDingTalk 钉钉通道
	ChannelTypeDingTalk ChannelType = "dingtalk"
	// ChannelTypeTelegram Telegram 通道
	ChannelTypeTelegram ChannelType = "telegram"
	// ChannelTypeDiscord Discord 通道
	ChannelTypeDiscord ChannelType = "discord"
	// ChannelTypeWhatsApp WhatsApp 通道
	ChannelTypeWhatsApp ChannelType = "whatsapp"
	// ChannelTypeWeCom 企微通道
	ChannelTypeWeCom ChannelType = "wecom"
	// ChannelTypeWeChat 微信通道
	ChannelTypeWeChat ChannelType = "wechat"
	// ChannelTypeCLI TUI/CLI 终端通道
	ChannelTypeCLI ChannelType = "tui"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// channelTypeLookup 字符串值到 ChannelType 枚举的查找表，用于 ParseChannelType/IsValidChannelType 的 O(1) 查找。
var channelTypeLookup map[string]ChannelType

// ──────────────────────────── 导出函数 ────────────────────────────

// AllChannelTypes 返回所有 ChannelType 枚举值。
func AllChannelTypes() []ChannelType {
	return []ChannelType{
		ChannelTypeACP,
		ChannelTypeWeb,
		ChannelTypeFeishu,
		ChannelTypeXiaoyi,
		ChannelTypeDingTalk,
		ChannelTypeTelegram,
		ChannelTypeDiscord,
		ChannelTypeWhatsApp,
		ChannelTypeWeCom,
		ChannelTypeWeChat,
		ChannelTypeCLI,
	}
}

// ParseChannelType 从字符串解析 ChannelType，不合法返回错误。
func ParseChannelType(s string) (ChannelType, error) {
	if ct, ok := channelTypeLookup[s]; ok {
		return ct, nil
	}
	return ChannelType(""), fmt.Errorf("不合法的 ChannelType 值: %q", s)
}

// IsValidChannelType 判断字符串是否为合法的 ChannelType 值。
func IsValidChannelType(s string) bool {
	_, ok := channelTypeLookup[s]
	return ok
}

// String 实现 fmt.Stringer 接口。
func (ct ChannelType) String() string {
	return string(ct)
}

// GoString 实现 fmt.GoStringer 接口，返回带类型名前缀的字符串表示。
func (ct ChannelType) GoString() string {
	return fmt.Sprintf("channel_manager.ChannelType(%q)", string(ct))
}

// IsAllowed 检查发送者是否被允许使用此 Channel。
//
// allowFrom 为空时允许所有人；senderID 中含 "|" 时按分隔符逐段匹配。
//
// 对应 Python: BaseChannel.is_allowed()
func IsAllowed(senderID string, allowFrom []string) bool {
	if len(allowFrom) == 0 {
		return true
	}
	if containsString(allowFrom, senderID) {
		return true
	}
	if strings.Contains(senderID, "|") {
		for _, part := range strings.Split(senderID, "|") {
			if part != "" && containsString(allowFrom, part) {
				return true
			}
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsString 检查字符串切片中是否包含目标值。
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

func init() {
	// 构建 ChannelType 查找表
	cts := AllChannelTypes()
	channelTypeLookup = make(map[string]ChannelType, len(cts))
	for _, ct := range cts {
		channelTypeLookup[string(ct)] = ct
	}
}
