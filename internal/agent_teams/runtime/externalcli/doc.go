// Package externalcli 提供外部 CLI 团队成员的运行时支撑。
//
// 本包对应 Python openjiuwen/agent_teams/spawn/external_cli_spawn.py。
// 按用户 2026-11-28 拍板（方案①），15.12 先落路径段（cwd 三分离与
// workspace meta 提取）；spawn 主体（成员生命周期、team-join 描述符、
// CLI 进程启动）挂 ⏵ 15.13 续写本包（偏差 D-15.12-e：Python 单文件
// 由两批填充同一 Go 包）。
//
// 文件目录：
//
//	externalcli/
//	├── doc.go           # 包文档
//	└── paths.go        # cwd 三分离与 workspace meta 路径段
//
// 对应 Python 代码：openjiuwen/agent_teams/spawn/external_cli_spawn.py（行 239-298 段）
package externalcli
