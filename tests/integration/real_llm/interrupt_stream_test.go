//go:build integration && llm

package real_llm

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptStreamSuite 真实 LLM 下 DeepAgent 流式中断/恢复测试套件。
//
// 对齐 Python: tests/system_tests/harness/test_deepagent_interrupt_stream.py
// （@pytest.mark.skipif(not API_KEY or not API_BASE) 可运行型守卫）
//
// 覆盖：
//   - 真实 LLM 生成 write 工具调用 → ConfirmInterruptRail 拦截 → 中断结果
//   - InteractiveInput 确认 → 恢复执行 → answer 结果、工具恰好执行 1 次
type InterruptStreamSuite struct {
	isuite.RealLLMSuite
	// writeFileDir write_file 工具的目标目录（每个测试方法独立）
	writeFileDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInterruptStreamSuite 运行真实 LLM 流式中断恢复测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestInterruptStreamSuite(t *testing.T) {
	suite.Run(t, new(InterruptStreamSuite))
}

// SetupTest 为每个测试方法创建独立的临时写入目录
func (s *InterruptStreamSuite) SetupTest() {
	s.writeFileDir = s.T().TempDir()
}

// TestInterruptStream_真实LLM_中断恢复 测试真实 LLM 下流式中断后确认恢复。
// 对齐 Python: test_deepagent_stream_interrupt_resume
//
// Python 原文通过 run_agent_streaming 消费 INTERACTION 输出判定中断；
// Go 侧 Stream 模式中断事件写入路径未完善（见 react_interrupt_stream_test.go
// 的 TODO），故对齐其语义等价路径：Invoke 返回 interrupt result，
// interrupt_ids 对应 Python 的 tool_call_id，恢复轮次用 InteractiveInput
// 触发，断言对齐 Python 原文（中断检测 + write 未执行 + 确认后 write
// 恰好执行 1 次 + 第二轮无中断）。
func (s *InterruptStreamSuite) TestInterruptStream_真实LLM_中断恢复() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 3*time.Minute)
	defer cancel()

	var writeInvokeCount atomic.Int32
	writeTool := s.newRealWriteFileTool(&writeInvokeCount)

	rail := interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file"))

	card := agentschema.NewAgentCard(
		agentschema.WithAgentID("test_deepagent_resume_agent"),
		agentschema.WithAgentName("TestDeepAgentResume"),
	)
	agent, err := harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card:          card,
		Model:         model,
		ToolInstances: []tool.Tool{writeTool},
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 5,
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// 独立 InMemoryCheckpointer 隔离跨测试状态（见记忆：NewTestSession Checkpointer 隔离）
	sess := session.NewSession(
		session.WithSessionID("test_resume_1"),
		session.WithEnvs(map[string]any{}),
		session.WithCheckpointer(checkpointer.NewInMemoryCheckpointer()),
	)
	s.Require().NoError(sess.PreRun(ctx), "Session PreRun 失败")
	defer sess.PostRun(ctx)

	// 第 1 轮：真实 LLM 应生成 write_file 工具调用并被 rail 拦截。
	// 对齐 Python: "请写入文件 test.txt 内容为 hello world"
	testFile := filepath.Join(s.writeFileDir, "test.txt")
	result, err := agent.Invoke(ctx, map[string]any{
		"query": "请写入文件 " + testFile + " 内容为 hello world",
	}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "首次 Invoke 失败")

	interruptIDs, _ := testhelpers.AssertInterruptResult(s.T(), result, -1)
	s.Require().NotEmpty(interruptIDs, "应检测到中断并获取 interrupt_id（若 LLM 未调用 write_file 工具则重跑）")
	s.Equal(int32(0), writeInvokeCount.Load(), "中断时 write_file 不应执行")

	// 第 2 轮：确认中断后恢复执行（对齐 Python InteractiveInput.update(tool_call_id, {...})）
	interactiveInput := testhelpers.ConfirmInterrupt(interruptIDs[0], false)
	result, err = agent.Invoke(ctx, map[string]any{"query": interactiveInput},
		agentinterfaces.WithSession(sess))
	s.Require().NoError(err, "恢复 Invoke 失败")

	testhelpers.AssertAnswerResult(s.T(), result)
	s.Equal(int32(1), writeInvokeCount.Load(),
		"确认后 write_file 应恰好执行 1 次（真实 LLM 可能在恢复轮次再次调用，工具幂等覆盖同一文件）")
}

// TestInterruptStreamDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *InterruptStreamSuite) TestInterruptStreamDoc_包引用验证() {
	s.NotNil(interrupt.NewConfirmInterruptRail)
	s.NotNil(tool.NewMapFunction)
	s.NotNil(testhelpers.ConfirmInterrupt)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRealWriteFileTool 创建真实写文件的 write_file 工具（对齐 Python WriteTool）。
// 工具函数体真实写入目标文件，并通过 invokeCount 追踪执行次数。
// 参数 schema 使用构造器（NewStringParam），避免结构体字面量零值陷阱。
func (s *InterruptStreamSuite) newRealWriteFileTool(invokeCount *atomic.Int32) tool.Tool {
	tc := tool.NewToolCardWithID(
		"write_file", "write_file", "写入文件",
		[]*schema.Param{
			schema.NewStringParam("filepath", "文件路径", true),
			schema.NewStringParam("content", "写入内容", true),
		},
		nil,
	)
	t, _ := tool.NewMapFunction(tc,
		func(_ context.Context, args map[string]any) (map[string]any, error) {
			invokeCount.Add(1)
			filepathRaw, ok1 := args["filepath"].(string)
			content, ok2 := args["content"].(string)
			if !ok1 || !ok2 {
				return map[string]any{"status": "error", "error": "参数类型错误"}, nil
			}
			// 真实写入文件
			if err := os.WriteFile(filepathRaw, []byte(content), 0o600); err != nil {
				return map[string]any{"status": "error", "error": err.Error()}, nil
			}
			return map[string]any{"status": "ok", "filepath": filepathRaw}, nil
		}, nil,
	)
	return t
}
