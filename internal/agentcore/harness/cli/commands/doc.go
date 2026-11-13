// Package commands CLI 的 cobra 命令树。
//
// 根命令（全局 flags：model/provider/api-key/api-base/remote/verbose/
// workspace + 无子命令时按 stdin 类型分派：TTY→chat、管道→run）+
// chat（REPL 装配 + SessionStore）+ run（非交互，支持 `run -` 管道）+
// auto-harness 组骨架。auto-harness 的 run / experience / gap-analyze /
// verify-ext 子命令依赖优化链路，⤵️ 15.6+ 回填。
//
// 覆盖率豁免见 .coverage-exempt（os.Exit 错误分支）。
//
// 文件目录：
//
//	commands/
//	├── doc.go          # 包文档
//	├── root.go         # NewRootCommand / chat / run / auto-harness
//	└── root_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/cli.py（1117 行）
package commands
