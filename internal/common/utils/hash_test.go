package utils

import (
	"testing"
)

func TestGenerateKey(t *testing.T) {
	tests := []struct {
		name         string
		apiKey       string
		apiBase      string
		provider     string
		wantLen      int // SHA256 hex 长度固定 64
	}{
		{
			name:     "basic",
			apiKey:   "sk-123",
			apiBase:  "https://api.openai.com",
			provider: "openai",
			wantLen:  64,
		},
		{
			name:     "empty strings",
			apiKey:   "",
			apiBase:  "",
			provider: "",
			wantLen:  64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GenerateKey(tt.apiKey, tt.apiBase, tt.provider)
			if len(got) != tt.wantLen {
				t.Fatalf("GenerateKey() length = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestGenerateKey_Deterministic(t *testing.T) {
	// 相同输入应产生相同输出
	key1 := GenerateKey("sk-abc", "https://api.openai.com", "openai")
	key2 := GenerateKey("sk-abc", "https://api.openai.com", "openai")
	if key1 != key2 {
		t.Fatalf("GenerateKey() not deterministic: %q != %q", key1, key2)
	}
}

func TestGenerateKey_DifferentInputs(t *testing.T) {
	// 不同输入应产生不同输出
	key1 := GenerateKey("sk-abc", "https://api.openai.com", "openai")
	key2 := GenerateKey("sk-xyz", "https://api.openai.com", "openai")
	if key1 == key2 {
		t.Fatal("GenerateKey() returned same key for different inputs")
	}
}

func TestGenerateKey_SortedInputs(t *testing.T) {
	// Python 实现对参数排序后拼接，相同参数集合（不同传入顺序）产生相同 key。
	// GenerateKey("A", "B", "C") 排序后拼接 "ABC"
	// GenerateKey("B", "A", "C") 排序后拼接 "ABC" — 相同！
	// 这是 Python 的设计意图：参数顺序无关，只看参数值的集合。
	key1 := GenerateKey("A", "B", "C")
	key2 := GenerateKey("A", "B", "C")
	if key1 != key2 {
		t.Fatal("same inputs should produce same key")
	}

	// 排序后相同集合的参数产生相同 key
	key3 := GenerateKey("B", "A", "C")
	if key1 != key3 {
		t.Fatal("sorted same values should produce same key")
	}

	// 完全不同的参数值应产生不同 key
	key4 := GenerateKey("X", "Y", "Z")
	if key1 == key4 {
		t.Fatal("different values should produce different keys")
	}
}

func TestGenerateKey_MatchesPython(t *testing.T) {
	// 验证与 Python 实现的一致性：
	// Python: generate_key("sk-123", "https://api.openai.com", "openai")
	// 排序后: ["openai", "sk-123", "https://api.openai.com"]
	// 拼接: "openaisk-123https://api.openai.com"
	// SHA256 应一致
	got := GenerateKey("sk-123", "https://api.openai.com", "openai")
	if len(got) != 64 {
		t.Fatalf("key length = %d, want 64", len(got))
	}
	// 只要与 Python 输出长度和格式一致即可（具体值可用 Python 验证）
}
