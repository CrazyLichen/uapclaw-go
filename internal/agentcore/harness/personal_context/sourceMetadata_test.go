package personal_context

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func ptrString(s string) *string { return &s }

func TestNormalizeSourceLocator_凭据剥离(t *testing.T) {
	out, err := NormalizeSourceLocator("https://user:token@example.com/repo/x?a=1")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/repo/x", out)

	// 非 http(s) 原样保留
	out, err = NormalizeSourceLocator("./local/file.md")
	require.NoError(t, err)
	require.Equal(t, "./local/file.md", out)

	// IPv6 主机
	out, err = NormalizeSourceLocator("https://user:pw@[::1]:8443/p")
	require.NoError(t, err)
	require.Equal(t, "https://[::1]:8443/p", out)
}

func TestSourceIDForLocator_稳定摘要(t *testing.T) {
	id, err := SourceIDForLocator("https://example.com/repo")
	require.NoError(t, err)
	require.Regexp(t, `^src_[0-9a-f]{32}$`, id)
	sum := sha256.Sum256([]byte("https://example.com/repo"))
	require.Equal(t, "src_"+hex.EncodeToString(sum[:])[:32], id)

	// 同输入稳定
	id2, _ := SourceIDForLocator("https://example.com/repo")
	require.Equal(t, id, id2)
}

func TestSourceItemVersion_哈希优先级(t *testing.T) {
	snapshot := "raw-bytes"
	item := &RawChangeItem{
		LogicalID:   "x",
		RevisionID:  "rev-1",
		Content:     ptrString("content"),
		RawSnapshot: []byte(snapshot),
	}
	revision, hash, err := SourceItemVersion(item)
	require.NoError(t, err)
	require.Equal(t, "rev-1", revision)
	sum := sha256.Sum256([]byte(snapshot))
	require.Equal(t, hex.EncodeToString(sum[:]), hash)

	// 无快照 → content
	item.RawSnapshot = nil
	_, hash2, _ := SourceItemVersion(item)
	sum2 := sha256.Sum256([]byte("content"))
	require.Equal(t, hex.EncodeToString(sum2[:]), hash2)

	// 无 content → revision
	item.Content = nil
	_, hash3, _ := SourceItemVersion(item)
	sum3 := sha256.Sum256([]byte("rev-1"))
	require.Equal(t, hex.EncodeToString(sum3[:]), hash3)
}

func TestUpsertAndReadSourceMetadata_往返(t *testing.T) {
	root := t.TempDir()
	item := &RawChangeItem{
		LogicalID:   "gh:1",
		RevisionID:  "abc123",
		Title:       ptrString("我的源  标题"),
		Content:     ptrString("# body"),
		OriginalRef: "https://example.com/repo",
		Metadata:    map[string]any{"resource": "readme"},
	}
	sourceID, err := UpsertSourceMetadata(root, item, "github", "svc-1", "2026-11-28T00:00:00Z")
	require.NoError(t, err)
	require.Regexp(t, `^src_[0-9a-f]{32}$`, sourceID)

	metadata, err := ReadSourceMetadata(filepath.Join(root, sourceID+".md"))
	require.NoError(t, err)
	require.Equal(t, "我的源 标题", metadata["title"])
	require.Equal(t, "readme", metadata["source_type"])
	require.Equal(t, "github", metadata["provider"])
	require.Equal(t, "2026-11-28T00:00:00Z", metadata["last_seen"])

	// 二次 upsert：first_seen 保留，last_seen 更新
	item2 := &RawChangeItem{
		LogicalID:   "gh:1",
		RevisionID:  "def456",
		Content:     ptrString("# body2"),
		OriginalRef: "https://example.com/repo",
		Metadata:    map[string]any{"resource": "readme"},
	}
	id2, err := UpsertSourceMetadata(root, item2, "github", "svc-1", "2026-11-29T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, sourceID, id2)
	updated, err := ReadSourceMetadata(filepath.Join(root, sourceID+".md"))
	require.NoError(t, err)
	require.Equal(t, "2026-11-28T00:00:00Z", updated["first_seen"])
	require.Equal(t, "2026-11-29T00:00:00Z", updated["last_seen"])
}

func TestReadSourceMetadata_校验拒绝(t *testing.T) {
	root := t.TempDir()
	// 文件名非法
	err := func() error {
		bad := filepath.Join(root, "not-an-id.md")
		require.NoError(t, os.WriteFile(bad, []byte("# x\n"), 0o644))
		_, err := ReadSourceMetadata(bad)
		return err
	}()
	require.ErrorContains(t, err, "filename is invalid")

	// symlink 链拒绝
	link := filepath.Join(root, "link")
	target := filepath.Join(root, "src_"+string(make([]byte, 0))+".md")
	require.NoError(t, os.Symlink(root, link))
	_, err = ReadSourceMetadata(filepath.Join(link, "whatever.md"))
	require.ErrorContains(t, err, "symlink")
	_ = target
}

func TestReadSourceDetail_公开字段(t *testing.T) {
	root := t.TempDir()
	item := &RawChangeItem{
		LogicalID:   "gh:2",
		RevisionID:  "r1",
		Title:       ptrString("Detail"),
		Content:     ptrString("c"),
		OriginalRef: "https://example.com/repo2",
		Metadata:    map[string]any{},
	}
	sourceID, err := UpsertSourceMetadata(root, item, "github", "svc", "2026-11-28T00:00:00Z")
	require.NoError(t, err)
	detail, err := ReadSourceDetail(root, sourceID)
	require.NoError(t, err)
	require.Equal(t, "Detail", detail["title"])
	require.Equal(t, "svc", detail["service_id"])

	// 非法 ID
	_, err = ReadSourceDetail(root, "bad")
	require.ErrorContains(t, err, "atomic source ID is invalid")
}

func TestAtomicMetadataWrite_失败清理(t *testing.T) {
	// 目标为目录 → rename 失败且不留临时文件
	root := t.TempDir()
	target := filepath.Join(root, "src_x.md")
	require.NoError(t, os.Mkdir(target, 0o755))
	err := atomicMetadataWrite(target, []byte("data"))
	require.Error(t, err)
	entries, _ := os.ReadDir(root)
	require.Len(t, entries, 1, "只应残留目标目录本身，无临时文件")
}
