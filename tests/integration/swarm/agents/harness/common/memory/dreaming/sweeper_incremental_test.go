//go:build integration

package dreaming

import (
	"errors"
	"os"
	"path/filepath"
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

// TestSweep_Code模式写consolidated 测试 code 模式写出 consolidated_*.md。
// 对齐 Python: TestDreamingE2ECode.test_sweep_writes_consolidated_files
func (s *SweeperIncrementalSuite) TestSweep_Code模式写consolidated() {
	env := s.newEnv()
	outDir := env.OutputDir("coding_memory")
	for _, sid := range []string{"sess-010", "sess-011", "sess-012"} {
		env.PlantSession(s.T(), sid, "code.normal", 5)
	}
	items := []map[string]string{
		{"title": "Python 性能优化", "content": "使用列表推导式替代 for 循环"},
		{"title": "Git 工作流最佳实践", "content": "推荐使用 feature branch + rebase"},
	}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	s.newSweeper(env, outDir, "code").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	files, err := filepath.Glob(filepath.Join(outDir, "consolidated_*.md"))
	s.Require().NoError(err)
	s.Len(files, 2, "内容相同的条目按 hash 归并，应只有 2 个文件")
	for _, f := range files {
		raw, readErr := os.ReadFile(f)
		s.Require().NoError(readErr)
		text := string(raw)
		s.Contains(text, "---")
		s.Contains(text, "name:")
		s.Contains(text, "source_session:")
		s.Contains(text, "created_at:")
	}
}

// TestSweep_Code内容哈希去重 测试同内容条目只落一个文件。
// 对齐 Python: TestDreamingE2ECode.test_content_hash_dedup
func (s *SweeperIncrementalSuite) TestSweep_Code内容哈希去重() {
	env := s.newEnv()
	outDir := env.OutputDir("coding_memory")
	for _, sid := range []string{"sess-020", "sess-021", "sess-022"} {
		env.PlantSession(s.T(), sid, "code.normal", 5)
	}
	shared := "完全相同的知识内容用于测试去重"
	items := []map[string]string{
		{"title": "知识点A", "content": shared},
		{"title": "知识点B", "content": shared},
	}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	s.newSweeper(env, outDir, "code").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	files, err := filepath.Glob(filepath.Join(outDir, "consolidated_*.md"))
	s.Require().NoError(err)
	s.Len(files, 1, "同内容只应产生 1 个文件")
}

// TestSweep_LLM报错无知识且会话仍记为已扫 测试模型报错时的实际落盘语义。
//
// 对齐 Python 的真实行为（非注释意图）：run_sweep 的 except 分支不可达，
// 因为 _extract_via_llm 内部吞掉全部异常返回 []；Go 的 extractViaLLM 同样把
// 错误降级为 (nil, nil)（sweeper.go S-02）。因此会话仍被记为已扫、不会重试。
func (s *SweeperIncrementalSuite) TestSweep_LLM报错无知识且会话仍记为已扫() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	env.QueueError(errors.New("mock llm failure"))
	env.QueueError(errors.New("mock llm failure"))
	env.QueueError(errors.New("mock llm failure"))

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount(), "三个会话仍应各尝试一次")
	s.Empty(env.ReadDreamingMD(outDir), "报错时不应产出任何知识")

	cp := env.ReadCheckpoint(s.T(), outDir)
	s.Len(cp.ScannedSessions, 3, "报错视为成功，检查点仍应推进（不会重试）")
	s.Equal(5, cp.ScannedSessions["sess-001"].RoundCount)

	env.Reset()
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(0, s.MockLLM.InvokeCallCount(), "已扫会话不应重试")
}

// TestSweep_新会话不足3且无过期会话则整轮跳过 测试 MIN_NEW_SESSIONS 门槛。
func (s *SweeperIncrementalSuite) TestSweep_新会话不足3且无过期会话则整轮跳过() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-002"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	env.QueueJSON(s.T(), []map[string]string{{"title": "不应产生", "content": "x"}})

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(0, s.MockLLM.InvokeCallCount(), "2 个新会话 < 3，不应调用模型")
	s.Empty(env.ReadDreamingMD(outDir))
}

// TestSweep_存在7天前会话则绕过门槛 测试 _MIN_SESSION_AGE_BYPASS_DAYS 旁路。
func (s *SweeperIncrementalSuite) TestSweep_存在7天前会话则绕过门槛() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	env.PlantSession(s.T(), "sess-old", "agent", 5)
	env.PlantSession(s.T(), "sess-new", "agent", 5)
	env.Touch(s.T(), "sess-old", -8*24*time.Hour)

	items := []map[string]string{{"title": "旁路知识", "content": "门槛被绕过"}}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(2, s.MockLLM.InvokeCallCount(), "存在 8 天旧会话时应绕过 3 会话门槛")
	s.Contains(env.ReadDreamingMD(outDir), "旁路知识")
}

// TestSweep_超30天会话标记已扫并丢弃 测试 _MAX_SESSION_AGE_DAYS 丢弃规则。
//
// 注意 saveCheckpoint 只在有成功会话时落盘，因此需另配 2 个正常会话
// 让检查点真正写出，才能观察被丢弃会话的零值快照。
func (s *SweeperIncrementalSuite) TestSweep_超30天会话标记已扫并丢弃() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	env.PlantSession(s.T(), "sess-ancient", "agent", 5)
	env.PlantSession(s.T(), "sess-b", "agent", 5)
	env.PlantSession(s.T(), "sess-c", "agent", 5)
	env.Touch(s.T(), "sess-ancient", -31*24*time.Hour)

	env.QueueJSON(s.T(), []map[string]string{{"title": "正常知识B", "content": "b"}})
	env.QueueJSON(s.T(), []map[string]string{{"title": "正常知识C", "content": "c"}})

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(2, s.MockLLM.InvokeCallCount(), "超 30 天会话不应被提取")

	content := env.ReadDreamingMD(outDir)
	s.Contains(content, "正常知识B")
	s.Contains(content, "正常知识C")

	cp := env.ReadCheckpoint(s.T(), outDir)
	snapshot, ok := cp.ScannedSessions["sess-ancient"]
	s.True(ok, "被丢弃的会话应被标记为已扫")
	s.Equal(0, snapshot.RoundCount, "标记为空快照")
	s.Equal(float64(0), snapshot.HistoryMtime, "标记为空快照")
}

// TestSweep_增量新轮次不足4则跳过 测试增量侧 minSessionRounds 门槛。
func (s *SweeperIncrementalSuite) TestSweep_增量新轮次不足4则跳过() {
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

	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.AppendRounds(s.T(), sid, 6, 7)
		env.Touch(s.T(), sid, 2*time.Second)
	}

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount(), "新增 2 轮 < 4 轮，应全部跳过")
}

// TestScanNewSessions_模式不匹配跳过 测试 metadata.mode 前缀过滤。
func (s *SweeperIncrementalSuite) TestScanNewSessions_模式不匹配跳过() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	env.PlantSession(s.T(), "sess-code", "code.normal", 5)

	sweeper := s.newSweeper(env, outDir, "agent")
	sessions := sweeper.ScanNewSessions()
	s.Len(sessions, 3, "code.normal 会话不应进入 agent 模式的扫描结果")
	for _, sess := range sessions {
		s.NotEqual("sess-code", sess.SessionID)
	}
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
