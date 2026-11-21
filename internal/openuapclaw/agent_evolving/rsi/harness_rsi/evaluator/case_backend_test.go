package evaluator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSnapshotWorkspace_剪除与快照 对齐 Python _snapshot_workspace 剪除表。
func TestSnapshotWorkspace_剪除与快照(t *testing.T) {
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "src", "a.txt"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "__pycache__"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "__pycache__", "m.pyc"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "AGENT.md"), []byte("x"), 0o644))
	snap := snapshotWorkspace(ws)
	assert.Contains(t, snap, "src/a.txt")
	assert.NotContains(t, snap, "__pycache__/m.pyc")
	assert.NotContains(t, snap, "AGENT.md") // 运行时自有文件
}

// TestWorkspaceChanges_三分支 对齐 Python added/modified/removed + 200 截断。
func TestWorkspaceChanges_三分支(t *testing.T) {
	before := map[string]map[string]any{
		"kept_same.txt": {"size": 1, "mtime_ns": int64(1)},
		"modified.txt":  {"size": 1, "mtime_ns": int64(1)},
		"removed.txt":   {"size": 1, "mtime_ns": int64(1)},
	}
	after := map[string]map[string]any{
		"kept_same.txt": {"size": 1, "mtime_ns": int64(1)},
		"modified.txt":  {"size": 2, "mtime_ns": int64(1)},
		"added.txt":     {"size": 1, "mtime_ns": int64(1)},
	}
	changes := workspaceChanges(before, after)
	assert.Equal(t, []string{"added.txt"}, changes["added"])
	assert.Equal(t, []string{"modified.txt"}, changes["modified"])
	assert.Equal(t, []string{"removed.txt"}, changes["removed"])
}

// TestSkipTraceSnapshotPath 对齐 Python 跳过表。
func TestSkipTraceSnapshotPath(t *testing.T) {
	assert.True(t, skipTraceSnapshotPath(".git/config"))
	assert.True(t, skipTraceSnapshotPath("context/foo"))
	assert.True(t, skipTraceSnapshotPath("skills/bar/.workspace"))
	assert.True(t, skipTraceSnapshotPath("x.pyc"))
	assert.False(t, skipTraceSnapshotPath("src/a.txt"))
}

// TestSingleHarnessSystemPrompt_文案逐字 对齐 Python 提示词。
func TestSingleHarnessSystemPrompt_文案逐字(t *testing.T) {
	got := singleHarnessSystemPrompt("solver", "/tmp/ws")
	assert.Contains(t, got, "You are the standalone evaluation agent for role `solver`.")
	assert.Contains(t, got, "Your task workspace and default output directory is `/tmp/ws`.")
	assert.NotContains(t, singleHarnessSystemPrompt("solver", ""), "task workspace")
}

// TestNormalizeCaseInputForBackend_容器路径提示 对齐 Python 文案逐字。
func TestNormalizeCaseInputForBackend_容器路径提示(t *testing.T) {
	swebenchCase := map[string]any{"swebench": map[string]any{}}
	got := normalizeCaseInputForBackend(swebenchCase, "task body")
	assert.Contains(t, got.(string), "the repository is mounted at `/testbed`")
	assert.Equal(t, "plain", normalizeCaseInputForBackend(map[string]any{}, "plain"))
}

// TestResolveSingleHarnessRef_唯一引用守卫 对齐 Python 长度校验。
func TestResolveSingleHarnessRef_唯一引用守卫(t *testing.T) {
	role, path, err := resolveSingleHarnessRef(map[string]string{"solver": "/tmp/h", "x": " "})
	require.NoError(t, err)
	assert.Equal(t, "solver", role)
	assert.Equal(t, "/tmp/h", path)
	_, _, err = resolveSingleHarnessRef(map[string]string{"a": "/1", "b": "/2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires exactly one harness ref")
}

// TestControlledSkillName_两形态 对齐 Python _controlled_skill_name。
func TestControlledSkillName_两形态(t *testing.T) {
	assert.Equal(t, "s1", controlledSkillName(map[string]any{
		"_controlled_skill_treatment": map[string]any{"skill_name": " s1 "}}))
	assert.Equal(t, "s2", controlledSkillName(map[string]any{
		"_controlled_skill_treatment": "s2"}))
	assert.Equal(t, "", controlledSkillName(map[string]any{}))
}

// TestArtifactFilesFromText_边界 对齐 Python 正则语义。
func TestArtifactFilesFromText_边界(t *testing.T) {
	assert.Equal(t, []string{"a.html"}, artifactFilesFromText("see a.html here"))
	assert.Equal(t, []string{"cfg.yaml"}, artifactFilesFromText("use cfg.yaml, then done"))
	// Python 贪婪 [\w.-]+：整词以匹配扩展结尾即命中（"prefix.a.html" 整词匹配）。
	assert.Equal(t, []string{"prefix.a.html"}, artifactFilesFromText("prefix.a.html"))
	assert.Equal(t, []string{"x.py.txt"}, artifactFilesFromText("x.py.txt"))
}

// TestSingleHarnessMetadata_键集 对齐 Python metadata 结构。
func TestSingleHarnessMetadata_键集(t *testing.T) {
	md := singleHarnessMetadata(singleHarnessMetadataParams{
		roleName:             "solver",
		workspaceBefore:      map[string]map[string]any{},
		workspaceAfter:       map[string]map[string]any{},
		teamSkillRefPath:     "",
		solverMaxIterations:  300,
		runtimeConfig:        map[string]any{},
		executionTermination: map[string]any{},
		runtimeLogs:          []string{},
	})
	assert.Equal(t, "solver", md["member_id"])
	assert.Equal(t, "solver", md["member_role"])
	assert.Contains(t, md, "workspace_changes")
	assert.Contains(t, md, "execution_evidence")
	// 空证据回落 capture_not_started。
	ev := md["execution_evidence"].(map[string]any)
	assert.Equal(t, "incomplete", ev["status"])
	assert.Contains(t, ev["reasons"].([]string), "capture_not_started")
}

// TestBuildBackend_注册表守卫 对齐 Python build_backend。
func TestBuildBackend_注册表守卫(t *testing.T) {
	// Backend 空串回落 single_harness？Python registry 仅 single_harness 且
	// config.backend 缺省 "single_harness"——空串为未知（逐字对齐）。
	require.Error(t, func() error { defer func() { _ = recover() }(); _, err := BuildBackend(nil); return err }())
}
