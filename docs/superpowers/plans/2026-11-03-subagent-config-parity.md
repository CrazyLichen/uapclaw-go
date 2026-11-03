# Subagent 配置链路 parity 修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Go 的 subagent 装配与配置写回链路和 Python `jiuwenswarm` 对齐，修复 code 模式 browser、code_agent 迭代上限、agents 写操作同步重载、AgentManager 错误传播四个缺口。

**Architecture:** 在 `DeepAdapter` 抽出共享的 browser 子代理构建方法，`CodeAdapter` 复用；`AgentManager.ReloadAgentsConfig` 改为失败即返回；`AgentServer` 新增同步重载 helper，五个 agents 写操作在配置写成功后调用。code 模式 `research_agent` 作为有意偏差保留。

**Tech Stack:** Go 1.26、`testing`、`testify`、`config.yaml`、`sqlite_fts5` 构建标签。

**设计文档:** `docs/superpowers/specs/2026-11-03-subagent-config-parity-design.md`

---

## 文件结构

| 文件 | 职责 | 本次动作 |
|---|---|---|
| `internal/swarm/server/adapter/deep_adapter_config.go` | Deep 模式 subagent 装配与配置解析 | 新增 `buildBrowserSubagentSpec`，Deep browser 分支改为调用它 |
| `internal/swarm/server/adapter/code_adapter.go` | Code 模式 subagent 装配 | 接入 browser、code_agent 迭代上限、research 注释 |
| `internal/swarm/server/runtime/agent_manager.go` | Agent 实例管理与配置重载 | 单 agent 重载失败立即返回 error |
| `internal/swarm/server/handle_agents.go` | agents.* RPC handler | 新增 `reloadAgentsConfig`，五个写操作接线 |
| `internal/swarm/server/adapter/code_adapter_test.go` | CodeAdapter 单测 | 新增 browser / code_agent / research 测试与 helper |
| `internal/swarm/server/adapter/deep_adapter_subagents_test.go` | DeepAdapter subagent 单测 | 新增 browser 特征测试 |
| `internal/swarm/server/runtime/agent_manager_test.go` | AgentManager 单测 | 新增重载错误传播测试 |
| `internal/swarm/server/handle_agents_test.go` | agents handler 单测 | 新增写链路与重载失败测试 |
| `resources/config.yaml` | 出厂配置模板 | 只补注释 |

---

### Task 1: Code 模式接入 browser_agent

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go`（`buildBrowserSubagentParams` 之后）
- Modify: `internal/swarm/server/adapter/code_adapter.go:190-225`、`:866-871`
- Test: `internal/swarm/server/adapter/code_adapter_test.go`
- Test: `internal/swarm/server/adapter/deep_adapter_subagents_test.go`

- [ ] **Step 1: 写 CodeAdapter browser 失败测试**

在 `internal/swarm/server/adapter/code_adapter_test.go` 的导入块补充：

```go
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
```

在文件末尾「非导出函数」区块添加：

```go
// newCodeAdapterWithModel 构造带可用模型的 CodeAdapter。
func newCodeAdapterWithModel(t *testing.T) *CodeAdapter {
	t.Helper()
	c := NewCodeAdapter()
	if model := c.deep.createModel(map[string]any{
		"models": map[string]any{
			"defaults": []any{modelEntry("m1", 8000, true, "")},
		},
	}); model == nil {
		t.Fatal("测试模型构建失败")
	}
	return c
}

// findSubagentSpecByName 按 AgentCard 名称查找子代理规格。
func findSubagentSpecByName(specs []hschema.SubagentSpec, name string) *hschema.SubAgentConfig {
	for _, spec := range specs {
		cfg, ok := spec.(*hschema.SubAgentConfig)
		if ok && cfg.AgentCard != nil && cfg.AgentCard.GetName() == name {
			return cfg
		}
	}
	return nil
}

// TestCodeAdapterBuildConfiguredSubagents_BrowserAgent 验证 browser runtime 开启时
// 装配 browser_agent，并注入 BROWSER_DRIVER / BROWSER_MANAGED_BINARY 默认值。
func TestCodeAdapterBuildConfiguredSubagents_BrowserAgent(t *testing.T) {
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "true")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "")
	t.Setenv("BROWSER_DRIVER", "")
	t.Setenv("BROWSER_MANAGED_BINARY", "")

	c := newCodeAdapterWithModel(t)
	specs, _ := c.buildConfiguredSubagents(map[string]any{
		"max_iterations": 20,
		"subagents": map[string]any{
			"browser_agent": map[string]any{"max_iterations": 7},
		},
	}, map[string]any{
		"browser": map[string]any{"chrome_path": "/opt/chrome"},
	})

	browser := findSubagentSpecByName(specs, subagents.BrowserAgentFactoryName)
	if browser == nil {
		t.Fatal("browser runtime 开启时应装配 browser_agent")
	}
	if browser.MaxIterations != 7 {
		t.Errorf("browser_agent MaxIterations = %d, want 7", browser.MaxIterations)
	}
	if got := os.Getenv("BROWSER_DRIVER"); got != "managed" {
		t.Errorf("BROWSER_DRIVER = %q, want %q", got, "managed")
	}
	if got := os.Getenv("BROWSER_MANAGED_BINARY"); got != "/opt/chrome" {
		t.Errorf("BROWSER_MANAGED_BINARY = %q, want %q", got, "/opt/chrome")
	}
}

// TestCodeAdapterBuildConfiguredSubagents_BrowserRuntime未开启 验证 runtime 关闭时不装配。
func TestCodeAdapterBuildConfiguredSubagents_BrowserRuntime未开启(t *testing.T) {
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "false")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "")

	c := newCodeAdapterWithModel(t)
	specs, _ := c.buildConfiguredSubagents(map[string]any{
		"subagents": map[string]any{
			"browser_agent": map[string]any{"enabled": true},
		},
	}, map[string]any{})

	if got := findSubagentSpecByName(specs, subagents.BrowserAgentFactoryName); got != nil {
		t.Fatal("browser runtime 未开启时不应装配 browser_agent")
	}
}
```

- [ ] **Step 2: 写 DeepAdapter browser 特征测试**

在 `internal/swarm/server/adapter/deep_adapter_subagents_test.go` 的导入块补充：

```go
	"os"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
```

在文件末尾添加：

```go
// TestDeepAdapterBuildConfiguredSubagents_BrowserAgent 锁定 Deep 模式 browser 装配行为，
// 作为 buildBrowserSubagentSpec 抽取前后的特征测试。
func TestDeepAdapterBuildConfiguredSubagents_BrowserAgent(t *testing.T) {
	t.Setenv("PLAYWRIGHT_RUNTIME_MCP_ENABLED", "true")
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "")
	t.Setenv("BROWSER_DRIVER", "")
	t.Setenv("BROWSER_MANAGED_BINARY", "")

	d := newAdapterWithModel(t)
	specs, _ := d.buildConfiguredSubagents(map[string]any{
		"max_iterations": 20,
		"subagents": map[string]any{
			"browser_agent": map[string]any{"max_iterations": 7},
		},
	}, map[string]any{
		"browser": map[string]any{"chrome_path": "/opt/chrome"},
	})

	browser := findSubagentSpecByName(specs, subagents.BrowserAgentFactoryName)
	if browser == nil {
		t.Fatal("Deep 模式 browser runtime 开启时应装配 browser_agent")
	}
	if browser.MaxIterations != 7 {
		t.Errorf("browser_agent MaxIterations = %d, want 7", browser.MaxIterations)
	}
	if got := os.Getenv("BROWSER_DRIVER"); got != "managed" {
		t.Errorf("BROWSER_DRIVER = %q, want %q", got, "managed")
	}
	if got := os.Getenv("BROWSER_MANAGED_BINARY"); got != "/opt/chrome" {
		t.Errorf("BROWSER_MANAGED_BINARY = %q, want %q", got, "/opt/chrome")
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ \
  -run '^TestCodeAdapterBuildConfiguredSubagents_BrowserAgent$' -count=1 -v
```

预期：FAIL，报 `browser runtime 开启时应装配 browser_agent`。

DeepAdapter 特征测试此时应 PASS（旧实现已内联同样逻辑）：

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ \
  -run '^TestDeepAdapterBuildConfiguredSubagents_BrowserAgent$' -count=1 -v
```

- [ ] **Step 4: 抽取 buildBrowserSubagentSpec**

在 `internal/swarm/server/adapter/deep_adapter_config.go` 的 `buildBrowserSubagentParams` 函数之后添加：

```go
// buildBrowserSubagentSpec 构建 browser 子代理规格。
// Python: interface_deep.py:912-948 / interface_code.py:731-760
//
// 调用方需先确认 browser runtime 已启用。本函数负责：
//  1. BROWSER_DRIVER 缺省注入 managed；
//  2. BROWSER_MANAGED_BINARY 从 browser.chrome_path 解析；
//  3. max_iterations 两级回退（browser_agent.max_iterations → react.max_iterations → 15）；
//  4. 调用 BuildBrowserAgentConfig。
func (d *DeepAdapter) buildBrowserSubagentSpec(
	model *llm.Model,
	config map[string]any,
	configBase map[string]any,
	browserAgentCfg map[string]any,
) *hschema.SubAgentConfig {
	// Python: if not str(os.getenv("BROWSER_DRIVER") or "").strip():
	//   os.environ["BROWSER_DRIVER"] = "managed"
	if strings.TrimSpace(os.Getenv("BROWSER_DRIVER")) == "" {
		// Python os.environ 赋值无错误返回；Setenv 失败仅影响默认值兜底，不阻断
		_ = os.Setenv("BROWSER_DRIVER", "managed")
		logger.Info(logComponent).
			Msg("browser subagent enabled without BROWSER_DRIVER; defaulting to managed mode")
	}
	// Python: if not str(os.getenv("BROWSER_MANAGED_BINARY") or "").strip():
	//   chrome_path = self._resolve_managed_browser_binary_from_config()
	//   if chrome_path: os.environ["BROWSER_MANAGED_BINARY"] = chrome_path
	if strings.TrimSpace(os.Getenv("BROWSER_MANAGED_BINARY")) == "" {
		if chromePath := d.resolveManagedBrowserBinaryFromConfig(configBase); chromePath != "" {
			// Python os.environ 赋值无错误返回；Setenv 失败仅影响默认值兜底，不阻断
			_ = os.Setenv("BROWSER_MANAGED_BINARY", chromePath)
			logger.Info(logComponent).
				Str("binary", chromePath).
				Msg("using browser.chrome_path for managed browser")
		}
	}
	params := d.buildBrowserSubagentParams(config, browserAgentCfg)
	return subagents.BuildBrowserAgentConfig(model, params)
}
```

把 `DeepAdapter.buildConfiguredSubagents` 的 browser 分支替换为：

```go
	// ── browser_agent: 由 browser_runtime_enabled 控制 ──
	// 对齐 Python _build_configured_subagents browser 分支：
	// browser_enabled → BROWSER_DRIVER 默认值注入 + chrome_path 解析 + 注册 subagent；
	// 未启用但配置了 browser_agent → 跳过并记录日志
	browserAgentCfg, _ := subagentsCfg["browser_agent"].(map[string]any)
	if d.browserRuntimeEnabled() {
		if cfg := d.buildBrowserSubagentSpec(model, config, configBase, browserAgentCfg); cfg != nil {
			specs = append(specs, cfg)
		}
	} else if subagentsCfg != nil && len(browserAgentCfg) > 0 {
		// Python: logger.info("browser_agent config detected but browser runtime is not enabled; skipping")
		logger.Info(logComponent).
			Msg("browser_agent config detected but browser runtime is not enabled; skipping browser subagent registration")
	}
```

- [ ] **Step 5: CodeAdapter 接入 browser**

把 `internal/swarm/server/adapter/code_adapter.go` 中：

```go
	// ── 按配置启用：browser_agent ──
	// Python: browser_agent 按配置启用
	// ⤵️ browser_agent: BuildBrowserAgentConfig 签名已变更，需要 SubagentCreateParams
	// 暂时跳过，等 browser 功能实现时回填
	// TODO: c.isSubagentExplicitlyEnabled(subagentsCfg, "browser_agent") — 待 browser 功能实现时回填
```

替换为：

```go
	// ── 按配置启用：browser_agent ──
	// Python: interface_code.py:731-760
	browserAgentCfg, _ := subagentsCfg["browser_agent"].(map[string]any)
	if c.deep.browserRuntimeEnabled() {
		if cfg := c.deep.buildBrowserSubagentSpec(c.deep.modelSnapshot(), config, configBase, browserAgentCfg); cfg != nil {
			specs = append(specs, cfg)
		}
	} else if subagentsCfg != nil && len(browserAgentCfg) > 0 {
		logger.Info(logComponent).
			Msg("browser_agent config detected but browser runtime is not enabled; skipping browser subagent registration")
	}
```

- [ ] **Step 6: 运行测试确认通过**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ \
  -run 'TestCodeAdapterBuildConfiguredSubagents_Browser|TestDeepAdapterBuildConfiguredSubagents_Browser' \
  -count=1 -v
```

预期：全部 PASS。

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_config.go \
  internal/swarm/server/adapter/code_adapter.go \
  internal/swarm/server/adapter/code_adapter_test.go \
  internal/swarm/server/adapter/deep_adapter_subagents_test.go
git commit -m "fix: code 模式接入 browser 子代理"
```

---

### Task 2: code_agent 迭代上限按配置回退

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go:786-789`、`:832-840`
- Test: `internal/swarm/server/adapter/code_adapter_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/swarm/server/adapter/code_adapter_test.go` 末尾添加：

```go
// TestCodeAdapterBuildConfiguredSubagents_CodeAgentMaxIterations 验证 code_agent
// 迭代上限按 code_agent.max_iterations → react.max_iterations → 15 回退。
func TestCodeAdapterBuildConfiguredSubagents_CodeAgentMaxIterations(t *testing.T) {
	tests := []struct {
		name     string
		reactMax int
		codeCfg  map[string]any
		want     int
	}{
		{
			name:     "子段优先",
			reactMax: 20,
			codeCfg:  map[string]any{"enabled": true, "max_iterations": 7},
			want:     7,
		},
		{
			name:     "回退react",
			reactMax: 20,
			codeCfg:  map[string]any{"enabled": true},
			want:     20,
		},
		{
			name:     "回退默认15",
			reactMax: 0,
			codeCfg:  map[string]any{"enabled": true},
			want:     15,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCodeAdapterWithModel(t)
			config := map[string]any{
				"subagents": map[string]any{"code_agent": tt.codeCfg},
			}
			if tt.reactMax > 0 {
				config["max_iterations"] = tt.reactMax
			}

			specs, _ := c.buildConfiguredSubagents(config, map[string]any{})
			code := findSubagentSpecByName(specs, subagents.CodeAgentFactoryName)
			if code == nil {
				t.Fatal("code_agent 未装配")
			}
			if code.MaxIterations != tt.want {
				t.Errorf("code_agent MaxIterations = %d, want %d", code.MaxIterations, tt.want)
			}
		})
	}
}

// TestCodeAdapterBuildConfiguredSubagents_ResearchAgent保留 锁定 Go 有意保留的
// code 模式 research_agent（Python interface_code.py:659-757 未注册该子代理）。
func TestCodeAdapterBuildConfiguredSubagents_ResearchAgent保留(t *testing.T) {
	c := newCodeAdapterWithModel(t)
	specs, _ := c.buildConfiguredSubagents(map[string]any{
		"subagents": map[string]any{
			"research_agent": map[string]any{"enabled": true},
		},
	}, map[string]any{})

	if got := findSubagentSpecByName(specs, subagents.ResearchAgentFactoryName); got == nil {
		t.Fatal("Go 有意保留的 code 模式 research_agent 不应被移除")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ \
  -run '^TestCodeAdapterBuildConfiguredSubagents_CodeAgentMaxIterations$' -count=1 -v
```

预期：FAIL；`子段优先` 报 `MaxIterations = 15, want 7`，`回退react` 报 `15, want 20`。

- [ ] **Step 3: 实现两级回退**

在 `internal/swarm/server/adapter/code_adapter.go` 的：

```go
	exploreAgentCfg, _ := subagentsCfg["explore_agent"].(map[string]any)
	planAgentCfg, _ := subagentsCfg["plan_agent"].(map[string]any)
```

下方补充：

```go
	codeAgentCfg, _ := subagentsCfg["code_agent"].(map[string]any)
```

把 `code_agent` 的：

```go
			MaxIterations: 15,
```

替换为：

```go
			// Python: interface_code.py:718-722
			// parse_int(code_agent_cfg.get("max_iterations"), react_cfg.get("max_iterations", 15))
			MaxIterations: parseIntAny(codeAgentCfg["max_iterations"], defaultMaxIterations),
```

- [ ] **Step 4: 给 research_agent 加有意偏差注释**

在 `internal/swarm/server/adapter/code_adapter.go` 的 research 分支前补充：

```go
	// ── 按配置启用：research_agent ──
	// 有意的 Go 额外能力：Python code 模式（interface_code.py:659-757）不注册 research_agent。
	// 测试 TestCodeAdapterBuildConfiguredSubagents_ResearchAgent保留 锁定该行为。
```

- [ ] **Step 5: 运行测试确认通过**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ \
  -run 'TestCodeAdapterBuildConfiguredSubagents' -count=1 -v
```

预期：全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/adapter/code_adapter.go \
  internal/swarm/server/adapter/code_adapter_test.go
git commit -m "fix: code_agent 迭代上限按配置回退"
```

---

### Task 3: AgentManager 重载错误立即返回

**Files:**
- Modify: `internal/swarm/server/runtime/agent_manager.go:392-402`
- Test: `internal/swarm/server/runtime/agent_manager_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/swarm/server/runtime/agent_manager_test.go` 的导入块补充：

```go
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
```

在「非导出函数」区块添加：

```go
// fakeReloadAdapter 仅覆盖 ReloadAgentConfig 的测试适配器。
type fakeReloadAdapter struct {
	adapter.AgentAdapter
	err error
}

// ReloadAgentConfig 返回预设错误。
func (f *fakeReloadAdapter) ReloadAgentConfig(context.Context, map[string]any, map[string]any) error {
	return f.err
}
```

在文件末尾添加：

```go
// TestReloadAgentsConfig_单Agent失败返回错误 验证单 agent 重载失败会向上传播，
// 而不是 Warn 后继续（对齐 Python agent_manager.py:308-340）。
func TestReloadAgentsConfig_单Agent失败返回错误(t *testing.T) {
	am := NewAgentManager()
	uc := NewUapClaw()
	uc.adapter = &fakeReloadAdapter{err: errors.New("reload boom")}
	am.PreSetAgent("web", "agent.plan", uc, "agent", "plan", "")

	err := am.ReloadAgentsConfig(context.Background(), map[string]any{}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reload boom")
	assert.Contains(t, err.Error(), "web")
	assert.Contains(t, err.Error(), "agent.plan")
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/runtime/ \
  -run '^TestReloadAgentsConfig_单Agent失败返回错误$' -count=1 -v
```

预期：FAIL，报 `An error is expected but got nil`。

- [ ] **Step 3: 实现错误传播**

在 `internal/swarm/server/runtime/agent_manager.go` 中，把：

```go
			if err := entry.agent.ReloadAgentConfig(configPayload, envAny); err != nil {
				// Python: 不中断，仅 warn
				logger.Warn(logComponent).
					Err(err).
					Str("channel_id", chKey).
					Str("cache_key", entry.cacheKey).
					Msg("[AgentManager] agent reload_agent_config failed")
			}
```

替换为：

```go
			if err := entry.agent.ReloadAgentConfig(configPayload, envAny); err != nil {
				// Python: agent_manager.py:327-330 直接 await，异常向外传播并中断后续 agent。
				logger.Warn(logComponent).
					Err(err).
					Str("channel_id", chKey).
					Str("cache_key", entry.cacheKey).
					Msg("[AgentManager] agent reload_agent_config failed")
				return fmt.Errorf("channel %s cache %s reload agent config failed: %w", chKey, entry.cacheKey, err)
			}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/runtime/ \
  -run 'TestReloadAgentsConfig' -count=1 -v
```

预期：全部 PASS，包含原有的空 manager 用例。

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/agent_manager.go \
  internal/swarm/server/runtime/agent_manager_test.go
git commit -m "fix: AgentManager 重载错误立即返回"
```

---

### Task 4: agents 写操作同步重载

**Files:**
- Modify: `internal/swarm/server/handle_agents.go:133-138`（新增 helper）
- Modify: `internal/swarm/server/handle_agents.go:209-286`（create）
- Modify: `internal/swarm/server/handle_agents.go:288-355`（update）
- Modify: `internal/swarm/server/handle_agents.go:357-411`（delete）
- Modify: `internal/swarm/server/handle_agents.go:413-503`（enable/disable）
- Test: `internal/swarm/server/handle_agents_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/swarm/server/handle_agents_test.go` 的导入块替换为：

```go
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)
```

在「非导出函数」区块添加：

```go
// newAgentsTestServer 构造隔离配置目录的 AgentServer。
// 必须在 t.Setenv 之后再创建 config 实例，避免 ConfigFile() 固化真实 HOME 路径。
func newAgentsTestServer(t *testing.T) *AgentServer {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", tmpDir)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)

	if err := os.MkdirAll(filepath.Join(tmpDir, "config"), 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		t.Fatalf("创建配置实例失败: %v", err)
	}

	s, _ := newTestServer()
	s.config = cfg
	s.agentConfigService = runtime.NewAgentConfigService(tmpDir)
	s.agentManager = runtime.NewAgentManager()
	return s
}

// newAgentsTestRequest 构造 agents.* 请求。
func newAgentsTestRequest(t *testing.T, method schema.ReqMethod, params map[string]any) *schema.AgentRequest {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("序列化参数失败: %v", err)
	}
	return schema.NewAgentRequest("req-"+string(method), "web", method, raw)
}

// assertAgentsApplied 断言 payload.applied 与 reload_error。
func assertAgentsApplied(t *testing.T, resp *schema.AgentResponse, want bool) {
	t.Helper()
	got, ok := resp.Payload["applied"].(bool)
	if !ok {
		t.Fatalf("payload.applied 类型错误: %#v", resp.Payload["applied"])
	}
	if got != want {
		t.Errorf("payload.applied = %v, want %v", got, want)
	}
	if want {
		if reloadErr, _ := resp.Payload["reload_error"].(string); reloadErr != "" {
			t.Errorf("payload.reload_error = %q, want empty", reloadErr)
		}
	}
}

// TestHandleAgents_写操作同步重载 覆盖 create → update → disable → delete 链路。
func TestHandleAgents_写操作同步重载(t *testing.T) {
	s := newAgentsTestServer(t)
	ctx := context.Background()

	createResp, err := s.handleAgentsCreate(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsCreate, map[string]any{
		"name":        "test-agent",
		"description": "测试 agent",
		"prompt":      "你是测试 agent",
		"location":    "project",
	}))
	if err != nil {
		t.Fatalf("agents.create 返回错误: %v", err)
	}
	if !createResp.OK {
		t.Fatalf("agents.create 失败: %#v", createResp.Payload)
	}
	assertAgentsApplied(t, createResp, true)

	updateResp, err := s.handleAgentsUpdate(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsUpdate, map[string]any{
		"name":        "test-agent",
		"description": "更新后的描述",
	}))
	if err != nil {
		t.Fatalf("agents.update 返回错误: %v", err)
	}
	if !updateResp.OK {
		t.Fatalf("agents.update 失败: %#v", updateResp.Payload)
	}
	assertAgentsApplied(t, updateResp, true)
	if _, ok := updateResp.Payload["reload_error"]; !ok {
		t.Error("agents.update payload 应包含 reload_error")
	}

	disableResp, err := s.handleAgentsDisable(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsDisable, map[string]any{
		"name": "test-agent",
	}))
	if err != nil {
		t.Fatalf("agents.disable 返回错误: %v", err)
	}
	if !disableResp.OK {
		t.Fatalf("agents.disable 失败: %#v", disableResp.Payload)
	}
	assertAgentsApplied(t, disableResp, true)

	deleteResp, err := s.handleAgentsDelete(ctx, newAgentsTestRequest(t, schema.ReqMethodAgentsDelete, map[string]any{
		"name": "test-agent",
	}))
	if err != nil {
		t.Fatalf("agents.delete 返回错误: %v", err)
	}
	if !deleteResp.OK {
		t.Fatalf("agents.delete 失败: %#v", deleteResp.Payload)
	}
	assertAgentsApplied(t, deleteResp, true)
}

// TestHandleAgentsCreate_重载失败回填 验证配置写成功后重载失败会回填 applied=false。
func TestHandleAgentsCreate_重载失败回填(t *testing.T) {
	s := newAgentsTestServer(t)
	s.agentManager = nil

	resp, err := s.handleAgentsCreate(context.Background(), newAgentsTestRequest(t, schema.ReqMethodAgentsCreate, map[string]any{
		"name":        "test-agent",
		"description": "测试 agent",
		"prompt":      "你是测试 agent",
		"location":    "project",
	}))
	if err != nil {
		t.Fatalf("agents.create 返回错误: %v", err)
	}
	assertAgentsApplied(t, resp, false)
	if reloadErr, _ := resp.Payload["reload_error"].(string); reloadErr == "" {
		t.Error("重载失败时 payload.reload_error 不应为空")
	}
}

// TestReloadAgentsConfig_未初始化返回错误 覆盖 helper 的两个 nil 守卫。
func TestReloadAgentsConfig_未初始化返回错误(t *testing.T) {
	s := &AgentServer{}
	if err := s.reloadAgentsConfig(context.Background()); err == nil {
		t.Fatal("agentManager 为 nil 时应返回错误")
	}

	s.agentManager = runtime.NewAgentManager()
	if err := s.reloadAgentsConfig(context.Background()); err == nil {
		t.Fatal("config 为 nil 时应返回错误")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/ \
  -run 'TestHandleAgents_写操作同步重载|TestHandleAgentsCreate_重载失败回填|TestReloadAgentsConfig_未初始化返回错误' \
  -count=1 -v
```

预期：编译失败，`s.reloadAgentsConfig undefined`（helper 尚未实现）。

- [ ] **Step 3: 新增 reloadAgentsConfig helper**

在 `internal/swarm/server/handle_agents.go` 的 `getAgentConfigService` 之后添加：

```go
// reloadAgentsConfig 同步重载所有 Agent 配置。
// Python: await self._agent_manager.reload_agents_config(get_config(), None)
func (s *AgentServer) reloadAgentsConfig(ctx context.Context) error {
	if s.agentManager == nil {
		return fmt.Errorf("agentManager 未初始化")
	}
	if s.config == nil {
		return fmt.Errorf("配置未初始化")
	}
	configData, err := s.config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}
	return s.agentManager.ReloadAgentsConfig(ctx, configData, nil)
}
```

- [ ] **Step 4: create 接线**

把 `handleAgentsCreate` 中的：

```go
	applied := true
	reloadError := ""
	if err := runtime.UpsertSubagentInConfig(agent.Name, true); err != nil {
		applied = false
		reloadError = err.Error()
		logger.Warn(logComponent).Err(err).Str("agent_name", agent.Name).Msg("agents.create update config failed")
	}
```

替换为：

```go
	applied := true
	reloadError := ""
	if err := runtime.UpsertSubagentInConfig(agent.Name, true); err != nil {
		applied = false
		reloadError = err.Error()
		logger.Warn(logComponent).Err(err).Str("agent_name", agent.Name).Msg("agents.create update config failed")
	} else if reloadErr := s.reloadAgentsConfig(ctx); reloadErr != nil {
		applied = false
		reloadError = reloadErr.Error()
		logger.Warn(logComponent).Err(reloadErr).Str("agent_name", agent.Name).Msg("agents.create reload failed")
	}
```

- [ ] **Step 5: update 接线并补响应字段**

在 `handleAgentsUpdate` 的 `UpdateAgent` 成功分支之后添加：

```go
	applied := true
	reloadError := ""
	if reloadErr := s.reloadAgentsConfig(ctx); reloadErr != nil {
		applied = false
		reloadError = reloadErr.Error()
		logger.Warn(logComponent).Err(reloadErr).Str("agent_name", name).Msg("agents.update reload failed")
	}
```

把该函数的返回 payload 从：

```go
		schema.WithPayload(map[string]any{
			"agent":     agentDefinitionToMap(agent),
			"generated": generated,
		}),
```

改为：

```go
		schema.WithPayload(map[string]any{
			"agent":        agentDefinitionToMap(agent),
			"generated":    generated,
			"applied":      applied,
			"reload_error": reloadError,
		}),
```

- [ ] **Step 6: delete 接线**

把 `handleAgentsDelete` 的签名：

```go
func (s *AgentServer) handleAgentsDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
```

改为：

```go
func (s *AgentServer) handleAgentsDelete(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
```

把：

```go
	applied := true
	reloadError := ""
	if _, rmErr := runtime.RemoveSubagentFromConfig(name); rmErr != nil {
		applied = false
		reloadError = rmErr.Error()
		logger.Warn(logComponent).Err(rmErr).Str("agent_name", name).Msg("agents.delete delete config failed")
	}
```

替换为：

```go
	applied := true
	reloadError := ""
	if _, rmErr := runtime.RemoveSubagentFromConfig(name); rmErr != nil {
		applied = false
		reloadError = rmErr.Error()
		logger.Warn(logComponent).Err(rmErr).Str("agent_name", name).Msg("agents.delete delete config failed")
	} else if reloadErr := s.reloadAgentsConfig(ctx); reloadErr != nil {
		applied = false
		reloadError = reloadErr.Error()
		logger.Warn(logComponent).Err(reloadErr).Str("agent_name", name).Msg("agents.delete reload failed")
	}
```

- [ ] **Step 7: enable/disable 接线**

把两个入口：

```go
func (s *AgentServer) handleAgentsEnable(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleAgentsSetEnabled(request, true)
}

func (s *AgentServer) handleAgentsDisable(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleAgentsSetEnabled(request, false)
}
```

改为：

```go
func (s *AgentServer) handleAgentsEnable(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleAgentsSetEnabled(ctx, request, true)
}

func (s *AgentServer) handleAgentsDisable(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return s.handleAgentsSetEnabled(ctx, request, false)
}
```

把 `handleAgentsSetEnabled` 的签名：

```go
func (s *AgentServer) handleAgentsSetEnabled(request *schema.AgentRequest, enabled bool) (*schema.AgentResponse, error) {
```

改为：

```go
func (s *AgentServer) handleAgentsSetEnabled(ctx context.Context, request *schema.AgentRequest, enabled bool) (*schema.AgentResponse, error) {
```

把：

```go
	applied := true
	reloadError := ""
	if err := runtime.UpsertSubagentInConfig(name, enabled); err != nil {
		applied = false
		reloadError = err.Error()
		logger.Warn(logComponent).Err(err).Str("agent_name", name).Bool("enabled", enabled).Msg("agents set enabled state failed")
	}
```

替换为：

```go
	applied := true
	reloadError := ""
	if err := runtime.UpsertSubagentInConfig(name, enabled); err != nil {
		applied = false
		reloadError = err.Error()
		logger.Warn(logComponent).Err(err).Str("agent_name", name).Bool("enabled", enabled).Msg("agents set enabled state failed")
	} else if reloadErr := s.reloadAgentsConfig(ctx); reloadErr != nil {
		applied = false
		reloadError = reloadErr.Error()
		logger.Warn(logComponent).Err(reloadErr).Str("agent_name", name).Bool("enabled", enabled).Msg("agents.set_enabled reload failed")
	}
```

- [ ] **Step 8: 运行测试确认通过**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/ \
  -run 'TestHandleAgents' -count=1 -v
```

预期：全部 PASS，包含原有 `TestHandleAgentsList/Get/Create/ToolsList` 与新增用例。

- [ ] **Step 9: 提交**

```bash
git add internal/swarm/server/handle_agents.go \
  internal/swarm/server/handle_agents_test.go
git commit -m "fix: agents 写操作同步重载配置"
```

---

### Task 5: 配置模板注释与全量验证

**Files:**
- Modify: `resources/config.yaml:169-178`
- Verify: 全部改动文件

- [ ] **Step 1: 补配置模板注释**

把 `resources/config.yaml` 的：

```yaml
  # 子代理开关：仅显式 enabled: true 才启用（adapter/deep_adapter_config.go:175-178）
  subagents:
```

替换为：

```yaml
  # 子代理开关：仅显式 enabled: true 才启用（adapter/deep_adapter_config.go:175-178）
  # code 模式：explore_agent / plan_agent 恒挂载，enabled 不生效，仅 max_iterations 生效；
  #            code_agent / research_agent 按 enabled 启用。
  # browser_agent：由 PLAYWRIGHT_RUNTIME_MCP_ENABLED / BROWSER_RUNTIME_MCP_ENABLED 控制，
  #                本段只覆盖 max_iterations 等参数。
  subagents:
```

- [ ] **Step 2: 格式化检查**

```bash
$(go env GOROOT)/bin/gofmt -l \
  internal/swarm/server/adapter/deep_adapter_config.go \
  internal/swarm/server/adapter/code_adapter.go \
  internal/swarm/server/runtime/agent_manager.go \
  internal/swarm/server/handle_agents.go \
  internal/swarm/server/adapter/code_adapter_test.go \
  internal/swarm/server/adapter/deep_adapter_subagents_test.go \
  internal/swarm/server/runtime/agent_manager_test.go \
  internal/swarm/server/handle_agents_test.go
```

预期：无输出。若有输出，执行 `$(go env GOROOT)/bin/gofmt -w <文件>` 后重跑。

- [ ] **Step 3: go vet 与 lint**

```bash
go vet -tags "sqlite_fts5 test" ./internal/swarm/server/...
```

预期：无输出、退出码 0。

```bash
if command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint run ./internal/swarm/server/...
else
  echo "golangci-lint 未安装，跳过（CI Lint job 会覆盖）"
fi
```

预期：已安装时无新增问题；未安装时打印跳过提示。

- [ ] **Step 4: 运行 server 子树全量测试**

```bash
go test -tags "sqlite_fts5 test" ./internal/swarm/server/... -count=1
```

预期：全部 PASS。

- [ ] **Step 5: 改动行覆盖率**

```bash
go test -tags "sqlite_fts5 test" -coverprofile=/tmp/subagent.cover ./internal/swarm/server/... -count=1
./scripts/check-changed-line-coverage.sh /tmp/subagent.cover 8532b4e2 85
```

预期：`changed line coverage ... >= 85%`，退出码 0。若脚本提示基准提交不存在，改用：

```bash
./scripts/check-changed-line-coverage.sh /tmp/subagent.cover "$(git merge-base origin/main HEAD)" 85
```

- [ ] **Step 6: 提交**

```bash
git add resources/config.yaml
git commit -m "docs: 补充 subagent 配置模板注释"
```

---

## 验收清单

- [ ] code 模式 browser runtime 开启时产出 `browser_agent`，关闭时不出。
- [ ] `BROWSER_DRIVER` 与 `BROWSER_MANAGED_BINARY` 的代码模式默认值注入生效。
- [ ] `code_agent.max_iterations` 按子段 → `react.max_iterations` → 15 回退。
- [ ] code 模式 `research_agent` 仍可用，并有测试锁定。
- [ ] `AgentManager.ReloadAgentsConfig` 单 agent 失败时返回带 channel/cache_key 的错误。
- [ ] `agents.create/update/delete/enable/disable` 均在配置写成功后同步重载。
- [ ] `agents.update` 响应包含 `applied` 与 `reload_error`。
- [ ] 重载失败时 `applied=false` 且 `reload_error` 非空。
- [ ] `gofmt`、`go vet`、server 子树测试、改动行覆盖率 85% 全部通过。
