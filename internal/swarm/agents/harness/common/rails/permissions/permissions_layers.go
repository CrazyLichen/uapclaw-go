package permissions

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// toolLevelAllow allow 级别
	toolLevelAllow = "allow"
	// toolLevelAsk ask 级别
	toolLevelAsk = "ask"
	// toolLevelDeny deny 级别
	toolLevelDeny = "deny"

	// defaultLockTimeout 默认文件锁超时
	defaultLockTimeout = 10 * time.Second

	// userPermissionsFileName 用户权限覆盖文件名
	userPermissionsFileName = "user_permissions.yaml"
	// sessionPermissionsFileName 会话权限覆盖文件名
	sessionPermissionsFileName = "session_permissions.yaml"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// toolLevels 合法工具级别集合
	toolLevels = map[string]struct{}{toolLevelAllow: {}, toolLevelAsk: {}, toolLevelDeny: {}}

	// toolLevelListKeys 工具名列表键，顺序即清理顺序（deny → ask → allow）
	toolLevelListKeys = []string{"deny_tools", "ask_tools", "allow_tools"}

	// toolLevelListKeyOf 级别 → 列表键
	toolLevelListKeyOf = map[string]string{
		toolLevelAllow: "allow_tools",
		toolLevelAsk:   "ask_tools",
		toolLevelDeny:  "deny_tools",
	}

	// overlayKeys overlay 段的合法键。用于判定一个 dict 是否已是 overlay
	// 形态（而非 {"permissions": {...}} 的包裹形态）。
	overlayKeys = map[string]struct{}{
		"allow_tools": {}, "ask_tools": {}, "deny_tools": {},
		"approval_overrides": {}, "file_guard": {}, "tools": {}, "rules": {},
		"net_guard": {}, "shell_guard": {},
	}

	// engineOnlyKeys 只属于引擎层、不应写进 overlay 的键
	engineOnlyKeys = []string{"net_guard", "shell_guard", "defaults", "tools", "enabled"}

	// overrideIDSlugRe 生成稳定 id 时的非法字符集合
	overrideIDSlugRe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

	// storageLockMu 进程内锁（先于文件锁获取），避免同进程多 goroutine 争用
	storageLockMu sync.Mutex
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionLayers 三层权限快照（Global / User / Session / 合成后的 effective）。
type PermissionLayers struct {
	// Global 全局层
	Global map[string]any
	// User 用户层
	User map[string]any
	// Session 会话层
	Session map[string]any
	// Effective 合成后的生效配置
	Effective map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PermissionStorageLock 按 Global → User → Session 顺序加协作锁，返回释放函数。
//
// 顺序固定避免死锁；不可重入（同一 goroutine 重复调用会自锁）。
// 使用 gofrs/flock 做跨进程互斥，另加进程内 mutex 降低同进程争用。
//
// Python: permission_storage_lock(session_id=None, *, lock_timeout=10.0)
func PermissionStorageLock(sessionID string) (func(), error) {
	return permissionStorageLockWithTimeout(sessionID, defaultLockTimeout)
}

// UserPermissionsPath 返回用户权限覆盖文件路径。
//
// Python: user_permissions_path()
func UserPermissionsPath() string {
	return filepath.Join(configDir(), userPermissionsFileName)
}

// SessionPermissionsPath 返回会话权限覆盖文件路径；sessionID 为空时返回错误。
//
// Python: session_permissions_path(session_id)
func SessionPermissionsPath(sessionID string) (string, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return "", fmt.Errorf("session_id is required")
	}
	return filepath.Join(agentSessionsDir(), sid, sessionPermissionsFileName), nil
}

// LoadGlobalPermissions 返回全局配置里的 permissions 段。
//
// Python: load_global_permissions()
func LoadGlobalPermissions() map[string]any {
	cfg := readGlobalConfig()
	inner, ok := cfg["permissions"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(inner))
	for k, v := range inner {
		out[k] = v
	}
	return out
}

// LoadUserPermissions 返回用户层权限覆盖。
//
// Python: load_user_permissions()
func LoadUserPermissions() map[string]any {
	return asPermissionsSection(loadYAMLStrict(UserPermissionsPath(), false))
}

// LoadSessionPermissions 返回会话层权限覆盖；sessionID 为空返回空。
//
// Python: load_session_permissions(session_id)
func LoadSessionPermissions(sessionID string) map[string]any {
	if strings.TrimSpace(sessionID) == "" {
		return map[string]any{}
	}
	path, err := SessionPermissionsPath(sessionID)
	if err != nil {
		return map[string]any{}
	}
	return asPermissionsSection(loadYAMLStrict(path, false))
}

// ReadPermissionLayersLocked 在持有 PermissionStorageLock 时严格读取三层。
//
// globalPermissions 非 nil 时用它（避免重复解析 Global YAML）。
//
// Python: read_permission_layers_locked(session_id=None, *, global_permissions=None)
func ReadPermissionLayersLocked(sessionID string, globalPermissions map[string]any) (map[string]any, map[string]any, map[string]any, error) {
	global := globalPermissions
	if global == nil {
		raw, err := loadYAMLStrictStrict(globalConfigPath())
		if err != nil {
			return nil, nil, nil, err
		}
		inner, ok := raw["permissions"].(map[string]any)
		if !ok {
			return nil, nil, nil, fmt.Errorf("Global permissions must contain a mapping")
		}
		global = inner
	}
	user, err := loadYAMLStrictStrict(UserPermissionsPath())
	if err != nil {
		return nil, nil, nil, err
	}
	session := map[string]any{}
	if strings.TrimSpace(sessionID) != "" {
		path, pathErr := SessionPermissionsPath(sessionID)
		if pathErr != nil {
			return nil, nil, nil, pathErr
		}
		session, err = loadYAMLStrictStrict(path)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return deepCopyAnyMap(global), asPermissionsSection(user), asPermissionsSection(session), nil
}

// CapturePermissionLayers 一次加锁读取并合成四层结果。
//
// Python: capture_permission_layers(session_id=None)
func CapturePermissionLayers(sessionID string) (PermissionLayers, error) {
	release, err := PermissionStorageLock(sessionID)
	if err != nil {
		return PermissionLayers{}, err
	}
	defer release()

	global, user, session, err := ReadPermissionLayersLocked(sessionID, nil)
	if err != nil {
		return PermissionLayers{}, err
	}
	effective := ComposeHostEffectivePermissions(global, user, session, "", false)
	return PermissionLayers{Global: global, User: user, Session: session, Effective: effective}, nil
}

// OverlayFromEffective 从生效配置抽取 User/Session 覆盖（生效减去更低层）。
//
// 参数语义（对齐 Python overlay_from_effective）：
//   - session=true：user 层参与"更低层"，session 层不参与
//   - session=false：session 层参与"更低层"，user 层不参与
//
// Python: overlay_from_effective(effective, *, session, global_permissions=None,
// user_permissions=None, session_permissions=None)
func OverlayFromEffective(
	effective map[string]any,
	session bool,
	globalPermissions map[string]any,
	userPermissions map[string]any,
	sessionPermissions map[string]any,
) map[string]any {
	src := effective
	if src == nil {
		src = map[string]any{}
	}
	globalLayer := globalPermissions
	if globalLayer == nil {
		globalLayer = LoadGlobalPermissions()
	}
	globalLayer = asPermissionsSection(globalLayer)

	var userLayer, skipLayer map[string]any
	if session {
		userLayer = userPermissions
		if userLayer == nil {
			userLayer = LoadUserPermissions()
		}
		userLayer = asPermissionsSection(userLayer)
		skipLayer = map[string]any{}
	} else {
		userLayer = map[string]any{}
		skipLayer = asPermissionsSection(sessionPermissions)
	}

	baseAllow := unionSets(
		toolNamesAtLevel(globalLayer, toolLevelAllow),
		toolNamesAtLevel(userLayer, toolLevelAllow),
		toolNamesAtLevel(skipLayer, toolLevelAllow),
	)
	baseAsk := toolNamesAtLevel(globalLayer, toolLevelAsk)
	baseDeny := toolNamesAtLevel(globalLayer, toolLevelDeny)
	baseOverrideKeys := unionOverrideKeys(
		overrideKeys(globalLayer["approval_overrides"]),
		overrideKeys(userLayer["approval_overrides"]),
		overrideKeys(skipLayer["approval_overrides"]),
	)
	baseOverrideIDs := unionStringSets(
		overrideIDs(globalLayer["approval_overrides"]),
		overrideIDs(userLayer["approval_overrides"]),
		overrideIDs(skipLayer["approval_overrides"]),
	)
	basePathKeys := unionPathKeys(
		pathKeys(globalLayer), pathKeys(userLayer), pathKeys(skipLayer),
	)

	out := map[string]any{}

	// allow_tools：生效层有、更低层没有的
	srcAllow := orderedToolNamesAtLevel(src, toolLevelAllow)
	srcAllowSet := toSetFromSlice(srcAllow)
	allow := make([]any, 0, len(srcAllow))
	for _, name := range srcAllow {
		if _, inBase := baseAllow[name]; inBase {
			continue
		}
		allow = append(allow, name)
	}
	if len(allow) > 0 {
		out["allow_tools"] = allow
	}

	// session 层只记 allow，不记 ask/deny（ask/deny 字段不构成授权）
	if !session {
		ask := make([]any, 0, 4)
		for _, name := range orderedToolNamesAtLevel(src, toolLevelAsk) {
			if _, inBase := baseAsk[name]; inBase {
				continue
			}
			if _, inAllow := srcAllowSet[name]; inAllow {
				continue
			}
			ask = append(ask, name)
		}
		deny := make([]any, 0, 4)
		for _, name := range orderedToolNamesAtLevel(src, toolLevelDeny) {
			if _, inBase := baseDeny[name]; inBase {
				continue
			}
			if _, inAllow := srcAllowSet[name]; inAllow {
				continue
			}
			deny = append(deny, name)
		}
		if len(ask) > 0 {
			out["ask_tools"] = ask
		}
		if len(deny) > 0 {
			out["deny_tools"] = deny
		}
	}

	// approval_overrides：排除更低层已有的签名键
	overrides := make([]map[string]any, 0, 4)
	for _, item := range listOfAnyMaps(src["approval_overrides"]) {
		overrideID, _ := item["id"].(string)
		if strings.TrimSpace(overrideID) == "" {
			continue
		}
		if _, inBase := baseOverrideKeys[overrideKeyString(item)]; inBase {
			continue
		}
		overrides = append(overrides, item)
	}
	if len(overrides) > 0 {
		out["approval_overrides"] = renameAndDedupOverrides(overrides, baseOverrideIDs)
	}

	// file_guard.paths：排除 builtin 层与更低层已有的路径键
	if fg := mapOf(src["file_guard"]); fg != nil {
		paths := make([]any, 0, 4)
		for _, item := range listOfAnyMaps(fg["paths"]) {
			if item["layer"] == "builtin" {
				continue
			}
			if _, inBase := basePathKeys[pathKeyString(item)]; inBase {
				continue
			}
			paths = append(paths, item)
		}
		if len(paths) > 0 {
			out["file_guard"] = map[string]any{"paths": paths}
		}
	}
	return out
}

// PersistUserOverlayFromEffective 从生效配置抽取 User overlay 并落盘。
//
// Python: persist_user_overlay_from_effective(effective, session_id=None)
func PersistUserOverlayFromEffective(effective map[string]any, sessionID string) bool {
	release, err := PermissionStorageLock(sessionID)
	if err != nil {
		return false
	}
	defer release()

	sessionPerms := map[string]any{}
	if strings.TrimSpace(sessionID) != "" {
		sessionPerms = LoadSessionPermissions(sessionID)
	}
	overlay := OverlayFromEffective(effective, false, nil, nil, sessionPerms)
	current := mergeOverlayIntoCurrent(LoadUserPermissions(), overlay, false)
	return dumpYAMLDict(UserPermissionsPath(), current)
}

// PersistSessionOverlayFromEffective 从生效配置抽取 Session overlay 并落盘。
//
// Python: persist_session_overlay_from_effective(session_id, effective)
func PersistSessionOverlayFromEffective(sessionID string, effective map[string]any) bool {
	if strings.TrimSpace(sessionID) == "" {
		return false
	}
	release, err := PermissionStorageLock(sessionID)
	if err != nil {
		return false
	}
	defer release()

	overlay := OverlayFromEffective(effective, true, nil, nil, nil)
	current := mergeOverlayIntoCurrent(LoadSessionPermissions(sessionID), overlay, true)
	path, pathErr := SessionPermissionsPath(sessionID)
	if pathErr != nil {
		return false
	}
	return dumpYAMLDict(path, current)
}

// ListSavedSessionIDs 返回已有 session_permissions.yaml 的会话 id（已排序）。
//
// Python: list_saved_session_ids()
func ListSavedSessionIDs() []string {
	root := agentSessionsDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	found := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), sessionPermissionsFileName)
		if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
			found = append(found, entry.Name())
		}
	}
	sort.Strings(found)
	return found
}

// SaveUserPermissions 覆盖写用户层权限。
//
// Python: save_user_permissions(data)
func SaveUserPermissions(data map[string]any) bool {
	return UpdateUserPermissions(func(map[string]any) map[string]any {
		if data == nil {
			return map[string]any{}
		}
		return deepCopyAnyMap(data)
	})
}

// SaveUserPermissionsLocked 保存用户层覆盖；调用方必须已持 PermissionStorageLock。
//
// Python: save_user_permissions_locked(data)
func SaveUserPermissionsLocked(data map[string]any) bool {
	return dumpYAMLDict(UserPermissionsPath(), data)
}

// SaveSessionPermissionsLocked 保存会话层覆盖；调用方必须已持 PermissionStorageLock。
//
// Python: save_session_permissions_locked(session_id, data)
func SaveSessionPermissionsLocked(sessionID string, data map[string]any) bool {
	path, err := SessionPermissionsPath(sessionID)
	if err != nil {
		return false
	}
	return dumpYAMLDict(path, data)
}

// UpdateUserPermissions 在共享锁内对最新用户层文档应用一次变更。
//
// Python: update_user_permissions(mutator)
func UpdateUserPermissions(mutator func(map[string]any) map[string]any) bool {
	release, err := PermissionStorageLock("")
	if err != nil {
		return false
	}
	defer release()

	current := LoadUserPermissions()
	updated := mutator(current)
	return dumpYAMLDict(UserPermissionsPath(), updated)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// lockSessionFileOnly 只加一个会话文件的锁，**不触碰 storageLockMu**。
//
// 供已持有 PermissionStorageLock（即已持 storageLockMu）的调用方使用。
// Python 侧对应 _session_file_lock：调用方必须已持 permission_storage_lock。
//
// 先获取 storageLockMu 会自锁——storageLockMu 是不可重入的 sync.Mutex。
func lockSessionFileOnly(sessionID string, timeout time.Duration) (func(), error) {
	path, err := SessionPermissionsPath(sessionID)
	if err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}
	lock := flock.New(lockPath)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	ok, lockErr := lock.TryLockContext(ctx, 50*time.Millisecond)
	if lockErr != nil || !ok {
		cancel()
		if lockErr == nil {
			lockErr = fmt.Errorf("permission session lock timeout: %s", lockPath)
		}
		return nil, lockErr
	}
	return func() {
		_ = lock.Unlock()
		cancel()
	}, nil
}

// permissionStorageLockWithTimeout 加锁的实现（含超时）。
func permissionStorageLockWithTimeout(sessionID string, timeout time.Duration) (func(), error) {
	storageLockMu.Lock()

	paths := []string{UserPermissionsPath()}
	if strings.TrimSpace(sessionID) != "" {
		if path, err := SessionPermissionsPath(sessionID); err == nil {
			paths = append(paths, path)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	held := make([]*flock.Flock, 0, len(paths))
	releaseAll := func() {
		for i := len(held) - 1; i >= 0; i-- {
			_ = held[i].Unlock()
		}
		cancel()
		storageLockMu.Unlock()
	}

	for _, path := range paths {
		lockPath := path + ".lock"
		if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
			releaseAll()
			return nil, err
		}
		lock := flock.New(lockPath)
		ok, err := lock.TryLockContext(ctx, 50*time.Millisecond)
		if err != nil || !ok {
			releaseAll()
			if err == nil {
				err = fmt.Errorf("permission storage lock timeout: %s", lockPath)
			}
			return nil, err
		}
		held = append(held, lock)
	}
	return releaseAll, nil
}

// scalarLevel 解析级别标量；dict 取 "*" 键，非法返回空串。
//
// Python: _scalar_level(raw)
func scalarLevel(raw any) string {
	if m, ok := raw.(map[string]any); ok {
		raw = m["*"]
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	level := strings.ToLower(strings.TrimSpace(s))
	if _, valid := toolLevels[level]; !valid {
		return ""
	}
	return level
}

// strNames 从列表里取非空字符串并 trim。
//
// Python: _str_names(raw)
func strNames(raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(list))
	for _, item := range list {
		if s, isStr := item.(string); isStr && strings.TrimSpace(s) != "" {
			names = append(names, strings.TrimSpace(s))
		}
	}
	return names
}

// orderedToolNamesAtLevel 按「tools 字段优先，列表键其次」取某级别的工具名（去重保序）。
//
// 顺序说明：tools 是 map，Go 遍历无序，因此 tools 内部的相对顺序不确定
// （Python 依赖 dict 插入序）。两个来源之间的先后与去重语义一致。
//
// Python: _ordered_tool_names_at_level(layer, level)
func orderedToolNamesAtLevel(layer map[string]any, level string) []string {
	names := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	add := func(name string) {
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	if tools := mapOf(layer["tools"]); tools != nil {
		// 排序保证确定性（Python 靠插入序，Go 需显式排序）
		keys := make([]string, 0, len(tools))
		for name := range tools {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			if strings.TrimSpace(name) == "" {
				continue
			}
			if scalarLevel(tools[name]) == level {
				add(strings.TrimSpace(name))
			}
		}
	}
	for _, name := range strNames(layer[toolLevelListKeyOf[level]]) {
		add(name)
	}
	return names
}

// toolNamesAtLevel 返回某级别的工具名集合。
//
// Python: _tool_names_at_level(layer, level)
func toolNamesAtLevel(layer map[string]any, level string) map[string]struct{} {
	return toSetFromSlice(orderedToolNamesAtLevel(layer, level))
}

// overrideIDs 取 approval_overrides 里的非空 id 集合。
//
// Python: _override_ids(raw)
func overrideIDs(raw any) map[string]struct{} {
	ids := make(map[string]struct{}, 4)
	for _, item := range listOfAnyMaps(raw) {
		if id, _ := item["id"].(string); strings.TrimSpace(id) != "" {
			ids[strings.TrimSpace(id)] = struct{}{}
		}
	}
	return ids
}

// normalizedToolNames 把 override 的 tools 字段归一为字符串列表。
//
// 三种输入形态（对齐 Python）：字符串、列表/元组/集合、其他（空）。
//
// Python: _normalized_tool_names(item)
func normalizedToolNames(item map[string]any) []string {
	raw := item["tools"]
	if raw == nil {
		return nil
	}
	if s, ok := raw.(string); ok {
		if name := strings.TrimSpace(s); name != "" {
			return []string{name}
		}
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(list))
	for _, tool := range list {
		if name := strings.TrimSpace(fmt.Sprintf("%v", tool)); name != "" && name != "<nil>" {
			names = append(names, name)
		}
	}
	return names
}

// overrideKey 返回 override 的冲突键。
//
// 两个分支（对齐 Python _override_key）：
//   - 有 tools/match_type/pattern 任一 → ("signature", ...)，其中 tools 排序
//   - 全空 → ("id", id)，只按 id 比对
//
// Python: _override_key(item)
func overrideKey(item map[string]any) [2]any {
	names := normalizedToolNames(item)
	sort.Strings(names)
	matchType := strings.ToLower(strings.TrimSpace(stringOf(item["match_type"])))
	pattern := stringOf(item["pattern"])
	action := strings.ToLower(strings.TrimSpace(stringOf(item["action"])))
	if len(names) > 0 || matchType != "" || pattern != "" {
		return [2]any{"signature", strings.Join(names, "\x00") + "\x01" + matchType + "\x01" + pattern + "\x01" + action}
	}
	return [2]any{"id", strings.TrimSpace(stringOf(item["id"]))}
}

// overrideKeyString 把 overrideKey 结果转成可做 map 键的字符串。
func overrideKeyString(item map[string]any) string {
	key := overrideKey(item)
	return fmt.Sprintf("%v\x02%v", key[0], key[1])
}

// overrideKeys 批量取 override 冲突键。
//
// Python: _override_keys(raw)
func overrideKeys(raw any) map[string]struct{} {
	keys := make(map[string]struct{}, 4)
	for _, item := range listOfAnyMaps(raw) {
		keys[overrideKeyString(item)] = struct{}{}
	}
	return keys
}

// stableOverrideID 生成稳定的 override id（由内容决定，含 8 位 sha1 后缀）。
//
// Python: _stable_override_id(item)
func stableOverrideID(item map[string]any) string {
	names := normalizedToolNames(item)
	sort.Strings(names)
	toolPart := strings.Join(names, "_")
	if toolPart == "" {
		toolPart = "tool"
	}
	matchType := strings.ToLower(strings.TrimSpace(stringOf(item["match_type"])))
	if matchType == "" {
		matchType = "rule"
	}
	pattern := stringOf(item["pattern"])
	action := strings.ToLower(strings.TrimSpace(stringOf(item["action"])))
	if action == "" {
		action = "allow"
	}
	raw := fmt.Sprintf("user_%s_%s_%s_%s", action, toolPart, matchType, pattern)

	slug := overrideIDSlugRe.ReplaceAllString(raw, "_")
	slug = strings.Trim(slug, "_")
	slug = strings.ToLower(slug)
	if slug == "" {
		slug = "user_override"
	}
	digest := sha1.Sum([]byte(raw))
	suffix := hex.EncodeToString(digest[:])[:8]
	full := slug + "_" + suffix
	if len(full) > 120 {
		full = full[:120]
	}
	return full
}

// uniqueOverrideIDs 对 override id 去重：冲突时追加 _2、_3… 序号。
//
// Python: _unique_override_ids(overrides, reserved_ids=None)
func uniqueOverrideIDs(overrides []map[string]any, reservedIDs map[string]struct{}) []map[string]any {
	seen := make(map[string]struct{}, len(reservedIDs)+len(overrides))
	for id := range reservedIDs {
		seen[id] = struct{}{}
	}
	result := make([]map[string]any, 0, len(overrides))
	for _, item := range overrides {
		overrideID := strings.TrimSpace(stringOf(item["id"]))
		if overrideID == "" {
			result = append(result, item)
			continue
		}
		candidate := overrideID
		suffix := 2
		for {
			if _, dup := seen[candidate]; !dup {
				break
			}
			candidate = fmt.Sprintf("%s_%d", overrideID, suffix)
			suffix++
		}
		seen[candidate] = struct{}{}
		if candidate == overrideID {
			result = append(result, item)
			continue
		}
		renamed := shallowCopyAnyMap(item)
		renamed["id"] = candidate
		result = append(result, renamed)
	}
	return result
}

// renameAndDedupOverrides 先按「与更低层 id 冲突或自身重复」重命名，再统一去重。
//
// 两层处理的顺序与 Python 一致：重命名基于当前批次的原始 id 统计，
// 之后 `uniqueOverrideIDs` 再处理重命名后可能仍存在的冲突。
//
// Python: overlay_from_effective 内的 colliding_ids + _unique_override_ids
func renameAndDedupOverrides(overrides []map[string]any, baseIDs map[string]struct{}) []any {
	outputIDs := make([]string, 0, len(overrides))
	for _, item := range overrides {
		outputIDs = append(outputIDs, strings.TrimSpace(stringOf(item["id"])))
	}
	colliding := make(map[string]struct{}, 4)
	for _, id := range outputIDs {
		if _, inBase := baseIDs[id]; inBase {
			colliding[id] = struct{}{}
			continue
		}
		count := 0
		for _, other := range outputIDs {
			if other == id {
				count++
			}
		}
		if count > 1 {
			colliding[id] = struct{}{}
		}
	}

	renamed := make([]map[string]any, 0, len(overrides))
	for _, item := range overrides {
		overrideID := strings.TrimSpace(stringOf(item["id"]))
		if _, hit := colliding[overrideID]; hit {
			withID := shallowCopyAnyMap(item)
			withID["id"] = stableOverrideID(item)
			renamed = append(renamed, withID)
			continue
		}
		renamed = append(renamed, item)
	}
	deduped := uniqueOverrideIDs(renamed, baseIDs)
	out := make([]any, 0, len(deduped))
	for _, item := range deduped {
		out = append(out, item)
	}
	return out
}

// pathKey 返回路径规则的冲突键（路径归一 + match 归一）。
//
// 路径归一：反斜杠转斜杠、去尾部斜杠、casefold（小写）。
//
// Python: _path_key(item)
func pathKey(item map[string]any) [2]string {
	path := strings.ReplaceAll(stringOf(item["path"]), `\`, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.ToLower(path)
	match := strings.ToLower(strings.TrimSpace(stringOf(item["match"])))
	if match == "" {
		match = "prefix"
	}
	return [2]string{path, match}
}

// pathKeyString 把 pathKey 转成可做 map 键的字符串。
func pathKeyString(item map[string]any) string {
	key := pathKey(item)
	return key[0] + "\x02" + key[1]
}

// pathKeys 取一层的所有文件路径键。
//
// Python: _path_keys(layer)
func pathKeys(layer map[string]any) map[string]struct{} {
	keys := make(map[string]struct{}, 4)
	fg := mapOf(layer["file_guard"])
	if fg == nil {
		return keys
	}
	for _, item := range listOfAnyMaps(fg["paths"]) {
		keys[pathKeyString(item)] = struct{}{}
	}
	return keys
}

// asPermissionsSection 把读取结果归一为 permissions 段。
//
// 若文件是 {"permissions": {...}} 包裹且顶层没有 overlay 键 → 解包；
// 否则原样返回（已是 overlay 形态）。
//
// Python: _as_permissions_section(raw)
func asPermissionsSection(raw any) map[string]any {
	m, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	inner, hasInner := m["permissions"].(map[string]any)
	if hasInner && !hasAnyOverlayKey(m) {
		out := make(map[string]any, len(inner))
		for k, v := range inner {
			out[k] = v
		}
		return out
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// hasAnyOverlayKey 判定 dict 是否含任一 overlay 键。
func hasAnyOverlayKey(m map[string]any) bool {
	for key := range overlayKeys {
		if _, has := m[key]; has {
			return true
		}
	}
	return false
}

// mergeOverlayIntoCurrent 把 overlay 合并进当前文档。
//
// 先剔除引擎层专属键与列表键（这些完全由 overlay 决定），
// file_guard 只保留 paths 之外的子键；session 层还要剔除 deny/ask/enabled。
//
// Python: _merge_overlay_into_current(current, overlay, *, session)
func mergeOverlayIntoCurrent(current, overlay map[string]any, session bool) map[string]any {
	out := make(map[string]any, len(current)+len(overlay))
	for k, v := range current {
		out[k] = v
	}
	for _, key := range engineOnlyKeys {
		delete(out, key)
	}
	for _, key := range toolLevelListKeys {
		delete(out, key)
	}
	delete(out, "approval_overrides")

	if fg := mapOf(out["file_guard"]); fg != nil {
		trimmed := make(map[string]any, len(fg))
		for k, v := range fg {
			if k == "paths" {
				continue
			}
			trimmed[k] = v
		}
		if len(trimmed) > 0 {
			out["file_guard"] = trimmed
		} else {
			delete(out, "file_guard")
		}
	}
	for k, v := range overlay {
		out[k] = v
	}
	if session {
		delete(out, "deny_tools")
		delete(out, "ask_tools")
		delete(out, "enabled")
	}
	return out
}

// loadYAMLStrict 读取 YAML 为 dict；strict 模式下解析/类型错误向上返回。
//
// Python: _load_yaml_dict(path, *, strict=False)
func loadYAMLStrict(path string, strict bool) map[string]any {
	data, err := loadYAMLStrictStrict(path)
	if err != nil {
		if strict {
			return nil
		}
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.load_failed")
		return map[string]any{}
	}
	return data
}

// loadYAMLStrictStrict 读取 YAML 并返回解析错误（供 strict 调用方使用）。
//
// 文件不存在返回空 map 与 nil 错误（对齐 Python 的 path.is_file() 短路）。
func loadYAMLStrictStrict(path string) (map[string]any, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if info.IsDir() {
		return map[string]any{}, nil
	}
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, readErr
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var decoded map[string]any
	if unmarshalErr := yaml.Unmarshal(raw, &decoded); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	if decoded == nil {
		return map[string]any{}, nil
	}
	return decoded, nil
}

// dumpYAMLDict 把 dict 写入 YAML 文件。
//
// Python: _dump_yaml_dict(path, data)
func dumpYAMLDict(path string, data map[string]any) bool {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.dump_failed")
		return false
	}
	encoded, err := yaml.Marshal(data)
	if err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.dump_failed")
		return false
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		logger.Warn(logComponent).Str("path", path).Err(err).
			Msg("permissions.layers.dump_failed")
		return false
	}
	return true
}

// stringOf 取字符串值，非字符串返回空串。
func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// mapOf 取 map[string]any，非该类型返回 nil。
func mapOf(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// listOfAnyMaps 从 any 里取 map 列表，忽略非 map 项。
func listOfAnyMaps(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		if typed, isTyped := v.([]map[string]any); isTyped {
			return typed
		}
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, isMap := item.(map[string]any); isMap {
			out = append(out, m)
		}
	}
	return out
}

// shallowCopyAnyMap 浅拷贝 map。
func shallowCopyAnyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// deepCopyAnyMap 深拷贝 map（只处理 map/slice 的嵌套）。
func deepCopyAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = deepCopyAnyValue(v)
	}
	return out
}

// deepCopyAnyValue 深拷贝任意值。
func deepCopyAnyValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		return deepCopyAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = deepCopyAnyValue(item)
		}
		return out
	}
	return v
}

// toSetFromSlice 把字符串切片转为集合。
func toSetFromSlice(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

// unionSets 合并多个字符串集合。
func unionSets(sets ...map[string]struct{}) map[string]struct{} {
	total := 0
	for _, s := range sets {
		total += len(s)
	}
	out := make(map[string]struct{}, total)
	for _, s := range sets {
		for k := range s {
			out[k] = struct{}{}
		}
	}
	return out
}

// unionOverrideKeys 合并多个 override 键集合。
func unionOverrideKeys(sets ...map[string]struct{}) map[string]struct{} {
	return unionSets(sets...)
}

// unionStringSets 合并多个字符串集合。
func unionStringSets(sets ...map[string]struct{}) map[string]struct{} {
	return unionSets(sets...)
}

// unionPathKeys 合并多个路径键集合。
func unionPathKeys(sets ...map[string]struct{}) map[string]struct{} {
	return unionSets(sets...)
}

// configDir 返回配置目录。
func configDir() string {
	if dir := os.Getenv("UAPCLAW_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".uapclaw"
	}
	return filepath.Join(home, ".uapclaw")
}

// agentSessionsDir 返回 agent 会话目录。
func agentSessionsDir() string {
	if dir := os.Getenv("UAPCLAW_SESSIONS_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(configDir(), "sessions")
}

// globalConfigPath 返回全局配置文件路径。
func globalConfigPath() string {
	if path := os.Getenv("UAPCLAW_CONFIG_YAML"); path != "" {
		return path
	}
	return filepath.Join(configDir(), "config.yaml")
}

// readGlobalConfig 读取全局配置文件（失败返回空 map）。
func readGlobalConfig() map[string]any {
	cfg, err := loadYAMLStrictStrict(globalConfigPath())
	if err != nil {
		logger.Warn(logComponent).Str("path", globalConfigPath()).Err(err).
			Msg("permissions.layers.global_config_load_failed")
		return map[string]any{}
	}
	return cfg
}

// ──────────────────────────── 用户层工具级别更新 ────────────────────────────

// UpdateUserToolLevel 设置用户层某个工具的级别。
//
// 四步（对齐 Python update_user_tool_level）：
//  1. 参数校验（工具名非空、级别合法）
//  2. 在共享锁内改用户层：把工具放进唯一一个列表（allow/ask/deny）
//  3. 级别为 ask 时撤销已记忆的允许：用户层与所有会话层的 allow_tools，
//     以及命名了该工具的 approval_overrides（路径规则保留不动）
//  4. 落盘并重新合成生效配置，返回 effective_level
//
// 返回 nil 表示失败（锁超时、写盘失败等）。
//
// Python: update_user_tool_level(tool_name, level)
func UpdateUserToolLevel(toolName, level string) map[string]any {
	name := strings.TrimSpace(toolName)
	lv := strings.ToLower(strings.TrimSpace(level))
	if name == "" {
		return nil
	}
	if _, valid := toolLevels[lv]; !valid {
		return nil
	}

	revoked := map[string]int{
		"user_allow":         0,
		"session_allow":      0,
		"approval_overrides": 0,
	}

	release, err := PermissionStorageLock("")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("permissions.layers.user_tool_update_lock_failed")
		return nil
	}
	user := LoadUserPermissions()
	if setUserToolLevel(user, name, lv) {
		revoked["user_allow"] = 1
	}
	if lv == toolLevelAsk {
		revoked["approval_overrides"] += stripToolOverrides(user, name)
		for _, sid := range ListSavedSessionIDs() {
			// 已持 PermissionStorageLock，这里只加会话文件锁，不能再次进
			// permissionStorageLockWithTimeout（storageLockMu 不可重入）。
			sessionRelease, lockErr := lockSessionFileOnly(sid, defaultLockTimeout)
			if lockErr != nil {
				release()
				logger.Warn(logComponent).Str("session", sid).Err(lockErr).
					Msg("permissions.layers.session_lock_failed")
				return nil
			}
			session := LoadSessionPermissions(sid)
			removed := stripToolOverrides(session, name)
			dropped := dropSessionAllow(session, name)
			revoked["approval_overrides"] += removed
			if dropped {
				revoked["session_allow"] = 1
			}
			if dropped || removed > 0 {
				path, _ := SessionPermissionsPath(sid)
				if !dumpYAMLDict(path, session) {
					sessionRelease()
					release()
					return nil
				}
			}
			sessionRelease()
		}
	}
	if !SaveUserPermissionsLocked(user) {
		release()
		return nil
	}
	savedUser := user
	globalPermissions := LoadGlobalPermissions()
	release()

	effective := ComposeHostEffectivePermissions(globalPermissions, savedUser, map[string]any{}, "", false)
	effectiveLevel := ""
	if tools := mapOf(effective["tools"]); tools != nil {
		effectiveLevel = stringOf(tools[name])
	}
	return map[string]any{
		"tool":            name,
		"level":           lv,
		"effective_level": effectiveLevel,
		"revoked":         revoked,
	}
}

// setUserToolLevel 把工具放进唯一一个用户层列表。返回是否移除了 allow 项。
//
// 三层清理（对齐 Python _set_user_tool_level）：
//  1. 从三个列表里都摘掉该工具，再按级别加进对应列表
//  2. 从 tools 字段里摘掉同名键（不论级别）
//  3. 空列表/空 tools 段直接删除，避免留空壳
//
// Python: _set_user_tool_level(layer, tool, level)
func setUserToolLevel(layer map[string]any, tool, level string) bool {
	removedAllow := listHasTool(toolList(layer, "allow_tools"), tool) && level != toolLevelAllow

	for _, key := range toolLevelListKeys {
		listed := map[string]string{
			"deny_tools":  toolLevelDeny,
			"ask_tools":   toolLevelAsk,
			"allow_tools": toolLevelAllow,
		}[key]
		kept := withoutTool(toolList(layer, key), tool)
		if listed == level {
			kept = append(kept, tool)
		}
		if len(kept) > 0 {
			layer[key] = kept
		} else {
			delete(layer, key)
		}
	}
	if tools := mapOf(layer["tools"]); tools != nil {
		for key := range tools {
			if strings.TrimSpace(key) == tool {
				delete(tools, key)
			}
		}
		if len(tools) == 0 {
			delete(layer, "tools")
		}
	}
	return removedAllow
}

// dropSessionAllow 移除会话层已记忆的允许。返回是否发生变更。
//
// 只处理 allow_tools 与 tools 里级别为 allow 的项——会话层的 ask/deny
// 字段不构成授权，无需清理。
//
// Python: _drop_session_allow(layer, tool)
func dropSessionAllow(layer map[string]any, tool string) bool {
	changed := false
	allowed := toolList(layer, "allow_tools")
	if listHasTool(allowed, tool) {
		kept := withoutTool(allowed, tool)
		if len(kept) > 0 {
			layer["allow_tools"] = kept
		} else {
			delete(layer, "allow_tools")
		}
		changed = true
	}
	if tools := mapOf(layer["tools"]); tools != nil {
		for key, value := range tools {
			if strings.TrimSpace(key) == tool && scalarLevel(value) == toolLevelAllow {
				delete(tools, key)
				changed = true
			}
		}
		if len(tools) == 0 {
			delete(layer, "tools")
		}
	}
	return changed
}

// stripToolOverrides 删除只命名了该工具的 override；多工具共享的只摘掉该名字。
//
// 返回处理条数（含被摘名但保留的条目）。
//
// Python: _strip_tool_overrides(layer, tool)
func stripToolOverrides(layer map[string]any, tool string) int {
	overrides, ok := layer["approval_overrides"].([]any)
	if !ok {
		return 0
	}
	kept := make([]any, 0, len(overrides))
	removed := 0
	changed := false
	for _, item := range overrides {
		m, isMap := item.(map[string]any)
		if !isMap {
			kept = append(kept, item)
			continue
		}
		rawTools, isList := m["tools"].([]any)
		if !isList {
			kept = append(kept, item)
			continue
		}
		names := make([]string, 0, len(rawTools))
		for _, t := range rawTools {
			if s, isStr := t.(string); isStr && strings.TrimSpace(s) != "" {
				names = append(names, strings.TrimSpace(s))
			}
		}
		if !containsStringValue(names, tool) {
			kept = append(kept, item)
			continue
		}
		changed = true
		removed++
		rest := make([]any, 0, len(names))
		for _, name := range names {
			if name != tool {
				rest = append(rest, name)
			}
		}
		if len(rest) == 0 {
			continue
		}
		updated := shallowCopyAnyMap(m)
		updated["tools"] = rest
		kept = append(kept, updated)
	}
	if changed {
		if len(kept) > 0 {
			layer["approval_overrides"] = kept
		} else {
			delete(layer, "approval_overrides")
		}
	}
	return removed
}

// toolList 取某一层的列表键（非列表返回空）。
//
// Python: _tool_list(layer, key)
func toolList(layer map[string]any, key string) []any {
	raw, ok := layer[key].([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(raw))
	out = append(out, raw...)
	return out
}

// listHasTool 判定列表里是否含指定工具名。
//
// Python: _list_has_tool(items, tool)
func listHasTool(items []any, tool string) bool {
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) == tool {
			return true
		}
	}
	return false
}

// withoutTool 过滤掉指定工具名。
//
// Python: _without_tool(items, tool)
func withoutTool(items []any, tool string) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) == tool {
			continue
		}
		out = append(out, item)
	}
	return out
}
