// Package common - projectless_workspace.go 提供无项目任务工作空间的
// 跨平台分配与目录注册。
//
// 对齐 Python: jiuwenswarm/common/projectless_workspace.py（517 行）。
// 磁盘布局：dated `<YYYY-MM-DD>/chat-<n>/` 目录、`.session_id` 碰撞标记、
// `metadata.json` sidecar、`<safe_session>.json -> root` 绑定注册。
//
// 已移植（15.16c 团队产物分配的消费面）：
// AllocateTaskWorkspace + registry/metadata/marker 全部私有助手 + Slugify。
// ⤴️ 单 agent 章节回填（本批无消费方且依赖 Go 不存在的 Documents/enterprise
// 面，登记偏差 D-15.16-c7）：
// get_projectless_tasks_dir / _get_documents_dir / _get_windows_documents_dir /
// _get_linux_documents_dir / get_projectless_task_workspace /
// get_registered_projectless_task_root / _registry_root 的私有默认分支
// （GetAgentWorkspaceDir 等价已存在，随回填接线）。
package common

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// tasksDirEnv 任务根目录的环境变量覆盖。
	// Python: _TASKS_DIR_ENV (projectless_workspace.py:32)
	tasksDirEnv = "JIUWENSWARM_TASKS_DIR"
	// taskRegistryDirEnv 任务绑定注册目录的环境变量覆盖。
	// Python: _TASK_REGISTRY_DIR_ENV (projectless_workspace.py:33)
	taskRegistryDirEnv = "JIUWENSWARM_TASK_REGISTRY_DIR"
	// registrySubdir 私有 agent 工作空间内的默认注册子目录。
	// Python: _REGISTRY_SUBDIR (projectless_workspace.py:34)
	registrySubdir = ".projectless_tasks"
	// legacyRegistryDir 旧版注册目录（团队侧由 team_artifacts 收敛）。
	// Python: _LEGACY_REGISTRY_DIR (projectless_workspace.py:35)
	legacyRegistryDir = ".jiuwenswarm"
	// chatDirPrefix chat 目录名前缀。
	// Python: _CHAT_DIR_PREFIX (projectless_workspace.py:36)
	chatDirPrefix = "chat"
	// metadataFilename 任务元数据 sidecar 文件名。
	// Python: _METADATA_FILENAME (projectless_workspace.py:37)
	metadataFilename = "metadata.json"
	// maxTaskNameLength slug 化后的最大长度。
	// Python: _MAX_TASK_NAME_LENGTH (projectless_workspace.py:38)
	maxTaskNameLength = 48
)

// windowsReservedNames Windows 保留设备名集合。
// Python: _WINDOWS_RESERVED_NAMES (projectless_workspace.py:39-71)
var windowsReservedNames = map[string]bool{
	"AUX": true, "CLOCK$": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"CON":  true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	"NUL": true, "PRN": true,
}

// slugNonWordChars Python `re.sub(r"[^\w.-]+", "-", text, flags=re.UNICODE)`
// 的 Go 等价模式。\w 在 Python 3 的 UNICODE 语义 ≈ Unicode 字母/数字/下划线
// （组合记号 \pM 亦被 \w 匹配，此处一并纳入以最小化偏差）。
var slugNonWordChars = regexp.MustCompile(`[^\pL\pN\pM_.-]+`)

// ──────────────────────────── 结构体 ────────────────────────────

// ProjectlessTaskWorkspace 暴露给一个无项目任务会话的文件系统位置。
// Python: ProjectlessTaskWorkspace (projectless_workspace.py:74-84)
type ProjectlessTaskWorkspace struct {
	// RootDir 任务根目录（dated chat-<n>）
	RootDir string
	// WorkDir 成员临时工作目录
	WorkDir string
	// OutputsDir 共享最终产物目录
	OutputsDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Slugify 把自由文本折叠为稳定、文件系统安全的名字片段。
// Python: slugify (projectless_workspace.py:348-374)
//
// reserved_prefix：结果撞 Windows 保留设备名（CON/NUL…）时的前缀防撞守卫，
// 正常路径永不出现。
func Slugify(value string, fallback string, reservedPrefix string) string {
	// Python: unicodedata.normalize("NFKC", str(value or "")).strip()
	text := strings.TrimSpace(norm.NFKC.String(value))
	// Python: re.sub(r"[^\w.-]+", "-", text, flags=re.UNICODE)
	text = slugNonWordChars.ReplaceAllString(text, "-")
	// Python: text.strip(" .-_")
	text = strings.Trim(text, " .-_")
	// Python: text[:_MAX_TASK_NAME_LENGTH].rstrip(" .-_")
	if len([]rune(text)) > maxTaskNameLength {
		text = string([]rune(text)[:maxTaskNameLength])
	}
	text = strings.TrimRight(text, " .-_")
	if text == "" {
		return fallback
	}
	// Python: text.split(".", 1)[0].upper() in _WINDOWS_RESERVED_NAMES
	if windowsReservedNames[strings.ToUpper(strings.SplitN(text, ".", 2)[0])] {
		return reservedPrefix + "-" + text
	}
	return text
}

// AllocateTaskWorkspace 在 tasks_dir 下分配或复用 `chat-<n>` 任务根目录。
// 单 agent 无项目运行（registry_dir=nil 回落私有 agent 工作空间注册）与
// 团队产物分配（tasks_dir=`<team-workspace>/artifacts`、registry_dir=其
// `.team_artifacts` 子目录，重定位的团队工作空间保持自包含）共用。
// Python: allocate_task_workspace (projectless_workspace.py:197-260)
func AllocateTaskWorkspace(tasksDir string, registryDir string, sessionID string, taskName string) (string, error) {
	tasksDir = resolvePath(tasksDir)
	safeSession := Slugify(sessionID, "default", "task")
	registeredRoot, err := readRegisteredRoot(tasksDir, safeSession, registryDir)
	if err != nil {
		return "", err
	}
	if registeredRoot == "" {
		taskDate := getSessionTaskDate(sessionID)
		if taskDate == "" {
			taskDate = localToday()
		}
		registeredRoot, err = allocateTaskRoot(tasksDir, taskDate, safeSession)
		if err != nil {
			return "", err
		}
		if err = writeRegisteredRoot(safeSession, registeredRoot, registryDir); err != nil {
			return "", err
		}
		if err = writeTaskMetadata(registeredRoot, sessionID, taskName); err != nil {
			return "", err
		}
	} else if !fileExists(filepath.Join(registeredRoot, metadataFilename)) {
		// 为更早版本创建的工作空间回填元数据。
		// Python: registered_root 存在但缺 metadata.json 分支（:256-263）
		if err = writeTaskMetadata(registeredRoot, sessionID, taskName); err != nil {
			return "", err
		}
	}
	return registeredRoot, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolvePath 等价 Python `Path(p).resolve()`：Abs + 符号链接消解；
// 路径不存在时（EvalSymlinks 失败）仅返回 Abs 结果。
func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// localToday 返回宿主本地时区的今天日期。
// Python: _local_today (projectless_workspace.py:262-265)
func localToday() string {
	return time.Now().Format("2006-01-02")
}

// getSessionTaskDate 读取会话不可变的创建日期用于任务目录绑定。
// session_metadata 以 UTC Unix 时间戳存 created_at；任务目录是用户可见的，
// 时间戳先转本地时区再取 YYYY-MM-DD。文件系统创建时间仅作无可读
// metadata.json 的旧会话兼容回退。
// Python: _get_session_task_date (projectless_workspace.py:267-330)
func getSessionTaskDate(sessionID string) string {
	rawSession := strings.TrimSpace(sessionID)
	if rawSession == "" {
		return ""
	}
	// Python: "."/".."、路径分隔符、绝对路径、name != raw 五条拒绝分支
	if rawSession == "." || rawSession == ".." {
		return ""
	}
	if strings.ContainsAny(rawSession, "/\\") {
		return ""
	}
	if filepath.IsAbs(rawSession) {
		return ""
	}
	if filepath.Base(rawSession) != rawSession {
		return ""
	}
	sessionDir := filepath.Join(workspace.AgentSessionsDir(), rawSession)
	var timestamp float64
	metadata, err := os.ReadFile(filepath.Join(sessionDir, metadataFilename))
	if err == nil {
		var record map[string]any
		if json.Unmarshal(metadata, &record) == nil {
			if v, ok := record["created_at"]; ok {
				timestamp = coerceTimestamp(v)
			}
		}
	}
	if timestamp == 0 {
		info, err := os.Stat(sessionDir)
		if err != nil {
			return ""
		}
		// 偏差 D-15.16-c7-a：Python 读 st_ctime；Go 无可移植创建时间，
		// 回退 ModTime（仅影响旧会话无 metadata 的兼容分支的日期目录归位）。
		timestamp = float64(info.ModTime().Unix())
	}
	if timestamp == 0 {
		return ""
	}
	return time.Unix(int64(timestamp), 0).Format("2006-01-02")
}

// coerceTimestamp 把数值或 ISO-8601 时间转 Unix 秒。
// Python: _coerce_timestamp (projectless_workspace.py:332-345)
func coerceTimestamp(value any) float64 {
	switch v := value.(type) {
	case bool:
		return 0
	case int:
		return mustPositive(float64(v))
	case int64:
		return mustPositive(float64(v))
	case float64:
		return mustPositive(v)
	case string:
		raw := strings.TrimSpace(v)
		if raw == "" {
			return 0
		}
		// 先按纯数值尝试（Python: float(raw)）
		if f, err := parsePythonFloat(raw); err == nil {
			return mustPositive(f)
		}
		// 再按 ISO-8601 尝试（Python: fromisoformat，Z 后缀先替换）
		normalized := strings.Replace(raw, "Z", "+00:00", 1)
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999", "2006-01-02T15:04:05", "2006-01-02"} {
			if t, err := time.Parse(layout, normalized); err == nil {
				return mustPositive(float64(t.Unix()))
			}
		}
		// Python: naive 时间戳补本地时区——time.Parse 无时区布局返回 UTC 零值，
		// 需按本地时区重新解释（偏差 D-15.16-c7-b：Go 无 fromisoformat 等价，
		// 枚举常见布局近似）。
		if t, err := time.ParseInLocation("2006-01-02T15:04:05.999999", normalized, time.Local); err == nil {
			return mustPositive(float64(t.Unix()))
		}
		return 0
	default:
		return 0
	}
}

// mustPositive Python `timestamp if timestamp > 0 and math.isfinite(timestamp) else 0`。
func mustPositive(f float64) float64 {
	if f > 0 && !math.IsInf(f, 0) && !math.IsNaN(f) {
		return f
	}
	return 0
}

// parsePythonFloat 等价 Python float(str)：全串解析（部分匹配视为失败），
// 容忍前后空白与科学计数法。
func parsePythonFloat(raw string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(raw), 64)
}

// allocateTaskRoot 在 date_dir 下分配或复用 chat-<n> 目录。
// Python: _allocate_task_root (projectless_workspace.py:376-394)
func allocateTaskRoot(tasksDir, taskDate, safeSession string) (string, error) {
	dateDir := filepath.Join(tasksDir, taskDate)
	if err := os.MkdirAll(dateDir, 0o755); err != nil {
		return "", err
	}
	for index := 1; ; index++ {
		candidate := filepath.Join(dateDir, fmt.Sprintf("%s-%d", chatDirPrefix, index))
		if fileExists(candidate) {
			// 已存在：碰撞标记与本会话一致则复用，否则继续取号。
			if marker, _ := readSessionMarker(candidate); marker == safeSession && marker != "" {
				return resolvePath(candidate), nil
			}
			continue
		}
		if err := os.Mkdir(candidate, 0o755); err != nil {
			if os.IsExist(err) {
				// Python: FileExistsError → continue（并发取号重试）
				continue
			}
			return "", err
		}
		writeSessionMarker(candidate, safeSession)
		return resolvePath(candidate), nil
	}
}

// registryRoot 返回无项目任务绑定的私有注册目录。
// Python: _registry_root (projectless_workspace.py:396-411)
// 偏差 D-15.16-c7：Python 默认分支 get_agent_workspace_dir()/.projectless_tasks
// 的接线 ⤴️ 单 agent 章节（本批消费方 team_artifacts 恒传 registry_dir 显式值）。
func registryRoot() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(taskRegistryDirEnv)); configured != "" {
		return resolvePath(configured), nil
	}
	return filepath.Join(workspace.AgentWorkspaceDir(), registrySubdir), nil
}

// resolveRegistryDir 返回注册目录，显式值优先，否则回落私有默认。
// Python: _resolve_registry_dir (projectless_workspace.py:413-418)
func resolveRegistryDir(registryDir string) (string, error) {
	if registryDir != "" {
		return resolvePath(registryDir), nil
	}
	return registryRoot()
}

// registryPath 返回 `<safe_session>.json` 注册文件路径。
// Python: _registry_path (projectless_workspace.py:420-422)
func registryPath(safeSession, registryDir string) (string, error) {
	dir, err := resolveRegistryDir(registryDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, safeSession+".json"), nil
}

// legacyRegistryPath 返回旧版注册文件路径。
// Python: _legacy_registry_path (projectless_workspace.py:424-426)
func legacyRegistryPath(tasksDir, safeSession string) string {
	return filepath.Join(tasksDir, legacyRegistryDir, safeSession+".json")
}

// readRegisteredRoot 从注册文件读取会话绑定的任务根目录。
// Python: _read_registered_root (projectless_workspace.py:428-451)
// 主路径不存在时跳过主记录、仅探旧路径；旧路径命中且非主路径时回写迁移。
func readRegisteredRoot(tasksDir, safeSession, registryDir string) (string, error) {
	primaryPath, err := registryPath(safeSession, registryDir)
	if err != nil {
		return "", err
	}
	legacyPath := legacyRegistryPath(tasksDir, safeSession)
	for _, path := range []string{primaryPath, legacyPath} {
		if path == primaryPath && !fileExists(path) {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record map[string]any
		if json.Unmarshal(raw, &record) != nil {
			continue
		}
		rawRoot, _ := record["root_dir"].(string)
		if strings.TrimSpace(rawRoot) == "" {
			continue
		}
		root := resolvePath(rawRoot)
		if !isRelative(root, resolvePath(tasksDir)) || !isDir(root) {
			continue
		}
		if path != primaryPath {
			// 旧路径命中：迁移回写主注册。
			if err := writeRegisteredRoot(safeSession, root, registryDir); err != nil {
				return "", err
			}
		}
		return root, nil
	}
	return "", nil
}

// writeRegisteredRoot 原子写 `<safe_session>.json -> root` 绑定。
// 唯一兄弟临时文件避免同会话并发轮次在 os.replace 前互相踩踏。
// Python: _write_registered_root (projectless_workspace.py:453-470)
func writeRegisteredRoot(safeSession, root, registryDir string) error {
	resolvedDir, err := resolveRegistryDir(registryDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(resolvedDir, 0o755); err != nil {
		return err
	}
	path, err := registryPath(safeSession, registryDir)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"root_dir": root})
	if err != nil {
		return err
	}
	return atomicWrite(path, payload, false)
}

// writeTaskMetadata 把 query/title 与纯 ASCII 工作空间名分离持久化。
// Python: _write_task_metadata (projectless_workspace.py:472-494)
func writeTaskMetadata(root, sessionID, taskName string) error {
	now := time.Now().Format("2006-01-02T15:04:05.000000-07:00")
	query := taskName
	metadata := map[string]string{
		"chat_id":    filepath.Base(root),
		"session_id": sessionID,
		"title":      query,
		"query":      query,
		"created_at": now,
	}
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(root, metadataFilename), payload, true)
}

// atomicWrite 唯一临时文件 + os.replace 原子落盘；finally 清理临时文件
// （Python: temporary.unlink(missing_ok=True)）。
// ensureTrailingNewline 对齐 Python json.dumps(...) + "\n"（仅 metadata 用）。
func atomicWrite(path string, payload []byte, ensureTrailingNewline bool) error {
	if ensureTrailingNewline {
		payload = append(payload, '\n')
	}
	suffix, err := randomHex()
	if err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s.%s.tmp", filepath.Base(path), suffix))
	defer func() {
		if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
			// missing_ok 语义：不存在即目标已替换成功，静默。
			_ = err
		}
	}()
	if err := os.WriteFile(temporary, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// randomHex 生成 16 字节随机 hex（Python uuid.uuid4().hex 等价长度）。
func randomHex() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// sessionMarkerPath 返回 `.session_id` 碰撞标记路径。
// Python: _session_marker_path (projectless_workspace.py:496-498)
func sessionMarkerPath(root string) string {
	return filepath.Join(root, ".session_id")
}

// readSessionMarker 读取碰撞标记；不存在或为空返回空串。
// Python: _read_session_marker (projectless_workspace.py:500-507)
func readSessionMarker(root string) (string, error) {
	raw, err := os.ReadFile(sessionMarkerPath(root))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// writeSessionMarker 写碰撞标记；失败静默（注册表才是事实源，标记仅用于
// 分配期的同名歧义消解）。
// Python: _write_session_marker (projectless_workspace.py:509-517)
func writeSessionMarker(root, safeSession string) {
	_ = os.WriteFile(sessionMarkerPath(root), []byte(safeSession), 0o644)
}

// fileExists 路径存在判定（文件或目录）。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isDir 路径存在且为目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isRelative 等价 Python `Path.is_relative_to`：root 是否位于 base 之下。
func isRelative(root, base string) bool {
	rel, err := filepath.Rel(base, root)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "."
}
