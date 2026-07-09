package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

func TestJiuWenClaw_BuildInputs_基本字段提取(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query": "你好世界",
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, paramsJSON)

	inputs, memoryMode, rawQuery := jw.BuildInputs(req)

	assert.Equal(t, "你好世界", rawQuery)
	_ = memoryMode // memoryMode 依赖 config，当前可能为空
	assert.NotNil(t, inputs["query"])
	assert.NotNil(t, inputs["conversation_id"])
	assert.NotNil(t, inputs["channel"])
	assert.NotNil(t, inputs["language"])
}

func TestJiuWenClaw_BuildInputs_projectDir优先级(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query":       "test",
		"project_dir": "/path/from/params",
	}
	paramsJSON, _ := json.Marshal(params)
	metadata := map[string]any{"project_dir": "/path/from/metadata"}
	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodChatSend, paramsJSON,
		schema.WithAgentMetadata(metadata))

	inputs, _, _ := jw.BuildInputs(req)

	// params 优先
	assert.Equal(t, "/path/from/params", inputs["project_dir"])
}

func TestJiuWenClaw_BuildInputs_cron字段转换(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query": "定时任务",
		"cron":  map[string]any{"schedule": "0 9 * * *"},
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-3", "cron", schema.ReqMethodChatSend, paramsJSON)

	inputs, _, _ := jw.BuildInputs(req)

	runVal, hasRun := inputs["run"]
	assert.True(t, hasRun)
	runMap, ok := runVal.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "cron", runMap["kind"])
}

func TestJiuWenClaw_BuildInputs_trustedDirs提取(t *testing.T) {
	jw := NewJiuWenClaw()
	params := map[string]any{
		"query":        "test",
		"trusted_dirs": []any{"/home/user/proj1", "/home/user/proj2"},
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-4", "web", schema.ReqMethodChatSend, paramsJSON)

	inputs, _, _ := jw.BuildInputs(req)

	dirs, ok := inputs["trusted_dirs"].([]string)
	assert.True(t, ok)
	assert.Len(t, dirs, 2)
}
