package httpapi

import (
	"bytes"
	"golang.org/x/text/encoding/simplifiedchinese"
	"testing"
)

func TestGB18030AcrossPacketBoundary(t *testing.T) {
	raw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("中文终端"))
	if err != nil {
		t.Fatal(err)
	}
	c := newStreamCodec("GB18030")
	var got []byte
	for _, b := range raw {
		got = append(got, c.transcode([]byte{b}, false)...)
	}
	if !bytes.Equal(got, []byte("中文终端")) {
		t.Fatalf("decoded %q", got)
	}
	if !c.set("UTF-8") || c.set("BIG5") {
		t.Fatal("encoding validation failed")
	}
}
