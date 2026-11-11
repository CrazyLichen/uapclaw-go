package runtime

import (
	"errors"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionSnapshotInput 选择宿主快照或一个语法安全的会话覆盖层。
// Python: permission_catalog.py:30-35 PermissionSnapshotInput
type PermissionSnapshotInput struct {
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识（可空：空即宿主快照）
	SessionID *string
}

// PermissionToolSnapshot 一个具名工具及其生效权限等级。
// Python: permission_catalog.py:38-46 PermissionToolSnapshot
type PermissionToolSnapshot struct {
	// Name 工具名
	Name string
	// Level 权限等级（allow/ask/deny）
	Level string
}

// PermissionRuleSnapshot 一条权限规则的可安全展示字段。
// Python: permission_catalog.py:49-70 PermissionRuleSnapshot
type PermissionRuleSnapshot struct {
	// RuleID 规则标识
	RuleID string
	// Tools 适用工具名集合
	Tools []string
	// Pattern 匹配模式
	Pattern string
	// Action 动作等级（可空）
	Action *string
	// Severity 严重级（大写）
	Severity string
	// Description 说明
	Description string
	// MatchType 匹配类型
	MatchType string
}

// PermissionLayerSnapshot 单层权限的不可变、可序列化视图。
// Python: permission_catalog.py:73-86 PermissionLayerSnapshot
type PermissionLayerSnapshot struct {
	// Enabled 是否启用（nil 表示未指定）
	Enabled *bool
	// Tools 具名工具等级（按名称排序）
	Tools []PermissionToolSnapshot
	// Rules 规则集合
	Rules []PermissionRuleSnapshot
}

// PermissionSnapshotResult 全局、用户、会话与生效四层权限视图。
// Python: permission_catalog.py:89-108 PermissionSnapshotResult
type PermissionSnapshotResult struct {
	// Scope 快照范围（host 或 session）
	Scope string
	// SessionID 会话标识（宿主快照为空）
	SessionID string
	// GlobalLayer 全局层
	GlobalLayer PermissionLayerSnapshot
	// UserLayer 用户层
	UserLayer PermissionLayerSnapshot
	// SessionLayer 会话层
	SessionLayer PermissionLayerSnapshot
	// Effective 生效层
	Effective PermissionLayerSnapshot
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PermissionLevelAllow 允许
	// Python: permission_catalog.py:11-12 _PERMISSION_LEVELS
	PermissionLevelAllow = "allow"
	// PermissionLevelAsk 询问
	PermissionLevelAsk = "ask"
	// PermissionLevelDeny 拒绝
	PermissionLevelDeny = "deny"

	// PermissionScopeHost 宿主快照范围
	// Python: permission_catalog.py:277 "host"
	PermissionScopeHost = "host"
	// PermissionScopeSession 会话快照范围
	// Python: permission_catalog.py:277 "session"
	PermissionScopeSession = "session"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrPermissionSnapshotInputRequired 快照输入类型不对
	// Python: permission_catalog.py:257-260 code=BAD_REQUEST
	ErrPermissionSnapshotInputRequired = errors.New("permission snapshot input is required")
	// ErrPermissionSessionIDInvalid 会话标识不合法
	// Python: permission_catalog.py:222 code=BAD_REQUEST
	ErrPermissionSessionIDInvalid = errors.New("invalid session id")
	// ErrPermissionSessionNotFound 会话不存在或非本渠道
	// Python: permission_catalog.py:246-249 code=NOT_FOUND
	ErrPermissionSessionNotFound = errors.New("session not found")
	// ErrPermissionReadFailed 读取权限失败
	// Python: permission_catalog.py:270-274 code=READ_FAILED
	ErrPermissionReadFailed = errors.New("failed to read permissions")
)

// permissionLevels 合法权限等级集合。
// Python: permission_catalog.py:12 _PERMISSION_LEVELS
var permissionLevels = map[string]struct{}{
	PermissionLevelAllow: {}, PermissionLevelAsk: {}, PermissionLevelDeny: {},
}

// severityActions 严重级到权限动作的映射。
// Python: permission_catalog.py:13-18 _SEVERITY_ACTIONS
var severityActions = map[string]string{
	"LOW":      PermissionLevelAllow,
	"MEDIUM":   PermissionLevelAllow,
	"HIGH":     PermissionLevelAsk,
	"CRITICAL": PermissionLevelDeny,
}

// legacyToolLevelKeys 遗留工具等级键 → 等级（按上游声明顺序）。
// Python: permission_catalog.py:132-136 legacy_levels
var legacyToolLevelKeys = [][2]string{
	{"deny_tools", PermissionLevelDeny},
	{"ask_tools", PermissionLevelAsk},
	{"allow_tools", PermissionLevelAllow},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadPermissionSnapshot 捕获并投影一次内部一致的权限快照。
// Python: permission_catalog.py:252-283 read_permission_snapshot
func ReadPermissionSnapshot(request PermissionSnapshotInput) (PermissionSnapshotResult, error) {
	// Python:261-262 归一化会话标识
	sessionID, err := normalizePermissionSessionID(request.SessionID)
	if err != nil {
		return PermissionSnapshotResult{}, err
	}
	// Python:262-263 有会话则校验归属
	if sessionID != "" {
		if err := requireOwnedSession(request.ChannelID, sessionID); err != nil {
			return PermissionSnapshotResult{}, err
		}
	}

	// Python:264-274 捕获四层
	layers, err := capturePermissionLayers(sessionID)
	if err != nil {
		return PermissionSnapshotResult{}, err
	}

	// Python:276-283 逐层投影
	scope := PermissionScopeHost
	if sessionID != "" {
		scope = PermissionScopeSession
	}
	return PermissionSnapshotResult{
		Scope:        scope,
		SessionID:    sessionID,
		GlobalLayer:  permissionLayerSnapshot(layers.Global),
		UserLayer:    permissionLayerSnapshot(layers.User),
		SessionLayer: permissionLayerSnapshot(layers.Session),
		Effective:    permissionLayerSnapshot(layers.Effective),
	}, nil
}

// PermissionToolSnapshotToDict 工具快照序列化。
// Python: permission_catalog.py:45-46 to_dict
func PermissionToolSnapshotToDict(snapshot PermissionToolSnapshot) map[string]any {
	return map[string]any{"name": snapshot.Name, "level": snapshot.Level}
}

// PermissionRuleSnapshotToDict 规则快照序列化（id 键名保持上游 "id"）。
// Python: permission_catalog.py:61-70 to_dict
func PermissionRuleSnapshotToDict(snapshot PermissionRuleSnapshot) map[string]any {
	var action any
	if snapshot.Action != nil {
		action = *snapshot.Action
	}
	return map[string]any{
		"id":          snapshot.RuleID,
		"tools":       append([]string{}, snapshot.Tools...),
		"pattern":     snapshot.Pattern,
		"action":      action,
		"severity":    snapshot.Severity,
		"description": snapshot.Description,
		"match_type":  snapshot.MatchType,
	}
}

// PermissionLayerSnapshotToDict 层快照序列化。
// Python: permission_catalog.py:81-86 to_dict
func PermissionLayerSnapshotToDict(snapshot PermissionLayerSnapshot) map[string]any {
	tools := make([]map[string]any, 0, len(snapshot.Tools))
	for _, item := range snapshot.Tools {
		tools = append(tools, PermissionToolSnapshotToDict(item))
	}
	rules := make([]map[string]any, 0, len(snapshot.Rules))
	for _, item := range snapshot.Rules {
		rules = append(rules, PermissionRuleSnapshotToDict(item))
	}
	var enabled any
	if snapshot.Enabled != nil {
		enabled = *snapshot.Enabled
	}
	return map[string]any{
		"enabled": enabled,
		"tools":   tools,
		"rules":   rules,
	}
}

// PermissionSnapshotResultToDict 四层快照序列化。
// Python: permission_catalog.py:100-108 to_dict
func PermissionSnapshotResultToDict(result PermissionSnapshotResult) map[string]any {
	return map[string]any{
		"scope":      result.Scope,
		"session_id": result.SessionID,
		"global":     PermissionLayerSnapshotToDict(result.GlobalLayer),
		"user":       PermissionLayerSnapshotToDict(result.UserLayer),
		"session":    PermissionLayerSnapshotToDict(result.SessionLayer),
		"effective":  PermissionLayerSnapshotToDict(result.Effective),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// permissionLevel 归一化权限等级；映射取 "*" 键；非法返回 nil。
// Python: permission_catalog.py:111-119 _permission_level
func permissionLevel(raw any) *string {
	// Python:112-113 映射时取 "*" 键
	if mapping, ok := raw.(map[string]any); ok {
		raw = mapping["*"]
	}
	text, ok := raw.(string)
	if !ok {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(text))
	if _, valid := permissionLevels[normalized]; !valid {
		return nil
	}
	return &normalized
}

// namedToolLevels 从一层配置提取具名工具等级。
// Python: permission_catalog.py:122-145 _named_tool_levels
func namedToolLevels(layer map[string]any) map[string]string {
	levels := make(map[string]string)

	// Python:124-130 tools 映射（后者覆盖前者）
	if tools, ok := layer["tools"].(map[string]any); ok {
		for rawName, rawLevel := range tools {
			name := strings.TrimSpace(rawName)
			level := permissionLevel(rawLevel)
			if name != "" && level != nil {
				levels[name] = *level
			}
		}
	}

	// Python:132-144 遗留三键（deny_tools → ask_tools → allow_tools 顺序覆盖）
	for _, pair := range legacyToolLevelKeys {
		names, ok := layer[pair[0]].([]any)
		if !ok {
			// 兼容 []string
			direct, okString := layer[pair[0]].([]string)
			if !okString {
				continue
			}
			for _, rawName := range direct {
				name := strings.TrimSpace(rawName)
				if name != "" {
					levels[name] = pair[1]
				}
			}
			continue
		}
		for _, rawName := range names {
			text, ok := rawName.(string)
			if !ok {
				continue
			}
			name := strings.TrimSpace(text)
			if name != "" {
				levels[name] = pair[1]
			}
		}
	}
	return levels
}

// ruleTools 规则工具名归一（去空白、保序去重）。
// Python: permission_catalog.py:148-155 _rule_tools
func ruleTools(raw any) []string {
	values := make([]any, 0, 1)
	if list, ok := raw.([]any); ok {
		values = append(values, list...)
	} else if list, ok := raw.([]string); ok {
		for _, item := range list {
			values = append(values, item)
		}
	} else {
		values = append(values, raw)
	}

	tools := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		name := strings.TrimSpace(text)
		if name == "" {
			continue
		}
		if _, duplicated := seen[name]; duplicated {
			continue
		}
		seen[name] = struct{}{}
		tools = append(tools, name)
	}
	return tools
}

// ruleAction 规则动作：优先 action，退化到 severity 映射。
// Python: permission_catalog.py:158-163 _rule_action
func ruleAction(rule map[string]any) *string {
	if action := permissionLevel(rule["action"]); action != nil {
		return action
	}
	severity := ""
	if text, ok := rule["severity"].(string); ok {
		severity = strings.TrimSpace(text)
	}
	severity = strings.ToUpper(severity)
	if mapped, ok := severityActions[severity]; ok {
		level := mapped
		return &level
	}
	return nil
}

// ruleSnapshots 提取规则快照集合。
// Python: permission_catalog.py:170-189 _rule_snapshots
func ruleSnapshots(layer map[string]any) []PermissionRuleSnapshot {
	rules, ok := layer["rules"].([]any)
	if !ok {
		return nil
	}
	snapshots := make([]PermissionRuleSnapshot, 0, len(rules))
	for _, rawRule := range rules {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			continue
		}
		snapshots = append(snapshots, PermissionRuleSnapshot{
			RuleID:      safeTextFromAny(rule["id"]),
			Tools:       ruleTools(rule["tools"]),
			Pattern:     safeTextFromAny(rule["pattern"]),
			Action:      ruleAction(rule),
			Severity:    strings.ToUpper(safeTextFromAny(rule["severity"])),
			Description: safeTextFromAny(rule["description"]),
			MatchType:   safeTextFromAny(rule["match_type"]),
		})
	}
	return snapshots
}

// permissionLayerSnapshot 把一层原始配置投影为快照。
// Python: permission_catalog.py:192-203 _layer_snapshot
func permissionLayerSnapshot(layer map[string]any) PermissionLayerSnapshot {
	source := layer
	if source == nil {
		source = map[string]any{}
	}

	// Python:194-197 enabled 仅接受 bool
	var enabled *bool
	if flag, ok := source["enabled"].(bool); ok {
		enabled = &flag
	}

	// Python:198-201 具名工具按名称排序
	levels := namedToolLevels(source)
	names := make([]string, 0, len(levels))
	for name := range levels {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := make([]PermissionToolSnapshot, 0, len(names))
	for _, name := range names {
		tools = append(tools, PermissionToolSnapshot{Name: name, Level: levels[name]})
	}

	return PermissionLayerSnapshot{
		Enabled: enabled,
		Tools:   tools,
		Rules:   ruleSnapshots(source),
	}
}

// capturePermissionLayers 一次加锁读取并合成四层。
// Python: permission_catalog.py:206-211 _capture_permission_layers
//
// 直接调用 rails/permissions 的实现（上游亦如此）。此前因为
// permissions 反向依赖 runtime 而成环，才退化为"注入点 + 报错默认值"；
// 把 RunContext / run_permissions 下沉到 runtime/ctx 后，两边都单向依赖
// 该子包，本包可正常 import permissions。
//
// 保留变量形态仅为测试可替换（与上游函数内 import 等价）。
var capturePermissionLayers = func(sessionID string) (permissions.PermissionLayers, error) {
	layers, err := permissions.CapturePermissionLayers(sessionID)
	if err != nil {
		// Python:268-274 非目录错误统一为 READ_FAILED
		return permissions.PermissionLayers{}, ErrPermissionReadFailed
	}
	return layers, nil
}

// normalizePermissionSessionID 归一化并校验会话标识。
// Python: permission_catalog.py:214-223 _normalize_session_id
func normalizePermissionSessionID(raw *string) (string, error) {
	if raw == nil {
		return "", nil
	}
	sessionID := strings.TrimSpace(*raw)
	if sessionID == "" {
		return "", nil
	}
	if !common.IsValidSessionID(sessionID) {
		return "", ErrPermissionSessionIDInvalid
	}
	return sessionID, nil
}

// requireOwnedSession 校验会话归属（缺失与外部渠道共用同一结果）。
// Python: permission_catalog.py:226-249 _read_owned_session + _require_owned_session
func requireOwnedSession(channelID, sessionID string) error {
	session, err := GetSession(SessionGetInput{ChannelID: channelID, SessionID: sessionID})
	if err != nil {
		// Python:244-245 SessionCatalogError → PermissionCatalogError 透传 code
		if errors.Is(err, ErrSessionChannelRequired) {
			return ErrSessionChannelRequired
		}
		if errors.Is(err, ErrSessionIDRequired) || errors.Is(err, ErrSessionIDInvalid) {
			return ErrPermissionSessionIDInvalid
		}
		return ErrPermissionReadFailed
	}
	// Python:246-249 缺失与外渠道故意共用一条结果，防止枚举他人会话
	if session == nil {
		return ErrPermissionSessionNotFound
	}
	return nil
}
