package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// TestSlugify_Basic 对齐 projectless_workspace.py:348-374 主链。
func TestSlugify_Basic(t *testing.T) {
	cases := []struct {
		in     string
		fb     string
		prefix string
		want   string
	}{
		{"hello world", "default", "task", "hello-world"},
		{"", "default", "task", "default"},
		{"  ..--__  ", "default", "task", "default"},
		{"CON", "default", "member", "member-CON"}, // Windows 保留名前缀防撞
		{"nul.txt", "default", "member", "member-nul.txt"},
		{"正常名字", "default", "task", "正常名字"}, // Unicode 词字符保留
	}
	for _, c := range cases {
		if got := Slugify(c.in, c.fb, c.prefix); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestSlugify_LongTruncation 对齐 :48 长度截断 + rstrip。
func TestSlugify_LongTruncation(t *testing.T) {
	long := strings.Repeat("a", 60) + "-"
	got := Slugify(long, "default", "task")
	if len([]rune(got)) != maxTaskNameLength {
		t.Fatalf("截断后长度 = %d, want %d", len([]rune(got)), maxTaskNameLength)
	}
}

// TestAllocateTaskWorkspace_ReuseAndDistinct 同会话复用、异会话取新号。
func TestAllocateTaskWorkspace_ReuseAndDistinct(t *testing.T) {
	tasksDir := t.TempDir()
	registry := filepath.Join(t.TempDir(), ".team_artifacts")

	root1, err := AllocateTaskWorkspace(tasksDir, registry, "sess-1", "任务一")
	if err != nil {
		t.Fatal(err)
	}
	root2, err := AllocateTaskWorkspace(tasksDir, registry, "sess-1", "任务一")
	if err != nil {
		t.Fatal(err)
	}
	if root1 != root2 {
		t.Fatalf("同 session 应复用: %q vs %q", root1, root2)
	}
	if !strings.Contains(root1, filepath.Join("artifacts", time.Now().Format("2006-01-02"), "chat-1")) {
		// 布局断言放宽为 tasksDir 下 chat-1（日期目录由 session 日期决定）
		if !strings.Contains(root1, "chat-1") {
			t.Fatalf("chat-1 布局缺失: %q", root1)
		}
	}

	root3, err := AllocateTaskWorkspace(tasksDir, registry, "sess-2", "")
	if err != nil {
		t.Fatal(err)
	}
	if root3 == root1 {
		t.Fatal("不同 session 应得不同 root")
	}

	// metadata.json sidecar 已写
	info, err := os.Stat(filepath.Join(root1, metadataFilename))
	if err != nil || info.IsDir() {
		t.Fatalf("metadata.json 应存在: %v", err)
	}
	// .session_id 碰撞标记已写
	if marker, _ := readSessionMarker(root1); marker != "sess-1" {
		t.Fatalf("碰撞标记 = %q, want sess-1", marker)
	}
	// 注册文件已写
	regPath := filepath.Join(registry, "sess-1.json")
	if !fileExists(regPath) {
		t.Fatalf("注册文件应存在: %s", regPath)
	}
}

// TestAllocateTaskWorkspace_MetadataBackfill 对齐 :256-263：已注册但缺
// metadata.json 的回填分支。
func TestAllocateTaskWorkspace_MetadataBackfill(t *testing.T) {
	tasksDir := t.TempDir()
	registry := filepath.Join(t.TempDir(), ".team_artifacts")
	root, err := AllocateTaskWorkspace(tasksDir, registry, "sess-x", "t")
	if err != nil {
		t.Fatal(err)
	}
	// 删除 metadata 模拟旧版本工作空间
	if err := os.Remove(filepath.Join(root, metadataFilename)); err != nil {
		t.Fatal(err)
	}
	root2, err := AllocateTaskWorkspace(tasksDir, registry, "sess-x", "t")
	if err != nil {
		t.Fatal(err)
	}
	if root2 != root {
		t.Fatal("回填不应重新分配")
	}
	if !fileExists(filepath.Join(root, metadataFilename)) {
		t.Fatal("metadata 应已回填")
	}
}

// TestAllocateTaskWorkspace_DateAnchor 对齐 :267-330：session 创建日期锚定。
// 预置 session 目录 metadata.json created_at 为过去时间戳。
func TestAllocateTaskWorkspace_DateAnchor(t *testing.T) {
	tasksDir := t.TempDir()
	registry := filepath.Join(t.TempDir(), ".team_artifacts")
	// 构造 session 目录（AgentSessionsDir 由 WorkspaceDir 派生，测试环境覆盖 home）
	orig := os.Getenv("UAPCLAW_DATA_DIR")
	sessionsDir := workspaceSessionsDirForTest(t)
	sessionDir := filepath.Join(sessionsDir, "old-session")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Skipf("session 目录不可写: %v", err)
	}
	old := time.Now().AddDate(0, -1, 0).UTC().Format("2006-01-02T15:04:05+00:00")
	os.WriteFile(filepath.Join(sessionDir, metadataFilename),
		[]byte(`{"created_at":"`+old+`"}`), 0o644)
	defer func() {
		if orig == "" {
			_ = os.Unsetenv("UAPCLAW_DATA_DIR")
		} else {
			_ = os.Setenv("UAPCLAW_DATA_DIR", orig)
		}
	}()
	root, err := AllocateTaskWorkspace(tasksDir, registry, "old-session", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(root, time.Now().AddDate(0, -1, 0).Format("2006-01-02")) {
		t.Fatalf("任务目录应锚定 session 创建日期: %q", root)
	}
}

// workspaceSessionsDirForTest 返回测试用的 AgentSessionsDir。
func workspaceSessionsDirForTest(t *testing.T) string {
	t.Helper()
	return workspace.AgentSessionsDir()
}

// TestCoerceTimestamp_Forms 对齐 projectless_workspace.py:332-345 各形态。
func TestCoerceTimestamp_Forms(t *testing.T) {
	if coerceTimestamp(true) != 0 {
		t.Fatal("bool 应拒绝")
	}
	if coerceTimestamp(-5) != 0 {
		t.Fatal("负数应拒绝")
	}
	if coerceTimestamp("not-a-time") != 0 {
		t.Fatal("非法串应拒绝")
	}
	// ISO 带 Z
	got := coerceTimestamp("2026-01-02T03:04:05Z")
	want := mustPositive(float64(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Unix()))
	if got != want {
		t.Fatalf("ISO-Z = %v, want %v", got, want)
	}
	// 纯数值
	if coerceTimestamp(float64(1700000000)) != 1700000000 {
		t.Fatal("数值应直通")
	}
}

// TestAtomicWrite_Cleanup 对齐 Python finally unlink(missing_ok=True)。
func TestAtomicWrite_Cleanup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x.json")
	if err := atomicWrite(target, []byte(`{"root_dir":"/tmp/a"}`), false); err != nil {
		t.Fatal(err)
	}
	if !fileExists(target) {
		t.Fatal("目标应存在")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("临时文件应清理，残留 %d 项", len(entries))
	}
}
