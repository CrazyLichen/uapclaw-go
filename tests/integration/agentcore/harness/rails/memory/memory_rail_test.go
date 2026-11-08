//go:build integration

package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	memory "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryRailSuite 测试 MemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 覆盖：
//   - Init 注册 5 个 memory 工具
//   - BeforeInvoke 初始化
//   - BeforeModelCall 注入 SectionMemory（默认/主动/只读模式）
//   - Uninit 清理工具和 Section
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/memory_rail.py
type MemoryRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMemoryRailSuite(t *testing.T) {
	suite.Run(t, new(MemoryRailSuite))
}

// TestMemoryRail_Init注册5个工具 测试 MemoryRail.Init 注册 5 个 memory 工具到 ability_manager。
// 对齐 Python: MemoryRail.init(agent) 中 _register_memory_tools
func (s *MemoryRailSuite) TestMemoryRail_Init注册5个工具() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Init 注册工具测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)
	s.Require().NotNil(agent)

	// 执行 Invoke 触发 Init + BeforeInvoke + BeforeModelCall
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "测试 memory"})
	s.Require().NoError(err, "Invoke 不应返回错误")

	// 验证 ability_manager 包含 5 个 memory 工具
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")
	s.NotNil(am.Get("memory_search"), "应注册 memory_search")
	s.NotNil(am.Get("memory_get"), "应注册 memory_get")
	s.NotNil(am.Get("write_memory"), "应注册 write_memory")
	s.NotNil(am.Get("edit_memory"), "应注册 edit_memory")
	s.NotNil(am.Get("read_memory"), "应注册 read_memory")
}

// TestMemoryRail_BeforeModelCall_默认模式注入 测试 BeforeModelCall 以默认（inactive）模式注入 SectionMemory。
// 对齐 Python: MemoryRail.before_model_call(ctx) 中 BuildMemorySection
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_默认模式注入() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("注入提示词测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// Invoke 触发 BeforeModelCall，注入 SectionMemory
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "记忆提示词"})
	s.Require().NoError(err, "BeforeModelCall 注入记忆节应成功完成")

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb, "SystemPromptBuilder 不应为 nil")
	s.True(spb.HasSection(hsections.SectionMemory), "应存在 SectionMemory 节")

	// 验证节内容非空
	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section, "SectionMemory 节不应为 nil")
	s.NotEmpty(section.Content, "SectionMemory 节内容不应为空")
}

// TestMemoryRail_BeforeModelCall_主动模式内容 测试 BeforeModelCall 以 proactive 模式注入包含主动记录指令的 SectionMemory。
// 对齐 Python: MemoryRail(is_proactive=True) 中 BuildMemorySection(mode="proactive")
func (s *MemoryRailSuite) TestMemoryRail_BeforeModelCall_主动模式内容() {
	rail := memory.NewMemoryRail(nil, true)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("主动模式测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "主动记录"})
	s.Require().NoError(err)

	// 验证 SectionMemory 已注入
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "主动模式应注入 SectionMemory")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	// 主动模式内容应包含"核心操作规范"或"proactive"关键字
	cnContent := section.Content["cn"]
	s.NotEmpty(cnContent, "中文内容不应为空")
}

// TestMemoryRail_Uninit清理工具和节 测试 Uninit 注销 5 个工具并移除 SectionMemory。
// 对齐 Python: MemoryRail.uninit(agent) 中工具移除 + 状态清理 + section 移除
func (s *MemoryRailSuite) TestMemoryRail_Uninit清理工具和节() {
	rail := memory.NewMemoryRail(nil, false)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("Uninit 测试"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Rails:         []agentinterfaces.AgentRail{rail},
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// 执行一次 Invoke
	_, _ = agent.Invoke(s.Ctx, map[string]any{"query": "测试"})

	// Uninit 应成功
	s.NotPanics(func() {
		rail.Uninit(agent)
	}, "Uninit 不应 panic")

	// 验证 5 个工具已从 AM 注销
	am := agent.AbilityManager()
	s.Require().NotNil(am)
	s.Nil(am.Get("memory_search"), "Uninit 后应注销 memory_search")
	s.Nil(am.Get("memory_get"), "Uninit 后应注销 memory_get")
	s.Nil(am.Get("write_memory"), "Uninit 后应注销 write_memory")
	s.Nil(am.Get("edit_memory"), "Uninit 后应注销 edit_memory")
	s.Nil(am.Get("read_memory"), "Uninit 后应注销 read_memory")

	// 验证 SectionMemory 已移除
	spb := agent.SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.False(spb.HasSection(hsections.SectionMemory), "Uninit 后应移除 SectionMemory 节")
}

// TestMemoryRail_运行时RegisterRail后read_memory可真实读取 回归两条注册路径的差异：
// RegisterRail（立即注册）跳过了 pending 循环，因此 DeepAgentRail 的
// workspace / sys_operation 注入必须由它自己完成（Python: register_rail
// deep_agent.py L1191-1193）。缺失时 MemoryRail 的 toolCtx.workspace 为 nil，
// read_memory 恒返回「工作空间未初始化」。
//
// 与同文件其它用例的差异：它们都通过 CreateDeepAgentParams.Rails（pending 路）
// 注册 rail，覆盖不到这条路径 —— 这正是该缺陷长期未被集成测试发现的原因。
func (s *MemoryRailSuite) TestMemoryRail_运行时RegisterRail后read_memory可真实读取() {
	root := s.T().TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	s.Require().NoError(os.MkdirAll(root, 0o755))
	// memory 节点目录：用例先落盘记忆文件，manager 首次检索时全量索引
	if memDir := ws.GetNodePath("memory"); memDir != nil {
		s.Require().NoError(os.MkdirAll(*memDir, 0o755))
	}

	sysOp, err := sys_operation.NewSysOperation(sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
		sys_operation.WithSysOpWorkConfig(&sys_operation.LocalWorkConfig{RestrictToSandbox: true}),
	))
	s.Require().NoError(err)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("初始化完成"))
	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		Workspace:     ws,
		SysOperation:  sysOp,
		MaxIterations: 3,
	})
	s.Require().NoError(err)

	// ensureInitialized 返回的 ctx 携带 CwdState(workspace)，即 fs 沙箱根；
	// 记忆工具读文件依赖它，直接用 s.Ctx 会被沙箱拒绝。
	initCtx, err := agent.EnsureInitialized(s.Ctx)
	s.Require().NoError(err)

	// 关键：运行时注册（RegisterRail），而不是走 CreateDeepAgentParams.Rails
	rail := memory.NewMemoryRail(nil, false)
	s.Require().NoError(agent.RegisterRail(initCtx, rail))

	writeTool := s.toolByName(agent, "write_memory")
	readTool := s.toolByName(agent, "read_memory")

	wres, err := writeTool.Invoke(initCtx, map[string]any{
		"path":    "MEMORY.md",
		"content": "- 集成标记: memory-rail-register-rail\n",
	})
	s.Require().NoError(err)
	s.Equal(true, wres["success"], "write_memory 应成功，实际: %+v", wres)

	rres, err := readTool.Invoke(initCtx, map[string]any{"path": "MEMORY.md"})
	s.Require().NoError(err)
	s.Equal(true, rres["success"],
		"read_memory 应成功（注入缺失时为「工作空间未初始化」），实际: %+v", rres)
	content, _ := rres["content"].(string)
	s.Contains(content, "memory-rail-register-rail", "应读到写入的内容")

	// 工具结果的展示文本是紧凑 JSON（前端可直接解析），不能是 Go 的 map[...]
	rendered := utils.PyText(rres)
	s.NotContains(rendered, "map[", "工具结果渲染不应出现 Go 的 map[...] 记法")
	s.Contains(rendered, `"content":`, "应渲染为 JSON 形式")

	// memory_search：检索不可用/无命中时 results 也必须是数组（Python 恒为 list）
	searchTool := s.toolByName(agent, "memory_search")
	sres, err := searchTool.Invoke(initCtx, map[string]any{"query": "集成标记"})
	s.Require().NoError(err)
	rawResults, ok := sres["results"]
	s.Require().True(ok, "memory_search 结果必须含 results 键，实际: %+v", sres)
	_, isSlice := rawResults.([]any)
	s.True(isSlice, "results 必须是数组而非 null，实际 %T", rawResults)
	s.NotContains(utils.PyText(sres), "map[", "检索结果渲染同样不得出现 map[...]")

	// 嵌套结构：工具结果里可能出现结构体，渲染不得 panic、也不得退回 map[...]
	type renderProbe struct {
		Name string `json:"name"`
	}
	probeRendered := utils.PyText(map[string]any{"probe": renderProbe{Name: "x"}})
	s.Contains(probeRendered, `"name":"x"`, "结构体应序列化为 JSON")
	s.NotContains(probeRendered, "map[", "结构体渲染同样不得出现 map[...]")

	// JSON 数字字面量：json.Number 原样输出（超 int64 的超界整数不得被 Float64 化）
	numRendered := utils.PyText(map[string]any{
		"i":   json.Number("42"),
		"big": json.Number("12345678901234567890"),
		"f":   json.Number("1.0"),
	})
	s.Equal(`{"big":12345678901234567890,"f":1.0,"i":42}`, numRendered)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolByName 从 AbilityManager 取卡片、再从 ResourceMgr 取工具实例。
// 记忆工具由 Rail 注册在全局 ResourceMgr 上，与 AbilityManager 的卡片同 ID。
func (s *MemoryRailSuite) toolByName(agent *harness.DeepAgent, name string) tool.Tool {
	s.T().Helper()
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")

	raw := am.Get(name)
	card, ok := raw.(*tool.ToolCard)
	s.Require().True(ok, "工具 %s 应已注册到 AbilityManager，实际 %T", name, raw)

	tools, err := runner.GetResourceMgr().GetTool([]string{card.ID})
	s.Require().NoError(err)
	s.Require().NotEmpty(tools, "ResourceMgr 应能取到工具实例: %s", card.ID)
	return tools[0]
}

// TestMemory_中文检索命中_真实索引 端到端：真实 Workspace + LocalSysOperation（沙箱）
// + 真实 sqlite/FTS + httptest 词袋 embedding。
//
// 这条用例钉的是「进程重启后已有记忆文件」这条生产路径：manager 初始化即全量索引，
// 文件读取走 sys_operation 的沙箱校验（沙箱根来自 ctx 的 CwdState —— 历史 P0：
// manager 用 context.Background() 初始化时，这里会被判「在沙箱根目录之外」，索引永远为空）。
func (s *MemoryRailSuite) TestMemory_中文检索命中_真实索引() {
	mgr, runCtx := s.newSandboxedManager(map[string]string{
		"MEMORY.md": "- VDS15800 支持手动组网模式，默认关闭\n",
	})
	defer lite.ClearMemoryManagerCache()

	// 纯中文查询：未分词时 FTS5 的 unicode61 把「支持手动组网模式」当一个 token，
	// 「组网」匹配不到；分词后 token 对齐才命中。
	results, err := mgr.Search(runCtx, "组网", lite.SearchOpts{MinScore: 0.01})
	s.Require().NoError(err)
	s.Require().NotEmpty(results, "中文查询应命中（分词/索引/token 对齐任一环节坏了都会空）")
	s.Contains(results[0].Path, "MEMORY.md")
}

// TestMemory_分词降级仍可英文检索 分词器不可用时退回「正则切分 + OR」，
// 记忆检索整体仍可用（中文退化为仅靠向量召回）。
func (s *MemoryRailSuite) TestMemory_分词降级仍可英文检索() {
	s.T().Setenv("UAPCLAW_DISABLE_SEGMENTER", "1")

	mgr, runCtx := s.newSandboxedManager(map[string]string{
		"MEMORY.md": "- VDS15800 支持手动组网模式\n",
	})
	defer lite.ClearMemoryManagerCache()

	results, err := mgr.Search(runCtx, "VDS15800", lite.SearchOpts{MinScore: 0.01})
	s.Require().NoError(err)
	s.NotEmpty(results, "降级后英文/数字 token 仍应命中（正则切分 + OR）")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newBagEmbeddingServer 启动 OpenAI 兼容的词袋 embedding 端点。
//
// 复用 recall 用例的确定性词袋向量（同一 token 落同一维），不依赖真实 API。
func newBagEmbeddingServer(t *testing.T) string {
	t.Helper()
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
	t.Cleanup(srv.Close)
	return srv.URL
}

// newSandboxedManager 构造「真实 sqlite + 沙箱 sys_operation + httptest embedding」的
// 记忆管理器，并把记忆文件先落盘（模拟进程重启后已有记忆）。
func (s *MemoryRailSuite) newSandboxedManager(files map[string]string) (lite.MemoryIndexManager, context.Context) {
	s.T().Helper()

	root := s.T().TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	nodePath := ws.GetNodePath("memory")
	s.Require().NotNil(nodePath, "Workspace 应包含 memory 节点")
	memDir := *nodePath
	s.Require().NoError(os.MkdirAll(memDir, 0o755))
	for name, body := range files {
		s.Require().NoError(os.WriteFile(filepath.Join(memDir, name), []byte(body), 0o644))
	}

	sysOp, err := sys_operation.NewSysOperation(sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
		sys_operation.WithSysOpWorkConfig(&sys_operation.LocalWorkConfig{RestrictToSandbox: true}),
	))
	s.Require().NoError(err)

	settings := lite.CreateMemorySettings(memDir, nil)
	settings.Sync.Watch = false
	settings.Sync.IntervalMinutes = 0

	// 沙箱根来自 ctx 的 CwdState：不传就等于「路径在沙箱根目录之外」
	runCtx := cwd.WithCwdState(s.Ctx, cwd.InitCwd(root, cwd.WithWorkspace(root)))

	mgr, err := lite.GetMemoryIndexManager(lite.MemoryManagerParams{
		AgentID:   "itest_memory_zh",
		Workspace: ws,
		Settings:  settings,
		EmbeddingConfig: &apiEmbedding.EmbeddingConfig{
			ModelName: "bag-of-words",
			BaseURL:   newBagEmbeddingServer(s.T()),
			APIKey:    "test-key",
		},
		SysOperation: sysOp,
		NodeName:     "memory",
		Ctx:          runCtx,
	})
	s.Require().NoError(err)
	return mgr, runCtx
}
