# devtools agentbuilder utils（14.1）设计文档

> 领域十四：devtools 开发者工具面 · 第 14.1 步
> Python 参考：`openjiuwen/dev_tools/agent_builder/utils/`（utils.py 253 行 + constants.py 62 行）
> 决策日期：2026-10-25 · brainstorming 全 5 节经用户逐节确认

## 1. 背景与定位

dev_tools 在 Python 项目中是与运行时会话链路（Gateway/E2A/AgentServer/DeepAgent）完全平行的"开发者工具面"，全仓 import 验证零交叉。agent_builder 是其核心子模块（NL→Agent DSL 生成器），而 **14.1 的 utils 是 agent_builder 的最底层地基**：所有 LLM 调用（Clarifier/Generator/DLGenerator/ResourceRetriever 等）的输出都经过本包的"剥围栏→安全解析→格式化上下文"管线。

**流程位置**：

```
开发者/前端 → AgentBuilder.build_agent(14.9)
   → AgentBuilderExecutor(14.5) ── create_core_model ─→ Model(已有)
   → BaseAgentBuilder 三态状态机(14.6)
       ├─ ResourceRetriever(14.4) ──┐
       ├─ Clarifier/Generator(14.7) ├─ 全部调用 ─→ 【14.1 共享 utils】
       └─ DLGenerator/Reflector(14.8)┘
             ├─ ExtractJSONFromText：LLM 回复剥掉 ``` 围栏（13 处消费，解析咽喉）
             ├─ FormatDialogHistory：多轮对话压成 prompt 文本（8 处）
             ├─ SafeJSONLoads：LLM 回复 JSON 容错解析（8 处）
             └─ LoadJSONFile：加载 plugins.json 资源清单（2 处）
```

**已拍板决策**（brainstorming 三问）：
- 方案 A：utils + constants **全量对齐**（含死代码——2 死函数 + 13 死常量，对齐项目 13.16"死代码也复刻"先例）
- 结构一：包路径 `internal/agentcore/devtools/agentbuilder/utils/`（Python 目录树一比一映射）
- 测试方案 A：按 Go 文件边界拆分（Python system 测试文件跨 14.1/14.2/14.3 三节共享，按归属拆入各自 _test.go）

## 2. 包结构与文件规划

```
internal/agentcore/devtools/agentbuilder/utils/
├── doc.go              # 包文档：文件目录树 + Python 对应路径
├── constants.go        # 20 常量（4 workflow 话术 + 3 默认配置 + 3 资源类型 + 1 正则
│                       #   + 2 API + 2 进度 + 5 长度限制，分组注释对齐 Python）
├── utils.go            # 7 函数 + jsonExtractRegexp/sessionIDRegexp 两个预编译 var
├── constants_test.go   # 21.5 用例（Python test_constants.py 21 + 混合用例常量半边）
└── utils_test.go       # 39.5 用例（Python test_utils.py 36 + system 3 + LoadJSONFile 4 增强用例）
```

**依赖**（全部就绪，零新增）：

```
internal/common/exception  → BuildError / StatusCommonJsonInputProcessError(188002 已预埋)
                             / StatusContextMessageInvalid(153003 已预埋)
internal/common/logger     → logger.Info/Debug/Warn/Error(ComponentAgentCore)
encoding/json, regexp, os, maps, errors, fmt → 标准库
```

**不复用声明**：
- `config.DeepMerge` 不复用——maxDepth=4 截断 + "用户有模板无保留"规则与 Python deep_merge_dict 语义不同
- `ace.SafeJSONLoads` 不复用——签名固定 `map[string]any` 无 default 三层分支语义
- 两者分属不同包，自建无命名冲突

## 3. 函数签名与语义翻译

| # | Go 签名 | 语义要点 |
|---|---------|---------|
| 1 | `func ExtractJSONFromText(text string) string` | 空串原样返回；`(?s)```(?:json)?\s*(.*?)\s*```` FindStringSubmatch 取第一组；无匹配返回原文 |
| 2 | `func FormatDialogHistory(history []map[string]string, separator ...string) string` | 变长参数默认 "\n"（13.x resolveChunkSize 先例）；空历史返空串；缺 role→"unknown"、缺 content→""（双值 ok 判断） |
| 3 | `func SafeJSONLoads(text string, def ...any) (any, error)` | 三层：①空串短路返 default ②def 未传→解析失败 `BuildError(StatusCommonJsonInputProcessError)` ③def 已传→Error 日志后返 default。变长参数区分"没传"与"传了 nil" |
| 4 | `func MergeDictLists(existing, newItems []map[string]any, uniqueKey ...string) []map[string]any` | uniqueKey 默认 "resource_id"；原地 append 返回（Python 就地语义，base.py 调用方依赖此行为）；isTruthy 辅助：key 缺失或空串/0/false 跳过 |
| 5 | `func LoadJSONFile(filePath string) (map[string]any, error)` | 不存在→`fmt.Errorf("File not found: %s: %w", path, fs.ErrNotExist)`+Error 日志；空文件→`{}`+Warn；顶层非 dict→error；解析异常→`BuildError(StatusContextMessageInvalid)`+cause 链 |
| 6 | `func DeepMergeDict(base, update map[string]any) map[string]any` | 死代码但实现；maps.Clone 浅拷贝起点+递归新建；双方值均 map 才递归否则 update 覆盖；不改原 map |
| 7 | `func ValidateSessionID(sessionID string) bool` | 空串 false；`^[a-zA-Z0-9_-]+$` 包级预编译 |

## 4. 常量实现

- Go 驼峰导出命名（`WorkflowRequestContent` 等 20 个）
- `APIBasePath = "/api/" + APIVersion` 编译期拼接等价 Python f-string
- untyped const（`ProgressUpdateInterval = 0.1` 等），Python isinstance 断言转 Go 值断言
- `JSONExtractPattern` 字符串常量本体 + utils.go `var jsonExtractRegexp = regexp.MustCompile(JSONExtractPattern)` 预编译（正则已通过 Go/Python 双端 9 边界用例交叉验证逐字节等价）
- 13 死常量 + 2 死函数全量落地（方案 A）

## 5. 测试设计

**constants_test.go**（21.5 用例）：按 Python 7 个 Test class 转 7 个 Go Test 函数 + t.Run 子用例；断言从 isinstance/isclose 转为编译期类型保证 + 值断言。

**utils_test.go**（39.5 用例）：

| Python class | Go Test 函数 | 转换要点 |
|--------------|-------------|---------|
| TestExtractJsonFromText(7) | TestExtractJSONFromText | None 入参转空串断言 |
| TestFormatDialogHistory(5) | TestFormatDialogHistory | missing keys 用 Go map 省略键表达 |
| TestSafeJsonLoads(5) | TestSafeJSONLoads | 数组断言 `[]any{float64(1),2,3}`（JSON number→float64） |
| TestValidateSessionId(8) | TestValidateSessionID | 4 正 4 反 |
| TestMergeDictLists(6) | TestMergeDictLists | missing unique key 跳过语义 |
| TestDeepMergeDict(5) | TestDeepMergeDict | 不改原 map 断言增强为嵌套层验证（测试增强非偏差） |
| （Python 无） | TestLoadJSONFile | **4 新增用例**：不存在→errors.Is(fs.ErrNotExist)/空文件→{}/非 dict→error/正常→内容；t.TempDir() 临时文件 |
| system 3 个 | TestUtils集成_* | JSON 提取解析工作流（LLM 回复样例两步链路）/对话历史格式化（CJK）/真实模式 SessionID 校验 |

**system 用例归属拆分**（回填锚点）：test_utils_integration.py 共 12 用例——3 个纯 14.1 并入 utils_test.go；`test_constants_enums_consistency` 混合用例常量半边并入 constants_test.go、枚举半边留 14.2；3 个枚举用例归 14.2；4 个 progress 用例归 14.3。

## 6. 显式偏差记录（Go↔Python）

1. **SafeJSONLoads 返回形态**：Python `Optional[Any]`+抛异常 → Go `(any, error)` 双返回；测试 pytest.raises 转 assert err != nil
2. **FileNotFoundError**：Go 用 fs.ErrNotExist 包裹，调用方 errors.Is 判别
3. **None 语义消失**：3 个 None 入参用例转空串等价断言（Go 字符串零值与 falsy 分支重合）
4. **JSON number 类型**：safe_json_loads 数组断言 float64（Go json 反序列化默认）
5. **测试增强**（非偏差）：LoadJSONFile 4 用例（Python 基线 0）+ DeepMergeDict 嵌套不改原断言

## 7. 日志同步

Python utils.py 4 处 logger + JsonUtils 内部 2 处（default 非空分支的 decode/type/value error）全部等价落位，组件 `ComponentAgentCore`，Msg 保留英文（logger 语言规则）：

- LoadJSONFile：`"File not found"`（Error，file_path 字段）/ `"JSON file is empty"`（Warn，file_path 字段）
- SafeJSONLoads：解析失败 Error 日志（default 已传分支）

## 8. 回填清单

| # | 回填项 | 目标位置 | 时机 |
|---|--------|---------|------|
| 1 | 领域十四章节写入（14.1~14.14 骨架，14.1 ✅ 其余 ☐/⤵️待裁） | IMPLEMENTATION_PLAN.md | spec 批准后立即 |
| 2 | 14.1 状态 ☐→✅ + 产出摘要 | 同上 | 实施完成后 |
| 3 | A-4 缺口决策更新（dev_tools→"领域十四已立项"） | 记忆 agentcore-test-alignment-gaps.md | 实施完成后 |
| 4 | system 用例归属预标注（14.2 3 个/14.3 4 个） | 计划文档 14.2/14.3 行内 ⤴️ | 14.1 完成时 |
| 5 | 混合用例拆分记录 | spec 本文件 §5 + 14.2 行内 | 已闭环 |
| 6 | 死代码全量决策 | spec 本文件 §1/§4 | 已闭环 |
| 7 | 错误码 188002/153003 消费 | 自动闭环 | 实施后自然达成 |

## 9. 验收标准

1. 5 文件落地，gofmt + 声明顺序合规（规范 2）
2. `go test ./internal/agentcore/devtools/agentbuilder/utils/ -cover` ≥ 85%（预估 95%+）
3. 全部用例 PASS（约 61 子用例）
4. 4+2 处日志等价落位（ComponentAgentCore，Msg 英文）
5. 全部注释中文；doc.go 含目录树 + Python 对应路径
6. 纯函数包无 tests/integration 需求（集成测试规则 6/7 不适用，单包 unit 即完成态）

## 10. 章节衔接

- 14.2 enums / 14.3 progress：同包续建，零前置依赖
- 14.4 ResourceRetriever：直接消费本包 4 函数
- 换序结论：14.1 无需换序；唯一换序候选是 14.5 createCoreModel 的 provider_map 语义确认（届时单独讨论）
- 14.11-14.14（prompt_builder/skill_creator/skill_evaluator/tune）处置**仍待裁定**，计划骨架中以 ⤵️ 占位，裁定后按 T-03（跳过+理由）/9.24（延后+回填点）先例落地，内容不丢失
