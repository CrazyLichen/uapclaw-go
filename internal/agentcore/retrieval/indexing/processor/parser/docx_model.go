package parser

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
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
