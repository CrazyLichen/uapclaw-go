package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestHandleCommandModel_AddModel add_model 分支。
func TestHandleCommandModel_AddModel(t *testing.T) {
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"add_model","target":"  qwen-max  "}`))

	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "model_added", resp.Payload["type"])
	assert.Equal(t, "qwen-max", resp.Payload["name"], "target 应去首尾空格")
}

// TestHandleCommandModel_SwitchModel_空EnvUpdates 复刻 Python 的 ok=false。
func TestHandleCommandModel_SwitchModel_空EnvUpdates(t *testing.T) {
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"switch_model","model":"m2"}`))

	require.NoError(t, err)
	assert.False(t, resp.OK, "无 env_updates 应 ok=false（Python 不是抛异常）")
	assert.Equal(t, "No env_updates provided", resp.Payload["error"])
}

// TestHandleCommandModel_SwitchModel_正常 写入环境变量并回 current。
func TestHandleCommandModel_SwitchModel_正常(t *testing.T) {
	t.Setenv("MODEL_NAME", "m2")
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"switch_model","model":"m2","env_updates":{"MODEL_NAME":"m2"}}`))

	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "m2", resp.Payload["current"])
	assert.Equal(t, "m2", resp.Payload["requested"])
	assert.Equal(t, "switched", resp.Payload["type"])
	assert.Equal(t, true, resp.Payload["applied"])
	assert.Equal(t, "m2", getenvOr("MODEL_NAME", ""), "os.environ 应已写入")
}

// TestHandleCommandModel_SwitchModel_非字符串Env 复刻 Python os.environ TypeError。
func TestHandleCommandModel_SwitchModel_非字符串Env(t *testing.T) {
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"switch_model","model":"m2","env_updates":{"TIMEOUT":30}}`))

	require.NoError(t, err)
	assert.False(t, resp.OK, "非字符串 env 值必须失败（Python TypeError 路径）")
	errText, _ := resp.Payload["error"].(string)
	assert.Contains(t, errText, "str expected")
}

// TestHandleCommandModel_缺省Action 返回当前模型与可用列表。
func TestHandleCommandModel_缺省Action(t *testing.T) {
	t.Setenv("MODEL_NAME", "m-cur")
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(), newModelRequest(t, `{}`))

	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "m-cur", resp.Payload["current"])
	assert.Equal(t, []string{"default-model"}, resp.Payload["available"])
}

// TestHandleCommandModel_ModelName未设置 current 回退 unknown。
// Python os.getenv("MODEL_NAME","unknown") 的 default 只在**未设置**时生效，
// 已设为空串时返回空串，因此这里必须真正 Unsetenv。
func TestHandleCommandModel_ModelName未设置(t *testing.T) {
	prev, had := os.LookupEnv("MODEL_NAME")
	require.NoError(t, os.Unsetenv("MODEL_NAME"))
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("MODEL_NAME", prev)
		}
	})

	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(), newModelRequest(t, `{}`))

	require.NoError(t, err)
	assert.Equal(t, "unknown", resp.Payload["current"])
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestMaskModelEnvForLog API_KEY 掩码。
func TestMaskModelEnvForLog(t *testing.T) {
	in := map[string]any{"API_KEY": "sk-secret", "MODEL_NAME": "m"}
	out := maskModelEnvForLog(in)
	assert.Equal(t, "***", out["API_KEY"])
	assert.Equal(t, "m", out["MODEL_NAME"])
	assert.Equal(t, "sk-secret", in["API_KEY"], "不得修改入参")
}

// newModelRequest 构造 command.model 请求。
func newModelRequest(t *testing.T, params string) *schema.AgentRequest {
	t.Helper()
	return &schema.AgentRequest{
		RequestID: "r-model-1",
		ChannelID: "c-model",
		Params:    json.RawMessage(params),
	}
}
