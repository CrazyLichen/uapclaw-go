// Package common 提供跨子系统共享的协议级基础能力。
//
// 本包只放"被 gateway 与 swarm 两侧共用、且不属于任何单一子领域"的协议标识
// 判定，不承载业务逻辑、不依赖上层包，可被任意子系统安全导入。
//
// 文件目录：
//
//	common/
//	├── doc.go           # 包文档
//	└── protocol_ids.go  # 外部输入的协议标识校验（会话 ID / workflow run ID）
//
// 对应 Python 代码：gateway/common/protocol_ids.py
package common
