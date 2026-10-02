package ltm

import (
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// addMessagesParams AddMessages 的全部参数（对齐 Python **kwargs）。
type addMessagesParams struct {
	// Messages 当前轮次消息列表
	Messages []llmschema.BaseMessage
	// AgentConfig Agent 记忆配置
	AgentConfig *config.AgentMemoryConfig
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// SessionID 会话标识
	SessionID string
	// Timestamp 时间戳
	Timestamp *time.Time
	// GenMem 是否生成记忆
	GenMem bool
	// GenMemWithHistoryMsgNum 获取历史消息窗口大小
	GenMemWithHistoryMsgNum int
}

// userScopeParams 用户+作用域通用参数，被搜索/删除/更新/查询操作复用。
type userScopeParams struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
}

// searchParams SearchUserMem / SearchUserHistorySummary 的全部参数。
type searchParams struct {
	// Query 搜索查询
	Query string
	// Num 返回结果数
	Num int
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// Threshold 最低相关度阈值
	Threshold float64
}

// ──────────────────────────── 枚举 ────────────────────────────

// AddMessagesOption AddMessages 的可选参数。
type AddMessagesOption func(*addMessagesParams)

// UserScopeOption 用户+作用域通用可选参数，被搜索/删除/更新/查询操作复用。
type UserScopeOption func(*userScopeParams)

// SearchOption SearchUserMem 的可选参数。
type SearchOption func(*searchParams)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultValue 占位符默认值（对齐 Python DEFAULT_VALUE = "__default__"）
	DefaultValue = "__default__"
	// ScopeConfigKey scope 配置在 KV 存储中的前缀（对齐 Python SCOPE_CONFIG_KEY）
	ScopeConfigKey = "memory_scope_config"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// WithUserID 设置用户标识。
func WithUserID(uid string) AddMessagesOption {
	return func(p *addMessagesParams) { p.UserID = uid }
}

// WithScopeID 设置作用域标识。
func WithScopeID(sid string) AddMessagesOption {
	return func(p *addMessagesParams) { p.ScopeID = sid }
}

// WithSessionID 设置会话标识。
func WithSessionID(sid string) AddMessagesOption {
	return func(p *addMessagesParams) { p.SessionID = sid }
}

// WithTimestamp 设置时间戳。
func WithTimestamp(t time.Time) AddMessagesOption {
	return func(p *addMessagesParams) { p.Timestamp = &t }
}

// WithGenMem 设置是否生成记忆。
func WithGenMem(gen bool) AddMessagesOption {
	return func(p *addMessagesParams) { p.GenMem = gen }
}

// WithGenMemWithHistoryMsgNum 设置获取历史消息窗口大小。
func WithGenMemWithHistoryMsgNum(n int) AddMessagesOption {
	return func(p *addMessagesParams) { p.GenMemWithHistoryMsgNum = n }
}

// Uid 设置用户标识。
func Uid(uid string) UserScopeOption {
	return func(p *userScopeParams) { p.UserID = uid }
}

// Sid 设置作用域标识。
func Sid(sid string) UserScopeOption {
	return func(p *userScopeParams) { p.ScopeID = sid }
}

// SearchWithUserID 设置用户标识。
func SearchWithUserID(uid string) SearchOption {
	return func(p *searchParams) { p.UserID = uid }
}

// SearchWithScopeID 设置作用域标识。
func SearchWithScopeID(sid string) SearchOption {
	return func(p *searchParams) { p.ScopeID = sid }
}

// SearchWithThreshold 设置最低相关度阈值。
func SearchWithThreshold(threshold float64) SearchOption {
	return func(p *searchParams) { p.Threshold = threshold }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAddMessagesParams 从选项构建参数（对齐 Python add_messages 的默认值）。
func newAddMessagesParams(messages []llmschema.BaseMessage, agentConfig *config.AgentMemoryConfig, opts ...AddMessagesOption) *addMessagesParams {
	p := &addMessagesParams{
		Messages:                messages,
		AgentConfig:             agentConfig,
		UserID:                  DefaultValue,
		ScopeID:                 DefaultValue,
		SessionID:               DefaultValue,
		GenMem:                  true,
		GenMemWithHistoryMsgNum: 2,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// newUserScopeParams 从选项构建用户+作用域参数。
func newUserScopeParams(opts ...UserScopeOption) *userScopeParams {
	p := &userScopeParams{
		UserID:  DefaultValue,
		ScopeID: DefaultValue,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// newSearchParams 从选项构建搜索参数。
func newSearchParams(query string, num int, opts ...SearchOption) *searchParams {
	p := &searchParams{
		Query:     query,
		Num:       num,
		UserID:    DefaultValue,
		ScopeID:   DefaultValue,
		Threshold: 0.3,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}
