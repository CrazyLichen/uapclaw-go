package permission_tools

import (
	"fmt"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionToolNameResolution 一次工具名解析的结果。
//
// Python: @dataclass(frozen=True) PermissionToolNameResolution
type PermissionToolNameResolution struct {
	// RegisteredName 调用方传入的原始名（仅做首尾空白裁剪）
	RegisteredName string
	// CanonicalName 归一化后的规范名；Conflict 为真时为空串
	CanonicalName string
	// Aliases 指向同一 CanonicalName 的其他别名，按字典序排序
	Aliases []string
	// Conflict 别名图是否歧义（歧义时调用方必须 fail-closed）
	Conflict bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// permissionToolAliases 别名 → 规范名映射。
//
// Python: PERMISSION_TOOL_ALIASES
var permissionToolAliases = map[string]string{
	"exec_command":  "mcp_exec_command",
	"fetch_webpage": "mcp_fetch_webpage",
	"free_search":   "mcp_free_search",
	"paid_search":   "mcp_paid_search",
}

// permissionLevelRank 权限级别严格度排名，数字越大越严。
//
// Python: PERMISSION_LEVEL_RANK
var permissionLevelRank = map[string]int{
	"allow": 0,
	"ask":   1,
	"deny":  2,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolvePermissionToolName 解析一个工具名，别名图歧义时 fail-closed。
//
// canonicalNames 是调用方已知的规范名集合（用于冲突检测），可为 nil。
//
// Python: resolve_permission_tool_name(tool_name, *, canonical_names=())
func ResolvePermissionToolName(toolName any, canonicalNames []string) PermissionToolNameResolution {
	// Python: try: registered_name = str(tool_name or "").strip()
	//         except Exception: → fail-closed
	registeredName, ok := safeString(toolName)
	if !ok {
		return PermissionToolNameResolution{Conflict: true}
	}
	registeredName = strings.TrimSpace(registeredName)

	// Python: canonical_name = PERMISSION_TOOL_ALIASES.get(registered_name, registered_name)
	canonicalName := registeredName
	if mapped, hit := permissionToolAliases[registeredName]; hit {
		canonicalName = mapped
	}

	// Python: alias_targets = frozenset(PERMISSION_TOOL_ALIASES.values())
	aliasTargets := make(map[string]struct{}, len(permissionToolAliases))
	for _, target := range permissionToolAliases {
		aliasTargets[target] = struct{}{}
	}

	canonicalSet := make(map[string]struct{}, len(canonicalNames))
	for _, n := range canonicalNames {
		canonicalSet[n] = struct{}{}
	}
	_, registeredIsCanonical := canonicalSet[registeredName]
	_, registeredIsAliasKey := permissionToolAliases[registeredName]

	// 三个 or 分支必须逐条照抄，不可合并——它们检查的是「同一个名字既是别名
	// 又是某个集合成员」的不同组合。
	conflict :=
		// 分支 1
		(registeredIsCanonical && registeredIsAliasKey && canonicalName != registeredName) ||
			// 分支 2
			func() bool {
				_, isAliasTarget := aliasTargets[registeredName]
				return isAliasTarget && registeredIsAliasKey && canonicalName != registeredName
			}() ||
			// 分支 3
			func() bool {
				next, isAliasKey := permissionToolAliases[canonicalName]
				return isAliasKey && next != canonicalName
			}()

	if conflict {
		return PermissionToolNameResolution{
			RegisteredName: registeredName,
			CanonicalName:  "",
			Aliases:        nil,
			Conflict:       true,
		}
	}

	// Python: aliases = tuple(sorted(alias for alias, target in ... if target == canonical_name and alias != canonical_name))
	aliases := make([]string, 0, 2)
	for alias, target := range permissionToolAliases {
		if target == canonicalName && alias != canonicalName {
			aliases = append(aliases, alias)
		}
	}
	sort.Strings(aliases)

	return PermissionToolNameResolution{
		RegisteredName: registeredName,
		CanonicalName:  canonicalName,
		Aliases:        aliases,
		Conflict:       false,
	}
}

// NormalizePermissionToolName 返回规范化后的工具名，供运行时权限检查使用。
//
// Python: normalize_permission_tool_name(tool_name)
func NormalizePermissionToolName(toolName any) string {
	return ResolvePermissionToolName(toolName, nil).CanonicalName
}

// StricterPermissionLevel 返回两个权限级别中更严的那个，未知输入按 ask。
//
// Python: stricter_permission_level(left, right)
func StricterPermissionLevel(left, right string) string {
	leftLevel := defaultAsk(strings.ToLower(strings.TrimSpace(left)))
	rightLevel := defaultAsk(strings.ToLower(strings.TrimSpace(right)))

	leftRank, okLeft := permissionLevelRank[leftLevel]
	if !okLeft {
		leftRank = permissionLevelRank["ask"]
	}
	rightRank, okRight := permissionLevelRank[rightLevel]
	if !okRight {
		rightRank = permissionLevelRank["ask"]
	}
	if leftRank >= rightRank {
		return leftLevel
	}
	return rightLevel
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeString 把任意值转字符串，对应 Python 的 str(tool_name or "")。
//
// 第二个返回值恒为 true：Go 的字符串转换不会抛异常，保留返回位以对齐
// Python 的 try/except 结构。
func safeString(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", true
	case string:
		return x, true
	default:
		return fmt.Sprint(x), true
	}
}

// defaultAsk 空串兜底为 "ask"，对应 Python 的 str(left or "ask")。
func defaultAsk(s string) string {
	if s == "" {
		return "ask"
	}
	return s
}
