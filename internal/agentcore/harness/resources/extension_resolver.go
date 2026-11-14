package resources

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	mtypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// 本文件对应 Python openjiuwen/harness/resources/extension_resolver.py 全量：
// 类型层（ResourceKind/ResourceRef/LoadRecord/ExtensionParts）、
// resolve 装配（resolve_plugin_parts / resolve_agent_template_parts）、
// persona 渲染（render_agent_template_system_prompt / render_prompt_content）。
//
// 用途：无法挂载 PromptSection 对象的 harness provider（Claude Code、Codex、
// DSH）需要把 persona 当纯文本交给外部 CLI。

// ──────────────────────────── 全局变量 ────────────────────────────

// parentModelExtrasKeyRes 父模型在 BuildContext.extras 里的键。
// Python: deep_agent.py load_agent_template 段 `ctx.extras["_parent_model"]`
// （与 harness 包的 parentModelExtrasKey 同值；两包互不依赖故各存一份。）
const parentModelExtrasKeyRes = "_parent_model"

// templatePlaceholderPattern 模板占位符：{{ name }}，name 为标识符。
// Python: extension_resolver.py:38 _TEMPLATE_PATTERN
var templatePlaceholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderAgentTemplateSystemPrompt 把模板的 persona 段落渲染成一段系统提示词。
// Python: extension_resolver.py:241-256 render_agent_template_system_prompt
//
// 段落按 Priority 升序排序，用空行连接，空段落跳过；ctx 为 nil 时按
// 上游 `BuildContext(language=language)` 兜底。
func RenderAgentTemplateSystemPrompt(spec hschema.AgentTemplateSpec, language string, ctx hschema.BuildContextInterface) string {
	orderedSections := make([]hschema.PromptSectionSpec, len(spec.PromptSections))
	copy(orderedSections, spec.PromptSections)
	// Python: sorted(spec.prompt_sections, key=lambda item: item.priority)
	// 稳定排序保证同 priority 时保持声明顺序（与 Python sorted 一致）
	sort.SliceStable(orderedSections, func(i, j int) bool {
		return orderedSections[i].Priority < orderedSections[j].Priority
	})

	rendered := make([]string, 0, len(orderedSections))
	for _, section := range orderedSections {
		text := renderPromptSectionText(section, ctx, language)
		if text != "" {
			rendered = append(rendered, text)
		}
	}
	return strings.Join(rendered, "\n\n")
}

// RenderPromptContent 渲染单段本地化提示词正文。
// Python: extension_resolver.py:259-268 render_prompt_content
//
// 语言选择四级回退（language → en → cn → 任一），再替换 {{ name }} 占位符；
// 未在 params 中的占位符原样保留。
func RenderPromptContent(content map[string]string, language string, params map[string]any) string {
	return renderTemplate(selectContent(content, language), params)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// renderPromptSectionText 渲染单段 persona 正文。
// Python: extension_resolver.py:236-238 _render_prompt_section_text
//
// render_params 会先与 BuildContext 派生的值合并（language / workspace），
// 用户显式给的键覆盖派生值（Python: values.update(dict(render_params or {}))）。
func renderPromptSectionText(section hschema.PromptSectionSpec, ctx hschema.BuildContextInterface, language string) string {
	content := selectContent(section.Content, language)
	return renderTemplate(content, renderParams(section.RenderParams, ctx))
}

// resolvePromptSection 解析一段提示词段落：逐语言渲染占位符后构造 PromptSection。
// Python: extension_resolver.py:230-234 _resolve_prompt_section
func resolvePromptSection(spec hschema.PromptSectionSpec, ctx hschema.BuildContextInterface) saprompts.PromptSection {
	params := renderParams(spec.RenderParams, ctx)
	content := make(map[string]string, len(spec.Content))
	for language, text := range spec.Content {
		content[language] = renderTemplate(text, params)
	}
	return saprompts.PromptSection{Name: spec.Name, Content: content, Priority: spec.Priority}
}

// renderParams 汇总占位符参数：language / workspace 派生值在前，render_params 覆盖在后。
// Python: extension_resolver.py:287-294 _render_params
func renderParams(renderParamsMap map[string]any, ctx hschema.BuildContextInterface) map[string]any {
	workspaceRoot := ""
	if ctx != nil && ctx.GetWorkspace() != nil {
		workspaceRoot = ctx.GetWorkspace().RootPath
	}
	values := map[string]any{
		"language":  defaultLanguage(languageFromCtx(ctx)),
		"workspace": workspaceRoot,
	}
	for key, value := range renderParamsMap {
		values[key] = value
	}
	return values
}

// languageFromCtx 取 ctx 的 language；nil ctx 视为空（由 defaultLanguage 回退 cn）。
// Python: ctx.language
func languageFromCtx(ctx hschema.BuildContextInterface) string {
	if ctx == nil {
		return ""
	}
	return ctx.GetLanguage()
}

// selectContent 按语言选正文。
// Python: extension_resolver.py:275-285 _select_content
//
// 四级回退：language → en → cn → 任一（map 遍历无序，Go 侧取字典序最小的键
// 以保证输出确定；Python 用 next(iter(...)) 取插入序第一个，两者都只在
// 「三种语言都没有」时才有差异，属极端兜底）。
func selectContent(content map[string]string, language string) string {
	if len(content) == 0 {
		return ""
	}
	if text, ok := content[language]; ok {
		return text
	}
	if text, ok := content["en"]; ok {
		return text
	}
	if text, ok := content["cn"]; ok {
		return text
	}
	keys := make([]string, 0, len(content))
	for key := range content {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return content[keys[0]]
}

// renderTemplate 替换 {{ name }} 占位符；未命中时保留原文。
// Python: extension_resolver.py:296-303 _render_template
func renderTemplate(text string, params map[string]any) string {
	if text == "" {
		return ""
	}
	return templatePlaceholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := templatePlaceholderPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		value, ok := params[sub[1]]
		if !ok {
			return match
		}
		if str, isString := value.(string); isString {
			return str
		}
		// Python: str(params[key]) —— 非字符串值走字符串化
		return fmt.Sprint(value)
	})
}

// defaultLanguage 空语言回退 cn。
// Python: extension_resolver.py:289 `ctx.language or "cn"`
func defaultLanguage(language string) string {
	if language == "" {
		return "cn"
	}
	return language
}

// ──────────────────────────── 15.15 类型层（对齐 extension_resolver.py:41-101）────────────────────────────

// ResourceKind Plugin / AgentTemplate 热加载记账的资源叶子类别。
// Python: extension_resolver.py:41-50 ResourceKind（str 枚举，成员全集不得裁剪）
type ResourceKind string

const (
	// ResourceKindTool 工具能力
	ResourceKindTool ResourceKind = "tool"
	// ResourceKindMCP MCP 服务器
	ResourceKindMCP ResourceKind = "mcp"
	// ResourceKindRail rail
	ResourceKindRail ResourceKind = "rail"
	// ResourceKindPromptSection 提示词段落
	ResourceKindPromptSection ResourceKind = "prompt_section"
	// ResourceKindSkill 技能目录
	ResourceKindSkill ResourceKind = "skill"
	// ResourceKindSubagent 子代理
	ResourceKindSubagent ResourceKind = "subagent"
)

// ResourceRef 本次 load 实际应用的一个运行时绑定引用。
// Python: extension_resolver.py:52-57 ResourceRef
type ResourceRef struct {
	// Kind 资源类别
	Kind ResourceKind `json:"kind"`
	// Identity 资源标识（工具名 / server_id / rail 类型名 / 段落名 / 技能目录 / subagent key）
	Identity string `json:"identity"`
	// Extra 类别相关的附加事实（ability_names / server_name / mode+directory / 快照等）
	Extra map[string]any `json:"extra,omitempty"`
}

// LoadRecord 一次 Plugin / AgentTemplate 热加载产出的事实。
// Python: extension_resolver.py:60-65 LoadRecord
type LoadRecord struct {
	// LoadID 32 位十六进制 uuid4().hex 等价
	LoadID string `json:"load_id"`
	// SourceURI 文件型加载的 manifest 绝对路径；内存 Spec 加载为 nil
	SourceURI string `json:"source_uri,omitempty"`
	// Refs 本次 load 产生的绑定引用（卸载时逆序撤销）
	Refs []ResourceRef `json:"refs,omitempty"`
}

// ResolvedSkill 解析后的技能目录引用。
// Python: extension_resolver.py:68-74 ResolvedSkill（frozen dataclass）
type ResolvedSkill struct {
	// Directory 技能目录绝对路径
	Directory string
	// Mode 加载模式："all" / "auto_list"
	Mode string
	// EnabledSkills 显式启用列表；nil 表示全量启用
	EnabledSkills []string
}

// ResolvedPromptSection 一个解析后的提示词段落 + 宿主绑定的冲突策略。
// Python: extension_resolver.py:77-88 ResolvedPromptSection
//
// ReplaceExisting 仅对 AgentTemplate 根 persona 的 identity 段为 true：
// 绑定时替换（并快照）既有同名段落而非让整批失败，卸载时恢复快照；
// Plugin 的提示词段落恒为 false。
type ResolvedPromptSection struct {
	// Section 段落本体
	Section saprompts.PromptSection
	// ReplaceExisting 同名冲突时替换并快照（仅 AgentTemplate identity 段）
	ReplaceExisting bool
}

// ExtensionParts 一次 Plugin / AgentTemplate 热加载的 resolve→apply 交接载体。
// Python: extension_resolver.py:91-100 ExtensionParts（dataclass）
type ExtensionParts struct {
	// Tools 预构建的工具或工具卡片
	Tools []any
	// Mcps MCP 服务器配置
	Mcps []mtypes.McpServerConfig
	// Rails rail 实例
	Rails []agentinterfaces.AgentRail
	// PromptSections 解析后的提示词段落
	PromptSections []ResolvedPromptSection
	// Skills 解析后的技能
	Skills []ResolvedSkill
	// Subagents 物化后的子代理配置
	Subagents []hschema.SubAgentConfig
}

// NewResourceRef 构造 ResourceRef，Extra 缺省为空 map。
// Python: pydantic Field(default_factory=dict)
func NewResourceRef(kind ResourceKind, identity string) ResourceRef {
	return ResourceRef{Kind: kind, Identity: identity, Extra: map[string]any{}}
}

// NewLoadRecord 构造 LoadRecord，load_id 为 uuid4().hex 等价。
// Python: extension_resolver.py:63 default_factory=lambda: uuid4().hex
func NewLoadRecord() LoadRecord {
	return LoadRecord{
		LoadID: uuidHex(),
		Refs:   []ResourceRef{},
	}
}

// NewExtensionParts 构造 ExtensionParts，各列表缺省为空切片。
// Python: dataclass field(default_factory=list)
func NewExtensionParts() ExtensionParts {
	return ExtensionParts{
		Tools:          []any{},
		Mcps:           []mtypes.McpServerConfig{},
		Rails:          []agentinterfaces.AgentRail{},
		PromptSections: []ResolvedPromptSection{},
		Skills:         []ResolvedSkill{},
		Subagents:      []hschema.SubAgentConfig{},
	}
}

// uuidHex 生成 32 位十六进制随机串（uuid4().hex 等价）。
func uuidHex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 随机源失败属不可恢复环境错误；回退到时间戳拼串保唯一性
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	// uuid4：第 7 字节高 4 位置 0b0100，第 9 字节高 2 位置 0b10（RFC 4122 变体）
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:])
}

// ──────────────────────────── resolve 装配层（对齐 extension_resolver.py:103-233）────────────────────────────

// ResolvePluginParts 把 PluginSpec 物化成活的 Parts。从不产出 subagents。
// Python: extension_resolver.py:103-117 resolve_plugin_parts
func ResolvePluginParts(spec hschema.PluginSpec, ctx hschema.BuildContextInterface) (ExtensionParts, error) {
	if err := hschema.ValidatePluginPaths(spec); err != nil {
		return ExtensionParts{}, err
	}
	language := defaultLanguage(languageFromCtx(ctx))
	tools, err := resolveTools(spec.Tools, language, ctx)
	if err != nil {
		return ExtensionParts{}, err
	}
	rails, err := resolveRails(spec.Rails, language, ctx)
	if err != nil {
		return ExtensionParts{}, err
	}
	parts := NewExtensionParts()
	parts.Tools = tools
	for i := range spec.Mcps {
		parts.Mcps = append(parts.Mcps, buildMcpServerConfig(&spec.Mcps[i]))
	}
	parts.Rails = rails
	for i := range spec.PromptSections {
		parts.PromptSections = append(parts.PromptSections, ResolvedPromptSection{
			Section: resolvePromptSection(spec.PromptSections[i], ctx), ReplaceExisting: false,
		})
	}
	for i := range spec.Skills {
		parts.Skills = append(parts.Skills, resolveSkill(&spec.Skills[i]))
	}
	return parts, nil
}

// ResolveAgentTemplateParts 把 AgentTemplateSpec 物化成活的 Parts：
// 覆盖根 persona/能力，并把直接 subagents 物化为 SubAgentConfig。
// 宿主的 agent_card / model 在此不动，由 DeepAgent 调用方保持不变。
// Python: extension_resolver.py:119-151 resolve_agent_template_parts
func ResolveAgentTemplateParts(spec hschema.AgentTemplateSpec, ctx hschema.BuildContextInterface) (ExtensionParts, error) {
	if err := hschema.ValidatePluginPaths(spec); err != nil {
		return ExtensionParts{}, err
	}
	language := defaultLanguage(languageFromCtx(ctx))
	// Python: parent_model = (ctx.extras or {}).get("_parent_model")
	var parentModel *llm.Model
	var parentExtras map[string]any
	if ctx != nil {
		parentExtras = ctx.GetExtras()
	}
	if raw, ok := parentExtras[parentModelExtrasKeyRes]; ok {
		if m, isModel := raw.(*llm.Model); isModel {
			parentModel = m
		}
	}
	if len(spec.Subagents) > 0 && parentModel == nil {
		return ExtensionParts{}, fmt.Errorf(
			"AgentTemplate subagent resolve requires BuildContext.extras['_parent_model']")
	}

	subagents := make([]hschema.SubAgentConfig, 0, len(spec.Subagents))
	for i := range spec.Subagents {
		child := &spec.Subagents[i]
		// Python: 子模板由 loader 固定 subagents=[]（非递归），此处无需再递归。
		subagentSpec := adaptAgentTemplateToSubagentSpec(child, ctx, language)
		built, err := subagentSpec.Build(parentModel, language, ctx)
		if err != nil {
			return ExtensionParts{}, err
		}
		if built == nil {
			return ExtensionParts{}, fmt.Errorf(
				"SubAgentSpec.build returned nil, expected SubAgentConfig")
		}
		subagents = append(subagents, *built)
	}

	tools, err := resolveTools(spec.Tools, language, ctx)
	if err != nil {
		return ExtensionParts{}, err
	}
	rails, err := resolveRails(spec.Rails, language, ctx)
	if err != nil {
		return ExtensionParts{}, err
	}
	parts := NewExtensionParts()
	parts.Tools = tools
	for i := range spec.Mcps {
		parts.Mcps = append(parts.Mcps, buildMcpServerConfig(&spec.Mcps[i]))
	}
	parts.Rails = rails
	// AgentTemplate 根 persona 的段落允许替换既有同名段（identity 覆盖语义）。
	for i := range spec.PromptSections {
		parts.PromptSections = append(parts.PromptSections, ResolvedPromptSection{
			Section: resolvePromptSection(spec.PromptSections[i], ctx), ReplaceExisting: true,
		})
	}
	for i := range spec.Skills {
		parts.Skills = append(parts.Skills, resolveSkill(&spec.Skills[i]))
	}
	parts.Subagents = subagents
	return parts, nil
}

// adaptAgentTemplateToSubagentSpec 内部适配器：直接子 AgentTemplateSpec → SubAgentSpec。
// 仅用于根模板的直接 subagents（loader 已固定 subagents=[]，构造上非递归）；
// 公开包 schema 不接触 SubAgentSpec。
// Python: extension_resolver.py:153-173 _adapt_agent_template_to_subagent_spec
func adaptAgentTemplateToSubagentSpec(
	template *hschema.AgentTemplateSpec,
	ctx hschema.BuildContextInterface,
	language string,
) *hschema.SubAgentSpec {
	orderedSections := make([]hschema.PromptSectionSpec, len(template.PromptSections))
	copy(orderedSections, template.PromptSections)
	sort.SliceStable(orderedSections, func(i, j int) bool {
		return orderedSections[i].Priority < orderedSections[j].Priority
	})
	rendered := make([]string, 0, len(orderedSections))
	for _, section := range orderedSections {
		rendered = append(rendered, renderPromptSectionText(section, ctx, language))
	}
	// Python: rails=list(template.rails) or None；skills=[skill.dir ...] or None
	rails := template.Rails
	if len(rails) == 0 {
		rails = nil
	}
	var skills []string
	for i := range template.Skills {
		skills = append(skills, template.Skills[i].Dir)
	}
	if len(skills) == 0 {
		skills = nil
	}
	var mcps []*mtypes.McpServerConfig
	for i := range template.Mcps {
		cfg := buildMcpServerConfig(&template.Mcps[i])
		mcps = append(mcps, &cfg)
	}
	return &hschema.SubAgentSpec{
		AgentCard:    &template.AgentCard,
		SystemPrompt: strings.Join(rendered, "\n\n"),
		BuiltinTools: template.Tools,
		Mcps:         mcps,
		Model:        template.Model,
		Rails:        rails,
		Skills:       skills,
		Language:     language,
	}
}

// resolveTools 逐条构建工具声明为实例（或卡片）列表。
// Python: extension_resolver.py:175-180 _resolve_tools
// （上游不传 tool_id；Go 签名要求，传空串保持同语义。）
func resolveTools(specs []hschema.BuiltinToolSpec, language string, ctx hschema.BuildContextInterface) ([]any, error) {
	tools := []any{}
	for i := range specs {
		built, err := specs[i].Build(language, "", ctx)
		if err != nil {
			return nil, err
		}
		tools = append(tools, asBuiltList(built)...)
	}
	return tools, nil
}

// resolveRails 逐条构建 rail 声明为实例列表。
// Python: extension_resolver.py:182-187 _resolve_rails
func resolveRails(specs []hschema.RailSpec, language string, ctx hschema.BuildContextInterface) ([]agentinterfaces.AgentRail, error) {
	rails := []agentinterfaces.AgentRail{}
	ws := workspaceOf(ctx)
	for i := range specs {
		built, err := specs[i].Build(language, ws, ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range asBuiltList(built) {
			rail, ok := item.(agentinterfaces.AgentRail)
			if !ok {
				return nil, fmt.Errorf("rail spec %q built unexpected type %T", specs[i].Type, item)
			}
			rails = append(rails, rail)
		}
	}
	return rails, nil
}

// asBuiltList 归一 build 产物：nil → 空列表；列表过滤 nil；单值包一元列表。
// Python: extension_resolver.py:189-195 _as_built_list
func asBuiltList(built any) []any {
	if built == nil {
		return nil
	}
	if items, ok := built.([]any); ok {
		filtered := make([]any, 0, len(items))
		for _, item := range items {
			if item != nil {
				filtered = append(filtered, item)
			}
		}
		return filtered
	}
	return []any{built}
}

// buildMcpServerConfig 把已绝对化的 McpServerSpec 转成运行时配置。
// 不做相对路径解析：包加载器保证所有路径字段（含 stdio cwd）已绝对化。
// Python: extension_resolver.py:197-228 _build_mcp_server_config
func buildMcpServerConfig(spec *hschema.McpServerSpec) mtypes.McpServerConfig {
	params := map[string]any{}
	for key, value := range spec.Params {
		params[key] = value
	}
	var serverPath string
	if spec.Type == "stdio" {
		// Python: params.setdefault(...) —— 已存在的键不被 spec 字段覆盖
		if spec.Command != "" {
			if _, exists := params["command"]; !exists {
				params["command"] = spec.Command
			}
		}
		if len(spec.Args) > 0 {
			if _, exists := params["args"]; !exists {
				params["args"] = append([]string(nil), spec.Args...)
			}
		}
		if len(spec.Env) > 0 {
			if _, exists := params["env"]; !exists {
				env := map[string]string{}
				for k, v := range spec.Env {
					env[k] = v
				}
				params["env"] = env
			}
		}
		if spec.Cwd != "" {
			if _, exists := params["cwd"]; !exists {
				params["cwd"] = spec.Cwd
			}
		}
		serverPath = spec.Command
		if serverPath == "" {
			serverPath = spec.ServerName
		}
		if serverPath == "" {
			serverPath = "stdio"
		}
	} else {
		serverPath = spec.URL
		if serverPath == "" {
			serverPath = spec.Command
		}
	}

	serverName := spec.ServerName
	if serverName == "" {
		serverName = spec.Command
	}
	if serverName == "" {
		serverName = "mcp_server"
	}

	config := mtypes.McpServerConfig{
		ServerName:      serverName,
		ServerPath:      serverPath,
		ClientType:      spec.Type,
		Params:          params,
		AuthHeaders:     cloneStringMap(spec.AuthHeaders),
		AuthQueryParams: cloneStringMap(spec.AuthQueryParams),
	}
	if spec.ServerID != "" {
		config.ServerID = spec.ServerID
	}
	return config
}

// resolveSkill 把 SkillSpec 转成解析后的技能引用。
// Python: extension_resolver.py:271-273 _resolve_skill
func resolveSkill(spec *hschema.SkillSpec) ResolvedSkill {
	return ResolvedSkill{Directory: spec.Dir, Mode: spec.Mode, EnabledSkills: spec.EnabledSkills}
}

// workspaceOf 取 ctx 的活工作空间；nil ctx 返回 nil。
func workspaceOf(ctx hschema.BuildContextInterface) *workspace.Workspace {
	if ctx == nil {
		return nil
	}
	return ctx.GetWorkspace()
}

// cloneStringMap 复制字符串映射，nil 保持 nil（对齐 Python dict(...) 的浅拷贝语义）。
func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
