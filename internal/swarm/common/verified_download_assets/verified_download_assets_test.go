package verified_download_assets

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestOwnerWithClock 测试构造：固定 root + 可拨动时钟，不启后台 sweeper。
// 返回 owner 与时钟指针（测试内拨动 *clock 模拟时间前进）。
func newTestOwnerWithClock(t *testing.T, opts OwnerOptions) (*VerifiedDownloadAssetOwner, *float64) {
	t.Helper()
	clock := 1.0
	opts.NowFn = func() float64 { return clock }
	opts.StartSweeper = false
	owner, err := NewVerifiedDownloadAssetOwner(t.TempDir(), opts)
	if err != nil {
		t.Fatalf("构造 owner 失败: %v", err)
	}
	return owner, &clock
}

// newTestOwner 常规假时钟构造。
func newTestOwner(t *testing.T) (*VerifiedDownloadAssetOwner, *float64) {
	return newTestOwnerWithClock(t, OwnerOptions{})
}

// writeFile 写入测试源文件并返回路径。
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	return p
}

func TestOwnerStage_密封拷贝与sidecar(t *testing.T) {
	owner, _ := newTestOwner(t)
	src := writeFile(t, t.TempDir(), "report.pdf", "payload-bytes")
	asset, err := owner.Stage(src, StageOptions{FileName: "report.pdf", ExpiresAt: 2.0})
	if err != nil {
		t.Fatalf("Stage 失败: %v", err)
	}
	data, err := os.ReadFile(asset.SealedPath)
	if err != nil {
		t.Fatalf("读密封文件失败: %v", err)
	}
	if string(data) != "payload-bytes" {
		t.Fatalf("密封内容不符: %q", string(data))
	}
	sum := sha256.Sum256(data)
	wantDigest := "sha256:" + hex.EncodeToString(sum[:])
	if asset.ContentDigest != wantDigest {
		t.Fatalf("digest 不符: %q != %q", asset.ContentDigest, wantDigest)
	}
	if asset.SizeBytes != int64(len("payload-bytes")) {
		t.Fatalf("size 不符: %d", asset.SizeBytes)
	}
	if !strings.HasSuffix(asset.SealedPath, ".pdf") {
		t.Fatalf("密封文件应保留后缀: %q", asset.SealedPath)
	}
	if asset.AssetID != strings.TrimSuffix(filepath.Base(asset.SealedPath), ".pdf") {
		t.Fatalf("asset_id 与 sealed 文件名不符: %q vs %q", asset.AssetID, asset.SealedPath)
	}
	// sidecar 存在且 digest/state 一致
	payload, err := owner.readSidecar(owner.sidecarPath(asset.AssetID))
	if err != nil {
		t.Fatalf("读 sidecar 失败: %v", err)
	}
	if payload["content_digest"] != wantDigest {
		t.Fatalf("sidecar digest 不符: %v", payload["content_digest"])
	}
	if payload["state"] != assetStateStaged {
		t.Fatalf("sidecar state 应为 staged: %v", payload["state"])
	}
}

func TestOwnerStage_过期时间拒绝(t *testing.T) {
	owner, clock := newTestOwner(t)
	*clock = 2.0
	src := writeFile(t, t.TempDir(), "a.txt", "x")
	_, err := owner.Stage(src, StageOptions{FileName: "a.txt", ExpiresAt: 1.0})
	if err == nil || !strings.Contains(err.Error(), "download_asset_expired") {
		t.Fatalf("应拒绝过期时间: %v", err)
	}
}

func TestOwnerStage_拒绝symlink源(t *testing.T) {
	owner, _ := newTestOwner(t)
	dir := t.TempDir()
	real := writeFile(t, dir, "real.txt", "data")
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("环境不支持 symlink: %v", err)
	}
	_, err := owner.Stage(link, StageOptions{FileName: "link.txt", ExpiresAt: 2.0})
	if err == nil || !strings.Contains(err.Error(), "download_asset_source_not_file") {
		t.Fatalf("应拒绝 symlink 源: %v", err)
	}
}

func TestOwnerStage_root为symlink拒绝(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("环境不支持 symlink: %v", err)
	}
	_, err := NewVerifiedDownloadAssetOwner(link, OwnerOptions{StartSweeper: false})
	if err == nil || !strings.Contains(err.Error(), "download_asset_root_invalid") {
		t.Fatalf("应拒绝 symlink root: %v", err)
	}
}

func TestOwnerCommitRevoke(t *testing.T) {
	owner, _ := newTestOwner(t)
	src := writeFile(t, t.TempDir(), "a.bin", "abc")
	asset, err := owner.Stage(src, StageOptions{FileName: "a.bin", ExpiresAt: 2.0})
	if err != nil {
		t.Fatal(err)
	}
	// commit：sidecar state → committed
	if err := owner.Commit(asset); err != nil {
		t.Fatalf("Commit 失败: %v", err)
	}
	payload, err := owner.readSidecar(owner.sidecarPath(asset.AssetID))
	if err != nil {
		t.Fatal(err)
	}
	if payload["state"] != assetStateCommitted {
		t.Fatalf("commit 后 state 应为 committed: %v", payload["state"])
	}
	if _, err := os.Stat(asset.SealedPath); err != nil {
		t.Fatalf("commit 后密封文件应保留: %v", err)
	}
	// commit 二次（state 仍 active）应成功（幂等，对齐 Python _ACTIVE_ASSET_STATES）
	if err := owner.Commit(asset); err != nil {
		t.Fatalf("重复 Commit 应成功: %v", err)
	}
	// revoke：sealed + sidecar 删除，重复 revoke 幂等
	owner.Revoke(asset)
	if _, err := os.Stat(asset.SealedPath); !os.IsNotExist(err) {
		t.Fatalf("revoke 后密封文件应删除: %v", err)
	}
	if _, err := os.Stat(filepath.Join(owner.root, asset.AssetID+".json")); !os.IsNotExist(err) {
		t.Fatalf("revoke 后 sidecar 应删除: %v", err)
	}
	owner.Revoke(asset) // 幂等
}

func TestOwnerCommit_账目不匹配拒绝(t *testing.T) {
	owner, _ := newTestOwner(t)
	src := writeFile(t, t.TempDir(), "a.txt", "abc")
	asset, err := owner.Stage(src, StageOptions{FileName: "a.txt", ExpiresAt: 2.0})
	if err != nil {
		t.Fatal(err)
	}
	tampered := *asset
	tampered.SizeBytes = 999
	err = owner.Commit(&tampered)
	if err == nil || !strings.Contains(err.Error(), "download_asset_registration_mismatch") {
		t.Fatalf("应拒绝不匹配账目: %v", err)
	}
}

func TestOwnerIsActive_校验矩阵(t *testing.T) {
	owner, clock := newTestOwner(t)
	src := writeFile(t, t.TempDir(), "a.txt", "hello")
	asset, err := owner.Stage(src, StageOptions{FileName: "a.txt", ExpiresAt: 2.0})
	if err != nil {
		t.Fatal(err)
	}
	claim := IsActiveClaim{
		AssetID:       asset.AssetID,
		SealedPath:    asset.SealedPath,
		ExpiresAt:     asset.ExpiresAt,
		SizeBytes:     asset.SizeBytes,
		ContentDigest: asset.ContentDigest,
	}
	*clock = 1.5
	if !owner.IsActive(claim) {
		t.Fatal("有效 claim 应 active")
	}
	*clock = 3.0
	if owner.IsActive(claim) {
		t.Fatal("过期 claim 应不 active")
	}
	*clock = 1.5
	bad := claim
	bad.ContentDigest = "sha256:" + strings.Repeat("0", 64)
	if owner.IsActive(bad) {
		t.Fatal("digest 篡改应不 active")
	}
	// sealed 内容篡改：is_active 不重哈希（对齐 Python L164-175 语义——
	// 内容完整性由密封副本 0400 权限与 root 归属保护，token↔sidecar 五字段一致性在此校验）
	if err := os.WriteFile(asset.SealedPath, []byte("tampered"), 0o400); err != nil {
		t.Fatal(err)
	}
	if !owner.IsActive(claim) {
		t.Fatal("is_active 不重哈希，篡改不应改变判定（对齐 Python）")
	}
}

func TestOwnerIsActive_路径逃逸拒绝(t *testing.T) {
	owner, _ := newTestOwner(t)
	claim := IsActiveClaim{
		AssetID:       strings.Repeat("a", 32),
		SealedPath:    "/etc/passwd",
		ExpiresAt:     2.0,
		SizeBytes:     1,
		ContentDigest: "sha256:" + strings.Repeat("a", 64),
	}
	if owner.IsActive(claim) {
		t.Fatal("root 外路径应不 active")
	}
}

func TestOwnerPrune_过期清理(t *testing.T) {
	owner, clock := newTestOwner(t)
	src := writeFile(t, t.TempDir(), "a.txt", "data")
	asset, err := owner.Stage(src, StageOptions{FileName: "a.txt", ExpiresAt: 2.0})
	if err != nil {
		t.Fatal(err)
	}
	// 未过期：保留
	*clock = 1.5
	owner.Prune(1.5)
	if _, err := os.Stat(asset.SealedPath); err != nil {
		t.Fatalf("未过期资产不应被清理: %v", err)
	}
	// 过期：sidecar + sealed 删除
	*clock = 3.0
	owner.Prune(3.0)
	if _, err := os.Stat(asset.SealedPath); !os.IsNotExist(err) {
		t.Fatalf("过期资产应清理: %v", err)
	}
}

func TestOwnerPrune_孤儿与损坏sidecar(t *testing.T) {
	// 真实时钟 + 小宽限，验证孤儿清理
	owner, err := NewVerifiedDownloadAssetOwner(t.TempDir(), OwnerOptions{
		OrphanGraceSeconds: 1.0,
		StartSweeper:       false,
	})
	if err != nil {
		t.Fatal(err)
	}
	src := writeFile(t, t.TempDir(), "a.txt", "data")
	now := time.Now()
	asset, err := owner.Stage(src, StageOptions{FileName: "a.txt", ExpiresAt: float64(now.Add(time.Hour).UnixMilli()) / 1000.0})
	if err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(owner.root, "orphan.bin")
	if err := os.WriteFile(orphan, []byte("old"), 0o400); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(orphan, past, past); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(owner.root, strings.Repeat("b", 32)+".json")
	if err := os.WriteFile(bad, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// 有效资产保留；孤儿（mtime 早于宽限）与损坏 sidecar 清理
	owner.Prune(0)
	if _, err := os.Stat(asset.SealedPath); err != nil {
		t.Fatalf("有效资产不应被清理: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("超宽限孤儿应清理: %v", err)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Fatalf("损坏 sidecar 应清理: %v", err)
	}
}

func TestSweeper_启停(t *testing.T) {
	owner, err := NewVerifiedDownloadAssetOwner(t.TempDir(), OwnerOptions{
		StartSweeper:         true,
		SweepIntervalSeconds: 0.05,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 首次 Stage 触发 sweeper 启动
	src := writeFile(t, t.TempDir(), "a.txt", "x")
	if _, err := owner.Stage(src, StageOptions{FileName: "a.txt", ExpiresAt: float64(time.Now().Add(time.Hour).UnixMilli()) / 1000.0}); err != nil {
		t.Fatal(err)
	}
	owner.mu.Lock()
	started := owner.sweeperStarted
	owner.mu.Unlock()
	if !started {
		t.Fatal("start_sweeper=true 时首 Stage 应启动后台 sweeper")
	}
	owner.Close() // join 内退出，不泄漏
}

func TestNormalizeAssetID(t *testing.T) {
	valid := strings.Repeat("A", 32)
	if got := normalizeAssetID(valid); got != strings.Repeat("a", 32) {
		t.Fatalf("应小写归一: %q", got)
	}
	if normalizeAssetID("") != "" {
		t.Fatal("空应返回空")
	}
	if normalizeAssetID(strings.Repeat("g", 32)) != "" {
		t.Fatal("非 hex 应返回空")
	}
	if normalizeAssetID(strings.Repeat("a", 31)) != "" {
		t.Fatal("长度不足应返回空")
	}
}

func TestNormalizeDigest(t *testing.T) {
	hex64 := strings.Repeat("a", 64)
	got, err := normalizeDigest("sha256:" + strings.ToUpper(hex64))
	if err != nil || got != "sha256:"+hex64 {
		t.Fatalf("归一失败: %q err=%v", got, err)
	}
	got, err = normalizeDigest(hex64)
	if err != nil || got != "sha256:"+hex64 {
		t.Fatalf("无前缀应补齐: %q err=%v", got, err)
	}
}

func TestNormalizeDigest_非法值报错(t *testing.T) {
	cases := []string{"", "abc", "sha256:zz" + strings.Repeat("a", 61), strings.Repeat("a", 63)}
	for _, c := range cases {
		_, err := normalizeDigest(c)
		if err == nil || !strings.Contains(err.Error(), "download_asset_digest_invalid") {
			t.Fatalf("应报 digest 非法: %q → %v", c, err)
		}
	}
}

func TestDefaultAssetRoot_环境变量(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JIUWENSWARM_DOWNLOAD_ASSET_ROOT", dir)
	root := defaultAssetRoot()
	if root != filepath.Clean(dir) {
		t.Fatalf("应取环境变量: %q != %q", root, filepath.Clean(dir))
	}
}

func TestGetVerifiedDownloadAssetOwner_单例(t *testing.T) {
	a := GetVerifiedDownloadAssetOwner()
	b := GetVerifiedDownloadAssetOwner()
	if a != b {
		t.Fatal("应返回进程级单例")
	}
}
