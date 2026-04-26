package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
	"mtproxy-control/apps/api/internal/deployflow"
	"mtproxy-control/apps/api/internal/healthchecks"
	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/serverevents"
	"mtproxy-control/apps/api/internal/serverops"
	"mtproxy-control/apps/api/internal/serverrelationships"
	"mtproxy-control/apps/api/internal/sshcredentials"
	"mtproxy-control/apps/api/internal/sshlayer"
	"mtproxy-control/apps/api/internal/telegramalerts"
	"mtproxy-control/apps/api/internal/telemtconfig"
)

const (
	sshTestTimeout            = 60 * time.Second
	defaultLogStreamInterval  = 2 * time.Second
	defaultOperationsLogsTail = 300
	discoverConfigBeginMarker = "__MTPROXY_CONTROL_CONFIG_BEGIN__"
	discoverConfigEndMarker   = "__MTPROXY_CONTROL_CONFIG_END__"
	sshTestResultEventType    = "ssh_test_result"
)

type Server struct {
	startedAt             time.Time
	servers               *inventory.Repository
	relationships         *serverrelationships.Repository
	configs               *telemtconfig.Repository
	events                *serverevents.Repository
	healthChecks          *healthchecks.Repository
	credentials           *sshcredentials.Repository
	telegramAlerts        *telegramalerts.Service
	sshTester             sshlayer.Tester
	sshExecutor           sshlayer.Executor
	logStreamPollInterval time.Duration
	healthCheckInterval   time.Duration
}

type Option func(*Server)

type serverView struct {
	inventory.Server
	SavedPrivateKeyPath          *string    `json:"saved_private_key_path,omitempty"`
	SavedPrivateKeyPathUpdatedAt *time.Time `json:"saved_private_key_path_updated_at,omitempty"`
}

type serverPayload struct {
	Server serverView `json:"server"`
}

type serversPayload struct {
	Servers []serverView `json:"servers"`
}

type serverRelationshipsPayload struct {
	Relationships []serverRelationshipView `json:"relationships"`
}

type allServerRelationshipsPayload struct {
	Relationships []serverrelationships.Relation `json:"relationships"`
}

type serverRelationshipView struct {
	serverrelationships.Relation
	Direction    string `json:"direction"`
	PeerServerID string `json:"peer_server_id"`
}

type configStatePayload struct {
	Current     *telemtconfig.StoredConfig     `json:"current,omitempty"`
	Revisions   []telemtconfig.RevisionSummary `json:"revisions"`
	DraftFields telemtconfig.Fields            `json:"draft_fields"`
}

type errorPayload struct {
	Error apiError `json:"error"`
}

type deployPreviewPayload struct {
	Preview deployflow.Preview `json:"preview"`
}

type deployApplyPayload struct {
	Result deployflow.ApplyResult `json:"result"`
	Config configStatePayload     `json:"config"`
}

type restartPayload struct {
	Result serverops.RestartResult `json:"result"`
}

type logsPayload struct {
	Logs serverops.LogsResult `json:"logs"`
}

type statusPayload struct {
	Status serverops.StatusResult `json:"status"`
}

type healthHistoryPayload struct {
	Latest *healthchecks.Check  `json:"latest,omitempty"`
	Checks []healthchecks.Check `json:"checks"`
}

type latestSSHTestPayload struct {
	Result       *sshlayer.TestResult `json:"result,omitempty"`
	TestedAt     *time.Time           `json:"tested_at,omitempty"`
	ErrorMessage string               `json:"error_message,omitempty"`
}

type healthSettingsPayload struct {
	Interval        string `json:"interval"`
	IntervalSeconds int64  `json:"interval_seconds"`
}

type telegramSettingsPayload struct {
	Settings telegramalerts.PublicSettings `json:"settings"`
}

type telegramTestPayload struct {
	SentAt string `json:"sent_at"`
}

type linkPayload struct {
	Link serverops.LinkResult `json:"link"`
}

type discoverServerSettingsPayload struct {
	Discovery discoveredServerSettings `json:"discovery"`
}

type deployErrorPayload struct {
	Error   apiError             `json:"error"`
	Preview *deployflow.Preview  `json:"preview,omitempty"`
	Events  []serverevents.Event `json:"events,omitempty"`
}

type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
}

type createServerRequest struct {
	Name           string  `json:"name"`
	Host           string  `json:"host"`
	SSHPort        int     `json:"ssh_port"`
	SSHUser        string  `json:"ssh_user"`
	PrivateKeyPath *string `json:"private_key_path"`
	PublicHost     *string `json:"public_host"`
	PublicIP       *string `json:"public_ip"`
	MTProtoPort    int     `json:"mtproto_port"`
	SNIDomain      *string `json:"sni_domain"`
	RemoteBasePath string  `json:"remote_base_path"`
	Status         string  `json:"status"`
	LastCheckedAt  *string `json:"last_checked_at"`
}

type updateServerRequest struct {
	Name           *string `json:"name"`
	Host           *string `json:"host"`
	SSHPort        *int    `json:"ssh_port"`
	SSHUser        *string `json:"ssh_user"`
	PrivateKeyPath *string `json:"private_key_path"`
	PublicHost     *string `json:"public_host"`
	PublicIP       *string `json:"public_ip"`
	MTProtoPort    *int    `json:"mtproto_port"`
	SNIDomain      *string `json:"sni_domain"`
	RemoteBasePath *string `json:"remote_base_path"`
	Status         *string `json:"status"`
	LastCheckedAt  *string `json:"last_checked_at"`
}

type updateServerRelationshipsRequest struct {
	Relationships []updateServerRelationship `json:"relationships"`
}

type updateServerRelationship struct {
	Type           string `json:"type"`
	TargetServerID string `json:"target_server_id"`
}

type sshTestRequest struct {
	ServerID *string `json:"server_id"`
	sshlayer.TestRequest
}

type sshDiscoverRequest struct {
	ServerID           *string `json:"server_id"`
	RemoteBasePathHint *string `json:"remote_base_path_hint"`
	sshlayer.TestRequest
}

type discoveredServerSettings struct {
	RemoteBasePath string  `json:"remote_base_path"`
	ConfigPath     string  `json:"config_path"`
	ConfigText     string  `json:"config_text,omitempty"`
	PublicHost     *string `json:"public_host,omitempty"`
	MTProtoPort    *int    `json:"mtproto_port,omitempty"`
	Secret         *string `json:"secret,omitempty"`
	SNIDomain      *string `json:"sni_domain,omitempty"`
}

type generateConfigRequest struct {
	PublicHost     *string `json:"public_host"`
	PublicPort     *int    `json:"public_port"`
	TLSDomain      *string `json:"tls_domain"`
	Secret         *string `json:"secret"`
	MaskHost       *string `json:"mask_host"`
	MaskPort       *int    `json:"mask_port"`
	APIPort        *int    `json:"api_port"`
	UseMiddleProxy *bool   `json:"use_middle_proxy"`
	LogLevel       *string `json:"log_level"`
}

type updateCurrentConfigRequest struct {
	ConfigText    string `json:"config_text"`
	MarkAsApplied bool   `json:"mark_as_applied"`
}

type deployRequest struct {
	AuthType             string  `json:"auth_type"`
	Password             *string `json:"password"`
	PrivateKeyText       *string `json:"private_key_text"`
	PrivateKeyPath       *string `json:"private_key_path"`
	Passphrase           *string `json:"passphrase"`
	ConfirmBlockers      bool    `json:"confirm_blockers"`
	PortConflictDecision string  `json:"port_conflict_decision"`
}

type updateTelegramSettingsRequest struct {
	TelegramBotToken       *string `json:"telegram_bot_token"`
	TelegramChatID         *string `json:"telegram_chat_id"`
	AlertsEnabled          *bool   `json:"alerts_enabled"`
	RepeatDownAfterMinutes *int    `json:"repeat_down_after_minutes"`
}

func New(db *database.DB) *Server {
	var repo *inventory.Repository
	if db != nil {
		repo = inventory.NewRepository(db)
	}
	sshService := sshlayer.NewTester()

	server := &Server{
		startedAt:             time.Now().UTC(),
		servers:               repo,
		configs:               telemtconfig.NewRepository(db),
		events:                serverevents.NewRepository(db),
		healthChecks:          healthchecks.NewRepository(db),
		credentials:           sshcredentials.NewRepository(db),
		telegramAlerts:        telegramalerts.NewService(telegramalerts.NewRepository(db), nil),
		sshTester:             sshService,
		sshExecutor:           sshService,
		logStreamPollInterval: defaultLogStreamInterval,
		healthCheckInterval:   healthchecks.DefaultInterval(),
	}
	if db != nil {
		server.relationships = serverrelationships.NewRepository(db)
	}

	return server
}

func WithSSHTester(tester sshlayer.Tester) Option {
	return func(server *Server) {
		server.sshTester = tester
	}
}

func WithSSHExecutor(executor sshlayer.Executor) Option {
	return func(server *Server) {
		server.sshExecutor = executor
	}
}

func WithLogStreamPollInterval(interval time.Duration) Option {
	return func(server *Server) {
		if interval > 0 {
			server.logStreamPollInterval = interval
		}
	}
}

func WithHealthCheckInterval(interval time.Duration) Option {
	return func(server *Server) {
		if interval > 0 {
			server.healthCheckInterval = interval
		}
	}
}

func WithTelegramAlertsService(service *telegramalerts.Service) Option {
	return func(server *Server) {
		if service != nil {
			server.telegramAlerts = service
		}
	}
}

func NewWithOptions(db *database.DB, options ...Option) *Server {
	server := New(db)
	for _, option := range options {
		option(server)
	}
	return server
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/healthchecks/settings", s.getHealthcheckSettings)
	mux.HandleFunc("GET /api/settings/telegram", s.getTelegramSettings)
	mux.HandleFunc("PUT /api/settings/telegram", s.updateTelegramSettings)
	mux.HandleFunc("POST /api/settings/telegram/test", s.sendTelegramTestAlert)
	mux.HandleFunc("GET /api/server-relationships", s.listServerRelationships)
	mux.HandleFunc("GET /api/servers", s.listServers)
	mux.HandleFunc("POST /api/servers", s.createServer)
	mux.HandleFunc("POST /api/ssh/test", s.testSSH)
	mux.HandleFunc("POST /api/ssh/discover", s.discoverServerSettings)
	mux.HandleFunc("GET /api/servers/{id}", s.getServer)
	mux.HandleFunc("GET /api/servers/{id}/ssh-test/latest", s.getLatestSSHTest)
	mux.HandleFunc("PATCH /api/servers/{id}", s.updateServer)
	mux.HandleFunc("DELETE /api/servers/{id}", s.deleteServer)
	mux.HandleFunc("GET /api/servers/{id}/relationships", s.getServerRelationships)
	mux.HandleFunc("PUT /api/servers/{id}/relationships", s.replaceServerRelationships)
	mux.HandleFunc("GET /api/servers/{id}/health", s.getServerHealthHistory)
	mux.HandleFunc("GET /api/servers/{id}/configs/current", s.getCurrentConfig)
	mux.HandleFunc("PUT /api/servers/{id}/configs/current", s.updateCurrentConfig)
	mux.HandleFunc("POST /api/servers/{id}/configs/generate", s.generateConfig)
	mux.HandleFunc("POST /api/servers/{id}/deploy/preview", s.deployPreview)
	mux.HandleFunc("POST /api/servers/{id}/deploy/apply", s.deployApply)
	mux.HandleFunc("POST /api/servers/{id}/restart", s.restartServer)
	mux.HandleFunc("GET /api/servers/{id}/logs", s.getServerLogs)
	mux.HandleFunc("GET /api/servers/{id}/logs/stream", s.streamServerLogs)
	mux.HandleFunc("GET /api/servers/{id}/status", s.getServerStatus)
	mux.HandleFunc("GET /api/servers/{id}/link", s.getServerLink)

	return withLogging(withCORS(mux))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":     "ok",
		"service":    "mtproxy-control-api",
		"started_at": s.startedAt.Format(time.RFC3339),
	})
}

func (s *Server) getHealthcheckSettings(w http.ResponseWriter, _ *http.Request) {
	interval := s.healthCheckInterval
	if interval <= 0 {
		interval = healthchecks.DefaultInterval()
	}

	writeJSON(w, http.StatusOK, healthSettingsPayload{
		Interval:        interval.String(),
		IntervalSeconds: int64(interval / time.Second),
	})
}

func (s *Server) getTelegramSettings(w http.ResponseWriter, r *http.Request) {
	if s.telegramAlerts == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "telegram alerts service is not configured", nil)
		return
	}

	settings, err := s.telegramAlerts.PublicSettings(r.Context())
	if err != nil {
		writeTelegramAlertError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, telegramSettingsPayload{Settings: settings})
}

func (s *Server) updateTelegramSettings(w http.ResponseWriter, r *http.Request) {
	if s.telegramAlerts == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "telegram alerts service is not configured", nil)
		return
	}

	var request updateTelegramSettingsRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	settings, err := s.telegramAlerts.UpdateSettings(r.Context(), telegramalerts.UpdateInput{
		TelegramBotToken:       request.TelegramBotToken,
		TelegramChatID:         request.TelegramChatID,
		AlertsEnabled:          request.AlertsEnabled,
		RepeatDownAfterMinutes: request.RepeatDownAfterMinutes,
	})
	if err != nil {
		writeTelegramAlertError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, telegramSettingsPayload{Settings: settings})
}

func (s *Server) sendTelegramTestAlert(w http.ResponseWriter, r *http.Request) {
	if s.telegramAlerts == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "telegram alerts service is not configured", nil)
		return
	}

	if err := s.telegramAlerts.SendTestAlert(r.Context()); err != nil {
		writeTelegramAlertError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, telegramTestPayload{SentAt: time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) {
	if s.servers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server inventory is not configured", nil)
		return
	}

	servers, err := s.servers.ListServers(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	views, err := s.buildServerViews(r.Context(), servers)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, serversPayload{Servers: views})
}

func (s *Server) listServerRelationships(w http.ResponseWriter, r *http.Request) {
	if s.relationships == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server relationships are not configured", nil)
		return
	}

	relations, err := s.relationships.ListAll(r.Context())
	if err != nil {
		writeServerRelationshipError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, allServerRelationshipsPayload{Relationships: relations})
}

func (s *Server) createServer(w http.ResponseWriter, r *http.Request) {
	if s.servers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server inventory is not configured", nil)
		return
	}

	var request createServerRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	input, err := request.toCreateInput()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	server, err := s.servers.CreateServer(r.Context(), input)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	s.rememberPrivateKeyPath(r.Context(), server.ID, sshlayer.AuthTypePrivateKeyPath, request.PrivateKeyPath)

	view, err := s.buildServerView(r.Context(), server)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, serverPayload{Server: view})
}

func (s *Server) testSSH(w http.ResponseWriter, r *http.Request) {
	if s.sshTester == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "ssh tester is not configured", nil)
		return
	}

	var request sshTestRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), sshTestTimeout)
	defer cancel()

	result, err := s.sshTester.Test(ctx, request.TestRequest)
	if err != nil {
		if request.ServerID != nil {
			s.recordLatestSSHTest(r.Context(), strings.TrimSpace(*request.ServerID), nil, err)
		}
		writeSSHError(w, err)
		return
	}
	if request.ServerID != nil {
		serverID := strings.TrimSpace(*request.ServerID)
		s.rememberPrivateKeyPath(r.Context(), serverID, request.AuthType, request.PrivateKeyPath)
		s.recordLatestSSHTest(r.Context(), serverID, &result, nil)
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) discoverServerSettings(w http.ResponseWriter, r *http.Request) {
	if s.sshExecutor == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "ssh executor is not configured", nil)
		return
	}

	var request sshDiscoverRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), sshTestTimeout)
	defer cancel()

	result, err := s.sshExecutor.Run(ctx, request.TestRequest, sshlayer.CommandRequest{
		Name:    "discover_server_settings",
		Command: buildDiscoverServerSettingsCommand(request.RemoteBasePathHint),
		Timeout: 15 * time.Second,
	})
	if err != nil {
		writeSSHError(w, err)
		return
	}
	if !result.OK {
		details := map[string]string{}
		if strings.TrimSpace(result.Command) != "" {
			details["command"] = strings.TrimSpace(result.Command)
		}
		if strings.TrimSpace(result.Stderr) != "" {
			details["stderr"] = strings.TrimSpace(result.Stderr)
		}
		writeError(w, http.StatusConflict, "discover_failed", "failed to discover remote Telemt settings", details)
		return
	}

	discovery, err := parseDiscoveredServerSettings(result.Stdout)
	if err != nil {
		writeError(w, http.StatusConflict, "discover_failed", err.Error(), nil)
		return
	}
	if err := s.ensureDiscoveredConfigText(r.Context(), request.TestRequest, &discovery); err != nil {
		details := map[string]string{}
		if path := strings.TrimSpace(discovery.ConfigPath); path != "" {
			details["config_path"] = path
		}
		writeError(w, http.StatusConflict, "discover_failed", err.Error(), details)
		return
	}
	if request.ServerID != nil {
		s.rememberPrivateKeyPath(r.Context(), strings.TrimSpace(*request.ServerID), request.AuthType, request.PrivateKeyPath)
	}

	writeJSON(w, http.StatusOK, discoverServerSettingsPayload{Discovery: discovery})
}

func (s *Server) getServer(w http.ResponseWriter, r *http.Request) {
	if s.servers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server inventory is not configured", nil)
		return
	}

	server, err := s.servers.GetServer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return
	}

	view, err := s.buildServerView(r.Context(), server)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, serverPayload{Server: view})
}

func (s *Server) getLatestSSHTest(w http.ResponseWriter, r *http.Request) {
	server, ok := s.loadServer(w, r)
	if !ok {
		return
	}

	payload, err := s.loadLatestSSHTest(r.Context(), server.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) updateServer(w http.ResponseWriter, r *http.Request) {
	if s.servers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server inventory is not configured", nil)
		return
	}

	var request updateServerRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	input, err := request.toUpdateInput()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}

	server, err := s.servers.UpdateServer(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeRepositoryError(w, err)
		return
	}
	s.rememberPrivateKeyPath(r.Context(), server.ID, sshlayer.AuthTypePrivateKeyPath, request.PrivateKeyPath)

	view, err := s.buildServerView(r.Context(), server)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, serverPayload{Server: view})
}

func (s *Server) deleteServer(w http.ResponseWriter, r *http.Request) {
	if s.servers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server inventory is not configured", nil)
		return
	}

	if err := s.servers.DeleteServer(r.Context(), r.PathValue("id")); err != nil {
		writeRepositoryError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getServerRelationships(w http.ResponseWriter, r *http.Request) {
	if s.relationships == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server relationships are not configured", nil)
		return
	}

	server, ok := s.loadServer(w, r)
	if !ok {
		return
	}

	relations, err := s.relationships.ListByServer(r.Context(), server.ID)
	if err != nil {
		writeServerRelationshipError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, serverRelationshipsPayload{Relationships: buildServerRelationshipViews(server.ID, relations)})
}

func (s *Server) replaceServerRelationships(w http.ResponseWriter, r *http.Request) {
	if s.relationships == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server relationships are not configured", nil)
		return
	}

	server, ok := s.loadServer(w, r)
	if !ok {
		return
	}

	var request updateServerRelationshipsRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	inputs := make([]serverrelationships.ReplaceInput, 0, len(request.Relationships))
	for _, relationship := range request.Relationships {
		inputs = append(inputs, serverrelationships.ReplaceInput{
			Type:           relationship.Type,
			TargetServerID: relationship.TargetServerID,
		})
	}

	relations, err := s.relationships.ReplaceOutgoing(r.Context(), server.ID, inputs)
	if err != nil {
		writeServerRelationshipError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, serverRelationshipsPayload{Relationships: buildServerRelationshipViews(server.ID, relations)})
}

func (s *Server) getServerHealthHistory(w http.ResponseWriter, r *http.Request) {
	if s.healthChecks == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "health checks service is not configured", nil)
		return
	}

	server, ok := s.loadServer(w, r)
	if !ok {
		return
	}

	limit, err := parseHealthHistoryLimit(r)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", map[string]string{
			"limit": err.Error(),
		})
		return
	}

	checks, err := s.healthChecks.ListByServer(r.Context(), server.ID, limit)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	var latest *healthchecks.Check
	if len(checks) > 0 {
		latest = &checks[0]
	}

	writeJSON(w, http.StatusOK, healthHistoryPayload{Latest: latest, Checks: checks})
}

func (s *Server) getCurrentConfig(w http.ResponseWriter, r *http.Request) {
	server, ok := s.loadServerForConfig(w, r)
	if !ok {
		return
	}

	current, err := s.configs.GetCurrent(r.Context(), server.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	payload, err := s.buildConfigStatePayload(r.Context(), server, current)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) updateCurrentConfig(w http.ResponseWriter, r *http.Request) {
	server, ok := s.loadServerForConfig(w, r)
	if !ok {
		return
	}

	var request updateCurrentConfigRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	var (
		current *telemtconfig.StoredConfig
		err     error
	)

	configText := strings.TrimSpace(request.ConfigText)
	switch {
	case configText != "":
		current, err = s.configs.SaveRevision(r.Context(), server.ID, request.ConfigText)
		if err != nil {
			writeConfigError(w, err)
			return
		}
	case request.MarkAsApplied:
		current, err = s.configs.GetCurrent(r.Context(), server.ID)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		if current == nil {
			writeError(w, http.StatusConflict, "config_required", "generate or save a Telemt config before marking it as applied", nil)
			return
		}
	default:
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", map[string]string{
			"config_text": "is required unless mark_as_applied is true",
		})
		return
	}

	if request.MarkAsApplied {
		current, err = s.configs.MarkApplied(r.Context(), server.ID, current.GeneratedLink, time.Now().UTC())
		if err != nil {
			if errors.Is(err, telemtconfig.ErrConfigNotFound) {
				writeError(w, http.StatusConflict, "config_required", "generate or save a Telemt config before marking it as applied", nil)
				return
			}
			writeInternalError(w, err)
			return
		}
	}

	payload, err := s.buildConfigStatePayload(r.Context(), server, current)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) generateConfig(w http.ResponseWriter, r *http.Request) {
	server, ok := s.loadServerForConfig(w, r)
	if !ok {
		return
	}

	var request generateConfigRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	fields, validationErr := telemtconfig.ApplyDefaults(telemtconfig.DefaultFields(server), request.toPartialFields())
	if validationErr != nil {
		writeConfigError(w, validationErr)
		return
	}

	configText, _, err := telemtconfig.Generate(fields)
	if err != nil {
		writeConfigError(w, err)
		return
	}

	current, err := s.configs.SaveRevision(r.Context(), server.ID, configText)
	if err != nil {
		writeConfigError(w, err)
		return
	}

	payload, err := s.buildConfigStatePayload(r.Context(), server, current)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) deployPreview(w http.ResponseWriter, r *http.Request) {
	server, current, ok := s.loadServerAndCurrentConfig(w, r)
	if !ok {
		return
	}

	var request deployRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	preview, err := deployflow.NewService(s.configs, s.events, s.sshExecutor).Preview(r.Context(), server, current, request.toServiceRequest())
	if err != nil {
		writeDeployError(w, err)
		return
	}

	s.rememberPrivateKeyPath(r.Context(), server.ID, request.AuthType, request.PrivateKeyPath)

	writeJSON(w, http.StatusOK, deployPreviewPayload{Preview: preview})
}

func (s *Server) deployApply(w http.ResponseWriter, r *http.Request) {
	server, current, ok := s.loadServerAndCurrentConfig(w, r)
	if !ok {
		return
	}

	var request deployRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	result, updatedCurrent, err := deployflow.NewService(s.configs, s.events, s.sshExecutor).Apply(r.Context(), server, current, request.toServiceRequest())
	if err != nil {
		s.maybeSendDeployFailureAlert(r.Context(), server, current, err)
		writeDeployError(w, err)
		return
	}

	s.rememberPrivateKeyPath(r.Context(), server.ID, request.AuthType, request.PrivateKeyPath)

	configPayload, buildErr := s.buildConfigStatePayload(r.Context(), server, updatedCurrent)
	if buildErr != nil {
		writeInternalError(w, buildErr)
		return
	}

	writeJSON(w, http.StatusOK, deployApplyPayload{Result: result, Config: configPayload})
}

func (s *Server) restartServer(w http.ResponseWriter, r *http.Request) {
	server, current, ok := s.loadServerAndCurrentConfig(w, r)
	if !ok {
		return
	}

	var request deployRequest
	if !decodeJSON(w, r, &request) {
		return
	}

	result, err := serverops.NewService(s.events, s.healthChecks, s.sshExecutor).Restart(r.Context(), server, current, request.toOperationsRequest())
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	s.rememberPrivateKeyPath(r.Context(), server.ID, request.AuthType, request.PrivateKeyPath)

	writeJSON(w, http.StatusOK, restartPayload{Result: result})
}

func (s *Server) getServerLogs(w http.ResponseWriter, r *http.Request) {
	server, current, ok := s.loadServerAndCurrentConfig(w, r)
	if !ok {
		return
	}

	request, err := parseOperationsQuery(r, true)
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	tail, err := parseLogsTail(r)
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	result, err := serverops.NewService(s.events, s.healthChecks, s.sshExecutor).Logs(r.Context(), server, current, *request, tail)
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	s.rememberPrivateKeyPath(r.Context(), server.ID, request.AuthType, request.PrivateKeyPath)

	writeJSON(w, http.StatusOK, logsPayload{Logs: result})
}

func (s *Server) streamServerLogs(w http.ResponseWriter, r *http.Request) {
	server, current, ok := s.loadServerAndCurrentConfig(w, r)
	if !ok {
		return
	}

	request, err := parseOperationsQuery(r, true)
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	tail, err := parseLogsTail(r)
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unsupported", "response streaming is not supported", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	service := serverops.NewService(s.events, s.healthChecks, s.sshExecutor)
	lastStdout := ""
	sendSnapshot := func() bool {
		result, err := service.Logs(r.Context(), server, current, *request, tail)
		if err != nil {
			_ = writeSSE(w, flusher, "stream-error", map[string]any{"error": operationErrorForStream(err)})
			return false
		}
		if result.Result.Stdout != lastStdout {
			lastStdout = result.Result.Stdout
			if writeErr := writeSSE(w, flusher, "logs", result); writeErr != nil {
				return false
			}
			return true
		}
		return writeSSE(w, flusher, "heartbeat", map[string]string{"timestamp": time.Now().UTC().Format(time.RFC3339Nano)}) == nil
	}

	if !sendSnapshot() {
		return
	}

	ticker := time.NewTicker(s.logStreamPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !sendSnapshot() {
				return
			}
		}
	}
}

func (s *Server) getServerStatus(w http.ResponseWriter, r *http.Request) {
	server, current, ok := s.loadServerAndCurrentConfig(w, r)
	if !ok {
		return
	}

	request, err := parseOperationsQuery(r, false)
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	result, err := serverops.NewService(s.events, s.healthChecks, s.sshExecutor).Status(r.Context(), server, current, request)
	if err != nil {
		writeOperationsError(w, err)
		return
	}
	if request != nil {
		s.rememberPrivateKeyPath(r.Context(), server.ID, request.AuthType, request.PrivateKeyPath)
	}

	writeJSON(w, http.StatusOK, statusPayload{Status: result})
}

func (s *Server) getServerLink(w http.ResponseWriter, r *http.Request) {
	server, current, ok := s.loadServerAndCurrentConfig(w, r)
	if !ok {
		return
	}

	request, err := parseOperationsQuery(r, false)
	if err != nil {
		writeOperationsError(w, err)
		return
	}

	result, err := serverops.NewService(s.events, s.healthChecks, s.sshExecutor).Link(r.Context(), server, current, request)
	if err != nil {
		writeOperationsError(w, err)
		return
	}
	if request != nil {
		s.rememberPrivateKeyPath(r.Context(), server.ID, request.AuthType, request.PrivateKeyPath)
	}

	writeJSON(w, http.StatusOK, linkPayload{Link: result})
}

func (r createServerRequest) toCreateInput() (inventory.CreateInput, error) {
	lastCheckedAt, err := parseOptionalTime(r.LastCheckedAt)
	if err != nil {
		return inventory.CreateInput{}, err
	}

	return inventory.CreateInput{
		Name:           r.Name,
		Host:           r.Host,
		SSHPort:        r.SSHPort,
		SSHUser:        r.SSHUser,
		PublicHost:     r.PublicHost,
		PublicIP:       r.PublicIP,
		MTProtoPort:    r.MTProtoPort,
		SNIDomain:      r.SNIDomain,
		RemoteBasePath: r.RemoteBasePath,
		Status:         r.Status,
		LastCheckedAt:  lastCheckedAt,
	}, nil
}

func (r updateServerRequest) toUpdateInput() (inventory.UpdateInput, error) {
	lastCheckedAt, err := parseOptionalTime(r.LastCheckedAt)
	if err != nil {
		return inventory.UpdateInput{}, err
	}

	return inventory.UpdateInput{
		Name:           r.Name,
		Host:           r.Host,
		SSHPort:        r.SSHPort,
		SSHUser:        r.SSHUser,
		PublicHost:     r.PublicHost,
		PublicIP:       r.PublicIP,
		MTProtoPort:    r.MTProtoPort,
		SNIDomain:      r.SNIDomain,
		RemoteBasePath: r.RemoteBasePath,
		Status:         r.Status,
		LastCheckedAt:  lastCheckedAt,
	}, nil
}

func parseOptionalTime(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return nil, errors.New("last_checked_at must be RFC3339")
	}

	utc := parsed.UTC()
	return &utc, nil
}

func (s *Server) loadServer(w http.ResponseWriter, r *http.Request) (inventory.Server, bool) {
	if s.servers == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "server inventory is not configured", nil)
		return inventory.Server{}, false
	}

	server, err := s.servers.GetServer(r.Context(), r.PathValue("id"))
	if err != nil {
		writeRepositoryError(w, err)
		return inventory.Server{}, false
	}

	return server, true
}

func (s *Server) loadServerForConfig(w http.ResponseWriter, r *http.Request) (inventory.Server, bool) {
	if s.configs == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "config service is not configured", nil)
		return inventory.Server{}, false
	}

	return s.loadServer(w, r)
}

func (s *Server) buildConfigStatePayload(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig) (configStatePayload, error) {
	revisions, err := s.configs.ListRevisions(ctx, server.ID)
	if err != nil {
		return configStatePayload{}, err
	}

	draftFields := telemtconfig.DefaultFields(server)
	if current != nil {
		draftFields = current.Fields
	}

	return configStatePayload{
		Current:     current,
		Revisions:   revisions,
		DraftFields: draftFields,
	}, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	defer r.Body.Close()

	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", describeDecodeError(err), nil)
		return false
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must contain a single JSON object", nil)
		return false
	}

	return true
}

func describeDecodeError(err error) string {
	switch {
	case errors.Is(err, io.EOF):
		return "request body is required"
	default:
		return err.Error()
	}
}

func (r generateConfigRequest) toPartialFields() telemtconfig.PartialFields {
	return telemtconfig.PartialFields{
		PublicHost:     r.PublicHost,
		PublicPort:     r.PublicPort,
		TLSDomain:      r.TLSDomain,
		Secret:         r.Secret,
		MaskHost:       r.MaskHost,
		MaskPort:       r.MaskPort,
		APIPort:        r.APIPort,
		UseMiddleProxy: r.UseMiddleProxy,
		LogLevel:       r.LogLevel,
	}
}

func (r deployRequest) toServiceRequest() deployflow.Request {
	return deployflow.Request{
		AuthType:             r.AuthType,
		Password:             r.Password,
		PrivateKeyText:       r.PrivateKeyText,
		PrivateKeyPath:       r.PrivateKeyPath,
		Passphrase:           r.Passphrase,
		ConfirmBlockers:      r.ConfirmBlockers,
		PortConflictDecision: r.PortConflictDecision,
	}
}

func (r deployRequest) toOperationsRequest() serverops.Request {
	return serverops.Request{
		AuthType:       r.AuthType,
		Password:       r.Password,
		PrivateKeyText: r.PrivateKeyText,
		PrivateKeyPath: r.PrivateKeyPath,
		Passphrase:     r.Passphrase,
	}
}

func parseOperationsQuery(r *http.Request, required bool) (*serverops.Request, error) {
	authType := strings.TrimSpace(r.URL.Query().Get("auth_type"))
	privateKeyPath := strings.TrimSpace(r.URL.Query().Get("private_key_path"))
	passphrase := strings.TrimSpace(r.URL.Query().Get("passphrase"))

	hasAuth := authType != "" || privateKeyPath != "" || passphrase != ""
	if !hasAuth {
		if required {
			return nil, &serverops.ValidationError{Fields: map[string]string{
				"private_key_path": "is required for this endpoint",
			}}
		}
		return nil, nil
	}

	if authType == "" {
		authType = sshlayer.AuthTypePrivateKeyPath
	}
	if authType != sshlayer.AuthTypePrivateKeyPath {
		return nil, &serverops.ValidationError{Fields: map[string]string{
			"auth_type": "GET endpoints support only private_key_path auth",
		}}
	}
	if privateKeyPath == "" {
		return nil, &serverops.ValidationError{Fields: map[string]string{
			"private_key_path": "is required when auth_type=private_key_path",
		}}
	}

	request := &serverops.Request{
		AuthType:       authType,
		PrivateKeyPath: &privateKeyPath,
	}
	if passphrase != "" {
		request.Passphrase = &passphrase
	}

	return request, nil
}

func parseLogsTail(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("tail"))
	if raw == "" {
		return defaultOperationsLogsTail, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, &serverops.ValidationError{Fields: map[string]string{
			"tail": "must be a positive integer",
		}}
	}

	return value, nil
}

func parseHealthHistoryLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 12, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > 100 {
		return 0, fmt.Errorf("must be a positive integer up to 100")
	}

	return value, nil
}

func buildDiscoverServerSettingsCommand(remoteBasePathHint *string) string {
	hint := ""
	if remoteBasePathHint != nil {
		hint = strings.TrimSpace(*remoteBasePathHint)
	}

	return fmt.Sprintf(`set -eu; hint=%s; probe_base() { base=$1; [ -n "$base" ] || return 1; config="$base/config.toml"; [ -f "$config" ] || return 1; printf "remote_base_path=%%s\n" "$base"; printf "config_path=%%s\n" "$config"; printf "%s\n"; cat "$config"; printf "\n%s\n"; return 0; }; probe_base "$hint" && exit 0; probe_base "/srv/telemt" && exit 0; if [ -n "${HOME:-}" ]; then probe_base "${HOME}/telemt" && exit 0; fi; probe_base "/opt/mtproto-panel/telemt" && exit 0; exit 1`, shellQuote(hint), discoverConfigBeginMarker, discoverConfigEndMarker)
}

func parseDiscoveredServerSettings(stdout string) (discoveredServerSettings, error) {
	var discovery discoveredServerSettings
	headers, configText := splitDiscoveredServerSettingsOutput(stdout)
	for _, line := range strings.Split(headers, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "remote_base_path":
			discovery.RemoteBasePath = value
		case "config_path":
			discovery.ConfigPath = value
		case "public_host":
			if value != "" {
				copy := value
				discovery.PublicHost = &copy
			}
		case "mtproto_port":
			if value == "" {
				continue
			}
			port, err := strconv.Atoi(value)
			if err != nil {
				return discoveredServerSettings{}, fmt.Errorf("parse discovered mtproto_port: %w", err)
			}
			discovery.MTProtoPort = &port
		case "secret":
			if value != "" {
				copy := value
				discovery.Secret = &copy
			}
		case "sni_domain":
			if value != "" {
				copy := value
				discovery.SNIDomain = &copy
			}
		}
	}

	if strings.TrimSpace(configText) != "" {
		if err := applyDiscoveredConfigText(&discovery, configText); err != nil {
			return discoveredServerSettings{}, err
		}
	}

	if strings.TrimSpace(discovery.RemoteBasePath) == "" || strings.TrimSpace(discovery.ConfigPath) == "" {
		return discoveredServerSettings{}, errors.New("remote Telemt config was not discovered")
	}

	return discovery, nil
}

func applyDiscoveredConfigText(discovery *discoveredServerSettings, configText string) error {
	if discovery == nil || strings.TrimSpace(configText) == "" {
		return nil
	}

	discovery.ConfigText = configText
	fields, err := telemtconfig.ParsePartial(configText)
	if err != nil {
		return fmt.Errorf("parse discovered Telemt config: %w", err)
	}

	if fields.PublicHost != nil && strings.TrimSpace(*fields.PublicHost) != "" {
		copy := strings.TrimSpace(*fields.PublicHost)
		discovery.PublicHost = &copy
	}
	if fields.PublicPort != nil && *fields.PublicPort > 0 {
		port := *fields.PublicPort
		discovery.MTProtoPort = &port
	}
	if fields.Secret != nil && strings.TrimSpace(*fields.Secret) != "" {
		copy := strings.TrimSpace(*fields.Secret)
		discovery.Secret = &copy
	}
	if fields.TLSDomain != nil && strings.TrimSpace(*fields.TLSDomain) != "" {
		copy := strings.TrimSpace(*fields.TLSDomain)
		discovery.SNIDomain = &copy
	}

	return nil
}

func (s *Server) ensureDiscoveredConfigText(ctx context.Context, request sshlayer.TestRequest, discovery *discoveredServerSettings) error {
	if s == nil || s.sshExecutor == nil || discovery == nil {
		return nil
	}
	if strings.TrimSpace(discovery.ConfigText) != "" {
		return nil
	}
	configPath := strings.TrimSpace(discovery.ConfigPath)
	if configPath == "" {
		return nil
	}

	result, err := s.sshExecutor.Run(ctx, request, sshlayer.CommandRequest{
		Name:    "read_discovered_config",
		Command: fmt.Sprintf("cat %s", shellQuote(configPath)),
		Timeout: 15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("read remote Telemt config at %s: %w", configPath, err)
	}
	if !result.OK {
		message := firstNonEmptyText(strings.TrimSpace(result.Stderr), strings.TrimSpace(result.Stdout), "read failed")
		return fmt.Errorf("read remote Telemt config at %s: %s", configPath, message)
	}
	if strings.TrimSpace(result.Stdout) == "" {
		return fmt.Errorf("remote Telemt config at %s is empty", configPath)
	}

	if err := applyDiscoveredConfigText(discovery, result.Stdout); err != nil {
		return err
	}
	return nil
}

func splitDiscoveredServerSettingsOutput(stdout string) (string, string) {
	start := strings.Index(stdout, discoverConfigBeginMarker)
	if start < 0 {
		return stdout, ""
	}

	headers := stdout[:start]
	rest := stdout[start+len(discoverConfigBeginMarker):]
	rest = strings.TrimPrefix(rest, "\n")

	end := strings.Index(rest, discoverConfigEndMarker)
	if end < 0 {
		return headers, strings.TrimSpace(rest)
	}

	configText := strings.TrimSuffix(rest[:end], "\n")
	return headers, configText
}

func formatTelemtValidationError(validationErr *telemtconfig.ValidationError) string {
	if validationErr == nil || len(validationErr.Fields) == 0 {
		return "validation failed"
	}

	keys := make([]string, 0, len(validationErr.Fields))
	for key := range validationErr.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s %s", key, validationErr.Fields[key]))
	}

	return strings.Join(parts, ", ")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *Server) rememberPrivateKeyPath(ctx context.Context, serverID, authType string, privateKeyPath *string) {
	if s.credentials == nil || strings.TrimSpace(authType) != sshlayer.AuthTypePrivateKeyPath || privateKeyPath == nil {
		return
	}
	_ = s.credentials.RememberPrivateKeyPath(ctx, serverID, *privateKeyPath)
}

func (s *Server) recordLatestSSHTest(ctx context.Context, serverID string, result *sshlayer.TestResult, testErr error) {
	if s.events == nil || strings.TrimSpace(serverID) == "" {
		return
	}

	input := serverevents.CreateInput{
		ServerID:  strings.TrimSpace(serverID),
		EventType: sshTestResultEventType,
		CreatedAt: time.Now().UTC(),
	}

	if testErr != nil {
		input.Level = "error"
		input.Message = strings.TrimSpace(testErr.Error())
	} else if result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			log.Printf("marshal ssh test snapshot for %s: %v", serverID, err)
			return
		}
		input.Level = "info"
		input.Message = "SSH connectivity verified"
		input.Stdout = string(encoded)
	} else {
		return
	}

	if _, err := s.events.Append(ctx, input); err != nil {
		log.Printf("append ssh test snapshot for %s: %v", serverID, err)
	}
}

func (s *Server) loadLatestSSHTest(ctx context.Context, serverID string) (latestSSHTestPayload, error) {
	if s.events == nil {
		return latestSSHTestPayload{}, nil
	}

	event, err := s.events.GetLatestByEventTypes(ctx, serverID, sshTestResultEventType)
	if err != nil {
		return latestSSHTestPayload{}, fmt.Errorf("load latest ssh test event: %w", err)
	}
	if event == nil {
		return latestSSHTestPayload{}, nil
	}

	payload := latestSSHTestPayload{
		TestedAt: &event.CreatedAt,
	}
	if strings.TrimSpace(event.Stdout) == "" {
		payload.ErrorMessage = strings.TrimSpace(event.Message)
		return payload, nil
	}

	var result sshlayer.TestResult
	if err := json.Unmarshal([]byte(event.Stdout), &result); err != nil {
		return latestSSHTestPayload{}, fmt.Errorf("decode latest ssh test event: %w", err)
	}
	payload.Result = &result
	return payload, nil
}

func (s *Server) buildServerViews(ctx context.Context, servers []inventory.Server) ([]serverView, error) {
	views := make([]serverView, 0, len(servers))
	for _, server := range servers {
		view, err := s.buildServerView(ctx, server)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Server) buildServerView(ctx context.Context, server inventory.Server) (serverView, error) {
	view := serverView{Server: server}
	if s.credentials == nil {
		return view, nil
	}

	credential, err := s.credentials.GetLatestForServer(ctx, server.ID)
	if err != nil {
		return serverView{}, fmt.Errorf("load saved ssh credential: %w", err)
	}
	if credential == nil {
		return view, nil
	}

	view.SavedPrivateKeyPath = credential.PrivateKeyPath
	view.SavedPrivateKeyPathUpdatedAt = &credential.UpdatedAt
	return view, nil
}

func buildServerRelationshipViews(serverID string, relations []serverrelationships.Relation) []serverRelationshipView {
	views := make([]serverRelationshipView, 0, len(relations))
	for _, relation := range relations {
		view := serverRelationshipView{
			Relation:     relation,
			Direction:    "outgoing",
			PeerServerID: relation.TargetServerID,
		}
		if relation.SourceServerID != serverID {
			view.Direction = "incoming"
			view.PeerServerID = relation.SourceServerID
		}
		views = append(views, view)
	}
	return views
}

func (s *Server) maybeSendDeployFailureAlert(ctx context.Context, server inventory.Server, current *telemtconfig.StoredConfig, err error) {
	if s == nil || s.telegramAlerts == nil || !shouldSendDeployFailureAlert(err) {
		return
	}

	settings, loadErr := s.telegramAlerts.Load(ctx)
	if loadErr != nil {
		log.Printf("load telegram settings for deploy alert: %v", loadErr)
		return
	}

	message := buildDeployFailureAlertMessage(server, current, err)
	if sendErr := s.telegramAlerts.SendWithSettings(ctx, settings, message, true); sendErr != nil {
		log.Printf("send deploy failure alert for %s: %v", server.Name, sendErr)
		return
	}

	if s.events != nil {
		if _, eventErr := s.events.Append(ctx, serverevents.CreateInput{
			ServerID:  server.ID,
			Level:     "error",
			EventType: telegramalerts.EventTypeAlertDeployFailed,
			Message:   fmt.Sprintf("Telegram deploy failure alert sent for %s", server.Name),
			CreatedAt: time.Now().UTC(),
		}); eventErr != nil {
			log.Printf("store deploy failure alert event for %s: %v", server.Name, eventErr)
		}
	}
}

func shouldSendDeployFailureAlert(err error) bool {
	var validationErr *deployflow.ValidationError
	var preconditionErr *deployflow.PreconditionError
	var sshValidationErr *sshlayer.ValidationError
	return !errors.As(err, &validationErr) && !errors.As(err, &preconditionErr) && !errors.As(err, &sshValidationErr)
}

func buildDeployFailureAlertMessage(server inventory.Server, current *telemtconfig.StoredConfig, err error) string {
	host := strings.TrimSpace(server.Host)
	if server.PublicHost != nil && strings.TrimSpace(*server.PublicHost) != "" {
		host = strings.TrimSpace(*server.PublicHost)
	}
	port := server.MTProtoPort
	if current != nil {
		if strings.TrimSpace(current.Fields.PublicHost) != "" {
			host = strings.TrimSpace(current.Fields.PublicHost)
		}
		if current.Fields.PublicPort > 0 {
			port = current.Fields.PublicPort
		}
	}

	target := host
	if target != "" && port > 0 {
		target = net.JoinHostPort(target, strconv.Itoa(port))
	}

	return strings.Join([]string{
		fmt.Sprintf("MTProto alert: %s deploy failed", server.Name),
		"Host: " + target,
		"Cause: " + strings.TrimSpace(err.Error()),
	}, "\n")
}

func writeRepositoryError(w http.ResponseWriter, err error) {
	var validationErr *inventory.ValidationError
	switch {
	case errors.As(err, &validationErr):
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", validationErr.Fields)
	case errors.Is(err, inventory.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "server not found", nil)
	default:
		writeInternalError(w, err)
	}
}

func writeServerRelationshipError(w http.ResponseWriter, err error) {
	var validationErr *serverrelationships.ValidationError
	switch {
	case errors.As(err, &validationErr):
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", validationErr.Fields)
	case errors.Is(err, serverrelationships.ErrServerNotFound):
		writeError(w, http.StatusNotFound, "not_found", "server not found", nil)
	default:
		writeInternalError(w, err)
	}
}

func writeTelegramAlertError(w http.ResponseWriter, err error) {
	var validationErr *telegramalerts.ValidationError
	var sendErr *telegramalerts.SendError

	switch {
	case errors.As(err, &validationErr):
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", validationErr.Fields)
	case errors.As(err, &sendErr):
		writeError(w, http.StatusBadGateway, "telegram_send_failed", sendErr.Message, nil)
	default:
		writeInternalError(w, err)
	}
}

func writeSSHError(w http.ResponseWriter, err error) {
	var validationErr *sshlayer.ValidationError
	var operationErr *sshlayer.OperationError
	switch {
	case errors.As(err, &validationErr):
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", validationErr.Fields)
	case errors.As(err, &operationErr):
		switch operationErr.Kind {
		case sshlayer.ErrorKindTimeout:
			writeError(w, http.StatusGatewayTimeout, "ssh_timeout", operationErr.Message, nil)
		case sshlayer.ErrorKindAuth:
			writeError(w, http.StatusBadGateway, "ssh_auth_failed", operationErr.Message, nil)
		case sshlayer.ErrorKindHostKey:
			writeError(w, http.StatusBadGateway, "ssh_host_key_failed", operationErr.Message, nil)
		default:
			writeError(w, http.StatusBadGateway, "ssh_connect_failed", operationErr.Message, nil)
		}
	default:
		writeInternalError(w, err)
	}
}

func writeConfigError(w http.ResponseWriter, err error) {
	var validationErr *telemtconfig.ValidationError
	switch {
	case errors.As(err, &validationErr):
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", validationErr.Fields)
	default:
		writeInternalError(w, err)
	}
}

func writeDeployError(w http.ResponseWriter, err error) {
	var validationErr *deployflow.ValidationError
	var preconditionErr *deployflow.PreconditionError
	var stepErr *deployflow.StepError
	var sshValidationErr *sshlayer.ValidationError
	var sshOperationErr *sshlayer.OperationError

	switch {
	case errors.As(err, &validationErr):
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", validationErr.Fields)
	case errors.As(err, &preconditionErr):
		writeJSON(w, http.StatusConflict, deployErrorPayload{
			Error:   apiError{Code: preconditionErr.Code, Message: preconditionErr.Message, Details: preconditionErr.Details},
			Preview: preconditionErr.Preview,
		})
	case errors.As(err, &stepErr):
		writeJSON(w, http.StatusBadGateway, deployErrorPayload{
			Error:   apiError{Code: stepErr.Code, Message: stepErr.Message, Details: stepErr.Details},
			Preview: stepErr.Preview,
			Events:  stepErr.Events,
		})
	case errors.As(err, &sshValidationErr), errors.As(err, &sshOperationErr):
		writeSSHError(w, err)
	default:
		writeInternalError(w, err)
	}
}

func writeOperationsError(w http.ResponseWriter, err error) {
	var validationErr *serverops.ValidationError
	var preconditionErr *serverops.PreconditionError
	var stepErr *serverops.StepError
	var sshValidationErr *sshlayer.ValidationError
	var sshOperationErr *sshlayer.OperationError

	switch {
	case errors.As(err, &validationErr):
		writeError(w, http.StatusUnprocessableEntity, "validation_error", "validation failed", validationErr.Fields)
	case errors.As(err, &preconditionErr):
		writeError(w, http.StatusConflict, preconditionErr.Code, preconditionErr.Message, nil)
	case errors.As(err, &stepErr):
		details := copyStringMap(stepErr.Details)
		if details == nil {
			details = map[string]string{}
		}
		details["step"] = stepErr.Step
		if stepErr.Result != nil {
			if stepErr.Result.Command != "" {
				details["command"] = stepErr.Result.Command
			}
			if stepErr.Result.ExitCode >= 0 {
				details["exit_code"] = strconv.Itoa(stepErr.Result.ExitCode)
			}
			if strings.TrimSpace(stepErr.Result.Stderr) != "" {
				details["stderr"] = strings.TrimSpace(stepErr.Result.Stderr)
			}
		}
		writeError(w, http.StatusBadGateway, stepErr.Code, stepErr.Message, details)
	case errors.As(err, &sshValidationErr), errors.As(err, &sshOperationErr):
		writeSSHError(w, err)
	default:
		writeInternalError(w, err)
	}
}

func operationErrorForStream(err error) apiError {
	var validationErr *serverops.ValidationError
	var preconditionErr *serverops.PreconditionError
	var stepErr *serverops.StepError
	var sshValidationErr *sshlayer.ValidationError
	var sshOperationErr *sshlayer.OperationError

	switch {
	case errors.As(err, &validationErr):
		return apiError{Code: "validation_error", Message: "validation failed", Details: validationErr.Fields}
	case errors.As(err, &preconditionErr):
		return apiError{Code: preconditionErr.Code, Message: preconditionErr.Message}
	case errors.As(err, &stepErr):
		details := copyStringMap(stepErr.Details)
		if details == nil {
			details = map[string]string{}
		}
		details["step"] = stepErr.Step
		if stepErr.Result != nil && strings.TrimSpace(stepErr.Result.Stderr) != "" {
			details["stderr"] = strings.TrimSpace(stepErr.Result.Stderr)
		}
		return apiError{Code: stepErr.Code, Message: stepErr.Message, Details: details}
	case errors.As(err, &sshValidationErr):
		return apiError{Code: "validation_error", Message: "validation failed", Details: sshValidationErr.Fields}
	case errors.As(err, &sshOperationErr):
		switch sshOperationErr.Kind {
		case sshlayer.ErrorKindTimeout:
			return apiError{Code: "ssh_timeout", Message: sshOperationErr.Message}
		case sshlayer.ErrorKindAuth:
			return apiError{Code: "ssh_auth_failed", Message: sshOperationErr.Message}
		case sshlayer.ErrorKindHostKey:
			return apiError{Code: "ssh_host_key_failed", Message: sshOperationErr.Message}
		default:
			return apiError{Code: "ssh_connect_failed", Message: sshOperationErr.Message}
		}
	default:
		return apiError{Code: "internal_error", Message: err.Error()}
	}
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}

	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (s *Server) loadServerAndCurrentConfig(w http.ResponseWriter, r *http.Request) (inventory.Server, *telemtconfig.StoredConfig, bool) {
	server, ok := s.loadServerForConfig(w, r)
	if !ok {
		return inventory.Server{}, nil, false
	}

	current, err := s.configs.GetCurrent(r.Context(), server.ID)
	if err != nil {
		writeInternalError(w, err)
		return inventory.Server{}, nil, false
	}

	return server, current, true
}

func writeInternalError(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", nil)
}

func writeError(w http.ResponseWriter, status int, code, message string, details map[string]string) {
	writeJSON(w, status, errorPayload{
		Error: apiError{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		writer := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(writer, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, writer.status, time.Since(startedAt).Truncate(time.Millisecond))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
