package schema

import (
	"encoding/json"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ⏵ 15.14 / 15.15a：本文件为最小形状桩。
//
// 完整的 manifest schema（校验、插件/模板扩展点、prompt_sections 渲染、
// memories / rubrics 装配）在 15.14 / 15.15a 落地后替换。
// 此处只提供 15.5 的 factory 需要的字段与 JSON 编解码。
//
// Python: openjiuwen/harness/schema/extension_spec.py
//
// 字段集合按上游全量声明（避免 15.14/15.15a 回填时改结构），
// 但只有 factory.py 实际读取的字段参与 15.5 的逻辑：
//
//	factory.py 读取：agent_card / model / mcps / skills / tools / rails / subagents
//	McpServer 读取：type / server_name / server_id / url / command / args / env / cwd / auth_headers

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
	Tools []map[string]any `json:"tools,omitempty"`
	// Mcps MCP 服务器声明
	Mcps []McpServerSpec `json:"mcps,omitempty"`
	// Rails rail 声明
	Rails []map[string]any `json:"rails,omitempty"`
	// Skills portable skills
	Skills []SkillSpec `json:"skills,omitempty"`
	// Memories 共享记忆（经验、约定、排障知识）
	Memories []map[string]any `json:"memories,omitempty"`
	// Rubrics 输出/动作质量评估量表
	Rubrics []map[string]any `json:"rubrics,omitempty"`
	// Subagents 直接子模板（物化为运行时子代理）
	Subagents []AgentTemplateSpec `json:"subagents,omitempty"`
	// Metadata 发现与展示元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ModelSpec 模板自带的模型定义。
// Python: extension_spec.py 的 ModelSpec（model.json 的结构）
type ModelSpec struct {
	// ModelClientConfig 客户端配置
	ModelClientConfig *ModelClientConfig `json:"model_client_config,omitempty"`
	// ModelRequestConfig 请求配置
	ModelRequestConfig *ModelRequestConfig `json:"model_request_config,omitempty"`
}

// ModelClientConfig 模型客户端配置。
// Python: core/foundation/llm/schema 的 ModelClientConfig
type ModelClientConfig struct {
	// APIBase 服务地址
	APIBase string `json:"api_base,omitempty"`
	// APIKey 凭据
	APIKey string `json:"api_key,omitempty"`
	// ClientProvider 客户端厂商（可能是枚举或字符串）
	ClientProvider any `json:"client_provider,omitempty"`
}

// ModelRequestConfig 模型请求配置。
// Python: core/foundation/llm/schema 的 ModelRequestConfig
type ModelRequestConfig struct {
	// ModelName 模型名
	ModelName string `json:"model_name,omitempty"`
}

// AgentRuntimeSpec AgentTemplate 包声明的外部 Harness 运行时。
// Python: extension_spec.py:84-90 AgentRuntimeSpec
type AgentRuntimeSpec struct {
	// ProviderName provider 名
	ProviderName string `json:"provider_name"`
	// ProviderVersion provider 版本
	ProviderVersion string `json:"provider_version,omitempty"`
}

// PromptSectionSpec 一个内联 persona 提示词段落。
// Python: extension_spec.py:109-120 PromptSectionSpec
type PromptSectionSpec struct {
	// Name 段落名（identity / sops / output_contract / safety ...）
	Name string `json:"name"`
	// Content 多语言段落正文：语言码 → 正文
	Content map[string]string `json:"content"`
	// Priority 排序权重（升序，默认 100）
	Priority int `json:"priority,omitempty"`
	// RenderParams 占位符替换参数
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
	// Params 传给服务器的额外参数
	Params map[string]any `json:"params,omitempty"`
	// AuthHeaders http 认证头
	AuthHeaders map[string]string `json:"auth_headers,omitempty"`
	// AuthQueryParams http 认证查询参数
	AuthQueryParams map[string]string `json:"auth_query_params,omitempty"`
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
