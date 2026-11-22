// Python 源：jiuwenswarm/common/utils.py:1806-1841（G2 缺口，用户 12-07 拍板本批落）。
// 无项目会话的默认任务工作区：<agent-workspace>/projects/<清洗后 session_id>。
// session_id 作目录名，需路径安全化清洗（utils.py:1834 正则）。
package workspace

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// unsafeSessionIDChars 目录名安全化：仅保留字母数字与 _ . -，其余替换为 _。
// Python: utils.py:1834 re.sub(r"[^A-Za-z0-9_.-]+", "_", raw_session)
var unsafeSessionIDChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// DefaultProjectWorkspaceDir 无项目绑定时用户任务产物的默认 cwd/workspace 边界。
// 记忆/技能/todo/会话等 agent 自有数据仍在 AgentWorkspaceDir() 直下。
// Python: utils.py:1806-1813 get_default_project_workspace_dir。
func DefaultProjectWorkspaceDir() string {
	return filepath.Join(AgentWorkspaceDir(), "projects")
}

// DefaultProjectSessionWorkspaceDir 单个会话的无项目任务工作区。
// Python: utils.py:1816-1841 get_default_project_session_workspace_dir。
// 步骤对齐：session_id 缺失（适配器早期初始化）→ 返回共享 projects 根（不建目录）；
// 清洗后为空 → 同上；否则建 <projects>/<safe_sid> 并返回。
func DefaultProjectSessionWorkspaceDir(sessionID string) (string, error) {
	base := DefaultProjectWorkspaceDir()
	rawSession := strings.TrimSpace(sessionID)
	if rawSession == "" {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return "", err
		}
		return base, nil
	}
	safeSession := strings.Trim(unsafeSessionIDChars.ReplaceAllString(rawSession, "_"), "._-")
	if safeSession == "" {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return "", err
		}
		return base, nil
	}
	workspacePath := filepath.Join(base, safeSession)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return "", err
	}
	return workspacePath, nil
}
