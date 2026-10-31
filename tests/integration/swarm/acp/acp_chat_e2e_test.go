//go:build integration

package acp_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	acpchat "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/acp_chat"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpChatToolSuite 出站 acp_chat 工具链套件
type AcpChatToolSuite struct {
	isuite.BaseIntegrationSuite
	// root 对端子进程工作目录（fs 读写用例的 cwd 边界）
	root string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// fakeAcpBin TestMain 预编译的假对端二进制路径（空 = 无 go 工具链，用例 Skip）
var fakeAcpBin string

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMain 预编译 testdata/fakeacp（对齐 internal/swarm/extensions/plugin/e2e_test.go 先例）
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "uapclaw-acp-itest-*")
	if err != nil {
		os.Exit(1)
	}
	bin := filepath.Join(tmp, "fakeacp")
	build := exec.Command("go", "build", "-o", bin, "./testdata/fakeacp/")
	build.Env = append(os.Environ(), "GO111MODULE=on")
	if _, err := build.CombinedOutput(); err == nil {
		fakeAcpBin = bin
	}
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// TestAcpChatToolSuite 运行出站 suite
func TestAcpChatToolSuite(t *testing.T) {
	suite.Run(t, new(AcpChatToolSuite))
}

// SetupSuite 预检假对端二进制 + 准备 cwd 根
func (s *AcpChatToolSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	if fakeAcpBin == "" {
		s.T().Skip("go 工具链不可用，无法编译 testdata/fakeacp")
	}
	s.root = s.T().TempDir()
}

// TestAcpChatTool_Echo文本聚合 session/update 文本聚合（对应 Python echo 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_Echo文本聚合() {
	cfg, profile := s.newConfig("echo", nil)
	s.Equal("echo:hello", s.invoke(cfg, profile, "hello", false))
}

// TestAcpChatTool_JSONRPC错误转文本 JSON-RPC error 转 [ERROR] 文本且不抛错（Python error 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_JSONRPC错误转文本() {
	cfg, profile := s.newConfig("error", nil)
	out := s.invoke(cfg, profile, "trigger", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "-32603")
	s.Contains(out, "agent failed")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newConfig 构造唯一 profile 的 cfg（profile 名含纳秒时间戳，规避全局缓存串台）
func (s *AcpChatToolSuite) newConfig(mode string, extra map[string]any) (map[string]any, string) {
	profile := fmt.Sprintf("it_%s_%d", mode, time.Now().UnixNano())
	spec := map[string]any{
		"command": fakeAcpBin,
		// 注意：BuildClientSpec 要求 []any（写 []string 会被判为非法 args）
		"args": []any{mode},
		"cwd":  s.root,
	}
	for k, v := range extra {
		spec[k] = v
	}
	cfg := map[string]any{"acp_agents": map[string]any{profile: spec}}
	return cfg, profile
}

// invoke 经工具卡片执行一次（真实调用 tool.ValidateToolCard / tool.WithTimeout）
func (s *AcpChatToolSuite) invoke(cfg map[string]any, profile, message string, newSession bool) string {
	tools := acpchat.NewAcpChatTool(cfg).GetTools()
	s.Require().Len(tools, 1, "acp_chat 应只注册一个工具")
	s.Require().NoError(tool.ValidateToolCard(tools[0].Card()))

	inputs := map[string]any{"agent": profile, "message": message}
	if newSession {
		inputs["new_session"] = true
	}
	ctx, cancel := context.WithTimeout(s.Ctx, 30*time.Second)
	defer cancel()
	res, err := tools[0].Invoke(ctx, inputs, tool.WithTimeout(30))
	s.Require().NoError(err, "工具不应向调用方抛错")
	out, ok := res["result"].(string)
	s.Require().True(ok, "result 应为字符串，实际 %T", res["result"])
	return out
}
