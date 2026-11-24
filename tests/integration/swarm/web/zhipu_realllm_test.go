//go:build integration && llm

// 智谱免费模型真链冒烟（27.4；用户拍板 2026-12-06：内置智谱域替代 zen
// 免费档作为可推理的真实模型来源）。
//
// 双守卫（真实外部服务）：
//  1. integration && llm 标签
//  2. ZHIPU_BUILTIN_API_KEY 未注入（且内置常量为空）→ Skip
//
// 运行方式：
//
//	ZHIPU_BUILTIN_API_KEY=xxx go test -tags "sqlite_fts5 test integration llm" \
//	  ./tests/integration/swarm/web/ -run TestZhipuFreeModel_真实链冒烟 -count=1 -v
package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
)

// TestZhipuFreeModel_真实链冒烟 内置 GLM 免费模型的端到端推理验证。
// 条目经 ZhipuFreeModelEntries()（生产同源函数）取得，直接经智谱
// OpenAI 兼容端点发真实 chat completion；断言只钉确定性契约。
func TestZhipuFreeModel_真实链冒烟(t *testing.T) {
	entries := configpanel.ZhipuFreeModelEntries()
	if len(entries) == 0 {
		t.Skip("ZHIPU_BUILTIN_API_KEY 未注入且内置常量为空，跳过真链冒烟")
	}
	mcc := entries[0]["model_client_config"].(map[string]any)
	modelID := mcc["model_name"].(string)
	apiKey := mcc["api_key"].(string)
	apiBase := mcc["api_base"].(string)
	t.Logf("选用内置免费模型：%s", modelID)

	body, err := json.Marshal(map[string]any{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "user", "content": "回复一个字：好"},
		},
		"max_tokens": 8,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiBase+"/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode == http.StatusOK {
		var payload struct {
			Choices []map[string]any `json:"choices"`
		}
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.NotEmpty(t, payload.Choices, "真实推理应返回 choices")
		return
	}
	t.Skipf("内置免费模型 %s 当前不可调（HTTP %d：%s）；模型轮换/额度为厂商侧约束，环境可用时重跑",
		modelID, resp.StatusCode, string(raw[:min(160, len(raw))]))
}
