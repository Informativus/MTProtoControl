package inventory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
)

const DefaultRemoteBasePath = "/opt/mtproto-panel/telemt"

var ErrNotFound = errors.New("server not found")

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

type Repository struct {
	db *database.DB
}

type Server struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Host           string     `json:"host"`
	SSHPort        int        `json:"ssh_port"`
	SSHUser        string     `json:"ssh_user"`
	PublicHost     *string    `json:"public_host,omitempty"`
	PublicIP       *string    `json:"public_ip,omitempty"`
	MTProtoPort    int        `json:"mtproto_port"`
	SNIDomain      *string    `json:"sni_domain,omitempty"`
	RemoteBasePath string     `json:"remote_base_path"`
	Status         string     `json:"status"`
	LastCheckedAt  *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type CreateInput struct {
	Name           string
	Host           string
	SSHPort        int
	SSHUser        string
	PublicHost     *string
	PublicIP       *string
	MTProtoPort    int
	SNIDomain      *string
	RemoteBasePath string
	Status         string
	LastCheckedAt  *time.Time
}

type UpdateInput struct {
	Name           *string
	Host           *string
	SSHPort        *int
	SSHUser        *string
	PublicHost     *string
	PublicIP       *string
	MTProtoPort    *int
	SNIDomain      *string
	RemoteBasePath *string
	Status         *string
	LastCheckedAt  *time.Time
}

type serverRow struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Host           string  `json:"host"`
	SSHPort        int     `json:"ssh_port"`
	SSHUser        string  `json:"ssh_user"`
	PublicHost     *string `json:"public_host"`
	PublicIP       *string `json:"public_ip"`
	MTProtoPort    int     `json:"mtproto_port"`
	SNIDomain      *string `json:"sni_domain"`
	RemoteBasePath string  `json:"remote_base_path"`
	Status         string  `json:"status"`
	LastCheckedAt  *string `json:"last_checked_at"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListServers(ctx context.Context) ([]Server, error) {
	var rows []serverRow
	if err := r.db.Query(ctx, `
		SELECT id, name, host, ssh_port, ssh_user, public_host, public_ip,
		       mtproto_port, sni_domain, remote_base_path, status, last_checked_at,
		       created_at, updated_at
		FROM servers
		ORDER BY created_at ASC, id ASC;
	`, &rows); err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}

	return mapRows(rows)
}

func (r *Repository) CreateServer(ctx context.Context, input CreateInput) (Server, error) {
	normalized, validationErr := normalizeCreate(input)
	if validationErr != nil {
		return Server{}, validationErr
	}

	now := nowUTC()
	server := Server{
		ID:             mustID(),
		Name:           normalized.Name,
		Host:           normalized.Host,
		SSHPort:        normalized.SSHPort,
		SSHUser:        normalized.SSHUser,
		PublicHost:     normalized.PublicHost,
		PublicIP:       normalized.PublicIP,
		MTProtoPort:    normalized.MTProtoPort,
		SNIDomain:      normalized.SNIDomain,
		RemoteBasePath: normalized.RemoteBasePath,
		Status:         normalized.Status,
		LastCheckedAt:  normalized.LastCheckedAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@id", server.ID),
		setParam("@name", server.Name),
		setParam("@host", server.Host),
		setParam("@ssh_port", strconv.Itoa(server.SSHPort)),
		setParam("@ssh_user", server.SSHUser),
		setNullableString("@public_host", server.PublicHost),
		setNullableString("@public_ip", server.PublicIP),
		setParam("@mtproto_port", strconv.Itoa(server.MTProtoPort)),
		setNullableString("@sni_domain", server.SNIDomain),
		setParam("@remote_base_path", server.RemoteBasePath),
		setParam("@status", server.Status),
		setNullableTime("@last_checked_at", server.LastCheckedAt),
		setParam("@created_at", server.CreatedAt.Format(time.RFC3339Nano)),
		setParam("@updated_at", server.UpdatedAt.Format(time.RFC3339Nano)),
		`INSERT INTO servers (
			id, name, host, ssh_port, ssh_user, public_host, public_ip,
			mtproto_port, sni_domain, remote_base_path, status, last_checked_at,
			created_at, updated_at
		) VALUES (
			@id, @name, @host, @ssh_port, @ssh_user, @public_host, @public_ip,
			@mtproto_port, @sni_domain, @remote_base_path, @status, @last_checked_at,
			@created_at, @updated_at
		);`,
	)); err != nil {
		return Server{}, fmt.Errorf("create server: %w", err)
	}

	return server, nil
}

func (r *Repository) GetServer(ctx context.Context, id string) (Server, error) {
	var rows []serverRow
	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@id", strings.TrimSpace(id)),
		`SELECT id, name, host, ssh_port, ssh_user, public_host, public_ip,
		        mtproto_port, sni_domain, remote_base_path, status, last_checked_at,
		        created_at, updated_at
		 FROM servers
		 WHERE id = @id;`,
	), &rows); err != nil {
		return Server{}, fmt.Errorf("get server: %w", err)
	}

	if len(rows) == 0 {
		return Server{}, ErrNotFound
	}

	servers, err := mapRows(rows[:1])
	if err != nil {
		return Server{}, err
	}

	return servers[0], nil
}

func (r *Repository) UpdateServer(ctx context.Context, id string, input UpdateInput) (Server, error) {
	current, err := r.GetServer(ctx, id)
	if err != nil {
		return Server{}, err
	}

	next := current
	applyUpdate(&next, input)
	validationErr := validateServer(next)
	if validationErr != nil {
		return Server{}, validationErr
	}
	next.UpdatedAt = nowUTC()

	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@id", next.ID),
		setParam("@name", next.Name),
		setParam("@host", next.Host),
		setParam("@ssh_port", strconv.Itoa(next.SSHPort)),
		setParam("@ssh_user", next.SSHUser),
		setNullableString("@public_host", next.PublicHost),
		setNullableString("@public_ip", next.PublicIP),
		setParam("@mtproto_port", strconv.Itoa(next.MTProtoPort)),
		setNullableString("@sni_domain", next.SNIDomain),
		setParam("@remote_base_path", next.RemoteBasePath),
		setParam("@status", next.Status),
		setNullableTime("@last_checked_at", next.LastCheckedAt),
		setParam("@updated_at", next.UpdatedAt.Format(time.RFC3339Nano)),
		`UPDATE servers
		 SET name = @name,
		     host = @host,
		     ssh_port = @ssh_port,
		     ssh_user = @ssh_user,
		     public_host = @public_host,
		     public_ip = @public_ip,
		     mtproto_port = @mtproto_port,
		     sni_domain = @sni_domain,
		     remote_base_path = @remote_base_path,
		     status = @status,
		     last_checked_at = @last_checked_at,
		     updated_at = @updated_at
		 WHERE id = @id;`,
	)); err != nil {
		return Server{}, fmt.Errorf("update server: %w", err)
	}

	return next, nil
}

func (r *Repository) DeleteServer(ctx context.Context, id string) error {
	var rows []struct {
		Affected int `json:"affected"`
	}

	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@id", strings.TrimSpace(id)),
		`DELETE FROM servers WHERE id = @id;
		 SELECT changes() AS affected;`,
	), &rows); err != nil {
		return fmt.Errorf("delete server: %w", err)
	}

	if len(rows) == 0 || rows[0].Affected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *Repository) UpdateHealthState(ctx context.Context, id, status string, checkedAt time.Time) error {
	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@id", strings.TrimSpace(id)),
		setParam("@status", firstNonEmptyStatus(status)),
		setParam("@last_checked_at", checkedAt.UTC().Format(time.RFC3339Nano)),
		setParam("@updated_at", nowUTC().Format(time.RFC3339Nano)),
		`UPDATE servers
		 SET status = @status,
		     last_checked_at = @last_checked_at,
		     updated_at = @updated_at
		 WHERE id = @id;`,
	)); err != nil {
		return fmt.Errorf("update server health state: %w", err)
	}

	return nil
}

func mapRows(rows []serverRow) ([]Server, error) {
	servers := make([]Server, 0, len(rows))
	for _, row := range rows {
		server, err := row.toServer()
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

func firstNonEmptyStatus(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func (r serverRow) toServer() (Server, error) {
	createdAt, err := parseTime(r.CreatedAt)
	if err != nil {
		return Server{}, fmt.Errorf("parse created_at: %w", err)
	}

	updatedAt, err := parseTime(r.UpdatedAt)
	if err != nil {
		return Server{}, fmt.Errorf("parse updated_at: %w", err)
	}

	var lastCheckedAt *time.Time
	if r.LastCheckedAt != nil {
		parsed, err := parseTime(*r.LastCheckedAt)
		if err != nil {
			return Server{}, fmt.Errorf("parse last_checked_at: %w", err)
		}
		lastCheckedAt = &parsed
	}

	return Server{
		ID:             r.ID,
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
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}

func applyUpdate(server *Server, input UpdateInput) {
	if input.Name != nil {
		server.Name = strings.TrimSpace(*input.Name)
	}
	if input.Host != nil {
		server.Host = strings.TrimSpace(*input.Host)
	}
	if input.SSHPort != nil {
		server.SSHPort = *input.SSHPort
	}
	if input.SSHUser != nil {
		server.SSHUser = strings.TrimSpace(*input.SSHUser)
	}
	if input.PublicHost != nil {
		server.PublicHost = optionalString(*input.PublicHost)
	}
	if input.PublicIP != nil {
		server.PublicIP = optionalString(*input.PublicIP)
	}
	if input.MTProtoPort != nil {
		server.MTProtoPort = *input.MTProtoPort
	}
	if input.SNIDomain != nil {
		server.SNIDomain = optionalString(*input.SNIDomain)
	}
	if input.RemoteBasePath != nil {
		server.RemoteBasePath = strings.TrimSpace(*input.RemoteBasePath)
	}
	if input.Status != nil {
		server.Status = strings.TrimSpace(*input.Status)
	}
	if input.LastCheckedAt != nil {
		value := input.LastCheckedAt.UTC()
		server.LastCheckedAt = &value
	}
}

func normalizeCreate(input CreateInput) (CreateInput, *ValidationError) {
	normalized := input
	normalized.Name = strings.TrimSpace(normalized.Name)
	normalized.Host = strings.TrimSpace(normalized.Host)
	normalized.SSHUser = strings.TrimSpace(normalized.SSHUser)
	normalized.RemoteBasePath = strings.TrimSpace(normalized.RemoteBasePath)
	normalized.Status = strings.TrimSpace(normalized.Status)

	if normalized.SSHPort == 0 {
		normalized.SSHPort = 22
	}
	if normalized.MTProtoPort == 0 {
		normalized.MTProtoPort = 443
	}
	if normalized.RemoteBasePath == "" {
		normalized.RemoteBasePath = DefaultRemoteBasePath
	}
	if normalized.Status == "" {
		normalized.Status = "unknown"
	}
	if normalized.PublicHost != nil {
		normalized.PublicHost = optionalString(*normalized.PublicHost)
	}
	if normalized.PublicIP != nil {
		normalized.PublicIP = optionalString(*normalized.PublicIP)
	}
	if normalized.SNIDomain != nil {
		normalized.SNIDomain = optionalString(*normalized.SNIDomain)
	}
	if normalized.LastCheckedAt != nil {
		value := normalized.LastCheckedAt.UTC()
		normalized.LastCheckedAt = &value
	}

	server := Server{
		Name:           normalized.Name,
		Host:           normalized.Host,
		SSHPort:        normalized.SSHPort,
		SSHUser:        normalized.SSHUser,
		PublicHost:     normalized.PublicHost,
		PublicIP:       normalized.PublicIP,
		MTProtoPort:    normalized.MTProtoPort,
		SNIDomain:      normalized.SNIDomain,
		RemoteBasePath: normalized.RemoteBasePath,
		Status:         normalized.Status,
		LastCheckedAt:  normalized.LastCheckedAt,
	}

	return normalized, validateServer(server)
}

func validateServer(server Server) *ValidationError {
	validationErr := &ValidationError{}

	if server.Name == "" {
		validationErr.add("name", "is required")
	}
	if server.Host == "" {
		validationErr.add("host", "is required")
	}
	if server.SSHUser == "" {
		validationErr.add("ssh_user", "is required")
	}
	if server.RemoteBasePath == "" {
		validationErr.add("remote_base_path", "is required")
	}
	if server.SSHPort < 1 || server.SSHPort > 65535 {
		validationErr.add("ssh_port", "must be between 1 and 65535")
	}
	if server.MTProtoPort < 1 || server.MTProtoPort > 65535 {
		validationErr.add("mtproto_port", "must be between 1 and 65535")
	}
	if server.Status == "" {
		validationErr.add("status", "is required")
	}

	if validationErr.empty() {
		return nil
	}

	return validationErr
}

func script(lines ...string) string {
	return strings.Join(lines, "\n")
}

func parameterInit() string {
	return ".parameter init"
}

func setParam(name, value string) string {
	return fmt.Sprintf(".parameter set %s %s", name, quote(value))
}

func setNullableString(name string, value *string) string {
	if value == nil {
		return fmt.Sprintf(".parameter set %s null", name)
	}
	return setParam(name, *value)
}

func setNullableTime(name string, value *time.Time) string {
	if value == nil {
		return fmt.Sprintf(".parameter set %s null", name)
	}
	return setParam(name, value.UTC().Format(time.RFC3339Nano))
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func mustID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return hex.EncodeToString(buffer)
}
