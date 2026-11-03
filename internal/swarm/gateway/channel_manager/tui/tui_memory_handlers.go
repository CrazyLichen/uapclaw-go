package tui

// ──────────────────────────── 常量 ────────────────────────────

const (
	// memoryRPCErrorCode 记忆 RPC 内部错误码，对齐 Python code="INTERNAL_ERROR"
	memoryRPCErrorCode = "INTERNAL_ERROR"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// memoryMode 取请求 mode，缺省 "plan"。
//
// Python: mode = params.get("mode", "plan")
func memoryMode(params map[string]any) string {
	if mode, ok := params["mode"].(string); ok && mode != "" {
		return mode
	}
	return "plan"
}

// resolveProjectDir 解析项目目录。
//
// Python: _resolve_project_dir —— params.project_dir → params.trusted_dirs[0] → params.cwd
func resolveProjectDir(params map[string]any) string {
	if projectDir, ok := params["project_dir"].(string); ok && projectDir != "" {
		return projectDir
	}
	if dirs, ok := params["trusted_dirs"].([]any); ok && len(dirs) > 0 {
		if projectDir, ok := dirs[0].(string); ok && projectDir != "" {
			return projectDir
		}
	}
	if cwd, ok := params["cwd"].(string); ok && cwd != "" {
		return cwd
	}
	return ""
}

// withProjectDir 返回注入 project_dir 后的 params 副本；未命中时原样返回。
//
// Python: if project_dir: params = {**params, "project_dir": project_dir}
func withProjectDir(params map[string]any, projectDir string) map[string]any {
	if projectDir == "" {
		return params
	}
	merged := make(map[string]any, len(params)+1)
	for key, value := range params {
		merged[key] = value
	}
	merged["project_dir"] = projectDir
	return merged
}
