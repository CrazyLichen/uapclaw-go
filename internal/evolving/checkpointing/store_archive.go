package checkpointing

import (
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
func (h *StoreArchiveHelper) CreateSkill(name string, description string, body string, frontmatter string) string {
	// 对齐 Python: 校验名称
	validNameRe := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if name == "" || !validNameRe.MatchString(name) {
		logger.Error(logger.ComponentAgentCore).
			Str("name", name).
			Msg("[EvolutionStore] create_skill: 无效名称")
		return ""
	}
	if strings.Contains(name, "..") || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		logger.Error(logger.ComponentAgentCore).
			Str("name", name).
			Msg("[EvolutionStore] create_skill: 路径遍历尝试")
		return ""
	}

	skillDir := h.store.ResolveSkillDir(name, true)
	if skillDir == "" {
		logger.Error(logger.ComponentAgentCore).
			Str("name", name).
			Msg("[EvolutionStore] create_skill: 无法解析技能目录")
		return ""
	}

	if isDir(skillDir) && hasFiles(skillDir) {
		logger.Error(logger.ComponentAgentCore).
			Str("name", name).
			Str("skill_dir", skillDir).
			Msg("[EvolutionStore] create_skill: 技能已存在")
		return ""
	}

	os.MkdirAll(skillDir, 0755)

	// 对齐 Python: 构建 SKILL.md 内容
	skillMDContent := ""
	if frontmatter != "" {
		skillMDContent = fmt.Sprintf("%s\n\n# %s\n\n%s\n", frontmatter, name, body)
	} else {
		skillMDContent = fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n# %s\n\n%s\n",
			name, description, name, body)
	}
	skillMDPath := filepath.Join(skillDir, "SKILL.md")
	h.store.WriteFileText(skillMDPath, skillMDContent)

	// 对齐 Python: 创建空 EvolutionLog
	emptyLog := EmptyEvolutionLog(name)
	h.store.SaveEvolutionLog(name, emptyLog, skillDir)

	// 对齐 Python: 创建 evolution 目录
	evoDir := filepath.Join(skillDir, "evolution")
	os.MkdirAll(evoDir, 0755)

	logger.Info(logger.ComponentAgentCore).
		Str("name", name).
		Str("skill_dir", skillDir).
		Msg("[EvolutionStore] 创建新技能")
	return skillDir
}

// ArchiveSkillBody 归档 SKILL.md。
// 对应 Python: StoreArchiveHelper.archive_skill_body(name)
func (h *StoreArchiveHelper) ArchiveSkillBody(name string) string {
	skillDir := h.store.ResolveSkillDir(name)
	if skillDir == "" {
		return ""
	}
	mdPath := h.store.FindSkillMD(skillDir)
	if mdPath == "" {
		return ""
	}
	archive := ArchiveDir(skillDir)
	suffix := tsSuffix()
	dest := filepath.Join(archive, fmt.Sprintf("SKILL.v%s.md", suffix))
	content := h.store.ReadFileText(mdPath)
	h.store.WriteFileText(dest, content)
	logger.Info(logger.ComponentAgentCore).
		Str("src", filepath.Base(mdPath)).
		Str("dest", filepath.Base(dest)).
		Msg("[EvolutionStore] 归档 SKILL.md")
	return filepath.Base(dest)
}

// ArchiveEvolutions 归档演进数据。
// 对应 Python: StoreArchiveHelper.archive_evolutions(name)
func (h *StoreArchiveHelper) ArchiveEvolutions(name string) string {
	skillDir := h.store.ResolveSkillDir(name)
	if skillDir == "" {
		return ""
	}
	evoPath := filepath.Join(skillDir, evolutionFilename)
	if !isFile(evoPath) {
		return ""
	}
	archive := ArchiveDir(skillDir)
	suffix := tsSuffix()
	dest := filepath.Join(archive, fmt.Sprintf("evolutions.v%s.json", suffix))
	content := h.store.ReadFileText(evoPath)
	h.store.WriteFileText(dest, content)
	logger.Info(logger.ComponentAgentCore).
		Str("dest", filepath.Base(dest)).
		Msg("[EvolutionStore] 归档演进数据")
	return filepath.Base(dest)
}

// ClearEvolutions 清空演进数据。
// 对应 Python: StoreArchiveHelper.clear_evolutions(name)
func (h *StoreArchiveHelper) ClearEvolutions(name string) {
	emptyLog := EmptyEvolutionLog(name)
	h.store.SaveEvolutionLog(name, emptyLog, "")
	h.store.RenderEvolutionMarkdown(name)
	logger.Info(logger.ComponentAgentCore).
		Str("skill", name).
		Msg("[EvolutionStore] 清空演进数据")
}

// ListArchives 列出归档文件。
// 对应 Python: StoreArchiveHelper.list_archives(name)
func (h *StoreArchiveHelper) ListArchives(name string) []string {
	skillDir := h.store.ResolveSkillDir(name)
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// ArchiveDir 创建/返回 archive 子目录。
// 对应 Python: StoreArchiveHelper.archive_dir(skill_dir)
func ArchiveDir(skillDir string) string {
	archive := filepath.Join(skillDir, "archive")
	os.MkdirAll(archive, 0755)
	return archive
}

// tsSuffix 生成 UTC 时间戳后缀。
// 对应 Python: StoreArchiveHelper.ts_suffix() → "%Y%m%dT%H%M%S"
func tsSuffix() string {
	return time.Now().UTC().Format("20060102T150405")
}
