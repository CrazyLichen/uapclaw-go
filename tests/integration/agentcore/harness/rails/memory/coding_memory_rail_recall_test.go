//go:build integration

package memory

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	// 空白导入：触发 GlobalRegistry 中 fs/shell/code 操作的 init 注册
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryRailRecallSuite 测试 CodingMemoryRail 的真实召回链路。
//
// 与 coding_memory_rail_test.go / coding_memory_rail_deep_test.go 的区别：
// 那两个 Suite 用 NewCodingMemoryRail(dir, nil, lang) 且不注入 Workspace/SysOperation，
// autoRecall 只会在降级分支上打转——真实召回链路
// （Search → 读文件 → 抽 body → 字节截断 → 注入全文）从未被覆盖。
//
// 本 Suite 补齐该链路：
//   - 真实 Workspace（含 coding_memory 节点）+ LocalSysOperation 注入 rail
//   - 真实 MemoryIndexManager（SQLite + 词袋 embedding 的本地 httptest 端点）
//   - G1 召回全文注入（frontmatter 剥离）/ G2 跳过 MEMORY.md / G3 10KB 截断
//   - G4 无召回降级注入索引正文 / G5 footer 记忆总数 / G6 updated_at 日期标注
//
// 对应 Python 代码：
//   - openjiuwen/harness/rails/memory/coding_memory_rail.py（_auto_recall / before_model_call）
//   - tests/system_tests/memory/test_coding_memory.py::TestCodingMemoryAutoRecall
//   - tests/system_tests/memory/test_coding_memory.py::TestCodingMemoryPromptInjection
//   - tests/system_tests/harness/test_coding_memory_rail_e2e.py::test_auto_recall_with_results
type CodingMemoryRailRecallSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// recallEmbeddingDims 词袋 embedding 维度（与 baseEmbeddingAdapter 的 1024 对齐）
	recallEmbeddingDims = 1024
	// recallPollAttempts 等待异步预取完成的最大轮询次数
	recallPollAttempts = 200
	// recallPollInterval 每轮轮询间隔
	recallPollInterval = 10 * time.Millisecond
	// recallSettleDelay 确认降级分支稳定的观察窗口（预取 goroutine 早已结束）
	recallSettleDelay = 300 * time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────

// recallTokenRe 词袋分词器
var recallTokenRe = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCodingMemoryRailRecallSuite 运行 CodingMemoryRail 真实召回链路测试套件
func TestCodingMemoryRailRecallSuite(t *testing.T) {
	suite.Run(t, new(CodingMemoryRailRecallSuite))
}

// TestRecall_注入正文且剥离Frontmatter G1：命中的记忆以 body 注入，frontmatter 被剥离。
// 对齐 Python: test_auto_recall_returns_content + test_before_model_call_injects_recall_content
func (s *CodingMemoryRailRecallSuite) TestRecall_注入正文且剥离Frontmatter() {
	files := map[string]string{
		"db_pref.md": "---\nname: Database Preference\ndescription: prefers PostgreSQL\ntype: feedback\n---\n\n" +
			"User prefers PostgreSQL for new projects. Always use PostgreSQL when starting a database.\n",
	}
	rail, agent := s.newRecallEnv(files)

	content := s.driveUntilRecall(rail, agent, "PostgreSQL database preference")

	s.Contains(content, "## 已加载的相关记忆", "命中记忆应注入全文标题")
	s.Contains(content, "### Database Preference [db_pref.md]", "注入内容应带记忆标题与文件路径")
	s.Contains(content, "Always use PostgreSQL", "注入内容应包含记忆 body")
	s.NotContains(content, "name: Database Preference", "frontmatter 应被剥离，不注入 name 字段")
	s.NotContains(content, "type: feedback", "frontmatter 应被剥离，不注入 type 字段")
}

// TestRecall_跳过MEMORY索引文件 G2：索引文件即便命中搜索也不注入正文。
// 对齐 Python: test_auto_recall_skips_memory_md
func (s *CodingMemoryRailRecallSuite) TestRecall_跳过MEMORY索引文件() {
	files := map[string]string{
		"db_pref.md": "---\nname: Database Preference\ndescription: prefers PostgreSQL\ntype: feedback\n---\n\n" +
			"User prefers PostgreSQL for new projects. PostgreSQL is the default database. " +
			"Always choose PostgreSQL for new projects. PostgreSQL PostgreSQL preference.\n",
		"MEMORY.md": "- [Database Preference](db_pref.md) — prefers PostgreSQL PostgreSQL preference\n",
	}
	rail, agent := s.newRecallEnv(files)

	content := s.driveUntilRecall(rail, agent, "PostgreSQL preference")

	s.Contains(content, "## 已加载的相关记忆", "命中记忆应注入全文标题")
	s.Contains(content, "### Database Preference [db_pref.md]", "普通记忆应被注入")
	s.NotContains(content, "### MEMORY.md [MEMORY.md]", "MEMORY.md 自身不应作为召回条目注入")
}

// TestRecall_超出字节上限截断 G3：召回总字节超过 10240 时截断并标注。
// 对齐 Python: test_auto_recall_respects_max_bytes（Python 仅断言常量，Go 验证真实截断）
func (s *CodingMemoryRailRecallSuite) TestRecall_超出字节上限截断() {
	files := map[string]string{}
	for i := 0; i < 3; i++ {
		var b strings.Builder
		fmt.Fprintf(&b, "---\nname: Big Memory %d\ndescription: large payload %d\ntype: reference\n---\n\n", i, i)
		for j := 0; j < 90; j++ {
			fmt.Fprintf(&b, "PostgreSQL large payload line %d for truncation verification purpose.\n", j)
		}
		files[fmt.Sprintf("big_%d.md", i)] = b.String()
	}
	rail, agent := s.newRecallEnv(files)

	content := s.driveUntilRecall(rail, agent, "PostgreSQL large payload truncation")

	s.Contains(content, "## 已加载的相关记忆", "命中记忆应注入全文标题")
	s.Contains(content, "... (truncated)", "超出 10KB 的召回内容应被截断并标注")
}

// TestRecall_无结果降级注入索引 G4：搜索无结果时注入 MEMORY.md 索引正文。
// 对齐 Python: test_before_model_call_fallback_to_index
//
// 构造：目录内只有 MEMORY.md（召回阶段会跳过它），因此召回内容必为空，
// BeforeModelCall 走降级分支注入索引全文。
func (s *CodingMemoryRailRecallSuite) TestRecall_无结果降级注入索引() {
	files := map[string]string{
		"MEMORY.md": "- [Unrelated Note](ui_note.md) — frontend css notes\n" +
			"- [Second Entry](second.md) — placeholder entry\n",
	}
	rail, agent := s.newRecallEnv(files)

	content := s.driveUntilDegradeIndex(rail, agent, "Unrelated Note frontend css")

	s.Contains(content, "## 当前记忆索引", "无召回结果时应注入索引标题")
	s.NotContains(content, "## 已加载的相关记忆", "无召回结果时不应注入全文标题")
	s.Contains(content, "Unrelated Note", "索引注入应包含 MEMORY.md 条目")
	s.Contains(content, "Second Entry", "索引注入应包含 MEMORY.md 全部条目")
}

// TestRecall_footer标注记忆总数 G5：召回注入末尾标注目录内记忆文件数（排除 MEMORY.md）。
func (s *CodingMemoryRailRecallSuite) TestRecall_footer标注记忆总数() {
	files := map[string]string{
		"a.md": "---\nname: Alpha\ndescription: PostgreSQL note alpha\ntype: user\n---\n\nPostgreSQL note alpha.\n",
		"b.md": "---\nname: Beta\ndescription: PostgreSQL note beta\ntype: user\n---\n\nPostgreSQL note beta.\n",
	}
	rail, agent := s.newRecallEnv(files)

	content := s.driveUntilRecall(rail, agent, "PostgreSQL note")

	s.Contains(content, "## 已加载的相关记忆", "命中记忆应注入全文标题")
	s.Contains(content, "共 2 条记忆", "footer 应标注目录内记忆文件总数")
	s.Contains(content, "### Alpha [a.md]", "应注入 a.md 条目")
	s.Contains(content, "### Beta [b.md]", "应注入 b.md 条目")
}

// TestRecall_标题带updated日期标注 G6：frontmatter 的 updated_at 出现在召回标题后缀。
func (s *CodingMemoryRailRecallSuite) TestRecall_标题带updated日期标注() {
	files := map[string]string{
		"dated.md": "---\nname: Dated Memory\ndescription: PostgreSQL with date\ntype: feedback\n" +
			"updated_at: 2026-05-01\n---\n\nPostgreSQL preference recorded on a known date.\n",
	}
	rail, agent := s.newRecallEnv(files)

	content := s.driveUntilRecall(rail, agent, "PostgreSQL preference known date")

	s.Contains(content, "### Dated Memory [dated.md]", "注入内容应带记忆标题与文件路径")
	s.Contains(content, "(updated: 2026-05-01)", "标题应附加 frontmatter 的 updated_at 日期标注")
}

// TearDownTest 清理 ResourceMgr 与 manager 缓存。
//
// 三件事必须做：
//  1. AgentSuite.TearDownTest 清理本 Suite 登记的工具/SysOperation；
//  2. ResourceMgr.Release 重建 registry —— rail Init 按固定 ID 注册 coding_memory
//     三工具，查重命中会复用上一用例绑定的旧 TempDir 工具实例（对齐 Python
//     fixture 的 Runner.stop() → resource_mgr.release()，先例
//     code_adapter_coding_memory_test.go）；
//  3. ClearMemoryManagerCache 关闭进程级 indexCache 中的 manager（持有 SQLite
//     句柄与 fsnotify watcher，不关会跨用例泄漏）。
func (s *CodingMemoryRailRecallSuite) TearDownTest() {
	s.AgentSuite.TearDownTest()
	lite.ClearMemoryManagerCache()
	if rm := s.GetResourceMgr(); rm != nil {
		_ = rm.Release(s.Ctx)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recallBagOfWordsVector 词袋哈希向量（FNV-1a 分桶计数）。
//
// 共享 token 越多 cosine 越高，用来模拟「语义近邻」而不引入真实 embedding 服务：
// 只在网络协议边界替换 provider，被测的 manager / rail 全链路均为真实实现。
func recallBagOfWordsVector(text string) []float64 {
	vec := make([]float64, recallEmbeddingDims)
	for _, tok := range recallTokenRe.FindAllString(strings.ToLower(text), -1) {
		h := uint32(2166136261)
		for i := 0; i < len(tok); i++ {
			h ^= uint32(tok[i])
			h *= 16777619
		}
		vec[h%recallEmbeddingDims]++
	}
	return vec
}

// newRecallEmbeddingServer 启动本地 OpenAI 兼容 embedding 端点。
func (s *CodingMemoryRailRecallSuite) newRecallEmbeddingServer() *httptest.Server {
	s.T().Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var texts []string
		switch v := req.Input.(type) {
		case string:
			texts = []string{v}
		case []any:
			for _, item := range v {
				if str, ok := item.(string); ok {
					texts = append(texts, str)
				}
			}
		}
		data := make([]map[string]any, 0, len(texts))
		for i, txt := range texts {
			data = append(data, map[string]any{"index": i, "embedding": recallBagOfWordsVector(txt)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	s.T().Cleanup(srv.Close)
	return srv
}

// newRecallEnv 构造 rail 运行环境：真实 Workspace + LocalSysOperation + 真实 manager。
//
// 记忆文件先落盘再交给 rail，manager 在 BeforeInvoke 首次初始化时全量索引，
// 与生产「进程重启后已有记忆文件」的路径一致。
func (s *CodingMemoryRailRecallSuite) newRecallEnv(
	files map[string]string,
) (*memrail.CodingMemoryRail, *harness.DeepAgent) {
	s.T().Helper()
	srv := s.newRecallEmbeddingServer()

	root := s.T().TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	nodePath := ws.GetNodePath("coding_memory")
	s.Require().NotNil(nodePath, "Workspace 应包含 coding_memory 节点")
	cmDir := *nodePath
	s.Require().NoError(os.MkdirAll(cmDir, 0o755))

	for name, body := range files {
		path := filepath.Join(cmDir, name)
		s.Require().NoError(os.MkdirAll(filepath.Dir(path), 0o755))
		s.Require().NoError(os.WriteFile(path, []byte(body), 0o644))
	}

	rail := memrail.NewCodingMemoryRail(cmDir, &apiEmbedding.EmbeddingConfig{
		ModelName: "bag-of-words",
		BaseURL:   srv.URL,
		APIKey:    "test-key",
	}, "cn")
	rail.SetWorkspace(ws)

	op, err := sysop.NewSysOperation(sysop.NewSysOperationCard())
	s.Require().NoError(err, "创建 LocalSysOperation 不应失败")
	rail.SetSysOperation(op)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("recall 链路测试"))
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{MaxIterations: 2})
	s.Require().NoError(err)
	s.Require().NoError(rail.Init(s.Ctx, agent))

	return rail, agent
}

// driveUntilRecall 驱动 BeforeInvoke + BeforeModelCall，轮询等待召回全文注入完成。
//
// 召回在 BeforeInvoke 中以 goroutine 预取，BeforeModelCall 非阻塞检查，
// 因此必须轮询到 SectionMemory 出现召回标题（不能依赖固定 sleep）。
func (s *CodingMemoryRailRecallSuite) driveUntilRecall(
	rail *memrail.CodingMemoryRail,
	agent *harness.DeepAgent,
	query string,
) string {
	s.T().Helper()
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString(query)}, nil)
	s.Require().NoError(rail.BeforeInvoke(s.Ctx, cbc))

	var content string
	for i := 0; i < recallPollAttempts; i++ {
		s.Require().NoError(rail.BeforeModelCall(s.Ctx, cbc))
		if sec := agent.SystemPromptBuilder().GetSection(hsections.SectionMemory); sec != nil {
			content = sec.Content["cn"]
		}
		if strings.Contains(content, "## 已加载的相关记忆") {
			return content
		}
		time.Sleep(recallPollInterval)
	}
	s.FailNow("等待召回全文注入超时", "SectionMemory 内容: %s", content)
	return content
}

// driveUntilDegradeIndex 驱动降级分支：等待预取结束后确认注入的是 MEMORY.md 索引。
//
// 与 driveUntilRecall 的差别：降级分支在预取未完成时也可能命中，因此先等到
// 索引标题出现，再等一个观察窗口后重新驱动一次，确认召回 goroutine 结束后
// 仍未切换到全文注入。
func (s *CodingMemoryRailRecallSuite) driveUntilDegradeIndex(
	rail *memrail.CodingMemoryRail,
	agent *harness.DeepAgent,
	query string,
) string {
	s.T().Helper()
	cbc := agentinterfaces.NewAgentCallbackContext(agent,
		&agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString(query)}, nil)
	s.Require().NoError(rail.BeforeInvoke(s.Ctx, cbc))

	var content string
	for i := 0; i < recallPollAttempts; i++ {
		s.Require().NoError(rail.BeforeModelCall(s.Ctx, cbc))
		if sec := agent.SystemPromptBuilder().GetSection(hsections.SectionMemory); sec != nil {
			content = sec.Content["cn"]
		}
		if strings.Contains(content, "## 当前记忆索引") {
			break
		}
		time.Sleep(recallPollInterval)
	}

	// 预取 goroutine 早已结束，再驱动一次确认仍为索引分支
	time.Sleep(recallSettleDelay)
	s.Require().NoError(rail.BeforeModelCall(s.Ctx, cbc))
	if sec := agent.SystemPromptBuilder().GetSection(hsections.SectionMemory); sec != nil {
		content = sec.Content["cn"]
	}
	return content
}
