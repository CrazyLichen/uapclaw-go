// Package experience —— 配对归档服务（skill evolution 状态回滚生命周期）。
//
// 对应 Python 代码：openjiuwen/agent_evolving/experience/archive.py（325 行 1:1）。
// Python async 语义映射：Go store API 为同步形态，async 方法去 await 直调（15.9
// 以来既定映射，非偏差）。
package experience

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// evolutionFilename 演进日志文件名
	// Python: _EVOLUTION_FILENAME（archive.py:17）
	evolutionFilename = "evolutions.json"
	// latestVersion 用户面 latest 版本记号
	// Python: _LATEST_VERSION（archive.py:18）
	latestVersion = "latest"
	// skillArchivePrefix skill 归档文件名前缀
	// Python: _SKILL_ARCHIVE_PREFIX（archive.py:19）
	skillArchivePrefix = "SKILL."
	// skillArchiveSuffix skill 归档文件名后缀
	// Python: _SKILL_ARCHIVE_SUFFIX（archive.py:20）
	skillArchiveSuffix = ".md"
	// evolutionArchivePrefix 演进归档文件名前缀
	// Python: _EVOLUTION_ARCHIVE_PREFIX（archive.py:21）
	evolutionArchivePrefix = "evolutions."
	// evolutionArchiveSuffix 演进归档文件名后缀
	// Python: _EVOLUTION_ARCHIVE_SUFFIX（archive.py:22）
	evolutionArchiveSuffix = ".json"
	// DefaultArchiveKeepLatest 归档默认保留最新对数
	// Python: DEFAULT_ARCHIVE_KEEP_LATEST（archive.py:23）
	DefaultArchiveKeepLatest = 10
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionArchivePair 一对可回滚的 SKILL.md / evolutions.json 归档。
// Python: EvolutionArchivePair dataclass（frozen）（archive.py:26-47）。
type EvolutionArchivePair struct {
	// Version 版本记号（vYYYYMMDDTHHMMSS[_NN]）
	Version string
	// SkillArchive SKILL.md 归档文件路径
	SkillArchive string
	// EvolutionArchive evolutions.json 归档文件路径
	EvolutionArchive string
}

// SkillArchiveName skill 归档文件名。
// Python: skill_archive_name property（archive.py:34-36）。
func (p EvolutionArchivePair) SkillArchiveName() string {
	return filepath.Base(p.SkillArchive)
}

// EvolutionArchiveName 演进归档文件名。
// Python: evolution_archive_name property（archive.py:38-40）。
func (p EvolutionArchivePair) EvolutionArchiveName() string {
	return filepath.Base(p.EvolutionArchive)
}

// ToPayload 序列化为 dict。
// Python: to_payload()（archive.py:42-47）。
func (p EvolutionArchivePair) ToPayload() map[string]string {
	return map[string]string{
		"version":          p.Version,
		"skill_archive":    p.SkillArchiveName(),
		"evolution_archive": p.EvolutionArchiveName(),
	}
}

// EvolutionArchiveService 持有配对归档不变量（回滚生命周期）。
// Python: EvolutionArchiveService（archive.py:50-318）。
type EvolutionArchiveService struct {
	store      *checkpointing.EvolutionStore
	keepLatest int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionArchiveService 构造归档服务。
// Python: __init__（archive.py:53-55）；keepLatest<=0 取默认值。
func NewEvolutionArchiveService(store *checkpointing.EvolutionStore, keepLatest ...int) *EvolutionArchiveService {
	keep := DefaultArchiveKeepLatest
	if len(keepLatest) > 0 {
		keep = keepLatest[0]
	}
	return &EvolutionArchiveService{store: store, keepLatest: keep}
}

// ListPairs 列出某 subject 的完整归档对，最新在前。
// Python: list_pairs()（archive.py:57-88）。
//
// 步骤：
//
//	Python: 1. name, kind = self._subject_name_and_kind(subject, subject_kind=...)
//	Python: 2. skill_dir = store.resolve_skill_dir(name, subject_kind=kind)；None → []
//	Python: 3. archive_dir = skill_dir / "archive"；非目录 → []
//	Python: 4. archive_names = set(store.list_archives(name, subject_kind=kind))
//	Python: 5. 逆序遍历：版本可解析 且 对应演进归档存在 → 收集配对
func (s *EvolutionArchiveService) ListPairs(ctx context.Context, subject any, subjectKind ...string) []EvolutionArchivePair {
	name, kind := subjectNameAndKind(subject, subjectKind...)
	skillDir := s.store.ResolveSkillDirForKind(ctx, name, false, kind)
	if skillDir == "" {
		return nil
	}
	archiveDir := filepath.Join(skillDir, "archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil
	}
	_ = entries // archive_names 以 store.list_archives 为真相源（与 Python 一致）
	archiveNames := s.store.ListArchives(ctx, name)
	archiveSet := make(map[string]bool, len(archiveNames))
	for _, n := range archiveNames {
		archiveSet[n] = true
	}
	var pairs []EvolutionArchivePair
	sortedNames := append([]string(nil), archiveNames...)
	sort.Sort(sort.Reverse(sort.StringSlice(sortedNames)))
	for _, filename := range sortedNames {
		version := versionFromSkillArchiveName(filename)
		if version == "" {
			continue
		}
		evolutionName := evolutionArchiveNameOf(version)
		if !archiveSet[evolutionName] {
			continue
		}
		pairs = append(pairs, EvolutionArchivePair{
			Version:          version,
			SkillArchive:     filepath.Join(archiveDir, filename),
			EvolutionArchive: filepath.Join(archiveDir, evolutionName),
		})
	}
	return pairs
}

// ArchiveCurrentPair 把当前 SKILL.md 与 evolutions.json 归为一对。
// Python: archive_current_pair()（archive.py:90-147，async）。
//
// 缺失的 evolutions.json 以空日志初始化，保证每个归档版本都是完整回滚目标。
func (s *EvolutionArchiveService) ArchiveCurrentPair(ctx context.Context, subject any, subjectKind ...string) (*EvolutionArchivePair, error) {
	name, kind := subjectNameAndKind(subject, subjectKind...)
	skillDir := s.store.ResolveSkillDirForKind(ctx, name, false, kind)
	if skillDir == "" {
		return nil, nil
	}
	// Python: skill_md = store.find_skill_md(skill_dir) or skill_dir / "SKILL.md"
	skillMD := s.store.FindSkillMD(ctx, skillDir)
	if skillMD == "" {
		skillMD = filepath.Join(skillDir, "SKILL.md")
	}
	if !isFile(skillMD) {
		return nil, nil
	}

	evolutionLog := filepath.Join(skillDir, evolutionFilename)
	if exists(evolutionLog) && !isFile(evolutionLog) {
		return nil, nil
	}
	// Python: await self._ensure_current_evolution_log(...)
	if err := s.ensureCurrentEvolutionLog(ctx, name, evolutionLog, kind); err != nil {
		return nil, err
	}
	if !isFile(evolutionLog) {
		return nil, nil
	}

	archiveDir := filepath.Join(skillDir, "archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return nil, err
	}
	version := nextPairVersion(archiveDir)
	skillArchive := filepath.Join(archiveDir, skillArchiveNameOf(version))
	evolutionArchive := filepath.Join(archiveDir, evolutionArchiveNameOf(version))

	skillContent, err := s.store.ReadFileText(ctx, skillMD)
	if err != nil {
		return nil, err
	}
	evolutionContent, err := s.store.ReadFileText(ctx, evolutionLog)
	if err != nil {
		return nil, err
	}
	// Python: try write pair; except → remove both and raise（:129-135）
	if err := s.store.WriteFileText(ctx, skillArchive, skillContent); err != nil {
		removeFile(skillArchive)
		removeFile(evolutionArchive)
		return nil, err
	}
	if err := s.store.WriteFileText(ctx, evolutionArchive, evolutionContent); err != nil {
		removeFile(skillArchive)
		removeFile(evolutionArchive)
		return nil, err
	}

	kindLabel := kind
	if kindLabel == "" {
		kindLabel = "default"
	}
	logger.Info(logComponent).
		Str("subject", name).Str("kind", kindLabel).Str("version", version).
		Msg("[EvolutionArchiveService] archived current pair")
	return &EvolutionArchivePair{
		Version:          version,
		SkillArchive:     skillArchive,
		EvolutionArchive: evolutionArchive,
	}, nil
}

// RollbackToPair 先归档当前状态，再恢复指定归档对。
// Python: rollback_to_pair()（archive.py:149-194，async）。
func (s *EvolutionArchiveService) RollbackToPair(ctx context.Context, subject any, pairOrVersion any, subjectKind ...string) bool {
	name, kind := subjectNameAndKind(subject, subjectKind...)
	pair := s.resolvePair(ctx, subject, pairOrVersion, kind)
	if pair == nil {
		return false
	}

	skillDir := s.store.ResolveSkillDirForKind(ctx, name, false, kind)
	if skillDir == "" {
		return false
	}
	skillMD := s.store.FindSkillMD(ctx, skillDir)
	if skillMD == "" {
		skillMD = filepath.Join(skillDir, "SKILL.md")
	}
	evolutionLog := filepath.Join(skillDir, evolutionFilename)
	if !currentTargetPathsAreValid(skillMD, evolutionLog) {
		return false
	}
	if !isFile(pair.SkillArchive) || !isFile(pair.EvolutionArchive) {
		return false
	}

	skillContent, err := s.store.ReadFileText(ctx, pair.SkillArchive)
	if err != nil || skillContent == "" {
		return false
	}
	evolutionContent, err := s.store.ReadFileText(ctx, pair.EvolutionArchive)
	if err != nil || evolutionContent == "" {
		return false
	}

	// Python: current_pair = await self.archive_current_pair(...)；None → False
	currentPair, err := s.ArchiveCurrentPair(ctx, name, kind)
	if err != nil || currentPair == nil {
		return false
	}

	if err := s.store.WriteFileText(ctx, skillMD, skillContent); err != nil {
		return false
	}
	if err := s.store.WriteFileText(ctx, evolutionLog, evolutionContent); err != nil {
		return false
	}
	// Python: await self._store.render_evolution_markdown(name, subject_kind=kind)
	if err := s.store.RenderEvolutionMarkdown(ctx, name, checkpointing.WithRecordsSubjectKind(kind)); err != nil {
		logger.Warn(logComponent).Err(err).Str("skill", name).
			Msg("[EvolutionArchiveService] render evolution markdown failed")
	}

	removeFile(pair.SkillArchive)
	removeFile(pair.EvolutionArchive)
	logger.Info(logComponent).Str("version", pair.Version).
		Msg("[EvolutionArchiveService] removed consumed archive pair")

	s.Prune(ctx, name, kind)
	return true
}

// Prune 清理超额完整对，返回移除的对数。
// Python: prune()（archive.py:196-215）。
func (s *EvolutionArchiveService) Prune(ctx context.Context, subject any, subjectKind ...string) int {
	var kind string
	if len(subjectKind) > 0 {
		kind = subjectKind[0]
	}
	keep := s.keepLatest
	if keep < 0 {
		keep = 0
	}
	pairs := s.ListPairs(ctx, subject, kind)
	if len(pairs) <= keep {
		return 0
	}
	pruned := 0
	for _, pair := range pairs[keep:] {
		removeFile(pair.SkillArchive)
		removeFile(pair.EvolutionArchive)
		pruned++
	}
	return pruned
}

// NormalizeArchiveVersion 归一用户面版本记号。
// Python: normalize_version()（archive.py:217-229，staticmethod → 包级函数）。
func NormalizeArchiveVersion(raw string) string {
	version := strings.TrimSpace(raw)
	if version == "" {
		return ""
	}
	if version == latestVersion {
		return latestVersion
	}
	if strings.HasPrefix(version, skillArchivePrefix) && strings.HasSuffix(version, skillArchiveSuffix) {
		version = version[len(skillArchivePrefix) : len(version)-len(skillArchiveSuffix)]
	}
	if strings.HasPrefix(version, "v") {
		return version
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolvePair 解析 pair 对象或版本记号。
// Python: _resolve_pair()（archive.py:231-249）。
func (s *EvolutionArchiveService) resolvePair(ctx context.Context, subject any, pairOrVersion any, kind string) *EvolutionArchivePair {
	switch v := pairOrVersion.(type) {
	case EvolutionArchivePair:
		return &v
	case *EvolutionArchivePair:
		return v
	case string:
		requestedVersion := NormalizeArchiveVersion(v)
		if requestedVersion == "" {
			return nil
		}
		pairs := s.ListPairs(ctx, subject, kind)
		if len(pairs) == 0 {
			return nil
		}
		if requestedVersion == latestVersion {
			return &pairs[0]
		}
		for i := range pairs {
			if pairs[i].Version == requestedVersion {
				return &pairs[i]
			}
		}
		return nil
	}
	return nil
}

// subjectNameAndKind 归一 subject 入参为 (name, kind)。
// Python: _subject_name_and_kind()（archive.py:251-260，staticmethod）。
func subjectNameAndKind(subject any, subjectKind ...string) (string, string) {
	if name, ok := subject.(string); ok {
		var kind string
		if len(subjectKind) > 0 {
			kind = subjectKind[0]
		}
		return name, kind
	}
	normalized, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return "", ""
	}
	return normalized.Name, normalized.Kind
}

// ensureCurrentEvolutionLog 缺失时以空日志初始化当前演进日志。
// Python: _ensure_current_evolution_log()（archive.py:262-277，async）。
func (s *EvolutionArchiveService) ensureCurrentEvolutionLog(ctx context.Context, name, evolutionLog, subjectKind string) error {
	if isFile(evolutionLog) {
		return nil
	}
	// Python: await self._store.save_evolution_log(name, EvolutionLog.empty(skill_id=name), skill_dir=..., subject_kind=...)
	skillDir := filepath.Dir(evolutionLog)
	return s.store.SaveEvolutionLog(ctx, name, checkpointing.EmptyEvolutionLog(name), skillDir)
}

// nextPairVersion 生成下一个配对版本号（秒级时间戳 + 冲突后缀）。
// Python: _next_pair_version()（archive.py:279-290，classmethod）。
func nextPairVersion(archiveDir string) string {
	baseVersion := time.Now().UTC().Format("v20060102T150405")
	version := baseVersion
	suffix := 1
	for fileExists(filepath.Join(archiveDir, skillArchiveNameOf(version))) ||
		fileExists(filepath.Join(archiveDir, evolutionArchiveNameOf(version))) {
		suffix++
		version = fmt.Sprintf("%s_%02d", baseVersion, suffix)
	}
	return version
}

// skillArchiveNameOf 版本 → SKILL 归档文件名。
// Python: _skill_archive_name()（archive.py:292-294，staticmethod）。
func skillArchiveNameOf(version string) string {
	return skillArchivePrefix + version + skillArchiveSuffix
}

// evolutionArchiveNameOf 版本 → 演进归档文件名。
// Python: _evolution_archive_name()（archive.py:296-298，staticmethod）。
func evolutionArchiveNameOf(version string) string {
	return evolutionArchivePrefix + version + evolutionArchiveSuffix
}

// versionFromSkillArchiveName 归档文件名 → 版本（非 SKILL.v*.md 返回空）。
// Python: _version_from_skill_archive_name()（archive.py:300-305，staticmethod）。
func versionFromSkillArchiveName(filename string) string {
	if !strings.HasPrefix(filename, skillArchivePrefix) || !strings.HasSuffix(filename, skillArchiveSuffix) {
		return ""
	}
	version := filename[len(skillArchivePrefix) : len(filename)-len(skillArchiveSuffix)]
	if strings.HasPrefix(version, "v") {
		return version
	}
	return ""
}

// currentTargetPathsAre_valid 校验当前回滚目标路径合法。
// Python: _current_target_paths_are_valid()（archive.py:307-313，staticmethod）。
func currentTargetPathsAreValid(skillMD, evolutionLog string) bool {
	if !isFile(skillMD) {
		return false
	}
	if exists(evolutionLog) && !isFile(evolutionLog) {
		return false
	}
	return true
}

// removeFile 尽力删除（OSError 抑制）。
// Python: _remove_file()（archive.py:315-318，staticmethod）。
func removeFile(path string) {
	_ = os.Remove(path)
}

// isFile 路径存在且为常规文件。
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// exists 路径存在（不区分类型）。
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// fileExists 同 exists（nextPairVersion 内可读性别名）。
func fileExists(path string) bool {
	return exists(path)
}
