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

// AgentTemplateCard 一个 AgentTemplate 包的展示信息。
// Python: template_card.py:37-63 AgentTemplateCard（dataclass frozen，D-2：Go 侧不可变靠约定）
type AgentTemplateCard struct {
	// Name manifest.name；缺失为空串
	Name string `json:"name"`
	// Description manifest.description；缺失为空串
	Description string `json:"description"`
	// DisplayName 展示名：纯字符串或 {语言: 文本} map（D-1：union 判别，string 或 map[string]string）
	DisplayName any `json:"display_name"`
	// DisplayDescription 展示描述：同 DisplayName 形状
	DisplayDescription any `json:"display_description"`
	// Category 分类；非字符串读空
	Category string `json:"category"`
	// Tags manifest tags 原样（纯字符串或本地化 map；非文本项丢弃）
	Tags []any `json:"tags,omitempty"`
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

// LocalizedText 按 language 取值（cn 兼读 zh），再英文，再任意文本。
// Python: template_card.py:79-87 localized_text
func LocalizedText(value any, language string) string {
	return localizedText(value, language)
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

// localizedText LocalizedText 的导出包装本体。
func localizedText(value any, language string) string {
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	m, ok := value.(map[string]any)
	if !ok {
		if ms, ok2 := value.(map[string]string); ok2 {
			m = mapToAnyMap(ms)
		} else {
			return ""
		}
	}
	keys := languageKeys[language]
	if keys == nil {
		keys = []string{language}
	}
	for _, key := range append(append([]string{}, keys...), "en", "zh", "cn") {
		if text := strings.TrimSpace(asString(m[key])); text != "" {
			return text
		}
	}
	// 全链缺失：取任意第一个非空文本
	for _, text := range m {
		if s := strings.TrimSpace(asString(text)); s != "" {
			return s
		}
	}
	return ""
}

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
func normalizeLocalized(value any) any {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, text := range v {
			if text != nil {
				out[key] = fmt.Sprintf("%v", text)
			}
		}
		return out
	default:
		return ""
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
func localizedListItems(value any) []any {
	var out []any
	for _, item := range cardAsList(value) {
		switch item.(type) {
		case string, map[string]any:
			out = append(out, normalizeLocalized(item))
		}
	}
	return out
}

// skillNames 技能目录名（str 或 {"dir": ...} 项），取路径基名 + 去重（D-4：PurePath.name→path.Base）。
// Python: template_card.py:173-180 _skill_names
func skillNames(items any) []string {
	var names []string
	for _, item := range asList(items) {
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
// Python: template_card.py:183-190 _connector_names
func connectorNames(items any) []string {
	var names []string
	for _, item := range asList(items) {
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

// mapToAnyMap map[string]string → map[string]any。
func mapToAnyMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// orString 非空优先。
func orString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

