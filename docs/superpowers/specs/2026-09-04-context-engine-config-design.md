# 5.17 ContextEngineConfig 设计文档

## 概述

实现上下文引擎的全局配置结构体 `ContextEngineConfig`，对应 Python `openjiuwen/core/context_engine/schema/config.py`。

## 流程位置与作用

`ContextEngineConfig` 位于 Agent 会话生命周期的 **「会话创建 → 上下文引擎初始化」** 阶段：

```
Agent 启动
  └→ 创建 ContextEngine（传入 ContextEngineConfig）
       └→ 创建 Session（会话）
            └→ Session 调用 ContextEngine.CreateContext()
                 └→ 创建 SessionModelContext
                      ├── max_context_message_num → 消息缓冲区上限
                      ├── default_window_message_num → 滑动窗口大小
                      ├── default_window_round_num → 默认对话轮数
                      ├── enable_kv_cache_release → KVCacheManager 创建条件
                      ├── enable_reload → reloader 工具注入
                      ├── context_window_tokens → resolve_context_max() fallback
                      ├── model_name → 解析模型名称
                      └── model_context_window_tokens → 模型→窗口token映射表
```

核心作用：控制消息上限、窗口策略、KV缓存释放、卸载重载、token 预算等上下文引擎行为。

## 设计决策

### 1. 纯结构体 + 校验方法

采用方案 A：值类型 + 默认值 + `Validate() error`，不用指针表示 Optional。

- int 字段：`0` 表示"不限"，设置时必须 > 0
- string 字段：空串表示"未指定"
- map 字段：空 map 表示"无映射"（统一初始化，杜绝 nil）

### 2. model_context_window_tokens 统一初始化空 map

在 `NewContextEngineConfig()` 构造函数中 `make(map[string]int)`，避免 nil map。
消费者通过 `len() == 0` 判断是否设置了映射。

### 3. 防御性校验

`Validate()` 中对 `model_context_window_tokens` 的 value 做 > 0 校验，比 Python 更严格但更安全。

### 4. 文件组织：新建 schema/ 子包

与 Python 目录结构对齐，后续 5.18（Offload 消息模型）、5.19（ContextEvent）也放 schema/ 子包。

## 文件结构

```
internal/agentcore/context_engine/
├── doc.go                          # 更新：添加 schema/ 子目录
├── base.go
├── base_test.go
├── token/
│   ├── doc.go
│   └── base.go
└── schema/                         # 新建
    ├── doc.go                      # 包文档
    ├── config.go                   # ContextEngineConfig 定义
    └── config_test.go              # 测试
```

## 字段定义

| 字段 | Go 类型 | 默认值 | 含义 | 校验规则 |
|------|---------|--------|------|---------|
| `MaxContextMessageNum` | `int` | `0` | 上下文消息数硬上限，0=不限 | > 0（设置时） |
| `DefaultWindowMessageNum` | `int` | `0` | 滑动窗口默认保留消息数，0=不限 | > 0（设置时） |
| `DefaultWindowRoundNum` | `int` | `0` | 默认对话轮数，0=不限 | > 0（设置时） |
| `EnableKVCacheRelease` | `bool` | `false` | 是否释放 KV-cache | 无 |
| `EnableReload` | `bool` | `false` | 是否启用卸载重载 | 无 |
| `EnableTiktokenCounter` | `bool` | `false` | 是否启用 tiktoken | 无 |
| `ContextWindowTokens` | `int` | `0` | 模型上下文窗口 token 数，0=不限 | > 0（设置时） |
| `ModelName` | `string` | `""` | LLM 模型名称 | 无 |
| `ModelContextWindowTokens` | `map[string]int` | `map[string]int{}` | 模型→窗口token映射 | value > 0 |

## API 设计

```go
// NewContextEngineConfig 创建上下文引擎配置，所有字段使用默认值
func NewContextEngineConfig() ContextEngineConfig

// Validate 校验配置字段合法性
func (c ContextEngineConfig) Validate() error
```

## 回填关系

5.17 本身无直接回填负担，主要为后续步骤提供类型定义：

| 后续步骤 | 消费方式 |
|---------|---------|
| 5.30 ContextEngine 门面 | 构造函数接收 `ContextEngineConfig`，内部持有 |
| 5.31 Context 实现 | 消费 config 各字段（消息上限、窗口大小、KV缓存、reload 等） |
| 5.20+ 各处理器 | 处理器自身有独立 Config，与 ContextEngineConfig 正交 |

`base.go` 中 `ContextEngine` 接口签名无需修改——config 在实现类内部持有，与 Python 一致。

## 对应 Python 代码

`openjiuwen/core/context_engine/schema/config.py`
