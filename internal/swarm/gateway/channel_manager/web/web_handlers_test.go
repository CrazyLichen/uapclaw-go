package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeCryptoHandler 用于测试的模拟加密提供者（带 error 签名，对齐 crypto.CryptoProvider）
type fakeCryptoHandler struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeCryptoHandler) Encrypt(_ context.Context, plaintext string) (string, error) {
	return "enc(" + plaintext + ")", nil
}

func (f *fakeCryptoHandler) Decrypt(_ context.Context, ciphertext string) (string, error) {
	return "dec(" + ciphertext + ")", nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewWebChannelMethods(t *testing.T) {
	d := NewWebChannel(WebChannelConfig{}, nil, nil)
	assert.NotNil(t, d)
	assert.Empty(t, d.methodHandlers)
}

func TestWebChannelMethods_Register(t *testing.T) {
	d := NewWebChannel(WebChannelConfig{}, nil, nil)
	called := false
	d.registerMethod("test.method", func(_ wsSender, _ string, _ map[string]any, _ string) {
		called = true
	}, false)
	assert.Len(t, d.methodHandlers, 1)
	assert.False(t, called) // 仅注册，未调用
}

func TestWebChannelMethods_Register_localOnly(t *testing.T) {
	d := NewWebChannel(WebChannelConfig{}, nil, nil)
	d.registerMethod("test.local", func(wsSender, string, map[string]any, string) {}, true)
	assert.True(t, d.localOnlyMethods["test.local"])

	d.unregisterMethod("test.local")
	assert.NotContains(t, d.methodHandlers, "test.local")
	assert.False(t, d.localOnlyMethods["test.local"])
}

func TestWebChannelMethods_Dispatch_正常分发(t *testing.T) {
	d := NewWebChannel(WebChannelConfig{}, nil, nil)
	d.registerMethod("test.echo", func(ws wsSender, reqID string, params map[string]any, _ string) {
		d.sendResponse(ws, reqID, true, params, "", "")
	}, false)

	result, err := dispatch(d, "test.echo", map[string]any{"msg": "hello"}, "sess_1")
	assert.NoError(t, err)
	assert.Equal(t, "hello", result["msg"])
}

func TestWebChannelMethods_Dispatch_方法未找到(t *testing.T) {
	d := NewWebChannel(WebChannelConfig{}, nil, nil)
	_, err := dispatch(d, "nonexistent.method", nil, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未找到")
}

func TestWebChannelMethods_Dispatch_并发安全(t *testing.T) {
	d := NewWebChannel(WebChannelConfig{}, nil, nil)
	d.registerMethod("test.concurrent", func(ws wsSender, reqID string, _ map[string]any, _ string) {
		d.sendResponse(ws, reqID, true, map[string]any{"ok": true}, "", "")
	}, false)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := dispatch(d, "test.concurrent", nil, "")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

func TestWebChannelMethods_Dispatch_handler返回错误(t *testing.T) {
	d := NewWebChannel(WebChannelConfig{}, nil, nil)
	d.registerMethod("test.err", func(ws wsSender, reqID string, _ map[string]any, _ string) {
		d.sendResponse(ws, reqID, false, nil, "handler error", WsErrInternalError)
	}, false)
	result, err := dispatch(d, "test.err", nil, "")
	assert.Error(t, err)
	assert.NotNil(t, result)
}

func TestRegisterWebHandlers_全量注册(t *testing.T) {
	d := RegisterWebHandlers(nil)

	// 验证核心方法已注册
	expectedMethods := []string{
		"config.get", "config.set",
		"models.list", "channel.get",
		"session.list", "session.create",
		"chat.send", "chat.resume", "chat.interrupt", "chat.user_answer",
		"config.save_all", "config.validate_model",
		"models.replace_all", "models.validate",
		"path.get", "path.set",
		"memory.compute",
		"locale.get_conf", "locale.set_conf",
		"heartbeat.get_conf", "heartbeat.set_conf",
		"updater.check", "updater.download", "updater.get_status",
		"hooks.list",
		"permissions.owner_scopes.get", "permissions.owner_scopes.set",
		"memory.forbidden.get", "memory.forbidden.set",
		"initialize",
		"history.get",
		"skills.list", "skills.install",
		"agents.list", "agents.create",
		"schedule.list", "schedule.create",
	}

	for _, method := range expectedMethods {
		_, ok := d.methodHandlers[method]
		assert.True(t, ok, "方法 %q 应已注册", method)
	}
}

func TestHandleChannelGet(t *testing.T) {
	handler := channelGet(newTestChannel(), nil)
	result, err := callHandler(handler, nil, "")
	require.NoError(t, err)

	channels, ok := result["channels"].([]map[string]any)
	require.True(t, ok)
	// 无 ChannelManager 时返回空数组
	assert.Empty(t, channels)
}

func TestHandleSessionList_空目录(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	// 确保会话目录存在
	sessionsDir := workspace.AgentSessionsDir()
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	result, err := callHandler(sessionList(newTestChannel()), nil, "")
	require.NoError(t, err)
	assert.Contains(t, result, "sessions")
	sessionList, ok := result["sessions"].([]map[string]any)
	require.True(t, ok, "sessions 应为 []map[string]any 类型")
	assert.Empty(t, sessionList)
	assert.Contains(t, result, "total")
	assert.Contains(t, result, "limit")
	assert.Contains(t, result, "offset")
}

func TestHandleSessionCreate(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	result, err := callHandler(sessionCreate(newTestChannel()), map[string]any{
		"session_id": "test-create-session",
		"title":      "test-session",
	}, "")
	require.NoError(t, err)
	// session.create 响应不再包含 "ok"（对齐 Python: payload 中无 ok）
	assert.NotContains(t, result, "ok")
	sessionID, ok := result["session_id"].(string)
	require.True(t, ok)
	assert.Equal(t, "test-create-session", sessionID)

	// 验证会话目录已创建
	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, sessionID)
	assert.DirExists(t, sessionDir)
	assert.FileExists(t, filepath.Join(sessionDir, "metadata.json"))
}

func TestHandleSessionCreate_缺少SessionID(t *testing.T) {
	_, err := callHandler(sessionCreate(newTestChannel()), map[string]any{}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrBadRequest, errorCode(err))
}

func TestMakeSessionID_格式(t *testing.T) {
	sid := MakeSessionID()

	// 格式应为 sess_{hex}_{6hex}
	assert.True(t, len(sid) > 10, "session ID 长度应 > 10")
	assert.True(t, len(sid) < 50, "session ID 长度应 < 50")

	// 前缀为 sess_
	parts := splitSessionID(sid)
	assert.Equal(t, "sess", parts[0])
	assert.Len(t, parts, 3) // 三段：sess、hex 时间戳、6 位 hex 后缀

	// 后缀为 6 个 hex 字符
	assert.Len(t, parts[2], 6)
}

func TestMakeSessionID_唯一性(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		sid := MakeSessionID()
		assert.False(t, ids[sid], "session ID 应唯一: %s", sid)
		ids[sid] = true
	}
}

func TestStubHandler(t *testing.T) {
	payload := map[string]any{"status": "ok", "count": 42}
	handler := stubHandler(newTestChannel(), payload)

	result, err := callHandler(handler, map[string]any{"foo": "bar"}, "sess_1")
	assert.NoError(t, err)
	assert.Equal(t, payload, result)
}

func TestChatMethods_Ack响应(t *testing.T) {
	// chat.send 方法
	result, err := callHandler(chatSend(newTestChannel()), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "sess_1", result["session_id"])

	// chat.resume 方法
	result, err = callHandler(chatResume(newTestChannel()), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))

	// chat.interrupt（无 intent 参数时不设置 intent 键）
	result, err = callHandler(chatInterrupt(newTestChannel()), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "sess_1", result["session_id"])

	// chat.interrupt（有 intent 参数时包含 intent）
	result, err = callHandler(chatInterrupt(newTestChannel()), map[string]any{"intent": "cancel"}, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "cancel", result["intent"])

	// chat.user_answer 方法
	result, err = callHandler(chatUserAnswer(newTestChannel()), map[string]any{"request_id": "req_1"}, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "req_1", result["request_id"])

	// chat handlers 仅返回 ack 响应，消息转发由两层架构第一层处理
}

func TestConfigEnvMap_条目数(t *testing.T) {
	assert.Len(t, configEnvMap, 46, "configEnvMap 应有 46 个条目（evolution_auto_scan / skill_create 环境变量映射已随上游收敛删除）")
}

func TestHandleChannelGet_有ChannelManager(t *testing.T) {
	// 测试 nil ChannelManager 返回空
	handler := channelGet(newTestChannel(), nil)
	result, err := callHandler(handler, nil, "")
	require.NoError(t, err)
	channels := result["channels"].([]map[string]any)
	assert.Empty(t, channels)
}

func TestHandleSessionList_有会话目录(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	sessionsDir := workspace.AgentSessionsDir()
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	// 创建一个会话目录含 metadata.json
	sessDir := filepath.Join(sessionsDir, "test-sess-1")
	require.NoError(t, os.MkdirAll(sessDir, 0o755))
	metaData, _ := json.Marshal(map[string]any{"title": "测试会话"})
	require.NoError(t, os.WriteFile(filepath.Join(sessDir, "metadata.json"), metaData, 0o644))

	// 创建一个无 metadata 的会话目录
	sessDir2 := filepath.Join(sessionsDir, "test-sess-2")
	require.NoError(t, os.MkdirAll(sessDir2, 0o755))

	result, err := callHandler(sessionList(newTestChannel()), nil, "")
	require.NoError(t, err)
	sessionList, ok := result["sessions"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, sessionList, 2)
}

func TestHandleSessionCreate_已存在(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	// 先创建
	_, err := callHandler(sessionCreate(newTestChannel()), map[string]any{
		"session_id": "dup-session",
	}, "")
	require.NoError(t, err)

	// 重复创建应报 ALREADY_EXISTS
	_, err = callHandler(sessionCreate(newTestChannel()), map[string]any{
		"session_id": "dup-session",
	}, "")
	require.Error(t, err)
	assert.Equal(t, WsErrAlreadyExists, errorCode(err))
}

func TestHandleSessionCreate_含全部字段(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	result, err := callHandler(sessionCreate(newTestChannel()), map[string]any{
		"session_id": "full-session",
		"channel_id": "web",
		"user_id":    "user1",
		"title":      "测试标题",
		"mode":       "BUILD",
	}, "")
	require.NoError(t, err)
	// session.create 响应不再包含 "ok"（对齐 Python: payload 中无 ok）
	assert.NotContains(t, result, "ok")
}

func TestSortedKeys_空map(t *testing.T) {
	keys := sortedKeys(map[string]string{})
	assert.Empty(t, keys)
}

func TestHandleHistoryGet(t *testing.T) {
	handler := historyGet(newTestChannel())
	result, err := callHandler(handler, nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "sess_1", result["session_id"])
}

func TestHandleHistoryGet_带参数(t *testing.T) {
	handler := historyGet(newTestChannel())
	result, err := callHandler(handler, map[string]any{
		"session_id": "custom-sess",
		"page_idx":   2,
	}, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "custom-sess", result["session_id"])
	assert.Equal(t, 2, result["page_idx"])
}

func TestHandleSessionList_分页(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	sessionsDir := workspace.AgentSessionsDir()
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	// 创建 5 个会话目录
	for i := 0; i < 5; i++ {
		sessDir := filepath.Join(sessionsDir, fmt.Sprintf("sess-%d", i))
		require.NoError(t, os.MkdirAll(sessDir, 0o755))
		metaData, _ := json.Marshal(map[string]any{"title": fmt.Sprintf("会话%d", i)})
		require.NoError(t, os.WriteFile(filepath.Join(sessDir, "metadata.json"), metaData, 0o644))
	}

	// 测试 limit=2, offset=1
	result, err := callHandler(sessionList(newTestChannel()), map[string]any{
		"limit":  2,
		"offset": 1,
	}, "")
	require.NoError(t, err)
	sessionList := result["sessions"].([]map[string]any)
	assert.Len(t, sessionList, 2)
	assert.Equal(t, 5, result["total"])
	assert.Equal(t, 2, result["limit"])
	assert.Equal(t, 1, result["offset"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitSessionID 拆分 session ID 的各部分。
func splitSessionID(sid string) []string {
	var parts []string
	start := 0
	for i, c := range sid {
		if c == '_' {
			parts = append(parts, sid[start:i])
			start = i + 1
		}
	}
	parts = append(parts, sid[start:])
	return parts
}

// TestStrOrEmpty 测试 strOrEmpty 转换
func TestStrOrEmpty(t *testing.T) {
	assert.Equal(t, "", strOrEmpty(nil))
	assert.Equal(t, "hello", strOrEmpty("hello"))
	assert.Equal(t, "42", strOrEmpty(42))
	assert.Equal(t, "", strOrEmpty((*string)(nil)))
	assert.Equal(t, "3.14", strOrEmpty(3.14))
}

// fakeCryptoFailDecrypt Decrypt 恒失败的 fake（测试解密失败保留原值语义）
type fakeCryptoFailDecrypt struct{}

func (f *fakeCryptoFailDecrypt) Encrypt(_ context.Context, plaintext string) (string, error) {
	return "enc(" + plaintext + ")", nil
}

func (f *fakeCryptoFailDecrypt) Decrypt(_ context.Context, ciphertext string) (string, error) {
	return "", errors.New("decrypt boom")
}
