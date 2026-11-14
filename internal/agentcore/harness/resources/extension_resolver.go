package resources

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	mtypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// 本文件对应 Python openjiuwen/harness/resources/extension_resolver.py 的
// persona 渲染部分：把模板的 prompt_sections 渲染成一段纯文本系统提示词。
//
// 用途：无法挂载 PromptSection 对象的 harness provider（Claude Code、Codex、
// DSH）需要把 persona 当纯文本交给外部 CLI。
//
// ⏵ 15.15a：本文件只实现渲染路径。resolve_agent_template_parts /
// resolve_plugin_parts（ExtensionParts 装配、plugin 能力覆盖、资源引用解析）
// 属 15.15a。

// ──────────────────────────── 全局变量 ────────────────────────────

// templatePlaceholderPattern 模板占位符：{{ name }}，name 为标识符。
// Python: extension_resolver.py:38 _TEMPLATE_PATTERN
var templatePlaceholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\}\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderAgentTemplateSystemPrompt 把模板的 persona 段落渲染成一段系统提示词。
// Python: extension_resolver.py:241-256 render_agent_template_system_prompt
//
// 段落按 Priority 升序排序，用空行连接，空段落跳过。
func RenderAgentTemplateSystemPrompt(spec hschema.AgentTemplateSpec, language string) string {
	sections := make([]hschema.PromptSectionSpec, len(spec.PromptSections))
	copy(sections, spec.PromptSections)
	// Python: sorted(spec.prompt_sections, key=lambda item: item.priority)
	// 稳定排序保证同 priority 时保持声明顺序（与 Python sorted 一致）
	sort.SliceStable(sections, func(i, j int) bool {
		return sections[i].Priority < sections[j].Priority
	})

	rendered := make([]string, 0, len(sections))
	for _, section := range sections {
		text := renderPromptSectionText(section, language)
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
func renderPromptSectionText(section hschema.PromptSectionSpec, language string) string {
	params := map[string]any{
		"language":  defaultLanguage(language),
		"workspace": "",
	}
	for key, value := range section.RenderParams {
		params[key] = value
	}
	return renderTemplate(selectContent(section.Content, language), params)
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
