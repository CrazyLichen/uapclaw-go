// Package modalityprobe 图像输入模态探测：判定模型能否接收原生图片输入。
//
// 通过向模型发送一张代码生成的 32×32 纯红 PNG 并提问颜色来判定其是否支持
// 原生图片输入；结论按 (api_base, model_name) 进程级缓存。判定未知（超时/
// 限流/截断等）不缓存，宁可下次重探。探测使用独立 RouterScope 的模型复印件，
// 失败不冷却模型组的正式部署。
//
// 文件目录：
//
//	modalityprobe/
//	├── doc.go       # 包文档
//	└── probe.go    # 探测器本体（假图问答/判定/缓存/路由隔离）
//
// 对应 Python 代码：openjiuwen/harness/image_modality_probe.py
package modalityprobe
