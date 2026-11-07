package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

// testAgentDataFileName agent-data.json 文件名（skillruntime 中的常量未导出，测试侧单独声明）
const testAgentDataFileName = "agent-data.json"

// ──────────────────────────── 非导出函数 ────────────────────────────

// setupDataDir 准备隔离的数据根并返回其绝对路径。
// 关键点：预先创建 <data>/config 目录，使 AgentWorkspaceDir() 走「已初始化」
// 分支而非 resources 回退，保证测试写入位置可控。
func setupDataDir(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "config"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "agent", "workspace"), 0o755))
	workspace.SetUserHome(workspace.UserHomeDir())
	t.Cleanup(func() { workspace.SetUserHome("") })
	return dataDir
}

// errorCode 从 JSON 错误响应体中取出 error 字段。
func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload), "响应应为 JSON: %s", string(body))
	code, _ := payload["error"].(string)
	return code
}
