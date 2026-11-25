package web

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/common/verified_download_assets"
)

// buildVerifiedTokenForTest 签发真实 verified_asset_v1 令牌（27.8 签发侧）。
func buildVerifiedTokenForTest(t *testing.T) (string, map[string]any) {
	t.Helper()
	root := t.TempDir()
	assetPath := filepath.Join(root, "asset.bin")
	require.NoError(t, os.WriteFile(assetPath, []byte("abc"), 0o644))
	owner, err := verified_download_assets.NewVerifiedDownloadAssetOwner(
		t.TempDir(), verified_download_assets.OwnerOptions{StartSweeper: false})
	require.NoError(t, err)
	asset, serr := owner.Stage(assetPath, verified_download_assets.StageOptions{
		FileName: "a.bin", ExpiresAt: float64(time.Now().Add(time.Hour).Unix())})
	require.NoError(t, serr)
	require.NoError(t, owner.Commit(asset))
	swarmtools.SetWebFileDownloadAssetOwner(owner)
	t.Cleanup(func() { swarmtools.SetWebFileDownloadAssetOwner(nil) })
	info := swarmtools.BuildVerifiedAssetDownloadInfo(asset, "a.bin", "s1", "u1")
	token := info["download_token"].(string)
	payload := swarmtools.ValidateFileDownloadToken(token, "", false)
	require.NotNil(t, payload)
	return token, payload
}

// TestHandleFileDownloadVerified分流 verified 令牌走 gateway 委托分支：
// 缺 user_id → 400；ac 未注入（单测环境）→ 503（e2e 覆盖全量流）。
// Python: container_file_http.py:838-856 download 分流 + _resolve_user_id。
func TestHandleFileDownloadVerified分流(t *testing.T) {
	token, _ := buildVerifiedTokenForTest(t)
	// 缺 user_id → 400 "user_id is required"（分流前拦截）
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/file-api/download?token="+token, nil)
	HandleFileDownload(rec, req)
	require.Equal(t, 400, rec.Code)
	require.Contains(t, rec.Body.String(), "user_id is required")

	// 带 user_id：委托 gateway；ac 未注入 → 503 AgentServer unavailable
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/file-api/download?token="+token+"&user_id=u1", nil)
	HandleFileDownload(rec2, req2)
	require.Equal(t, 503, rec2.Code)
	require.Contains(t, rec2.Body.String(), "AgentServer is unavailable")
}
