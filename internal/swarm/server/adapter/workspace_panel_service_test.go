package adapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestZones白名单与归一 对齐 zones.py 全量。
func TestZones白名单与归一(t *testing.T) {
	// DELETABLE_ZONES 仅三类（zones.py:27-32）
	require.True(t, workspaceIsDeletable("agent/workspace/projects/p1/a.txt"))
	require.True(t, workspaceIsDeletable("agent/workspace/todo/t.json"))
	require.True(t, workspaceIsDeletable("agent/workspace/prompt_attachment/x.png"))
	require.True(t, workspaceIsDeletable("agent/workspace/received_files/y.bin"))
	// 禁删 zone
	require.False(t, workspaceIsDeletable("agent/sessions/meta.json")) // session_meta
	require.False(t, workspaceIsDeletable("agent/workspace/skills/s/SKILL.md"))
	require.False(t, workspaceIsDeletable("agent/workspace/memory/m.md"))
	require.False(t, workspaceIsDeletable("agent/workspace/AGENT.md"))     // role_md
	require.False(t, workspaceIsDeletable("agent/workspace/extensions/e")) // runtime
	require.False(t, workspaceIsDeletable("anything/else"))                // other

	// normalize_relative_path（zones.py:60-83）
	rel, perr := workspaceNormalizeRelativePath("  ./a/b/  ")
	require.Equal(t, "a/b", rel)
	require.Empty(t, perr)
	_, perr2 := workspaceNormalizeRelativePath("../escape")
	require.Equal(t, pathErrTraversal, perr2)
	_, perr3 := workspaceNormalizeRelativePath("/abs/path")
	require.Equal(t, pathErrAbsolute, perr3)
	_, perr4 := workspaceNormalizeRelativePath("C:/Windows")
	require.Equal(t, pathErrAbsolute, perr4)
	rel5, _ := workspaceNormalizeRelativePath("")
	require.Equal(t, "", rel5) // 根
}

// TestWorkspaceTree 树项 zone/deletable/排序。
func TestWorkspaceTree(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "agent", "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "projects"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "skills"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "projects", "a.txt"), []byte("hi"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "AGENT.md"), []byte("hi"), 0o644))

	svc := NewWorkspaceService(root)
	payload, werr := svc.ListTree("")
	require.Nil(t, werr)
	entries := payload["entries"].([]map[string]any)
	require.Len(t, entries, 1) // 根下只有 agent/
	require.Equal(t, "agent", entries[0]["name"])
	// 进入 workspace 层看 zone/deletable
	payload2, werr2 := svc.ListTree("agent/workspace")
	require.Nil(t, werr2)
	entries2 := payload2["entries"].([]map[string]any)
	byName := map[string]map[string]any{}
	for _, e := range entries2 {
		byName[e["name"].(string)] = e
	}
	require.Equal(t, "role_md", byName["AGENT.md"]["zone"])
	require.False(t, byName["AGENT.md"]["deletable"].(bool))
	require.Equal(t, "session_workspace", byName["projects"]["zone"])
	require.True(t, byName["projects"]["deletable"].(bool))
	// 目录 size_bytes 为 null
	require.Nil(t, byName["projects"]["size_bytes"])
	// 文件 size_bytes 为 int64
	require.Equal(t, int64(2), byName["AGENT.md"]["size_bytes"])
}

// TestWorkspacePreview 文本/二进制/目录（service.py:255-309）。
func TestWorkspacePreview(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "text.md"), []byte("你好世界"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "bin.dat"), []byte{0x00, 0x01, 0x02}, 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dir"), 0o755))
	// 需在白名单 zone 内才能 resolve 到？preview 不做 zone 限制，仅路径校验
	svc := NewWorkspaceService(root)

	got, werr := svc.PreviewFile("text.md", nil)
	require.Nil(t, werr)
	require.Equal(t, "你好世界", got["content"])
	require.False(t, got["truncated"].(bool))

	bin, werr2 := svc.PreviewFile("bin.dat", nil)
	require.Nil(t, werr2)
	require.Nil(t, bin["content"]) // 二进制 content=null

	_, werr3 := svc.PreviewFile("dir", nil)
	require.NotNil(t, werr3)
	require.Equal(t, "BAD_REQUEST", werr3.Code) // 目录 400

	_, werr4 := svc.PreviewFile("missing.txt", nil)
	require.NotNil(t, werr4)
	require.Equal(t, "NOT_FOUND", werr4.Code)
}

// TestWorkspaceEntriesDelete 白名单内外与上限（service.py:125-234）。
func TestWorkspaceEntriesDelete(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "agent", "workspace")
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "projects", "p1"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "skills"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "projects", "p1", "a.txt"), []byte("0123456789"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "skills", "keep.md"), []byte("x"), 0o644))

	svc := NewWorkspaceService(root)
	out := svc.DeleteEntries([]any{
		"agent/workspace/projects/p1",    // 白名单内 → ok
		"agent/workspace/skills/keep.md", // 禁删 zone → forbidden_zone
		"../escape",                      // 穿越 → bad_path
		"agent/workspace/projects/none",  // 不存在 → not_found
	})
	results := out["results"].([]map[string]any)
	require.Len(t, results, 4)
	require.True(t, results[0]["ok"].(bool))
	require.Equal(t, int64(10), results[0]["freed_bytes"])
	require.False(t, results[1]["ok"].(bool))
	require.Equal(t, "forbidden_zone", results[1]["error"])
	require.False(t, results[2]["ok"].(bool))
	require.Equal(t, "bad_path", results[2]["error"])
	require.False(t, results[3]["ok"].(bool))
	require.Equal(t, "not_found", results[3]["error"])
	_, err := os.Stat(filepath.Join(ws, "projects", "p1"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(ws, "skills", "keep.md"))
	require.NoError(t, err) // 禁删项仍在
}

// TestWorkspaceUsage单机 du 快照。
func TestWorkspaceUsage单机(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "f.txt"), []byte("0123456789"), 0o644))
	svc := NewWorkspaceService(root)
	require.Equal(t, int64(10), svc.MeasureUsedBytes())
}

// TestAdapter分发四方法 adapter 方法表与 WorkspaceError → code。
func TestAdapter分发四方法(t *testing.T) {
	// 方法表已注册
	require.True(t, HandlesWorkspaceFileMethod(schema.ReqMethodWorkspaceTree))
	require.True(t, HandlesWorkspaceFileMethod(schema.ReqMethodWorkspacePreview))
	require.True(t, HandlesWorkspaceFileMethod(schema.ReqMethodWorkspaceEntriesDelete))
	require.True(t, HandlesWorkspaceFileMethod(schema.ReqMethodWorkspaceUsage))

	// entries_delete 非列表 → BAD_REQUEST
	_, ok, code, err := HandleWorkspaceFileRequest(schema.ReqMethodWorkspaceEntriesDelete,
		map[string]any{"relative_paths": "not-a-list"}, "s1")
	require.False(t, ok)
	require.Equal(t, "BAD_REQUEST", code)
	require.NotNil(t, err)

	// usage 单机快照（quota 层不移植 D-B2-4）
	payload, ok2, _, _ := HandleWorkspaceFileRequest(schema.ReqMethodWorkspaceUsage, map[string]any{}, "s1")
	require.True(t, ok2)
	require.Contains(t, payload, "used_bytes")
	require.Nil(t, payload["quota"])
}

// TestPreview截断 limit+1 读取与 truncated 标记。
func TestPreview截断(t *testing.T) {
	root := t.TempDir()
	content := []byte("0123456789abcdef")
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), content, 0o644))
	svc := NewWorkspaceService(root)
	got, werr := svc.PreviewFile("a.txt", float64(8))
	require.Nil(t, werr)
	require.Equal(t, "01234567", got["content"])
	require.True(t, got["truncated"].(bool))
}
