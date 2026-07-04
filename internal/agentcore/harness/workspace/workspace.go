package workspace

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Workspace 工作空间定义，管理 DeepAgent 的目录和文件布局。
//
// 对应 Python: openjiuwen/harness/workspace/workspace.py (Workspace)
type Workspace struct {
	// RootPath 工作空间根路径
	RootPath string `json:"root_path" yaml:"root_path"`
	// Directories 顶层目录节点列表
	Directories []DirectoryNode `json:"directories" yaml:"directories"`
	// Language 工作空间语言（"cn" 或 "en"）
	Language string `json:"language" yaml:"language"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// WorkspaceNode 工作空间标准目录节点名称枚举，提供类型安全的标准目录访问。
//
// 对应 Python: WorkspaceNode(Enum)
type WorkspaceNode string

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

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// DirectoryNode 目录节点类型，表示工作空间中的一个目录或文件定义。
//
// 对应 Python: DirectoryNode = Dict[str, Any]
type DirectoryNode = map[string]any

// defaultWorkspaceSchemaCN 中文默认工作空间模式
//
// 对应 Python: DEFAULT_WORKSPACE_SCHEMA
var defaultWorkspaceSchemaCN = []DirectoryNode{
	{
		"name":            "AGENT.md",
		"description":     "基础配置和能力",
		"path":            "AGENT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":            "SOUL.md",
		"description":     "人格、性格和价值观",
		"path":            "SOUL.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":            "HEARTBEAT.md",
		"description":     "心跳日志和状态记录",
		"path":            "HEARTBEAT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":            "IDENTITY.md",
		"description":     "身份凭证和权限",
		"path":            "IDENTITY.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
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
				"default_content": "",
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
				"default_content": "",
			},
		},
	},
}

// defaultWorkspaceSchemaEN 英文默认工作空间模式
//
// 对应 Python: DEFAULT_WORKSPACE_SCHEMA_EN
var defaultWorkspaceSchemaEN = []DirectoryNode{
	{
		"name":            "AGENT.md",
		"description":     "Basic agent configuration and capabilities",
		"path":            "AGENT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":            "SOUL.md",
		"description":     "Agent personality, character, values, and behavioral guidelines",
		"path":            "SOUL.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":            "HEARTBEAT.md",
		"description":     "Heartbeat log / status recording",
		"path":            "HEARTBEAT.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
	},
	{
		"name":            "IDENTITY.md",
		"description":     "Identity credentials, unique identifier, and permission information",
		"path":            "IDENTITY.md",
		"children":        []DirectoryNode{},
		"is_file":         true,
		"default_content": "",
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
				"default_content": "",
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
				"default_content": "",
			},
		},
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWorkspace 创建新的工作空间实例。
//
// 如果 directories 为空，则根据 language 填充默认模式；
// 如果用户提供的 directories 缺少默认模式中的节点，会自动补全。
//
// 对应 Python: Workspace.__post_init__
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

// GetDirectory 递归查找指定名称的目录节点，返回其 path 字段。
//
// 先在用户提供的 directories 中查找，找不到则在默认模式中查找。
// name 可以为 string 或 WorkspaceNode 类型。
//
// 对应 Python: Workspace.get_directory
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

// SetDirectory 添加或更新顶层目录节点。
//
// 接受单个 DirectoryNode 或 []DirectoryNode，每个节点会校验；
// 如果同名节点已存在则替换，否则追加。
//
// 对应 Python: Workspace.set_directory
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

// GetNodePath 返回顶层节点的完整文件系统路径。
//
// 仅查找顶层节点（directories 的直接子节点），不支持嵌套节点。
// 返回 nil 表示未找到。
//
// 对应 Python: Workspace.get_node_path
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

// GetDefaultDirectory 返回指定语言的默认目录模式深拷贝。
//
// 对应 Python: Workspace.get_default_directory
func GetDefaultDirectory(language string) []DirectoryNode {
	return getWorkspaceSchema(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateDirectoryNode 校验单个目录节点的格式和字段。
//
// 校验规则：
//   - name: 非空字符串，不含路径分隔符
//   - path: 若提供必须为字符串
//   - description: 若提供必须为字符串
//   - is_file: 若提供必须为 bool
//   - default_content: 若提供必须为字符串
//   - children: 若提供必须为列表，递归校验每个子节点
//
// 对应 Python: _validate_directory_node
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

// getWorkspaceSchema 根据语言返回默认工作空间模式的深拷贝。
//
// 对应 Python: get_workspace_schema
func getWorkspaceSchema(language string) []DirectoryNode {
	if language == "en" {
		return deepCopyNodes(defaultWorkspaceSchemaEN)
	}
	return deepCopyNodes(defaultWorkspaceSchemaCN)
}

// resolveName 将 any 类型的名称解析为字符串。
//
// 支持 string 和 WorkspaceNode 类型。
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

// findInNodes 在节点列表中递归查找指定名称的目录节点，返回其 path 字段。
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

// deepCopyNode 对单个 DirectoryNode 进行深拷贝，避免共享引用。
// 使用 JSON 序列化/反序列化实现深拷贝，并规范化子节点类型。
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

// deepCopyNodes 对 DirectoryNode 列表进行深拷贝。
func deepCopyNodes(nodes []DirectoryNode) []DirectoryNode {
	result := make([]DirectoryNode, len(nodes))
	for i, node := range nodes {
		result[i] = deepCopyNode(node)
	}
	return result
}

// toDirectoryNodeSlice 将 any 类型的 children 转换为 []DirectoryNode。
// 支持 []DirectoryNode 和 []any（JSON 反序列化结果）两种类型。
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

// normalizeNode 递归规范化节点，将 []any 类型的 children 转换为 []DirectoryNode。
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
