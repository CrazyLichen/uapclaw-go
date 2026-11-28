package personal_context

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegisterSourceLinks_登记与令牌化(t *testing.T) {
	markdown := "文档 [指南](./docs/guide.md) 与 [官网](https://example.com)"
	out, book, err := RegisterSourceLinks(markdown, "https://example.com/repo")
	require.NoError(t, err)
	require.Len(t, book, 2)
	// Python：render_link 存在时所有链接（含 web）都登记为令牌
	require.Contains(t, out, "pcs-source-link:")
	require.NotContains(t, out, "./docs/guide.md")
	require.NotContains(t, out, "https://example.com)")

	// 记录字段与身份稳定
	for identity, record := range book {
		require.Equal(t, identity, record.Identity())
		expectedSourceID, _ := SourceIDForLocator("https://example.com/repo")
		require.Equal(t, expectedSourceID, record.SourceID)
	}
}

func TestRegisterSourceLinks_同目标同身份(t *testing.T) {
	_, book1, _ := RegisterSourceLinks("[a](./x.md)", "https://e.com")
	_, book2, _ := RegisterSourceLinks("[a](./x.md)", "https://e.com")
	require.Len(t, book1, 1)
	require.Len(t, book2, 1)
	for id := range book1 {
		_, exists := book2[id]
		require.True(t, exists, "同 origin+target 应产生同身份")
	}
}

func TestSourceLinkPreview_保留标签(t *testing.T) {
	text := "前 [标签](pcs-source-link:0123456789abcdef0123456789abcdef) 后"
	// Python _LINK.sub(lambda m: m.group(1))：去方括号保标签
	require.Equal(t, "前 标签 后", SourceLinkPreview(text))
}

func TestCollectSourceLinkBook_往返校验(t *testing.T) {
	_, book, _ := RegisterSourceLinks("[a](./x.md) [b](https://e.com)", "https://e.com")
	docs := []map[string]any{{"source_links": map[string]any{}}}
	for identity, record := range book {
		docs[0]["source_links"].(map[string]any)[identity] = map[string]any{
			"source_id":       record.SourceID,
			"origin":          record.Origin,
			"original_target": record.OriginalTarget,
			"resolved_target": record.ResolvedTarget,
			"label":           record.Label,
		}
	}
	collected, err := CollectSourceLinkBook(docs)
	require.NoError(t, err)
	require.Equal(t, len(book), len(collected))

	// 身份篡改 → 拒绝
	for identity := range book {
		docs[0]["source_links"].(map[string]any)["deadbeefdeadbeefdeadbeefdeadbeef"] = docs[0]["source_links"].(map[string]any)[identity]
		_, err = CollectSourceLinkBook(docs)
		require.ErrorContains(t, err, "identity is invalid")
		break
	}
}

func TestResolveSourceLinks_发布期重写(t *testing.T) {
	root := t.TempDir()
	item := &RawChangeItem{
		LogicalID: "1", RevisionID: "r",
		Content: ptrString("c"), OriginalRef: "https://e.com/repo/guide.md",
		Metadata: map[string]any{},
	}
	sourceID, err := UpsertSourceMetadata(root, item, "github", "svc", "2026-11-28T00:00:00Z")
	require.NoError(t, err)

	// 采集：登记指向同一 origin 的相对目标（应命中 source-meta 里的 src_*.md）
	markdown := "见 [指南](./guide.md)"
	registered, book, err := RegisterSourceLinks(markdown, "https://e.com/repo/guide.md")
	require.NoError(t, err)

	// 发布树里放一篇带令牌的页
	contextRoot := filepath.Join(root, "sandbox")
	finalRoot := filepath.Join(root, "published")
	require.NoError(t, os.MkdirAll(contextRoot, 0o755))
	require.NoError(t, os.MkdirAll(finalRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextRoot, "page.md"), []byte(registered), 0o644))

	require.NoError(t, ResolveSourceLinks(contextRoot, finalRoot, root, book))
	out, _ := os.ReadFile(filepath.Join(contextRoot, "page.md"))
	require.NotContains(t, string(out), "pcs-source-link:")
	require.Contains(t, string(out), "指南")
	// 目标解析到 source-meta 文件 → 相对路径引用
	require.Contains(t, string(out), "](<../"+sourceID+".md>)")
}

func TestResolveSourceLinks_未登记令牌拒绝(t *testing.T) {
	root := t.TempDir()
	contextRoot := filepath.Join(root, "ctx")
	require.NoError(t, os.MkdirAll(contextRoot, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contextRoot, "p.md"),
		[]byte("[x](pcs-source-link:0123456789abcdef0123456789abcdef)"), 0o644))
	err := ResolveSourceLinks(contextRoot, root, root, SourceLinkBook{})
	require.ErrorContains(t, err, "unregistered source link")
}

func TestLocatorKey_分流(t *testing.T) {
	// Windows 盘符
	key, err := locatorKey(`C:\Docs\A.md`)
	require.NoError(t, err)
	require.Equal(t, `c:\docs\a.md`, key)
	// http 归一（凭据剥离）
	key, err = locatorKey("https://u:p@e.com/x")
	require.NoError(t, err)
	require.Equal(t, "https://e.com/x", key)
	// file scheme → 递归 posix
	key, err = locatorKey("file:///home/u/a.md")
	require.NoError(t, err)
	require.Equal(t, "/home/u/a.md", key)
	// 普通相对路径
	key, err = locatorKey("./docs/../x.md")
	require.NoError(t, err)
	require.Equal(t, "x.md", key)
}
