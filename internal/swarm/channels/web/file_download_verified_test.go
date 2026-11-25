package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/common/verified_download_assets"
)

// buildVerifiedAssetForTest 构造真实 sealed 资产（Stage+Commit）与
// verified_asset_v1 令牌（走 27.8 签发侧全链）。
func buildVerifiedAssetForTest(t *testing.T, content []byte, fileName, sessionID string) (string, map[string]any, string) {
	t.Helper()
	root := t.TempDir()
	assetPath := filepath.Join(root, "asset.bin")
	require.NoError(t, os.WriteFile(assetPath, content, 0o644))

	owner, err := verified_download_assets.NewVerifiedDownloadAssetOwner(
		t.TempDir(), verified_download_assets.OwnerOptions{StartSweeper: false})
	require.NoError(t, err)
	asset, serr := owner.Stage(assetPath, verified_download_assets.StageOptions{
		FileName:  fileName,
		ExpiresAt: float64(time.Now().Add(time.Hour).Unix()),
	})
	require.NoError(t, serr)
	require.NoError(t, owner.Commit(asset))
	swarmtools.SetWebFileDownloadAssetOwner(owner)
	t.Cleanup(func() { swarmtools.SetWebFileDownloadAssetOwner(nil) })

	info := swarmtools.BuildVerifiedAssetDownloadInfo(asset, fileName, sessionID, "u1")
	token := info["download_token"].(string)
	payload := swarmtools.ValidateFileDownloadToken(token, "", false)
	require.NotNil(t, payload)
	return token, payload, assetPath
}

// TestServeVerifiedDownload全量流 sealed 资产 → 全量拼装一致。
func TestServeVerifiedDownload全量流(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789"), 1000) // 10000 字节
	token, payload, _ := buildVerifiedAssetForTest(t, content, "report.pdf", "s1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file-api/download?token="+token+"&user_id=u1", nil)
	serveVerifiedDownload(rec, req, "u1", token, payload)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "attachment; filename*=UTF-8''report.pdf", rec.Header().Get("Content-Disposition"))
	require.Equal(t, strconvI64(len(content)), rec.Header().Get("Content-Length"))
	require.Equal(t, content, rec.Body.Bytes())
}

// TestServeFileChunksRange断点 Range 206 / 越界 416。
func TestServeFileChunksRange断点(t *testing.T) {
	content := bytes.Repeat([]byte("abcdefghij"), 50) // 500 字节
	token, payload, _ := buildVerifiedAssetForTest(t, content, "a.bin", "s1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file-api/download?token="+token, nil)
	req.Header.Set("Range", "bytes=100-199")
	serveVerifiedDownload(rec, req, "u1", token, payload)
	require.Equal(t, 206, rec.Code)
	require.Equal(t, "bytes 100-199/500", rec.Header().Get("Content-Range"))
	require.Equal(t, content[100:200], rec.Body.Bytes())

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/file-api/download?token="+token, nil)
	req2.Header.Set("Range", "bytes=99999-")
	serveVerifiedDownload(rec2, req2, "u1", token, payload)
	require.Equal(t, 416, rec2.Code)
	require.Equal(t, "bytes */500", rec2.Header().Get("Content-Range"))
}

// TestServeFileChunksHEAD HEAD 只回头不回体。
func TestServeFileChunksHEAD(t *testing.T) {
	content := []byte("hello world")
	token, payload, _ := buildVerifiedAssetForTest(t, content, "a.bin", "s1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("HEAD", "/file-api/download?token="+token, nil)
	serveVerifiedDownload(rec, req, "u1", token, payload)
	require.Equal(t, 200, rec.Code)
	require.Empty(t, rec.Body.Bytes())
}

// TestServeVerifiedDownload缺sid payload 缺 sid → 400。
func TestServeVerifiedDownload缺sid(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file-api/download", nil)
	serveVerifiedDownload(rec, req, "u1", "faketoken", map[string]any{"kind": verifiedAssetTokenKind})
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_download_token")
}

// TestHandleFileDownload分流 verified 分支接入 HandleFileDownload。
func TestHandleFileDownload分流(t *testing.T) {
	content := []byte("abc")
	token, _, _ := buildVerifiedAssetForTest(t, content, "a.bin", "s1")

	// 缺 user_id → 400 "user_id is required"（Python :852-856）
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file-api/download?token="+token, nil)
	HandleFileDownload(rec, req)
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "user_id is required")

	// 带 user_id → 200 全量
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/file-api/download?token="+token+"&user_id=u1", nil)
	HandleFileDownload(rec2, req2)
	require.Equal(t, 200, rec2.Code)
	require.Equal(t, content, rec2.Body.Bytes())
}

// TestParseSingleByteRange 单区间解析边界。
func TestParseSingleByteRange(t *testing.T) {
	s, e, ok := parseSingleByteRange("bytes=0-0", 100)
	require.True(t, ok)
	require.Equal(t, int64(0), s)
	require.Equal(t, int64(0), e)

	s, e, ok = parseSingleByteRange("bytes=50-", 100)
	require.True(t, ok)
	require.Equal(t, int64(50), s)
	require.Equal(t, int64(99), e)

	s, e, ok = parseSingleByteRange("bytes=-10", 100)
	require.True(t, ok)
	require.Equal(t, int64(90), s)
	require.Equal(t, int64(99), e)

	_, _, ok = parseSingleByteRange("bytes=0-99,200-299", 100) // 多区间
	require.False(t, ok)
	_, _, ok = parseSingleByteRange("bytes=100-", 100) // 越界
	require.False(t, ok)
	_, _, ok = parseSingleByteRange("chars=0-1", 100)
	require.False(t, ok)
}

// TestDecodeBase64Field 解码往返。
func TestDecodeBase64Field(t *testing.T) {
	got, err := decodeBase64Field(base64.StdEncoding.EncodeToString([]byte("hi")))
	require.NoError(t, err)
	require.Equal(t, []byte("hi"), got)
	_, err = decodeBase64Field(123)
	require.Error(t, err)
}

// strconvI64 int → string。
func strconvI64(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
