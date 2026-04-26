package telemtconfig

import (
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
)

func TestGenerateProducesValidTelemtConfig(t *testing.T) {
	configText, fields, err := Generate(Fields{
		PublicHost: "mt.example.com",
	})
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	parsed, validationErr := Parse(configText)
	if validationErr != nil {
		t.Fatalf("parse generated config: %#v", validationErr.Fields)
	}

	if len(fields.Secret) != 32 {
		t.Fatalf("expected 32-char secret, got %q", fields.Secret)
	}
	if _, err := hex.DecodeString(fields.Secret); err != nil {
		t.Fatalf("expected hex secret, got %q: %v", fields.Secret, err)
	}
	if parsed.Secret != fields.Secret {
		t.Fatalf("expected parsed secret %q, got %q", fields.Secret, parsed.Secret)
	}

	link, err := url.Parse(PreviewLink(fields))
	if err != nil {
		t.Fatalf("parse preview link: %v", err)
	}
	if got := link.Query().Get("secret"); got != "ee"+fields.Secret+hex.EncodeToString([]byte(fields.TLSDomain)) {
		t.Fatalf("unexpected preview secret payload %q", got)
	}
	if got := link.Query().Get("server"); got != fields.PublicHost {
		t.Fatalf("expected preview host %q, got %q", fields.PublicHost, got)
	}
}

func TestParseRejectsMissingSecret(t *testing.T) {
	_, validationErr := Parse(strings.TrimSpace(`
[general]
use_middle_proxy = true
log_level = "normal"

[general.modes]
classic = false
secure = false
tls = true

[general.links]
show = "*"
public_host = "mt.example.com"
public_port = 443

[server]
port = 443

[server.api]
enabled = true
listen = "127.0.0.1:9091"
whitelist = ["127.0.0.1/32"]

[censorship]
tls_domain = "mt.example.com"
mask = true
mask_host = "www.yandex.ru"
mask_port = 443
tls_emulation = false
tls_front_dir = "tlsfront"
`))
	if validationErr == nil {
		t.Fatal("expected validation error")
	}
	if validationErr.Fields["secret"] != "must be 32 hex chars" {
		t.Fatalf("expected secret validation error, got %#v", validationErr.Fields)
	}
}

func TestParsePartialPrefersDefaultUserSecret(t *testing.T) {
	fields, err := ParsePartial(strings.TrimSpace(`
[access.users]
alpha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
default = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
`))
	if err != nil {
		t.Fatalf("parse partial config: %v", err)
	}
	if fields.Secret == nil || *fields.Secret != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("expected default user secret, got %#v", fields.Secret)
	}
}
