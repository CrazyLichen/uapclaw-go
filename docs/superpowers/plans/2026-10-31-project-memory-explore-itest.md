# Project Memory 装配 + explore 开关 集成测试迁移 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `jiuwenswarm-develop/tests/system_tests/test_project_memory.py` 按「分层全迁」拆到 Go 的 internal 单测与 adapter 装配层集成测试，并修复迁移过程中暴露的 3 处行为偏差。

**Architecture:** 纯函数行为（文件发现 / 内容合并）补 `internal/.../project_memory/files_test.go`；装配 / 开关 / 传播行为新增 `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`，复用该目录已有的 `CreateInstance` + MockLLM + 真落盘范式。三处偏差（子代理迭代上限两段回退失效、plan 默认值偏离、附加目录三级回退缺失）在 `internal/swarm/server/adapter/code_adapter.go` 内就地修复，集成断言作为回归锚点。

**Tech Stack:** Go 1.26 / testify suite / build tag `integration` / `tests/integration/mockllm` / `callback.GetCallbackFramework()`

**Spec:** `docs/superpowers/specs/2026-10-31-project-memory-explore-itest-design.md`

**通用约定：**
- 所有注释用中文（项目规范 1）
- 编译/测试前先 `pgrep -f 'go (build|test)'`，有残留则 kill（项目规范 5 单进程互斥）
- 集成测试命令统一带 `CGO_ENABLED=1` 与 `-tags "sqlite_fts5 test integration"`
- 每个 Task 结束都提交，提交信息用 `test(...)` / `fix(...)` / `docs(...)` 前缀

---

### Task 0: 基线确认

**Files:** 无改动

- [ ] **Step 1: 确认工作区干净（除他人在途文件）**

Run: `cd /home/opensource/uap-claw-go && git status --short && git log --oneline -3`

Expected: 仅可能出现 `?? tests/integration/agentcore/harness/taskloop_stream_diag_test.go`（他人文件，不要动、不要提交）

- [ ] **Step 2: 记录单测基线绿**

Run: `CGO_ENABLED=1 go test ./internal/swarm/agents/harness/common/rails/... ./internal/swarm/server/adapter/...`

Expected: `ok` 若干包，`FAIL` 0

- [ ] **Step 3: 记录集成基线绿（仅目标目录）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run TestCodeAdapterCodingMemorySuite -count=1`

Expected: `ok  	github.com/uapclaw/uapclaw-go/tests/integration/swarm/server/adapter`

---

### Task 1: 单测补口 C2–C4（纯测试）

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/project_memory/files_test.go`

> **计划订正（执行期发现）**：原 C1「修 `TestDiscoverAndLoadMemoryFiles_Local优先级最高` 假断言」的前提错误——
> 该用例（files_test.go:114-138）本就断言了 local 索引在 project 之后，是有效的。
> 早先判断源于 `sed -n '60,120p;205,260p'` 多区间输出拼接，把 `FrontmatterPaths作用域` 的
> `scoped.md` 断言误读成它的函数体。C1 取消；Python 原用例断言的「merge 后 project 在 local 之前」
> 由 C3（四类优先级排序）覆盖，语义无丢失。

- [ ] **Step 1: 追加 C2 子目录向上走查**

在文件末尾追加：

```go
// TestDiscoverAndLoadMemoryFiles_子目录向上走查 验证 workspace 指向子目录时仍能加载项目根的记忆文件。
// 对齐 Python: test_discovery_walks_up_from_subdir
// （files.py walk_dirs 从 cwd 逐级上溯到 project_root，reverse 后 root 优先加载）
func TestDiscoverAndLoadMemoryFiles_子目录向上走查(t *testing.T) {
	tmpDir := t.TempDir()
	os.Mkdir(filepath.Join(tmpDir, ".git"), 0o755)
	os.WriteFile(filepath.Join(tmpDir, "UAPCLAWSWARM.md"), []byte("ROOT-RULE"), 0o644)

	sub := filepath.Join(tmpDir, "src", "feature")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	ClearProjectMemoryCache()

	files, _ := DiscoverAndLoadMemoryFiles(context.Background(), sub, sub, nil)
	found := false
	for _, f := range files {
		if strings.Contains(f.Content, "ROOT-RULE") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected root UAPCLAWSWARM.md to be loaded from subdir workspace, got %+v", files)
	}
}
```

- [ ] **Step 2: 追加 C3 四类优先级排序**

```go
// TestMergeMemoryContent_四类优先级排序 验证 merge 输出顺序为 managed < user < project < local。
// 对齐 Python: test_merge_orders_by_priority
func TestMergeMemoryContent_四类优先级排序(t *testing.T) {
	files := []LoadedMemoryFile{
		{Path: "/local.md", Kind: "local", Content: "LOCAL", Priority: 40},
		{Path: "/project.md", Kind: "project", Content: "PROJECT", Priority: 30},
		{Path: "/user.md", Kind: "user", Content: "USER", Priority: 20},
		{Path: "/managed.md", Kind: "managed", Content: "MANAGED", Priority: 10},
	}
	merged := MergeMemoryContent(files, DefaultMaxChars)

	idx := func(s string) int { return strings.Index(merged, s) }
	if idx("MANAGED") < 0 || idx("USER") < 0 || idx("PROJECT") < 0 || idx("LOCAL") < 0 {
		t.Fatalf("merged should contain all four kinds, got: %s", merged)
	}
	if !(idx("MANAGED") < idx("USER") && idx("USER") < idx("PROJECT") && idx("PROJECT") < idx("LOCAL")) {
		t.Fatalf("merged order should be managed<user<project<local, got: %s", merged)
	}
}
```

- [ ] **Step 3: 追加 C4 不同路径不合并**

```go
// TestMergeMemoryContent_不同路径同内容不合并 验证 merge 按文件逐个追加、不按内容去重。
// 对齐 Python: test_merge_keeps_different_paths_separate
func TestMergeMemoryContent_不同路径同内容不合并(t *testing.T) {
	files := []LoadedMemoryFile{
		{Path: "/a/UAPCLAWSWARM.md", Kind: "project", Content: "SAME-CONTENT", Priority: 30},
		{Path: "/b/UAPCLAWSWARM.md", Kind: "project", Content: "SAME-CONTENT", Priority: 30},
	}
	merged := MergeMemoryContent(files, DefaultMaxChars)
	if !strings.Contains(merged, "/a/UAPCLAWSWARM.md") || !strings.Contains(merged, "/b/UAPCLAWSWARM.md") {
		t.Fatalf("both file headers should appear, got: %s", merged)
	}
}
```

- [ ] **Step 4: 运行 3 个用例**

Run: `CGO_ENABLED=1 go test ./internal/swarm/agents/harness/common/rails/project_memory/ -run 'TestDiscoverAndLoadMemoryFiles_(Local优先级最高|子目录向上走查)|TestMergeMemoryContent_(四类优先级排序|不同路径同内容不合并)' -v -count=1`

Expected: 3 个 `--- PASS`。若 C2 失败，说明 `FindProjectRoot` 的向上查找未生效——此时**不要**放宽断言，改为在 `files.go:FindProjectRoot` 定位并修复后重跑。

- [ ] **Step 5: 全包回归**

Run: `CGO_ENABLED=1 go test ./internal/swarm/agents/harness/common/rails/... -count=1`

Expected: 全 `ok`

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/project_memory/files_test.go
git commit -m "test(project_memory): 补走查/merge 优先级/不同路径三条语义"
```

---

### Task 2: 抽取集成测试 env 基建

**Files:**
- Create: `tests/integration/swarm/server/adapter/itest_env_helpers_test.go`
- Modify: `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go`（SetupTest / TearDownTest / 删除 saveAndSetenv / restoreEnv / 字段改为 env）

- [ ] **Step 1: 新建共享 env 助手**

创建 `tests/integration/swarm/server/adapter/itest_env_helpers_test.go`：

```go
//go:build integration

package adapter_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// adapterTestEnv CodeAdapter 集成测试的隔离环境。
//
// 隔离三件套（先例 code_adapter_coding_memory_test.go）：
//  1. UAPCLAW_DATA_DIR → t.TempDir()，先 pathutil.ResetCache() 再做任何路径调用
//  2. 创建 config/ 目录（模拟已初始化，防 getResolvedPaths 回退 ResourcesDir）
//  3. 创建 agent/workspace/（os.WriteFile 不建父目录）
//
// MockLLM 注册必须先于写 config.yaml（YAML 中 client_provider 引用此注册名）。
type adapterTestEnv struct {
	// DataDir 每测试隔离的 UAPCLAW_DATA_DIR
	DataDir string
	// Provider MockLLM 在 ClientRegistry 的注册名（config.yaml 引用）
	Provider string
	// envBackup 环境变量备份（Cleanup 恢复）
	envBackup [][3]string
}

// ──────────────────────────── 导出函数（测试包内使用） ────────────────────────────

// newAdapterTestEnv 构建隔离环境并从 t.Cleanup 注册收尾。
//
// prefix 用于生成唯一 provider 名，避免同包多个套件并发/顺序执行时相互覆盖。
func newAdapterTestEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *adapterTestEnv {
	t.Helper()

	env := &adapterTestEnv{
		Provider: fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()),
	}
	model_clients.GetClientRegistry().Register(env.Provider, "llm", mock.Factory())

	env.DataDir = t.TempDir()
	env.Setenv(t, "UAPCLAW_DATA_DIR", env.DataDir)
	pathutil.ResetCache()
	// 持久化检查点器单例重置：同进程内连续 CreateInstance 且各用 TempDir 隔离，
	// 首次绑定的 SQLite 路径随目录清理失效。
	adapter.ResetPersistentCheckpointer()

	if err := os.MkdirAll(filepath.Join(env.DataDir, "config"), 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(env.DataDir, "agent", "workspace"), 0o755); err != nil {
		t.Fatalf("创建 agent/workspace 目录失败: %v", err)
	}

	t.Cleanup(func() {
		_ = model_clients.GetClientRegistry().Unregister(env.Provider, "llm")
		// ResourceMgr 释放（对齐 Python Runner.stop() 语义）：
		// rail 注册逻辑含 get_tool 查重，前一个测试的工具实例会绑已删除的 TempDir。
		if rm := runner.GetResourceMgr(); rm != nil {
			_ = rm.Release(context.Background())
		}
		env.restoreEnv()
		pathutil.ResetCache()
	})
	return env
}

// Setenv 记录原值并设置环境变量。
func (e *adapterTestEnv) Setenv(t *testing.T, key, val string) {
	t.Helper()
	orig, had := os.LookupEnv(key)
	e.envBackup = append(e.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	if err := os.Setenv(key, val); err != nil {
		t.Fatalf("设置环境变量 %s 失败: %v", key, err)
	}
}

// WriteConfigYAML 写入标准 config/config.yaml。
//
// reactExtra 为追加到 react: 段末尾的原始 YAML 片段（调用方负责 2 空格缩进与换行），
// 例如 "  max_iterations: 8\n" 或
// "  subagents:\n    explore_agent:\n      max_iterations: 5\n"。
// modes.code.tools/rails 置空，避免拉起无关工具与护栏。
func (e *adapterTestEnv) WriteConfigYAML(t *testing.T, reactExtra string) {
	t.Helper()
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
%s`, e.Provider, e.Provider, reactExtra)

	if err := os.WriteFile(filepath.Join(e.DataDir, "config", "config.yaml"), []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("写 config.yaml 失败: %v", err)
	}
}

// restoreEnv 恢复环境变量。
func (e *adapterTestEnv) restoreEnv() {
	for _, item := range e.envBackup {
		if item[2] == "false" {
			_ = os.Unsetenv(item[0])
			continue
		}
		_ = os.Setenv(item[0], item[1])
	}
	e.envBackup = nil
}
```

注意：`context.Background()` 用于 ResourceMgr 释放（收尾路径不依赖测试 ctx）。

- [ ] **Step 2: 让 coding_memory 套件改用共享 env**

`code_adapter_coding_memory_test.go` 中：

1. 结构体字段 `providerName string` / `envBackup [][3]string` / `dataDir string` 替换为 `env *adapterTestEnv`
2. `SetupTest` 改为：

```go
func (s *CodeAdapterCodingMemorySuite) SetupTest() {
	s.env = newAdapterTestEnv(s.T(), s.MockLLM, "mock_coding_mem_itest")
}
```

3. `TearDownTest` 删除（收尾已由 `t.Cleanup` 覆盖）
4. 删除 `saveAndSetenv` / `restoreEnv` 两个方法
5. 其余方法内 `s.providerName` → `s.env.Provider`，`s.dataDir` → `s.env.DataDir`

- [ ] **Step 3: 编译并跑该套件**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run TestCodeAdapterCodingMemorySuite -count=1 -v`

Expected: 6 个用例全 `PASS`。任何失败都说明抽取改变了行为，必须修到全绿再继续（`pathutil.ResetCache` 顺序、`ResetPersistentCheckpointer` 调用点是最常见偏差源）。

- [ ] **Step 4: gofmt**

Run: `gofmt -l tests/integration/swarm/server/adapter/`

Expected: 无输出

- [ ] **Step 5: Commit**

```bash
git add tests/integration/swarm/server/adapter/itest_env_helpers_test.go tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go
git commit -m "test(adapter_itest): 抽取集成测试 env 隔离基建为共享助手（coding_memory 套件改用）"
```

---

### Task 3: D1 各 code 子模式均挂载 + D3 默认迭代上限（先红后绿）

**Files:**
- Create: `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`（`buildConfiguredSubagents` 内 explore/plan 的 `MaxIterations`）

- [ ] **Step 1: 新建套件骨架 + D1**

创建 `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`：

```go
//go:build integration

package adapter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	commonrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapterProjectMemorySuite 测试 CodeAdapter 装配链中的 ProjectMemoryRail 与 explore/plan 子代理装配。
//
// 对齐 Python: jiuwenswarm tsts/system_tests/test_project_memory.py
// （类 3 装配、类 4 explore 开关、类 6 模式切换真实化）
type CodeAdapterProjectMemorySuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodeAdapterProjectMemorySuite(t *testing.T) {
	suite.Run(t, new(CodeAdapterProjectMemorySuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newCodeAdapterWithProject 在指定 env 下创建 CodeAdapter（project_dir 指向真实目录）。
func (s *CodeAdapterProjectMemorySuite) newCodeAdapterWithProject(env *adapterTestEnv, subMode string) (*adapter.CodeAdapter, string) {
	projDir := filepath.Join(env.DataDir, "proj")
	s.Require().NoError(os.MkdirAll(projDir, 0o755))

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{"project_dir": projDir}, "code", subMode))
	s.T().Cleanup(func() { _ = c.Cleanup() })
	return c, projDir
}

// subagentByName 从 DeepConfig().Subagents 中按 AgentCard.Name 取子代理配置。
func (s *CodeAdapterProjectMemorySuite) subagentByName(inst *harness.DeepAgent, name string) *hschema.SubAgentConfig {
	s.Require().NotNil(inst.DeepConfig())
	for _, spec := range inst.DeepConfig().Subagents {
		cfg, ok := spec.(*hschema.SubAgentConfig)
		if !ok || cfg == nil || cfg.AgentCard == nil {
			continue
		}
		if cfg.AgentCard.Name == name {
			return cfg
		}
	}
	return nil
}

// TestCreateInstance_各code子模式均挂载 验证 code.normal / code.plan 均装配 ProjectMemoryRail，
// 且 workspace 取 project_dir、语言固定 en。
// 对齐 Python: TestProjectMemoryRailModeSwitching（Python 原用例为 pass/assert True 空壳，此处真实化）
func (s *CodeAdapterProjectMemorySuite) TestCreateInstance_各code子模式均挂载() {
	for _, subMode := range []string{"normal", "plan"} {
		subMode := subMode
		s.Run("子模式_"+subMode, func() {
			env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_mode_"+subMode)
			env.WriteConfigYAML(s.T(), "")

			c, projDir := s.newCodeAdapterWithProject(env, subMode)
			inst := c.Instance()
			s.Require().NotNil(inst)

			found := inst.FindRailsByType(reflect.TypeOf(&commonrails.ProjectMemoryRail{}))
			s.Require().Len(found, 1, "ProjectMemoryRail 应恰好装配 1 个")

			rail, ok := found[0].(*commonrails.ProjectMemoryRail)
			s.Require().True(ok, "rail 具体类型应为 *ProjectMemoryRail")
			s.Equal(projDir, rail.ResolveWorkspacePath(), "workspace 应取 project_dir")
			s.Equal("en", rail.GetLanguage(), "code 族运行时语言固定 en")
		})
	}
}
```

- [ ] **Step 2: 运行 D1，确认绿**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/TestCreateInstance_各code子模式均挂载' -count=1 -v`

Expected: 2 个子用例 `PASS`

- [ ] **Step 3: 追加 D3（默认迭代上限），先确认红**

在同文件追加：

```go
// TestCreateInstance_explore与plan默认迭代上限 验证未配置 max_iterations 时两者取 15。
// 对齐 Python: _build_configured_subagents 中 parse_int(cfg, react_cfg.get("max_iterations", 15))
// 修复前现状：explore 硬编码 15、plan 硬编码 25（plan 断言将失败）
func (s *CodeAdapterProjectMemorySuite) TestCreateInstance_explore与plan默认迭代上限() {
	env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_iters_default")
	env.WriteConfigYAML(s.T(), "")

	c, _ := s.newCodeAdapterWithProject(env, "normal")
	inst := c.Instance()
	s.Require().NotNil(inst)

	explore := s.subagentByName(inst, "explore_agent")
	s.Require().NotNil(explore, "explore_agent 应恒挂载")
	s.Equal(15, explore.MaxIterations, "explore 默认迭代上限应为 15")

	plan := s.subagentByName(inst, "plan_agent")
	s.Require().NotNil(plan, "plan_agent 应恒挂载")
	s.Equal(15, plan.MaxIterations, "plan 默认迭代上限应为 15（Python 恒传 react.max_iterations）")
}
```

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/TestCreateInstance_explore与plan默认迭代上限' -count=1 -v`

Expected: **FAIL**，报 `plan 默认迭代上限应为 15`，实际 `25`

- [ ] **Step 4: 修 B1/B2**

`internal/swarm/server/adapter/code_adapter.go::buildConfiguredSubagents` 中，在 `subagentsCfg` 解析之后加入两段回退：

```go
	subagentsCfg, _ := config["subagents"].(map[string]any)

	// Python: parse_int(explore_agent_cfg.get("max_iterations"), react_cfg.get("max_iterations", 15))
	// explore/plan 均以 react.max_iterations 为回退基准（plan builder 自身的 25 默认值永不生效）
	defaultMaxIterations := parseIntAny(config["max_iterations"], 15)
	exploreAgentCfg, _ := subagentsCfg["explore_agent"].(map[string]any)
	planAgentCfg, _ := subagentsCfg["plan_agent"].(map[string]any)
	exploreMaxIterations := parseIntAny(exploreAgentCfg["max_iterations"], defaultMaxIterations)
	planMaxIterations := parseIntAny(planAgentCfg["max_iterations"], defaultMaxIterations)
```

然后把 explore 的 `MaxIterations: 15,` 改为 `MaxIterations: exploreMaxIterations,`，plan 的 `MaxIterations: 25,` 改为 `MaxIterations: planMaxIterations,`。

- [ ] **Step 5: 运行 D3，确认转绿**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite' -count=1 -v`

Expected: D1（2 子用例）+ D3 全 `PASS`

- [ ] **Step 6: adapter 包单测回归**

Run: `CGO_ENABLED=1 go test ./internal/swarm/server/adapter/... -count=1`

Expected: 全 `ok`（若有既有单测断言了 plan=25，按 Python 语义同步更新该断言并在提交信息中注明）

- [ ] **Step 7: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go
git commit -m "fix(adapter): explore/plan 迭代上限按 Python parse_int 两段回退（plan 默认 25→15）"
```

---

### Task 4: D4 开关语义 + D5 覆盖与回退

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`

- [ ] **Step 1: 追加 D4**

```go
// TestBuildConfiguredSubagents_explore开关被忽略 验证 explore_agent 恒挂载，
// 而 code_agent 受 enabled 开关控制。
// 对齐 Python: test_explore_agent_always_enabled（"Code 模式核心子代理，始终启用，enabled: False 被忽略"）
func (s *CodeAdapterProjectMemorySuite) TestBuildConfiguredSubagents_explore开关被忽略() {
	env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_switch")
	env.WriteConfigYAML(s.T(), "  subagents:\n    explore_agent:\n      enabled: false\n    code_agent:\n      enabled: false\n")

	c, _ := s.newCodeAdapterWithProject(env, "normal")
	inst := c.Instance()
	s.Require().NotNil(inst)

	s.NotNil(s.subagentByName(inst, "explore_agent"), "explore_agent 恒挂载，enabled:false 被忽略")
	s.NotNil(s.subagentByName(inst, "plan_agent"), "plan_agent 恒挂载")
	s.Nil(s.subagentByName(inst, "code_agent"), "code_agent 受 enabled 开关控制，false 时不挂载")
}
```

- [ ] **Step 2: 追加 D5（两个子场景）**

```go
// TestBuildConfiguredSubagents_迭代上限覆盖与回退 验证 react.max_iterations 回退与子配置覆盖。
// 对齐 Python: test_explore_agent_with_custom_max_iterations + parse_int 两段回退语义
func (s *CodeAdapterProjectMemorySuite) TestBuildConfiguredSubagents_迭代上限覆盖与回退() {
	s.Run("仅react回退_8", func() {
		env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_iters_react")
		env.WriteConfigYAML(s.T(), "  max_iterations: 8\n")

		c, _ := s.newCodeAdapterWithProject(env, "normal")
		inst := c.Instance()
		s.Require().NotNil(inst)

		s.Equal(8, s.subagentByName(inst, "explore_agent").MaxIterations, "explore 应回退到 react.max_iterations")
		s.Equal(8, s.subagentByName(inst, "plan_agent").MaxIterations, "plan 应回退到 react.max_iterations")
	})

	s.Run("子配置覆盖_5", func() {
		env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_iters_override")
		env.WriteConfigYAML(s.T(), "  max_iterations: 8\n  subagents:\n    explore_agent:\n      max_iterations: 5\n")

		c, _ := s.newCodeAdapterWithProject(env, "normal")
		inst := c.Instance()
		s.Require().NotNil(inst)

		s.Equal(5, s.subagentByName(inst, "explore_agent").MaxIterations, "子配置应覆盖 react 值")
		s.Equal(8, s.subagentByName(inst, "plan_agent").MaxIterations, "plan 未配置子项仍取 react 值")
	})
}
```

- [ ] **Step 3: 运行 D4 + D5**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/(TestBuildConfiguredSubagents_explore开关被忽略|TestBuildConfiguredSubagents_迭代上限覆盖与回退)' -count=1 -v`

Expected: 全 `PASS`。若 D4 中 `code_agent` 断言失败（意外挂载），检查 `isSubagentExplicitlyEnabled` 对 `enabled:false` 的处理，不要放宽断言。

- [ ] **Step 4: Commit**

```bash
git add tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go
git commit -m "test(adapter_itest): explore 恒挂载 + code_agent 开关 + 迭代上限覆盖回退"
```

---

### Task 5: D2 附加目录三级回退（先红后绿）

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`（`buildProjectMemoryRail` 的 additionalDirs 解析 + 新增展平助手）

- [ ] **Step 1: 追加辅助方法（发请求）**

```go
// processOnce 发一轮完整请求（ProcessMessageImpl），返回响应。
// 先例：code_adapter_coding_memory_test.go::TestProcessMessage_记忆Section注入
func (s *CodeAdapterProjectMemorySuite) processOnce(c *adapter.CodeAdapter, tag string, params map[string]any) {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("ok"))
	raw, err := json.Marshal(params)
	s.Require().NoError(err)

	sessionID := "itest-pm-" + tag
	req := swarmschema.NewAgentRequest(
		"itest-req-"+tag, "itest-channel", swarmschema.ReqMethodChatSend, raw,
		swarmschema.WithAgentSessionID(sessionID),
	)
	inputs := map[string]any{
		"conversation_id": sessionID,
		"query":           "读取项目记忆",
		"channel":         "itest-channel",
		"language":        "zh",
	}
	resp, err := c.ProcessMessageImpl(s.Ctx, req, inputs)
	s.Require().NoError(err)
	s.Require().NotNil(resp)
	s.True(resp.OK, "响应应成功: %+v", resp)
}
```

- [ ] **Step 2: 追加 D2**

```go
// TestCreateInstance_附加目录三级回退 验证 instance_overrides → configCache → env 的优先级：
// 配置存在时 env 不再参与（Python: if raw_additional_dirs is None: raw = os.getenv(...)）。
func (s *CodeAdapterProjectMemorySuite) TestCreateInstance_附加目录三级回退() {
	env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_adddirs")

	cfgDir := filepath.Join(env.DataDir, "extra_cfg")
	envDir := filepath.Join(env.DataDir, "extra_env")
	s.Require().NoError(os.MkdirAll(cfgDir, 0o755))
	s.Require().NoError(os.MkdirAll(envDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(cfgDir, "UAPCLAWSWARM.md"), []byte("CFG-DIR-RULE"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(envDir, "UAPCLAWSWARM.md"), []byte("ENV-DIR-RULE"), 0o644))

	env.Setenv(s.T(), "UAPCLAWSWARM_ADDITIONAL_DIRECTORIES", envDir)
	env.WriteConfigYAML(s.T(), "  project_memory:\n    additional_directories:\n      - "+cfgDir+"\n")

	c, _ := s.newCodeAdapterWithProject(env, "normal")
	s.processOnce(c, "adddirs", map[string]any{"query": "读取项目记忆", "mode": "code.plan", "files": map[string]any{}})

	spb := c.Instance().SystemPromptBuilder()
	s.Require().NotNil(spb)
	section := spb.GetSection(project_memory.SectionName)
	s.Require().NotNil(section, "应注入 project_memory section")
	s.Contains(section.Content["en"], "CFG-DIR-RULE", "配置目录内容应被加载")
	s.NotContains(section.Content["en"], "ENV-DIR-RULE", "配置存在时 env 不参与（Python 语义）")
}
```

- [ ] **Step 3: 运行 D2，确认红**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/TestCreateInstance_附加目录三级回退' -count=1 -v`

Expected: **FAIL**（两种可能：`CFG-DIR-RULE` 缺失 = 未读 configCache；`ENV-DIR-RULE` 出现 = env 无条件追加）

- [ ] **Step 4: 修 B3**

在 `code_adapter.go` 新增展平助手（放在文件「非导出函数」区块靠下位置）：

```go
// flattenStringDirs 展平额外目录配置值（对齐 Python _build_project_memory_rail 的解析分支）。
//
//   - string：按 os.PathListSeparator 切分后 trim
//   - []any / []string：逐项转字符串后 trim
//   - nil 与其他类型：返回空切片（非 nil，确保「已配置但为空」与「未配置」可区分）
func flattenStringDirs(value any) []string {
	result := make([]string, 0)
	appendTrimmed := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			result = append(result, s)
		}
	}
	switch v := value.(type) {
	case string:
		for _, item := range filepath.SplitList(v) {
			appendTrimmed(item)
		}
	case []string:
		for _, item := range v {
			appendTrimmed(item)
		}
	case []any:
		for _, item := range v {
			appendTrimmed(fmt.Sprintf("%v", item))
		}
	}
	return result
}

// projectMemoryAdditionalDirectories 解析项目记忆附加目录（Python 三级回退）。
//
// Python: raw = self._instance_overrides.get(
//     "project_memory_additional_directories",
//     self._config_cache.get("project_memory", {}).get("additional_directories"))
//     if raw is None: raw = os.getenv("JIUWENSWARM_ADDITIONAL_DIRECTORIES", "")
func (c *CodeAdapter) projectMemoryAdditionalDirectories() []string {
	if raw, ok := c.deep.instanceOverrides["project_memory_additional_directories"]; ok {
		return flattenStringDirs(raw)
	}
	if pmCfg, ok := c.deep.configCache["project_memory"].(map[string]any); ok {
		if raw, ok := pmCfg["additional_directories"]; ok {
			return flattenStringDirs(raw)
		}
	}
	return flattenStringDirs(os.Getenv("UAPCLAWSWARM_ADDITIONAL_DIRECTORIES"))
}
```

然后把 `buildProjectMemoryRail` 中原有的两段解析（`instanceOverrides` 的 `[]any` 分支 + 无条件追加 env 的分支）整段替换为：

```go
	// Python: raw_additional_dirs = self._instance_overrides.get(...) → config_cache → env
	additionalDirs := c.projectMemoryAdditionalDirectories()
```

并确认 `strings` / `fmt` / `filepath` / `os` 已在 import 中（该文件已有前三个，`os` 也已用于 `os.Getenv`）。

- [ ] **Step 5: 运行 D2，确认转绿**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/TestCreateInstance_附加目录三级回退' -count=1 -v`

Expected: `PASS`

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go
git commit -m "fix(adapter): project memory 附加目录三级回退对齐 Python（env 仅在两级皆缺时兜底）"
```

---

### Task 6: D6 模型入参端到端（双层断言）

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`

- [ ] **Step 1: 追加捕获器**

```go
// ──────────────────────────── 捕获器 ────────────────────────────

// llmPromptCapture 通过进程级回调框架捕获模型入参（对齐先例
// real_llm/system_prompt_capture_test.go::registerPromptCapture）。
//
// 注意：callback.GetCallbackFramework() 是进程级单例，注册后必须反注册。
type llmPromptCapture struct {
	// mu 保护 events 并发写
	mu sync.Mutex
	// systemMessages 每次调用中 system 角色消息的拼接结果
	systemMessages []string
}

// registerLLMPromptCapture 注册捕获器并在测试结束反注册。
func registerLLMPromptCapture(t *testing.T) *llmPromptCapture {
	t.Helper()
	c := &llmPromptCapture{}
	fw := callback.GetCallbackFramework()
	onEvent := func(_ context.Context, data *callback.LLMCallEventData) any {
		c.mu.Lock()
		c.systemMessages = append(c.systemMessages, extractSystemMessages(data.Messages)...)
		c.mu.Unlock()
		return nil
	}
	fw.OnLLM(callback.LLMInvokeInput, onEvent)
	fw.OnLLM(callback.LLMStreamInput, onEvent)
	t.Cleanup(func() {
		fw.OffLLM(callback.LLMInvokeInput, onEvent)
		fw.OffLLM(callback.LLMStreamInput, onEvent)
	})
	return c
}

// firstSystemMessage 返回首次捕获到的第一条 system 消息。
func (c *llmPromptCapture) firstSystemMessage() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.systemMessages) == 0 {
		return ""
	}
	return c.systemMessages[0]
}

// extractSystemMessages 从事件 Messages 字段提取 system 角色消息。
// Messages 运行时为 model_clients.MessagesParam，含 text/messages/dicts 三形态。
func extractSystemMessages(messages any) []string {
	mp, ok := messages.(model_clients.MessagesParam)
	if !ok {
		return nil
	}
	var results []string
	if mp.IsMessages() {
		for _, m := range mp.Messages() {
			if m.GetRole() == llmschema.RoleTypeSystem {
				results = append(results, m.GetContent().String())
			}
		}
		return results
	}
	for _, item := range mp.Dicts() {
		role, _ := item["role"].(string)
		if role != "system" {
			continue
		}
		if content, ok := item["content"].(string); ok {
			results = append(results, content)
		}
	}
	return results
}
```

需额外 import：`"context"`、`"sync"`、`model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"`、`llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"`。

`MessagesParam` 已提供 `IsMessages()` / `Messages()` / `Dicts()`（`model_clients/messages_param.go:84-107`），上述分支可直接编译。

- [ ] **Step 2: 追加 D6**

```go
// TestProcessMessage_ProjectMemory写入模型入参 双层断言：
// ① SystemPromptBuilder 中存在 project_memory section；② 模型入参的 system 消息含该内容。
// 对齐 Python: TestProjectMemoryRailEndToEnd（Python 仅断言到 MagicMock builder，此处打穿到模型入参）
func (s *CodeAdapterProjectMemorySuite) TestProcessMessage_ProjectMemory写入模型入参() {
	env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_e2e")
	env.WriteConfigYAML(s.T(), "")

	c, projDir := s.newCodeAdapterWithProject(env, "normal")
	s.Require().NoError(os.Mkdir(filepath.Join(projDir, ".git"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(projDir, "UAPCLAWSWARM.md"),
		[]byte("# Test\nPROJECT-MEMORY-MARKER\n"), 0o644))

	capture := registerLLMPromptCapture(s.T())
	s.processOnce(c, "e2e", map[string]any{"query": "读取项目记忆", "mode": "code.plan", "files": map[string]any{}})

	inst := c.Instance()
	spb := inst.SystemPromptBuilder()
	s.Require().NotNil(spb)

	// 断言 ①：builder 层
	section := spb.GetSection(project_memory.SectionName)
	s.Require().NotNil(section, "builder 应注入 project_memory section")
	s.Contains(section.Content["en"], "PROJECT-MEMORY-MARKER")

	// 断言 ②：模型入参层
	prompt := capture.firstSystemMessage()
	s.Require().NotEmpty(prompt, "应捕获到 system 消息")
	s.Contains(prompt, "## Project Memory", "system 消息应含项目记忆抬头")
	s.Contains(prompt, "PROJECT-MEMORY-MARKER", "system 消息应含记忆正文")

	// 断言 ③：装配未被请求往返拆掉
	found := inst.FindRailsByType(reflect.TypeOf(&commonrails.ProjectMemoryRail{}))
	s.Require().Len(found, 1, "请求往返后 rail 应仍在")
	rail := found[0].(*commonrails.ProjectMemoryRail)
	s.Equal("en", rail.GetLanguage())
}
```

- [ ] **Step 3: 运行 D6**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/TestProcessMessage_ProjectMemory写入模型入参' -count=1 -v`

Expected: `PASS`。若断言 ② 为空：确认 `callback.LLMInvokeInput` 与 `LLMStreamInput` 都已注册（Runner 主路径可能走 Stream），必要时只保留能触发的那个并注释原因。

- [ ] **Step 4: 全目录回归（防回调单例污染）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -count=1`

Expected: 全 `PASS`（coding_memory + project_memory 两套件）

- [ ] **Step 5: Commit**

```bash
git add tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go
git commit -m "test(adapter_itest): project memory 双层断言（section + 模型入参 LLM_INPUT 捕获）"
```

---

### Task 7: D7 trusted_dirs 注入

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`

- [ ] **Step 1: 追加 D7**

```go
// TestProcessMessage_trustedDirs注入附加目录 验证请求 trusted_dirs 经
// updateRuntimeConfig → ProjectMemoryRail.SetAdditionalDirectories 生效。
// 对齐 Python: _update_runtime_config 中
//   if runtime_config.trusted_dirs: self._project_memory_rail.set_additional_directories(...)
func (s *CodeAdapterProjectMemorySuite) TestProcessMessage_trustedDirs注入附加目录() {
	env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_trusted")
	env.WriteConfigYAML(s.T(), "")

	c, projDir := s.newCodeAdapterWithProject(env, "normal")
	// project_dir 自身无记忆文件，内容只来自 trusted_dirs
	s.Require().NoError(os.Mkdir(filepath.Join(projDir, ".git"), 0o755))

	extraDir := filepath.Join(env.DataDir, "trusted_extra")
	s.Require().NoError(os.MkdirAll(extraDir, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(extraDir, "UAPCLAWSWARM.md"), []byte("TRUSTED-DIR-RULE"), 0o644))

	s.processOnce(c, "trusted", map[string]any{
		"query":        "读取项目记忆",
		"mode":         "code.plan",
		"files":        map[string]any{},
		"trusted_dirs": []any{extraDir},
	})

	section := c.Instance().SystemPromptBuilder().GetSection(project_memory.SectionName)
	s.Require().NotNil(section, "trusted_dirs 注入后应出现 project_memory section")
	s.Contains(section.Content["en"], "TRUSTED-DIR-RULE")
}
```

- [ ] **Step 2: 运行 D7**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/TestProcessMessage_trustedDirs注入附加目录' -count=1 -v`

Expected: `PASS`

- [ ] **Step 3: Commit**

```bash
git add tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go
git commit -m "test(adapter_itest): 请求 trusted_dirs 注入项目记忆附加目录"
```

---

### Task 8: D8 SwitchMode 往返

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go`

- [ ] **Step 1: 追加 D8**

```go
// TestSwitchMode_ProjectMemoryRail不被卸载 验证 code 族切模式不触发 deep 的
// updateAgentModeRails（会卸 SubagentRail 并挂 ContextAssembleRail）。
// 对齐 Python: JiuwenClawCodeAdapter._update_rails_for_mode（code 族仅卸
// TaskPlanning/SkillEvolution，保留 Subagent/ProjectMemory/CodingMemory）
func (s *CodeAdapterProjectMemorySuite) TestSwitchMode_ProjectMemoryRail不被卸载() {
	env := newAdapterTestEnv(s.T(), s.MockLLM, "mock_pm_switchmode")
	env.WriteConfigYAML(s.T(), "")

	c, projDir := s.newCodeAdapterWithProject(env, "normal")
	s.Require().NoError(os.Mkdir(filepath.Join(projDir, ".git"), 0o755))
	s.Require().NoError(os.WriteFile(
		filepath.Join(projDir, "UAPCLAWSWARM.md"),
		[]byte("SWITCH-MODE-MARKER\n"), 0o644))

	inst := c.Instance()
	s.Require().NotNil(inst)
	s.Require().NoError(c.SwitchMode(s.Ctx, "itest-pm-switchmode-session", "plan"))

	// ① ProjectMemoryRail 仍在
	s.Require().Len(inst.FindRailsByType(reflect.TypeOf(&commonrails.ProjectMemoryRail{})), 1,
		"切模式后 ProjectMemoryRail 应仍在")
	// ② SubagentRail 仍在
	s.NotEmpty(inst.FindRailsByType(reflect.TypeOf(&subagent.SubagentRail{})),
		"切模式后 SubagentRail 应仍在")
	// ③ 未误挂 ContextAssembleRail（deep 的 agent.fast 分支产物）
	s.Empty(inst.FindRailsByType(reflect.TypeOf(&cerails.ContextAssembleRail{})),
		"code 族不应挂 ContextAssembleRail")

	// ④ 切模式后请求仍注入项目记忆
	s.processOnce(c, "switchmode", map[string]any{"query": "读取项目记忆", "mode": "code.plan", "files": map[string]any{}})
	section := inst.SystemPromptBuilder().GetSection(project_memory.SectionName)
	s.Require().NotNil(section, "切模式后仍应注入 project_memory section")
	s.Contains(section.Content["en"], "SWITCH-MODE-MARKER")
}
```

需额外 import：`subagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"`、`cerails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"`。

- [ ] **Step 2: 运行 D8**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -run 'TestCodeAdapterProjectMemorySuite/TestSwitchMode_ProjectMemoryRail不被卸载' -count=1 -v`

Expected: `PASS`。若 ② 失败（SubagentRail 不存在），说明 `CreateDeepAgent` 未在 `len(subagents)>0` 时装配 SubagentRail——检查 `harness/factory.go:611` 分支是否被 code 模式参数绕过，按实际行为调整断言前必须先确认 Python 语义，不得直接删断言。

- [ ] **Step 3: Commit**

```bash
git add tests/integration/swarm/server/adapter/code_adapter_project_memory_test.go
git commit -m "test(adapter_itest): SwitchMode 往返后 project memory/subagent rail 保留且不误挂 ContextAssembleRail"
```

---

### Task 9: 文档、门禁与 CI

**Files:**
- Modify: `tests/integration/swarm/server/adapter/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

在 `doc.go` 的「文件目录」树中补两行，并更新包概述一句：

```
//	├── doc.go                                  # 包文档（本文件）
//	├── itest_env_helpers_test.go               # 集成测试共享 env 隔离基建
//	├── code_adapter_coding_memory_test.go      # CodingMemoryRail 装配级集成测试
//	└── code_adapter_project_memory_test.go     # ProjectMemoryRail + explore/plan 子代理装配级集成测试
```

- [ ] **Step 2: 格式与静态检查**

Run:

```bash
gofmt -l internal/swarm/server/adapter tests/integration/swarm/server/adapter
CGO_ENABLED=1 go vet -tags "sqlite_fts5 test integration" ./internal/swarm/server/adapter/... ./tests/integration/swarm/server/adapter/...
go build ./...
```

Expected: `gofmt` 无输出；`vet`/`build` 无报错

- [ ] **Step 3: 单测全量回归（相关包）**

Run: `CGO_ENABLED=1 go test ./internal/swarm/agents/harness/common/rails/... ./internal/swarm/server/adapter/... ./internal/agentcore/harness/subagents/... -count=1`

Expected: 全 `ok`

- [ ] **Step 4: 目标目录集成全量**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/... -count=1 -v`

Expected: `TestCodeAdapterCodingMemorySuite`（6）+ `TestCodeAdapterProjectMemorySuite`（D1 两子用例 + D2–D8）全 `PASS`

- [ ] **Step 5: 集成审计两层门禁**

Run: `go run ./cmd/integration-audit -verify`

Expected: 目标目录 `tests/integration/swarm/server/adapter` 未列入不通过清单；若有告警，检查是否因新文件调用跨度不足（必要时在用例中补真实调用而非加豁免）

- [ ] **Step 6: Commit 文档**

```bash
git add tests/integration/swarm/server/adapter/doc.go
git commit -m "docs(adapter_itest): 补 project memory 装配测试与 env 助手到目录清单"
```

- [ ] **Step 7: Push 并跟踪 CI**

```bash
git push origin HEAD
gh run list --limit 3
```

Expected: 触发的 CI 四个 job（Build / Lint / Unit Test / Integration Test）全 SUCCESS。任何红 job 都要修到绿（项目规范 6）。

---

## 附：Task ↔ Spec 覆盖对照

| Spec 段落 | 落地 Task |
|---|---|
| §4 B1/B2 子代理迭代上限 | Task 3 |
| §4 B3 附加目录三级回退 | Task 5 |
| §5 C2–C4 单测补口（C1 执行期取消，见 Task 1 订正说明） | Task 1 |
| §6.1 文件与基建 | Task 2、Task 3（骨架）、Task 9（doc.go） |
| §6.2 D1 | Task 3 |
| §6.2 D2 | Task 5 |
| §6.2 D3 | Task 3 |
| §6.2 D4/D5 | Task 4 |
| §6.2 D6 | Task 6 |
| §6.2 D7 | Task 7 |
| §6.2 D8 | Task 8 |
| §6.3 捕获方式 | Task 6 Step 1 |
| §7 明确不做 | 全 Task 均不涉及（无对应步骤） |
| §8 验证与门禁 | Task 9 |

---

## 执行期订正（实施时发现）

| # | 计划原样 | 实际执行 | 原因 |
|---|---|---|---|
| 1 | Task 1 C1「修 local 优先级假断言」 | **取消** | 原用例（files_test.go:114-138）本就有效；先前判断源于 `sed` 多区间输出误读。Python 的 merge 顺序语义由 C3 覆盖 |
| 2 | env 助手放 `tests/integration/swarm/server/adapter/itest_env_helpers_test.go` | 迁至 `tests/integration/testhelpers/adapter_env.go`（导出 `AdapterTestEnv` / `NewAdapterTestEnv`） | `cmd/integration-audit` 对 integration 目录**按文件**强制「必须调用 `suite.Run(`」，纯助手文件判违规；testhelpers 为纯辅助包，不在审计目录清单 |
| 3 | D6 断言 system 消息含 `## Project Memory` | 改为「含记忆正文标记 + 抬头中/英任一」 | 实测 code 模式下 builder 渲染为中文抬头（`## 项目记忆`）。见下「新发现偏差」 |

### 新发现偏差（未修，待评估）

**N-01 code 模式系统提示词语言未按 `_resolve_prompt_language()` 固定为 en**

- 现象：`CodeAdapter` 请求往返后 `SystemPromptBuilder` 渲染出中文 section（`## 项目记忆`），实测提示词整体为中文。
- 语义依据：Python `JiuwenClawCodeAdapter._resolve_prompt_language()` 固定返回 `"en"`，`_update_runtime_config` 中
  `_update_prompt_for_mode(runtime_config.mode, resolved_language)` 的 `resolved_language` 来自
  `_resolve_runtime_language()`（code 族 = `en` 或 override）。
- Go 侧成因：`CodeAdapter.updateRuntimeConfig` 调用 `c.deep.updatePromptForMode(config.Mode)`，
  而 `updatePromptForMode` 内部用 `d.resolveRuntimeLanguage()`——`d` 是 `DeepAdapter` 接收者，
  Go 无虚方法派发，取到的是 Deep 的偏好语言（默认 cn），CodeAdapter 的覆盖未被使用。
- 影响面：code 模式提示词语言（用户可见的大范围行为），**不在本次迁移的 3 项修复范围**，
  未擅自改动；如需对齐，建议单独立项（涉及 `updatePromptForMode` 签名或语言显式传参）。
