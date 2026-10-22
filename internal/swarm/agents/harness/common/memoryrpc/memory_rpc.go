// Package memoryrpc 的 handler 实现见各函数。
// 包注释见 doc.go（多文件包注释只在 doc.go 一处）。
package memoryrpc

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	utilpath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
	projectmemory "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识（对齐 memory 包既有约定）
var logComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// HandleMemoryEdit 编辑（或创建）记忆文件。
// Python: handle_memory_edit (memory_rpc.py L245-286)
func HandleMemoryEdit(ctx context.Context, workspace string, params map[string]any) map[string]any {
	// Python: raw_path = params.get("path", ""); project_dir = params.get("project_dir")
	rawPath, _ := params["path"].(string)
	projectDir, _ := params["project_dir"].(string)
	isValid, resolved := validateEditPath(rawPath, workspace, projectDir)

	if !isValid {
		// Python L253-260：返回原始 raw_path + editable=False
		return map[string]any{
			"path":            rawPath,
			"exists":          false,
			"content_preview": "",
			"kind":            "unknown",
			"editable":        false,
		}
	}

	// Python: exists = Path(resolved).is_file()
	info, statErr := os.Stat(resolved)
	exists := statErr == nil && !info.IsDir()
	kind := classifyMemoryFile(resolved, workspace)

	if exists {
		// Python: 读全文 splitlines 取前 20 行；超 20 行追加 "\n... (truncated)"
		contentPreview := ""
		if text, err := os.ReadFile(resolved); err == nil {
			// 对齐 Python splitlines 语义：\r\n 是行界（不保留 \r）、尾换行不产生末尾空行。
			// 孤立 \r 与 Unicode 行界（\v\f\u2028 等）不在此处理（md 记忆文件极罕见）
			normalized := strings.ReplaceAll(string(text), "\r\n", "\n")
			normalized = strings.TrimSuffix(normalized, "\n")
			lines := strings.Split(normalized, "\n")
			if len(lines) > 20 {
				contentPreview = strings.Join(lines[:20], "\n") + "\n... (truncated)"
			} else {
				contentPreview = strings.Join(lines, "\n")
			}
		}
		return map[string]any{
			"path":            resolved,
			"exists":          true,
			"content_preview": contentPreview,
			"kind":            kind,
			"editable":        true,
		}
	}

	// Python L276-278：不存在 → parent.mkdir(parents=True, exist_ok=True) + touch
	// M-04: Python 该两步在 try 外，OSError 上抛由注册层转 ok=False；
	// Go handler 无 error 返回通道，改用响应内嵌 error 字段协议
	// （11.15 注册层检查该字段转 ok=False，对齐 Python 外层 wrapper 语义）。
	// editable 保持 true 不动：payload 结构稳定，Python 异常路径无 payload 可参照。
	createErr := ""
	parent := filepath.Dir(resolved)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		logger.Error(logComponent).Err(err).Str("path", resolved).Msg("memory.edit mkdir failed")
		createErr = err.Error()
	} else if f, err := os.OpenFile(resolved, os.O_CREATE|os.O_WRONLY, 0o644); err != nil {
		logger.Error(logComponent).Err(err).Str("path", resolved).Msg("memory.edit touch failed")
		createErr = err.Error()
	} else {
		_ = f.Close()
	}

	result := map[string]any{
		"path":            resolved,
		"exists":          false,
		"content_preview": "",
		"kind":            kind,
		"editable":        true,
	}
	if createErr != "" {
		result["error"] = createErr
	}
	return result
}

// HandleMemoryOpen 返回各记忆目录路径。
// Python: handle_memory_open (L459-473)
func HandleMemoryOpen(ctx context.Context, workspace string, params map[string]any) map[string]any {
	projectDir, _ := params["project_dir"].(string)
	// Python: {"memory_dir": os.path.join(workspace, "memory"), "project_memory_dir": workspace}
	result := map[string]any{
		"memory_dir":         filepath.Join(workspace, "memory"),
		"project_memory_dir": workspace,
	}
	if projectDir != "" {
		result["project_dir"] = projectDir
	}
	// Python: if os.path.isdir(coding_dir): result["coding_memory_dir"] = coding_dir
	codingDir := getCodingMemoryDir(workspace, projectDir)
	if info, err := os.Stat(codingDir); err == nil && info.IsDir() {
		result["coding_memory_dir"] = codingDir
	}
	return result
}

// HandleMemoryList 列出所有记忆文件。
// Python: handle_memory_list (memory_rpc.py L198-242)
func HandleMemoryList(ctx context.Context, workspace, mode string, params map[string]any) map[string]any {
	// Python: include_project = params.get("include_project", True)
	includeProject := true
	if v, ok := params["include_project"].(bool); ok {
		includeProject = v
	}
	// Python: additional_dirs = params.get("additional_directories")
	var additionalDirs []string
	if dirs, ok := params["additional_directories"].([]any); ok {
		for _, d := range dirs {
			if s, ok := d.(string); ok {
				additionalDirs = append(additionalDirs, s)
			}
		}
	}
	projectDir, _ := params["project_dir"].(string)

	files := []map[string]any{}
	seenPaths := map[string]bool{}

	// Python L210-228：project 层发现（project 优先，占位去重集合）
	if includeProject {
		// Python: discover_workspace = project_dir or workspace
		discoverWorkspace := workspace
		if projectDir != "" {
			discoverWorkspace = projectDir
		}
		projectFiles, discoverErr := projectmemory.DiscoverAndLoadMemoryFiles(ctx, discoverWorkspace, discoverWorkspace, additionalDirs)
		// M-06: 吞错改为日志可观测（发现层故障时 List 展示与真实状态可区分）
		if discoverErr != nil {
			logger.Warn(logComponent).
				Str("method", "MemoryList").
				Str("workspace", discoverWorkspace).
				Err(discoverErr).
				Msg("discover memory files failed")
		}
		for _, f := range projectFiles {
			stat := safeStat(f.Path)
			// Python: "lines": f.content.count("\n") + 1 if f.content else 0
			lines := 0
			if f.Content != "" {
				lines = strings.Count(f.Content, "\n") + 1
			}
			files = append(files, map[string]any{
				"path":          f.Path,
				"relative_path": relativePath(f.Path, workspace, projectDir),
				"kind":          f.Kind,
				"exists":        true,
				"size":          stat["size"],
				"mtime":         stat["mtime"],
				"lines":         lines,
			})
			seenPaths[f.Path] = true
		}
	}

	// Python L230-234：auto 层（workspace/memory 扫描）
	memoryDir := filepath.Join(workspace, "memory")
	for _, item := range scanMDFiles(memoryDir, "auto", workspace, projectDir) {
		if p := item["path"].(string); !seenPaths[p] {
			files = append(files, item)
			seenPaths[p] = true
		}
	}

	// Python L236-240：coding 层（coding_memory 扫描）
	codingDir := getCodingMemoryDir(workspace, projectDir)
	for _, item := range scanMDFiles(codingDir, "coding", workspace, projectDir) {
		if p := item["path"].(string); !seenPaths[p] {
			files = append(files, item)
			seenPaths[p] = true
		}
	}

	// Python: return {"files": files, "mode": mode}
	return map[string]any{"files": files, "mode": mode}
}

// HandleMemoryStatus 返回记忆系统总状态。
// Python: handle_memory_status (memory_rpc.py L289-389)
func HandleMemoryStatus(ctx context.Context, workspace, mode string, params map[string]any) map[string]any {
	// Python: detailed = params.get("detailed", False); config = get_config()
	detailed := false
	if v, ok := params["detailed"].(bool); ok {
		detailed = v
	}
	cfg := loadConfigMap()

	// Python L297-299：code 模式硬编码 true/false，非 code 模式查配置
	codeMode := isCodeMode(mode)
	enabled := true
	proactive := false
	if !codeMode {
		enabled = commmem.IsMemoryEnabled(mode, cfg)
		proactive = commmem.IsProactiveMemory(mode, cfg)
	}

	// Python L303: storage_mode = config.get("memory", {}).get("mode", "local")
	storageMode := "local"
	if memCfg, ok := cfg["memory"].(map[string]any); ok {
		if m, ok := memCfg["mode"].(string); ok {
			storageMode = m
		}
	}

	// Python L301-308：基本块
	result := map[string]any{
		"current_mode":      mode,
		"storage_mode":      storageMode,
		"engine":            commmem.GetMemoryEngine(cfg),
		"enabled":           enabled,
		"proactive":         proactive,
		"forbidden_enabled": isForbiddenEnabled(cfg),
	}

	if !detailed {
		return result
	}

	// 块 1 Python L310-324：index（管理器状态，.get 默认值语义）
	if manager, err := commmem.GetMemoryManager(ctx, "default", workspace, nil); err == nil && manager != nil {
		status := manager.Status()
		index := map[string]any{
			"available":    anyOr(status["available"], false),
			"provider":     anyOr(status["provider"], nil),
			"model":        anyOr(status["model"], nil),
			"files_count":  anyOr(status["files"], 0),
			"chunks_count": anyOr(status["chunks"], 0),
			"dirty":        anyOr(status["dirty"], false),
			"fts":          anyOr(status["fts"], map[string]any{}),
			"vector":       anyOr(status["vector"], map[string]any{}),
			"cache":        anyOr(status["cache"], map[string]any{}),
		}
		result["index"] = index
	}

	// 块 2 Python L326-340：project_memory 统计 + 日志
	projectDir, _ := params["project_dir"].(string)
	// Python: discover_workspace = project_dir or workspace
	discoverWorkspace := workspace
	if projectDir != "" {
		discoverWorkspace = projectDir
	}
	projectmemory.ClearProjectMemoryCache(discoverWorkspace)
	projectFiles, discoverErr := projectmemory.DiscoverAndLoadMemoryFiles(ctx, discoverWorkspace, discoverWorkspace, nil)
	// M-06: 吞错改为日志可观测（对齐 Python 注册层 ok=False 通道的 Go 侧补偿，
	// 11.15 注册层实现时可用 error 字段协议透传）
	if discoverErr != nil {
		logger.Warn(logComponent).
			Str("method", "MemoryStatus").
			Str("workspace", discoverWorkspace).
			Err(discoverErr).
			Msg("discover memory files failed")
	}
	totalChars := 0
	for _, f := range projectFiles {
		// S-03: Python len() 按 Unicode 字符计数，Go 用 RuneCountInString 对齐
		// （byte 长度对 CJK 有 ~3x 偏差，total_chars 统计失真）
		totalChars += utf8.RuneCountInString(f.Content)
	}
	largeFiles := projectmemory.GetLargeMemoryFiles(projectFiles, projectmemory.MaxMemoryCharCount)
	// Python logger.info（规则 3 日志同步：5 字段）
	logger.Info(logComponent).
		Str("workspace", workspace).
		Str("project_dir", projectDir).
		Str("discover_workspace", discoverWorkspace).
		Int("files", len(projectFiles)).
		Int("large_files", len(largeFiles)).
		Msg("[memory_rpc] memory.status detailed")
	projectMemory := map[string]any{
		"files_count": len(projectFiles),
		"total_chars": totalChars,
		"max_chars":   projectmemory.DefaultMaxChars,
		"threshold":   projectmemory.MaxMemoryCharCount,
	}
	// Python L348-349: if project_dir: result["project_memory"]["project_dir"]
	if projectDir != "" {
		projectMemory["project_dir"] = projectDir
	}
	result["project_memory"] = projectMemory

	// 块 3 Python L347: result["large_files"] = large_files
	if largeFiles == nil {
		largeFiles = []map[string]any{}
	}
	result["large_files"] = largeFiles

	// 块 4 Python L351-364：coding_memory 统计（逐文件读字符数）
	codingDir := getCodingMemoryDir(workspace, projectDir)
	codingFiles := scanMDFiles(codingDir, "coding", workspace, "")
	codingTotalChars := 0
	for _, cf := range codingFiles {
		if data, err := os.ReadFile(cf["path"].(string)); err == nil {
			// S-03: rune 计数对齐 Python len()（CJK 偏差 3x）
			codingTotalChars += utf8.RuneCountInString(string(data))
		}
	}
	result["coding_memory"] = map[string]any{
		"files_count": len(codingFiles),
		"total_chars": codingTotalChars,
		// Python: "dir": coding_dir if os.path.isdir(coding_dir) else ""
		"dir": dirOrEmpty(codingDir),
	}

	// 块 5 Python L366-379：auto_memory 统计
	memoryDir := filepath.Join(workspace, "memory")
	autoFiles := scanMDFiles(memoryDir, "auto", workspace, "")
	autoTotalChars := 0
	for _, af := range autoFiles {
		if data, err := os.ReadFile(af["path"].(string)); err == nil {
			// S-03: rune 计数对齐 Python len()（CJK 偏差 3x）
			autoTotalChars += utf8.RuneCountInString(string(data))
		}
	}
	result["auto_memory"] = map[string]any{
		"files_count": len(autoFiles),
		"total_chars": autoTotalChars,
		"dir":         dirOrEmpty(memoryDir),
	}

	// 块 6 Python L381-387：external_memory（engine external/both）
	engine := commmem.GetMemoryEngine(cfg)
	if engine == "external" || engine == "both" {
		extCfg := commmem.GetExternalMemoryConfig(cfg)
		// Python: "provider": ext_cfg.get("provider", "unknown") if ext_cfg else "unknown"
		// Go GetExternalMemoryConfig 恒返回非 nil（等价 Python 恒非空 dict），
		// provider 原样返回（键存在时空串不触发 "unknown" 默认）
		result["external_memory"] = map[string]any{
			"provider": extCfg.Provider,
			"enabled":  commmem.IsExternalMemoryAllowed(cfg),
		}
	}

	return result
}

// HandleMemoryToggle 切换记忆开关并写回 config。
// Python: handle_memory_toggle (memory_rpc.py L392-444)
func HandleMemoryToggle(ctx context.Context, workspace, mode string, params map[string]any) map[string]any {
	// Python: key = params.get("key", ""); config = get_config()
	key, _ := params["key"].(string)
	cfg := loadConfigMap()

	// Python L402-412：全局禁止记忆开关
	if key == "memory_forbidden_enabled" {
		old := isForbiddenEnabled(cfg)
		newVal := !old
		// Python: update_memory_forbidden_enabled_in_config(new)
		if err := updateMemoryForbiddenEnabledInConfig(newVal); err != nil {
			// D6 决策：写回失败 log + new_value 置 old_value 如实上报未变更
			logger.Error(logComponent).Err(err).Msg("memory.toggle write forbidden enabled failed")
			newVal = old
		}
		return map[string]any{
			"key":           key,
			"old_value":     old,
			"new_value":     newVal,
			"mode_affected": "global",
			"needs_restart": true,
		}
	}

	// Python L414-424：模式级 memory_enabled
	if key == "memory_enabled" {
		old := commmem.IsMemoryEnabled(mode, cfg)
		newVal := !old
		if err := updateModeMemoryConfig(mode, "enabled", newVal); err != nil {
			logger.Error(logComponent).Err(err).Msg("memory.toggle write mode enabled failed")
			newVal = old
		}
		return map[string]any{
			"key":           key,
			"old_value":     old,
			"new_value":     newVal,
			"mode_affected": mode,
			"needs_restart": true,
		}
	}

	// Python L426-436：模式级 memory_proactive
	if key == "memory_proactive" {
		old := commmem.IsProactiveMemory(mode, cfg)
		newVal := !old
		if err := updateModeMemoryConfig(mode, "is_proactive", newVal); err != nil {
			logger.Error(logComponent).Err(err).Msg("memory.toggle write mode proactive failed")
			newVal = old
		}
		return map[string]any{
			"key":           key,
			"old_value":     old,
			"new_value":     newVal,
			"mode_affected": mode,
			"needs_restart": true,
		}
	}

	// Python L438-444：未知 key 兜底
	return map[string]any{
		"key":           key,
		"old_value":     false,
		"new_value":     false,
		"mode_affected": "",
		"needs_restart": false,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isForbiddenEnabled 判断禁止记忆定义是否启用。
// Python: _is_forbidden_enabled (memory_rpc.py L30-33)
func isForbiddenEnabled(cfg map[string]any) bool {
	// Python: mem_cfg = (config or {}).get("memory", {}) if isinstance(config, dict) else {}
	memCfg, _ := cfg["memory"].(map[string]any)
	if memCfg == nil {
		memCfg = map[string]any{}
	}
	// Python: forbidden = mem_cfg.get("forbidden_memory_definition", {})
	forbidden, _ := memCfg["forbidden_memory_definition"].(map[string]any)
	if forbidden == nil {
		forbidden = map[string]any{}
	}
	// Python: return bool(forbidden.get("enabled", False))
	// T-05: 非 bool 类型（如字符串 "false"/数字）时 Go 类型断言失败判 false，
	// Python truthy 判 true（含 "false"→true 的坑）。仅配置误写场景触发；
	// Go 的 fail-safe 方向更合理，有意不复刻 Python truthy 语义。
	enabled, _ := forbidden["enabled"].(bool)
	return enabled
}

// getCodingMemoryDir 返回 coding memory 目录路径。
// Python: _get_coding_memory_dir (L36-38)：project_dir 为空时用 "default"
func getCodingMemoryDir(workspace, projectDir string) string {
	projectName := "default"
	if projectDir != "" {
		projectName = filepath.Base(projectDir)
	}
	return filepath.Join(workspace, "coding_memory", projectName)
}

// getAllowedDirs 返回记忆编辑白名单目录。
// Python: _get_allowed_dirs (L41-51)：
// 白名单包含 memory/coding_memory/workspace·.uapclaw/~/.uapclaw/project_dir/project_dir·.uapclaw 等目录
func getAllowedDirs(workspace, projectDir string) []string {
	allowedDirs := []string{
		filepath.Join(workspace, "memory"),
		getCodingMemoryDir(workspace, projectDir),
		filepath.Join(workspace, ".uapclaw"),
		filepath.Join(utilpath.UserHomeDir(), ".uapclaw"),
	}
	if projectDir != "" {
		allowedDirs = append(allowedDirs, projectDir)
		allowedDirs = append(allowedDirs, filepath.Join(projectDir, ".uapclaw"))
	}
	// Python: return [os.path.normpath(d) for d in allowed_dirs]
	for i, d := range allowedDirs {
		allowedDirs[i] = filepath.Clean(d)
	}
	return allowedDirs
}

// isInAllowedDirs 判断绝对路径是否在白名单目录内。
// Python: _is_in_allowed_dirs (L54-67)：Windows 大小写不敏感
func isInAllowedDirs(absPath, workspace, projectDir string) bool {
	if runtime.GOOS == "windows" {
		// Python: abs_path_lower = abs_path.lower()
		lower := strings.ToLower(absPath)
		for _, dir := range getAllowedDirs(workspace, projectDir) {
			dirLower := strings.ToLower(dir)
			if lower == dirLower || strings.HasPrefix(lower, dirLower+string(os.PathSeparator)) {
				return true
			}
		}
		return false
	}
	for _, dir := range getAllowedDirs(workspace, projectDir) {
		if absPath == dir || strings.HasPrefix(absPath, dir+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

// validateEditPath 校验编辑路径合法性并返回解析后的绝对路径。
// Python: _validate_edit_path (L70-102)
func validateEditPath(rawPath, workspace string, projectDir string) (bool, string) {
	// Python: normalized = raw_path.replace("\\", "/")
	normalized := strings.ReplaceAll(rawPath, "\\", "/")
	// Python: expanded = os.path.expanduser(normalized)
	expanded := expandUser(normalized)

	var absPath string
	if filepath.IsAbs(expanded) {
		// Python: abs_path = os.path.normpath(expanded)
		absPath = filepath.Clean(expanded)
	} else {
		// Python: workspace_resolved = os.path.normpath(os.path.abspath(os.path.join(workspace, expanded)))
		absPath = filepath.Clean(absoluteJoin(workspace, expanded))
		if projectDir != "" {
			// Python: project_resolved 在白名单内时优先采用（L79-82）
			projectResolved := filepath.Clean(absoluteJoin(projectDir, expanded))
			if isInAllowedDirs(projectResolved, workspace, projectDir) {
				absPath = projectResolved
			}
		}
	}

	if isInAllowedDirs(absPath, workspace, projectDir) {
		return true, absPath
	}

	// 特例：UAPCLAWSWARM.md / UAPCLAWSWARM.local.md 且父目录是 workspace/project_dir 根
	// Python: basename in ("JIUWENSWARM.md", "JIUWENSWARM.local.md")（品牌映射）
	basename := filepath.Base(absPath)
	if basename == "UAPCLAWSWARM.md" || basename == "UAPCLAWSWARM.local.md" {
		parent := filepath.Dir(absPath)
		workspaceNorm := filepath.Clean(workspace)
		if runtime.GOOS == "windows" {
			// Python: parent.lower() == workspace_norm.lower()
			if strings.EqualFold(parent, workspaceNorm) {
				return true, absPath
			}
			if projectDir != "" && strings.EqualFold(parent, filepath.Clean(projectDir)) {
				return true, absPath
			}
		} else {
			if parent == workspaceNorm {
				return true, absPath
			}
			if projectDir != "" && parent == filepath.Clean(projectDir) {
				return true, absPath
			}
		}
	}

	return false, "Path not in allowed memory directories: " + rawPath
}

// classifyMemoryFile 分类记忆文件 kind。
// Python: _classify_memory_file (L105-118)
func classifyMemoryFile(path, workspace string) string {
	// Python: if "coding_memory" in path: return "coding"
	if strings.Contains(path, "coding_memory") {
		return "coding"
	}
	// Python: parts = Path(path).parts; if "memory" in parts: return "auto"
	if pathPartContains(path, "memory") {
		return "auto"
	}
	// Python: if ".jiuwen" in parts: return "project"（品牌映射 .uapclaw）
	if pathPartContains(path, ".uapclaw") {
		return "project"
	}
	// Python: if path.endswith(".local.md"): return "local"
	if strings.HasSuffix(path, ".local.md") {
		return "local"
	}
	// Python: jiuwen_dir = os.path.expanduser("~/.jiuwen"); if path.startswith(jiuwen_dir): return "user"
	jiuwenDir := filepath.Join(utilpath.UserHomeDir(), ".uapclaw")
	if strings.HasPrefix(path, jiuwenDir) {
		return "user"
	}
	return "project"
}

// pathPartContains 判断路径段中是否含指定目录名（对齐 Python Path.parts 成员判定）
func pathPartContains(path, part string) bool {
	for _, p := range strings.Split(filepath.ToSlash(path), "/") {
		if p == part {
			return true
		}
	}
	return false
}

// relativePath 返回相对 project_dir 或 workspace 的路径，都不在则返回原路径。
// Python: _relative_path (L121-156)
func relativePath(absPath, workspace, projectDir string) string {
	absPathNorm := filepath.Clean(absPath)
	if projectDir != "" {
		projectDirNorm := filepath.Clean(projectDir)
		if runtime.GOOS == "windows" {
			absLower := strings.ToLower(absPathNorm)
			projLower := strings.ToLower(projectDirNorm)
			if strings.HasPrefix(absLower, projLower+string(os.PathSeparator)) || absLower == projLower {
				// Python: return os.path.relpath(abs_path, project_dir)（原始 abs_path）
				if rel, err := filepath.Rel(projectDirNorm, absPath); err == nil {
					return rel
				}
			}
		} else {
			if strings.HasPrefix(absPathNorm, projectDirNorm+string(os.PathSeparator)) || absPathNorm == projectDirNorm {
				if rel, err := filepath.Rel(projectDirNorm, absPath); err == nil {
					return rel
				}
			}
		}
	}
	workspaceNorm := filepath.Clean(workspace)
	if runtime.GOOS == "windows" {
		absLower := strings.ToLower(absPathNorm)
		wsLower := strings.ToLower(workspaceNorm)
		if strings.HasPrefix(absLower, wsLower+string(os.PathSeparator)) || absLower == wsLower {
			if rel, err := filepath.Rel(workspaceNorm, absPath); err == nil {
				return rel
			}
		}
	} else {
		if strings.HasPrefix(absPathNorm, workspaceNorm+string(os.PathSeparator)) || absPathNorm == workspaceNorm {
			if rel, err := filepath.Rel(workspaceNorm, absPath); err == nil {
				return rel
			}
		}
	}
	// Python: return abs_path（原始路径兜底）
	return absPath
}

// expandUser 展开 ~ 前缀（对齐 Python os.path.expanduser 的 ~ 与 ~/ 前缀分支）。
// T-06: 不支持 ~user 形式（Python 会查 passwd 解析到该用户 home）；
// 调用方白名单目录不含其他用户 home 路径，实际不可达，无需支持。
func expandUser(path string) string {
	if path == "~" {
		return utilpath.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(utilpath.UserHomeDir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}

// absoluteJoin 拼接并转绝对路径（对齐 Python os.path.abspath：相对基路径时补 cwd）
func absoluteJoin(base, rel string) string {
	joined := filepath.Join(base, rel)
	if !filepath.IsAbs(joined) {
		if abs, err := filepath.Abs(joined); err == nil {
			return abs
		}
	}
	return joined
}

// safeStat 安全取文件 stat，失败返回 {size:0, mtime:0.0}。
// Python: _safe_stat (L159-164)
func safeStat(path string) map[string]any {
	if s, err := os.Stat(path); err == nil {
		// Python: {"size": s.st_size, "mtime": s.st_mtime}
		// M-02: st_mtime 是 float 秒（含亚秒精度），UnixMilli 是 int64 毫秒——
		// 量纲与类型均偏差（值大 1000 倍、丢亚秒），TUI 前端按秒渲染会错乱
		return map[string]any{"size": s.Size(), "mtime": float64(s.ModTime().UnixNano()) / 1e9}
	}
	// M-02: 失败分支同步 float 秒（原 int64(0) 类型不一致）
	return map[string]any{"size": int64(0), "mtime": float64(0)}
}

// countLines 统计文件行数（utf-8 容错读），失败或空文件返回 0。
// Python: _count_lines (L167-172)：逐行迭代计数（"l1\nl2\n"→2，"l1\nl2\nl3"→3，""→0）
// T-04: 行界对齐 Python 文本模式 universal newlines 语义——\n、\r\n、孤立 \r
// 都是行界；原 bufio.Scanner 只认 \n（孤立 \r 少计）且有 64KB 单行上限
// （超长行触发 token too long 后静默停止，行数少计无信号——detailed 分支
// 恰恰扫描 40000+ 字符大文件，长段落 md 单行可超 64KB）。
func countLines(path string) int {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return 0
	}
	// \n 与 \r 合计为行界数，再扣除 \r\n 的双重计数
	count := strings.Count(string(data), "\n") + strings.Count(string(data), "\r") - strings.Count(string(data), "\r\n")
	// 尾部无行界的最后一行：Python 逐行迭代同样计入（"l1\nl2" → 2 行）
	last := data[len(data)-1]
	if last != '\n' && last != '\r' {
		count++
	}
	return count
}

// scanMDFiles 扫描目录下 .md 文件（按文件名排序）。
// Python: _scan_md_files (L175-191)：os.scandir + sorted(key=name)
func scanMDFiles(dir, kind, workspace string, projectDir string) []map[string]any {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		// Python: if not os.path.isdir(directory): return []
		return nil
	}
	entries, err := os.ReadDir(dir) // ReadDir 天然按 name 排序
	if err != nil {
		return nil
	}
	var results []map[string]any
	for _, entry := range entries {
		// Python: if entry.is_file() and entry.name.endswith(".md")
		// T-07: entry.IsDir() 基于 lstat 不 follow symlink——指向目录的 .md symlink
		// 会被误纳入（后续读内容失败计 0 行）。Python is_file() follow symlink，
		// 故用 os.Stat + IsRegular() 二次判定对齐（symlink→文件保留，symlink→目录排除）。
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		if fi, err := os.Stat(full); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		stat := safeStat(full)
		results = append(results, map[string]any{
			"path":          full,
			"relative_path": relativePath(full, workspace, projectDir),
			"kind":          kind,
			"exists":        true,
			"size":          stat["size"],
			"mtime":         stat["mtime"],
			"lines":         countLines(full),
		})
	}
	return results
}

// isCodeMode 判断是否 code 模式（status 特判用）。
// Python: _is_code_mode (L194-195)：mode.startswith("code")
func isCodeMode(mode string) bool {
	return strings.HasPrefix(mode, "code")
}

// loadConfigMap 读取全局 config（不缓存，每次现读）。
// 对齐 Python get_config()（toggle/status 每次取最新）。
// T-08: Python get_config() = resolve_env_vars + _normalize_config（custom_headers
// JSON 解析 + web.send_file_allowed 默认）；Go Load 已含 env 解析，未应用 normalize。
// memoryrpc 仅消费 memory/modes 键，normalize 影响的 custom_headers/web 键无行为差异。
func loadConfigMap() map[string]any {
	cfg, err := config.New("")
	if err != nil {
		return map[string]any{}
	}
	data, err := cfg.Load()
	if err != nil || data == nil {
		return map[string]any{}
	}
	return data
}

// updateModeMemoryConfig 更新 modes.agent.{mode}.memory.{field} 并写回。
// Python: _update_mode_memory_config (memory_rpc.py L447-456)：
// round-trip 写回 modes→agent→{mode}→memory→{field}。
// T-11: config.yaml 不存在时 Go 自动创建写入（Python ruamel 抛 FileNotFoundError
// 由注册层转 ok=False）——Go 行为更健壮，有意增强，M-05 round-trip 实现延续该语义。
func updateModeMemoryConfig(mode, field string, value bool) error {
	cfg, err := config.New("")
	if err != nil {
		return err
	}
	if _, err := cfg.Load(); err != nil {
		return err
	}
	// M-05: SetRoundTrip（yaml.v3 Node 保注释）替代 Set（yaml.Marshal 全量重写丢注释）
	return cfg.SetRoundTrip("modes.agent."+mode+".memory."+field, value)
}

// updateMemoryForbiddenEnabledInConfig 更新 memory.forbidden_memory_definition.enabled 并写回。
// Python: update_memory_forbidden_enabled_in_config (common/config.py L1190-1198)。
// M-05: SetRoundTrip（yaml.v3 Node 保注释）替代 Set，对齐 Python ruamel round-trip
func updateMemoryForbiddenEnabledInConfig(value bool) error {
	cfg, err := config.New("")
	if err != nil {
		return err
	}
	if _, err := cfg.Load(); err != nil {
		return err
	}
	return cfg.SetRoundTrip("memory.forbidden_memory_definition.enabled", value)
}

// dirOrEmpty 目录存在返回原路径，否则空串（对齐 Python os.path.isdir 三元）
func dirOrEmpty(dir string) string {
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

// anyOr 取 map 值，键缺失或 nil 时返回默认值（对齐 Python dict.get(key, default)）
func anyOr(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}
