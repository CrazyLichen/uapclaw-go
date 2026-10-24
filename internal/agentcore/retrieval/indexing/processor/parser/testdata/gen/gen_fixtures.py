#!/usr/bin/env python3
# coding: utf-8
"""13.13 fixture 生成器：造 .docx/.xlsx/.csv/.tsv 样本 + 跑真实 Python 解析产出黄金基准 JSON。

运行方式：cd internal/agentcore/retrieval/indexing/processor/parser/testdata/gen && python3 gen_fixtures.py
产物写入 ../ (testdata/)。
"""
import io
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.dirname(HERE)  # testdata/

# ─────────────── docx fixture ───────────────

def png1x1():
    """构造 1×1 RGBA 绿色 PNG 字节。"""
    import struct
    import zlib

    sig = b"\x89PNG\r\n\x1a\n"

    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)

    ihdr = struct.pack(">IIBBBBB", 1, 1, 8, 6, 0, 0, 0)
    idat = zlib.compress(b"\x00\xff\x00\x00\xff\x00")  # RGBA 绿
    return sig + chunk(b"IHDR", ihdr) + chunk(b"IDAT", idat) + chunk(b"IEND", b"")


def gen_docx():
    """python-docx 构造带 4 保真点结构的 docx。"""
    from docx import Document
    from docx.oxml.ns import qn
    from docx.shared import Inches

    doc = Document()
    # 已有 Title/Heading 1..9 样式（默认模板自带，BabelFish 实测路径）
    doc.add_paragraph("Doc Title", style="Title")
    doc.add_paragraph("Chapter One", style="Heading 1")
    doc.add_paragraph("Chapter One One", style="Heading 2")
    # tab/br 段落（python-docx 无直接 API，用 oxml 注入）
    p = doc.add_paragraph()
    r = p.add_run("before")
    r.add_tab()
    p.add_run("after_tab")
    br_run = p.add_run()
    br_run._element.append(br_run._element.makeelement(qn("w:br"), {}))
    p.add_run("after_br")
    # hyperlink 段落（手工 oxml：外链 rId 语义走 python-docx part.rels）
    from docx.opc.constants import RELATIONSHIP_TYPE as RT
    from docx.oxml import OxmlElement

    part = doc.part
    r_id = part.relate_to("https://example.com", RT.HYPERLINK, is_external=True)
    hp = OxmlElement("w:hyperlink")
    hp.set(qn("r:id"), r_id)
    hr = OxmlElement("w:r")
    ht = OxmlElement("w:t")
    ht.text = "link text"
    hr.append(ht)
    hp.append(hr)
    para = doc.add_paragraph()
    para._element.append(hp)
    # 普通段落
    doc.add_paragraph("plain paragraph")
    # 带图段落（1×1 PNG，RGBA）
    pic_para = doc.add_paragraph()
    run = pic_para.add_run()
    run.add_picture(io.BytesIO(png1x1()), width=Inches(1))
    # 表格：2×2，r0c0 vMerge restart + r1c0 vMerge 续格（纵向合并）
    tbl = doc.add_table(rows=2, cols=2)
    tbl.cell(0, 0).text = "master"
    tbl.cell(0, 1).text = "r0c1"
    a = tbl.cell(0, 0)
    b = tbl.cell(1, 0)
    a.merge(b)
    tbl.cell(1, 1).text = "r1c1"
    # gridSpan 横向合并表（独立 2 行表）
    tbl2 = doc.add_table(rows=2, cols=3)
    tbl2.cell(0, 0).text = "h0"
    tbl2.cell(0, 1).text = "h1"
    tbl2.cell(0, 2).text = "h2"
    merged = tbl2.cell(1, 0).merge(tbl2.cell(1, 1))
    merged.text = "span2"
    tbl2.cell(1, 2).text = "c2"
    path = os.path.join(OUT, "fixture_word.docx")
    doc.save(path)
    return path


def gen_docx_golden(path):
    """跑 word_parser.py 逻辑产出黄金基准（复刻 _parse 不含 captioner——captioner 禁用态）。"""
    from docx import Document
    from docx.table import Table
    from docx.text.paragraph import Paragraph

    def table_to_markdown(table):
        rows = [[cell.text.strip().replace("|", "\\|") for cell in row.cells] for row in table.rows]
        if not rows:
            return ""
        lines = ["| " + " | ".join(rows[0]) + " |"]
        lines.append("| " + " | ".join("---" for _ in rows[0]) + " |")
        for row in rows[1:]:
            lines.append("| " + " | ".join(row) + " |")
        lines.append("")
        return "\n".join(lines)

    def paragraph_to_markdown(paragraph):
        text = paragraph.text.strip()
        if not text:
            return ""
        style_name = None
        try:
            if paragraph.style is not None:
                style_name = getattr(paragraph.style, "name", None)
        except Exception:
            style_name = None
        if style_name is None:
            return text
        style_name = str(style_name).strip()
        if style_name == "Title":
            return "# " + text
        if style_name.startswith("Heading "):
            try:
                level = int(style_name.split()[1])
                if 1 <= level <= 9:
                    return "#" * (level + 1) + " " + text
            except (IndexError, ValueError):
                pass
        return text

    doc = Document(path)
    content = []
    for block in doc.iter_inner_content():
        if isinstance(block, Paragraph):
            md = paragraph_to_markdown(block)
            if md:
                content.append(md)
        elif isinstance(block, Table):
            md = table_to_markdown(block)
            if md:
                content.append(md)
    result = "\n".join([line for line in content if line.strip()])
    return {"result": result if result else None}

# ─────────────── xlsx/csv/tsv fixture ───────────────

def gen_excel():
    from openpyxl import Workbook

    wb = Workbook()
    ws = wb.active
    ws.title = "Data"
    ws.append(["Name", "", "Age"])        # 空表头列（列 2 回落 Column 2）
    ws.append(["alice", "x", 30])          # int 30
    ws.append(["bob", "", None])           # None cell
    # 注意 openpyxl 写侧 safe_string 用 %.16g：1e15 存为整数字面量 "1000000000000000"，
    # 读侧 _cast_number 无 ./e/E → int（str() 输出无 ".0"）。大 float 的 int 化是
    # xlsx 存储格式固有行为，golden 如实记录（pyFloatStr 的 float 锚点由纯函数单测覆盖）。
    ws.append([3.5, 1e15, True])           # float / 大数（int 化）/ bool
    ws.append(["", "", ""])                # 全空行（表头非空仍产出行文档）
    # 第二个 sheet
    ws2 = wb.create_sheet("Calc")
    ws2.append(["f1", "f2"])
    ws2.append([0.1 + 0.2, "tail"])
    path = os.path.join(OUT, "fixture_excel.xlsx")
    wb.save(path)
    # csv（带 BOM，对齐 utf-8-sig 路径）
    import codecs

    csv_path = os.path.join(OUT, "fixture_csv.csv")
    with codecs.open(csv_path, "w", "utf-8-sig") as f:
        f.write("h1,h2\nv1,v2\n,v\n")  # 空表头列 + 空值格
    # tsv
    tsv_path = os.path.join(OUT, "fixture_tsv.tsv")
    with open(tsv_path, "w", encoding="utf-8") as f:
        f.write("a\tb\n1\t2\n")
    return path, csv_path, tsv_path


def gen_excel_golden():
    """stub 依赖后 importlib 加载真实 excel_parser.py，跑 rowsToDocuments 黄金基准。"""
    import importlib.util
    import types

    sys.path.insert(0, "/home/opensource/agent-core")

    docmod = types.ModuleType("docmod")

    class Document:
        def __init__(self, id_, text, metadata):
            self.id = id_
            self.text = text
            self.metadata = metadata

    docmod.Document = Document
    afp = types.ModuleType("afp")
    afp.register_parser = lambda exts: lambda cls: cls
    base = types.ModuleType("base")

    class Parser:
        def __init__(self, **kw):
            pass

    base.Parser = Parser

    for name, mod in {
        "openjiuwen": types.ModuleType("x"),
        "openjiuwen.core": types.ModuleType("x"),
        "openjiuwen.core.common": types.ModuleType("x"),
        "openjiuwen.core.common.logging": types.ModuleType("x"),
        "openjiuwen.core.retrieval": types.ModuleType("x"),
        "openjiuwen.core.retrieval.common": types.ModuleType("x"),
        "openjiuwen.core.retrieval.common.document": docmod,
        "openjiuwen.core.retrieval.indexing": types.ModuleType("x"),
        "openjiuwen.core.retrieval.indexing.processor": types.ModuleType("x"),
        "openjiuwen.core.retrieval.indexing.processor.parser": types.ModuleType("x"),
        "openjiuwen.core.retrieval.indexing.processor.parser.auto_file_parser": afp,
        "openjiuwen.core.retrieval.indexing.processor.parser.base": base,
    }.items():
        sys.modules[name] = mod
    sys.modules["openjiuwen.core.common.logging"].logger = print

    spec = importlib.util.spec_from_file_location(
        "excel_parser",
        "/home/opensource/agent-core/openjiuwen/core/retrieval/indexing/processor/parser/excel_parser.py",
    )
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)

    from openpyxl import load_workbook

    wb = load_workbook(os.path.join(OUT, "fixture_excel.xlsx"), read_only=True, data_only=True)
    docs = []
    try:
        for si, sheet in enumerate(wb.worksheets):
            rows = list(sheet.iter_rows(values_only=True))
            docs.extend(mod._rows_to_documents(rows, sheet.title, "base", si, include_header=True))
    finally:
        wb.close()

    import csv as pycsv

    with open(os.path.join(OUT, "fixture_csv.csv"), encoding="utf-8-sig", newline="") as f:
        csv_rows = list(pycsv.reader(f))
    csv_docs = mod._rows_to_documents(csv_rows, "fixture_csv.csv", "base", 0, include_header=True)

    with open(os.path.join(OUT, "fixture_tsv.tsv"), encoding="utf-8-sig", newline="") as f:
        tsv_rows = list(pycsv.reader(f, delimiter="\t"))
    tsv_docs = mod._rows_to_documents(tsv_rows, "fixture_tsv.tsv", "base", 0, include_header=True)

    def ser(docs):
        return [{"id": d.id, "text": d.text, "metadata": d.metadata} for d in docs]

    return {"xlsx": ser(docs), "csv": ser(csv_docs), "tsv": ser(tsv_docs)}


def main():
    word_path = gen_docx()
    word_golden = gen_docx_golden(word_path)
    with open(os.path.join(OUT, "fixture_word.golden.json"), "w", encoding="utf-8") as f:
        json.dump(word_golden, f, ensure_ascii=False, indent=2)
    gen_excel()
    excel_golden = gen_excel_golden()
    with open(os.path.join(OUT, "fixture_excel.golden.json"), "w", encoding="utf-8") as f:
        json.dump(excel_golden, f, ensure_ascii=False, indent=2)
    print("fixtures + golden written to", OUT)


if __name__ == "__main__":
    main()
