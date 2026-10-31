//go:build integration

package dreaming

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StartWiringSuite 测试 dreaming 的启停生命周期与 adapter→sweeper 接线。
//
// 接线断言落在副作用上：StartDreaming 内部会 Sweeper.Init()，
// 而 Init 会创建 outputDir 与 outputDir/.dreams。断言这两个目录存在，
// 即可证明 adapter 算出的 sessions/output 目录正确、sweeper 真被初始化。
// 不能等待 tick：Orchestrator 首轮 sweep 需等一个 interval（下限 60s），
// 且 tick() 未导出、无法手动驱动。
type StartWiringSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestStartWiringSuite(t *testing.T) {
	suite.Run(t, new(StartWiringSuite))
}

// SetupTest 每个用例前重置 mock 与全局 orchestrator 注册表。
func (s *StartWiringSuite) SetupTest() {
	s.MockLLM.SetResponses()
	swarmdreaming.StopDreaming(s.Ctx, "")
}

// TearDownTest 用例结束后清理全局注册表，避免跨用例串味。
func (s *StartWiringSuite) TearDownTest() {
	swarmdreaming.StopDreaming(s.Ctx, "")
}

// TestStartStop_启用配置 测试启用配置下启动与停止的注册表行为。
// 对齐 Python: TestDreamingE2ELifecycle.test_start_stop
func (s *StartWiringSuite) TestStartStop_启用配置() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_AGENT_ENABLED", "true")
	outDir := env.OutputDir("memory")

	orch, err := swarmdreaming.StartDreaming(s.Ctx, env.SessionsDir, outDir, "agent", "zh", func() bool { return false })
	s.Require().NoError(err)
	s.Require().NotNil(orch)
	s.Same(orch, swarmdreaming.GetDreamingOrchestrator("agent"), "注册表应返回同一实例")
	s.True(orch.IsRunning())
	s.DirExists(outDir, "启动后 sweeper.Init 应创建输出目录")
	s.DirExists(filepath.Join(outDir, ".dreams"), "启动后应创建 .dreams 目录")

	swarmdreaming.StopDreaming(s.Ctx, "agent")
	s.Nil(swarmdreaming.GetDreamingOrchestrator("agent"), "停止后注册表应为空")
}

// TestStart_禁用配置 测试 enabled=false 时不启动。
// 对齐 Python: TestDreamingE2ELifecycle.test_start_disabled_config
func (s *StartWiringSuite) TestStart_禁用配置() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_AGENT_ENABLED", "false")

	orch, err := swarmdreaming.StartDreaming(s.Ctx, env.SessionsDir, env.OutputDir("memory"), "agent", "zh", nil)
	s.Require().NoError(err)
	s.Nil(orch, "禁用配置应返回 nil")
	s.Nil(swarmdreaming.GetDreamingOrchestrator("agent"))
}

// TestWiring_CodeAdapter真跑后启动Dreaming 测试 code 模式 adapter 全链路接线。
//
// 覆盖：CreateInstance 设置 dreamingMode=code 与 agent/workspace 目录 →
// TryStartDreaming 用 workspace.AgentSessionsDir 与 coding_memory 输出目录
// 调用 StartDreaming → sweeper.Init 落目录 → TryStopDreaming 注销。
func (s *StartWiringSuite) TestWiring_CodeAdapter真跑后启动Dreaming() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_CODE_ENABLED", "true")
	s.Equal(workspace.AgentSessionsDir(), env.SessionsDir, "沙箱 sessions 目录应与生产路径一致")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "code.normal", 5)
	}

	codeAdapter := adapter.NewCodeAdapter()
	s.Require().NoError(codeAdapter.CreateInstance(s.Ctx, map[string]any{"project_dir": filepath.Join(env.DataDir, "project")}, "code", ""))
	s.Require().NoError(codeAdapter.TryStartDreaming(s.Ctx, func() bool { return false }))

	orch := swarmdreaming.GetDreamingOrchestrator("code")
	s.Require().NotNil(orch, "code 模式 orchestrator 应已注册")
	s.True(orch.IsRunning())

	codingMemoryDir := filepath.Join(env.DataDir, "agent", "workspace", "coding_memory")
	s.DirExists(codingMemoryDir, "sweeper.Init 应创建 coding_memory 目录")
	s.DirExists(filepath.Join(codingMemoryDir, ".dreams"), "sweeper.Init 应创建 .dreams 目录")

	s.Require().NoError(codeAdapter.TryStopDreaming(s.Ctx))
	s.Nil(swarmdreaming.GetDreamingOrchestrator("code"))
}

// TestWiring_DeepAdapter真跑后启动Dreaming 测试 agent 模式 adapter 全链路接线。
//
// 生产链路（对齐 Python interface.py:_adapter_mode_for_request 与 Go
// resolveModeFromRequest）把 "agent.plan" 拆成 mode="agent" + subMode="plan"，
// 因此 CreateInstance 收到的 mode 恒为 "agent"：dreamingMode="agent"、
// 配置键 memory.dreaming.agent、环境变量 DREAMING_AGENT_ENABLED，
// 输出目录为 <DataDir>/agent/workspace/memory。
// 注意：Python/Go 的 output_name 判定是 mode == "agent"，若直接把 "agent.plan"
// 传给 CreateInstance 两侧都会落到 coding_memory，这属测试误用而非生产行为。
func (s *StartWiringSuite) TestWiring_DeepAdapter真跑后启动Dreaming() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_AGENT_ENABLED", "true")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "agent.plan", 5)
	}

	deepAdapter := adapter.NewDeepAdapter()
	s.Require().NoError(deepAdapter.CreateInstance(s.Ctx, nil, "agent", ""))
	s.Require().NoError(deepAdapter.TryStartDreaming(s.Ctx, func() bool { return false }))

	orch := swarmdreaming.GetDreamingOrchestrator("agent")
	s.Require().NotNil(orch, "agent 模式 orchestrator 应已注册")
	s.True(orch.IsRunning())

	memoryDir := filepath.Join(env.DataDir, "agent", "workspace", "memory")
	s.DirExists(memoryDir, "sweeper.Init 应创建 memory 目录")
	s.DirExists(filepath.Join(memoryDir, ".dreams"), "sweeper.Init 应创建 .dreams 目录")

	s.Require().NoError(deepAdapter.TryStopDreaming(s.Ctx))
	s.Nil(swarmdreaming.GetDreamingOrchestrator("agent"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newEnv 构建隔离环境。
func (s *StartWiringSuite) newEnv() *testhelpers.DreamingEnv {
	return testhelpers.NewDreamingEnv(s.T(), s.MockLLM, "dreaming_wiring")
}
