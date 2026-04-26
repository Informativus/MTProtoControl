package sshlayer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	AuthTypePrivateKeyText = "private_key_text"
	AuthTypePrivateKeyPath = "private_key_path"
	AuthTypePassword       = "password"

	defaultSSHPort        = 22
	defaultConnectTimeout = 10 * time.Second
	defaultCommandTimeout = 5 * time.Second
	idleClientTimeout     = 45 * time.Second
	connectRetryDelay     = 150 * time.Millisecond
	maxConnectAttempts    = 3
)

type ErrorKind string

const (
	ErrorKindConnect ErrorKind = "connect"
	ErrorKindAuth    ErrorKind = "auth"
	ErrorKindHostKey ErrorKind = "host_key"
	ErrorKindTimeout ErrorKind = "timeout"
)

type Tester interface {
	Test(ctx context.Context, input TestRequest) (TestResult, error)
}

type Service struct {
	connectTimeout time.Duration
	commandTimeout time.Duration

	poolMu     sync.Mutex
	clients    map[string]*pooledClient
	connecting map[string]chan struct{}
}

type pooledClient struct {
	client    *ssh.Client
	refs      int
	idleTimer *time.Timer
}

type clientLease struct {
	client  *ssh.Client
	release func(discard bool)
}

type TestRequest struct {
	Host           string  `json:"host"`
	SSHUser        string  `json:"ssh_user"`
	SSHPort        int     `json:"ssh_port"`
	AuthType       string  `json:"auth_type"`
	Password       *string `json:"password"`
	PrivateKeyText *string `json:"private_key_text"`
	PrivateKeyPath *string `json:"private_key_path"`
	Passphrase     *string `json:"passphrase"`
}

type TestResult struct {
	OK       bool            `json:"ok"`
	Facts    ServerFacts     `json:"facts"`
	Commands []CommandResult `json:"commands"`
}

type ServerFacts struct {
	Hostname             string     `json:"hostname,omitempty"`
	CurrentUser          string     `json:"current_user,omitempty"`
	Architecture         string     `json:"architecture,omitempty"`
	OSRelease            *OSRelease `json:"os_release,omitempty"`
	DockerPath           string     `json:"docker_path,omitempty"`
	DockerVersion        string     `json:"docker_version,omitempty"`
	DockerComposeVersion string     `json:"docker_compose_version,omitempty"`
}

type OSRelease struct {
	Name       string `json:"name,omitempty"`
	PrettyName string `json:"pretty_name,omitempty"`
	ID         string `json:"id,omitempty"`
	VersionID  string `json:"version_id,omitempty"`
}

type CommandResult struct {
	Name       string `json:"name"`
	Command    string `json:"command"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
	OK         bool   `json:"ok"`
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
	e.Fields[field] = message
}

func (e *ValidationError) empty() bool {
	return len(e.Fields) == 0
}

type OperationError struct {
	Kind    ErrorKind
	Message string
	err     error
}

func (e *OperationError) Error() string {
	return e.Message
}

func (e *OperationError) Unwrap() error {
	return e.err
}

type commandSpec struct {
	name    string
	command string
}

var testCommands = []commandSpec{
	{name: "hostname", command: "hostname"},
	{name: "current_user", command: "id -un"},
	{name: "architecture", command: "uname -m"},
	{name: "os_release", command: "cat /etc/os-release"},
	{name: "docker_path", command: "command -v docker"},
	{name: "docker_version", command: "docker --version"},
	{name: "docker_compose_version", command: "docker compose version"},
}

func NewTester() *Service {
	return &Service{
		connectTimeout: defaultConnectTimeout,
		commandTimeout: defaultCommandTimeout,
		clients:        map[string]*pooledClient{},
		connecting:     map[string]chan struct{}{},
	}
}

func (s *Service) Test(ctx context.Context, input TestRequest) (TestResult, error) {
	normalized, validationErr := normalizeRequest(input)
	if validationErr != nil {
		return TestResult{}, validationErr
	}

	authMethods, authFingerprint, err := resolveAuth(normalized)
	if err != nil {
		return TestResult{}, err
	}

	hostKeyCallback, err := newHostKeyCallback()
	if err != nil {
		return TestResult{}, &OperationError{
			Kind:    ErrorKindHostKey,
			Message: conciseMessage(err),
			err:     err,
		}
	}

	config := &ssh.ClientConfig{
		User:            normalized.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         s.connectTimeout,
	}

	connectCtx, cancelConnect := context.WithTimeout(ctx, s.connectTimeout)
	defer cancelConnect()

	lease, err := s.borrowClient(connectCtx, normalized, authFingerprint, config)
	if err != nil {
		return TestResult{}, classifyConnectionError(err)
	}
	defer lease.release(false)

	results := make([]CommandResult, 0, len(testCommands))
	for _, command := range testCommands {
		commandCtx, cancelCommand := context.WithTimeout(ctx, s.commandTimeout)
		results = append(results, runCommand(commandCtx, lease.client, command))
		cancelCommand()
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return TestResult{}, &OperationError{
			Kind:    ErrorKindTimeout,
			Message: "ssh test timed out",
			err:     ctx.Err(),
		}
	}

	return TestResult{
		OK:       true,
		Facts:    deriveFacts(results),
		Commands: results,
	}, nil

}

func normalizeRequest(input TestRequest) (TestRequest, *ValidationError) {
	normalized := input
	normalized.Host = strings.TrimSpace(normalized.Host)
	normalized.SSHUser = strings.TrimSpace(normalized.SSHUser)
	normalized.AuthType = strings.TrimSpace(normalized.AuthType)

	if normalized.SSHPort == 0 {
		normalized.SSHPort = defaultSSHPort
	}
	if normalized.PrivateKeyText != nil {
		value := strings.TrimSpace(*normalized.PrivateKeyText)
		normalized.PrivateKeyText = &value
	}
	if normalized.Password != nil {
		value := strings.TrimSpace(*normalized.Password)
		if value == "" {
			normalized.Password = nil
		} else {
			normalized.Password = &value
		}
	}
	if normalized.PrivateKeyPath != nil {
		value := strings.TrimSpace(*normalized.PrivateKeyPath)
		normalized.PrivateKeyPath = &value
	}
	if normalized.Passphrase != nil && *normalized.Passphrase == "" {
		normalized.Passphrase = nil
	}

	validationErr := &ValidationError{}
	if normalized.Host == "" {
		validationErr.add("host", "is required")
	}
	if normalized.SSHUser == "" {
		validationErr.add("ssh_user", "is required")
	}
	if normalized.SSHPort < 1 || normalized.SSHPort > 65535 {
		validationErr.add("ssh_port", "must be between 1 and 65535")
	}

	switch normalized.AuthType {
	case AuthTypePrivateKeyText:
		if normalized.PrivateKeyText == nil || *normalized.PrivateKeyText == "" {
			validationErr.add("private_key_text", "is required when auth_type=private_key_text")
		}
		if normalized.Password != nil && *normalized.Password != "" {
			validationErr.add("password", "must not be provided when auth_type=private_key_text")
		}
		if normalized.PrivateKeyPath != nil && *normalized.PrivateKeyPath != "" {
			validationErr.add("private_key_path", "must not be provided when auth_type=private_key_text")
		}
	case AuthTypePrivateKeyPath:
		if normalized.PrivateKeyPath == nil || *normalized.PrivateKeyPath == "" {
			validationErr.add("private_key_path", "is required when auth_type=private_key_path")
		} else {
			expandedPath, err := expandHomePath(*normalized.PrivateKeyPath)
			if err != nil {
				validationErr.add("private_key_path", "could not resolve home directory")
			} else {
				normalized.PrivateKeyPath = &expandedPath
			}
		}
		if normalized.PrivateKeyText != nil && *normalized.PrivateKeyText != "" {
			validationErr.add("private_key_text", "must not be provided when auth_type=private_key_path")
		}
		if normalized.Password != nil && *normalized.Password != "" {
			validationErr.add("password", "must not be provided when auth_type=private_key_path")
		}
	case AuthTypePassword:
		if normalized.Password == nil || *normalized.Password == "" {
			validationErr.add("password", "is required when auth_type=password")
		}
		if normalized.PrivateKeyText != nil && *normalized.PrivateKeyText != "" {
			validationErr.add("private_key_text", "must not be provided when auth_type=password")
		}
		if normalized.PrivateKeyPath != nil && *normalized.PrivateKeyPath != "" {
			validationErr.add("private_key_path", "must not be provided when auth_type=password")
		}
		if normalized.Passphrase != nil {
			validationErr.add("passphrase", "must not be provided when auth_type=password")
		}
	default:
		validationErr.add("auth_type", "must be private_key_text, private_key_path, or password")
	}

	if validationErr.empty() {
		return normalized, nil
	}

	return normalized, validationErr
}

func loadSigner(input TestRequest) (ssh.Signer, error) {
	privateKey, fieldName, err := loadPrivateKey(input)
	if err != nil {
		return nil, err
	}

	signer, err := parseSigner(privateKey, fieldName, input.Passphrase)
	if err != nil {
		return nil, err
	}

	return signer, nil
}

func resolveAuth(input TestRequest) ([]ssh.AuthMethod, string, error) {
	switch input.AuthType {
	case AuthTypePassword:
		return passwordAuthMethods(*input.Password), passwordFingerprint(*input.Password), nil
	case AuthTypePrivateKeyText, AuthTypePrivateKeyPath:
		signer, err := loadSigner(input)
		if err != nil {
			return nil, "", err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, ssh.FingerprintSHA256(signer.PublicKey()), nil
	default:
		validationErr := &ValidationError{}
		validationErr.add("auth_type", "must be private_key_text, private_key_path, or password")
		return nil, "", validationErr
	}
}

func loadPrivateKey(input TestRequest) ([]byte, string, error) {
	switch input.AuthType {
	case AuthTypePrivateKeyText:
		return []byte(*input.PrivateKeyText), "private_key_text", nil
	case AuthTypePrivateKeyPath:
		privateKey, err := os.ReadFile(*input.PrivateKeyPath)
		if err != nil {
			validationErr := &ValidationError{}
			validationErr.add("private_key_path", "could not read private key file")
			return nil, "", validationErr
		}
		return privateKey, "private_key_path", nil
	default:
		validationErr := &ValidationError{}
		validationErr.add("auth_type", "must be private_key_text, private_key_path, or password")
		return nil, "", validationErr
	}
}

func passwordAuthMethods(password string) []ssh.AuthMethod {
	return []ssh.AuthMethod{
		ssh.Password(password),
		ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for index := range answers {
				answers[index] = password
			}
			return answers, nil
		}),
	}
}

func passwordFingerprint(password string) string {
	sum := sha256.Sum256([]byte(password))
	return "password-sha256:" + fmt.Sprintf("%x", sum)
}

func parseSigner(privateKey []byte, fieldName string, passphrase *string) (ssh.Signer, error) {
	if passphrase != nil {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(privateKey, []byte(*passphrase))
		if err == nil {
			return signer, nil
		}
		if signer, fallbackErr := ssh.ParsePrivateKey(privateKey); fallbackErr == nil {
			return signer, nil
		}

		validationErr := &ValidationError{}
		validationErr.add("passphrase", "could not decrypt private key with the provided passphrase")
		return nil, validationErr
	}

	signer, err := ssh.ParsePrivateKey(privateKey)
	if err == nil {
		return signer, nil
	}

	validationErr := &ValidationError{}
	var missingPassphrase *ssh.PassphraseMissingError
	if errors.As(err, &missingPassphrase) {
		validationErr.add("passphrase", "is required for the provided private key")
		return nil, validationErr
	}
	validationErr.add(fieldName, "contains an invalid private key")
	return nil, validationErr
}

func newHostKeyCallback() (ssh.HostKeyCallback, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	paths := make([]string, 0, 2)
	for _, name := range []string{"known_hosts", "known_hosts2"} {
		path := filepath.Join(sshDir, name)
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("no known_hosts file found in %s", sshDir)
	}

	callback, err := knownhosts.New(paths...)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}

	return callback, nil
}

func dialClient(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	var dialer net.Dialer
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}

	if err := setConnectionDeadline(connection, ctx); err != nil {
		connection.Close()
		return nil, err
	}

	clientConn, channels, requests, err := ssh.NewClientConn(connection, address, config)
	if err != nil {
		connection.Close()
		return nil, err
	}

	if err := connection.SetDeadline(time.Time{}); err != nil {
		clientConn.Close()
		return nil, err
	}

	return ssh.NewClient(clientConn, channels, requests), nil
}

func (s *Service) borrowClient(ctx context.Context, input TestRequest, authFingerprint string, config *ssh.ClientConfig) (*clientLease, error) {
	address := net.JoinHostPort(input.Host, strconv.Itoa(input.SSHPort))
	key := connectionPoolKey(input, authFingerprint)

	for {
		if lease, ok := s.borrowIdleClient(ctx, key); ok {
			return lease, nil
		}

		waitCh, shouldConnect := s.beginConnect(key)
		if !shouldConnect {
			if waitCh == nil {
				continue
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-waitCh:
				continue
			}
		}

		client, err := dialClientWithRetry(ctx, address, config)
		if err != nil {
			s.finishConnect(key, nil)
			return nil, err
		}

		lease := s.finishConnect(key, client)
		return lease, nil
	}
}

func (s *Service) borrowIdleClient(ctx context.Context, key string) (*clientLease, bool) {
	s.poolMu.Lock()
	entry := s.clients[key]
	if entry == nil {
		s.poolMu.Unlock()
		return nil, false
	}

	wasIdle := entry.refs == 0
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
		entry.idleTimer = nil
	}
	entry.refs++
	client := entry.client
	s.poolMu.Unlock()

	if wasIdle {
		if err := checkClientAlive(ctx, client); err != nil {
			s.releaseBorrowedClient(key, client, true)
			return nil, false
		}
	}

	return &clientLease{
		client: client,
		release: func(discard bool) {
			s.releaseBorrowedClient(key, client, discard)
		},
	}, true
}

func (s *Service) beginConnect(key string) (<-chan struct{}, bool) {
	s.poolMu.Lock()
	defer s.poolMu.Unlock()

	if _, ok := s.clients[key]; ok {
		return nil, false
	}
	if waitCh, ok := s.connecting[key]; ok {
		return waitCh, false
	}

	waitCh := make(chan struct{})
	s.connecting[key] = waitCh
	return waitCh, true
}

func (s *Service) finishConnect(key string, client *ssh.Client) *clientLease {
	s.poolMu.Lock()
	waitCh := s.connecting[key]
	delete(s.connecting, key)
	if waitCh != nil {
		close(waitCh)
	}

	if client == nil {
		s.poolMu.Unlock()
		return nil
	}

	if existing := s.clients[key]; existing != nil {
		existing.refs++
		borrowed := existing.client
		s.poolMu.Unlock()
		_ = client.Close()
		return &clientLease{
			client: borrowed,
			release: func(discard bool) {
				s.releaseBorrowedClient(key, borrowed, discard)
			},
		}
	}

	s.clients[key] = &pooledClient{client: client, refs: 1}
	s.poolMu.Unlock()

	return &clientLease{
		client: client,
		release: func(discard bool) {
			s.releaseBorrowedClient(key, client, discard)
		},
	}
}

func (s *Service) releaseBorrowedClient(key string, client *ssh.Client, discard bool) {
	var toClose *ssh.Client

	s.poolMu.Lock()
	entry := s.clients[key]
	if entry == nil || entry.client != client {
		s.poolMu.Unlock()
		if discard {
			_ = client.Close()
		}
		return
	}

	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
		entry.idleTimer = nil
	}

	if discard {
		delete(s.clients, key)
		toClose = entry.client
		s.poolMu.Unlock()
		_ = toClose.Close()
		return
	}

	if entry.refs > 0 {
		entry.refs--
	}
	if entry.refs == 0 {
		entry.idleTimer = time.AfterFunc(idleClientTimeout, func() {
			s.closeIdleClient(key, client)
		})
	}
	s.poolMu.Unlock()
}

func (s *Service) closeIdleClient(key string, client *ssh.Client) {
	var toClose *ssh.Client

	s.poolMu.Lock()
	entry := s.clients[key]
	if entry != nil && entry.client == client && entry.refs == 0 {
		delete(s.clients, key)
		toClose = entry.client
	}
	s.poolMu.Unlock()

	if toClose != nil {
		_ = toClose.Close()
	}
}

func dialClientWithRetry(ctx context.Context, address string, config *ssh.ClientConfig) (*ssh.Client, error) {
	var lastErr error
	backoff := connectRetryDelay

	for attempt := 1; attempt <= maxConnectAttempts; attempt++ {
		client, err := dialClient(ctx, address, config)
		if err == nil {
			return client, nil
		}

		lastErr = err
		if attempt == maxConnectAttempts || !shouldRetryConnectError(err) {
			break
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
	}

	return nil, lastErr
}

func shouldRetryConnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}

	message := strings.ToLower(conciseMessage(err))
	return strings.Contains(message, "handshake failed: eof") ||
		strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "connection closed by remote host") ||
		strings.Contains(message, "kex_exchange_identification")
}

func checkClientAlive(ctx context.Context, client *ssh.Client) error {
	type result struct {
		err error
	}

	response := make(chan result, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		response <- result{err: err}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case reply := <-response:
		return reply.err
	}
}

func connectionPoolKey(input TestRequest, authFingerprint string) string {
	return strings.Join([]string{
		strings.TrimSpace(input.Host),
		strconv.Itoa(input.SSHPort),
		strings.TrimSpace(input.SSHUser),
		authFingerprint,
	}, "|")
}

func setConnectionDeadline(connection net.Conn, ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil
	}
	return connection.SetDeadline(deadline)
}

func runCommand(ctx context.Context, client *ssh.Client, command commandSpec) CommandResult {
	result := CommandResult{
		Name:     command.name,
		Command:  command.command,
		ExitCode: -1,
	}

	startedAt := time.Now()
	session, err := client.NewSession()
	if err != nil {
		result.Stderr = conciseMessage(err)
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Start(shellCommand(command.command)); err != nil {
		result.Stderr = conciseMessage(err)
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case err := <-done:
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		if err == nil {
			result.ExitCode = 0
			result.OK = true
			return result
		}

		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitStatus()
			if result.Stderr == "" {
				result.Stderr = conciseMessage(err)
			}
			return result
		}

		result.Stderr = appendMessage(result.Stderr, conciseMessage(err))
		return result
	case <-ctx.Done():
		result.Stdout = stdout.String()
		result.Stderr = appendMessage(stderr.String(), "command timed out")
		result.DurationMS = time.Since(startedAt).Milliseconds()
		result.TimedOut = true
		_ = session.Close()
		return result
	}
}

func deriveFacts(results []CommandResult) ServerFacts {
	facts := ServerFacts{}
	for _, result := range results {
		if !result.OK {
			continue
		}

		value := strings.TrimSpace(result.Stdout)
		switch result.Name {
		case "hostname":
			facts.Hostname = value
		case "current_user":
			facts.CurrentUser = value
		case "architecture":
			facts.Architecture = value
		case "os_release":
			facts.OSRelease = parseOSRelease(result.Stdout)
		case "docker_path":
			facts.DockerPath = value
		case "docker_version":
			facts.DockerVersion = value
		case "docker_compose_version":
			facts.DockerComposeVersion = value
		}
	}
	return facts
}

func parseOSRelease(output string) *OSRelease {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = decodeOSReleaseValue(value)
	}

	if len(values) == 0 {
		return nil
	}

	return &OSRelease{
		Name:       values["NAME"],
		PrettyName: values["PRETTY_NAME"],
		ID:         values["ID"],
		VersionID:  values["VERSION_ID"],
	}
}

func decodeOSReleaseValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) >= 2 && strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
		unquoted, err := strconv.Unquote(trimmed)
		if err == nil {
			return unquoted
		}
	}
	return trimmed
}

func classifyConnectionError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &OperationError{
			Kind:    ErrorKindTimeout,
			Message: "ssh connection timed out",
			err:     err,
		}
	}

	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		return &OperationError{
			Kind:    ErrorKindHostKey,
			Message: describeHostKeyError(keyErr),
			err:     err,
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &OperationError{
			Kind:    ErrorKindTimeout,
			Message: "ssh connection timed out",
			err:     err,
		}
	}

	if strings.Contains(err.Error(), "unable to authenticate") {
		return &OperationError{
			Kind:    ErrorKindAuth,
			Message: "ssh authentication failed",
			err:     err,
		}
	}

	return &OperationError{
		Kind:    ErrorKindConnect,
		Message: appendMessage("ssh connection failed", conciseMessage(err)),
		err:     err,
	}
}

func describeHostKeyError(err *knownhosts.KeyError) string {
	if len(err.Want) == 0 {
		return "ssh host key is not present in known_hosts"
	}
	return "ssh host key does not match known_hosts"
}

func shellCommand(command string) string {
	return "sh -lc " + shellQuote(command)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func conciseMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}

func appendMessage(base, value string) string {
	base = strings.TrimSpace(base)
	value = strings.TrimSpace(value)
	if base == "" {
		return value
	}
	if value == "" {
		return base
	}
	return base + ": " + value
}

func expandHomePath(path string) (string, error) {
	if path == "~" {
		return os.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(homeDir, path[2:]), nil
	}
	return path, nil
}
