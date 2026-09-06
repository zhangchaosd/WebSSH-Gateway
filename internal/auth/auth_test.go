package auth

import (
	"net/http/httptest"
	"testing"
	"time"
	"webssh/internal/store"
)

func TestPasswordAndSessionLifecycle(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse battery staple") || CheckPassword(hash, "wrong") {
		t.Fatal("password verification mismatch")
	}
	m := NewManager(20*time.Millisecond, false)
	w := httptest.NewRecorder()
	s := m.Login(w, store.User{ID: 7, Username: "admin"})
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(w.Result().Cookies()[0])
	if got, ok := m.Get(r); !ok || got.CSRF != s.CSRF {
		t.Fatal("session was not available")
	}
	time.Sleep(25 * time.Millisecond)
	if _, ok := m.Get(r); ok {
		t.Fatal("expired session was accepted")
	}
}
