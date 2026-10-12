package redact

import "testing"

func TestClean(t *testing.T) {
	const token = "ghp_SUPERSECRETTOKEN"
	in := "GET https://user:" + token + "@github.com/x?access_token=" + token + " Authorization: Bearer " + token + " raw " + token
	out := Clean(in, token)
	if stringsContains(out, token) || stringsContains(out, "SUPERSECRET") {
		t.Fatalf("secret leaked: %s", out)
	}
	if !stringsContains(out, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", out)
	}

	plain := "authorization: token ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	got := Clean(plain, "")
	if stringsContains(got, "ghp_") {
		t.Fatalf("prefix token leaked: %s", got)
	}
}

func stringsContains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub)))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
