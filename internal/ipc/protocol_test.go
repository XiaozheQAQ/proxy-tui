package ipc

import (
	"bytes"
	"testing"
)

func TestFramingRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	request := Request{Version: Version, RequestID: "abc", Operation: OpPing}
	if err := WriteMessage(&buffer, request); err != nil {
		t.Fatalf("write: %v", err)
	}
	var decoded Request
	if err := ReadMessage(&buffer, &decoded); err != nil {
		t.Fatalf("read: %v", err)
	}
	if decoded.RequestID != "abc" || decoded.Operation != OpPing {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestFramingRejectsOversize(t *testing.T) {
	data := make([]byte, MaxMessageSize+1)
	buffer := bytes.NewBuffer(append([]byte{0, 0x10, 0, 1}, data...))
	var request Request
	if err := ReadMessage(buffer, &request); err == nil {
		t.Fatal("oversize message must be rejected")
	}
}
