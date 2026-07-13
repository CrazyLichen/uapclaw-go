// Package interfaces 定义 session 模块各子包共用的接口类型。
//
// 本包是 session 模块的接口层，消除各子包之间的接口重复定义和循环依赖。
// 所有子包（checkpointer、internal、interaction、session 根包）通过导入本包
// 获取统一的接口定义，避免各自重复声明或产生循环导入。
//
// 接口与 Python 的对应关系：
//   - SessionFacade      → Python agent.Session 和 node.Session 的共有方法集
//   - InnerSession      → Python BaseSession(ABC)，所有会话类型的基类
//   - BaseSession       → 已废弃的 InnerSession 别名
//   - Checkpointer       → Python Checkpointer(ABC)，检查点器接口
//   - Storage            → Python Storage(ABC)，状态存储接口
//   - AgentIDProvider    → Python hasattr(session, "agent_id") 检测
//   - TeamIDProvider     → Python hasattr(session, "team_id") 检测
//   - WorkflowIDProvider → Python hasattr(session, "workflow_id") 检测
//   - ParentProvider     → Python isinstance(session.parent(), AgentSession) 检测
//   - CheckpointerConfigProvider → Python session.config().get_env() 调用
//   - ExecutableIDProvider      → Python hasattr(session, "executable_id") 检测
//
// 文件目录：
//
//	interfaces/
//	├── doc.go            # 包文档
//	├── facade.go         # SessionFacade 门面会话共有接口
//	└── interfaces.go     # InnerSession/Checkpointer/Storage/*Provider 接口
//
// 对应 Python 代码：无独立文件，接口分散在 openjiuwen/core/session/ 各子模块中
package interfaces
