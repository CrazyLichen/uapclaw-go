# ⤵️ 回填标记审计与修复设计

## 背景

IMPLEMENTATION_PLAN.md 中多处章节标注了 `⤵️`（等待回填）标记，指向依赖章节。经代码级验证，部分回填目标章节已 ✅ 完成但标记未清理，部分代码中回填逻辑未真正实现。

## 审计结果

### 类别1：纯计划标记清理（8处，代码已回填）

| # | 章节 | 当前⤵️文本 | 修复为 |
|---|------|-----------|--------|
| 1 | 5.2 | `⤵️ Config 返回类型待 5.12 回填` | 删除⤵️，补充 `✅ 5.12 已回填 Config 返回类型` |
| 2 | 5.3 | `⤵️ Config 返回类型待 5.12 回填` | 删除⤵️，补充 `✅ 5.12 已回填 Config 返回类型` |
| 3 | 5.4 | `⤵️ Config 返回类型待 5.12 回填` | 删除⤵️，补充 `✅ 5.12 已回填 Config 返回类型` |
| 4 | 5.5 | `⤵️ 5.12 回填 Config` | 删除⤵️，补充 `✅ 5.12 已回填 Config` |
| 5 | 5.8 | `⤵️ 5.12 回填 Config 返回类型` | 删除⤵️，补充 `✅ 5.12 已回填 Config 返回类型` |
| 6 | 5.30 | `⤵️ 6.4-6.10 回填事件触发` | 删除⤵️，补充 `✅ 6.4-6.10 已回填事件触发（6处TriggerContext）` |
| 7 | 6.8 | `ConsumeRetryRequest/HasForceFinishRequest 从 panic 桩改为安全桩(⤵️ 6.10 回填)` | 删除⤵️，补充 `✅ 6.10 已回填 ConsumeRetryRequest/HasForceFinishRequest 真实实现` |
| 8 | 3.8 | `FormHandler（form_params 暂 fallback 到 body，3.10 回填）` | 删除⤵️，补充 `✅ 3.10 已回填 FormHandler（FormHandlerManager 集成）` |

### 类别2：代码回填（1处，立即实现）

| # | 章节 | 修复内容 |
|---|------|---------|
| 9 | 6.19 | 回填 `Recognize()` LLM 调用逻辑，对齐 Python；删除代码中 `⤵️ 6.23` 标记；计划中 `⤵️ 6.23 回填 LLM 调用` → `✅ 6.23 已回填 LLM 调用` |

**6.19 回填细节**：

Python `IntentRecognizer.recognize()` 核心逻辑（`openjiuwen/core/controller/modules/intent_recognizer.py L120-186`）：
1. 从 ContextEngine 获取/创建上下文
2. 校验事件类型为 InputEvent，提取文本
3. 通过 `Runner.resource_mgr.get_model()` 获取 LLM 模型
4. 构建 user message（含当前任务列表）
5. 调用 `model.invoke(messages, tools=intentToolkits.get_openai_tool_schemas())`
6. 循环处理 `response.tool_calls`：
   - 调用 IntentToolkits 实例方法获取 Intent
   - 添加 ToolMessage 到上下文
   - 继续调用 model 直到无 tool_calls
7. 返回收集的 Intent 列表

Go 实现计划：
- `ModelProvider` 接口保持（解耦 IntentRecognizer 与 ResourceMgr 的直接依赖）
- Controller 层在初始化 IntentRecognizer 时通过 `SetModelProvider()` 注入真实适配器
- `Recognize()` 实现完整 LLM 调用 + tool_calls 循环
- `processContinueTaskIntent` 中 `⤵️ 6.23 ContextEngine.GetContext 回填` 同步完成
- 更新测试

### 类别3：延后保留（1处，修正描述）

| # | 章节 | 当前 | 修复为 |
|---|------|------|--------|
| 10 | 9.26 | `⤵️ 9.38-49 回填 Playwright MCP 工具集` | `⤵️ 9.38-49 Playwright MCP 端到端集成（6处占位：CodeExecutor/ManagedBrowserDriver/WorkerAgent/runTaskOnce/Probe占位/restart）` |

### 额外发现

| # | 文件 | 问题 | 修复 |
|---|------|------|------|
| 1 | `session/tracer/workflow.go:242` | 过时注释"Config() 返回 any" | 更新注释为当前真实状态 |

## 修改文件清单

| 文件 | 修改类型 |
|------|---------|
| `IMPLEMENTATION_PLAN.md` | 8处标记清理 + 1处标记更新 + 1处描述修正 |
| `internal/agentcore/controller/modules/intent_recognizer.go` | Recognize() 回填 + 删除⤵️标记 |
| `internal/agentcore/controller/modules/intent_recognizer_test.go` | 更新测试 |
| `internal/agentcore/controller/base.go` 或 controller 初始化处 | 注入 ModelProvider 适配器 |
| `internal/agentcore/session/tracer/workflow.go` | 修复过时注释 |
