// Package multimodal 提供多模态工具实现，包括视觉 OCR/问答、音频转写/问答/元数据、视频理解。
//
// 本包对齐 Python openjiuwen/harness/tools/multimodal/ 的完整逻辑，
// 通过 BaseModelClient 统一入口调用视觉模型、音频转写 API、ACRCloud 元数据识别等。
//
// 工具使用 tool.NewTool[I,O] 模式，工厂函数对齐 Python 的
// create_vision_tools/create_audio_tools。
//
// 文件目录：
//
//	multimodal/
//	├── doc.go               # 包文档
//	├── vision.go            # ImageOCRTool + VisualQuestionAnsweringTool + CreateVisionTools
//	├── vision_helpers.go    # 视觉辅助函数（buildImageContent/callVisionModel/extractResponseText）
//	├── audio.go             # AudioTranscriptionTool + AudioQATool + AudioMetadataTool + CreateAudioTools
//	├── audio_helpers.go     # 音频辅助函数（resolveAudioPath/getAudioDuration/encodeAudioFile/invokeACRMetadata）
//	├── video_understanding.go # VideoUnderstandingTool + NewVideoUnderstandingTool
//	├── video_helpers.go     # 视频辅助函数（normalizeVideoURL + 参数裁剪）
//
// 对应 Python 代码：openjiuwen/harness/tools/multimodal/
package multimodal
