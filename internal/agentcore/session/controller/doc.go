// Package controller 提供会话控制器，管理会话生命周期、作用域隔离、数据持久化和跨 Agent 批量操作。
//
// 本包实现链式会话（ChainSession）和两级控制器（SessionController 管理单 Agent、
// GlobalSessionController 管理全局），支持 Scope/Subject 维度的数据隔离、
// 下游会话单向可见性、磁盘持久化（sessions.json + state.data + downstreams/*.link）。
//
// 文件目录：
//
//	controller/
//	├── doc.go                # 包文档
//	├── scope.go              # Scope/Subject 接口体系 + SessionScope + SessionScopeKey
//	├── scope_factory.go      # SessionScopeFactory 工厂
//	├── schema.go             # SessionMeta + ScopeSessionsMeta 元数据
//	├── data_container.go     # DataContainer 接口 + 工厂 + AgentSessionContainer
//	├── chain_session.go      # ChainSession 链式会话
//	├── session_controller.go # SessionController 单 Agent 管理器
//	├── global_controller.go  # GlobalSessionController 全局单例 + 便捷方法 + 回调
//	└── paths.go              # SessionPaths 路径工具
//
// 对应 Python 代码：openjiuwen/core/session/session_controller/
//
// 核心类型/接口索引：
//
//	Scope                — 隔离边界接口
//	MainScope            — 主域
//	Subject              — 会话参与者接口
//	DirectSubject        — 私聊参与者
//	GroupSubject         — 群聊参与者
//	GroupUserSubject     — 群内用户参与者
//	SessionScope         — 会话作用域（Scope + Subject）
//	SessionScopeKey      — 全局唯一键（agent:{id}:{scope}）
//	SessionScopeFactory  — 作用域工厂
//	SessionMeta          — 单会话元数据
//	ScopeSessionsMeta    — 单 Scope 下会话元数据集合
//	DataContainer        — 数据容器接口
//	StateAccessor        — 会话状态访问最小接口
//	Permission           — 访问权限枚举
//	SharingPolicy        — 下游共享策略
//	DataContainerFactory — 数据容器工厂
//	AgentSessionContainer — 默认数据容器（委托 StateAccessor）
//	ChainSession         — 链式会话
//	SessionController    — 单 Agent 会话管理器
//	GlobalSessionController — 全局会话控制器单例
//	SessionPaths         — 路径工具
package controller
