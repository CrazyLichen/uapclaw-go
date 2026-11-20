// Package runner 非交互运行模式。
//
// RunOnce 执行一次查询并按三种格式输出：text（终端渲染，与 REPL 相同）、
// json（result/chunks/model 单对象）、stream-json（每块一行 JSONL）。
// PrintError 把 API 错误分类转成友好提示（限流 / 认证 / 上下文超长 /
// 超时）写 stderr。
//
// ⏵ backend.run_streaming 依赖 create_agent 回填（15.6-15.9）；当前
// 骨架后端返回空流，三格式分支与错误分类本身可测。
//
// 文件目录：
//
//	runner/
//	├── doc.go          # 包文档
//	├── runner.go      # RunOnce / 三格式输出 / PrintError
//	└── runner_test.go
//
// 对应 Python 代码：openjiuwen/harness/cli/ui/runner.py（146 行）
package runner
