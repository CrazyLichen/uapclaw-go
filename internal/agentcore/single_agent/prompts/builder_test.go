//go:build test

package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSupportedLanguages 验证支持的语言列表
func TestSupportedLanguages(t *testing.T) {
	assert.Equal(t, [2]string{"cn", "en"}, SupportedLanguages)
}

// TestDefaultLanguage 验证默认语言
func TestDefaultLanguage(t *testing.T) {
	assert.Equal(t, "cn", DefaultLanguage)
}

// TestNewSystemPromptBuilder 验证系统提示词构建器构造
func TestNewSystemPromptBuilder(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.NotNil(t, builder)
	assert.Equal(t, "cn", builder.Language())
	assert.NotNil(t, builder.sections)
	assert.Nil(t, builder.sectionsFilter)
}

// TestNewSystemPromptBuilderWithFilter 验证带过滤函数的构建器构造
func TestNewSystemPromptBuilderWithFilter(t *testing.T) {
	filter := func(sections []PromptSection) []PromptSection {
		return sections
	}
	builder := NewSystemPromptBuilderWithFilter("en", filter)
	assert.Equal(t, "en", builder.Language())
	assert.NotNil(t, builder.sectionsFilter)
}

// TestNewPromptSection 验证提示节构造
func TestNewPromptSection(t *testing.T) {
	section := NewPromptSection("test", map[string]string{"cn": "测试"}, 10)
	assert.Equal(t, "test", section.Name)
	assert.Equal(t, 10, section.Priority)
	assert.Equal(t, "测试", section.Content["cn"])
}

// TestPromptSection_Render 验证提示节渲染
func TestPromptSection_Render(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}

	// 匹配指定语言
	assert.Equal(t, "中文", section.Render("cn"))
	assert.Equal(t, "English", section.Render("en"))
}

// TestPromptSection_Render_回退到默认语言 验证无精确匹配时回退到 DefaultLanguage
func TestPromptSection_Render_回退到默认语言(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}
	// fr 不存在，回退到 "cn" (DefaultLanguage)
	assert.Equal(t, "中文", section.Render("fr"))
}

// TestPromptSection_Render_回退到首个值 验证无默认语言时回退到 map 中首个值
func TestPromptSection_Render_回退到首个值(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"en": "English"},
		Priority: 10,
	}
	// fr 不存在，"cn" (DefaultLanguage) 也不存在，回退到 map 中首个值
	assert.Equal(t, "English", section.Render("fr"))
}

// TestPromptSection_Render_空内容 验证空 Content 时返回空字符串
func TestPromptSection_Render_空内容(t *testing.T) {
	section := PromptSection{Name: "empty", Content: map[string]string{}}
	assert.Equal(t, "", section.Render("cn"))
}

// TestPromptSection_CharCount 验证字符数计算
func TestPromptSection_CharCount(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文内容"},
		Priority: 10,
	}
	assert.Equal(t, 4, section.CharCount("cn"))
}

// TestPromptSection_CharCount_多语言 验证不同语言字符数不同
func TestPromptSection_CharCount_多语言(t *testing.T) {
	section := PromptSection{
		Name:     "test",
		Content:  map[string]string{"cn": "中文", "en": "English"},
		Priority: 10,
	}
	assert.Equal(t, 2, section.CharCount("cn"))
	assert.Equal(t, 7, section.CharCount("en"))
}

// TestSystemPromptBuilder_AddSection 验证添加节
func TestSystemPromptBuilder_AddSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 10})

	assert.True(t, builder.HasSection("a"))
	assert.True(t, builder.HasSection("b"))
}

// TestSystemPromptBuilder_AddSection_同名称覆盖 验证同名称节覆盖
func TestSystemPromptBuilder_AddSection_同名称覆盖(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "BBB"}, Priority: 20})
	assert.Equal(t, "BBB", builder.Build())
}

// TestSystemPromptBuilder_AddSection_链式调用 验证 AddSection 返回自身支持链式调用
func TestSystemPromptBuilder_AddSection_链式调用(t *testing.T) {
	builder := NewSystemPromptBuilder()
	result := builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	assert.Equal(t, builder, result)
}

// TestSystemPromptBuilder_RemoveSection 验证移除节
func TestSystemPromptBuilder_RemoveSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	assert.True(t, builder.HasSection("a"))

	builder.RemoveSection("a")
	assert.False(t, builder.HasSection("a"))
}

// TestSystemPromptBuilder_RemoveSection_不存在时不报错 验证移除不存在的节不 panic
func TestSystemPromptBuilder_RemoveSection_不存在时不报错(t *testing.T) {
	builder := NewSystemPromptBuilder()
	result := builder.RemoveSection("nonexistent")
	assert.Equal(t, builder, result)
}

// TestSystemPromptBuilder_GetAllSections 验证返回所有节的副本
func TestSystemPromptBuilder_GetAllSections(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 20})

	sections := builder.GetAllSections()
	assert.Equal(t, 2, len(sections))
	assert.Contains(t, sections, "a")
	assert.Contains(t, sections, "b")
}

// TestSystemPromptBuilder_GetAllSections_副本隔离 验证修改返回值不影响原 builder
func TestSystemPromptBuilder_GetAllSections_副本隔离(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})

	sections := builder.GetAllSections()
	sections["a"] = PromptSection{Name: "a", Content: map[string]string{"cn": "MODIFIED"}, Priority: 99}

	// 原 builder 不受影响
	original := builder.GetAllSections()
	assert.Equal(t, 10, original["a"].Priority)
	assert.Equal(t, "AAA", original["a"].Content["cn"])
}

// TestSystemPromptBuilder_GetSection 验证按名称获取节
func TestSystemPromptBuilder_GetSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})

	section := builder.GetSection("a")
	assert.NotNil(t, section)
	assert.Equal(t, "a", section.Name)
	assert.Equal(t, "AAA", section.Content["cn"])
}

// TestSystemPromptBuilder_GetSection_不存在 验证不存在返回 nil
func TestSystemPromptBuilder_GetSection_不存在(t *testing.T) {
	builder := NewSystemPromptBuilder()
	section := builder.GetSection("nonexistent")
	assert.Nil(t, section)
}

// TestSystemPromptBuilder_HasSection 验证节存在检查
func TestSystemPromptBuilder_HasSection(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})

	assert.True(t, builder.HasSection("a"))
	assert.False(t, builder.HasSection("nonexistent"))
}

// TestSystemPromptBuilder_Build 验证构建结果按优先级排序
func TestSystemPromptBuilder_Build(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "low", Content: map[string]string{"cn": "LOW"}, Priority: 30})
	builder.AddSection(PromptSection{Name: "mid", Content: map[string]string{"cn": "MID"}, Priority: 20})
	builder.AddSection(PromptSection{Name: "high", Content: map[string]string{"cn": "HIGH"}, Priority: 10})

	result := builder.Build()
	assert.Equal(t, "HIGH\n\nMID\n\nLOW", result)
}

// TestSystemPromptBuilder_Build_跳过空内容 验证空内容节被跳过
func TestSystemPromptBuilder_Build_跳过空内容(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "empty", Content: map[string]string{}, Priority: 10})
	builder.AddSection(PromptSection{Name: "nonempty", Content: map[string]string{"cn": "VALUE"}, Priority: 20})
	assert.Equal(t, "VALUE", builder.Build())
}

// TestSystemPromptBuilder_Build_无节时返回空字符串 验证空构建器返回空字符串
func TestSystemPromptBuilder_Build_无节时返回空字符串(t *testing.T) {
	builder := NewSystemPromptBuilder()
	assert.Equal(t, "", builder.Build())
}

// TestSystemPromptBuilder_Build_单节 验证单节构建不添加换行
func TestSystemPromptBuilder_Build_单节(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "only", Content: map[string]string{"cn": "ONLY"}, Priority: 10})
	assert.Equal(t, "ONLY", builder.Build())
}

// TestSystemPromptBuilder_Build_过滤钩子 验证 sectionsFilter 过滤节
func TestSystemPromptBuilder_Build_过滤钩子(t *testing.T) {
	filter := func(sections []PromptSection) []PromptSection {
		result := make([]PromptSection, 0)
		for _, s := range sections {
			if s.Name == "a" {
				result = append(result, s)
			}
		}
		return result
	}
	builder := NewSystemPromptBuilderWithFilter("cn", filter)
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 20})

	result := builder.Build()
	assert.Equal(t, "AAA", result)
}

// TestSystemPromptBuilder_Build_过滤钩子为nil 验证 sectionsFilter 为 nil 时不过滤
func TestSystemPromptBuilder_Build_过滤钩子为nil(t *testing.T) {
	builder := NewSystemPromptBuilder()
	builder.AddSection(PromptSection{Name: "a", Content: map[string]string{"cn": "AAA"}, Priority: 10})
	builder.AddSection(PromptSection{Name: "b", Content: map[string]string{"cn": "BBB"}, Priority: 20})

	result := builder.Build()
	assert.Equal(t, "AAA\n\nBBB", result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
