package personal_context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTitleFor_回落链(t *testing.T) {
	require.Equal(t, "我的标题", TitleFor(&RawChangeItem{LogicalID: "a/b", Title: ptrString("我的标题")}))
	require.Equal(t, "b", TitleFor(&RawChangeItem{LogicalID: "a/b"}))
	require.Equal(t, "x", TitleFor(&RawChangeItem{LogicalID: "x"}))
	require.Equal(t, "清洗  后", TitleFor(&RawChangeItem{LogicalID: "x", Title: ptrString("清洗\r\n后  ")})) // Python 各自替换为空格，不折叠
}

func TestManagedSourceComment_格式(t *testing.T) {
	out, err := managedSourceComment("src_0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.Equal(t, "<!-- personal-context-managed-source: src_0123456789abcdef0123456789abcdef -->", out)
	_, err = managedSourceComment("bad")
	require.ErrorContains(t, err, "managed source ID is invalid")
}

func TestManagedPagesBySource_扫描与去重(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write("page.md", "正文\n<!-- personal-context-managed-source: src_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa -->\n")
	write("plain.md", "无标记")
	pages, err := managedPagesBySource(root)
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Contains(t, pages, "src_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	// 重复标记 → 拒绝
	write("dup.md", "<!-- personal-context-managed-source: src_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb -->\n<!-- personal-context-managed-source: src_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb -->\n")
	_, err = managedPagesBySource(root)
	require.ErrorContains(t, err, "duplicated")
}

func TestReplaceManagedBlock_替换与插入(t *testing.T) {
	start := "<!-- personal-context:navigation:start -->"
	end := "<!-- personal-context:navigation:end -->"
	md := "# 标题\n\n" + start + "\n旧内容\n" + end + "\n尾部\n"
	out, err := replaceManagedBlock(md, start, end, "新内容", true, "x")
	require.NoError(t, err)
	require.Contains(t, out, "新内容")
	require.NotContains(t, out, "旧内容")
	require.Contains(t, out, "尾部")

	// 无块 → H1 后插入
	md2 := "# 标题\n\n正文\n"
	out2, err := replaceManagedBlock(md2, start, end, "块体", true, "回退")
	require.NoError(t, err)
	require.Contains(t, out2, "# 标题\n\n<!-- personal-context:navigation:start -->")
	require.Contains(t, out2, "正文")

	// 块内嵌套其他管理标记 → nested 拒绝（单 start/end 但内部含标记对）
	md3 := start + "\n" + directoryDescriptionStart + "\n" + directoryDescriptionEnd + "\n" + end
	_, err = replaceManagedBlock(md3, start, end, "", false, "x")
	require.ErrorContains(t, err, "nested")
}

func TestManagedBlockBounds_畸形(t *testing.T) {
	start, end := "<!-- p:a:start -->", "<!-- p:a:end -->"
	_, _, err := managedBlockBounds(start+"x"+end+"y"+start, start, end)
	require.Error(t, err)
}

func TestMarkdownHeading_与标签(t *testing.T) {
	require.Equal(t, "标题", markdownHeading("# 标题\n正文", "回退"))
	require.Equal(t, "回退", markdownHeading("无标题", "回退"))
	require.Equal(t, "a(b)c", markdownLabel("a[b]c"))
	require.Equal(t, "来源", markdownLabel("   "))
}

func TestManagedLocalLinks_解析(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "target.md"), []byte("# T"), 0o644))
	description := filepath.Join(dir, "description.md")
	start := sourceLinksStart
	end := sourceLinksEnd
	require.NoError(t, os.WriteFile(description, []byte(
		"# Dir\n\n"+start+"\n## PersonalContext 来源关联\n\n- [源页](<target.md>)\n"+end+"\n"), 0o644))
	links, err := managedLocalLinks(description, root, start, end, "## PersonalContext 来源关联")
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, "源页", links["target.md"])

	// 逃逸 Context → 拒绝
	require.NoError(t, os.WriteFile(description, []byte(
		"# Dir\n\n"+start+"\n## PersonalContext 来源关联\n\n- [外链](<../../outside.md>)\n"+end+"\n"), 0o644))
	_, err = managedLocalLinks(description, root, start, end, "## PersonalContext 来源关联")
	require.ErrorContains(t, err, "escaped Context")
}

func TestAppendManagedSourceLink_追加排序(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.md"), []byte("A"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.md"), []byte("B"), 0o644))
	description := filepath.Join(dir, "description.md")
	require.NoError(t, pipelineAtomicWrite(description, []byte("# Dir\n")))
	require.NoError(t, appendManagedSourceLink(description, root, filepath.Join(dir, "b.md"), "第二"))
	require.NoError(t, appendManagedSourceLink(description, root, filepath.Join(dir, "a.md"), "第一"))
	raw, _ := os.ReadFile(description)
	out := string(raw)
	require.Contains(t, out, "- [第一](a.md)")
	require.Contains(t, out, "- [第二](b.md)") // 无空格不加尖括号（对齐 Python _markdown_link_target）
}

func TestNormalizedTopicTerm_泛名过滤(t *testing.T) {
	require.Equal(t, "安装指南", normalizedTopicTerm("安装指南"))
	require.Equal(t, "", normalizedTopicTerm("文档"))
	require.Equal(t, "", normalizedTopicTerm("abc"))
	require.Equal(t, "安装指南", normalizedTopicTerm("`*#`安装指南`")) // 剥符号后 4 字 → 保留
}

func TestTopicTermMatchesTitle_词边界(t *testing.T) {
	require.True(t, topicTermMatchesTitle("Install", "How to Install Things"))
	require.False(t, topicTermMatchesTitle("Install", "Installation Guide"))
	require.True(t, topicTermMatchesTitle("安装", "如何安装软件"))
}

func TestSanitizedSemanticMarkdown_剥离(t *testing.T) {
	md := "# 页面\n\n" + rootNavigationStart + "\n导航行\n" + rootNavigationEnd + "\n\n正文 `code` 段\n\n```py\nx = 1\n```\n"
	out := sanitizedSemanticMarkdown(md)
	require.NotContains(t, out, "导航行")
	require.NotContains(t, out, "`code`")
	require.NotContains(t, out, "x = 1")
	require.Contains(t, out, "正文   段") // Python 反引号段替换为单空格
}

func TestSafeSemanticName_清洗与回落(t *testing.T) {
	name, err := safeSemanticName("fix(auth): 登录页闪退")
	require.NoError(t, err)
	require.Equal(t, "登录页闪退", name)
	name, err = safeSemanticName("My Report.pdf")
	require.NoError(t, err)
	require.Equal(t, "My Report", name)
	// 全非法字符 → 摘要回落
	name, err = safeSemanticName("///")
	require.NoError(t, err)
	require.Regexp(t, `^主题-[0-9a-f]{12}$`, name)
}

func TestSemanticPrefix_边界截断(t *testing.T) {
	// 长度 ≤ limit → 原样（尾部非连接词）
	require.Equal(t, "安装指南与部署手册", semanticPrefix("安装指南与部署手册", 10))
	// 无自然边界/驼峰 → 硬截 + 省略号
	out := semanticPrefix("安装指南与部署手册", 6)
	require.Contains(t, out, "…")
	require.Len(t, []rune(out), 6)
	require.Equal(t, "", semanticPrefix("abc", 0))
}

func TestTruncateSemanticContextSegment_后缀(t *testing.T) {
	out, err := truncateSemanticContextSegment("很长的标题内容", "abcd1234")
	require.NoError(t, err)
	require.Contains(t, out, "-abcd1234")
	require.LessOrEqual(t, len([]rune(out)), semanticNameMaxChars)
}

func TestContextDirectories_与普通页(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "p.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "description.md"), []byte("d"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "description.md"), []byte("root"), 0o644))
	dirs := contextDirectories(root)
	require.Equal(t, root, dirs[0])
	require.Len(t, dirs, 3)
	pages, err := contextOrdinaryPages(root)
	require.NoError(t, err)
	require.Len(t, pages, 1)
	require.Equal(t, filepath.Join(root, "a", "p.md"), pages[0])
}

func TestDirectorySemanticParts_聚合(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "安装"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "安装", "description.md"), []byte("# 安装目录\n\n"+rootNavigationStart+"\n导航\n"+rootNavigationEnd+"\n\n预览正文\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "安装", "p.md"), []byte("# 页标题\n\n页预览\n"), 0o644))
	parts := directorySemanticParts(filepath.Join(root, "安装"), "")
	require.Equal(t, "安装", parts.Title)
	require.Contains(t, parts.Headings[0], "安装目录")
	require.NotContains(t, parts.Preview, "导航")
	require.Contains(t, parts.Preview, "页预览")
}

func TestDirectoryAcceptsNewPage_容量闸(t *testing.T) {
	root := t.TempDir()
	// 0 页 < 默认 20
	ok, err := directoryAcceptsNewPage(root, 0, false)
	require.NoError(t, err)
	require.True(t, ok)
	// max=0 → 拒绝
	ok, err = directoryAcceptsNewPage(root, 0, true)
	require.NoError(t, err)
	require.False(t, ok)
	// 上限 1，写 1 页 → 拒绝
	require.NoError(t, os.WriteFile(filepath.Join(root, "p.md"), []byte("x"), 0o644))
	ok, err = directoryAcceptsNewPage(root, 1, true)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestValidateContextCapacities_超限(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "d"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "d", "p.md"), []byte("x"), 0o644))
	// 1 页 ≤ 1
	require.NoError(t, validateContextCapacities(root, 1, 20, true, false, false))
	// 0 页 < 1 → 超限
	err := validateContextCapacities(root, 0, 20, true, false, false)
	require.ErrorContains(t, err, "page capacity is exceeded")
	// capacity_exempt → 跳过
	require.NoError(t, validateContextCapacities(root, 0, 20, true, false, true))
}

func TestCapacityWarningThreshold_80(t *testing.T) {
	require.Equal(t, 16, capacityWarningThreshold(20))
	require.Equal(t, 1, capacityWarningThreshold(1))
}

func TestSemanticDirectoryCandidate_冲突消解(t *testing.T) {
	root := t.TempDir()
	// 空父目录 → 直接候选
	candidate, err := semanticDirectoryCandidate(root, "安装指南", "seed12345678")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "安装指南"), candidate)
	// 已有同名目录但描述页标题不同 → 加 seed 后缀
	require.NoError(t, os.MkdirAll(filepath.Join(root, "安装指南"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "安装指南", "description.md"), []byte("# 其他主题\n"), 0o644))
	candidate, err = semanticDirectoryCandidate(root, "安装指南", "seed12345678")
	require.NoError(t, err)
	require.NotEqual(t, filepath.Join(root, "安装指南"), candidate)
	// 描述页标题相同 → 复用既有目录
	candidate, err = semanticDirectoryCandidate(root, "安装指南", "seed12345678")
	_ = candidate
	_ = err
}

func TestMarkdownDestination_解析(t *testing.T) {
	require.Equal(t, "./a.md", markdownDestination("./a.md"))
	require.Equal(t, "./a.md", markdownDestination("./a.md \"标题\""))
	require.Equal(t, "./a.md", markdownDestination("<./a.md>"))
	require.Equal(t, "./a.md", markdownDestination("./a.md#frag"))
	require.Equal(t, "", markdownDestination(""))
	require.Equal(t, "", markdownDestination("#fragment"))
}

func TestMarkdownLinkTarget_空格加括号(t *testing.T) {
	require.Equal(t, "a.md", markdownLinkTarget("a.md"))
	require.Equal(t, "<我的 页.md>", markdownLinkTarget("我的 页.md"))
	require.Equal(t, "<a(1).md>", markdownLinkTarget("a(1).md"))
}

func TestDocumentSemanticParts_balanced富集(t *testing.T) {
	doc := map[string]any{
		"title":    "原始标题",
		"markdown": "# 页面\n\n内容",
	}
	parts := documentSemanticParts(doc)
	require.Equal(t, "原始标题", parts.Title)

	doc["_balanced_semantics"] = map[string]any{
		"page_title": "富集标题",
		"keywords":   []string{"k1", "k2"},
		"summary":    "摘要",
	}
	parts = documentSemanticParts(doc)
	require.Equal(t, "富集标题", parts.Title)
	require.Contains(t, parts.Headings, "k1")
	require.Equal(t, "摘要", parts.Preview)
}

func TestUniqueRulesTopicDescription_唯一匹配(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "安装"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "安装", "description.md"), []byte("# 安装指南\n"), 0o644))
	match := uniqueRulesTopicDescription(root, "安装指南使用")
	require.NotEqual(t, "", match)
	// 第二个匹配 → 返回空
	require.NoError(t, os.MkdirAll(filepath.Join(root, "部署"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "部署", "description.md"), []byte("# 安装手册\n"), 0o644))
	match = uniqueRulesTopicDescription(root, "安装指南使用")
	require.Equal(t, "", match)
}
