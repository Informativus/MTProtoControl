package sshcredentials

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
)

type Repository struct {
	db *database.DB
}

type Credential struct {
	ID             string    `json:"id"`
	ServerID       string    `json:"server_id"`
	AuthType       string    `json:"auth_type"`
	PrivateKeyPath *string   `json:"private_key_path,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type credentialRow struct {
	ID             string  `json:"id"`
	ServerID       string  `json:"server_id"`
	AuthType       string  `json:"auth_type"`
	PrivateKeyPath *string `json:"private_key_path"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) RememberPrivateKeyPath(ctx context.Context, serverID, privateKeyPath string) error {
	if r == nil || r.db == nil {
		return nil
	}

	serverID = strings.TrimSpace(serverID)
	privateKeyPath = strings.TrimSpace(privateKeyPath)
	if serverID == "" || privateKeyPath == "" {
		return nil
	}

	now := time.Now().UTC()
	current, err := r.GetLatestForServer(ctx, serverID)
	if err != nil {
		return err
	}

	if current != nil {
		if err := r.db.Exec(ctx, script(
			parameterInit(),
			setParam("@id", current.ID),
			setParam("@private_key_path", privateKeyPath),
			setParam("@updated_at", now.Format(time.RFC3339Nano)),
			`UPDATE ssh_credentials
			 SET auth_type = 'private_key_path',
			     private_key_path = @private_key_path,
			     updated_at = @updated_at
			 WHERE id = @id;`,
		)); err != nil {
			return fmt.Errorf("update ssh credential path: %w", err)
		}
		return nil
	}

	if err := r.db.Exec(ctx, script(
		parameterInit(),
		setParam("@id", mustID()),
		setParam("@server_id", serverID),
		setParam("@auth_type", "private_key_path"),
		setParam("@private_key_path", privateKeyPath),
		setParam("@created_at", now.Format(time.RFC3339Nano)),
		setParam("@updated_at", now.Format(time.RFC3339Nano)),
		`INSERT INTO ssh_credentials (
			id, server_id, auth_type, private_key_path, created_at, updated_at
		) VALUES (
			@id, @server_id, @auth_type, @private_key_path, @created_at, @updated_at
		);`,
	)); err != nil {
		return fmt.Errorf("insert ssh credential path: %w", err)
	}

	return nil
}

func (r *Repository) GetLatestForServer(ctx context.Context, serverID string) (*Credential, error) {
	if r == nil || r.db == nil {
		return nil, nil
	}

	var rows []credentialRow
	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		`SELECT id, server_id, auth_type, private_key_path, created_at, updated_at
		 FROM ssh_credentials
		 WHERE server_id = @server_id
		   AND auth_type = 'private_key_path'
		   AND private_key_path IS NOT NULL
		 ORDER BY updated_at DESC, created_at DESC, id DESC
		 LIMIT 1;`,
	), &rows); err != nil {
		return nil, fmt.Errorf("get ssh credential path: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	credential, err := rows[0].toCredential()
	if err != nil {
		return nil, err
	}

	return &credential, nil
}

func (r credentialRow) toCredential() (Credential, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return Credential{}, fmt.Errorf("parse ssh credential created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return Credential{}, fmt.Errorf("parse ssh credential updated_at: %w", err)
	}

	credential := Credential{
		ID:        r.ID,
		ServerID:  r.ServerID,
		AuthType:  r.AuthType,
		CreatedAt: createdAt.UTC(),
		UpdatedAt: updatedAt.UTC(),
	}
	if r.PrivateKeyPath != nil {
		value := strings.TrimSpace(*r.PrivateKeyPath)
		credential.PrivateKeyPath = &value
	}

	return credential, nil
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

func mustID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate ssh credential id: %v", err))
	}
	return hex.EncodeToString(buffer)
}
