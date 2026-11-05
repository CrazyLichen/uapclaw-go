package skills

import (
	"testing"
)

// TestMarkdownHasImageReference 图片引用检测
func TestMarkdownHasImageReference(t *testing.T) {
	cases := map[string]bool{
		"![shot](./assets/a.PNG)":              true,
		`<img src="b.webp">`:                   true,
		"see reference.png for details":        true,
		"![doc](./guide.md)":                   false,
		"[link](https://x.com/a.mp4)":          false,
		"plain text without media":             false,
		"![remote](https://x.com/a.jpg?raw=1)": true,
	}
	for input, want := range cases {
		if got := markdownHasImageReference(input); got != want {
			t.Errorf("markdownHasImageReference(%q) = %v, want %v", input, got, want)
		}
	}
}

// TestMarkdownHasVideoReference 视频引用检测
func TestMarkdownHasVideoReference(t *testing.T) {
	if !markdownHasVideoReference("[demo](./assets/demo.mp4)") {
		t.Error("期望识别 markdown 链接视频")
	}
	if !markdownHasVideoReference(`<video src="x.mov"></video>`) {
		t.Error("期望识别 HTML video")
	}
	if markdownHasVideoReference("![shot](a.png)") {
		t.Error("图片不应被判为视频")
	}
}
