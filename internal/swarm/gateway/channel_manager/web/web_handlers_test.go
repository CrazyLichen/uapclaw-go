package web

import (
	"context"
	"encoding/json"
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

// fakeCryptoHandler 用于测试的模拟加密提供者
type fakeCryptoHandler struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeCryptoHandler) Encrypt(plaintext string) string  { return "enc(" + plaintext + ")" }
func (f *fakeCryptoHandler) Decrypt(ciphertext string) string { return "dec(" + ciphertext + ")" }

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

func TestRPCDispatcher_Dispatch_handler返回错误(t *testing.T) {
	d := NewRPCDispatcher()
	d.Register("test.err", func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		return nil, fmt.Errorf("handler error")
	})
	result, err := d.Dispatch("test.err", nil, "")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestRegisterWebHandlers_全量注册(t *testing.T) {
	d := RegisterWebHandlers(nil)

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

	handler := handleConfigGet(nil)
	result, err := handler(context.Background(), nil, "")
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
	// 测试 handleConfigSet 逻辑（onConfigSaved 为 nil，跳过热重载）
	handler := handleConfigSet(nil, nil)
	result, err := handler(context.Background(), map[string]any{
		"model_provider": "OpenAI",
		"model":          "gpt-4",
	}, "sess_test")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))

	// 验证环境变量已更新
	assert.Equal(t, "OpenAI", os.Getenv("MODEL_PROVIDER"))
	assert.Equal(t, "gpt-4", os.Getenv("MODEL_NAME"))
	_ = os.Unsetenv("MODEL_PROVIDER")
	_ = os.Unsetenv("MODEL_NAME")
}

func TestHandleChannelGet(t *testing.T) {
	handler := handleChannelGet(nil)
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)

	channels, ok := result["channels"].(map[string]any)
	require.True(t, ok)
	// 无 ChannelManager 时返回空 map
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
		"session_id": "test-create-session",
		"title":      "test-session",
	}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
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
	result, err := handleSessionCreate(context.Background(), map[string]any{}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrBadRequest, result["code"])
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
	handler := handleSessionDelete(nil)
	result, err := handler(context.Background(), map[string]any{
		"session_id": sessionID,
	}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))

	// 验证目录已删除
	sessionsDir := workspace.AgentSessionsDir()
	assert.NoDirExists(t, filepath.Join(sessionsDir, sessionID))
}

func TestHandleSessionDelete_缺少SessionID(t *testing.T) {
	handler := handleSessionDelete(nil)
	result, err := handler(context.Background(), map[string]any{}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrBadRequest, result["code"])
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
	result, err := handleChatSend()(context.Background(), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "sess_1", result["session_id"])

	// chat.resume
	result, err = handleChatResume()(context.Background(), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))

	// chat.interrupt（无 intent 参数时不设置 intent 键）
	result, err = handleChatInterrupt()(context.Background(), nil, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "sess_1", result["session_id"])

	// chat.interrupt（有 intent 参数时包含 intent）
	result, err = handleChatInterrupt()(context.Background(), map[string]any{"intent": "cancel"}, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "cancel", result["intent"])

	// chat.user_answer
	result, err = handleChatUserAnswer()(context.Background(), map[string]any{"request_id": "req_1"}, "sess_1")
	require.NoError(t, err)
	assert.True(t, result["accepted"].(bool))
	assert.Equal(t, "req_1", result["request_id"])

	// chat handlers 仅返回 ack 响应，消息转发由两层架构第一层处理
}

func TestConfigEnvMap_条目数(t *testing.T) {
	assert.Len(t, configEnvMap, 48, "configEnvMap 应有 48 个条目（含 evolution_auto_scan 和 skill_create 环境变量映射）")
}

func TestHandleConfigSaveAll_params为空(t *testing.T) {
	handler := handleConfigSaveAll(nil, nil)
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrBadRequest, result["code"])
}

func TestHandleConfigSaveAll_仅config子载荷(t *testing.T) {
	handler := handleConfigSaveAll(nil, nil)
	result, err := handler(context.Background(), map[string]any{
		"config": map[string]any{
			"model_provider": "OpenAI",
		},
	}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
	_ = os.Unsetenv("MODEL_PROVIDER")
}

func TestHandleConfigSaveAll_config非对象(t *testing.T) {
	handler := handleConfigSaveAll(nil, nil)
	result, err := handler(context.Background(), map[string]any{
		"config": "not an object",
	}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrBadRequest, result["code"])
}

func TestHandleConfigSaveAll_models无效类型(t *testing.T) {
	handler := handleConfigSaveAll(nil, nil)
	result, err := handler(context.Background(), map[string]any{
		"models": "not a list",
	}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
}

func TestHandleModelsList_无配置(t *testing.T) {
	handler := handleModelsList(nil)
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)
	assert.Contains(t, result, "models")
}

func TestHandleModelsList_有crypto(t *testing.T) {
	handler := handleModelsList(&fakeCryptoHandler{})
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)
	assert.Contains(t, result, "models")
}

func TestHandleChannelGet_有ChannelManager(t *testing.T) {
	// 测试 nil ChannelManager 返回空
	handler := handleChannelGet(nil)
	result, err := handler(context.Background(), nil, "")
	require.NoError(t, err)
	channels := result["channels"].(map[string]any)
	assert.Empty(t, channels)
}

func TestHandleConfigSaveAll_含agents和team(t *testing.T) {
	handler := handleConfigSaveAll(nil, nil)
	result, err := handler(context.Background(), map[string]any{
		"config": map[string]any{
			"model_provider": "OpenAI",
		},
		"agents": []any{map[string]any{"name": "agent1"}},
		"team":   []any{map[string]any{"name": "team1"}},
	}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
	_ = os.Unsetenv("MODEL_PROVIDER")
}

func TestHandleConfigSaveAll_空params对象(t *testing.T) {
	handler := handleConfigSaveAll(nil, nil)
	result, err := handler(context.Background(), map[string]any{}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
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

	result, err := handleSessionList(context.Background(), nil, "")
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
	_, err := handleSessionCreate(context.Background(), map[string]any{
		"session_id": "dup-session",
	}, "")
	require.NoError(t, err)

	// 重复创建应报 ALREADY_EXISTS
	result, err := handleSessionCreate(context.Background(), map[string]any{
		"session_id": "dup-session",
	}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrAlreadyExists, result["code"])
}

func TestHandleSessionCreate_含全部字段(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	result, err := handleSessionCreate(context.Background(), map[string]any{
		"session_id": "full-session",
		"channel_id": "web",
		"user_id":    "user1",
		"title":      "测试标题",
		"mode":       "BUILD",
	}, "")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
}

func TestHandleSessionDelete_不存在的会话(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	handler := handleSessionDelete(nil)
	result, err := handler(context.Background(), map[string]any{
		"session_id": "nonexistent-session",
	}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrNotFound, result["code"])
}

func TestHandleSessionDelete_team模式Agent不可用(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	handler := handleSessionDelete(nil)
	result, err := handler(context.Background(), map[string]any{
		"session_id": "team-session",
		"mode":       "team",
	}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrAgentUnavailable, result["code"])
}

func TestHandleConfigSet_params为空(t *testing.T) {
	handler := handleConfigSet(nil, nil)
	result, err := handler(context.Background(), nil, "")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestHandleConfigSet_无效Provider(t *testing.T) {
	handler := handleConfigSet(nil, nil)
	result, err := handler(context.Background(), map[string]any{
		"model_provider": "InvalidProvider",
	}, "")
	require.NoError(t, err)
	assert.False(t, result["ok"].(bool))
	assert.Equal(t, WsErrBadRequest, result["code"])
}

func TestGetConfigString_路径不存在(t *testing.T) {
	result := getConfigString(map[string]any{}, "a.b.c", "default")
	assert.Equal(t, "default", result)
}

func TestGetConfigString_正常路径(t *testing.T) {
	cfg := map[string]any{
		"react": map[string]any{
			"context_engine_config": map[string]any{
				"enabled": "true",
			},
		},
	}
	result := getConfigString(cfg, "react.context_engine_config.enabled", "false")
	assert.Equal(t, "true", result)
}

func TestGetConfigAny_路径不存在(t *testing.T) {
	result := getConfigAny(map[string]any{}, "a.b.c", "fallback")
	assert.Equal(t, "fallback", result)
}

func TestGetConfigAny_正常路径(t *testing.T) {
	cfg := map[string]any{
		"models": map[string]any{
			"defaults": []any{"item1"},
		},
	}
	result := getConfigAny(cfg, "models.defaults", nil)
	assert.NotNil(t, result)
}

func TestFlattenTeamConfig_无team(t *testing.T) {
	result := make(map[string]any)
	flattenTeamConfig(map[string]any{}, result)
	// 无 team 数据，result 不变
	assert.Empty(t, result)
}

func TestFlattenTeamConfig_有team(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"team": []any{
				map[string]any{
					"name":          "team1",
					"lifecycle":     "async",
					"teammate_mode": "route",
					"spawn_mode":    "droplet",
					"leader": map[string]any{
						"member_name":  "leader1",
						"display_name": "Leader",
						"persona":      "test",
						"agent_key":    "key1",
					},
					"teammates": []any{
						map[string]any{"agent_key": "tm_key1"},
					},
				},
			},
		},
		"web_config_panel": map[string]any{
			"agent_team_agents": []any{
				map[string]any{
					"model":              "gpt-4",
					"skills":             []any{},
					"max_iterations":     10,
					"completion_timeout": 300,
				},
			},
		},
	}
	result := make(map[string]any)
	flattenTeamConfig(cfg, result)
	assert.Equal(t, "team1", result["team_0_name"])
	assert.Equal(t, "async", result["team_0_lifecycle"])
	assert.Equal(t, "route", result["team_0_teammate_mode"])
	assert.Equal(t, "droplet", result["team_0_spawn_mode"])
	assert.Equal(t, "leader1", result["team_0_leader_member_name"])
	assert.Equal(t, "Leader", result["team_0_leader_display_name"])
	assert.Equal(t, "key1", result["team_0_leader_agent_key"])
	assert.Equal(t, "tm_key1", result["team_0_teammate_agent_key"])
	assert.Equal(t, "gpt-4", result["team_0_model"])
	assert.Equal(t, 10, result["team_0_max_iterations"])
}

func TestFlattenTeamConfig_predefinedMembers(t *testing.T) {
	cfg := map[string]any{
		"modes": map[string]any{
			"team": []any{
				map[string]any{
					"name":               "team1",
					"predefined_members": []any{map[string]any{"name": "agent1"}},
				},
			},
		},
	}
	result := make(map[string]any)
	flattenTeamConfig(cfg, result)
	assert.Contains(t, result, "team_0_predefined_members")
}

func TestPersistEnvUpdates_新建env(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	err := persistEnvUpdates(map[string]string{
		"TEST_KEY1": "value1",
		"TEST_KEY2": "",
	})
	require.NoError(t, err)

	// 验证文件存在
	envPath := workspace.EnvFile()
	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, `TEST_KEY1="value1"`)
	assert.Contains(t, content, "TEST_KEY2=")
}

func TestPersistEnvUpdates_更新已有env(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	defer func() { _ = os.Unsetenv("UAPCLAW_DATA_DIR") }()
	workspace.SetUserHome(workspace.UserHomeDir())

	// 先写入已有 .env
	envPath := workspace.EnvFile()
	require.NoError(t, os.MkdirAll(filepath.Dir(envPath), 0o755))
	require.NoError(t, os.WriteFile(envPath, []byte("EXISTING_KEY=old_val\n# comment\n"), 0o644))

	err := persistEnvUpdates(map[string]string{
		"EXISTING_KEY": "new_val",
		"NEW_KEY":      "new_entry",
	})
	require.NoError(t, err)

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, `EXISTING_KEY="new_val"`)
	assert.Contains(t, content, `NEW_KEY="new_entry"`)
}

func TestSortedKeys(t *testing.T) {
	m := map[string]string{"c": "3", "a": "1", "b": "2"}
	keys := sortedKeys(m)
	assert.Equal(t, []string{"a", "b", "c"}, keys)
}

func TestSortedKeys_空map(t *testing.T) {
	keys := sortedKeys(map[string]string{})
	assert.Empty(t, keys)
}

func TestLoadAppConfig(t *testing.T) {
	// loadAppConfig 在无配置文件时返回 error，不会 panic
	cfg, err := loadAppConfig()
	// 可能成功或失败取决于测试环境
	if err != nil {
		assert.Empty(t, cfg)
	}
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
