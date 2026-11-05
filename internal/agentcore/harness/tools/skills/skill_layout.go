package skills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// skillTreeMaxDepth 目录树最大深度
	// Python: _TREE_MAX_DEPTH (skill_tool.py:21)
	skillTreeMaxDepth = 4
	// skillTreeMaxLines 目录树最大行数
	// Python: _TREE_MAX_LINES (skill_tool.py:22)
	skillTreeMaxLines = 200
	// skillNestedNamesMax 嵌套子技能名上限
	// Python: _MAX_NESTED_SKILL_NAMES (skill_tool.py:23)
	skillNestedNamesMax = 50
)

// ──────────────────────────── 全局变量 ────────────────────────────

// skillTreeSkipDirs 目录树跳过的目录名
// Python: _TREE_SKIP_DIR_NAMES (skill_tool.py:24-29)
var skillTreeSkipDirs = map[string]struct{}{
	"output": {}, "temp": {}, "assets": {}, "node_modules": {},
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isSafeRelativeFilePath 判断技能内相对路径是否安全（拒绝绝对路径与 .. 穿越）。
// Python: _is_safe_relative_file_path (skill_tool.py:94-104)
func isSafeRelativeFilePath(filePath string) bool {
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return false
	}
	// Windows 盘符：C:/ 或 C:xxx
	if len(normalized) >= 2 && normalized[1] == ':' {
		return false
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// buildSkillDirectoryAsciiTree 构建技能根目录 ASCII 树。
// Python: _build_skill_directory_ascii_tree (skill_tool.py:164-196)
func buildSkillDirectoryAsciiTree(skillRoot string) (tree []string, truncated bool, errText string) {
	info, err := os.Stat(skillRoot)
	if err != nil {
		return nil, false, "skill directory does not exist"
	}
	if !info.IsDir() {
		return nil, false, "skill path is not a directory"
	}
	rootName := filepath.Base(skillRoot)
	if rootName == "" || rootName == "." {
		rootName = "."
	}
	lines := []string{rootName + "/"}
	truncated = appendSkillTreeLines(skillRoot, "", 0, &lines, map[string]struct{}{})
	return []string{strings.Join(lines, "\n")}, truncated, ""
}

// appendSkillTreeLines 递归追加树行，返回是否因行数上限截断。
// Python: _append_skill_ascii_tree_lines (skill_tool.py:114-161)
func appendSkillTreeLines(dir, prefix string, depth int, out *[]string, visited map[string]struct{}) bool {
	if depth > skillTreeMaxDepth || len(*out) >= skillTreeMaxLines {
		return len(*out) >= skillTreeMaxLines
	}
	key := dir
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		key = resolved
	}
	if _, ok := visited[key]; ok {
		return false
	}
	visited[key] = struct{}{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	type treeChild struct {
		// path 子项绝对路径
		path string
		// name 子项名称
		name string
		// isDir 是否目录
		isDir bool
	}
	children := make([]treeChild, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		isDir := entry.IsDir()
		if skipSkillTreeName(name, isDir) {
			continue
		}
		children = append(children, treeChild{path: filepath.Join(dir, name), name: name, isDir: isDir})
	}
	sort.Slice(children, func(i, j int) bool {
		if children[i].isDir != children[j].isDir {
			return children[i].isDir
		}
		return strings.ToLower(children[i].name) < strings.ToLower(children[j].name)
	})

	for idx, c := range children {
		if len(*out) >= skillTreeMaxLines {
			return true
		}
		isLast := idx == len(children)-1
		connector, childPrefix := "├── ", "│   "
		if isLast {
			connector, childPrefix = "└── ", "    "
		}
		display := c.name
		if c.isDir {
			display += "/"
		}
		*out = append(*out, prefix+connector+display)
		if c.isDir && appendSkillTreeLines(c.path, prefix+childPrefix, depth+1, out, visited) {
			return true
		}
	}
	return false
}

// skipSkillTreeName 判断是否跳过该条目（隐藏文件/指定目录）。
// Python: _skill_tree_skip_name (skill_tool.py:106-112)
func skipSkillTreeName(name string, isDir bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if isDir {
		if _, ok := skillTreeSkipDirs[name]; ok {
			return true
		}
	}
	return false
}

// collectNestedSkillNames 收集含 SKILL.md 的子目录相对路径（POSIX 分隔）。
// Python: _collect_nested_skill_names (skill_tool.py:198-264)
func collectNestedSkillNames(skillRoot string) (names []string, truncated bool) {
	info, err := os.Stat(skillRoot)
	if err != nil || !info.IsDir() {
		return nil, false
	}
	resolvedRoot := skillRoot
	if resolved, err := filepath.EvalSymlinks(skillRoot); err == nil {
		resolvedRoot = resolved
	}
	visited := map[string]struct{}{}
	var walk func(dir string)
	walk = func(dir string) {
		if truncated {
			return
		}
		if len(names) >= skillNestedNamesMax {
			truncated = true
			return
		}
		key := dir
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			key = resolved
		}
		if _, ok := visited[key]; ok {
			return
		}
		visited[key] = struct{}{}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		sort.Slice(entries, func(i, j int) bool {
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		for _, entry := range entries {
			if len(names) >= skillNestedNamesMax {
				truncated = true
				return
			}
			if !entry.IsDir() || skipSkillTreeName(entry.Name(), true) {
				continue
			}
			childPath := filepath.Join(dir, entry.Name())
			rel, err := filepath.Rel(resolvedRoot, childPath)
			if err != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
				continue
			}
			if _, err := os.Stat(filepath.Join(childPath, defaultSkillFileName)); err == nil {
				names = append(names, strings.ReplaceAll(rel, "\\", "/"))
				if len(names) >= skillNestedNamesMax {
					truncated = true
					return
				}
			}
			walk(childPath)
		}
	}
	walk(resolvedRoot)
	return names, truncated
}

// skillLayoutMetadata 组装 skill_tool 成功时的目录布局字段。
// Python: _skill_layout_metadata (skill_tool.py:267-281)
func skillLayoutMetadata(skillDirectory string) map[string]any {
	meta := map[string]any{}
	if tree, truncated, errText := buildSkillDirectoryAsciiTree(skillDirectory); len(tree) > 0 {
		meta["directory_tree"] = tree
		meta["directory_tree_truncated"] = truncated
		if errText != "" {
			meta["directory_tree_partial_errors"] = errText
		}
	}
	names, nestedTruncated := collectNestedSkillNames(skillDirectory)
	meta["discovered_skill_names"] = names
	meta["discovered_skill_names_truncated"] = nestedTruncated
	return meta
}

// formatLayoutAppendixForModel 渲染目录布局附录（供 data.content 使用）。
// Python: _format_layout_appendix_for_model (skill_tool.py:283-330)
func formatLayoutAppendixForModel(layout map[string]any, skillName string) string {
	var parts []string
	if treeText := firstTreeBlock(layout["directory_tree"]); treeText != "" {
		suffix := ""
		if truncated, _ := layout["directory_tree_truncated"].(bool); truncated {
			suffix = "\n…(truncated)"
		}
		parts = append(parts, "## Directory layout\n```\n"+treeText+suffix+"\n```")
	}
	names, _ := layout["discovered_skill_names"].([]string)
	if len(names) > 0 {
		var lines []string
		for _, name := range names {
			if strings.TrimSpace(name) == "" {
				continue
			}
			lines = append(lines, "- "+name+"/SKILL.md")
		}
		if len(lines) > 0 {
			note := ""
			if truncated, _ := layout["discovered_skill_names_truncated"].(bool); truncated {
				note = "\n…(truncated)"
			}
			parent := strings.TrimSpace(skillName)
			if parent == "" {
				parent = "<this skill>"
			}
			example := names[0]
			parts = append(parts,
				"## Nested skills\n"+
					"Directories inside this skill. They are **not** top-level skills — "+
					"`skill_tool(skill_name=\""+example+"\")` will not find them. Keep this "+
					"skill's name and point relative_file_path at the one you want:\n"+
					"`skill_tool(skill_name=\""+parent+"\", relative_file_path=\""+example+"/SKILL.md\")`\n"+
					strings.Join(lines, "\n")+note)
		}
	}
	return strings.Join(parts, "\n\n")
}

// firstTreeBlock 取目录树文本块（Python 侧为单元素列表）。
func firstTreeBlock(raw any) string {
	switch typed := raw.(type) {
	case []string:
		if len(typed) > 0 {
			return strings.TrimSpace(typed[0])
		}
	case string:
		return strings.TrimSpace(typed)
	}
	return ""
}
