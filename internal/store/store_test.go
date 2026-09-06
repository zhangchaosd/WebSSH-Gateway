package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestHostCredentialLifecycle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c, err := s.CreateCredential(Credential{Name: "lab", Type: "password", EncryptedSecret: "encrypted"})
	if err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateGroup(Group{Name: "Linux"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := s.CreateHost(Host{Name: "router", Hostname: "192.168.1.1", Port: 22, Username: "root", CredentialID: c.ID, GroupID: &g.ID, Encoding: "UTF-8"})
	if err != nil {
		t.Fatal(err)
	}
	if h.CredentialName != "lab" || h.GroupName != "Linux" {
		t.Fatalf("joined host fields missing: %+v", h)
	}
	list, err := s.ListHosts("route", "name")
	if err != nil || len(list) != 1 {
		t.Fatalf("search: %v %#v", err, list)
	}
	if err = s.DeleteCredential(c.ID); !errors.Is(err, ErrReferenced) {
		t.Fatalf("referenced credential delete: %v", err)
	}
	if err = s.PinHostKey(h.ID, "ssh-ed25519", "SHA256:test"); err != nil {
		t.Fatal(err)
	}
	h, _ = s.HostByID(h.ID)
	if h.HostKeyFingerprint != "SHA256:test" {
		t.Fatal("host key was not pinned")
	}
}
