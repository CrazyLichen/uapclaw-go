// 授权 send_file 全链集成测试（2026-11-16 SendFileToolkit 全量对齐批）。
// 链路：permissions rail 发布执行授权（grant）→ SendFileToolkit 授权路径消费
// → verified_download_assets 密封副本 + HMAC 资产令牌 → 令牌验签 + is_active 校验。
// 真实跨越 permissions / tools / verified_download_assets 三个子系统。

//go:build integration

package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	verifieddownloadassets "github.com/uapclaw/uapclaw-go/internal/swarm/common/verified_download_assets"
)

// SendFileAuthSuite 授权 send_file 集成套件。
type SendFileAuthSuite struct {
	suite.Suite
}

// TestSendFileAuthSuite 套件入口。
func TestSendFileAuthSuite(t *testing.T) {
	suite.Run(t, new(SendFileAuthSuite))
}

// TestSendFileAuthSuite_授权全链 授权消费→资产密封→chat.file→令牌可验。
func (s *SendFileAuthSuite) TestSendFileAuthSuite_授权全链() {
	// 环境隔离：固定密钥 + 临时资产根
	tools.ResetWebFileDownloadManager()
	s.T().Cleanup(func() { tools.ResetWebFileDownloadManager() })
	assetRoot := s.T().TempDir()
	owner, err := verifieddownloadassets.NewVerifiedDownloadAssetOwner(assetRoot, verifieddownloadassets.OwnerOptions{StartSweeper: false})
	s.Require().NoError(err)
	tools.SetWebFileDownloadAssetOwner(owner)
	s.T().Cleanup(func() { tools.SetWebFileDownloadAssetOwner(nil) })

	// 推送/历史捕获
	var pushMsgs []map[string]any
	var pushMu sync.Mutex
	tools.SetSendFilePushFunc(func(_ context.Context, msg map[string]any) error {
		pushMu.Lock()
		pushMsgs = append(pushMsgs, msg)
		pushMu.Unlock()
		return nil
	})
	tools.SetSendFileHistoryFunc(func(string, string, string, string, string,
		float64, string, map[string]any, map[string]any, string) {})
	s.T().Cleanup(func() { tools.SetSendFilePushFunc(nil); tools.SetSendFileHistoryFunc(nil) })

	// 交付源文件
	src := filepath.Join(s.T().TempDir(), "report.pdf")
	s.Require().NoError(os.WriteFile(src, []byte("authorized-content"), 0o644))

	// 步骤 1：permissions rail 发布执行授权（精确路径 + 目标渠道）
	grant, err := permissions.CreateSendFileExecutionGrant([]string{src}, "web")
	s.Require().NoError(err)
	ctx := permissions.PublishSendFileExecutionGrant(context.Background(), grant)

	// 步骤 2：授权模式 SendFileToolkit 执行（消费 grant + 密封资产）
	tk := tools.NewSendFileToolkit("req-a1", "sess-a1", "web", tools.SendFileRuntimeContext{
		RequireExecutionAuthorization: boolPtrAuth(true),
		AssetOwner:                    owner,
	})
	result, err := tk.SendFile(ctx, src, "web")
	s.Require().NoError(err)
	s.Require().Contains(result, "成功发送 1 个文件")

	// 步骤 3：chat.file 事件载荷为密封路径 + 资产令牌
	pushMu.Lock()
	defer pushMu.Unlock()
	s.Require().Len(pushMsgs, 1)
	payload, ok := pushMsgs[0]["payload"].(map[string]any)
	s.Require().True(ok)
	s.Equal("chat.file", payload["event_type"])
	files, ok := payload["files"].([]map[string]any)
	s.Require().True(ok)
	s.Require().Len(files, 1)
	sealedPath, _ := files[0]["path"].(string)
	s.True(strings.HasPrefix(sealedPath, assetRoot), "path 应为密封副本: %s", sealedPath)
	token, _ := files[0]["download_token"].(string)
	s.Require().NotEmpty(token)

	// 步骤 4：令牌验签 + is_active 通过（TTL 内）
	filePayload := tools.ValidateFileDownloadToken(token, "sess-a1", true)
	s.Require().NotNil(filePayload, "资产令牌应通过验签与 is_active")

	// 步骤 5：revoke 后同一令牌被拒（账本终态）
	assetID, _ := filePayload["asset_id"].(string)
	s.Require().NotEmpty(assetID)
	expiresAt, _ := filePayload["exp"].(float64)
	sizeBytes, _ := filePayload["size"].(float64)
	digest, _ := filePayload["digest"].(string)
	asset := &verifieddownloadassets.VerifiedDownloadAsset{
		AssetID:       assetID,
		SealedPath:    sealedPath,
		ExpiresAt:     expiresAt,
		SizeBytes:     int64(sizeBytes),
		ContentDigest: digest,
	}
	owner.Revoke(asset)
	s.Nil(tools.ValidateFileDownloadToken(token, "sess-a1", true), "revoke 后令牌应拒绝")

	// 步骤 6：未授权 ctx 下授权模式 toolkit 拒绝执行（grant 缺失）
	tk2 := tools.NewSendFileToolkit("req-a2", "sess-a1", "web", tools.SendFileRuntimeContext{
		RequireExecutionAuthorization: boolPtrAuth(true),
		AssetOwner:                    owner,
	})
	result2, err := tk2.SendFile(context.Background(), src)
	s.Require().NoError(err)
	s.Require().Contains(result2, "提交文件失败", "无授权 ctx 应拒绝")
}

// TestSendFileAuthSuite_授权路径需精确匹配 授权路径与请求路径不符 → 拒绝。
func (s *SendFileAuthSuite) TestSendFileAuthSuite_授权路径需精确匹配() {
	tools.ResetWebFileDownloadManager()
	s.T().Cleanup(func() { tools.ResetWebFileDownloadManager() })
	other := filepath.Join(s.T().TempDir(), "other.txt")
	s.Require().NoError(os.WriteFile(other, []byte("x"), 0o644))

	grant, err := permissions.CreateSendFileExecutionGrant([]string{other}, nil)
	s.Require().NoError(err)
	ctx := permissions.PublishSendFileExecutionGrant(context.Background(), grant)

	tk := tools.NewSendFileToolkit("req-a3", "sess-a3", "web", tools.SendFileRuntimeContext{
		RequireExecutionAuthorization: boolPtrAuth(true),
	})
	result, err := tk.SendFile(ctx, filepath.Join(s.T().TempDir(), "not-granted.txt"))
	s.Require().NoError(err)
	s.Require().Contains(result, "提交文件失败", "请求路径与授权不符应拒绝")
}

// boolPtrAuth 可选 bool 助手（外部包不可复用 tools 内部 helper）。
func boolPtrAuth(b bool) *bool { return &b }
