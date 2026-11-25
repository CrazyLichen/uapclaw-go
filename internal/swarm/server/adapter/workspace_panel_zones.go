package adapter

import (
	"mime"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// workspaceZone 工作区路径 zone（相对租户根，最长前缀匹配）。
// Python: jiuwenswarm/enterprise/capabilities/workspace/zones.py WorkspaceZone。
type workspaceZone string

// zone 枚举成员（Python zones.py:14-24，全量映射不裁剪）。
const (
	zoneSessionWorkspace workspaceZone = "session_workspace"
	zoneSessionTodo      workspaceZone = "session_todo"
	zoneSessionMeta      workspaceZone = "session_meta"
	zoneSkills           workspaceZone = "skills"
	zoneRoleMD           workspaceZone = "role_md"
	zoneMemory           workspaceZone = "memory"
	zoneRuntime          workspaceZone = "runtime"
	zoneArtifact         workspaceZone = "artifact"
	zoneOther            workspaceZone = "other"
)

// deletableZones 可删除 zone 白名单：仅 session_workspace / session_todo / artifact。
// Python: zones.py:27-32 DELETABLE_ZONES。
var deletableZones = map[workspaceZone]bool{
	zoneSessionWorkspace: true,
	zoneSessionTodo:      true,
	zoneArtifact:         true,
}

// roleMDNames role_md 专属文件名。
// Python: zones.py:35-41 _ROLE_MD_NAMES。
var roleMDNames = map[string]bool{
	"AGENT.md":     true,
	"SOUL.md":      true,
	"IDENTITY.md":  true,
	"HEARTBEAT.md": true,
	"USER.md":      true,
}

// zonePrefixRule zone 前缀规则。
type zonePrefixRule struct {
	prefix string
	zone   workspaceZone
}

// prefixRules 前缀规则表（最长前缀匹配）。
// Python: zones.py:44-57 _PREFIX_RULES。
var prefixRules = []zonePrefixRule{
	{"agent/workspace/projects/", zoneSessionWorkspace},
	{"agent/workspace/todo/", zoneSessionTodo},
	{"agent/sessions/", zoneSessionMeta},
	{"agent/workspace/skills/", zoneSkills},
	{"agent/workspace/memory/", zoneMemory},
	{"agent/workspace/coding_memory/", zoneMemory},
	{"agent/workspace/context/", zoneMemory},
	{"agent/workspace/agents/", zoneRuntime},
	{"agent/workspace/extensions/", zoneRuntime},
	{"agent/workspace/messages/", zoneRuntime},
	{"agent/workspace/prompt_attachment/", zoneArtifact},
	{"agent/workspace/received_files/", zoneArtifact},
}

// workspacePathError 归一化失败原因（对齐 Python ValueError 语义）。
type workspacePathError string

const (
	pathErrTraversal workspacePathError = "path_traversal"
	pathErrAbsolute  workspacePathError = "absolute_path"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// workspaceNormalizeRelativePath 归一为 posix 相对路径，去前导 ./ 与尾部 /
// （根除外）。拒绝 ..、NUL 与绝对形态（POSIX /…、UNC //…、Windows C:/…）。
// Python: zones.py:60-83 normalize_relative_path。
func workspaceNormalizeRelativePath(relativePath string) (string, workspacePathError) {
	raw := strings.TrimSpace(relativePath)
	raw = strings.ReplaceAll(raw, "\\", "/")
	if raw == "" || raw == "." || raw == "./" {
		return "", ""
	}
	if strings.ContainsRune(raw, '\x00') {
		return "", pathErrTraversal
	}
	// 绝对/盘符路径不得静默转成相对路径。
	if strings.HasPrefix(raw, "/") {
		return "", pathErrAbsolute
	}
	if len(raw) >= 2 && isASCIILetter(raw[0]) && raw[1] == ':' {
		return "", pathErrAbsolute
	}
	parts := []string{}
	for _, part := range strings.Split(raw, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			return "", pathErrTraversal
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/"), ""
}

// isASCIILetter ASCII 字母判定（对齐 Python isalpha 的 ASCII 子集场景）。
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// workspaceClassifyZone 归类租户根相对路径的 zone（最长前缀匹配）。
// Python: zones.py:86-106 classify_zone。
func workspaceClassifyZone(relativePath string) workspaceZone {
	pathValue, perr := workspaceNormalizeRelativePath(relativePath)
	if perr != "" || pathValue == "" {
		return zoneOther
	}
	name := pathValue
	if idx := strings.LastIndex(pathValue, "/"); idx >= 0 {
		name = pathValue[idx+1:]
	}
	parent := ""
	if idx := strings.LastIndex(pathValue, "/"); idx >= 0 {
		parent = strings.TrimRight(pathValue[:idx], "/")
	}
	if roleMDNames[name] && parent == "agent/workspace" {
		return zoneRoleMD
	}
	best := workspaceZone("")
	bestLen := -1
	for _, rule := range prefixRules {
		trimmed := strings.TrimRight(rule.prefix, "/")
		if pathValue == trimmed || strings.HasPrefix(pathValue, rule.prefix) {
			if len(rule.prefix) > bestLen {
				best = rule.zone
				bestLen = len(rule.prefix)
			}
		}
	}
	if best == "" {
		return zoneOther
	}
	return best
}

// workspaceIsDeletable zone 白名单判定。
// Python: zones.py:109-110 is_deletable。
func workspaceIsDeletable(relativePath string) bool {
	return deletableZones[workspaceClassifyZone(relativePath)]
}

// workspaceMimeHint 依据扩展名猜测 mime（对齐 Python mimetypes.guess_type：
// 系统 mime 数据库 + 内建小表；未识别返回空串）。
func workspaceMimeHint(name string) string {
	return mime.TypeByExtension(filepath.Ext(name))
}

// workspaceSortedChildren 目录项排序：目录优先、名称小写序。
// Python: service.py:84-87 sorted(key=lambda p: (not p.is_dir(), p.name.lower()))。
func workspaceSortedChildren(names []string, isDir func(string) bool) []string {
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Slice(sorted, func(i, j int) bool {
		di, dj := isDir(sorted[i]), isDir(sorted[j])
		if di != dj {
			return di // 目录优先
		}
		return strings.ToLower(sorted[i]) < strings.ToLower(sorted[j])
	})
	return sorted
}

// workspaceCommonPathPrefix 对齐 Python os.path.commonpath 两元素判定：
// 两路径的公共前缀目录是否恰为 root。
func workspaceCommonPathPrefix(root, candidate string) bool {
	root = path.Clean(root)
	candidate = path.Clean(candidate)
	if candidate == root {
		return true
	}
	return strings.HasPrefix(candidate, root+"/")
}
