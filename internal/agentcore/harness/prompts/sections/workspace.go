package sections

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	wscontent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/workspace_content"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DirNode 目录树节点
type DirNode struct {
	// Name 文件或目录名称
	Name string
	// Path 完整路径
	Path string
	// Description 目录/文件描述
	Description string
	// IsFile 是否为文件
	IsFile bool
	// Children 子节点
	Children []DirNode
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ScanDirectoryStructure 扫描目录结构（最大深度限制）
//
// rootPath 为当前扫描目录；currentDepth 为当前递归深度（0=根）；
// maxDepth 为最大深度（2=扫描到第2层）；lang 为语言标识。
func ScanDirectoryStructure(rootPath string, currentDepth int, maxDepth int, lang string) []DirNode {
	if currentDepth > maxDepth {
		return nil
	}

	var nodes []DirNode

	// 读取目录条目
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil
	}

	// 分离目录和文件
	var dirs, files []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		} else {
			files = append(files, entry)
		}
	}

	// 按名称排序
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	// 处理目录
	for _, dir := range dirs {
		dirName := dir.Name()
		fullPath := filepath.Join(rootPath, dirName)
		desc := GetDirectoryDescription(dirName, lang)

		var children []DirNode
		if currentDepth < maxDepth {
			children = ScanDirectoryStructure(fullPath, currentDepth+1, maxDepth, lang)
		}

		nodes = append(nodes, DirNode{
			Name:        dirName,
			Path:        fullPath,
			Description: desc,
			IsFile:      false,
			Children:    children,
		})
	}

	// 处理文件
	if currentDepth < maxDepth {
		for _, file := range files {
			fileName := file.Name()
			fullPath := filepath.Join(rootPath, fileName)
			desc := GetDirectoryDescription(fileName, lang)

			nodes = append(nodes, DirNode{
				Name:        fileName,
				Path:        fullPath,
				Description: desc,
				IsFile:      true,
				Children:    nil,
			})
		}
	}

	return nodes
}

// GetDirectoryDescription 根据 dir/file 名称获取描述
func GetDirectoryDescription(name string, lang string) string {
	var descs map[string]string
	if lang == "en" {
		descs = wscontent.DirectoryDescriptionsEN
	} else {
		descs = wscontent.DirectoryDescriptionsCN
	}
	if desc, ok := descs[name]; ok {
		return desc
	}
	return ""
}

// FormatTree 将目录节点列表格式化为树形文本
func FormatTree(nodes []DirNode, lang string) string {
	var lines []string
	for i, node := range nodes {
		isLast := (i == len(nodes)-1)
		formatNode(node, &lines, "", isLast, lang)
	}
	return strings.Join(lines, "\n")
}

// BuildWorkspaceSection 构建工作空间节（Priority 70）
//
// rootPath 为工作目录根路径；dirTree 为目录树文本（可为空）。
func BuildWorkspaceSection(rootPath string, dirTree string, lang string) saprompt.PromptSection {
	var header string
	var importantFiles string

	if lang == "en" {
		header = wscontent.WorkspaceHeaderEN
		importantFiles = wscontent.ImportantFilesEN
	} else {
		header = wscontent.WorkspaceHeaderCN
		importantFiles = wscontent.ImportantFilesCN
	}

	var pathLine string
	if lang == "en" {
		pathLine = fmt.Sprintf("Your working directory is: `%s`\n\n%s", rootPath, importantFiles)
	} else {
		pathLine = fmt.Sprintf("你的工作目录是：`%s`\n\n%s", rootPath, importantFiles)
	}

	content := header + pathLine

	// 追加目录树（若有）
	if dirTree != "" {
		content += "\n\n" + dirTree
	}

	return saprompt.PromptSection{
		Name:     SectionWorkspace,
		Content:  map[string]string{lang: content},
		Priority: 70,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatNode 格式化单个目录节点并递归处理其子节点
func formatNode(node DirNode, lines *[]string, prefix string, isLast bool, lang string) {
	connector := "└── "
	if !isLast {
		connector = "├── "
	}

	if node.IsFile {
		*lines = append(*lines, prefix+connector+node.Name)
	} else {
		line := prefix + connector + node.Name + "/"
		if node.Description != "" {
			line += "  # " + node.Description
		}
		*lines = append(*lines, line)
	}

	for i, child := range node.Children {
		childIsLast := (i == len(node.Children)-1)
		var childPrefix string
		if childIsLast {
			childPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "│   "
		}
		formatNode(child, lines, childPrefix, childIsLast, lang)
	}
}
