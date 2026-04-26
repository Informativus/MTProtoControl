package serverops

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/healthchecks"
	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/serverevents"
	"mtproxy-control/apps/api/internal/sshlayer"
	"mtproxy-control/apps/api/internal/telemtconfig"
)

const (
	defaultLogsTail           = 300
	maxLogsTail               = 2000
	restartTimeout            = 30 * time.Second
	logsTimeout               = 30 * time.Second
	statusTimeout             = 15 * time.Second
	publicReachabilityTimeout = 3 * time.Second
	containerName             = "telemt-mtproto"
	telemtImagePrefix         = "ghcr.io/telemt/telemt"
)

type Service struct {
	events      *serverevents.Repository
	health      *healthchecks.Repository
	ssh         sshlayer.Executor
	now         func() time.Time
	dialContext func(ctx context.Context, network, address string) (net.Conn, error)
}

type Request struct {
	AuthType       string  `json:"auth_type"`
	Password       *string `json:"password"`
	PrivateKeyText *string `json:"private_key_text"`
	PrivateKeyPath *string `json:"private_key_path"`
	Passphrase     *string `json:"passphrase"`
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

type PreconditionError struct {
	Code    string
	Message string
}

func (e *PreconditionError) Error() string {
	return e.Message
}

type StepError struct {
	Code    string
	Step    string
	Message string
	Result  *sshlayer.CommandResult
	Details map[string]string
}

func (e *StepError) Error() string {
	return e.Message
}

type RestartResult struct {
	Result sshlayer.CommandResult `json:"result"`
	Events []serverevents.Event   `json:"events"`
}

type LogsResult struct {
	Result    sshlayer.CommandResult `json:"result"`
	FetchedAt time.Time              `json:"fetched_at"`
}

type CommandStatus struct {
	Status  string                  `json:"status"`
	Summary string                  `json:"summary"`
	Result  *sshlayer.CommandResult `json:"result,omitempty"`
	Error   string                  `json:"error,omitempty"`
}

type TelemtAPIStatus struct {
	Status        string                  `json:"status"`
	Summary       string                  `json:"summary"`
	Result        *sshlayer.CommandResult `json:"result,omitempty"`
	Error         string                  `json:"error,omitempty"`
	UserCount     int                     `json:"user_count"`
	GeneratedLink string                  `json:"generated_link,omitempty"`
}

type PublicPortStatus struct {
	Checked    bool   `json:"checked"`
	Reachable  bool   `json:"reachable"`
	Target     string `json:"target,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	Error      string `json:"error,omitempty"`
	Summary    string `json:"summary"`
	PublicPort int    `json:"public_port"`
}

type CurrentConfigStatus struct {
	Version       int        `json:"version"`
	GeneratedLink string     `json:"generated_link,omitempty"`
	AppliedAt     *time.Time `json:"applied_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type StatusResult struct {
	Container           CommandStatus        `json:"container"`
	TelemtAPI           TelemtAPIStatus      `json:"telemt_api"`
	PublicPort          PublicPortStatus     `json:"public_port"`
	LatestHealth        *healthchecks.Check  `json:"latest_health,omitempty"`
	CurrentConfig       *CurrentConfigStatus `json:"current_config,omitempty"`
	GeneratedLink       string               `json:"generated_link,omitempty"`
	GeneratedLinkSource string               `json:"generated_link_source,omitempty"`
}

type LinkResult struct {
	GeneratedLink string                  `json:"generated_link"`
	Source        string                  `json:"source"`
	ConfigVersion int                     `json:"config_version,omitempty"`
	LiveResult    *sshlayer.CommandResult `json:"live_result,omitempty"`
	Warning       string                  `json:"warning,omitempty"`
}

func NewService(events *serverevents.Repository, health *healthchecks.Repository, ssh sshlayer.Executor) *Service {
	dialer := &net.Dialer{}
	return &Service{
		events: events,
		health: health,
		ssh:    ssh,
		now: func() time.Time {
			return time.Now().UTC()
		},
		dialContext: dialer.DialContext,
	}
}

func (s *Service) Restart(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, request Request) (RestartResult, error) {
	if s.ssh == nil || s.events == nil {
		return RestartResult{}, &PreconditionError{
			Code:    "service_unavailable",
			Message: "server operations service is not configured",
		}
	}

	result, err := s.ssh.Run(ctx, request.toSSHRequest(server), sshlayer.CommandRequest{
		Name:    "restart_compose",
		Command: restartCommand(server.RemoteBasePath, current),
		Timeout: restartTimeout,
	})
	if err != nil {
		return RestartResult{}, err
	}

	level := "info"
	if !result.OK {
		level = "error"
	}

	if _, err := s.appendEvent(ctx, server.ID, level, "restart_compose", "Run Telemt restart command", result); err != nil {
		return RestartResult{}, fmt.Errorf("append restart event: %w", err)
	}

	events, err := s.events.ListByServer(ctx, server.ID, 20)
	if err != nil {
		return RestartResult{}, fmt.Errorf("list restart events: %w", err)
	}

	if !result.OK {
		return RestartResult{}, &StepError{
			Code:    "operation_failed",
			Step:    "restart_compose",
			Message: "Telemt restart command failed",
			Result:  &result,
			Details: map[string]string{
				"step":      "restart_compose",
				"command":   result.Command,
				"exit_code": strconv.Itoa(result.ExitCode),
			},
		}
	}

	return RestartResult{Result: result, Events: events}, nil
}

func (s *Service) Logs(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, request Request, tail int) (LogsResult, error) {
	if s.ssh == nil {
		return LogsResult{}, &PreconditionError{
			Code:    "service_unavailable",
			Message: "server operations service is not configured",
		}
	}

	tail = sanitizeTail(tail)
	result, err := s.ssh.Run(ctx, request.toSSHRequest(server), sshlayer.CommandRequest{
		Name:    "logs_telemt",
		Command: logsCommand(server.RemoteBasePath, current, tail),
		Timeout: logsTimeout,
	})
	if err != nil {
		return LogsResult{}, err
	}

	if !result.OK {
		return LogsResult{}, &StepError{
			Code:    "operation_failed",
			Step:    "logs_telemt",
			Message: "Telemt logs command failed",
			Result:  &result,
			Details: map[string]string{
				"step":      "logs_telemt",
				"command":   result.Command,
				"exit_code": strconv.Itoa(result.ExitCode),
			},
		}
	}

	return LogsResult{Result: result, FetchedAt: s.now()}, nil
}

func (s *Service) Status(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, request *Request) (StatusResult, error) {
	status := StatusResult{
		Container: CommandStatus{
			Status:  "unknown",
			Summary: "SSH auth required for live container status.",
		},
		TelemtAPI: TelemtAPIStatus{
			Status:  "unknown",
			Summary: "SSH auth required for live Telemt API status.",
		},
		PublicPort: s.checkPublicPort(ctx, server, current),
	}

	if current != nil {
		status.CurrentConfig = &CurrentConfigStatus{
			Version:       current.Version,
			GeneratedLink: current.GeneratedLink,
			AppliedAt:     current.AppliedAt,
			CreatedAt:     current.CreatedAt,
		}
		status.GeneratedLink = current.GeneratedLink
		status.GeneratedLinkSource = "config_revision"
	}

	if s.health != nil {
		latestHealth, err := s.health.GetLatest(ctx, server.ID)
		if err != nil {
			return StatusResult{}, err
		}
		status.LatestHealth = latestHealth
	}

	if request == nil || s.ssh == nil {
		if current == nil {
			status.TelemtAPI.Summary = "Generate or apply a config to discover the Telemt API port."
		}
		return status, nil
	}

	sshRequest := request.toSSHRequest(server)
	containerResult, containerErr := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
		Name:    "status_container",
		Command: containerStatusCommand(current),
		Timeout: statusTimeout,
	})
	status.Container = buildContainerStatus(containerResult, containerErr)

	if current == nil {
		status.TelemtAPI = TelemtAPIStatus{
			Status:  "unknown",
			Summary: "Generate or apply a config to discover the Telemt API port.",
		}
		return status, nil
	}

	apiResult, apiErr := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
		Name:    "status_telemt_api",
		Command: telemtAPICommand(current.Fields.APIPort),
		Timeout: statusTimeout,
	})
	status.TelemtAPI = buildTelemtAPIStatus(apiResult, apiErr)
	if status.TelemtAPI.GeneratedLink != "" {
		status.GeneratedLink = status.TelemtAPI.GeneratedLink
		status.GeneratedLinkSource = "telemt_api"
	}

	return status, nil
}

func (s *Service) Link(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, request *Request) (LinkResult, error) {
	if current == nil {
		return LinkResult{}, &PreconditionError{
			Code:    "config_required",
			Message: "generate or deploy a Telemt config before requesting a proxy link",
		}
	}

	link := LinkResult{
		GeneratedLink: current.GeneratedLink,
		Source:        "config_revision",
		ConfigVersion: current.Version,
	}

	if request == nil || s.ssh == nil {
		return link, nil
	}

	result, err := s.ssh.Run(ctx, request.toSSHRequest(server), sshlayer.CommandRequest{
		Name:    "link_telemt_api",
		Command: telemtAPICommand(current.Fields.APIPort),
		Timeout: statusTimeout,
	})
	if err != nil {
		link.Warning = err.Error()
		return link, nil
	}
	link.LiveResult = &result

	if !result.OK {
		link.Warning = firstNonEmpty(strings.TrimSpace(result.Stderr), "Telemt API lookup failed")
		return link, nil
	}

	generatedLink := extractGeneratedLink(result.Stdout)
	if generatedLink == "" {
		link.Warning = "Telemt API response did not include a proxy link"
		return link, nil
	}

	link.GeneratedLink = generatedLink
	link.Source = "telemt_api"
	return link, nil
}

func (s *Service) appendEvent(ctx context.Context, serverID, level, eventType, message string, result sshlayer.CommandResult) (serverevents.Event, error) {
	var exitCode *int
	if result.ExitCode >= 0 {
		value := result.ExitCode
		exitCode = &value
	}

	return s.events.Append(ctx, serverevents.CreateInput{
		ServerID:  serverID,
		Level:     level,
		EventType: eventType,
		Message:   message,
		Stdout:    result.Stdout,
		Stderr:    result.Stderr,
		ExitCode:  exitCode,
		CreatedAt: s.now(),
	})
}

func (r Request) toSSHRequest(server inventory.Server) sshlayer.TestRequest {
	return sshlayer.TestRequest{
		Host:           server.Host,
		SSHUser:        server.SSHUser,
		SSHPort:        server.SSHPort,
		AuthType:       r.AuthType,
		Password:       r.Password,
		PrivateKeyText: r.PrivateKeyText,
		PrivateKeyPath: r.PrivateKeyPath,
		Passphrase:     r.Passphrase,
	}
}

func sanitizeTail(tail int) int {
	if tail <= 0 {
		return defaultLogsTail
	}
	if tail > maxLogsTail {
		return maxLogsTail
	}
	return tail
}

func restartCommand(remoteBasePath string, current *telemtconfig.StoredConfig) string {
	composePath := path.Join(remoteBasePath, "docker-compose.yml")
	return strings.TrimSpace(fmt.Sprintf(`compose=%s
base=%s
if [ -f "$compose" ]; then
  cd "$base" && docker compose restart && exit 0
fi
%s
if [ -z "$container" ]; then
  printf 'Telemt container was not found on the host\n' >&2
  exit 1
fi
docker restart "$container"`, shellQuote(composePath), shellQuote(remoteBasePath), resolveTelemtContainerShell(current)))
}

func logsCommand(remoteBasePath string, current *telemtconfig.StoredConfig, tail int) string {
	tail = sanitizeTail(tail)
	composePath := path.Join(remoteBasePath, "docker-compose.yml")
	return strings.TrimSpace(fmt.Sprintf(`compose=%s
base=%s
if [ -f "$compose" ]; then
  cd "$base" && docker compose logs --tail=%d telemt && exit 0
fi
%s
if [ -z "$container" ]; then
  printf 'Telemt container was not found on the host\n' >&2
  exit 1
fi
docker logs --tail=%d "$container"`, shellQuote(composePath), shellQuote(remoteBasePath), tail, resolveTelemtContainerShell(current), tail))
}

func containerStatusCommand(current *telemtconfig.StoredConfig) string {
	return strings.TrimSpace(fmt.Sprintf(`%s
if [ -z "$container" ]; then
  exit 0
fi
docker ps -a --format "{{.Names}} {{.Status}}" | while read -r name rest; do
  if [ "$name" = "$container" ]; then
    printf '%%s\n' "$rest"
    break
  fi
done`, resolveTelemtContainerShell(current)))
}

func resolveTelemtContainerShell(current *telemtconfig.StoredConfig) string {
	parts := []string{
		`container=""`,
		fmt.Sprintf(`if docker inspect %s >/dev/null 2>&1; then container=%s; fi`, shellQuote(containerName), shellQuote(containerName)),
	}

	if current != nil && current.Fields.APIPort > 0 {
		port := current.Fields.APIPort
		parts = append(parts, fmt.Sprintf(`if [ -z "$container" ]; then container="$(docker ps -a --format "{{.Names}} {{.Ports}}" | while read -r name rest; do case "$rest" in *"127.0.0.1:%d->"*|*"0.0.0.0:%d->"*|*":%d->"* ) printf '%%s\n' "$name"; break ;; esac; done)"; fi`, port, port, port))
	}

	parts = append(parts, fmt.Sprintf(`if [ -z "$container" ]; then container="$(docker ps -a --format "{{.Names}} {{.Image}}" | while read -r name image; do case "$image" in %s* ) printf '%%s\n' "$name"; break ;; esac; done)"; fi`, telemtImagePrefix))

	return strings.Join(parts, "\n")
}

func telemtAPICommand(apiPort int) string {
	return fmt.Sprintf("curl -fsS --max-time 10 http://127.0.0.1:%d/v1/users", apiPort)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func buildContainerStatus(result sshlayer.CommandResult, err error) CommandStatus {
	if err != nil {
		return CommandStatus{
			Status:  "failed",
			Summary: "SSH command failed before container status was collected.",
			Error:   err.Error(),
		}
	}

	trimmed := strings.TrimSpace(result.Stdout)
	if !result.OK {
		return CommandStatus{
			Status:  "failed",
			Summary: firstNonEmpty(strings.TrimSpace(result.Stderr), "docker reported a container status error"),
			Result:  &result,
		}
	}
	if trimmed == "" {
		return CommandStatus{
			Status:  "missing",
			Summary: "Panel-managed Telemt container was not found on the host.",
			Result:  &result,
		}
	}

	return CommandStatus{
		Status:  "ok",
		Summary: trimmed,
		Result:  &result,
	}
}

func buildTelemtAPIStatus(result sshlayer.CommandResult, err error) TelemtAPIStatus {
	if err != nil {
		return TelemtAPIStatus{
			Status:  "failed",
			Summary: "SSH command failed before the Telemt API was queried.",
			Error:   err.Error(),
		}
	}

	if !result.OK {
		return TelemtAPIStatus{
			Status:  "failed",
			Summary: firstNonEmpty(strings.TrimSpace(result.Stderr), "Telemt API query failed"),
			Result:  &result,
		}
	}

	userCount, generatedLink := parseTelemtAPI(result.Stdout)
	summary := fmt.Sprintf("Telemt API reachable, %d user(s) returned.", userCount)
	if generatedLink == "" {
		summary = fmt.Sprintf("Telemt API reachable, %d user(s) returned, but no proxy link was found.", userCount)
	}

	return TelemtAPIStatus{
		Status:        "ok",
		Summary:       summary,
		Result:        &result,
		UserCount:     userCount,
		GeneratedLink: generatedLink,
	}
}

func (s *Service) checkPublicPort(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig) PublicPortStatus {
	port := server.MTProtoPort
	if current != nil && current.Fields.PublicPort > 0 {
		port = current.Fields.PublicPort
	}

	targetHost := firstNonEmpty(stringValue(server.PublicHost), stringValue(server.PublicIP), server.Host)
	if targetHost == "" || port <= 0 {
		return PublicPortStatus{
			Checked:    false,
			Reachable:  false,
			Summary:    "No public endpoint is available for a TCP reachability check.",
			PublicPort: port,
		}
	}

	target := net.JoinHostPort(targetHost, strconv.Itoa(port))
	checkCtx, cancel := context.WithTimeout(ctx, publicReachabilityTimeout)
	defer cancel()

	startedAt := time.Now()
	conn, err := s.dialContext(checkCtx, "tcp", target)
	if err != nil {
		return PublicPortStatus{
			Checked:    true,
			Reachable:  false,
			Target:     target,
			Error:      err.Error(),
			Summary:    "Public TCP endpoint is not reachable from the panel host.",
			PublicPort: port,
		}
	}
	_ = conn.Close()

	return PublicPortStatus{
		Checked:    true,
		Reachable:  true,
		Target:     target,
		LatencyMS:  time.Since(startedAt).Milliseconds(),
		Summary:    "Public TCP endpoint accepted a connection from the panel host.",
		PublicPort: port,
	}
}

func parseTelemtAPI(stdout string) (int, string) {
	var payload any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return 0, ""
	}

	return findUserCount(payload), findGeneratedLink(payload)
}

func findUserCount(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range []string{"users", "data"} {
			users, ok := typed[key]
			if !ok {
				continue
			}
			list, ok := users.([]any)
			if !ok {
				continue
			}
			return len(list)
		}
		return 0
	default:
		return 0
	}
}

func extractGeneratedLink(stdout string) string {
	var payload any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		return ""
	}
	return findGeneratedLink(payload)
}

func findGeneratedLink(value any) string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if strings.HasPrefix(trimmed, "https://t.me/proxy?") || strings.HasPrefix(trimmed, "tg://proxy?") {
			return trimmed
		}
	case []any:
		for _, item := range typed {
			if link := findGeneratedLink(item); link != "" {
				return link
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if link := findGeneratedLink(typed[key]); link != "" {
				return link
			}
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
