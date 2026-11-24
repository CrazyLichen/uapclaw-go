# 8.24 QuestionerComp 设计 spec

> 日期：2026-11-24
> 状态：已与用户讨论定稿（拍板记录见 §8）
> Python 真相源：`openjiuwen/core/workflow/components/llm/questioner_comp.py`（888 行）

## 1. 定位与作用

`QuestionerComp` 是领域八 Component 层 llm 子包的第三个 LLM 类组件（8.18 LLMComp / 8.23 IntentDetectionComp 之后），也是 **6.25 Runner 交互中断恢复回填区（8.14-8.24）的最后一节**。

在 agent 会话中的位置与作用：

1. **图结构角色**：工作流 DAG 原子节点（"交互提问"节点类型），与 8.18/8.23 同属 Python `workflow/components/llm/` 包，全仓 grep 证实除 `workflow/__init__.py` 导出外零消费方（能力面完备性组件）。
2. **执行语义**：多轮人机交互式参数收集。START 态取历史 → 二分支——配置 `question_content` 则模板渲染直发问；配置字段抽取则 LLM 从对话历史抽取参数 JSON，必填缺失则生成追问。经 `session.interact` 抛 GraphInterrupt 挂起，用户回答后从 checkpoint 恢复、重执行该节点，`_load_state_from_session` 反序列化回 USER_INTERACT 态继续抽取，循环至抽齐（END，清状态支持重入）或超过 `max_response`（抛错）。
3. **匹配机制**（讨论澄清）：参数匹配不是代码做的，是 LLM 做的——字段表 `field_names`（field_name/description/type/cn_field_name/required/default_value）是固定配置，经提示词 `{{required_params_list}}` 塞给 LLM，约定其输出按 field_name 键名的 JSON；代码只做三道事后工序：剥壳解析（```json/'''json → json.loads，失败返空 dict）、`is_valid_value` 剔除无效值（None/""/{}/[]/"null"/"none"）、`validate_and_convert_type` 按 FieldInfo.type 四类转换（bool≠int、整值 float 才算 int、str 解析，失败即视为未抽取）。跨轮累积靠 `state.extracted_key_fields`，必填齐 = `required && field_name not in state` 全空；`default_value` 兜底始终未抽到的字段。
4. **用户问题来源**：两套模板——`question_content` 配置模板（`{{}}` 占位符用输入除 query 外字段渲染）；追问固定语句（中英双语逐字复制，代码拼接不走 LLM）。喂 LLM 的是第三套（QUESTIONER_SYSTEM/USER_TEMPLATE_ZH/EN），三者不混。

## 2. 中断恢复链路（讨论澄清，非本节新增机制）

两层中断各管各的，8.24 只对齐层 1：

- **层 1（workflow 节点级）**：`wait_user_inputs` 写 `INTERACTION` 帧到 output writer（前端看到问题）→ raise GraphInterrupt；引擎顶层返回 `{TASK_STATUS_INTERRUPT}`（engine.py:269），checkpointer `post_workflow_execute` 存 checkpoint（reason=interaction_required，persistence.py:882-910 三分支：真异常才 re-raise 终止、正常完成才 clear）。GraphInterrupt 是控制流非异常终止。
- **恢复**：用户回答包成 `InteractiveInput.update(node_id, answer)` → 同 session_id 二次调用 → checkpoint 恢复、pending 节点重执行 invoke → `_load_state_from_session` 见 status=USER_INTERACT 用恢复态 → `wait_user_inputs` 见 workflow_state[INTERACTIVE_INPUT] 有值直接返回（interaction.py:37-48）。`_get_latest_human_feedback` 的 `range(response_num+1)` 循环 = 重放已收集反馈、最后一轮等新反馈。
- **INPUT_REQUIRED 的产生机制（2026-11-24 补充实证）**：中断后双路传播——异常路（GraphInterrupt 穿透 vertex（vertex.py:656 仅捕 ExecutionError）→ engine.py:283 返回 `{TASK_STATUS_INTERRUPT}` → checkpointer 存档，workflow.py 不读该键）；状态路（wait_user_inputs 抛前先写 INTERACTION 帧 → writer 队列 → `_stream` 消费 yield）。`invoke` 的 `_invoke_task`（workflow.py:360-383）收全 chunks **逐帧嗅探** `type==INTERACTION` → `WorkflowOutput(INPUT_REQUIRED)`，否则 `get_outputs(end_comp_id)` → COMPLETED。writer 挂载点在 invoke 内部 `_create_workflow_session`（:359/:720 新建独立 StreamWriterManager），ability_mgr 传入的裸 session（agent.py:295 初始 None）由此装配；end 节点输出双记账（END_NODE_STREAM 流帧 + state().get_outputs 兜底）。一帧两用（顶层直出前端 + invoke 状态判定），无双发。
- **层 2（ReAct 工具调用级）**：两种形状并存，勿混（2026-11-24 勘误，初版误写为单一 `result_type:interrupt` 形状）：
  - **workflow-as-tool**（本组件作为 ReAct 工具）：`_run_workflow`（ability_manager.py:1511）经 `session.create_workflow_session()` 建**独立 StreamWriterManager**（agent.py:295 + internal/workflow.py:34 初始 None；workflow.py:720 新建 StreamEmitter，不接父 writer）→ `wait_user_inputs` 的 INTERACTION 帧写入 workflow 自有 emitter，ReAct tool 拓扑下无人消费、不外泄；返回形状为 `WorkflowOutput(state=INPUT_REQUIRED)`（ability_manager.py:1528），走 ReAct `interrupted_workflows` 状态机，最终 `_write_invoke_result_to_stream`（react_agent.py:3398）**按 pending_component_id 过滤单发**（注释："callers see exactly one interaction chunk"）——不发两遍。
  - **agent-as-tool**（子代理）：显式共享父 writer（ability_manager.py:1685）直出；ReAct 仅补发 value 为 InterruptRequest 的载荷（handler.py:204 过滤）。
  - 用户回复回填：`handle_resume` → `_build_sub_agent_resume_tool_call` / `tc.arguments = collected_input`（react_agent.py:2722）→ 重新执行 tool → `InteractiveInput` 经 workflow checkpointer `pre_workflow_execute` → `_process_interactive_inputs`（persistence.py:336-352）按节点 id 写入节点状态 INTERACTIVE_INPUT → 落 checkpoint。**此桥属 ReAct 侧能力，8.26 ReactComponent 实施时对齐；本节集成用例走 WorkflowAgent/Runner 直调路径（Python test_runner_agent_resource_management 同款，顶层路径：workflow 自有 emitter 直出前端），ReAct 包装路径 ⤴️8.26 回填登记。**

## 3. Go 侧实现方式（前置件全就绪）

| Python 依赖 | Go 等价物 | 状态 |
|---|---|---|
| `session.interact` 返回反馈 | `NodeSessionFacade.InteractWithResult`（5.7 已回填，streamMode 拒绝语义已有） | ✅ |
| `session.get_state/update_state`（`questioner_state` 键） | `GetState/UpdateState(map[string]any)` | ✅ |
| `ModelContext.get_context_window/add_messages` | `core/context_engine`（8.18 全链） | ✅ |
| `PromptTemplate.format/to_messages` | `foundation/prompt`（8.23 复用） | ✅ |
| `Model.invoke` + 双路径创建 | `foundation/llm/model.go` + 8.23 D-23-3 同款（mgr==nil / GetModel 未命中防御分支，批量签名取 clients[0]） | ✅ |
| `is_valid_value` | pythonsem PyFalsy + "null"/"none" 字符串特判（本组件特有，非全 falsy） | ✅ 复用+小增量 |
| `validate_and_convert_type` | **新增入 pythonsem 共享包**（拍板②，见 D-24-2） | 🔄 本节产出 |
| 错误码 101070-101076 七个 COMPONENT_QUESTIONER_* | `common/exception/codes` 逐个核对 Python 真值（吸取 8.22 182012→102000 勘误教训，**本批已核实无该问题**） | 🔄 本节登记 |
| workflow logger / UserConfig.is_sensitive / ExceptionUtils | 既有 | ✅ |

实现形态：

- 新文件 `internal/openuapclaw/core/workflow/components/llm/questioner_comp.go`（+ `_test.go`），一比一映射 Python 单文件（规则 9），doc.go 更新 llm 子包文件树。
- 基座接线按 8.20 槽位惯例：`ToExecutableFn` 首调构造，**勿 override 基座两方法**（8.20 拍板⚠️）。
- 类型清单（Python → Go，全量无裁剪）：
  - 常量/双语文案（QUESTIONER_*_TEMPLATE_ZH/EN 四段、CONTINUE_ASK 两句、START_STR/END_STR/USER_INTERACT_STR 等）逐字复制（提示词一比一纪律）
  - `ExecutionStatus` / `QuestionerEvent` / `ResponseType`（单成员死枚举仍复刻）
  - `FieldInfo` / `QuestionerConfig` / `QuestionerDefaultConfig`（from_language）/ `QuestionerInput`（query 三态，extra=allow）/ `OutputCache` / `QuestionerOutput`（json tag 对齐 pydantic 字段名）
  - 状态机四类 `QuestionerState` / `QuestionerStartState` / `QuestionerInteractState` / `QuestionerEndState`（含 EndState→START 回环、deserialize 后 handle_event）
  - `QuestionerUtils` 五静态方法（format_template 正则渲染含缺键怪癖 / format_continue_ask / format_questioner_output exclude_defaults / validate_inputs / is_valid_value / validate_and_convert_type）
  - `QuestionerDirectReplyHandler` 全部方法归属不变（handle 三态分发 / _handle_start_state / _handle_user_interact_state / _handle_end_state / 抽取链 / LLM 调用解析 / 状态更新 / context 读写）
  - `QuestionerExecutable`（_load/_store_state_from/to_session / 三静态校验 / state / invoke / _create_llm_instance / _initialize_if_needed / _init_prompt / _handle_questioner_direct_reply(_safe) 含 `_state` 传递缝）与 `QuestionerComponent`（组合面）
  - 疑死代码仍复刻（ResponseType 单成员、`_handle_questioner_direct_reply` 非 safe 版等）。

## 4. 测试策略（拍板：单测全覆盖 + 集成端到端，用户 2026-11-24 明确要求）

**单测**（真实 session 管线 + 真实 ModelContext，仅 LLM 网络边界 fake，feedback-real-chain-testing）：

1. 构造期配置校验三分支（response_type 非法 / extract_fields 且字段表空 / max_response≤0）
2. 双语默认模板选择 + question_content `{{}}` 渲染（含缺键输出 "None" 怪癖分支）
3. START 态两分支：question_content 直发问挂起 / LLM 抽取（抽齐→END、缺必填→追问挂起、两者皆缺→101072）
4. USER_INTERACT 态恢复：状态反序列化 → 反馈重放（response_num+1 循环语义）→ 再抽取 → END/继续追问/超限抛 101074
5. LLM 输出解析全分支：剥壳两正则 / JSON 失败返空 dict / 非 dict 抛 101076 / 无效值剔除 / 四类类型转换与 bool≠int、整值 float、str 解析边界 / 未知键回落 string / 转换失败剔除+warning 日志
6. default_value 兜底、exclude_defaults 输出语义、END 态清状态重入
7. 双路径模型创建（model_id 走 resource manager / 直接构造 / 皆缺报 101075）
8. 错误路径日志（LLM_CALL_ERROR 事件、event_type/字段对齐规则 3）
9. 状态序列化往返（serialize → deserialize → handle_event）

**集成**（tests/integration，兑现 6.25 回填义务）：

10. **端到端：WorkflowAgent/Runner → workflow 中断 → 恢复**——真实 workflow 含 questioner 节点，首调收到 interrupt（INTERACTION 问题帧）→ checkpoint 落盘；以 `InteractiveInput.update(node_id, 回答)` 二次调用（同 session_id）→ 验证恢复执行到下游节点且输出含抽取字段与 user_response（对齐 Python test_runner_agent_resource_management 欠的 Questioner `__interaction__` → 二次调用恢复场景）
11. `tests/integration/agentcore/runner` 注释/doc.go 的"A 类缺口（8.14-8.24 未移植）"标注同步改写（回填兑现）
12. ReAct→workflow 中断桥（`result_type: interrupt` / interrupt_answers 映射）⤴️8.26 回填，本节不测

覆盖率目标 ≥85%（llm 包按文件整体）。

## 5. 已知偏差 / 澄清区

| # | 条目 | 性质 | 处置 |
|---|---|---|---|
| D-24-1 | `format_template` 缺键输出 `str(None)`="None"（Python 怪癖，re.sub 内 `user_fields.get(key)` 无默认） | 语言层怪癖 | 对齐复刻（行为等价优先） |
| D-24-2 | `validate_and_convert_type` 定义在 Python 组件内、Go 入 pythonsem 共享包 | 结构偏移 | 拍板②已确认（2026-11-24）；pythonsem 纪律：后续抽取类组件（8.26 等）复用，禁止包内私抄 |
| D-24-3 | `QuestionerInput.query` Union[str, dict, None] → Go any + 三态处理（str 直用 / dict 包 [dict] 入历史 / 空回落 ""） | 类型系统差异 | 复刻三态分支 |
| D-24-4 | `is_valid_value` 的 "null"/"none" 字符串特判不属 PyFalsy 全 falsy 语义 | 语义边界 | pythonsem PyFalsy + 组件内字符串特判，不复刻为全 falsy（8.23 拍板②同口径） |
| D-24-5 | 状态机四类在 Go 的多态形态（四结构体同方法 vs 单结构体 kind 字段） | 实施形态 | 待 writing-plans 实施时定，以可逐行对照优先；8.23 同款 Python 子类模式先例为四结构体 |
| D-24-6 | ReAct→workflow 中断桥（层 2）Go 侧待 8.26 | 章节归属 | ⤴️8.26 回填，本节集成用例走 WorkflowAgent/Runner 直调路径 |

## 6. 回填义务核查

1. **6.25 Runner 交互中断恢复集成测试**：`tests/integration/agentcore/runner` 注释明确"依赖 WorkflowAgent/IntentDetection/Questioner（8.14-8.24 未移植，A 类缺口）"。8.24 为区间最后一节，实施后全部解锁（本节 Task 兑现，见 §4.10-11）。
2. 全计划 grep 证实无其他 ⤴️ 指向 8.24 的待回填项（与 8.22/8.23 spec 同结论）。
3. IMPLEMENTATION_PLAN 8.24 状态 ☐→🔄→✅ 随实施同步；追加二十二/二十三次实施顺序注记（8.23 收口记录 + 本次 8.24）。

## 7. 删除清单（规则 8）

纯新增，无被替代旧实现。实施前 `grep -r Questioner internal/ tests/ cmd/` 应仅命中 runner 集成测试注释/doc.go（实施中按 §4.11 改写，锚点归零）。

## 8. 拍板记录

1. **章节顺序**：方案 A 按编号——8.24 → 8.25 → 8.26（维持二十一次注记口径；8.25 依赖 13.26-13.28 已就绪无阻塞）。
2. **类型转换助手落点**：方案 A 入 pythonsem 共享包（与既有 IsBool/PyInt 语义同源，8.26 可复用）。
3. **测试策略**：单测全覆盖 + 集成用例补齐，重点补"ReAct → workflow 中断 → 恢复"端到端验证（用户 2026-11-24 明确要求）；经澄清该桥属 ReAct 侧（层 2），本节集成走 WorkflowAgent/Runner 直调路径，层 2 ⤴️8.26。
4. IMPLEMENTATION_PLAN 8.24 状态随实施同步。
