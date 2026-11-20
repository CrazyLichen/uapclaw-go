package evaluator

import "testing"

// TestTransientTransportError 瞬态传输错误判定全分支（Python transport.py）。
func TestTransientTransportError(t *testing.T) {
	cases := []struct {
		err    string
		expect string
	}{
		{"Connection Reset by peer", "Connection Reset by peer"}, // connection reset 子串命中，原文返回
		{"requests.exceptions.ConnectionError: boom", "requests.exceptions.ConnectionError: boom"},
		{"Temporary failure in name resolution", "Temporary failure in name resolution"},
		{"HTTP 502 Bad Gateway", "HTTP 502 Bad Gateway"},
		{"rate limit exceeded", "rate limit exceeded"},
		{"operation timed out", "operation timed out"},
		{"service unavailable", "service unavailable"},
		{"peer closed connection", "peer closed connection"},
		{"RemoteDisconnected", "RemoteDisconnected"},
		{"some other failure", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := TransientTransportError(tc.err); got != tc.expect {
			t.Fatalf("TransientTransportError(%q)=%q 期望 %q", tc.err, got, tc.expect)
		}
	}
}
