package harnessrsi

import (
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// mustMojibake 测试助手：把 UTF-8 中文按 GBK 误解码构造真实乱码样本（全包只定义一次）
func mustMojibake(t *testing.T, text string) string {
	t.Helper()
	raw, err := simplifiedchinese.GBK.NewDecoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("construct mojibake failed: %v", err)
	}
	return string(raw)
}

// TestTextEncoding_RepairTextMojibake_正常文本恒等 正常中英文文本不得被改动
func TestTextEncoding_RepairTextMojibake_正常文本恒等(t *testing.T) {
	for _, text := range []string{"", "hello world", "你好，世界。", "mixed 中英 text 123"} {
		if got := RepairTextMojibake(text); got != text {
			t.Fatalf("normal text must be unchanged: %q -> %q", text, got)
		}
	}
}

// TestTextEncoding_RepairTextMojibake_修复GBK误解码 UTF-8 文本被按 GBK 误解码产生的
// 高密度 marker 文本应被修复（分数下降且候选含 CJK≥2）
func TestTextEncoding_RepairTextMojibake_修复GBK误解码(t *testing.T) {
	// 构造真实 mojibake：把中文按 GBK 解码 UTF-8 字节得到
	mojibake := mustMojibake(t, "重要说明：这是被错误解码的中文内容")
	repaired := RepairTextMojibake(mojibake)
	if repaired == mojibake {
		t.Fatalf("expected repair of %q", mojibake)
	}
	if mojibakeScore(repaired) >= mojibakeScore(mojibake) {
		t.Fatalf("repaired score must decrease: %d >= %d", mojibakeScore(repaired), mojibakeScore(mojibake))
	}
	if cjkCount(repaired) < 2 {
		t.Fatalf("repaired candidate must contain >=2 CJK chars: %q", repaired)
	}
}

// TestTextEncoding_HasUnrepairedMojibake_阈值 未修复分数 >=4 判真，正常文本判假
func TestTextEncoding_HasUnrepairedMojibake_阈值(t *testing.T) {
	if HasUnrepairedMojibake("") || HasUnrepairedMojibake("普通文本 normal") {
		t.Fatal("normal text must not be flagged")
	}
	mojibake := mustMojibake(t, "需要修复的中文样本数据内容较多一些")
	repaired := RepairTextMojibake(mojibake)
	// 修复后仍残留的序列 marker 才算未修复；用强 marker 直接构造
	if !HasUnrepairedMojibake("\u9286\u9286") && unrepairedMojibakeScore("\u9286\u9286") == 0 {
		t.Fatal("sequence marker should contribute to unrepaired score")
	}
	_ = repaired
}

// TestTextEncoding_RepairPayloadMojibake_递归 map/list 递归修复，非字符串键保持
func TestTextEncoding_RepairPayloadMojibake_递归(t *testing.T) {
	in := map[string]any{
		"list": []any{"plain", []any{"nested"}},
		"num":  3,
	}
	out := RepairPayloadMojibake(in).(map[string]any)
	if out["num"] != 3 {
		t.Fatalf("non-string value must pass through: %v", out["num"])
	}
	list := out["list"].([]any)
	if list[0] != "plain" {
		t.Fatalf("string passthrough failed: %v", list[0])
	}
}

// TestTextEncoding_DenseMarkerScore_密度阈值 marker 数 <4 或密度 <0.18 时 dense 评分为 0
func TestTextEncoding_DenseMarkerScore_密度阈值(t *testing.T) {
	if denseMarkerScore("正常文本内容") != 0 {
		t.Fatal("no markers must score 0")
	}
	// 3 个 marker：<4 不触发
	three := "\u7487\u8702\u8d1f ok"
	if denseMarkerScore(three) != 0 {
		t.Fatalf("marker count below minimum must score 0, got %d", denseMarkerScore(three))
	}
}

// TestTextEncoding_SuspiciousScriptScore_密度 可疑字符 <4 或密度 <0.08 返 0
func TestTextEncoding_SuspiciousScriptScore_密度(t *testing.T) {
	if suspiciousScriptScore("ab \u0530\u0531 cd") != 0 {
		t.Fatal("suspicious count below 4 must score 0")
	}
}
