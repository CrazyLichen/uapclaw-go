package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/evolving/sharing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LocalFileBackend 基于本地文件系统的经验共享后端。
//
// Hub 目录布局（hubPath 下）：
//
//	packages/<skill_id>/skill.tar.gz   - 不可变技能包
//	packages/<skill_id>/meta.json      - 技能元数据
//	bundles/<skill_id>/sb_xxx.json     - 经验 bundle
//	index/<skill_id>.jsonl             - 按 skill_id 的关键词索引
//	index/global.jsonl                 - 全局技能搜索索引
//	.outbox/<skill_id>/sb_xxx.json     - 上传失败的 bundle
//
// Python: openjiuwen/agent_evolving/sharing/backends/local_file.py LocalFileBackend
type LocalFileBackend struct {
	// hubPath Hub 根路径
	hubPath string
	// packagesDir 技能包目录
	packagesDir string
	// bundlesDir 经验 bundle 目录
	bundlesDir string
	// indexDir 索引目录
	indexDir string
	// outboxDir 失败上传暂存目录
	outboxDir string
	// dedupJaccardThreshold 去重 Jaccard 阈值
	dedupJaccardThreshold float64
	// mu 读写互斥锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultHubPath 默认 Hub 路径
	defaultHubPath = "~/.openjiuwen/experience_hub"
	// globalIndexFileName 全局索引文件名
	globalIndexFileName = "global.jsonl"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件常量（与 sharing 包的 interface.go 中声明同值，
// backend 是独立 Go 包，无法跨包引用 sharing.logComponent）
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLocalFileBackend 创建 LocalFileBackend 实例。
//
// hubPath 为空时使用默认路径 ~/.openjiuwen/experience_hub。
// dedupJaccardThreshold 默认 0.85。
//
// Python: LocalFileBackend.__init__()
func NewLocalFileBackend(hubPath string, dedupJaccardThreshold float64) *LocalFileBackend {
	resolved := hubPath
	if resolved == "" {
		resolved = defaultHubPath
	}
	resolved = path.ExpandHome(resolved)
	abs, err := filepath.Abs(resolved)
	if err != nil {
		abs = resolved
	}
	if dedupJaccardThreshold <= 0 {
		dedupJaccardThreshold = 0.85
	}
	return &LocalFileBackend{
		hubPath:               abs,
		packagesDir:           filepath.Join(abs, "packages"),
		bundlesDir:            filepath.Join(abs, "bundles"),
		indexDir:              filepath.Join(abs, "index"),
		outboxDir:             filepath.Join(abs, ".outbox"),
		dedupJaccardThreshold: dedupJaccardThreshold,
	}
}

// HubPath 返回 Hub 根路径。
//
// Python: LocalFileBackend.hub_path (property)
func (b *LocalFileBackend) HubPath() string {
	return b.hubPath
}

// OutboxDir 返回 outbox 目录路径。
//
// Python: LocalFileBackend.outbox_dir (property)
func (b *LocalFileBackend) OutboxDir() string {
	return b.outboxDir
}

// UploadBundle 上传经验 bundle，返回上传结果。
//
// 写操作，加写锁。重复 bundle（高 Jaccard 相似度）被拒绝。
// 写入失败时将 bundle 暂存到 outbox。
//
// Python: LocalFileBackend.upload_bundle()
func (b *LocalFileBackend) UploadBundle(ctx context.Context, bundle sharing.SharedSkillBundle) sharing.UploadResult {
	skillID := strings.TrimSpace(bundle.SkillID)
	if skillID == "" {
		return sharing.UploadResult{OK: false, Reason: "bundle.skill_id is required for upload"}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	dupReason := b.duplicateRejectionReason(skillID, bundle.KeywordsAggregate)
	if dupReason != "" {
		logger.Info(logComponent).
			Str("bundle_id", bundle.BundleID).
			Str("skill_name", bundle.SkillName).
			Str("skill_id", skillID).
			Str("reason", dupReason).
			Msg("[LocalFileBackend] rejected bundle")
		return sharing.UploadResult{OK: false, Reason: dupReason}
	}

	// 写入 bundle 文件
	bundleDir := b.bundleDir(skillID)
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[LocalFileBackend] upload failed; routing to outbox")
		b.spoolToOutbox(bundle)
		return sharing.UploadResult{OK: false, Reason: err.Error(), Retryable: true}
	}
	if err := os.MkdirAll(b.indexDir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[LocalFileBackend] upload failed; routing to outbox")
		b.spoolToOutbox(bundle)
		return sharing.UploadResult{OK: false, Reason: err.Error(), Retryable: true}
	}

	bundleFile := filepath.Join(bundleDir, bundle.BundleID+".json")
	bundleData, _ := json.MarshalIndent(bundle.ToDict(), "", "  ")
	if err := os.WriteFile(bundleFile, bundleData, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[LocalFileBackend] upload failed; routing to outbox")
		b.spoolToOutbox(bundle)
		return sharing.UploadResult{OK: false, Reason: err.Error(), Retryable: true}
	}

	// 写入索引行
	indexLineData := map[string]any{
		"bundle_id":     bundle.BundleID,
		"skill_id":      skillID,
		"skill_name":    bundle.SkillName,
		"skill_version": bundle.SkillVersion,
		"keywords":      bundle.KeywordsAggregate,
		"summary":       bundle.SummaryAggregate,
		"created_at":    bundle.CreatedAt,
	}
	indexLine, _ := json.Marshal(indexLineData)
	indexFile, err := os.OpenFile(b.indexPath(skillID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[LocalFileBackend] upload failed; routing to outbox")
		b.spoolToOutbox(bundle)
		return sharing.UploadResult{OK: false, Reason: err.Error(), Retryable: true}
	}
	if _, err := fmt.Fprintf(indexFile, "%s\n", indexLine); err != nil {
		indexFile.Close()
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[LocalFileBackend] upload failed; routing to outbox")
		b.spoolToOutbox(bundle)
		return sharing.UploadResult{OK: false, Reason: err.Error(), Retryable: true}
	}
	indexFile.Close()

	// 更新全局索引
	b.upsertGlobalIndex(skillID, bundle)

	logger.Info(logComponent).
		Str("bundle_id", bundle.BundleID).
		Str("skill_name", bundle.SkillName).
		Str("skill_id", skillID).
		Int("experience_count", len(bundle.Experiences)).
		Msg("[LocalFileBackend] uploaded bundle")

	return sharing.UploadResult{OK: true, BundleID: bundle.BundleID}
}

// DownloadBundles 按 skill_id 和关键词检索，返回最多 topK 个 bundle。
//
// 读操作，不加锁。
//
// Python: LocalFileBackend.download_bundles()
func (b *LocalFileBackend) DownloadBundles(ctx context.Context, skillID string, query sharing.QueryKeywords, topK int) []sharing.SharedSkillBundle {
	resolvedID := strings.TrimSpace(skillID)
	if resolvedID == "" {
		return nil
	}

	indexEntries := b.readIndex(resolvedID)
	if len(indexEntries) == 0 {
		logger.Info(logComponent).
			Str("skill_id", resolvedID).
			Msg("[LocalFileBackend] download_bundles: no index entries")
		return nil
	}

	type scored struct {
		score float64
		entry map[string]any
	}
	var ranked []scored
	for _, entry := range indexEntries {
		keywords := toStringSliceFromAny(entry["keywords"])
		score := jaccard(query.Keywords, keywords)
		ranked = append(ranked, scored{score: score, entry: entry})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	if topK < 0 {
		topK = 0
	}
	if topK > len(ranked) {
		topK = len(ranked)
	}

	var results []sharing.SharedSkillBundle
	for i := 0; i < topK; i++ {
		if ranked[i].score <= 0.0 {
			continue
		}
		bundleID, _ := ranked[i].entry["bundle_id"].(string)
		bundle := b.loadBundle(resolvedID, bundleID)
		if bundle != nil {
			logger.Info(logComponent).
				Str("bundle_id", bundle.BundleID).
				Str("skill_id", resolvedID).
				Float64("score", ranked[i].score).
				Msg("[LocalFileBackend] selected bundle")
			results = append(results, *bundle)
		}
	}
	return results
}

// HasSkillPackage Hub 是否已有该技能包。
//
// 读操作，不加锁。
//
// Python: LocalFileBackend.has_skill_package()
func (b *LocalFileBackend) HasSkillPackage(ctx context.Context, skillID string) bool {
	resolvedID := strings.TrimSpace(skillID)
	if resolvedID == "" {
		return false
	}
	_, err := os.Stat(b.packageArchive(resolvedID))
	return err == nil
}

// UploadSkillPackage 上传初始技能包（不可变，重复上传为 no-op）。
//
// 写操作，加写锁。
//
// Python: LocalFileBackend.upload_skill_package()
func (b *LocalFileBackend) UploadSkillPackage(ctx context.Context, skillID string, packageBytes []byte, meta sharing.SkillPackageMeta) error {
	resolvedID := strings.TrimSpace(skillID)
	if resolvedID == "" {
		return fmt.Errorf("skill_id is required for upload_skill_package")
	}
	if len(packageBytes) == 0 {
		return fmt.Errorf("package_bytes is empty")
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// 已存在则跳过
	if _, err := os.Stat(b.packageArchive(resolvedID)); err == nil {
		logger.Debug(logComponent).
			Str("skill_id", resolvedID).
			Msg("[LocalFileBackend] skill package already exists; skipping upload")
		return nil
	}

	packageDir := b.packageDir(resolvedID)
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", resolvedID).
			Err(err).
			Msg("[LocalFileBackend] upload_skill_package failed")
		return err
	}
	if err := os.WriteFile(b.packageArchive(resolvedID), packageBytes, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", resolvedID).
			Err(err).
			Msg("[LocalFileBackend] upload_skill_package failed")
		return err
	}

	// 写入 meta.json
	metaPayload := sharing.SkillPackageMeta{
		SkillID:     resolvedID,
		SkillName:   meta.SkillName,
		Description: meta.Description,
		UploadedAt:  meta.UploadedAt,
	}
	metaData, _ := json.MarshalIndent(metaPayload.ToDict(), "", "  ")
	if err := os.WriteFile(b.packageMetaPath(resolvedID), metaData, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", resolvedID).
			Err(err).
			Msg("[LocalFileBackend] upload_skill_package failed")
		return err
	}

	b.ensureGlobalIndexEntry(resolvedID, metaPayload)

	logger.Info(logComponent).
		Str("skill_id", resolvedID).
		Str("skill_name", meta.SkillName).
		Int("bytes", len(packageBytes)).
		Msg("[LocalFileBackend] uploaded skill package")

	return nil
}

// DownloadSkillPackage 下载技能包字节。
//
// 读操作，不加锁。
//
// Python: LocalFileBackend.download_skill_package()
func (b *LocalFileBackend) DownloadSkillPackage(ctx context.Context, skillID string) ([]byte, error) {
	resolvedID := strings.TrimSpace(skillID)
	if resolvedID == "" {
		return nil, nil
	}
	archive := b.packageArchive(resolvedID)
	if _, err := os.Stat(archive); err != nil {
		return nil, nil
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// GetSkillPackageMeta 获取技能包元数据。
//
// 读操作，不加锁。
//
// Python: LocalFileBackend.get_skill_package_meta()
func (b *LocalFileBackend) GetSkillPackageMeta(ctx context.Context, skillID string) (*sharing.SkillPackageMeta, error) {
	return b.readPackageMeta(strings.TrimSpace(skillID)), nil
}

// SearchSkills 全局关键词搜索技能。
//
// 读操作，不加锁。
//
// Python: LocalFileBackend.search_skills()
func (b *LocalFileBackend) SearchSkills(ctx context.Context, query sharing.QueryKeywords, topK int) []sharing.SkillSearchResult {
	entries := b.readGlobalIndex()
	if len(entries) == 0 {
		return nil
	}

	type scored struct {
		score float64
		entry map[string]any
	}
	var ranked []scored
	for _, entry := range entries {
		keywords := toStringSliceFromAny(entry["keywords"])
		skillName, _ := entry["skill_name"].(string)
		description, _ := entry["description"].(string)
		searchTerms := append(keywords, skillName, description)
		score := jaccard(query.Keywords, searchTerms)
		ranked = append(ranked, scored{score: score, entry: entry})
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	if topK < 0 {
		topK = 0
	}
	if topK > len(ranked) {
		topK = len(ranked)
	}

	var results []sharing.SkillSearchResult
	for i := 0; i < topK; i++ {
		if ranked[i].score <= 0.0 {
			continue
		}
		entry := ranked[i].entry
		sid, _ := entry["skill_id"].(string)
		sname, _ := entry["skill_name"].(string)
		desc, _ := entry["description"].(string)
		ec := toIntFromAny(entry["experience_count"])
		kws := toStringSliceFromAny(entry["keywords"])
		results = append(results, sharing.SkillSearchResult{
			SkillID:         sid,
			SkillName:       sname,
			Description:     desc,
			ExperienceCount: ec,
			Keywords:        kws,
			Score:           ranked[i].score,
		})
	}
	return results
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// jaccard 计算两组关键词的 Jaccard 相似度，含子串降级逻辑。
//
// 1. 精确匹配：大小写不敏感，空集返回 0.0
// 2. 子串降级：若无精确交集，任一对存在子串关系则返回 0.5/max(len(union),1)
//
// Python: _jaccard()
func jaccard(a, b []string) float64 {
	setA := make(map[string]struct{})
	for _, item := range a {
		if item != "" {
			setA[strings.ToLower(item)] = struct{}{}
		}
	}
	setB := make(map[string]struct{})
	for _, item := range b {
		if item != "" {
			setB[strings.ToLower(item)] = struct{}{}
		}
	}

	if len(setA) == 0 && len(setB) == 0 {
		return 0.0
	}

	// 计算交集
	intersection := 0
	for k := range setA {
		if _, ok := setB[k]; ok {
			intersection++
		}
	}
	if intersection > 0 {
		union := len(setA) + len(setB) - intersection
		return float64(intersection) / float64(union)
	}

	// 子串降级
	for kwA := range setA {
		for kwB := range setB {
			if strings.Contains(kwA, kwB) || strings.Contains(kwB, kwA) {
				union := len(setA) + len(setB) - intersection
				return 0.5 / float64(max(union, 1))
			}
		}
	}
	return 0.0
}

// packageDir 返回技能包目录路径。
//
// Python: LocalFileBackend._package_dir()
func (b *LocalFileBackend) packageDir(skillID string) string {
	return filepath.Join(b.packagesDir, skillID)
}

// packageArchive 返回技能包归档文件路径。
//
// Python: LocalFileBackend._package_archive()
func (b *LocalFileBackend) packageArchive(skillID string) string {
	return filepath.Join(b.packageDir(skillID), "skill.tar.gz")
}

// packageMetaPath 返回技能包元数据文件路径。
//
// Python: LocalFileBackend._package_meta_path()
func (b *LocalFileBackend) packageMetaPath(skillID string) string {
	return filepath.Join(b.packageDir(skillID), "meta.json")
}

// bundleDir 返回 bundle 目录路径。
//
// Python: LocalFileBackend._bundle_dir()
func (b *LocalFileBackend) bundleDir(skillID string) string {
	return filepath.Join(b.bundlesDir, skillID)
}

// indexPath 返回按 skill_id 的索引文件路径。
//
// Python: LocalFileBackend._index_path()
func (b *LocalFileBackend) indexPath(skillID string) string {
	return filepath.Join(b.indexDir, skillID+".jsonl")
}

// globalIndexPath 返回全局索引文件路径。
//
// Python: LocalFileBackend._global_index_path()
func (b *LocalFileBackend) globalIndexPath() string {
	return filepath.Join(b.indexDir, globalIndexFileName)
}

// outboxSkillDir 返回 outbox 中 skill 的目录路径。
//
// Python: LocalFileBackend._outbox_skill_dir()
func (b *LocalFileBackend) outboxSkillDir(skillID string) string {
	return filepath.Join(b.outboxDir, skillID)
}

// duplicateRejectionReason 检查 bundle 是否与已有索引条目重复。
//
// 返回空字符串表示无重复，否则返回拒绝原因。
//
// Python: LocalFileBackend._duplicate_rejection_reason()
func (b *LocalFileBackend) duplicateRejectionReason(skillID string, keywords []string) string {
	if len(keywords) == 0 {
		return ""
	}
	for _, entry := range b.readIndex(skillID) {
		existing := toStringSliceFromAny(entry["keywords"])
		score := jaccard(keywords, existing)
		if score >= b.dedupJaccardThreshold {
			existingID, _ := entry["bundle_id"].(string)
			if existingID == "" {
				existingID = "?"
			}
			return fmt.Sprintf("keywords overlap existing bundle %s (jaccard=%.2f, threshold=%.2f)",
				existingID, score, b.dedupJaccardThreshold)
		}
	}
	return ""
}

// upsertGlobalIndex 更新或插入全局索引条目。
//
// 合并关键词、递增经验数、优先使用 meta.json 中的名称和描述。
//
// Python: LocalFileBackend._upsert_global_index()
func (b *LocalFileBackend) upsertGlobalIndex(skillID string, bundle sharing.SharedSkillBundle) {
	entries := b.readGlobalIndex()
	mergedKeywords := make([]string, len(bundle.KeywordsAggregate))
	copy(mergedKeywords, bundle.KeywordsAggregate)
	experienceCount := 1
	skillName := bundle.SkillName
	description := ""

	for i, entry := range entries {
		if getStrFromAny(entry["skill_id"]) != skillID {
			continue
		}
		entryKws := toStringSliceFromAny(entry["keywords"])
		for _, kw := range entryKws {
			if kw != "" && !containsString(mergedKeywords, kw) {
				mergedKeywords = append(mergedKeywords, kw)
			}
		}
		experienceCount = toIntFromAny(entry["experience_count"]) + 1
		if sn := getStrFromAny(entry["skill_name"]); sn != "" {
			skillName = sn
		}
		if d := getStrFromAny(entry["description"]); d != "" {
			description = d
		}
		// 移除旧条目
		entries = append(entries[:i], entries[i+1:]...)
		break
	}

	// 优先从 meta.json 获取名称和描述
	meta := b.readPackageMeta(skillID)
	if meta != nil {
		if meta.SkillName != "" {
			skillName = meta.SkillName
		}
		if meta.Description != "" {
			description = meta.Description
		}
	}

	entries = append(entries, map[string]any{
		"skill_id":         skillID,
		"skill_name":       skillName,
		"description":      description,
		"keywords":         mergedKeywords,
		"experience_count": experienceCount,
		"updated_at":       bundle.CreatedAt,
	})
	b.writeGlobalIndex(entries)
}

// spoolToOutbox 将 bundle 暂存到 outbox 目录。
//
// Python: LocalFileBackend._spool_to_outbox()
func (b *LocalFileBackend) spoolToOutbox(bundle sharing.SharedSkillBundle) {
	skillID := strings.TrimSpace(bundle.SkillID)
	if skillID == "" {
		logger.Error(logComponent).
			Str("bundle_id", bundle.BundleID).
			Msg("[LocalFileBackend] cannot spool bundle to outbox without skill_id")
		return
	}
	if err := os.MkdirAll(b.outboxSkillDir(skillID), 0o755); err != nil {
		logger.Error(logComponent).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[LocalFileBackend] outbox spool also failed")
		return
	}
	outboxFile := filepath.Join(b.outboxSkillDir(skillID), bundle.BundleID+".json")
	data, _ := json.MarshalIndent(bundle.ToDict(), "", "  ")
	if err := os.WriteFile(outboxFile, data, 0o644); err != nil {
		logger.Error(logComponent).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[LocalFileBackend] outbox spool also failed")
	}
}

// ensureGlobalIndexEntry 确保全局索引中存在 skill_id 条目。
//
// 已存在则更新名称和描述，不存在则创建新条目。
//
// Python: LocalFileBackend._ensure_global_index_entry()
func (b *LocalFileBackend) ensureGlobalIndexEntry(skillID string, meta sharing.SkillPackageMeta) {
	entries := b.readGlobalIndex()
	for i, entry := range entries {
		if getStrFromAny(entry["skill_id"]) == skillID {
			if meta.SkillName != "" {
				entry["skill_name"] = meta.SkillName
			} else {
				entry["skill_name"] = getStrFromAny(entry["skill_name"])
			}
			if meta.Description != "" {
				entry["description"] = meta.Description
			} else {
				entry["description"] = getStrFromAny(entry["description"])
			}
			entries[i] = entry
			b.writeGlobalIndex(entries)
			return
		}
	}
	entries = append(entries, map[string]any{
		"skill_id":         skillID,
		"skill_name":       meta.SkillName,
		"description":      meta.Description,
		"keywords":         []string{},
		"experience_count": 0,
		"updated_at":       meta.UploadedAt,
	})
	b.writeGlobalIndex(entries)
}

// readPackageMeta 读取技能包元数据。
//
// Python: LocalFileBackend._read_package_meta()
func (b *LocalFileBackend) readPackageMeta(skillID string) *sharing.SkillPackageMeta {
	if skillID == "" {
		return nil
	}
	metaPath := b.packageMetaPath(skillID)
	if _, err := os.Stat(metaPath); err != nil {
		return nil
	}
	data, err := os.ReadFile(metaPath)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Err(err).
			Msg("[LocalFileBackend] meta read failed")
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Err(err).
			Msg("[LocalFileBackend] meta read failed")
		return nil
	}
	return sharing.FromDictSkillPackageMeta(raw)
}

// readIndex 读取按 skill_id 的索引文件。
//
// Python: LocalFileBackend._read_index()
func (b *LocalFileBackend) readIndex(skillID string) []map[string]any {
	indexFile := b.indexPath(skillID)
	if _, err := os.Stat(indexFile); err != nil {
		return nil
	}
	data, err := os.ReadFile(indexFile)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Err(err).
			Msg("[LocalFileBackend] index read failed")
		return nil
	}
	var entries []map[string]any
	for lineNo, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			logger.Warn(logComponent).
				Str("path", indexFile).
				Int("line", lineNo+1).
				Err(err).
				Msg("[LocalFileBackend] index corrupt; skipping line")
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// readGlobalIndex 读取全局索引文件。
//
// Python: LocalFileBackend._read_global_index()
func (b *LocalFileBackend) readGlobalIndex() []map[string]any {
	path := b.globalIndexPath()
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("[LocalFileBackend] global index read failed")
		return nil
	}
	var entries []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// writeGlobalIndex 写入全局索引文件。
//
// Python: LocalFileBackend._write_global_index()
func (b *LocalFileBackend) writeGlobalIndex(entries []map[string]any) {
	_ = os.MkdirAll(b.indexDir, 0o755)
	var lines []string
	for _, entry := range entries {
		line, _ := json.Marshal(entry)
		lines = append(lines, string(line))
	}
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	_ = os.WriteFile(b.globalIndexPath(), []byte(content), 0o644)
}

// loadBundle 从文件加载 bundle。
//
// Python: LocalFileBackend._load_bundle()
func (b *LocalFileBackend) loadBundle(skillID, bundleID string) *sharing.SharedSkillBundle {
	if bundleID == "" {
		return nil
	}
	bundleFile := filepath.Join(b.bundleDir(skillID), bundleID+".json")
	if _, err := os.Stat(bundleFile); err != nil {
		logger.Debug(logComponent).
			Str("path", bundleFile).
			Msg("[LocalFileBackend] bundle file missing")
		return nil
	}
	data, err := os.ReadFile(bundleFile)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", bundleFile).
			Err(err).
			Msg("[LocalFileBackend] bundle load failed")
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		logger.Warn(logComponent).
			Str("path", bundleFile).
			Err(err).
			Msg("[LocalFileBackend] bundle load failed")
		return nil
	}
	bundle, err := sharing.FromDictSharedSkillBundle(raw)
	if err != nil {
		logger.Warn(logComponent).
			Str("path", bundleFile).
			Err(err).
			Msg("[LocalFileBackend] bundle decode failed")
		return nil
	}
	return bundle
}

// toStringSliceFromAny 从 map[string]any 中的值安全提取 []string。
func toStringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	slice, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		s, ok := item.(string)
		if ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

// getStrFromAny 从 any 类型安全提取 string。
func getStrFromAny(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// toIntFromAny 从 any 类型安全提取 int。
func toIntFromAny(v any) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	}
	return 0
}

// containsString 检查字符串切片是否包含指定字符串。
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
