package lifecycle

import (
	pathpkg "path"
	"strings"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProjectCandidate 项目反查表的候选条目。
// Python: lifecycle.py build_project_lookup 的 (project_id, work_mode) 元组
type ProjectCandidate struct {
	// ProjectID 项目标识
	ProjectID string
	// WorkMode 工作模式（work / code）
	WorkMode string
}

// ProjectRecord 项目存储对外暴露的最小字段。
// Python: project_store.list_projects 返回的 Project 对象（Go 侧只取这两项）
type ProjectRecord struct {
	// ProjectID 项目标识
	ProjectID string
	// ProjectDir 项目目录
	ProjectDir string
	// WorkMode 工作模式
	WorkMode string
}

// ProjectLookup 项目目录 → 项目列表的反查表。
// Python: lifecycle.py build_project_lookup 的返回值
type ProjectLookup struct {
	// ByDirectory 规范化目录 → 候选列表
	ByDirectory map[string][]ProjectCandidate
	// WorkModes 项目标识 → 工作模式
	WorkModes map[string]string
}

// ProjectSource 项目存储的窄接口（依赖倒置：lifecycle 作为使用方定义）。
//
// ⚠️ 项目存储（Python server/runtime/session/project_store.py，1213 行）
// 尚未迁移到 Go 侧。此处只声明 lifecycle 用到的两个能力，迁移完成后由
// server 侧注入真实实现；未注入时退化为"无项目数据"，与上游空库行为一致。
type ProjectSource interface {
	// ListProjects 列出全部项目（含隐藏）
	ListProjects() ([]ProjectRecord, error)
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// projectSourceMu 保护 projectSource
	projectSourceMu sync.RWMutex
	// projectSource 已注入的项目存储（nil 表示未迁移）
	projectSource ProjectSource
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetProjectSource 注入项目存储（server 侧启动时调用）。
func SetProjectSource(source ProjectSource) {
	projectSourceMu.Lock()
	defer projectSourceMu.Unlock()
	projectSource = source
}

// ListProjects 列出全部项目；未注入项目存储时返回空列表。
// Python: project_store.list_projects(include_hidden=True, cache_bust=True)
func ListProjects() []ProjectRecord {
	projectSourceMu.RLock()
	source := projectSource
	projectSourceMu.RUnlock()
	if source == nil {
		return nil
	}
	projects, err := source.ListProjects()
	if err != nil {
		return nil
	}
	return projects
}

// NormalizePathForMatch 路径规范化，供目录反查使用。
// Python: project_store._normalize_path_for_match
//
// 大小写不敏感 + 统一分隔符 + 去尾部分隔符：会话元数据里存的历史路径
// 与项目表里的路径可能大小写/分隔符不一致，不规范化会漏匹配。
func NormalizePathForMatch(path string) string {
	// 先统一分隔符再 Clean：Linux 的 filepath.Clean 不把反斜杠当分隔符，
	// 先 Clean 会留下 "C:\Work\Proj\" 这样的尾分隔符，导致跨平台漏匹配。
	normalized := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	cleaned := pathpkg.Clean(normalized)
	return strings.ToLower(cleaned)
}
