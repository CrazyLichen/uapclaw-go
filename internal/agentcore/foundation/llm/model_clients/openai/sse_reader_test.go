package openai

import (
	"io"
	"strings"
	"testing"
)

func TestSSEReader_NormalStream(t *testing.T) {
	input := "data: {\"id\":\"1\"}\n\ndata: {\"id\":\"2\"}\n\ndata: [DONE]\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	data1, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() 1st failed: %v", err)
	}
	if data1 != `{"id":"1"}` {
		t.Errorf("ReadEvent() 1st = %q, want %q", data1, `{"id":"1"}`)
	}

	data2, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() 2nd failed: %v", err)
	}
	if data2 != `{"id":"2"}` {
		t.Errorf("ReadEvent() 2nd = %q, want %q", data2, `{"id":"2"}`)
	}

	_, err = reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("ReadEvent() 3rd err = %v, want io.EOF", err)
	}
}

func TestSSEReader_CommentsAndEmptyLines(t *testing.T) {
	input := ": comment\n\ndata: {\"content\":\"hello\"}\n\n\n\ndata: [DONE]\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() failed: %v", err)
	}
	if data != `{"content":"hello"}` {
		t.Errorf("ReadEvent() = %q, want %q", data, `{"content":"hello"}`)
	}

	_, err = reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("ReadEvent() err = %v, want io.EOF", err)
	}
}

func TestSSEReader_NonDataLines(t *testing.T) {
	input := "event: chat\ndata: {\"content\":\"hi\"}\nid: 123\ndata: [DONE]\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() failed: %v", err)
	}
	if data != `{"content":"hi"}` {
		t.Errorf("ReadEvent() = %q, want %q", data, `{"content":"hi"}`)
	}
}

func TestSSEReader_StreamEndWithoutDone(t *testing.T) {
	input := "data: {\"content\":\"hi\"}\n\n"
	reader := NewSSEReader(strings.NewReader(input))

	data, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent() failed: %v", err)
	}
	if data != `{"content":"hi"}` {
		t.Errorf("ReadEvent() = %q, want %q", data, `{"content":"hi"}`)
	}

	_, err = reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("ReadEvent() err = %v, want io.EOF", err)
	}
}

func TestSSEReader_EmptyStream(t *testing.T) {
	reader := NewSSEReader(strings.NewReader(""))
	_, err := reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("ReadEvent() err = %v, want io.EOF", err)
	}
}
