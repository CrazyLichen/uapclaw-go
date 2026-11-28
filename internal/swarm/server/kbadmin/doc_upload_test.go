package kbadmin

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newTestDomainWithUpload 构造带上传槽位表的真实链 Domain（真实 SQLite + 真实 Chroma 本地向量库）
func newTestDomainWithUpload(t *testing.T) *Domain {
	t.Helper()
	d := newTestDomain(t)
	d.Doc.SetUploadStore(NewUploadStore(filepath.Join(t.TempDir(), "kb_uploads")))
	// 建一个 KB 供上传链使用
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "kb", ReqMethod: schema.ReqMethodKBKBCreate,
		Params: []byte(`{"id":"kb1","name":"测试库"}`),
	})
	require.True(t, resp.OK)
	return d
}

// TestDocUpload_发起收尾全流程 测试 kb.doc.upload → 落盘 → kb.file.upload_complete 全链
func TestDocUpload_发起收尾全流程(t *testing.T) {
	d := newTestDomainWithUpload(t)
	data := []byte("hello kb upload")
	sum := sha256HexOf(data)

	// 发起：返回槽位
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocUpload,
		Params: []byte(`{"kb_id":"kb1","file_name":"hello.txt","size":15,"file_hash":"` + sum + `"}`),
	})
	require.True(t, resp.OK)
	slot := resp.Payload["upload"].(map[string]any)
	uploadID := slot["upload_id"].(string)
	resolved := slot["resolved_path"].(string)

	// 数据面落盘（file.upload_chunk append 语义由既有 adapter 承接，测试直写等价）
	require.NoError(t, os.WriteFile(resolved, data, 0o644))

	// 收尾：hash 一致 → 摘除槽位 + 返回文件标识
	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r2", ReqMethod: schema.ReqMethodKBFileUploadComplete,
		Params: []byte(`{"upload_id":"` + uploadID + `"}`),
	})
	require.True(t, resp.OK)
	file := resp.Payload["file"].(map[string]any)
	require.Equal(t, "hello.txt", file["file_name"])
	require.Equal(t, int64(15), int64(file["size"].(float64)))
	finalPath := file["final_path"].(string)
	require.FileExists(t, finalPath)
	// 槽位已摘除
	_, err := d.Doc.uploads.Get(uploadID)
	require.ErrorIs(t, err, ErrKBUploadNotFound)
}

// TestDocUpload_hash不符清理 测试收尾 hash 不符返回 UPLOAD_HASH_MISMATCH 且分片清理
func TestDocUpload_hash不符清理(t *testing.T) {
	d := newTestDomainWithUpload(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocUpload,
		Params: []byte(`{"kb_id":"kb1","file_name":"a.txt","size":3,"file_hash":"deadbeef"}`),
	})
	require.True(t, resp.OK)
	slot := resp.Payload["upload"].(map[string]any)
	require.NoError(t, os.WriteFile(slot["resolved_path"].(string), []byte("abc"), 0o644))

	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r2", ReqMethod: schema.ReqMethodKBFileUploadComplete,
		Params: []byte(`{"upload_id":"` + slot["upload_id"].(string) + `"}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, "UPLOAD_HASH_MISMATCH", resp.Payload["error_code"])
	require.NoDirExists(t, filepath.Dir(slot["resolved_path"].(string)))
}

// TestDocUpload_KB不存在 测试发起时 KB 校验失败返回 KB_NOT_FOUND
func TestDocUpload_KB不存在(t *testing.T) {
	d := newTestDomainWithUpload(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocUpload,
		Params: []byte(`{"kb_id":"nope","file_name":"a.txt","size":1,"file_hash":""}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, ErrCodeKBNotFound, resp.Payload["error_code"])
}

// TestDocUpload_超限拒绝 测试超 50MiB 返回 FILE_TOO_LARGE
func TestDocUpload_超限拒绝(t *testing.T) {
	d := newTestDomainWithUpload(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocUpload,
		Params: []byte(`{"kb_id":"kb1","file_name":"big.bin","size":52428801,"file_hash":""}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, "FILE_TOO_LARGE", resp.Payload["error_code"])
}

// TestDocUpload_cancel 测试取消清理分片
func TestDocUpload_cancel(t *testing.T) {
	d := newTestDomainWithUpload(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocUpload,
		Params: []byte(`{"kb_id":"kb1","file_name":"a.txt","size":1,"file_hash":""}`),
	})
	require.True(t, resp.OK)
	slot := resp.Payload["upload"].(map[string]any)
	uploadID := slot["upload_id"].(string)
	resolved := slot["resolved_path"].(string)
	require.NoError(t, os.WriteFile(resolved, []byte("x"), 0o644))

	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r2", ReqMethod: schema.ReqMethodKBDocCancelUpload,
		Params: []byte(`{"upload_id":"` + uploadID + `"}`),
	})
	require.True(t, resp.OK)
	require.NoFileExists(t, resolved)
	// 二次取消 → KB_UPLOAD_NOT_FOUND
	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r3", ReqMethod: schema.ReqMethodKBDocCancelUpload,
		Params: []byte(`{"upload_id":"` + uploadID + `"}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, "KB_UPLOAD_NOT_FOUND", resp.Payload["error_code"])
}

// TestDocUpload_未知uploadID 测试 complete 引用不存在槽位
func TestDocUpload_未知uploadID(t *testing.T) {
	d := newTestDomainWithUpload(t)
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBFileUploadComplete,
		Params: []byte(`{"upload_id":"nope"}`),
	})
	require.False(t, resp.OK)
	require.Equal(t, "KB_UPLOAD_NOT_FOUND", resp.Payload["error_code"])
}

// TestDocDownloadChunk_分帧读回 测试反向分块下载还原
func TestDocDownloadChunk_分帧读回(t *testing.T) {
	d := newTestDomainWithUpload(t)
	data := []byte("0123456789")
	resp := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r1", ReqMethod: schema.ReqMethodKBDocUpload,
		Params: []byte(`{"kb_id":"kb1","file_name":"a.txt","size":10,"file_hash":""}`),
	})
	require.True(t, resp.OK)
	slot := resp.Payload["upload"].(map[string]any)
	require.NoError(t, os.WriteFile(slot["resolved_path"].(string), data, 0o644))
	resp = HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "r2", ReqMethod: schema.ReqMethodKBFileUploadComplete,
		Params: []byte(`{"upload_id":"` + slot["upload_id"].(string) + `"}`),
	})
	require.True(t, resp.OK)
	finalPath := resp.Payload["file"].(map[string]any)["final_path"].(string)

	// 三帧读完
	var got []byte
	offset := 0
	for {
		r := HandleKBRequest(d, &schema.AgentRequest{
			RequestID: "d", ReqMethod: schema.ReqMethodKBDocDownload,
			Params: []byte(`{"path":"` + finalPath + `","offset":` + itoa(offset) + `,"limit":4}`),
		})
		require.True(t, r.OK)
		chunk, err := base64.StdEncoding.DecodeString(r.Payload["data_base64"].(string))
		require.NoError(t, err)
		got = append(got, chunk...)
		if r.Payload["eof"].(bool) {
			break
		}
		offset += len(chunk)
	}
	require.Equal(t, data, got)
	// 不存在路径 → KB_UPLOAD_NOT_FOUND
	r := HandleKBRequest(d, &schema.AgentRequest{
		RequestID: "d2", ReqMethod: schema.ReqMethodKBDocDownload,
		Params: []byte(`{"path":"/no/such/file","offset":0,"limit":4}`),
	})
	require.False(t, r.OK)
	require.Equal(t, "KB_UPLOAD_NOT_FOUND", r.Payload["error_code"])
	// Service 层错误哨兵直查（分发映射已在上面覆盖）
	_, _, err := d.Doc.DownloadChunk("/no/such/file", 0, 4)
	require.True(t, errors.Is(err, ErrKBUploadNotFound))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sha256HexOf 测试助手：字节串 sha256 十六进制
func sha256HexOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// itoa 测试助手：int → 十进制字符串
func itoa(v int) string {
	return strconv.Itoa(v)
}
