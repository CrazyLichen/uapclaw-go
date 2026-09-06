package permissions

import (
	"fmt"
	"strings"
	"sync"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
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

// permissionContextKey context.Context 中存储 OwnerScopesPermissionContext 的键类型。
// 对齐 Python: TOOL_PERMISSION_CONTEXT ContextVar
type permissionContextKey struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// severityMap 权限级别严重度映射，数值越大越严格。
// 对齐 Python: _severity = {"allow": 0, "ask": 1, "deny": 2}
var severityMap = map[string]int{
	"allow": 0,
	"ask":   1,
	"deny":  2,
}

// logComponent 日志组件
var logComponent = logger.ComponentPermissions

// persistLock 持久化写锁，对齐 Python: _persist_lock = threading.Lock()
var persistLock sync.Mutex

// matchers 全局匹配器实例（只读，线程安全）。
// 未导出字段 pm 为零值 PatternMatcher{}，不影响功能（空结构体零值可用）。
var matchers = struct {
	command harnesssecurity.CommandMatcher
	path    harnesssecurity.PathMatcher
	url     harnesssecurity.URLMatcher
	wild    harnesssecurity.PatternMatcher
}{}

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

// MatchArgs 简化的参数模式匹配。
// 遍历 toolArgs 的 key/value，按参数名选择匹配器：
// command/cmd → CommandMatcher, url → URLMatcher, path/file_path → PathMatcher, 其他 → MatchWildcard。
//
// 对齐 Python: _match_args() (owner_scopes.py L209-231)
func MatchArgs(pattern string, toolArgs map[string]any) bool {
	for key, value := range toolArgs {
		s, ok := value.(string)
		if !ok {
			continue
		}
		if (key == "command" || key == "cmd") && matchers.command.MatchCommand(pattern, s) {
			return true
		}
		if key == "url" && matchers.url.MatchURL(pattern, s) {
			return true
		}
		if (key == "path" || key == "file_path") && matchers.path.MatchPath(pattern, s) {
			return true
		}
		if harnesssecurity.MatchWildcard(s, pattern) {
			return true
		}
	}
	return false
}

// ResolveOwnerScopeLevel 在 owner-scope 层按三级优先级匹配。
// 返回 "allow"/"deny"/"ask" 或空字符串（无匹配）。
//
// 优先级：1. scopeCfg.tools.<tool>.patterns → 2. scopeCfg.tools.<tool>.* → 3. scopeCfg.defaults.*
//
// 对齐 Python: _resolve_owner_scope_level() (owner_scopes.py L172-206)
func ResolveOwnerScopeLevel(scopeCfg map[string]any, toolName string, toolArgs map[string]any) string {
	if scopeCfg == nil {
		return ""
	}

	toolsCfg, _ := scopeCfg["tools"].(map[string]any)
	if toolEntry, ok := toolsCfg[toolName]; ok {
		// 级别 1 & 2：检查 tool 配置
		switch v := toolEntry.(type) {
		case string:
			// 直接字符串级别（如 "allow"/"deny"）
			return v
		case map[string]any:
			// 优先级 1：patterns 匹配
			if patterns, ok := v["patterns"].(map[string]any); ok {
				for pattern, perm := range patterns {
					if MatchArgs(fmt.Sprintf("%v", pattern), toolArgs) {
						if s, ok := perm.(string); ok {
							return s
						}
					}
				}
			}
			// 优先级 2：wildcard (*)
			if star, ok := v["*"]; ok {
				if s, ok := star.(string); ok {
					return s
				}
			}
		}
	}

	// 优先级 3：defaults.*
	if defaults, ok := scopeCfg["defaults"].(map[string]any); ok {
		if star, ok := defaults["*"]; ok {
			if s, ok := star.(string); ok {
				return s
			}
		}
	}

	return ""
}

// CheckAvatarPermission 数字分身场景单工具 owner_scopes 权限检查。
// 返回 "allow" 或 "deny"（ASK 自动降级为 DENY，数字分身不支持交互审批）。
//
// permCfg 为当前 permissions 配置快照（由调用方从 config 读取并传入），
// 因为 Go 端没有 Python 的全局 get_config()，采用参数注入更符合 Go 风格。
//
// 对齐 Python: check_avatar_permission() (owner_scopes.py L96-169)
func CheckAvatarPermission(permCfg map[string]any, toolName string, toolArgs map[string]any, channelID string, principalUserID string) (string, error) {
	// 对齐 Python: if perm_ctx is None or not perm_ctx.principal_user_id: return "deny"
	if strings.TrimSpace(principalUserID) == "" {
		logger.Info(logComponent).
			Str("tool_name", toolName).
			Msg("[check_avatar_permission] principalUserID 为空，返回 deny")
		return "deny", nil
	}

	if permCfg == nil {
		permCfg = map[string]any{}
	}

	// 对齐 Python: owner_scopes = perm_cfg.get("owner_scopes")
	ownerScopes, _ := permCfg["owner_scopes"].(map[string]any)
	cid := strings.TrimSpace(channelID)
	uid := strings.TrimSpace(principalUserID)

	logger.Info(logComponent).
		Str("tool_name", toolName).
		Str("channel_id", cid).
		Str("principal_user_id", uid).
		Str("owner_scopes_type", fmt.Sprintf("%T", ownerScopes)).
		Msg("[check_avatar_permission] 开始检查")

	// 对齐 Python: if not isinstance(owner_scopes, dict) or not owner_scopes: return "allow"
	if len(ownerScopes) == 0 {
		logger.Info(logComponent).
			Str("tool_name", toolName).
			Msg("[check_avatar_permission] owner_scopes 为空，返回 allow")
		return "allow", nil
	}

	// 对齐 Python: scope_cfg = (owner_scopes.get(cid) or {}).get(uid)
	chScopes, _ := ownerScopes[cid].(map[string]any)
	scopeCfg, _ := chScopes[uid].(map[string]any)

	logger.Info(logComponent).
		Str("channel_id", cid).
		Str("principal_user_id", uid).
		Bool("scope_cfg_found", scopeCfg != nil).
		Msg("[check_avatar_permission] lookup 完成")

	// 对齐 Python: level = _resolve_owner_scope_level(scope_cfg, tool_name, tool_args)
	level := ResolveOwnerScopeLevel(scopeCfg, toolName, toolArgs)

	logger.Info(logComponent).
		Str("tool_name", toolName).
		Str("resolved_level", level).
		Msg("[check_avatar_permission] owner_scope 级别已解析")

	// 对齐 Python: engine.evaluate_global_policy_directly(tool_name, tool_args, include_external_directory=True)
	workspaceRoot := workspace.WorkspaceDir()
	engine := harnesssecurity.NewPermissionEngine(permCfg, nil, "", workspaceRoot)

	globalLevel, _ := engine.EvaluateGlobalPolicyDirectly(toolName, toolArgs, true)
	globalLevelStr := globalLevel.String()

	// 对齐 Python: severity 严格度比较
	finalLevel := level
	if level == "" {
		// 对齐 Python: if level is None: 如果 global 是 allow 则 allow，否则 deny
		if globalLevel == harnesssecurity.PermissionLevelAllow {
			return "allow", nil
		}
		return "deny", nil
	}

	// 对齐 Python: 取 owner_level 和 global_level 中更严者
	globalSeverity, globalOk := severityMap[globalLevelStr]

	if globalOk && globalSeverity > severityMap[level] {
		finalLevel = globalLevelStr
	}

	// 对齐 Python: 数字分身场景 ASK 自动降级为 DENY
	if finalLevel == "ask" {
		finalLevel = "deny"
	}

	if finalLevel == "allow" {
		return "allow", nil
	}
	return "deny", nil
}

// PersistToOwnerScope 将规则持久化到 config.yaml 的 owner_scopes 节点。
// permCfg 为当前完整 permissions 配置，写盘后返回更新后的配置。
//
// 对齐 Python: persist_to_owner_scope() (owner_scopes.py L234-259)
func PersistToOwnerScope(toolName string, pattern string, channelID string, userID string, permCfg map[string]any, configPath string) bool {
	persistLock.Lock()
	defer persistLock.Unlock()

	if permCfg == nil {
		permCfg = map[string]any{}
	}

	// 对齐 Python: scopes = perm_cfg.setdefault("owner_scopes", {})
	scopes, _ := permCfg["owner_scopes"].(map[string]any)
	if scopes == nil {
		scopes = map[string]any{}
		permCfg["owner_scopes"] = scopes
	}

	// 对齐 Python: ch = scopes.setdefault(channel_id, {})
	ch, _ := scopes[channelID].(map[string]any)
	if ch == nil {
		ch = map[string]any{}
		scopes[channelID] = ch
	}

	// 对齐 Python: user = ch.setdefault(user_id, {})
	usr, _ := ch[userID].(map[string]any)
	if usr == nil {
		usr = map[string]any{}
		ch[userID] = usr
	}

	// 对齐 Python: tools = user.setdefault("tools", {})
	tools, _ := usr["tools"].(map[string]any)
	if tools == nil {
		tools = map[string]any{}
		usr["tools"] = tools
	}

	// 对齐 Python: existing = tools.get(tool_name); if isinstance(existing, dict): existing["*"] = pattern; else: tools[tool_name] = pattern
	if existing, ok := tools[toolName].(map[string]any); ok {
		existing["*"] = pattern
	} else {
		tools[toolName] = pattern
	}

	// 写盘
	if configPath == "" {
		configPath = workspace.ConfigFile()
	}
	ok := harnesssecurity.WritePermissionsSectionToAgentConfigYAML(configPath, permCfg)
	if !ok {
		logger.Warn(logComponent).
			Str("config_path", configPath).
			Msg("persistToOwnerScope 写盘失败")
		return false
	}

	logger.Info(logComponent).
		Str("tool_name", toolName).
		Str("channel_id", channelID).
		Str("user_id", userID).
		Msg("persistToOwnerScope 写盘成功")
	return true
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
// 对齐 Python owner_scopes.PermissionContext.owner_scope_key：对两个字段 TrimSpace
func (p *OwnerScopesPermissionContext) OwnerScopeKey() [2]string {
	return [2]string{strings.TrimSpace(p.ChannelID), strings.TrimSpace(p.PrincipalUserID)}
}
