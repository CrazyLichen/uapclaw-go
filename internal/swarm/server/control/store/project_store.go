package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Project 项目实体（对应 projects.json 中单个项目记录）。
// Python: jiuwenswarm/server/runtime/session/project_store.py Project（:128-168）。
type Project struct {
	// ProjectID 项目唯一标识
	ProjectID string
	// Name 项目名称
	Name string
	// ProjectDir 项目目录
	ProjectDir string
	// Pinned 是否置顶
	Pinned bool
	// PinOrder 置顶顺序
	PinOrder int
	// Hidden 是否软删除（隐藏）
	Hidden bool
	// CreatedAt 创建时间戳
	CreatedAt float64
	// UpdatedAt 更新时间戳
	UpdatedAt float64
	// WorkMode 工作模式："code" 或 "work"；旧数据兜底为 "work"
	WorkMode string
	// Git Git 状态快照（由 ProjectGitService 写入，本模块不维护内容）
	Git map[string]any
}

// CronProjectBinding cron 任务的项目归属解析结果。
// Python: project_store.py CronProjectBinding（:170-181）。
type CronProjectBinding struct {
	// ProjectID 归属项目标识
	ProjectID string
	// WorkMode 生效工作模式
	WorkMode string
	// Error 错误信息（nil 表示成功）
	Error string
	// Code 错误码
	Code string
	// Hidden 命中的项目是否隐藏
	Hidden bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// projectsCache 项目注册表进程内缓存（对齐 _CACHE；cache_bust 强制读盘）。
var projectsCache []map[string]any

// ──────────────────────────── 非导出函数 ────────────────────────────

// agentRootOverride Agent 根目录测试注入点（对齐 Python 测试对
// get_agent_root_dir 的 monkeypatch；生产恒走 workspace.AgentRootDir）。
var agentRootOverride string

// projectsFile projects.json 路径：Agent 根目录（与 sessions/ 同级）。
// Python: project_store.py _projects_file（get_agent_root_dir() / "projects.json"）。
func projectsFile() string {
	root := agentRootOverride
	if root == "" {
		root = workspace.AgentRootDir()
	}
	return filepath.Join(root, "projects.json")
}

// readDiskProjects 文件锁外读取（单进程文件锁语义由调用方保证；文件缺失/损坏返回空表）。
// Python: project_store.py _read_disk_locked（:197-211）。
func readDiskProjects(path string) []map[string]any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return []map[string]any{}
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []map[string]any{}
	}
	items, _ := doc["projects"].([]any)
	out := []map[string]any{}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// writeDiskProjects 原子写回（tmp + rename；Python _write_disk_locked :233-247）。
func writeDiskProjects(path string, projects []map[string]any) error {
	data, err := json.MarshalIndent(map[string]any{"projects": projects}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := strings.TrimSuffix(path, filepath.Ext(path)) + ".json.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadProjects 读缓存；cacheBust=true 强制读盘（跨进程同步场景）。
// Python: project_store.py _load_cache（:267-305），含惰性迁移：
// 缺 work_mode 的老项目推断并写回磁盘，保证磁盘 schema 统一。
func loadProjects(cacheBust bool) []map[string]any {
	if !cacheBust && projectsCache != nil {
		return projectsCache
	}
	path := projectsFile()
	raw := readDiskProjects(path)
	changed := false
	for _, p := range raw {
		existingWm, _ := p["work_mode"].(string)
		if supportedWorkModes[asciiLower(strings.TrimSpace(existingWm))] {
			continue
		}
		p["work_mode"] = inferLegacyProjectWorkMode(p)
		changed = true
	}
	if changed {
		if err := writeDiskProjects(path, raw); err != nil {
			logger.Warn(logger.ComponentAgentServer).Err(err).
				Msg("Project 惰性迁移写回 projects.json 失败")
		}
	}
	projectsCache = raw
	return raw
}

// inferLegacyProjectWorkMode 旧项目记录推断 work_mode：有合法字段用之，否则回退 "work"。
// Python: work_mode.py infer_legacy_project_work_mode。
func inferLegacyProjectWorkMode(rawProject map[string]any) string {
	return NormalizeWorkMode(rawProject["work_mode"], WorkModeWeb)
}

// projectFromDict dict → Project（对齐 Project.from_dict :150-168）。
func projectFromDict(d map[string]any) Project {
	gitRaw, _ := d["git"].(map[string]any)
	git := map[string]any{}
	for k, v := range gitRaw {
		git[k] = v
	}
	return Project{
		ProjectID:  stringFromAny(d["project_id"]),
		Name:       stringFromAny(d["name"]),
		ProjectDir: stringFromAny(d["project_dir"]),
		Pinned:     boolFromAny(d["pinned"]),
		PinOrder:   intFromAny(d["pin_order"]),
		Hidden:     boolFromAny(d["hidden"]),
		CreatedAt:  floatFromAny(d["created_at"]),
		UpdatedAt:  floatFromAny(d["updated_at"]),
		WorkMode:   inferLegacyProjectWorkMode(d),
		Git:        git,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ListProjects 列出项目。includeHidden=false（默认）时排除已软删除项目。
// Python: project_store.py list_projects（:482-492）。
func ListProjects(includeHidden bool, cacheBust bool) []Project {
	var result []Project
	for _, p := range loadProjects(cacheBust) {
		if !includeHidden {
			if hidden, _ := p["hidden"].(bool); hidden {
				continue
			}
		}
		result = append(result, projectFromDict(p))
	}
	return result
}

// GetProjectByID 按 project_id 查找项目（默认项目不入库，不会命中）。
// Python: project_store.py get_project_by_id（:307-315）。
func GetProjectByID(projectID string, cacheBust bool) *Project {
	for _, p := range loadProjects(cacheBust) {
		if id, _ := p["project_id"].(string); id == projectID {
			proj := projectFromDict(p)
			return &proj
		}
	}
	return nil
}

// NormalizePathForMatch 路径规范化匹配（大小写不敏感 + 统一分隔符 + Clean）。
// Python: project_store.py _normalize_path_for_match（:334-340）。
func NormalizePathForMatch(path string) string {
	// 先统一分隔符再 Clean：Linux 的 filepath.Clean 不把反斜杠当分隔符
	normalized := strings.ReplaceAll(path, "\\", "/")
	normalized = filepath.Clean(normalized)
	return asciiLower(normalized)
}

// ResolveCronProjectID cron 侧 (work_mode, project_dir) → project_id 解析。
// Python: project_store.py resolve_cron_project_id（:494-532）。
// project_dir 为空 → ""；非绝对路径 → error；绝对路径按 (work_mode, 目录) 匹配可见项目。
func ResolveCronProjectID(projectDir, workMode string) (string, error) {
	mode := asciiLower(strings.TrimSpace(workMode))
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return "", nil
	}
	if !filepath.IsAbs(projectDir) {
		return "", fmt.Errorf("project_dir must be an absolute path")
	}
	norm := NormalizePathForMatch(projectDir)
	for _, p := range ListProjects(true, true) {
		pdir := p.ProjectDir
		if pdir == "" {
			continue
		}
		if p.WorkMode == mode && NormalizePathForMatch(pdir) == norm && !p.Hidden {
			return p.ProjectID, nil
		}
	}
	return "", nil
}

// ResolveCronProjectBinding 解析 cron 任务的项目归属。
// Python: project_store.py resolve_cron_project_binding（:535-630）。
func ResolveCronProjectBinding(projectID, projectDir, workMode any) CronProjectBinding {
	inputMode := asciiLower(strings.TrimSpace(stringFromAny(workMode)))
	if inputMode == "" {
		inputMode = WorkModeWeb
	}
	matchMode := asciiLower(strings.TrimSpace(stringFromAny(workMode)))
	if !executionWorkModes[inputMode] {
		return CronProjectBinding{WorkMode: inputMode,
			Error: fmt.Sprintf("cron is not supported for work_mode: %q", inputMode), Code: "BAD_REQUEST"}
	}
	rawProjectID := strings.TrimSpace(stringFromAny(projectID))
	if rawProjectID == DefaultProjectIDWork || rawProjectID == DefaultProjectIDCode {
		wm := WorkModeWeb
		if rawProjectID == DefaultProjectIDCode {
			wm = WorkModeCode
		}
		return CronProjectBinding{ProjectID: rawProjectID, WorkMode: wm}
	}
	if rawProjectID != "" {
		proj := GetProjectByID(rawProjectID, true)
		if proj == nil {
			return CronProjectBinding{WorkMode: inputMode,
				Error: fmt.Sprintf("project not found: %q", rawProjectID), Code: "NOT_FOUND"}
		}
		if proj.Hidden {
			return CronProjectBinding{WorkMode: inputMode, Hidden: true,
				Error: fmt.Sprintf("project is hidden: %q", rawProjectID), Code: "NOT_FOUND"}
		}
		if !executionWorkModes[proj.WorkMode] {
			return CronProjectBinding{WorkMode: proj.WorkMode,
				Error: fmt.Sprintf("cron is not supported for work_mode: %q", proj.WorkMode), Code: "BAD_REQUEST"}
		}
		wm := proj.WorkMode
		if wm == "" {
			wm = WorkModeWeb
		}
		return CronProjectBinding{ProjectID: rawProjectID, WorkMode: wm}
	}
	projectDirVal := strings.TrimSpace(stringFromAny(projectDir))
	resolvedProjectID, err := ResolveCronProjectID(projectDirVal, matchMode)
	if err != nil {
		return CronProjectBinding{WorkMode: inputMode, Error: err.Error(), Code: "BAD_REQUEST"}
	}
	if IsDefaultProjectID(resolvedProjectID) {
		resolvedProjectID = ResolveDefaultProjectID(inputMode)
		wm := WorkModeWeb
		if resolvedProjectID == DefaultProjectIDCode {
			wm = WorkModeCode
		}
		return CronProjectBinding{ProjectID: resolvedProjectID, WorkMode: wm}
	}
	resolvedWorkMode := inputMode
	if proj := GetProjectByID(resolvedProjectID, true); proj != nil {
		resolvedWorkMode = proj.WorkMode
		if resolvedWorkMode == "" {
			resolvedWorkMode = WorkModeWeb
		}
		if !executionWorkModes[resolvedWorkMode] {
			return CronProjectBinding{WorkMode: resolvedWorkMode,
				Error: fmt.Sprintf("cron is not supported for work_mode: %q", resolvedWorkMode), Code: "BAD_REQUEST"}
		}
	}
	return CronProjectBinding{ProjectID: resolvedProjectID, WorkMode: resolvedWorkMode}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringFromAny any → string（nil 安全）。
func stringFromAny(v any) string {
	s, _ := v.(string)
	return s
}

// boolFromAny any → bool（nil 安全）。
func boolFromAny(v any) bool {
	b, _ := v.(bool)
	return b
}

// intFromAny any → int（float64/int 兼容，JSON 解码值为 float64）。
func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// floatFromAny any → float64（nil 安全）。
func floatFromAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}
