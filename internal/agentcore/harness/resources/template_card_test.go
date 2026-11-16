package resources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 15.14 AgentTemplate 展示卡片测试。
// Python: tests/unit_tests/harness/resources/test_template_card.py

// manifestCardPayload 构造最小合法 manifest payload（对齐 Python 测试助手 _manifest）。
func manifestCardPayload(t *testing.T, fields map[string]any) []byte {
	payload := map[string]any{
		"package_type": "agent_template",
		"name":         "finance",
		"description":  "Finance",
	}
	for k, v := range fields {
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

// TestParseAgentTemplateCard_展示字段按语言渲染 Python: test_card_keeps_display_fields_and_renders_them_by_language。
func TestParseAgentTemplateCard_展示字段按语言渲染(t *testing.T) {
	card, err := ParseAgentTemplateCard(manifestCardPayload(t, map[string]any{
		"version":             " 1.2.0 ",
		"category":            "IndustryConsultant",
		"avatar":              "avatars/a.png",
		"display_name":        map[string]any{"zh": "理财专家", "en": "Finance Expert"},
		"display_description": map[string]any{"en": "Funds and stocks"},
		"tags":                []any{map[string]any{"zh": "基金", "en": "Funds"}, "plain", map[string]any{"en": ""}, 3},
		"skills":              []any{map[string]any{"dir": "./skills/a-share"}, "skills/fund", map[string]any{"dir": "skills/fund"}},
		"mcps":                []any{map[string]any{"connector": "github"}, map[string]any{"name": "local", "command": "x"}},
	}))
	require.NoError(t, err)
	assert.Equal(t, "1.2.0", card.Version)
	// cn 读取 manifest 的 zh 键
	assert.Equal(t, "理财专家", card.DisplayLabel("cn"), "cn reads the manifest's zh key")
	assert.Equal(t, "Finance Expert", card.DisplayLabel("en"))
	// 缺失语言回退英文
	assert.Equal(t, "Funds and stocks", card.DisplayText("cn"), "missing language falls back to English")
	assert.Equal(t, []string{"基金", "plain"}, card.TagLabels("cn"))
	assert.Equal(t, []string{"Funds", "plain"}, card.TagLabels("en"))
	// 非文本 tag（数字 3）丢弃；空文本 map tag 保留
	wantTags := []any{
		map[string]any{"zh": "基金", "en": "Funds"},
		"plain",
		map[string]any{"en": ""},
	}
	assert.True(t, reflect.DeepEqual(card.Tags, wantTags), "tags = %#v", card.Tags)
	// skills 取目录名去重（"skills/fund" 与 {"dir": "skills/fund"} 同名）
	assert.Equal(t, []string{"a-share", "fund"}, card.Skills)
	assert.Equal(t, []string{"github"}, card.Connectors)
	assert.Equal(t, "native", card.Runtime)
	assert.Nil(t, card.RuntimeProvider)
}

// TestParseAgentTemplateCard_展示名回退到name Python: test_display_name_falls_back_to_name。
func TestParseAgentTemplateCard_展示名回退到name(t *testing.T) {
	card, err := ParseAgentTemplateCard(manifestCardPayload(t, nil))
	require.NoError(t, err)
	assert.Equal(t, "finance", card.DisplayLabel("cn"))
	assert.Equal(t, "Finance", card.DisplayText("en"))
	assert.Equal(t, "", card.DisplayName)
}

// TestParseAgentTemplateCard_外部runtime标记 Python: test_runtime_marks_an_external_template。
func TestParseAgentTemplateCard_外部runtime标记(t *testing.T) {
	card, err := ParseAgentTemplateCard(manifestCardPayload(t, map[string]any{
		"runtime": map[string]any{"provider_name": " claudecode ", "config": map[string]any{"skill_conflict": "replace"}},
	}))
	require.NoError(t, err)
	assert.Equal(t, "external", card.Runtime)
	require.NotNil(t, card.RuntimeProvider)
	assert.Equal(t, "claudecode", *card.RuntimeProvider, "provider_name strip 后取值")
	assert.Equal(t, map[string]any{"skill_conflict": "replace"}, card.RuntimeConfig)
	// 空 runtime dict（falsy）→ native，对齐 Python if runtime 判断
	empty, err := ParseAgentTemplateCard(manifestCardPayload(t, map[string]any{"runtime": map[string]any{}}))
	require.NoError(t, err)
	assert.Equal(t, "native", empty.Runtime)
}

// TestParseAgentTemplateCard_除packageType外宽松 Python: test_card_is_lenient_except_for_package_type。
func TestParseAgentTemplateCard_除packageType外宽松(t *testing.T) {
	card, err := ParseAgentTemplateCard([]byte(`{"package_type": "agent_template"}`))
	require.NoError(t, err)
	want := AgentTemplateCard{Name: "", Description: "", DisplayName: "", DisplayDescription: "", Runtime: "native", RuntimeConfig: map[string]any{}}
	assert.Equal(t, want, card)
	_, err = ParseAgentTemplateCard([]byte(`{"package_type": "plugin"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 'agent_template'")
}

// TestLocalizedText_顺序 Python: test_localized_text_order。
func TestLocalizedText_顺序(t *testing.T) {
	assert.Equal(t, "中", localizedText(map[string]any{"cn": "中", "zh": "文"}, "cn"), "cn 优先取 cn 键")
	assert.Equal(t, "bonjour", localizedText(map[string]any{"fr": "bonjour"}, "cn"), "全链缺失取任意非空")
	assert.Equal(t, "plain", localizedText("  plain ", "en"), "str strip")
	assert.Equal(t, "", localizedText(map[string]any{}, "en"), "空映射读空")
}

// TestReadCard_只读manifest不校验包体 Python: test_read_card_reads_only_the_manifest。
func TestReadCard_只读manifest不校验包体(t *testing.T) {
	dir := t.TempDir()
	body := manifestCardPayload(t, map[string]any{
		"persona": map[string]any{"dir": "missing-dir"},
		"tools":   []any{map[string]any{"file": "absent.py"}},
	})
	p := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(p, body, 0o644))
	card, err := ReadAgentTemplateCard(p) // D-3：异步入口同步实现
	require.NoError(t, err)
	assert.Equal(t, "finance", card.Name, "卡片不校验 persona/tools，全量 loader 负责")
}

// TestReadCard_不可读manifest映射为错误 Python: test_read_card_maps_unreadable_manifests_to_value_error。
func TestReadCard_不可读manifest映射为错误(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "manifest.json")
	_, err := ReadAgentTemplateCard(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not readable")
	require.NoError(t, os.WriteFile(p, []byte("{broken"), 0o644))
	_, err = ReadAgentTemplateCard(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid JSON")
	require.NoError(t, os.WriteFile(p, []byte("[]"), 0o644))
	_, err = ReadAgentTemplateCard(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON object")
}

// TestSkillNames_非列表读空 卡片侧 _as_list 语义：skills 为字符串（非列表）→ 空目录名。
// Python: template_card.py:169-170 _as_list 非 list 一律返回空（与 loader 侧标量包装不同）。
func TestSkillNames_非列表读空(t *testing.T) {
	card, err := ParseAgentTemplateCard([]byte(`{"package_type":"agent_template","name":"n","description":"d","skills":"py"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Skills) != 0 {
		t.Fatalf("scalar skills must read empty, got %v", card.Skills)
	}
}
