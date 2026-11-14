package common

import "testing"

// 错误码常量也在 model_errors_test.go 单独覆盖一份文件级锚点（每文件需有测试的口径下冗余无妨）。
func TestModelErrors_toPayload只含code与message(t *testing.T) {
	e := NewModelSelectionError(ModelGroupInvalid, "no model is configured", nil)
	payload := e.ToPayload()
	if len(payload) != 2 {
		t.Errorf("nil details 时 payload 应只有 code/message 两键: %v", payload)
	}
}
