// Package taskrender 任务看板行/看板渲染（render_task_line / render_task_board）。
//
// 渲染格式的单一真源：进程内 TaskBoardHandler（coordination handlers）与
// 进程外成员（external 包）共用。下沉为独立叶子包的原因：external 包经
// spawn.go 反向依赖 agent 包，coordination handlers 直接 import external
// 会构成包级循环——同 tracker 子包（D-9）先例，按规则 9.6 登记为已知偏差
// D-25.4-1（包归属：Python 单文件 external/format.py 的渲染段 → Go 叶子子包），
// 回收条件 = external 包拆出不含 spawn 依赖的 format 子包。
//
// 文件目录：
//
//	taskrender/
//	├── doc.go           # 包文档
//	└── task_render.go  # TaskLike / RenderTaskLine / RenderTaskBoard
//
// 对应 Python 代码：openjiuwen/agent_teams/external/format.py（渲染段 L28-210）
package taskrender
