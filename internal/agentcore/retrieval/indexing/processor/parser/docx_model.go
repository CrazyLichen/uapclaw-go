package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// docxModel DOCX 解析中间模型（stdlib 自研，非导出）。
//
// 持有 zip 解包后的部件字节与三张解析表，为 WordParser 提供四保真点数据：
//  1. 保序块遍历（parseBlocks）
//  2. 样式名（styleName + babelFishInternal2UI）
//  3. 合并单元格（rowCells 展开）
//  4. paragraph.text（paragraphText 拼装）
//
// 对齐 python-docx Document 的读侧子集（word_parser.py 实际消费面）。
type docxModel struct {
	// docBytes word/document.xml 原始字节
	docBytes []byte
	// styles styleId → w:name（styles.xml 的 w:style 表）
	styles map[string]string
	// rels rId → target（word/_rels/document.xml.rels 的关系表）
	rels map[string]string
	// files zip 全部部件（图片按 rels target 查字节）
	files map[string][]byte
	// docDir document.xml 所在目录（media 路径拼接基准，通常 "word"）
	docDir string
}

// docxBlock 文档块（段落或表格）。
//
// 对齐 python-docx iter_inner_content 的保序产出（Paragraph|Table 交替）。
type docxBlock struct {
	// kind 块类型："para" | "table"
	kind string
	// text 段落文本（paragraph.text 语义；表格块为空）
	text string
	// styleID 段落样式 ID（w:pStyle@w:val；无样式空串）
	styleID string
	// styleUI 样式 UI 名（w:name 经 BabelFish；无样式空串）
	styleUI string
	// rows 表格行（合并单元格展开后的网格；段落块为 nil）
	rows [][]string
	// paraXML 段落原始 XML 字节（图片提取用，EncodeToken 重建）
	paraXML []byte
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// docxMainNS wml 主命名空间
	docxMainNS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	// docxRelNS r 命名空间（r:id/r:embed 属性）
	docxRelNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	// docxDrawingNS a 命名空间（a:blip）
	docxDrawingNS = "http://schemas.openxmlformats.org/drawingml/2006/main"
	// docxRelTypeStyles styles 关系类型
	docxRelTypeStyles = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	// docxStylesRelKey styles 关系 target 的表内专用键
	//（rels 表 rId 与 target 多对一共存，styles target 单独记一键便于按类型取）
	docxStylesRelKey = "__styles__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// babelFishUI python-docx BabelFish 内部名 → UI 名转换表。
//
// Python: docx/styles/__init__.py BabelFish.ui_style_names（style_aliases
// 12 对反转，逐字复制：Caption/Footer/Header/Heading 1..9）
var babelFishUI = map[string]string{
	"caption":   "Caption",
	"footer":    "Footer",
	"header":    "Header",
	"heading 1": "Heading 1",
	"heading 2": "Heading 2",
	"heading 3": "Heading 3",
	"heading 4": "Heading 4",
	"heading 5": "Heading 5",
	"heading 6": "Heading 6",
	"heading 7": "Heading 7",
	"heading 8": "Heading 8",
	"heading 9": "Heading 9",
}

// ──────────────────────────── 导出函数 ────────────────────────────
// ──────────────────────────── 非导出函数 ────────────────────────────

// loadDocxModel 打开 DOCX zip 并解析三部件。
//
// 部件缺失容忍度对齐 python-docx：document.xml 缺失 → error（打开即失败，
// 外层软失败）；styles.xml/rels 缺失 → 空表（python-docx 容忍无样式文档）。
func loadDocxModel(filePath string) (*docxModel, error) {
	zr, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	dm := &docxModel{
		styles: map[string]string{},
		rels:   map[string]string{},
		files:  map[string][]byte{},
	}

	// 全量解包到内存（fixture 级文档 KB 量级；python-docx 也是惰性但读侧等效）
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip entry %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("read zip entry %s: %w", f.Name, err)
		}
		// zip 条目名统一正斜杠（Windows 产物容错）
		name := strings.ReplaceAll(f.Name, "\\", "/")
		dm.files[name] = data
	}

	docBytes, ok := dm.files["word/document.xml"]
	if !ok {
		return nil, fmt.Errorf("word/document.xml not found in docx")
	}
	dm.docBytes = docBytes
	dm.docDir = "word"

	// rels：word/_rels/document.xml.rels（缺失容忍）
	if relsBytes, ok := dm.files["word/_rels/document.xml.rels"]; ok {
		if err := dm.parseRels(relsBytes); err != nil {
			return nil, err
		}
	}

	// styles：rel target 解析（对齐 python-docx 经 document.xml.rels 找 styles part）
	if target, ok := dm.relsByType(docxRelTypeStyles); ok {
		stylePath := joinDocPath(dm.docDir, target)
		if styleBytes, ok := dm.files[stylePath]; ok {
			if err := dm.parseStyles(styleBytes); err != nil {
				return nil, err
			}
		}
	}
	return dm, nil
}

// parseRels 解析关系表。
//
// Python: docx.opc.package 惰性解析；Go 直接全量建表。
func (dm *docxModel) parseRels(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
			id, target, relType := "", "", ""
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "Id":
					id = a.Value
				case "Target":
					target = a.Value
				case "Type":
					relType = a.Value
				}
			}
			if id != "" && target != "" {
				// rId → target（hyperlink 外链与 styles/image 关系共用表）
				dm.rels[id] = target
				if relType == docxRelTypeStyles {
					dm.rels[docxStylesRelKey] = target
				}
			}
		}
	}
}

// parseStyles 解析 styles.xml 的 w:style → styleId/w:name 表。
func (dm *docxModel) parseStyles(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var inStyle bool
	var styleID, styleName string
	flush := func() {
		if inStyle && styleID != "" && styleName != "" {
			dm.styles[styleID] = styleName
		}
		inStyle, styleID, styleName = false, "", ""
	}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			flush()
			return nil
		}
		if err != nil {
			return err
		}
		switch elem := tok.(type) {
		case xml.StartElement:
			switch elem.Name.Local {
			case "style":
				flush()
				inStyle = true
				for _, a := range elem.Attr {
					if a.Name.Local == "styleId" {
						styleID = a.Value
					}
				}
			case "name":
				if inStyle {
					for _, a := range elem.Attr {
						if a.Name.Local == "val" {
							styleName = a.Value
						}
					}
				}
			}
		case xml.EndElement:
			if elem.Name.Local == "style" {
				flush()
			}
		}
	}
}

// styleName 返回 styleId 对应的原始 w:name（BabelFish 输入侧）。
func (dm *docxModel) styleName(styleID string) string {
	return dm.styles[styleID]
}

// relsByType 按 rel 类型取 target（仅 styles 用）。
func (dm *docxModel) relsByType(relType string) (string, bool) {
	_ = relType // 当前仅 styles 一类需要按类型取（表内专用键已建）
	if t, ok := dm.rels[docxStylesRelKey]; ok {
		return t, true
	}
	return "", false
}

// mediaTarget 返回 rId 的 media target（r:embed 查询）。
func (dm *docxModel) mediaTarget(rID string) string {
	return dm.rels[rID]
}

// mediaBytes 按 rels target 取图片字节（word/ 前缀拼接）。
func (dm *docxModel) mediaBytes(rID string) ([]byte, bool) {
	target := dm.rels[rID]
	if target == "" {
		return nil, false
	}
	data, ok := dm.files[joinDocPath(dm.docDir, target)]
	return data, ok
}

// joinDocPath 拼接 docDir 与 rel target（"/media/x.png" 与 "media/x.png" 均兼容）。
func joinDocPath(dir, target string) string {
	t := strings.TrimPrefix(target, "/")
	if dir == "" {
		return t
	}
	return dir + "/" + t
}

// parseBlocks 保序块遍历（对齐 python-docx iter_inner_content）。
//
// w:body 直接子元素中的 w:p/w:tbl 按文档序交替产出；嵌套在其他元素内
// 的段落/表格（如 textbox）不产出（python-docx 同款只遍历 body 直下块）。
func (dm *docxModel) parseBlocks() []docxBlock {
	var blocks []docxBlock
	dec := xml.NewDecoder(bytes.NewReader(dm.docBytes))
	// 手写深度跟踪（StartElement/EndElement 配对计数）
	depth := 0
	const bodyDepth = 2 // w:document(1) > w:body(2)
	for {
		tok, err := dec.Token()
		if err != io.EOF && err != nil {
			return blocks
		}
		if err == io.EOF {
			return blocks
		}
		switch te := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == bodyDepth+1 && te.Name.Local == "p" {
				blocks = append(blocks, dm.parseParagraphBlock(dec, te))
				// parseParagraphBlock 消费到 </w:p>，深度回到 bodyDepth
				depth = bodyDepth
			} else if depth == bodyDepth+1 && te.Name.Local == "tbl" {
				blocks = append(blocks, dm.parseTableBlock(dec, te))
				depth = bodyDepth
			}
		case xml.EndElement:
			depth--
		}
	}
}

// parseParagraphBlock 消费一个 w:p 元素到结束，产出段落块。
//
// 同遍历完成三件事：text 拼装（w:t/w:tab/w:br/hyperlink 子 runs——
// 对齐 python-docx paragraph.text：hyperlink 内 w:t 同样计入）、
// styleID 提取（w:pStyle）、paraXML 重建（EncodeToken 重建原始字节，
// 保证图片提取时能对 paraXML 再跑一次独立 xml.Decoder 找 a:blip）。
func (dm *docxModel) parseParagraphBlock(dec *xml.Decoder, start xml.StartElement) docxBlock {
	blk := docxBlock{kind: "para"}
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	_ = enc.EncodeToken(start)
	// 段落深度（1 = w:p 自身）
	depth := 1
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch te := tok.(type) {
		case xml.StartElement:
			depth++
			_ = enc.EncodeToken(te)
			switch te.Name.Local {
			case "pStyle":
				for _, a := range te.Attr {
					if a.Name.Local == "val" {
						blk.styleID = a.Value
					}
				}
			case "t":
				// 文本节点：读 CharData（w:t 是叶子，StartElement 后紧跟
				// CharData 再 EndElement；空 <w:t></w:t> 时 cd 断言失败不追加，
				// 由通用 EndElement 分支处理闭合）
				if txt, err := dec.Token(); err == nil {
					if cd, ok := txt.(xml.CharData); ok {
						blk.text += string(cd)
						_ = enc.EncodeToken(cd)
					}
				}
			case "tab":
				blk.text += "\t"
			case "br":
				blk.text += "\n"
			}
		case xml.EndElement:
			depth--
			_ = enc.EncodeToken(te)
			if depth == 0 {
				_ = enc.Flush()
				blk.paraXML = buf.Bytes()
				// 样式名解析（BabelFish UI 名）
				blk.styleUI = dm.styleUI(blk.styleID)
				return blk
			}
		default:
			// CharData 等非结构 token（w:t 分支已消费其 CharData）
			_ = enc.EncodeToken(tok)
		}
	}
	return blk
}

// styleUI styleId → w:name → BabelFish UI 名（空 styleID 返回空串）。
//
// 对齐 python-docx paragraph.style.name：styles.xml 的内部名经
// BabelFish.internal2ui 转换（"heading 1" → "Heading 1"）。
func (dm *docxModel) styleUI(styleID string) string {
	if styleID == "" {
		return ""
	}
	internal := dm.styleName(styleID)
	if internal == "" {
		return ""
	}
	if ui, ok := babelFishUI[internal]; ok {
		return ui
	}
	return internal
}

// parseTableBlock 消费一个 w:tbl 元素到结束，产出网格展开的表格块。
//
// 对齐 python-docx Table.rows → row.cells 语义：
//   - gridSpan：cell 横向重复 N 次（同内容）
//   - vMerge restart：本格是纵向合并 master 起点
//   - vMerge 续格（无 val 或 val="continue"）：内容视为空，回填所在列
//     上方最近 master 的已拼装文本（python-docx CT_Tc 跨行合并语义：
//     续格 cell 与 master 指向同一 tc 元素，读出即 master 文本）
func (dm *docxModel) parseTableBlock(dec *xml.Decoder, start xml.StartElement) docxBlock {
	_ = start // w:tbl 起始无属性消费
	blk := docxBlock{kind: "table"}
	// 行级状态
	var rows [][]string
	var curRow []string
	// 列位置跟踪（gridSpan 展开后）
	curCol := 0
	// 每列最近的 vMerge master 文本（展开后列号 → 文本）
	masterText := map[int]string{}

	depth := 1
	// cell 级状态
	var cellText strings.Builder
	var gridSpan int
	var vMergeVal string
	var hasVMerge bool
	inCell := false

	endCell := func() {
		if !inCell {
			return
		}
		text := cellText.String()
		span := gridSpan
		if span < 1 {
			span = 1
		}
		if hasVMerge && vMergeVal != "restart" {
			// 续格：回填本列最近 master 文本（无 master 时空串）
			text = masterText[curCol]
			span = 1 // 续格自身无 gridSpan 语义（Python row.cells 续格占 1 列）
		} else if hasVMerge {
			// master：span 覆盖的每列都记录（供下方续格按展开后列号回填——
			// python-docx 续格按网格列对齐，master 若带 gridSpan 其覆盖列都需登记）
			for i := 0; i < span; i++ {
				masterText[curCol+i] = text
			}
		}
		for i := 0; i < span; i++ {
			curRow = append(curRow, text)
		}
		curCol += span
		inCell = false
		cellText.Reset()
		gridSpan = 0
		vMergeVal = ""
		hasVMerge = false
	}

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch te := tok.(type) {
		case xml.StartElement:
			depth++
			switch te.Name.Local {
			case "tr":
				curRow = []string{}
				curCol = 0
			case "tc":
				endCell()
				inCell = true
				cellText.Reset()
				gridSpan = 0
				vMergeVal = ""
				hasVMerge = false
			case "gridSpan":
				for _, a := range te.Attr {
					if a.Name.Local == "val" {
						if n, err := strconv.Atoi(a.Value); err == nil {
							gridSpan = n
						}
					}
				}
			case "vMerge":
				hasVMerge = true
				for _, a := range te.Attr {
					if a.Name.Local == "val" {
						vMergeVal = a.Value
					}
				}
			case "t":
				if inCell {
					if txt, err := dec.Token(); err == nil {
						if cd, ok := txt.(xml.CharData); ok {
							cellText.Write(cd)
						}
					}
				}
			case "tab":
				if inCell {
					cellText.WriteString("\t")
				}
			case "br":
				if inCell {
					cellText.WriteString("\n")
				}
			}
		case xml.EndElement:
			depth--
			switch te.Name.Local {
			case "tc":
				endCell()
			case "tr":
				if curRow != nil {
					rows = append(rows, curRow)
					curRow = nil
				}
			case "tbl":
				if depth == 0 {
					blk.rows = rows
					return blk
				}
			}
		}
	}
	blk.rows = rows
	return blk
}
