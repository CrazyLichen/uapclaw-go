// Package harnessrsi 提供单 harness 自我改进（standalone Harness improvement）的
// 共享契约、编码守卫、模型调用重试守卫与运行时策略校验。
//
// 本包是 17.4 harness 演进的地基（对应上游 openjiuwen/rsi/harness_rsi/ 顶层散件），
// 供 17.4b（诊断分析）、17.4c（member_optimizer）、17.4d（单 harness 迭代循环）、
// 17.4e（auto_harness 流水线）消费。config 与 data_loader 子包随本包交付。
//
// 文件目录：
//
//	harness_rsi/
//	├── doc.go                  # 包文档
//	├── schema.go               # 共享数据契约（7 个 frozen dataclass 等价物）
//	├── text_encoding.go        # mojibake 乱码评分与修复
//	├── model_call.go           # 模型调用瞬态失败重试守卫
//	├── feedback.go             # 输出修复预算与停滞追踪
//	├── progress.go             # 优化编排器进度事件
//	├── runtime_policy.go       # runtime.yaml 白名单校验（宿主上限封顶）
//	├── runtime_reliability.go  # 单 Agent 可靠性轨（占位工厂 ⤴️ 25.1g 回填）
//	├── artifact_io.go          # 文件系统 YAML/JSON 产物小助手
//	├── config/                 # AutoCoordinatingHarnessConfig 全链配置
//	├── data_loader/            # 评估数据集载入与批次规划
//	└── resource/              # 默认配置模板（go:embed）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/（顶层 8 文件）
package harnessrsi
