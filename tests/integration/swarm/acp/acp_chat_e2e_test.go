//go:build integration

package acp_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	s.Regexp(`^echo:hello:pid=\d+$`, s.invoke(cfg, profile, "hello", false))
}

// TestAcpChatTool_会话缓存复用 同 profile 连续两次调用复用同一子进程（Go 增强）
func (s *AcpChatToolSuite) TestAcpChatTool_会话缓存复用() {
	cfg, profile := s.newConfig("echo", nil)
	first := pidOf(s.T(), s.invoke(cfg, profile, "one", false))
	second := pidOf(s.T(), s.invoke(cfg, profile, "two", false))
	s.Equal(first, second, "同 profile 第二次应复用缓存子进程")
}

// TestAcpChatTool_newSession重启 new_session=true 重启子进程（Go 增强）
func (s *AcpChatToolSuite) TestAcpChatTool_newSession重启() {
	cfg, profile := s.newConfig("echo", nil)
	first := pidOf(s.T(), s.invoke(cfg, profile, "one", false))
	second := pidOf(s.T(), s.invoke(cfg, profile, "one", true))
	s.NotEqual(first, second, "new_session=true 应拉起新子进程")
}

// TestAcpChatTool_JSONRPC错误转文本 JSON-RPC error 转 [ERROR] 文本且不抛错（Python error 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_JSONRPC错误转文本() {
	cfg, profile := s.newConfig("error", nil)
	out := s.invoke(cfg, profile, "trigger", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "-32603")
	s.Contains(out, "agent failed")
}

// TestAcpChatTool_权限自动批准 自动应答 allow-once（Python permission 模式 + env 开关）
func (s *AcpChatToolSuite) TestAcpChatTool_权限自动批准() {
	s.T().Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "true")
	cfg, profile := s.newConfig("permission", nil)
	s.Equal("after-perm:need perm:selected:allow-once", s.invoke(cfg, profile, "need perm", false))
}

// TestAcpChatTool_权限取消 自动应答 cancelled（Go 增强：Python 只测了批准分支）
func (s *AcpChatToolSuite) TestAcpChatTool_权限取消() {
	s.T().Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "false")
	cfg, profile := s.newConfig("permission", nil)
	s.Contains(s.invoke(cfg, profile, "need perm", false), "after-perm:need perm:cancelled:")
}

// TestAcpChatTool_fs读写穿透 对端 fs/read（line/limit 切片）与 fs/write 均生效（Python fs_read 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_fs读写穿透() {
	root := s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(root, "in_root.txt"),
		[]byte("line1\nline2\nline3\n"), 0o644))
	// 对齐 Python monkeypatch.chdir(tmp_path)：对端发的是相对路径，
	// 客户端按「进程 cwd」解析后校验是否落在会话 cwd（cwd=root）内。
	s.T().Chdir(root)

	profile := fmt.Sprintf("it_fsread_%d", time.Now().UnixNano())
	cfg := map[string]any{"acp_agents": map[string]any{
		profile: map[string]any{
			"command": fakeAcpBin,
			"args":    []any{"fsread"},
			"cwd":     root,
		},
	}}

	out := s.invoke(cfg, profile, "read file", false)
	// line=1 limit=2 → 只返回前两行；客户端聚合后 TrimSpace 去尾换行
	s.Equal("line1\nline2", out)

	written, err := os.ReadFile(filepath.Join(root, "written.txt"))
	s.Require().NoError(err, "对端 fs/write_text_file 应落盘")
	s.Equal("hello-acp", string(written))
}

// TestAcpChatTool_Profile缺失 未知 profile 转 [ERROR] 文本（Python _get_spec 返回 None）
func (s *AcpChatToolSuite) TestAcpChatTool_Profile缺失() {
	cfg := map[string]any{"acp_agents": map[string]any{}}
	out := s.invoke(cfg, "no_such_profile", "hi", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "unknown acp_agents profile")
}

// TestAcpChatTool_Profile无command command 为空转 [ERROR] 文本（Python _run_once needs command）
func (s *AcpChatToolSuite) TestAcpChatTool_Profile无command() {
	profile := fmt.Sprintf("it_nocmd_%d", time.Now().UnixNano())
	cfg := map[string]any{"acp_agents": map[string]any{
		profile: map[string]any{"command": "   ", "args": []any{"echo"}},
	}}
	out := s.invoke(cfg, profile, "hi", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "needs non-empty command")
}

// TestAcpChatTool_args非列表 args 非列表转 [ERROR] 文本（Python args 类型校验，Go 走真链路）
func (s *AcpChatToolSuite) TestAcpChatTool_args非列表() {
	profile := fmt.Sprintf("it_badargs_%d", time.Now().UnixNano())
	cfg := map[string]any{"acp_agents": map[string]any{
		profile: map[string]any{"command": fakeAcpBin, "args": "echo"},
	}}
	out := s.invoke(cfg, profile, "hi", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "args must be a list of strings")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pidOf 从 "echo:<text>:pid=<n>" 中取出 pid
func pidOf(t *testing.T, out string) string {
	t.Helper()
	idx := strings.LastIndex(out, "pid=")
	if idx < 0 {
		t.Fatalf("输出缺少 pid 标记: %q", out)
	}
	return out[idx+len("pid="):]
}

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
