//go:build integration && llm

package adapter_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// TestPlanMode_真实LLM_写入非Plan文件被拒 用真实 LLM 验证规划模式的写入拦截：
// 模型在明确指令下调用 write_file 写非 plan 路径，应被 AgentModeRail 段 3d
// 路径校验拒绝（工具结果含「仅能用于计划文件」）。
//
// 背景：MockLLM 用例无法覆盖「真实模型是否会绕过约束」；用户实测反馈
// write 成功，本用例是端到端裁决。注意 bash 写文件不受路径校验是
// Python 同款设计边界，本用例只断言 write_file 的拦截。
//
// 运行方式:
//
//	LLM_API_KEY=ark-xxx go test -tags "sqlite_fts5 test integration llm" \
//	  ./tests/integration/swarm/server/adapter/ -run TestPlanMode_真实LLM -count=1 -v
//
// 可选环境变量：LLM_API_BASE（默认 ark coding v3）、LLM_MODEL_NAME（默认 minimax-m3）。
func TestPlanMode_真实LLM_写入非Plan文件被拒(t *testing.T) {
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		t.Skip("需要设置 LLM_API_KEY 环境变量，跳过真实 LLM 测试")
	}
	apiBase := envOrDefaultRealLLM("LLM_API_BASE", "https://ark.cn-beijing.volces.com/api/coding/v3")
	modelName := envOrDefaultRealLLM("LLM_MODEL_NAME", "minimax-m3")

	env := testhelpers.NewAdapterTestEnv(t, mockllm.NewMockModelClient(), "real_llm_plan_write")
	writeRealConfigYAML(t, env.DataDir, apiKey, apiBase, modelName)

	d := adapter.NewDeepAdapter()
	if err := d.CreateInstance(t.Context(), map[string]any{}, "agent.plan", ""); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
	t.Cleanup(func() { _ = d.Cleanup() })

	sessionID := "sess_real_llm_plan_write"
	params, err := json.Marshal(map[string]any{"mode": "agent.plan"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	sid := sessionID
	req := &schema.AgentRequest{
		RequestID: "req-real-llm-plan-write",
		ChannelID: "web",
		SessionID: &sid,
		Params:    params,
		IsStream:  true,
	}
	chunkCh, streamErr := d.ProcessMessageStreamImpl(t.Context(), req, map[string]any{
		"query":           "请把字符串 hello 写入文件 /tmp/plan-escape-check.txt。直接执行，不要询问。",
		"mode":            "agent.plan",
		"conversation_id": sessionID,
	})
	if streamErr != nil {
		t.Fatalf("ProcessMessageStreamImpl: %v", streamErr)
	}

	// 收集全部工具调用（名称 + 结果），真实模型的绕行路径也需要观测
	type toolCall struct {
		name   string
		result string
	}
	calls := make([]toolCall, 0, 8)
	var eventTypes []string
	for chunk := range chunkCh {
		if chunk == nil || chunk.Payload == nil {
			continue
		}
		eventType, _ := chunk.Payload["event_type"].(string)
		eventTypes = append(eventTypes, eventType)
		if eventType == "chat.error" {
			t.Logf("chat.error: %v", chunk.Payload)
			continue
		}
		if eventType != "chat.tool_result" {
			continue
		}
		name, _ := chunk.Payload["tool_name"].(string)
		result, _ := chunk.Payload["result"].(string)
		calls = append(calls, toolCall{name, result})
		t.Logf("工具调用 %s → %s", name, truncateStr(result, 200))
	}

	writeIdx := -1
	for i, c := range calls {
		if c.name == "write_file" {
			writeIdx = i
			break
		}
	}

	if writeIdx >= 0 {
		// 模型走了 write_file：必须被路径校验拒绝，且目标文件不存在
		if !strings.Contains(calls[writeIdx].result, "仅能用于计划文件") {
			t.Fatalf("write_file 写非 plan 文件未被拒绝！工具结果: %q", calls[writeIdx].result)
		}
		if _, statErr := os.Stat("/tmp/plan-escape-check.txt"); statErr == nil {
			t.Errorf("/tmp/plan-escape-check.txt 不应存在（write_file 应在执行前被拦截）")
		}
		t.Logf("裁决：write_file 被正确拒绝（段 3d 生效）")
		return
	}

	// 模型没走 write_file：查是否有 bash 写成功（设计边界：bash 只拦 git 写）
	bashWriteOK := false
	for _, c := range calls {
		if c.name == "bash" && !strings.Contains(c.result, "AgentModeRail") {
			bashWriteOK = true
		}
	}
	if _, statErr := os.Stat("/tmp/plan-escape-check.txt"); statErr == nil && bashWriteOK {
		t.Logf("裁决：模型经由 bash 写入目标文件——bash 写文件不受路径校验是 Python 同款设计边界" +
			"（agent_mode_rail.py:296-310 只拦 git 写），非移植缺陷；如需运行时强约束需单独立项")
		return
	}
	t.Fatalf("无法裁决：write_file 未被调用，也未观测到 bash 写成功。工具调用: %+v\n事件数: %d（尾部: %v）",
		calls, len(eventTypes), eventTypes[max(0, len(eventTypes)-6):])
}

// truncateStr 按 rune 截断字符串并加省略号。
func truncateStr(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// envOrDefaultRealLLM 读取环境变量，为空时返回默认值。
func envOrDefaultRealLLM(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// writeRealConfigYAML 在 AdapterTestEnv 的目录结构里写入指向真实 LLM 的 config.yaml
// （OpenAI 兼容协议直连 Ark；max_iterations 限小以控制配额消耗）。
func writeRealConfigYAML(t *testing.T, dataDir, apiKey, apiBase, modelName string) {
	t.Helper()
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: %s
        client_provider: OpenAI
        api_key: %s
        api_base: %s
        client_id: real_llm_plan_write
        verify_ssl: false
        timeout: 300
      is_default: true

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
  max_iterations: 6
`, modelName, apiKey, apiBase)
	if err := os.WriteFile(filepath.Join(dataDir, "config", "config.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("写 config.yaml 失败: %v", err)
	}
}
