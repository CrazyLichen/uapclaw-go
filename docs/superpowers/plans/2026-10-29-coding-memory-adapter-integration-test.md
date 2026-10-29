# Coding Memory 装配级集成测试实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 CodeAdapter 三个移植偏差（embed 配置位置 / agentWorkspaceDir 层级 / configBase 字段遗漏），并新建 coding memory 装配级集成测试（7 用例，含 MockLLM 工具调用全流程）。

**Architecture:** 提交1 修 `internal/swarm/server/adapter/code_adapter.go` 三处（先写失败单测再修）；提交2 在 `tests/integration/swarm/server/adapter/` 新建集成测试套件，走 `CodeAdapter.CreateInstance` → `ProcessMessageImpl` 完整装配链，MockLLM 经 ClientRegistry 注入，`UAPCLAW_DATA_DIR` 三件套隔离。

**Tech Stack:** Go testing + testify/suite + tests/integration/suite 基类（RunnerSuite）+ mockllm.CreateToolCallResponse

**设计文档:** `docs/superpowers/specs/2026-10-29-coding-memory-adapter-integration-test-design.md`

---

## 文件结构

| 文件 | 动作 | 职责 |
|------|------|------|
| `internal/swarm/server/adapter/code_adapter.go` | 修改 | 三处修复：L278 AgentRootDir→AgentWorkspaceDir；L212 后补 configBase 赋值；resolveEmbeddingConfig 改读 configBase |
| `internal/swarm/server/adapter/code_adapter_test.go` | 修改 | 追加三个修复的失败单测（包内测试可访问非导出字段） |
| `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go` | 新建 | 集成测试套件（7 用例） |
| `tests/integration/swarm/server/adapter/doc.go` | 新建 | 包文档（规则1） |

**注意：** `tests/integration/swarm/server/adapter/` 是新目录。现有 `tests/integration/swarm/server/skill_manager_test.go` 位于 `server` 目录（package server_test），adapter 子目录不冲突。

---

## Task 1: 修复 agentWorkspaceDir 层级偏差

**背景:** Python `interface_code.py:250` 用 `get_agent_workspace_dir()` = `{DATA_DIR}/agent/workspace`；Go `code_adapter.go:278` 误用 `workspace.AgentRootDir()` = `{DATA_DIR}/agent`（少一层 /workspace）。Go 自己的 member 分支（L1519）用的是正确的 `workspace.AgentWorkspaceDir()`，证明 L278 是笔误。此偏差导致 coding_memory 与 user_todos 落盘位置与 Python 不一致。

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go:278`
- Test: `internal/swarm/server/adapter/code_adapter_test.go`

- [ ] **Step 1: 写失败单测（包内测试，可访问 c.deep 非导出字段）**

在 `code_adapter_test.go` 末尾追加（该文件 `package adapter`，同包可访问非导出字段）：

```go
// TestCodeAdapter_CreateInstance_agentWorkspaceDir路径 测试 agentWorkspaceDir 对齐
// Python get_agent_workspace_dir() = {DATA_DIR}/agent/workspace（interface_code.py:250）。
// 回归背景：曾误用 AgentRootDir()（少一层 /workspace），导致 coding_memory/user_todos
// 落盘到 {DATA_DIR}/agent/coding_memory 而非 {DATA_DIR}/agent/workspace/coding_memory。
func TestCodeAdapter_CreateInstance_agentWorkspaceDir路径(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	// 模拟已初始化：创建 config 目录防 getResolvedPaths 回退 ResourcesDir
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := NewCodeAdapter()
	ctx := t.Context()
	if err := c.CreateInstance(ctx, map[string]any{}, "agent.plan", ""); err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}

	want := filepath.Join(tmpDir, "agent", "workspace")
	if c.deep.agentWorkspaceDir != want {
		t.Errorf("agentWorkspaceDir = %q, want %q", c.deep.agentWorkspaceDir, want)
	}
}
```

需在文件头部 import 区补 `"github.com/uapclaw/uapclaw-go/internal/common/utils/path"`（别名 `pathutil`，若已有则跳过）。注意 `os` 已在 import 中。

- [ ] **Step 2: 跑测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/adapter/ -run TestCodeAdapter_CreateInstance_agentWorkspaceDir路径 -v
```

预期：FAIL，`agentWorkspaceDir = "{tmp}/agent", want "{tmp}/agent/workspace"`

- [ ] **Step 3: 修复**

`code_adapter.go:278`：

```go
	// 步骤 10: agentWorkspaceDir 始终指向系统 workspace
	// Python: self._agent_workspace_dir = str(get_agent_workspace_dir())
	// get_agent_workspace_dir() = agent_root/workspace，曾误用 AgentRootDir() 少一层 /workspace
	c.deep.agentWorkspaceDir = workspace.AgentWorkspaceDir()
```

只改 `AgentRootDir()` → `AgentWorkspaceDir()`，注释补一行说明。**L273 的 `workspaceDir` 回退保持不动**（设计文档 2.2 节：workspaceDir 是项目上下文路径，不影响 coding memory 落盘）。

- [ ] **Step 4: 跑测试确认通过 + 全包回归**

```bash
go test ./internal/swarm/server/adapter/ -run TestCodeAdapter_CreateInstance_agentWorkspaceDir路径 -v
go test ./internal/swarm/server/adapter/ -count=1
```

预期：新测试 PASS；全包无回归（已 grep 确认无既有测试断言 agentWorkspaceDir 路径）。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go internal/swarm/server/adapter/code_adapter_test.go
git commit -m "fix(adapter): CodeAdapter agentWorkspaceDir 补齐 /workspace 层级

对齐 Python get_agent_workspace_dir() = agent/workspace（interface_code.py:250）。
主分支曾误用 AgentRootDir()（member 分支 L1519 用法正确，证明是笔误），
导致 coding_memory 与 user_todos 落盘到 {DATA_DIR}/agent/ 而非
{DATA_DIR}/agent/workspace/，与 Python 路径不一致。"
```

---

## Task 2: 补 configBase 字段赋值（acp_chat 激活根因）

**背景:** `DeepAdapter.CreateInstance` 步骤 6 有 `d.configBase = configBase`（deep_adapter.go:412），但 `CodeAdapter.CreateInstance` 重写后漏了这行，configBase 只是局部变量。下游 `getToolCards` 的 `c.deep.configBase["acp_agents"]`（code_adapter.go:866）因此恒为 nil，acp_chat 在 CodeAdapter 路径下从未激活。

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go:218` 附近
- Test: `internal/swarm/server/adapter/code_adapter_test.go`

- [ ] **Step 1: 写失败单测**

```go
// TestCodeAdapter_CreateInstance_configBase已缓存 测试 configBase 缓存到 deep 字段。
// 回归背景：CodeAdapter.CreateInstance 曾漏存（DeepAdapter 步骤 6 有此赋值），
// 导致 getToolCards 中 c.deep.configBase["acp_agents"] 恒为 nil，acp_chat 永不激活。
func TestCodeAdapter_CreateInstance_configBase已缓存(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := NewCodeAdapter()
	ctx := t.Context()
	if err := c.CreateInstance(ctx, map[string]any{}, "agent.plan", ""); err != nil {
		t.Fatalf("CreateInstance error: %v", err)
	}

	if c.deep.configBase == nil {
		t.Fatal("configBase 应缓存到 deep 字段（getToolCards 读取 acp_agents 依赖它）")
	}
	if _, ok := c.deep.configBase["models"]; !ok {
		t.Error("configBase 应包含顶层 models 段")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/swarm/server/adapter/ -run TestCodeAdapter_CreateInstance_configBase已缓存 -v
```

预期：FAIL，`configBase 应缓存到 deep 字段`

- [ ] **Step 3: 修复**

在 `code_adapter.go` 步骤 3 加载完成后（`configBase, err := cfg.Load()` 的 err 检查之后、步骤 3.5 load_dotenv 之前）插入：

```go
	// 步骤 3.1: configBase 缓存到 deep 字段
	// Python: get_config() 全量配置；DeepAdapter.CreateInstance 步骤 6 有此赋值，
	// CodeAdapter 重写后曾遗漏，导致 getToolCards 读取 acp_agents 时恒为 nil
	c.deep.configBase = configBase
```

- [ ] **Step 4: 跑测试确认通过 + 全包回归**

```bash
go test ./internal/swarm/server/adapter/ -run TestCodeAdapter_CreateInstance_configBase已缓存 -v
go test ./internal/swarm/server/adapter/ -count=1
```

预期：PASS，无回归。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go internal/swarm/server/adapter/code_adapter_test.go
git commit -m "fix(adapter): CodeAdapter.CreateInstance 补 configBase 字段赋值

DeepAdapter.CreateInstance 步骤 6 有 d.configBase = configBase，CodeAdapter
重写后遗漏。getToolCards 的 c.deep.configBase[\"acp_agents\"] 因此恒为 nil，
acp_chat 工具在 CodeAdapter 路径下从未激活。"
```

---

## Task 3: 修 resolveEmbeddingConfig 读顶层 embed 段

**背景:** Python `create_coding_memory_rail(config=get_config())` 读顶层 `embed` 段（默认 config.yaml L350 就有）；Go `resolveEmbeddingConfig` 读 `configCache["embed"]`，而 configCache 只从 `react` 段复制，实际读 `react.embed`。同一份配置 Python 能启用向量召回、Go 静默降级。

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go:921-955`（resolveEmbeddingConfig）
- Test: `internal/swarm/server/adapter/code_adapter_test.go`

- [ ] **Step 1: 写失败单测**

```go
// TestCodeAdapter_ResolveEmbeddingConfig_顶层embed段 测试 embedding 配置从顶层 embed 段读取。
// Python: create_coding_memory_rail(config=get_config()) → config.get("embed")
// （interface_code.py:121，默认 config.yaml 顶层 L350 即有 embed 段）。
// 回归背景：曾误读 configCache（react 段），导致顶层 embed 配置永远读不到、静默降级。
func TestCodeAdapter_ResolveEmbeddingConfig_顶层embed段(t *testing.T) {
	c := NewCodeAdapter()
	// 模拟 CreateInstance 后的 configBase（顶层 embed 段，非 react.embed）
	c.deep.configBase = map[string]any{
		"embed": map[string]any{
			"embed_api_key":  "test-key",
			"embed_base_url": "http://127.0.0.1:1",
			"embed_model":    "text-embedding-test",
		},
	}
	// configCache 只有 react 段内容（不含 embed）——修复后不应从这里读
	c.deep.configCache = map[string]any{
		"agent_name": "main_agent",
	}

	emb := c.resolveEmbeddingConfig()
	if emb == nil {
		t.Fatal("顶层 embed 段存在时应返回 EmbeddingConfig")
	}
	if emb.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", emb.APIKey, "test-key")
	}
	if emb.BaseURL != "http://127.0.0.1:1" {
		t.Errorf("BaseURL = %q, want %q", emb.BaseURL, "http://127.0.0.1:1")
	}
	if emb.ModelName != "text-embedding-test" {
		t.Errorf("ModelName = %q, want %q", emb.ModelName, "text-embedding-test")
	}
}

// TestCodeAdapter_ResolveEmbeddingConfig_无embed段降级 测试无 embed 段时返回 nil（buildCodingMemoryRail 降级空配置）。
func TestCodeAdapter_ResolveEmbeddingConfig_无embed段降级(t *testing.T) {
	c := NewCodeAdapter()
	c.deep.configBase = map[string]any{"models": map[string]any{}}
	c.deep.configCache = map[string]any{}

	if emb := c.resolveEmbeddingConfig(); emb != nil {
		t.Errorf("无 embed 段时应返回 nil, got %+v", emb)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/swarm/server/adapter/ -run TestCodeAdapter_ResolveEmbeddingConfig -v
```

预期：`顶层embed段` FAIL（读到 configCache，embed 段不在其中返回 nil）；`无embed段降级` PASS（现状恰好满足）。

- [ ] **Step 3: 修复**

`resolveEmbeddingConfig`（code_adapter.go:926 起）改读取源：

```go
// resolveEmbeddingConfig 从 configBase 顶层 embed 段解析嵌入配置。
// Python: create_coding_memory_rail() (interface_code.py)
//
// 从 config 顶层 embed 段解析 embed_api_key/embed_base_url/embed_model，
// 配置不完整时返回 EmbeddingConfig（api_key 为空），让 Rail 降级到 fallback provider。
// 注意：读 configBase 顶层（对齐 Python config.get("embed")），不是 react 段的 configCache
// ——默认 config.yaml 顶层即有 embed 段（resources/config.yaml L350）。
func (c *CodeAdapter) resolveEmbeddingConfig() *embedding.EmbeddingConfig {
	embedRaw, ok := c.deep.configBase["embed"]
	if !ok {
		return nil
	}
	embedConfig, ok := embedRaw.(map[string]any)
	if !ok {
		return nil
	}

	apiKey, _ := embedConfig["embed_api_key"].(string)
	baseURL, _ := embedConfig["embed_base_url"].(string)
	if baseURL == "" {
		baseURL, _ = embedConfig["embed_api_base"].(string)
	}
	modelName, _ := embedConfig["embed_model"].(string)
	if modelName == "" {
		modelName = "text-embedding-v3"
	}

	// Python: 即使配置不完整也返回 EmbeddingConfig（api_key 为空时降级）
	return &embedding.EmbeddingConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
}
```

（函数体只改第一行读取源 `configCache` → `configBase`，注释同步更新。）

- [ ] **Step 4: 跑测试确认通过 + 全包回归**

```bash
go test ./internal/swarm/server/adapter/ -run TestCodeAdapter_ResolveEmbeddingConfig -v
go test ./internal/swarm/server/adapter/ -count=1
```

预期：两个测试 PASS，无回归。

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go internal/swarm/server/adapter/code_adapter_test.go
git commit -m "fix(adapter): resolveEmbeddingConfig 改读顶层 embed 段

对齐 Python create_coding_memory_rail(config=get_config()) → config.get(\"embed\")
（interface_code.py:121）。曾误读 configCache（react 段），默认 config.yaml 顶层
embed 段（L350）永远读不到，向量召回静默降级到 fallback provider。

依赖 Task 2 的 configBase 字段赋值（读取源从 configCache 改为 configBase）。"
```

---

## Task 4: 集成测试目录骨架 + doc.go

**Files:**
- Create: `tests/integration/swarm/server/adapter/doc.go`
- Create: `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go`（骨架）

- [ ] **Step 1: 创建 doc.go（规则1）**

```go
//go:build integration

// Package adapter_test 提供 swarm 装配层（CodeAdapter）集成测试。
//
// 对齐 Python tests/system_tests/ 的装配级测试形态（样板：
// test_project_memory.py::TestCodeModeIntegration），验证 CodeAdapter
// 把 agentcore 组件装配到一起后的端到端行为：
// CreateInstance（配置加载→rails 装配→DeepAgent 创建）→ ProcessMessageImpl
// （回调触发→工具执行→落盘验证）。
//
// 文件目录：
//
//	adapter/
//	├── doc.go                                  # 包文档
//	└── code_adapter_coding_memory_test.go      # CodingMemoryRail 装配级集成测试
//
// 对应 Python 代码：jiuwenswarm/tests/unit_tests/agentserver/test_code_adapter_acp_chat_tool.py
// 运行方式：go test -tags=integration ./tests/integration/swarm/server/adapter/...
package adapter_test
```

- [ ] **Step 2: 创建测试文件骨架（SetupTest 隔离三件套）**

```go
//go:build integration

package adapter_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeAdapterCodingMemorySuite 测试 CodeAdapter 装配链中的 CodingMemoryRail 行为。
//
// 覆盖：
//   - CreateInstance 装配 CodingMemoryRail 到 rails 列表
//   - embedding 配置降级（无 embed 段时 Rail 仍创建）
//   - coding_memory 目录按 project_name 创建（agent/workspace/coding_memory/{name}）
//   - 3 个 coding_memory 工具注册到 AbilityManager
//   - ProcessMessageImpl 注入 memory section
//   - coding_memory_write 工具调用全流程（落盘 + 索引更新）
//   - code_agent subagent 的 CodingMemoryRail 注入
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_code.py
type CodeAdapterCodingMemorySuite struct {
	isuite.RunnerSuite
	// providerName MockLLM 在 ClientRegistry 的注册名（config.yaml 引用）
	providerName string
	// envBackup 环境变量备份（TearDownTest 恢复）
	envBackup [][3]string
	// dataDir 每测试隔离的 UAPCLAW_DATA_DIR
	dataDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodeAdapterCodingMemorySuite(t *testing.T) {
	suite.Run(t, new(CodeAdapterCodingMemorySuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个测试方法前构建隔离环境。
//
// 隔离三件套（先例 tui_cli_route_test.go::setupDataDir + image_gen CI 路径坑）：
//  1. UAPCLAW_DATA_DIR → t.TempDir()，先 pathutil.ResetCache() 再做任何路径调用
//  2. 创建 config/ 目录（模拟已初始化，防 getResolvedPaths 回退 ResourcesDir）
//  3. 创建 agent/workspace/（os.WriteFile 不建父目录）
//
// MockLLM 注册必须先于写 config.yaml（YAML 中 client_provider 引用此注册名）。
func (s *CodeAdapterCodingMemorySuite) SetupTest() {
	// 注册 MockLLM（provider 名带时间戳防并发冲突，先例 runner_suite.go）
	s.providerName = fmt.Sprintf("mock_coding_mem_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	s.dataDir = s.T().TempDir()
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	pathutil.ResetCache()

	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))
}

// TearDownTest 恢复环境变量与缓存。
func (s *CodeAdapterCodingMemorySuite) TearDownTest() {
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	s.restoreEnv()
	pathutil.ResetCache()
}

// writeConfigYaml 写入 config/config.yaml。
//
// 关键点（对齐 buildModelCacheFromDefaults / NewModelClientConfig 校验）：
//   - model_name 必须在 model_client_config 内层（外层会被跳过）
//   - api_base 与 client_id 为必填校验项
//   - embed 写顶层段（Task 3 修复后的行为，对齐 Python）
//   - react.subagents.code_agent.enabled 控制测试 7 的 subagent 装配
func (s *CodeAdapterCodingMemorySuite) writeConfigYaml(enableCodeAgent bool) {
	yamlContent := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true

embed:
  embed_api_key: test-embed-key
  embed_base_url: http://127.0.0.1:1
  embed_model: text-embedding-test

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
  subagents:
    code_agent:
      enabled: %t
`, s.providerName, s.providerName, enableCodeAgent)

	s.Require().NoError(os.WriteFile(
		filepath.Join(s.dataDir, "config", "config.yaml"),
		[]byte(yamlContent), 0o644))
}

// writeConfigYamlNoEmbed 写入无 embed 段的 config（测试 2 降级场景）。
func (s *CodeAdapterCodingMemorySuite) writeConfigYamlNoEmbed() {
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
`, s.providerName, s.providerName)

	s.Require().NoError(os.WriteFile(
		filepath.Join(s.dataDir, "config", "config.yaml"),
		[]byte(yamlContent), 0o644))
}

// saveAndSetenv 记录原值并设置环境变量（TearDown 恢复）。
func (s *CodeAdapterCodingMemorySuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	s.envBackup = append(s.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *CodeAdapterCodingMemorySuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "false" {
			_ = os.Unsetenv(e[0])
		} else {
			_ = os.Setenv(e[0], e[1])
		}
	}
	s.envBackup = nil
}
```

- [ ] **Step 3: 编译验证**

```bash
go build -tags=integration ./tests/integration/swarm/server/adapter/...
go vet -tags=integration ./tests/integration/swarm/server/adapter/...
```

预期：无错误（此时还没有测试方法，gofmt 检查 import 分组：标准库一组，其余一组）。

- [ ] **Step 4: gofmt 检查**

```bash
gofmt -l tests/integration/swarm/server/adapter/
```

预期：无输出（有输出则 `gofmt -w` 修正——注意 CJK 注释 gofmt 坑，见 memory）。

- [ ] **Step 5: Commit**

```bash
git add tests/integration/swarm/server/adapter/
git commit -m "test(integration): CodeAdapter coding memory 测试骨架与隔离三件套

RunnerSuite 基类 + UAPCLAW_DATA_DIR 三件套隔离（先例 tui_cli_route_test.go
setupDataDir）+ MockLLM 经 ClientRegistry 注入。config.yaml 模板：顶层 embed 段
（Task 3 修复后行为）+ model_client_config 内层必填校验字段。"
```

---

## Task 5: 用例 1-3（rails 装配 / embedding 降级 / 目录按项目名）

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go`

- [ ] **Step 1: 追加三个测试方法**

在测试文件非导出函数区之前（suite.Run 之后按 testify 惯例放测试方法，参考 skill_manager_test.go 的排列）追加。需补 import：`"context"`、`"reflect"`、`memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"`：

```go
// TestCreateInstance_Rails装配 验证 CreateInstance 把 CodingMemoryRail 装配进 DeepAgent。
// 对齐 Python: JiuwenClawCodeAdapter._build_agent_rails() 固定 rail 之一
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_Rails装配() {
	s.writeConfigYaml(false)

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	defer func() { _ = c.Cleanup() }()

	inst := c.Instance()
	s.Require().NotNil(inst)

	rails := inst.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "CodingMemoryRail 应装配到 rails 列表")
}

// TestBuildCodingMemoryRail_Embedding降级 验证无 embed 段时 Rail 仍创建（降级空 EmbeddingConfig）。
// 对齐 Python: test_code_adapter_builds_coding_memory_rail_without_embedding_config
func (s *CodeAdapterCodingMemorySuite) TestBuildCodingMemoryRail_Embedding降级() {
	s.writeConfigYamlNoEmbed()

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	defer func() { _ = c.Cleanup() }()

	inst := c.Instance()
	s.Require().NotNil(inst)

	// buildCodingMemoryRail 永远返回非 nil（构造器无条件返回，降级到 fallback provider）
	rails := inst.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "无 embed 段时 CodingMemoryRail 仍应创建（降级）")
}

// TestCreateInstance_目录按项目名 验证 coding_memory 目录按 project_name 创建。
// Python: coding_memory_dir = agent_workspace_dir/coding_memory/{project_basename}
// （interface_code.py:139-141），agent_workspace_dir = {DATA_DIR}/agent/workspace
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_目录按项目名() {
	s.writeConfigYaml(false)
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "myproj"), 0o755))

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	defer func() { _ = c.Cleanup() }()

	// Task 1 修复后路径：{DATA_DIR}/agent/workspace/coding_memory/{project_name}
	want := filepath.Join(s.dataDir, "agent", "workspace", "coding_memory", "myproj")
	info, err := os.Stat(want)
	s.Require().NoError(err, "目录应创建: %s", want)
	s.True(info.IsDir())
}
```

- [ ] **Step 2: 跑测试**

```bash
go test -tags=integration ./tests/integration/swarm/server/adapter/ -v -count=1 -timeout 300s
```

预期：3 个测试 PASS。若 `目录按项目名` FAIL 显示路径 `{dataDir}/agent/coding_memory/myproj`，说明 Task 1 修复未生效，回头检查。

**排查提示**：若 CreateInstance 因模型配置失败，检查 config.yaml 的 `client_id` 是否漏写（buildModelCacheFromDefaults 必填校验）；若路径断言错，先 `logger.Setup` + debug 日志定位 agentWorkspaceDir 实际值。

- [ ] **Step 3: Commit**

```bash
git add tests/integration/swarm/server/adapter/
git commit -m "test(integration): coding memory 装配用例 1-3（rails/embedding降级/目录按项目名）"
```

---

## Task 6: 用例 4-5（工具注册 / Section 注入）

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go`

- [ ] **Step 1: 追加两个测试方法**

需补 import：`hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"`：

```go
// TestCreateInstance_工具注册 验证 3 个 coding_memory 工具注册到 AbilityManager。
// 对齐 Python: CodingMemoryRail.init() → _register_coding_memory_tools
func (s *CodeAdapterCodingMemorySuite) TestCreateInstance_工具注册() {
	s.writeConfigYaml(false)

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	defer func() { _ = c.Cleanup() }()

	am := c.Instance().AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("coding_memory_read"), "应注册 coding_memory_read")
	s.NotNil(am.Get("coding_memory_write"), "应注册 coding_memory_write")
	s.NotNil(am.Get("coding_memory_edit"), "应注册 coding_memory_edit")
}

// TestProcessMessage_记忆Section注入 验证 ProcessMessageImpl 触发 BeforeModelCall 注入 memory section。
// 对齐 Python: CodingMemoryRail.before_model_call() → system_prompt_builder.add_section
func (s *CodeAdapterCodingMemorySuite) TestProcessMessage_记忆Section注入() {
	s.writeConfigYaml(false)
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("section 注入验证"))

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	defer func() { _ = c.Cleanup() }()

	// ProcessMessageImpl 完整装配链入口（先例 real_llm/system_prompt_capture_test.go:372-391）
	params, _ := json.Marshal(map[string]any{
		"query": "查询记忆",
		"mode":  "agent.plan",
		"files": map[string]any{},
	})
	req := swarmschema.NewAgentRequest(
		"itest-req-1", "itest-channel", swarmschema.ReqMethodChatSend, params,
		swarmschema.WithAgentSessionID("itest-session-cm"),
	)
	inputs := map[string]any{
		"conversation_id": "itest-session-cm",
		"query":           "查询记忆",
		"channel":         "itest-channel",
		"language":        "zh",
	}
	resp, err := c.ProcessMessageImpl(s.Ctx, req, inputs)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	spb := c.Instance().SystemPromptBuilder()
	s.Require().NotNil(spb)
	s.True(spb.HasSection(hsections.SectionMemory), "应注入 SectionMemory")

	section := spb.GetSection(hsections.SectionMemory)
	s.Require().NotNil(section)
	s.NotEmpty(section.Content, "SectionMemory 内容不应为空")
}
```

- [ ] **Step 2: 跑测试**

```bash
go test -tags=integration ./tests/integration/swarm/server/adapter/ -v -count=1 -timeout 300s
```

预期：5 个测试 PASS。

**排查提示**：若 Section 注入失败——Rail 的 BeforeModelCall 只在 `system_prompt_builder != nil` 时注入；CreateInstance 后 builder 一定非 nil（CreateDeepAgent 装配），若仍失败用 debug 日志查 Rail Init 是否执行（EnsureInitialized → registerRails）。

- [ ] **Step 3: Commit**

```bash
git add tests/integration/swarm/server/adapter/
git commit -m "test(integration): coding memory 装配用例 4-5（工具注册/Section注入）"
```

---

## Task 7: 用例 6（写记忆全流程——核心用例）

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go`

- [ ] **Step 1: 追加核心测试方法**

```go
// TestProcessMessage_写记忆全流程 验证 MockLLM 驱动 coding_memory_write 工具调用→真实落盘→索引更新。
// 对齐 Python: CodingMemoryRail 注册工具 → agent 循环调用 → coding_memory_write_with_context
// （write 文件 + upsertMemoryIndex 更新 MEMORY.md）
func (s *CodeAdapterCodingMemorySuite) TestProcessMessage_写记忆全流程() {
	s.writeConfigYaml(false)

	// 工具内容：合法 frontmatter（name/description/type 三字段必填，type 枚举
	// user/feedback/project/reference）
	memContent := "---\nname: user_pref\ndescription: user preference for tests\ntype: user\n---\nalways run focused tests first"
	argsJSON := fmt.Sprintf(`{"path":"user_pref.md","content":%q}`, memContent)

	s.MockLLM.SetResponses(
		// 第 1 轮：触发 coding_memory_write 工具调用
		mockllm.CreateToolCallResponse("coding_memory_write", argsJSON),
		// 第 2 轮：文本收尾
		mockllm.CreateTextResponse("记忆已保存。"),
	)

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	defer func() { _ = c.Cleanup() }()

	params, _ := json.Marshal(map[string]any{
		"query": "记住：测试优先跑聚焦用例",
		"mode":  "agent.plan",
		"files": map[string]any{},
	})
	req := swarmschema.NewAgentRequest(
		"itest-req-2", "itest-channel", swarmschema.ReqMethodChatSend, params,
		swarmschema.WithAgentSessionID("itest-session-cm-write"),
	)
	inputs := map[string]any{
		"conversation_id": "itest-session-cm-write",
		"query":           "记住：测试优先跑聚焦用例",
		"channel":         "itest-channel",
		"language":        "zh",
	}
	resp, err := c.ProcessMessageImpl(s.Ctx, req, inputs)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// 断言 1：MockLLM 被调用 2 次（工具调用轮 + 文本轮）
	s.Equal(2, s.MockLLM.InvokeCallCount(), "应为两轮模型调用")

	// 断言 2：记忆文件落盘（Task 1 修复后路径）
	memFile := filepath.Join(s.dataDir, "agent", "workspace", "coding_memory", "myproj", "user_pref.md")
	content, err := os.ReadFile(memFile)
	s.Require().NoError(err, "记忆文件应落盘: %s", memFile)
	s.Contains(string(content), "always run focused tests first", "文件内容应包含工具写入的 body")
	s.Contains(string(content), "name: user_pref", "文件应保留 frontmatter")

	// 断言 3：MEMORY.md 索引更新（upsertMemoryIndex）
	indexPath := filepath.Join(s.dataDir, "agent", "workspace", "coding_memory", "myproj", "MEMORY.md")
	indexContent, err := os.ReadFile(indexPath)
	s.Require().NoError(err, "MEMORY.md 索引应更新: %s", indexPath)
	s.Contains(string(indexContent), "user_pref.md", "索引应含新条目文件名")
}
```

- [ ] **Step 2: 跑测试**

```bash
go test -tags=integration ./tests/integration/swarm/server/adapter/ -run TestProcessMessage_写记忆全流程 -v -count=1 -timeout 300s
```

预期：PASS。

**排查提示（三层故障定位）**：
1. `InvokeCallCount == 1` 且 resp 带 error → 工具调用轮失败。查工具是否注册（Task 6 用例 4 先行验证）、tool_call args JSON 是否能被 schema 解析（`fmt.Sprintf %q` 转义是否正确）
2. InvokeCallCount == 2 但文件不存在 → 工具执行失败。查 Rail 的 toolCtx.CodingMemoryDir 是否为 `{dataDir}/agent/workspace/coding_memory/myproj`（Rail Init 时从构造参数恢复绝对路径）；logger debug 查 CodingMemoryWriteWithContext 的 error 返回
3. 文件存在但 MEMORY.md 无条目 → upsertMemoryIndex 的 frontmatter name/description 校验失败（空值跳过索引更新，lite/coding_memory_tool_ops.go:648 附近）

- [ ] **Step 3: Commit**

```bash
git add tests/integration/swarm/server/adapter/
git commit -m "test(integration): coding memory 写记忆全流程用例（工具调用→落盘→索引更新）"
```

---

## Task 8: 用例 7（code_agent subagent 注入）

**Files:**
- Modify: `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go`

- [ ] **Step 1: 追加测试方法**

说明：不走 `c.Instance().CreateSubagent(...)`（那是运行时 ReAct 循环触发路径，需要编排 subagent 工具调用的 Mock 响应链），而是直接断言**装配产物**——`buildConfiguredSubagents` 产出的 `SubAgentConfig.Rails`。这更贴近被测对象（CodeAdapter 的装配行为），且不受 DeepAgent 运行时影响。

但 `buildConfiguredSubagents` 是非导出方法，外部测试包无法访问。**方案：通过 Instance 的 subagent 注册状态验证**。需先确认：DeepAgent 创建时 `Subagents` 参数存到哪、如何枚举（见 Step 1 的探查命令，若 `findSubagentSpec` 无导出枚举 API，则改用 `Instance().CreateSubagent(s.Ctx, "code_agent", "sub-sess")` 运行时路径——它内部走 `findSubagentSpec("code_agent")` → `CreateCodeAgent(ctx, kwargs)`，`kwargs.Rails` 即装配的 rails，返回的 DeepAgent 实例可 `FindRailsByType` 断言）。

```go
// TestCodeAgentSubagent_CodingMemory注入 验证 code_agent subagent 装配包含 CodingMemoryRail。
// 对齐 Python: _build_configured_subagents 中
// if coding_memory_rail is not None: code_agent_rails = [SysOperationRail(), coding_memory_rail]
func (s *CodeAdapterCodingMemorySuite) TestCodeAgentSubagent_CodingMemory注入() {
	s.writeConfigYaml(true) // react.subagents.code_agent.enabled: true

	c := adapter.NewCodeAdapter()
	s.Require().NoError(c.CreateInstance(s.Ctx, map[string]any{
		"project_dir": filepath.Join(s.dataDir, "myproj"),
	}, "agent.plan", ""))
	defer func() { _ = c.Cleanup() }()

	// 运行时创建 code_agent subagent（内部 findSubagentSpec → CreateCodeAgent，
	// kwargs.Rails 来自装配的 codeCfg.Rails）
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("subagent 就绪"))
	sub, err := c.Instance().CreateSubagent(s.Ctx, "code_agent", "itest-sub-session")
	s.Require().NoError(err)
	s.Require().NotNil(sub)

	// subagent 的 rails 应含 CodingMemoryRail（FindRailsByType 是 DeepAgentInterface 方法）
	rails := sub.FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))
	s.NotEmpty(rails, "code_agent subagent 应注入 CodingMemoryRail")

	// subagent 的工具也应注册（Rail Init 在子实例 EnsureInitialized 时触发）
	am := sub.AbilityManager()
	s.Require().NotNil(am)
	s.NotNil(am.Get("coding_memory_write"), "subagent 应注册 coding_memory_write")
}
```

**执行前探查（Step 1 前必做）**：

```bash
grep -n "func (d \*DeepAgent) CreateSubagent" -A 3 internal/agentcore/harness/deep_agent.go
grep -n "CreateCodeAgent" internal/agentcore/harness/deep_agent.go
grep -rn "code_agent.*Enabled\|enabled" internal/agentcore/harness/subagents/code_agent.go | head -5
```

确认：`CreateCodeAgent` 返回 `*DeepAgent`（code_agent_factory.go:42），`DeepAgent` 实现 `DeepAgentInterface`（含 FindRailsByType/AbilityManager）。若 `CreateSubagent` 因 `findSubagentSpec` 返回 nil 失败（spec 未存），改用 `subCfg := subagents.BuildCodeAgentConfig` 直构方案并在计划中记录偏差。

- [ ] **Step 2: 跑测试**

```bash
go test -tags=integration ./tests/integration/swarm/server/adapter/ -run TestCodeAgentSubagent -v -count=1 -timeout 300s
```

预期：PASS。7 个用例全部就位。

**排查提示**：若 `findSubagentSpec` 找不到 code_agent——检查 `react.subagents.code_agent.enabled` 是否被 `isSubagentExplicitlyEnabled` 正确读取（deep_adapter_config.go:241，dict 值的 enabled 字段）；若 subagent Rails 为空——检查 `c.codingMemoryRail != nil`（buildConfiguredSubagents 在 CreateInstance 步骤 18 执行，此时 codingMemoryRail 已在步骤 16 装配，顺序正确）。

- [ ] **Step 3: Commit**

```bash
git add tests/integration/swarm/server/adapter/
git commit -m "test(integration): code_agent subagent 的 CodingMemoryRail 注入用例"
```

---

## Task 9: 全量本地回归 + 规则7自检 + 推送 CI

**Files:** 无新文件

- [ ] **Step 1: 全量本地测试（单进程互斥，规则5）**

```bash
pgrep -f 'go (build|test)' || true
go test ./internal/swarm/server/adapter/ -count=1
go test -tags=integration ./tests/integration/swarm/server/adapter/ -count=1 -timeout 600s
```

预期：全 PASS。

- [ ] **Step 2: 全项目编译 + lint**

```bash
go build ./...
go vet ./internal/swarm/server/adapter/ ./tests/integration/swarm/server/adapter/
staticcheck ./internal/swarm/server/adapter/ ./tests/integration/swarm/server/adapter/ 2>/dev/null || true
```

预期：无错误。

- [ ] **Step 3: 规则7 integration-audit 自检**

```bash
go run ./cmd/integration-audit -verify
```

预期：通过（swarm/server/adapter 目录静跨度 ≥3：swarm/server/adapter + agentcore/harness + agentcore/memory/lite + common/config + common/workspace；覆盖交叉验证同理）。

若灰区告警：确认目录真实调用（`adapter.NewCodeAdapter(...)`、`memrail.CodingMemoryRail` 类型引用 + `FindRailsByType` 调用链）跨足 3 个子系统；本计划测试 6 的真实落盘路径会产生 memory/lite 的覆盖行。

- [ ] **Step 4: gofmt 终检 + 推送**

```bash
gofmt -l internal/ tests/ | grep -v _test.go || true
gofmt -l internal/swarm/server/adapter/ tests/integration/swarm/server/adapter/
```

预期：无输出。然后推送：

```bash
git push origin main
gh run list --limit 3
```

- [ ] **Step 5: 监控 CI 到全绿（规则6完成标准）**

```bash
gh run watch <run-id>
```

预期：Build / Lint / Unit Test / Integration Test 四 job 全绿。

**已知 CI 风险（来自 memory）**：
- SA5011 假阳性风暴历史（本地 0 issues 即可推送，计数器自愈）
- gofmt CJK 坑（第 2 次）：必须完整路径 gofmt 检查
- 集成测试偶发 race（race 复现须 count=5 整包跑）
- Push Protection 拦密钥：本计划 config.yaml 中 api_key 均为 mock 值，无真实密钥风险

- [ ] **Step 6: 更新 IMPLEMENTATION_PLAN.md（规则2，如适用）**

检查 `IMPLEMENTATION_PLAN.md` 中是否有本工作对应的条目（装配级集成测试属于测试补充，可能无对应步骤；确认后如无对应条目则跳过此步）。

---

## 自审记录

1. **Spec 覆盖**：设计文档 §2（三个修复）→ Task 1/2/3；§3.1-3.3（结构/隔离/YAML）→ Task 4；§3.4 七用例 → Task 5（1-3）/Task 6（4-5）/Task 7（6）/Task 8（7）；§5 完成标准 → Task 9。无缺口。
2. **占位符扫描**：Task 8 Step 1 含"执行前探查"命令与两个备选方案（含完整代码），不是占位符而是已识别的实现分叉点；其余任务代码完整。
3. **类型一致性**：`workspace.AgentWorkspaceDir()`（Task 1）与断言路径 `agent/workspace`（Task 5/7 用例）一致；`c.deep.configBase`（Task 2 写入、Task 3 读取）一致；`memrail.CodingMemoryRail` 与 `FindRailsByType(reflect.TypeOf(&memrail.CodingMemoryRail{}))`（Task 5/8）一致；`CreateToolCallResponse` 签名与 mock_client.go:88 一致。
