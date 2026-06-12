// Package codec 提供存储层编解码器。
//
// 本包定义了存储内容的加解密编解码接口和实现，
// 用于 SqlMessageStore 等存储组件对持久化数据进行透明加解密。
// 当加密密钥为空时，编解码器以 passthrough 模式运行，
// 不对数据进行任何加解密处理。
//
// 文件目录：
//
//	codec/
//	├── doc.go                  # 包文档
//	├── aes_storage_codec.go    # AES-256-GCM 存储编解码器
//	└── aes_storage_codec_test.go
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/codec/
//
// 核心类型/接口索引：
//
//	AesStorageCodec — AES-256-GCM 存储编解码器，key 为空时 passthrough，key 非空时严格模式
package codec
