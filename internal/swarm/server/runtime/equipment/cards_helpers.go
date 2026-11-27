package equipment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	hresources "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// localizedWire LocalizedText → wire 形态（字符串或 map），供 _i18n 消费。
// Python: template_card 的 display_name/display_description 原样值。
func localizedWire(t hresources.LocalizedText) any {
	if t.Map != nil {
		m := map[string]any{}
		for k, v := range t.Map {
			m[k] = v
		}
		return m
	}
	return t.Str
}

// agentGroupMemberCard 分组成员的 manifest 卡；不可读返回 nil。
// Python: _agent_group_member_card (:362-374)。
func agentGroupMemberCard(pkgDir, memberID string) *hresources.AgentTemplateCard {
	card, err := hresources.LoadAgentTemplateCard(filepath.Join(pkgDir, "agents", memberID, "manifest.json"))
	if err != nil {
		return nil
	}
	return &card
}

// parseSkillFrontmatter SKILL.md YAML frontmatter 的 name/description（尽力解析）。
// Python: _parse_skill_frontmatter_text (:430-453)：YAML 优先，失败回退行解析。
func parseSkillFrontmatter(skillMD string) map[string]any {
	data, err := os.ReadFile(skillMD)
	if err != nil {
		return map[string]any{}
	}
	text := string(data)
	fmRe := regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n?`)
	match := fmRe.FindStringSubmatch(text)
	if match == nil {
		return map[string]any{}
	}
	var loaded map[string]any
	if err := yamlUnmarshal([]byte(match[1]), &loaded); err == nil && loaded != nil {
		return loaded
	}
	logger.Debug(logger.ComponentAgentServer).
		Msg("[equipment] SKILL.md frontmatter YAML parse failed, falling back to line parser")
	meta := map[string]any{}
	for _, line := range strings.Split(match[1], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.Trim(strings.TrimSpace(line[idx+1:]), "'\"")
		meta[key] = val
	}
	return meta
}

// readReadmeDetails 包 README.md 全文；缺失/不可解码返回空串。
// Python: _read_readme_details (:405-416)。
func readReadmeDetails(pkgDir string) string {
	data, err := os.ReadFile(filepath.Join(pkgDir, "README.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

// manifestModTimeISO manifest.json mtime 的 UTC ISO 串。
// Python: _build_agent_group_card includeDetails 段（:2626-2636）。
func manifestModTimeISO(pkgDir string) string {
	info, err := os.Stat(filepath.Join(pkgDir, "manifest.json"))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(info.ModTime().UTC().Format(time.RFC3339), "+00:00", "Z")
}

// orDefault 空串回退。
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// jsonBytes 导出 json.Marshal 供测试辅助使用。
func jsonBytes(v any) ([]byte, error) {
	return json.Marshal(v)
}

// rejectPreviewPathSymlink 解析包相对路径并拒绝符号链接段。
// Python: _reject_preview_path_symlink (:1707-1727)：逐段检查 symlink，
// 解析后必须仍在包内，且必须是文件。
func rejectPreviewPathSymlink(pkgDir, rel string) (string, error) {
	pkgResolved, err := filepath.EvalSymlinks(pkgDir)
	if err != nil {
		return "", err
	}
	cursor := pkgDir
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		cursor = filepath.Join(cursor, part)
		if isSymlink(cursor) {
			return "", &PackageConflictError{Message: "symlink not allowed: " + rel}
		}
	}
	full, err := filepath.EvalSymlinks(cursor)
	if err != nil {
		return "", &PackageConflictError{Message: "file not found: " + rel}
	}
	if !subpathOf(pkgResolved, full) {
		return "", &PackageConflictError{Message: "path escapes package: " + rel}
	}
	if isSymlink(full) {
		return "", &PackageConflictError{Message: "symlink not allowed: " + rel}
	}
	if st, err := os.Stat(full); err != nil || st.IsDir() {
		return "", &PackageConflictError{Message: "file not found: " + rel}
	}
	return full, nil
}

// isSymlink 路径是否为符号链接。
func isSymlink(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// buildFileTree 包目录文件树（跳过 dot 文件与符号链接；目录带 children）。
// Python: _build_file_tree (:1641-1667)。
func buildFileTree(directory, root string) []map[string]any {
	result := []map[string]any{}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return result
	}
	// 排序：目录优先、名称升序（Python key=(not is_dir, name)）
	sortFilesFirst(entries)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") || isSymlink(filepath.Join(directory, entry.Name())) {
			continue
		}
		rel, relErr := filepath.Rel(root, filepath.Join(directory, entry.Name()))
		if relErr != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			children := buildFileTree(filepath.Join(directory, entry.Name()), root)
			if len(children) > 0 {
				result = append(result, map[string]any{
					"path": rel + "/", "type": "dir", "children": children,
				})
			}
			continue
		}
		size := int64(0)
		if info, infoErr := entry.Info(); infoErr == nil {
			size = info.Size()
		}
		result = append(result, map[string]any{
			"path":        rel,
			"type":        "file",
			"size":        size,
			"previewable": isPreviewableFile(rel),
		})
	}
	return result
}

// sortFilesFirst 目录优先排序（对齐 Python key=(not p.is_dir(), p.name)）。
func sortFilesFirst(entries []os.DirEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})
}

// previewableExts 前端可预览的扩展名集合。
// Python: _CATALOG_PREVIEWABLE_EXTS (:4266 前常量段)。
var previewableExts = map[string]bool{
	".md": true, ".mdx": true, ".json": true, ".py": true, ".pdf": true,
}

// isPreviewableFile 包相对路径是否可预览。Python: _is_previewable_file (:179-188)。
func isPreviewableFile(relPosix string) bool {
	if relPosix == "" {
		return false
	}
	for _, part := range strings.Split(relPosix, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return previewableExts[strings.ToLower(filepath.Ext(relPosix))]
}

// yamlUnmarshal YAML 解析（对齐 Python yaml.safe_load 的 dict 形态）。
func yamlUnmarshal(data []byte, out *map[string]any) error {
	return yaml.Unmarshal(data, out)
}
