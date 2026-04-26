package sshlayer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type CommandRequest struct {
	Name    string
	Command string
	Timeout time.Duration
}

type UploadRequest struct {
	Name       string
	RemotePath string
	Content    []byte
	Mode       string
	Timeout    time.Duration
}

type Executor interface {
	Run(ctx context.Context, input TestRequest, request CommandRequest) (CommandResult, error)
	Upload(ctx context.Context, input TestRequest, request UploadRequest) (CommandResult, error)
}

func (s *Service) Run(ctx context.Context, input TestRequest, request CommandRequest) (CommandResult, error) {
	lease, err := s.leaseClient(ctx, input)
	if err != nil {
		return CommandResult{}, err
	}
	defer lease.release(false)

	commandCtx, cancel := context.WithTimeout(ctx, withDefaultTimeout(request.Timeout, s.commandTimeout))
	defer cancel()

	return runCommand(commandCtx, lease.client, commandSpec{
		name:    strings.TrimSpace(request.Name),
		command: request.Command,
	}), nil
}

func (s *Service) Upload(ctx context.Context, input TestRequest, request UploadRequest) (CommandResult, error) {
	lease, err := s.leaseClient(ctx, input)
	if err != nil {
		return CommandResult{}, err
	}
	defer lease.release(false)

	mode := strings.TrimSpace(request.Mode)
	if mode == "" {
		mode = "600"
	}

	command := fmt.Sprintf(
		"mkdir -p %s && cat > %s && chmod %s %s",
		shellQuote(path.Dir(request.RemotePath)),
		shellQuote(request.RemotePath),
		mode,
		shellQuote(request.RemotePath),
	)

	uploadCtx, cancel := context.WithTimeout(ctx, withDefaultTimeout(request.Timeout, s.commandTimeout))
	defer cancel()

	return runCommandWithInput(uploadCtx, lease.client, strings.TrimSpace(request.Name), command, bytes.NewReader(request.Content)), nil
}

func (s *Service) leaseClient(ctx context.Context, input TestRequest) (*clientLease, error) {
	normalized, validationErr := normalizeRequest(input)
	if validationErr != nil {
		return nil, validationErr
	}

	authMethods, authFingerprint, err := resolveAuth(normalized)
	if err != nil {
		return nil, err
	}

	hostKeyCallback, err := newHostKeyCallback()
	if err != nil {
		return nil, &OperationError{
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
		return nil, classifyConnectionError(err)
	}

	return lease, nil
}

func runCommandWithInput(ctx context.Context, client *ssh.Client, name, command string, stdin io.Reader) CommandResult {
	result := CommandResult{
		Name:     name,
		Command:  command,
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
	session.Stdin = stdin

	if err := session.Start(shellCommand(command)); err != nil {
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

func withDefaultTimeout(timeout, fallback time.Duration) time.Duration {
	if timeout > 0 {
		return timeout
	}
	return fallback
}
