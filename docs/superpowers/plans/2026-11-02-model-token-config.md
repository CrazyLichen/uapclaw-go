# 模型 Token 配置可配化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `models.defaults[].model_client_config.max_input_tokens` / `max_output_tokens` 可在前端配置、在 config.yaml 手改生效，并真实驱动上下文窗口百分比、压缩触发阈值与请求 `max_tokens`。

**Architecture:** 配置走「前端表单 → `models.replace_all` 写 YAML → `models.list` 回填」；运行时由 `deep_adapter.go:deepAgentContextEngineConfig` 把 `max_input_tokens` 收集成 `ContextEngineConfig.ModelContextWindowTokens`（复用 `ResolveContextMax` 既有优先级链），`buildModelFromEntry` 把 `max_output_tokens` 落成 `ModelRequestConfig.MaxTokens`。压缩阈值在 `ContextProcessorRail.Init` 中于 `MergeProcessors` 之后按 `min(绝对, ratio×window)` 收口。

**Tech Stack:** Go 1.22+、`internal/agentcore/*`、`internal/swarm/*`、React + TypeScript（Vite）、`tests/integration`（build tag `integration`）。

**Spec:** `docs/superpowers/specs/2026-11-02-model-token-config-design.md`（决策 D1-D14、改动点 B1-B8/A1-A3/F1-F3/T1/P1-P6）

> **2026-11-03 修订**：Task 6/7 的分子口径由 `input_tokens` 改为 `total_tokens`
> （本轮输出下一轮会重发，占用应含输出）。下方历史代码片段保留原样仅作记录，
> 以设计稿 §6.5 修订说明为准。

**执行注意（项目规则）**
- 所有 Go 注释中文；文件内声明顺序：接口/结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数
- 每个包新增文件必须同步该目录 `doc.go` 的文件目录树
- 编译前 `pgrep -f 'go (build|test)'` 确认无并发编译；`export GOPROXY=https://goproxy.cn,direct`
- 改动某文件后覆盖率门禁看「改动行」85%/5%
- 集成测试改动必须推远端并等 CI 四个 job 全绿才算完成

---

## 文件结构

**修改（Go）**

| 文件 | 职责变化 |
|---|---|
| `internal/agentcore/context_engine/schema/config.go` | 新增两个比例字段 + 校验 |
| `internal/swarm/gateway/channel_manager/web/config_apply.go` | 前端 flat → YAML 嵌套时解析两个 token 字段 |
| `internal/swarm/gateway/channel_manager/web/web_handlers.go` | `models.list` 回填两个字段 |
| `internal/swarm/server/adapter/deep_adapter.go` | 白名单、`MaxTokens` 映射、`deepAgentContextEngineConfig` 收集窗口与比例 |
| `internal/swarm/agents/harness/common/rails/stream_event_emit.go` | `context.usage` 分母取 ceConfig、分子取 `total_tokens`（2026-11-03 修订） |
| `internal/agentcore/context_engine/context/session_model_context.go` | `statMessages` 分子取 `total_tokens`（2026-11-03 修订） |
| `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go` | 新增阈值收口纯函数 + `Init` 接线 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | processor rail 读取 yaml 五段 |
| `resources/config.yaml` | 模板整段对齐 Python + 两个 token 键 + 两个 ratio 键 |

**新建**

| 文件 | 职责 |
|---|---|
| `internal/agentcore/context_engine/schema/config_ratio_test.go` | 比例字段序列化与校验 |
| `internal/agentcore/harness/rails/context_engineer/compression_ratio_test.go` | 阈值收口纯函数表驱动测试 |
| `tests/integration/.../model_token_config_test.go` | 四条集成用例 |

**修改（前端）**：`types/index.ts`、`components/ConfigPanel/index.tsx`、`i18n/locales/{zh,en}.json`

---

## Task 1: ContextEngineConfig 增加压缩比例字段

**Files:**
- Modify: `internal/agentcore/context_engine/schema/config.go:14-74`
- Test: `internal/agentcore/context_engine/schema/config_ratio_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package schema

import (
	"encoding/json"
	"testing"
)

// TestContextEngineConfig_压缩比例字段 验证比例字段的序列化与零值语义
func TestContextEngineConfig_压缩比例字段(t *testing.T) {
	cfg := NewContextEngineConfig()
	if cfg.CompressionTriggerRatio != 0 || cfg.CompressionTargetRatio != 0 {
		t.Fatalf("默认应为 0（不启用比例），实际 trigger=%v target=%v",
			cfg.CompressionTriggerRatio, cfg.CompressionTargetRatio)
	}

	cfg.CompressionTriggerRatio = 0.85
	cfg.CompressionTargetRatio = 0.55
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if raw["compression_trigger_ratio"] != 0.85 {
		t.Errorf("compression_trigger_ratio 应为 0.85，实际 %v", raw["compression_trigger_ratio"])
	}
	if raw["compression_target_ratio"] != 0.55 {
		t.Errorf("compression_target_ratio 应为 0.55，实际 %v", raw["compression_target_ratio"])
	}
}

// TestContextEngineConfigValidate_压缩比例校验 验证比例区间与相对大小校验
func TestContextEngineConfigValidate_压缩比例校验(t *testing.T) {
	cases := []struct {
		name    string
		trigger float64
		target  float64
		wantErr bool
	}{
		{"都为0不启用", 0, 0, false},
		{"正常区间", 0.85, 0.55, false},
		{"target大于trigger", 0.55, 0.85, true},
		{"target等于trigger", 0.6, 0.6, true},
		{"trigger超过1", 1.2, 0.5, true},
		{"target为负", 0.85, -0.1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewContextEngineConfig()
			cfg.CompressionTriggerRatio = tc.trigger
			cfg.CompressionTargetRatio = tc.target
			err := cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v，实际 err=%v", tc.wantErr, err)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/context_engine/schema/ -run '压缩比例' -v`
Expected: FAIL，`cfg.CompressionTriggerRatio undefined (type ContextEngineConfig has no field ...)`

- [ ] **Step 3: 实现字段与校验**

在 `ContextEngineConfig` 的 `ModelContextWindowTokens` 之后追加（保持结构体字段顺序连续）：

```go
	// CompressionTriggerRatio 压缩触发水位比例（相对上下文窗口大小），0 表示不按比例缩放
	CompressionTriggerRatio float64 `json:"compression_trigger_ratio"`
	// CompressionTargetRatio 压缩目标水位比例（相对上下文窗口大小），0 表示不按比例缩放
	CompressionTargetRatio float64 `json:"compression_target_ratio"`
```

在 `Validate()` 的 `for model, tokens := range c.ModelContextWindowTokens` 循环之后追加：

```go
	if c.CompressionTriggerRatio < 0 || c.CompressionTriggerRatio > 1 {
		return fmt.Errorf("compression_trigger_ratio 必须在 [0, 1] 区间，当前值: %v", c.CompressionTriggerRatio)
	}
	if c.CompressionTargetRatio < 0 || c.CompressionTargetRatio > 1 {
		return fmt.Errorf("compression_target_ratio 必须在 [0, 1] 区间，当前值: %v", c.CompressionTargetRatio)
	}
	if c.CompressionTriggerRatio > 0 && c.CompressionTargetRatio > 0 &&
		c.CompressionTargetRatio >= c.CompressionTriggerRatio {
		return fmt.Errorf("compression_target_ratio(%v) 必须小于 compression_trigger_ratio(%v)",
			c.CompressionTargetRatio, c.CompressionTriggerRatio)
	}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/context_engine/schema/ -v`
Expected: PASS（含既有用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/schema/config.go internal/agentcore/context_engine/schema/config_ratio_test.go
git commit -m "feat(context_engine): ContextEngineConfig 增加压缩水位比例字段与校验"
```

---

## Task 2: 写侧解析 max_input_tokens / max_output_tokens（B1）

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/config_apply.go:560-635`
- Test: `internal/swarm/gateway/channel_manager/web/config_apply_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

在 `config_apply_test.go` 追加（复用该文件已有的 `buildModelsDefaultsFromFrontend` 调用方式；若已有 helper 构造 ctx，直接沿用）：

```go
// TestBuildModelsDefaultsFromFrontend_Token字段 验证两个 token 字段的写入与非法值拦截
func TestBuildModelsDefaultsFromFrontend_Token字段(t *testing.T) {
	ctx := context.Background()

	// 正常值写入 mcc
	models := []any{
		map[string]any{
			"model_name":        "qwen3-coder",
			"api_base":          "https://example.com/v1",
			"api_key":           "sk-test",
			"model_provider":    "OpenAI",
			"max_input_tokens":  32000,
			"max_output_tokens": 8192,
		},
	}
	got, err := buildModelsDefaultsFromFrontend(ctx, models, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	mcc, _ := got[0]["model_client_config"].(map[string]any)
	if mcc["max_input_tokens"] != 32000 {
		t.Errorf("max_input_tokens 应为 32000，实际 %v", mcc["max_input_tokens"])
	}
	if mcc["max_output_tokens"] != 8192 {
		t.Errorf("max_output_tokens 应为 8192，实际 %v", mcc["max_output_tokens"])
	}

	// 未填 → 写 0（0 表示未配置，可覆盖旧值）
	models[0] = map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "model_provider": "OpenAI",
	}
	got, err = buildModelsDefaultsFromFrontend(ctx, models, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	mcc, _ = got[0]["model_client_config"].(map[string]any)
	if mcc["max_input_tokens"] != 0 || mcc["max_output_tokens"] != 0 {
		t.Errorf("未配置时应写 0，实际 input=%v output=%v", mcc["max_input_tokens"], mcc["max_output_tokens"])
	}

	// 负数 / 非整数 → ConfigBadRequest
	bad := []any{map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "model_provider": "OpenAI",
		"max_input_tokens": -1,
	}}
	if _, err := buildModelsDefaultsFromFrontend(ctx, bad, nil); err == nil {
		t.Error("负数应返回错误")
	} else if _, ok := err.(*ConfigBadRequest); !ok {
		t.Errorf("应为 *ConfigBadRequest，实际 %T", err)
	}

	bad[0].(map[string]any)["max_input_tokens"] = "abc"
	if _, err := buildModelsDefaultsFromFrontend(ctx, bad, nil); err == nil {
		t.Error("非整数应返回错误")
	}
}
```

- [ ] **Step 1b: 追加 merge 保留用例**

```go
// TestBuildModelsDefaultsFromFrontend_Token字段覆盖与保留 验证 merge 的"未改保留 / 改则覆盖"语义
func TestBuildModelsDefaultsFromFrontend_Token字段覆盖与保留(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	pathutil.SetUserHome(home)
	t.Cleanup(pathutil.ResetCache)
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("建配置目录失败: %v", err)
	}
	fixture := `models:
  defaults:
    - model_client_config:
        model_name: qwen3-coder
        api_base: https://example.com/v1
        api_key: sk-test
        client_provider: OpenAI
        max_input_tokens: 32000
      model_config_obj:
        temperature: 0.95
      is_default: true
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(fixture), 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}

	build := func(tokens any) map[string]any {
		item := map[string]any{
			"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
			"api_key": "sk-test", "model_provider": "OpenAI",
		}
		if tokens != nil {
			item["max_input_tokens"] = tokens
		}
		got, err := buildModelsDefaultsFromFrontend(ctx, []any{item}, nil)
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		return got[0]["model_client_config"].(map[string]any)
	}

	if v := build(32000)["max_input_tokens"]; v != 32000 {
		t.Errorf("未改动应保留 32000，实际 %v", v)
	}
	if v := build(64000)["max_input_tokens"]; v != 64000 {
		t.Errorf("改动应覆盖为 64000，实际 %v", v)
	}
	if v := build(0)["max_input_tokens"]; v != 0 {
		t.Errorf("清空应写 0，实际 %v", v)
	}
}
```

> 注入原理同 Task 3（`pathutil.SetUserHome` + `ResetCache`）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/swarm/gateway/channel_manager/web/ -run 'Token字段' -v`
Expected: FAIL，`max_input_tokens 应为 32000，实际 <nil>`

- [ ] **Step 3: 实现解析**

在 `config_apply.go` 中 `verifySSL` 解析块之后追加：

```go
		maxInputTokens := 0
		if v, ok := itemMap["max_input_tokens"]; ok && v != nil && fmt.Sprint(v) != "" {
			n, perr := parseInt(v)
			if perr != nil || n < 0 {
				return nil, &ConfigBadRequest{
					Message: fmt.Sprintf("models[%d].max_input_tokens 必须是非负整数", idx),
				}
			}
			maxInputTokens = n
		}

		maxOutputTokens := 0
		if v, ok := itemMap["max_output_tokens"]; ok && v != nil && fmt.Sprint(v) != "" {
			n, perr := parseInt(v)
			if perr != nil || n < 0 {
				return nil, &ConfigBadRequest{
					Message: fmt.Sprintf("models[%d].max_output_tokens 必须是非负整数", idx),
				}
			}
			maxOutputTokens = n
		}
```

并在 `entry` 的 `model_client_config` 映射中追加两个键（放在 `"verify_ssl": verifySSL,` 之后）：

```go
				"max_input_tokens":  maxInputTokens,
				"max_output_tokens": maxOutputTokens,
```

说明：**始终写入**（0 表示未配置）——因为 `mergeModelsForReplaceAll` 只遍历前端提交的键，省略键会导致用户清空后旧值残留。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/swarm/gateway/channel_manager/web/ -run 'Token字段' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/channel_manager/web/config_apply.go internal/swarm/gateway/channel_manager/web/config_apply_test.go
git commit -m "feat(web): models.replace_all 解析 max_input/output_tokens 并拦截非法值"
```

---

## Task 3: models.list 回填两个字段（B2）

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/web_handlers.go:960-1002`
- Test: `internal/swarm/gateway/channel_manager/web/web_handlers_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestHandleModelsList_Token字段回填 验证两个 token 字段从 YAML 回填到扁平结构
func TestHandleModelsList_Token字段回填(t *testing.T) {
	home := t.TempDir()
	pathutil.SetUserHome(home)          // 数据根 ~/.uapclaw 的替身
	t.Cleanup(pathutil.ResetCache)      // 必须在 cleanup 里重置 path 包缓存
	cfgDir := filepath.Join(home, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("建配置目录失败: %v", err)
	}
	cfgPath := filepath.Join(cfgDir, "config.yaml")
	content := `models:
  defaults:
    - model_client_config:
        model_name: qwen3-coder
        api_base: https://example.com/v1
        api_key: sk-test
        client_provider: OpenAI
        max_input_tokens: 32000
        max_output_tokens: 8192
      model_config_obj:
        temperature: 0.95
      is_default: true
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试配置失败: %v", err)
	}
	handler := handleModelsList()
	resp, err := handler(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("handler 报错: %v", err)
	}
	models, _ := resp["models"].([]map[string]any)
	if len(models) != 1 {
		t.Fatalf("应返回 1 条，实际 %d", len(models))
	}
	if models[0]["max_input_tokens"] != 32000 {
		t.Errorf("max_input_tokens 应为 32000，实际 %v", models[0]["max_input_tokens"])
	}
	if models[0]["max_output_tokens"] != 8192 {
		t.Errorf("max_output_tokens 应为 8192，实际 %v", models[0]["max_output_tokens"])
	}
}
```

> 注入原理：`config.New("")` 走 `pathutil.ConfigFile()` = `ConfigDir()/config.yaml`，而 `ConfigDir()` 在已初始化时是 `WorkspaceDir()/config`。`pathutil.SetUserHome(home)` 把数据根指到临时目录并重置缓存，`ResetCache` 必须在 `t.Cleanup` 中调用（该函数非并发安全）。import 需补 `pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/swarm/gateway/channel_manager/web/ -run 'Token字段回填' -v`
Expected: FAIL，`max_input_tokens 应为 32000，实际 <nil>`

- [ ] **Step 3: 实现回填**

在 `handleModelsList` 内 `if v, ok := mcc["client_provider"].(string); ok { ... }` 之后追加：

```go
				if n, perr := parseInt(mcc["max_input_tokens"]); perr == nil && n > 0 {
					flat["max_input_tokens"] = n
				}
				if n, perr := parseInt(mcc["max_output_tokens"]); perr == nil && n > 0 {
					flat["max_output_tokens"] = n
				}
```

（`n <= 0` 不回填，前端显示空 = 未配置，避免界面上出现一排 "0"。）

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/swarm/gateway/channel_manager/web/ -run 'Token字段回填' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/channel_manager/web/web_handlers.go internal/swarm/gateway/channel_manager/web/web_handlers_test.go
git commit -m "feat(web): models.list 回填 max_input/output_tokens"
```

---

## Task 4: 白名单与 max_output_tokens → ModelRequestConfig.MaxTokens（B3+B4）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:2117-2208`
- Test: `internal/swarm/server/adapter/deep_adapter_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestBuildModelFromEntry_输出上限 验证 max_output_tokens 落成 ModelRequestConfig.MaxTokens 且不进 Extra
func TestBuildModelFromEntry_输出上限(t *testing.T) {
	d := &DeepAdapter{}

	mcc := map[string]any{
		"model_name":        "qwen3-coder",
		"api_base":          "https://example.com/v1",
		"api_key":           "sk-test",
		"client_provider":   "OpenAI",
		"max_input_tokens":  32000,
		"max_output_tokens": 8192,
	}
	mco := map[string]any{"temperature": 0.95}

	m, err := d.buildModelFromEntry(mcc, mco)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m.ModelConfig == nil || m.ModelConfig.MaxTokens == nil {
		t.Fatal("MaxTokens 不应为 nil")
	}
	if *m.ModelConfig.MaxTokens != 8192 {
		t.Errorf("MaxTokens 应为 8192，实际 %d", *m.ModelConfig.MaxTokens)
	}
	// 白名单生效：两个键都不应进 client Extra
	if m.ClientConfig != nil {
		if _, ok := m.ClientConfig.Extra["max_output_tokens"]; ok {
			t.Error("max_output_tokens 不应进入 ModelClientConfig.Extra")
		}
		if _, ok := m.ClientConfig.Extra["max_input_tokens"]; ok {
			t.Error("max_input_tokens 不应进入 ModelClientConfig.Extra")
		}
	}

	// 未配置 → nil
	mcc2 := map[string]any{
		"model_name": "qwen3-coder", "api_base": "https://example.com/v1",
		"api_key": "sk-test", "client_provider": "OpenAI",
	}
	m2, err := d.buildModelFromEntry(mcc2, mco)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if m2.ModelConfig.MaxTokens != nil {
		t.Errorf("未配置时 MaxTokens 应为 nil，实际 %d", *m2.ModelConfig.MaxTokens)
	}
}
```

> 字段名以 `llm.Model` 实际导出字段为准（`ModelConfig` / `ClientConfig`）；如不同，按实际结构调整断言，**不要**改动生产代码的结构。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/swarm/server/adapter/ -run '输出上限' -v`
Expected: FAIL，`MaxTokens 不应为 nil`

- [ ] **Step 3: 实现**

在 `deep_adapter.go` 文件末尾「非导出函数」区追加：

```go
// toPositiveInt 将任意数值或字符串转为正整数；非正数或不可解析返回 (0,false)。
func toPositiveInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, n > 0
	case int32:
		return int(n), n > 0
	case int64:
		return int(n), n > 0
	case float64:
		i := int(n)
		return i, i > 0
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, false
		}
		return i, i > 0
	default:
		return 0, false
	}
}
```

在 `buildModelFromEntry` 的 `mConfigOpts` 组装处（`top_p` 分支之后）追加：

```go
	if n, ok := toPositiveInt(mcc["max_output_tokens"]); ok {
		mConfigOpts = append(mConfigOpts, llmschema.WithMaxTokens(n))
	}
```

在 `knownKeys` 映射中追加两项：

```go
		"max_input_tokens": {}, "max_output_tokens": {},
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/swarm/server/adapter/ -run '输出上限' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_test.go
git commit -m "feat(adapter): max_output_tokens 落成 ModelRequestConfig.MaxTokens 并加入 mcc 白名单"
```

---

## Task 5: 窗口映射 + 比例 + 恢复默认值（B5、A1）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:595`、`:2024-2048`、`:2830`
- Test: `internal/swarm/server/adapter/deep_adapter_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestDeepAgentContextEngineConfig_窗口映射 验证 max_input_tokens 收集成窗口映射并恢复 200/10 默认值
func TestDeepAgentContextEngineConfig_窗口映射(t *testing.T) {
	d := &DeepAdapter{}
	reactCfg := map[string]any{
		"context_engine_config": map[string]any{
			"enable_kv_cache_release":    true,
			"compression_trigger_ratio":  0.85,
			"compression_target_ratio":   0.55,
		},
	}
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name":        "qwen3-coder",
						"api_base":          "https://example.com/v1",
						"api_key":           "sk-test",
						"client_provider":   "OpenAI",
						"max_input_tokens":  32000,
						"max_output_tokens": 8192,
					},
					"is_default": true,
				},
			},
		},
	}

	got := d.deepAgentContextEngineConfig(reactCfg, configBase)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	if got.ModelContextWindowTokens["qwen3-coder"] != 32000 {
		t.Errorf("窗口应为 32000，实际 %d", got.ModelContextWindowTokens["qwen3-coder"])
	}
	if got.ModelName != "qwen3-coder" {
		t.Errorf("ModelName 应为 qwen3-coder，实际 %q", got.ModelName)
	}
	if got.CompressionTriggerRatio != 0.85 || got.CompressionTargetRatio != 0.55 {
		t.Errorf("比例应为 0.85/0.55，实际 %v/%v", got.CompressionTriggerRatio, got.CompressionTargetRatio)
	}
	// §4.5：恢复 Python ReActAgentConfig 默认值
	if got.MaxContextMessageNum != 200 {
		t.Errorf("MaxContextMessageNum 应为 200，实际 %d", got.MaxContextMessageNum)
	}
	if got.DefaultWindowRoundNum != 10 {
		t.Errorf("DefaultWindowRoundNum 应为 10，实际 %d", got.DefaultWindowRoundNum)
	}
	if got.ContextWindowTokens != 0 {
		t.Errorf("ContextWindowTokens（优先级1）应保持 0，实际 %d", got.ContextWindowTokens)
	}
	if !got.EnableKVCacheRelease {
		t.Error("EnableKVCacheRelease 应为 true")
	}
}

// TestDeepAgentContextEngineConfig_无cec段返回nil 保持旧行为
func TestDeepAgentContextEngineConfig_无cec段返回nil(t *testing.T) {
	d := &DeepAdapter{}
	if got := d.deepAgentContextEngineConfig(map[string]any{}, map[string]any{}); got != nil {
		t.Errorf("无 context_engine_config 段时应返回 nil，实际 %+v", got)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/swarm/server/adapter/ -run '窗口映射|无cec段' -v`
Expected: FAIL，`too many arguments in call to d.deepAgentContextEngineConfig`

- [ ] **Step 3: 实现**

把 `deepAgentContextEngineConfig` 整体替换为：

```go
// deepAgentContextEngineConfig 从 react 配置与 models 段构建上下文引擎配置。
// Python: _deep_agent_context_engine_config(react_cfg)
//
// 与 Python 的差异（已对齐的点）：
//   - Python 以 ReActAgentConfig() 默认值 model_copy(update=...) 起底，因此
//     max_context_message_num=200 / default_window_round_num=10 始终保留（§4.5）；
//   - 窗口值来自 models.defaults[].model_client_config.max_input_tokens。
func (d *DeepAdapter) deepAgentContextEngineConfig(config, configBase map[string]any) *ceschema.ContextEngineConfig {
	if config == nil {
		return nil
	}
	cecRaw, ok := config["context_engine_config"]
	if !ok {
		return nil
	}
	cecMap, ok := cecRaw.(map[string]any)
	if !ok {
		return nil
	}

	result := ceschema.NewContextEngineConfig()
	// 对齐 Python ReActAgentConfig 默认值，避免零值导致消息数/轮数不限（§4.5）
	result.MaxContextMessageNum = 200
	result.DefaultWindowRoundNum = 10

	if v, ok := cecMap["enable_kv_cache_release"].(bool); ok {
		result.EnableKVCacheRelease = v
	}
	if f, ok := toRatio(cecMap["compression_trigger_ratio"]); ok {
		result.CompressionTriggerRatio = f
	}
	if f, ok := toRatio(cecMap["compression_target_ratio"]); ok {
		result.CompressionTargetRatio = f
	}

	// 收集各模型条目的输入上限（同模型多条目冲突时 is_default 优先，见 D10）
	defaultModelName := ""
	for _, entry := range config.GetDefaultModels(configBase) {
		mcc, _ := entry["model_client_config"].(map[string]any)
		if mcc == nil {
			continue
		}
		modelName, _ := mcc["model_name"].(string)
		if modelName == "" {
			continue
		}
		isDefault, _ := entry["is_default"].(bool)
		if isDefault && defaultModelName == "" {
			defaultModelName = modelName
		}
		tokens, ok := toPositiveInt(mcc["max_input_tokens"])
		if !ok {
			continue
		}
		if prev, exists := result.ModelContextWindowTokens[modelName]; exists && prev != tokens {
			if isDefault {
				result.ModelContextWindowTokens[modelName] = tokens
			}
			logger.Warn(logComponent).
				Str("model_name", modelName).
				Int("kept", result.ModelContextWindowTokens[modelName]).
				Int("ignored", tokens).
				Msg("max_input_tokens 在同名模型条目间不一致，已按默认条目取值")
			continue
		}
		result.ModelContextWindowTokens[modelName] = tokens
	}

	// 当前生效模型名：react.model_name 优先，否则取默认条目
	if v, ok := config["model_name"].(string); ok && v != "" {
		result.ModelName = v
	} else {
		result.ModelName = defaultModelName
	}

	return &result
}
```

在「非导出函数」区追加比例解析助手：

```go
// toRatio 将任意数值或字符串解析为 (0,1] 区间的比例；不可解析或越界返回 (0,false)。
func toRatio(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, n > 0 && n <= 1
	case int:
		f := float64(n)
		return f, f > 0 && f <= 1
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		return f, f > 0 && f <= 1
	default:
		return 0, false
	}
}
```

更新两处调用点：

```go
// :595（CreateInstance 内）
		ContextEngineConfig: d.deepAgentContextEngineConfig(config, configBase),

// :2830（makeDeepAgentConfig 内）
		ContextEngineConfig: d.deepAgentContextEngineConfig(config, d.configBase),
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/swarm/server/adapter/ -run '窗口映射|无cec段' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_test.go
git commit -m "feat(adapter): 窗口值从 models.defaults 灌入 ContextEngineConfig 并恢复 200/10 默认值"
```

---

## Task 6: context.usage 分母与分子（B6）

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/stream_event_emit.go:176-229`
- Test: `internal/swarm/agents/harness/common/rails/stream_event_emit_test.go`（新建或追加）

- [ ] **Step 1: 写失败测试**

```go
package rails

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestContextUsageTokens_分母取ceConfig_分子取InputTokens 验证 context.usage 的口径
func TestContextUsageTokens_分母取ceConfig_分子取InputTokens(t *testing.T) {
	usage := &llmschema.UsageMetadata{InputTokens: 2500, OutputTokens: 300, TotalTokens: 2800}
	if got := contextUsageTokensUsed(usage); got != 2500 {
		t.Errorf("分子应优先 InputTokens=2500，实际 %d", got)
	}
	// InputTokens 缺失时回退 TotalTokens
	if got := contextUsageTokensUsed(&llmschema.UsageMetadata{TotalTokens: 2800}); got != 2800 {
		t.Errorf("回退 TotalTokens=2800，实际 %d", got)
	}
	if got := contextUsageTokensUsed(nil); got != 0 {
		t.Errorf("nil 应返回 0，实际 %d", got)
	}
	_ = context.Background()
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/swarm/agents/harness/common/rails/ -run '分母取ceConfig' -v`
Expected: FAIL，`undefined: contextUsageTokensUsed`

- [ ] **Step 3: 实现**

在 `stream_event_emit.go` 的「非导出函数」区追加：

```go
// contextUsageTokensUsed 计算 context.usage 的分子：优先输入侧 token，回退总 token。
func contextUsageTokensUsed(usage *llmschema.UsageMetadata) int {
	if usage == nil {
		return 0
	}
	if usage.InputTokens > 0 {
		return usage.InputTokens
	}
	return usage.TotalTokens
}
```

把 `emitContextUsage` 中取模型名与计算分母的两段改为：

```go
	// 获取模型名称与上下文引擎配置，对齐 Python: agent._config.model_name
	var modelName string
	var ceConfig ceschema.ContextEngineConfig
	func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Debug(logComponent).Any("recover", r).Msg("Failed to get model_name from ctx.agent")
			}
		}()
		if agent := cbCtx.Agent(); agent != nil {
			if config := agent.Config(); config != nil {
				modelName = config.ModelName()
				ceConfig = config.GetContextEngineConfig()
			}
		}
	}()

	// 计算上下文窗口上限：优先显式覆盖值，其次模型映射，再次内置表与 200000 兜底
	rawTotalTokens := cecontext.ResolveContextMax(
		modelName, ceConfig.ContextWindowTokens, ceConfig.ModelContextWindowTokens)

	// 获取当前 token 用量：优先输入侧
	var currentContextTokens int
	if inputs := cbCtx.Inputs(); inputs != nil {
		if mcInputs, ok := inputs.(*sainterfaces.ModelCallInputs); ok && mcInputs.Response != nil {
			currentContextTokens = contextUsageTokensUsed(mcInputs.Response.UsageMetadata)
		}
	}
```

并补齐 import：`ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"`（`llmschema` 若已引入则复用）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/swarm/agents/harness/common/rails/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/stream_event_emit.go internal/swarm/agents/harness/common/rails/stream_event_emit_test.go
git commit -m "fix(rails): context.usage 分母改取 ContextEngineConfig、分子改用 input_tokens"
```

---

## Task 7: statMessages 分子口径（B7）

**Files:**
- Modify: `internal/agentcore/context_engine/context/session_model_context.go:1050-1072`
- Test: `internal/agentcore/context_engine/context/session_model_context_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestStatMessages_优先InputTokens 验证统计口径优先输入侧 token
func TestStatMessages_优先InputTokens(t *testing.T) {
	mc := newTestSessionModelContext() // 该文件已有的测试构造助手

	am := llmschema.NewAssistantMessage("hi")
	am.UsageMetadata = &llmschema.UsageMetadata{InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280}
	msgs := []llmschema.BaseMessage{llmschema.NewUserMessage("hello"), am}

	stat := &iface.ContextStats{}
	mc.statMessages(stat, msgs)
	if stat.TotalTokens != 1200 {
		t.Errorf("应使用 InputTokens=1200，实际 %d", stat.TotalTokens)
	}
}
```

> `llmschema` 为 `internal/agentcore/foundation/llm/schema` 的别名（该测试文件已按此别名引入）。`newTestSessionModelContext()` 定义在同文件 `session_model_context_test.go:114`。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/context_engine/context/ -run '优先InputTokens' -v`
Expected: FAIL，`应使用 InputTokens=1200，实际 1280`

- [ ] **Step 3: 实现**

把 `statMessages` 中的 usage 优先段替换为：

```go
	// 优先使用最后一条 AssistantMessage 的 usage_metadata：优先输入侧 token
	// Python: usage_tokens = self._get_last_assistant_usage_tokens(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		if am, ok := messages[i].(*llm_schema.AssistantMessage); ok {
			if am.UsageMetadata == nil {
				continue
			}
			tokens := am.UsageMetadata.InputTokens
			if tokens == 0 {
				tokens = am.UsageMetadata.TotalTokens
			}
			if tokens > 0 {
				stat.TotalTokens = tokens
				return
			}
		}
	}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/context_engine/context/ -v`
Expected: PASS（含既有统计用例；若有旧用例断言 TotalTokens 含输出，按新口径修正并在提交信息里说明）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/context/session_model_context.go internal/agentcore/context_engine/context/session_model_context_test.go
git commit -m "fix(context): ContextStats 优先使用 input_tokens 作为上下文占用"
```

---

## Task 8: 压缩阈值按比例收口（B8）

**Files:**
- Modify: `internal/agentcore/harness/rails/context_engineer/context_processor_rail.go:84-128`（`Init`）+ 非导出函数区
- Test: `internal/agentcore/harness/rails/context_engineer/compression_ratio_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package context_engineer

import (
	"testing"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
)

// TestApplyCompressionRatios_只下调不放大 验证 min(绝对, ratio×window) 与不参与字段不变
func TestApplyCompressionRatios_只下调不放大(t *testing.T) {
	cases := []struct {
		name            string
		window          int
		wantTrigger     int
		wantTarget      int
		wantDialogue    int
	}{
		{"32K窗口", 32000, 27200, 17600, 27200},
		{"128K窗口", 128000, 108800, 70400, 100000},
		{"256K窗口", 256000, 217600, 140800, 100000},
		{"512K窗口", 512000, 230000, 160000, 100000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			specs := []ceiface.ProcessorSpec{
				{Type: "DialogueCompressor", Config: &compressor.DialogueCompressorConfig{TokensThreshold: 100000}},
				{Type: "CurrentRoundCompressor", Config: &compressor.CurrentRoundCompressorConfig{TokensThreshold: 100000}},
				{Type: "RoundLevelCompressor", Config: &compressor.RoundLevelCompressorConfig{
					TriggerTotalTokens: 230000, TargetTotalTokens: 160000}},
				{Type: "FullCompactProcessor", Config: &compressor.FullCompactProcessorConfig{
					TriggerTotalTokens: 180000, MessagesToKeep: 10}},
			}
			out := applyCompressionRatios(specs, tc.window, 0.85, 0.55)

			dialogue := out[0].Config.(*compressor.DialogueCompressorConfig)
			roundLevel := out[2].Config.(*compressor.RoundLevelCompressorConfig)
			if dialogue.TokensThreshold != tc.wantDialogue {
				t.Errorf("Dialogue 阈值应为 %d，实际 %d", tc.wantDialogue, dialogue.TokensThreshold)
			}
			if roundLevel.TriggerTotalTokens != tc.wantTrigger {
				t.Errorf("trigger 应为 %d，实际 %d", tc.wantTrigger, roundLevel.TriggerTotalTokens)
			}
			if roundLevel.TargetTotalTokens != tc.wantTarget {
				t.Errorf("target 应为 %d，实际 %d", tc.wantTarget, roundLevel.TargetTotalTokens)
			}
			// 不参与收口的字段保持不变；MessagesToKeep 不做缩放
			full := out[3].Config.(*compressor.FullCompactProcessorConfig)
			if full.MessagesToKeep != 10 {
				t.Errorf("MessagesToKeep 不应被改动，实际 %d", full.MessagesToKeep)
			}
		})
	}
}

// TestApplyCompressionRatios_比例缺省时保持绝对值 验证 ratio=0 时不改动
func TestApplyCompressionRatios_比例缺省时保持绝对值(t *testing.T) {
	specs := []ceiface.ProcessorSpec{
		{Type: "RoundLevelCompressor", Config: &compressor.RoundLevelCompressorConfig{
			TriggerTotalTokens: 230000, TargetTotalTokens: 160000}},
	}
	out := applyCompressionRatios(specs, 32000, 0, 0)
	rl := out[0].Config.(*compressor.RoundLevelCompressorConfig)
	if rl.TriggerTotalTokens != 230000 || rl.TargetTotalTokens != 160000 {
		t.Errorf("比例缺省时不应收敛，实际 %d/%d", rl.TriggerTotalTokens, rl.TargetTotalTokens)
	}
}

// TestApplyCompressionRatios_target不小于trigger时兜底 验证极窄窗口的兜底
func TestApplyCompressionRatios_target不小于trigger时兜底(t *testing.T) {
	specs := []ceiface.ProcessorSpec{
		{Type: "RoundLevelCompressor", Config: &compressor.RoundLevelCompressorConfig{
			TriggerTotalTokens: 1000, TargetTotalTokens: 2000}},
	}
	out := applyCompressionRatios(specs, 1000, 0.85, 0.55)
	rl := out[0].Config.(*compressor.RoundLevelCompressorConfig)
	if rl.TargetTotalTokens >= rl.TriggerTotalTokens {
		t.Errorf("target(%d) 应小于 trigger(%d)", rl.TargetTotalTokens, rl.TriggerTotalTokens)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/agentcore/harness/rails/context_engineer/ -run 'ApplyCompressionRatios' -v`
Expected: FAIL，`undefined: applyCompressionRatios`

- [ ] **Step 3: 实现**

在 `context_processor_rail.go` 的「非导出函数」区追加：

```go
// applyCompressionRatios 按上下文窗口与比例收口各处理器的总量阈值。
//
// 规则：阈值 = min(绝对阈值, round(ratio × window))，即只下调不放大。
// 只处理与「整个上下文总量」同量纲的字段；per-message 与单次输出长度类字段不缩放。
// Python 侧无对应实现（本设计新增，见 spec §6.1）。
func applyCompressionRatios(specs []ceiface.ProcessorSpec, window int, triggerRatio, targetRatio float64) []ceiface.ProcessorSpec {
	if window <= 0 || (triggerRatio <= 0 && targetRatio <= 0) {
		return specs
	}
	for i := range specs {
		switch cfg := specs[i].Config.(type) {
		case *compressor.DialogueCompressorConfig:
			cfg.TokensThreshold = shrinkThreshold(cfg.TokensThreshold, window, triggerRatio)
		case *compressor.CurrentRoundCompressorConfig:
			cfg.TokensThreshold = shrinkThreshold(cfg.TokensThreshold, window, triggerRatio)
		case *compressor.RoundLevelCompressorConfig:
			cfg.TriggerTotalTokens = shrinkThreshold(cfg.TriggerTotalTokens, window, triggerRatio)
			cfg.TargetTotalTokens = shrinkThreshold(cfg.TargetTotalTokens, window, targetRatio)
			if cfg.TargetTotalTokens >= cfg.TriggerTotalTokens {
				fallback := int(math.Round(float64(cfg.TriggerTotalTokens) * 0.7))
				if fallback < 1 {
					fallback = 1
				}
				logger.Warn(logComponent).
					Str("processor", specs[i].Type).
					Int("trigger", cfg.TriggerTotalTokens).
					Int("target", cfg.TargetTotalTokens).
					Int("fallback_target", fallback).
					Msg("压缩目标阈值不小于触发阈值，已回退为触发的 70%")
				cfg.TargetTotalTokens = fallback
			}
		case *compressor.FullCompactProcessorConfig:
			cfg.TriggerTotalTokens = shrinkThreshold(cfg.TriggerTotalTokens, window, triggerRatio)
		}
	}
	return specs
}

// shrinkThreshold 返回 min(absolute, round(ratio×window))；ratio<=0 或绝对值为 0 时保持不变。
func shrinkThreshold(absolute, window int, ratio float64) int {
	if ratio <= 0 || window <= 0 || absolute <= 0 {
		return absolute
	}
	scaled := int(math.Round(ratio * float64(window)))
	if scaled <= 0 {
		return absolute
	}
	if scaled < absolute {
		return scaled
	}
	return absolute
}
```

在 `Init` 中 `allProcessors := MergeProcessors(...)` 之后、`config.ContextProcessors = allProcessors` 之前插入：

```go
	// 阈值收口：必须在 MergeProcessors 之后，否则 yaml 里的绝对值会绕开 min（spec §6.6）
	if reactCfg := getReactAgentConfig(agent); reactCfg != nil {
		ce := reactCfg.ContextEngineConfig
		window := cecontext.ResolveContextMax(ce.ModelName, ce.ContextWindowTokens, ce.ModelContextWindowTokens)
		allProcessors = applyCompressionRatios(
			allProcessors, window, ce.CompressionTriggerRatio, ce.CompressionTargetRatio)
	}
```

补齐 import：`"math"`（`compressor`、`cecontext`、`logger` 应已存在）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/agentcore/harness/rails/context_engineer/ -v`
Expected: PASS（含 session memory 既有用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/context_engineer/context_processor_rail.go internal/agentcore/harness/rails/context_engineer/compression_ratio_test.go
git commit -m "feat(rails): 压缩阈值按窗口比例收口（min 绝对阈值，只下调不放大）"
```

---

## Task 9: 主 Agent processor rail 读取 yaml 五段（A2）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:786-790`
- Test: `internal/swarm/server/adapter/deep_adapter_rails_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestBuildContextProcessorRail_读取yaml五段 验证 adapter 把 yaml 的 processor 配置传给 rail
func TestBuildContextProcessorRail_读取yaml五段(t *testing.T) {
	d := &DeepAdapter{}
	reactCfg := map[string]any{
		"context_engine_config": map[string]any{
			"dialogue_compressor_config":      map[string]any{"tokens_threshold": 50000},
			"round_level_compressor_config":   map[string]any{"trigger_total_tokens": 120000},
			"full_compact_processor_config":   map[string]any{"trigger_total_tokens": 90000},
		},
	}
	rail := d.buildContextProcessorRail(reactCfg)
	if rail == nil {
		t.Fatal("不应返回 nil")
	}
	ceRail, ok := rail.(*cerails.ContextProcessorRail)
	if !ok {
		t.Fatalf("类型应为 *ContextProcessorRail，实际 %T", rail)
	}
	names := ceRail.UserProcessorTypes() // 见 Step 3 的访问器
	want := map[string]bool{"DialogueCompressor": true, "RoundLevelCompressor": true, "FullCompactProcessor": true}
	if len(names) != len(want) {
		t.Fatalf("应为 %d 段，实际 %v", len(want), names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("意外的 processor: %s", n)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/swarm/server/adapter/ -run '读取yaml五段' -v`
Expected: FAIL，`too many arguments in call to d.buildContextProcessorRail`

- [ ] **Step 3: 实现**

把 `buildContextProcessorRail` 替换为：

```go
// buildContextProcessorRail 构建上下文处理护栏，并把 yaml 里的 processor 配置透传进去。
//
// Python: _build_context_processor_rail(config) (interface_deep.py L346-395)
//
// 覆盖机制走 ProcessorSpec.ConfigOverrides（字段级部分覆盖），
// 因此 yaml 里的绝对阈值会先合并进 preset，再被比例收口（spec §6.6）。
func (d *DeepAdapter) buildContextProcessorRail(config map[string]any) sainterfaces.AgentRail {
	ctxCfg, _ := config["context_engine_config"].(map[string]any)
	userProcessors := make([]ceiface.ProcessorSpec, 0, 5)
	specs := []struct {
		configKey string
		typeName  string
	}{
		{"message_summary_offloader_config", "MessageSummaryOffloader"},
		{"dialogue_compressor_config", "DialogueCompressor"},
		{"current_round_compressor_config", "CurrentRoundCompressor"},
		{"round_level_compressor_config", "RoundLevelCompressor"},
		{"full_compact_processor_config", "FullCompactProcessor"},
	}
	for _, spec := range specs {
		if m, _ := ctxCfg[spec.configKey].(map[string]any); len(m) > 0 {
			userProcessors = append(userProcessors, ceiface.ProcessorSpec{
				Type:            spec.typeName,
				ConfigOverrides: utils.DeepCopyMap(m),
			})
		}
	}
	return cerails.NewContextProcessorRail(cerails.WithUserProcessors(userProcessors))
}
```

在 rail 包新增只读访问器（供测试与适配层排查）：

```go
// UserProcessorTypes 返回用户自定义处理器类型列表（只读，供测试与诊断）。
func (r *ContextProcessorRail) UserProcessorTypes() []string {
	names := make([]string, 0, len(r.userProcessors))
	for _, spec := range r.userProcessors {
		names = append(names, spec.Type)
	}
	return names
}
```

更新调用点 `deep_adapter_rails.go:940`：`d.buildContextProcessorRail()` → `d.buildContextProcessorRail(d.configCache)`。补齐 import：`ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"`、`utils`（若未引入）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/swarm/server/adapter/ -run '读取yaml五段' -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter_rails_test.go internal/agentcore/harness/rails/context_engineer/context_processor_rail.go
git commit -m "feat(adapter): 主 Agent processor rail 读取 yaml 四段并新增 full_compact 段"
```

---

## Task 10: 团队与子代理复用同一窗口映射（A3）

**Files:**
- Modify: `internal/swarm/agents/harness/team/config_loader.go:265-305`
- Test: `internal/swarm/agents/harness/team/config_loader_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestBuildDefaultModelDict_Token字段透传 验证团队默认模型字典保留两个 token 键
func TestBuildDefaultModelDict_Token字段透传(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{
					"model_client_config": map[string]any{
						"model_name":        "qwen3-coder",
						"api_base":          "https://example.com/v1",
						"api_key":           "sk-test",
						"client_provider":   "OpenAI",
						"max_input_tokens":  32000,
						"max_output_tokens": 8192,
					},
					"is_default": true,
				},
			},
		},
	}
	got := buildDefaultModelDict(configBase)
	mcc, _ := got["model_client_config"].(map[string]any)
	if mcc["max_input_tokens"] != 32000 {
		t.Errorf("max_input_tokens 应透传 32000，实际 %v", mcc["max_input_tokens"])
	}
	if mcc["max_output_tokens"] != 8192 {
		t.Errorf("max_output_tokens 应透传 8192，实际 %v", mcc["max_output_tokens"])
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/swarm/agents/harness/team/ -run 'Token字段透传' -v`
Expected: 若已通（`buildDefaultModelDict` 深拷贝整段 mcc），则本任务退化为**只补测试**；若失败，进入 Step 3。

- [ ] **Step 3: 实现（仅在 Step 2 失败时）**

`buildDefaultModelDict` 目前 `utils.DeepCopyMap(mcc)` 会保留全部键，通常无需改动；若发现键被过滤，在 `mccCopy` 组装后补：

```go
	if n, ok := toPositiveInt(mcc["max_input_tokens"]); ok {
		mccCopy["max_input_tokens"] = n
	}
	if n, ok := toPositiveInt(mcc["max_output_tokens"]); ok {
		mccCopy["max_output_tokens"] = n
	}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/swarm/agents/harness/team/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/agents/harness/team/config_loader.go internal/swarm/agents/harness/team/config_loader_test.go
git commit -m "test(team): 覆盖默认模型字典透传 max_input/output_tokens"
```

---

## Task 11: 配置模板对齐 Python（T1）

**Files:**
- Modify: `resources/config.yaml:120-180`
- Test: `internal/common/config/config_yaml_parity_test.go`（追加用例）

- [ ] **Step 1: 写失败测试**

```go
// TestResourcesConfigTemplate_ContextEngine段 验证模板包含 token 字段与压缩段
func TestResourcesConfigTemplate_ContextEngine段(t *testing.T) {
	raw, err := os.ReadFile("../../../resources/config.yaml")
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	text := string(raw)
	required := []string{
		"max_input_tokens",
		"max_output_tokens",
		"compression_trigger_ratio: 0.85",
		"compression_target_ratio: 0.55",
		"enabled: true",
		"enable_reload: true",
		"message_summary_offloader_config",
		"dialogue_compressor_config",
		"current_round_compressor_config",
		"round_level_compressor_config",
		"full_compact_processor_config",
	}
	for _, key := range required {
		if !strings.Contains(text, key) {
			t.Errorf("模板缺少 %q", key)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/common/config/ -run 'ResourcesConfigTemplate' -v`
Expected: FAIL，`模板缺少 "max_input_tokens"`

- [ ] **Step 3: 实现：改模板**

在 `models.defaults[0].model_client_config` 的 `custom_headers: {}` 之后追加：

```yaml
        # 输入上限（token）：该模型单次请求可接受的输入量，作为上下文窗口分母。
        # 0 / 缺省 = 未知，回退内置模型表或 200000 兜底。
        max_input_tokens: 0
        # 输出上限（token）：作为请求 max_tokens 的默认值；请求级显式传值优先。
        # 0 / 缺省 = 不限制，交由服务端默认。
        max_output_tokens: 0
```

把 `react.context_engine_config` 段整体替换为（值逐字段对齐 `jiuwenswarm/resources/config.yaml:269-303`，仅追加 2 个 ratio 键）：

```yaml
  # 上下文工程配置（对齐 Python jiuwenswarm/resources/config.yaml:269-303）
  context_engine_config:
    # 压缩总开关：false 时整个 ContextProcessorRail 不注册（deep_adapter_rails.go:936）
    enabled: true
    # 允许模型通过 reload 提示重取已卸载的消息
    enable_reload: true
    # 是否释放 KV 缓存
    enable_kv_cache_release: false
    # 压缩水位比例（相对 max_input_tokens）；0 = 不按比例，仅用下列绝对阈值
    compression_trigger_ratio: 0.85
    compression_target_ratio: 0.55
    message_summary_offloader_config:
      messages_threshold: null
      tokens_threshold: 60000
      large_message_threshold: 60000
      offload_message_type: ["tool"]
      protected_tool_names: ["read_file:*SKILL.md", "reload_original_context_messages"]
      messages_to_keep: null
      keep_last_round: false
      customized_summary_prompt: null
      enable_adaptive_compression: true
      summary_max_tokens: 900
      enable_precise_step: false
      step_summary_max_context_messages: 8
      content_max_chars_for_compression: 200000
    dialogue_compressor_config:
      messages_threshold: null
      tokens_threshold: 100000
      messages_to_keep: 10
      keep_last_round: false
      compression_target_tokens: 1800
      offload_writeback_enabled: false
    current_round_compressor_config:
      tokens_threshold: 100000
      messages_to_keep: 3
      min_selected_tokens_for_compression: 20000
      compression_target_tokens: 4000
      summary_merge_target_tokens: 4000
      accumulated_summary_token_limit: 20000
      summary_merge_min_blocks: 3
      prior_context_window_size: 10
      offload_writeback_enabled: false
    round_level_compressor_config:
      rounds_threshold: 2
      tokens_threshold: 230000
      trigger_total_tokens: 230000
      target_total_tokens: 160000
      compression_call_max_tokens: 250000
      keep_last_round: true
      keep_recent_messages: 6
      messages_to_keep: 6
      first_pass_target_tokens: 30000
      second_pass_target_tokens: 20000
      third_pass_target_tokens: 10000
      truncate_head_ratio: 0.2
      offload_writeback_enabled: false
    # 新增段：session_memory 路径的预设压缩器（Python 侧当前无此段）
    full_compact_processor_config:
      trigger_total_tokens: 180000
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/common/config/ -v`
Expected: PASS（既有 config 模板一致性用例同时保持通过；若有 golden 快照需同步更新）

- [ ] **Step 5: 提交**

```bash
git add resources/config.yaml internal/common/config/config_yaml_parity_test.go
git commit -m "feat(config): 模板对齐 Python（enabled/四段）+ 新增两 token 键与压缩比例键"
```

---

## Task 12: 前端表单与回填（F1/F2/F3）

**Files:**
- Modify: `internal/swarm/channels/web/frontend/src/types/index.ts:32-51`
- Modify: `internal/swarm/channels/web/frontend/src/components/ConfigPanel/index.tsx:236-249`、`:1098`、`:1145`、`:860-905`
- Modify: `internal/swarm/channels/web/frontend/src/i18n/locales/{zh,en}.json`

- [ ] **Step 1: 加类型字段**

`types/index.ts` 的 `ModelEntry` 内，`alias?: string;` 之后追加：

```ts
  /** 输入上限（token）：该模型可接受的输入量，用作上下文窗口分母；0 = 未配置 */
  max_input_tokens?: number;
  /** 输出上限（token）：请求 max_tokens 的默认值；0 = 不限制 */
  max_output_tokens?: number;
```

- [ ] **Step 2: 加校验分支**

`ConfigPanel/index.tsx` 的 `getFieldLengthErrorKey` 之后新增：

```ts
/** 校验 token 类字段：必须是非负整数 */
function getTokenFieldError(field: keyof ModelEntry, value: string): string | null {
  if (field !== "max_input_tokens" && field !== "max_output_tokens") return null;
  if (value.trim() === "") return null;
  if (!/^\d+$/.test(value.trim())) return "config.modelList.tokenNotInteger";
  return null;
}

/** 数字输入更新：空串视为未配置（发送 0） */
const updateModelNumber = (idx: number, field: "max_input_tokens" | "max_output_tokens", raw: string) => {
  const errorKey = getTokenFieldError(field, raw);
  if (errorKey) {
    setLocalError(t(errorKey));
    return;
  }
  setLocalError(null);
  onClearExternalError?.();
  const copy = [...models];
  copy[idx] = { ...copy[idx], [field]: raw.trim() === "" ? 0 : Number(raw.trim()) };
  onModelsChange(copy);
};
```

- [ ] **Step 3: 加表单输入框**

在 `:1145` 的字段元组之后（同一卡片内）追加两个数字输入，样式与相邻字段一致：

```tsx
                  <label className="flex flex-col gap-1">
                    <span className="text-xs text-text-muted">
                      {t("config.modelList.maxInputTokens")}
                    </span>
                    <input
                      type="number"
                      min={0}
                      step={1}
                      inputMode="numeric"
                      className="mono bg-input border border-border rounded px-2 py-1 text-sm"
                      value={models[idx]?.max_input_tokens ?? ""}
                      onChange={(e) => updateModelNumber(idx, "max_input_tokens", e.target.value)}
                      placeholder={t("config.modelList.maxInputTokensPlaceholder")}
                    />
                  </label>
                  <label className="flex flex-col gap-1">
                    <span className="text-xs text-text-muted">
                      {t("config.modelList.maxOutputTokens")}
                    </span>
                    <input
                      type="number"
                      min={0}
                      step={1}
                      inputMode="numeric"
                      className="mono bg-input border border-border rounded px-2 py-1 text-sm"
                      value={models[idx]?.max_output_tokens ?? ""}
                      onChange={(e) => updateModelNumber(idx, "max_output_tokens", e.target.value)}
                      placeholder={t("config.modelList.maxOutputTokensPlaceholder")}
                    />
                  </label>
```

新增模型草稿（`:74-77` 的 `newModel` 与 `resetNewModelDraft`）同样补两个字段默认为 0 的初始化。

- [ ] **Step 4: 加 i18n 文案**

`zh.json` 的 `config.modelList` 段内追加：

```json
"maxInputTokens": "输入上限（token）",
"maxInputTokensPlaceholder": "如 32000，0 或留空表示未配置",
"maxOutputTokens": "输出上限（token）",
"maxOutputTokensPlaceholder": "如 8192，0 或留空表示不限制",
"tokenNotInteger": "请输入非负整数"
```

`en.json` 同 key：

```json
"maxInputTokens": "Max input tokens",
"maxInputTokensPlaceholder": "e.g. 32000; 0 or empty = unset",
"maxOutputTokens": "Max output tokens",
"maxOutputTokensPlaceholder": "e.g. 8192; 0 or empty = unlimited",
"tokenNotInteger": "Please enter a non-negative integer"
```

- [ ] **Step 5: 构建验证并提交**

```bash
cd internal/swarm/channels/web/frontend
npm run build          # 需通过 tsc + vite build
cd -
git add internal/swarm/channels/web/frontend/src/types/index.ts \
        internal/swarm/channels/web/frontend/src/components/ConfigPanel/index.tsx \
        internal/swarm/channels/web/frontend/src/i18n/locales/zh.json \
        internal/swarm/channels/web/frontend/src/i18n/locales/en.json
git commit -m "feat(web/frontend): 模型配置支持输入/输出上限并回显"
```

冒烟（手工，CI 不覆盖）：填 32000/8192 → 保存 → 刷新回显 → 检查 `config.yaml` 落盘 → 填 `-1` 与 `abc` 被拦截并提示。

---

## Task 13: 集成测试四条用例

**Files:**
- Create: `tests/integration/model_token_config/model_token_config_test.go`（`//go:build integration`）

> 目录需满足 `cmd/integration-audit` 两层门禁：真实调用 ≥3 个 internal 二级子系统、有效测试方法 ≥1、断言 ≥2、文件带 `//go:build integration` 且调用 `suite.Run(`。

- [ ] **Step 1: 写用例骨架（先跑失败）**

```go
//go:build integration

package model_token_config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ModelTokenConfigSuite struct {
	suite.Suite
	workspace string
}

func TestModelTokenConfigSuite(t *testing.T) {
	suite.Run(t, new(ModelTokenConfigSuite))
}

// SetupSuite 准备临时工作空间与基础 config.yaml
func (s *ModelTokenConfigSuite) SetupSuite() {
	s.workspace = s.T().TempDir()
	// 写 config.yaml：models.defaults 带 max_input_tokens=32000 / max_output_tokens=8192
	// react.context_engine_config.enabled=true + ratio 0.85/0.55 + 四段
}

// Test写入落盘与回填 覆盖用例 1：models.replace_all → config.yaml → models.list
func (s *ModelTokenConfigSuite) Test写入落盘与回填() {
	s.Require().NotEmpty(s.workspace)
	// 断言：磁盘 YAML 两个键值为 32000/8192；models.list 回填一致
}

// Test装配与显示 覆盖用例 2：deepAgentContextEngineConfig → ResolveContextMax → context.usage
func (s *ModelTokenConfigSuite) Test装配与显示() {
	s.Require().NotEmpty(s.workspace)
	// 断言：context.usage 事件 context_max=32000；chat.usage_summary 的 context_window_tokens=32000
}

// Test阈值联动 覆盖用例 3：rail Init 的阈值 = min(绝对, 0.85×32000)
func (s *ModelTokenConfigSuite) Test阈值联动() {
	s.Require().NotEmpty(s.workspace)
	// 断言：RoundLevel trigger=27200 / target=17600；enabled=false 时不发生压缩
}

// Test输出上限进请求体 覆盖用例 4：buildModelFromEntry → base_client
func (s *ModelTokenConfigSuite) Test输出上限进请求体() {
	s.Require().NotEmpty(s.workspace)
	// httptest 捕获请求体：max_tokens=8192；显式 WithInvokeMaxTokens(1024) 时为 1024
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test -tags=integration ./tests/integration/model_token_config/ -v`
Expected: FAIL（用例体为空断言 / 未实现），并记录实际 API 签名

- [ ] **Step 3: 填充实现**

按下面链路实现四个用例（复用 `tests/integration/suite` 基类与 `mockllm`）：

1. 用例 1：`web.NewRouter` 或直接调用 `web` 包的 handler 注册表 → 发 `models.replace_all` → 读回 `config.yaml`（`gopkg.in/yaml.v3` 解析）→ 发 `models.list`
2. 用例 2：构造 `DeepAdapter`（复用 `suite` 基类的 adapter 构造）→ 调 `CreateInstance` 或 `deepAgentContextEngineConfig` → 从 `context.usage` 事件流取 payload；**再触发一次热重载**（`makeDeepAgentConfig` 路径）后 `context_max` 仍为 32000，覆盖 A1 的「热重载不丢窗口值」
3. 用例 3：取 rail 的 `allProcessors`（通过 `UserProcessorTypes` 与类型断言），断言 `RoundLevelCompressorConfig.TriggerTotalTokens == 27200`
4. 用例 4：`httptest.NewServer` 回显请求体 → `buildModelFromEntry` 造 Model → `Invoke` → 断言 `max_tokens` 字段

- [ ] **Step 4: 运行确认通过**

Run: `go test -tags=integration ./tests/integration/model_token_config/ -v`
Expected: 4 个用例 PASS

- [ ] **Step 5: 本地门禁自检并提交**

```bash
go run ./cmd/integration-audit -verify
git add tests/integration/model_token_config/
git commit -m "test(integration): 模型 token 配置端到端（写入/装配/阈值/请求体）"
```

---

## Task 14: Python 侧同构交付物（P1-P6，跨仓库）

> 本仓库（`uap-claw-go`）无法提交 Python 改动。本任务产出**可直接应用的补丁说明文档**，Python 仓库落地由对应负责人执行。

**Files:**
- Create: `docs/superpowers/specs/2026-11-02-model-token-config-python-patch.md`

- [ ] **Step 1: 写补丁说明**

文档需逐条给出「文件:行 → 改前 → 改后」：

1. `jiuwenswarm/gateway/channel_manager/web/app_web_handlers.py:784-857`：`_build_models_defaults_from_frontend` 白名单追加 `max_input_tokens` / `max_output_tokens`（含非负整数校验）
2. 同文件 `_models_list`（L1023-1055）：回填两字段
3. `jiuwenswarm/server/runtime/agent_adapter/interface_deep.py:309-320`：`_deep_agent_context_engine_config(react_cfg, config_base)` 收集 `model_client_config.max_input_tokens` → `model_context_window_tokens`
4. 同文件 `:1482-1490`：`_build_model_from_entry` 把 `mcc["max_output_tokens"]` 传入 `ModelRequestConfig(max_tokens=...)`
5. 同文件 `:4927-4966` 与 `_emit_context_usage`：分子改 `usage_metadata.input_tokens`，分母取 DeepAgent 返回值
6. `openjiuwen/core/context_engine/processor/context_processor_rail.py`：新增 `compression_trigger_ratio` / `compression_target_ratio` 读取与 `min(绝对, ratio×window)` 收口（顺序在 processor 配置合并之后）
7. `jiuwenswarm/resources/config.yaml:269-303`：追加两个 ratio 键与 `full_compact_processor_config` 段

- [ ] **Step 2: 自查**

逐条确认文档里每个「改后」片段都能被 `python -c "import ast; ast.parse(open(p).read())"` 解析（用 `python3 - <<'EOF'` 批量校验），且不引用仓库中不存在的符号。

- [ ] **Step 3: 提交**

```bash
git add docs/superpowers/specs/2026-11-02-model-token-config-python-patch.md
git commit -m "docs(spec): Python 侧同构改动补丁说明（P1-P6）"
```

---

## Task 15: doc.go 同步与全量回归

**Files:**
- Modify: 涉及的各包 `doc.go`（仅当新增文件时）
- Modify: `IMPLEMENTATION_PLAN.md`（若本次改动对应其中条目）

- [ ] **Step 1: 同步 doc.go**

检查本次新增的测试文件是否需要登记：

```bash
git diff --name-only HEAD~13 | grep '_test.go$'
```

`doc.go` 文件目录**不收录** `_test.go`，若仅新增测试文件则无需改动；若新增了生产文件（如 `compression_ratio.go`，本计划未拆），必须补进目录树。

- [ ] **Step 2: 全量单测**

Run: `go test ./... 2>&1 | tail -30`
Expected: 无 FAIL

- [ ] **Step 3: 覆盖率抽查**

Run: `go test -cover ./internal/agentcore/context_engine/schema/ ./internal/agentcore/harness/rails/context_engineer/ ./internal/swarm/gateway/channel_manager/web/ ./internal/swarm/server/adapter/`
Expected: 各包 ≥85%（或改动行 85%）

- [ ] **Step 4: 集成测试与门禁**

Run: `go test -tags=integration ./tests/integration/model_token_config/ && go run ./cmd/integration-audit -verify`
Expected: PASS

- [ ] **Step 5: 推送并等 CI 四绿**

```bash
git push
gh run list --limit 3
gh run watch <run-id>
```

Expected: Build / Lint / Unit Test / Integration Test 四个 job 全 SUCCESS。红灯则修复后重推，**不得在红灯状态宣告完成**。

---

## 验收清单（汇总）

- [ ] 界面可填输入/输出上限，保存后刷新仍回显
- [ ] `config.yaml` 手改两键生效（重启/热重载后）
- [ ] `context.usage` 的 `context_max` 等于 `max_input_tokens`；分子为 `input_tokens`
- [ ] 32K 模型下 `RoundLevelCompressor` 在 27200 触发、压到 17600
- [ ] `enabled: false` 时压缩不发生（阈值全部休眠）
- [ ] 请求体带 `max_tokens=8192`，显式 `WithInvokeMaxTokens` 仍优先
- [ ] `ContextEngineConfig` 的 `MaxContextMessageNum=200` / `DefaultWindowRoundNum=10` 已恢复
- [ ] 四个 CI job 全绿
