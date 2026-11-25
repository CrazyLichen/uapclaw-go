package adapter

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	webfiledownload "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	vda "github.com/uapclaw/uapclaw-go/internal/swarm/common/verified_download_assets"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// tinyPNGBytes 1x1 PNG。
var tinyPNGBytes = func() []byte {
	b, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	return b
}()

// ──────────────────────────── 导出函数 ────────────────────────────

// adapterTestEnv 隔离 DATA_DIR（agent 树）与用户目录，返回 (agentUploads, dataRoot)。
func adapterTestEnv(t *testing.T, sid string) (string, string) {
	t.Helper()
	root := t.TempDir()
	uploads := filepath.Join(root, "agent", "sessions", sid, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "agent", "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UAPCLAW_DATA_DIR", root)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return uploads, root
}

// b64e base64 编码助手。
func b64e(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// TestHandlesWorkspaceFileMethod_集合 对齐 methods frozenset 与排除项。
// Python: workspace_file_adapter.py:109-127。
func TestHandlesWorkspaceFileMethod_集合(t *testing.T) {
	yes := []schema.ReqMethod{
		schema.ReqMethodMediaPersist, schema.ReqMethodMediaDiscard,
		schema.ReqMethodDocumentPersist, schema.ReqMethodDocumentFormats,
		schema.ReqMethodPathSelectDirectory, schema.ReqMethodPathSelectFiles,
		schema.ReqMethodFileImportURL, schema.ReqMethodFileUploadChunk,
		schema.ReqMethodFileReadUploadChunk, schema.ReqMethodFileDownloadVerifiedChunk,
	}
	for _, m := range yes {
		if !HandlesWorkspaceFileMethod(m) {
			t.Fatalf("%s 应由文件域承接", m)
		}
	}
	// workspace.tree 原按 D-27.8-1/2 挂账 27.11；2026-11-25 B2 裁决 6 改判
	// 个人版面板随 B2 交付，已入方法表（见 workspace_panel_adapter.go）。
	no := []schema.ReqMethod{"im.file_persist", schema.ReqMethodChatSend}
	for _, m := range no {
		if HandlesWorkspaceFileMethod(m) {
			t.Fatalf("%s 不应由文件域承接（im.file_persist 归 IM 渠道域）", m)
		}
	}
	if !HandlesWorkspaceFileMethod("workspace.tree") {
		t.Fatalf("workspace.tree 应由 workspace 面板承接（27-A B2 裁决 6）")
	}
}

// TestHandleMediaPersist_回传key白名单 对齐 :253-257（只回四个 key）。
func TestHandleMediaPersist_回传key白名单(t *testing.T) {
	adapterTestEnv(t, "adm-sess-1")
	params := map[string]any{
		"content":     "",
		"query":       "q",
		"extra_noise": "不应回传",
		"media_items": []any{
			map[string]any{"type": "image", "filename": "a.png", "mime_type": "image/png", "base64_data": b64e(tinyPNGBytes)},
		},
	}
	payload, ok, code, err := HandleWorkspaceFileRequest(schema.ReqMethodMediaPersist, params, "adm-sess-1")
	if err != nil || !ok || code != "" {
		t.Fatalf("应成功: %v %q %v", ok, code, err)
	}
	if _, exists := payload["media_items"]; !exists {
		t.Fatalf("payload 缺 media_items: %v", payload)
	}
	if _, exists := payload["extra_noise"]; exists {
		t.Fatalf("payload 不应含白名单外 key: %v", payload)
	}
}

// TestHandleMediaDiscard_必填与删除 对齐 :266-288。
func TestHandleMediaDiscard_必填与删除(t *testing.T) {
	uploads, _ := adapterTestEnv(t, "adm-sess-2")
	target := filepath.Join(uploads, "x.png")
	if err := os.WriteFile(target, tinyPNGBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok, code, err := HandleWorkspaceFileRequest(schema.ReqMethodMediaDiscard, map[string]any{}, "adm-sess-2")
	if ok || code != "BAD_REQUEST" || err.Error() != "path is required" {
		t.Fatalf("空 path 形状不符: %q %v", code, err)
	}
	payload, _, _, err := HandleWorkspaceFileRequest(schema.ReqMethodMediaDiscard,
		map[string]any{"path": target}, "adm-sess-2")
	if err != nil || payload["deleted"] != true {
		t.Fatalf("删除失败: %v %v", payload, err)
	}
}

// TestHandleDocumentFormats 对齐 :326-333。
func TestHandleDocumentFormats(t *testing.T) {
	payload, ok, _, err := HandleWorkspaceFileRequest(schema.ReqMethodDocumentFormats, map[string]any{}, "")
	if err != nil || !ok {
		t.Fatalf("应成功: %v %v", ok, err)
	}
	if _, exists := payload["forbidden_formats"]; !exists {
		t.Fatalf("缺 forbidden_formats: %v", payload)
	}
}

// TestHandleSelectDirectory_分支 对齐 :335-381 四分支。
func TestHandleSelectDirectory_分支(t *testing.T) {
	_, root := adapterTestEnv(t, "adm-sess-3")

	_, ok, code, err := HandleWorkspaceFileRequest(schema.ReqMethodPathSelectDirectory, map[string]any{}, "adm-sess-3")
	if ok || code != "UNSUPPORTED" || !strings.Contains(err.Error(), "enter the project name and absolute path manually") {
		t.Fatalf("UNSUPPORTED 分支不符: %q %v", code, err)
	}
	_, _, code, err = HandleWorkspaceFileRequest(schema.ReqMethodPathSelectDirectory,
		map[string]any{"initial_dir": "/etc"}, "adm-sess-3")
	if code != "BAD_REQUEST" || !strings.Contains(err.Error(), "must be inside the agent workspace") {
		t.Fatalf("越界分支不符: %q %v", code, err)
	}
	payload, ok, _, _ := HandleWorkspaceFileRequest(schema.ReqMethodPathSelectDirectory,
		map[string]any{"initial_dir": filepath.Join(root, "agent", "workspace", "nope")}, "adm-sess-3")
	if !ok || payload["cancelled"] != true {
		t.Fatalf("不存在分支不符: %v", payload)
	}
	want := filepath.Join(root, "agent", "workspace")
	payload, _, _, _ = HandleWorkspaceFileRequest(schema.ReqMethodPathSelectDirectory,
		map[string]any{"initial_dir": want}, "adm-sess-3")
	if payload["path"] != want || payload["cancelled"] != false {
		t.Fatalf("选中分支不符: %v", payload)
	}
	f := filepath.Join(root, "agent", "workspace", "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, code, err = HandleWorkspaceFileRequest(schema.ReqMethodPathSelectDirectory,
		map[string]any{"initial_dir": f}, "adm-sess-3")
	if code != "BAD_REQUEST" || err.Error() != "path is not a directory" {
		t.Fatalf("非目录分支不符: %q %v", code, err)
	}
}

// TestHandleSelectFiles_枚举与图片base64 对齐 :383-435/:784-818。
func TestHandleSelectFiles_枚举与图片base64(t *testing.T) {
	_, root := adapterTestEnv(t, "adm-sess-4")
	ws := filepath.Join(root, "agent", "workspace")
	if err := os.WriteFile(filepath.Join(ws, "pic.png"), tinyPNGBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "doc.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, ok, _, err := HandleWorkspaceFileRequest(schema.ReqMethodPathSelectFiles,
		map[string]any{"initial_dir": ws}, "adm-sess-4")
	if err != nil || !ok {
		t.Fatalf("枚举失败: %v %v", ok, err)
	}
	files := payload["files"].([]map[string]any)
	if len(files) != 2 {
		t.Fatalf("应枚举 2 个文件（排除隐藏与目录）: %d", len(files))
	}
	var pic map[string]any
	for _, f := range files {
		if f["filename"] == "pic.png" {
			pic = f
		}
	}
	if pic == nil || pic["kind"] != "image" || pic["base64"] == "" {
		t.Fatalf("图片条目应附 base64 与 kind=image: %v", pic)
	}
	_, _, code, _ := HandleWorkspaceFileRequest(schema.ReqMethodPathSelectFiles,
		map[string]any{"initial_dir": ws, "allow_multiple": "yes"}, "adm-sess-4")
	if code != "BAD_REQUEST" {
		t.Fatalf("allow_multiple 类型校验不符: %q", code)
	}
}

// TestHandleImportURL_下载与逐项容错 对齐 :673-730/:740-781。
func TestHandleImportURL_下载与逐项容错(t *testing.T) {
	_, _ = adapterTestEnv(t, "adm-sess-5")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("downloaded-content"))
	}))
	defer srv.Close()

	params := map[string]any{"files": []any{
		map[string]any{"url": srv.URL + "/f.txt", "name": "got.txt"},
		map[string]any{"name": "no-url.txt"},
		map[string]any{"url": "not-a-valid-url"},
	}}
	payload, ok, _, err := HandleWorkspaceFileRequest(schema.ReqMethodFileImportURL, params, "adm-sess-5")
	if err != nil || !ok {
		t.Fatalf("应整体成功（逐项容错）: %v %v", ok, err)
	}
	files := payload["files"].([]any)
	if len(files) != 3 {
		t.Fatalf("应回 3 项: %d", len(files))
	}
	first := files[0].(map[string]any)
	if first["filename"] != "got.txt" {
		t.Fatalf("下载项不符: %v", first)
	}
	b, err := os.ReadFile(first["path"].(string))
	if err != nil || string(b) != "downloaded-content" {
		t.Fatalf("下载落盘不符: %v", err)
	}
	if _, hasErr := files[1].(map[string]any)["error"]; !hasErr {
		t.Fatalf("缺 url 项应带 error: %v", files[1])
	}
	_, _, code, _ := HandleWorkspaceFileRequest(schema.ReqMethodFileImportURL,
		map[string]any{"files": "x"}, "adm-sess-5")
	if code != "BAD_REQUEST" {
		t.Fatalf("files 类型校验不符: %q", code)
	}
}

// TestHandleUploadChunk_首块认领与越界 对齐 :495-545。
func TestHandleUploadChunk_首块认领与越界(t *testing.T) {
	_, _ = adapterTestEnv(t, "adm-sess-6")
	chunk := b64e([]byte("hello-chunk"))
	payload, ok, _, err := HandleWorkspaceFileRequest(schema.ReqMethodFileUploadChunk, map[string]any{
		"upload_id": "u1", "target_rel_path": "uploads/rel/file.bin", "data": chunk,
	}, "adm-sess-6")
	if err != nil || !ok {
		t.Fatalf("首块失败: %v %v", ok, err)
	}
	claimed := payload["path"].(string)
	if payload["final"] != false {
		t.Fatalf("final 默认 false: %v", payload)
	}
	payload, _, _, err = HandleWorkspaceFileRequest(schema.ReqMethodFileUploadChunk, map[string]any{
		"upload_id": "u1", "target_rel_path": "uploads/rel/file.bin",
		"resolved_path": claimed, "data": b64e([]byte("-tail")), "final": true,
	}, "adm-sess-6")
	if err != nil || payload["final"] != true {
		t.Fatalf("追加块失败: %v %v", payload, err)
	}
	b, err := os.ReadFile(claimed)
	if err != nil || string(b) != "hello-chunk-tail" {
		t.Fatalf("拼装不符: %q %v", b, err)
	}

	cases := []struct {
		name   string
		params map[string]any
		code   string
		want   string
	}{
		{"缺参数", map[string]any{"upload_id": "", "target_rel_path": "a", "data": chunk}, "BAD_REQUEST", "required"},
		{"绝对路径", map[string]any{"upload_id": "u", "target_rel_path": "/abs", "data": chunk}, "BAD_REQUEST", "invalid_target_path"},
		{"点点段", map[string]any{"upload_id": "u", "target_rel_path": "a/../b", "data": chunk}, "BAD_REQUEST", "invalid_target_path"},
		{"非法base64", map[string]any{"upload_id": "u", "target_rel_path": "a/b", "data": "!!"}, "BAD_REQUEST", "invalid base64"},
		{"空块", map[string]any{"upload_id": "u", "target_rel_path": "a/b", "data": ""}, "BAD_REQUEST", "empty"},
		// Python: resolved_path 直给时以 is_path_within_user_dirs 判界（:520-526）
		{"越出用户目录", map[string]any{"upload_id": "u", "target_rel_path": "a/b", "resolved_path": "/etc/evil.bin", "data": chunk}, "BAD_REQUEST", "path_outside_workspace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, code, err := HandleWorkspaceFileRequest(schema.ReqMethodFileUploadChunk, tc.params, "adm-sess-6")
			if code != tc.code || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("code=%q err=%v, want %q %q", code, err, tc.code, tc.want)
			}
		})
	}
}

// TestHandleReadUploadChunk_四态错误码 对齐 :547-569。
func TestHandleReadUploadChunk_四态错误码(t *testing.T) {
	uploads, _ := adapterTestEnv(t, "adm-sess-7")
	name := "f.bin"
	if err := os.WriteFile(filepath.Join(uploads, name), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, ok, _, err := HandleWorkspaceFileRequest(schema.ReqMethodFileReadUploadChunk, map[string]any{
		"path": filepath.Join(uploads, name), "offset": float64(0), "limit": float64(10),
	}, "adm-sess-7")
	if err != nil || !ok || payload["size"].(int64) != 10 {
		t.Fatalf("正常读不符: %v %v", payload, err)
	}
	_, _, code, _ := HandleWorkspaceFileRequest(schema.ReqMethodFileReadUploadChunk, map[string]any{
		"path": filepath.Join(uploads, name), "offset": float64(0), "limit": float64(10),
	}, "../evil")
	if code != "BAD_REQUEST" {
		t.Fatalf("非法会话应 BAD_REQUEST: %q", code)
	}
	_, _, code, _ = HandleWorkspaceFileRequest(schema.ReqMethodFileReadUploadChunk, map[string]any{
		"path": filepath.Join(t.TempDir(), "else.bin"), "offset": float64(0), "limit": float64(10),
	}, "adm-sess-7")
	if code != "FORBIDDEN" {
		t.Fatalf("越界应 FORBIDDEN: %q", code)
	}
	_, _, code, _ = HandleWorkspaceFileRequest(schema.ReqMethodFileReadUploadChunk, map[string]any{
		"path": filepath.Join(uploads, name), "offset": float64(99), "limit": float64(10),
	}, "adm-sess-7")
	if code != "BAD_REQUEST" {
		t.Fatalf("offset 超大小应 BAD_REQUEST: %q", code)
	}
}

// TestHandleVerifiedDownloadChunk_合法token全链 走 Stage→Commit→token→adapter 校验读块。
func TestHandleVerifiedDownloadChunk_合法token全链(t *testing.T) {
	_, root := adapterTestEnv(t, "adm-sess-8")
	src := filepath.Join(root, "src.bin")
	content := []byte("0123456789abcdef")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	owner, err := vda.NewVerifiedDownloadAssetOwner(filepath.Join(root, "vda"), vda.OwnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	asset, err := owner.Stage(src, vda.StageOptions{FileName: "asset.bin", ExpiresAt: 9999999999})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Commit(asset); err != nil {
		t.Fatal(err)
	}
	webfiledownload.SetWebFileDownloadAssetOwner(owner)
	mgr := webfiledownload.GetWebFileDownloadManager()
	token := mgr.GenerateVerifiedAssetToken(asset, "asset.bin", "adm-sess-8")

	payload, ok, code, err := HandleWorkspaceFileRequest(schema.ReqMethodFileDownloadVerifiedChunk,
		map[string]any{"token": token, "offset": float64(4), "limit": float64(4)}, "adm-sess-8")
	if err != nil || !ok || code != "" {
		t.Fatalf("合法 token 应通过: ok=%v code=%q err=%v", ok, code, err)
	}
	if payload["offset"] != 4 || payload["eof"] != false || payload["chunk_size"] != 4 || payload["size"] != len(content) {
		t.Fatalf("payload 形状不符: %v", payload)
	}
	// 伪造 token → FORBIDDEN
	_, _, code, err = HandleWorkspaceFileRequest(schema.ReqMethodFileDownloadVerifiedChunk,
		map[string]any{"token": "fake.token.sig", "offset": float64(0)}, "adm-sess-8")
	if code != "FORBIDDEN" || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("伪造 token 分支不符: %q %v", code, err)
	}
	// offset 超声明 → BAD_REQUEST
	_, _, code, _ = HandleWorkspaceFileRequest(schema.ReqMethodFileDownloadVerifiedChunk,
		map[string]any{"token": token, "offset": float64(len(content) + 1)}, "adm-sess-8")
	if code != "BAD_REQUEST" {
		t.Fatalf("offset 超声明应 BAD_REQUEST: %q", code)
	}
}

// TestHandleWorkspaceFileRequest_未知方法 对齐 :193-197。
func TestHandleWorkspaceFileRequest_未知方法(t *testing.T) {
	_, _, code, err := HandleWorkspaceFileRequest(schema.ReqMethod("im.file_persist"), map[string]any{}, "s")
	if code != "BAD_REQUEST" || !strings.Contains(err.Error(), "unsupported method") {
		t.Fatalf("未知方法不符: %q %v", code, err)
	}
}

// TestHandleDocumentPersist_回填与兜底 对齐 :290-324。
func TestHandleDocumentPersist_回填与兜底(t *testing.T) {
	adapterTestEnv(t, "adm-sess-9")
	params := map[string]any{"documents": []any{
		map[string]any{"filename": "n.txt", "base64_data": b64e([]byte("ok"))},
	}}
	payload, ok, _, err := HandleWorkspaceFileRequest(schema.ReqMethodDocumentPersist, params, "adm-sess-9")
	if err != nil || !ok {
		t.Fatalf("应成功: %v %v", ok, err)
	}
	for _, key := range []string{"media_items", "forbidden_formats"} {
		if _, exists := payload[key]; !exists {
			t.Fatalf("payload 缺 %s: %v", key, payload)
		}
	}
	if _, exists := payload["content"]; exists {
		t.Fatalf("content 缺省时不应回传: %v", payload)
	}
}

// TestResolveWithinWorkspace_相对与波浪号 对齐 :80-103 边界。
func TestResolveWithinWorkspace_相对与波浪号(t *testing.T) {
	base := "/tmp/ws-root"
	if got := resolveWithinWorkspace("", base); got != base {
		t.Fatalf("空值应回落根: %q", got)
	}
	if got := resolveWithinWorkspace("sub/dir", base); got != "/tmp/ws-root/sub/dir" {
		t.Fatalf("相对路径应拼接: %q", got)
	}
	if got := resolveWithinWorkspace("/tmp/ws-root/inner", base); got != "/tmp/ws-root/inner" {
		t.Fatalf("根内绝对路径应通过: %q", got)
	}
	if got := resolveWithinWorkspace("/tmp/other", base); got != "" {
		t.Fatalf("越界应返回空: %q", got)
	}
}

// TestContainsDotDotSegment_助手 路径段判定。
func TestContainsDotDotSegment_助手(t *testing.T) {
	if !containsDotDotSegment("a/../b") {
		t.Fatal("a/../b 应检出")
	}
	if containsDotDotSegment("a..b/c") {
		t.Fatal("a..b 不含独立段")
	}
}

// TestIntHelpers_双形态 JSON float64 / YAML int 双形态（YAML 整数解出 int 非 float64 坑）。
func TestIntHelpers_双形态(t *testing.T) {
	if intOf(float64(3.9), 0) != 3 || intOf(7, 0) != 7 || intOf(nil, 5) != 5 {
		t.Fatal("intOf 形态不符")
	}
	if v, ok := intFromAny(int64(9)); !ok || v != 9 {
		t.Fatal("intFromAny int64 形态不符")
	}
	if _, ok := intFromAny("x"); ok {
		t.Fatal("字符串不应可转")
	}
}
