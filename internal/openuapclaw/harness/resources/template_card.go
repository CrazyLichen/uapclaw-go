package resources

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
)

// AgentTemplate 展示卡片：只从 manifest.json 读取展示字段。
// Python: openjiuwen/harness/resources/template_card.py
//
// load_agent_template_package 产出运行期 AgentTemplateSpec，只保留运行所需内容；
// manifest 的展示字段（display_name、category、tags、version...）不属于该 spec，
// 由卡片承载（对应 Card / Config 拆分）：列出或展示一个包只读 manifest.json，
// 绝不加载 persona、tools 或 skills。
//
// 卡片刻意宽松：只要求 package_type 正确；包能否真正运行由全量 loader 决定。

// ──────────────────────────── 结构体 ────────────────────────────

// LocalizedText manifest 展示值：纯字符串或多语言映射。
// Python: template_card.py:26 LocalizedText = str | dict[str, str]
// 偏差 D-1（二次收窄，用户 2026-11-30 拍板）：union → 结构化两形态类型，
// 卡片字段不再承载 any；Marshal/Unmarshal 保持 wire 形状（字符串形态↔纯字符串）。
type LocalizedText struct {
	// Str 纯字符串形态（Map 为 nil 时生效）
	Str string `json:"-"`
	// Map 多语言形态 {语言码: 文本}（nil 表示字符串形态）
	Map map[string]string `json:"-"`
}

// MarshalJSON 字符串形态序列化为纯字符串，map 形态为对象（对齐 Python union wire）。
func (t LocalizedText) MarshalJSON() ([]byte, error) {
	if t.Map != nil {
		return json.Marshal(t.Map)
	}
	return json.Marshal(t.Str)
}

// UnmarshalJSON 纯字符串读 Str 形态，对象读 Map 形态（值字符串化，null 值丢弃）。
// Python: template_card.py:160-166 normalize_localized
func (t *LocalizedText) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		t.Str = str
		t.Map = nil
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		// 数字/布尔等非文本值读空（normalize_localized 的 default 分支）
		t.Str, t.Map = "", nil
		return nil
	}
	t.Str = ""
	t.Map = make(map[string]string, len(raw))
	for key, text := range raw {
		if text != nil {
			t.Map[key] = fmt.Sprintf("%v", text)
		}
	}
	return nil
}

// AgentTemplateCard 一个 AgentTemplate 包的展示信息。
// Python: template_card.py:37-63 AgentTemplateCard（dataclass frozen，D-2：Go 侧不可变靠约定）
type AgentTemplateCard struct {
	// Name manifest.name；缺失为空串
	Name string `json:"name"`
	// Description manifest.description；缺失为空串
	Description string `json:"description"`
	// DisplayName 展示名：纯字符串或 {语言: 文本}（D-1：union → 结构化 LocalizedText，无 any）
	DisplayName LocalizedText `json:"display_name"`
	// DisplayDescription 展示描述：同 DisplayName 形状
	DisplayDescription LocalizedText `json:"display_description"`
	// Category 分类；非字符串读空
	Category string `json:"category"`
	// Tags manifest tags（纯字符串或本地化 map；非文本项丢弃）
	Tags []LocalizedText `json:"tags,omitempty"`
	// Version 版本
	Version string `json:"version"`
	// Avatar 包相对头像路径（manifest 原样）
	Avatar string `json:"avatar"`
	// Runtime manifest 声明 runtime（非空 dict）为 "external"，否则 "native"
	Runtime string `json:"runtime"`
	// RuntimeProvider 外部 runtime 的 manifest.runtime.provider_name
	RuntimeProvider *string `json:"runtime_provider,omitempty"`
	// RuntimeConfig manifest.runtime.config 原样：宿主交给 provider 的配置（不加载/不校验整个包）
	RuntimeConfig map[string]any `json:"runtime_config"`
	// Skills 技能目录名（manifest 顺序）
	Skills []string `json:"skills,omitempty"`
	// Connectors 宿主托管 MCP connector 名（{"connector": name} 项）
	Connectors []string `json:"connectors,omitempty"`
}

// ──────────────────────────── 常量 ────────────────────────────

// languageKeys 团队 / harness 语言码与 manifest 使用的键映射。
// Python: template_card.py:30-34 _LANGUAGE_KEYS
var languageKeys = map[string][]string{
	"cn": {"cn", "zh"},
	"zh": {"zh", "cn"},
	"en": {"en"},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DisplayLabel 指定语言的展示名，回退到 name。
// Python: template_card.py:65-67 display_label
func (c AgentTemplateCard) DisplayLabel(language string) string {
	return orString(localizedText(c.DisplayName, language), c.Name)
}

// DisplayText 指定语言的展示描述，回退到 description。
// Python: template_card.py:69-71 display_text
func (c AgentTemplateCard) DisplayText(language string) string {
	return orString(localizedText(c.DisplayDescription, language), c.Description)
}

// TagLabels 标签按语言渲染；空标签丢弃。
// Python: template_card.py:73-76 tag_labels
func (c AgentTemplateCard) TagLabels(language string) []string {
	labels := make([]string, 0, len(c.Tags))
	for _, tag := range c.Tags {
		if label := localizedText(tag, language); label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

// localizedText 按 language 取值（cn 兼读 zh），再英文，再任意文本。
// Python: template_card.py:79-87 localized_text
func localizedText(value LocalizedText, language string) string {
	if value.Map == nil {
		return strings.TrimSpace(value.Str)
	}
	keys := languageKeys[language]
	if keys == nil {
		keys = []string{language}
	}
	for _, key := range append(append([]string{}, keys...), "en", "zh", "cn") {
		if text := strings.TrimSpace(value.Map[key]); text != "" {
			return text
		}
	}
	// 全链缺失：取任意第一个非空文本
	for _, text := range value.Map {
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}

// ParseAgentTemplateCard 从已解析的 manifest.json 构造卡片（纯函数）。
// Python: template_card.py:90-119 parse_agent_template_card
func ParseAgentTemplateCard(manifestJSON []byte) (AgentTemplateCard, error) {
	var payload map[string]any
	if err := json.Unmarshal(manifestJSON, &payload); err != nil || payload == nil {
		return AgentTemplateCard{}, fmt.Errorf("AgentTemplate manifest must contain a mapping")
	}
	if payload["package_type"] != "agent_template" {
		return AgentTemplateCard{}, fmt.Errorf(
			"manifest declares package_type=%q, expected 'agent_template'", payload["package_type"])
	}
	runtime, _ := payload["runtime"].(map[string]any)
	var provider *string
	if runtime != nil {
		if raw, ok := runtime["provider_name"].(string); ok && strings.TrimSpace(raw) != "" {
			p := strings.TrimSpace(raw)
			provider = &p
		}
	}
	// Python: runtime = "external" if runtime else "native" —— 空 dict falsy → native
	runtimeName := "native"
	if runtime != nil && len(runtime) > 0 {
		runtimeName = "external"
	}
	return AgentTemplateCard{
		Name:               manifestText(payload["name"]),
		Description:        manifestText(payload["description"]),
		DisplayName:        normalizeLocalized(payload["display_name"]),
		DisplayDescription: normalizeLocalized(payload["display_description"]),
		Category:           manifestText(payload["category"]),
		Tags:               localizedListItems(payload["tags"]),
		Version:            manifestText(payload["version"]),
		Avatar:             asString(payload["avatar"]),
		Runtime:            runtimeName,
		RuntimeProvider:    provider,
		RuntimeConfig:      runtimeConfigOf(runtime),
		Skills:             skillNames(payload["skills"]),
		Connectors:         connectorNames(payload["mcps"]),
	}, nil
}

// ReadAgentTemplateCard 异步 LoadAgentTemplateCard 语义入口（D-3：沿 15.15 D-15b 同步实现）。
// Python: template_card.py:122-124 read_agent_template_card
func ReadAgentTemplateCard(manifestPath string) (AgentTemplateCard, error) {
	return LoadAgentTemplateCard(manifestPath)
}

// LoadAgentTemplateCard 只读 manifest.json 构造卡片。
// Python: template_card.py:127-137 load_agent_template_card
func LoadAgentTemplateCard(manifestPath string) (AgentTemplateCard, error) {
	payload, err := readManifestMapping(manifestPath)
	if err != nil {
		return AgentTemplateCard{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return AgentTemplateCard{}, fmt.Errorf("manifest is not valid JSON: %s", manifestPath)
	}
	return ParseAgentTemplateCard(raw)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readManifestMapping 读 JSON manifest，读/解析失败统一映射 error。
// Python: template_card.py:140-152 read_manifest_mapping
func readManifestMapping(pathStr string) (map[string]any, error) {
	expanded, err := expandUserPath(pathStr)
	if err != nil {
		return nil, fmt.Errorf("manifest not readable: %s", pathStr)
	}
	text, err := os.ReadFile(expanded)
	if err != nil {
		return nil, fmt.Errorf("manifest not readable: %s", expanded)
	}
	// Python: json.loads 先解析（语法错误→not valid JSON），再 isinstance dict 校验（→JSON object）
	var probe any
	if err := json.Unmarshal(text, &probe); err != nil {
		return nil, fmt.Errorf("manifest is not valid JSON: %s", expanded)
	}
	payload, ok := probe.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("manifest must contain a JSON object: %s", expanded)
	}
	return payload, nil
}

// manifestText manifest 文本字段 strip；非字符串读空。
// Python: template_card.py:155-157 manifest_text
func manifestText(value any) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// normalizeLocalized 归一化 manifest 展示值：保留字符串，map 值字符串化，其余清空。
// Python: template_card.py:160-166 normalize_localized
func normalizeLocalized(value any) LocalizedText {
	switch v := value.(type) {
	case string:
		return LocalizedText{Str: v}
	case map[string]any:
		m := make(map[string]string, len(v))
		for key, text := range v {
			if text != nil {
				m[key] = fmt.Sprintf("%v", text)
			}
		}
		return LocalizedText{Map: m}
	default:
		return LocalizedText{}
	}
}

// cardAsList 非列表读空（与 extension_loader 的 asList 语义不同：Python 两个模块
// 各有同名 _as_list，卡片侧列表外一律返回空；loader 侧标量包装为单元素列表）。
// Python: template_card.py:169-170 _as_list
func cardAsList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return nil
}

// localizedListItems tags 归一化：仅保留字符串或 map 项（非文本项丢弃）。
// Python: template_card.py:109-111 tags=tuple(normalize_localized(tag) for tag in _as_list(...) if isinstance(tag, (str, Mapping)))
func localizedListItems(value any) []LocalizedText {
	var out []LocalizedText
	for _, item := range cardAsList(value) {
		switch item.(type) {
		case string, map[string]any:
			out = append(out, normalizeLocalized(item))
		}
	}
	return out
}

// skillNames 技能目录名（str 或 {"dir": ...} 项），取路径基名 + 去重（D-4：PurePath.name→path.Base）。
// Python: template_card.py:173-180 _skill_names —— 注意用卡片侧 _as_list（非列表→空），
// 不能用 loader 侧 asList（标量包装），两模块同名函数语义不同。
func skillNames(items any) []string {
	var names []string
	for _, item := range cardAsList(items) {
		var raw string
		switch v := item.(type) {
		case string:
			raw = v
		case map[string]any:
			if d, ok := v["dir"].(string); ok {
				raw = d
			}
		}
		name := ""
		if raw != "" {
			name = path.Base(raw)
		}
		if name != "" && !containsString(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// connectorNames connector 名去重。
// Python: template_card.py:183-190 _connector_names —— 卡片侧 _as_list（非列表→空）
func connectorNames(items any) []string {
	var names []string
	for _, item := range cardAsList(items) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := m["connector"].(string); ok {
			name := strings.TrimSpace(c)
			if name != "" && !containsString(names, name) {
				names = append(names, name)
			}
		}
	}
	return names
}

// runtimeConfigOf 提取 runtime.config（非 map → 空 map）。
// Python: template_card.py:116 runtime_config=dict(runtime.get("config") or {}) if isinstance(runtime, Mapping) else {}
func runtimeConfigOf(runtime map[string]any) map[string]any {
	out := map[string]any{}
	if runtime == nil {
		return out
	}
	cfg, ok := runtime["config"].(map[string]any)
	if !ok || cfg == nil {
		return out
	}
	for k, v := range cfg {
		out[k] = v
	}
	return out
}

// asString 仅字符串取值，其余空串。
func asString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// orString 非空优先。
func orString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ──────────────────────────── 导出包装（experts 域跨包消费，15.16e） ────────────────────────────

// ReadManifestMapping 读 manifest 映射的导出包装。
// Python: read_manifest_mapping (template_card.py)——harness.resources 公共 API，
// experts/agent_group 跨包消费；包内既有私有 readManifestMapping 保持。
func ReadManifestMapping(path string) (map[string]any, error) {
	return readManifestMapping(path)
}

// ManifestText manifest 文本值导出包装。
// Python: manifest_text
func ManifestText(value any) string {
	return manifestText(value)
}

// NormalizeLocalized 本地化值归一导出包装。
// Python: normalize_localized
func NormalizeLocalized(value any) LocalizedText {
	return normalizeLocalized(value)
}

// LocalizedTextOf 按语言取本地化文本导出包装。
// Python: localized_text
func LocalizedTextOf(value LocalizedText, language string) string {
	return localizedText(value, language)
}
