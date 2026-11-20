package prompts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// canonicalJSON 输出键排序、无空白、不转义 HTML 的 JSON。
// 关键：Go 的 json.Marshal 会把 < > & 转成 \u003c 等，
// 与上游 json.dumps(ensure_ascii=False, sort_keys=True, separators=(",", ":")) 不一致，
// 会导致同一内容算出不同哈希（每轮多发 delta）。
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// HashPromptAttachment 计算模型可见内容的稳定哈希。
// Python: hash_prompt_attachment（只覆盖 section/kind/content/content_kind，
// 时间戳、priority、source、metadata 均不参与）。
func HashPromptAttachment(a *Attachment) string {
	if a == nil {
		return ""
	}
	payload := map[string]any{
		"section":      a.Section,
		"kind":         string(a.Kind),
		"content":      a.Content,
		"content_kind": a.ContentKind,
	}
	raw, err := canonicalJSON(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// sha256Hex 计算原始内容哈希（写入 ContentSHA256 用）。
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// stateBySection 生成 section → hash 交付账本。
func stateBySection(items []*Attachment) map[string]string {
	out := make(map[string]string, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out[item.Section] = HashPromptAttachment(item)
	}
	return out
}
