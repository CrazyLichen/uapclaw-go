package web

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestPrePersistLargeMedia_超预算改写为persisted 对齐 _pre_persist_large_media 主链。
// Python: app_web_handlers.py:2063-2119。
// 无真实 AgentClient（nil）时上传失败 → 保留原 base64（Python 同语义）；
// 预算扣减与小图原样保留在此可测。
func TestPrePersistLargeMedia_预算扣减与失败保留(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

	// 大图：解码后 >4MiB（必超预算）；小图：合法 PNG（远小于预算）
	bigBase64 := base64.StdEncoding.EncodeToString(append(make([]byte, 5*1024*1024), png...))
	params := map[string]any{
		"content": "",
		"media_items": []any{
			map[string]any{"type": "image", "filename": "big.png", "mime_type": "image/png", "base64_data": bigBase64},
			map[string]any{"type": "image", "filename": "small.png", "mime_type": "image/png", "base64_data": base64.StdEncoding.EncodeToString(png)},
			map[string]any{"type": "text", "text": "非图片透传"},
			"非 dict 透传",
		},
	}
	prePersistLargeMedia(params, "rw-sess", nil) // ac=nil → 上传必失败

	items := params["media_items"].([]any)
	if len(items) != 4 {
		t.Fatalf("条目数应保持 4: %d", len(items))
	}
	// 大图上传失败 → 保留原 base64（下游链路处理）
	big := items[0].(map[string]any)
	if _, hasBase64 := big["base64_data"]; !hasBase64 {
		t.Fatalf("上传失败应保留原 base64: %v", big)
	}
	if big["_persisted"] == true {
		t.Fatalf("ac=nil 时不应标 _persisted: %v", big["_persisted"])
	}
	// 小图原样保留
	small := items[1].(map[string]any)
	if small["_persisted"] == true {
		t.Fatalf("预算内小图不应分流: %v", small["_persisted"])
	}
	// 其余原样
	if _, isMap := items[2].(map[string]any); !isMap {
		t.Fatal("非图片项应原样透传")
	}
	if _, isString := items[3].(string); !isString {
		t.Fatal("非 dict 项应原样透传")
	}
}

// TestPrePersistLargeMedia_非法base64透传 非法 base64 项保留原样。
func TestPrePersistLargeMedia_非法base64透传(t *testing.T) {
	params := map[string]any{"media_items": []any{
		map[string]any{"type": "image", "mime_type": "image/png", "base64_data": "!!not-base64!!"},
	}}
	prePersistLargeMedia(params, "rw-sess-2", nil)
	item := params["media_items"].([]any)[0].(map[string]any)
	if item["_persisted"] == true {
		t.Fatal("非法 base64 不应标 _persisted")
	}
}

// TestUploadMediaItemViaE2A_不支持mime返回nil 对齐 :2006-2008（suffix 缺失→None）。
func TestUploadMediaItemViaE2A_不支持mime返回nil(t *testing.T) {
	if got := uploadMediaItemViaE2A(
		map[string]any{"mime_type": "image/heic"}, []byte("x"), "s", 0, nil); got != nil {
		t.Fatalf("不支持 mime 应返回 nil: %v", got)
	}
}

// TestUploadMediaItemViaE2A_落盘路径形状 用真实 AgentServer 链路外的直接落盘对拍：
// 经 sendUploadChunk 不可用（ac=nil）时返回 nil；落盘命名与 storeImageItem 一致由
// 集成 e2e（media_gateway_e2e_test.go）覆盖。
func TestUploadMediaItemViaE2A_nil客户端(t *testing.T) {
	if got := uploadMediaItemViaE2A(
		map[string]any{"mime_type": "image/png", "filename": "a.png"}, []byte{1}, "s", 0, nil); got != nil {
		t.Fatalf("ac=nil 应上传失败返回 nil: %v", got)
	}
}

// TestMediaPersist_注册齐全 media.persist/discard + rsi.artifact 三代理已注册。
// Python: app_web_handlers.py:5811-5812/6492-6494。
func TestMediaPersist_注册齐全(t *testing.T) {
	d := RegisterWebHandlers(nil)
	for _, m := range []string{"media.persist", "media.discard",
		"document.persist", "document.formats", "path.select_directory", "path.select_files",
		"file.upload_chunk", "file.import_url",
		"rsi.artifact.download", "rsi.artifact.files.list", "rsi.artifact.files.get"} {
		if _, ok := d.methodHandlers[m]; !ok {
			t.Fatalf("%s 未注册", m)
		}
	}
}

// TestMediaPersist_无客户端回SERVICE_UNAVAILABLE 与薄代理同款缺客户端文案。
func TestMediaPersist_无客户端回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := newTestChannel()
	_, err := callHandler(mediaPersist(wc, nil), map[string]any{}, "sid")
	require.Error(t, err)
	assert.Equal(t, WsErrServiceUnavailable, errorCode(err))
}

// TestMediaDiscard_无客户端回SERVICE_UNAVAILABLE 薄代理缺客户端文案。
func TestMediaDiscard_无客户端回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := newTestChannel()
	_, err := callHandler(sessionProxyForward(wc, nil, "media.discard"), map[string]any{}, "sid")
	require.Error(t, err)
	assert.Equal(t, WsErrServiceUnavailable, errorCode(err))
	assert.Equal(t, "AgentServer is unavailable", err.Error())
}
