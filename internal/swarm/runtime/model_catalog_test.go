package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// modelEntry 构造一条模型配置条目。
// Python: model_catalog.py:93-122 _descriptor 消费的形状
func modelEntry(modelName, alias, provider string, isDefault bool, extra map[string]any) map[string]any {
	entry := map[string]any{
		"model_client_config": map[string]any{
			"model_name":      modelName,
			"client_provider": provider,
		},
		"is_default": isDefault,
	}
	if alias != "" {
		entry["alias"] = alias
	}
	for key, value := range extra {
		entry[key] = value
	}
	return entry
}

// ──────────────────────────── 目录构建 ────────────────────────────

// TestBuildModelCatalog_selectionKey带全局索引 selection_key 为 model_name#全局下标。
// Python: model_catalog.py:112-115 f"{model_name}#{global_index}"
func TestBuildModelCatalog_selectionKey带全局索引(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", false, nil),
		modelEntry("m2", "", "p", false, nil),
	}, "")
	require.Len(t, catalog.Models, 2)
	require.Equal(t, "m1#0", catalog.Models[0].SelectionKey)
	require.Equal(t, "m2#1", catalog.Models[1].SelectionKey)
}

// TestBuildModelCatalog_跳过条目不重排下标 被跳过条目仍占据全局位置。
// Python: model_catalog.py:150-153 "skipped entries still retain their position"
func TestBuildModelCatalog_跳过条目不重排下标(t *testing.T) {
	catalog := BuildModelCatalog([]any{
		modelEntry("m1", "", "p", false, nil),
		"not-a-mapping",                          // 跳过但仍占 index 1
		map[string]any{"no_client_config": true}, // 跳过但仍占 index 2
		modelEntry("m4", "", "p", false, nil),
	}, "")
	require.Len(t, catalog.Models, 2)
	require.Equal(t, "m1#0", catalog.Models[0].SelectionKey)
	require.Equal(t, "m4#3", catalog.Models[1].SelectionKey, "跳过条目仍占全局下标")
}

// TestBuildModelCatalog_过滤多模态专用档 video/audio/vision 不作为聊天模型。
// Python: model_catalog.py:102-107 _RESERVED_MULTIMODAL_NAMES
func TestBuildModelCatalog_过滤多模态专用档(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("video", "", "p", false, nil),
		modelEntry("audio", "", "p", false, nil),
		modelEntry("vision", "", "p", false, nil),
		modelEntry("VISION", "", "p", false, nil), // 大小写不敏感
		modelEntry("chat-model", "", "p", false, nil),
	}, "")
	require.Len(t, catalog.Models, 1)
	require.Equal(t, "chat-model", catalog.Models[0].ModelName)
}

// TestBuildModelCatalog_别名作展示名 alias 优先作 display_name。
// Python: model_catalog.py:100-101 display_name = alias or model_name
func TestBuildModelCatalog_别名作展示名(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "友好名", "p", false, nil),
		modelEntry("m2", "", "p", false, nil),
	}, "")
	require.Equal(t, "友好名", catalog.Models[0].DisplayName)
	require.Equal(t, "m1", catalog.Models[0].ModelName)
	require.Equal(t, "m2", catalog.Models[1].DisplayName)
}

// TestBuildModelCatalog_agentos不算默认 agentos 来源的 is_default 强制为假。
// Python: model_catalog.py:120 is_default=bool(entry.get("is_default")) and not is_agentos
func TestBuildModelCatalog_agentos不算默认(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("agentos-model", "", "p", true, map[string]any{
			"model_config_obj": map[string]any{"_source": "agentos"},
		}),
		modelEntry("normal-model", "", "p", false, nil),
	}, "")
	require.False(t, catalog.Models[0].IsDefault, "agentos 来源不算默认")
	require.True(t, catalog.Models[0].IsAgentOS)
	require.Equal(t, "agentos-model", catalog.CurrentSelection[:len("agentos-model")],
		"无默认时取首项为当前")
}

// TestBuildModelCatalog_推理等级off reasoning_level 为 false 时归一为 "off"。
// Python: model_catalog.py:87-90 _reasoning_level
func TestBuildModelCatalog_推理等级off(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", false, map[string]any{
			"model_config_obj": map[string]any{"reasoning_level": false},
		}),
		modelEntry("m2", "", "p", false, map[string]any{
			"model_config_obj": map[string]any{"reasoning_level": "high"},
		}),
	}, "")
	require.Equal(t, "off", catalog.Models[0].ReasoningLevel)
	require.Equal(t, "high", catalog.Models[1].ReasoningLevel)
}

// TestBuildModelCatalog_空列表返回空模型 无有效条目时 models 为空切片。
// Python: model_catalog.py:163-164
func TestBuildModelCatalog_空列表返回空模型(t *testing.T) {
	catalog := BuildModelCatalog(nil, "")
	require.Empty(t, catalog.Models)
	require.Equal(t, "", catalog.CurrentSelection)
}

// TestBuildModelCatalog_当前选择按默认 未指定时取默认项。
// Python: model_catalog.py:168-169 next(item for item in models if item.is_default)
func TestBuildModelCatalog_当前选择按默认(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", false, nil),
		modelEntry("m2", "", "p", true, nil),
	}, "")
	require.Equal(t, "m2#1", catalog.CurrentSelection)
	require.False(t, catalog.Models[0].IsCurrent)
	require.True(t, catalog.Models[1].IsCurrent)
}

// TestBuildModelCatalog_当前选择按请求 指定 selection_key 时精确命中并标记。
// Python: model_catalog.py:166-173
func TestBuildModelCatalog_当前选择按请求(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", true, nil),
		modelEntry("m2", "", "p", false, nil),
	}, "m2#1")
	require.Equal(t, "m2#1", catalog.CurrentSelection)
	require.False(t, catalog.Models[0].IsCurrent)
	require.True(t, catalog.Models[1].IsCurrent)
}

// TestBuildModelCatalog_当前选择按模型名 用裸模型名解析并标记。
// Python: model_catalog.py:133-135 按 model_name 匹配
func TestBuildModelCatalog_当前选择按模型名(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", true, nil),
		modelEntry("m2", "", "p", false, nil),
	}, "m2")
	require.Equal(t, "m2#1", catalog.CurrentSelection)
}

// ──────────────────────────── 模型选择 ────────────────────────────

// TestResolveModelSelection_空选择报错 空串/空白报 required。
// Python: model_catalog.py:186-188
func TestResolveModelSelection_空选择报错(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", false, nil),
	}, "")
	for _, requested := range []string{"", "   "} {
		_, err := ResolveModelSelection(catalog, requested)
		require.ErrorIs(t, err, ErrModelSelectionRequired)
	}
}

// TestResolveModelSelection_多模态专用档报错 video/audio/vision 拒绝（大小写不敏感）。
// Python: model_catalog.py:189-193
func TestResolveModelSelection_多模态专用档报错(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", false, nil),
	}, "")
	for _, requested := range []string{"video", "AUDIO", "Vision"} {
		_, err := ResolveModelSelection(catalog, requested)
		require.ErrorIs(t, err, ErrModelMultimodalOnly, "requested=%q", requested)
	}
}

// TestResolveModelSelection_精确键命中 selection_key 精确匹配。
// Python: model_catalog.py:129-131
func TestResolveModelSelection_精确键命中(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", false, nil),
		modelEntry("m2", "", "p", false, nil),
	}, "")
	got, err := ResolveModelSelection(catalog, "m2#1")
	require.NoError(t, err)
	require.Equal(t, "m2", got.ModelName)
	require.True(t, got.IsCurrent, "Python:207 replace(selected, is_current=True)")
}

// TestResolveModelSelection_按模型名优先默认 同名多条时取 is_default。
// Python: model_catalog.py:133-135
func TestResolveModelSelection_按模型名优先默认(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("dup", "", "p", false, nil),
		modelEntry("dup", "", "p", true, nil),
	}, "")
	got, err := ResolveModelSelection(catalog, "dup")
	require.NoError(t, err)
	require.Equal(t, "dup#1", got.SelectionKey, "优先 is_default 那条")
}

// TestResolveModelSelection_唯一别名命中 别名唯一时可解析。
// Python: model_catalog.py:137-142
func TestResolveModelSelection_唯一别名命中(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "别名一", "p", false, nil),
		modelEntry("m2", "别名二", "p", false, nil),
	}, "")
	got, err := ResolveModelSelection(catalog, "别名二")
	require.NoError(t, err)
	require.Equal(t, "m2", got.ModelName)
}

// TestResolveModelSelection_歧义别名报错 多个模型同别名时报 ambiguous。
// Python: model_catalog.py:196-205
func TestResolveModelSelection_歧义别名报错(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "同名", "p", false, nil),
		modelEntry("m2", "同名", "p", false, nil),
	}, "")
	_, err := ResolveModelSelection(catalog, "同名")
	require.ErrorIs(t, err, ErrModelAmbiguous)
}

// TestResolveModelSelection_未找到报错 完全不匹配时报 not found。
// Python: model_catalog.py:206
func TestResolveModelSelection_未找到报错(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "", "p", false, nil),
	}, "")
	_, err := ResolveModelSelection(catalog, "never-exists")
	require.ErrorIs(t, err, ErrModelNotFound)
}

// ──────────────────────────── 全局索引语法 ────────────────────────────

// TestResolveConfiguredModelEntry_带索引语法 按 name#index 定位原始条目。
// Python: model_catalog.py:224-241
func TestResolveConfiguredModelEntry_带索引语法(t *testing.T) {
	entries := []any{
		modelEntry("m1", "", "p", false, nil),
		modelEntry("m2", "", "p", false, nil),
	}
	got, ok := ResolveConfiguredModelEntry(entries, "m2#1")
	require.True(t, ok)
	require.Equal(t, "m2", got["model_client_config"].(map[string]any)["model_name"])
}

// TestResolveConfiguredModelEntry_名字与索引不符返回false 语法合法但名字不匹配。
// Python: model_catalog.py:241 return entry if configured_name == bare_name else None
func TestResolveConfiguredModelEntry_名字与索引不符返回false(t *testing.T) {
	entries := []any{modelEntry("m1", "", "p", false, nil)}
	_, ok := ResolveConfiguredModelEntry(entries, "wrong#0")
	require.False(t, ok)
}

// TestResolveConfiguredModelEntry_索引越界返回false 越界与非法索引均拒绝。
// Python: model_catalog.py:232-233
func TestResolveConfiguredModelEntry_索引越界返回false(t *testing.T) {
	entries := []any{modelEntry("m1", "", "p", false, nil)}
	for _, requested := range []string{"m1#5", "m1#-1", "m1#abc", "#0"} {
		_, ok := ResolveConfiguredModelEntry(entries, requested)
		require.False(t, ok, "requested=%q", requested)
	}
}

// TestResolveConfiguredModelEntry_裸名匹配优先默认 无索引时按名匹配优先 is_default。
// Python: model_catalog.py:243-255
func TestResolveConfiguredModelEntry_裸名匹配优先默认(t *testing.T) {
	entries := []any{
		modelEntry("dup", "别名甲", "p", false, nil),
		modelEntry("dup", "别名乙", "p", true, nil),
	}
	got, ok := ResolveConfiguredModelEntry(entries, "dup")
	require.True(t, ok)
	require.Equal(t, "别名乙", got["alias"], "优先 is_default 条目")
}

// TestResolveConfiguredModelEntry_唯一别名 别名唯一时命中。
// Python: model_catalog.py:256-261
func TestResolveConfiguredModelEntry_唯一别名(t *testing.T) {
	entries := []any{
		modelEntry("m1", "唯一", "p", false, nil),
		modelEntry("m2", "别的", "p", false, nil),
	}
	got, ok := ResolveConfiguredModelEntry(entries, "唯一")
	require.True(t, ok)
	require.Equal(t, "m1", got["model_client_config"].(map[string]any)["model_name"])
}

// TestResolveConfiguredModelEntry_空请求返回false 空请求不解析。
// Python: model_catalog.py:220-222
func TestResolveConfiguredModelEntry_空请求返回false(t *testing.T) {
	entries := []any{modelEntry("m1", "", "p", false, nil)}
	_, ok := ResolveConfiguredModelEntry(entries, "")
	require.False(t, ok)
}

// ──────────────────────────── 序列化 ────────────────────────────

// TestRuntimeModelDescriptorToDict_八字段 投影含全部 8 个安全字段。
// Python: model_catalog.py:43-54 to_dict
func TestRuntimeModelDescriptorToDict_八字段(t *testing.T) {
	got := RuntimeModelDescriptorToDict(RuntimeModelDescriptor{
		SelectionKey: "m1#0", DisplayName: "M1", ModelName: "m1",
		Provider: "p", ReasoningLevel: "high", IsDefault: true,
		IsAgentOS: false, IsCurrent: true,
	})
	require.Len(t, got, 8)
	require.Equal(t, "m1#0", got["selection_key"])
	require.Equal(t, "M1", got["display_name"])
	require.Equal(t, "m1", got["model_name"])
	require.Equal(t, "p", got["provider"])
	require.Equal(t, "high", got["reasoning_level"])
	require.Equal(t, true, got["is_default"])
	require.Equal(t, false, got["is_agentos"])
	require.Equal(t, true, got["is_current"])
}

// TestModelCatalogResultToDict_三字段 含 models/current_selection/current_display_name。
// Python: model_catalog.py:65-71 to_dict
func TestModelCatalogResultToDict_三字段(t *testing.T) {
	catalog := BuildModelCatalogFromEntries([]map[string]any{
		modelEntry("m1", "名一", "p", true, nil),
	}, "")
	got := ModelCatalogResultToDict(catalog)
	require.Len(t, got, 3)
	require.Equal(t, "m1#0", got["current_selection"])
	require.Equal(t, "名一", got["current_display_name"])
	models, ok := got["models"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, models, 1)
}

// TestModelCatalogResultToDict_不含连接密钥 输出中无 api_key 等敏感字段。
// Python: model_catalog.py:66 "without model connection secrets"
func TestModelCatalogResultToDict_不含连接密钥(t *testing.T) {
	entry := modelEntry("m1", "", "p", true, nil)
	entry["model_client_config"].(map[string]any)["api_key"] = "secret-value"

	catalog := BuildModelCatalogFromEntries([]map[string]any{entry}, "")
	got := ModelCatalogResultToDict(catalog)

	serialized := ""
	for key, value := range got["models"].([]map[string]any)[0] {
		serialized += key
		if text, ok := value.(string); ok {
			serialized += text
		}
	}
	require.NotContains(t, serialized, "secret-value", "目录输出不得含凭据")
	require.NotContains(t, serialized, "api_key")
}
