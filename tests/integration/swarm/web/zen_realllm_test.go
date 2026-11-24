//go:build integration && llm

// zen 免费模型真链冒烟（27.4）。
//
// 用户拍板（2026-12-06）：zen 开关改读 models.enable_free_models 后，
// 免费模型成为「无需付费 API key 的真实模型来源」。本测试验证真链：
// 真实拉取 models.dev 目录 + Zen 实时目录求交 → 缓存免费条目 →
// 经 Zen OpenAI 兼容端点（匿名 bearer "public"）发真实 chat completion。
//
// 双守卫（真实外部服务，无法 mock）：
//  1. 文件级 integration && llm 标签
//  2. 运行时守卫：无免费模型 / RegionError / FreeUsageLimit / 模型下线
//     → Skip 并打印 zenLastFetchError（匿名额度与地域封锁是服务端硬约束）
//
// 运行方式：
//
//	go test -tags "sqlite_fts5 test integration llm" \
//	  ./tests/integration/swarm/web/ -run TestZenFreeModel_真实链冒烟 -count=1 -v
package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestZenFreeModel_真实链冒烟 免费模型可用时的真实推理链路。
// 断言只钉确定性契约：缓存非空 + 目录条目形状 + 真实响应含 choices；
// 不断言具体模型行为（同一提示词输出不可复现）。
func TestZenFreeModel_真实链冒烟(t *testing.T) {
	// 隔离数据目录（先建 config 目录再 ResetCache，见 goal_contract_helpers_test 铁律）
	dataDir := t.TempDir()
	t.Setenv(pathutil.EnvDataDir, dataDir)
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "config"), 0o755))
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	// 写开关：用户拍板后 zen 由 models.enable_free_models 驱动
	fixture := "models:\n  enable_free_models: true\n"
	require.NoError(t, os.WriteFile(pathutil.ConfigFile(), []byte(fixture), 0o644))

	// 1. 预热（真实网络：models.dev + Zen /models）
	configpanel.WarmZenFreeModels("realllm-smoke", false)
	entries := configpanel.GetZenFreeModelEntries()
	if len(entries) == 0 {
		reason := configpanel.ZenLastFetchError()
		if reason == "" {
			reason = "免费目录为空（可能：模型轮换下线 / 地域封锁 RegionError / 匿名额度 FreeUsageLimitError）"
		}
		t.Skipf("当前环境无可用免费模型，跳过真链冒烟：%s", reason)
	}
	require.Contains(t, entries[0], "model_client_config", "缓存条目应为 models.defaults 形状")
	mcc := entries[0]["model_client_config"].(map[string]any)
	modelID := mcc["model_name"].(string)
	wantKey := configpanel.ZenAnonAPIKey
	if os.Getenv("OPENCODE_ZEN_API_KEY") != "" {
		wantKey = os.Getenv("OPENCODE_ZEN_API_KEY")
	}
	require.Equal(t, wantKey, fmt.Sprintf("%v", mcc["api_key"]), "缓存条目 bearer 应与开关解析键一致")
	t.Logf("选用免费模型：%s", modelID)

	// 2. 真实 chat completion（Zen OpenAI 兼容端点）
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
		configpanel.ZenAPIBase+"/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+configpanel.ZenAPIKey())
	req.Header.Set("Content-Type", "application/json")
	for k, v := range configpanel.ZenClientHeaders() {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode == http.StatusOK {
		// 确定性契约：OpenAI 兼容响应面
		var payload struct {
			Choices []map[string]any `json:"choices"`
		}
		require.NoError(t, json.Unmarshal(raw, &payload))
		require.NotEmpty(t, payload.Choices, "真实推理应返回 choices")
		t.Logf("真实响应：%s", strings.TrimSpace(string(raw))[:min(120, len(raw))])
		return
	}
	// 免费档失败（额度/地域/下线，与账号无关——服务端对第三方客户端限额）：
	// 注入过个人 key（OPENCODE_ZEN_API_KEY）时兜底尝试付费模型（按量计费）。
	freeErr := strings.TrimSpace(string(raw))
	if key := os.Getenv("OPENCODE_ZEN_API_KEY"); key != "" {
		for _, paid := range []string{"glm-5.3-flash", "deepseek-v4.1-flash"} {
			pbody, perr := json.Marshal(map[string]any{
				"model": paid,
				"messages": []map[string]string{
					{"role": "user", "content": "回复一个字：好"},
				},
				"max_tokens": 8,
			})
			require.NoError(t, perr)
			preq, perr := http.NewRequestWithContext(ctx, http.MethodPost,
				configpanel.ZenAPIBase+"/chat/completions", bytes.NewReader(pbody))
			require.NoError(t, perr)
			preq.Header.Set("Authorization", "Bearer "+key)
			preq.Header.Set("Content-Type", "application/json")
			for k, v := range configpanel.ZenClientHeaders() {
				preq.Header.Set(k, v)
			}
			presp, perr := http.DefaultClient.Do(preq)
			require.NoError(t, perr)
			praw, _ := io.ReadAll(presp.Body)
			_ = presp.Body.Close()
			if presp.StatusCode == http.StatusOK {
				var ppayload struct {
					Choices []map[string]any `json:"choices"`
				}
				require.NoError(t, json.Unmarshal(praw, &ppayload))
				require.NotEmpty(t, ppayload.Choices, "付费模型真实推理应返回 choices")
				t.Logf("免费档 %s 限额，付费兜底 %s 成功", modelID, paid)
				return
			}
			if strings.Contains(string(praw), "Insufficient balance") {
				t.Skipf("免费档 %s 限额 + 付费模型余额不足（Zen 按量：充值少量 credits 或订 Go $10/月后重跑）", modelID)
			}
		}
	}
	t.Skipf("免费模型 %s 当前不可调（HTTP %d：%s）；额度与地域为服务端硬约束，环境可用时重跑",
		modelID, resp.StatusCode, freeErr[:min(160, len(freeErr))])
}
