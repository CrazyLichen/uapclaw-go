package personal_context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// 造一棵已发布 Context 树：root description.md + 两目录 + 页面 + 内链。
func buildGraphFixture(t *testing.T) string {
	root := t.TempDir()
	write := func(rel string, content string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write("description.md", "# PersonalContext\n\n- [安装](安装/description.md)\n- [知识库](知识库/description.md)\n")
	write("安装/description.md", "# 安装\n\n- [Install](install.md)\n- [FAQ](faq.md)\n")
	write("安装/install.md", "# Install\n\npip install\n\n另见 [FAQ](faq.md) 与 [搜索](../知识库/search.md)\n")
	write("安装/faq.md", "# FAQ\n\n常见问题\n")
	write("知识库/description.md", "# 知识库\n\n- [Search](search.md)\n")
	write("知识库/search.md", "# Search\n\nBM25F 检索算法\n")
	return root
}

func TestBuildContextGraph_节点与双边(t *testing.T) {
	root := buildGraphFixture(t)
	slice, err := BuildContextGraph(root, "", 3)
	require.NoError(t, err)
	require.True(t, slice.ContextReady)

	byID := map[string]GraphNode{}
	for _, node := range slice.Nodes {
		byID[node.ID] = node
	}
	require.Equal(t, "directory", byID["page:description.md"].Kind)
	require.Equal(t, "document", byID["page:安装/install.md"].Kind)
	require.Equal(t, "directory.1", byID["page:安装/description.md"].Subkind)
	require.True(t, byID["page:安装/description.md"].HasChildren)

	kinds := map[string]int{}
	for _, edge := range slice.Edges {
		kinds[edge.Kind]++
	}
	require.Greater(t, kinds["contains"], 0)
	// references：install→faq（同目录）+ install→search（跨目录）
	require.Equal(t, 2, kinds["references"])
}

func TestBuildContextTree_无references边(t *testing.T) {
	root := buildGraphFixture(t)
	slice, err := BuildContextTree(root, "", 3)
	require.NoError(t, err)
	for _, edge := range slice.Edges {
		require.Equal(t, "contains", edge.Kind)
	}
}

func TestBuildContextGraph_depth裁剪(t *testing.T) {
	root := buildGraphFixture(t)
	slice, err := BuildContextGraph(root, "", 1)
	require.NoError(t, err)
	for _, node := range slice.Nodes {
		require.NotEqual(t, "page:安装/install.md", node.ID, "depth=1 不应包含第三层")
	}
}

func TestBuildContextGraph_空态与根校验(t *testing.T) {
	empty := t.TempDir()
	slice, err := BuildContextGraph(empty, "", 3)
	require.NoError(t, err)
	require.False(t, slice.ContextReady)
	require.Empty(t, slice.Nodes)

	// 非法 depth
	_, err = BuildContextGraph(empty, "", 11)
	require.ErrorContains(t, err, "depth must be between 1 and 10")
}

func TestReadContextGraphPage_读取(t *testing.T) {
	root := buildGraphFixture(t)
	page, err := ReadContextGraphPage(root, "page:安装/install.md")
	require.NoError(t, err)
	require.Equal(t, "Install", page.Title)
	require.Contains(t, page.Markdown, "pip install")

	_, err = ReadContextGraphPage(root, "page:不存在.md")
	require.Error(t, err)
	_, err = ReadContextGraphPage(root, "page:../escape.md")
	require.ErrorContains(t, err, "node ID is invalid")
}

func TestSearchContextGraph_BM25F与CJK(t *testing.T) {
	root := buildGraphFixture(t)
	results, err := SearchContextGraph(root, "检索")
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "page:知识库/search.md", results[0].NodeID)
	require.Contains(t, results[0].Snippet, "检索")

	// 标题字段权重更高：query=Install 应命中 install 页
	results, err = SearchContextGraph(root, "Install")
	require.NoError(t, err)
	require.Equal(t, "page:安装/install.md", results[0].NodeID)

	// 无命中
	results, err = SearchContextGraph(root, "zzz-not-exist")
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestTokenize_CJK单字与二元组(t *testing.T) {
	tokens := tokenize("检索")
	require.Equal(t, []string{"检", "检索", "索"}, tokens)
	tokens = tokenize("abc_1")
	require.Equal(t, []string{"abc_1"}, tokens)
}

func TestGraphTarget_链接解析边界(t *testing.T) {
	pageIDs := map[string]string{"docs/a.md": "page:docs/a.md"}
	require.Equal(t, "page:docs/a.md",
		graphTarget("/ctx", "/ctx/docs/b.md", "a.md", pageIDs))
	require.Equal(t, "page:docs/a.md",
		graphTarget("/ctx", "/ctx/docs/b.md", "<a.md>", pageIDs))
	require.Equal(t, "", graphTarget("/ctx", "/ctx/docs/b.md", "https://e.com/x.md", pageIDs))
	require.Equal(t, "", graphTarget("/ctx", "/ctx/docs/b.md", "/abs/a.md", pageIDs))
	require.Equal(t, "", graphTarget("/ctx", "/ctx/docs/b.md", "a.txt", pageIDs))
}
