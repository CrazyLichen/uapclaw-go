package parser

// pdfcoord gomupdf 提取门面（30.3 Task2）。
// 单点收敛 gomupdf API 调用：词框/图片字节/placement 均经本文件出包，
// 后续库 API 变动只在此单点修正（spec §4 门面纪律）。

import (
	"fmt"

	"github.com/srijanmukherjee/gomupdf"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// ──────────────────────────── 结构体 ────────────────────────────

// pdfPageData 单页提取产物（gomupdf 词框 + 图片）
type pdfPageData struct {
	// PageNo 页号（1 起）
	PageNo int
	// Words 词列表（读取序）
	Words []common.PageWord
	// Images 展示图片（fill order）
	Images []common.PageImage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// extractPDFPages 用 gomupdf 逐页提取词框与嵌入图片（placement + 字节）。
// Document 独占 MuPDF context，用后即关（gomupdf 并发纪律：每 goroutine
// 一个 Document）。
func extractPDFPages(path string) ([]pdfPageData, error) {
	doc, err := gomupdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer doc.Close() // 关闭失败仅资源回收语义，不覆盖主错误

	pages := make([]pdfPageData, 0, doc.PageCount())
	for i, page := range doc.Pages() {
		pd := pdfPageData{PageNo: i + 1}

		words, werr := page.Words() // 词框 = 字符 quad 并集（PDF points）
		if werr != nil {
			return nil, fmt.Errorf("page %d words: %w", i+1, werr)
		}
		for _, w := range words {
			pd.Words = append(pd.Words, common.PageWord{
				Text: w.Text,
				// gomupdf.Rect 为 {X,Y,W,H}（左上原点，y 向下）；折算为
				// [x0,y0,x1,y1] 四元组（同左上原点，pdf.js viewport 直接可用）
				BBox: common.BBox{w.BBox.X, w.BBox.Y, w.BBox.X1(), w.BBox.Y1()},
				Line: w.Line,
			})
		}

		infos, ierr := page.GetImages() // placement bbox + 原始尺寸（fill order）
		if ierr != nil {
			return nil, fmt.Errorf("page %d images: %w", i+1, ierr)
		}
		for _, info := range infos {
			extracted, derr := page.ExtractImage(info.Index)
			if derr != nil {
				return nil, fmt.Errorf("page %d image %d: %w", i+1, info.Index, derr)
			}
			if extracted == nil {
				continue
			}
			pd.Images = append(pd.Images, common.PageImage{
				BBox: common.BBox{info.BBox.X0, info.BBox.Y0, info.BBox.X1, info.BBox.Y1},
				Data: extracted.Bytes,
				Ext:  extracted.Ext,
			})
		}
		pages = append(pages, pd)
	}
	return pages, nil
}
