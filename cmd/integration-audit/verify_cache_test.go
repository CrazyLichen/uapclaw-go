package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countingRunner 计数 fake runner：记录每次真实 go test 调用。
type countingRunner struct {
	calls int
}

// RunGoTest 计数并产出固定档案。
func (r *countingRunner) RunGoTest(testDir string, profilePath string) error {
	r.calls++
	return os.WriteFile(profilePath, []byte("mode: set\n"), 0o644)
}

// writeGoFile 写入一个 .go 源文件（含父目录创建）。
func writeGoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSourceFingerprint_内容变动即变 源码任一文件增删改都会改变指纹。
func TestSourceFingerprint_内容变动即变(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "internal/a/a.go", "package a\n")
	fp1, err := sourceFingerprintFn(root)
	if err != nil {
		t.Fatalf("指纹计算失败: %v", err)
	}
	// 修改内容 → 指纹变
	writeGoFile(t, root, "internal/a/a.go", "package a\n// changed\n")
	fp2, _ := sourceFingerprintFn(root)
	if fp1 == fp2 {
		t.Fatal("内容修改后指纹未变")
	}
	// 新增文件 → 指纹变
	writeGoFile(t, root, "tests/integration/x/x_test.go", "package x\n")
	fp3, _ := sourceFingerprintFn(root)
	if fp2 == fp3 {
		t.Fatal("新增文件后指纹未变")
	}
	// 非 .go 文件不影响指纹
	writeGoFile(t, root, "internal/a/notes.txt", "hello")
	fp4, _ := sourceFingerprintFn(root)
	if fp4 != fp3 {
		t.Fatal("非 .go 文件不应影响指纹")
	}
}

// TestVerifyCoverage_缓存命中免重跑 同指纹二次验证直接复用档案。
func TestVerifyCoverage_缓存命中免重跑(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "internal/a/a.go", "package a\n")
	cacheDir := filepath.Join(root, "cache")
	runner := &countingRunner{}
	audit := &AuditResult{Dirs: []*DirAudit{{
		Dir:             "agentcore/gray",
		PendingCoverage: true,
	}}}
	fp, err := sourceFingerprintFn(root)
	if err != nil {
		t.Fatal(err)
	}
	fpFn := func() (string, error) { return fp, nil }

	cfg := &VerifyConfig{
		RepoRoot: root, Runner: runner, CacheDir: cacheDir,
		FingerprintFn: fpFn, WorkDir: filepath.Join(root, "work"),
	}
	vr1, err := VerifyCoverage(cfg, audit)
	if err != nil {
		t.Fatal(err)
	}
	if vr1.Dirs[0].Cached {
		t.Fatal("首次验证不应命中缓存")
	}
	if runner.calls != 1 {
		t.Fatalf("首次验证应真实跑一次，实际 %d", runner.calls)
	}
	// 同源码指纹二次验证：命中缓存，不再跑 go test
	vr2, err := VerifyCoverage(cfg, audit)
	if err != nil {
		t.Fatal(err)
	}
	if !vr2.Dirs[0].Cached {
		t.Fatal("二次验证应命中缓存")
	}
	if runner.calls != 1 {
		t.Fatalf("缓存命中不应重跑，实际累计 %d 次", runner.calls)
	}
	if !vr2.Passed {
		t.Fatal("缓存命中应沿用档案判定结果")
	}
}

// TestVerifyCoverage_源码变动缓存失效 指纹变化后必须重跑。
func TestVerifyCoverage_源码变动缓存失效(t *testing.T) {
	root := t.TempDir()
	writeGoFile(t, root, "internal/a/a.go", "package a\n")
	cacheDir := filepath.Join(root, "cache")
	runner := &countingRunner{}
	audit := &AuditResult{Dirs: []*DirAudit{{
		Dir:             "agentcore/gray",
		PendingCoverage: true,
	}}}
	fp, err := sourceFingerprintFn(root)
	if err != nil {
		t.Fatal(err)
	}
	fpFn := func() (string, error) { return fp, nil }
	cfg := &VerifyConfig{
		RepoRoot: root, Runner: runner, CacheDir: cacheDir,
		FingerprintFn: fpFn, WorkDir: filepath.Join(root, "work"),
	}
	if _, err = VerifyCoverage(cfg, audit); err != nil {
		t.Fatal(err)
	}
	// 源码变动 → 注入新指纹 → 必须重跑
	fpFn = func() (string, error) { return fp + "-changed", nil }
	cfg.FingerprintFn = fpFn
	if _, err = VerifyCoverage(cfg, audit); err != nil {
		t.Fatal(err)
	}
	if runner.calls != 2 {
		t.Fatalf("源码变动后应重跑，实际累计 %d 次", runner.calls)
	}
}

// TestVerifyCoverage_OnlyChanged_未改动目录跳过不判罚。
func TestVerifyCoverage_OnlyChanged_未改动目录跳过不判罚(t *testing.T) {
	root := t.TempDir()
	runner := &countingRunner{}
	audit := &AuditResult{Dirs: []*DirAudit{
		{Dir: "agentcore/changed", PendingCoverage: true},
		{Dir: "agentcore/stale", PendingCoverage: true},
	}}
	cfg := &VerifyConfig{
		RepoRoot: root, Runner: runner,
		WorkDir:     filepath.Join(root, "work"),
		ChangedDirs: map[string]bool{"agentcore/changed": true},
	}
	vr, err := VerifyCoverage(cfg, audit)
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 {
		t.Fatalf("只有改动目录应真实运行，实际 %d 次", runner.calls)
	}
	if !vr.Dirs[0].Passed {
		t.Fatal("改动目录应正常验证")
	}
	if !vr.Dirs[1].Skipped || !vr.Dirs[1].Passed {
		t.Fatal("未改动目录应跳过且不判罚")
	}
	if !strings.Contains(vr.Dirs[1].SkipReason, "本地未改动") {
		t.Fatalf("跳过原因不符: %s", vr.Dirs[1].SkipReason)
	}
	if !vr.Passed {
		t.Fatal("未改动目录跳过不应拉低整体判定")
	}
}

// TestDirChanged_子路径命中 目录本身或其子路径命中都算改动。
func TestDirChanged_子路径命中(t *testing.T) {
	changed := map[string]bool{"agentcore/x/sub": true}
	if !DirChanged(changed, "agentcore/x") {
		t.Fatal("灰区目录为改动子路径的前缀时应命中")
	}
	if !DirChanged(changed, "agentcore/x/sub") {
		t.Fatal("目录本身命中应返回 true")
	}
	// 全路径形式的灰区目录（AuditResult.Dir 口径）同样归一化命中
	if !DirChanged(changed, "tests/integration/agentcore/x") {
		t.Fatal("带 tests/integration 前缀的全路径应归一化命中")
	}
	if DirChanged(changed, "agentcore/y") {
		t.Fatal("未命中的目录不应返回 true")
	}
}

// TestChangedIntegrationDirs_前缀收敛 git 路径收敛为测试根相对目录。
func TestChangedIntegrationDirs_前缀收敛(t *testing.T) {
	// 非仓库目录走 git 必失败 → 回退 nil
	if got := ChangedIntegrationDirs(t.TempDir(), "origin/main"); got != nil {
		t.Fatal("git 不可用应返回 nil")
	}
}

// TestCacheKey_参数变则键变 运行参数参与键计算。
func TestCacheKey_参数变则键变(t *testing.T) {
	k1 := CacheKey("fp", "tags-a", "", "", "dir")
	k2 := CacheKey("fp", "tags-b", "", "", "dir")
	k3 := CacheKey("fp", "tags-a", "", "", "dir2")
	if k1 == k2 {
		t.Fatal("tags 变化应改变键")
	}
	if k1 == k3 {
		t.Fatal("目录变化应改变键")
	}
}

// TestProfileCache_StoreLoad 往返存取。
func TestProfileCache_StoreLoad(t *testing.T) {
	c := NewProfileCache(filepath.Join(t.TempDir(), "cache"))
	key := "abc123"
	if c.Load(key) != nil {
		t.Fatal("未存储前不应命中")
	}
	c.Store(key, []byte("mode: set\n"))
	data := c.Load(key)
	if data == nil || string(data) != "mode: set\n" {
		t.Fatalf("存取往返不符: %q", data)
	}
	// nil 缓存全部静默禁用
	var nilCache *ProfileCache
	if nilCache.Load(key) != nil {
		t.Fatal("nil 缓存不应命中")
	}
	nilCache.Store(key, []byte("x"))
}
