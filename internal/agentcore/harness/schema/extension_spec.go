package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// Plugin / AgentTemplate 热加载 Spec 全量定义。
// Python: openjiuwen/harness/schema/extension_spec.py
//
// 上游为 pydantic 严格模型（extra="forbid"，Rubric/Memory 为 extra="allow"），
// Go 侧等价语义：结构体字段固定 + 显式校验函数（构造/装载方调用）。
//
// 消费方：
//   - factory.py 读取：agent_card / model / mcps / skills / tools / rails / subagents
//   - McpServer 读取：type / server_name / server_id / url / command / args / env / cwd / auth_headers
//   - 15.15 loader/binder：PluginSpec 全字段、路径校验

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTemplateSpec 一个 AgentTemplate 角色的声明式模板。
// Python: extension_spec.py:135-171 AgentTemplateSpec
type AgentTemplateSpec struct {
	// AgentCard 静态身份（由嵌套 agentCard 或扁平 manifest 字段归一化而来）
	AgentCard saschema.AgentCard `json:"agent_card"`
	// Model 可选模型定义（来自 model.json）
	Model *ModelSpec `json:"model,omitempty"`
	// Runtime 可选外部 Harness 运行时；nil 表示用原生 DeepAgent
	Runtime *AgentRuntimeSpec `json:"runtime,omitempty"`
	// PromptSections persona 提示词段落（identity / sops / output_contract / safety ...）
	PromptSections []PromptSectionSpec `json:"prompt_sections,omitempty"`
	// Tools 模板自有工具能力（可能被 Plugin 能力覆盖）
	Tools []BuiltinToolSpec `json:"tools,omitempty"`
	// Mcps MCP 服务器声明
	Mcps []McpServerSpec `json:"mcps,omitempty"`
	// Rails rail 声明
	Rails []RailSpec `json:"rails,omitempty"`
	// Skills 可移植技能声明
	Skills []SkillSpec `json:"skills,omitempty"`
	// Memories 共享记忆（经验、约定、排障知识）；extra="allow" 语义，未知字段原样保留
	Memories []MemorySpec `json:"memories,omitempty"`
	// Rubrics 输出/动作质量评估量表；extra="allow" 语义，未知字段原样保留
	Rubrics []RubricSpec `json:"rubrics,omitempty"`
	// Subagents 直接子模板（物化为运行时子代理）
	Subagents []AgentTemplateSpec `json:"subagents,omitempty"`
	// Metadata 发现与展示元数据（仅 plain data）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PluginSpec 功能能力包，挂载到宿主 DeepAgent。
// Python: extension_spec.py:174-198 PluginSpec
type PluginSpec struct {
	// ID 插件标识（必填）
	ID string `json:"id"`
	// Name 展示名
	Name string `json:"name,omitempty"`
	// Description 描述
	Description string `json:"description,omitempty"`
	// PromptSections 仅覆盖宿主 DeepAgent 的提示词段落
	PromptSections []PromptSectionSpec `json:"prompt_sections,omitempty"`
	// Tools 挂载到宿主的工具能力
	Tools []BuiltinToolSpec `json:"tools,omitempty"`
	// Mcps MCP 服务器声明
	Mcps []McpServerSpec `json:"mcps,omitempty"`
	// Rails rail 声明
	Rails []RailSpec `json:"rails,omitempty"`
	// Skills 可移植技能声明
	Skills []SkillSpec `json:"skills,omitempty"`
	// Metadata 发布/展示元数据（仅 plain data）
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ModelSpec 模板自带的模型定义。
// Python: deep_agent_spec.py:135-138 —— ModelSpec 是 TeamModelConfig 的别名，
// 两个名字指同一类型，跨 import 路径的 identity 判断（is）一致。
type ModelSpec = TeamModelConfig

// AgentRuntimeSpec AgentTemplate 包声明的外部 Harness 运行时。
// Python: extension_spec.py:84-103 AgentRuntimeSpec
type AgentRuntimeSpec struct {
	// ProviderName provider 名（非空，strip 后校验）
	ProviderName string `json:"provider_name"`
	// ProviderVersion provider 版本
	ProviderVersion string `json:"provider_version,omitempty"`
	// SdkPaths SDK 路径列表（load_plugin_spec 语义下必须绝对）
	SdkPaths []string `json:"sdk_paths,omitempty"`
	// Config 透传配置（仅 plain data）
	Config map[string]any `json:"config,omitempty"`
}

// PromptSectionSpec 一个内联 persona 提示词段落。
// Python: extension_spec.py:109-120 PromptSectionSpec
type PromptSectionSpec struct {
	// Name 段落名（identity / sops / output_contract / safety ...）
	Name string `json:"name"`
	// Content 多语言段落正文：语言码 → 正文
	Content map[string]string `json:"content"`
	// Priority 排序权重（升序，上游默认 100）
	Priority int `json:"priority,omitempty"`
	// RenderParams 占位符替换参数（仅 plain data）
	RenderParams map[string]any `json:"render_params,omitempty"`
}

// McpServerSpec 统一的 MCP 服务器声明。
// Python: extension_spec.py:44-67 McpServerSpec
//
// 接受规范字段名（type / server_name），也接受 jiuwenswarm config.yaml 的
// mcp.servers 别名（transport / name）——后者由包加载器归一化后再校验。
type McpServerSpec struct {
	// Type 传输类型："stdio" / "sse" / "streamable_http"
	Type string `json:"type,omitempty"`
	// ServerName 服务器名
	ServerName string `json:"server_name,omitempty"`
	// ServerID 服务器标识
	ServerID string `json:"server_id,omitempty"`
	// URL http/sse 端点
	URL string `json:"url,omitempty"`
	// Command stdio 启动命令
	Command string `json:"command,omitempty"`
	// Args stdio 命令参数
	Args []string `json:"args,omitempty"`
	// Env 环境变量
	Env map[string]string `json:"env,omitempty"`
	// Cwd 工作目录
	Cwd string `json:"cwd,omitempty"`
	// Params 传给服务器的额外参数（仅 plain data）
	Params map[string]any `json:"params,omitempty"`
	// AuthHeaders http 认证头
	AuthHeaders map[string]string `json:"auth_headers,omitempty"`
	// AuthQueryParams http 认证查询参数
	AuthQueryParams map[string]string `json:"auth_query_params,omitempty"`
}

// McpDirSpec 引用一个包含 mcp.json / mcps.yaml 的包目录。
// Python: extension_spec.py:70-74 McpDirSpec
type McpDirSpec struct {
	// Dir 相对包根的 MCP 目录路径
	Dir string `json:"dir"`
}

// SkillSpec skill 目录声明（相对包根）。
// Python: extension_spec.py:76-81 SkillSpec
type SkillSpec struct {
	// Dir skill 目录
	Dir string `json:"dir"`
	// Mode 加载模式："all" 或 "auto_list"
	Mode string `json:"mode,omitempty"`
	// EnabledSkills 显式启用的 skill 列表
	EnabledSkills []string `json:"enabled_skills,omitempty"`
}

// RubricSpec AgentTemplate 评估量表。
// Python: extension_spec.py:123-126，extra="allow"——未知字段原样保留，Go 用自由 map 等价。
type RubricSpec map[string]any

// MemorySpec AgentTemplate 共享记忆引用。
// Python: extension_spec.py:129-132，extra="allow"——未知字段原样保留，Go 用自由 map 等价。
type MemorySpec map[string]any

// ──────────────────────────── 导出函数 ────────────────────────────

// ModelDump 把清单转成 JSON 可表示的 map。
// Python: extension_spec.py 的 model_dump(mode="json")（pydantic BaseModel）
//
// factory.py:306 用 manifest.model_dump(mode="json") 把整份清单交给 native 系
// provider 的 agent_template 配置项。
func (s AgentTemplateSpec) ModelDump() map[string]any {
	encoded, err := json.Marshal(s)
	if err != nil {
		return map[string]any{}
	}
	var dumped map[string]any
	if err := json.Unmarshal(encoded, &dumped); err != nil {
		return map[string]any{}
	}
	return dumped
}

// ModelDump 把 skill 声明转成 JSON 可表示的 map。
// Python: skill.model_dump(exclude_none=True)（factory.py:311）
func (s SkillSpec) ModelDump() map[string]any {
	dumped := map[string]any{"dir": s.Dir}
	if s.Mode != "" {
		dumped["mode"] = s.Mode
	}
	if len(s.EnabledSkills) > 0 {
		dumped["enabled_skills"] = s.EnabledSkills
	}
	return dumped
}

// ValidatePluginSpec 校验 PluginSpec 构造期必填约束（pydantic 字段校验的 Go 等价）。
// Python: PluginSpec.id 必填（:178）+ Metadata plain-data（:194-197）
func ValidatePluginSpec(spec PluginSpec) error {
	if spec.ID == "" {
		return fmt.Errorf("id: field required")
	}
	if err := ValidatePlainMapping(spec.Metadata); err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	return nil
}

// ValidatePlainMapping 校验 map 为 JSON/YAML 可序列化的 plain data。
// Python: extension_spec.py:33-35 _validate_plain_mapping（递归 :17-30 _plain_data）
func ValidatePlainMapping(value map[string]any) error {
	return validatePlainData(value)
}

// ValidatePluginPaths 校验 PluginSpec / AgentTemplateSpec 的每个路径字段必须为绝对路径。
// Python: extension_spec.py:208-236 validate_plugin_paths
//
// Spec 不携带包根：包装载器在进入模型前已把路径绝对化，而
// DeepAgent.load_plugin_spec（无包根）必须拒绝相对路径而非按 cwd 解析。
// 递归进入 AgentTemplateSpec.subagents（仅直接子模板）。
func ValidatePluginPaths(spec any) error {
	switch s := spec.(type) {
	case PluginSpec:
		return validateSpecPaths(s.Tools, s.Rails, s.Mcps, s.Skills, nil)
	case *PluginSpec:
		if s == nil {
			return nil
		}
		return validateSpecPaths(s.Tools, s.Rails, s.Mcps, s.Skills, nil)
	case AgentTemplateSpec:
		return validateTemplatePaths(&s)
	case *AgentTemplateSpec:
		return validateTemplatePaths(s)
	default:
		return fmt.Errorf("validate_plugin_paths: unsupported spec type %T", spec)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateSpecPaths PluginSpec 形状的路径校验。
// Python: validate_plugin_paths :216-234 主体
func validateSpecPaths(tools []BuiltinToolSpec, rails []RailSpec, mcps []McpServerSpec, skills []SkillSpec, runtime *AgentRuntimeSpec) error {
	for _, tool := range tools {
		// Python: if tool.type == "harness.tool.file"（:217）
		if tool.Type == "harness.tool.file" {
			filePath, _ := tool.Params["file_path"].(string)
			if filePath != "" {
				if err := requireAbsolutePath(filePath, "tools.params.file_path"); err != nil {
					return err
				}
			}
		}
	}
	for _, rail := range rails {
		// Python: if rail.type == "harness.rail.file"（:222）
		if rail.Type == "harness.rail.file" {
			filePath, _ := rail.Params["file_path"].(string)
			if filePath != "" {
				if err := requireAbsolutePath(filePath, "rails.params.file_path"); err != nil {
					return err
				}
			}
		}
	}
	for _, mcp := range mcps {
		if mcp.Cwd != "" {
			if err := requireAbsolutePath(mcp.Cwd, "mcps.cwd"); err != nil {
				return err
			}
		}
	}
	for _, skill := range skills {
		if err := requireAbsolutePath(skill.Dir, "skills.dir"); err != nil {
			return err
		}
	}
	if runtime != nil {
		for _, sdkPath := range runtime.SdkPaths {
			if err := requireAbsolutePath(sdkPath, "runtime.sdk_paths"); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateTemplatePaths AgentTemplateSpec 形状：主体校验 + subagents 递归。
// Python: validate_plugin_paths :235-236
func validateTemplatePaths(spec *AgentTemplateSpec) error {
	if spec == nil {
		return nil
	}
	if err := validateSpecPaths(spec.Tools, spec.Rails, spec.Mcps, spec.Skills, spec.Runtime); err != nil {
		return err
	}
	for i := range spec.Subagents {
		if err := validateTemplatePaths(&spec.Subagents[i]); err != nil {
			return err
		}
	}
	return nil
}

// requireAbsolutePath 单字段绝对路径校验。
// Python: extension_spec.py:200-205 _require_absolute_path
func requireAbsolutePath(value, fieldName string) error {
	// Python: Path(value).expanduser().is_absolute()；~ 前缀展开后判绝对
	expanded := value
	if strings.HasPrefix(expanded, "~") {
		expanded = filepath.Join(homeDirOf(), strings.TrimPrefix(expanded, "~"))
	}
	if !filepath.IsAbs(expanded) {
		return fmt.Errorf(
			"%s must already be an absolute path (the package loader or the caller of load_plugin_spec is responsible for absolutizing it): %q",
			fieldName, value)
	}
	return nil
}

// validatePlainData 递归校验值为 plain data（标量/列表/字符串键映射）。
// Python: extension_spec.py:17-30 _plain_data
func validatePlainData(value any) error {
	switch v := value.(type) {
	case nil, bool, int, int64, float64, string:
		return nil
	case []any:
		for _, item := range v {
			if err := validatePlainData(item); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		// Python: if not isinstance(key, str): raise（:26-27）
		// map[string]any 键恒为 string，非字符串键在上游解码阶段即报错；
		// 这里保留对 map[any]any 形状的防御性检查。
		for _, item := range v {
			if err := validatePlainData(item); err != nil {
				return err
			}
		}
		return nil
	case map[any]any:
		for key := range v {
			if _, ok := key.(string); !ok {
				return fmt.Errorf("mapping keys must be strings")
			}
		}
		for _, item := range v {
			if err := validatePlainData(item); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%T is not JSON/YAML serializable data", value)
	}
}

// homeDirOf 返回当前用户主目录（~ 展开用）。
func homeDirOf() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
