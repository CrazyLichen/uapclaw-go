package sysop_builder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// policyBuilder filesystem policy 构建器，封装可变状态。
// 对齐 Python: build_filesystem_policy 内部的闭包状态
type policyBuilder struct {
	allowFiles       []map[string]any
	allowDirs        []map[string]any
	bindMounts       []map[string]any
	uploadList       []map[string]string
	writablePaths    []string
	readWritePromote []string
	readOnlyPromote  []string
}

// ──────────────────────────── 常量 ────────────────────────────

// envSandboxProjectDir 沙箱项目目录环境变量。
// 对齐 Python: JIUSWARM_SANDBOX_PROJECT_DIR
const envSandboxProjectDir = "UAPCLAW_SANDBOX_PROJECT_DIR"

// ──────────────────────────── 全局变量 ────────────────────────────

// intrinsicRWFilePathFuncs 固有 rw 文件路径函数列表。
// 对齐 Python: _INTRINSIC_RW_FILE_PATH_FUNCS
var intrinsicRWFilePathFuncs = []func() string{
	workspace.DeepAgentAgentMDPath,
	workspace.DeepAgentHeartbeatPath,
	workspace.DeepAgentIdentityMDPath,
	workspace.DeepAgentSoulMDPath,
	workspace.DeepAgentUserMDPath,
}

// intrinsicROFilePathFuncs 固有 ro 文件路径函数列表。
// 对齐 Python: _INTRINSIC_RO_FILE_PATH_FUNCS
var intrinsicROFilePathFuncs = []func() string{
	workspace.ConfigFile,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildFilesystemPolicy 组装沙箱 filesystem policy。
// 对齐 Python: build_filesystem_policy(files_runtime, *, project_dir=None, is_code_agent=False)
//
// 参数：
//   - filesRuntime: config.yaml::sandbox.files 字典，含 allow/deny 列表
//   - projectDir: 工程目录覆盖值（仅 isCodeAgent 时消费）
//   - isCodeAgent: 是否 code-agent 形态
//
// 返回：
//   - policyDict: {"filesystem_policy": {"files": [], "directories": [], "bind_mounts": [], "read_write": [], "read_only": []}}
//   - uploadList: 当前始终为空列表（mount 模式下走 bind_mounts）
//   - error: files.allow/deny 中 path 不存在时返回 os.ErrNotExist
func BuildFilesystemPolicy(
	filesRuntime map[string]any,
	projectDir string,
	isCodeAgent bool,
) (map[string]any, []map[string]string, error) {
	if filesRuntime == nil {
		filesRuntime = make(map[string]any)
	}

	b := &policyBuilder{
		allowFiles:       make([]map[string]any, 0),
		allowDirs:        make([]map[string]any, 0),
		bindMounts:       make([]map[string]any, 0),
		uploadList:       make([]map[string]string, 0),
		writablePaths:    make([]string, 0),
		readWritePromote: make([]string, 0),
		readOnlyPromote:  make([]string, 0),
	}

	// 收集固有目标
	intrinsicFiles, intrinsicDirs, intrinsicROFiles := collectIntrinsicTargets()

	// 注册固有 rw 文件
	for _, path := range intrinsicFiles {
		b.recordRWBind(path, path, false, "0666")
	}

	// 注册固有 rw 目录
	for _, path := range intrinsicDirs {
		b.recordRWBind(path, path, true, "0777")
	}

	// 注册固有 ro 文件（对齐 Python: _record_ro_resource_bind）
	for _, path := range intrinsicROFiles {
		b.recordROResourceBind(path, path)
	}

	// 注册 agent_skills 目录
	agentSkills := resolveAgentSkillsDir()
	if agentSkills != "" {
		b.recordRWBind(agentSkills, agentSkills, true, "0777")
	}

	// 仅 code-agent 才挂用户工程目录
	// 对齐 Python: if is_code_agent: resolved_project = _resolve_project_dir(project_dir)
	if isCodeAgent {
		resolvedProject := resolveProjectDir(projectDir)
		if resolvedProject != "" {
			b.recordRWBind(resolvedProject, resolvedProject, true, "0777")
		}
	}

	// 处理 files.allow
	// 对齐 Python: for entry in files_runtime.get("allow") or []:
	allowEntries := filesRuntime["allow"]
	if allowList, ok := toSlice(allowEntries); ok {
		for _, entry := range allowList {
			normalized := normalizeFSEntry(entry, "0666")
			if normalized == nil {
				continue
			}
			path, _ := normalized["path"].(string)
			path = strings.TrimRight(path, "/")
			if path == "" {
				path = "/"
			}
			normalized["path"] = path

			// 校验 host 上存在并判断是否目录
			info, statErr := os.Stat(path)
			if statErr != nil {
				return nil, nil, fmt.Errorf("沙箱 files.allow 路径在主机上不存在: %q", path)
			}
			isDir := info.IsDir()
			permStr := "0666"
			if p, ok := normalized["permissions"].(string); ok && p != "" {
				permStr = p
			}
			b.recordRWBind(path, path, isDir, permStr)

			// 读写提升
			if !containsString(b.readWritePromote, path) {
				b.readWritePromote = append(b.readWritePromote, path)
			}
		}
	}

	// 处理 files.deny
	// 对齐 Python: for entry in files_runtime.get("deny") or []:
	denyEntries := filesRuntime["deny"]
	if denyList, ok := toSlice(denyEntries); ok {
		for _, entry := range denyList {
			normalized := normalizeFSEntry(entry, "0000")
			if normalized == nil {
				continue
			}
			path, _ := normalized["path"].(string)
			path = strings.TrimRight(path, "/")
			if path == "" {
				path = "/"
			}
			normalized["path"] = path

			// 校验 host 上存在
			if _, statErr := os.Stat(path); statErr != nil {
				return nil, nil, fmt.Errorf("沙箱 files.deny 路径在主机上不存在: %q", path)
			}

			b.recordUserDenyBind(path, path)
		}
	}

	policy, uploadList := b.build()
	return policy, uploadList, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordRWBind 注册 rw bind mount。
// 对齐 Python: _record_rw_bind(host_path, sandbox_path, *, is_dir, permissions)
func (b *policyBuilder) recordRWBind(hostPath, sandboxPath string, isDir bool, permissions string) {
	b.bindMounts = append(b.bindMounts, map[string]any{
		"host_path":    hostPath,
		"sandbox_path": sandboxPath,
		"mode":         "rw",
	})
	if !containsString(b.writablePaths, sandboxPath) {
		b.writablePaths = append(b.writablePaths, sandboxPath)
	}
}

// recordUserDenyBind 注册 deny_write bind：bind_mount mode=rw + read_only patch。
// 对齐 Python: _record_user_deny_bind(host_path, sandbox_path)
func (b *policyBuilder) recordUserDenyBind(hostPath, sandboxPath string) {
	b.bindMounts = append(b.bindMounts, map[string]any{
		"host_path":    hostPath,
		"sandbox_path": sandboxPath,
		"mode":         "rw",
	})
	if !containsString(b.readOnlyPromote, sandboxPath) {
		b.readOnlyPromote = append(b.readOnlyPromote, sandboxPath)
	}
}

// recordROResourceBind 注册内置只读资源 bind（mode=ro + read_only promote）。
// 对齐 Python: _record_ro_resource_bind(host_path, sandbox_path)
func (b *policyBuilder) recordROResourceBind(hostPath, sandboxPath string) {
	b.bindMounts = append(b.bindMounts, map[string]any{
		"host_path":    hostPath,
		"sandbox_path": sandboxPath,
		"mode":         "ro",
	})
	if !containsString(b.readOnlyPromote, sandboxPath) {
		b.readOnlyPromote = append(b.readOnlyPromote, sandboxPath)
	}
}

// build 组装最终 filesystem policy dict。
// 对齐 Python: build_filesystem_policy 末尾组装逻辑
func (b *policyBuilder) build() (map[string]any, []map[string]string) {
	fsPolicy := map[string]any{
		"files":       b.allowFiles,
		"directories": b.allowDirs,
	}
	if len(b.bindMounts) > 0 {
		fsPolicy["bind_mounts"] = b.bindMounts
	}
	if len(b.readWritePromote) > 0 {
		fsPolicy["read_write"] = b.readWritePromote
	}
	if len(b.readOnlyPromote) > 0 {
		fsPolicy["read_only"] = b.readOnlyPromote
	}

	return map[string]any{"filesystem_policy": fsPolicy}, b.uploadList
}

// collectIntrinsicTargets 收集 deep agent 固有路径，分 rw/ro 两类返回。
// 对齐 Python: _collect_intrinsic_targets() → (rw_files, rw_dirs, ro_files)
func collectIntrinsicTargets() (rwFiles, rwDirs, roFiles []string) {
	rwFiles = make([]string, 0)
	rwDirs = make([]string, 0)
	roFiles = make([]string, 0)

	// 固有 rw 文件（AGENT.md / HEARTBEAT.md / IDENTITY.md / SOUL.md / USER.md）
	for _, fn := range intrinsicRWFilePathFuncs {
		raw := fn()
		if raw == "" {
			continue
		}
		if ensureIntrinsicFile(raw) {
			rwFiles = append(rwFiles, raw)
		}
	}

	// daily_memory 目录（仅 host 存在时加入，不自动创建）
	dailyMemoryPath := filepath.Join(workspace.AgentMemoryDir(), "daily_memory")
	if info, err := os.Stat(dailyMemoryPath); err == nil && info.IsDir() {
		rwDirs = append(rwDirs, dailyMemoryPath)
	} else {
		logger.Info(logComponent).
			Str("path", dailyMemoryPath).
			Msg("daily_memory 不存在于 host，跳过沙箱 bind 列表")
	}

	// 固有 ro 文件（config.yaml）
	for _, fn := range intrinsicROFilePathFuncs {
		raw := fn()
		if raw == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(raw)
		if err != nil {
			logger.Debug(logComponent).
				Str("path", raw).
				Err(err).
				Msg("固有 ro 文件路径解析失败，跳过")
			continue
		}
		if _, statErr := os.Stat(resolved); statErr != nil {
			logger.Debug(logComponent).
				Str("path", resolved).
				Msg("固有 ro 文件不存在于 host，跳过沙箱 bind 列表")
			continue
		}
		roFiles = append(roFiles, resolved)
	}

	return rwFiles, rwDirs, roFiles
}

// resolveProjectDir 解析挂入沙箱的主写入根目录。
// 对齐 Python: _resolve_project_dir(override)
// 优先级：override → UAPCLAW_SANDBOX_PROJECT_DIR 环境变量 → os.Getwd()
// 拒绝挂载文件系统根 "/"。
func resolveProjectDir(override string) string {
	candidates := make([]string, 0, 3)
	if override != "" {
		candidates = append(candidates, override)
	}
	if envVal := os.Getenv(envSandboxProjectDir); envVal != "" {
		candidates = append(candidates, envVal)
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		candidates = append(candidates, cwd)
	}

	for _, cand := range candidates {
		resolved, err := resolveSymlinkAbs(cand)
		if err != nil {
			logger.Debug(logComponent).
				Str("candidate", cand).
				Err(err).
				Msg("project_dir 候选路径解析失败")
			continue
		}
		info, statErr := os.Stat(resolved)
		if statErr != nil || !info.IsDir() {
			logger.Debug(logComponent).
				Str("candidate", resolved).
				Msg("project_dir 候选不是目录，跳过")
			continue
		}
		// 拒绝文件系统根
		if resolved == "/" || resolved == filepath.VolumeName(resolved)+string(os.PathSeparator) {
			logger.Warn(logComponent).
				Str("candidate", resolved).
				Msg("拒绝将文件系统根作为 rw project 目录")
			return ""
		}
		return resolved
	}
	return ""
}

// resolveAgentSkillsDir 解析内置技能目录。
// 对齐 Python: _resolve_agent_skills_dir()
func resolveAgentSkillsDir() string {
	raw := workspace.AgentSkillsDir()
	if raw == "" {
		return ""
	}
	resolved, err := filepath.Abs(raw)
	if err != nil {
		logger.Debug(logComponent).
			Str("path", raw).
			Err(err).
			Msg("内置技能目录路径解析失败")
		return ""
	}
	info, statErr := os.Stat(resolved)
	if statErr != nil || !info.IsDir() {
		logger.Debug(logComponent).
			Str("path", resolved).
			Msg("内置技能目录不存在，跳过")
		return ""
	}
	return resolved
}

// ensureIntrinsicFile 确保固有文件存在，不存在则 touch 空文件。
// 对齐 Python: _ensure_intrinsic_file(path) → bool
func ensureIntrinsicFile(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	// 创建父目录
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("确保固有文件失败：创建父目录失败")
		return false
	}
	// touch 空文件
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("确保固有文件失败：创建文件失败")
		return false
	}
	_ = f.Close()
	logger.Info(logComponent).
		Str("path", path).
		Msg("创建空固有文件")
	return true
}

// normalizeFSEntry 归一化 {path, permissions} 项，接受 string 或 map[string]any。
// 对齐 Python: _normalize_fs_entry(entry, default_permissions)
func normalizeFSEntry(entry any, defaultPermissions string) map[string]any {
	if entry == nil {
		return nil
	}
	switch v := entry.(type) {
	case string:
		path := strings.TrimSpace(v)
		if path == "" {
			return nil
		}
		return map[string]any{"path": path, "permissions": defaultPermissions}
	case map[string]any:
		rawPath := v["path"]
		if rawPath == nil {
			return nil
		}
		path := strings.TrimSpace(fmt.Sprintf("%v", rawPath))
		if path == "" {
			return nil
		}
		perm := v["permissions"]
		permStr := defaultPermissions
		if perm != nil {
			if s, ok := perm.(string); ok && s != "" {
				permStr = s
			}
		}
		return map[string]any{"path": path, "permissions": permStr}
	default:
		return nil
	}
}

// containsString 检查字符串切片中是否包含目标字符串。
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}

// toSlice 将 any 转为 []any，支持 []any 和 nil。
func toSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	switch s := v.(type) {
	case []any:
		return s, true
	case []string:
		result := make([]any, len(s))
		for i, item := range s {
			result[i] = item
		}
		return result, true
	default:
		return nil, false
	}
}

// expandHome 展开 ~ 为用户 home 目录。
// 对齐 Python: Path.expanduser()
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") && path != "~" {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[1:]), nil
}

// resolveSymlinkAbs 解析路径：展开 ~ → 绝对路径 → 解析符号链接。
// 对齐 Python: Path.expanduser().resolve()
// EvalSymlinks 失败时 fallback 到 Abs 结果。
func resolveSymlinkAbs(path string) (string, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// EvalSymlinks 失败时 fallback 到 Abs 结果
		return abs, nil
	}
	return resolved, nil
}
