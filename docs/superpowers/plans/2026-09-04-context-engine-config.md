# 5.17 ContextEngineConfig 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现上下文引擎配置结构体 ContextEngineConfig，为后续 5.18-5.31 提供类型定义基础。

**Architecture:** 在 context_engine 包下新建 schema/ 子包，放置 ContextEngineConfig 结构体。采用纯结构体 + Validate() 校验方法，值类型字段 + 默认值表示 Optional（0=不限），NewContextEngineConfig() 构造函数初始化空 map。

**Tech Stack:** Go 标准库（fmt）

---

## 文件结构

```
internal/agentcore/context_engine/
├── doc.go                          # 修改：添加 schema/ 子目录
├── base.go                         # 不变
├── base_test.go                    # 不变
├── token/                          # 不变
│   ├── doc.go
│   └── base.go
└── schema/                         # 新建
    ├── doc.go                      # 包文档
    ├── config.go                   # ContextEngineConfig 定义 + Validate
    └── config_test.go              # 测试
```

---

### Task 1: 创建 schema/ 子包 — doc.go

**Files:**
- Create: `internal/agentcore/context_engine/schema/doc.go`

- [ ] **Step 1: 创建 schema/doc.go**

```go
// Package schema 提供上下文引擎的数据模型定义。
//
// 包含上下文引擎的配置、消息模型、事件模型等数据结构。
// 这些类型由 ContextEngine 和各 Processor 共同使用。
//
// 文件目录：
//
//	schema/
//	├── doc.go       # 包文档
//	└── config.go    # ContextEngineConfig 上下文引擎配置
//
// 对应 Python 代码：openjiuwen/core/context_engine/schema/
package schema
```

- [ ] **Step 2: 更新 context_engine/doc.go，添加 schema/ 子目录**

在文件目录树中 `└── token/` 块之后追加 `schema/` 条目：

```
//	context_engine/
//	├── doc.go           # 包文档
//	├── base.go          # ModelContext 接口 + ContextStats + ContextWindow + ContextEngine 接口
//	│                   # NewContextWindow 构造函数 + StatMessages/StatTools/StatContextWindow 预留方法
//	│                   # ContextEngine 方法直接使用 *session.Session（循环依赖已通过 single_agent/interfaces 解决）
//	│                   # ⤵️ StatMessages/StatTools/StatContextWindow 实际逻辑待 5.31 回填
//	├── schema/
//	│   ├── doc.go       # Schema 子包文档
//	│   └── config.go    # ContextEngineConfig 上下文引擎配置
//	└── token/
//	    ├── doc.go       # Token 子包文档
//	    └── base.go      # TokenCounter 接口定义
```

注意：schema/ 排在 token/ 之前（按核心类型 → 辅助功能排序）。

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_engine/schema/`
Expected: 成功，无输出

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/schema/doc.go internal/agentcore/context_engine/doc.go
git commit -m "feat(context_engine): 添加 schema 子包文档"
```

---

### Task 2: 实现 ContextEngineConfig 结构体 + 构造函数 + Validate

**Files:**
- Create: `internal/agentcore/context_engine/schema/config.go`

- [ ] **Step 1: 创建 config.go**

```go
package schema

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEngineConfig 上下文引擎全局配置，控制消息上限、窗口策略、KV 缓存释放、
// 卸载重载、Token 预算等核心行为。
//
// 配置在 ContextEngine 构造时传入，并在每次 CreateContext 创建 SessionModelContext 时被消费。
// int 字段使用 0 表示"不限"（与 Python None 语义对齐），设置时必须 > 0。
//
// 对应 Python: openjiuwen/core/context_engine/schema/config.py (ContextEngineConfig)
type ContextEngineConfig struct {
	// MaxContextMessageNum 上下文消息数硬上限，0 表示不限
	MaxContextMessageNum int `json:"max_context_message_num"`
	// DefaultWindowMessageNum 滑动窗口默认保留消息数，0 表示不限
	DefaultWindowMessageNum int `json:"default_window_message_num"`
	// DefaultWindowRoundNum 滑动窗口默认保留对话轮数，优先于 DefaultWindowMessageNum，0 表示不限
	DefaultWindowRoundNum int `json:"default_window_round_num"`
	// EnableKVCacheRelease 是否释放卸载消息的 KV-cache 以减少 GPU 显存压力
	EnableKVCacheRelease bool `json:"enable_kv_cache_release"`
	// EnableReload 是否启用自动重载卸载消息（通过 reload 提示符如 [[HANDLE:xxx]]）
	EnableReload bool `json:"enable_reload"`
	// EnableTiktokenCounter 是否启用 tiktoken 计数器
	EnableTiktokenCounter bool `json:"enable_tiktoken_counter"`
	// ContextWindowTokens 模型上下文窗口 Token 数（含输入输出），用于压缩遥测，0 表示不限
	ContextWindowTokens int `json:"context_window_tokens"`
	// ModelName LLM 模型名称，用于从 ModelContextWindowTokens 查找默认上下文窗口大小
	ModelName string `json:"model_name"`
	// ModelContextWindowTokens 模型名称到上下文窗口 Token 数的映射表（最佳努力 fallback），
	// 显式运行时值和 ContextWindowTokens 优先
	ModelContextWindowTokens map[string]int `json:"model_context_window_tokens"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEngineConfig 创建上下文引擎配置，所有字段使用默认值。
// ModelContextWindowTokens 初始化为空 map（非 nil），避免后续使用时需要 nil 检查。
func NewContextEngineConfig() ContextEngineConfig {
	return ContextEngineConfig{
		ModelContextWindowTokens: make(map[string]int),
	}
}

// Validate 校验配置字段合法性。
// int 字段设置时必须 > 0（0 表示不限，不校验）；
// ModelContextWindowTokens 中每个 value 必须 > 0。
func (c ContextEngineConfig) Validate() error {
	if c.MaxContextMessageNum < 0 {
		return fmt.Errorf("max_context_message_num 不能为负数，当前值: %d", c.MaxContextMessageNum)
	}
	if c.DefaultWindowMessageNum < 0 {
		return fmt.Errorf("default_window_message_num 不能为负数，当前值: %d", c.DefaultWindowMessageNum)
	}
	if c.DefaultWindowRoundNum < 0 {
		return fmt.Errorf("default_window_round_num 不能为负数，当前值: %d", c.DefaultWindowRoundNum)
	}
	if c.ContextWindowTokens < 0 {
		return fmt.Errorf("context_window_tokens 不能为负数，当前值: %d", c.ContextWindowTokens)
	}
	for model, tokens := range c.ModelContextWindowTokens {
		if tokens <= 0 {
			return fmt.Errorf("model_context_window_tokens[%q] 必须 > 0，当前值: %d", model, tokens)
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/context_engine/schema/`
Expected: 成功，无输出

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_engine/schema/config.go
git commit -m "feat(context_engine): 实现 ContextEngineConfig 结构体、构造函数和 Validate"
```

---

### Task 3: 编写 ContextEngineConfig 测试

**Files:**
- Create: `internal/agentcore/context_engine/schema/config_test.go`

- [ ] **Step 1: 创建 config_test.go**

```go
package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewContextEngineConfig 测试 NewContextEngineConfig 构造函数
func TestNewContextEngineConfig(t *testing.T) {
	config := NewContextEngineConfig()

	// int 字段默认为 0（不限）
	if config.MaxContextMessageNum != 0 {
		t.Errorf("MaxContextMessageNum 应为 0，实际 %d", config.MaxContextMessageNum)
	}
	if config.DefaultWindowMessageNum != 0 {
		t.Errorf("DefaultWindowMessageNum 应为 0，实际 %d", config.DefaultWindowMessageNum)
	}
	if config.DefaultWindowRoundNum != 0 {
		t.Errorf("DefaultWindowRoundNum 应为 0，实际 %d", config.DefaultWindowRoundNum)
	}
	if config.ContextWindowTokens != 0 {
		t.Errorf("ContextWindowTokens 应为 0，实际 %d", config.ContextWindowTokens)
	}

	// bool 字段默认为 false
	if config.EnableKVCacheRelease {
		t.Error("EnableKVCacheRelease 应为 false")
	}
	if config.EnableReload {
		t.Error("EnableReload 应为 false")
	}
	if config.EnableTiktokenCounter {
		t.Error("EnableTiktokenCounter 应为 false")
	}

	// string 字段默认为空串
	if config.ModelName != "" {
		t.Errorf("ModelName 应为空串，实际 %q", config.ModelName)
	}

	// map 字段应为空 map（非 nil）
	if config.ModelContextWindowTokens == nil {
		t.Error("ModelContextWindowTokens 应为空 map，不应为 nil")
	}
	if len(config.ModelContextWindowTokens) != 0 {
		t.Errorf("ModelContextWindowTokens 长度应为 0，实际 %d", len(config.ModelContextWindowTokens))
	}
}

// TestNewContextEngineConfig_JSON序列化 测试 JSON 序列化和反序列化
func TestNewContextEngineConfig_JSON序列化(t *testing.T) {
	config := NewContextEngineConfig()

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	// model_context_window_tokens 应为空对象而非 null
	mct, ok := parsed["model_context_window_tokens"]
	if !ok {
		t.Error("JSON 中应包含 model_context_window_tokens 字段")
	}
	mctMap, ok := mct.(map[string]any)
	if !ok {
		t.Errorf("model_context_window_tokens 应为对象，实际类型 %T", mct)
	}
	if len(mctMap) != 0 {
		t.Errorf("model_context_window_tokens 应为空对象，实际 %v", mctMap)
	}
}

// TestNewContextEngineConfig_JSON反序列化 测试从 JSON 反序列化配置
func TestNewContextEngineConfig_JSON反序列化(t *testing.T) {
	jsonStr := `{
		"max_context_message_num": 200,
		"default_window_message_num": 50,
		"default_window_round_num": 10,
		"enable_kv_cache_release": true,
		"enable_reload": true,
		"enable_tiktoken_counter": false,
		"context_window_tokens": 128000,
		"model_name": "qwen-max",
		"model_context_window_tokens": {"qwen-max": 32768, "qwen-plus": 131072}
	}`

	var config ContextEngineConfig
	if err := json.Unmarshal([]byte(jsonStr), &config); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}

	if config.MaxContextMessageNum != 200 {
		t.Errorf("MaxContextMessageNum 应为 200，实际 %d", config.MaxContextMessageNum)
	}
	if config.DefaultWindowMessageNum != 50 {
		t.Errorf("DefaultWindowMessageNum 应为 50，实际 %d", config.DefaultWindowMessageNum)
	}
	if config.DefaultWindowRoundNum != 10 {
		t.Errorf("DefaultWindowRoundNum 应为 10，实际 %d", config.DefaultWindowRoundNum)
	}
	if !config.EnableKVCacheRelease {
		t.Error("EnableKVCacheRelease 应为 true")
	}
	if !config.EnableReload {
		t.Error("EnableReload 应为 true")
	}
	if config.EnableTiktokenCounter {
		t.Error("EnableTiktokenCounter 应为 false")
	}
	if config.ContextWindowTokens != 128000 {
		t.Errorf("ContextWindowTokens 应为 128000，实际 %d", config.ContextWindowTokens)
	}
	if config.ModelName != "qwen-max" {
		t.Errorf("ModelName 应为 qwen-max，实际 %q", config.ModelName)
	}
	if len(config.ModelContextWindowTokens) != 2 {
		t.Errorf("ModelContextWindowTokens 长度应为 2，实际 %d", len(config.ModelContextWindowTokens))
	}
	if config.ModelContextWindowTokens["qwen-max"] != 32768 {
		t.Errorf("ModelContextWindowTokens[qwen-max] 应为 32768，实际 %d", config.ModelContextWindowTokens["qwen-max"])
	}
}

// TestContextEngineConfig_Validate_默认值通过 测试默认配置通过校验
func TestContextEngineConfig_Validate_默认值通过(t *testing.T) {
	config := NewContextEngineConfig()
	if err := config.Validate(); err != nil {
		t.Errorf("默认配置应通过校验，实际错误: %v", err)
	}
}

// TestContextEngineConfig_Validate_合法值通过 测试合法配置通过校验
func TestContextEngineConfig_Validate_合法值通过(t *testing.T) {
	config := ContextEngineConfig{
		MaxContextMessageNum:     200,
		DefaultWindowMessageNum:  50,
		DefaultWindowRoundNum:    10,
		EnableKVCacheRelease:     true,
		EnableReload:             true,
		EnableTiktokenCounter:    true,
		ContextWindowTokens:      128000,
		ModelName:                "qwen-max",
		ModelContextWindowTokens: map[string]int{"qwen-max": 32768},
	}
	if err := config.Validate(); err != nil {
		t.Errorf("合法配置应通过校验，实际错误: %v", err)
	}
}

// TestContextEngineConfig_Validate_负数失败 测试 int 字段为负数时校验失败
func TestContextEngineConfig_Validate_负数失败(t *testing.T) {
	tests := []struct {
		name   string
		config ContextEngineConfig
		field  string
	}{
		{
			name: "MaxContextMessageNum 为负",
			config: ContextEngineConfig{MaxContextMessageNum: -1, ModelContextWindowTokens: map[string]int{}},
			field: "max_context_message_num",
		},
		{
			name: "DefaultWindowMessageNum 为负",
			config: ContextEngineConfig{DefaultWindowMessageNum: -1, ModelContextWindowTokens: map[string]int{}},
			field: "default_window_message_num",
		},
		{
			name: "DefaultWindowRoundNum 为负",
			config: ContextEngineConfig{DefaultWindowRoundNum: -1, ModelContextWindowTokens: map[string]int{}},
			field: "default_window_round_num",
		},
		{
			name: "ContextWindowTokens 为负",
			config: ContextEngineConfig{ContextWindowTokens: -1, ModelContextWindowTokens: map[string]int{}},
			field: "context_window_tokens",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err == nil {
				t.Errorf("%s 为负数时应返回错误", tt.field)
			}
		})
	}
}

// TestContextEngineConfig_Validate_mapValue非正失败 测试 ModelContextWindowTokens value <= 0 时校验失败
func TestContextEngineConfig_Validate_mapValue非正失败(t *testing.T) {
	tests := []struct {
		name   string
		config ContextEngineConfig
	}{
		{
			name: "value 为 0",
			config: ContextEngineConfig{
				ModelContextWindowTokens: map[string]int{"qwen-max": 0},
			},
		},
		{
			name: "value 为负数",
			config: ContextEngineConfig{
				ModelContextWindowTokens: map[string]int{"qwen-max": -1},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err == nil {
				t.Error("ModelContextWindowTokens value <= 0 时应返回错误")
			}
		})
	}
}

// TestContextEngineConfig_Validate_mapValue正数通过 测试 ModelContextWindowTokens value > 0 时校验通过
func TestContextEngineConfig_Validate_mapValue正数通过(t *testing.T) {
	config := ContextEngineConfig{
		ModelContextWindowTokens: map[string]int{"qwen-max": 32768, "qwen-plus": 131072},
	}
	if err := config.Validate(); err != nil {
		t.Errorf("ModelContextWindowTokens value > 0 时应通过校验，实际错误: %v", err)
	}
}

// TestContextEngineConfig_值类型字段 测试结构体为值类型，赋值时复制而非共享
func TestContextEngineConfig_值类型字段(t *testing.T) {
	config1 := ContextEngineConfig{
		MaxContextMessageNum:     200,
		ModelContextWindowTokens: map[string]int{"qwen-max": 32768},
	}
	config2 := config1

	// 修改 config2 不影响 config1（值类型复制）
	config2.MaxContextMessageNum = 100
	if config1.MaxContextMessageNum != 200 {
		t.Errorf("值类型赋值应为复制，config1.MaxContextMessageNum 应仍为 200，实际 %d", config1.MaxContextMessageNum)
	}

	// 注意：map 是引用类型，config2.ModelContextWindowTokens 与 config1 共享底层数据
	// 这是 Go 的标准行为，与 Python 的 Pydantic model_copy(update) 语义不同
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test -v ./internal/agentcore/context_engine/schema/ -run TestNewContextEngineConfig -run TestContextEngineConfig`
Expected: 全部 PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/context_engine/schema/`
Expected: 覆盖率 >= 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/schema/config_test.go
git commit -m "test(context_engine): 添加 ContextEngineConfig 测试"
```

---

### Task 4: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` 第 357 行

- [ ] **Step 1: 更新 5.17 状态**

将第 357 行从：
```
| 5.17 | ☐ | ContextEngineConfig | 上下文引擎配置 | `openjiuwen/core/context_engine/schema/config.py` |
```
改为：
```
| 5.17 | ✅ | ContextEngineConfig | 上下文引擎配置；✅ 纯结构体+Validate()校验；✅ NewContextEngineConfig()构造函数初始化空map；✅ schema/子包与Python对齐 | `openjiuwen/core/context_engine/schema/config.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.17 ContextEngineConfig 状态为已完成"
```
