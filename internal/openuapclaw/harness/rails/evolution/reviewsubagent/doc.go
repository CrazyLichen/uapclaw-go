// Package reviewsubagent 提供稳定的 Skill 演进审查子 agent 配置装配：
// binding 元数据、配置构建（挂五只读审查工具）、subagents 列表的 ensure/remove/replace
// 与工具 id 清洗。
//
// 偏差 D-15.24c-1（包落点）：Python review/subagent.py 与 review 包同目录，
// 但 Go 侧 review 是叶包（experience/tools 反向依赖它），装配半区需引用
// agent_evolving.tools → 下沉独立叶包承载（15.24b evoschema 叶包先例），
// 单文件对单文件的映射仍可在本 doc.go 核对。
//
// 文件目录：
//
//	reviewsubagent/
//	├── doc.go        # 包文档
//	└── subagent.go   # 审查子 agent 配置装配（subagent.py:87-220 1:1）
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/review/subagent.py
package reviewsubagent
