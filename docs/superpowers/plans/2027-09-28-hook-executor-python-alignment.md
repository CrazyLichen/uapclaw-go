# HookExecutor 对齐 Python 无参构造 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 HookExecutor 和 UserHookRail 的构造方式对齐 Python 无参构造，通过全局 Config 注册点替代显式 LLMConfig 注入。

**Architecture:** 在 hooks 包内新增全局 `*config.Config` 注册点（`RegisterConfig`），`HookExecutor.queryLLM` 运行时从全局配置读取 LLM 参数，等价 Python `get_config()`。移除 `LLMConfig` 结构体、`extractLLMConfig`/`strFromMap` 辅助函数。`NewUserHookRail` 简化为只接收 `HooksConfig` 单参数。

**Tech Stack:** Go 1.x, 项目内 `config.Config` 单例模式

---

### Task 1: 修改 executor.go — 移除 LLMConfig，新增全局 Config 注册

**Files:**
- Modify: `internal/swarm/server/hooks/executor.go`

- [ ] **Step 1: 移除 LLMConfig 结构体**

删除 `executor.go` 中 LLMConfig 结构体定义（L38-49）：

```go
// 删除以下代码：
// LLMConfig prompt hook 使用的 LLM 配置
// 对齐 Python _query_llm 中从 config 提取的 APIKey/APIBase/ClientProvider/DefaultModel
type LLMConfig struct {
	// APIKey LLM API 密钥
	APIKey string
	// APIBase LLM API 地址
	APIBase string
	// ClientProvider LLM 客户端提供者
	ClientProvider string
	// DefaultModel 默认模型名
	DefaultModel string
}
```

- [ ] **Step 2: 移除 HookExecutor.llmConfig 字段**

将 HookExecutor 结构体改为空体：

```go
// HookExecutor hook 执行器，对齐 Python HookExecutor
// 统一调度 command/prompt 两类 hook，返回 HookResult 列表
// LLM 配置在 queryLLM 调用时从全局 Config 读取，对齐 Python _query_llm 中动态 get_config()
type HookExecutor struct{}
```

- [ ] **Step 3: 添加全局 Config 注册机制**

在 `全局变量` 区块新增全局 Config 变量和注册函数。在 `导入函数` 区块新增 `RegisterConfig`。在 `非导出函数` 区块新增 `getGlobalConfig`：

```go
// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// globalCfg 全局 Config 实例，由 AgentServer 启动时通过 RegisterConfig 注册
	// 对齐 Python: from jiuwenswarm.common.config import get_config
	globalCfg  *config.Config
	globalCfgMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterConfig 注册全局 Config 实例，对齐 Python get_config() 的全局可用性。
// 必须在首次创建 HookExecutor 之前调用（通常由 AgentServer 启动时注册）。
func RegisterConfig(cfg *config.Config) {
	globalCfgMu.Lock()
	defer globalCfgMu.Unlock()
	globalCfg = cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getGlobalConfig 获取全局 Config 实例
func getGlobalConfig() *config.Config {
	globalCfgMu.RLock()
	defer globalCfgMu.RUnlock()
	return globalCfg
}
```

同时在 import 中新增：

```go
"github.com/uapclaw/uapclaw-go/internal/common/config"
```

- [ ] **Step 4: 修改 NewHookExecutor 为无参构造**

```go
// NewHookExecutor 创建 HookExecutor，对齐 Python HookExecutor()（无参构造）
func NewHookExecutor() *HookExecutor {
	return &HookExecutor{}
}
```

- [ ] **Step 5: 重写 queryLLM 方法**

将 queryLLM 方法改为从全局 Config 读取 LLM 参数，对齐 Python `_query_llm` 中 `from jiuwenswarm.common.config import get_config; config_base = get_config()` 的行为：

```go
// queryLLM 调用 LLM 执行 hook 审查，对齐 Python _query_llm
// 运行时从全局 Config 读取 LLM 配置，对齐 Python: config_base = get_config()
// 集成测试覆盖：由 //go:build llm 标签的 executor_llm_test.go 覆盖，不纳入单元测试覆盖率基线
func (e *HookExecutor) queryLLM(ctx context.Context, prompt, modelName string) (string, error) {
	// 对齐 Python: config_base = get_config()
	cfg := getGlobalConfig()
	if cfg == nil {
		return "", fmt.Errorf("全局 Config 未注册，请先调用 RegisterConfig")
	}
	configBase, err := cfg.Load()
	if err != nil {
		return "", fmt.Errorf("读取配置失败: %w", err)
	}

	// 对齐 Python: models_cfg = config_base.get("models", {})
	modelsCfg, _ := configBase["models"].(map[string]any)
	defaultCfg, _ := modelsCfg["default"].(map[string]any)
	clientCfg, _ := defaultCfg["model_client_config"].(map[string]any)
	apiKey, _ := clientCfg["api_key"].(string)
	apiBase, _ := clientCfg["api_base"].(string)
	clientProvider, _ := clientCfg["client_provider"].(string)
	defaultModel, _ := clientCfg["model_name"].(string)

	clientConfig, cfgErr := llmschema.NewModelClientConfig(clientProvider, apiKey, apiBase)
	if cfgErr != nil {
		return "", fmt.Errorf("创建 ModelClientConfig 失败: %w", cfgErr)
	}
	model, modelErr := llm.NewModel(clientConfig, nil)
	if modelErr != nil {
		return "", fmt.Errorf("创建 Model 失败: %w", modelErr)
	}

	// 对齐 Python: model = model_name or default_model
	effectiveModel := modelName
	if effectiveModel == "" {
		effectiveModel = defaultModel
	}

	// 对齐 Python: response = await model.invoke(messages=[{"role": "user", "content": prompt}], temperature=0.0, max_tokens=1024, model=model_name)
	messages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))
	opts := []model_clients.InvokeOption{
		model_clients.WithInvokeTemperature(0.0),
		model_clients.WithInvokeMaxTokens(1024),
		model_clients.WithInvokeModel(effectiveModel),
	}

	response, invokeErr := model.Invoke(ctx, messages, opts...)
	if invokeErr != nil {
		return "", fmt.Errorf("LLM Invoke 失败: %w", invokeErr)
	}

	// 对齐 Python: content = response.content
	content := response.Content
	if content.IsText() {
		return content.Text(), nil
	}

	// 多模态 → 拼接文本部分
	var textParts []string
	for _, part := range content.Parts() {
		if part.Type == "text" && part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}
	return strings.Join(textParts, "\n"), nil
}
```

- [ ] **Step 6: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/hooks/`
Expected: 编译成功（测试文件中引用 LLMConfig 的地方会暂时报错，下一步修复）

---

### Task 2: 修改 user_hook_rail.go — 简化 NewUserHookRail 签名

**Files:**
- Modify: `internal/swarm/server/hooks/user_hook_rail.go`

- [ ] **Step 1: 修改 NewUserHookRail 签名**

将双参数改为单参数，内部无参创建 HookExecutor：

```go
// NewUserHookRail 创建 UserHookRail，对齐 Python UserHookRail(hooks_config)
func NewUserHookRail(config hookscfg.HooksConfig) *UserHookRail {
	r := &UserHookRail{
		config:   config,
		executor: NewHookExecutor(),
	}
	// 对齐 Python: priority=60
	base := agentinterfaces.NewBaseRail().WithPriority(60)
	r.DeepAgentRail = rails.DeepAgentRail{BaseRail: *base}
	return r
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/hooks/`
Expected: 编译失败（测试文件引用旧签名），下一步修复

---

### Task 3: 修改测试文件 — 适配无参构造

**Files:**
- Modify: `internal/swarm/server/hooks/executor_test.go`
- Modify: `internal/swarm/server/hooks/executor_llm_test.go`
- Modify: `internal/swarm/server/hooks/user_hook_rail_test.go`

- [ ] **Step 1: 修改 executor_test.go**

1. 删除 `TestLLMConfig_字段` 测试函数（LLMConfig 已不存在）
2. 全局替换 `NewHookExecutor(LLMConfig{})` → `NewHookExecutor()`
3. 新增 `TestRegisterConfig` 测试验证全局注册机制：

```go
// TestRegisterConfig_注册和获取 测试全局 Config 注册与获取
func TestRegisterConfig_注册和获取(t *testing.T) {
	// 保存原始值
	globalCfgMu.Lock()
	orig := globalCfg
	globalCfg = nil
	globalCfgMu.Unlock()
	defer func() {
		RegisterConfig(orig)
	}()

	// 未注册时应返回 nil
	if getGlobalConfig() != nil {
		t.Error("getGlobalConfig() 应返回 nil（未注册时）")
	}

	// 注册后应返回非 nil
	RegisterConfig(config.New(""))
	if getGlobalConfig() == nil {
		t.Error("getGlobalConfig() 应返回非 nil（注册后）")
	}
}
```

在 executor_test.go 的 import 中新增 `"github.com/uapclaw/uapclaw-go/internal/common/config"`

- [ ] **Step 2: 修改 executor_llm_test.go**

全局替换 `NewHookExecutor(cfg)` → `NewHookExecutor()`，删除 `LLMConfig` 变量：

```go
//go:build llm

package hooks

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// TestHookExecutor_queryLLM_真实调用 测试 queryLLM 真实 LLM 调用
// 运行方式: go test -tags=llm ./internal/swarm/server/hooks/... -v -run TestHookExecutor_queryLLM
// 需要: 真实 API Key 和网络连接
func TestHookExecutor_queryLLM_真实调用(t *testing.T) {
	// 确保全局 Config 已注册
	if getGlobalConfig() == nil {
		RegisterConfig(config.New(""))
	}
	exec := NewHookExecutor()
	_, err := exec.queryLLM(context.Background(), "test prompt", "test-model")
	if err != nil {
		t.Logf("queryLLM failed (expected without real API key): %v", err)
	}
}

// TestHookExecutor_runPromptHook_真实LLM 测试 runPromptHook 真实 LLM 调用
// 运行方式: go test -tags=llm ./internal/swarm/server/hooks/... -v -run TestHookExecutor_runPromptHook
func TestHookExecutor_runPromptHook_真实LLM(t *testing.T) {
	if getGlobalConfig() == nil {
		RegisterConfig(config.New(""))
	}
	exec := NewHookExecutor()
	result := exec.runPromptHook(context.Background(), map[string]any{
		"type":    "prompt",
		"prompt":  "Is this safe?",
		"timeout": 10,
	}, map[string]any{"tool_name": "test"})
	t.Logf("runPromptHook result: outcome=%q, error=%q", result.Outcome, result.Error)
}
```

- [ ] **Step 3: 修改 user_hook_rail_test.go**

1. 删除所有 `exec := NewHookExecutor(LLMConfig{})` 行
2. 将所有 `NewUserHookRail(cfg, exec)` → `NewUserHookRail(cfg)`
3. 移除 import 中不再需要的 LLMConfig 相关导入（如有）

- [ ] **Step 4: 验证编译和测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -count=1 ./internal/swarm/server/hooks/... -v -run "TestRegisterConfig|TestHookOutcome|TestParseCommandOutput|TestExtractJSONFromResponse|TestHookExecutor_RunAll|TestNewUserHookRail|TestUserHookRail" 2>&1 | tail -30`
Expected: 所有测试 PASS

---

### Task 4: 简化 adapter 层 — 移除 extractLLMConfig 和手动构造

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 简化 deep_adapter_rails.go 中的 UserHookRail 注册**

将步骤 21 的注册代码简化（L180-198）：

```go
	// 步骤 21: userHookRail — 用户配置的 hooks，对齐 Python interface_deep.py L2200-2211
	// 对齐 Python: try/except 包裹注册流程，失败时 warning 并继续
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("UserHookRail 加载失败，跳过")
			}
		}()
		hooksCfg := hookscfg.LoadHooksConfig(configBase)
		if len(hooksCfg.Events) > 0 {
			userHookRail := serverhooks.NewUserHookRail(*hooksCfg)
			railsList = append(railsList, userHookRail)
			logger.Info(logComponent).Int("event_types", len(hooksCfg.Events)).Msg("UserHookRail 加载完成")
		}
	}()
```

- [ ] **Step 2: 删除 deep_adapter_rails.go 中的 extractLLMConfig 和 strFromMap**

删除 L693-719 的 `extractLLMConfig` 和 `strFromMap` 函数。

- [ ] **Step 3: 简化 code_adapter.go 中的 UserHookRail 注册**

将 L848-865 的注册代码简化：

```go
	// ─── UserHookRail — 用户配置的 hooks ───
	// 对齐 Python: try/except 包裹注册流程，失败时 warning 并继续
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Warn(logComponent).Any("panic", r).
					Msg("UserHookRail 加载失败，跳过")
			}
		}()
		hooksCfg := hookscfg.LoadHooksConfig(configBase)
		if len(hooksCfg.Events) > 0 {
			userHookRail := serverhooks.NewUserHookRail(*hooksCfg)
			railsList = append(railsList, userHookRail)
			logger.Info(logComponent).Int("event_types", len(hooksCfg.Events)).Msg("UserHookRail 加载完成")
		}
	}()
```

- [ ] **Step 4: 清理 adapter 包中不再使用的 import**

检查 `deep_adapter_rails.go` 和 `code_adapter.go` 的 import，移除不再需要的 `serverhooks` 相关导入（注意 `serverhooks.NewUserHookRail` 仍在用，保留 `serverhooks` 导入）。

- [ ] **Step 5: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/`
Expected: 编译成功

---

### Task 5: AgentServer 启动时注册全局 Config

**Files:**
- Modify: `internal/swarm/server/agent_server.go`

- [ ] **Step 1: 在 Start 方法中注册 Config**

在 `Start` 方法中、启动 goroutine 之前，添加 `serverhooks.RegisterConfig(s.config)`：

```go
func (s *AgentServer) Start(ctx context.Context) error {
	s.runningMu.Lock()
	if s.running {
		s.runningMu.Unlock()
		logger.Warn(logComponent).Msg("AgentServer 已在运行中，跳过重复启动")
		return nil
	}
	s.running = true
	s.runningMu.Unlock()

	// 注册全局 Config，供 HookExecutor.queryLLM 运行时读取
	// 对齐 Python: from jiuwenswarm.common.config import get_config
	serverhooks.RegisterConfig(s.config)

	ctx, s.cancel = context.WithCancel(ctx)
	go func() {
		err := s.run(ctx)
		...
	}()
	return nil
}
```

在 import 中新增：

```go
serverhooks "github.com/uapclaw/uapclaw-go/internal/swarm/server/hooks"
```

注意：`agent_server.go` 可能已有 `serverhooks` 的间接引用，先检查是否需要新增 import。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/`
Expected: 编译成功

---

### Task 6: 全量编译和测试验证

**Files:**
- 无新增修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 2: hooks 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -count=1 ./internal/swarm/server/hooks/... -v 2>&1 | tail -40`
Expected: 所有测试 PASS

- [ ] **Step 3: adapter 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -count=1 ./internal/swarm/server/adapter/... -v 2>&1 | tail -40`
Expected: 所有测试 PASS

- [ ] **Step 4: server 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -count=1 ./internal/swarm/server/... -v 2>&1 | tail -40`
Expected: 所有测试 PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "refactor(hooks): 对齐 Python HookExecutor 无参构造，新增 RegisterConfig 全局注册点

- 移除 LLMConfig 结构体和 HookExecutor.llmConfig 字段
- NewHookExecutor() 无参构造，对齐 Python HookExecutor()
- queryLLM 运行时从全局 Config 读取 LLM 参数，对齐 Python get_config()
- NewUserHookRail(hooksCfg) 单参数，对齐 Python UserHookRail(hooks_config)
- 移除 extractLLMConfig/strFromMap 辅助函数
- AgentServer.Start() 注册全局 Config
- 适配所有测试文件"
```
