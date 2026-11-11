package processcli

import (
	"strings"
	"testing"
)

// TestExecuteQuerySource_冲突参数先报错 冲突检查在解码之前，不构造 Runtime。
func TestExecuteQuerySource_冲突参数先报错(t *testing.T) {
	var out strings.Builder
	code := ExecuteQuerySource(QueryEntryOptions{
		Source:               "-",
		Stdin:                strings.NewReader(`{"schema_version":"0.1","type":"query","operation":"mode.list"}`),
		Stdout:               &out,
		ConflictingArguments: true,
	})
	if code != 2 {
		t.Errorf("退出码 = %d, 期望 2", code)
	}
	if !strings.Contains(out.String(), "INVALID_INPUT") {
		t.Errorf("应含 INVALID_INPUT：%q", out.String())
	}
}

// TestExecuteQuerySource_非法文档不构造Runtime。
func TestExecuteQuerySource_非法文档不构造Runtime(t *testing.T) {
	var out strings.Builder
	code := ExecuteQuerySource(QueryEntryOptions{
		Source: "-",
		Stdin:  strings.NewReader("not-json"),
		Stdout: &out,
		ClientFactory: func() *InProcessRuntimeClient {
			panic("非法输入不得构造 Runtime")
		},
	})
	if code != 2 {
		t.Errorf("退出码 = %d, 期望 2", code)
	}
	if !strings.Contains(out.String(), "INVALID_INPUT") {
		t.Errorf("应含 INVALID_INPUT：%q", out.String())
	}
}

// TestExecuteQuerySource_协议目录不起Runtime。
func TestExecuteQuerySource_协议目录不起Runtime(t *testing.T) {
	cases := []string{"protocol.capabilities", "protocol.schema"}
	for _, operation := range cases {
		var out strings.Builder
		code := ExecuteQuerySource(QueryEntryOptions{
			Source: "-",
			Stdin: strings.NewReader(
				`{"schema_version":"0.1","type":"query","request_id":"q1","operation":"` + operation + `"}`),
			Stdout: &out,
			ClientFactory: func() *InProcessRuntimeClient {
				panic("协议目录查询不得构造 Runtime")
			},
		})
		if code != 0 {
			t.Errorf("%s 退出码 = %d, 期望 0；输出 %s", operation, code, out.String())
		}
		if !strings.Contains(out.String(), `"type":"query_result"`) {
			t.Errorf("%s 输出缺少 query_result：%q", operation, out.String())
		}
		if !strings.Contains(out.String(), `"sequence":0`) {
			t.Errorf("%s 输出缺少 sequence：%q", operation, out.String())
		}
	}
}

// TestExecuteQuerySource_requestID回填。
func TestExecuteQuerySource_requestID回填(t *testing.T) {
	var out strings.Builder
	code := ExecuteQuerySource(QueryEntryOptions{
		Source: "-",
		Stdin: strings.NewReader(
			`{"schema_version":"0.1","type":"query","request_id":"custom","operation":"protocol.schema"}`),
		Stdout: &out,
	})
	if code != 0 {
		t.Errorf("退出码 = %d", code)
	}
	if !strings.Contains(out.String(), `"request_id":"custom"`) {
		t.Errorf("request_id 未回填：%q", out.String())
	}
}

// TestExecuteQuerySource_未知operation报错 schema 校验拒绝未知操作。
func TestExecuteQuerySource_未知operation报错(t *testing.T) {
	var out strings.Builder
	code := ExecuteQuerySource(QueryEntryOptions{
		Source:               "-",
		Stdin:                strings.NewReader(`{"schema_version":"0.1","type":"query","operation":"session.delete"}`),
		Stdout:               &out,
		ConflictingArguments: false,
		ClientFactory: func() *InProcessRuntimeClient {
			panic("非法 operation 不得构造 Runtime")
		},
	})
	if code != 2 {
		t.Errorf("退出码 = %d, 期望 2；输出 %s", code, out.String())
	}
	if !strings.Contains(out.String(), "INVALID_INPUT") {
		t.Errorf("应含 INVALID_INPUT：%q", out.String())
	}
}
