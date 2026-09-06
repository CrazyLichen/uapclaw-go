package permissions

import (
	"context"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	utils "github.com/uapclaw/uapclaw-go/internal/common/utils"
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

// logComponent 日志组件
var logComponent = logger.ComponentPermissions

// persistLock 持久化写锁，对齐 Python: _persist_lock = threading.Lock()
var persistLock sync.Mutex

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

// SetupPermissionContext 从 metadata 构造 OwnerScopesPermissionContext 并注入 context.Context。
// 返回新的 context.Context；若无需设置（非 avatar_mode 且 enable_memory 非 false），返回原始 ctx。
//
// 对齐 Python: setup_permission_context(request) -> Token | None
func SetupPermissionContext(ctx context.Context, channelID string, metadata map[string]any) context.Context {
	avatarMode, _ := metadata["avatar_mode"].(bool)
	if !avatarMode {
		enableMem, hasEnableMem := metadata["enable_memory"]
		if hasEnableMem && enableMem == false {
			permCtx := &OwnerScopesPermissionContext{
				ChannelID:    channelID,
				EnableMemory: false,
				AvatarMode:   false,
			}
			return context.WithValue(ctx, permissionContextKey{}, permCtx)
		}
		return ctx
	}

	groupDigitalAvatar, _ := metadata["group_digital_avatar"].(bool)
	principalUserID, _ := metadata["principal_user_id"].(string)
	triggeringUserID, _ := metadata["triggering_user_id"].(string)
	enableMemory := true
	if v, ok := metadata["enable_memory"].(bool); ok {
		enableMemory = v
	}
	avatarPrincipalName, _ := metadata["avatar_principal_name"].(string)

	permCtx := &OwnerScopesPermissionContext{
		ChannelID:           channelID,
		GroupDigitalAvatar:  groupDigitalAvatar,
		PrincipalUserID:     principalUserID,
		TriggeringUserID:    triggeringUserID,
		EnableMemory:        enableMemory,
		AvatarPrincipalName: avatarPrincipalName,
		AvatarMode:          avatarMode,
	}
	return context.WithValue(ctx, permissionContextKey{}, permCtx)
}

// CleanupPermissionContext 在 Go 中无需显式清理（context.Context 随生命周期自动回收）。
// 保留此函数以对齐 Python cleanup_permission_context(token) 调用点。
//
// 对齐 Python: cleanup_permission_context(token)
func CleanupPermissionContext(_ context.Context) {
	// Go 的 context.Context 无需重置，此函数仅为 API 对齐
}

// PermissionContextFromCtx 从 context.Context 中提取权限上下文。
// 返回 nil 表示未设置。
func PermissionContextFromCtx(ctx context.Context) *OwnerScopesPermissionContext {
	if ctx == nil {
		return nil
	}
	v := ctx.Value(permissionContextKey{})
	if v == nil {
		return nil
	}
	pc, ok := v.(*OwnerScopesPermissionContext)
	if !ok {
		return nil
	}
	return pc
}

// CheckAvatarPermission 单工具 owner_scopes 权限检查。返回 "allow" 或 "deny"
// （ASK 自动降级为 DENY）。
//
// 对齐 Python: check_avatar_permission(tool_name, tool_args, channel_id, session_id)
func CheckAvatarPermission(
	ctx context.Context,
	toolName string,
	toolArgs map[string]any,
	channelID string,
	sessionID string,
) string {
	permCtx := PermissionContextFromCtx(ctx)
	if permCtx == nil || permCtx.PrincipalUserID == "" {
		logger.Info(logComponent).Msg("[check_avatar_permission] perm_ctx is None or no principal_user_id")
		return "deny"
	}

	// 加载权限配置
	cfg, err := loadPermissionsConfig()
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[check_avatar_permission] load config failed")
		cfg = make(map[string]any)
	}
	permCfg, _ := cfg["permissions"].(map[string]any)
	if permCfg == nil {
		permCfg = make(map[string]any)
	}

	// 创建 PermissionEngine
	engine := security.NewPermissionEngine(permCfg, nil, "", workspace.WorkspaceDir())

	ownerScopes := permCfg["owner_scopes"]
	logger.Info(logComponent).
		Str("tool", toolName).
		Str("channel", permCtx.ChannelID).
		Str("user", permCtx.PrincipalUserID).
		Str("owner_scopes_type", typeOf(ownerScopes)).
		Strs("owner_scopes_keys", dictKeys(ownerScopes)).
		Msg("[check_avatar_permission]")

	ownerScopesMap, ok := ownerScopes.(map[string]any)
	if !ok || len(ownerScopesMap) == 0 {
		logger.Info(logComponent).Msg("[check_avatar_permission] owner_scopes is empty or not dict")
		return "allow"
	}

	cid := strings.TrimSpace(permCtx.ChannelID)
	uid := strings.TrimSpace(permCtx.PrincipalUserID)

	// owner_scopes.<channel>.<user>
	chMap, _ := ownerScopesMap[cid].(map[string]any)
	scopeCfg, _ := chMap[uid].(map[string]any)
	logger.Info(logComponent).
		Str("cid", cid).
		Str("uid", uid).
		Bool("scope_cfg", scopeCfg != nil).
		Msg("[check_avatar_permission] lookup")

	level := resolveOwnerScopeLevel(scopeCfg, toolName, toolArgs)
	logger.Info(logComponent).
		Str("level", level).
		Msg("[check_avatar_permission] resolved level")

	// 全局策略评估
	var globalLevelValue string
	globalLevel, _ := engine.EvaluateGlobalPolicyDirectly(toolName, toolArgs, true)
	if globalLevel != security.PermissionLevelNone {
		globalLevelValue = globalLevel.String()
	}

	if level == "" {
		if globalLevelValue == "allow" {
			return "allow"
		}
		return "deny"
	}

	// 严重度比较（越低越严：deny=0, ask=1, allow=2）
	severity := map[string]int{"allow": 0, "ask": 1, "deny": 2}
	finalLevel := level
	globalSev, globalOk := severity[globalLevelValue]
	levelSev, levelOk := severity[level]
	if globalOk && levelOk && globalSev > levelSev {
		finalLevel = globalLevelValue
	}

	if finalLevel == "allow" {
		return "allow"
	}
	return "deny"
}

// PersistToOwnerScope 将规则持久化到 config.yaml 的 owner_scopes 节点。
//
// 对齐 Python: persist_to_owner_scope(tool_name, pattern, channel_id, user_id, config)
func PersistToOwnerScope(toolName, pattern, channelID, userID string, cfg map[string]any) {
	persistLock.Lock()
	defer persistLock.Unlock()

	// 深拷贝一份以避免外部修改
	raw := utils.DeepCopyMap(cfg)

	permCfg, _ := raw["permissions"].(map[string]any)
	if permCfg == nil {
		permCfg = make(map[string]any)
		raw["permissions"] = permCfg
	}

	scopes, _ := permCfg["owner_scopes"].(map[string]any)
	if scopes == nil {
		scopes = make(map[string]any)
		permCfg["owner_scopes"] = scopes
	}

	ch, _ := scopes[channelID].(map[string]any)
	if ch == nil {
		ch = make(map[string]any)
		scopes[channelID] = ch
	}

	user, _ := ch[userID].(map[string]any)
	if user == nil {
		user = make(map[string]any)
		ch[userID] = user
	}

	tools, _ := user["tools"].(map[string]any)
	if tools == nil {
		tools = make(map[string]any)
		user["tools"] = tools
	}

	existing := tools[toolName]
	if existingMap, ok := existing.(map[string]any); ok {
		existingMap["*"] = pattern
	} else {
		tools[toolName] = pattern
	}

	// 写回配置文件
	if err := savePermissionsConfig(raw); err != nil {
		logger.Warn(logComponent).Err(err).Msg("persist_to_owner_scope failed")
	}
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

// resolveOwnerScopeLevel 在 owner-scope 层按优先级匹配，返回 "allow"/"deny"/"ask" 或 ""。
//
// 优先级（匹配到即返回，不再 fallback）：
// 1. owner_scopes.<channel>.<user>.tools.<tool>.patterns
// 2. owner_scopes.<channel>.<user>.tools.<tool>.* (或直接字符串)
// 3. owner_scopes.<channel>.<user>.defaults.*
//
// 对齐 Python: _resolve_owner_scope_level(scope_cfg, tool_name, tool_args)
func resolveOwnerScopeLevel(scopeCfg map[string]any, toolName string, toolArgs map[string]any) string {
	if scopeCfg == nil || len(scopeCfg) == 0 {
		return ""
	}

	toolsCfg, _ := scopeCfg["tools"].(map[string]any)
	if toolsCfg != nil {
		if toolEntry, ok := toolsCfg[toolName]; ok {
			// 直接字符串：owner_scopes.<channel>.<user>.tools.<tool> = "allow"
			if entryStr, ok := toolEntry.(string); ok {
				return entryStr
			}
			// 字典形式：patterns + 通配
			if entryMap, ok := toolEntry.(map[string]any); ok {
				patterns, _ := entryMap["patterns"].(map[string]any)
				if patterns != nil {
					for pattern, perm := range patterns {
						if permStr, ok := perm.(string); ok && matchArgs(pattern, toolArgs) {
							return permStr
						}
					}
				}
				if wildcard, ok := entryMap["*"].(string); ok {
					return wildcard
				}
			}
		}
	}

	defaultsCfg, _ := scopeCfg["defaults"].(map[string]any)
	if defaultsCfg != nil {
		if wildcard, ok := defaultsCfg["*"].(string); ok {
			return wildcard
		}
	}

	return ""
}

// matchArgs 简化的参数模式匹配（复用 harness patterns）。
//
// 对齐 Python: _match_args(pattern, tool_args)
func matchArgs(pattern string, toolArgs map[string]any) bool {
	defer func() {
		// 对齐 Python: except Exception: return False
		recover()
	}()

	cmdMatcher := &security.CommandMatcher{}
	urlMatcher := &security.URLMatcher{}
	pathMatcher := &security.PathMatcher{}

	for key, value := range toolArgs {
		val, ok := value.(string)
		if !ok {
			continue
		}
		// command / cmd → MatchCommand
		if key == "command" || key == "cmd" {
			if cmdMatcher.MatchCommand(pattern, val) {
				return true
			}
		}
		// url → MatchURL
		if key == "url" {
			if urlMatcher.MatchURL(pattern, val) {
				return true
			}
		}
		// path / file_path → MatchPath
		if key == "path" || key == "file_path" {
			if pathMatcher.MatchPath(pattern, val) {
				return true
			}
		}
		// fallback: 通配匹配
		if security.MatchWildcard(val, pattern) {
			return true
		}
	}
	return false
}

// loadPermissionsConfig 从配置文件加载完整配置。
// 对齐 Python: get_config()
func loadPermissionsConfig() (map[string]any, error) {
	cfg, err := config.New("")
	if err != nil {
		return make(map[string]any), err
	}
	data, err := cfg.Load()
	if err != nil {
		return make(map[string]any), err
	}
	if data == nil {
		return make(map[string]any), nil
	}
	return data, nil
}

// savePermissionsConfig 将完整配置写回文件。
// 对齐 Python: set_config(raw)
func savePermissionsConfig(data map[string]any) error {
	cfg, err := config.New("")
	if err != nil {
		return err
	}
	return cfg.Save(data)
}

// typeOf 返回值的类型名称（对齐 Python type(x).__name__）
func typeOf(v any) string {
	if v == nil {
		return "NoneType"
	}
	switch v.(type) {
	case map[string]any:
		return "dict"
	case []any:
		return "list"
	case string:
		return "str"
	case bool:
		return "bool"
	case int, int64, float64:
		return "number"
	default:
		return "unknown"
	}
}

// dictKeys 返回 dict 的键列表（对齐 Python list(d.keys())）
func dictKeys(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
