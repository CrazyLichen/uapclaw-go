// Package utils 提供通用工具函数。
//
// 包含单例模式、哈希工具、网络工具、字典操作、端口等待、后台任务管理
// 和连接池管理等工具，作为 agentcore 和 swarm 共用的工具基础层。
//
// 文件目录：
//
//	utils/
//	├── doc.go            # 包文档
//	├── singleton.go      # 泛型单例：Singleton[T]
//	├── hash.go           # 哈希工具：SHA256 密钥生成
//	├── net.go            # 网络工具：本机 IP + URL 脱敏 + 递归值脱敏
//	├── dict.go           # 字典工具：嵌套字典操作 + RemoveZeroValues + ValidateArgs
//	├── port.go           # 端口等待：WaitForTCPPort + WaitForPIDExit
//	├── background.go     # 后台任务：BackgroundTask + Task + TaskManager
//	└── pool.go           # 引用计数连接池：RefCountedResource + TransportPool + ResourcePool[T]
//
// 对应 Python 代码：
//   - jiuwenswarm/common/utils.py
//   - openjiuwen/core/common/utils/
//   - openjiuwen/core/common/clients/
//   - openjiuwen/core/common/background_tasks.py
//   - openjiuwen/core/common/task_manager/
package utils
