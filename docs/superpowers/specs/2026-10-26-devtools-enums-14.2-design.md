# 14.2 枚举层设计（devtools agent_builder utils/enums）

- 日期：2026-10-26
- 状态：已批准（brainstorming 会话定稿）
- Python 参考：`openjiuwen/dev_tools/agent_builder/utils/enums.py`（49 行）
- Python 测试：`tests/unit_tests/dev_tools/agent_builder/utils/test_enums.py`（12 用例）+ `tests/system_tests/dev_tools/agent_builder/utils/test_utils_integration.py`（TestEnumIntegration 4 用例 + 混合用例枚举半边）
- 前置：14.1 已完成（同包，基线覆盖率 96.7%）

## 0. 已锁定决策（brainstorming 会话）

| 决策点 | 结论 |
|---|---|
| 枚举表示 | **方案 A**：`type X string` 开放类型 + 显式 Parse 函数（仓库惯例：TaskStatus/IndexTypeKind/StoreType 等 15+ 先例） |
| 回填落位 | **enums_test.go 统一收口**：12 unit + 4.5 system 全进单文件，按 `TestXxx_场景描述` 区分来源（14.1 先例：system 3 个并入 utils_test.go） |
| 章节顺序 | **维持原序**：14.2 → 14.3 → 14.4 → 14.5 → 14.6，依赖链自洽（详见 §7） |

## 1. 流程位置与作用

```
开发者/前端 ──▶ AgentBuilder 门面 (14.9 main.py)
                    │ build_agent(session_id, agent_type, query)
                    ▼
              AgentBuilderExecutor (14.5 executor.py)
                    │ ① AgentType(query.agent_type)      ← 14.2：入参校验关口
                    │    值不合法 → ValueError → 140032
                    ▼
              AgentBuilderFactory (14.6 factory.py)
                    │ ② _builders: Dict[AgentType, Builder类] ← 14.2：工厂注册键
                    ▼
              BaseAgentBuilder 状态机 (14.6 base.py)
                    │ ③ _state: BuildState INITIAL→PROCESSING→COMPLETED ← 14.2：三态生命周期
                    ▼
              ProgressReporter (14.3 progress.py)
                    │ ④ current_stage: ProgressStage / current_status: ProgressStatus
                    ▼
              Clarifier/Generator (14.7) 与 DLGenerator/Reflector (14.8)
                    └─▶ utils 函数 (14.1) 剥围栏→解析
```

作用：agent_builder 的"类型词汇表"——输入域（AgentType）、状态机（BuildState）、进度上报轴（ProgressStage×ProgressStatus，前端轮询/WebSocket 渲染字典键）。14.3 全部字段类型、14.5 校验关口、14.6 注册键的直接前置。

Python 消费点（已核实）：executor.py L155/L166（构造+遍历值）、factory.py L22/L43（注册键+错误日志 agent_type.value）、base.py×6 + workflow/llm_agent builder×4（BuildState 状态机）、progress.py 全文（Stage/Status 字段类型）。

## 2. 文件与声明结构

同包续建零新包：`internal/agentcore/devtools/agentbuilder/utils/`（Python enums.py 本就在 utils/ 内）。

```
utils/
├── doc.go            # 目录树补 enums.go / enums_test.go 两条目
├── enums.go          # 新增：4 类型 + 26 常量 + 4 Parse + 4 All
├── enums_test.go     # 新增：12 unit + 4.5 system 回填 + 增强用例（~20 子用例）
└── (constants/utils 及其测试 4 文件不动)
```

enums.go 声明顺序（规范 2）：枚举区块（4 个 `type X string`）→ 常量区块（4 个 const 块，ProgressStage 保留 Python 三段注释组 Common/LLM Agent/Workflow Agent 中文注释）→ 导出函数区块（8 函数）。

## 3. 组件细节

### 3.1 四类型 26 常量（值逐一对齐 Python）

| 类型 | 值数 | 常量（示例/全部） |
|---|---|---|
| AgentType | 2 | `AgentTypeLLMAgent = "llm_agent"`、`AgentTypeWorkflow = "workflow"` |
| BuildState | 3 | `BuildStateInitial = "initial"`、`BuildStateProcessing = "processing"`、`BuildStateCompleted = "completed"` |
| ProgressStage | 15 | Common 组：`ProgressStageInitializing`/`Clarifying`/`ResourceRetrieving`/`Completed`/`Error`/`Optimizing`；LLM Agent 组：`GeneratingConfig`/`TransformingDSL`；Workflow Agent 组：`DetectingIntention`/`GeneratingWorkflowDesign`/`GeneratingDL`/`ValidatingDL`/`RefiningDL`/`TransformingMermaid`/`TransformingWorkflowDSL` |
| ProgressStatus | 5 | `ProgressStatusPending`/`Running`/`Success`/`Failed`/`Warning` |

### 3.2 Parse 函数（×4）

签名统一 `(s string) (X, error)`：

- `ParseAgentType(s string) (AgentType, error)`：**唯一有真实消费者的 Parse**——14.5 executor 的 `AgentType(query.agent_type)` 非法串入口；error 消息含合法值清单（如 `unsupported agent type: %q, valid: [llm_agent workflow]`），14.5 以 `%w` 链入 BuildError cause（对应 Python ValueError→捕获→重抛 ValidationError 链路，错误码 140032 AGENT_BUILDER_AGENT_TYPE_NOT_SUPPORTED）
- `ParseBuildState` / `ParseProgressStage` / `ParseProgressStatus`：API 对称补齐。依据：Python str-Enum 的 from-string 构造是统一语言特性（任何值可构造、非法抛 ValueError），全量对齐先例（14.1 的 13 个死常量照搬）覆盖"语言特性级"等价物，3 个小函数成本可忽略

### 3.3 All 函数（×4）

签名统一 `() []X`，按 const 声明序返回新切片（防调用方篡改）：

- `AllAgentTypes()`：消费者为 14.5 的 `supported_types=[t.value for t in AgentType]`（错误详情字段）
- `AllBuildStates()`：消费者为混合回填用例 `len(BuildState)>=3` 断言
- `AllProgressStages()` / `AllProgressStatuses()`：对称补齐（Python Enum 可迭代是统一特性）

### 3.4 AgentTypeLiteral

无 Go 对应物（显式偏差 §4-1）。

## 4. 显式偏差记录（Go↔Python）

1. **AgentTypeLiteral**：Python 纯静态类型别名（`Literal["llm_agent", "workflow"]`，零运行时行为）→ Go 不设对应物。`type AgentType string` 本身即约束边界，`ParseAgentType` 提供运行时收口——比 Python Literal（仅静态检查）更强
2. **is-string-enum 断言**：`isinstance(AgentType.LLM_AGENT, str)` → Go `string(AgentTypeLLMAgent) == "llm_agent"`（类型转换即 isinstance 等价物）
3. **from-string 构造**：`AgentType("workflow")` 语言特性 → 显式 `ParseAgentType` 函数；错误语义 ValueError → error 返回值（不再 panic）
4. **计数勘误归档**：14.1 spec §5 的 system 用例拆分算式少算 1 个枚举用例——实际 `TestEnumIntegration` 有 **4** 个方法（agent_type_string_conversion/build_state_transitions/progress_stage_values/progress_status_values），正确算式 12 = 3 纯 14.1 + 1 混合 + **4 枚举** + 4 progress。本 spec 完成回填时同步修正计划行 14.2 的"⤴️ system 用例 3 个"→"4 个 + 混合半边"
5. **Literal 用例转换**：`test_agent_type_literal_values`（Python 里是 list 字面量成员自证）→ Go 转为 `AllAgentTypes()` 产出集合恰为 {"llm_agent","workflow"} 的断言（语义内容等价转换，测试增强非语义丢失）

## 5. 测试对齐（enums_test.go 统一收口）

| Python 来源 | 用例 | Go 落位 |
|---|---|---|
| test_enums.py TestAgentType(4) | 值断言/is-string/from-string/invalid | `TestAgentType` + t.Run 子用例（invalid → assert err != nil + 错误消息含合法值清单） |
| test_enums.py TestBuildState(3) | 值断言/is-string/from-string | `TestBuildState` + 子用例 |
| test_enums.py TestProgressStage(2) | 15 值全断言/is-string | `TestProgressStage` + 子用例 |
| test_enums.py TestProgressStatus(2) | 5 值全断言/is-string | `TestProgressStatus` + 子用例 |
| test_enums.py TestAgentTypeLiteral(1) | Literal 值自证 | `TestAgentTypeLiteral`（转 AllAgentTypes 集合断言，§4-5） |
| system TestEnumIntegration(4) | 4 方法（来源标注 system） | `TestEnumIntegration_*` 4 个回填子用例 |
| system 混合用例枚举半边 | `len(AgentType)>=2` / `len(BuildState)>=3` | `TestConstantsEnumsConsistency_枚举半边` 2 断言（转 `len(AllAgentTypes())>=2` 等） |
| （Python 无） | Parse×3 正反例 + All×4 顺序断言 | 测试增强（14.1 LoadJSONFile 4 用例先例） |

预估 ~20 子用例；12 unit 用例 1:1 可审计映射（14.1 先例）。

## 6. 日志同步

Python enums.py **0 处 logger 调用** → Go 0 处日志，天然对齐（规则 3 无操作项）。

## 7. 章节衔接

- **下游 14.3（下一节，已裁决）**：ProgressStage/ProgressStatus 直接作为 BuildProgress/ProgressStep 字段类型；4 个 progress system 用例回填同包闭环
- **14.5 executor**：ParseAgentType err→140032（`%w` cause 链）；AllAgentTypes→supported_types 错误详情；历史遗留换序候选（createCoreModel provider_map）经核实 Go 侧 `ValidateAndNormalizeProvider` 已实现等价归一（providerTypeMap 四 provider 全注册：OpenAI/OpenRouter/SiliconFlow/DashScope），届时 14.5 内部只需对齐"未知 provider 原样透传"行为差异，**非换序问题**
- **14.6 factory**：`map[AgentType]...` 注册键；agent_type.value→string(t) 错误日志字段
- **14.9 map_state_to_status**：Python 用原始字符串 map（`state_mapping: Dict[str,str]` 未消费 BuildState 枚举），Go 按对齐原则同样用原始字符串，不消费 ParseBuildState
- 依赖方向（Python 已核实）：executor(14.5) imports factory(14.6)/builders-base(14.6)/history_manager/enum(14.2)/progress(14.3)；resource(14.4) 仅被 builders-base(14.6) 消费——计划顺序 14.2→14.3→14.4→14.5→14.6 自底向上自洽，**无需换序**

## 8. 回填清单

| # | 回填项 | 目标位置 | 时机 |
|---|--------|---------|------|
| 1 | 14.2 行 ☐→✅ + 产出摘要（含勘误后计数：4 system + 混合半边） | IMPLEMENTATION_PLAN.md | 实施完成后 |
| 2 | 14.1 spec §5 锚点行勘误补丁（3→4 枚举用例，算式 12=3+1+4+4） | 本 spec §4-4 已归档，实施完成时补丁 14.1 spec 原文 | 实施完成后 |
| 3 | 记忆 domain-14-devtools-progress.md 更新 14.2 状态 + 勘误记录 | 记忆文件 | 实施完成后 |
| 4 | 14.3 的 4 个 system 用例回填锚点 | 计划行 14.3 已有 ⤴️ 标注，不变 | 14.3 实施时 |

## 9. 验收标准

1. enums.go + enums_test.go + doc.go 目录树更新落地，gofmt + 声明顺序合规（规范 2）
2. `go test ./internal/agentcore/devtools/agentbuilder/utils/ -cover` ≥ 85%（当前包基线 96.7%，enums.go 预估近 100%，包级不降）
3. 全部用例 PASS（~20 子用例，含 12 unit 1:1 映射 + 4.5 system 回填 + 增强）
4. 注释全中文；doc.go 树含新条目 + Python 对应路径不变
5. 纯类型包无集成测试需求（规则 6/7 不适用，单包 unit 即完成态）
6. §8 回填清单 1-3 项全部执行
