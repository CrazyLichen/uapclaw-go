// Package external 提供外部 CLI 成员全层：静态知识层（cli_agent 子层）+
// 生命周期编排层（client/runtime/member_runtime）+ AgentTemplate 翻译面。
//
// 文件目录（对应 Python openjiuwen/agent_teams/external/ 全层，15.16e 收口批
// 自 runtime/externalcli 聚合包整迁回归位，D-15.16-e20）：
//
//	external/
//	├── doc.go               # 包文档
//	├── seam.go              # Go 版 lazy import 注入（tools 侧 6+2 符号回填）
//	├── manifest.go          # AgentTemplate runtime → ExternalCliAgentSpec（manifest.py）
//	├── backends.go          # cli_agent/backends.py 190 行 1:1
//	├── model_compat.go      # cli_agent/model_compat.py 110 行 1:1
//	├── adapters.go          # cli_agent/adapters.py 552 行 1:1
//	├── transport/           # cli_agent/transport/ 子目录
//	├── injector.go          # cli_agent/injector.py
//	├── session_cleanup.go   # cli_agent/session_cleanup.py
//	├── spawn.go             # cli_agent/spawn.py
//	├── client.go            # external/client.py 593 行 1:1
//	├── runtime.go           # external/runtime.py
//	├── member_runtime.go    # external/member_runtime.py
//	├── reliability.go       # external/reliability.py
//	├── descriptor.go        # external/descriptor.py
//	├── catalog.go           # 成员目录查询（Go 侧辅助，无 Python 锚点）
//	├── paths.go             # CLI 状态路径（Go 侧辅助）
//	├── model_plan.go        # 模型规划（Go 侧辅助）
//	└── activity_emitter.go  # workflow/backends/activity_emitter.py（归包偏差 D-15.16-a4）
//
// 15.16e 收口批（同日二轮）：runtime/externalcli 聚合包整体回归本包——
// 14 模块 + transport 子目录全部 1:1 对位 Python external/ 布局；
// e14 adapter 双缝撤销（同包直调）；e20 登记 Go 版 lazy import 函数缝。
//
// 依赖方向（Python lazy import → Go 函数缝，D-15.16-e20）：本包顶层
// （client/member_runtime）静态 import tools（Python client.py:36-39 同款）；
// tools 对本包的 6+2 个纯函数翻译消费经 tools/external_seam.go 的函数变量，
// 由本包 seam.go 的 init 回填——与 Python 函数内 lazy import 运行期解析语义
// 等价，消除了 tools→external 的静态环边。
//
// 对应 Python 代码：openjiuwen/agent_teams/external/ 全层。
package external
