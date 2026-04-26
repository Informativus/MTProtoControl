package serverevents

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

type Event struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"server_id"`
	Level     string    `json:"level"`
	EventType string    `json:"event_type"`
	Message   string    `json:"message"`
	Stdout    string    `json:"stdout,omitempty"`
	Stderr    string    `json:"stderr,omitempty"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	ServerID  string
	Level     string
	EventType string
	Message   string
	Stdout    string
	Stderr    string
	ExitCode  *int
	CreatedAt time.Time
}

type eventRow struct {
	ID        string  `json:"id"`
	ServerID  string  `json:"server_id"`
	Level     string  `json:"level"`
	EventType string  `json:"event_type"`
	Message   string  `json:"message"`
	Stdout    *string `json:"stdout"`
	Stderr    *string `json:"stderr"`
	ExitCode  *int    `json:"exit_code"`
	CreatedAt string  `json:"created_at"`
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Append(ctx context.Context, input CreateInput) (Event, error) {
	event := Event{
		ID:        mustID(),
		ServerID:  strings.TrimSpace(input.ServerID),
		Level:     strings.TrimSpace(input.Level),
		EventType: strings.TrimSpace(input.EventType),
		Message:   strings.TrimSpace(input.Message),
		Stdout:    strings.TrimSpace(input.Stdout),
		Stderr:    strings.TrimSpace(input.Stderr),
		ExitCode:  input.ExitCode,
		CreatedAt: input.CreatedAt.UTC(),
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}

	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@id", event.ID),
		setParam("@server_id", event.ServerID),
		setParam("@level", event.Level),
		setParam("@event_type", event.EventType),
		setParam("@message", event.Message),
		setNullableString("@stdout", emptyToNil(event.Stdout)),
		setNullableString("@stderr", emptyToNil(event.Stderr)),
		setNullableInt("@exit_code", event.ExitCode),
		setParam("@created_at", event.CreatedAt.Format(time.RFC3339Nano)),
		`INSERT INTO server_events (
			id, server_id, level, event_type, message, stdout, stderr, exit_code, created_at
		) VALUES (
			@id, @server_id, @level, @event_type, @message, @stdout, @stderr, @exit_code, @created_at
		);`,
	)); err != nil {
		return Event{}, fmt.Errorf("append server event: %w", err)
	}

	return event, nil
}

func (r *Repository) ListByServer(ctx context.Context, serverID string, limit int) ([]Event, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	var rows []eventRow
	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		setParam("@limit", strconv.Itoa(limit)),
		`SELECT id, server_id, level, event_type, message, stdout, stderr, exit_code, created_at
		 FROM server_events
		 WHERE server_id = @server_id
		 ORDER BY created_at DESC, id DESC
		 LIMIT @limit;`,
	), &rows); err != nil {
		return nil, fmt.Errorf("list server events: %w", err)
	}

	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		event, err := row.toEvent()
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

func (r *Repository) GetLatestByEventTypes(ctx context.Context, serverID string, eventTypes ...string) (*Event, error) {
	if r == nil || r.db == nil || len(eventTypes) == 0 {
		return nil, nil
	}

	lines := []string{
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
	}
	placeholders := make([]string, 0, len(eventTypes))
	for index, eventType := range eventTypes {
		name := fmt.Sprintf("@event_type_%d", index)
		lines = append(lines, setParam(name, strings.TrimSpace(eventType)))
		placeholders = append(placeholders, name)
	}
	lines = append(lines, fmt.Sprintf(`SELECT id, server_id, level, event_type, message, stdout, stderr, exit_code, created_at
		 FROM server_events
		 WHERE server_id = @server_id
		   AND event_type IN (%s)
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1;`, strings.Join(placeholders, ", ")))

	var rows []eventRow
	if err := r.db.Query(ctx, script(lines...), &rows); err != nil {
		return nil, fmt.Errorf("get latest server event by type: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	event, err := rows[0].toEvent()
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (r eventRow) toEvent() (Event, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return Event{}, fmt.Errorf("parse server event created_at: %w", err)
	}

	event := Event{
		ID:        r.ID,
		ServerID:  r.ServerID,
		Level:     r.Level,
		EventType: r.EventType,
		Message:   r.Message,
		ExitCode:  r.ExitCode,
		CreatedAt: createdAt.UTC(),
	}
	if r.Stdout != nil {
		event.Stdout = *r.Stdout
	}
	if r.Stderr != nil {
		event.Stderr = *r.Stderr
	}

	return event, nil
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
		panic(fmt.Sprintf("generate event id: %v", err))
	}
	return hex.EncodeToString(buffer)
}
