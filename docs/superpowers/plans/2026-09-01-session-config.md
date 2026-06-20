# 5.12 Session Config 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现会话配置体系（SessionConfig），包括常量定义、环境变量管理、工作流/Agent配置索引，并回填所有 ⤵️ 5.12 标记点。

**Architecture:** 先实现 5.13 Session Constants 包，再实现 5.12 Session Config 包（接口在 interfaces/，实现在 config/），最后回填 15 个标记点。环境变量加载三层优先级：os.Getenv > context.Value > 内置默认值。SessionConfig 采用接口+默认实现模式（同 5.9 EntityHooks），通过 BuiltinConfigLoader 钩子支持定制。

**Tech Stack:** Go 1.22+, context.Context, os.Getenv, stretchr/testify

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `session/constants/doc.go` | 常量包文档 |
| `session/constants/constants.go` | 配置键名、环境变量键名、默认值、映射表 |
| `session/constants/constants_test.go` | 常量测试 |
| `session/config/doc.go` | config 包文档 |
| `session/config/config.go` | MetadataLike、BuiltinConfigLoader、defaultSessionConfig、defaultBuiltinConfigLoader |
| `session/config/config_test.go` | config 测试 |
| `session/config/env_loader.go` | trySetEnv、loadEnvConfigs、loadContextEnvs、loadOSEnvs |
| `session/config/env_loader_test.go` | 环境加载测试 |
| `session/config/context.go` | WithEnvs context 注入函数 |
| `session/config/context_test.go` | context 注入测试 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `session/interfaces/interfaces.go` | Config() 返回 SessionConfig；新增 SessionConfig/WorkflowConfigProvider/AgentConfigProvider 接口；移除 CheckpointerConfigProvider |
| `session/internal/agent_session.go` | config any → SessionConfig；WithConfig 参数类型回填；AgentID() 启用 config 逻辑 |
| `session/internal/workflow_session.go` | config any → SessionConfig；无 parent 时创建默认 SessionConfig；NodeConfig() 实现 |
| `session/agent.go` | NewSession config 创建方式回填；GetEnv/GetEnvs 改用 config 方法 |
| `session/workflow.go` | WithWorkflowSessionParent 从 parent.Config().GetEnvs() 获取 |
| `session/node.go` | GetEnv/GetNodeConfig 委托实现 |
| `session/checkpointer/base.go` | 移除 CheckpointerConfigProvider 别名；GetConfigEnv 改为直接调用 Config().GetEnv()；迁移 ForceDelWorkflowStateKey/InteractiveInputKey |
| `session/checkpointer/inmemory.go` | 迁移 ForceDelWorkflowStateKey 引用到 constants 包 |
| `session/checkpointer/persistence.go` | 迁移 ForceDelWorkflowStateKey 引用到 constants 包 |
| `session/tracer/workflow.go` | getWorkflowMetadata 从 config 获取 version/name |
| `session/doc.go` | 更新包文档，添加 constants/config 子包 |

---

## Task 1: 实现 session/constants 包

**Files:**
- Create: `internal/agentcore/session/constants/doc.go`
- Create: `internal/agentcore/session/constants/constants.go`
- Create: `internal/agentcore/session/constants/constants_test.go`

- [ ] **Step 1: 创建 constants/doc.go**

```go
// Package constants 提供会话配置的键名、环境变量键名、默认值和映射表。
//
// 对应 Python 代码：openjiuwen/core/session/constants.py
//
// 文件目录：
//
//	constants/
//	├── doc.go           # 包文档
//	├── constants.go     # 常量定义
//	└── constants_test.go # 常量测试
package constants
```

- [ ] **Step 2: 创建 constants/constants.go**

```go
package constants

// ──────────────────────────── 结构体 ────────────────────────────

// EnvConfigEntry 环境变量到配置键的映射条目
type EnvConfigEntry struct {
	// EnvKey 环境变量键名（如 "WORKFLOW_EXECUTE_TIMEOUT"）
	EnvKey string
	// ConfigKey 配置键名（如 "_execute_timeout"）
	ConfigKey string
}

// ──────────────────────────── 枚举 ────────────────────────────

// EnvConfigType 环境变量值类型枚举
type EnvConfigType int

const (
	// EnvConfigTypeFloat 浮点类型
	EnvConfigTypeFloat EnvConfigType = iota
	// EnvConfigTypeInt 整数类型
	EnvConfigTypeInt
	// EnvConfigTypeBool 布尔类型
	EnvConfigTypeBool
)

// ──────────────────────────── 常量 ────────────────────────────

// 配置键名（对应 Python 配置键，存入 env 字典的 key）
const (
	// WorkflowExecuteTimeoutKey 工作流执行超时
	WorkflowExecuteTimeoutKey = "_execute_timeout"
	// WorkflowStreamFrameTimeoutKey 工作流流帧超时
	WorkflowStreamFrameTimeoutKey = "_stream_frame_timeout"
	// WorkflowStreamFirstFrameTimeoutKey 工作流首帧超时
	WorkflowStreamFirstFrameTimeoutKey = "_stream_first_frame_timeout"
	// CompStreamCallTimeoutKey 组件流调用超时
	CompStreamCallTimeoutKey = "_comp_stream_call_timeout"
	// StreamInputGenTimeoutKey 流输入生成器超时
	StreamInputGenTimeoutKey = "_stream_input_generator_timeout"
	// EndCompTemplateRenderPositionTimeoutKey 终端组件模板渲染位置超时
	EndCompTemplateRenderPositionTimeoutKey = "_end_comp_template_render_position_timeout"
	// EndCompTemplateBranchRenderTimeoutKey 终端组件模板分支渲染超时
	EndCompTemplateBranchRenderTimeoutKey = "_end_comp_template_branch_render_timeout"
	// LoopNumberMaxLimitKey 循环次数上限
	LoopNumberMaxLimitKey = "_loop_number_max_limit"
	// ForceDelWorkflowStateKey 强制删除工作流状态
	ForceDelWorkflowStateKey = "_force_del_workflow_state"
)

// 环境变量键名（对应 Python 的环境变量键名，从 os.Getenv 读取的 key）
const (
	// WorkflowExecuteTimeoutEnvKey 工作流执行超时环境变量
	WorkflowExecuteTimeoutEnvKey = "WORKFLOW_EXECUTE_TIMEOUT"
	// WorkflowStreamFrameTimeoutEnvKey 工作流流帧超时环境变量
	WorkflowStreamFrameTimeoutEnvKey = "WORKFLOW_STREAM_FRAME_TIMEOUT"
	// WorkflowStreamFirstFrameTimeoutEnvKey 工作流首帧超时环境变量
	WorkflowStreamFirstFrameTimeoutEnvKey = "WORKFLOW_STREAM_FIRST_FRAME_TIMEOUT"
	// CompStreamCallTimeoutEnvKey 组件流调用超时环境变量
	CompStreamCallTimeoutEnvKey = "COMP_STREAM_CALL_TIMEOUT"
	// StreamInputGenTimeoutEnvKey 流输入生成器超时环境变量
	StreamInputGenTimeoutEnvKey = "STREAM_INPUT_GEN_TIMEOUT"
	// LoopNumberMaxLimitEnvKey 循环次数上限环境变量
	LoopNumberMaxLimitEnvKey = "LOOP_NUMBER_MAX_LIMIT"
	// ForceDelWorkflowStateEnvKey 强制删除工作流状态环境变量
	ForceDelWorkflowStateEnvKey = "FORCE_DEL_WORKFLOW_STATE"
)

// 默认值
const (
	// WorkflowExecuteTimeoutDefault 工作流执行超时默认值（秒）
	WorkflowExecuteTimeoutDefault = 60.0
	// WorkflowStreamFrameTimeoutDefault 工作流流帧超时默认值（-1 表示不超时）
	WorkflowStreamFrameTimeoutDefault = -1.0
	// WorkflowStreamFirstFrameTimeoutDefault 工作流首帧超时默认值（-1 表示不超时）
	WorkflowStreamFirstFrameTimeoutDefault = -1.0
	// CompStreamCallTimeoutDefault 组件流调用超时默认值（-1 表示不超时）
	CompStreamCallTimeoutDefault = -1.0
	// StreamInputGenTimeoutDefault 流输入生成器超时默认值（-1 表示不超时）
	StreamInputGenTimeoutDefault = -1.0
	// EndCompTemplateRenderPositionTimeoutDefault 终端组件模板渲染位置超时默认值（秒）
	EndCompTemplateRenderPositionTimeoutDefault = 5.0
	// EndCompTemplateBranchRenderTimeoutDefault 终端组件模板分支渲染超时默认值（秒）
	EndCompTemplateBranchRenderTimeoutDefault = 5.0
	// LoopNumberMaxLimitDefault 循环次数上限默认值
	LoopNumberMaxLimitDefault = 1000
	// ForceDelWorkflowStateDefault 强制删除工作流状态默认值
	ForceDelWorkflowStateDefault = false
)

// 交互输入在 session state 中的键（从 checkpointer/base.go 迁移）
const (
	// InteractiveInputKey 交互输入在 session state 中的键。
	// 对应 Python: openjiuwen/core/common/constants/constant.py (INTERACTIVE_INPUT)
	InteractiveInputKey = "__interactive_input__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// EnvConfigKeys 环境变量键到配置键的映射表。
	// 对应 Python: _ENV_CONFIG_KEYS
	EnvConfigKeys = []EnvConfigEntry{
		{WorkflowExecuteTimeoutEnvKey, WorkflowExecuteTimeoutKey},
		{WorkflowStreamFrameTimeoutEnvKey, WorkflowStreamFrameTimeoutKey},
		{WorkflowStreamFirstFrameTimeoutEnvKey, WorkflowStreamFirstFrameTimeoutKey},
		{CompStreamCallTimeoutEnvKey, CompStreamCallTimeoutKey},
		{StreamInputGenTimeoutEnvKey, StreamInputGenTimeoutKey},
		{LoopNumberMaxLimitEnvKey, LoopNumberMaxLimitKey},
		{ForceDelWorkflowStateEnvKey, ForceDelWorkflowStateKey},
	}

	// EnvConfigTypes 环境变量键到值类型的映射表。
	// 对应 Python: _ENV_CONFIG_TYPES
	EnvConfigTypes = map[string]EnvConfigType{
		WorkflowExecuteTimeoutEnvKey:          EnvConfigTypeFloat,
		WorkflowStreamFrameTimeoutEnvKey:      EnvConfigTypeFloat,
		WorkflowStreamFirstFrameTimeoutEnvKey: EnvConfigTypeFloat,
		CompStreamCallTimeoutEnvKey:           EnvConfigTypeFloat,
		StreamInputGenTimeoutEnvKey:           EnvConfigTypeFloat,
		LoopNumberMaxLimitEnvKey:              EnvConfigTypeInt,
		ForceDelWorkflowStateEnvKey:           EnvConfigTypeBool,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuiltinDefaults 返回内置默认配置的完整字典。
// 对应 Python: Config._load_builtin_configs_ 中的 builtin_configs 字典
func BuiltinDefaults() map[string]any {
	return map[string]any{
		CompStreamCallTimeoutKey:                    CompStreamCallTimeoutDefault,
		StreamInputGenTimeoutKey:                    StreamInputGenTimeoutDefault,
		EndCompTemplateBranchRenderTimeoutKey:       EndCompTemplateBranchRenderTimeoutDefault,
		EndCompTemplateRenderPositionTimeoutKey:     EndCompTemplateRenderPositionTimeoutDefault,
		WorkflowExecuteTimeoutKey:                   WorkflowExecuteTimeoutDefault,
		WorkflowStreamFrameTimeoutKey:               WorkflowStreamFrameTimeoutDefault,
		WorkflowStreamFirstFrameTimeoutKey:          WorkflowStreamFirstFrameTimeoutDefault,
		LoopNumberMaxLimitKey:                       LoopNumberMaxLimitDefault,
		ForceDelWorkflowStateKey:                    ForceDelWorkflowStateDefault,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 创建 constants/constants_test.go**

```go
package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuiltinDefaults 测试内置默认配置完整性
func TestBuiltinDefaults(t *testing.T) {
	defaults := BuiltinDefaults()

	// 验证所有配置键都有默认值
	assert.Equal(t, 60.0, defaults[WorkflowExecuteTimeoutKey])
	assert.Equal(t, -1.0, defaults[WorkflowStreamFrameTimeoutKey])
	assert.Equal(t, -1.0, defaults[WorkflowStreamFirstFrameTimeoutKey])
	assert.Equal(t, -1.0, defaults[CompStreamCallTimeoutKey])
	assert.Equal(t, -1.0, defaults[StreamInputGenTimeoutKey])
	assert.Equal(t, 5.0, defaults[EndCompTemplateRenderPositionTimeoutKey])
	assert.Equal(t, 5.0, defaults[EndCompTemplateBranchRenderTimeoutKey])
	assert.Equal(t, 1000, defaults[LoopNumberMaxLimitKey])
	assert.Equal(t, false, defaults[ForceDelWorkflowStateKey])
}

// TestBuiltinDefaults_包含所有配置键 测试默认值字典覆盖所有键
func TestBuiltinDefaults_包含所有配置键(t *testing.T) {
	defaults := BuiltinDefaults()
	expectedKeys := []string{
		WorkflowExecuteTimeoutKey,
		WorkflowStreamFrameTimeoutKey,
		WorkflowStreamFirstFrameTimeoutKey,
		CompStreamCallTimeoutKey,
		StreamInputGenTimeoutKey,
		EndCompTemplateRenderPositionTimeoutKey,
		EndCompTemplateBranchRenderTimeoutKey,
		LoopNumberMaxLimitKey,
		ForceDelWorkflowStateKey,
	}
	assert.Len(t, defaults, len(expectedKeys))
	for _, key := range expectedKeys {
		_, exists := defaults[key]
		assert.True(t, exists, "默认值字典缺少键: %s", key)
	}
}

// TestEnvConfigKeys_映射完整 测试环境变量映射表覆盖所有可配置项
func TestEnvConfigKeys_映射完整(t *testing.T) {
	assert.Len(t, EnvConfigKeys, 7)
	// 验证每个 EnvConfigKey 都有对应的类型定义
	for _, entry := range EnvConfigKeys {
		_, exists := EnvConfigTypes[entry.EnvKey]
		assert.True(t, exists, "EnvConfigTypes 缺少键: %s", entry.EnvKey)
	}
}

// TestEnvConfigTypes_类型正确 测试环境变量类型映射
func TestEnvConfigTypes_类型正确(t *testing.T) {
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[WorkflowExecuteTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[WorkflowStreamFrameTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[WorkflowStreamFirstFrameTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[CompStreamCallTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeFloat, EnvConfigTypes[StreamInputGenTimeoutEnvKey])
	assert.Equal(t, EnvConfigTypeInt, EnvConfigTypes[LoopNumberMaxLimitEnvKey])
	assert.Equal(t, EnvConfigTypeBool, EnvConfigTypes[ForceDelWorkflowStateEnvKey])
}

// TestInteractiveInputKey_值正确 测试交互输入键的值与 Python 一致
func TestInteractiveInputKey_值正确(t *testing.T) {
	assert.Equal(t, "__interactive_input__", InteractiveInputKey)
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/constants/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/constants/
git commit -m "feat(session): 实现 5.13 Session Constants 包

- 配置键名常量（WorkflowExecuteTimeoutKey 等 9 个）
- 环境变量键名常量（WorkflowExecuteTimeoutEnvKey 等 7 个）
- 默认值常量（对齐 Python _load_builtin_configs_ 中的默认值）
- InteractiveInputKey 从 checkpointer/base.go 迁移
- EnvConfigKeys 映射表 + EnvConfigTypes 类型映射
- BuiltinDefaults() 函数返回内置默认配置完整字典"
```

---

## Task 2: 实现 session/config 包 — 接口与核心类型

**Files:**
- Create: `internal/agentcore/session/config/doc.go`
- Create: `internal/agentcore/session/config/config.go`
- Modify: `internal/agentcore/session/interfaces/interfaces.go`

- [ ] **Step 1: 在 interfaces/interfaces.go 中添加 SessionConfig、WorkflowConfigProvider、AgentConfigProvider 接口，修改 Config() 返回类型，移除 CheckpointerConfigProvider**

在 `interfaces.go` 中：

1. 在 `BaseSession` 接口前添加 `SessionConfig`、`WorkflowConfigProvider`、`AgentConfigProvider` 接口定义
2. 修改 `BaseSession.Config()` 返回类型从 `any` 改为 `SessionConfig`
3. 移除 `CheckpointerConfigProvider` 接口

变更摘要：

```go
// SessionConfig 会话配置接口。
// 对应 Python: openjiuwen/core/session/config/base.py (Config)
type SessionConfig interface {
	// GetEnv 获取环境变量值
	GetEnv(key string, defaultValue ...any) any
	// GetEnvs 获取所有环境变量（深拷贝）
	GetEnvs() map[string]any
	// SetEnvs 合并环境变量
	SetEnvs(envs map[string]any)
	// GetWorkflowConfig 按 workflowID 获取工作流配置
	GetWorkflowConfig(workflowID string) WorkflowConfigProvider
	// GetAgentConfig 获取 Agent 配置
	GetAgentConfig() AgentConfigProvider
	// SetAgentConfig 设置 Agent 配置
	SetAgentConfig(agentConfig AgentConfigProvider)
	// AddWorkflowConfig 添加工作流配置
	AddWorkflowConfig(workflowID string, workflowConfig WorkflowConfigProvider)
}

// WorkflowConfigProvider 工作流配置提供者接口。
// ⤵️ 8.15 回填：WorkflowConfig 实现后替换为具体类型
type WorkflowConfigProvider interface{}

// AgentConfigProvider Agent 配置提供者接口。
// ⤵️ 6.3 回填：AgentConfig 实现后替换为具体类型
type AgentConfigProvider interface {
	// ID 获取 Agent ID
	ID() string
}
```

将 `BaseSession` 中的 `Config() any` 改为 `Config() SessionConfig`，删除 `⤵️ 5.12 回填` 注释。

删除 `CheckpointerConfigProvider` 接口及其注释。

- [ ] **Step 2: 创建 config/doc.go**

```go
// Package config 提供会话配置的具体实现。
//
// 对应 Python 代码：openjiuwen/core/session/config/base.py
//
// 核心类型：
//   - MetadataLike         — 回调元数据结构体
//   - BuiltinConfigLoader  — 内置配置加载钩子接口
//   - defaultSessionConfig — SessionConfig 的默认实现
//
// 文件目录：
//
//	config/
//	├── doc.go            # 包文档
//	├── config.go         # MetadataLike、BuiltinConfigLoader、defaultSessionConfig
//	├── env_loader.go     # 环境变量加载（trySetEnv、loadEnvConfigs 等）
//	├── context.go        # WithEnvs context 注入函数
//	├── config_test.go    # config 测试
//	├── env_loader_test.go # 环境加载测试
//	└── context_test.go   # context 注入测试
package config
```

- [ ] **Step 3: 创建 config/context.go**

```go
package config

import (
	"context"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// envsContextKey 请求级环境变量的 context key
	envsContextKey = "session_envs"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithEnvs 将请求级环境变量注入到 context 中。
// 优先级：os.Getenv > context.Value > 内置默认值。
// 对应 Python: workflow_session_vars (contextvars.ContextVar)
func WithEnvs(ctx context.Context, envs map[string]any) context.Context {
	return context.WithValue(ctx, envsContextKey, envs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getEnvsFromContext 从 context 中获取请求级环境变量
func getEnvsFromContext(ctx context.Context) map[string]any {
	if envs, ok := ctx.Value(envsContextKey).(map[string]any); ok {
		return envs
	}
	return nil
}
```

- [ ] **Step 4: 创建 config/env_loader.go**

```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// trySetEnv 尝试将环境变量值设置到 envs 字典中，根据类型映射进行转换。
// 对应 Python: _try_set_env
func trySetEnv(envs map[string]any, configKey, envKey string, value any) {
	if value == nil {
		return
	}
	envType, exists := constants.EnvConfigTypes[envKey]
	if !exists {
		// 无类型映射，直接设置
		envs[configKey] = value
		return
	}

	switch envType {
	case constants.EnvConfigTypeFloat:
		trySetFloat(envs, configKey, envKey, value)
	case constants.EnvConfigTypeInt:
		trySetInt(envs, configKey, envKey, value)
	case constants.EnvConfigTypeBool:
		trySetBool(envs, configKey, envKey, value)
	default:
		envs[configKey] = value
	}
}

// trySetFloat 尝试将值转换为 float64 并设置
func trySetFloat(envs map[string]any, configKey, envKey string, value any) {
	switch v := value.(type) {
	case float64:
		envs[configKey] = v
	case float32:
		envs[configKey] = float64(v)
	case int:
		envs[configKey] = float64(v)
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			logger.Warn(logComponent).
				Str("env_key", envKey).
				Str("value", v).
				Str("expected_type", "float").
				Msg("环境变量 float 转换失败，使用默认值")
			return
		}
		envs[configKey] = f
	default:
		logger.Warn(logComponent).
			Str("env_key", envKey).
			Str("value_type", fmt.Sprintf("%T", value)).
			Str("expected_type", "float").
			Msg("环境变量 float 转换失败，使用默认值")
	}
}

// trySetInt 尝试将值转换为 int 并设置
func trySetInt(envs map[string]any, configKey, envKey string, value any) {
	switch v := value.(type) {
	case int:
		envs[configKey] = v
	case string:
		i, err := strconv.Atoi(v)
		if err != nil {
			logger.Warn(logComponent).
				Str("env_key", envKey).
				Str("value", v).
				Str("expected_type", "int").
				Msg("环境变量 int 转换失败，使用默认值")
			return
		}
		envs[configKey] = i
	default:
		logger.Warn(logComponent).
			Str("env_key", envKey).
			Str("value_type", fmt.Sprintf("%T", value)).
			Str("expected_type", "int").
			Msg("环境变量 int 转换失败，使用默认值")
	}
}

// trySetBool 尝试将值转换为 bool 并设置
func trySetBool(envs map[string]any, configKey, envKey string, value any) {
	switch v := value.(type) {
	case bool:
		envs[configKey] = v
	case string:
		lower := strings.ToLower(v)
		if lower != "true" && lower != "false" {
			logger.Warn(logComponent).
				Str("env_key", envKey).
				Str("value", v).
				Str("expected_type", "bool").
				Msg("环境变量 bool 转换失败，使用默认值")
			return
		}
		envs[configKey] = lower == "true"
	default:
		logger.Warn(logComponent).
			Str("env_key", envKey).
			Str("value_type", fmt.Sprintf("%T", value)).
			Str("expected_type", "bool").
			Msg("环境变量 bool 转换失败，使用默认值")
	}
}

// loadEnvConfigs 从 os.Getenv 和 context 加载环境变量配置。
// 对应 Python: _load_env_configs
// 优先级：os.Getenv > context.Value > 内置默认值
func loadEnvConfigs(ctx context.Context) map[string]any {
	envConfigs := make(map[string]any)

	for _, entry := range constants.EnvConfigKeys {
		// 先从 os.Getenv 读取
		if osValue := os.Getenv(entry.EnvKey); osValue != "" {
			trySetEnv(envConfigs, entry.ConfigKey, entry.EnvKey, osValue)
		}
		// 再从 context.Value 读取（os.Getenv 优先级更高，如果已经设置则跳过）
		if _, exists := envConfigs[entry.ConfigKey]; exists {
			continue
		}
		if ctxEnvs := getEnvsFromContext(ctx); ctxEnvs != nil {
			if ctxValue, ok := ctxEnvs[entry.EnvKey]; ok {
				trySetEnv(envConfigs, entry.ConfigKey, entry.EnvKey, ctxValue)
			}
		}
	}

	return envConfigs
}
```

- [ ] **Step 5: 创建 config/config.go**

```go
package config

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MetadataLike 回调元数据结构体。
// 对应 Python: openjiuwen/core/session/config/base.py (MetadataLike TypedDict)
type MetadataLike struct {
	// Name 名称
	Name string
	// Event 事件
	Event string
}

// BuiltinConfigLoader 内置配置加载钩子接口。
// 对应 Python: Config._load_builtin_configs_
// Go 不支持虚方法分派，通过接口注入实现模板方法模式（同 5.9 EntityHooks 模式）。
type BuiltinConfigLoader interface {
	// LoadBuiltinConfigs 加载内置默认配置到 envs 字典
	LoadBuiltinConfigs(envs map[string]any)
}

// defaultSessionConfig SessionConfig 的默认实现。
// 对应 Python: openjiuwen/core/session/config/base.py (Config)
type defaultSessionConfig struct {
	// env 环境变量字典
	env map[string]any
	// callbackMetadata 回调元数据（预留，后续回调系统实现时回填）
	callbackMetadata map[string]MetadataLike
	// workflowConfigs 按 workflowID 索引的工作流配置
	workflowConfigs map[string]interfaces.WorkflowConfigProvider
	// agentConfig Agent 配置
	agentConfig interfaces.AgentConfigProvider
	// loader 内置配置加载钩子
	loader BuiltinConfigLoader
}

// defaultBuiltinConfigLoader 默认内置配置加载器
type defaultBuiltinConfigLoader struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 确保 defaultSessionConfig 实现 SessionConfig 接口
var _ interfaces.SessionConfig = (*defaultSessionConfig)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionConfig 创建默认 SessionConfig 实例。
// 对应 Python: Config()
func NewSessionConfig(ctx context.Context) *defaultSessionConfig {
	return NewSessionConfigWithLoader(ctx, &defaultBuiltinConfigLoader{})
}

// NewSessionConfigWithLoader 创建注入自定义 loader 的 SessionConfig 实例。
func NewSessionConfigWithLoader(ctx context.Context, loader BuiltinConfigLoader) *defaultSessionConfig {
	cfg := &defaultSessionConfig{
		env:              make(map[string]any),
		callbackMetadata: make(map[string]MetadataLike),
		workflowConfigs:  make(map[string]interfaces.WorkflowConfigProvider),
		loader:           loader,
	}
	cfg.loadEnvs(ctx)
	return cfg
}

// GetEnv 获取环境变量值。
// 对应 Python: Config.get_env(key, default)
func (c *defaultSessionConfig) GetEnv(key string, defaultValue ...any) any {
	if v, exists := c.env[key]; exists {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// GetEnvs 获取所有环境变量（深拷贝）。
// 对应 Python: Config.get_envs() → deepcopy(self._env)
func (c *defaultSessionConfig) GetEnvs() map[string]any {
	result := make(map[string]any, len(c.env))
	for k, v := range c.env {
		result[k] = v
	}
	return result
}

// SetEnvs 合并环境变量。
// 对应 Python: Config.set_envs(envs)
func (c *defaultSessionConfig) SetEnvs(envs map[string]any) {
	if envs == nil {
		return
	}
	for k, v := range envs {
		c.env[k] = v
	}
}

// GetWorkflowConfig 按 workflowID 获取工作流配置。
// 对应 Python: Config.get_workflow_config(workflow_id)
func (c *defaultSessionConfig) GetWorkflowConfig(workflowID string) interfaces.WorkflowConfigProvider {
	if workflowID == "" {
		return nil
	}
	return c.workflowConfigs[workflowID]
}

// GetAgentConfig 获取 Agent 配置。
// 对应 Python: Config.get_agent_config()
func (c *defaultSessionConfig) GetAgentConfig() interfaces.AgentConfigProvider {
	return c.agentConfig
}

// SetAgentConfig 设置 Agent 配置。
// 对应 Python: Config.set_agent_config(agent_config)
func (c *defaultSessionConfig) SetAgentConfig(agentConfig interfaces.AgentConfigProvider) {
	c.agentConfig = agentConfig
}

// AddWorkflowConfig 添加工作流配置。
// 对应 Python: Config.add_workflow_config(workflow_id, workflow_config)
func (c *defaultSessionConfig) AddWorkflowConfig(workflowID string, workflowConfig interfaces.WorkflowConfigProvider) {
	if workflowID == "" {
		return
	}
	if workflowConfig == nil {
		return
	}
	c.workflowConfigs[workflowID] = workflowConfig
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadEnvs 加载环境变量配置。
// 对应 Python: Config._load_envs_()
// 三层优先级：os.Getenv > context.Value > 内置默认值
func (c *defaultSessionConfig) loadEnvs(ctx context.Context) {
	// 1. 加载内置默认值
	c.loader.LoadBuiltinConfigs(c.env)
	// 2. 从 context.Value 和 os.Getenv 覆盖（优先级更高）
	envConfigs := loadEnvConfigs(ctx)
	for k, v := range envConfigs {
		c.env[k] = v
	}
}

// LoadBuiltinConfigs 默认加载器实现。
// 对应 Python: Config._load_builtin_configs_
func (l *defaultBuiltinConfigLoader) LoadBuiltinConfigs(envs map[string]any) {
	defaults := constants.BuiltinDefaults()
	for k, v := range defaults {
		envs[k] = v
	}
}
```

- [ ] **Step 6: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`
Expected: 编译通过

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/session/config/ internal/agentcore/session/interfaces/interfaces.go
git commit -m "feat(session): 实现 5.12 SessionConfig 接口与默认实现

- interfaces 包新增 SessionConfig/WorkflowConfigProvider/AgentConfigProvider 接口
- BaseSession.Config() 返回类型从 any 改为 SessionConfig
- 移除 CheckpointerConfigProvider 过渡接口
- config 包实现 defaultSessionConfig + BuiltinConfigLoader 钩子
- context.go 实现 WithEnvs() context 注入
- env_loader.go 实现 trySetEnv/loadEnvConfigs 三层优先级加载"
```

---

## Task 3: 实现 session/config 包测试

**Files:**
- Create: `internal/agentcore/session/config/config_test.go`
- Create: `internal/agentcore/session/config/env_loader_test.go`
- Create: `internal/agentcore/session/config/context_test.go`

- [ ] **Step 1: 创建 config/context_test.go**

```go
package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWithEnvs_注入环境变量 测试 WithEnvs 注入 context
func TestWithEnvs_注入环境变量(t *testing.T) {
	ctx := context.Background()
	envs := map[string]any{"KEY": "value"}

	newCtx := WithEnvs(ctx, envs)

	// 验证可以从 context 中取回
	result := getEnvsFromContext(newCtx)
	assert.Equal(t, envs, result)
}

// TestWithEnvs_空map 测试注入空 map
func TestWithEnvs_空map(t *testing.T) {
	ctx := context.Background()
	envs := map[string]any{}

	newCtx := WithEnvs(ctx, envs)

	result := getEnvsFromContext(newCtx)
	assert.Equal(t, envs, result)
}

// TestGetEnvsFromContext_无注入 测试无注入时返回 nil
func TestGetEnvsFromContext_无注入(t *testing.T) {
	ctx := context.Background()

	result := getEnvsFromContext(ctx)
	assert.Nil(t, result)
}
```

- [ ] **Step 2: 创建 config/env_loader_test.go**

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
)

// TestTrySetEnv_float类型 测试 float 类型转换
func TestTrySetEnv_float类型(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.WorkflowExecuteTimeoutKey, constants.WorkflowExecuteTimeoutEnvKey, "120.5")

	assert.Equal(t, 120.5, envs[constants.WorkflowExecuteTimeoutKey])
}

// TestTrySetEnv_int类型 测试 int 类型转换
func TestTrySetEnv_int类型(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.LoopNumberMaxLimitKey, constants.LoopNumberMaxLimitEnvKey, "500")

	assert.Equal(t, 500, envs[constants.LoopNumberMaxLimitKey])
}

// TestTrySetEnv_bool类型_true 测试 bool 类型转换 true
func TestTrySetEnv_bool类型_true(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.ForceDelWorkflowStateKey, constants.ForceDelWorkflowStateEnvKey, "true")

	assert.Equal(t, true, envs[constants.ForceDelWorkflowStateKey])
}

// TestTrySetEnv_bool类型_false 测试 bool 类型转换 false
func TestTrySetEnv_bool类型_false(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.ForceDelWorkflowStateKey, constants.ForceDelWorkflowStateEnvKey, "false")

	assert.Equal(t, false, envs[constants.ForceDelWorkflowStateKey])
}

// TestTrySetEnv_nil值跳过 测试 nil 值不设置
func TestTrySetEnv_nil值跳过(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, "key", "env_key", nil)

	_, exists := envs["key"]
	assert.False(t, exists)
}

// TestTrySetEnv_无效float跳过 测试无效 float 值跳过
func TestTrySetEnv_无效float跳过(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.WorkflowExecuteTimeoutKey, constants.WorkflowExecuteTimeoutEnvKey, "not_a_number")

	_, exists := envs[constants.WorkflowExecuteTimeoutKey]
	assert.False(t, exists)
}

// TestTrySetEnv_无效bool跳过 测试无效 bool 值跳过
func TestTrySetEnv_无效bool跳过(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.ForceDelWorkflowStateKey, constants.ForceDelWorkflowStateEnvKey, "yes")

	_, exists := envs[constants.ForceDelWorkflowStateKey]
	assert.False(t, exists)
}

// TestTrySetEnv_直接float64值 测试直接传入 float64
func TestTrySetEnv_直接float64值(t *testing.T) {
	envs := make(map[string]any)

	trySetEnv(envs, constants.WorkflowExecuteTimeoutKey, constants.WorkflowExecuteTimeoutEnvKey, 99.5)

	assert.Equal(t, 99.5, envs[constants.WorkflowExecuteTimeoutKey])
}
```

- [ ] **Step 3: 创建 config/config_test.go**

```go
package config

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
)

// TestNewSessionConfig_默认值加载 测试默认配置加载
func TestNewSessionConfig_默认值加载(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 验证内置默认值
	assert.Equal(t, 60.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.WorkflowStreamFrameTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.WorkflowStreamFirstFrameTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.CompStreamCallTimeoutKey))
	assert.Equal(t, -1.0, cfg.GetEnv(constants.StreamInputGenTimeoutKey))
	assert.Equal(t, 5.0, cfg.GetEnv(constants.EndCompTemplateRenderPositionTimeoutKey))
	assert.Equal(t, 5.0, cfg.GetEnv(constants.EndCompTemplateBranchRenderTimeoutKey))
	assert.Equal(t, 1000, cfg.GetEnv(constants.LoopNumberMaxLimitKey))
	assert.Equal(t, false, cfg.GetEnv(constants.ForceDelWorkflowStateKey))
}

// TestNewSessionConfig_GetEnv默认值 测试 GetEnv 的 defaultValue 参数
func TestNewSessionConfig_GetEnv默认值(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 不存在的键返回 defaultValue
	assert.Equal(t, "fallback", cfg.GetEnv("nonexistent_key", "fallback"))
	// 不存在的键无 defaultValue 返回 nil
	assert.Nil(t, cfg.GetEnv("nonexistent_key"))
}

// TestNewSessionConfig_SetEnvs 测试 SetEnvs 合并环境变量
func TestNewSessionConfig_SetEnvs(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	cfg.SetEnvs(map[string]any{
		constants.WorkflowExecuteTimeoutKey: 120.0,
		"custom_key":                        "custom_value",
	})

	assert.Equal(t, 120.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
	assert.Equal(t, "custom_value", cfg.GetEnv("custom_key"))
}

// TestNewSessionConfig_SetEnvs_nil 测试 SetEnvs 传入 nil 不 panic
func TestNewSessionConfig_SetEnvs_nil(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	assert.NotPanics(t, func() {
		cfg.SetEnvs(nil)
	})
}

// TestNewSessionConfig_GetEnvs深拷贝 测试 GetEnvs 返回深拷贝
func TestNewSessionConfig_GetEnvs深拷贝(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	envs := cfg.GetEnvs()
	envs[constants.WorkflowExecuteTimeoutKey] = 999.0

	// 原始值不受影响
	assert.Equal(t, 60.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
}

// TestNewSessionConfig_WorkflowConfig 测试工作流配置的存取
func TestNewSessionConfig_WorkflowConfig(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 无配置时返回 nil
	assert.Nil(t, cfg.GetWorkflowConfig("wf1"))

	// 添加配置
	cfg.AddWorkflowConfig("wf1", nil) // nil WorkflowConfigProvider 也跳过
	assert.Nil(t, cfg.GetWorkflowConfig("wf1"))

	// 空字符串 workflowID 跳过
	cfg.AddWorkflowConfig("", nil)
}

// TestNewSessionConfig_AgentConfig 测试 Agent 配置的存取
func TestNewSessionConfig_AgentConfig(t *testing.T) {
	cfg := NewSessionConfig(context.Background())

	// 无配置时返回 nil
	assert.Nil(t, cfg.GetAgentConfig())
}

// TestNewSessionConfig_ContextEnvs 测试 context 注入环境变量
func TestNewSessionConfig_ContextEnvs(t *testing.T) {
	ctx := context.Background()
	ctx = WithEnvs(ctx, map[string]any{
		constants.WorkflowExecuteTimeoutEnvKey: 200.0,
	})

	cfg := NewSessionConfig(ctx)

	assert.Equal(t, 200.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
}

// TestNewSessionConfigWithLoader_自定义Loader 测试自定义 loader
func TestNewSessionConfigWithLoader_自定义Loader(t *testing.T) {
	loader := &testConfigLoader{}
	cfg := NewSessionConfigWithLoader(context.Background(), loader)

	assert.Equal(t, "from_test_loader", cfg.GetEnv("test_key"))
	assert.Equal(t, 60.0, cfg.GetEnv(constants.WorkflowExecuteTimeoutKey))
}

// testConfigLoader 测试用自定义 loader
type testConfigLoader struct{}

func (l *testConfigLoader) LoadBuiltinConfigs(envs map[string]any) {
	defaults := constants.BuiltinDefaults()
	for k, v := range defaults {
		envs[k] = v
	}
	envs["test_key"] = "from_test_loader"
}
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/config/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/config/
git commit -m "test(session): 添加 SessionConfig 单元测试

- context_test: WithEnvs 注入和读取测试
- env_loader_test: trySetEnv 各类型转换测试
- config_test: 默认值加载、SetEnvs、GetEnvs 深拷贝、WorkflowConfig/AgentConfig 存取、自定义 loader 测试"
```

---

## Task 4: 回填 interfaces 层 — 移除 CheckpointerConfigProvider + 更新 checkpointer/base.go

**Files:**
- Modify: `internal/agentcore/session/checkpointer/base.go`
- Modify: `internal/agentcore/session/checkpointer/inmemory.go`
- Modify: `internal/agentcore/session/checkpointer/persistence.go`

- [ ] **Step 1: 修改 checkpointer/base.go**

1. 删除 `CheckpointerConfigProvider` 类型别名（第 32 行）
2. 修改 `GetConfigEnv` 函数，改为直接调用 `session.Config().GetEnv()`
3. 将 `ForceDelWorkflowStateKey` 和 `InteractiveInputKey` 常量改为引用 `constants` 包

变更摘要：

```go
// 删除这两行：
// CheckpointerConfigProvider = interfaces.CheckpointerConfigProvider

// 修改 ForceDelWorkflowStateKey 和 InteractiveInputKey：
// 1. 删除 base.go 中的 ForceDelWorkflowStateKey 和 InteractiveInputKey 常量定义
// 2. 改为引用 constants 包

// 修改 GetConfigEnv：
func GetConfigEnv(session interfaces.BaseSession, key string, defaultValue ...any) (any, bool) {
	cfg := session.Config()
	if cfg == nil {
		return nil, false
	}
	return cfg.GetEnv(key, defaultValue...), true
}
```

- [ ] **Step 2: 修改 checkpointer/inmemory.go**

1. 将所有 `ForceDelWorkflowStateKey` 引用改为 `constants.ForceDelWorkflowStateKey`
2. 将所有 `InteractiveInputKey` 引用改为 `constants.InteractiveInputKey`
3. 添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"` 导入

- [ ] **Step 3: 修改 checkpointer/persistence.go**

1. 将所有 `ForceDelWorkflowStateKey` 引用改为 `constants.ForceDelWorkflowStateKey`
2. 添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"` 导入

- [ ] **Step 4: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`
Expected: 编译通过

- [ ] **Step 5: 运行 checkpointer 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/checkpointer/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/checkpointer/
git commit -m "refactor(session): 移除 CheckpointerConfigProvider，GetConfigEnv 改用 SessionConfig

- 删除 CheckpointerConfigProvider 类型别名（已在 interfaces 中移除）
- GetConfigEnv 改为直接调用 session.Config().GetEnv()
- ForceDelWorkflowStateKey/InteractiveInputKey 迁移到 constants 包"
```

---

## Task 5: 回填 internal 层 — AgentSession 和 WorkflowSession

**Files:**
- Modify: `internal/agentcore/session/internal/agent_session.go`
- Modify: `internal/agentcore/session/internal/workflow_session.go`

- [ ] **Step 1: 修改 agent_session.go**

1. `config any` 字段 → `config interfaces.SessionConfig`
2. `WithConfig(config any)` → `WithConfig(config interfaces.SessionConfig)`
3. `Config() any` → `Config() interfaces.SessionConfig`
4. `AgentID()` 启用从 config 获取 agent_config.id 的逻辑

变更摘要：

```go
// 字段修改：
config interfaces.SessionConfig

// 选项函数修改：
func WithConfig(config interfaces.SessionConfig) AgentSessionOption {

// 方法修改：
func (s *AgentSession) Config() interfaces.SessionConfig {
    return s.config
}

// AgentID() 启用注释掉的逻辑：
func (s *AgentSession) AgentID() string {
    if s.config != nil {
        if ac := s.config.GetAgentConfig(); ac != nil {
            if id := ac.ID(); id != "" {
                return id
            }
        }
    }
    if s.card != nil {
        return s.card.AbilityID()
    }
    return ""
}
```

- [ ] **Step 2: 修改 workflow_session.go**

1. `config any` 字段 → `config interfaces.SessionConfig`
2. `Config() any` → `Config() interfaces.SessionConfig`
3. 无 parent 时创建 `NewSessionConfig()`
4. `NodeConfig()` 实现 `get_workflow_config` 逻辑

变更摘要：

```go
// 字段修改：
config interfaces.SessionConfig

// 方法修改：
func (s *WorkflowSession) Config() interfaces.SessionConfig {
    return s.config
}

// NewWorkflowSession 中取消注释默认 config 创建：
if s.config == nil {
    s.config = config.NewSessionConfig(context.Background())
}

// NodeConfig() 实现真实逻辑：
func (n *NodeSession) NodeConfig() any {
    cfg := n.delegate.Config()
    if cfg == nil {
        return nil
    }
    wfc := cfg.GetWorkflowConfig(n.workflowID)
    if wfc == nil {
        return nil
    }
    // ⤵️ 8.15 回填：WorkflowConfig 实现后，从 spec.comp_configs 获取 nodeID 对应的配置
    return nil
}
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/internal/
git commit -m "feat(session): 回填 AgentSession/WorkflowSession Config 类型

- AgentSession.config: any → SessionConfig
- AgentSession.AgentID(): 启用 config.GetAgentConfig().ID() 优先级链
- WorkflowSession.config: any → SessionConfig
- WorkflowSession 无 parent 时创建 NewSessionConfig()
- NodeSession.NodeConfig(): 实现 GetWorkflowConfig 查询骨架"
```

---

## Task 6: 回填公开层 — agent.go, workflow.go, node.go

**Files:**
- Modify: `internal/agentcore/session/agent.go`
- Modify: `internal/agentcore/session/workflow.go`
- Modify: `internal/agentcore/session/node.go`

- [ ] **Step 1: 修改 agent.go**

1. `NewSession` 中 config 创建改为 `NewSessionConfig() + SetEnvs()`
2. `GetEnv()` 改为调用 `config.GetEnv()`
3. `GetEnvs()` 改为调用 `config.GetEnvs()`

变更摘要：

```go
// NewSession 中替换：
// 旧：config := any(s.envs)
// 新：
cfg := config.NewSessionConfig(context.Background())
if len(s.envs) > 0 {
    cfg.SetEnvs(s.envs)
}

// WithConfig 参数类型已变，无需额外修改
s.inner = internal.NewAgentSession(s.sessionID,
    internal.WithConfig(cfg),
    // ...
)

// GetEnv 替换为：
func (s *Session) GetEnv(key string, defaultValue ...any) any {
    cfg := s.inner.Config()
    if cfg == nil {
        return nil
    }
    return cfg.GetEnv(key, defaultValue...)
}

// GetEnvs 替换为：
func (s *Session) GetEnvs() map[string]any {
    cfg := s.inner.Config()
    if cfg == nil {
        return nil
    }
    return cfg.GetEnvs()
}
```

- [ ] **Step 2: 修改 workflow.go**

1. `WithWorkflowSessionParent` 从 `parent.Config().GetEnvs()` 获取 envs

变更摘要：

```go
func WithWorkflowSessionParent(parent BaseSession) WorkflowSessionOption {
    return func(ws *WorkflowSession) {
        var envs map[string]any
        if parent != nil {
            envs = parent.Config().GetEnvs()
        }
        if envs == nil {
            envs = make(map[string]any)
        }
        ws.envs = envs
    }
}
```

- [ ] **Step 3: 修改 node.go**

1. `GetEnv()` 委托 `inner.Config().GetEnv(key)`
2. `GetNodeConfig()` 委托 `inner.NodeConfig()`

变更摘要：

```go
func (f *NodeSessionFacade) GetEnv(key string) any {
    cfg := f.inner.Config()
    if cfg == nil {
        return nil
    }
    return cfg.GetEnv(key)
}

func (f *NodeSessionFacade) GetNodeConfig() any {
    return f.inner.NodeConfig()
}
```

- [ ] **Step 4: 运行编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/...`
Expected: 编译通过

- [ ] **Step 5: 运行全部 session 测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/agent.go internal/agentcore/session/workflow.go internal/agentcore/session/node.go
git commit -m "feat(session): 回填公开层 Config 类型

- agent.go: NewSession 改用 NewSessionConfig() + SetEnvs()；GetEnv/GetEnvs 委托 config 方法
- workflow.go: WithWorkflowSessionParent 从 parent.Config().GetEnvs() 获取
- node.go: GetEnv/GetNodeConfig 委托 inner Config 方法"
```

---

## Task 7: 回填 tracer/workflow.go — getWorkflowMetadata

**Files:**
- Modify: `internal/agentcore/session/tracer/workflow.go`

- [ ] **Step 1: 修改 getWorkflowMetadata**

从 config 获取 WorkflowCard 提取 version 和 name。

变更摘要：

```go
func getWorkflowMetadata(session BaseWorkflowSession) map[string]any {
    workflowID := session.WorkflowID()
    metadata := map[string]any{
        "workflow_id":      workflowID,
        "workflow_version": "",
        "workflow_name":    "",
    }

    cfg := session.Config()
    if cfg == nil {
        return metadata
    }
    wfc := cfg.GetWorkflowConfig(workflowID)
    if wfc == nil {
        return metadata
    }
    // ⤵️ 8.15 回填：WorkflowConfig 实现后，从 wfc 提取 card.version 和 card.name
    // 当前 WorkflowConfigProvider 为空接口，无法提取字段
    return metadata
}
```

- [ ] **Step 2: 运行编译和测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... && go test ./internal/agentcore/session/tracer/... -v -count=1`
Expected: 编译通过，测试 PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/tracer/workflow.go
git commit -m "feat(session): 回填 tracer getWorkflowMetadata 使用 SessionConfig

- getWorkflowMetadata 从 session.Config().GetWorkflowConfig() 获取工作流配置
- WorkflowConfigProvider 当前为空接口，version/name 暂时仍为空字符串
- ⤵️ 8.15 回填：WorkflowConfig 实现后提取 card.version 和 card.name"
```

---

## Task 8: 更新 doc.go 和 session 包整体测试

**Files:**
- Modify: `internal/agentcore/session/doc.go`
- Modify: `internal/agentcore/session/interfaces/doc.go`（如有）

- [ ] **Step 1: 更新 session/doc.go**

1. 在文件目录中添加 `constants/` 和 `config/` 子包条目
2. 更新核心类型/接口索引，添加 `SessionConfig`、`WorkflowConfigProvider`、`AgentConfigProvider`
3. 更新 Config/ActorManager 的占位说明，Config 已于 5.12 回填

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 状态**

1. 将 5.12 状态从 `☐` 改为 `✅`
2. 将 5.13 状态从 `☐` 改为 `✅`
3. 更新 5.12/5.13 的详细说明

- [ ] **Step 3: 运行全量 session 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(session): 更新 doc.go 和 IMPLEMENTATION_PLAN.md

- doc.go 添加 constants/config 子包条目和核心类型索引
- IMPLEMENTATION_PLAN.md 5.12/5.13 状态标记为 ✅"
```
