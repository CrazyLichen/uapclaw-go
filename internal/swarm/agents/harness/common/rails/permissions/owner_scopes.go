package permissions

import (
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OwnerScopesPermissionContext 数字分身场景下的权限上下文。
// 对齐 Python: owner_scopes.PermissionContext
// 不放入 schema/agent.py，不序列化到 AgentRequest；
// 仅从 metadata 构建 → Context 注入 → 匹配。
type OwnerScopesPermissionContext struct {
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// GroupDigitalAvatar 是否为数字分身场景
	GroupDigitalAvatar bool `json:"group_digital_avatar"`
	// PrincipalUserID 权限 owner
	PrincipalUserID string `json:"principal_user_id"`
	// TriggeringUserID 触发者
	TriggeringUserID string `json:"triggering_user_id"`
	// EnableMemory 是否启用记忆
	EnableMemory bool `json:"enable_memory"`
	// AvatarPrincipalName 数字分身主体名称
	AvatarPrincipalName string `json:"avatar_principal_name"`
	// AvatarMode 是否为群聊消息
	AvatarMode bool `json:"avatar_mode"`
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOwnerScopesPermissionContextFromDict 从字典创建权限上下文。
// 对齐 Python: setup_permission_context(request) 中构建 PermissionContext
func NewOwnerScopesPermissionContextFromDict(data map[string]any) *OwnerScopesPermissionContext {
	pc := &OwnerScopesPermissionContext{EnableMemory: true}
	if v, ok := data["channel_id"].(string); ok {
		pc.ChannelID = v
	}
	if v, ok := data["group_digital_avatar"].(bool); ok {
		pc.GroupDigitalAvatar = v
	}
	if v, ok := data["principal_user_id"].(string); ok {
		pc.PrincipalUserID = v
	}
	if v, ok := data["triggering_user_id"].(string); ok {
		pc.TriggeringUserID = v
	}
	if v, ok := data["enable_memory"].(bool); ok {
		pc.EnableMemory = v
	}
	if v, ok := data["avatar_principal_name"].(string); ok {
		pc.AvatarPrincipalName = v
	}
	if v, ok := data["avatar_mode"].(bool); ok {
		pc.AvatarMode = v
	}
	return pc
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Scene 返回权限场景类型。
// 对齐 Python: PermissionContext.scene
func (p *OwnerScopesPermissionContext) Scene() string {
	if p.GroupDigitalAvatar {
		return "group_digital_avatar"
	}
	if strings.TrimSpace(p.ChannelID) == "web" {
		return "web"
	}
	return "normal_im"
}

// OwnerScopeKey 返回 (channel_id, principal_user_id)。
// 对齐 Python: PermissionContext.owner_scope_key
func (p *OwnerScopesPermissionContext) OwnerScopeKey() [2]string {
	return [2]string{p.ChannelID, p.PrincipalUserID}
}
