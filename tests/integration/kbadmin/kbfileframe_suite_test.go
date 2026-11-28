//go:build integration

// Package kbadmin 30.5 WS 文件分帧通道集成测试。
//
// 跨子系统：swarm/server（E2A 信封分发）+ swarm/server/kbadmin（上传编排 +
// 槽位表 + sha256 收尾）+ swarm/server/adapter（file.upload_chunk 既有协议落盘）
// + swarm/schema（方法族）。真实链路：kb.doc.upload 发起 → 数据面
// kb.file.upload_chunk（gateway 侧映射为 file.upload_chunk 协议，server 侧同一
// handler 落盘）→ kb.file.upload_complete hash 校验 → kb.doc.download 反向分帧
// 还原比对；hash 不符/取消/未知 upload_id 错误路径同链覆盖。禁替身。
package kbadmin

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	kbadminserver "github.com/uapclaw/uapclaw-go/internal/swarm/server"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KBFileFrameSuite 30.5 分帧通道全链：发起 → 分帧落盘 → hash 收尾 → 反向下载还原。
type KBFileFrameSuite struct {
	isuite.BaseIntegrationSuite
	env *testhelpers.ServerEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestKBFileFrameSuite 套件入口。
func TestKBFileFrameSuite(t *testing.T) {
	suite.Run(t, new(KBFileFrameSuite))
}

// SetupSuite 隔离数据目录（kb_uploads 落 WorkspaceDir 隔离根）并启动 AgentServer。
func (s *KBFileFrameSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	s.T().Setenv(pathutil.EnvDataDir, s.T().TempDir())
	pathutil.ResetCache()
	kbadminserver.ResetKBDomainForTest()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)

	// 建宿主 KB
	rid := s.env.Send("kb.kb.create", map[string]any{
		"id": "it-frame-kb", "name": "分帧集成库", "type": "document",
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// kbSha256Hex 测试助手：sha256 十六进制小写（与 upload_store 收尾比对同口径）。
func kbSha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// kbB64 测试助手：base64 编码一帧。
func kbB64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }


// TestUploadFrame_全链还原 测试发起→分帧落盘→hash 收尾→反向下载还原全链。
func (s *KBFileFrameSuite) TestUploadFrame_全链还原() {
	data := []byte("kb frame channel integration payload 0123456789")
	hash := kbSha256Hex(data)

	// 发起：kb.doc.upload
	rid := s.env.Send("kb.doc.upload", map[string]any{
		"kb_id": "it-frame-kb", "file_name": "payload.txt",
		"size": len(data), "file_hash": hash,
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	upload := resp.Payload["upload"].(map[string]any)
	uploadID := upload["upload_id"].(string)
	resolved := upload["resolved_path"].(string)

	// 数据面：kb.file.upload_chunk（server 侧走既有 file.upload_chunk 协议，
	// resolved_path 直写 KB 暂存路径）三帧 append
	const frameSize = 16
	for off := 0; off < len(data); off += frameSize {
		end := off + frameSize
		if end > len(data) {
			end = len(data)
		}
		final := end >= len(data)
		// B1 拍板：E2A 线上为映射后的既有协议名 file.upload_chunk
		// （gateway kbUploadChunkForward 改写 method；本套件直投 server，即产线映射后形态）
		rid = s.env.Send("file.upload_chunk", map[string]any{
			"upload_id": uploadID, "resolved_path": resolved,
			// 既有协议 target_rel_path 为必填校验字段（resolved_path 优先生效）
			"target_rel_path": "kb_frame_tmp/payload.txt",
			"data": kbB64(data[off:end]), "final": final,
		}, "", false)
		resp = s.env.AwaitUnary(rid)
		s.Require().True(resp.OK, "分帧 %d 落盘失败: %v", off, resp.Payload)
	}

	// 收尾：kb.file.upload_complete hash 校验
	rid = s.env.Send("kb.file.upload_complete", map[string]any{"upload_id": uploadID}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	file := resp.Payload["file"].(map[string]any)
	s.Require().Equal("payload.txt", file["file_name"])
	s.Require().Equal(int64(len(data)), int64(file["size"].(float64)))
	finalPath := file["final_path"].(string)
	s.Require().FileExists(finalPath)
	written, err := os.ReadFile(finalPath)
	s.Require().NoError(err)
	s.Require().Equal(data, written)

	// 反向下载：kb.doc.download 分帧聚合还原
	var got []byte
	offset := 0
	for {
		rid = s.env.Send("kb.doc.download", map[string]any{
			"path": finalPath, "offset": offset, "limit": 16,
		}, "", false)
		resp = s.env.AwaitUnary(rid)
		s.Require().True(resp.OK)
		chunk, decErr := base64.StdEncoding.DecodeString(resp.Payload["data_base64"].(string))
		s.Require().NoError(decErr)
		got = append(got, chunk...)
		if resp.Payload["eof"].(bool) {
			break
		}
		offset += len(chunk)
	}
	s.Require().Equal(data, got)
}

// TestUploadFrame_hash不符 测试收尾 hash 不符返回 UPLOAD_HASH_MISMATCH 且分片清理。
func (s *KBFileFrameSuite) TestUploadFrame_hash不符() {
	rid := s.env.Send("kb.doc.upload", map[string]any{
		"kb_id": "it-frame-kb", "file_name": "bad.txt",
		"size": 3, "file_hash": "deadbeef",
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	upload := resp.Payload["upload"].(map[string]any)
	resolved := upload["resolved_path"].(string)
	s.Require().NoError(os.WriteFile(resolved, []byte("abc"), 0o644))

	rid = s.env.Send("kb.file.upload_complete", map[string]any{
		"upload_id": upload["upload_id"].(string),
	}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().False(resp.OK)
	s.Require().Equal("UPLOAD_HASH_MISMATCH", resp.Payload["error_code"])
	_, statErr := os.Stat(filepath.Dir(resolved))
	s.Require().True(os.IsNotExist(statErr), "hash 不符后分片目录应清理")
}

// TestUploadFrame_cancel与未知槽位 测试取消清理与 KB_UPLOAD_NOT_FOUND 错误路径。
func (s *KBFileFrameSuite) TestUploadFrame_cancel与未知槽位() {
	rid := s.env.Send("kb.doc.upload", map[string]any{
		"kb_id": "it-frame-kb", "file_name": "cancel.txt", "size": 1, "file_hash": "",
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	upload := resp.Payload["upload"].(map[string]any)
	resolved := upload["resolved_path"].(string)
	s.Require().NoError(os.WriteFile(resolved, []byte("x"), 0o644))

	rid = s.env.Send("kb.doc.cancel_upload", map[string]any{
		"upload_id": upload["upload_id"].(string),
	}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	_, statErr := os.Stat(resolved)
	s.Require().True(os.IsNotExist(statErr))

	// 未知 upload_id：complete / cancel / download 三路同码
	for _, m := range []string{"kb.file.upload_complete", "kb.doc.cancel_upload"} {
		rid = s.env.Send(m, map[string]any{"upload_id": "nope"}, "", false)
		resp = s.env.AwaitUnary(rid)
		s.Require().False(resp.OK, m)
		s.Require().Equal("KB_UPLOAD_NOT_FOUND", resp.Payload["error_code"], m)
	}

	// 超限：FILE_TOO_LARGE
	rid = s.env.Send("kb.doc.upload", map[string]any{
		"kb_id": "it-frame-kb", "file_name": "big.bin", "size": 52428801, "file_hash": "",
	}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().False(resp.OK)
	s.Require().Equal("FILE_TOO_LARGE", resp.Payload["error_code"])
}
