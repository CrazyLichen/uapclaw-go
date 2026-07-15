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
