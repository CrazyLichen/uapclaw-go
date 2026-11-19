// Package externalclipanel 覆盖 15.16d 第三方面板配置的跨子系统契约集成测试。
//
// 对齐 Python jiuwenswarm/common/{external_cli_catalog,config_panel/external_cli_read,
// config_panel/config_set_handlers}.py 的保存→探测→展平→装配链，以及
// agent-core workflow/backends/external_model.py 的 spawn 模型挑选解析链。
//
// 本目录只验证"跨子系统契约"：config 写链（swarm/common）→ 目录探测
// （externalcli + harness_providers 注册缝）→ 面板展平四键 → 装配预处理
// （swarm/agents/harness/team）→ transport 装配（swarm/agents/swarm）→
// spawn 模型选择（externalcli/model_plan → schema/adapter 边界）。
// 私有分支（claude/codex 探测体 ⤴️15.7/15.8、安装器 D-15.16-d7 桌面版专属）
// 不在集成面内。
//
// 文件目录：
//
//	externalclipanel/
//	├── doc.go                    # 包文档
//	└── panel_pipeline_test.go    # PanelPipelineSuite：三条全链用例
//
// 对应 Python 代码：jiuwenswarm/common/external_cli_catalog.py ·
// jiuwenswarm/common/config_panel/{external_cli_read,config_set_handlers}.py ·
// openjiuwen/agent_teams/workflow/backends/external_model.py ·
// jiuwenswarm/agents/harness/team/config_loader.py · agents/swarm/assembly.py
package externalclipanel_test
