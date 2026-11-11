package recommendation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseRFC3339 解析 RFC3339 时间戳，供测试校验 LastUpdated 格式。
func parseRFC3339(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

// parseDateOnly 解析 YYYY-MM-DD 日期，供测试校验每日计数 key 格式。
func parseDateOnly(value string) (time.Time, error) {
	return time.Parse("2006-01-02", value)
}

// newProactiveAgentCapturing 构造把模型收到的 prompt 写入 captured 的真 DeepAgent。
func newProactiveAgentCapturing(t *testing.T, captured *string, reply string) *harness.DeepAgent {
	t.Helper()

	model := newProactiveMockModel(t, func(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
		*captured = messagesToText(messages)
		return llmschema.NewAssistantMessage(reply), nil
	})
	agent, err := harness.CreateDeepAgent(context.Background(), harness_config.CreateDeepAgentParams{
		Model:                  model,
		Card:                   agentschema.NewAgentCard(agentschema.WithAgentName("proactive_capture")),
		SystemPrompt:           "严格输出 JSON 对象。",
		EnableTaskLoop:         false,
		MaxIterations:          1,
		AddGeneralPurposeAgent: false,
	})
	if err != nil {
		t.Fatalf("创建 DeepAgent 失败: %v", err)
	}
	t.Cleanup(func() { _ = agent.Stop(context.Background()) })
	return agent
}

// findRepoResourcesDirForTest 从当前工作目录向上回溯定位仓库根 resources/。
func findRepoResourcesDirForTest() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, "resources", "agent", "workspace", "skills")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return filepath.Join(dir, "resources"), true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
