package externalcli

import (
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
	if err := rt.Start(); err != nil {
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
	if rt.SessionID() != "" {
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
	if err := rt.Start(); err != nil {
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
	if rt.SessionID() != "" {
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
	if err := rt.Start(); err != nil {
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
	if got := rt.drainPending(); got != "mid-turn" {
		t.Fatalf("缓冲: %q", got)
	}
	if rt.drainPending() != "" {
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
	if err := rt.Start(); err != nil {
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
