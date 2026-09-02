package security

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionResult 权限判定结果
type PermissionResult struct {
	// Permission 权限级别
	Permission PermissionLevel `json:"permission"`
	// MatchedRule 匹配的规则标识
	MatchedRule string `json:"matched_rule,omitempty"`
	// Reason 判定原因
	Reason string `json:"reason,omitempty"`
	// ExternalPaths 涉及的外部路径
	ExternalPaths []string `json:"external_paths,omitempty"`
}

// PermissionConfirmResponse 工具权限确认响应
//
// 在 ASK 场景下用户对「允许一次 / 记住并写回策略 / 拒绝」的确认结果。
// Approved 且 AutoConfirm 时，护栏走合并 permissions、更新内存并写盘的路径；
// 仅 Approved 则为本次放行。
type PermissionConfirmResponse struct {
	// Approved 是否批准
	Approved bool `json:"approved"`
	// Feedback 用户反馈
	Feedback string `json:"feedback,omitempty"`
	// AutoConfirm 是否自动确认（记住并写回策略）
	AutoConfirm bool `json:"auto_confirm,omitempty"`
}

// ApprovalOverrideEntry 用户/CLI 覆盖条目
//
// match_type 表示 pattern 作用在哪种输入上（如 path 对路径参数、command 对命令文本）；
// pattern 则是该维度上的具体表达式（re:… 正则或路径/通配写法）。
type ApprovalOverrideEntry struct {
	// ID 覆盖条目标识
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// Tools 适用的工具列表
	Tools []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	// MatchType 匹配类型（如 path、command）
	MatchType string `json:"match_type,omitempty" yaml:"match_type,omitempty"`
	// Pattern 匹配模式（正则或通配）
	Pattern string `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	// Action 执行动作（allow/ask/deny）
	Action string `json:"action,omitempty" yaml:"action,omitempty"`
}

// PermissionsSection 权限配置段
//
// 与 agent YAML 中 permissions: 段落常见字段对齐。
// schema（可选）：建议写 tiered_policy 等，便于人类阅读或与旧文档对齐；
// 引擎不根据该字段切换实现路径。
type PermissionsSection struct {
	// Enabled 是否启用权限系统
	Enabled bool `json:"enabled" yaml:"enabled"`
	// Schema 权限策略模式名称
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty"`
	// Defaults 默认权限策略
	Defaults map[string]any `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	// Tools 工具级权限策略
	Tools map[string]any `json:"tools,omitempty" yaml:"tools,omitempty"`
	// Rules 权限规则列表
	Rules []map[string]any `json:"rules,omitempty" yaml:"rules,omitempty"`
	// ApprovalOverrides 用户/CLI 覆盖条目
	ApprovalOverrides []ApprovalOverrideEntry `json:"approval_overrides,omitempty" yaml:"approval_overrides,omitempty"`
	// ExternalDirectory 外部目录权限映射
	ExternalDirectory map[string]string `json:"external_directory,omitempty" yaml:"external_directory,omitempty"`
}

// PermissionSceneHookInput 传给 PermissionSceneHook 的入参。
//
// 对齐 Python: PermissionSceneHookInput (host.py L15-23)
type PermissionSceneHookInput struct {
	// Ctx 上下文
	Ctx any
	// ToolCall 工具调用
	ToolCall any
	// UserInput 用户输入
	UserInput any
	// NormalizedToolName 归一化工具名
	NormalizedToolName string
	// ToolArgs 工具参数
	ToolArgs map[string]any
	// Engine 权限引擎
	Engine any
}

// PermissionConfirmationRequest 传给 RequestPermissionConfirmationHook 的入参。
//
// 对齐 Python: PermissionConfirmationRequest (host.py L36-43)
type PermissionConfirmationRequest struct {
	// Ctx 上下文
	Ctx any
	// ToolCall 工具调用
	ToolCall any
	// Result 权限判定结果
	Result *PermissionResult
	// AutoConfirmKey 自动确认键
	AutoConfirmKey string
}

// ToolPermissionHost 由 Agent 服务或 CLI 在构造 DeepAgent / PermissionInterruptRail 时注入。
//
// 对齐 Python: ToolPermissionHost (host.py L62-99)
type ToolPermissionHost struct {
	// GetPermissionsSnapshot 返回与 config['permissions'] 同结构的 dict
	GetPermissionsSnapshot func() map[string]any
	// PersistAllowRule 自定义「总是允许」写盘
	PersistAllowRule func(permissions map[string]any) bool
	// ResolveWorkspaceDir 外部路径校验用的 workspace 根目录
	ResolveWorkspaceDir func() string
	// PermissionYAMLPath Agent 配置文件路径
	PermissionYAMLPath string
	// ToolPermissionChecksActive 若返回假则跳过工具权限校验
	ToolPermissionChecksActive func() bool
	// RequestPermissionConfirmation 对 ASK 征求用户确认
	RequestPermissionConfirmation RequestPermissionConfirmationHook
	// PermissionSceneHook 宿主场景钩子
	PermissionSceneHook PermissionSceneHookFn
}

// ──────────────────────────── 枚举 ────────────────────────────

// PermissionLevel 权限级别枚举
type PermissionLevel int

const (
	// PermissionLevelAllow 允许执行，无需确认
	PermissionLevelAllow PermissionLevel = iota
	// PermissionLevelAsk 弹出确认框，用户决定
	PermissionLevelAsk
	// PermissionLevelDeny 拒绝执行，返回错误
	PermissionLevelDeny
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsePermissionLevel 从字符串解析 PermissionLevel
func ParsePermissionLevel(s string) (PermissionLevel, error) {
	switch strings.ToLower(s) {
	case "allow":
		return PermissionLevelAllow, nil
	case "ask":
		return PermissionLevelAsk, nil
	case "deny":
		return PermissionLevelDeny, nil
	default:
		return PermissionLevelAllow, fmt.Errorf("未知的 PermissionLevel: %q", s)
	}
}

// PermissionSceneHookFn 宿主场景钩子函数类型。
// 在通用 tiered 判定前介入（如数字分身 / owner_scopes）。
// 返回 nil 表示继续走引擎 tiered 判定；
// 返回 ("approve",) 直接放行；("reject", msg) 拒绝。
//
// 对齐 Python: PermissionSceneHook (host.py L26-33)
type PermissionSceneHookFn func(input PermissionSceneHookInput) ([]string, error)

// RequestPermissionConfirmationHook 对 PermissionLevel.ASK 征求用户确认的钩子。
//
// 对齐 Python: RequestPermissionConfirmationHook (host.py L48-51)
type RequestPermissionConfirmationHook func(req PermissionConfirmationRequest) (*PermissionConfirmResponse, error)

// IsAllowed 判断权限是否为允许
func (r *PermissionResult) IsAllowed() bool {
	return r.Permission == PermissionLevelAllow
}

// IsDenied 判断权限是否为拒绝
func (r *PermissionResult) IsDenied() bool {
	return r.Permission == PermissionLevelDeny
}

// NeedsApproval 判断是否需要用户确认
func (r *PermissionResult) NeedsApproval() bool {
	return r.Permission == PermissionLevelAsk
}

// String 返回 PermissionLevel 的字符串表示
func (l PermissionLevel) String() string {
	switch l {
	case PermissionLevelAllow:
		return "allow"
	case PermissionLevelAsk:
		return "ask"
	case PermissionLevelDeny:
		return "deny"
	default:
		return fmt.Sprintf("unknown(%d)", l)
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (l PermissionLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (l *PermissionLevel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("PermissionLevel 应为字符串，解析失败: %w", err)
	}
	parsed, err := ParsePermissionLevel(s)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
