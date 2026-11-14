//go:build integration

package native

import (
	"context"
	"fmt"
	"os"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hp "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	nativeprov "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers/native"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// NativeE2ESuite native provider 端到端协议符合性（manifest → create_harness →
// start/send/turn_events/abort/stop 全公开协议面）。
//
// 对应 Python: tests/system_tests/harness_providers/test_native_e2e.py +
// _contract.py。上游用真模型；本套件用本地 OpenAI 兼容 SSE 假端点脚本化
// 响应（15.5 CLI 集成模式），行为断言逐条对齐 _contract 的协议不变量。
type NativeE2ESuite struct {
	isuite.AgentSuite
	// lastFrames 帧耗尽后复用的最后一组
	lastFrames []string
	// llmSrv OpenAI 兼容假端点
	llmSrv *httptest.Server
	// framesQueue 每次模型调用消费一组帧（FIFO；空时复用最后一组）
	framesMu sync.Mutex
	frames   [][]string
	// slowDrip 慢滴模式：每帧之间停顿，供中止用例在首个输出后抢停
	slowDrip bool
}

func TestNativeE2ESuiteEntry(t *testing.T) {
	suite.Run(t, new(NativeE2ESuite))
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 起假 LLM 端点。
func (s *NativeE2ESuite) SetupSuite() {
	s.AgentSuite.SetupSuite()
	s.llmSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.framesMu.Lock()
		var frames []string
		if len(s.frames) > 0 {
			frames = s.frames[0]
			s.frames = s.frames[1:]
		} else if len(s.frames) == 0 && s.lastFrames != nil {
			frames = s.lastFrames
		}
		slow := s.slowDrip
		s.framesMu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for i, frame := range frames {
			_, _ = w.Write([]byte(frame + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			if slow {
				time.Sleep(400 * time.Millisecond)
			}
			_ = i
		}
	}))
}

// TearDownSuite 关闭假端点。
func (s *NativeE2ESuite) TearDownSuite() {
	s.llmSrv.Close()
	s.AgentSuite.TearDownSuite()
}

// TestE2E_文本轮次 started → answer → FINISHED → IDLE → stop → TERMINATED。
// Python: run_text_turn。
func (s *NativeE2ESuite) TestE2E_文本轮次() {
	s.setFrames(textStopFrames("PONG"))
	h := s.newE2EHarness(false)
	s.requireNoError(h.Start(s.Ctx, s.e2eContext(nil, nil)))
	s.Equal(hp.HarnessStateIdle, h.State())

	input, err := hp.NewHarnessInput("Reply with exactly the single word PONG and nothing else.", hp.JsonObject{})
	s.requireNoError(err)
	receipt, err := h.Send(s.Ctx, input, hp.DeliveryModeAuto)
	s.requireNoError(err)
	s.T().Logf("receipt mode=%s", receipt.AcceptedMode)

	events := s.collectTurn(h, nonNilPtr(receipt.TurnID))
	s.assertTurnInvariants(events, nonNilPtr(receipt.TurnID))
	terminal := s.terminalOf(events)
	s.Equal(hp.TurnEventKindFinished, terminal.Kind)
	s.Contains(strings.ToUpper(s.answerText(terminal, events)), "PONG")
	s.assertHasOutputEvent(events)
	s.NotNil(terminal.Result)

	s.requireNoError(h.Stop(s.Ctx))
	s.Equal(hp.HarnessStateTerminated, h.State())
	s.requireNoError(h.Stop(s.Ctx)) // 二次 stop 幂等（Python: await harness.stop() ×2）
}

// TestE2E_工具轮读文件 sys_operation 挂载的 read_file 真实执行，工具项可观测。
// Python: run_tool_turn。
func (s *NativeE2ESuite) TestE2E_工具轮读文件() {
	workdir := s.T().TempDir()
	token := fmt.Sprintf("TOKEN-%X", time.Now().UnixNano())
	s.Require().NoError(writeE2EFile(workdir+"/secret.txt", token+"\n"))

	// 两次模型调用各自一组帧（组间以 [DONE] 分隔）。
	s.setFrames([]string{
		toolCallFrame("call-e2e-rf", "read_file", `{"path": "secret.txt"}`),
		toolStopFrame(), "data: [DONE]",
	}, textStopFrames("The token is "+token))
	h := s.newE2EHarnessFromManifest(s.e2eManifest(false), workdir)
	s.requireNoError(h.Start(s.Ctx, s.e2eContext(nil, &workdir)))
	defer func() { s.requireNoError(h.Stop(s.Ctx)) }()

	input, err := hp.NewHarnessInput("Read secret.txt and reply with the token.", hp.JsonObject{})
	s.requireNoError(err)
	receipt, err := h.Send(s.Ctx, input, hp.DeliveryModeAuto)
	s.requireNoError(err)

	events := s.collectTurn(h, nonNilPtr(receipt.TurnID))
	s.assertTurnInvariants(events, nonNilPtr(receipt.TurnID))
	terminal := s.terminalOf(events)
	s.Equal(hp.TurnEventKindFinished, terminal.Kind)
	s.Contains(s.answerText(terminal, events), token)

	items := s.toolItems(events)
	s.NotEmpty(items, "工具项应可观测")
	kinds := map[hp.ItemEventKind]bool{}
	for _, kind := range items {
		kinds[kind] = true
	}
	s.True(kinds[hp.ItemEventKindStarted] && kinds[hp.ItemEventKindCompleted], "started/completed 都应出现")
}

// TestE2E_多轮共享一条流 背靠背两次发送串行成两轮，事件流可区分轮次。
// Python: run_follow_up_turns。
func (s *NativeE2ESuite) TestE2E_多轮共享一条流() {
	s.setFrames(textStopFrames("ALPHA"), textStopFrames("BRAVO"))
	h := s.newE2EHarness(false)
	s.requireNoError(h.Start(s.Ctx, s.e2eContext(nil, nil)))
	defer func() { s.requireNoError(h.Stop(s.Ctx)) }()

	first := s.sendInput(h, "Reply with exactly the word ALPHA and nothing else.", hp.DeliveryModeAuto)
	second := s.sendInput(h, "Reply with exactly the word BRAVO and nothing else.", hp.DeliveryModeAuto)
	s.NotEqual(first.TurnID, second.TurnID)

	firstEvents := s.collectTurn(h, nonNilPtr(first.TurnID))
	secondEvents := s.collectTurn(h, nonNilPtr(second.TurnID))
	s.assertTurnInvariants(firstEvents, nonNilPtr(first.TurnID))
	s.assertTurnInvariants(secondEvents, nonNilPtr(second.TurnID))
	s.Equal(hp.TurnEventKindFinished, s.terminalOf(firstEvents).Kind)
	s.Contains(strings.ToUpper(s.answerText(s.terminalOf(firstEvents), firstEvents)), "ALPHA")
	s.Equal(hp.TurnEventKindFinished, s.terminalOf(secondEvents).Kind)
	s.Contains(strings.ToUpper(s.answerText(s.terminalOf(secondEvents), secondEvents)), "BRAVO")
}

// TestE2E_中止活跃轮次 首个输出后中止 → ABORTED；会话存活，下一轮完成。
// Python: run_abort_turn。消费与中止并行：缓冲区背压为 Block，不并行消费
// 会在 buffer 满时把 Abort/Send 挂死。
func (s *NativeE2ESuite) TestE2E_中止活跃轮次() {
	s.setFrames([]string{
		textFrame("1"), textFrame("2"), textFrame("3"), textFrame("4"), textFrame("5"),
		stopFrame(), "data: [DONE]",
	})
	s.setSlowDrip(true)
	defer s.setSlowDrip(false)

	h := s.newE2EHarness(false)
	s.requireNoError(h.Start(s.Ctx, s.e2eContext(nil, nil)))
	defer func() { s.requireNoError(h.Stop(s.Ctx)) }()

	input, err := hp.NewHarnessInput("Count slowly.", hp.JsonObject{})
	s.requireNoError(err)
	receipt, err := h.Send(s.Ctx, input, hp.DeliveryModeAuto)
	s.requireNoError(err)

	// 单消费者后台收集；见到首个 OutputEvent 经信号通道通知主流程中止
	// （Python: 消费循环内 abort；Go 缓冲区背压 Block，消费必须常驻）。
	signalCh := make(chan struct{}, 1)
	eventsCh := make(chan []hp.HarnessEvent, 1)
	go func() {
		eventsCh <- s.collectUntilFirstOutput(h, nonNilPtr(receipt.TurnID), signalCh)
	}()
	select {
	case <-signalCh:
	case <-time.After(20 * time.Second):
		s.FailNow("等待输出事件超时")
	}
	// Python: graceful 优先；native card 只声明 FORCE_ABORT。
	s.requireNoError(h.Abort(s.Ctx, hp.AbortModeForce))

	events := <-eventsCh
	s.Require().NotEmpty(events, "轮次在产出输出前结束")
	terminal := s.terminalOf(events)
	s.T().Logf("abort terminal=%v", terminal.Kind)
	s.Equal(hp.TurnEventKindAborted, terminal.Kind, "中止后轮次应 ABORTED")
	s.Equal(hp.HarnessStateIdle, h.State(), "中止后会话回到 IDLE")

	s.setFrames(textStopFrames("RESUMED"))
	again := s.sendInput(h, "Reply with exactly the word RESUMED and nothing else.", hp.DeliveryModeAuto)
	nextEvents := s.collectTurn(h, nonNilPtr(again.TurnID))
	s.Equal(hp.TurnEventKindFinished, s.terminalOf(nextEvents).Kind)
	s.Contains(strings.ToUpper(s.answerText(s.terminalOf(nextEvents), nextEvents)), "RESUMED")
}

// TestE2E_引导活跃轮次 steer 回执指向活跃轮次，轮次正常完成。
// Python: run_steer_turn。消费与 steer 并行（同中止用例的背压理由）。
func (s *NativeE2ESuite) TestE2E_引导活跃轮次() {
	s.setFrames(textStopFrames("sea poem MOUNTAIN"))
	s.setSlowDrip(true)
	defer s.setSlowDrip(false)

	h := s.newE2EHarness(false)
	s.requireNoError(h.Start(s.Ctx, s.e2eContext(nil, nil)))
	defer func() { s.requireNoError(h.Stop(s.Ctx)) }()

	input, err := hp.NewHarnessInput("Write a poem.", hp.JsonObject{})
	s.requireNoError(err)
	receipt, err := h.Send(s.Ctx, input, hp.DeliveryModeAuto)
	s.requireNoError(err)

	signalCh := make(chan struct{}, 1)
	eventsCh := make(chan []hp.HarnessEvent, 1)
	go func() {
		eventsCh <- s.collectUntilFirstOutput(h, nonNilPtr(receipt.TurnID), signalCh)
	}()
	select {
	case <-signalCh:
	case <-time.After(20 * time.Second):
		s.FailNow("等待 steer 时机超时")
	}
	steerInput, err := hp.NewHarnessInput("End with MOUNTAIN.", hp.JsonObject{})
	s.requireNoError(err)
	steerReceipt, err := h.Send(s.Ctx, steerInput, hp.DeliveryModeSteer)
	s.requireNoError(err)
	s.Equal(receipt.TurnID, steerReceipt.TurnID, "steer 回执应指向活跃轮次")
	s.Equal(hp.DeliveryModeSteer, steerReceipt.AcceptedMode)

	events := <-eventsCh
	s.Require().NotEmpty(events, "轮次事件流为空")
	s.Equal(hp.TurnEventKindFinished, s.terminalOf(events).Kind)
}

// TestE2E_用户输入路由到宿主 ask_user 中断经宿主 handler 往返。
// Python: test_ask_user_interrupt_routes_to_the_host（RecordingUserInputHandler）。
func (s *NativeE2ESuite) TestE2E_用户输入路由到宿主() {
	handler := newRecordingUserInputHandler("teal")
	s.setFrames([]string{
		toolCallFrame("call-e2e-ask", "ask_user",
			`{"questions": [{"header": "Color", "question": "What is your favorite color?", "options": []}]}`),
		toolStopFrame(), "data: [DONE]",
	}, textStopFrames("Your favorite color is teal."))
	h := s.newE2EHarness(true)
	s.requireNoError(h.Start(s.Ctx, s.e2eContext(handler, nil)))
	defer func() { s.requireNoError(h.Stop(s.Ctx)) }()

	input, err := hp.NewHarnessInput("Ask me a question with your ask-user tool.", hp.JsonObject{})
	s.requireNoError(err)
	receipt, err := h.Send(s.Ctx, input, hp.DeliveryModeAuto)
	s.requireNoError(err)

	events := s.collectTurn(h, nonNilPtr(receipt.TurnID))
	s.Equal(hp.TurnEventKindFinished, s.terminalOf(events).Kind)

	requests := handler.all()
	s.Require().NotEmpty(requests, "宿主应收到 user input 请求")
	s.Contains(requests[0].Prompt, "favorite color", "提问应路由到宿主 handler")
	s.Contains(s.answerText(s.terminalOf(events), events), "teal")
}

// TestE2E_清单提示词段落进入代理 manifest prompt_sections 热加载到 agent。
// Python: test_manifest_prompt_sections_reach_the_agent（脚本化端点下改为
// 断言 agent 系统提示词builder 真实包含段落正文 + 回答标记）。
func (s *NativeE2ESuite) TestE2E_清单提示词段落进入代理() {
	codeword := "When asked for the codeword, answer with exactly AMBER-7 and nothing else."
	s.setFrames(textStopFrames("AMBER-7"))

	manifest := s.e2eManifest(false)
	manifest.PromptSections = append(manifest.PromptSections, hschema.PromptSectionSpec{
		Name:    "codeword",
		Content: map[string]string{"en": codeword},
	})
	h := s.newE2EHarnessFromManifest(manifest, "")
	s.requireNoError(h.Start(s.Ctx, s.e2eContext(nil, nil)))
	defer func() { s.requireNoError(h.Stop(s.Ctx)) }()

	// 段落经 LoadAgentTemplateSpec 热加载进 agent 的系统提示词。
	agent := h.Agent()
	s.Require().NotNil(agent)
	s.Require().NotNil(agent.SystemPromptBuilder())
	s.True(agent.SystemPromptBuilder().HasSection("codeword"), "manifest 提示词段落应加载进 builder")
	section := agent.SystemPromptBuilder().GetSection("codeword")
	s.Require().NotNil(section)
	s.Contains(section.Content["en"], "AMBER-7")

	input, err := hp.NewHarnessInput("What is the codeword?", hp.JsonObject{})
	s.requireNoError(err)
	receipt, err := h.Send(s.Ctx, input, hp.DeliveryModeAuto)
	s.requireNoError(err)
	events := s.collectTurn(h, nonNilPtr(receipt.TurnID))
	terminal := s.terminalOf(events)
	s.Equal(hp.TurnEventKindFinished, terminal.Kind)
	s.Contains(strings.ToUpper(s.answerText(terminal, events)), "AMBER-7")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setFrames 设置后续模型调用消费的帧组队列。
func (s *NativeE2ESuite) setFrames(groups ...[]string) {
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	s.frames = groups
	if len(groups) > 0 {
		s.lastFrames = groups[len(groups)-1]
	}
}

// setSlowDrip 开关慢滴模式。
func (s *NativeE2ESuite) setSlowDrip(value bool) {
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	s.slowDrip = value
}

// e2eManifest 构造 e2e manifest（模型指向假端点）。
// Python: _manifest。
func (s *NativeE2ESuite) e2eManifest(withAskUser bool) hschema.AgentTemplateSpec {
	rails := []hschema.RailSpec{{Type: "core.sys_operation"}}
	if withAskUser {
		rails = append(rails, hschema.RailSpec{Type: "core.ask_user"})
	}
	card := saschema.NewAgentCard()
	card.Name = "native-e2e"
	card.Description = "Native harness e2e agent."
	card.ID = "native-e2e"
	return hschema.AgentTemplateSpec{
		AgentCard: *card,
		Model: &hschema.ModelSpec{
			ModelClientConfig: llmschema.ModelClientConfig{
				ClientProvider: "OpenAI",
				APIKey:         "itest-key",
				APIBase:        s.llmSrv.URL,
				VerifySSL:      false,
				Timeout:        60,
				MaxRetries:     3,
			},
			ModelRequestConfig: &llmschema.ModelRequestConfig{ModelName: "gpt-4o"},
		},
		PromptSections: []hschema.PromptSectionSpec{
			{Name: "identity", Content: map[string]string{"en": "You are a concise assistant."}},
		},
		Rails: rails,
	}
}

// newE2EHarness 经 CreateHarness（provider="native"）构造协议 harness。
func (s *NativeE2ESuite) newE2EHarness(withAskUser bool) *nativeHarness {
	return s.newE2EHarnessFromManifest(s.e2eManifest(withAskUser), "")
}

// newE2EHarnessFromManifest 带显式 deep_agent 配置构造。
func (s *NativeE2ESuite) newE2EHarnessFromManifest(manifest hschema.AgentTemplateSpec, workspace string) *nativeHarness {
	config := map[string]any{"deep_agent": map[string]any{"enable_task_loop": false}}
	if workspace != "" {
		config["deep_agent"].(map[string]any)["workspace"] = map[string]any{"root_path": workspace}
	}
	protocol, err := harness_providers.CreateHarness(manifest, harness_providers.CreateHarnessOptions{
		Provider: "native",
		Config:   config,
		Language: "en",
	})
	s.Require().NoError(err, "create_harness 失败")
	impl, ok := protocol.(*nativeHarness)
	s.Require().True(ok, "native provider 应产出 DeepAgentHarness，实际 %T", protocol)
	return impl
}

// e2eContext 构造宿主上下文。
func (s *NativeE2ESuite) e2eContext(
	interactions hp.HarnessInteractionHandler, cwd *string,
) hp.HarnessContext {
	opts := hp.ContextOptions{
		Interactions: interactions,
	}
	if cwd != nil {
		opts.Cwd = cwd
	}
	ctx, err := hp.NewHarnessContext("e2e-agent", "e2e_e2e-agent",
		fmt.Sprintf("e2e-session-%d", suiteUniqueID.Add(1)), "", opts)
	s.Require().NoError(err)
	return ctx
}

// sendInput 发送输入并要求成功。
func (s *NativeE2ESuite) sendInput(h *nativeHarness, content string, mode hp.DeliveryMode) hp.SendReceipt {
	input, err := hp.NewHarnessInput(content, hp.JsonObject{})
	s.Require().NoError(err)
	receipt, err := h.Send(s.Ctx, input, mode)
	s.Require().NoError(err)
	return receipt
}

// collectTurn 收集轮次事件直到流结束。
func (s *NativeE2ESuite) collectTurn(h *nativeHarness, turnID *string) []hp.HarnessEvent {
	cursor := h.TurnEvents(turnID)
	defer func() { _ = cursor.Close(s.Ctx) }()
	events := []hp.HarnessEvent{}
	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-deadline:
			s.FailNow("collectTurn 超时")
		default:
		}
		event, err := cursor.Next(s.Ctx)
		if err != nil {
			if isEventStreamDone(err) {
				return events
			}
			s.Require().NoError(err, "event stream error")
		}
		events = append(events, *event)
	}
}

// collectUntilFirstOutput 收集轮次事件直到流结束；遇到首个 OutputEvent 或
// 生命周期事件时向 signalCh 发一次信号（不中断收集）。
func (s *NativeE2ESuite) collectUntilFirstOutput(
	h *nativeHarness, turnID *string, signalCh chan struct{},
) []hp.HarnessEvent {
	cursor := h.TurnEvents(turnID)
	defer func() { _ = cursor.Close(s.Ctx) }()
	events := []hp.HarnessEvent{}
	signalled := false
	for {
		event, err := cursor.Next(s.Ctx)
		if err != nil {
			if isEventStreamDone(err) {
				return events
			}
			return events
		}
		events = append(events, *event)
		if !signalled {
			if _, isOutput := event.Event.(hp.OutputEvent); isOutput {
				signalled = true
				signalCh <- struct{}{}
			}
		}
	}
}

// terminalOf 取终态事件。
func (s *NativeE2ESuite) terminalOf(events []hp.HarnessEvent) hp.TurnLifecycleEvent {
	s.Require().NotEmpty(events, "事件流为空")
	lifecycle, ok := events[len(events)-1].Event.(hp.TurnLifecycleEvent)
	s.Require().True(ok, "最后事件应为 TurnLifecycleEvent，实际 %T", events[len(events)-1].Event)
	s.NotEqual(hp.TurnEventKindStarted, lifecycle.Kind)
	return lifecycle
}

// answerText 终态 final_output 或 ANSWER 通道拼接。
func (s *NativeE2ESuite) answerText(terminal hp.TurnLifecycleEvent, events []hp.HarnessEvent) string {
	if terminal.Result != nil {
		if text, ok := terminal.Result.FinalOutput.(string); ok && text != "" {
			return text
		}
	}
	parts := []string{}
	for _, event := range events {
		if output, ok := event.Event.(hp.OutputEvent); ok && output.Channel == hp.OutputChannelAnswer {
			if text, ok := hp.JsonValueToBuiltin(output.Content).(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "")
}

// assertTurnInvariants 协议不变量：一次 STARTED、一个终态、序列单调、轮次隔离。
// Python: assert_turn_invariants。
func (s *NativeE2ESuite) assertTurnInvariants(events []hp.HarnessEvent, turnID *string) {
	s.Require().NotEmpty(events)
	started := 0
	terminals := 0
	lastSequence := -1
	for _, event := range events {
		if turnID != nil {
			s.Require().Equal(*turnID, event.TurnID, "turn view 泄漏其他轮次")
		}
		s.Require().Greater(event.Sequence, lastSequence, "sequence 应严格递增")
		lastSequence = event.Sequence
		s.Require().NotEmpty(event.HostSessionID)
		s.Require().NotEmpty(event.AgentID)
		if lifecycle, ok := event.Event.(hp.TurnLifecycleEvent); ok {
			switch lifecycle.Kind {
			case hp.TurnEventKindStarted:
				started++
			default:
				terminals++
			}
		}
	}
	s.Equal(1, started, "应恰好一次 STARTED")
	s.Equal(1, terminals, "应恰好一个终态")
}

// assertHasOutputEvent 断言至少一个 OutputEvent。
func (s *NativeE2ESuite) assertHasOutputEvent(events []hp.HarnessEvent) {
	for _, event := range events {
		if _, ok := event.Event.(hp.OutputEvent); ok {
			return
		}
	}
	s.Fail("no OutputEvent observed")
}

// waitForOutputEvent 等待轮次产生首个 OutputEvent。
func (s *NativeE2ESuite) waitForOutputEvent(h *nativeHarness, turnID *string) {
	cursor := h.TurnEvents(turnID)
	defer func() { _ = cursor.Close(s.Ctx) }()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			s.FailNow("等待输出事件超时")
		default:
		}
		event, err := cursor.Next(s.Ctx)
		if err != nil {
			if isEventStreamDone(err) {
				s.FailNow("轮次在产出输出前结束")
			}
			s.Require().NoError(err)
		}
		if _, ok := event.Event.(hp.OutputEvent); ok {
			return
		}
	}
}

// toolItems 提取工具项事件种类。
func (s *NativeE2ESuite) toolItems(events []hp.HarnessEvent) []hp.ItemEventKind {
	kinds := []hp.ItemEventKind{}
	for _, event := range events {
		if item, ok := event.Event.(hp.ItemLifecycleEvent); ok && item.ItemType == "tool" {
			kinds = append(kinds, item.Kind)
		}
	}
	return kinds
}

// nonNilPtr 字符串转指针（TurnEvents 入参）。
func nonNilPtr(value string) *string { return &value }

// requireNoError 断言无错。
func (s *NativeE2ESuite) requireNoError(err error, msgAndArgs ...any) {
	s.Require().NoError(err, msgAndArgs...)
}

// writeE2EFile 写文件。
func writeE2EFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// nativeHarness 协议 harness 别名（e2e 用）。
type nativeHarness = nativeprov.DeepAgentHarness

// recordingUserInputHandler 记录请求并以固定文本应答的宿主 handler。
// Python: RecordingUserInputHandler。
type recordingUserInputHandler struct {
	answer string

	mu       sync.Mutex
	requests []hp.UserInputRequest
}

func newRecordingUserInputHandler(answer string) *recordingUserInputHandler {
	return &recordingUserInputHandler{answer: answer}
}

func (h *recordingUserInputHandler) Handle(_ context.Context,
	request hp.HarnessInteractionRequest) (hp.HarnessInteractionResponse, error) {
	req, ok := request.(hp.UserInputRequest)
	if !ok {
		if pointer, isPointer := request.(*hp.UserInputRequest); isPointer && pointer != nil {
			req = *pointer
		}
	}
	h.mu.Lock()
	h.requests = append(h.requests, req)
	h.mu.Unlock()
	return hp.NewUserInputResponse(req.RequestID, hp.InteractionResponseStatusCompleted,
		hp.UserInputResponseOptions{Content: h.answer})
}

func (h *recordingUserInputHandler) Cancel(_ context.Context, _ string,
	_ ...hp.CancelOption) error { return nil }

func (h *recordingUserInputHandler) all() []hp.UserInputRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]hp.UserInputRequest{}, h.requests...)
}


// ──────────────────────────── SSE 帧助手 ────────────────────────────

// textFrame 文本增量帧。
func textFrame(content string) string {
	return `data: {"choices":[{"delta":{"content":"` + content + `"},"finish_reason":null}]}`
}

// toolStopFrame 工具调用终止帧（finish_reason=tool_calls，带 usage）。
func toolStopFrame() string {
	return `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
}

// stopFrame 终止帧（带 usage）。
func stopFrame() string {
	return `data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
}

// toolCallFrame 工具调用帧。
func toolCallFrame(callID, name, argsJSON string) string {
	args := strings.ReplaceAll(argsJSON, `"`, `\"`)
	// 注意 delta/tool_calls/choice 三层括号收口：function} TC} ] } 后才是
	// finish_reason（曾多写一个 } 导致整帧 JSON 非法、tool_calls 被静默丢弃——
	// CLI 套件同款模板畸形即其 e2e 中断缺口登记的根因）。
	return `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"` + callID +
		`","type":"function","function":{"name":"` + name + `","arguments":"` + args + `"}}]},"finish_reason":null}]}`
}

// textStopFrames 一轮文本回复 + stop。
func textStopFrames(content string) []string {
	return []string{textFrame(content), stopFrame(), "data: [DONE]"}
}
