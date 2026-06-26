package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionContext 权限上下文，统一承载权限判定所需的身份与场景信息。
//
// 作为 AgentRequest 的字段，携带请求方的身份标识、渠道信息和场景标记，
// 供 AgentServer 和权限模块判定是否允许执行操作。
//
// 对应 Python: jiuwenswarm/common/schema/agent.py (PermissionContext)
type PermissionContext struct {
	// PrincipalUserID 权限 owner（channel config 的 my_user_id）
	PrincipalUserID string `json:"principal_user_id"`
	// TriggeringUserID 触发者（IM sender）
	TriggeringUserID string `json:"triggering_user_id"`
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// GroupDigitalAvatar 是否为数字分身场景
	GroupDigitalAvatar bool `json:"group_digital_avatar"`
	// WebUserID 预留：第二期 web 端本人审批
	WebUserID string `json:"web_user_id"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionContext 创建权限上下文实例，所有字段为零值。
func NewPermissionContext(opts ...PermissionContextOption) *PermissionContext {
	pc := &PermissionContext{}
	for _, opt := range opts {
		opt(pc)
	}
	return pc
}

// NewPermissionContextFromDict 从 dict 反序列化创建 PermissionContext。
//
// 对齐 Python: PermissionContext.from_dict()
func NewPermissionContextFromDict(data map[string]any) *PermissionContext {
	pc := &PermissionContext{}
	if v, ok := data["principal_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.PrincipalUserID = s
		}
	}
	if v, ok := data["triggering_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.TriggeringUserID = s
		}
	}
	if v, ok := data["channel_id"]; ok {
		if s, ok := v.(string); ok {
			pc.ChannelID = s
		}
	}
	if v, ok := data["group_digital_avatar"]; ok {
		if b, ok := v.(bool); ok {
			pc.GroupDigitalAvatar = b
		}
	}
	if v, ok := data["web_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.WebUserID = s
		}
	}
	return pc
}

// PermissionContextOption 权限上下文可选配置函数。
type PermissionContextOption func(*PermissionContext)

// WithPermissionPrincipalUserID 设置权限 owner。
func WithPermissionPrincipalUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.PrincipalUserID = id }
}

// WithPermissionTriggeringUserID 设置触发者。
func WithPermissionTriggeringUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.TriggeringUserID = id }
}

// WithPermissionChannelID 设置渠道标识。
func WithPermissionChannelID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.ChannelID = id }
}

// WithPermissionGroupDigitalAvatar 设置数字分身场景。
func WithPermissionGroupDigitalAvatar(v bool) PermissionContextOption {
	return func(pc *PermissionContext) { pc.GroupDigitalAvatar = v }
}

// WithPermissionWebUserID 设置 web 端用户标识。
func WithPermissionWebUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.WebUserID = id }
}

// Scene 根据渠道和数字分身标记派生场景类型。
//
// 派生规则（对齐 Python PermissionContext.scene）：
//   - channel_id == "web" → "web"
//   - group_digital_avatar == true → "group_digital_avatar"
//   - 其他 → "normal_im"
func (p *PermissionContext) Scene() string {
	if p.ChannelID == "web" {
		return "web"
	}
	if p.GroupDigitalAvatar {
		return "group_digital_avatar"
	}
	return "normal_im"
}

// OwnerScopeKey 返回用于 owner_scopes 配置查找的 key。
//
// 返回 [ChannelID, PrincipalUserID]，对齐 Python tuple[str, str]。
func (p *PermissionContext) OwnerScopeKey() [2]string {
	return [2]string{p.ChannelID, p.PrincipalUserID}
}

// ToDict 序列化为 dict（供 E2A WebSocket 传输）。
//
// 对齐 Python: PermissionContext.to_dict()
func (p *PermissionContext) ToDict() map[string]any {
	return map[string]any{
		"principal_user_id":    p.PrincipalUserID,
		"triggering_user_id":   p.TriggeringUserID,
		"channel_id":           p.ChannelID,
		"group_digital_avatar": p.GroupDigitalAvatar,
		"web_user_id":          p.WebUserID,
	}
}

// Validate 校验 PermissionContext 必填字段。
//
// 校验规则（对齐 Python 实际使用）：
//   - principal_user_id 非空
func (p *PermissionContext) Validate() error {
	if p.PrincipalUserID == "" {
		return fmt.Errorf("principal_user_id 不能为空")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 注意：PermissionContext 的 JSON 序列化由 encoding/json + struct tag 自动处理，
// 不需要手写 marshal/unmarshal 方法。ToDict/FromDict 提供给需要 map 形式的场景。
var _ json.Marshaler = nil // 确保 encoding/json 可用（编译期检查）
