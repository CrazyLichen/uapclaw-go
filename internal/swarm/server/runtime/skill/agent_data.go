package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// agentDataEntry agent-data.json 中的单个文件条目
//
// 对齐 Python: _generate_agent_data_for_workspace 写入的 dict 元素
// {"name": str, "path": str, "isMarkdown": bool}
type agentDataEntry struct {
	// Name 展示名（已做 _zh/_en 语言后缀归一）
	Name string `json:"name"`
	// Path 相对项目根的展示路径，形如 agent/workspace/skills/demo/SKILL.md
	Path string `json:"path"`
	// IsMarkdown 是否为 Markdown 文档（.md / .mdx）
	IsMarkdown bool `json:"isMarkdown"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// agentDataRootKey 顶层目录（agent 根下无中间目录）在索引中的 key
	// Python: root_folder_key = "__root__"
	agentDataRootKey = "__root__"
	// agentDataDisplayPrefix 展示路径前缀（相对项目根）
	// Python: f"agent/{relative_folder_path}/{display_name}"
	agentDataDisplayPrefix = "agent"
	// agentDataFileName 索引文件名
	agentDataFileName = "agent-data.json"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateAgentDataForWorkspace 为指定 agent workspace 目录重建 agent-data.json。
//
// workspaceDir 是 agent 工作区目录（即 skills 目录的父目录，形如 <data>/agent/workspace），
// 输出文件写在 workspaceDir 下；展示路径以 agent 根（workspaceDir 的父目录）为基准，
// 因此顶层文件的 key 为 "workspace"。
//
// 对齐 Python: app_web.py::_generate_agent_data（模板契约版本）；
// 注意不要对齐 skill_manager.py 的同名实现，后者以 agent_root.parent 为基准会拼出
// "agent/agent/workspace/..." 的双前缀。
//
// 返回 error 仅用于目录不存在的场景（对齐 Python: FileNotFoundError / NotADirectoryError）。
func GenerateAgentDataForWorkspace(workspaceDir string) error {
	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(absWorkspace)
	if statErr != nil || !info.IsDir() {
		return os.ErrNotExist
	}

	// agent 根目录：workspace 目录的父目录（<data>/agent）
	agentRoot := filepath.Dir(absWorkspace)

	folderData := make(map[string][]agentDataEntry)
	seenPaths := make(map[string]map[string]bool)

	walkErr := filepath.WalkDir(absWorkspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// 单个条目不可读时跳过，不中断整体重建（对齐 Python: 异常仅记 warning）
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			// 隐藏目录整棵跳过
			if path != absWorkspace && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}

		relToRoot, relErr := filepath.Rel(agentRoot, path)
		if relErr != nil {
			return nil
		}
		relToRoot = filepath.ToSlash(relToRoot)
		// 路径中任一段为隐藏目录则跳过（对齐 Python: any(part.startswith(".")))
		for _, part := range strings.Split(relToRoot, "/") {
			if strings.HasPrefix(part, ".") {
				return nil
			}
		}

		dirRel, dirErr := filepath.Rel(agentRoot, filepath.Dir(path))
		if dirErr != nil {
			return nil
		}
		dirRel = filepath.ToSlash(dirRel)
		folderKey := dirRel
		if folderKey == "." || folderKey == "" {
			folderKey = agentDataRootKey
		}

		displayName := normalizeLangSuffix(entry.Name())
		displayPath := agentDataDisplayPrefix + "/" + relToRoot
		if idx := strings.LastIndex(displayPath, "/"); idx >= 0 {
			displayPath = displayPath[:idx+1] + displayName
		}

		seen := seenPaths[folderKey]
		if seen == nil {
			seen = make(map[string]bool)
			seenPaths[folderKey] = seen
		}
		if seen[displayPath] {
			return nil
		}
		seen[displayPath] = true

		folderData[folderKey] = append(folderData[folderKey], agentDataEntry{
			Name:       displayName,
			Path:       displayPath,
			IsMarkdown: isMarkdownFileName(entry.Name()),
		})
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	// 排序：folder key 升序；folder 内按 path 升序（对齐 Python sorted）
	folderKeys := make([]string, 0, len(folderData))
	for key := range folderData {
		folderKeys = append(folderKeys, key)
	}
	sort.Strings(folderKeys)

	sortedData := make(map[string][]agentDataEntry, len(folderKeys))
	for _, key := range folderKeys {
		entries := folderData[key]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		sortedData[key] = entries
	}

	outputPath := filepath.Join(absWorkspace, agentDataFileName)
	outFile, createErr := os.Create(outputPath)
	if createErr != nil {
		return createErr
	}
	defer func() { _ = outFile.Close() }()

	// 已知差异（无消费者，不改）：json.Encoder.Encode 会在末尾写一个换行，
	// Python 的 app_web.py:127-130 用 write_text(json.dumps(...)) 没有尾换行。
	// 前端 JSON 解析对尾换行无感；按仓库既有结论（"无消费者的字节一致性不改"）保留差异，
	// 若将来做 golden 对拍，改成 json.Marshal + Write 即可对齐。
	encoder := json.NewEncoder(outFile)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(sortedData)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// refreshAgentDataIndexes 生成 agent-data.json 索引文件。
// 遍历 skillsDir 的 parent 目录 + getMirrorSkillsDirs() 的 parent 目录，逐个重建索引。
//
// Python: SkillManager._refresh_agent_data_indexes
func (sm *SkillManager) refreshAgentDataIndexes() {
	// 收集需要生成索引的目录
	dirs := []string{filepath.Dir(sm.skillsDir)}
	for _, mirrorDir := range sm.getMirrorSkillsDirs() {
		dirs = append(dirs, filepath.Dir(mirrorDir))
	}

	for _, dir := range dirs {
		if err := GenerateAgentDataForWorkspace(dir); err != nil {
			logger.Warn(logComponent).Err(err).Str("workspace_dir", dir).
				Msg("rebuild agent-data.json failed")
		}
	}
}

// normalizeLangSuffix 将 xxx_zh.MD / xxx_en.MD 归一为 xxx.MD。
//
// Python: SkillManager._normalize_lang_suffix
func normalizeLangSuffix(name string) string {
	dotIdx := strings.LastIndex(name, ".")
	if dotIdx <= 0 {
		return name
	}
	stem := name[:dotIdx]
	suffix := name[dotIdx+1:]
	lowerSuffix := strings.ToLower(suffix)
	if lowerSuffix != "md" && lowerSuffix != "mdx" {
		return name
	}
	lowerStem := strings.ToLower(stem)
	if strings.HasSuffix(lowerStem, "_zh") || strings.HasSuffix(lowerStem, "_en") {
		return stem[:len(stem)-3] + "." + suffix
	}
	return name
}

// isMarkdownFileName 判断文件名是否为 Markdown 文档。
//
// Python: entry.suffix.lower() in {".md", ".mdx"}
func isMarkdownFileName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".mdx"
}
