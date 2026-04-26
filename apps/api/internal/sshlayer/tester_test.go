package sshlayer

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestNormalizeRequestDefaultsAndValidates(t *testing.T) {
	privateKeyText := "  -----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----  "
	request := TestRequest{
		Host:           "  203.0.113.10  ",
		SSHUser:        "  operator  ",
		AuthType:       AuthTypePrivateKeyText,
		PrivateKeyText: &privateKeyText,
	}

	normalized, validationErr := normalizeRequest(request)
	if validationErr != nil {
		t.Fatalf("expected valid request, got %v", validationErr)
	}
	if normalized.SSHPort != defaultSSHPort {
		t.Fatalf("expected default ssh port %d, got %d", defaultSSHPort, normalized.SSHPort)
	}
	if normalized.Host != "203.0.113.10" {
		t.Fatalf("expected trimmed host, got %q", normalized.Host)
	}
	if normalized.SSHUser != "operator" {
		t.Fatalf("expected trimmed ssh user, got %q", normalized.SSHUser)
	}
	if *normalized.PrivateKeyText != "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----" {
		t.Fatalf("expected trimmed private key text, got %q", *normalized.PrivateKeyText)
	}

	path := "/tmp/test-key"
	request = TestRequest{
		Host:           "host",
		SSHUser:        "user",
		SSHPort:        70000,
		AuthType:       AuthTypePrivateKeyText,
		PrivateKeyPath: &path,
	}
	_, validationErr = normalizeRequest(request)
	if validationErr == nil {
		t.Fatal("expected validation error")
	}
	if validationErr.Fields["private_key_text"] != "is required when auth_type=private_key_text" {
		t.Fatalf("expected private_key_text validation, got %#v", validationErr.Fields)
	}
	if validationErr.Fields["private_key_path"] != "must not be provided when auth_type=private_key_text" {
		t.Fatalf("expected private_key_path validation, got %#v", validationErr.Fields)
	}
	if validationErr.Fields["ssh_port"] != "must be between 1 and 65535" {
		t.Fatalf("expected ssh_port validation, got %#v", validationErr.Fields)
	}
}

func TestNormalizeRequestSupportsPasswordAuth(t *testing.T) {
	password := "  hunter2  "
	request := TestRequest{
		Host:     "  203.0.113.10  ",
		SSHUser:  "  operator  ",
		AuthType: AuthTypePassword,
		Password: &password,
	}

	normalized, validationErr := normalizeRequest(request)
	if validationErr != nil {
		t.Fatalf("expected valid password request, got %v", validationErr)
	}
	if normalized.SSHPort != defaultSSHPort {
		t.Fatalf("expected default ssh port %d, got %d", defaultSSHPort, normalized.SSHPort)
	}
	if normalized.Password == nil || *normalized.Password != "hunter2" {
		t.Fatalf("expected trimmed password, got %#v", normalized.Password)
	}

	privateKeyPath := "/tmp/test-key"
	passphrase := "secret"
	request = TestRequest{
		Host:           "host",
		SSHUser:        "user",
		AuthType:       AuthTypePassword,
		PrivateKeyPath: &privateKeyPath,
		Passphrase:     &passphrase,
	}

	_, validationErr = normalizeRequest(request)
	if validationErr == nil {
		t.Fatal("expected validation error")
	}
	if validationErr.Fields["password"] != "is required when auth_type=password" {
		t.Fatalf("expected password validation, got %#v", validationErr.Fields)
	}
	if validationErr.Fields["private_key_path"] != "must not be provided when auth_type=password" {
		t.Fatalf("expected private_key_path validation, got %#v", validationErr.Fields)
	}
	if validationErr.Fields["passphrase"] != "must not be provided when auth_type=password" {
		t.Fatalf("expected passphrase validation, got %#v", validationErr.Fields)
	}
}

func TestParseOSRelease(t *testing.T) {
	parsed := parseOSRelease(`NAME="Ubuntu"
PRETTY_NAME="Ubuntu 24.04.2 LTS"
ID=ubuntu
VERSION_ID="24.04"
`)
	if parsed == nil {
		t.Fatal("expected parsed os-release")
	}
	if parsed.Name != "Ubuntu" {
		t.Fatalf("expected name Ubuntu, got %q", parsed.Name)
	}
	if parsed.PrettyName != "Ubuntu 24.04.2 LTS" {
		t.Fatalf("expected pretty name, got %q", parsed.PrettyName)
	}
	if parsed.ID != "ubuntu" {
		t.Fatalf("expected id ubuntu, got %q", parsed.ID)
	}
	if parsed.VersionID != "24.04" {
		t.Fatalf("expected version id 24.04, got %q", parsed.VersionID)
	}
}

func TestDeriveFacts(t *testing.T) {
	results := []CommandResult{
		{Name: "hostname", Stdout: "proxy-node-1\n", OK: true},
		{Name: "current_user", Stdout: "operator\n", OK: true},
		{Name: "architecture", Stdout: "x86_64\n", OK: true},
		{Name: "os_release", Stdout: "ID=ubuntu\nPRETTY_NAME=\"Ubuntu 24.04 LTS\"\n", OK: true},
		{Name: "docker_path", Stdout: "/usr/bin/docker\n", OK: true},
		{Name: "docker_version", Stdout: "Docker version 29.4.1, build deadbeef\n", OK: true},
		{Name: "docker_compose_version", Stdout: "Docker Compose version v2.40.3\n", OK: true},
	}

	facts := deriveFacts(results)
	if facts.Hostname != "proxy-node-1" {
		t.Fatalf("expected hostname proxy-node-1, got %q", facts.Hostname)
	}
	if facts.CurrentUser != "operator" {
		t.Fatalf("expected current user operator, got %q", facts.CurrentUser)
	}
	if facts.Architecture != "x86_64" {
		t.Fatalf("expected architecture x86_64, got %q", facts.Architecture)
	}
	if facts.OSRelease == nil || facts.OSRelease.ID != "ubuntu" {
		t.Fatalf("expected os release ubuntu, got %#v", facts.OSRelease)
	}
	if facts.DockerPath != "/usr/bin/docker" {
		t.Fatalf("expected docker path, got %q", facts.DockerPath)
	}
	if facts.DockerVersion != "Docker version 29.4.1, build deadbeef" {
		t.Fatalf("expected docker version, got %q", facts.DockerVersion)
	}
	if facts.DockerComposeVersion != "Docker Compose version v2.40.3" {
		t.Fatalf("expected docker compose version, got %q", facts.DockerComposeVersion)
	}
}

func TestShellCommandEscapesSingleQuotes(t *testing.T) {
	output, err := exec.Command("sh", "-lc", shellCommand(`printf '%s\n' "quoted"`)).CombinedOutput()
	if err != nil {
		t.Fatalf("run shell command: %v\n%s", err, output)
	}
	if string(output) != "quoted\n" {
		t.Fatalf("expected quoted output, got %q", output)
	}
}

func TestDialClientTimesOutDuringHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan struct{})
	defer close(serverDone)

	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()

		<-serverDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	startedAt := time.Now()
	_, err = dialClient(ctx, listener.Addr().String(), listener.Addr().String(), &ssh.ClientConfig{
		User:            "operator",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	})
	if err == nil {
		t.Fatal("expected handshake timeout")
	}

	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("expected timeout error, got %v", err)
	}

	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("expected handshake to stop quickly, took %s", elapsed)
	}
}

func TestShouldRetryConnectError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "eof", err: io.EOF, want: true},
		{name: "handshake eof", err: errors.New("ssh: handshake failed: EOF"), want: true},
		{name: "remote closed", err: errors.New("kex_exchange_identification: Connection closed by remote host"), want: true},
		{name: "auth failed", err: errors.New("ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryConnectError(tt.err); got != tt.want {
				t.Fatalf("expected retry=%v, got %v", tt.want, got)
			}
		})
	}
}

func TestConnectionPoolKeyIncludesTargetAndSigner(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	key := connectionPoolKey(TestRequest{Host: "203.0.113.10", SSHPort: 22, SSHUser: "operator"}, ssh.FingerprintSHA256(signer.PublicKey()))
	if !strings.HasPrefix(key, "203.0.113.10|22|operator|") {
		t.Fatalf("expected pool key to include host, port, and user, got %q", key)
	}

	otherKey := connectionPoolKey(TestRequest{Host: "203.0.113.11", SSHPort: 22, SSHUser: "operator"}, ssh.FingerprintSHA256(signer.PublicKey()))
	if key == otherKey {
		t.Fatalf("expected different hosts to produce different pool keys, got %q", key)
	}
	if !strings.HasSuffix(key, ssh.FingerprintSHA256(signer.PublicKey())) {
		t.Fatalf("expected pool key to include signer fingerprint, got %q", key)
	}
	if strings.Contains(key, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatalf("pool key must not include raw private key material: %q", key)
	}
}

func TestConnectionPoolKeyHashesPasswordAuth(t *testing.T) {
	key := connectionPoolKey(TestRequest{Host: "203.0.113.10", SSHPort: 22, SSHUser: "operator"}, passwordFingerprint("hunter2"))
	if !strings.HasPrefix(key, "203.0.113.10|22|operator|password-sha256:") {
		t.Fatalf("expected password fingerprint prefix, got %q", key)
	}
	if strings.Contains(key, "hunter2") {
		t.Fatalf("pool key must not include raw password: %q", key)
	}
	if other := connectionPoolKey(TestRequest{Host: "203.0.113.10", SSHPort: 22, SSHUser: "operator"}, passwordFingerprint("hunter3")); key == other {
		t.Fatalf("expected different passwords to produce different pool keys, got %q", key)
	}
}

func TestResolveSSHAddressesUsesSelfHostAliasForLoopback(t *testing.T) {
	dialAddress, hostKeyAddress := resolveSSHAddresses("127.0.0.1", 22, "host.docker.internal")
	if dialAddress != "host.docker.internal:22" {
		t.Fatalf("expected host alias dial address, got %q", dialAddress)
	}
	if hostKeyAddress != "127.0.0.1:22" {
		t.Fatalf("expected original host key address, got %q", hostKeyAddress)
	}

	dialAddress, hostKeyAddress = resolveSSHAddresses("localhost", 2222, "host.docker.internal")
	if dialAddress != "host.docker.internal:2222" {
		t.Fatalf("expected localhost to route via alias, got %q", dialAddress)
	}
	if hostKeyAddress != "localhost:2222" {
		t.Fatalf("expected localhost host key address, got %q", hostKeyAddress)
	}
}

func TestResolveSSHAddressesLeavesRemoteHostsUntouched(t *testing.T) {
	dialAddress, hostKeyAddress := resolveSSHAddresses("203.0.113.10", 22, "host.docker.internal")
	if dialAddress != "203.0.113.10:22" {
		t.Fatalf("expected remote dial address, got %q", dialAddress)
	}
	if hostKeyAddress != "203.0.113.10:22" {
		t.Fatalf("expected remote host key address, got %q", hostKeyAddress)
	}
}
