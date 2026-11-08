package harness_config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MetaSchema 治理元数据，用于展示和权限管理，运行时不使用
type MetaSchema struct {
	// Owner 负责人
	Owner string `yaml:"owner" json:"owner"`
	// Tags 标签列表
	Tags []string `yaml:"tags" json:"tags"`
	// Visibility 可见性，默认 "internal"
	Visibility string `yaml:"visibility" json:"visibility"`
}

// SectionSchema 单个提示词段条目
//
// 无 file：内联 YAML 内容，由 HarnessConfigBuilder 静态组装。
// 有 file：内容由 HarnessConfigBuilder 写入 workspace/{file}，
//
//	由 ContextEngineeringRail 在每次模型调用时动态读回。
type SectionSchema struct {
	// Name 段名称
	Name string `yaml:"name" json:"name"`
	// Priority 优先级
	Priority *int `yaml:"priority" json:"priority,omitempty"`
	// File 文件名（若设置，内容写入工作空间文件）
	File *string `yaml:"file" json:"file,omitempty"`
	// Content 段内容，支持 string 或 map[string]string
	Content any `yaml:"content" json:"content,omitempty"`
}

// ToolResourceSchema 工具资源规格
type ToolResourceSchema struct {
	// Type 类型：builtin / package / entry_point
	Type string `yaml:"type" json:"type"`
	// Names 内置工具组名称列表（builtin 类型使用）
	Names []string `yaml:"names" json:"names,omitempty"`
	// Name 单个工具组名称或 entry_point 名称
	Name *string `yaml:"name" json:"name,omitempty"`
	// Package pip 包名（仅信息性）
	Package *string `yaml:"package" json:"package,omitempty"`
	// Module 点分模块路径
	Module *string `yaml:"module" json:"module,omitempty"`
	// ClassName 类名（YAML 中映射为 class）
	ClassName *string `yaml:"class" json:"class,omitempty"`
}

// RailResourceSchema Rail 资源规格
type RailResourceSchema struct {
	// Type 类型：builtin / package / entry_point
	Type string `yaml:"type" json:"type"`
	// Name Rail 名称
	Name *string `yaml:"name" json:"name,omitempty"`
	// Package pip 包名
	Package *string `yaml:"package" json:"package,omitempty"`
	// Module 点分模块路径
	Module *string `yaml:"module" json:"module,omitempty"`
	// ClassName 类名（YAML 中映射为 class）
	ClassName *string `yaml:"class" json:"class,omitempty"`
}

// SkillsSchema 技能配置
type SkillsSchema struct {
	// Dirs 技能目录列表
	Dirs []string `yaml:"dirs" json:"dirs"`
	// Mode 模式：all / auto_list
	Mode string `yaml:"mode" json:"mode"`
}

// McpResourceSchema MCP 服务器规格
type McpResourceSchema struct {
	// Type 客户端类型：stdio / sse / streamable_http
	Type string `yaml:"type" json:"type"`
	// Command 启动命令
	Command string `yaml:"command" json:"command"`
	// Args 命令参数
	Args []string `yaml:"args" json:"args,omitempty"`
	// Env 环境变量
	Env map[string]string `yaml:"env" json:"env,omitempty"`
}

// ResourcesSchema 运行时资源：工具、Rail、技能、MCP
type ResourcesSchema struct {
	// Tools 工具资源列表
	Tools []ToolResourceSchema `yaml:"tools" json:"tools,omitempty"`
	// Rails Rail 资源列表
	Rails []RailResourceSchema `yaml:"rails" json:"rails,omitempty"`
	// Skills 技能配置
	Skills *SkillsSchema `yaml:"skills" json:"skills,omitempty"`
	// Mcps MCP 服务器列表
	Mcps []McpResourceSchema `yaml:"mcps" json:"mcps,omitempty"`
}

// PromptsSchema 提示词段声明
type PromptsSchema struct {
	// Sections 段列表
	Sections []SectionSchema `yaml:"sections" json:"sections,omitempty"`
}

// WorkspaceSchema 工作空间（文件操作根目录）
type WorkspaceSchema struct {
	// RootPath 根路径，默认 "./"
	RootPath string `yaml:"root_path" json:"root_path"`
}

// HarnessConfig harness_config.yaml 顶层 Schema
type HarnessConfig struct {
	// SchemaVersion Schema 版本，默认 "harness_config.v0.1"
	SchemaVersion string `yaml:"schema_version" json:"schema_version"`
	// Meta 治理元数据
	Meta *MetaSchema `yaml:"meta" json:"meta,omitempty"`
	// ID Agent 标识 → AgentCard.ID
	ID *string `yaml:"id" json:"id,omitempty"`
	// Name Agent 名称 → AgentCard.Name
	Name *string `yaml:"name" json:"name,omitempty"`
	// Description Agent 描述 → AgentCard.Description
	Description *string `yaml:"description" json:"description,omitempty"`
	// Workspace 工作空间配置
	Workspace *WorkspaceSchema `yaml:"workspace" json:"workspace,omitempty"`
	// Prompts 提示词配置
	Prompts *PromptsSchema `yaml:"prompts" json:"prompts,omitempty"`
	// Resources 资源配置
	Resources *ResourcesSchema `yaml:"resources" json:"resources,omitempty"`
	// Language 语言，默认 "cn"
	Language string `yaml:"language" json:"language"`
	// MaxIterations 最大迭代次数
	MaxIterations *int `yaml:"max_iterations" json:"max_iterations,omitempty"`
	// CompletionTimeout 完成超时秒数
	CompletionTimeout *float64 `yaml:"completion_timeout" json:"completion_timeout,omitempty"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultSchemaVersion 默认 Schema 版本
	DefaultSchemaVersion = "harness_config.v0.1"
	// DefaultVisibility 默认可见性
	DefaultVisibility = "internal"
	// DefaultSkillsMode 默认技能模式
	DefaultSkillsMode = "all"
	// DefaultMcpType 默认 MCP 客户端类型
	DefaultMcpType = "stdio"
	// DefaultWorkspaceRootPath 默认工作空间根路径
	DefaultWorkspaceRootPath = "./"
	// DefaultLanguage 默认语言
	DefaultLanguage = "cn"
	// DefaultSectionPriority 默认段优先级
	DefaultSectionPriority = 30
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateHarnessConfig 校验 HarnessConfig 的 schema_version 字段
func ValidateHarnessConfig(cfg *HarnessConfig) error {
	if cfg == nil {
		return fmt.Errorf("harness_config 不能为 nil")
	}
	if cfg.SchemaVersion == "" {
		return fmt.Errorf("schema_version 不能为空")
	}
	if cfg.SchemaVersion != DefaultSchemaVersion {
		return fmt.Errorf("不支持的 schema_version: %s，当前仅支持 %s", cfg.SchemaVersion, DefaultSchemaVersion)
	}
	return nil
}

// ToYAML 将 HarnessConfig 序列化为 YAML 字符串。
// 若提供 outputPath，同时将内容写入该文件。
func (cfg *HarnessConfig) ToYAML(outputPath ...string) (string, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("序列化 HarnessConfig 到 YAML 失败: %w", err)
	}
	yamlStr := string(data)
	if len(outputPath) > 0 && outputPath[0] != "" {
		if err := os.WriteFile(outputPath[0], data, 0644); err != nil {
			return yamlStr, fmt.Errorf("写入 YAML 文件失败: %w", err)
		}
	}
	return yamlStr, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setDefaults 为 HarnessConfig 设置默认值
func setDefaults(cfg *HarnessConfig) {
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = DefaultSchemaVersion
	}
	if cfg.Language == "" {
		cfg.Language = DefaultLanguage
	}
	if cfg.Meta != nil && cfg.Meta.Visibility == "" {
		cfg.Meta.Visibility = DefaultVisibility
	}
	if cfg.Workspace != nil && cfg.Workspace.RootPath == "" {
		cfg.Workspace.RootPath = DefaultWorkspaceRootPath
	}
	if cfg.Resources != nil {
		for i := range cfg.Resources.Mcps {
			if cfg.Resources.Mcps[i].Type == "" {
				cfg.Resources.Mcps[i].Type = DefaultMcpType
			}
		}
		if cfg.Resources.Skills != nil && cfg.Resources.Skills.Mode == "" {
			cfg.Resources.Skills.Mode = DefaultSkillsMode
		}
	}
}
