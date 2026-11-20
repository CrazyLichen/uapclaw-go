// desc 资产防回归（e15 审查批，2026-12-02 用户要求）：防止工具描述再丢失。
//
// 三道防线：
// ①索引完整性——descs 树全部非 fragments md 必须入索引（新增文件自动纳入，
//   路径错/语言根错即失败）；
// ②全量渲染非空——每个工具描述渲染结果非空且无 {{ 残留（任何一个工具的
//   描述空串/装配残缺都在本测试暴露，不再依赖逐工具手动断言）；
// ③fragments 引用闭合——全部 md 引用的 slot 必须在 fragments/ 有对应文件
//   （模板引用了不存在的片段 = 装配 bug）。

package locales

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
)

// TestDescIndex_覆盖全部工具md 索引条目数 == descs/{lang} 下非 fragments 的 md 数。
func TestDescIndex_覆盖全部工具md(t *testing.T) {
	for _, lang := range []string{"cn", "en"} {
		index := descIndexOf(lang)
		// 平铺键去重计数（同名 md 跨子目录冲突时后写覆盖——Python _desc_index
		// 同款行为；现知 case：cn/en 根级与 workspace/ 子级各有 workspace_meta.md）。
		expected := map[string]bool{}
		require.NoError(t, fs.WalkDir(descFS, "descs/"+lang, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
				return err
			}
			if strings.Contains(strings.TrimPrefix(p, "descs/"+lang+"/"), fragmentsDirname+"/") {
				return nil
			}
			key := strings.TrimSuffix(path.Base(p), ".md")
			expected[key] = true
			return nil
		}))
		require.Len(t, index, len(expected),
			"descs/%s 索引不完整：期望 %d 个工具描述，实际 %d——新增 md 未入索引或路径规则漂移", lang, len(expected), len(index))
	}
}

// TestRenderDesc_全量工具描述非空无残留 全部工具描述渲染非空且无 {{ 残留。
func TestRenderDesc_全量工具描述非空无残留(t *testing.T) {
	index := descIndexOf("cn")
	require.NotEmpty(t, index)
	empty := []string{}
	residual := []string{}
	for tool := range index {
		rendered := RenderDesc(tool, atschema.LanguageCN, map[string]bool{})
		if rendered == "" {
			empty = append(empty, tool)
			continue
		}
		if strings.Contains(rendered, "{{") {
			residual = append(residual, tool)
		}
	}
	require.Empty(t, empty, "以下工具描述渲染为空（md 缺失或渲染失败）：%v", empty)
	require.Empty(t, residual, "以下工具描述残留 {{ 占位符（slot 填充失败）：%v", residual)
	// en 语言根同验
	indexEn := descIndexOf("en")
	emptyEn := []string{}
	for tool := range indexEn {
		if RenderDesc(tool, atschema.LanguageEN, map[string]bool{}) == "" {
			emptyEn = append(emptyEn, tool)
		}
	}
	require.Empty(t, emptyEn, "en 语言根以下工具描述渲染为空：%v", emptyEn)
}

// TestFragments_引用闭合 全部 desc 引用的 slot 必须有对应 fragment 文件。
func TestFragments_引用闭合(t *testing.T) {
	missing := []string{}
	for _, lang := range []string{"cn", "en"} {
		for tool := range descIndexOf(lang) {
			for _, slot := range SlotsOf(loadToolDesc(tool, lang)) {
				if loadFragment(slot, lang) == "" {
					missing = append(missing, lang+"/"+tool+" -> fragments/"+slot+".md")
				}
			}
		}
	}
	require.Empty(t, missing, "以下 slot 引用无对应 fragment 文件：%v", missing)
}

// TestRenderDesc_语言回退 en 缺失文件回退 cn（loadFragment 与 loadToolDesc 同规）。
func TestRenderDesc_语言回退(t *testing.T) {
	// cn 全量已在上一用例验证；此处验证 en 根索引与 cn 根键集一致
	// （两侧文件布局镜像，缺失即某侧文案丢失）。
	cnKeys := map[string]bool{}
	for k := range descIndexOf("cn") {
		cnKeys[k] = true
	}
	enKeys := map[string]bool{}
	for k := range descIndexOf("en") {
		enKeys[k] = true
	}
	diffCn := []string{}
	for k := range cnKeys {
		if !enKeys[k] {
			diffCn = append(diffCn, k)
		}
	}
	diffEn := []string{}
	for k := range enKeys {
		if !cnKeys[k] {
			diffEn = append(diffEn, k)
		}
	}
	require.Empty(t, diffCn, "cn 有而 en 缺的描述：%v", diffCn)
	require.Empty(t, diffEn, "en 有而 cn 缺的描述：%v", diffEn)
}
