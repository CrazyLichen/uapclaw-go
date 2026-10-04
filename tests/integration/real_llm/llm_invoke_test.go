//go:build integration && llm

package real_llm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LLMInvokeSuite 真实 LLM Invoke/Stream 基础连通测试套件。
// 对齐 Python: tests/system_tests/foundation/llm/test_custom_headers_system.py
type LLMInvokeSuite struct {
	isuite.RealLLMSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLLMInvokeSuite 运行真实 LLM 连通测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestLLMInvokeSuite(t *testing.T) {
	suite.Run(t, new(LLMInvokeSuite))
}

// TestLLMInvoke_真实调用 测试真实 LLM Invoke 调用
// 对齐 Python: Model(model_client_config=...).invoke(messages)
func (s *LLMInvokeSuite) TestLLMInvoke_真实调用() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 60*time.Second)
	defer cancel()

	messages := model_clients.NewTextMessagesParam("1+1等于几？只回答数字")
	resp, err := model.Invoke(ctx, messages)
	s.Require().NoError(err, "真实 LLM Invoke 调用失败")
	s.Require().NotNil(resp, "响应不应为 nil")

	content := resp.Content.String()
	s.NotEmpty(content, "响应内容不应为空")
	s.True(strings.Contains(content, "2"), "1+1 的回答应包含 2，实际: %s", content)
}

// TestLLMStream_真实调用 测试真实 LLM Stream 流式调用
// 对齐 Python: Model(model_client_config=...).stream(messages)
func (s *LLMInvokeSuite) TestLLMStream_真实调用() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 60*time.Second)
	defer cancel()

	messages := model_clients.NewTextMessagesParam("说一个字：好")
	ch, err := model.Stream(ctx, messages)
	s.Require().NoError(err, "真实 LLM Stream 调用失败")
	s.Require().NotNil(ch, "流式通道不应为 nil")

	// 收集流式块，确保至少收到一个非空块
	receivedContent := false
	timeout := time.After(30 * time.Second)
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				goto done
			}
			if chunk != nil && chunk.Content.String() != "" {
				receivedContent = true
			}
		case <-timeout:
			goto done
		}
	}
done:
	s.True(receivedContent, "流式响应应收到非空内容")
}

// TestLLMInvoke_长文本 测试真实 LLM 处理长文本
// 对齐 Python: system_tests 中长文本输入场景
func (s *LLMInvokeSuite) TestLLMInvoke_长文本() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 60*time.Second)
	defer cancel()

	// 发送较长文本，验证 LLM 能正常处理
	longQuery := "请用三句话总结以下内容：Go语言是由Robert Griesemer、Rob Pike和Ken Thompson在2007年开始设计，2009年作为开源项目发布。它是一种静态类型、编译型语言，具有垃圾回收、并发编程等特性。Go语言的设计目标是简洁、高效、可靠，特别适合构建大规模网络服务和分布式系统。"
	messages := model_clients.NewTextMessagesParam(longQuery)
	resp, err := model.Invoke(ctx, messages)
	s.Require().NoError(err, "长文本调用失败")
	s.NotEmpty(resp.Content.String(), "长文本响应不应为空")
}

// TestLLMInvoke_工具调用 测试真实 LLM 工具调用
// 对齐 Python: create_tool_call_response() 在真实 LLM 下的行为
func (s *LLMInvokeSuite) TestLLMInvoke_工具调用() {
	s.SkipIfNoRealLLM()

	model, err := s.NewRealModel()
	s.Require().NoError(err, "创建真实 Model 失败")

	ctx, cancel := context.WithTimeout(s.Ctx, 60*time.Second)
	defer cancel()

	// 要求 LLM 使用工具
	messages := model_clients.NewTextMessagesParam("请使用 read_file 工具读取 /tmp/test.txt 文件")
	resp, err := model.Invoke(ctx, messages)
	s.Require().NoError(err, "工具调用测试失败")

	// 真实 LLM 可能返回工具调用，也可能直接回答
	// 这里只验证响应非空
	s.NotNil(resp, "响应不应为 nil")

	if len(resp.ToolCalls) > 0 {
		s.Equal("tool_calls", resp.FinishReason, "工具调用时 FinishReason 应为 tool_calls")
		s.Equal("read_file", resp.ToolCalls[0].Name, "工具名应为 read_file")
	}
}

// TestLLMDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *LLMInvokeSuite) TestLLMDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(llmschema.ModelClientConfig{})
	s.NotNil(model_clients.NewClientRegistry)
	s.NotNil(runner.GetResourceMgr)
}
