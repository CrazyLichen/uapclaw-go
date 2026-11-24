//go:build integration && llm

package real_llm_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	schema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	adapter "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalHarnessCBCSuite 15.18 真实 codebuddy CLI 拉起全链（双守卫：CLI 缺失 Skip）。
//
// 链路：expert 包（codebuddy runtime，真 CLI）→ equipment 解析 → adapter 绑定/
// 懒启动真子进程 → ProcessExternalHarnessStream 真 LLM 整轮。只硬断言确定性契约
// （final 到达且内容非空；错误链上抛），不断言模型行为（feedback-real-llm-assertion-scope）。
type ExternalHarnessCBCSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExternalHarnessCBCSuite 套件入口。
func TestExternalHarnessCBCSuite(t *testing.T) {
	suite.Run(t, new(ExternalHarnessCBCSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestRealCBC_ExternalHarnessTextTurn 真 CLI 经 15.18 全链跑一轮。
func (s *ExternalHarnessCBCSuite) TestRealCBC_ExternalHarnessTextTurn() {
	cliPath := os.Getenv("CODEBUDDY_CLI_PATH")
	if cliPath == "" {
		cliPath = "codebuddy"
	}
	if _, err := exec.LookPath(cliPath); err != nil {
		s.T().Skip("codebuddy CLI 不在 PATH，跳过真跑")
	}
	model := os.Getenv("CODEBUDDY_MODEL")
	if model == "" {
		model = "hy4flash-free"
	}

	tmp := s.T().TempDir()
	s.T().Setenv("UAPCLAW_DATA_DIR", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "config"), 0o755); err != nil {
		s.T().Fatal(err)
	}
	path.ResetCache()
	s.T().Cleanup(path.ResetCache)

	// 专家包：真 CLI + 真模型
	pkgDir := filepath.Join(tmp, "agent", "workspace", "plugins", "agent_templates", "local", "expert_real")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		s.T().Fatal(err)
	}
	manifest := `{"name":"expert_real","description":"real codebuddy expert","package_type":"agent_template",` +
		`"agent_card":{"name":"expert_real"},` +
		`"runtime":{"provider_name":"codebuddy","config":{"cli_path":"` + cliPath + `","model":"` + model + `"}}}`
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		s.T().Fatal(err)
	}
	// 面板启用 codebuddy
	cfgDir := filepath.Join(tmp, "config")
	body := "modes:\n  team:\n    jiuwen_team:\n      external_cli_agents:\n        - cli_agent: codebuddy\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(body), 0o644); err != nil {
		s.T().Fatal(err)
	}

	sid := "sess_real_cbc"
	binding, err := adapter.ResolveExternalHarnessBinding(sid,
		map[string]any{"agent_template_name": "expert_real"})
	s.Require().NoError(err)
	s.Require().Equal("codebuddy", binding.ProviderName)

	request := schema.NewAgentRequest("req-real-1", "chan-real", schema.ReqMethodChatSend,
		[]byte(`{"query":"只回复两个字：好的"}`))
	request.SessionID = &sid
	state := &adapter.ExternalHarnessSessionState{
		Binding:         binding,
		ParentSessionID: sid,
	}
	root := &adapter.DeepAdapter{}
	ch := root.ProcessExternalHarnessStream(context.Background(), state, request,
		map[string]any{"query": "只回复两个字：好的"})

	// 确定性契约：流在时限内收口且 final 非空；非 final 的 chat.error 视为真跑失败链
	var finalContent string
	var sawError string
	deadline := time.After(180 * time.Second)
	closed := false
	for !closed {
		select {
		case chunk, ok := <-ch:
			if !ok {
				closed = true
				break
			}
			switch chunk.Payload["event_type"] {
			case "chat.final":
				if content, _ := chunk.Payload["content"].(string); content != "" {
					finalContent = content
				}
			case "chat.error":
				if text, _ := chunk.Payload["error"].(string); text != "" {
					sawError = text
				}
			}
		case <-deadline:
			s.T().Fatal("180s 未收口")
		}
	}
	if sawError != "" && finalContent == "" {
		s.T().Fatalf("真跑失败链上抛: %s", sawError)
	}
	s.Require().NotEmpty(finalContent, "final 内容应非空")
	s.Require().True(strings.TrimSpace(finalContent) != "")
}
