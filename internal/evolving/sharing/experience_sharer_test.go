package sharing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ──────────────────────────── 辅助 ────────────────────────────

// mockBackend 内存模拟 SharingBackend，用于单元测试。
type mockBackend struct {
	mu             sync.RWMutex
	bundles        map[string][]SharedSkillBundle  // skillID → bundles
	skillPackages  map[string][]byte               // skillID → packageBytes
	skillMetas     map[string]SkillPackageMeta      // skillID → meta
	uploadResults  []UploadResult                   // 每次 UploadBundle 返回的结果队列
	uploadCallIdx  int
}

func newMockBackend() *mockBackend {
	return &mockBackend{
		bundles:       make(map[string][]SharedSkillBundle),
		skillPackages: make(map[string][]byte),
		skillMetas:    make(map[string]SkillPackageMeta),
	}
}

func (m *mockBackend) UploadBundle(_ context.Context, bundle SharedSkillBundle) UploadResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.uploadResults) > 0 && m.uploadCallIdx < len(m.uploadResults) {
		r := m.uploadResults[m.uploadCallIdx]
		m.uploadCallIdx++
		if r.OK {
			m.bundles[bundle.SkillID] = append(m.bundles[bundle.SkillID], bundle)
		}
		return r
	}
	m.bundles[bundle.SkillID] = append(m.bundles[bundle.SkillID], bundle)
	return UploadResult{OK: true, BundleID: bundle.BundleID}
}

func (m *mockBackend) DownloadBundles(_ context.Context, skillID string, query QueryKeywords, topK int) []SharedSkillBundle {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bundles := m.bundles[skillID]
	if topK < len(bundles) {
		return bundles[:topK]
	}
	return bundles
}

func (m *mockBackend) HasSkillPackage(_ context.Context, skillID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.skillPackages[skillID]
	return ok
}

func (m *mockBackend) UploadSkillPackage(_ context.Context, skillID string, packageBytes []byte, meta SkillPackageMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.skillPackages[skillID]; ok {
		return nil
	}
	m.skillPackages[skillID] = packageBytes
	m.skillMetas[skillID] = meta
	return nil
}

func (m *mockBackend) DownloadSkillPackage(_ context.Context, skillID string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.skillPackages[skillID]
	if !ok {
		return nil, nil
	}
	return data, nil
}

func (m *mockBackend) GetSkillPackageMeta(_ context.Context, skillID string) (*SkillPackageMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	meta, ok := m.skillMetas[skillID]
	if !ok {
		return nil, nil
	}
	return &meta, nil
}

func (m *mockBackend) SearchSkills(_ context.Context, query QueryKeywords, topK int) []SkillSearchResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var results []SkillSearchResult
	for id, meta := range m.skillMetas {
		results = append(results, SkillSearchResult{
			SkillID:     id,
			SkillName:   meta.SkillName,
			Description: meta.Description,
			Score:       1.0,
		})
		if len(results) >= topK {
			break
		}
	}
	return results
}

// newTestSharer 创建使用 mockBackend 的 ExperienceSharer。
func newTestSharer(t *testing.T) (*ExperienceSharer, *mockBackend, string) {
	t.Helper()
	cacheDir := t.TempDir()
	bk := newMockBackend()
	es := NewExperienceSharer(bk, cacheDir, 3, 0.01, nil)
	return es, bk, cacheDir
}

// fakeProvider 创建模拟的 SkillSharingContextProvider。
func fakeProvider(skillID string, pkgBytes []byte, resolvedName, description string) SkillSharingContextProvider {
	return func(_ context.Context, _ string) (string, []byte, string, string, error) {
		return skillID, pkgBytes, resolvedName, description, nil
	}
}

// makeExp 创建测试用 SharedExperience 指针。
func makeExp(recordID string, keywords []string) *SharedExperience {
	record := makeTestRecord(0.8, "test")
	record.ID = recordID
	return &SharedExperience{
		Record:   record,
		Keywords: keywords,
		Summary:  "test summary",
	}
}

// ──────────────────────────── StageForUpload 去重测试 ────────────────────────────

// TestExperienceSharer_StageForUpload_Dedup 按 (skill, record_id) 去重
func TestExperienceSharer_StageForUpload_Dedup(t *testing.T) {
	es, _, _ := newTestSharer(t)

	exp1 := makeExp("rec_001", []string{"a"})
	exp2 := makeExp("rec_001", []string{"b"}) // 同 record_id，应去重
	exp3 := makeExp("rec_002", []string{"c"}) // 不同 record_id

	es.StageForUpload("skill_x", exp1)
	es.StageForUpload("skill_x", exp2) // 去重
	es.StageForUpload("skill_x", exp3)

	if !es.HasPending("skill_x") {
		t.Fatal("应有待上传")
	}
	es.mu.RLock()
	queue := es.pendingUploads["skill_x"]
	es.mu.RUnlock()
	if len(queue) != 2 {
		t.Errorf("队列长度 = %d, 期望 2（去重后）", len(queue))
	}
}

// TestExperienceSharer_StageForUpload_EmptySkillName 空技能名称不入队
func TestExperienceSharer_StageForUpload_EmptySkillName(t *testing.T) {
	es, _, _ := newTestSharer(t)

	exp := makeExp("rec_001", []string{"a"})
	es.StageForUpload("", exp)

	if es.HasPending("") {
		t.Error("空技能名称不应入队")
	}
}

// TestExperienceSharer_StageForUpload_NilExp nil SharedExperience 不入队
func TestExperienceSharer_StageForUpload_NilExp(t *testing.T) {
	es, _, _ := newTestSharer(t)

	es.StageForUpload("skill_x", nil)
	if es.HasPending("skill_x") {
		t.Error("nil SharedExperience 不应入队")
	}
}

// ──────────────────────────── DiscardPendingUploads 测试 ────────────────────────────

// TestExperienceSharer_DiscardPendingUploads 丢弃队列
func TestExperienceSharer_DiscardPendingUploads(t *testing.T) {
	es, _, _ := newTestSharer(t)

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	es.StageForUpload("skill_x", makeExp("r2", []string{"b"}))

	count := es.DiscardPendingUploads("skill_x")
	if count != 2 {
		t.Errorf("丢弃数量 = %d, 期望 2", count)
	}
	if es.HasPending("skill_x") {
		t.Error("丢弃后不应有待上传")
	}

	count = es.DiscardPendingUploads("nonexistent")
	if count != 0 {
		t.Errorf("不存在的技能丢弃数量 = %d, 期望 0", count)
	}
}

// ──────────────────────────── FlushPendingUploads 测试 ────────────────────────────

// TestExperienceSharer_FlushPendingUploads_UploadsInitialPackage 首次上传技能包
func TestExperienceSharer_FlushPendingUploads_UploadsInitialPackage(t *testing.T) {
	es, bk, _ := newTestSharer(t)
	ctx := context.Background()

	es.SetSkillSharingContextProvider(fakeProvider("sk_init", []byte("pkg-bytes"), "resolved-name", "desc"))

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if !result.OK {
		t.Fatalf("FlushPendingUploads 失败: %s", result.Reason)
	}

	if !bk.HasSkillPackage(ctx, "sk_init") {
		t.Error("首次 flush 后 Hub 应已有技能包")
	}
}

// TestExperienceSharer_FlushPendingUploads_SkillIDUnavailable skill_id 为空跳过上传
func TestExperienceSharer_FlushPendingUploads_SkillIDUnavailable(t *testing.T) {
	es, _, _ := newTestSharer(t)
	ctx := context.Background()

	es.SetSkillSharingContextProvider(fakeProvider("", nil, "", ""))

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if result.OK {
		t.Error("skill_id 不可用时不应成功")
	}
	if result.Reason != "skill_id unavailable" {
		t.Errorf("Reason = %q, 期望 %q", result.Reason, "skill_id unavailable")
	}
}

// TestExperienceSharer_FlushPendingUploads_NoProvider 无 provider 且 skill_id 为空时跳过上传
func TestExperienceSharer_FlushPendingUploads_NoProvider(t *testing.T) {
	es, _, _ := newTestSharer(t)
	ctx := context.Background()

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if result.OK {
		t.Error("无 provider 且 skill_id 为空时应跳过上传")
	}
}

// TestExperienceSharer_FlushPendingUploads_Success 正常上传成功
func TestExperienceSharer_FlushPendingUploads_Success(t *testing.T) {
	es, bk, cacheDir := newTestSharer(t)
	ctx := context.Background()

	es.SetSkillSharingContextProvider(fakeProvider("sk_ok", []byte("pkg"), "ok-skill", "ok desc"))

	es.StageForUpload("skill_x", makeExp("r1", []string{"python", "debug"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if !result.OK {
		t.Fatalf("FlushPendingUploads 失败: %s", result.Reason)
	}

	// bundle 应已在 Hub 中
	query := QueryKeywords{Keywords: []string{"python"}}
	downloaded := bk.DownloadBundles(ctx, "sk_ok", query, 3)
	if len(downloaded) == 0 {
		t.Error("Hub 中应有 bundle")
	}

	// 验证 uploaded 缓存文件路径（mirrorBundle 写入 uploaded/ 目录）
	uploadedDir := filepath.Join(cacheDir, "uploaded", "sk_ok")
	entries, err := os.ReadDir(uploadedDir)
	if err != nil {
		t.Fatalf("读取 uploaded 目录失败: %v", err)
	}
	jsonCount := 0
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			jsonCount++
		}
	}
	if jsonCount == 0 {
		t.Error("uploaded 目录应有 .json 文件")
	}
}

// TestExperienceSharer_FlushPendingUploads_EmptyQueue 空队列返回成功
func TestExperienceSharer_FlushPendingUploads_EmptyQueue(t *testing.T) {
	es, _, _ := newTestSharer(t)
	ctx := context.Background()

	result := es.FlushPendingUploads(ctx, "nonexistent")
	if !result.OK {
		t.Errorf("空队列应返回 OK, 实际: %s", result.Reason)
	}
}

// TestExperienceSharer_FlushPendingUploads_PackageAlreadyPresent 技能包已存在不再上传
func TestExperienceSharer_FlushPendingUploads_PackageAlreadyPresent(t *testing.T) {
	es, bk, _ := newTestSharer(t)
	ctx := context.Background()

	pkgContent := []byte("existing-pkg")
	_ = bk.UploadSkillPackage(ctx, "sk_exists", pkgContent, SkillPackageMeta{
		SkillID: "sk_exists", SkillName: "existing", UploadedAt: "2027-01-01T00:00:00Z",
	})

	es.SetSkillSharingContextProvider(fakeProvider("sk_exists", []byte("new-pkg"), "existing", ""))

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if !result.OK {
		t.Fatalf("FlushPendingUploads 失败: %s", result.Reason)
	}

	// 技能包应保持原有内容
	data, err := bk.DownloadSkillPackage(ctx, "sk_exists")
	if err != nil {
		t.Fatalf("DownloadSkillPackage 失败: %v", err)
	}
	if string(data) != string(pkgContent) {
		t.Errorf("技能包内容 = %q, 期望 %q（不可变）", string(data), string(pkgContent))
	}
}

// TestExperienceSharer_FlushPendingUploads_EmptyPackageBytes 空技能包跳过上传
func TestExperienceSharer_FlushPendingUploads_EmptyPackageBytes(t *testing.T) {
	es, bk, _ := newTestSharer(t)
	ctx := context.Background()

	es.SetSkillSharingContextProvider(fakeProvider("sk_empty", nil, "", ""))

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if !result.OK {
		t.Logf("结果: OK=%v Reason=%q", result.OK, result.Reason)
	}
	if bk.HasSkillPackage(ctx, "sk_empty") {
		t.Error("空 packageBytes 不应上传技能包")
	}
}

// ──────────────────────────── DownloadRelevant 测试 ────────────────────────────

// TestExperienceSharer_DownloadRelevant 下载并镜像
func TestExperienceSharer_DownloadRelevant(t *testing.T) {
	es, _, _ := newTestSharer(t)
	ctx := context.Background()

	// 先上传一个 bundle
	es.SetSkillSharingContextProvider(fakeProvider("sk_dl", []byte("pkg"), "dl-skill", "desc"))
	es.StageForUpload("skill_dl", makeExp("r1", []string{"python", "debug"}))
	result := es.FlushPendingUploads(ctx, "skill_dl")
	if !result.OK {
		t.Fatalf("上传失败: %s", result.Reason)
	}

	// 下载
	query := QueryKeywords{Keywords: []string{"python"}}
	bundles := es.DownloadRelevant(ctx, "sk_dl", query, 3, "skill_dl")
	if len(bundles) == 0 {
		t.Fatal("应下载到 bundle")
	}

	// 本地缓存应有 downloaded 镜像
	cached := es.ListCachedBundles("sk_dl")
	if len(cached) == 0 {
		t.Error("下载后本地缓存应有 bundle")
	}
}

// TestExperienceSharer_DownloadRelevant_EmptySkillID 空 skill_id 返回 nil
func TestExperienceSharer_DownloadRelevant_EmptySkillID(t *testing.T) {
	es, _, _ := newTestSharer(t)
	ctx := context.Background()

	query := QueryKeywords{Keywords: []string{"python"}}
	bundles := es.DownloadRelevant(ctx, "", query, 3, "")
	if bundles != nil {
		t.Error("空 skill_id 应返回 nil")
	}
}

// ──────────────────────────── ListCachedBundles 测试 ────────────────────────────

// TestExperienceSharer_ListCachedBundles 列出缓存
func TestExperienceSharer_ListCachedBundles(t *testing.T) {
	es, _, cacheDir := newTestSharer(t)

	skillDir := filepath.Join(cacheDir, "downloaded", "sk_list")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	bundle := MakeSharedSkillBundle("skill_list", []SharedExperience{
		{Keywords: []string{"a"}, Summary: "test"},
	}, "", "")
	bundle.SkillID = "sk_list"

	bundleData, _ := json.MarshalIndent(bundle.ToDict(), "", "  ")
	if err := os.WriteFile(filepath.Join(skillDir, bundle.BundleID+".json"), bundleData, 0o644); err != nil {
		t.Fatalf("写入缓存文件失败: %v", err)
	}

	cached := es.ListCachedBundles("sk_list")
	if len(cached) != 1 {
		t.Errorf("缓存 bundle 数量 = %d, 期望 1", len(cached))
	}
	if cached[0].SkillID != "sk_list" {
		t.Errorf("SkillID = %q, 期望 %q", cached[0].SkillID, "sk_list")
	}
}

// TestExperienceSharer_ListCachedBundles_NoCacheDir 无缓存目录
func TestExperienceSharer_ListCachedBundles_NoCacheDir(t *testing.T) {
	bk := newMockBackend()
	es := NewExperienceSharer(bk, "", 3, 0.01, nil)

	cached := es.ListCachedBundles("sk_any")
	if cached != nil {
		t.Error("无缓存目录应返回 nil")
	}
}

// ──────────────────────────── ResolveSkillID 测试 ────────────────────────────

// TestExperienceSharer_ResolveSkillID 通过 provider 获取
func TestExperienceSharer_ResolveSkillID(t *testing.T) {
	es, _, _ := newTestSharer(t)
	ctx := context.Background()

	id := es.ResolveSkillID(ctx, "skill_x")
	if id != "" {
		t.Errorf("无 provider 时应返回空, 实际: %q", id)
	}

	es.SetSkillSharingContextProvider(fakeProvider("sk_resolved", nil, "resolved", "desc"))
	id = es.ResolveSkillID(ctx, "skill_x")
	if id != "sk_resolved" {
		t.Errorf("skill_id = %q, 期望 %q", id, "sk_resolved")
	}

	id = es.ResolveSkillID(ctx, "")
	if id != "" {
		t.Errorf("空 skillName 应返回空, 实际: %q", id)
	}
}

// TestExperienceSharer_ResolveSkillID_ProviderError provider 出错返回空
func TestExperienceSharer_ResolveSkillID_ProviderError(t *testing.T) {
	es, _, _ := newTestSharer(t)
	ctx := context.Background()

	errorProvider := func(_ context.Context, _ string) (string, []byte, string, string, error) {
		return "", nil, "", "", os.ErrNotExist
	}
	es.SetSkillSharingContextProvider(errorProvider)

	id := es.ResolveSkillID(ctx, "skill_x")
	if id != "" {
		t.Errorf("provider 出错时应返回空, 实际: %q", id)
	}
}

// ──────────────────────────── HasPending 测试 ────────────────────────────

// TestExperienceSharer_HasPending 是否有待上传
func TestExperienceSharer_HasPending(t *testing.T) {
	es, _, _ := newTestSharer(t)

	if es.HasPending("skill_x") {
		t.Error("初始状态不应有待上传")
	}

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	if !es.HasPending("skill_x") {
		t.Error("入队后应有待上传")
	}
	if es.HasPending("skill_y") {
		t.Error("其他技能不应有待上传")
	}
}

// ──────────────────────────── SearchSkills 测试 ────────────────────────────

// TestExperienceSharer_SearchSkills 搜索技能
func TestExperienceSharer_SearchSkills(t *testing.T) {
	es, bk, _ := newTestSharer(t)
	ctx := context.Background()

	_ = bk.UploadSkillPackage(ctx, "sk_search", []byte("pkg"), SkillPackageMeta{
		SkillID: "sk_search", SkillName: "search-skill", Description: "search desc",
		UploadedAt: "2027-01-01T00:00:00Z",
	})

	query := QueryKeywords{Keywords: []string{"search"}}
	results := es.SearchSkills(ctx, query, 5)
	if len(results) == 0 {
		t.Error("应搜索到结果")
	}
}

// ──────────────────────────── DownloadSkillPackage 测试 ────────────────────────────

// TestExperienceSharer_DownloadSkillPackage 下载技能包
func TestExperienceSharer_DownloadSkillPackage(t *testing.T) {
	es, bk, _ := newTestSharer(t)
	ctx := context.Background()

	content := []byte("skill-pkg-content")
	_ = bk.UploadSkillPackage(ctx, "sk_pkg_dl", content, SkillPackageMeta{
		SkillID: "sk_pkg_dl", SkillName: "pkg-skill", UploadedAt: "2027-01-01T00:00:00Z",
	})

	data := es.DownloadSkillPackage(ctx, "sk_pkg_dl")
	if string(data) != string(content) {
		t.Errorf("内容 = %q, 期望 %q", string(data), string(content))
	}

	data = es.DownloadSkillPackage(ctx, "nonexistent")
	if data != nil {
		t.Error("不存在的技能包应返回 nil")
	}
}

// ──────────────────────────── GetSkillPackageMeta 测试 ────────────────────────────

// TestExperienceSharer_GetSkillPackageMeta 获取元数据
func TestExperienceSharer_GetSkillPackageMeta(t *testing.T) {
	es, bk, _ := newTestSharer(t)
	ctx := context.Background()

	_ = bk.UploadSkillPackage(ctx, "sk_meta", []byte("pkg"), SkillPackageMeta{
		SkillID: "sk_meta", SkillName: "meta-skill", Description: "test", UploadedAt: "2027-01-01T00:00:00Z",
	})

	meta := es.GetSkillPackageMeta(ctx, "sk_meta")
	if meta == nil {
		t.Fatal("应返回元数据")
	}
	if meta.SkillName != "meta-skill" {
		t.Errorf("SkillName = %q, 期望 %q", meta.SkillName, "meta-skill")
	}
}

// ──────────────────────────── mirrorBundle 测试 ────────────────────────────

// TestExperienceSharer_MirrorBundle_InvalidKind 无效 kind 返回 error
func TestExperienceSharer_MirrorBundle_InvalidKind(t *testing.T) {
	es, _, _ := newTestSharer(t)

	bundle := &SharedSkillBundle{BundleID: "sb_test", SkillID: "sk_test"}
	err := es.mirrorBundle(bundle, "invalid")
	if err == nil {
		t.Error("无效 kind 应返回 error")
	}
	if err.Error() != "unsupported mirror kind: invalid" {
		t.Errorf("error = %q, 期望 %q", err.Error(), "unsupported mirror kind: invalid")
	}
}

// ──────────────────────────── Backend / LocalCacheDir 属性测试 ────────────────────────────

// TestExperienceSharer_Properties Backend/LocalCacheDir 属性
func TestExperienceSharer_Properties(t *testing.T) {
	es, bk, cacheDir := newTestSharer(t)

	if es.Backend() != bk {
		t.Error("Backend() 应返回构造时传入的后端")
	}
	if es.LocalCacheDir() == nil || *es.LocalCacheDir() != cacheDir {
		t.Error("LocalCacheDir() 应返回构造时传入的缓存目录")
	}
}

// TestExperienceSharer_NoCacheDir 无缓存目录
func TestExperienceSharer_NoCacheDir(t *testing.T) {
	bk := newMockBackend()
	es := NewExperienceSharer(bk, "", 3, 0.01, nil)

	if es.LocalCacheDir() != nil {
		t.Error("空缓存目录应返回 nil")
	}
}

// TestExperienceSharer_FlushPendingUploads_RetryableFail 可重试失败最终成功
func TestExperienceSharer_FlushPendingUploads_RetryableFail(t *testing.T) {
	bk := newMockBackend()
	// 前两次失败，第三次成功
	bk.uploadResults = []UploadResult{
		{OK: false, Reason: "temp error", Retryable: true},
		{OK: false, Reason: "temp error 2", Retryable: true},
	}
	cacheDir := t.TempDir()
	es := NewExperienceSharer(bk, cacheDir, 3, 0.001, nil)
	ctx := context.Background()

	es.SetSkillSharingContextProvider(fakeProvider("sk_retry", []byte("pkg"), "retry-skill", ""))

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if !result.OK {
		t.Errorf("重试后应成功, 实际: OK=%v Reason=%q", result.OK, result.Reason)
	}
}

// TestExperienceSharer_FlushPendingUploads_NonRetryableFail 不可重试失败立即返回
func TestExperienceSharer_FlushPendingUploads_NonRetryableFail(t *testing.T) {
	bk := newMockBackend()
	bk.uploadResults = []UploadResult{
		{OK: false, Reason: "permanent error", Retryable: false},
	}
	cacheDir := t.TempDir()
	es := NewExperienceSharer(bk, cacheDir, 3, 0.001, nil)
	ctx := context.Background()

	es.SetSkillSharingContextProvider(fakeProvider("sk_perm", []byte("pkg"), "perm-skill", ""))

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	if result.OK {
		t.Error("不可重试失败不应返回成功")
	}
	if result.Reason != "permanent error" {
		t.Errorf("Reason = %q, 期望 %q", result.Reason, "permanent error")
	}
}

// TestExperienceSharer_FlushPendingUploads_ProviderError provider 异常仍能继续
func TestExperienceSharer_FlushPendingUploads_ProviderError(t *testing.T) {
	bk := newMockBackend()
	cacheDir := t.TempDir()
	es := NewExperienceSharer(bk, cacheDir, 3, 0.001, nil)
	ctx := context.Background()

	// provider 抛出异常
	errorProvider := func(_ context.Context, _ string) (string, []byte, string, string, error) {
		return "", nil, "", "", os.ErrNotExist
	}
	es.SetSkillSharingContextProvider(errorProvider)

	es.StageForUpload("skill_x", makeExp("r1", []string{"a"}))
	result := es.FlushPendingUploads(ctx, "skill_x")
	// provider 异常 → skill_id 不会被设置 → 跳过上传
	if result.OK {
		t.Error("provider 异常时不应成功")
	}
	if result.Reason != "skill_id unavailable" {
		t.Errorf("Reason = %q, 期望 %q", result.Reason, "skill_id unavailable")
	}
}
