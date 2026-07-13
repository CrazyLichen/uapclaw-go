package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	wc "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/workspace_content"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

type Workspace struct {
	// RootPath 工作空间根路径
	RootPath string `json:"root_path" yaml:"root_path"`
	// Directories 顶层目录节点列表
	Directories []DirectoryNode `json:"directories" yaml:"directories"`
	// Language 工作空间语言（"cn" 或 "en"）
	Language string `json:"language" yaml:"language"`
}

// ──────────────────────────── 枚举 ────────────────────────────

type WorkspaceNode string

type DirectoryNode = map[string]any

// ──────────────────────────── 常量 ────────────────────────────

const (
	// WorkspaceNodeAGENT_MD 基础配置和能力
	WorkspaceNodeAGENTMD WorkspaceNode = "AGENT.md"
	// WorkspaceNodeSOUL_MD 人格、性格和价值观
	WorkspaceNodeSOULMD WorkspaceNode = "SOUL.md"
	// WorkspaceNodeHEARTBEAT_MD 心跳日志和状态记录
	WorkspaceNodeHEARTBEATMD WorkspaceNode = "HEARTBEAT.md"
	// WorkspaceNodeIDENTITY_MD 身份凭证和权限
	WorkspaceNodeIDENTITYMD WorkspaceNode = "IDENTITY.md"
	// WorkspaceNodeUSER_MD 用户数据目录
	WorkspaceNodeUSERMD WorkspaceNode = "USER.md"
	// WorkspaceNodeMemory 记忆核心模块
	WorkspaceNodeMemory WorkspaceNode = "memory"
	// WorkspaceNodeCodingMemory Coding Agent 记忆模块
	WorkspaceNodeCodingMemory WorkspaceNode = "coding_memory"
	// WorkspaceNodeTODO 待办事项目录
	WorkspaceNodeTODO WorkspaceNode = "todo"
	// WorkspaceNodeMessages 消息历史目录
	WorkspaceNodeMessages WorkspaceNode = "messages"
	// WorkspaceNodeSkills 技能库目录
	WorkspaceNodeSkills WorkspaceNode = "skills"
	// WorkspaceNodeAgents 子智能体嵌套目录
	WorkspaceNodeAgents WorkspaceNode = "agents"
	// WorkspaceNodeMemoryMD 长期记忆索引和摘要
	WorkspaceNodeMemoryMD WorkspaceNode = "MEMORY.md"
	// WorkspaceNodeDailyMemory 每日结构化记忆
	WorkspaceNodeDailyMemory WorkspaceNode = "daily_memory"
	// WorkspaceNodeTeamLinks 团队链接目录
	WorkspaceNodeTeamLinks WorkspaceNode = ".team"
	// WorkspaceNodeWorktreeLinks 工作树链接目录
	WorkspaceNodeWorktreeLinks WorkspaceNode = ".worktree"
)

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// TeamLinksDir 团队链接目录名
	TeamLinksDir = ".team"
	// WorktreeLinksDir 工作树链接目录名
	WorktreeLinksDir = ".worktree"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var defaultWorkspaceSchemaCN = []DirectoryNode{
	{
		"name":            "AGENT.md",
		"description":     "基础配置和能力",
		"path":            "AGENT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.AgentMDCN,
	},
	{
		"name":            "SOUL.md",
		"description":     "人格、性格和价值观",
		"path":            "SOUL.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.SoulMDCN,
	},
	{
		"name":            "HEARTBEAT.md",
		"description":     "心跳日志和状态记录",
		"path":            "HEARTBEAT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.HeartbeatMDCN,
	},
	{
		"name":            "IDENTITY.md",
		"description":     "身份凭证和权限",
		"path":            "IDENTITY.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.IdentityMDCN,
	},
	{
		"name":            "USER.md",
		"description":     "用户数据目录",
		"path":            "USER.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":        "memory",
		"description": "记忆核心模块",
		"path":        "memory",
		"children": []DirectoryNode{
			{
				"name":            "MEMORY.md",
				"description":     "长期记忆索引和摘要",
				"path":            "MEMORY.md",
				"children":        []DirectoryNode{},
				"is_file":         true,
				"default_content": wc.MemoryMDCN,
			},
			{
				"name":        "daily_memory",
				"description": "每日结构化记忆",
				"path":        "daily_memory",
				"children":    []DirectoryNode{},
			},
		},
	},
	{
		"name":        "coding_memory",
		"description": "Coding Agent 记忆模块",
		"path":        "coding_memory",
		"children": []DirectoryNode{
			{
				"name":            "MEMORY.md",
				"description":     "Coding 记忆索引",
				"path":            "MEMORY.md",
				"children":        []DirectoryNode{},
				"is_file":         true,
				"default_content": "",
			},
		},
	},
	{
		"name":        "todo",
		"description": "待办事项目录",
		"path":        "todo",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "messages",
		"description": "消息历史目录",
		"path":        "messages",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "skills",
		"description": "技能库目录",
		"path":        "skills",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "agents",
		"description": "子智能体嵌套目录",
		"path":        "agents",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "context",
		"description": "上下文offload以及session memory目录",
		"path":        "context",
		"children": []DirectoryNode{
			{
				"name":            "session_memory.md",
				"description":     "session memory模版",
				"path":            "session_memory.md",
				"children":        []DirectoryNode{},
				"is_file":         true,
				"default_content": wc.SessionMemoryMDCN,
			},
		},
	},
}

var defaultWorkspaceSchemaEN = []DirectoryNode{
	{
		"name":            "AGENT.md",
		"description":     "Basic agent configuration and capabilities",
		"path":            "AGENT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.AgentMDEN,
	},
	{
		"name":            "SOUL.md",
		"description":     "Agent personality, character, values, and behavioral guidelines",
		"path":            "SOUL.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.SoulMDEN,
	},
	{
		"name":            "HEARTBEAT.md",
		"description":     "Heartbeat log / status recording",
		"path":            "HEARTBEAT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.HeartbeatMDEN,
	},
	{
		"name":            "IDENTITY.md",
		"description":     "Identity credentials, unique identifier, and permission information",
		"path":            "IDENTITY.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": wc.IdentityMDEN,
	},
	{
		"name":            "USER.md",
		"description":     "User data directory",
		"path":            "USER.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":        "memory",
		"description": "Memory core module",
		"path":        "memory",
		"children": []DirectoryNode{
			{
				"name":            "MEMORY.md",
				"description":     "Memory overview, index, and important memory summaries",
				"path":            "MEMORY.md",
				"children":        []DirectoryNode{},
				"is_file":         true,
				"default_content": wc.MemoryMDEN,
			},
			{
				"name":        "daily_memory",
				"description": "Daily structured memory",
				"path":        "daily_memory",
				"children":    []DirectoryNode{},
			},
		},
	},
	{
		"name":        "todo",
		"description": "Todo items",
		"path":        "todo",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "messages",
		"description": "Message history module",
		"path":        "messages",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "skills",
		"description": "Skills library directory",
		"path":        "skills",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "agents",
		"description": "Sub-agent nesting directory",
		"path":        "agents",
		"children":    []DirectoryNode{},
	},
	{
		"name":        "context",
		"description": "context offload and session memory file",
		"path":        "context",
		"children": []DirectoryNode{
			{
				"name":            "session_memory.md",
				"description":     "session memory模版",
				"path":            "session_memory.md",
				"children":        []DirectoryNode{},
				"is_file":         true,
				"default_content": wc.SessionMemoryMDEN,
			},
		},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

func NewWorkspace(rootPath string, language string) *Workspace {
	w := &Workspace{
		RootPath:    rootPath,
		Directories: []DirectoryNode{},
		Language:    language,
	}

	// 未提供 directories 时，使用默认模式的深拷贝
	if len(w.Directories) == 0 {
		w.Directories = deepCopyNodes(getWorkspaceSchema(w.Language))
	}

	// 校验所有顶层节点
	for _, node := range w.Directories {
		// 校验失败时 panic（与 Python raise 行为一致）
		if err := validateDirectoryNode(node); err != nil {
			panic(err)
		}
	}

	// 补充默认模式中缺失的目录节点
	defaultSchema := getWorkspaceSchema(w.Language)
	existingNames := make(map[string]bool)
	for _, node := range w.Directories {
		if name, ok := node["name"].(string); ok {
			existingNames[name] = true
		}
	}
	for _, defaultNode := range defaultSchema {
		name, _ := defaultNode["name"].(string)
		if name != "" && !existingNames[name] {
			w.Directories = append(w.Directories, deepCopyNode(defaultNode))
			existingNames[name] = true
			pathStr, _ := defaultNode["path"].(string)
			logger.Info(logComponent).
				Str("name", name).
				Str("path", pathStr).
				Msg("Workspace: supplemented missing default directory")
		}
	}

	return w
}

func (w *Workspace) GetDirectory(name any) string {
	nameStr := resolveName(name)

	result := findInNodes(nameStr, w.Directories)
	if result != nil {
		return *result
	}

	// 在默认模式中查找
	defaultSchema := getWorkspaceSchema(w.Language)
	result = findInNodes(nameStr, defaultSchema)
	if result != nil {
		return *result
	}

	return ""
}

func (w *Workspace) SetDirectory(nodes any) error {
	var nodeList []DirectoryNode

	switch v := nodes.(type) {
	case DirectoryNode:
		nodeList = []DirectoryNode{v}
	case []DirectoryNode:
		nodeList = v
	default:
		return exception.BuildError(exception.StatusDeepagentConfigParamError,
			exception.WithMsg("set_directory expects a directory node (map) or list of nodes."))
	}

	for _, node := range nodeList {
		if err := validateDirectoryNode(node); err != nil {
			return err
		}
		name, _ := node["name"].(string)
		replaced := false
		for i, existing := range w.Directories {
			if existingName, ok := existing["name"].(string); ok && existingName == name {
				w.Directories[i] = deepCopyNode(node)
				replaced = true
				break
			}
		}
		if !replaced {
			w.Directories = append(w.Directories, deepCopyNode(node))
		}
	}

	return nil
}

func (w *Workspace) GetNodePath(node any) *string {
	nameStr := resolveName(node)

	for _, nodeDef := range w.Directories {
		if nodeName, ok := nodeDef["name"].(string); ok && nodeName == nameStr {
			relativePath := nameStr
			if p, ok := nodeDef["path"].(string); ok && p != "" {
				relativePath = p
			}
			fullPath := filepath.Join(w.RootPath, relativePath)
			return &fullPath
		}
	}

	return nil
}

func GetDefaultDirectory(language string) []DirectoryNode {
	return getWorkspaceSchema(language)
}

func (w *Workspace) LinkTeam(name, targetPath string) error {
	return w.createLink(TeamLinksDir, name, targetPath)
}

func (w *Workspace) UnlinkTeam(name string) error {
	return w.removeLink(TeamLinksDir, name)
}

func (w *Workspace) LinkWorktree(name, targetPath string) error {
	return w.createLink(WorktreeLinksDir, name, targetPath)
}

func (w *Workspace) UnlinkWorktree(name string) error {
	return w.removeLink(WorktreeLinksDir, name)
}

func (w *Workspace) ListTeamLinks() []string {
	return w.listLinks(TeamLinksDir)
}

func (w *Workspace) ListWorktreeLinks() []string {
	return w.listLinks(WorktreeLinksDir)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func validateDirectoryNode(node DirectoryNode) error {
	if node == nil {
		return exception.BuildError(exception.StatusDeepagentConfigParamError,
			exception.WithMsg("Each directory entry must be a map."))
	}

	name, nameOk := node["name"].(string)
	if !nameOk || name == "" {
		return exception.BuildError(exception.StatusDeepagentConfigParamError,
			exception.WithMsg("Directory `name` must be a non-empty string."))
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return exception.BuildError(exception.StatusDeepagentConfigParamError,
			exception.WithMsg(fmt.Sprintf("Directory `name` must not contain path separators: %q", name)))
	}

	if path, ok := node["path"]; ok && path != nil {
		if _, strOk := path.(string); !strOk {
			return exception.BuildError(exception.StatusDeepagentConfigParamError,
				exception.WithMsg("Directory `path` must be a string when provided."))
		}
	}

	if desc, ok := node["description"]; ok && desc != nil {
		if _, strOk := desc.(string); !strOk {
			return exception.BuildError(exception.StatusDeepagentConfigParamError,
				exception.WithMsg("Directory `description` must be a string when provided."))
		}
	}

	if isFile, ok := node["is_file"]; ok && isFile != nil {
		if _, boolOk := isFile.(bool); !boolOk {
			return exception.BuildError(exception.StatusDeepagentConfigParamError,
				exception.WithMsg("`is_file` must be a bool when provided."))
		}
	}

	if defaultContent, ok := node["default_content"]; ok && defaultContent != nil {
		if _, strOk := defaultContent.(string); !strOk {
			return exception.BuildError(exception.StatusDeepagentConfigParamError,
				exception.WithMsg("`default_content` must be a string when provided."))
		}
	}

	if children, ok := node["children"]; ok && children != nil {
		childrenList, err := toDirectoryNodeSlice(children)
		if err != nil {
			return exception.BuildError(exception.StatusDeepagentConfigParamError,
				exception.WithMsg("Directory `children` must be a list when provided."))
		}
		for _, child := range childrenList {
			if err := validateDirectoryNode(child); err != nil {
				return err
			}
		}
	}

	return nil
}

func getWorkspaceSchema(language string) []DirectoryNode {
	if language == "en" {
		return deepCopyNodes(defaultWorkspaceSchemaEN)
	}
	return deepCopyNodes(defaultWorkspaceSchemaCN)
}

func resolveName(name any) string {
	switch v := name.(type) {
	case WorkspaceNode:
		return string(v)
	case string:
		return v
	default:
		return fmt.Sprintf("%v", name)
	}
}

func findInNodes(name string, nodes []DirectoryNode) *string {
	for _, node := range nodes {
		if nodeName, ok := node["name"].(string); ok && nodeName == name {
			if path, pathOk := node["path"].(string); pathOk {
				return &path
			}
			return &nodeName
		}
		if children, ok := node["children"]; ok {
			if childrenList, err := toDirectoryNodeSlice(children); err == nil {
				result := findInNodes(name, childrenList)
				if result != nil {
					return result
				}
			}
		}
	}
	return nil
}

func deepCopyNode(node DirectoryNode) DirectoryNode {
	data, err := json.Marshal(node)
	if err != nil {
		return node
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return node
	}
	return normalizeNode(raw)
}

func deepCopyNodes(nodes []DirectoryNode) []DirectoryNode {
	result := make([]DirectoryNode, len(nodes))
	for i, node := range nodes {
		result[i] = deepCopyNode(node)
	}
	return result
}

func toDirectoryNodeSlice(children any) ([]DirectoryNode, error) {
	switch v := children.(type) {
	case []DirectoryNode:
		return v, nil
	case []any:
		result := make([]DirectoryNode, len(v))
		for i, item := range v {
			node, ok := item.(DirectoryNode)
			if !ok {
				return nil, fmt.Errorf("children[%d] is not a map", i)
			}
			result[i] = node
		}
		return result, nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("children must be a list, got %T", v)
	}
}

func normalizeNode(node map[string]any) DirectoryNode {
	result := DirectoryNode{}
	for k, v := range node {
		if k == "children" {
			if children, err := toDirectoryNodeSlice(v); err == nil {
				normalized := make([]DirectoryNode, len(children))
				for i, child := range children {
					normalized[i] = normalizeNode(child)
				}
				result[k] = normalized
			} else {
				result[k] = v
			}
		} else {
			result[k] = v
		}
	}
	return result
}

func (w *Workspace) ensureLinkDir(dir string) (string, error) {
	fullDir := filepath.Join(w.RootPath, dir)
	if err := os.MkdirAll(fullDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create link directory %q: %w", fullDir, err)
	}
	return fullDir, nil
}

func createDirectoryLink(linkPath, targetPath string) error {
	if runtime.GOOS == "windows" {
		return createWindowsJunction(linkPath, targetPath)
	}
	return os.Symlink(targetPath, linkPath)
}

func createWindowsJunction(linkPath, targetPath string) error {
	// Windows junction 通过 syscall 或外部命令实现
	// 此处简化处理：先尝试 symlink，失败则返回错误
	if err := os.Symlink(targetPath, linkPath); err != nil {
		return fmt.Errorf("failed to create directory link %q -> %q on windows: %w", linkPath, targetPath, err)
	}
	return nil
}

func removeDirectoryLink(linkPath string) error {
	return os.Remove(linkPath)
}

func isDirectoryLink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

func (w *Workspace) createLink(subdir, name, targetPath string) error {
	linkDir, err := w.ensureLinkDir(subdir)
	if err != nil {
		return err
	}
	linkPath := filepath.Join(linkDir, name)

	// 已存在则跳过
	if _, err := os.Lstat(linkPath); err == nil {
		return nil
	}

	if err := createDirectoryLink(linkPath, targetPath); err != nil {
		logger.Error(logComponent).
			Str("link_path", linkPath).
			Str("target_path", targetPath).
			Err(err).
			Msg("Workspace: failed to create directory link")
		return err
	}

	logger.Info(logComponent).
		Str("link_path", linkPath).
		Str("target_path", targetPath).
		Msg("Workspace: created directory link")
	return nil
}

func (w *Workspace) removeLink(subdir, name string) error {
	linkPath := filepath.Join(w.RootPath, subdir, name)

	if !isDirectoryLink(linkPath) {
		logger.Warn(logComponent).
			Str("link_path", linkPath).
			Msg("Workspace: link does not exist or is not a symlink")
		return nil
	}

	if err := removeDirectoryLink(linkPath); err != nil {
		logger.Error(logComponent).
			Str("link_path", linkPath).
			Err(err).
			Msg("Workspace: failed to remove directory link")
		return err
	}

	logger.Info(logComponent).
		Str("link_path", linkPath).
		Msg("Workspace: removed directory link")
	return nil
}

func (w *Workspace) listLinks(subdir string) []string {
	linkDir := filepath.Join(w.RootPath, subdir)
	entries, err := os.ReadDir(linkDir)
	if err != nil {
		return nil
	}

	var result []string
	for _, entry := range entries {
		if isDirectoryLink(filepath.Join(linkDir, entry.Name())) {
			result = append(result, entry.Name())
		}
	}
	return result
}
