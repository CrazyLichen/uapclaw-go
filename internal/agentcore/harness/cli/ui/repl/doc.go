// Package repl REPL 交互循环。
//
// ⏵ 本包只落可分离的纯函数部分（spec §7.7 预告）：slash 命令表（内置
// 九个 + /quit 隐藏别名 + 启动期动态注册的技能命令）、_answered_as_typed
// 的应答形态判定、SKILL.md 描述读取与技能目录扫描（~ 展开、非存在目录
// 静默跳过）、shell 透传（! cmd）。REPL 主循环（行编辑 / 历史浏览 /
// slash 补全渲染 / 交互应答渲染）依赖真实 TTY，⏵ PTY 测试，
// .coverage-exempt 登记整文件。
//
// 文件目录：
//
//	repl/
//	├── doc.go          # 包文档
//	├── repl.go         # slash 表 / 技能扫描 / shell 透传 / 应答形态
//	└── repl_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/ui/repl.py（1316 行）
package repl
