package sharing

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SharingBackend 经验共享后端接口。
//
// 与 backend.SharingBackend 方法签名等价，定义在 sharing 包中避免循环依赖。
// backend.LocalFileBackend 隐式实现此接口。
//
// Python: openjiuwen/agent_evolving/sharing/backends/base.py SharingBackend
type SharingBackend interface {
	// UploadBundle 上传经验 bundle，返回上传结果。
	UploadBundle(ctx context.Context, bundle SharedSkillBundle) UploadResult
	// DownloadBundles 按 skill_id 和关键词检索，返回最多 topK 个 bundle。
	DownloadBundles(ctx context.Context, skillID string, query QueryKeywords, topK int) []SharedSkillBundle
	// HasSkillPackage Hub 是否已有该技能包。
	HasSkillPackage(ctx context.Context, skillID string) bool
	// UploadSkillPackage 上传初始技能包（不可变，重复上传为 no-op）。
	UploadSkillPackage(ctx context.Context, skillID string, packageBytes []byte, meta SkillPackageMeta) error
	// DownloadSkillPackage 下载技能包字节。
	DownloadSkillPackage(ctx context.Context, skillID string) ([]byte, error)
	// GetSkillPackageMeta 获取技能包元数据。
	GetSkillPackageMeta(ctx context.Context, skillID string) (*SkillPackageMeta, error)
	// SearchSkills 全局关键词搜索技能。
	SearchSkills(ctx context.Context, query QueryKeywords, topK int) []SkillSearchResult
}

// SkillSharingContextProvider 技能共享上下文提供者函数类型。
//
// 根据技能名称返回 skill_id、技能包字节、解析后名称和描述。
//
// Python: SkillSharingContextProvider = Callable[[str], Awaitable[Tuple[str, bytes, str, str]]]
type SkillSharingContextProvider func(ctx context.Context, skillName string) (skillID string, packageBytes []byte, resolvedName string, description string, err error)

// ExperienceSharer 基于 SharingBackend 的上传/下载门面。
//
// 职责：
//   - 维护每个技能的内存待上传经验队列
//   - flush 时确保 skill_id，上传初始技能包（仅一次），然后上传经验 bundle
//   - 按 skill_id 下载 bundle 和搜索技能
//
// Python: openjiuwen/agent_evolving/sharing/experience_sharer.py ExperienceSharer
type ExperienceSharer struct {
	// backend 共享后端
	backend SharingBackend
	// localCacheDir 本地镜像缓存目录（可选，nil 则不镜像）
	localCacheDir *string
	// maxUploadRetries 最大上传重试次数，默认 3
	maxUploadRetries int
	// backoffBaseSecs 退避基准秒数，默认 0.5
	backoffBaseSecs float64
	// pendingUploads 待上传队列：skill_name → 经验列表
	pendingUploads map[string][]SharedExperience
	// pendingKeys 去重集合：skill_name → dedupKey 集合
	pendingKeys map[string]map[dedupKey]bool
	// mu 读写互斥锁
	mu sync.RWMutex
	// skillSharingContextProvider 技能共享上下文提供者（可选，后置绑定）
	skillSharingContextProvider SkillSharingContextProvider
}

// dedupKey 去重键：按 (skill_name, record_id) 去重。
type dedupKey struct {
	skillName string
	recordID  string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultUploadRetries 默认最大上传重试次数
	defaultUploadRetries = 3
	// defaultBackoffSecs 默认退避基准秒数
	defaultBackoffSecs = 0.5
)

// logComponent 日志组件常量（同 keyword_extractor.go 中声明，包内共享）
// 不再重复声明

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExperienceSharer 创建 ExperienceSharer 实例。
//
// maxUploadRetries 最小为 1，backoffBaseSecs 最小为 0。
// localCacheDir 为空则不镜像；含 ~ 会展开为主目录。
//
// Python: ExperienceSharer.__init__()
func NewExperienceSharer(
	bk SharingBackend,
	localCacheDir string,
	maxUploadRetries int,
	backoffBaseSecs float64,
	provider SkillSharingContextProvider,
) *ExperienceSharer {
	if maxUploadRetries < 1 {
		maxUploadRetries = 1
	}
	if backoffBaseSecs < 0 {
		backoffBaseSecs = 0
	}

	var cacheDir *string
	if localCacheDir != "" {
		expanded := path.ExpandHome(localCacheDir)
		abs, err := filepath.Abs(expanded)
		if err != nil {
			abs = expanded
		}
		cacheDir = &abs
	}

	return &ExperienceSharer{
		backend:                     bk,
		localCacheDir:               cacheDir,
		maxUploadRetries:            maxUploadRetries,
		backoffBaseSecs:             backoffBaseSecs,
		pendingUploads:              make(map[string][]SharedExperience),
		pendingKeys:                 make(map[string]map[dedupKey]bool),
		skillSharingContextProvider: provider,
	}
}

// SetSkillSharingContextProvider 后置绑定技能共享上下文提供者。
//
// Python: ExperienceSharer.set_skill_sharing_context_provider()
func (es *ExperienceSharer) SetSkillSharingContextProvider(provider SkillSharingContextProvider) {
	es.skillSharingContextProvider = provider
}

// Backend 返回共享后端。
//
// Python: ExperienceSharer.backend (property)
func (es *ExperienceSharer) Backend() SharingBackend {
	return es.backend
}

// LocalCacheDir 返回本地缓存目录路径（可能为 nil）。
//
// Python: ExperienceSharer.local_cache_dir (property)
func (es *ExperienceSharer) LocalCacheDir() *string {
	return es.localCacheDir
}

// ResolveSkillID 通过 provider 获取 skill_id。
//
// provider 为空或 skillName 为空时返回空字符串。
//
// Python: ExperienceSharer.resolve_skill_id()
func (es *ExperienceSharer) ResolveSkillID(ctx context.Context, skillName string) string {
	provider := es.skillSharingContextProvider
	if provider == nil || skillName == "" {
		return ""
	}
	skillID, _, _, _, err := provider(ctx, skillName)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill", skillName).
			Err(err).
			Msg("[ExperienceSharer] resolve_skill_id failed")
		return ""
	}
	return strings.TrimSpace(skillID)
}

// HasPending 是否有待上传的经验。
//
// Python: ExperienceSharer.has_pending()
func (es *ExperienceSharer) HasPending(skillName string) bool {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return len(es.pendingUploads[skillName]) > 0
}

// StageForUpload 将经验入队，按 (skill_name, record_id) 去重。
//
// skillName 为空时或 exp 为 nil 指针时不入队。
//
// Python: ExperienceSharer.stage_for_upload()
func (es *ExperienceSharer) StageForUpload(skillName string, exp *SharedExperience) {
	if skillName == "" || exp == nil {
		return
	}
	recordID := strings.TrimSpace(exp.Record.ID)

	es.mu.Lock()
	defer es.mu.Unlock()

	dk := dedupKey{skillName: skillName, recordID: recordID}
	keys, ok := es.pendingKeys[skillName]
	if !ok {
		keys = make(map[dedupKey]bool)
		es.pendingKeys[skillName] = keys
	}
	if keys[dk] {
		logger.Debug(logComponent).
			Str("skill", skillName).
			Str("record", recordID).
			Msg("[ExperienceSharer] stage_for_upload deduplicated")
		return
	}
	keys[dk] = true
	es.pendingUploads[skillName] = append(es.pendingUploads[skillName], *exp)
	logger.Debug(logComponent).
		Str("skill", skillName).
		Int("queue", len(es.pendingUploads[skillName])).
		Msg("[ExperienceSharer] staged 1 experience")
}

// DiscardPendingUploads 丢弃指定技能的待上传队列，返回丢弃数量。
//
// Python: ExperienceSharer.discard_pending_uploads()
func (es *ExperienceSharer) DiscardPendingUploads(skillName string) int {
	es.mu.Lock()
	defer es.mu.Unlock()

	count := len(es.pendingUploads[skillName])
	delete(es.pendingUploads, skillName)
	delete(es.pendingKeys, skillName)
	if count > 0 {
		logger.Info(logComponent).
			Int("count", count).
			Str("skill", skillName).
			Msg("[ExperienceSharer] discarded pending experience(s)")
	}
	return count
}

// FlushPendingUploads 打包并上传指定技能的所有待上传经验。
//
// 加写锁取走队列 → MakeSharedSkillBundle → syncSkillPackage → 重试循环 → 成功后 mirrorBundle。
//
// Python: ExperienceSharer.flush_pending_uploads()
func (es *ExperienceSharer) FlushPendingUploads(ctx context.Context, skillName string) UploadResult {
	es.mu.Lock()
	experiences := es.pendingUploads[skillName]
	delete(es.pendingUploads, skillName)
	delete(es.pendingKeys, skillName)
	es.mu.Unlock()

	if len(experiences) == 0 {
		return UploadResult{OK: true}
	}

	bundle := MakeSharedSkillBundle(skillName, experiences, "", "")
	es.syncSkillPackage(ctx, bundle, skillName)

	if bundle.SkillID == "" {
		reason := "skill_id unavailable"
		logger.Warn(logComponent).
			Str("skill", skillName).
			Str("reason", reason).
			Msg("[ExperienceSharer] skipping upload")
		return UploadResult{OK: false, Reason: reason}
	}

	attempt := 0
	lastResult := UploadResult{OK: false, Reason: "upload not attempted"}
	for attempt < es.maxUploadRetries {
		attempt++
		result := es.backend.UploadBundle(ctx, *bundle)
		if result.OK {
			es.mirrorBundle(bundle, "uploaded")
			logger.Info(logComponent).
				Str("bundle_id", firstNonEmpty(result.BundleID, bundle.BundleID)).
				Str("skill", skillName).
				Str("skill_id", bundle.SkillID).
				Int("attempts", attempt).
				Msg("[ExperienceSharer] flushed bundle")
			return result
		}

		lastResult = result
		logger.Warn(logComponent).
			Int("attempt", attempt).
			Int("max_attempts", es.maxUploadRetries).
			Str("skill", skillName).
			Str("skill_id", bundle.SkillID).
			Str("reason", result.Reason).
			Msg("[ExperienceSharer] upload attempt rejected")

		if !result.Retryable {
			return result
		}
		if attempt < es.maxUploadRetries && es.backoffBaseSecs > 0 {
			delay := es.backoffBaseSecs * math.Pow(2, float64(attempt-1))
			time.Sleep(time.Duration(delay * float64(time.Second)))
		}
	}

	logger.Error(logComponent).
		Str("skill", skillName).
		Str("skill_id", bundle.SkillID).
		Int("attempts", es.maxUploadRetries).
		Str("reason", lastResult.Reason).
		Msg("[ExperienceSharer] giving up upload")
	return lastResult
}

// DownloadRelevant 下载并镜像最多 topK 个相关 bundle。
//
// Python: ExperienceSharer.download_relevant()
func (es *ExperienceSharer) DownloadRelevant(
	ctx context.Context,
	skillID string,
	query QueryKeywords,
	topK int,
	skillName string,
) []SharedSkillBundle {
	resolvedID := strings.TrimSpace(skillID)
	if resolvedID == "" {
		return nil
	}

	var bundles []SharedSkillBundle
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).
					Str("skill", skillName).
					Str("skill_id", resolvedID).
					Any("panic", r).
					Msg("[ExperienceSharer] backend download failed")
			}
		}()
		bundles = es.backend.DownloadBundles(ctx, resolvedID, query, topK)
	}()

	for i := range bundles {
		es.mirrorBundle(&bundles[i], "downloaded")
	}
	if len(bundles) > 0 {
		logger.Info(logComponent).
			Int("count", len(bundles)).
			Str("skill", firstNonEmpty(skillName, "?")).
			Str("skill_id", resolvedID).
			Int("top_k", topK).
			Msg("[ExperienceSharer] downloaded bundle(s)")
	}
	return bundles
}

// SearchSkills 全局搜索技能。
//
// Python: ExperienceSharer.search_skills()
func (es *ExperienceSharer) SearchSkills(ctx context.Context, query QueryKeywords, topK int) []SkillSearchResult {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Any("panic", r).
				Msg("[ExperienceSharer] search_skills failed")
		}
	}()
	return es.backend.SearchSkills(ctx, query, topK)
}

// DownloadSkillPackage 下载技能包字节。
//
// Python: ExperienceSharer.download_skill_package()
func (es *ExperienceSharer) DownloadSkillPackage(ctx context.Context, skillID string) []byte {
	resolvedID := strings.TrimSpace(skillID)
	if resolvedID == "" {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("skill_id", resolvedID).
				Any("panic", r).
				Msg("[ExperienceSharer] download_skill_package failed")
		}
	}()
	data, err := es.backend.DownloadSkillPackage(ctx, resolvedID)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill_id", resolvedID).
			Err(err).
			Msg("[ExperienceSharer] download_skill_package failed")
		return nil
	}
	return data
}

// GetSkillPackageMeta 获取技能包元数据。
//
// Python: ExperienceSharer.get_skill_package_meta()
func (es *ExperienceSharer) GetSkillPackageMeta(ctx context.Context, skillID string) *SkillPackageMeta {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("skill_id", skillID).
				Any("panic", r).
				Msg("[ExperienceSharer] get_skill_package_meta failed")
		}
	}()
	meta, err := es.backend.GetSkillPackageMeta(ctx, strings.TrimSpace(skillID))
	if err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Err(err).
			Msg("[ExperienceSharer] get_skill_package_meta failed")
		return nil
	}
	return meta
}

// ListCachedBundles 列出本地下载缓存中的 bundle。
//
// Python: ExperienceSharer.list_cached_bundles()
func (es *ExperienceSharer) ListCachedBundles(skillID string) []SharedSkillBundle {
	resolvedID := strings.TrimSpace(skillID)
	if es.localCacheDir == nil || resolvedID == "" {
		return nil
	}
	skillDir := filepath.Join(*es.localCacheDir, "downloaded", resolvedID)
	info, err := os.Stat(skillDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return nil
	}

	var bundles []SharedSkillBundle
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		bundleFile := filepath.Join(skillDir, entry.Name())
		data, err := os.ReadFile(bundleFile)
		if err != nil {
			logger.Warn(logComponent).
				Str("path", bundleFile).
				Err(err).
				Msg("[ExperienceSharer] cached bundle decode failed")
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			logger.Warn(logComponent).
				Str("path", bundleFile).
				Err(err).
				Msg("[ExperienceSharer] cached bundle decode failed")
			continue
		}
		bundle, err := FromDictSharedSkillBundle(raw)
		if err != nil {
			logger.Warn(logComponent).
				Str("path", bundleFile).
				Err(err).
				Msg("[ExperienceSharer] cached bundle decode failed")
			continue
		}
		bundles = append(bundles, *bundle)
	}
	return bundles
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// syncSkillPackage 通过 provider 确保 skill_id 并上传初始技能包。
//
// Python: ExperienceSharer._sync_skill_package()
func (es *ExperienceSharer) syncSkillPackage(ctx context.Context, bundle *SharedSkillBundle, skillName string) {
	provider := es.skillSharingContextProvider
	if provider == nil {
		return
	}

	skillID, packageBytes, resolvedName, description, err := provider(ctx, skillName)
	if err != nil {
		logger.Warn(logComponent).
			Str("skill", skillName).
			Err(err).
			Msg("[ExperienceSharer] skill_sharing_context_provider failed")
		return
	}

	skillID = strings.TrimSpace(skillID)
	if skillID == "" {
		logger.Debug(logComponent).
			Str("skill", skillName).
			Msg("[ExperienceSharer] skill_sharing_context_provider returned empty skill_id")
		return
	}

	bundle.SkillID = skillID
	if resolvedName != "" {
		bundle.SkillName = resolvedName
	}

	alreadyPresent := es.backend.HasSkillPackage(ctx, skillID)
	if alreadyPresent {
		logger.Debug(logComponent).
			Str("skill_id", skillID).
			Msg("[ExperienceSharer] hub already has skill package")
		return
	}

	if len(packageBytes) == 0 {
		logger.Warn(logComponent).
			Str("skill", skillName).
			Str("skill_id", skillID).
			Msg("[ExperienceSharer] empty skill package; skipping package upload")
		return
	}

	meta := SkillPackageMeta{
		SkillID:     skillID,
		SkillName:   firstNonEmpty(resolvedName, skillName),
		Description: description,
	}
	if err := es.backend.UploadSkillPackage(ctx, skillID, packageBytes, meta); err != nil {
		logger.Warn(logComponent).
			Str("skill_id", skillID).
			Err(err).
			Msg("[ExperienceSharer] upload_skill_package failed; bundle upload will continue")
	} else {
		logger.Info(logComponent).
			Str("skill", skillName).
			Str("skill_id", skillID).
			Msg("[ExperienceSharer] uploaded initial skill package")
	}
}

// mirrorBundle 将 bundle 写入本地缓存目录。
//
// kind 只允许 "uploaded" 或 "downloaded"。
// 路径: local_cache_dir/{kind}/{skill_id}/{bundle_id}.json
//
// Python: ExperienceSharer._mirror_bundle()
func (es *ExperienceSharer) mirrorBundle(bundle *SharedSkillBundle, kind string) {
	skillID := strings.TrimSpace(bundle.SkillID)
	if es.localCacheDir == nil || bundle.BundleID == "" || skillID == "" {
		return
	}
	if kind != "uploaded" && kind != "downloaded" {
		panic(fmt.Sprintf("unsupported mirror kind: %s", kind))
	}

	targetDir := filepath.Join(*es.localCacheDir, kind, skillID)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("kind", kind).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[ExperienceSharer] mirror failed")
		return
	}
	data, err := json.MarshalIndent(bundle.ToDict(), "", "  ")
	if err != nil {
		logger.Warn(logComponent).
			Str("kind", kind).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[ExperienceSharer] mirror failed")
		return
	}
	targetFile := filepath.Join(targetDir, bundle.BundleID+".json")
	if err := os.WriteFile(targetFile, data, 0o644); err != nil {
		logger.Warn(logComponent).
			Str("kind", kind).
			Str("bundle_id", bundle.BundleID).
			Err(err).
			Msg("[ExperienceSharer] mirror failed")
	}
}

// firstNonEmpty 返回第一个非空字符串，都为空则返回空。
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
