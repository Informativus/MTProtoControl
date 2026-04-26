package deployflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/serverevents"
	"mtproxy-control/apps/api/internal/sshlayer"
	"mtproxy-control/apps/api/internal/telemtconfig"
)

const (
	DockerImage                 = "ghcr.io/telemt/telemt:latest"
	ContainerName               = "telemt-mtproto"
	ComposeFileName             = "docker-compose.yml"
	ConfigFileName              = "config.toml"
	BackupsDirName              = "backups"
	PortDecisionStopExisting    = "stop_existing_service"
	PortDecisionUseSNIRouter    = "use_sni_router"
	PortDecisionChooseOtherPort = "choose_another_port"
	PortDecisionCancel          = "cancel"
)

const (
	diagnosticTimeout = 15 * time.Second
	uploadTimeout     = 30 * time.Second
	composeUpTimeout  = 90 * time.Second
	healthWaitTimeout = 90 * time.Second
	apiQueryTimeout   = 20 * time.Second
)

type Service struct {
	configs *telemtconfig.Repository
	events  *serverevents.Repository
	ssh     sshlayer.Executor
	now     func() time.Time
}

type Request struct {
	AuthType             string  `json:"auth_type"`
	Password             *string `json:"password"`
	PrivateKeyText       *string `json:"private_key_text"`
	PrivateKeyPath       *string `json:"private_key_path"`
	Passphrase           *string `json:"passphrase"`
	ConfirmBlockers      bool    `json:"confirm_blockers"`
	PortConflictDecision string  `json:"port_conflict_decision"`
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

type Preview struct {
	RemoteBasePath        string            `json:"remote_base_path"`
	Files                 []PlannedFile     `json:"files"`
	DockerImage           string            `json:"docker_image"`
	Ports                 []PortBinding     `json:"ports"`
	Commands              []string          `json:"commands"`
	Checks                []DiagnosticCheck `json:"checks"`
	Risks                 []Risk            `json:"risks"`
	RequiresConfirmation  bool              `json:"requires_confirmation"`
	RequiredDecision      *RequiredDecision `json:"required_decision,omitempty"`
	ExistingPanelInstance bool              `json:"existing_panel_instance"`
}

type PlannedFile struct {
	Path       string `json:"path"`
	SizeBytes  int    `json:"size_bytes"`
	Exists     bool   `json:"exists"`
	WillBackup bool   `json:"will_backup"`
}

type PortBinding struct {
	Label         string `json:"label"`
	HostAddress   string `json:"host_address"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
}

type DiagnosticCheck struct {
	Name    string                 `json:"name"`
	Status  string                 `json:"status"`
	Summary string                 `json:"summary"`
	Result  sshlayer.CommandResult `json:"result"`
}

type Risk struct {
	Code             string `json:"code"`
	Severity         string `json:"severity"`
	Message          string `json:"message"`
	Blocking         bool   `json:"blocking"`
	RequiresDecision bool   `json:"requires_decision"`
}

type RequiredDecision struct {
	Reason  string   `json:"reason"`
	Options []string `json:"options"`
}

type ApplyResult struct {
	GeneratedLink string               `json:"generated_link"`
	Events        []serverevents.Event `json:"events"`
	Preview       Preview              `json:"preview"`
}

type PreconditionError struct {
	Code    string
	Message string
	Preview *Preview
	Details map[string]string
}

func (e *PreconditionError) Error() string {
	return e.Message
}

type StepError struct {
	Code    string
	Step    string
	Message string
	Preview *Preview
	Events  []serverevents.Event
	Details map[string]string
}

func (e *StepError) Error() string {
	return e.Message
}

type previewRuntime struct {
	configPath               string
	composePath              string
	backupsPath              string
	composeText              string
	panelContainerPresent    bool
	publicPortListeners      []string
	existingFiles            map[string]bool
	dockerCheck              sshlayer.CommandResult
	composeCheck             sshlayer.CommandResult
	listenerCheck            sshlayer.CommandResult
	fileCheck                sshlayer.CommandResult
	panelContainerCheck      sshlayer.CommandResult
	preview                  Preview
	steps                    []string
	request                  sshlayer.TestRequest
	server                   inventory.Server
	current                  *telemtconfig.StoredConfig
	portConflictRiskDetected bool
}

func NewService(configs *telemtconfig.Repository, events *serverevents.Repository, ssh sshlayer.Executor) *Service {
	return &Service{
		configs: configs,
		events:  events,
		ssh:     ssh,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

func (s *Service) Preview(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, request Request) (Preview, error) {
	if err := request.validate(); err != nil {
		return Preview{}, err
	}
	if current == nil {
		return Preview{}, &PreconditionError{
			Code:    "config_required",
			Message: "generate or save a Telemt config before deploy",
		}
	}

	runtime, err := s.buildPreviewRuntime(ctx, server, current, request)
	if err != nil {
		return Preview{}, err
	}

	return runtime.preview, nil
}

func (s *Service) Apply(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, request Request) (ApplyResult, *telemtconfig.StoredConfig, error) {
	if err := request.validate(); err != nil {
		return ApplyResult{}, nil, err
	}
	if s.configs == nil || s.events == nil || s.ssh == nil {
		return ApplyResult{}, nil, &PreconditionError{
			Code:    "service_unavailable",
			Message: "deploy service is not configured",
		}
	}
	if current == nil {
		return ApplyResult{}, nil, &PreconditionError{
			Code:    "config_required",
			Message: "generate or save a Telemt config before deploy",
		}
	}

	runtime, err := s.buildPreviewRuntime(ctx, server, current, request)
	if err != nil {
		return ApplyResult{}, nil, err
	}

	if err := validateApplyDecision(runtime.preview, request); err != nil {
		if preconditionErr, ok := err.(*PreconditionError); ok && preconditionErr.Preview == nil {
			preconditionErr.Preview = &runtime.preview
		}
		return ApplyResult{}, nil, err
	}

	if hasBlockingRisk(runtime.preview) && !request.ConfirmBlockers {
		return ApplyResult{}, nil, &PreconditionError{
			Code:    "deploy_blocked",
			Message: "deploy blocked by diagnostics; confirm blockers to continue",
			Preview: &runtime.preview,
		}
	}

	events := make([]serverevents.Event, 0, 6)
	backupStamp := s.now().Format("20060102T150405Z")

	event, err := s.runStep(ctx, server.ID, runtime.preview, &events, runtime.request, "create_remote_directories", "Prepare remote base path", sshlayer.CommandRequest{
		Name:    "create_remote_directories",
		Command: fmt.Sprintf("mkdir -p %s %s", shellQuote(runtime.server.RemoteBasePath), shellQuote(runtime.backupsPath)),
		Timeout: diagnosticTimeout,
	})
	if err != nil {
		return ApplyResult{}, nil, err
	}
	_ = event

	_, err = s.runStep(ctx, server.ID, runtime.preview, &events, runtime.request, "backup_existing_files", "Backup existing panel files", sshlayer.CommandRequest{
		Name: "backup_existing_files",
		Command: strings.Join([]string{
			fmt.Sprintf("if [ -f %s ]; then cp %s %s; fi", shellQuote(runtime.configPath), shellQuote(runtime.configPath), shellQuote(path.Join(runtime.backupsPath, ConfigFileName+"."+backupStamp+".bak"))),
			fmt.Sprintf("if [ -f %s ]; then cp %s %s; fi", shellQuote(runtime.composePath), shellQuote(runtime.composePath), shellQuote(path.Join(runtime.backupsPath, ComposeFileName+"."+backupStamp+".bak"))),
		}, " && "),
		Timeout: diagnosticTimeout,
	})
	if err != nil {
		return ApplyResult{}, nil, err
	}

	if _, err := s.uploadStep(ctx, server.ID, runtime.preview, &events, runtime.request, "upload_config", "Upload config.toml", sshlayer.UploadRequest{
		Name:       "upload_config",
		RemotePath: runtime.configPath,
		Content:    []byte(runtime.current.ConfigText),
		Mode:       "600",
		Timeout:    uploadTimeout,
	}); err != nil {
		return ApplyResult{}, nil, err
	}

	if _, err := s.uploadStep(ctx, server.ID, runtime.preview, &events, runtime.request, "upload_compose", "Upload docker-compose.yml", sshlayer.UploadRequest{
		Name:       "upload_compose",
		RemotePath: runtime.composePath,
		Content:    []byte(runtime.composeText),
		Mode:       "644",
		Timeout:    uploadTimeout,
	}); err != nil {
		return ApplyResult{}, nil, err
	}

	if _, err := s.runStep(ctx, server.ID, runtime.preview, &events, runtime.request, "docker_compose_up", "Run docker compose up -d", sshlayer.CommandRequest{
		Name:    "docker_compose_up",
		Command: fmt.Sprintf("docker compose -f %s up -d", shellQuote(runtime.composePath)),
		Timeout: composeUpTimeout,
	}); err != nil {
		return ApplyResult{}, nil, err
	}

	if _, err := s.runStep(ctx, server.ID, runtime.preview, &events, runtime.request, "wait_container_health", "Wait for Telemt container health", sshlayer.CommandRequest{
		Name:    "wait_container_health",
		Command: waitForHealthCommand(),
		Timeout: healthWaitTimeout,
	}); err != nil {
		return ApplyResult{}, nil, err
	}

	apiResult, err := s.runStep(ctx, server.ID, runtime.preview, &events, runtime.request, "query_telemt_api", "Query Telemt API for generated link", sshlayer.CommandRequest{
		Name:    "query_telemt_api",
		Command: telemtAPICommand(runtime.current.Fields.APIPort),
		Timeout: apiQueryTimeout,
	})
	if err != nil {
		return ApplyResult{}, nil, err
	}

	generatedLink := extractGeneratedLink(apiResult.Stdout)
	if generatedLink == "" {
		return ApplyResult{}, nil, &StepError{
			Code:    "deploy_step_failed",
			Step:    "query_telemt_api",
			Message: "Telemt API response did not include a proxy link",
			Preview: &runtime.preview,
			Events:  events,
			Details: map[string]string{
				"step": "query_telemt_api",
			},
		}
	}

	updatedConfig, err := s.configs.MarkApplied(ctx, server.ID, generatedLink, s.now())
	if err != nil {
		return ApplyResult{}, nil, &StepError{
			Code:    "deploy_step_failed",
			Step:    "store_generated_link",
			Message: fmt.Sprintf("store deploy result: %v", err),
			Preview: &runtime.preview,
			Events:  events,
			Details: map[string]string{
				"step": "store_generated_link",
			},
		}
	}

	return ApplyResult{
		GeneratedLink: generatedLink,
		Events:        events,
		Preview:       runtime.preview,
	}, updatedConfig, nil
}

func (s *Service) buildPreviewRuntime(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, request Request) (*previewRuntime, error) {
	composePath := path.Join(server.RemoteBasePath, ComposeFileName)
	configPath := path.Join(server.RemoteBasePath, ConfigFileName)
	backupsPath := path.Join(server.RemoteBasePath, BackupsDirName)
	composeText := composeFile(current.Fields)
	sshRequest := request.toSSHRequest(server)

	dockerCheck, err := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
		Name:    "check_docker",
		Command: "command -v docker",
		Timeout: diagnosticTimeout,
	})
	if err != nil {
		return nil, err
	}

	composeCheck, err := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
		Name:    "check_docker_compose",
		Command: "docker compose version",
		Timeout: diagnosticTimeout,
	})
	if err != nil {
		return nil, err
	}

	listenerCheck, err := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
		Name:    "check_public_port",
		Command: "ss -ltnp",
		Timeout: diagnosticTimeout,
	})
	if err != nil {
		return nil, err
	}

	fileCheck, err := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
		Name: "check_remote_files",
		Command: strings.Join([]string{
			fmt.Sprintf("if [ -e %s ]; then printf 'config=present\\n'; else printf 'config=missing\\n'; fi", shellQuote(configPath)),
			fmt.Sprintf("if [ -e %s ]; then printf 'compose=present\\n'; else printf 'compose=missing\\n'; fi", shellQuote(composePath)),
			fmt.Sprintf("if [ -e %s ]; then printf 'backups=present\\n'; else printf 'backups=missing\\n'; fi", shellQuote(backupsPath)),
		}, " && "),
		Timeout: diagnosticTimeout,
	})
	if err != nil {
		return nil, err
	}

	panelContainerCheck, err := s.ssh.Run(ctx, sshRequest, sshlayer.CommandRequest{
		Name:    "check_panel_container",
		Command: fmt.Sprintf(`docker ps -a --filter name=^/%s$ --format "{{.Names}}\t{{.Status}}"`, ContainerName),
		Timeout: diagnosticTimeout,
	})
	if err != nil {
		return nil, err
	}

	existingFiles := parsePresenceMap(fileCheck.Stdout)
	publicPortListeners := extractPortListeners(listenerCheck.Stdout, current.Fields.PublicPort)
	panelContainerPresent := strings.TrimSpace(panelContainerCheck.Stdout) != ""

	preview := Preview{
		RemoteBasePath: server.RemoteBasePath,
		Files: []PlannedFile{
			{Path: composePath, SizeBytes: len(composeText), Exists: existingFiles["compose"], WillBackup: existingFiles["compose"]},
			{Path: configPath, SizeBytes: len(current.ConfigText), Exists: existingFiles["config"], WillBackup: existingFiles["config"]},
			{Path: backupsPath, Exists: existingFiles["backups"]},
		},
		DockerImage: DockerImage,
		Ports: []PortBinding{
			{Label: "MTProto public", HostAddress: "0.0.0.0", HostPort: current.Fields.PublicPort, ContainerPort: current.Fields.PublicPort},
			{Label: "Telemt API", HostAddress: "127.0.0.1", HostPort: current.Fields.APIPort, ContainerPort: current.Fields.APIPort},
		},
		Commands: []string{
			fmt.Sprintf("mkdir -p %s %s", server.RemoteBasePath, backupsPath),
			fmt.Sprintf("cp %s %s/<timestamp> and cp %s %s/<timestamp> when files exist", configPath, backupsPath, composePath, backupsPath),
			fmt.Sprintf("upload %s", configPath),
			fmt.Sprintf("upload %s", composePath),
			fmt.Sprintf("docker compose -f %s up -d", composePath),
			fmt.Sprintf("poll container %s health", ContainerName),
			fmt.Sprintf("curl http://127.0.0.1:%d/v1/users", current.Fields.APIPort),
		},
		Checks: []DiagnosticCheck{
			buildCheck("docker", dockerCheck, "Docker binary available"),
			buildCheck("docker_compose", composeCheck, "docker compose available"),
			buildCheck("public_port", listenerCheck, portCheckSummary(current.Fields.PublicPort, publicPortListeners, panelContainerPresent)),
			buildCheck("remote_files", fileCheck, fileCheckSummary(existingFiles)),
			buildCheck("panel_container", panelContainerCheck, panelContainerSummary(panelContainerPresent)),
		},
		ExistingPanelInstance: panelContainerPresent,
	}

	risks := make([]Risk, 0, 4)
	if !dockerCheck.OK {
		risks = append(risks, Risk{Code: "docker_missing", Severity: "critical", Message: "Docker is not available on the target host.", Blocking: true})
	}
	if !composeCheck.OK {
		risks = append(risks, Risk{Code: "docker_compose_missing", Severity: "critical", Message: "docker compose is not available on the target host.", Blocking: true})
	}
	if len(publicPortListeners) > 0 && !panelContainerPresent {
		preview.RequiresConfirmation = true
		preview.RequiredDecision = &RequiredDecision{
			Reason: fmt.Sprintf("public port %d is already in use by another service", current.Fields.PublicPort),
			Options: []string{
				PortDecisionStopExisting,
				PortDecisionUseSNIRouter,
				PortDecisionChooseOtherPort,
				PortDecisionCancel,
			},
		}
		risks = append(risks, Risk{
			Code:             "public_port_in_use",
			Severity:         "critical",
			Message:          fmt.Sprintf("Port %d already has listeners: %s", current.Fields.PublicPort, strings.Join(publicPortListeners, " | ")),
			Blocking:         true,
			RequiresDecision: true,
		})
	}
	if existingFiles["compose"] || existingFiles["config"] {
		risks = append(risks, Risk{Code: "existing_files_backup", Severity: "warning", Message: "Existing panel files will be backed up before replacement."})
	}
	preview.Risks = risks

	return &previewRuntime{
		configPath:            configPath,
		composePath:           composePath,
		backupsPath:           backupsPath,
		composeText:           composeText,
		panelContainerPresent: panelContainerPresent,
		publicPortListeners:   publicPortListeners,
		existingFiles:         existingFiles,
		dockerCheck:           dockerCheck,
		composeCheck:          composeCheck,
		listenerCheck:         listenerCheck,
		fileCheck:             fileCheck,
		panelContainerCheck:   panelContainerCheck,
		preview:               preview,
		request:               sshRequest,
		server:                server,
		current:               current,
	}, nil
}

func (s *Service) runStep(ctx context.Context, serverID string, preview Preview, events *[]serverevents.Event, request sshlayer.TestRequest, eventType, message string, command sshlayer.CommandRequest) (sshlayer.CommandResult, error) {
	result, err := s.ssh.Run(ctx, request, command)
	if err != nil {
		failure, appendErr := s.appendEvent(ctx, serverID, "error", eventType, message+": "+err.Error(), sshlayer.CommandResult{Name: command.Name, Command: command.Command})
		if appendErr == nil {
			*events = append(*events, failure)
		}
		return sshlayer.CommandResult{}, &StepError{
			Code:    "deploy_step_failed",
			Step:    eventType,
			Message: message + ": " + err.Error(),
			Preview: &preview,
			Events:  *events,
			Details: map[string]string{
				"step": eventType,
			},
		}
	}

	level := "info"
	if !result.OK {
		level = "error"
	}

	event, appendErr := s.appendEvent(ctx, serverID, level, eventType, message, result)
	if appendErr == nil {
		*events = append(*events, event)
	}

	if !result.OK {
		return result, &StepError{
			Code:    "deploy_step_failed",
			Step:    eventType,
			Message: message,
			Preview: &preview,
			Events:  *events,
			Details: map[string]string{
				"step":      eventType,
				"command":   result.Command,
				"exit_code": strconv.Itoa(result.ExitCode),
			},
		}
	}

	return result, nil
}

func (s *Service) uploadStep(ctx context.Context, serverID string, preview Preview, events *[]serverevents.Event, request sshlayer.TestRequest, eventType, message string, upload sshlayer.UploadRequest) (sshlayer.CommandResult, error) {
	result, err := s.ssh.Upload(ctx, request, upload)
	if err != nil {
		failure, appendErr := s.appendEvent(ctx, serverID, "error", eventType, message+": "+err.Error(), sshlayer.CommandResult{Name: upload.Name, Command: upload.RemotePath})
		if appendErr == nil {
			*events = append(*events, failure)
		}
		return sshlayer.CommandResult{}, &StepError{
			Code:    "deploy_step_failed",
			Step:    eventType,
			Message: message + ": " + err.Error(),
			Preview: &preview,
			Events:  *events,
			Details: map[string]string{
				"step": eventType,
			},
		}
	}

	event, appendErr := s.appendEvent(ctx, serverID, "info", eventType, message, result)
	if appendErr == nil {
		*events = append(*events, event)
	}

	if !result.OK {
		return result, &StepError{
			Code:    "deploy_step_failed",
			Step:    eventType,
			Message: message,
			Preview: &preview,
			Events:  *events,
			Details: map[string]string{
				"step":      eventType,
				"command":   result.Command,
				"exit_code": strconv.Itoa(result.ExitCode),
			},
		}
	}

	return result, nil
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

func buildCheck(name string, result sshlayer.CommandResult, summary string) DiagnosticCheck {
	status := "ok"
	if !result.OK {
		status = "failed"
	}
	return DiagnosticCheck{Name: name, Status: status, Summary: summary, Result: result}
}

func hasBlockingRisk(preview Preview) bool {
	for _, risk := range preview.Risks {
		if risk.Blocking {
			return true
		}
	}
	return false
}

func validateApplyDecision(preview Preview, request Request) error {
	for _, risk := range preview.Risks {
		if !risk.Blocking {
			continue
		}
		if !risk.RequiresDecision {
			return &PreconditionError{
				Code:    "deploy_blocked",
				Message: "deploy blocked by diagnostics",
			}
		}
	}

	if preview.RequiredDecision == nil {
		return nil
	}

	switch request.PortConflictDecision {
	case PortDecisionStopExisting:
		return nil
	case "":
		return &PreconditionError{
			Code:    "deploy_blocked",
			Message: "select how to handle the public port conflict before deploy",
		}
	case PortDecisionUseSNIRouter:
		return &PreconditionError{
			Code:    "deploy_blocked",
			Message: "set up the SNI router outside the panel, then rerun deploy preview",
		}
	case PortDecisionChooseOtherPort:
		return &PreconditionError{
			Code:    "deploy_blocked",
			Message: "update the Telemt config to another public port, then rerun deploy preview",
		}
	case PortDecisionCancel:
		return &PreconditionError{
			Code:    "deploy_cancelled",
			Message: "deploy cancelled by operator choice",
		}
	default:
		return &ValidationError{Fields: map[string]string{
			"port_conflict_decision": "must be stop_existing_service, use_sni_router, choose_another_port, or cancel",
		}}
	}
}

func (r Request) validate() error {
	validationErr := &ValidationError{}
	decision := strings.TrimSpace(r.PortConflictDecision)
	if decision != "" {
		switch decision {
		case PortDecisionStopExisting, PortDecisionUseSNIRouter, PortDecisionChooseOtherPort, PortDecisionCancel:
		default:
			validationErr.add("port_conflict_decision", "must be stop_existing_service, use_sni_router, choose_another_port, or cancel")
		}
	}
	if validationErr.empty() {
		return nil
	}
	return validationErr
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

func composeFile(fields telemtconfig.Fields) string {
	return strings.TrimSpace(fmt.Sprintf(`services:
  telemt:
    image: %s
    container_name: %s
    restart: unless-stopped
    working_dir: /etc/telemt
    ports:
      - "%d:%d"
      - "127.0.0.1:%d:%d"
    volumes:
      - ./config.toml:/etc/telemt/config.toml:ro
    tmpfs:
      - /etc/telemt:rw,mode=1777,size=4m
    healthcheck:
      test: ["CMD", "/app/telemt", "healthcheck", "/etc/telemt/config.toml", "--mode", "liveness"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE
    read_only: true
    security_opt:
      - no-new-privileges:true
    ulimits:
      nofile:
        soft: 65536
        hard: 262144
`, DockerImage, ContainerName, fields.PublicPort, fields.PublicPort, fields.APIPort, fields.APIPort)) + "\n"
}

func waitForHealthCommand() string {
	return strings.TrimSpace(fmt.Sprintf(`status=""
for attempt in $(seq 1 30); do
	status="$(docker inspect --format "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}" %s 2>/dev/null || true)"
	if [ "$status" = "healthy" ] || [ "$status" = "running" ]; then
	  printf 'status=%%s\n' "$status"
	  exit 0
  fi
  sleep 2
done
printf 'container %s not ready, last status=%%s\n' "$status" >&2
exit 1`, ContainerName, ContainerName))
}

func telemtAPICommand(apiPort int) string {
	return fmt.Sprintf("curl -fsS --max-time 10 http://127.0.0.1:%d/v1/users", apiPort)
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

func parsePresenceMap(stdout string) map[string]bool {
	result := map[string]bool{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(value) == "present"
	}
	return result
}

func extractPortListeners(stdout string, port int) []string {
	portMarker := ":" + strconv.Itoa(port)
	listeners := make([]string, 0)
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.Contains(trimmed, portMarker) {
			continue
		}
		listeners = append(listeners, trimmed)
	}
	return listeners
}

func portCheckSummary(port int, listeners []string, panelContainerPresent bool) string {
	if len(listeners) == 0 {
		return fmt.Sprintf("Port %d is free", port)
	}
	if panelContainerPresent {
		return fmt.Sprintf("Port %d already serves the existing panel-managed Telemt container", port)
	}
	return fmt.Sprintf("Port %d is occupied by another service", port)
}

func fileCheckSummary(existingFiles map[string]bool) string {
	parts := make([]string, 0, 3)
	for _, key := range []string{"config", "compose", "backups"} {
		status := "missing"
		if existingFiles[key] {
			status = "present"
		}
		parts = append(parts, key+"="+status)
	}
	return strings.Join(parts, ", ")
}

func panelContainerSummary(panelContainerPresent bool) string {
	if panelContainerPresent {
		return "Panel-managed Telemt container already exists on the host"
	}
	return "No existing panel-managed Telemt container detected"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
