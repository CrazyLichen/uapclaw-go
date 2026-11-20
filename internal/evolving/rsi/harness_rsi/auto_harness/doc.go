// Package autoharness 实现 Auto Harness Agent（自动优化代理）的契约件与基建层。
//
// 本包对齐 Python openjiuwen/rsi/harness_rsi/auto_harness/：
//   - schema.go      —— 数据契约（配置/枚举/产物 dataclass，对齐 schema.py）
//   - pipelines_constants.go —— 流水线名常量前置件（⤴️e4 回填完整流水线定义）
//   - resource/      —— 出厂资源 go:embed（config.yaml / ci_gate.yaml）
//   - infra/         —— 基建工具层（git/worktree/CI 门禁/parsers/预算/扩展合并等）
//
// 本节（17.4e2）不含 stages/agents/pipelines 装配（17.4e3/e4 消费本包；
// evaluator 走 17.4f 评估桩，取消机制桩在 17.4f/17.7 回填）。
//
// 文件目录：
//
//	auto_harness/
//	├── doc.go                  # 包文档
//	├── pipelines_constants.go  # 流水线名常量前置件（对齐 pipelines/__init__.py）
//	├── schema.go               # 契约件（对齐 schema.py）
//	├── resource/               # 出厂资源 go:embed
//	└── infra/                 # 基建层（对齐 auto_harness/infra/）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/auto_harness/
package autoharness
