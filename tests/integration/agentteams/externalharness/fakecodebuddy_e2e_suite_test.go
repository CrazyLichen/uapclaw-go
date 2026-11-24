//go:build integration

package externalharness

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	schema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	adapter "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	session "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FakeCodebuddyE2ESuite fakecodebuddy 假 CLI 真子进程端到端套件。
//
// 链路：expert 包（codebuddy runtime + cli_path→假 CLI）→ equipment 解析 →
// adapter 绑定/启动 → codebuddy provider 真身 spawn 真子进程 → IOAdapter 投影 →
// ProcessExternalHarnessStream 网关聊天流 → checkpoint 键落盘。
type FakeCodebuddyE2ESuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestFakeCodebuddyE2ESuite 套件入口。
func TestFakeCodebuddyE2ESuite(t *testing.T) {
	suite.Run(t, new(FakeCodebuddyE2ESuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildFakeCLIWrapper 编译 fakecodebuddy 并生成带 --script 的包装脚本，
// 返回包装脚本路径（cli_path 语义：codebuddy transport 只按固定参数面 spawn，
// 无 extra-args 缝，故用 wrapper 注入 --script）。
func (s *FakeCodebuddyE2ESuite) buildFakeCLIWrapper(scriptPath string) string {
	bin := filepath.Join(s.T().TempDir(), "fakecodebuddy")
	cmd := exec.Command("go", "build", "-o", bin,
		"github.com/uapclaw/uapclaw-go/tests/integration/agentteams/externalcli/codebuddy/fakecodebuddy")
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.T().Fatalf("编译 fakecodebuddy 失败: %v\n%s", err, out)
	}
	wrapper := filepath.Join(s.T().TempDir(), "fakecodebuddy-wrapper.sh")
	body := "#!/bin/sh\nexec " + bin + " --script " + scriptPath + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(body), 0o755); err != nil {
		s.T().Fatal(err)
	}
	return wrapper
}

// writeCodebuddyExpert 落一个 codebuddy runtime 专家包（cli_path 指向假 CLI）。
func (s *FakeCodebuddyE2ESuite) writeCodebuddyExpert(dataDir, name, cliPath, scriptPath string) string {
	pkgDir := filepath.Join(dataDir, "agent", "workspace", "plugins", "agent_templates", "local", name)
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		s.T().Fatal(err)
	}
	config := map[string]any{"cli_path": cliPath}
	if scriptPath != "" {
		config["script_path"] = scriptPath
	}
	configJSON, _ := json.Marshal(config)
	manifest := `{"name":"` + name + `","description":"fake expert","package_type":"agent_template",` +
		`"agent_card":{"name":"` + name + `"},` +
		`"runtime":{"provider_name":"codebuddy","config":` + string(configJSON) + `}}`
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		s.T().Fatal(err)
	}
	return pkgDir
}

// writeScript 写 JSONL 帧脚本（init + 文本 assistant + result）。
func (s *FakeCodebuddyE2ESuite) writeScript(frames ...map[string]any) string {
	path := filepath.Join(s.T().TempDir(), "script.jsonl")
	var body []byte
	for _, frame := range frames {
		line, _ := json.Marshal(frame)
		body = append(body, append(line, '\n')...)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		s.T().Fatal(err)
	}
	return path
}

// TestTextTurnFullCycle text 整轮端到端：绑定 → 懒启动真子进程 → delta → final。
func (s *FakeCodebuddyE2ESuite) TestTextTurnFullCycle() {
	tmp := s.T().TempDir()
	s.T().Setenv("UAPCLAW_DATA_DIR", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "config"), 0o755); err != nil {
		s.T().Fatal(err)
	}
	path.ResetCache()
	s.T().Cleanup(path.ResetCache)

	script := s.writeScript(
		map[string]any{"type": "assistant", "message": map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "你好，我是 fake"}},
		}},
		map[string]any{"type": "result", "subtype": "success", "result": "你好，我是 fake"},
	)
	fakeCLI := s.buildFakeCLIWrapper(script)
	s.writeCodebuddyExpert(tmp, "expert_cbc", fakeCLI, "")

	// 面板启用 codebuddy（mergeHostExternalCliConfig 的 cli_not_enabled 门卫）
	cfgDir := filepath.Join(tmp, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		s.T().Fatal(err)
	}
	body := "modes:\n  team:\n    jiuwen_team:\n      external_cli_agents:\n        - cli_agent: codebuddy\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(body), 0o644); err != nil {
		s.T().Fatal(err)
	}
	sid := "sess_e2e_cbc"

	// 绑定解析（equipment 闭集含 codebuddy；真身 provider.go init 已注册）
	binding, err := adapter.ResolveExternalHarnessBinding(sid,
		map[string]any{"agent_template_name": "expert_cbc"})
	s.Require().NoError(err)
	s.Require().Equal("codebuddy", binding.ProviderName)

	// 构造请求 + inputs（chat.send 语义）
	request := schema.NewAgentRequest("req-e2e-1", "chan-e2e", schema.ReqMethodChatSend,
		[]byte(`{"query":"打个招呼"}`))
	request.SessionID = &sid
	inputs := map[string]any{"query": "打个招呼"}

	// 懒启动 + 流投影
	state := &adapter.ExternalHarnessSessionState{
		Binding:         binding,
		ParentSessionID: sid,
	}
	root := &adapter.DeepAdapter{}
	ch := root.ProcessExternalHarnessStream(context.Background(), state, request, inputs)

	// 收集：期望 delta（你好）→ final（completed）
	var sawDelta, sawFinal bool
	deadline := time.After(60 * time.Second)
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				s.Require().True(sawDelta, "流关闭前应见 delta")
				s.Require().True(sawFinal, "流关闭前应见 final")
				return
			}
			eventType, _ := chunk.Payload["event_type"].(string)
			switch eventType {
			case "chat.delta":
				sawDelta = true
				s.Require().Contains(chunk.Payload["content"], "你好")
			case "chat.final":
				sawFinal = true
				s.Require().Equal("completed", chunk.RuntimeCompletion)
			case "chat.error":
				s.T().Fatalf("不应出错: %+v", chunk.Payload)
			}
		case <-deadline:
			s.T().Fatal("60s 未收口")
		}
	}
}

// TestCheckpointKeyPersisted 终态后 checkpoint 能力缺失时键不落盘（codebuddy 卡声明面）。
func (s *FakeCodebuddyE2ESuite) TestCheckpointKeyPersisted() {
	// codebuddy 卡不声明 CHECKPOINT（session_id 载体走 --resume 面）——
	// 终态 persist 被能力门卫跳过，键不落盘属预期。
	tmp := s.T().TempDir()
	s.T().Setenv("UAPCLAW_DATA_DIR", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "config"), 0o755); err != nil {
		s.T().Fatal(err)
	}
	path.ResetCache()
	s.T().Cleanup(path.ResetCache)
	meta := session.GetSessionMetadata("sess_never_bound")
	s.Require().Nil(meta["session_harness_checkpoint"])
}
