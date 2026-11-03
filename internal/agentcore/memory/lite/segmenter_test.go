package lite

import (
	"errors"
	"sync"
	"testing"
)

// TestSegmentForFTS_过滤规则 用真实词典验证：空格/标点被过滤、英文小写化、停用词被过滤。
//
// 实测锚点（gse v1.3.1 + LoadDictEmbed("zh_s")）：
// "VDS15800 组网" → gse 原始输出 ["vds15800", " ", "组网"]，过滤后 ["vds15800", "组网"]
func TestSegmentForFTS_过滤规则(t *testing.T) {
	seg, err := ensureSegmenter()
	if err != nil || seg == nil {
		t.Skipf("分词器不可用，跳过: %v", err)
	}

	got := segmentForFTS("VDS15800 组网")
	if len(got) != 2 || got[0] != "vds15800" || got[1] != "组网" {
		t.Errorf("segmentForFTS = %v, want [vds15800 组网]", got)
	}

	tokens := segmentForFTS("这个设备的组网方式")
	for _, tok := range tokens {
		if tok == "的" || tok == "，" || tok == " " {
			t.Errorf("停用词/标点未过滤: %v", tokens)
		}
	}
	if len(tokens) == 0 {
		t.Error("正常中文句子不应切出空结果")
	}
}

// TestSegmentForFTS_不可用返回nil 分词器加载失败时必须返回 nil（调用方据此降级）
func TestSegmentForFTS_不可用返回nil(t *testing.T) {
	prevFn, prevOnce, prevInst := segmenterLoadFn, segOnce, segInst
	defer func() { segmenterLoadFn, segOnce, segInst = prevFn, prevOnce, prevInst }()

	segmenterLoadFn = func() (segmenterHandle, error) { return nil, errors.New("dict load failed") }
	segOnce = sync.Once{}
	segInst = nil

	if got := segmentForFTS("组网"); got != nil {
		t.Errorf("降级时应返回 nil，实际 %v", got)
	}
	if id := segmenterID(); id != "none" {
		t.Errorf("降级时 segmenterID = %q, want none", id)
	}
}

// TestJoinSegments 验证写进 FTS 的文本形态
func TestJoinSegments(t *testing.T) {
	if got := joinSegments([]string{"a", "b"}); got != "a b" {
		t.Errorf("joinSegments = %q, want %q", got, "a b")
	}
	if got := joinSegments(nil); got != "" {
		t.Errorf("joinSegments(nil) = %q, want 空串", got)
	}
}
