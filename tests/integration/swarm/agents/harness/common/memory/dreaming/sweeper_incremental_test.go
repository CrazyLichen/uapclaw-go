//go:build integration

package dreaming

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SweeperIncrementalSuite 测试 dreaming 增量管线的端到端语义。
//
// 对齐 Python: jiuwenswarm-develop/tests/system_tests/test_dreaming_e2e.py
// 与 Python 的差别：Python 用 mock.patch 替换 _extract_via_llm；Go 通过
// testhelpers.NewDreamingEnv 注册 mock provider，让 extractViaLLM 真实执行
// （提示词模板 + ```json 围栏 + JSON 解析），模型响应由队列预置。
type SweeperIncrementalSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSweeperIncrementalSuite(t *testing.T) {
	suite.Run(t, new(SweeperIncrementalSuite))
}

// TestSweep_首次扫描写出知识与检查点 测试一次 sweep 写出 DREAMING.md 与 checkpoint。
// 对齐 Python: TestDreamingE2EAgent.test_sweep_writes_dreaming_md
func (s *SweeperIncrementalSuite) TestSweep_首次扫描写出知识与检查点() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	items := []map[string]string{
		{"title": "用户偏好 Python", "content": "用户倾向于使用 Python 开发"},
		{"title": "项目使用 FastAPI", "content": "当前项目框架是 FastAPI"},
	}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	sweeper := s.newSweeper(env, outDir, "agent")
	sweeper.RunSweep(s.Ctx)

	s.Equal(3, s.MockLLM.InvokeCallCount(), "三个会话各应调用一次模型")

	content := env.ReadDreamingMD(outDir)
	entries := swarmdreaming.ParseDreamingEntries(content)
	s.Len(entries, 2, "两条不同标题应各保留一条")
	s.Contains(content, "_source: sess-001", "条目应记录来源会话")

	cp := env.ReadCheckpoint(s.T(), outDir)
	s.Len(cp.ScannedSessions, 3, "三个会话都应写入 checkpoint")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		snapshot, ok := cp.ScannedSessions[sid]
		s.True(ok, "%s 应在 checkpoint 中", sid)
		s.Equal(5, snapshot.RoundCount, "%s 轮次应记录为 5", sid)
		s.Greater(snapshot.HistoryMtime, float64(0), "%s mtime 应被记录", sid)
	}
	s.NotEmpty(cp.LastScanTs, "last_scan_ts 不应为空")
}

// TestSweep_第二次同标题不重复 测试跨轮 sweep 的标题去重。
//
// 对齐 Python: TestDreamingE2EAgent.test_dedup_skips_existing_title
// 但修正其空转缺陷：Python 第二轮只新增 2 个会话（<MIN_NEW_SESSIONS=3），
// scan_new_sessions 直接返回空列表，第二轮 sweep 从未执行、断言恒真。
// 本用例第二轮补足 3 个新会话，让门槛放行并真正命中去重分支。
func (s *SweeperIncrementalSuite) TestSweep_第二次同标题不重复() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-a", "sess-b", "sess-c"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	first := []map[string]string{{"title": "用户偏好 Python", "content": "第一轮提取"}}
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	for _, sid := range []string{"sess-002", "sess-003", "sess-004"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	second := []map[string]string{{"title": "用户偏好 Python", "content": "第二轮提取（应被去重）"}}
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(6, s.MockLLM.InvokeCallCount(), "第二轮应真的执行了三次提取")

	content := env.ReadDreamingMD(outDir)
	s.Equal(1, strings.Count(content, "## 用户偏好 Python"), "同标题只应保留一条")
	s.NotContains(content, "第二轮提取", "去重命中后内容应保持第一轮")
}

// TestSweep_增量只重扫新轮次 测试已扫会话追加轮次后只压缩新增部分。
//
// 对齐 Python: TestDreamingE2EAgent.test_incremental_scan_reprocesses_updated_session
// 并加强：断言第二轮 prompt 含新问题、不含首轮问题（Python 只断言产物共存）。
func (s *SweeperIncrementalSuite) TestSweep_增量只重扫新轮次() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	first := []map[string]string{{"title": "第一轮知识", "content": "第一次扫描提取"}}
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.AppendRounds(s.T(), sid, 6, 9)
		env.Touch(s.T(), sid, 2*time.Second)
	}
	second := []map[string]string{{"title": "第二轮新增知识", "content": "增量扫描提取"}}
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(6, s.MockLLM.InvokeCallCount(), "三个会话各应重扫一次")

	prompts := env.UserPrompts()
	s.Require().Len(prompts, 6)
	incrementalPrompt := prompts[3]
	s.Contains(incrementalPrompt, "第6个问题", "增量 prompt 应含新轮次")
	s.Contains(incrementalPrompt, "第9个问题", "增量 prompt 应含最后一轮新轮次")
	s.NotContains(incrementalPrompt, "第1个问题", "增量 prompt 不应重复首轮内容")

	content := env.ReadDreamingMD(outDir)
	s.Contains(content, "第一轮知识")
	s.Contains(content, "第二轮新增知识")

	cp := env.ReadCheckpoint(s.T(), outDir)
	s.Equal(9, cp.ScannedSessions["sess-001"].RoundCount, "checkpoint 轮次应推进到 9")
}

// TestSweep_未变更会话跳过 测试无变更时第二次 sweep 不调用模型且不改文件。
//
// 对齐 Python: TestDreamingE2EAgent.test_skips_unchanged_session（断言更强）
func (s *SweeperIncrementalSuite) TestSweep_未变更会话跳过() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	items := []map[string]string{{"title": "初次知识", "content": "第一次扫描"}}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())
	before := env.ReadDreamingMD(outDir)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount(), "无变更时不应再调用模型")
	s.Equal(before, env.ReadDreamingMD(outDir), "DREAMING.md 应保持字节级不变")
	s.NotContains(env.ReadDreamingMD(outDir), "第二次扫描不应该发生")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newEnv 构建隔离环境并清空 mock 状态（MockLLM 为套件级共享）。
func (s *SweeperIncrementalSuite) newEnv() *testhelpers.DreamingEnv {
	env := testhelpers.NewDreamingEnv(s.T(), s.MockLLM, "dreaming_itest")
	env.Reset()
	return env
}

// newSweeper 在 env 的会话目录上创建 sweeper 并 Init。
func (s *SweeperIncrementalSuite) newSweeper(env *testhelpers.DreamingEnv, outDir, mode string) *swarmdreaming.Sweeper {
	sweeper := swarmdreaming.NewSweeper(env.SessionsDir, outDir, mode, "zh")
	s.Require().NoError(sweeper.Init())
	return sweeper
}
