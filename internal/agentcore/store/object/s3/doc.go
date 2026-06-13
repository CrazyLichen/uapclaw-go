// Package s3 提供对象存储的 S3 兼容后端实现。
//
// 基于 aws-sdk-go-v2 实现 S3 兼容的对象存储客户端，
// 支持华为云 OBS 以及任何 S3 兼容的对象存储服务。
// 客户端为长生命周期，并发安全，底层连接池自动管理。
//
// 文件目录：
//
//	s3/
//	├── doc.go     # 包文档
//	└── s3.go      # S3Client 实现（NewS3Client + 6 个接口方法）
//
// 对应 Python 代码：openjiuwen/core/foundation/store/object/aioboto_storage_client.py
package s3
