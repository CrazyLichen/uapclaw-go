package sharing

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeHubBackend 测试用的内存 SharingBackend 实现。
// 支持技能包上传/下载/搜索，经验 bundle 上传/下载。
type fakeHubBackend struct {
	mu             sync.RWMutex
	packages       map[string][]byte             // skillID → packageBytes
	packageMetas   map[string]*SkillPackageMeta  // skillID → meta
	bundles        map[string][]SharedSkillBundle // skillID → bundles
	searchIndex    []SkillSearchResult
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// newFakeHubBackend 创建 fakeHubBackend 实例
func newFakeHubBackend() *fakeHubBackend {
	return &fakeHubBackend{
		packages:     make(map[string][]byte),
		packageMetas: make(map[string]*SkillPackageMeta),
		bundles:      make(map[string][]SharedSkillBundle),
	}
}

// UploadBundle 上传经验 bundle
func (f *fakeHubBackend) UploadBundle(ctx context.Context, bundle SharedSkillBundle) UploadResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	skillID := bundle.SkillID
	if skillID == "" {
		return UploadResult{OK: false, Reason: "skill_id is required"}
	}
	f.bundles[skillID] = append(f.bundles[skillID], bundle)
	return UploadResult{OK: true, BundleID: bundle.BundleID}
}

// DownloadBundles 下载经验 bundle
func (f *fakeHubBackend) DownloadBundles(ctx context.Context, skillID string, query QueryKeywords, topK int) []SharedSkillBundle {
	f.mu.RLock()
	defer f.mu.RUnlock()
	bundles := f.bundles[skillID]
	if len(bundles) > topK {
		return bundles[:topK]
	}
	return bundles
}

// HasSkillPackage Hub 是否已有该技能包
func (f *fakeHubBackend) HasSkillPackage(ctx context.Context, skillID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.packages[skillID]
	return ok
}

// UploadSkillPackage 上传技能包
func (f *fakeHubBackend) UploadSkillPackage(ctx context.Context, skillID string, packageBytes []byte, meta SkillPackageMeta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.packages[skillID]; ok {
		return nil // 不可变，重复上传为 no-op
	}
	f.packages[skillID] = packageBytes
	metaCopy := meta
	f.packageMetas[skillID] = &metaCopy
	// 同时更新搜索索引
	f.searchIndex = append(f.searchIndex, SkillSearchResult{
		SkillID:         skillID,
		SkillName:       meta.SkillName,
		Description:     meta.Description,
		ExperienceCount: 0,
		Keywords:        []string{},
	})
	return nil
}

// DownloadSkillPackage 下载技能包
func (f *fakeHubBackend) DownloadSkillPackage(ctx context.Context, skillID string) ([]byte, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	data, ok := f.packages[skillID]
	if !ok {
		return nil, nil
	}
	return data, nil
}

// GetSkillPackageMeta 获取技能包元数据
func (f *fakeHubBackend) GetSkillPackageMeta(ctx context.Context, skillID string) (*SkillPackageMeta, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	meta, ok := f.packageMetas[skillID]
	if !ok {
		return nil, nil
	}
	return meta, nil
}

// SearchSkills 全局搜索技能
func (f *fakeHubBackend) SearchSkills(ctx context.Context, query QueryKeywords, topK int) []SkillSearchResult {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if topK > len(f.searchIndex) {
		topK = len(f.searchIndex)
	}
	if topK <= 0 {
		return nil
	}
	// 简单关键字匹配
	var results []SkillSearchResult
	for _, entry := range f.searchIndex {
		matched := false
		for _, kw := range query.Keywords {
			if kw == entry.SkillName || kw == entry.SkillID {
				matched = true
				break
			}
		}
		if matched {
			results = append(results, entry)
		}
	}
	if len(results) > topK {
		return results[:topK]
	}
	return results
}

// TestNewExperienceHubClient 测试构造函数
func TestNewExperienceHubClient(t *testing.T) {
	tmpDir := t.TempDir()
	bk := newFakeHubBackend()
	store := checkpointing.NewEvolutionStore([]string{filepath.Join(tmpDir, "skills")})

	client := NewExperienceHubClient(bk, store)
	require.NotNil(t, client)
	require.NotNil(t, client.Sharer())
}

// TestExperienceHubClient_SearchSkills 测试搜索技能
func TestExperienceHubClient_SearchSkills(t *testing.T) {
	tmpDir := t.TempDir()
	bk := newFakeHubBackend()
	skillsDir := filepath.Join(tmpDir, "skills")
	store := checkpointing.NewEvolutionStore([]string{skillsDir})

	client := NewExperienceHubClient(bk, store)
	ctx := context.Background()

	// 上传一个技能包到 Hub
	srcDir := filepath.Join(tmpDir, "src_skill")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.md"),
		[]byte("---\nname: test_skill\n---\n\n# Test Skill"), 0644))

	pkg, err := checkpointing.PackSkillDirectory(srcDir, "", "")
	require.NoError(t, err)

	skillID := "sid_search_test"
	meta := SkillPackageMeta{
		SkillID:     skillID,
		SkillName:   "test_skill",
		Description: "a test skill",
	}
	require.NoError(t, bk.UploadSkillPackage(ctx, skillID, pkg, meta))

	// 搜索
	query := QueryKeywords{
		Keywords: []string{"test_skill"},
		Intent:   "find test skill",
	}
	results := client.SearchSkills(ctx, query, 5)
	require.Len(t, results, 1)
	require.Equal(t, skillID, results[0].SkillID)
	require.Equal(t, "test_skill", results[0].SkillName)
}

// TestExperienceHubClient_InstallSkill 测试下载并安装技能
func TestExperienceHubClient_InstallSkill(t *testing.T) {
	tmpDir := t.TempDir()
	bk := newFakeHubBackend()
	skillsDir := filepath.Join(tmpDir, "skills")
	store := checkpointing.NewEvolutionStore([]string{skillsDir})

	client := NewExperienceHubClient(bk, store)
	ctx := context.Background()

	// 准备技能源目录
	srcDir := filepath.Join(tmpDir, "src_install")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.md"),
		[]byte("---\nname: install_skill\n---\n\n# Install Skill"), 0644))

	pkg, err := checkpointing.PackSkillDirectory(srcDir, "", "")
	require.NoError(t, err)

	skillID := "sid_install_test"
	meta := SkillPackageMeta{
		SkillID:     skillID,
		SkillName:   "install_skill",
		Description: "skill to install",
	}
	require.NoError(t, bk.UploadSkillPackage(ctx, skillID, pkg, meta))

	// 安装
	installed, err := client.InstallSkill(ctx, skillID, "installed_skill")
	require.NoError(t, err)
	require.NotEmpty(t, installed)

	// 验证安装结果
	skillMD := filepath.Join(installed, "SKILL.md")
	info, err := os.Stat(skillMD)
	require.NoError(t, err)
	require.False(t, info.IsDir())
}

// TestExperienceHubClient_InstallSkill_使用元数据名称 测试 skillName 为空时使用 meta.SkillName
func TestExperienceHubClient_InstallSkill_使用元数据名称(t *testing.T) {
	tmpDir := t.TempDir()
	bk := newFakeHubBackend()
	skillsDir := filepath.Join(tmpDir, "skills")
	store := checkpointing.NewEvolutionStore([]string{skillsDir})

	client := NewExperienceHubClient(bk, store)
	ctx := context.Background()

	// 准备技能源目录
	srcDir := filepath.Join(tmpDir, "src_meta_name")
	require.NoError(t, os.MkdirAll(srcDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "SKILL.md"),
		[]byte("---\nname: meta_skill\n---\n\n# Meta Skill"), 0644))

	pkg, err := checkpointing.PackSkillDirectory(srcDir, "", "")
	require.NoError(t, err)

	skillID := "sid_meta_name_test"
	meta := SkillPackageMeta{
		SkillID:     skillID,
		SkillName:   "meta_skill",
		Description: "skill from meta",
	}
	require.NoError(t, bk.UploadSkillPackage(ctx, skillID, pkg, meta))

	// 不传 skillName，应使用 meta.SkillName
	installed, err := client.InstallSkill(ctx, skillID, "")
	require.NoError(t, err)
	require.NotEmpty(t, installed)
	require.Contains(t, installed, "meta_skill")
}

// TestExperienceHubClient_InstallSkill_EmptySkillID 测试空 skill_id 返回错误
func TestExperienceHubClient_InstallSkill_EmptySkillID(t *testing.T) {
	tmpDir := t.TempDir()
	bk := newFakeHubBackend()
	skillsDir := filepath.Join(tmpDir, "skills")
	store := checkpointing.NewEvolutionStore([]string{skillsDir})

	client := NewExperienceHubClient(bk, store)
	ctx := context.Background()

	_, err := client.InstallSkill(ctx, "", "some_skill")
	require.Error(t, err)

	_, err = client.InstallSkill(ctx, "   ", "some_skill")
	require.Error(t, err)
}

// TestExperienceHubClient_InstallSkill_包不存在 测试下载到空包返回错误
func TestExperienceHubClient_InstallSkill_包不存在(t *testing.T) {
	tmpDir := t.TempDir()
	bk := newFakeHubBackend()
	skillsDir := filepath.Join(tmpDir, "skills")
	store := checkpointing.NewEvolutionStore([]string{skillsDir})

	client := NewExperienceHubClient(bk, store)
	ctx := context.Background()

	_, err := client.InstallSkill(ctx, "nonexistent_skill_id", "some_skill")
	require.Error(t, err)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
