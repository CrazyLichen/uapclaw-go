package parser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pdfapi "github.com/pdfcpu/pdfcpu/pkg/api"
	pdfmodel "github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PDFParser PDF 文档解析器。
//
// 嵌入 BaseParser 仅覆写 ParseContent（对齐 Python PDFParser 仅覆写 _parse）；
// Parse/LazyParse/Process/Supports 继承 13.10 模板默认。
//
// 双通道（对齐 Python pdfplumber 语义的 Go 工程等价，偏差归档 spec §8）：
//   - 文本：ledongthuc/pdf 逐页 GetPlainText（extract_text 对应物）
//   - 图片：pdfcpu ExtractImages 抽嵌入对象原生字节（page.images +
//     crop + to_image(300) 的产出面对应物——渲染路径需 PDFium/C++，
//     抽对象产出语义等价）
//
// Python: openjiuwen/core/retrieval/indexing/processor/parser/pdf_parser.py (PDFParser)
type PDFParser struct {
	// BaseParser 默认适配器（Parse/LazyParse/Process/Supports 默认实现）
	*BaseParser
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────
// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期断言 PDFParser 实现 Parser
var _ Parser = (*PDFParser)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPDFParser 构造 PDFParser。
//
// 零参数（对齐 Python __init__(**kwargs): pass）；NewBaseParser(self)
// 注入使模板方法动态派发到本类覆写。
//
// Python: pdf_parser.py PDFParser.__init__
func NewPDFParser() (*PDFParser, error) {
	p := &PDFParser{}
	bp, err := NewBaseParser(p)
	if err != nil {
		return nil, err
	}
	p.BaseParser = bp
	return p, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册到 registry（对齐 Python @register_parser([".pdf", ".PDF"])
// import 时执行；".PDF" 被 first-wins 小写归一跳过——13.16 既定语义）。
func init() {
	RegisterParser([]string{".pdf", ".PDF"}, func() Parser {
		p, _ := NewPDFParser()
		return p
	})
}

// extractImagesByPage 用 pdfcpu 提取全部嵌入图片并按页分桶落盘。
//
// 对齐 Python _extract_images_from_pdf_page 的产出面（图片文件路径列表），
// 重组为一次性批量 + img.PageNr 分桶（pdfcpu 回调天然按页序遍历，
// 页内对象号升序——偏差归档 spec §8-5）：
//   - 命名复刻 Python：{filename}__page_{页码}__img_{页内序号}.{原生扩展名}
//     （Python 恒 .png 是渲染路径产物；Go 存原生格式——偏差 §8-3）
//   - img.Reader 为解码后字节，直接落盘
//   - 软失败：pdfcpu 出错 → Error 日志 + 空 map + nil error（图片通道独立
//     软失败，文本通道继续——对齐 Python 该路径被整体 except 吞的净效果，
//     Go 显式化日志不改变软失败语义）
//
// Python: pdf_parser.py PDFParser._extract_images_from_pdf_page
func extractImagesByPage(ctx context.Context, pdfBytes []byte, outputDir, filename string) map[int][]string {
	imgByPage := map[int][]string{}

	if err := pdfapi.ExtractImages(ctx, bytes.NewReader(pdfBytes), nil, func(img pdfmodel.Image, _ bool, _ int) error {
		data, err := io.ReadAll(img)
		if err != nil {
			return fmt.Errorf("读取图片流失败: %w", err)
		}
		// 落盘目录（对齐 Python os.makedirs(output_dir, exist_ok=True)）
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("创建图片目录失败: %w", err)
		}
		// 页内序号 = 该页已落盘张数（回调页内顺序确定性：对象号升序）
		imgIndex := len(imgByPage[img.PageNr])
		ext := img.FileType
		if ext == "" {
			ext = "png" // 防御：无扩展名回落 png
		}
		imagePath := filepath.Join(outputDir, fmt.Sprintf("%s__page_%d__img_%d.%s", filename, img.PageNr, imgIndex, ext))
		if err := os.WriteFile(imagePath, data, 0o644); err != nil {
			return fmt.Errorf("落盘图片失败: %w", err)
		}
		imgByPage[img.PageNr] = append(imgByPage[img.PageNr], imagePath)
		return nil
	}, nil); err != nil {
		// conf=nil → 默认 UnsupportedResourceSkip（不支持的格式软跳过），
		// 此处 err 为文档级失败（损坏/加密等）
		logger.Error(logComponent).
			Err(err).
			Msg("failed to extract images from pdf")
		return map[int][]string{}
	}
	return imgByPage
}
