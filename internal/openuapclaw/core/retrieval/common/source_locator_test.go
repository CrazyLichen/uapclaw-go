package common

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── SourceLocator 测试（30.3 Task1） ────────────────────────────

func TestSourceLocatorMarshalRoundtrip(t *testing.T) {
	loc := SourceLocator{Type: LocatorTypeText, PageNo: 3,
		LineBBoxes: []BBox{{72, 700, 200, 712}}, Quote: "Alpha Bravo"}
	b, err := json.Marshal([]SourceLocator{loc})
	if err != nil {
		t.Fatal(err)
	}
	var out []SourceLocator
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].PageNo != 3 || len(out[0].LineBBoxes) != 1 ||
		out[0].Quote != "Alpha Bravo" {
		t.Fatalf("roundtrip 不符: %+v", out)
	}
}

func TestTextChunkSourceLocatorsOmitEmpty(t *testing.T) {
	b, err := json.Marshal(TextChunk{ID: "c1", Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if bytesContains(b, []byte("source_locators")) {
		t.Fatalf("空 locators 不应序列化: %s", b)
	}
	ch := TextChunk{ID: "c2", Text: "y",
		SourceLocators: []SourceLocator{{Type: LocatorTypeParagraph, Paragraph: 2}}}
	b2, _ := json.Marshal(ch)
	if !bytesContains(b2, []byte("source_locators")) {
		t.Fatalf("非空 locators 应序列化: %s", b2)
	}
}

func TestLocatorTypeConstants(t *testing.T) {
	cases := map[string]string{
		LocatorTypeText:      "text",
		LocatorTypeImage:     "image",
		LocatorTypeParagraph: "paragraph",
		LocatorTypeCharRange: "char_range",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("常量 %q != %q", got, want)
		}
	}
	if MetaKeySourcePageMap != "source_page_map" ||
		MetaKeySourceParagraphMap != "source_paragraph_map" ||
		MetaKeySourceCharRange != "source_char_range" {
		t.Fatalf("sidecar 键常量不符")
	}
}

// bytesContains 局部助手（避免引入 bytes 全文件依赖差异）
func bytesContains(b, sub []byte) bool {
	return len(sub) == 0 || (len(b) >= len(sub) && stringContains(string(b), string(sub)))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
