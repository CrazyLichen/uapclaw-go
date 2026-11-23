// Package llm 提供工作流 LLM 调用组件（8.18 LLMComp、8.23 IntentDetectionComp）：
// 构图期组件注册、运行期 Executable 执行「组装提示词 → 调用 foundation/llm.Model
// → 按输出配置格式化响应」的完整链路；附六个工具类（校验/Schema 生成/JSON 解析/
// 输出格式化/提示词改写）的包级函数等价面。
//
// 职责边界（spec docs/superpowers/specs/2026-11-21-llm-comp-8.18-design.md §1）：
//   - SSE 解析、多 provider 分发、首 token/idle 超时全部复用 foundation/llm.Model，
//     本包零 SSE 代码；调用 Model 时不传 tools——纯单轮文本进出
//   - extractContent 只取 .content，reasoning/usage 字段逐字对齐即丢弃
//
// 文件目录：
//
//	llm/
//	├── doc.go                      # 包文档
//	├── llm_comp.go                # 8.18 LLMComp：常量/枚举/配置/校验链/工具类/LLMExecutable/LLMComponent
//	└── intent_detection_comp.go   # 8.23 IntentDetectionComp：常量/配置/解析层/执行体/组合面
//
// 对应 Python 代码：openjiuwen/core/workflow/components/llm/
// （llm_comp.py → llm_comp.go；intent_detection_comp.py → intent_detection_comp.go）
package llm
