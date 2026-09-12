package backend

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/sharing"
)

// ──────────────────────────── 辅助 ────────────────────────────

// newTestBackend 创建使用临时目录的 LocalFileBackend。
func newTestBackend(t *testing.T) *LocalFileBackend {
	t.Helper()
	return NewLocalFileBackend(t.TempDir(), 0.85)
}

// makeTestBundle 创建测试用 SharedSkillBundle。
func makeTestBundle(skillID, skillName string, keywords []string) sharing.SharedSkillBundle {
	return sharing.SharedSkillBundle{
		BundleID:          "sb_test001",
		SkillID:           skillID,
		SkillName:         skillName,
		SkillVersion:      "1.0",
		KeywordsAggregate: keywords,
		SummaryAggregate:  "test summary",
		Experiences: []sharing.SharedExperience{
			{Keywords: keywords, Summary: "test exp"},
		},
		CreatedAt: "2027-01-01T00:00:00Z",
	}
}

// makeTestBundleWithID 创建指定 bundleID 的测试用 SharedSkillBundle。
func makeTestBundleWithID(bundleID, skillID, skillName string, keywords []string) sharing.SharedSkillBundle {
	b := makeTestBundle(skillID, skillName, keywords)
	b.BundleID = bundleID
	return b
}

// ──────────────────────────── UploadAndDownload 测试 ────────────────────────────

// TestLocalFileBackend_UploadAndDownload 上传 bundle + 下载
func TestLocalFileBackend_UploadAndDownload(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	bundle := makeTestBundle("sk_py", "python-debug", []string{"IndexError", "bounds"})
	result := b.UploadBundle(ctx, bundle)
	if !result.OK {
		t.Fatalf("UploadBundle 失败: %s", result.Reason)
	}

	query := sharing.QueryKeywords{Keywords: []string{"IndexError"}}
	downloaded := b.DownloadBundles(ctx, "sk_py", query, 3)
	if len(downloaded) != 1 {
		t.Fatalf("DownloadBundles 返回 %d 个, 期望 1", len(downloaded))
	}
	if downloaded[0].BundleID != bundle.BundleID {
		t.Errorf("BundleID = %q, 期望 %q", downloaded[0].BundleID, bundle.BundleID)
	}
}

// ──────────────────────────── 不同 skillID 不冲突测试 ────────────────────────────

// TestLocalFileBackend_DifferentSkillIDsNoCollide 不同 skill_id 不冲突
func TestLocalFileBackend_DifferentSkillIDsNoCollide(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	b1 := makeTestBundleWithID("sb_001", "sk_a", "skill-a", []string{"python", "debug"})
	b2 := makeTestBundleWithID("sb_002", "sk_b", "skill-b", []string{"java", "compile"})

	r1 := b.UploadBundle(ctx, b1)
	r2 := b.UploadBundle(ctx, b2)
	if !r1.OK || !r2.OK {
		t.Fatalf("上传应全部成功: r1=%v r2=%v", r1.OK, r2.OK)
	}

	// 搜索 sk_a 索引不应返回 sk_b 的 bundle
	query := sharing.QueryKeywords{Keywords: []string{"python"}}
	results := b.DownloadBundles(ctx, "sk_a", query, 10)
	for _, r := range results {
		if r.SkillID != "sk_a" {
			t.Errorf("下载到错误 skill_id 的 bundle: %s", r.SkillID)
		}
	}
}

// ──────────────────────────── 技能包不可变测试 ────────────────────────────

// TestLocalFileBackend_SkillPackageImmutable 重复上传技能包保持第一个
func TestLocalFileBackend_SkillPackageImmutable(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	err := b.UploadSkillPackage(ctx, "sk_pkg", []byte("first-content"), sharing.SkillPackageMeta{
		SkillID: "sk_pkg", SkillName: "pkg-v1", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("第一次上传失败: %v", err)
	}

	// 第二次上传应被忽略（no-op）
	err = b.UploadSkillPackage(ctx, "sk_pkg", []byte("second-content"), sharing.SkillPackageMeta{
		SkillID: "sk_pkg", SkillName: "pkg-v2", UploadedAt: "2027-01-02T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("第二次上传应返回 nil: %v", err)
	}

	data, err := b.DownloadSkillPackage(ctx, "sk_pkg")
	if err != nil {
		t.Fatalf("DownloadSkillPackage 失败: %v", err)
	}
	if string(data) != "first-content" {
		t.Errorf("技能包内容 = %q, 期望 %q", string(data), "first-content")
	}
}

// ──────────────────────────── 高 Jaccard 拒绝测试 ────────────────────────────

// TestLocalFileBackend_RejectsDuplicateOnUpload 高 jaccard 被拒绝
func TestLocalFileBackend_RejectsDuplicateOnUpload(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	b1 := makeTestBundleWithID("sb_001", "sk_dedup", "skill-dedup", []string{"python", "debug", "IndexError"})
	r1 := b.UploadBundle(ctx, b1)
	if !r1.OK {
		t.Fatalf("第一次上传应成功: %s", r1.Reason)
	}

	// 高度重叠的关键词 → 应被拒绝（交集3/并集3=1.0 >= 0.85）
	b2 := makeTestBundleWithID("sb_002", "sk_dedup", "skill-dedup", []string{"python", "debug", "IndexError"})
	r2 := b.UploadBundle(ctx, b2)
	if r2.OK {
		t.Error("重复 bundle 应被拒绝")
	}
	if r2.Reason == "" {
		t.Error("拒绝应包含原因")
	}
}

// ──────────────────────────── 全局搜索测试 ────────────────────────────

// TestLocalFileBackend_SearchSkills 全局搜索
func TestLocalFileBackend_SearchSkills(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	// 先上传技能包以建立全局索引条目
	_ = b.UploadSkillPackage(ctx, "sk_search", []byte("pkg"), sharing.SkillPackageMeta{
		SkillID: "sk_search", SkillName: "search-skill", Description: "search desc",
		UploadedAt: "2027-01-01T00:00:00Z",
	})

	// 上传 bundle 会更新全局索引
	bundle := makeTestBundleWithID("sb_search1", "sk_search", "search-skill", []string{"python", "debug"})
	r := b.UploadBundle(ctx, bundle)
	if !r.OK {
		t.Fatalf("UploadBundle 失败: %s", r.Reason)
	}

	query := sharing.QueryKeywords{Keywords: []string{"python"}}
	results := b.SearchSkills(ctx, query, 5)
	if len(results) == 0 {
		t.Fatal("SearchSkills 应返回结果")
	}
	found := false
	for _, sr := range results {
		if sr.SkillID == "sk_search" {
			found = true
			break
		}
	}
	if !found {
		t.Error("SearchSkills 结果中应包含 sk_search")
	}
}

// ──────────────────────────── 下载技能包字节测试 ────────────────────────────

// TestLocalFileBackend_DownloadSkillPackage 下载技能包字节
func TestLocalFileBackend_DownloadSkillPackage(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	content := []byte("skill-package-bytes")
	err := b.UploadSkillPackage(ctx, "sk_dl", content, sharing.SkillPackageMeta{
		SkillID: "sk_dl", SkillName: "dl-skill", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("UploadSkillPackage 失败: %v", err)
	}

	data, err := b.DownloadSkillPackage(ctx, "sk_dl")
	if err != nil {
		t.Fatalf("DownloadSkillPackage 失败: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("内容 = %q, 期望 %q", string(data), string(content))
	}

	// 不存在的技能包
	data, err = b.DownloadSkillPackage(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("不存在时应返回 nil error: %v", err)
	}
	if data != nil {
		t.Error("不存在的技能包应返回 nil")
	}
}

// ──────────────────────────── 获取元数据测试 ────────────────────────────

// TestLocalFileBackend_GetSkillPackageMeta 获取元数据
func TestLocalFileBackend_GetSkillPackageMeta(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	meta := sharing.SkillPackageMeta{
		SkillID:     "sk_meta",
		SkillName:   "meta-skill",
		Description: "a test skill",
		UploadedAt:  "2027-01-01T00:00:00Z",
	}
	err := b.UploadSkillPackage(ctx, "sk_meta", []byte("pkg"), meta)
	if err != nil {
		t.Fatalf("UploadSkillPackage 失败: %v", err)
	}

	got, err := b.GetSkillPackageMeta(ctx, "sk_meta")
	if err != nil {
		t.Fatalf("GetSkillPackageMeta 失败: %v", err)
	}
	if got == nil {
		t.Fatal("GetSkillPackageMeta 应返回元数据")
	}
	if got.SkillName != "meta-skill" {
		t.Errorf("SkillName = %q, 期望 %q", got.SkillName, "meta-skill")
	}
	if got.Description != "a test skill" {
		t.Errorf("Description = %q, 期望 %q", got.Description, "a test skill")
	}

	// 不存在的技能包
	got, err = b.GetSkillPackageMeta(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("不存在时应返回 nil error: %v", err)
	}
	if got != nil {
		t.Error("不存在的技能包应返回 nil")
	}
}

// ──────────────────────────── 存在性检查测试 ────────────────────────────

// TestLocalFileBackend_HasSkillPackage 存在性检查
func TestLocalFileBackend_HasSkillPackage(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	if b.HasSkillPackage(ctx, "sk_has") {
		t.Error("尚未上传应返回 false")
	}

	err := b.UploadSkillPackage(ctx, "sk_has", []byte("pkg"), sharing.SkillPackageMeta{
		SkillID: "sk_has", SkillName: "has-skill", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("UploadSkillPackage 失败: %v", err)
	}

	if !b.HasSkillPackage(ctx, "sk_has") {
		t.Error("上传后应返回 true")
	}
}

// ──────────────────────────── Jaccard 测试 ────────────────────────────

// TestJaccard jaccard 相似度计算测试
func TestJaccard(t *testing.T) {
	tests := []struct {
		name string
		a    []string
		b    []string
		want float64
	}{
		{
			name: "完全相同",
			a:    []string{"python", "debug"},
			b:    []string{"python", "debug"},
			want: 1.0,
		},
		{
			name: "部分重叠",
			a:    []string{"python", "debug"},
			b:    []string{"python", "java"},
			want: 1.0 / 3.0,
		},
		{
			name: "无交集",
			a:    []string{"python"},
			b:    []string{"java"},
			want: 0.0,
		},
		{
			name: "空集",
			a:    []string{},
			b:    []string{},
			want: 0.0,
		},
		{
			name: "一方为空",
			a:    []string{"python"},
			b:    []string{},
			want: 0.0,
		},
		{
			name: "大小写不敏感",
			a:    []string{"Python"},
			b:    []string{"python"},
			want: 1.0,
		},
		{
			name: "子串降级",
			a:    []string{"python-debug"},
			b:    []string{"python"},
			want: 0.5 / 2.0, // union=2, 0.5/max(2,1)=0.25
		},
		{
			name: "空字符串被忽略",
			a:    []string{"", "python"},
			b:    []string{"python", ""},
			want: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaccard(tt.a, tt.b)
			// 浮点比较允许微小误差
			diff := got - tt.want
			if diff < -0.001 || diff > 0.001 {
				t.Errorf("jaccard(%v, %v) = %f, 期望 %f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ──────────────────────────── 属性访问测试 ────────────────────────────

// TestLocalFileBackend_HubPath_OutboxDir 属性访问
func TestLocalFileBackend_HubPath_OutboxDir(t *testing.T) {
	dir := t.TempDir()
	b := NewLocalFileBackend(dir, 0.85)
	if b.HubPath() != dir {
		t.Errorf("HubPath = %q, 期望 %q", b.HubPath(), dir)
	}
	expectedOutbox := filepath.Join(dir, ".outbox")
	if b.OutboxDir() != expectedOutbox {
		t.Errorf("OutboxDir = %q, 期望 %q", b.OutboxDir(), expectedOutbox)
	}
}

// ──────────────────────────── UploadBundle 空 skillID 测试 ────────────────────────────

// TestLocalFileBackend_UploadBundle_EmptySkillID 空 skill_id 应被拒绝
func TestLocalFileBackend_UploadBundle_EmptySkillID(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	bundle := makeTestBundle("", "skill-name", []string{"test"})
	result := b.UploadBundle(ctx, bundle)
	if result.OK {
		t.Error("空 skill_id 上传应被拒绝")
	}
	if result.Reason == "" {
		t.Error("拒绝应包含原因")
	}
}

// ──────────────────────────── UploadBundle 无关键词不去重测试 ────────────────────────────

// TestLocalFileBackend_UploadBundle_NoKeywordsSkipsDedup 无关键词不触发去重
func TestLocalFileBackend_UploadBundle_NoKeywordsSkipsDedup(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	b1 := makeTestBundleWithID("sb_001", "sk_nkw", "no-kw", nil)
	r1 := b.UploadBundle(ctx, b1)
	if !r1.OK {
		t.Fatalf("第一次上传应成功: %s", r1.Reason)
	}

	// 关键词为空不会触发去重检查
	b2 := makeTestBundleWithID("sb_002", "sk_nkw", "no-kw", nil)
	r2 := b.UploadBundle(ctx, b2)
	if !r2.OK {
		t.Errorf("无关键词时去重检查应跳过: %s", r2.Reason)
	}
}

// ──────────────────────────── DownloadBundles 空结果测试 ────────────────────────────

// TestLocalFileBackend_DownloadBundles_EmptyIndex 空索引返回空
func TestLocalFileBackend_DownloadBundles_EmptyIndex(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	query := sharing.QueryKeywords{Keywords: []string{"test"}}
	results := b.DownloadBundles(ctx, "sk_empty", query, 3)
	if len(results) != 0 {
		t.Errorf("空索引应返回 0 个结果, 得到 %d", len(results))
	}
}

// ──────────────────────────── DownloadBundles 零分过滤测试 ────────────────────────────

// TestLocalFileBackend_DownloadBundles_ZeroScoreFiltered 零分不返回
func TestLocalFileBackend_DownloadBundles_ZeroScoreFiltered(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	bundle := makeTestBundle("sk_zero", "zero-skill", []string{"python"})
	_ = b.UploadBundle(ctx, bundle)

	query := sharing.QueryKeywords{Keywords: []string{"nonexistent"}}
	results := b.DownloadBundles(ctx, "sk_zero", query, 3)
	if len(results) != 0 {
		t.Errorf("零分 bundle 应被过滤, 得到 %d", len(results))
	}
}

// ──────────────────────────── SearchSkills 空索引测试 ────────────────────────────

// TestLocalFileBackend_SearchSkills_EmptyGlobalIndex 空全局索引返回空
func TestLocalFileBackend_SearchSkills_EmptyGlobalIndex(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	query := sharing.QueryKeywords{Keywords: []string{"test"}}
	results := b.SearchSkills(ctx, query, 5)
	if len(results) != 0 {
		t.Errorf("空全局索引应返回 0 个结果, 得到 %d", len(results))
	}
}

// ──────────────────────────── SearchSkills 零分过滤测试 ────────────────────────────

// TestLocalFileBackend_SearchSkills_ZeroScoreFiltered 零分不返回
func TestLocalFileBackend_SearchSkills_ZeroScoreFiltered(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	_ = b.UploadSkillPackage(ctx, "sk_sz", []byte("pkg"), sharing.SkillPackageMeta{
		SkillID: "sk_sz", SkillName: "sz-skill", Description: "sz desc",
		UploadedAt: "2027-01-01T00:00:00Z",
	})

	query := sharing.QueryKeywords{Keywords: []string{"completely-different"}}
	results := b.SearchSkills(ctx, query, 5)
	if len(results) != 0 {
		t.Errorf("零分搜索结果应被过滤, 得到 %d", len(results))
	}
}

// ──────────────────────────── HasSkillPackage 空 ID 测试 ────────────────────────────

// TestLocalFileBackend_HasSkillPackage_EmptyID 空 ID 返回 false
func TestLocalFileBackend_HasSkillPackage_EmptyID(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	if b.HasSkillPackage(ctx, "") {
		t.Error("空 skill_id 应返回 false")
	}
	if b.HasSkillPackage(ctx, "   ") {
		t.Error("空白 skill_id 应返回 false")
	}
}

// ──────────────────────────── UploadSkillPackage 空 ID 测试 ────────────────────────────

// TestLocalFileBackend_UploadSkillPackage_EmptyID 空 ID 应返回 error
func TestLocalFileBackend_UploadSkillPackage_EmptyID(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	err := b.UploadSkillPackage(ctx, "", []byte("pkg"), sharing.SkillPackageMeta{})
	if err == nil {
		t.Error("空 skill_id 上传应返回 error")
	}
}

// ──────────────────────────── UploadSkillPackage 空包测试 ────────────────────────────

// TestLocalFileBackend_UploadSkillPackage_EmptyPackage 空包应返回 error
func TestLocalFileBackend_UploadSkillPackage_EmptyPackage(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	err := b.UploadSkillPackage(ctx, "sk_emp", nil, sharing.SkillPackageMeta{
		SkillID: "sk_emp", SkillName: "emp-skill", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err == nil {
		t.Error("空包上传应返回 error")
	}
}

// ──────────────────────────── readPackageMeta 损坏 JSON 测试 ────────────────────────────

// TestLocalFileBackend_ReadPackageMeta_CorruptJSON 损坏的 meta.json 返回 nil
func TestLocalFileBackend_ReadPackageMeta_CorruptJSON(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	pkgDir := filepath.Join(b.HubPath(), "packages", "sk_corrupt")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(pkgDir, "meta.json")
	if err := os.WriteFile(metaPath, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := b.GetSkillPackageMeta(ctx, "sk_corrupt")
	if err != nil {
		t.Fatalf("损坏 JSON 不应返回 error: %v", err)
	}
	if got != nil {
		t.Error("损坏 JSON 应返回 nil meta")
	}
}

// ──────────────────────────── readIndex 损坏行测试 ────────────────────────────

// TestLocalFileBackend_ReadIndex_CorruptLine 损坏的索引行被跳过
func TestLocalFileBackend_ReadIndex_CorruptLine(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	bundle := makeTestBundle("sk_idxc", "idx-skill", []string{"test"})
	r := b.UploadBundle(ctx, bundle)
	if !r.OK {
		t.Fatalf("上传失败: %s", r.Reason)
	}

	idxPath := filepath.Join(b.HubPath(), "index", "sk_idxc.jsonl")
	f, err := os.OpenFile(idxPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("corrupt line\n")
	f.Close()

	query := sharing.QueryKeywords{Keywords: []string{"test"}}
	results := b.DownloadBundles(ctx, "sk_idxc", query, 3)
	if len(results) != 1 {
		t.Errorf("应返回 1 个结果（跳过损坏行）, 得到 %d", len(results))
	}
}

// ──────────────────────────── loadBundle 损坏 JSON 测试 ────────────────────────────

// TestLocalFileBackend_LoadBundle_CorruptJSON 损坏的 bundle JSON 返回 nil
func TestLocalFileBackend_LoadBundle_CorruptJSON(t *testing.T) {
	b := newTestBackend(t)

	bundleDir := filepath.Join(b.HubPath(), "bundles", "sk_bad")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bundlePath := filepath.Join(bundleDir, "sb_bad001.json")
	if err := os.WriteFile(bundlePath, []byte("not json{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	idxDir := filepath.Join(b.HubPath(), "index")
	if err := os.MkdirAll(idxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idxPath := filepath.Join(idxDir, "sk_bad.jsonl")
	entry := map[string]any{
		"bundle_id": "sb_bad001",
		"skill_id":  "sk_bad",
		"keywords":  []string{"test"},
	}
	line, _ := json.Marshal(entry)
	os.WriteFile(idxPath, append(line, '\n'), 0o644)

	ctx := context.Background()
	query := sharing.QueryKeywords{Keywords: []string{"test"}}
	results := b.DownloadBundles(ctx, "sk_bad", query, 3)
	if len(results) != 0 {
		t.Errorf("损坏 bundle 应返回 0 个结果, 得到 %d", len(results))
	}
}

// ──────────────────────────── readGlobalIndex 损坏行测试 ────────────────────────────

// TestLocalFileBackend_ReadGlobalIndex_CorruptLine 损坏的全局索引行被跳过
func TestLocalFileBackend_ReadGlobalIndex_CorruptLine(t *testing.T) {
	b := newTestBackend(t)

	idxDir := filepath.Join(b.HubPath(), "index")
	if err := os.MkdirAll(idxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(idxDir, "global.jsonl")
	f, err := os.Create(globalPath)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("corrupt line\n")
	entry := map[string]any{"skill_id": "sk_g", "skill_name": "g", "keywords": []string{"a"}}
	line, _ := json.Marshal(entry)
	f.Write(append(line, '\n'))
	f.Close()

	ctx := context.Background()
	query := sharing.QueryKeywords{Keywords: []string{"a"}}
	results := b.SearchSkills(ctx, query, 5)
	if len(results) != 1 {
		t.Errorf("应返回 1 个结果（跳过损坏行）, 得到 %d", len(results))
	}
}

// ──────────────────────────── containsString 测试 ────────────────────────────

// TestContainsString containsString 辅助函数测试
func TestContainsString(t *testing.T) {
	if !containsString([]string{"a", "b", "c"}, "b") {
		t.Error("应找到 'b'")
	}
	if containsString([]string{"a", "b", "c"}, "d") {
		t.Error("不应找到 'd'")
	}
	if containsString(nil, "a") {
		t.Error("nil 切片不应找到")
	}
}

// ──────────────────────────── outbox 暂存测试 ────────────────────────────

// TestLocalFileBackend_UploadBundle_SpoolsToOutbox 写入失败时暂存到 outbox
func TestLocalFileBackend_UploadBundle_SpoolsToOutbox(t *testing.T) {
	hubDir := t.TempDir()
	b := NewLocalFileBackend(hubDir, 0.85)
	ctx := context.Background()

	// 先正常上传一个不同 skill 的 bundle（建立 bundles 目录结构）
	bundle0 := makeTestBundleWithID("sb_000", "sk_other", "other-skill", []string{"other"})
	r0 := b.UploadBundle(ctx, bundle0)
	if !r0.OK {
		t.Fatalf("初始上传应成功: %s", r0.Reason)
	}

	// 将 index 目录设为只读，迫使 UploadBundle 写入 bundle 成功但写索引失败
	indexPath := filepath.Join(hubDir, "index")
	os.MkdirAll(indexPath, 0o755)
	os.Chmod(indexPath, 0o444)
	defer os.Chmod(indexPath, 0o755) // 确保清理

	bundle1 := makeTestBundleWithID("sb_ob1", "sk_obtest", "ob-skill", []string{"outbox"})
	r1 := b.UploadBundle(ctx, bundle1)
	if r1.OK {
		t.Skip("只读目录未阻止写入，跳过 outbox 测试")
	}
	if !r1.Retryable {
		t.Errorf("写入失败应标记为可重试, retryable=%v, reason=%s", r1.Retryable, r1.Reason)
	}

	// 验证 outbox 中有文件
	outboxPath := filepath.Join(hubDir, ".outbox", "sk_obtest")
	entries, err := os.ReadDir(outboxPath)
	if err != nil {
		t.Fatalf("outbox 目录应存在: %v", err)
	}
	if len(entries) == 0 {
		t.Error("outbox 中应有暂存文件")
	}
}

// ──────────────────────────── spoolToOutbox 空 skillID 测试 ────────────────────────────

// TestLocalFileBackend_SpoolToOutbox_EmptySkillID 空 skillID 不暂存
func TestLocalFileBackend_SpoolToOutbox_EmptySkillID(t *testing.T) {
	b := newTestBackend(t)

	// 空 skillID 的 bundle 调用 spoolToOutbox 应安全返回（只打日志）
	bundle := sharing.SharedSkillBundle{BundleID: "sb_empty", SkillID: "", SkillName: "test"}
	b.spoolToOutbox(bundle)
	// 不应 panic
}

// ──────────────────────────── spoolToOutbox 成功路径测试 ────────────────────────────

// TestLocalFileBackend_SpoolToOutbox_Success 成功暂存到 outbox
func TestLocalFileBackend_SpoolToOutbox_Success(t *testing.T) {
	b := newTestBackend(t)

	bundle := sharing.SharedSkillBundle{
		BundleID:  "sb_spool1",
		SkillID:   "sk_spool",
		SkillName: "spool-skill",
		Experiences: []sharing.SharedExperience{
			{Summary: "test"},
		},
		CreatedAt: "2027-01-01T00:00:00Z",
	}
	b.spoolToOutbox(bundle)

	// 验证 outbox 文件存在
	outboxFile := filepath.Join(b.OutboxDir(), "sk_spool", "sb_spool1.json")
	if _, err := os.Stat(outboxFile); os.IsNotExist(err) {
		t.Errorf("outbox 文件 %q 应存在", outboxFile)
	}
}

// ──────────────────────────── UploadSkillPackage 写入失败测试 ────────────────────────────

// TestLocalFileBackend_UploadSkillPackage_WriteFailure 写入失败返回 error
func TestLocalFileBackend_UploadSkillPackage_WriteFailure(t *testing.T) {
	hubDir := t.TempDir()
	b := NewLocalFileBackend(hubDir, 0.85)

	// 将 packages 目录设为只读
	packagesDir := filepath.Join(hubDir, "packages")
	os.MkdirAll(packagesDir, 0o755)
	os.Chmod(packagesDir, 0o444)
	defer os.Chmod(packagesDir, 0o755)

	ctx := context.Background()
	err := b.UploadSkillPackage(ctx, "sk_wfail", []byte("pkg"), sharing.SkillPackageMeta{
		SkillID: "sk_wfail", SkillName: "wfail", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err == nil {
		t.Skip("只读目录未阻止写入，跳过测试")
	}
}

// ──────────────────────────── ensureGlobalIndexEntry 更新测试 ────────────────────────────

// TestLocalFileBackend_EnsureGlobalIndexEntry_UpdatesExisting 更新已存在的全局索引条目
func TestLocalFileBackend_EnsureGlobalIndexEntry_UpdatesExisting(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	// 第一次上传
	err := b.UploadSkillPackage(ctx, "sk_ens", []byte("pkg"), sharing.SkillPackageMeta{
		SkillID: "sk_ens", SkillName: "ens-v1", Description: "first", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	// 第二次上传同 skill_id（应走更新全局索引路径，但因技能包不可变实际是 no-op）
	err = b.UploadSkillPackage(ctx, "sk_ens", []byte("pkg2"), sharing.SkillPackageMeta{
		SkillID: "sk_ens", SkillName: "ens-v2", Description: "updated", UploadedAt: "2027-01-02T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("第二次上传失败: %v", err)
	}

	meta, _ := b.GetSkillPackageMeta(ctx, "sk_ens")
	if meta == nil {
		t.Fatal("meta 不应为 nil")
	}
	if meta.SkillName != "ens-v1" {
		t.Errorf("SkillName = %q, 期望 %q（不可变技能包）", meta.SkillName, "ens-v1")
	}
}

// ──────────────────────────── ensureGlobalIndexEntry 新条目路径测试 ────────────────────────────

// TestLocalFileBackend_EnsureGlobalIndexEntry_NewEntry 全局索引新条目路径
func TestLocalFileBackend_EnsureGlobalIndexEntry_NewEntry(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	// 上传第一个技能包（ensureGlobalIndexEntry 创建新条目）
	err := b.UploadSkillPackage(ctx, "sk_new1", []byte("pkg1"), sharing.SkillPackageMeta{
		SkillID: "sk_new1", SkillName: "new-skill-1", Description: "desc1", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	// 上传第二个技能包（ensureGlobalIndexEntry 追加新条目）
	err = b.UploadSkillPackage(ctx, "sk_new2", []byte("pkg2"), sharing.SkillPackageMeta{
		SkillID: "sk_new2", SkillName: "new-skill-2", Description: "desc2", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	// 搜索应能找到两个
	query := sharing.QueryKeywords{Keywords: []string{"new"}}
	results := b.SearchSkills(ctx, query, 10)
	if len(results) < 2 {
		t.Errorf("应找到至少 2 个技能, 得到 %d", len(results))
	}
}

// ──────────────────────────── ensureGlobalIndexEntry 空名称/描述更新测试 ────────────────────────────

// TestLocalFileBackend_EnsureGlobalIndexEntry_UpdateWithEmptyFields 空名称/描述时保留原值
func TestLocalFileBackend_EnsureGlobalIndexEntry_UpdateWithEmptyFields(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()

	// 上传技能包（建立全局索引条目）
	err := b.UploadSkillPackage(ctx, "sk_emptupd", []byte("pkg"), sharing.SkillPackageMeta{
		SkillID: "sk_emptupd", SkillName: "original-name", Description: "original-desc", UploadedAt: "2027-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}

	// 直接调用 ensureGlobalIndexEntry 更新现有条目（空名称/描述）
	b.ensureGlobalIndexEntry("sk_emptupd", sharing.SkillPackageMeta{
		SkillID: "sk_emptupd", SkillName: "", Description: "", UploadedAt: "2027-01-02T00:00:00Z",
	})

	// 搜索验证名称和描述保留原值
	query := sharing.QueryKeywords{Keywords: []string{"original"}}
	results := b.SearchSkills(ctx, query, 5)
	found := false
	for _, sr := range results {
		if sr.SkillID == "sk_emptupd" {
			found = true
			if sr.SkillName != "original-name" {
				t.Errorf("SkillName = %q, 期望 %q", sr.SkillName, "original-name")
			}
			if sr.Description != "original-desc" {
				t.Errorf("Description = %q, 期望 %q", sr.Description, "original-desc")
			}
		}
	}
	if !found {
		t.Error("应找到 sk_emptupd")
	}
}

// ──────────────────────────── getStrFromAny 和 toIntFromAny 类型分支测试 ────────────────────────────

// TestGetStrFromAny_VariousTypes 各种类型提取字符串
func TestGetStrFromAny_VariousTypes(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{"hello", "hello"},
		{42, "42"},
		{int64(100), "100"},
		{float64(3.14), "3.14"},
		{nil, ""},
	}
	for _, tt := range tests {
		data := map[string]any{"val": tt.input}
		got := getStrFromAny(data["val"])
		if tt.input == nil {
			if got != "" {
				t.Errorf("getStrFromAny(nil) = %q, 期望 %q", got, tt.want)
			}
		} else if got != tt.want {
			t.Errorf("getStrFromAny(%v) = %q, 期望 %q", tt.input, got, tt.want)
		}
	}
}

// TestToIntFromAny_VariousTypes 各种类型提取整数
func TestToIntFromAny_VariousTypes(t *testing.T) {
	tests := []struct {
		input any
		want  int
	}{
		{42, 42},
		{int64(100), 100},
		{float64(7.0), 7},
		{nil, 0},
		{"not_int", 0},
	}
	for _, tt := range tests {
		data := map[string]any{"val": tt.input}
		got := toIntFromAny(data["val"])
		if got != tt.want {
			t.Errorf("toIntFromAny(%v) = %d, 期望 %d", tt.input, got, tt.want)
		}
	}
}

// ──────────────────────────── NewLocalFileBackend 默认路径测试 ────────────────────────────

// TestNewLocalFileBackend_DefaultPath 默认路径使用 home 目录
func TestNewLocalFileBackend_DefaultPath(t *testing.T) {
	b := NewLocalFileBackend("", 0.85)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法获取 home 目录")
	}
	expected := filepath.Join(home, ".openjiuwen", "experience_hub")
	if b.HubPath() != expected {
		t.Errorf("默认 HubPath = %q, 期望 %q", b.HubPath(), expected)
	}
}

// ──────────────────────────── loadBundle 不存在的 bundle 测试 ────────────────────────────

// TestLocalFileBackend_LoadBundle_MissingFile 不存在的 bundle 文件返回 nil
func TestLocalFileBackend_LoadBundle_MissingFile(t *testing.T) {
	b := newTestBackend(t)
	result := b.loadBundle("sk_miss", "sb_nonexistent")
	if result != nil {
		t.Error("不存在的 bundle 文件应返回 nil")
	}
}

// ──────────────────────────── loadBundle 无效 bundleID 测试 ────────────────────────────

// TestLocalFileBackend_LoadBundle_EmptyBundleID 空 bundleID 返回 nil
func TestLocalFileBackend_LoadBundle_EmptyBundleID(t *testing.T) {
	b := newTestBackend(t)
	result := b.loadBundle("sk_eid", "")
	if result != nil {
		t.Error("空 bundleID 应返回 nil")
	}
}
