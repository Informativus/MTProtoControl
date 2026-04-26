package serverops

import "testing"

func TestParseTelemtAPIHandlesUsersEnvelope(t *testing.T) {
	userCount, generatedLink := parseTelemtAPI(`{"users":[{"links":{"tls":["tg://proxy?server=mt.example.com&port=443&secret=abc"]}}]}`)
	if userCount != 1 {
		t.Fatalf("expected one user, got %d", userCount)
	}
	if generatedLink == "" {
		t.Fatal("expected generated link from users envelope")
	}
}

func TestParseTelemtAPIHandlesDataEnvelope(t *testing.T) {
	userCount, generatedLink := parseTelemtAPI(`{"ok":true,"data":[{"links":{"tls":["tg://proxy?server=mt.example.com&port=443&secret=abc"]}}]}`)
	if userCount != 1 {
		t.Fatalf("expected one user, got %d", userCount)
	}
	if generatedLink == "" {
		t.Fatal("expected generated link from data envelope")
	}
}
