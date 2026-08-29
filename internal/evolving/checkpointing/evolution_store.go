package checkpointing

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvolutionStoreReader 技能经验优化器所需的演进存储只读接口。
// 从 skill_call/team_optimizer.go 迁移至此，与 Python evolution_store.py 位置对齐。
//
// 对应 Python: EvolutionStore
type EvolutionStoreReader interface {
	// ReadSkillContent 读取技能内容
	ReadSkillContent(ctx context.Context, skillName string) (string, error)
	// LoadFullEvolutionLog 加载完整演进日志
	LoadFullEvolutionLog(ctx context.Context, skillName string) (*EvolutionLog, error)
}

// EvolutionStore 技能演进数据的文件系统 IO 门面。
//
// 组合三个 Helper（Records/Projection/Archive）+ SysOperation 路由 + RWMutex 并发控制。
// 所有方法为同步方法（对齐 Python 的 asyncio 在 Go 中改为同步 + 锁）。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/evolution_store.py EvolutionStore
type EvolutionStore struct {
	// baseDirs 配置的技能基础目录列表（resolve 后的绝对路径）
	baseDirs []string
	// sysOperation 可选注入的 SysOperation，缺省用本地 os/fs
	sysOperation sys_operation.SysOperation
	// skillLocks 技能级读写锁（惰性创建）
	skillLocks map[string]*sync.RWMutex
	// mu 保护 skillLocks 创建
	mu sync.Mutex
	// records 记录持久化辅助
	records *StoreRecordsHelper
	// projection Markdown 投影辅助
	projection *StoreProjectionHelper
	// archive 归档/创建辅助
	archive *StoreArchiveHelper
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// evolutionFilename 演进数据文件名
	// 对应 Python: _EVOLUTION_FILENAME = "evolutions.json"
	evolutionFilename = "evolutions.json"
	// totalWarningThreshold 演进记录总量告警阈值
	// 对应 Python: _TOTAL_WARNING_THRESHOLD = 30
	totalWarningThreshold = 30
	// maxInjectDesc 最大注入描述经验条数
	// 对应 Python: _MAX_INJECT_DESC = 5
	maxInjectDesc = 5
)

const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言：EvolutionStore 必须满足 EvolutionStoreReader 接口
var _ EvolutionStoreReader = (*EvolutionStore)(nil)

// evolutionIndexPattern evolution-index 块正则
// 对应 Python: _EVOLUTION_INDEX_PATTERN
var evolutionIndexPattern = regexp.MustCompile(
	`<!-- evolution-index-start -->.*?<!-- evolution-index-end -->`,
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvolutionStore 创建 EvolutionStore 实例。
//
// 对应 Python: EvolutionStore.__init__(skills_base_dir: Union[str, List[str]])
// skillsBaseDirs 支持 string 和 []string 两种输入，对齐 Python Union[str, List[str]]
func NewEvolutionStore(skillsBaseDirs any, sysOp sys_operation.SysOperation) *EvolutionStore {
	var dirs []string
	switch v := skillsBaseDirs.(type) {
	case string:
		dirs = normalizeBaseDirs(v)
	case []string:
		dirs = normalizeBaseDirsFromList(v)
	default:
		panic(fmt.Sprintf("skillsBaseDirs 必须为 string 或 []string，实际类型: %T", skillsBaseDirs))
	}
	if len(dirs) == 0 {
		panic("skills_base_dir 为空")
	}
	s := &EvolutionStore{
		baseDirs:     dirs,
		sysOperation: sysOp,
		skillLocks:   map[string]*sync.RWMutex{},
	}
	s.records = &StoreRecordsHelper{store: s}
	s.projection = &StoreProjectionHelper{store: s}
	s.archive = &StoreArchiveHelper{store: s}
	return s
}

// BaseDirs 返回配置的技能基础目录列表。
func (s *EvolutionStore) BaseDirs() []string {
	return s.baseDirs
}

// BaseDir 返回第一个配置的技能基础目录（兼容属性）。
func (s *EvolutionStore) BaseDir() string {
	return s.baseDirs[0]
}

// ListSkillNames 列出所有技能名称。
// 对应 Python: EvolutionStore.list_skill_names()
func (s *EvolutionStore) ListSkillNames(ctx context.Context) []string {
	var names []string
	seen := map[string]bool{}
	for _, root := range s.baseDirs {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
				continue
			}
			if seen[entry.Name()] {
				continue
			}
			seen[entry.Name()] = true
			names = append(names, entry.Name())
		}
	}
	return names
}

// SkillExists 判断技能是否存在。
func (s *EvolutionStore) SkillExists(ctx context.Context, name string) bool {
	return s.ResolveSkillDir(ctx, name) != ""
}

// ResolveSkillDir 查找技能目录路径，create=true 时返回第一个 baseDir/name。
// 对应 Python: EvolutionStore.resolve_skill_dir(name, create)
func (s *EvolutionStore) ResolveSkillDir(ctx context.Context, name string, create ...bool) string {
	doCreate := len(create) > 0 && create[0]
	for _, base := range s.baseDirs {
		candidate := filepath.Join(base, name)
		if isDir(candidate) {
			return candidate
		}
	}
	if doCreate && len(s.baseDirs) > 0 {
		return filepath.Join(s.baseDirs[0], name)
	}
	return ""
}

// FindSkillMD 查找技能的 Markdown 入口文件。
// 对应 Python: EvolutionStore._find_skill_md(skill_dir)
func (s *EvolutionStore) FindSkillMD(ctx context.Context, skillDir string) string {
	skillMD := filepath.Join(skillDir, "SKILL.md")
	if isFile(skillMD) {
		return skillMD
	}
	// 对齐 Python: md_files = list(skill_dir.glob("*.md"))
	entries, err := os.ReadDir(skillDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			return filepath.Join(skillDir, entry.Name())
		}
	}
	return ""
}

// ReadFileText 读取文本文件，路由通过 sysOperation。
// 对应 Python: EvolutionStore.read_file_text(path)
func (s *EvolutionStore) ReadFileText(ctx context.Context, path string) (string, error) {
	localRead := func() (string, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	if s.sysOperation != nil {
		fsOp := s.sysOperation.Fs()
		if fsOp != nil {
			// 对齐 Python: result = await self.sys_operation.fs().read_file(...)
			result, err := fsOp.ReadFile(ctx, path)
			if err != nil || result == nil || result.Code != 0 {
				logger.Warn(logComponent).
					Str("path", path).
					Err(err).
					Msg("[EvolutionStore] sys_operation 读文件失败，fallback 到本地")
				return localRead()
			}
			if result.Data != nil && result.Data.Content != "" {
				return result.Data.Content, nil
			}
			return localRead()
		}
	}
	return localRead()
}

// WriteFileText 写入文本文件，路由通过 sysOperation。
// 对应 Python: EvolutionStore.write_file_text(path, content)
func (s *EvolutionStore) WriteFileText(ctx context.Context, path string, content string) error {
	localWrite := func() error {
		// 确保父目录存在
		dir := filepath.Dir(path)
		_ = os.MkdirAll(dir, 0755)
		return os.WriteFile(path, []byte(content), 0644)
	}

	if s.sysOperation != nil {
		fsOp := s.sysOperation.Fs()
		if fsOp != nil {
			result, err := fsOp.WriteFile(ctx, path, content)
			if err != nil {
				logger.Warn(logComponent).
					Str("path", path).
					Err(err).
					Msg("[EvolutionStore] sys_operation 写文件失败")
				return localWrite()
			}
			if result != nil && result.Code != 0 {
				logger.Warn(logComponent).
					Str("path", path).
					Str("message", result.Message).
					Msg("[EvolutionStore] sys_operation 写文件返回非零状态码")
				return localWrite()
			}
			return nil
		}
	}
	return localWrite()
}

// ReadSkillContent 读取技能 SKILL.md 内容。
// 对应 Python: EvolutionStore.read_skill_content(name)
func (s *EvolutionStore) ReadSkillContent(ctx context.Context, name string) (string, error) {
	skillDir := s.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return "", nil
	}
	mdPath := s.FindSkillMD(ctx, skillDir)
	if mdPath == "" {
		return "", nil
	}
	return s.ReadFileText(ctx, mdPath)
}

// ReadPristineSkillContent 读取不含 evolution-index 块的 SKILL.md 内容。
// 对应 Python: EvolutionStore.read_pristine_skill_content(name)
func (s *EvolutionStore) ReadPristineSkillContent(ctx context.Context, name string) (string, error) {
	content, err := s.ReadSkillContent(ctx, name)
	if err != nil {
		return "", err
	}
	if content == "" {
		return "", nil
	}
	stripped := evolutionIndexPattern.ReplaceAllString(content, "")
	return strings.TrimSpace(stripped) + "\n", nil
}

// ReadSkillID 从 SKILL.md frontmatter 读取 skill_id。
func (s *EvolutionStore) ReadSkillID(ctx context.Context, name string) (string, error) {
	content, err := s.ReadSkillContent(ctx, name)
	if err != nil {
		return "", err
	}
	if content == "" {
		return "", nil
	}
	return ReadSkillIDFromContent(content), nil
}

// EnsureSkillID 确保 SKILL.md 包含 skill_id。
func (s *EvolutionStore) EnsureSkillID(ctx context.Context, name string) (string, error) {
	skillDir := s.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return "", nil
	}
	mdPath := s.FindSkillMD(ctx, skillDir)
	if mdPath == "" {
		return "", nil
	}
	content, err := s.ReadFileText(ctx, mdPath)
	if err != nil {
		return "", err
	}
	if content == "" {
		return "", nil
	}
	updated, skillID := EnsureSkillIDInContent(content)
	if updated != content {
		if err := s.WriteFileText(ctx, mdPath, updated); err != nil {
			return "", err
		}
		logger.Info(logComponent).
			Str("skill_id", skillID).
			Str("skill", name).
			Msg("[EvolutionStore] 分配 skill_id")
	}
	return skillID, nil
}

// PackSkillForSharing 构建技能目录的 tarball 用于 hub 上传。
func (s *EvolutionStore) PackSkillForSharing(ctx context.Context, name string) ([]byte, error) {
	skillDir := s.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return nil, nil
	}
	mdPath := s.FindSkillMD(ctx, skillDir)
	if mdPath == "" {
		return PackSkillDirectory(skillDir, "", "")
	}
	pristine, err := s.ReadPristineSkillContent(ctx, name)
	if err != nil {
		return nil, err
	}
	if pristine == "" {
		return PackSkillDirectory(skillDir, "", "")
	}
	relpath, err := filepath.Rel(skillDir, mdPath)
	if err != nil {
		relpath = "SKILL.md"
	}
	relpath = filepath.ToSlash(relpath)
	return PackSkillDirectory(skillDir, relpath, pristine)
}

// InstallSkillPackage 从 hub 技能包解压到本地目录。
func (s *EvolutionStore) InstallSkillPackage(ctx context.Context, packageBytes []byte, skillName string) (string, error) {
	if len(packageBytes) == 0 {
		return "", nil
	}

	resolvedName := strings.TrimSpace(skillName)
	if resolvedName == "" {
		// 对齐 Python: 从 tarball 推断名称
		resolvedName = inferSkillNameFromPackage(packageBytes)
	}
	if resolvedName == "" {
		logger.Warn(logComponent).Msg("[EvolutionStore] install_skill_package: 无法推断技能名")
		return "", nil
	}

	destDir := s.ResolveSkillDir(ctx, resolvedName, true)
	if destDir == "" {
		return "", nil
	}
	if isDir(destDir) && hasFiles(destDir) {
		logger.Warn(logComponent).
			Str("dest_dir", destDir).
			Msg("[EvolutionStore] install_skill_package: 技能目录已存在")
		return "", nil
	}

	if err := UnpackSkillPackage(packageBytes, destDir); err != nil {
		return "", err
	}
	logger.Info(logComponent).
		Str("dest_dir", destDir).
		Msg("[EvolutionStore] 安装技能包")
	return destDir, nil
}

// WriteSkillContent 写入技能 SKILL.md 内容。
func (s *EvolutionStore) WriteSkillContent(ctx context.Context, name string, content string) (bool, error) {
	skillDir := s.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		logger.Warn(logComponent).
			Str("skill", name).
			Msg("[EvolutionStore] write_skill_content: 技能未找到")
		return false, nil
	}
	mdPath := s.FindSkillMD(ctx, skillDir)
	if mdPath == "" {
		mdPath = filepath.Join(skillDir, "SKILL.md")
	}
	if err := s.WriteFileText(ctx, mdPath, content); err != nil {
		logger.Error(logComponent).
			Str("skill", name).
			Err(err).
			Msg("[EvolutionStore] write_skill_content 失败")
		return false, err
	}
	logger.Info(logComponent).
		Str("skill", name).
		Msg("[EvolutionStore] 写入 SKILL.md")
	return true, nil
}

// LoadEvolutionLog 加载演进日志（可按 target 过滤）。
func (s *EvolutionStore) LoadEvolutionLog(ctx context.Context, name string, target *signal.EvolutionTarget) *EvolutionLog {
	evoLog, err := s.LoadFullEvolutionLog(ctx, name)
	if err != nil {
		logger.Warn(logComponent).Str("skill", name).Err(err).Msg("[EvolutionStore] load_evolution_log 失败")
		return EmptyEvolutionLog(name)
	}
	if target != nil {
		filtered := make([]EvolutionRecord, 0)
		for _, record := range evoLog.Entries {
			if record.Change.Target == *target {
				filtered = append(filtered, record)
			}
		}
		return &EvolutionLog{
			SkillID:   evoLog.SkillID,
			Version:   evoLog.Version,
			UpdatedAt: evoLog.UpdatedAt,
			Entries:   filtered,
		}
	}
	return evoLog
}

// AppendRecord 追加或合并一条演进记录。
// 对应 Python: EvolutionStore.append_record(name, record)
func (s *EvolutionStore) AppendRecord(ctx context.Context, name string, record EvolutionRecord) error {
	lock := s.getSkillLock(name)
	lock.Lock()
	defer lock.Unlock()

	skillDir := s.ResolveSkillDir(ctx, name, true)
	if skillDir == "" {
		return nil
	}

	if record.Change.Target == signal.EvolutionTargetScript {
		if err := s.records.PersistScript(ctx, skillDir, &record); err != nil {
			logger.Warn(logComponent).Err(err).Str("record_id", record.ID).
				Msg("[EvolutionStore] 持久化脚本失败")
		}
	}

	evoLog, loadErr := s.LoadFullEvolutionLog(ctx, name)
	if loadErr != nil {
		lock.Unlock()
		return fmt.Errorf("load evolution log for append_record: %w", loadErr)
	}
	mergeTarget := record.Change.MergeTarget
	if mergeTarget != nil && *mergeTarget != "" {
		replaced := false
		for idx, existing := range evoLog.Entries {
			if existing.ID == *mergeTarget {
				evoLog.Entries[idx] = record
				replaced = true
				logger.Info(logComponent).
					Str("record_id", record.ID).
					Str("merge_target", *mergeTarget).
					Msg("[EvolutionStore] 合并记录替换")
				break
			}
		}
		if !replaced {
			evoLog.Entries = append(evoLog.Entries, record)
		}
	} else {
		evoLog.Entries = append(evoLog.Entries, record)
	}

	evoLog.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.records.SaveEvolutionLog(ctx, name, evoLog, skillDir); err != nil {
		return err
	}

	logger.Info(logComponent).
		Str("skill", name).
		Str("file", evolutionFilename).
		Str("record_id", record.ID).
		Str("target", string(record.Change.Target)).
		Msg("[EvolutionStore] 写入演进记录")

	total := len(evoLog.Entries)
	if total >= totalWarningThreshold {
		logger.Warn(logComponent).
			Str("skill", name).
			Int("total", total).
			Msg("[EvolutionStore] 演进经验过多，建议 /evolve_simplify")
	}

	_ = s.RenderEvolutionMarkdown(ctx, name)
	return nil
}

// LoadFullEvolutionLog 加载完整演进日志。
func (s *EvolutionStore) LoadFullEvolutionLog(ctx context.Context, name string) (*EvolutionLog, error) {
	return s.records.LoadFullEvolutionLog(ctx, name)
}

// SaveEvolutionLog 持久化演进日志。
func (s *EvolutionStore) SaveEvolutionLog(ctx context.Context, name string, evoLog *EvolutionLog, skillDir string) error {
	return s.records.SaveEvolutionLog(ctx, name, evoLog, skillDir)
}

// GetPendingRecords 获取待定记录列表。
func (s *EvolutionStore) GetPendingRecords(ctx context.Context, name string, target *signal.EvolutionTarget) []EvolutionRecord {
	return s.LoadEvolutionLog(ctx, name, target).PendingEntries()
}

// RenderEvolutionMarkdown 渲染演进 Markdown。
func (s *EvolutionStore) RenderEvolutionMarkdown(ctx context.Context, name string) error {
	return s.projection.RenderEvolutionMarkdown(ctx, name)
}

// FormatDescExperienceText 格式化描述层经验文本。
func (s *EvolutionStore) FormatDescExperienceText(ctx context.Context, name string, maxItems int) string {
	return s.projection.FormatDescExperienceText(ctx, name, maxItems)
}

// FormatAllDescExperiences 格式化所有技能的描述经验。
func (s *EvolutionStore) FormatAllDescExperiences(ctx context.Context, names []string) map[string]string {
	return s.projection.FormatAllDescExperiences(ctx, names)
}

// FormatBodyExperienceText 格式化主体层经验文本。
func (s *EvolutionStore) FormatBodyExperienceText(ctx context.Context, name string) string {
	return s.projection.FormatBodyExperienceText(ctx, name)
}

// ListPendingSummary 列出待定经验摘要。
func (s *EvolutionStore) ListPendingSummary(ctx context.Context, names []string) string {
	return s.projection.ListPendingSummary(ctx, names)
}

// UpdateRecordScores 更新记录分数（RMW 操作，加写锁）。
func (s *EvolutionStore) UpdateRecordScores(ctx context.Context, name string, updates map[string]map[string]any) (int, error) {
	lock := s.getSkillLock(name)
	lock.Lock()
	defer lock.Unlock()
	return s.records.UpdateRecordScores(ctx, name, updates)
}

// GetRecordsByScore 按分数获取记录（只读操作，加读锁）。
func (s *EvolutionStore) GetRecordsByScore(ctx context.Context, name string, minScore *float64) []EvolutionRecord {
	lock := s.getSkillLock(name)
	lock.RLock()
	defer lock.RUnlock()
	return s.records.GetRecordsByScore(ctx, name, minScore)
}

// DeleteRecords 删除记录（RMW 操作，加写锁）。
func (s *EvolutionStore) DeleteRecords(ctx context.Context, name string, recordIDs []string) (int, error) {
	lock := s.getSkillLock(name)
	lock.Lock()
	defer lock.Unlock()
	return s.records.DeleteRecords(ctx, name, recordIDs)
}

// MarkRecordsApplied 标记记录已应用（RMW 操作，加写锁）。
func (s *EvolutionStore) MarkRecordsApplied(ctx context.Context, name string, recordIDs []string) (int, error) {
	lock := s.getSkillLock(name)
	lock.Lock()
	defer lock.Unlock()
	return s.records.MarkRecordsApplied(ctx, name, recordIDs)
}

// MergeRecords 合并记录（RMW 操作，加写锁）。
func (s *EvolutionStore) MergeRecords(ctx context.Context, name string, primaryID string, removeIDs []string, newContent string, newScore *float64) (*EvolutionRecord, error) {
	lock := s.getSkillLock(name)
	lock.Lock()
	defer lock.Unlock()
	return s.records.MergeRecords(ctx, name, primaryID, removeIDs, newContent, newScore)
}

// UpdateRecordContent 更新记录内容（RMW 操作，加写锁）。
func (s *EvolutionStore) UpdateRecordContent(ctx context.Context, name string, recordID string, newContent string, newScore *float64) (*EvolutionRecord, error) {
	lock := s.getSkillLock(name)
	lock.Lock()
	defer lock.Unlock()
	return s.records.UpdateRecordContent(ctx, name, recordID, newContent, newScore)
}

// CreateSkill 创建新技能。
func (s *EvolutionStore) CreateSkill(ctx context.Context, name string, description string, body string, frontmatter string) (string, error) {
	return s.archive.CreateSkill(ctx, name, description, body, frontmatter)
}

// ListSkillNamesWithDescriptions 列出所有技能及描述。
func (s *EvolutionStore) ListSkillNamesWithDescriptions(ctx context.Context) []struct {
	Name        string
	Description string
} {
	var result []struct {
		Name        string
		Description string
	}
	for _, name := range s.ListSkillNames(ctx) {
		content, _ := s.ReadSkillContent(ctx, name)
		description := ExtractDescriptionFromSkillMD(content)
		result = append(result, struct {
			Name        string
			Description string
		}{Name: name, Description: description})
	}
	return result
}

// ExtractDescriptionFromSkillMD 从 SKILL.md 内容提取 description。
func ExtractDescriptionFromSkillMD(content string) string {
	return StoreProjectionHelperExtractDescriptionFromSkillMD(content)
}

// ArchiveSkillBody 归档 SKILL.md。
func (s *EvolutionStore) ArchiveSkillBody(ctx context.Context, name string) (string, error) {
	return s.archive.ArchiveSkillBody(ctx, name)
}

// ArchiveEvolutions 归档演进数据。
func (s *EvolutionStore) ArchiveEvolutions(ctx context.Context, name string) (string, error) {
	return s.archive.ArchiveEvolutions(ctx, name)
}

// ClearEvolutions 清空演进数据。
func (s *EvolutionStore) ClearEvolutions(ctx context.Context, name string) error {
	return s.archive.ClearEvolutions(ctx, name)
}

// ListArchives 列出归档文件。
func (s *EvolutionStore) ListArchives(ctx context.Context, name string) []string {
	return s.archive.ListArchives(ctx, name)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSkillLock 获取或创建技能级读写锁（惰性创建）。
func (s *EvolutionStore) getSkillLock(name string) *sync.RWMutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.skillLocks[name]; ok {
		return l
	}
	l := &sync.RWMutex{}
	s.skillLocks[name] = l
	return l
}

// normalizeBaseDirs 解析和规范化技能基础目录列表。
// 对应 Python: EvolutionStore._normalize_base_dirs(skills_base_dir)
func normalizeBaseDirs(skillsBaseDir string) []string {
	parsed := parseBaseDirs(skillsBaseDir)
	var result []string
	seen := map[string]bool{}
	for _, rawDir := range parsed {
		resolved, err := filepath.Abs(rawDir)
		if err != nil {
			continue
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		result = append(result, resolved)
	}
	return result
}

// normalizeBaseDirsFromList 从 []string 规范化基础目录列表。
// 对应 Python: EvolutionStore._normalize_base_dirs(skills_base_dir: List[str])
func normalizeBaseDirsFromList(baseDirs []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, rawDir := range baseDirs {
		parsed := parseBaseDirs(rawDir)
		for _, d := range parsed {
			resolved, err := filepath.Abs(d)
			if err != nil {
				continue
			}
			if seen[resolved] {
				continue
			}
			seen[resolved] = true
			result = append(result, resolved)
		}
	}
	return result
}

// parseBaseDirs 解析分号/逗号分隔的多路径。
// 对应 Python: EvolutionStore._parse_base_dirs(raw)
func parseBaseDirs(raw string) []string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil
	}
	normalized := strings.ReplaceAll(text, ",", ";")
	parts := strings.Split(normalized, ";")
	var result []string
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// inferSkillNameFromPackage 从 tarball 推断技能名。
// 对齐 Python: install_skill_package 中的 top_level_names 逻辑
// 先检查是否只有一个顶级目录（Python: len(top_level_names) == 1），再回退到 SKILL.md 搜索
func inferSkillNameFromPackage(packageBytes []byte) string {
	buf := bytes.NewReader(packageBytes)
	gzReader, err := gzip.NewReader(buf)
	if err != nil {
		return ""
	}
	defer func() { _ = gzReader.Close() }()

	// 第一遍：收集所有顶级目录名（对齐 Python: top_level_names）
	topLevelNames := map[string]struct{}{}
	// 第二遍数据需重新读取，先保存所有 header
	type tarEntry struct {
		name string
	}
	var entries []tarEntry

	tr := tar.NewReader(gzReader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ""
		}
		// 对齐 Python: member.name and not member.name.startswith("/")
		if header.Name != "" && !strings.HasPrefix(header.Name, "/") {
			parts := strings.Split(filepath.ToSlash(header.Name), "/")
			if len(parts) > 0 && parts[0] != "" {
				topLevelNames[parts[0]] = struct{}{}
			}
			entries = append(entries, tarEntry{name: header.Name})
		}
	}

	// 对齐 Python: if len(top_level_names) == 1 → resolved_name = next(iter(top_level_names))
	if len(topLevelNames) == 1 {
		for name := range topLevelNames {
			return name
		}
	}

	// 回退：搜索 SKILL.md（对齐 Python: for member in members if member.name.endswith("SKILL.md")）
	for _, entry := range entries {
		if strings.HasSuffix(entry.name, "SKILL.md") {
			parts := strings.Split(filepath.ToSlash(entry.name), "/")
			if len(parts) > 0 {
				return parts[0]
			}
		}
	}
	return ""
}

// isDir 判断路径是否为目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isFile 判断路径是否为文件。
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// hasFiles 判断目录是否包含文件。
func hasFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}
