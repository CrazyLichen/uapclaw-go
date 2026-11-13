// 调试 CLI 可执行入口。
//
// 三个二进制并列：uapclaw（网关）/ uapclaw-process（process_cli）/
// uapclaw-debug（harness/cli 调试）。参数极简（--query / --output）：
// 有 --query 走 runner.RunOnce 单命令；否则走完整命令树（chat REPL）。
//
// 不经 harness/cli/cli.py 的参数解析层（spec D2：该文件不交付，
// 与已交付的 processcli 功能重叠）。
//
// 对应 Python 代码：openjiuwen/harness/cli/__main__.py（5 行）
package main
