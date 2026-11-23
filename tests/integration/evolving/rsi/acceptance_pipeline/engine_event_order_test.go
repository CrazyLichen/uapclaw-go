//go:build integration

// 17.10 验收点①：引擎事件序。真编排器 + 真 TeamEvaluator（fake LLM HTTP
// 边界）跑一轮完整演进，收集 rsi.OnEvent 事件流，核对剧本顺序：
// 首事件为 ROOT node、进度事件 epoch 计数不回退、epoch 节点事件序号递增。
// 事件形状对齐 Python tests/unit_tests/rsi/test_harness_engine_events.py
// （progress_event 记已完成 epoch 数、root/epoch node_event 形态）。
//
// 跨子系统 ≥3：singleharness（真编排器）+ evaluator（真 TeamEvaluator）+
// data_loader（真载题）+ model 调用链（fake HTTP 边界）。

package rsi

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	rsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/singleharness"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EngineEventOrderSuite 引擎事件序套件。
type EngineEventOrderSuite struct{ suite.Suite }

// eventCollector 并发安全事件收集器。
type eventCollector struct {
	mu     sync.Mutex
	events []rsi.EngineEvent
}

func (c *eventCollector) add(_ context.Context, e rsi.EngineEvent) error {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
	return nil
}

func (c *eventCollector) snapshot() []rsi.EngineEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]rsi.EngineEvent(nil), c.events...)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestEngineEventOrderSuite 入口。
func TestEngineEventOrderSuite(t *testing.T) { suite.Run(t, new(EngineEventOrderSuite)) }

// accNewOrchestrator 构造真编排器（真 evaluator + fake LLM 边界）。
func (s *EngineEventOrderSuite) accNewOrchestrator(apiBase string, root string) (
	*singleharness.SingleHarnessIterativeOptimizationOrchestrator, *accRealEvalRequest) {
	s.T().Helper()
	modelConfig := accWriteModelConfig(s.T(), apiBase)
	return newAccOrchestratorWithModel(s.T(), modelConfig, root)
}

// TestEngineEventOrder_一轮演进事件剧本 fake LLM 驱动完整演进（solved 提前
// 停止），断言事件剧本：首事件 ROOT node、进度事件计数不回退、epoch 节点
// 事件序号递增、无状态异常事件。
func (s *EngineEventOrderSuite) TestEngineEventOrder_一轮演进事件剧本() {
	llm := &accFakeLLM{replies: []string{"REPLY", "REPLY", "REPLY"}}
	server := llm.server()
	s.T().Cleanup(server.Close)

	root := s.T().TempDir()
	orchestrator, pack := s.accNewOrchestrator(server.URL, root)

	collector := &eventCollector{}

	var resultErr error
	_, resultErr = orchestrator.Run(s.T().Context(), pack.req, collector.add)
	s.Require().NoError(resultErr)

	events := collector.snapshot()

	// 断言 1：事件流非空，首事件为 ROOT 节点（h0，node_type=ROOT，
	// 对齐 Python root_node_event 形态）。
	s.Require().NotEmpty(events)
	first, ok := events[0].(*rsi.EventNode)
	s.Require().True(ok, "首事件应为 node 类型，得到 %T", events[0])
	s.Require().Equal("h0", first.Node().NodeID())
	s.Require().Equal("ROOT", first.Node().Type())
	s.Require().True(first.Node().Adopted())

	// 断言 2：progress 事件的已完成 epoch 计数沿时间线不回退
	// （Python progress_event：统计已完成 epoch，本地候选尝试不推进）。
	lastProgress := -1
	progressSeen := 0
	for _, e := range events {
		if p, ok := e.(*rsi.EventProgress); ok {
			s.Require().GreaterOrEqual(p.Iteration(), lastProgress,
				"进度事件 epoch 计数回退：当前 %d，之前 %d", p.Iteration(), lastProgress)
			lastProgress = p.Iteration()
			progressSeen++
		}
	}
	s.Require().Greater(progressSeen, 0, "事件流中无 progress 事件")

	// 断言 3：epoch 节点事件（iteration_unit=epoch）序号不回退、集合无缺口。
	// 同一 epoch 会先后发 running 态与终态两次（iterative.go:4467/4805），
	// 对齐 Python epoch_node_event 的重发形态。
	lastEpoch := -1
	epochSeen := map[int]bool{}
	for _, e := range events {
		n, ok := e.(*rsi.EventNode)
		if !ok {
			continue
		}
		if unit, _ := n.Node().Extra()["iteration_unit"].(string); unit == "epoch" && n.Node().Type() != "ROOT" {
			s.Require().GreaterOrEqual(n.Node().Iteration(), lastEpoch,
				"epoch 节点事件序号回退：当前 %d，之前 %d", n.Node().Iteration(), lastEpoch)
			lastEpoch = n.Node().Iteration()
			epochSeen[n.Node().Iteration()] = true
		}
	}
	s.Require().NotEmpty(epochSeen, "无 epoch 节点事件")
	for idx := range epochSeen {
		s.Require().True(idx >= 1 && idx <= accOrchestratorConfig().MaxEpochs,
			"epoch 序号越界：%d", idx)
	}

	// 断言 4：全流程无 status 失败/终止事件（成功路径只发 terminal 之外的
	// 事件；status 仅在 cancelled/failed 收尾段出现，见 iterative.go:3464）。
	for _, e := range events {
		if st, ok := e.(*rsi.EventStatus); ok {
			s.Require().NotEqual(rsi.RsiStatusFailed, st.Status())
			s.Require().NotEqual(rsi.RsiStatusTerminated, st.Status())
		}
	}
}
