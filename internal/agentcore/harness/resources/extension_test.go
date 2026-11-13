package resources

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// 本文件覆盖 15.5 新增的 manifest 加载与 persona 渲染。
// 完整实现（资源装配、plugin 覆盖）属 15.14 / 15.15a。

// writeManifest 在 dir 下写一个 manifest.json。
func writeManifest(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "manifest.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// validManifest 一个通过全部必填校验的最小清单。
const validManifest = `{
	"package_type": "agent_template",
	"name": "expert",
	"description": "领域专家",
	"agent_card": {"id": "a1", "name": "expert", "description": "领域专家"}
}`

// ──────────────────────────── FindAgentTemplateManifest 测试分组 ────────────────────────────

// TestFindAgentTemplateManifest_传目录 返回目录下的 manifest.json 绝对路径。
func TestFindAgentTemplateManifest_传目录(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, validManifest)

	got, err := FindAgentTemplateManifest(dir)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got))
	assert.Equal(t, filepath.Join(dir, "manifest.json"), got)
}

// TestFindAgentTemplateManifest_传文件 原样返回。
func TestFindAgentTemplateManifest_传文件(t *testing.T) {
	dir := t.TempDir()
	manifestPath := writeManifest(t, dir, validManifest)

	got, err := FindAgentTemplateManifest(manifestPath)
	require.NoError(t, err)
	assert.Equal(t, manifestPath, got)
}

// TestFindAgentTemplateManifest_目录无清单 报错（Python: FileNotFoundError）。
func TestFindAgentTemplateManifest_目录无清单(t *testing.T) {
	_, err := FindAgentTemplateManifest(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.json not found")
}

// TestFindAgentTemplateManifest_文件名不符 报错（无 YAML 回退）。
func TestFindAgentTemplateManifest_文件名不符(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "manifest.yaml")
	require.NoError(t, os.WriteFile(other, []byte("x"), 0o600))

	_, err := FindAgentTemplateManifest(other)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.json not found")
}

// ──────────────────────────── LoadAgentTemplatePackage 测试分组 ────────────────────────────

// TestLoadAgentTemplatePackage_正常读取 完整字段解析。
func TestLoadAgentTemplatePackage_正常读取(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, validManifest)

	spec, err := LoadAgentTemplatePackage(path)
	require.NoError(t, err)
	assert.Equal(t, "a1", spec.AgentCard.ID)
	assert.Equal(t, "expert", spec.AgentCard.Name)
	assert.Equal(t, "领域专家", spec.AgentCard.Description)
}

// TestLoadAgentTemplatePackage_扁平字段归一化 无 agent_card 时用顶层 name/description。
func TestLoadAgentTemplatePackage_扁平字段归一化(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{
		"package_type": "agent_template", "name": "flat", "description": "扁平声明"
	}`)

	spec, err := LoadAgentTemplatePackage(path)
	require.NoError(t, err)
	assert.Equal(t, "flat", spec.AgentCard.Name)
	assert.Equal(t, "扁平声明", spec.AgentCard.Description)
}

// TestLoadAgentTemplatePackage_packageType不符 对齐 extension_loader.py:204-207。
func TestLoadAgentTemplatePackage_packageType不符(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{"package_type":"plugin","name":"x","description":"y"}`)

	_, err := LoadAgentTemplatePackage(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 'agent_template'")
}

// TestLoadAgentTemplatePackage_缺name 对齐 extension_loader.py:209-211。
func TestLoadAgentTemplatePackage_缺name(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{"package_type":"agent_template","description":"y"}`)

	_, err := LoadAgentTemplatePackage(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required non-empty 'name'")
}

// TestLoadAgentTemplatePackage_缺description 对齐 extension_loader.py:212-214。
func TestLoadAgentTemplatePackage_缺description(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{"package_type":"agent_template","name":"x"}`)

	_, err := LoadAgentTemplatePackage(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required non-empty 'description'")
}

// TestLoadAgentTemplatePackage_文件名不符 对齐 extension_loader.py:200-201。
func TestLoadAgentTemplatePackage_文件名不符(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "other.json")
	require.NoError(t, os.WriteFile(other, []byte(validManifest), 0o600))

	_, err := LoadAgentTemplatePackage(other)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `must be named "manifest.json"`)
}

// TestLoadAgentTemplatePackage_非法JSON 报解析错误。
func TestLoadAgentTemplatePackage_非法JSON(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `{not json`)

	_, err := LoadAgentTemplatePackage(path)
	require.Error(t, err)
}

// ──────────────────────────── RenderAgentTemplateSystemPrompt 测试分组 ────────────────────────────

// TestRenderAgentTemplateSystemPrompt_按priority排序 对齐
// extension_resolver.py:254 sorted(spec.prompt_sections, key=priority)。
func TestRenderAgentTemplateSystemPrompt_按priority排序(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{
			{Name: "safety", Content: map[string]string{"cn": "安全"}, Priority: 30},
			{Name: "identity", Content: map[string]string{"cn": "身份"}, Priority: 10},
			{Name: "sops", Content: map[string]string{"cn": "流程"}, Priority: 20},
		},
	}
	got := RenderAgentTemplateSystemPrompt(spec, "cn")
	assert.Equal(t, "身份\n\n流程\n\n安全", got)
}

// TestRenderAgentTemplateSystemPrompt_空段落跳过 空正文不产生多余空行。
func TestRenderAgentTemplateSystemPrompt_空段落跳过(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{
			{Name: "a", Content: map[string]string{"cn": "内容"}, Priority: 1},
			{Name: "b", Content: nil, Priority: 2},
			{Name: "c", Content: map[string]string{"cn": ""}, Priority: 3},
		},
	}
	assert.Equal(t, "内容", RenderAgentTemplateSystemPrompt(spec, "cn"))
}

// TestRenderAgentTemplateSystemPrompt_无段落 返回空串。
func TestRenderAgentTemplateSystemPrompt_无段落(t *testing.T) {
	assert.Equal(t, "", RenderAgentTemplateSystemPrompt(hschema.AgentTemplateSpec{}, "cn"))
}

// TestRenderAgentTemplateSystemPrompt_语言回退 对齐
// extension_resolver.py:275-285 四级回退。
func TestRenderAgentTemplateSystemPrompt_语言回退(t *testing.T) {
	content := map[string]string{"en": "English", "cn": "中文"}
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{{Content: content, Priority: 1}},
	}

	// 命中指定语言
	assert.Equal(t, "中文", RenderAgentTemplateSystemPrompt(spec, "cn"))
	assert.Equal(t, "English", RenderAgentTemplateSystemPrompt(spec, "en"))
	// 未命中 → en
	assert.Equal(t, "English", RenderAgentTemplateSystemPrompt(spec, "fr"))
}

// TestRenderAgentTemplateSystemPrompt_语言回退cn 无 en 时回退 cn。
func TestRenderAgentTemplateSystemPrompt_语言回退cn(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{{
			Content: map[string]string{"cn": "中文"}, Priority: 1,
		}},
	}
	assert.Equal(t, "中文", RenderAgentTemplateSystemPrompt(spec, "fr"))
}

// TestRenderAgentTemplateSystemPrompt_占位符替换 对齐
// extension_resolver.py:296-303 _render_template。
func TestRenderAgentTemplateSystemPrompt_占位符替换(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{{
			Content:      map[string]string{"cn": "工作区在 {{ workspace }}，语言 {{language}}"},
			Priority:     1,
			RenderParams: map[string]any{"workspace": "/tmp/ws"},
		}},
	}
	got := RenderAgentTemplateSystemPrompt(spec, "cn")
	// workspace 来自 render_params；language 来自派生的 ctx
	assert.Equal(t, "工作区在 /tmp/ws，语言 cn", got)
}

// TestRenderAgentTemplateSystemPrompt_未知占位符原样保留 对齐
// extension_resolver.py:299-300 `if key not in params: return match`。
func TestRenderAgentTemplateSystemPrompt_未知占位符原样保留(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{{
			Content:  map[string]string{"cn": "保留 {{ unknown }} 原文"},
			Priority: 1,
		}},
	}
	assert.Equal(t, "保留 {{ unknown }} 原文", RenderAgentTemplateSystemPrompt(spec, "cn"))
}

// TestRenderPromptContent_直接渲染单段 暴露给持有正文而非整模板的调用方。
func TestRenderPromptContent_直接渲染单段(t *testing.T) {
	content := map[string]string{"cn": "你好 {{ who }}", "en": "Hello {{ who }}"}
	assert.Equal(t, "你好 世界", RenderPromptContent(content, "cn", map[string]any{"who": "世界"}))
	assert.Equal(t, "Hello world", RenderPromptContent(content, "en", map[string]any{"who": "world"}))
}

// TestRenderAgentTemplateSystemPrompt_空语言回退cn 对齐
// extension_resolver.py:289 `ctx.language or "cn"`。
func TestRenderAgentTemplateSystemPrompt_空语言回退cn(t *testing.T) {
	spec := hschema.AgentTemplateSpec{
		PromptSections: []hschema.PromptSectionSpec{{
			Content:  map[string]string{"cn": "语言是 {{ language }}"},
			Priority: 1,
		}},
	}
	assert.Equal(t, "语言是 cn", RenderAgentTemplateSystemPrompt(spec, ""))
}

// TestSelectContent_兜底取任一语言 三个常见语言都缺时取字典序最小的键。
// Python 用 next(iter(content.values())) 取插入序第一个；Go 的 map 无序，
// 取字典序最小以保证输出确定。
func TestSelectContent_兜底取任一语言(t *testing.T) {
	content := map[string]string{"ja": "日本語", "de": "Deutsch"}
	assert.Equal(t, "Deutsch", RenderPromptContent(content, "fr", nil))
}

// TestSelectContent_空map 返回空串。
func TestSelectContent_空map(t *testing.T) {
	assert.Equal(t, "", RenderPromptContent(nil, "cn", nil))
	assert.Equal(t, "", RenderPromptContent(map[string]string{}, "cn", nil))
}

// TestRenderTemplate_非字符串参数走字符串化 对齐 Python 的 str(params[key])。
func TestRenderTemplate_非字符串参数走字符串化(t *testing.T) {
	got := RenderPromptContent(
		map[string]string{"cn": "数量 {{ n }} 比例 {{ r }}"},
		"cn",
		map[string]any{"n": 42, "r": 0.5},
	)
	assert.Equal(t, "数量 42 比例 0.5", got)
}

// TestRenderTemplate_空文本 空正文不 panic。
func TestRenderTemplate_空文本(t *testing.T) {
	assert.Equal(t, "", RenderPromptContent(map[string]string{"cn": ""}, "cn", nil))
}

// TestExpandUserPath_波浪号展开 前导 ~ 展开成主目录。
func TestExpandUserPath_波浪号展开(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	got, err := expandUserPath("~")
	require.NoError(t, err)
	assert.Equal(t, home, got)

	got, err = expandUserPath("~/sub/dir")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "sub/dir"), got)

	// 非 ~ 开头原样返回
	got, err = expandUserPath("/abs/path")
	require.NoError(t, err)
	assert.Equal(t, "/abs/path", got)

	// ~user 形式不展开（与 Python expanduser 的宽松语义对齐）
	got, err = expandUserPath("~otheruser/x")
	require.NoError(t, err)
	assert.Equal(t, "~otheruser/x", got)
}

// TestLoadAgentTemplatePackage_波浪号路径 通过 ~ 定位清单。
func TestLoadAgentTemplatePackage_波浪号路径(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	dir := t.TempDir()
	writeManifest(t, dir, validManifest)
	if filepath.Dir(dir) != home {
		t.Skip("临时目录不在主目录下，跳过 ~ 路径用例")
	}
	rel, relErr := filepath.Rel(home, dir)
	require.NoError(t, relErr)
	_, err = LoadAgentTemplatePackage("~/" + rel + "/manifest.json")
	require.NoError(t, err)
}
