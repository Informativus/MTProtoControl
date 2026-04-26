package healthchecks

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/serverevents"
	"mtproxy-control/apps/api/internal/sshcredentials"
	"mtproxy-control/apps/api/internal/sshlayer"
	"mtproxy-control/apps/api/internal/telegramalerts"
	"mtproxy-control/apps/api/internal/telemtconfig"
)

const (
	defaultInterval     = 30 * time.Second
	serverCheckTimeout  = 20 * time.Second
	dnsCheckTimeout     = 5 * time.Second
	tcpCheckTimeout     = 3 * time.Second
	remoteCheckTimeout  = 10 * time.Second
	healthContainerName = "telemt-mtproto"
	onlineStatus        = "online"
	degradedStatus      = "degraded"
	offlineStatus       = "offline"
	unknownStatus       = "unknown"
)

type Service struct {
	servers     *inventory.Repository
	configs     *telemtconfig.Repository
	credentials *sshcredentials.Repository
	health      *Repository
	events      *serverevents.Repository
	alerts      *telegramalerts.Service
	ssh         sshlayer.Executor
	now         func() time.Time
	lookupHost  func(context.Context, string) ([]string, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
}

type RunResult struct {
	Check      Check      `json:"check"`
	Transition Transition `json:"transition"`
}

func NewService(servers *inventory.Repository, configs *telemtconfig.Repository, credentials *sshcredentials.Repository, health *Repository, events *serverevents.Repository, alerts *telegramalerts.Service, ssh sshlayer.Executor) *Service {
	resolver := &net.Resolver{}
	dialer := &net.Dialer{}
	return &Service{
		servers:     servers,
		configs:     configs,
		credentials: credentials,
		health:      health,
		events:      events,
		alerts:      alerts,
		ssh:         ssh,
		now: func() time.Time {
			return time.Now().UTC()
		},
		lookupHost:  resolver.LookupHost,
		dialContext: dialer.DialContext,
	}
}

func DefaultInterval() time.Duration {
	return defaultInterval
}

func ParseInterval(raw string) (time.Duration, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultInterval, nil
	}

	interval, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse health check interval: %w", err)
	}
	if interval < 5*time.Second {
		return 0, fmt.Errorf("parse health check interval: value must be at least 5s")
	}

	return interval, nil
}

func (s *Service) RunCycle(ctx context.Context) error {
	if s == nil || s.servers == nil || s.health == nil {
		return fmt.Errorf("health checks service is not configured")
	}

	servers, err := s.servers.ListServers(ctx)
	if err != nil {
		return fmt.Errorf("list servers for health checks: %w", err)
	}

	var firstErr error
	for _, server := range servers {
		serverCtx, cancel := context.WithTimeout(ctx, serverCheckTimeout)
		_, err := s.RunServer(serverCtx, server)
		cancel()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("run health check for %s: %w", server.Name, err)
		}
	}

	return firstErr
}

func (s *Service) RunServer(ctx context.Context, server inventory.Server) (RunResult, error) {
	if s == nil || s.health == nil || s.servers == nil {
		return RunResult{}, fmt.Errorf("health checks service is not configured")
	}

	var current *telemtconfig.StoredConfig
	if s.configs != nil {
		loaded, err := s.configs.GetCurrent(ctx, server.ID)
		if err != nil {
			return RunResult{}, fmt.Errorf("load current config: %w", err)
		}
		current = loaded
	}

	issues := make([]string, 0, 6)
	check := Check{
		ServerID:  server.ID,
		Status:    unknownStatus,
		CreatedAt: s.now(),
	}

	dnsHost := publicHost(server)
	if s.runDNSCheck(ctx, server) {
		check.DNSOK = true
	} else {
		issues = append(issues, fmt.Sprintf("DNS lookup failed for %s", firstNonEmpty(dnsHost, "public_host")))
	}

	tcpOK, latencyMS, tcpMessage := s.runTCPCheck(ctx, server, current)
	check.TCPOK = tcpOK
	check.LatencyMS = latencyMS
	if !tcpOK {
		issues = append(issues, tcpMessage)
	}

	credential, err := s.loadCredential(ctx, server.ID)
	if err != nil {
		return RunResult{}, err
	}

	generatedLink := generatedLinkFromConfig(current)
	if generatedLink != "" {
		check.LinkOK = true
	}

	if credential == nil || s.ssh == nil {
		issues = append(issues, "worker SSH checks skipped because no saved private_key_path is available")
		if current == nil {
			issues = append(issues, "no saved Telemt config is available for API and link checks")
		}
	} else {
		sshRequest := sshlayer.TestRequest{
			Host:           server.Host,
			SSHUser:        server.SSHUser,
			SSHPort:        server.SSHPort,
			AuthType:       sshlayer.AuthTypePrivateKeyPath,
			PrivateKeyPath: credential.PrivateKeyPath,
		}

		containerResult, containerErr := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
			Name:    "health_container_status",
			Command: containerStatusCommand(),
			Timeout: remoteCheckTimeout,
		})
		if containerErr != nil {
			issues = append(issues, fmt.Sprintf("SSH check failed: %s", strings.TrimSpace(containerErr.Error())))
		} else {
			check.SSHOK = true
			check.DockerOK = containerHealthy(containerResult)
			if !check.DockerOK {
				issues = append(issues, firstNonEmpty(strings.TrimSpace(containerResult.Stderr), "Telemt container is not running or healthy"))
			}
		}

		if current == nil {
			issues = append(issues, "no saved Telemt config is available for API and link checks")
		} else {
			apiResult, apiErr := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
				Name:    "health_telemt_api",
				Command: telemtAPICommand(current.Fields.APIPort),
				Timeout: remoteCheckTimeout,
			})
			if apiErr != nil {
				issues = append(issues, fmt.Sprintf("Telemt API SSH check failed: %s", strings.TrimSpace(apiErr.Error())))
			} else if !apiResult.OK {
				issues = append(issues, firstNonEmpty(strings.TrimSpace(apiResult.Stderr), "Telemt API query failed"))
			} else {
				check.SSHOK = true
				check.TelemtAPIOK = true
				if liveLink := extractGeneratedLink(apiResult.Stdout); liveLink != "" {
					generatedLink = liveLink
					check.LinkOK = true
				}
			}
		}
	}

	if !check.LinkOK {
		issues = append(issues, "no generated proxy link is available")
	}

	check.Status = deriveStatus(check)
	check.Message = buildMessage(check.Status, issues)

	recorded, transition, err := s.health.Append(ctx, CreateInput{
		ServerID:    check.ServerID,
		Status:      check.Status,
		DNSOK:       check.DNSOK,
		TCPOK:       check.TCPOK,
		SSHOK:       check.SSHOK,
		TelemtAPIOK: check.TelemtAPIOK,
		DockerOK:    check.DockerOK,
		LinkOK:      check.LinkOK,
		LatencyMS:   check.LatencyMS,
		Message:     check.Message,
		CreatedAt:   check.CreatedAt,
	})
	if err != nil {
		return RunResult{}, err
	}

	if err := s.servers.UpdateHealthState(ctx, server.ID, recorded.Status, recorded.CreatedAt); err != nil {
		return RunResult{}, err
	}

	if err := s.maybeSendAlert(ctx, server, current, recorded, transition); err != nil {
		log.Printf("health alert for %s: %v", server.Name, err)
	}

	return RunResult{Check: recorded, Transition: transition}, nil
}

func (s *Service) maybeSendAlert(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, check Check, transition Transition) error {
	if s == nil || s.alerts == nil {
		return nil
	}

	settings, err := s.alerts.Load(ctx)
	if err != nil {
		return err
	}

	dispatch, ok, err := s.buildAlertDispatch(ctx, settings, server, current, check, transition)
	if err != nil || !ok {
		return err
	}

	if err := s.alerts.SendWithSettings(ctx, settings, dispatch.text, true); err != nil {
		return err
	}

	if s.events != nil {
		if _, err := s.events.Append(ctx, serverevents.CreateInput{
			ServerID:  server.ID,
			Level:     dispatch.level,
			EventType: dispatch.eventType,
			Message:   dispatch.summary,
			CreatedAt: check.CreatedAt,
		}); err != nil {
			return err
		}
	}

	return nil
}

type alertDispatch struct {
	eventType string
	level     string
	summary   string
	text      string
}

func (s *Service) buildAlertDispatch(ctx context.Context, settings telegramalerts.Settings, server inventory.Server, current *telemtconfig.StoredConfig, check Check, transition Transition) (alertDispatch, bool, error) {
	if !settings.AlertsEnabled || strings.TrimSpace(settings.TelegramBotToken) == "" || strings.TrimSpace(settings.TelegramChatID) == "" {
		return alertDispatch{}, false, nil
	}

	status := strings.ToLower(strings.TrimSpace(check.Status))
	if status == onlineStatus && transition.PreviousStatus != "" && transition.PreviousStatus != onlineStatus {
		outageStart, err := s.health.FindOutageStart(ctx, server.ID, check.CreatedAt)
		if err != nil {
			return alertDispatch{}, false, err
		}
		downtime := time.Duration(0)
		if outageStart != nil {
			downtime = check.CreatedAt.Sub(outageStart.CreatedAt).Round(time.Second)
			if downtime < 0 {
				downtime = 0
			}
		}
		text := buildRecoveryAlertMessage(server, current, check, transition.PreviousStatus, downtime, outageStart)
		return alertDispatch{
			eventType: telegramalerts.EventTypeAlertRecovered,
			level:     "info",
			summary:   fmt.Sprintf("Telegram recovery alert sent for %s", server.Name),
			text:      text,
		}, true, nil
	}

	if status != offlineStatus && status != degradedStatus {
		return alertDispatch{}, false, nil
	}

	lastOK, err := s.health.GetLatestByStatusBefore(ctx, server.ID, onlineStatus, check.CreatedAt)
	if err != nil {
		return alertDispatch{}, false, err
	}

	if transition.Changed || transition.PreviousStatus == "" {
		return downAlertDispatch(server, current, check, lastOK, false), true, nil
	}

	if settings.RepeatDownAfterMinutes <= 0 || s.events == nil {
		return alertDispatch{}, false, nil
	}

	latestAlert, err := s.events.GetLatestByEventTypes(ctx, server.ID, alertEventTypesForStatus(status)...)
	if err != nil {
		return alertDispatch{}, false, err
	}
	if latestAlert != nil && check.CreatedAt.Sub(latestAlert.CreatedAt) < time.Duration(settings.RepeatDownAfterMinutes)*time.Minute {
		return alertDispatch{}, false, nil
	}

	return downAlertDispatch(server, current, check, lastOK, latestAlert != nil), true, nil
}

func downAlertDispatch(server inventory.Server, current *telemtconfig.StoredConfig, check Check, lastOK *Check, repeated bool) alertDispatch {
	status := strings.ToLower(strings.TrimSpace(check.Status))
	eventType := telegramalerts.EventTypeAlertOffline
	level := "error"
	if status == degradedStatus {
		eventType = telegramalerts.EventTypeAlertDegraded
		level = "warning"
	}
	if repeated {
		if status == degradedStatus {
			eventType = telegramalerts.EventTypeAlertDegradedRepeated
		} else {
			eventType = telegramalerts.EventTypeAlertOfflineRepeated
		}
	}

	return alertDispatch{
		eventType: eventType,
		level:     level,
		summary:   fmt.Sprintf("Telegram %s alert sent for %s", status, server.Name),
		text:      buildDownAlertMessage(server, current, check, lastOK, repeated),
	}
}

func alertEventTypesForStatus(status string) []string {
	switch status {
	case degradedStatus:
		return []string{telegramalerts.EventTypeAlertDegraded, telegramalerts.EventTypeAlertDegradedRepeated}
	default:
		return []string{telegramalerts.EventTypeAlertOffline, telegramalerts.EventTypeAlertOfflineRepeated}
	}
}

func buildDownAlertMessage(server inventory.Server, current *telemtconfig.StoredConfig, check Check, lastOK *Check, repeated bool) string {
	label := strings.ToLower(strings.TrimSpace(check.Status))
	if label == "" {
		label = offlineStatus
	}
	lines := []string{
		fmt.Sprintf("MTProto alert: %s %s", server.Name, label),
		"Host: " + alertHost(server, current),
		"Cause: " + firstNonEmpty(strings.TrimSpace(check.Message), "No error message recorded."),
	}
	if lastOK != nil {
		lines = append(lines, "Last ok: "+formatAlertTime(lastOK.CreatedAt))
	}
	if repeated {
		lines = append(lines, "Repeat: server is still "+label)
	}
	return strings.Join(lines, "\n")
}

func buildRecoveryAlertMessage(server inventory.Server, current *telemtconfig.StoredConfig, check Check, previousStatus string, downtime time.Duration, outageStart *Check) string {
	lines := []string{
		fmt.Sprintf("MTProto alert: %s recovered", server.Name),
		"Host: " + alertHost(server, current),
	}
	if downtime > 0 {
		lines = append(lines, "Downtime: "+downtime.String())
	}
	if outageStart != nil {
		lines = append(lines, "Outage started: "+formatAlertTime(outageStart.CreatedAt))
	}
	lines = append(lines,
		"Previous state: "+firstNonEmpty(strings.TrimSpace(previousStatus), offlineStatus),
		"Current check: "+firstNonEmpty(strings.TrimSpace(check.Message), "All health checks passed."),
	)
	return strings.Join(lines, "\n")
}

func alertHost(server inventory.Server, current *telemtconfig.StoredConfig) string {
	host := publicHost(server)
	port := server.MTProtoPort
	if current != nil {
		host = firstNonEmpty(strings.TrimSpace(current.Fields.PublicHost), host)
		if current.Fields.PublicPort > 0 {
			port = current.Fields.PublicPort
		}
	}
	if host == "" {
		host = firstNonEmpty(stringValue(server.PublicIP), strings.TrimSpace(server.Host))
	}
	if port <= 0 {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func formatAlertTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05 MST")
}

func (s *Service) runDNSCheck(ctx context.Context, server inventory.Server) bool {
	host := publicHost(server)
	if host == "" || s.lookupHost == nil {
		return false
	}

	lookupCtx, cancel := context.WithTimeout(ctx, dnsCheckTimeout)
	defer cancel()

	addresses, err := s.lookupHost(lookupCtx, host)
	return err == nil && len(addresses) > 0
}

func (s *Service) runTCPCheck(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig) (bool, *int, string) {
	targetHost := firstNonEmpty(publicHost(server), stringValue(server.PublicIP), strings.TrimSpace(server.Host))
	port := server.MTProtoPort
	if current != nil && current.Fields.PublicPort > 0 {
		port = current.Fields.PublicPort
	}
	if targetHost == "" || port <= 0 || s.dialContext == nil {
		return false, nil, "public TCP endpoint is not configured"
	}

	checkCtx, cancel := context.WithTimeout(ctx, tcpCheckTimeout)
	defer cancel()

	target := net.JoinHostPort(targetHost, strconv.Itoa(port))
	startedAt := time.Now()
	conn, err := s.dialContext(checkCtx, "tcp", target)
	if err != nil {
		return false, nil, fmt.Sprintf("public TCP check failed for %s: %s", target, strings.TrimSpace(err.Error()))
	}
	_ = conn.Close()

	latency := int(time.Since(startedAt).Milliseconds())
	return true, &latency, ""
}

func (s *Service) loadCredential(ctx context.Context, serverID string) (*sshcredentials.Credential, error) {
	if s.credentials == nil {
		return nil, nil
	}

	credential, err := s.credentials.GetLatestForServer(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("load ssh credential: %w", err)
	}

	return credential, nil
}

func deriveStatus(check Check) string {
	if !check.TCPOK {
		return offlineStatus
	}
	if check.DNSOK && check.SSHOK && check.DockerOK && check.TelemtAPIOK && check.LinkOK {
		return onlineStatus
	}
	return degradedStatus
}

func buildMessage(status string, issues []string) string {
	filtered := make([]string, 0, len(issues))
	for _, issue := range issues {
		trimmed := strings.TrimSpace(issue)
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	if len(filtered) == 0 {
		return "All health checks passed."
	}
	if status == onlineStatus {
		return "All health checks passed."
	}
	return strings.Join(filtered, "; ")
}

func publicHost(server inventory.Server) string {
	return stringValue(server.PublicHost)
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func generatedLinkFromConfig(current *telemtconfig.StoredConfig) string {
	if current == nil {
		return ""
	}
	return strings.TrimSpace(current.GeneratedLink)
}

func containerStatusCommand() string {
	return fmt.Sprintf(`docker ps -a --filter name=^/%s$ --format "{{.Status}}"`, healthContainerName)
}

func telemtAPICommand(apiPort int) string {
	return fmt.Sprintf("curl -fsS --max-time 10 http://127.0.0.1:%d/v1/users", apiPort)
}

func containerHealthy(result sshlayer.CommandResult) bool {
	if !result.OK {
		return false
	}

	status := strings.ToLower(strings.TrimSpace(result.Stdout))
	if status == "" {
		return false
	}
	return strings.Contains(status, "healthy") || strings.Contains(status, "up") || strings.Contains(status, "running")
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
		for _, item := range typed {
			if link := findGeneratedLink(item); link != "" {
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
