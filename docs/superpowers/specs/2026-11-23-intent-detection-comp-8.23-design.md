# 8.23 IntentDetectionComp 设计 spec

> 日期：2026-11-23
> 状态：已与用户讨论定稿（拍板记录见 §7）
> Python 真相源：`openjiuwen/core/workflow/components/llm/intent_detection_comp.py`

## 1. 定位与作用

`IntentDetectionComp` 是领域八 Component 层 llm 子包的第二个 LLM 类组件（8.18 LLMComp 之后、8.24 QuestionerComp 之前）。

在 agent 会话工作流中的位置与作用：

1. **图结构角色**：唯一「组件自带路由」的可组合组件——`AddComponent` 在 `AddNode` 后紧跟 `AddConditionalEdges(node_id, router)`，`AddBranch` 注册分类→下游节点映射。
2. **执行语义**：取会话历史（enable_history 时从 ModelContext 取 N 轮窗口）→ 组装中/英双模板提示词（分类列表 + few-shot + 默认分类）→ 调 LLM → 将本轮 user/assistant 消息写回 ModelContext → 解析 JSON 输出（true/false/null 修复 → Python 字面量求值 → 正则规整 `分类N`/`CategoryN` → 不在类别表回落默认分类并写 reason）→ 输出 `{classification_id, reason, category_name}`。
3. **容错语义**：LLM 输出解析失败 / 缺 class 字段 / 类别不在列表，三种失败均不报错，回落默认分类（`分类0`/`Category0`，id=0）并在 reason 写中/英文解释。

## 2. Go 侧实现方式（已盘点，前置件全就绪）

| Python 依赖 | Go 等价物 | 状态 |
|---|---|---|
| `BranchRouter` + set_session | `components/flow/branch_router.go`（AddBranch/SetSession，8.19） | ✅ |
| `ModelContext.get_context_window / add_messages` | `core/context_engine`（GetContextWindow/AddMessages，8.18 全链修复） | ✅ |
| `PromptTemplate.format / to_messages` | `foundation/prompt/template.go`（多余 key 过滤、未填占位符保留，已对齐） | ✅ |
| `Model(...).invoke` | `foundation/llm/model.go`（8.18） | ✅ |
| 错误码 / workflow logger / UserConfig.is_sensitive | `common/exception` / `logger` / `UserConfig` | ✅ |
| `Session.get_executable_id / get_session_id` | `session.Session.GetExecutableID/GetSessionID` | ✅ |

实现形态：

- 新文件 `internal/openuapclaw/core/workflow/components/llm/intent_detection_comp.go`（+ `_test.go`），一比一映射 Python 单文件（规则 9）。
- 类型清单（Python → Go）：
  - `IntentDetectionCompConfig` → `IntentDetectionCompConfig`（model_id / model_client_config / model_config / category_name_list / user_prompt / example_content / enable_history / chat_history_max_turn / accept_language）
  - `IntentDetectionDefaultConfig` → 非导出对应结构（category_list / intent_detection_template / default_class / enable_input）
  - `IntentDetectionInput`（query，extra=allow）/ `IntentDetectionOutput`（classification_id=-1 / reason="" / category_name=""）
  - `IntentDetectionExecutable`（含 set_router / post_commit / 全部私有方法）
  - `IntentDetectionComponent`（ComponentComposable：executable 懒加载 / add_component / to_executable / add_branch / router）
- 模块级常量与**死常量**（RESULT/FEW_SHOT_NUM/ENABLE_Q2L/SEARCH_TYPE/KG_FILTER_* 等定义未使用者）按「疑死代码仍复刻」惯例原样搬入。
- 中英文默认提示词模板逐字复制（提示词一比一纪律），`ROLE_MAP_ZH/EN`、三组失败 reason 文案逐字对齐。
- `_refix_llm_output` 的正则 `\{.*\}`（DOTALL）+ `false/true/null → False/True/None` 替换照搬；`ast.literal_eval` 用 Python 字面量求值等价物（复用既有对齐实现，如有则共享，无则按 pythonsem 惯例入 `internal/common/pythonsem`）。
- 基座接线按 8.19 BranchComp 同形态（组件自持 router），勿 override 基座两方法（8.20 槽优先基座注意事项）。
- doc.go 更新 llm 子包文件树。

## 3. 路由与条件的取值语义（讨论澄清）

- 条件**不感知 LLM 原始文本**：LLM 输出已在 invoke 内解析为三字段输出，图引擎将输出 commit 进 session state 后，BranchRouter 才逐 Branch 求值。
- 三条件形态：string（ExpressionCondition，`${字段}` 占位符经 `get_global` 绑定）/ Callable（FuncCondition）/ 自定义 Condition。
- **本组件贡献的路由字段只有 classification_id / reason / category_name**；表达式读的是全局状态（含此前节点产物），但本组件分支路由的判定语义仅针对本组件分类结果设计。
- 输出经 `model_dump(exclude_defaults=True)`：等于默认值的字段被剔除（极端情况为空 dict）——表达式缺键语义见 §6 偏差澄清。

## 4. 使用示例（装配态）

```go
cfg := &components.IntentDetectionCompConfig{
    ModelClientConfig: ...,
    ModelConfig:       ...,
    CategoryNameList:  []string{"退款申请", "物流查询", "人工客服"},
    UserPrompt:        "你是电商客服，请判断用户想办理哪类业务。",
    ExampleContent:    []string{...},
    EnableHistory:     true,
    ChatHistoryMaxTurn: 3,
    AcceptLanguage:    "zh",
}
comp := components.NewIntentDetectionComponent(cfg)
comp.AddBranch(condition.NumberEqual(1), []string{"refund_node"}, "branch_1")
comp.AddBranch("${category_name} == '物流查询'", []string{"logistics_node"}, "branch_2")
comp.AddBranch(condition.NumberEqual(0), []string{"human_agent_node"}, "branch_default")
comp.AddComponent(graph, "intent_node", false)
// 运行期输入仅 {"query": "..."}
```

## 5. 测试策略（拍板 A：真实链路 + 边界 fake）

- 真实 session 管线 + 真实 ModelContext；只在 LLM 网络边界用 fake client（真实 Model 挂 fake transport）。
- 覆盖点：
  1. 构造期 category_list 自动生成（分类1..N / Category1..N）+ 默认分类追加头部
  2. 提示词模板中/英双语填充（含 example_content 拼接、chat_history 格式化角色映射）
  3. 历史链路：enable_history=true 时 GetContextWindow 取 N 轮 + user/assistant 写回 ModelContext（真实链验证）
  4. LLM 输出解析全分支：JSON 失败 / 缺 class / 类别规整（正则+忽略大小写+中英）/ 不在列表回落 / 正常命中
  5. 输出 exclude_defaults 语义（默认值剔除、空 dict 场景）
  6. 路由集成：输出 commit 后 BranchRouter 命中对应 target
  7. 双路径模型创建：model_id 走 resource manager、直接构造路径、两者皆缺报 COMPONENT_INTENT_DETECTION_INVOKE_CALL_FAILED
  8. 错误路径日志（LLM_CALL_ERROR 事件、event_type/字段对齐规则 3）
- 覆盖率目标 ≥85%（llm 包按文件整体）。

## 6. 已知偏差 / 澄清区

| # | 条目 | 性质 | 处置 |
|---|---|---|---|
| D-23-1 | `ast.literal_eval` 无 Go 原生等价 | 语言层缺失 | 用 Python 字面量求值等价实现（pythonsem 或包内私有，实施时定）；Python 行号 387 |
| D-23-2 | ExpressionCondition 缺键求值语义 | 需实施时实证 | Python `get_global` 缺键返回 None；Go 表达式引擎缺键行为需核对（condition/expression.go 既有实现），若不一致登记偏差。expression.go 属 8.19 既有资产，本节只在必要时核对，不重写 |
| D-23-3 | `Runner.resource_mgr.get_model` → Go resource manager | 平台映射 | model_id 路径走 Go 侧资源管理器取 Model（拍板 A：双路径照搬） |

## 7. 拍板记录

1. **章节顺序**：按 8.23 直接实施，不换章节（与 8.18 同包复用 LLM/ModelContext 链路）。
2. **LLM 实例创建**：Option A——双路径照搬（model_id 走 resource manager；否则直接构造）。
3. **测试策略**：Option A——真实链路 + 边界 fake，禁止替身承载能力面（feedback-real-chain-testing）。
4. **回填核查**：IMPLEMENTATION_PLAN 无 ⤴️8.23 待回填项；8.22 行状态 ☐ 与实际完成不符，实施时顺带更正为 ✅。
5. IMPLEMENTATION_PLAN 8.23 状态随实施同步 ☐→🔄→✅。

## 8. 删除清单（规则 8）

本节为纯新增，无被替代旧实现；`grep -r IntentDetection internal/ tests/ cmd/` 实施前应为 0 命中（防重复实现）。
