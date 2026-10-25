package im_pipeline

import (
	"fmt"
	"strconv"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IMPlatformAdapter 统一的平台适配器接口，同时服务入站和出站管线。
// 每个数字分身 IM 平台（飞书 11.16 / 企微 11.21）实现一个适配器，
// 注册后供 IMInboundPipeline 和 IMOutboundPipeline 共享使用。
//
// Python: im_inbound.py L67-127 (IMPlatformAdapter Protocol，10 方法 + channel_id
// 类属性经构造上下文隐含——注册表 key 即 channel_id)
type IMPlatformAdapter interface {
	// GetPrincipalUserID 返回数字分身主体用户 ID
	// Python: get_principal_user_id (L78)
	GetPrincipalUserID() string
	// GetPrincipalDisplayName 返回主体显示名
	// Python: get_principal_display_name (L81)
	GetPrincipalDisplayName() string
	// ResolveUserDisplayName 将用户 ID 解析为显示名
	// Python: resolve_user_display_name (L84)
	ResolveUserDisplayName(userID string) string
	// GetBotMentionTokens 返回机器人提及 token 列表
	// Python: get_bot_mention_tokens (L87)
	GetBotMentionTokens() []string
	// LoadRecentMessages 加载 thread 群聊历史（limit 默认 500）
	// Python: load_recent_messages (L90-92)
	LoadRecentMessages(threadID string, limit int) []IMHistoryMessage
	// BuildRelevanceMetadata 构建相关性 metadata 补丁
	// Python: build_relevance_metadata (L95-101)
	BuildRelevanceMetadata(metadata map[string]any, senderUserID string, relevant bool) map[string]any
	// PlatformName 平台显示名，用于 LLM prompt（飞书/企业微信）
	// Python: platform_name 属性 (L106-109)
	PlatformName() string
	// ReplyUserIDKey metadata 中设置回复目标用户 ID 的 key
	// 飞书: "reply_feishu_open_id"；企微: "reply_wecom_user_id"
	// Python: reply_user_id_key 属性 (L111-118)
	ReplyUserIDKey() string
	// UseKeywordOverride LLM 判断为 CHAT 但关键词命中时，是否覆盖为 DM。
	// Python Protocol 声明，全仓零消费——死字段保留，飞书/企微均返回 true（偏差①）。
	// Python: use_keyword_override 属性 (L120-123)
	UseKeywordOverride() bool
	// GetCandidateUserID 从 metadata 中提取候选私发目标用户 ID；无候选返回空串
	// Python: get_candidate_user_id (L125-127)
	GetCandidateUserID(metadata map[string]any) string
}

// imUserIDNameResolver 可选能力接口（非导出）：根据用户名解析平台用户 ID。
// 对齐 Python `hasattr(adapter, "resolve_user_id_by_name")` 动态探测；
// 消费点在 handleGroupFollowup 目标解析三层的第一层；飞书/企微真实适配器均实现
// （11.16/11.21），测试 fakeAdapter 按需实现。13.7 三态接口先例。
type imUserIDNameResolver interface {
	// ResolveUserIDByName 根据用户名在群聊历史中查找对应平台用户 ID
	ResolveUserIDByName(name string) string
}

// IMHistoryMessage IM 群聊历史消息。
// Python: im_inbound.py L59-64 (dataclass IMHistoryMessage)
type IMHistoryMessage struct {
	// UserID 发送者平台用户 ID
	UserID string `json:"user_id"`
	// UserName 发送者显示名
	UserName string `json:"user_name"`
	// Content 消息内容
	Content string `json:"content"`
	// TimestampMs 毫秒时间戳
	TimestampMs int64 `json:"timestamp_ms"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识（Gateway 域）
const logComponent = logger.ComponentGateway

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// truthy 对齐 Python bool() 真值语义（JSON 域值：nil/false/空串/0/空容器为假）。
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	case map[string]any:
		return len(t) > 0
	case []any:
		return len(t) > 0
	default:
		return true
	}
}

// pythonStr 对齐 Python str() 显示语义（JSON 域值）。
// 与 message_handler.pythonOrStr 先例同款语义复制（跨包非导出无法复用）；
// float 系数整除无小数（strconv.FormatFloat -1），bool 取 "True"/"False"。
func pythonStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		return fmt.Sprint(v)
	}
}

// orStr 对齐 Python str(a or b or ...)：取第一个 truthy 值转字符串，全 falsy 返回空串。
func orStr(values ...any) string {
	for _, v := range values {
		if truthy(v) {
			return pythonStr(v)
		}
	}
	return ""
}

// truncateStr 截断字符串（Python [:n] 字节域近似，resolveInboundReferences 8000 截断先例）。
func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
