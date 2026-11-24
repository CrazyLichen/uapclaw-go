// Package discovery 提供 Symphony 的结构化 live Skill 发现面。
//
// 对应 Python 代码：openjiuwen/symphony/discovery/（3,225 行，一比一对齐）。
// 入口：config（设置）/models（扫描与缓存）/skillfs（live 目录与冻结快照）/
// toolkit（增量通知基线）/directory_toolkit（skill_index 单元工具）。
//
// 文件目录：
//
//	discovery/
//	├── doc.go                # 包文档
//	├── config.go             # DiscoverySettings 与配置装载
//	├── models.go             # SkillRecord/SkillInventory/扫描/五层缓存
//	├── skillfs.go            # SkillFS/树视图/四态策略/冻结快照
//	├── toolkit.go            # 通知基线表/DCI 结果/增量会话
//	└── directory_toolkit.go  # InstalledSkillsDirectoryToolkit/skill_index 三操作
package discovery
