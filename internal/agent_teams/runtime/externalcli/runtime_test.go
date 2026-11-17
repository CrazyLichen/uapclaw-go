package externalcli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli/transport"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// writeScript 在 t.TempDir 下写一个可执行假 CLI 脚本。
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

// newEchoStreamingRuntime 构造一个用假 CLI 脚本驱动的 ExternalCliRuntime：
// 脚本读 stdin，按行回显，收到 END 行输出完成哨兵并退出。
func newEchoStreamingRuntime(t *testing.T, memberName string) (*ExternalCliRuntime, string) {
	t.Helper()
	script := writeScript(t, "stream_cli.sh", `while IFS= read -r line; do
  echo "echo:$line"
  echo "<<END_OF_TURN>>"
done`)
	tr := &transport.LocalTransport{}
	proc, err := tr.Run([]string{"/bin/sh", script}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := BuildAdapter("generic", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Completion = CompletionMarkerPrefix + "<<END_OF_TURN>>"
	rt := NewExternalCliRuntime(memberName, adapter, NewStdinPipeInjector(proc.Stdin()),
		proc.Stdout(), proc, tr, "agent-1")
	if err := rt.Start(nil); err != nil {
		t.Fatal(err)
	}
	return rt, script
}

// collectOutputs 消费输出通道直到关闭或超时，返回摘要文本列表。
func collectOutputs(t *testing.T, ch <-chan stream.OutputSchema, timeout time.Duration) []string {
	t.Helper()
	var texts []string
	deadline := time.After(timeout)
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				return texts
			}
			if m, ok := chunk.Payload.(map[string]any); ok {
				texts = append(texts, m["content"].(string))
			}
		case <-deadline:
			t.Fatal("collectOutputs 超时")
		}
	}
}

// TestExternalCliRuntime_整轮与完成判定 对齐 _drive 主路径：stdin 投递、
// 叙述块产出、完成哨兵结束轮次、相位回 IDLE。
func TestExternalCliRuntime_整轮与完成判定(t *testing.T) {
	rt, _ := newEchoStreamingRuntime(t, "beta")
	var roundKinds []string
	var mu sync.Mutex
	rt.Subscribe(nil, func(kind string, roundID int) {
		mu.Lock()
		roundKinds = append(roundKinds, kind)
		mu.Unlock()
	})
	if _, err := rt.Send("hello", false); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateIdle, 5*time.Second)
	// outputs 迭代只在 stop 时终止（Python _END 哨兵语义）；先停再收集。
	rt.Stop()
	texts := collectOutputs(t, rt.Outputs(), 5*time.Second)
	if len(texts) == 0 || !strings.Contains(texts[0], "echo:") {
		t.Fatalf("叙述块缺失: %v", texts)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(roundKinds) < 2 || roundKinds[0] != "started" || roundKinds[len(roundKinds)-1] != "finished" {
		t.Fatalf("轮次事件: %v", roundKinds)
	}
	if rt.SessionID() != nil {
		t.Fatal("CLI runtime 的 session_id 恒空")
	}
}

// TestExternalCliRuntime_steering对运行轮生效 对齐 steer 分支。
func TestExternalCliRuntime_steering对运行轮生效(t *testing.T) {
	rt, _ := newEchoStreamingRuntime(t, "beta")
	if _, err := rt.Send("first", false); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateRunning, 5*time.Second)
	if _, err := rt.Send("steered", true); err != nil {
		t.Fatal(err)
	}
	rt.Stop()
}

// TestExternalCliRuntime_stop幂等与输出终止 对齐 stop 的终止语义。
func TestExternalCliRuntime_stop幂等与输出终止(t *testing.T) {
	rt, _ := newEchoStreamingRuntime(t, "beta")
	rt.Stop()
	if rt.State() != harnessprotocol.HarnessStateTerminated {
		t.Fatal("stop 后相位应为 TERMINATED")
	}
	rt.Stop() // 幂等
	for range rt.Outputs() {
		t.Fatal("stop 后输出通道应关闭")
	}
}

// TestReinvokeCliRuntime_每轮新起与后续轮resume 对齐 _run_once 的
// build_turn_command 与每轮重起语义。
func TestReinvokeCliRuntime_每轮新起与后续轮resume(t *testing.T) {
	script := writeScript(t, "oneshot_cli.sh", `echo "out:$*"`)
	adapter, err := BuildAdapter("generic", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Completion = CompletionNone // one-shot 以 stdout EOF 结束轮次
	adapter.PromptFlag = "--prompt"
	adapter.SessionFlag = "--session"
	adapter.Command = []string{script}
	rt := NewReinvokeCliRuntime("gamma", adapter, nil, "",
		"", []string{"--mcp-flag"}, "agent-2")
	rt.SetInactivityTimeoutS(30)
	rt.SetTurnTimeoutS(60)
	if err := rt.Start(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("turn one", false); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateIdle, 10*time.Second)
	// outputs 迭代只在 stop 时终止；先停再收集（同 Python _END 语义）。
	rt.Stop()
	texts := collectOutputs(t, rt.Outputs(), 10*time.Second)
	if len(texts) == 0 || !strings.Contains(texts[0], "--prompt") {
		t.Fatalf("首轮 argv 未含 prompt: %v", texts)
	}
	if !strings.Contains(texts[0], "--session") || !strings.Contains(texts[0], "--mcp-flag") {
		t.Fatalf("首轮 argv 未含 session/launch 参数: %v", texts)
	}
	if rt.SessionID() != nil {
		t.Fatal("CLI runtime 的 session_id 恒空")
	}
}

// TestReinvokeCliRuntime_轮中消息缓冲为followup 对齐 steer 缓冲分支。
func TestReinvokeCliRuntime_轮中消息缓冲为followup(t *testing.T) {
	adapter, err := BuildAdapter("generic", nil)
	if err != nil {
		t.Fatal(err)
	}
	rt := NewReinvokeCliRuntime("gamma", adapter, nil, "", "", nil, "agent-2")
	if err := rt.Start(nil); err != nil {
		t.Fatal(err)
	}
	// 无在飞轮次时 followUp 直接缓冲（Python RUNNING 才缓冲；这里先手工置 RUNNING 验证）
	rt.mu.Lock()
	rt.phase = harnessprotocol.HarnessStateRunning
	rt.mu.Unlock()
	if err := rt.Steer("mid-turn"); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.phase = harnessprotocol.HarnessStateIdle
	rt.mu.Unlock()
	if got, ok := rt.drainPending(); !ok || got != "mid-turn" {
		t.Fatalf("缓冲: %q %v", got, ok)
	}
	if _, ok := rt.drainPending(); ok {
		t.Fatal("排空后应为空")
	}
}

// TestReinvokeCliRuntime_abort抢占 对齐 _abort_turn 的立即终止语义。
func TestReinvokeCliRuntime_abort抢占(t *testing.T) {
	script := writeScript(t, "slow_cli.sh", `sleep 30`)
	adapter, err := BuildAdapter("generic", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Completion = CompletionNone
	adapter.Command = []string{script}
	rt := NewReinvokeCliRuntime("gamma", adapter, nil, "", "", nil, "agent-2")
	rt.SetInactivityTimeoutS(60)
	if err := rt.Start(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("slow", false); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateRunning, 5*time.Second)
	rt.Abort(false)
	waitPhase(t, rt, harnessprotocol.HarnessStateIdle, 10*time.Second)
	rt.Stop()
}

// phaseReader 相位读取的最小接口（两种 runtime 共享 cliRuntimeBase.State）。
type phaseReader interface {
	State() harnessprotocol.HarnessState
}

func waitPhase(t *testing.T, rt phaseReader, want harnessprotocol.HarnessState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rt.State() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("相位未达 %v（当前 %v）", want, rt.State())
}

// failingTransport spawn 必然失败的 transport 探针。
type failingTransport struct{}

func (f *failingTransport) Run(argv []string, env map[string]string, cwd string) (transport.ProcessLike, error) {
	return nil, errors.New("spawn failed: binary not found")
}
func (f *failingTransport) Aclose() error { return nil }

// TestReinvokeCliRuntime_spawn失败传播 对齐 runtime.py:944-966：spawn 异常经
// turn task 传播出 _drive，落 _drive_turn 的 except 分支 → failed 轮次事件、
// 相位回 IDLE（不吞错）。
func TestReinvokeCliRuntime_spawn失败传播(t *testing.T) {
	adapter, err := BuildAdapter("generic", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Completion = CompletionNone
	rt := NewReinvokeCliRuntime("gamma", adapter, nil, "", "", nil, "agent-2")
	rt.SetTransport(&failingTransport{})
	if err := rt.Start(nil); err != nil {
		t.Fatal(err)
	}
	var roundKinds []string
	var mu sync.Mutex
	rt.Subscribe(nil, func(kind string, roundID int) {
		mu.Lock()
		roundKinds = append(roundKinds, kind)
		mu.Unlock()
	})
	if _, err := rt.Send("go", false); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateIdle, 5*time.Second)
	rt.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(roundKinds) < 2 || roundKinds[0] != "started" || roundKinds[len(roundKinds)-1] != "failed" {
		t.Fatalf("spawn 失败应发 failed 轮次事件: %v", roundKinds)
	}
}

// TestExternalCliRuntime_stop不因满输出挂死 对齐 D-15.13-j：输出队列无界
// （Push 永不阻塞），Stop 不依赖消费者排空，恒在有限时间内返回；消费者之后
// 仍能读回全部已缓冲叙述块（Python _END 前队列内容可消费语义）。
func TestExternalCliRuntime_stop不因满输出挂死(t *testing.T) {
	script := writeScript(t, "chatty_cli.sh", `i=0
while [ $i -lt 20000 ]; do
  echo "line:$i"
  i=$((i+1))
done
echo "<<END_OF_TURN>>"`)
	tr := &transport.LocalTransport{}
	proc, err := tr.Run([]string{"/bin/sh", script}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := BuildAdapter("generic", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Completion = CompletionMarkerPrefix + "<<END_OF_TURN>>"
	rt := NewExternalCliRuntime("beta", adapter, NewStdinPipeInjector(proc.Stdin()),
		proc.Stdout(), proc, tr, "agent-1")
	if err := rt.Start(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("go", false); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateIdle, 10*time.Second)
	// 不启动消费者，直接 Stop；有界队列（容量 1024 < 20000 行）下此调用挂死。
	done := make(chan struct{})
	go func() {
		rt.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 挂死：输出队列有界导致轮次泵阻塞")
	}
	// Stop 后仍能读回全部缓冲叙述块（20000 行 + 完成哨兵行）
	texts := collectOutputs(t, rt.Outputs(), 5*time.Second)
	if len(texts) != 20001 {
		t.Fatalf("Stop 后应读回全部缓冲块: %d", len(texts))
	}
}

// TestCliRuntimeBase_resume空串仍发送 对齐 runtime.py:436 `query is not None`：
// 空串是合法输入，照常投递（附时钟锚），开新一轮。
func TestCliRuntimeBase_resume空串仍发送(t *testing.T) {
	rt, _ := newEchoStreamingRuntime(t, "beta")
	var roundKinds []string
	var mu sync.Mutex
	rt.Subscribe(nil, func(kind string, roundID int) {
		mu.Lock()
		roundKinds = append(roundKinds, kind)
		mu.Unlock()
	})
	if err := rt.Resume(""); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateIdle, 5*time.Second)
	rt.Stop()
	mu.Lock()
	defer mu.Unlock()
	if len(roundKinds) < 2 || roundKinds[0] != "started" || roundKinds[len(roundKinds)-1] != "finished" {
		t.Fatalf("空串 resume 应照常开轮: %v", roundKinds)
	}
}

// TestCliRuntimeBase_resume终止后NoOp 对齐 runtime.py:436 TERMINATED 短路。
func TestCliRuntimeBase_resume终止后NoOp(t *testing.T) {
	rt, _ := newEchoStreamingRuntime(t, "beta")
	rt.Stop()
	if err := rt.Resume("late"); err != nil {
		t.Fatalf("terminated 后 resume 应 no-op: %v", err)
	}
	if rt.State() != harnessprotocol.HarnessStateTerminated {
		t.Fatal("相位应保持 TERMINATED")
	}
}

// TestReinvokeCliRuntime_首轮空prompt照常重起 对齐 runtime.py:882
// `while prompt is not None`：空串是合法 prompt，照常发起子进程。
func TestReinvokeCliRuntime_首轮空prompt照常重起(t *testing.T) {
	script := writeScript(t, "oneshot_cli.sh", `echo "out:$#"`)
	adapter, err := BuildAdapter("generic", nil)
	if err != nil {
		t.Fatal(err)
	}
	adapter.Completion = CompletionNone
	adapter.PromptFlag = "--prompt"
	adapter.Command = []string{script}
	rt := NewReinvokeCliRuntime("gamma", adapter, nil, "", "", nil, "agent-2")
	rt.SetInactivityTimeoutS(30)
	if err := rt.Start(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Send("", false); err != nil {
		t.Fatal(err)
	}
	waitPhase(t, rt, harnessprotocol.HarnessStateIdle, 10*time.Second)
	rt.Stop()
	texts := collectOutputs(t, rt.Outputs(), 10*time.Second)
	// 空串 prompt 照常传参：子进程跑起来并输出（$# 为 2 = flag + 空参）
	if len(texts) == 0 || !strings.Contains(texts[0], "out:2") {
		t.Fatalf("空 prompt 应照常重起子进程: %v", texts)
	}
}
