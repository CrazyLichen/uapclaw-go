package web

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewRPCDispatcher(t *testing.T) {
	d := NewRPCDispatcher()
	assert.NotNil(t, d)
	assert.Empty(t, d.handlers)
}

func TestRPCDispatcher_Register(t *testing.T) {
	d := NewRPCDispatcher()
	called := false
	d.Register("test.method", func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		called = true
		return map[string]any{"ok": true}, nil
	})
	assert.Len(t, d.handlers, 1)
	assert.False(t, called) // 仅注册，未调用
}

func TestRPCDispatcher_Dispatch_正常分发(t *testing.T) {
	d := NewRPCDispatcher()
	d.Register("test.echo", func(_ context.Context, params map[string]any, _ string) (map[string]any, error) {
		return params, nil
	})

	result, err := d.Dispatch("test.echo", map[string]any{"msg": "hello"}, "sess_1")
	assert.NoError(t, err)
	assert.Equal(t, "hello", result["msg"])
}

func TestRPCDispatcher_Dispatch_方法未找到(t *testing.T) {
	d := NewRPCDispatcher()
	_, err := d.Dispatch("nonexistent.method", nil, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "未找到")
}

func TestRPCDispatcher_Dispatch_并发安全(t *testing.T) {
	d := NewRPCDispatcher()
	d.Register("test.concurrent", func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := d.Dispatch("test.concurrent", nil, "")
			assert.NoError(t, err)
			assert.True(t, result["ok"].(bool))
		}()
	}
	wg.Wait()
}

func TestNewAppRPCHandlers_全量注册(t *testing.T) {
	d := NewAppRPCHandlers(nil, nil)

	// 验证核心方法已注册
	expectedMethods := []string{
		"config.get", "config.set",
		"models.list", "channel.get",
		"session.list", "session.create", "session.delete",
		"chat.send", "chat.resume", "chat.interrupt", "chat.user_answer",
		"config.save_all", "config.validate_model",
		"models.replace_all", "models.validate",
		"session.switch",
		"path.get", "path.set",
		"memory.compute",
		"locale.get_conf", "locale.set_conf",
		"heartbeat.get_conf", "heartbeat.set_conf", "heartbeat.get_path",
		"updater.check", "updater.download", "updater.install", "updater.cancel", "updater.get_status",
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
		_, ok := d.handlers[method]
		assert.True(t, ok, "方法 %q 应已注册", method)
	}
}

func TestHandleConfigGet(t *testing.T) {
	// 设置测试环境变量
	_ = os.Setenv("MODEL_PROVIDER", "openai")
	_ = os.Setenv("MODEL_NAME", "gpt-4")
	defer func() {
		_ = os.Unsetenv("MODEL_PROVIDER")
		_ = os.Unsetenv("MODEL_NAME")
	}()

	result, err := handleConfigGet(context.Background(), nil, "")
	require.NoError(t, err)

	// 验证环境变量映射
	assert.Equal(t, "openai", result["model_provider"])
	assert.Equal(t, "gpt-4", result["model"])

	// 验证 app_version 存在
	assert.Contains(t, result, "app_version")

	// 验证 config.yaml 补充字段存在
	assert.Contains(t, result, "context_engine_enabled")
	assert.Contains(t, result, "permissions_enabled")
	assert.Contains(t, result, "memory_forbidden_enabled")
}

func TestHandleConfigSet(t *testing.T) {
	// 测试 handleConfigSet 逻辑
	handler := handleConfigSet(nil)
	result, err := handler(context.Background(), map[string]any{
		"model_provider": "anthropic",
	}, "sess_test")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))

	// 验证环境变量已更新
	assert.Equal(t, "anthropic", os.Getenv("MODEL_PROVIDER"))
	_ = os.Unsetenv("MODEL_PROVIDER")
}

func TestHandleChannelGet(t *testing.T) {
	result, err := handleChannelGet(context.Background(), nil, "")
	require.NoError(t, err)

	channels, ok := result["channels"].(map[string]any)
	require.True(t, ok)
	web, ok := channels["web"].(map[string]any)
	require.True(t, ok)
	assert.True(t, web["enabled"].(bool))
}

func TestHandleSessionList_空目录(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	// 确保会话目录存在
	sessionsDir := workspace.AgentSessionsDir()
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))

	result, err := handleSessionList(context.Background(), nil, "")
	require.NoError(t, err)
	assert.Contains(t, result, "sessions")
	sessionList, ok := result["sessions"].([]map[string]any)
	require.True(t, ok, "sessions 应为 []map[string]any 类型")
	assert.Empty(t, sessionList)
}

func TestHandleSessionCreate(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	result, err := handleSessionCreate(context.Background(), map[string]any{
		"name": "test-session",
	}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
	sessionID, ok := result["session_id"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, sessionID)

	// 验证会话目录已创建
	sessionsDir := workspace.AgentSessionsDir()
	sessionDir := filepath.Join(sessionsDir, sessionID)
	assert.DirExists(t, sessionDir)
	assert.FileExists(t, filepath.Join(sessionDir, "metadata.json"))
}

func TestHandleSessionDelete(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	// 先创建会话
	createResult, err := handleSessionCreate(context.Background(), map[string]any{
		"session_id": "test-del-session",
	}, "")
	require.NoError(t, err)
	sessionID := createResult["session_id"].(string)

	// 删除会话
	result, err := handleSessionDelete(context.Background(), map[string]any{
		"session_id": sessionID,
	}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))

	// 验证目录已删除
	sessionsDir := workspace.AgentSessionsDir()
	assert.NoDirExists(t, filepath.Join(sessionsDir, sessionID))
}

func TestHandleSessionDelete_缺少SessionID(t *testing.T) {
	_, err := handleSessionDelete(context.Background(), map[string]any{}, "")
	assert.Error(t, err)
}

func TestMakeSessionID_格式(t *testing.T) {
	sid := MakeSessionID()

	// 格式应为 sess_{hex}_{6hex}
	assert.True(t, len(sid) > 10, "session ID 长度应 > 10")
	assert.True(t, len(sid) < 50, "session ID 长度应 < 50")

	// 前缀为 sess_
	parts := splitSessionID(sid)
	assert.Equal(t, "sess", parts[0])
	assert.Len(t, parts, 3) // sess, hex_ts, 6hex

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
	handler := stubHandler("test", payload)

	result, err := handler(context.Background(), map[string]any{"foo": "bar"}, "sess_1")
	assert.NoError(t, err)
	assert.Equal(t, payload, result)
}

func TestChatMethods_Ack响应(t *testing.T) {
	// chat.send
	result, err := handleChatSend(nil)(context.Background(), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "sess_1", result["session_id"])

	// chat.resume
	result, err = handleChatResume(nil)(context.Background(), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))

	// chat.interrupt
	result, err = handleChatInterrupt(nil)(context.Background(), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "interrupt", result["intent"])

	// chat.user_answer
	result, err = handleChatUserAnswer(nil)(context.Background(), map[string]any{"request_id": "req_1"}, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "req_1", result["request_id"])

	// chat handlers 现在通过 OnMessage 回调转发，不再发送模拟事件
}

func TestConfigEnvMap_条目数(t *testing.T) {
	assert.Len(t, configEnvMap, 48, "configEnvMap 应有 48 个条目（含 evolution_auto_scan 和 skill_create 环境变量映射）")
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
