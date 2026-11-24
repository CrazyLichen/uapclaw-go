package discovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeFS 构造带 N 个技能的 SkillFS（真盘）
func makeFS(t *testing.T, root string, count int) (*SkillFS, DiscoverySettings) {
	t.Helper()
	for i := 0; i < count; i++ {
		name := strings.Repeat("s", 1) + "-skill-" + string(rune('a'+i))
		makeSkillDir(t, root, name, "name: "+name+"\ndescription: desc "+name+"\n", "body "+name)
	}
	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return fs, settings
}

// TestSkillFS_SmallMode 少量技能 → small 全量注入
func TestSkillFS_SmallMode(t *testing.T) {
	root := t.TempDir()
	fs, settings := makeFS(t, root, 2)
	snap := fs.PromptSnapshot(false)
	if snap.Mode != "small" {
		t.Fatalf("少量技能应为 small: %s", snap.Mode)
	}
	if !snap.AllCandidatesIncluded() || snap.TotalCount != 2 || len(snap.Entries) != 2 {
		t.Fatalf("全量注入不符: %+v", snap)
	}
	if snap.CandidateBudgetTokens != settings.CandidateBudgetTokens() {
		t.Fatalf("预算不符: %d", snap.CandidateBudgetTokens)
	}
	// 布局 flat（无索引）
	if fs.Artifact().Layout != "flat" || fs.Artifact().IndexState != "missing" {
		t.Fatalf("无索引应 flat/missing: %+v", fs.Artifact())
	}
}

// TestSkillFS_LargeFlat 大量技能 → large-flat 截断 + preferred 优先
func TestSkillFS_LargeFlat(t *testing.T) {
	root := t.TempDir()
	// 造 50 个长描述技能（估 token 超预算 200000*1%=2000）
	for i := 0; i < 50; i++ {
		name := fmt.Sprintf("skill-%02d", i)
		desc := strings.Repeat("long description ", 20) + name
		makeSkillDir(t, root, name, "name: "+name+"\ndescription: "+desc+"\n", "body")
	}
	// preferred 技能
	makeSkillDir(t, root, "skill-creator", "name: skill-creator\ndescription: preferred one\n", "body")
	settings := DefaultDiscoverySettings()
	settings.MaxListEntries = 40
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	snap := fs.PromptSnapshot(false)
	if snap.Mode != "large-flat" {
		t.Fatalf("大量技能应为 large-flat: %s (est=%d budget=%d)", snap.Mode, snap.EstimatedCandidateTokens, snap.CandidateBudgetTokens)
	}
	// preferred 优先保留
	found := false
	for _, e := range snap.Entries {
		if e.WorkerID == "skill-creator" {
			found = true
		}
	}
	if !found {
		t.Fatal("preferred 技能应保留")
	}
	if len(snap.Entries) > settings.MaxListEntries {
		t.Fatalf("条目应封顶: %d", len(snap.Entries))
	}
	if snap.OmittedCount() != snap.TotalCount-len(snap.Entries) {
		t.Fatalf("省略计数不符: %+v", snap)
	}
}

// TestSkillFS_Freeze 冻结后 refresh 不变
func TestSkillFS_Freeze(t *testing.T) {
	root := t.TempDir()
	fs, settings := makeFS(t, root, 2)
	// 重建为 live 闭包（makeFS 返回的是缓存清单的 fs，这里换成实时扫描）
	fs, err := NewSkillFS(func() (SkillInventory, error) {
		return ScanSkillDirectories([]string{root}, nil, nil)
	}, SkillFSOptions{Settings: settings, ArtifactRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	snap1 := fs.PromptSnapshot(false)
	// 冻结后再取（refresh=true 会重扫，但内容未变 → 相同指纹）
	snap2 := fs.PromptSnapshot(true)
	if snap1.EstimatedCandidateTokens != snap2.EstimatedCandidateTokens {
		t.Fatal("同内容快照应一致")
	}
	// 磁盘新增技能后 refresh=true 会反映（live 路径），但 rail 用的是冻结副本
	makeSkillDir(t, root, "new-skill", "name: new-skill\ndescription: added\n", "")
	snap3 := fs.PromptSnapshot(true)
	if snap3.TotalCount != 3 {
		t.Fatalf("refresh 应感知新增: %d", snap3.TotalCount)
	}
	// 冻结副本（早先取得）不变
	if snap1.TotalCount != 2 {
		t.Fatal("冻结快照不应变化")
	}
}

// TestSkillFS_SelectionCards 卡表投影
func TestSkillFS_SelectionCards(t *testing.T) {
	root := t.TempDir()
	fs, _ := makeFS(t, root, 2)
	cards := fs.SelectionCards(true)
	if len(cards) != 2 {
		t.Fatalf("卡数不符: %d", len(cards))
	}
	for wid, card := range cards {
		if card["name"] != wid {
			t.Fatalf("卡 name 应为 worker_id: %+v", card)
		}
	}
}

// TestSkillFS_View 路径投影与虚拟寻址
func TestSkillFS_View(t *testing.T) {
	root := t.TempDir()
	fs, _ := makeFS(t, root, 2)
	view := fs.Directory()
	children, err := view.Children("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 2 {
		t.Fatalf("根子条目数: %d", len(children))
	}
	// META.md 虚拟寻址
	wid := children[0].WorkerID
	meta, err := view.MetadataPath(wid)
	if err != nil || !strings.HasSuffix(meta, "/META.md") {
		t.Fatalf("虚拟寻址不符: %s %v", meta, err)
	}
	// 磁盘不需要存在（spec §80②）
	if _, err := os.Stat(meta); !os.IsNotExist(err) {
		t.Log("META.md 磁盘存在与否不影响虚拟寻址语义")
	}
	rec, err := view.ResolveMetadataPath(meta)
	if err != nil || rec.WorkerID != wid {
		t.Fatalf("虚拟寻址解析不符: %+v %v", rec, err)
	}
	// 未知 ID
	if _, err := view.MetadataPath("nope"); err == nil {
		t.Fatal("未知 ID 应报错")
	}
	// NormalizePath 拒绝 ..
	if _, err := view.NormalizePath("/a/../b"); err == nil {
		t.Fatal(".. 应被拒绝")
	}
	// SkillCount
	if view.SkillCount("/") != 2 {
		t.Fatalf("技能计数不符: %d", view.SkillCount("/"))
	}
}

// TestSkillFS_SearchContent 词法检索（flat 临时索引）
func TestSkillFS_SearchContent(t *testing.T) {
	root := t.TempDir()
	fs, _ := makeFS(t, root, 3)
	view := fs.Directory()
	records := view.ScopedRecords([]string{"/"}, -1)
	hits, err := fs.SearchContent(records, "desc s-skill-a", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || hits[0] != "s-skill-a" {
		t.Fatalf("检索不符: %v", hits)
	}
	// 缺词
	missing := fs.MissingContentTerms("zzzznotexist")
	if len(missing) != 1 {
		t.Fatalf("缺词不符: %v", missing)
	}
}

// TestSkillFS_ReadBody 正文读取与越界防护
func TestSkillFS_ReadBody(t *testing.T) {
	root := t.TempDir()
	fs, _ := makeFS(t, root, 2)
	view := fs.Directory()
	records := view.ScopedRecords([]string{"/"}, -1)
	body := fs.ReadBody(records[0])
	if !strings.Contains(body, "body") {
		t.Fatalf("正文不符: %q", body)
	}
	// 缓存命中（同 worker+hash）
	if fs.ReadBody(records[0]) != body {
		t.Fatal("缓存应一致")
	}
}

// TestSkillIndexSnapshotFromDict 校验分支
func TestSkillIndexSnapshotFromDict(t *testing.T) {
	// 缺 fingerprint
	if _, err := SkillIndexSnapshotFromDict(map[string]any{"nodes": []any{}, "record_hashes": map[string]any{"a": "h"}}); err == nil {
		t.Fatal("缺 fingerprint 应报错")
	}
	// 空 record_hashes
	if _, err := SkillIndexSnapshotFromDict(map[string]any{"nodes": []any{}, "record_hashes": map[string]any{}, "fingerprint": "f"}); err == nil {
		t.Fatal("空 hashes 应报错")
	}
	// 合法单 leaf
	valid := map[string]any{
		"nodes":         []any{map[string]any{"cid": "1", "type": "leaf", "worker_id": "a"}},
		"record_hashes": map[string]any{"a": "h1"},
		"fingerprint":   "f",
	}
	snap, err := SkillIndexSnapshotFromDict(valid)
	if err != nil {
		t.Fatal(err)
	}
	if snap.TaxonomyFingerprint() == "" {
		t.Fatal("taxonomy 指纹应计算")
	}
	// ToDict 往返
	dict := snap.ToDict()
	snap2, err := SkillIndexSnapshotFromDict(dict)
	if err != nil || snap2.Fingerprint != "f" {
		t.Fatalf("往返不符: %v", err)
	}
	// 孤儿节点（leaf 无 parent branch）
	orphan := map[string]any{
		"nodes":         []any{map[string]any{"cid": "2.1", "type": "leaf", "worker_id": "a"}},
		"record_hashes": map[string]any{"a": "h1"},
		"fingerprint":   "f",
	}
	if _, err := SkillIndexSnapshotFromDict(orphan); err == nil {
		t.Fatal("孤儿节点应报错")
	}
	// leaf worker_id 不匹配 hashes
	mismatch := map[string]any{
		"nodes":         []any{map[string]any{"cid": "1", "type": "leaf", "worker_id": "b"}},
		"record_hashes": map[string]any{"a": "h1"},
		"fingerprint":   "f",
	}
	if _, err := SkillIndexSnapshotFromDict(mismatch); err == nil {
		t.Fatal("worker_id 不匹配应报错")
	}
}

// TestSkillFS_IndexedFresh 预置合法索引 → indexed 模式 + 分支行
func TestSkillFS_IndexedFresh(t *testing.T) {
	root := t.TempDir()
	indexRoot := t.TempDir()
	indexDir := filepath.Join(indexRoot, "index")
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 技能 a/b 归入分支 1
	longDesc := strings.Repeat("detailed description ", 220)
	makeSkillDir(t, root, "skill-a", "name: skill-a\ndescription: "+longDesc+" A\n", "body a")
	makeSkillDir(t, root, "skill-b", "name: skill-b\ndescription: "+longDesc+" B\n", "body b")
	// tree_index.yaml：分支+两 leaf
	_ = os.WriteFile(filepath.Join(indexDir, "tree_index.yaml"), []byte(
		"nodes:\n  - cid: 1\n    type: branch\n    description: Group One\n  - cid: 1.1\n    type: leaf\n    worker_id: skill-a\n    description: A\n  - cid: 1.2\n    type: leaf\n    worker_id: skill-b\n    description: B\n"), 0o644)
	_ = os.WriteFile(filepath.Join(indexDir, "catalog.jsonl"), []byte(
		`{"name": "skill-a", "cid": "1.1", "worker_id": "skill-a"}`+"\n"+`{"name": "skill-b", "cid": "1.2", "worker_id": "skill-b"}`+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(indexDir, "manifest.json"), []byte(`{"mode": "taxonomy", "count": 2, "worker_ids": ["skill-a", "skill-b"]}`), 0o644)
	commit := map[string]any{"fingerprint": "rev-1", "record_hashes": map[string]any{"skill-a": "hash-a", "skill-b": "hash-b"}}
	// 真实 content_hash
	inv, err := ScanSkillDirectories([]string{root}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	hashes := map[string]any{}
	for _, item := range inv.Items {
		hashes[item.WorkerID] = item.ContentHash
	}
	commit["record_hashes"] = hashes
	data, _ := json.Marshal(commit)
	_ = os.WriteFile(filepath.Join(indexDir, "commit.json"), data, 0o644)

	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: indexRoot, IndexRoot: indexRoot})
	if err != nil {
		t.Fatal(err)
	}
	art := fs.Artifact()
	if art.Layout != "tree" {
		t.Fatalf("有索引应 tree: %s", art.Layout)
	}
	if art.IndexState != "fresh" {
		t.Fatalf("哈希一致应 fresh: %s", art.IndexState)
	}
	snap := fs.PromptSnapshot(false)
	if snap.Mode != "indexed" {
		t.Fatalf("tree+fresh 应 indexed: %s (est=%d budget=%d)", snap.Mode, snap.EstimatedCandidateTokens, snap.CandidateBudgetTokens)
	}
	if len(snap.Branches) == 0 {
		t.Fatal("indexed 应有分支行")
	}
	// stale：改技能内容（hash 变）
	makeSkillDir(t, root, "skill-a", "name: skill-a\ndescription: "+longDesc+" A changed\n", "body a changed")
	inv2, _ := ScanSkillDirectories([]string{root}, nil, nil)
	fs2, err := NewSkillFS(func() (SkillInventory, error) { return inv2, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: indexRoot, IndexRoot: indexRoot})
	if err != nil {
		t.Fatal(err)
	}
	if fs2.Artifact().IndexState != "stale" {
		t.Fatalf("哈希不一致应 stale: %s", fs2.Artifact().IndexState)
	}
	snap2 := fs2.PromptSnapshot(false)
	if snap2.Mode != "indexed-stale" {
		t.Fatalf("tree+stale 应 indexed-stale: %s", snap2.Mode)
	}
	// stale 仍有分支行（不要求 fresh）
	if len(snap2.Branches) == 0 {
		t.Fatal("stale 也应有分支行")
	}
}

// TestCaptureIndexSnapshot 缺索引恒 nil（D-16-4-5 钉住）
func TestCaptureIndexSnapshot_缺索引(t *testing.T) {
	if CaptureIndexSnapshot(t.TempDir()) != nil {
		t.Fatal("无索引应返回 nil")
	}
}

// TestSegmentBase 段名安全化
func TestSegmentBase(t *testing.T) {
	if segmentBase("ok.name-1") != "ok.name-1" {
		t.Fatal("合法段应原样")
	}
	if segmentBase("a/b\\c!") != "a-b-c" { // 尾部 - 被 strip
		t.Fatalf("非法字符应替换: %q", segmentBase("a/b\\c!"))
	}
	if segmentBase("") != "unnamed" {
		t.Fatal("空应回落")
	}
}

// TestEstimateTokens 估算
func TestEstimateTokens(t *testing.T) {
	if estimateTokens("") != 0 {
		t.Fatal("空应 0")
	}
	if estimateTokens("abcd") != 1 {
		t.Fatalf("4 字符应 1: %d", estimateTokens("abcd"))
	}
	if estimateTokens("abcde") != 2 {
		t.Fatalf("5 字符应 2: %d", estimateTokens("abcde"))
	}
}

// TestSkillFS_Overlay 索引外技能进 overlay 分支
func TestSkillFS_Overlay(t *testing.T) {
	root := t.TempDir()
	indexRoot := t.TempDir()
	indexDir := filepath.Join(indexRoot, "index")
	_ = os.MkdirAll(indexDir, 0o755)
	makeSkillDir(t, root, "indexed-one", "name: indexed-one\ndescription: in index\n", "body")
	makeSkillDir(t, root, "fresh-one", "name: fresh-one\ndescription: not in index\n", "body")
	_ = os.WriteFile(filepath.Join(indexDir, "tree_index.yaml"), []byte(
		"nodes:\n  - cid: 1\n    type: branch\n  - cid: 1.1\n    type: leaf\n    worker_id: indexed-one\n    description: in index\n"), 0o644)
	_ = os.WriteFile(filepath.Join(indexDir, "catalog.jsonl"), []byte(
		`{"name": "indexed-one", "cid": "1.1", "worker_id": "indexed-one"}`+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(indexDir, "manifest.json"), []byte(`{"mode": "taxonomy", "count": 1, "worker_ids": ["indexed-one"]}`), 0o644)
	inv, _ := ScanSkillDirectories([]string{root}, nil, nil)
	hashes := map[string]any{}
	for _, item := range inv.Items {
		hashes[item.WorkerID] = item.ContentHash
	}
	// hashes 只含索引内技能（对齐 Python expected 只含 leaf worker）
	commitData, _ := json.Marshal(map[string]any{"fingerprint": "rev-1", "record_hashes": map[string]any{"indexed-one": inv.Items[0].ContentHash}})
	_ = os.WriteFile(filepath.Join(indexDir, "commit.json"), commitData, 0o644)
	_ = hashes

	settings := DefaultDiscoverySettings()
	settings.UseExistingIndex = true
	fs, err := NewSkillFS(func() (SkillInventory, error) { return inv, nil },
		SkillFSOptions{Settings: settings, ArtifactRoot: indexRoot, IndexRoot: indexRoot})
	if err != nil {
		t.Fatal(err)
	}
	art := fs.Artifact()
	if art.IndexState != "stale" {
		t.Fatalf("索引只含部分技能应 stale: %s", art.IndexState)
	}
	// overlay 分支存在
	view := fs.Directory()
	children, _ := view.Children("/")
	foundOverlay := false
	for _, c := range children {
		if c.Label == overlaySegment {
			foundOverlay = true
		}
	}
	if !foundOverlay {
		t.Fatalf("应有 newly_installed_skills overlay 分支: %+v", children)
	}
}
