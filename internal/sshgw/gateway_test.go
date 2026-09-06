package sshgw

import "testing"

func TestTargetDenylist(t *testing.T) {
	for _, h := range []string{"169.254.169.254", "metadata.google.internal", "100.100.100.200"} {
		if validateAddress(h, 22, "0.0.0.0:8080") == nil {
			t.Fatalf("metadata target %s was allowed", h)
		}
	}
	if validateAddress("192.168.1.5", 22, "0.0.0.0:8080") != nil {
		t.Fatal("LAN SSH target was rejected")
	}
	if validateAddress("127.0.0.1", 8080, "0.0.0.0:8080") == nil {
		t.Fatal("management listener loop was allowed")
	}
}
