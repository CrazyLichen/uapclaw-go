package checkpointing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StoreArchiveHelper 归档、清空和创建技能辅助。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/store_archive.py StoreArchiveHelper
type StoreArchiveHelper struct {
	// store 所属的 EvolutionStore 实例
	store *EvolutionStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateSkill 创建新技能。
// 对应 Python: StoreArchiveHelper.create_skill(name, description, body, frontmatter)
func (h *StoreArchiveHelper) CreateSkill(ctx context.Context, name string, description string, body string, frontmatter string) (string, error) {
	// 对齐 Python: 校验名称
	validNameRe := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if name == "" || !validNameRe.MatchString(name) {
		logger.Error(logComponent).
			Str("name", name).
			Msg("[EvolutionStore] create_skill: 无效名称")
		return "", nil
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		logger.Error(logComponent).
			Str("name", name).
			Msg("[EvolutionStore] create_skill: 路径遍历尝试")
		return "", nil
	}

	skillDir := h.store.ResolveSkillDir(ctx, name, true)
	if skillDir == "" {
		logger.Error(logComponent).
			Str("name", name).
			Msg("[EvolutionStore] create_skill: 无法解析技能目录")
		return "", nil
	}

	if isDir(skillDir) && hasFiles(skillDir) {
		logger.Error(logComponent).
			Str("name", name).
			Str("skill_dir", skillDir).
			Msg("[EvolutionStore] create_skill: 技能已存在")
		return "", nil
	}

	_ = os.MkdirAll(skillDir, 0755)

	// 对齐 Python: 构建 SKILL.md 内容
	skillMDContent := ""
	if frontmatter != "" {
		skillMDContent = fmt.Sprintf("%s\n\n# %s\n\n%s\n", frontmatter, name, body)
	} else {
		skillMDContent = fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n\n%s\n",
			name, description, name, body)
	}
	skillMDPath := filepath.Join(skillDir, "SKILL.md")
	if err := h.store.WriteFileText(ctx, skillMDPath, skillMDContent); err != nil {
		return "", err
	}

	// 对齐 Python: 创建空 EvolutionLog
	emptyLog := EmptyEvolutionLog(name)
	if err := h.store.SaveEvolutionLog(ctx, name, emptyLog, skillDir); err != nil {
		return "", err
	}

	// 对齐 Python: 创建 evolution 目录
	evoDir := filepath.Join(skillDir, "evolution")
	_ = os.MkdirAll(evoDir, 0755)

	logger.Info(logComponent).
		Str("name", name).
		Str("skill_dir", skillDir).
		Msg("[EvolutionStore] 创建新技能")
	return skillDir, nil
}

// ArchiveSkillBody 归档 SKILL.md。
// 对应 Python: StoreArchiveHelper.archive_skill_body(name)
func (h *StoreArchiveHelper) ArchiveSkillBody(ctx context.Context, name string) (string, error) {
	skillDir := h.store.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return "", nil
	}
	mdPath := h.store.FindSkillMD(ctx, skillDir)
	if mdPath == "" {
		return "", nil
	}
	archive := ArchiveDir(skillDir)
	suffix := tsSuffix()
	dest := filepath.Join(archive, fmt.Sprintf("SKILL.v%s.md", suffix))
	content, err := h.store.ReadFileText(ctx, mdPath)
	if err != nil {
		return "", err
	}
	if err := h.store.WriteFileText(ctx, dest, content); err != nil {
		return "", err
	}
	logger.Info(logComponent).
		Str("src", filepath.Base(mdPath)).
		Str("dest", filepath.Base(dest)).
		Msg("[EvolutionStore] 归档 SKILL.md")
	return filepath.Base(dest), nil
}

// ArchiveEvolutions 归档演进数据。
// 对应 Python: StoreArchiveHelper.archive_evolutions(name)
func (h *StoreArchiveHelper) ArchiveEvolutions(ctx context.Context, name string) (string, error) {
	skillDir := h.store.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return "", nil
	}
	evoPath := filepath.Join(skillDir, evolutionFilename)
	if !isFile(evoPath) {
		return "", nil
	}
	archive := ArchiveDir(skillDir)
	suffix := tsSuffix()
	dest := filepath.Join(archive, fmt.Sprintf("evolutions.v%s.json", suffix))
	content, err := h.store.ReadFileText(ctx, evoPath)
	if err != nil {
		return "", err
	}
	if err := h.store.WriteFileText(ctx, dest, content); err != nil {
		return "", err
	}
	logger.Info(logComponent).
		Str("dest", filepath.Base(dest)).
		Msg("[EvolutionStore] 归档演进数据")
	return filepath.Base(dest), nil
}

// ClearEvolutions 清空演进数据。
// 对应 Python: StoreArchiveHelper.clear_evolutions(name)
func (h *StoreArchiveHelper) ClearEvolutions(ctx context.Context, name string) error {
	emptyLog := EmptyEvolutionLog(name)
	if err := h.store.SaveEvolutionLog(ctx, name, emptyLog, ""); err != nil {
		return err
	}
	if err := h.store.RenderEvolutionMarkdown(ctx, name); err != nil {
		return err
	}
	logger.Info(logComponent).
		Str("skill", name).
		Msg("[EvolutionStore] 清空演进数据")
	return nil
}

// ListArchives 列出归档文件。
// 对应 Python: StoreArchiveHelper.list_archives(name)
func (h *StoreArchiveHelper) ListArchives(ctx context.Context, name string) []string {
	skillDir := h.store.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return nil
	}
	archive := filepath.Join(skillDir, "archive")
	if !isDir(archive) {
		return nil
	}
	entries, err := os.ReadDir(archive)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names
}

// ArchiveDir 创建/返回 archive 子目录。
// 对应 Python: StoreArchiveHelper.archive_dir(skill_dir)
func ArchiveDir(skillDir string) string {
	archive := filepath.Join(skillDir, "archive")
	_ = os.MkdirAll(archive, 0755)
	return archive
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tsSuffix 生成 UTC 时间戳后缀。
// 对应 Python: StoreArchiveHelper.ts_suffix() → "%Y%m%dT%H%M%S"
func tsSuffix() string {
	return time.Now().UTC().Format("20060102T150405")
}
