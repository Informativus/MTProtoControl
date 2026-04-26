package healthchecks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
)

type Repository struct {
	db *database.DB
}

type Check struct {
	ID          string    `json:"id"`
	ServerID    string    `json:"server_id"`
	Status      string    `json:"status"`
	DNSOK       bool      `json:"dns_ok"`
	TCPOK       bool      `json:"tcp_ok"`
	SSHOK       bool      `json:"ssh_ok"`
	TelemtAPIOK bool      `json:"telemt_api_ok"`
	DockerOK    bool      `json:"docker_ok"`
	LinkOK      bool      `json:"link_ok"`
	LatencyMS   *int      `json:"latency_ms,omitempty"`
	Message     string    `json:"message,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateInput struct {
	ServerID    string
	Status      string
	DNSOK       bool
	TCPOK       bool
	SSHOK       bool
	TelemtAPIOK bool
	DockerOK    bool
	LinkOK      bool
	LatencyMS   *int
	Message     string
	CreatedAt   time.Time
}

type Transition struct {
	PreviousStatus string `json:"previous_status,omitempty"`
	CurrentStatus  string `json:"current_status"`
	Changed        bool   `json:"changed"`
}

type healthRow struct {
	ID          string  `json:"id"`
	ServerID    string  `json:"server_id"`
	Status      string  `json:"status"`
	DNSOK       int     `json:"dns_ok"`
	TCPOK       int     `json:"tcp_ok"`
	SSHOK       int     `json:"ssh_ok"`
	TelemtAPIOK int     `json:"telemt_api_ok"`
	DockerOK    int     `json:"docker_ok"`
	LinkOK      int     `json:"link_ok"`
	LatencyMS   *int    `json:"latency_ms"`
	Message     *string `json:"message"`
	CreatedAt   string  `json:"created_at"`
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Append(ctx context.Context, input CreateInput) (Check, Transition, error) {
	if r == nil || r.db == nil {
		return Check{}, Transition{}, fmt.Errorf("append health check: repository is not configured")
	}

	createdAt := input.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	check := Check{
		ID:          mustID(),
		ServerID:    strings.TrimSpace(input.ServerID),
		Status:      strings.TrimSpace(input.Status),
		DNSOK:       input.DNSOK,
		TCPOK:       input.TCPOK,
		SSHOK:       input.SSHOK,
		TelemtAPIOK: input.TelemtAPIOK,
		DockerOK:    input.DockerOK,
		LinkOK:      input.LinkOK,
		LatencyMS:   input.LatencyMS,
		Message:     strings.TrimSpace(input.Message),
		CreatedAt:   createdAt,
	}
	if check.Status == "" {
		check.Status = "unknown"
	}

	previous, err := r.GetLatest(ctx, check.ServerID)
	if err != nil {
		return Check{}, Transition{}, err
	}

	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@id", check.ID),
		setParam("@server_id", check.ServerID),
		setParam("@status", check.Status),
		setParam("@dns_ok", boolInt(check.DNSOK)),
		setParam("@tcp_ok", boolInt(check.TCPOK)),
		setParam("@ssh_ok", boolInt(check.SSHOK)),
		setParam("@telemt_api_ok", boolInt(check.TelemtAPIOK)),
		setParam("@docker_ok", boolInt(check.DockerOK)),
		setParam("@link_ok", boolInt(check.LinkOK)),
		setNullableInt("@latency_ms", check.LatencyMS),
		setNullableString("@message", emptyToNil(check.Message)),
		setParam("@created_at", check.CreatedAt.Format(time.RFC3339Nano)),
		`INSERT INTO health_checks (
			id, server_id, status, dns_ok, tcp_ok, ssh_ok, telemt_api_ok, docker_ok, link_ok, latency_ms, message, created_at
		) VALUES (
			@id, @server_id, @status, @dns_ok, @tcp_ok, @ssh_ok, @telemt_api_ok, @docker_ok, @link_ok, @latency_ms, @message, @created_at
		);`,
	)); err != nil {
		return Check{}, Transition{}, fmt.Errorf("append health check: %w", err)
	}

	transition := Transition{
		CurrentStatus: check.Status,
	}
	if previous != nil {
		transition.PreviousStatus = previous.Status
		transition.Changed = previous.Status != check.Status
	}

	return check, transition, nil
}

func (r *Repository) GetLatest(ctx context.Context, serverID string) (*Check, error) {
	checks, err := r.ListByServer(ctx, serverID, 1)
	if err != nil {
		return nil, err
	}
	if len(checks) == 0 {
		return nil, nil
	}

	return &checks[0], nil
}

func (r *Repository) ListByServer(ctx context.Context, serverID string, limit int) ([]Check, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}

	var rows []healthRow
	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		setParam("@limit", strconv.Itoa(limit)),
		`SELECT id, server_id, status, dns_ok, tcp_ok, ssh_ok, telemt_api_ok, docker_ok, link_ok, latency_ms, message, created_at
		 FROM health_checks
		 WHERE server_id = @server_id
		 ORDER BY created_at DESC, id DESC
		 LIMIT @limit;`,
	), &rows); err != nil {
		return nil, fmt.Errorf("list health checks: %w", err)
	}

	checks := make([]Check, 0, len(rows))
	for _, row := range rows {
		check, err := row.toCheck()
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}

	return checks, nil
}

func (r *Repository) GetLatestByStatusBefore(ctx context.Context, serverID, status string, before time.Time) (*Check, error) {
	return r.getSingle(ctx, script(
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		setParam("@status", strings.TrimSpace(status)),
		setParam("@before", before.UTC().Format(time.RFC3339Nano)),
		`SELECT id, server_id, status, dns_ok, tcp_ok, ssh_ok, telemt_api_ok, docker_ok, link_ok, latency_ms, message, created_at
		 FROM health_checks
		 WHERE server_id = @server_id
		   AND status = @status
		   AND created_at < @before
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1;`,
	))
}

func (r *Repository) FindOutageStart(ctx context.Context, serverID string, recoveredAt time.Time) (*Check, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	lastOnline, err := r.GetLatestByStatusBefore(ctx, serverID, onlineStatus, recoveredAt)
	if err != nil {
		return nil, err
	}

	lines := []string{
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		setParam("@before", recoveredAt.UTC().Format(time.RFC3339Nano)),
	}
	query := `SELECT id, server_id, status, dns_ok, tcp_ok, ssh_ok, telemt_api_ok, docker_ok, link_ok, latency_ms, message, created_at
	 FROM health_checks
	 WHERE server_id = @server_id
	   AND status != 'online'
	   AND created_at < @before`
	if lastOnline != nil {
		lines = append(lines, setParam("@after", lastOnline.CreatedAt.UTC().Format(time.RFC3339Nano)))
		query += "\n   AND created_at > @after"
	}
	query += "\n ORDER BY created_at ASC, id ASC\n LIMIT 1;"
	lines = append(lines, query)

	return r.getSingle(ctx, script(lines...))
}

func (r *Repository) getSingle(ctx context.Context, query string) (*Check, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	var rows []healthRow
	if err := r.db.Query(ctx, query, &rows); err != nil {
		return nil, fmt.Errorf("query health check: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	check, err := rows[0].toCheck()
	if err != nil {
		return nil, err
	}
	return &check, nil
}

func (r healthRow) toCheck() (Check, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return Check{}, fmt.Errorf("parse health check created_at: %w", err)
	}

	check := Check{
		ID:          r.ID,
		ServerID:    r.ServerID,
		Status:      r.Status,
		DNSOK:       r.DNSOK != 0,
		TCPOK:       r.TCPOK != 0,
		SSHOK:       r.SSHOK != 0,
		TelemtAPIOK: r.TelemtAPIOK != 0,
		DockerOK:    r.DockerOK != 0,
		LinkOK:      r.LinkOK != 0,
		LatencyMS:   r.LatencyMS,
		CreatedAt:   createdAt.UTC(),
	}
	if r.Message != nil {
		check.Message = strings.TrimSpace(*r.Message)
	}

	return check, nil
}

func boolInt(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func emptyToNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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

func setNullableInt(name string, value *int) string {
	if value == nil {
		return fmt.Sprintf(".parameter set %s null", name)
	}
	return fmt.Sprintf(".parameter set %s %d", name, *value)
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func mustID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate health check id: %v", err))
	}
	return hex.EncodeToString(buffer)
}
