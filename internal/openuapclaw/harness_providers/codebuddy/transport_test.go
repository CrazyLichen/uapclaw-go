package codebuddy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// TransportSuite 传输缝与线帧套件。
type TransportSuite struct{ suite.Suite }

// TestTransportSuite 套件入口。
func TestTransportSuite(t *testing.T) { suite.Run(t, new(TransportSuite)) }

// TestParseFrame_四类帧 实测帧样例可解码（偏差 D-15.22-c 记录源：CLI 2.181.0）。
func (s *TransportSuite) TestParseFrame_四类帧() {
	samples := []struct {
		line string
		kind string
		sub  string
	}{
		{`{"type":"system","subtype":"init","session_id":"s1","model":"m1"}`, "system", "init"},
		{`{"type":"system","subtype":"usage_update","session_id":"s1"}`, "system", "usage_update"},
		{`{"type":"assistant","session_id":"s1","message":{"id":"m1","content":[{"type":"text","text":"PONG"}]}}`, "assistant", ""},
		{`{"type":"result","subtype":"success","is_error":false,"result":"PONG","duration_ms":4069,"num_turns":2,"session_id":"s1"}`, "result", "success"},
	}
	for _, sample := range samples {
		frame, err := parseFrame(json.RawMessage(sample.line))
		s.Require().NoError(err, sample.line)
		s.Equal(sample.kind, frame.Type)
		s.Equal(sample.sub, frame.Subtype)
	}
}

// TestParseFrame_非法JSON 非法行报错不崩。
func (s *TransportSuite) TestParseFrame_非法JSON() {
	_, err := parseFrame(json.RawMessage(`{not json`))
	s.Require().Error(err)
}

// TestTransport_WriteRead_roundtrip fake 缝上双工收发一帧。
func (s *TransportSuite) TestTransport_WriteRead_roundtrip() {
	inR, inW := io.Pipe()
	_ = inR                 // 进程侧 stdin（inR = CLI 读到的）
	outR, outW := io.Pipe() // 进程侧 stdout（outW = CLI 写出的）
	tr := newPipeTransport(inW, bufio.NewReader(outR))
	go func() {
		line, err := bufio.NewReader(inR).ReadString('\n')
		s.Require().NoError(err)
		s.Require().Contains(line, "user")
		_, _ = io.WriteString(outW, `{"type":"result","subtype":"success","is_error":false}`+"\n")
	}()
	s.Require().NoError(tr.SendFrame(map[string]any{"type": "user"}))
	frame, err := tr.ReadFrame(context.Background())
	s.Require().NoError(err)
	s.Equal("result", frame.Type)
	s.Require().NoError(tr.Close())
}

// TestTransport_ReadFrame_超时 无帧到达按 inactivity 超时报错。
func (s *TransportSuite) TestTransport_ReadFrame_超时() {
	inR, inW := io.Pipe()
	_ = inR
	outR, _ := io.Pipe()
	tr := newPipeTransport(inW, bufio.NewReader(outR))
	tr.inactivity = 30 * time.Millisecond
	_, err := tr.ReadFrame(context.Background())
	s.Require().Error(err)
	s.Require().NoError(tr.Close())
}

// TestTransport_ReadFrame_ctx取消 ctx 取消立即返回。
func (s *TransportSuite) TestTransport_ReadFrame_ctx取消() {
	inR, inW := io.Pipe()
	_ = inR
	outR, _ := io.Pipe()
	tr := newPipeTransport(inW, bufio.NewReader(outR))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tr.ReadFrame(ctx)
	s.Require().Error(err)
	s.Require().NoError(tr.Close())
}

// TestTransport_Close幂等 二次 Close 不报错。
func (s *TransportSuite) TestTransport_Close幂等() {
	inR, inW := io.Pipe()
	_ = inR
	outR, _ := io.Pipe()
	tr := newPipeTransport(inW, bufio.NewReader(outR))
	s.Require().NoError(tr.Close())
	s.Require().NoError(tr.Close())
}
