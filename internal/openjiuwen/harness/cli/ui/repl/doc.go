// Package repl REPL 交互循环。
//
// 全量对齐 openjiuwen/harness/cli/ui/repl.py（1316 行）：RunREPL 主循环
// （slash 分派 → 技能命令展开 → shell 透传 → 普通查询流渲染 → 中断恢复
// 循环）、InterruptManager 三层 Ctrl+C、八个内置 slash 处理器
// （/auto-harness 做桩挂 ⏵ 领域十四）、交互请求渲染与按 kind 形态的
// 应答收集（ask_user 逐问 / 审批 y-a-n / 自由形态 JSON 优先）、欢迎横幅、
// SKILL.md 描述读取与技能目录扫描。
//
// 已知偏差 D-prompt：Python 的 prompt_toolkit.PromptSession 提供行编辑、
// 历史浏览与 slash 补全；Go 侧默认 bufio 按行读取（PromptFn 可注入），
// go-prompt 接线 ⏵ 后续打磨。
//
// 文件目录：
//
//	repl/
//	├── doc.go           # 包文档
//	├── repl.go         # RunREPL 主循环 / InterruptManager / slash 处理器 / 交互应答 / 技能扫描
//	├── repl_test.go    # 纯函数层用例
//	└── repl_run_test.go # 主循环 / 中断恢复 / slash 分派用例
//
// 对应 Python 代码：openjiuwen/harness/cli/ui/repl.py（1316 行）
package repl
