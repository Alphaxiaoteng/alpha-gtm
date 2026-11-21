package auth

import "testing"

func TestHashAndToken(t *testing.T) {
	h, err := HashPassword("demo123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("demo123", h) {
		t.Fatal("check failed")
	}
	if CheckPassword("wrong", h) {
		t.Fatal("wrong password should fail")
	}
	tok, exp, err := IssueToken("secret-key-for-test", "demo", "admin", 0)
	if err != nil || tok == "" || exp.IsZero() {
		t.Fatalf("issue: %v", err)
	}
	u, role, err := ParseToken("secret-key-for-test", tok)
	if err != nil || u != "demo" || role != "admin" {
		t.Fatalf("parse: %v %s %s", err, u, role)
	}
	if _, _, err := ParseToken("other", tok); err == nil {
		t.Fatal("expected sig fail")
	}
}
