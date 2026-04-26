package telemtconfig

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
)

type Repository struct {
	db *database.DB
}

var ErrConfigNotFound = errors.New("telemt config not found")

type StoredConfig struct {
	ID            string     `json:"id"`
	ServerID      string     `json:"server_id"`
	Version       int        `json:"version"`
	ConfigText    string     `json:"config_text"`
	GeneratedLink string     `json:"generated_link"`
	AppliedAt     *time.Time `json:"applied_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	Fields        Fields     `json:"fields"`
}

type RevisionSummary struct {
	ID            string     `json:"id"`
	Version       int        `json:"version"`
	GeneratedLink string     `json:"generated_link"`
	AppliedAt     *time.Time `json:"applied_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type configRow struct {
	ID            string  `json:"id"`
	ServerID      string  `json:"server_id"`
	Version       int     `json:"version"`
	ConfigText    string  `json:"config_text"`
	GeneratedLink *string `json:"generated_link"`
	AppliedAt     *string `json:"applied_at"`
	CreatedAt     string  `json:"created_at"`
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetCurrent(ctx context.Context, serverID string) (*StoredConfig, error) {
	rows, err := r.queryRows(ctx, serverID, `
		SELECT id, server_id, version, config_text, generated_link, applied_at, created_at
		FROM telemt_configs
		WHERE server_id = @server_id
		ORDER BY version DESC, created_at DESC
		LIMIT 1;
	`)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}

	config, err := rows[0].toStoredConfig()
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func (r *Repository) ListRevisions(ctx context.Context, serverID string) ([]RevisionSummary, error) {
	rows, err := r.queryRows(ctx, serverID, `
		SELECT id, server_id, version, config_text, generated_link, applied_at, created_at
		FROM telemt_configs
		WHERE server_id = @server_id
		ORDER BY version DESC, created_at DESC;
	`)
	if err != nil {
		return nil, err
	}

	revisions := make([]RevisionSummary, 0, len(rows))
	for _, row := range rows {
		revision, err := row.toRevisionSummary()
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}

	return revisions, nil
}

func (r *Repository) SaveRevision(ctx context.Context, serverID, configText string) (*StoredConfig, error) {
	fields, validationErr := Parse(configText)
	if validationErr != nil {
		return nil, validationErr
	}

	now := time.Now().UTC()
	generatedLink := PreviewLink(fields)
	configID := mustID()

	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@id", configID),
		setParam("@server_id", strings.TrimSpace(serverID)),
		setParam("@generated_link", generatedLink),
		setParam("@created_at", now.Format(time.RFC3339Nano)),
		fmt.Sprintf(`INSERT INTO telemt_configs (
			id, server_id, version, config_text, generated_link, applied_at, created_at
		) VALUES (
			@id,
			@server_id,
			(SELECT COALESCE(MAX(version), 0) + 1 FROM telemt_configs WHERE server_id = @server_id),
			%s,
			@generated_link,
			null,
			@created_at
		);`, textLiteral(configText)),
	)); err != nil {
		return nil, fmt.Errorf("save telemt config revision: %w", err)
	}

	return r.GetCurrent(ctx, serverID)
}

func (r *Repository) MarkApplied(ctx context.Context, serverID, generatedLink string, appliedAt time.Time) (*StoredConfig, error) {
	var rows []struct {
		Affected int `json:"affected"`
	}

	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		setParam("@generated_link", strings.TrimSpace(generatedLink)),
		setParam("@applied_at", appliedAt.UTC().Format(time.RFC3339Nano)),
		`UPDATE telemt_configs
		 SET generated_link = @generated_link,
		     applied_at = @applied_at
		 WHERE id = (
		   SELECT id
		   FROM telemt_configs
		   WHERE server_id = @server_id
		   ORDER BY version DESC, created_at DESC
		   LIMIT 1
		 );
		 SELECT changes() AS affected;`,
	), &rows); err != nil {
		return nil, fmt.Errorf("mark telemt config applied: %w", err)
	}

	if len(rows) == 0 || rows[0].Affected == 0 {
		return nil, ErrConfigNotFound
	}

	return r.GetCurrent(ctx, serverID)
}

func (r *Repository) queryRows(ctx context.Context, serverID, query string) ([]configRow, error) {
	var rows []configRow
	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		query,
	), &rows); err != nil {
		return nil, fmt.Errorf("query telemt configs: %w", err)
	}

	return rows, nil
}

func (r configRow) toStoredConfig() (StoredConfig, error) {
	createdAt, appliedAt, err := parseTimes(r.CreatedAt, r.AppliedAt)
	if err != nil {
		return StoredConfig{}, err
	}

	fields, validationErr := Parse(r.ConfigText)
	if validationErr != nil {
		return StoredConfig{}, validationErr
	}

	generatedLink := PreviewLink(fields)
	if r.GeneratedLink != nil && strings.TrimSpace(*r.GeneratedLink) != "" {
		generatedLink = strings.TrimSpace(*r.GeneratedLink)
	}

	return StoredConfig{
		ID:            r.ID,
		ServerID:      r.ServerID,
		Version:       r.Version,
		ConfigText:    r.ConfigText,
		GeneratedLink: generatedLink,
		AppliedAt:     appliedAt,
		CreatedAt:     createdAt,
		Fields:        fields,
	}, nil
}

func (r configRow) toRevisionSummary() (RevisionSummary, error) {
	createdAt, appliedAt, err := parseTimes(r.CreatedAt, r.AppliedAt)
	if err != nil {
		return RevisionSummary{}, err
	}

	generatedLink := ""
	if r.GeneratedLink != nil {
		generatedLink = strings.TrimSpace(*r.GeneratedLink)
	}

	return RevisionSummary{
		ID:            r.ID,
		Version:       r.Version,
		GeneratedLink: generatedLink,
		AppliedAt:     appliedAt,
		CreatedAt:     createdAt,
	}, nil
}

func parseTimes(createdAtRaw string, appliedAtRaw *string) (time.Time, *time.Time, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtRaw)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("parse telemt config created_at: %w", err)
	}

	var appliedAt *time.Time
	if appliedAtRaw != nil {
		parsed, err := time.Parse(time.RFC3339Nano, *appliedAtRaw)
		if err != nil {
			return time.Time{}, nil, fmt.Errorf("parse telemt config applied_at: %w", err)
		}
		value := parsed.UTC()
		appliedAt = &value
	}

	return createdAt.UTC(), appliedAt, nil
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

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func textLiteral(value string) string {
	return fmt.Sprintf("CAST(X'%s' AS TEXT)", hex.EncodeToString([]byte(value)))
}

func mustID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return hex.EncodeToString(buffer)
}
