// Package constants 提供跨包共享的安全护栏常量。
//
// 本包只承载 constant.py 中尚无 Go 归属的常量；已有归属的常量
// （INPUTS_KEY/CONFIG_KEY → graph/executable、END_FRAME → stream_actor 等）
// 不迁入，避免双落点。
//
// 文件目录：
//
//	constants/
//	├── doc.go        # 包文档
//	└── constants.go # 安全护栏常量
//
// 对应 Python 代码：openjiuwen/core/common/constants/constant.py
package constants
