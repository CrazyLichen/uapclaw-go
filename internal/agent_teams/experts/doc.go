// Package experts 把 AgentTemplate 装配成 Team 成员的唯一转换层。
//
// 模板成员 = persona（模板 prompt_sections 渲染拍平成成员私有 prompt，Team 把人格
// 放进成员 prompt lane 而不是让 harness 替换 prompt sections）+ 能力快照
// （去掉 prompt_sections 的模板，存 DeepAgentSpec.agent_template_spec，由
// NativeHarness 在成员运行前热挂载 skills/tools/rails/MCPs，并把模板技能目录名
// 并集进 DeepAgentSpec.skills）。
//
// 预定义 AgentGroup 成员（host 在建团前装配）调用 ComposeTemplateMember；
// spawn_expert 运行时生成的成员在写成员行时走同三步（persona + 快照），构建
// 成员时应用快照。同一模板 + 同一 base spec 在两条路径产出相同 spec 与 persona。
// 本包全部纯函数；消费接线（spawn 链、catalog、external manifest 翻译、
// _EXPERT_ROLE_SECTION prompt attachment）⤵️ 15.16。
//
// 文件目录：
//
//	experts/
//	├── doc.go              # 包文档
//	└── template_member.go # persona 拍平 + 能力快照 + compose 三步
//
// 对应 Python 代码：openjiuwen/agent_teams/experts/template_member.py
package experts
