package telemtconfig

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"mtproxy-control/apps/api/internal/inventory"
)

const (
	DefaultUserName = "default"
	DefaultMaskHost = "www.yandex.ru"
	DefaultMaskPort = 443
	DefaultAPIPort  = 9091
	DefaultLogLevel = "normal"
)

var allowedLogLevels = map[string]struct{}{
	"debug":   {},
	"verbose": {},
	"normal":  {},
	"silent":  {},
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "validation failed"
}

func (e *ValidationError) add(field, message string) {
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	if _, exists := e.Fields[field]; !exists {
		e.Fields[field] = message
	}
}

func (e *ValidationError) empty() bool {
	return len(e.Fields) == 0
}

type Fields struct {
	PublicHost     string `json:"public_host"`
	PublicPort     int    `json:"public_port"`
	TLSDomain      string `json:"tls_domain"`
	Secret         string `json:"secret"`
	MaskHost       string `json:"mask_host"`
	MaskPort       int    `json:"mask_port"`
	APIPort        int    `json:"api_port"`
	UseMiddleProxy bool   `json:"use_middle_proxy"`
	LogLevel       string `json:"log_level"`
}

type PartialFields struct {
	PublicHost     *string
	PublicPort     *int
	TLSDomain      *string
	Secret         *string
	MaskHost       *string
	MaskPort       *int
	APIPort        *int
	UseMiddleProxy *bool
	LogLevel       *string
}

type document struct {
	General generalSection `toml:"general"`
	Server  serverSection  `toml:"server"`
	Access  accessSection  `toml:"access"`

	Censorship censorshipSection `toml:"censorship"`
}

type generalSection struct {
	UseMiddleProxy bool         `toml:"use_middle_proxy"`
	LogLevel       string       `toml:"log_level"`
	Modes          modesSection `toml:"modes"`
	Links          linksSection `toml:"links"`
}

type modesSection struct {
	Classic bool `toml:"classic"`
	Secure  bool `toml:"secure"`
	TLS     bool `toml:"tls"`
}

type linksSection struct {
	Show       any    `toml:"show"`
	PublicHost string `toml:"public_host"`
	PublicPort int    `toml:"public_port"`
}

type serverSection struct {
	Port      int              `toml:"port"`
	API       serverAPI        `toml:"api"`
	Listeners []serverListener `toml:"listeners"`
}

type serverAPI struct {
	Enabled                 bool     `toml:"enabled"`
	Listen                  string   `toml:"listen"`
	Whitelist               []string `toml:"whitelist"`
	MinimalRuntimeEnabled   bool     `toml:"minimal_runtime_enabled"`
	MinimalRuntimeCacheTTLM int      `toml:"minimal_runtime_cache_ttl_ms"`
}

type serverListener struct {
	IP string `toml:"ip"`
}

type accessSection struct {
	Users map[string]string `toml:"users"`
}

type censorshipSection struct {
	TLSDomain    string `toml:"tls_domain"`
	Mask         bool   `toml:"mask"`
	MaskHost     string `toml:"mask_host"`
	MaskPort     int    `toml:"mask_port"`
	TLSEmulation bool   `toml:"tls_emulation"`
	TLSFrontDir  string `toml:"tls_front_dir"`
}

func DefaultFields(server inventory.Server) Fields {
	publicHost := strings.TrimSpace(server.Host)
	if server.PublicHost != nil {
		if value := strings.TrimSpace(*server.PublicHost); value != "" {
			publicHost = value
		}
	}

	publicPort := server.MTProtoPort
	if publicPort == 0 {
		publicPort = 443
	}

	tlsDomain := publicHost
	if server.SNIDomain != nil {
		if value := strings.TrimSpace(*server.SNIDomain); value != "" {
			tlsDomain = value
		}
	}

	return Fields{
		PublicHost:     publicHost,
		PublicPort:     publicPort,
		TLSDomain:      tlsDomain,
		Secret:         mustSecret(),
		MaskHost:       DefaultMaskHost,
		MaskPort:       DefaultMaskPort,
		APIPort:        DefaultAPIPort,
		UseMiddleProxy: true,
		LogLevel:       DefaultLogLevel,
	}
}

func ApplyDefaults(base Fields, overrides PartialFields) (Fields, *ValidationError) {
	next := normalizeDraft(base)

	if overrides.PublicHost != nil {
		next.PublicHost = strings.TrimSpace(*overrides.PublicHost)
	}
	if overrides.PublicPort != nil {
		next.PublicPort = *overrides.PublicPort
	}
	if overrides.TLSDomain != nil {
		next.TLSDomain = strings.TrimSpace(*overrides.TLSDomain)
	}
	if overrides.Secret != nil {
		next.Secret = strings.TrimSpace(*overrides.Secret)
	}
	if overrides.MaskHost != nil {
		next.MaskHost = strings.TrimSpace(*overrides.MaskHost)
	}
	if overrides.MaskPort != nil {
		next.MaskPort = *overrides.MaskPort
	}
	if overrides.APIPort != nil {
		next.APIPort = *overrides.APIPort
	}
	if overrides.UseMiddleProxy != nil {
		next.UseMiddleProxy = *overrides.UseMiddleProxy
	}
	if overrides.LogLevel != nil {
		next.LogLevel = strings.TrimSpace(*overrides.LogLevel)
	}

	next = normalizeDraft(next)
	validationErr := validate(next)
	if validationErr != nil {
		return Fields{}, validationErr
	}

	return next, nil
}

func Generate(fields Fields) (string, Fields, error) {
	normalized, validationErr := ApplyDefaults(fields, PartialFields{})
	if validationErr != nil {
		return "", Fields{}, validationErr
	}

	payload := document{
		General: generalSection{
			UseMiddleProxy: normalized.UseMiddleProxy,
			LogLevel:       normalized.LogLevel,
			Modes: modesSection{
				Classic: false,
				Secure:  false,
				TLS:     true,
			},
			Links: linksSection{
				Show:       "*",
				PublicHost: normalized.PublicHost,
				PublicPort: normalized.PublicPort,
			},
		},
		Server: serverSection{
			Port: normalized.PublicPort,
			API: serverAPI{
				Enabled:   true,
				Listen:    net.JoinHostPort("127.0.0.1", strconv.Itoa(normalized.APIPort)),
				Whitelist: []string{"127.0.0.1/32", "::1/128"},
			},
			Listeners: []serverListener{{IP: "0.0.0.0"}},
		},
		Access: accessSection{
			Users: map[string]string{DefaultUserName: normalized.Secret},
		},
		Censorship: censorshipSection{
			TLSDomain:    normalized.TLSDomain,
			Mask:         true,
			MaskHost:     normalized.MaskHost,
			MaskPort:     normalized.MaskPort,
			TLSEmulation: false,
			TLSFrontDir:  "tlsfront",
		},
	}

	encoded, err := toml.Marshal(payload)
	if err != nil {
		return "", Fields{}, fmt.Errorf("marshal telemt config: %w", err)
	}

	return string(encoded), normalized, nil
}

func Parse(text string) (Fields, *ValidationError) {
	var payload document
	if err := toml.Unmarshal([]byte(text), &payload); err != nil {
		validationErr := &ValidationError{}
		validationErr.add("config_text", err.Error())
		return Fields{}, validationErr
	}

	apiPort, apiPortMessage := parseAPIPort(payload.Server.API.Listen)
	secret := firstSecret(payload.Access.Users)

	fields := normalizeParsed(Fields{
		PublicHost:     payload.General.Links.PublicHost,
		PublicPort:     payload.General.Links.PublicPort,
		TLSDomain:      payload.Censorship.TLSDomain,
		Secret:         secret,
		MaskHost:       payload.Censorship.MaskHost,
		MaskPort:       payload.Censorship.MaskPort,
		APIPort:        apiPort,
		UseMiddleProxy: payload.General.UseMiddleProxy,
		LogLevel:       payload.General.LogLevel,
	})

	if fields.PublicPort == 0 {
		fields.PublicPort = payload.Server.Port
	}
	if fields.TLSDomain == "" {
		fields.TLSDomain = fields.PublicHost
	}

	validationErr := validate(fields)
	if apiPortMessage != "" {
		if validationErr == nil {
			validationErr = &ValidationError{}
		}
		validationErr.add("api_port", apiPortMessage)
	}
	if validationErr != nil {
		return Fields{}, validationErr
	}

	return fields, nil
}

func ParsePartial(text string) (PartialFields, error) {
	var payload document
	if err := toml.Unmarshal([]byte(text), &payload); err != nil {
		return PartialFields{}, err
	}

	partial := PartialFields{}

	if value := strings.TrimSpace(payload.General.Links.PublicHost); value != "" {
		partial.PublicHost = &value
	}

	publicPort := payload.General.Links.PublicPort
	if publicPort == 0 {
		publicPort = payload.Server.Port
	}
	if publicPort > 0 {
		partial.PublicPort = &publicPort
	}

	tlsDomain := strings.TrimSpace(payload.Censorship.TLSDomain)
	if tlsDomain == "" && partial.PublicHost != nil {
		tlsDomain = *partial.PublicHost
	}
	if tlsDomain != "" {
		partial.TLSDomain = &tlsDomain
	}

	if secret := firstSecret(payload.Access.Users); secret != "" {
		partial.Secret = &secret
	}

	if value := strings.TrimSpace(payload.Censorship.MaskHost); value != "" {
		partial.MaskHost = &value
	}
	if payload.Censorship.MaskPort > 0 {
		maskPort := payload.Censorship.MaskPort
		partial.MaskPort = &maskPort
	}

	if apiPort, _ := parseAPIPort(payload.Server.API.Listen); apiPort > 0 {
		partial.APIPort = &apiPort
	}

	if value := strings.TrimSpace(payload.General.LogLevel); value != "" {
		value = strings.ToLower(value)
		partial.LogLevel = &value
	}

	partial.UseMiddleProxy = &payload.General.UseMiddleProxy

	return partial, nil
}

func PreviewLink(fields Fields) string {
	return fmt.Sprintf(
		"https://t.me/proxy?server=%s&port=%d&secret=%s",
		url.QueryEscape(fields.PublicHost),
		fields.PublicPort,
		url.QueryEscape("ee"+fields.Secret+hex.EncodeToString([]byte(fields.TLSDomain))),
	)
}

func normalizeDraft(fields Fields) Fields {
	fields.PublicHost = strings.TrimSpace(fields.PublicHost)
	fields.TLSDomain = strings.TrimSpace(fields.TLSDomain)
	fields.Secret = strings.ToLower(strings.TrimSpace(fields.Secret))
	fields.MaskHost = strings.TrimSpace(fields.MaskHost)
	fields.LogLevel = strings.ToLower(strings.TrimSpace(fields.LogLevel))

	if fields.PublicPort == 0 {
		fields.PublicPort = 443
	}
	if fields.TLSDomain == "" {
		fields.TLSDomain = fields.PublicHost
	}
	if fields.Secret == "" {
		fields.Secret = mustSecret()
	}
	if fields.MaskHost == "" {
		fields.MaskHost = DefaultMaskHost
	}
	if fields.MaskPort == 0 {
		fields.MaskPort = DefaultMaskPort
	}
	if fields.APIPort == 0 {
		fields.APIPort = DefaultAPIPort
	}
	if fields.LogLevel == "" {
		fields.LogLevel = DefaultLogLevel
	}

	return fields
}

func normalizeParsed(fields Fields) Fields {
	fields.PublicHost = strings.TrimSpace(fields.PublicHost)
	fields.TLSDomain = strings.TrimSpace(fields.TLSDomain)
	fields.Secret = strings.ToLower(strings.TrimSpace(fields.Secret))
	fields.MaskHost = strings.TrimSpace(fields.MaskHost)
	fields.LogLevel = strings.ToLower(strings.TrimSpace(fields.LogLevel))
	return fields
}

func validate(fields Fields) *ValidationError {
	validationErr := &ValidationError{}

	if fields.PublicHost == "" {
		validationErr.add("public_host", "is required")
	}
	if fields.PublicPort < 1 || fields.PublicPort > 65535 {
		validationErr.add("public_port", "must be between 1 and 65535")
	}
	if fields.TLSDomain == "" {
		validationErr.add("tls_domain", "is required")
	}
	if !isSecret(fields.Secret) {
		validationErr.add("secret", "must be 32 hex chars")
	}
	if fields.MaskHost == "" {
		validationErr.add("mask_host", "is required")
	}
	if fields.MaskPort < 1 || fields.MaskPort > 65535 {
		validationErr.add("mask_port", "must be between 1 and 65535")
	}
	if fields.APIPort < 1 || fields.APIPort > 65535 {
		validationErr.add("api_port", "must be between 1 and 65535")
	}
	if _, ok := allowedLogLevels[fields.LogLevel]; !ok {
		validationErr.add("log_level", "must be one of debug, verbose, normal, silent")
	}

	if validationErr.empty() {
		return nil
	}

	return validationErr
}

func parseAPIPort(value string) (int, string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, "server.api.listen must be host:port"
	}

	_, port, err := net.SplitHostPort(trimmed)
	if err != nil {
		return 0, "server.api.listen must be host:port"
	}

	parsed, err := strconv.Atoi(port)
	if err != nil {
		return 0, "server.api.listen port must be numeric"
	}

	return parsed, ""
}

func firstSecret(users map[string]string) string {
	if len(users) == 0 {
		return ""
	}

	if secret := strings.TrimSpace(users[DefaultUserName]); secret != "" {
		return secret
	}

	keys := make([]string, 0, len(users))
	for key := range users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.TrimSpace(users[keys[0]])
}

func isSecret(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func mustSecret() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate secret: %v", err))
	}
	return hex.EncodeToString(buffer)
}
