package serverrelationships

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

const (
	TypeParentChild   = "parent_child"
	TypeDependsOn     = "depends_on"
	TypeReplica       = "replica"
	TypeRouteThrough  = "route_through"
	TypeSharedIngress = "shared_ingress"
)

var ErrServerNotFound = errors.New("server not found")

var allowedTypes = map[string]struct{}{
	TypeParentChild:   {},
	TypeDependsOn:     {},
	TypeReplica:       {},
	TypeRouteThrough:  {},
	TypeSharedIngress: {},
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
	if _, exists := e.Fields[field]; exists {
		return
	}
	e.Fields[field] = message
}

func (e *ValidationError) empty() bool {
	return len(e.Fields) == 0
}

type Repository struct {
	db *database.DB
}

type Relation struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	SourceServerID string    `json:"source_server_id"`
	TargetServerID string    `json:"target_server_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ReplaceInput struct {
	Type           string
	TargetServerID string
}

type relationRow struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	SourceServerID string `json:"source_server_id"`
	TargetServerID string `json:"target_server_id"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func NewRepository(db *database.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListAll(ctx context.Context) ([]Relation, error) {
	var rows []relationRow
	if err := r.db.Query(ctx, `
		SELECT id,
		       relation_type AS type,
		       source_server_id,
		       target_server_id,
		       created_at,
		       updated_at
		FROM server_relationships
		ORDER BY relation_type ASC, source_server_id ASC, target_server_id ASC, id ASC;
	`, &rows); err != nil {
		return nil, fmt.Errorf("list server relationships: %w", err)
	}

	return mapRows(rows)
}

func (r *Repository) ListByServer(ctx context.Context, serverID string) ([]Relation, error) {
	trimmedID := strings.TrimSpace(serverID)

	var rows []relationRow
	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@server_id", trimmedID),
		`SELECT id,
		        relation_type AS type,
		        source_server_id,
		        target_server_id,
		        created_at,
		        updated_at
		 FROM server_relationships
		 WHERE source_server_id = @server_id OR target_server_id = @server_id
		 ORDER BY relation_type ASC, source_server_id ASC, target_server_id ASC, id ASC;`,
	), &rows); err != nil {
		return nil, fmt.Errorf("list server relationships by server: %w", err)
	}

	return mapRows(rows)
}

func (r *Repository) ReplaceOutgoing(ctx context.Context, sourceServerID string, inputs []ReplaceInput) ([]Relation, error) {
	trimmedSourceID := strings.TrimSpace(sourceServerID)
	normalized, validationErr := normalizeReplaceInputs(trimmedSourceID, inputs)
	if validationErr != nil {
		return nil, validationErr
	}

	exists, err := r.serverExists(ctx, trimmedSourceID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrServerNotFound
	}

	if err := r.validateTargets(ctx, trimmedSourceID, normalized); err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	lines := []string{
		parameterInit(),
		setParam("@source_server_id", trimmedSourceID),
		"BEGIN;",
		"DELETE FROM server_relationships WHERE source_server_id = @source_server_id;",
	}

	for index, input := range normalized {
		lines = append(lines,
			setParam(paramName("@id", index), mustID()),
			setParam(paramName("@relation_type", index), input.Type),
			setParam(paramName("@target_server_id", index), input.TargetServerID),
			setParam(paramName("@created_at", index), now.Format(time.RFC3339Nano)),
			setParam(paramName("@updated_at", index), now.Format(time.RFC3339Nano)),
			fmt.Sprintf(`INSERT INTO server_relationships (
				id, relation_type, source_server_id, target_server_id, created_at, updated_at
			) VALUES (
				%s, %s, @source_server_id, %s, %s, %s
			);`,
				paramName("@id", index),
				paramName("@relation_type", index),
				paramName("@target_server_id", index),
				paramName("@created_at", index),
				paramName("@updated_at", index),
			),
		)
	}

	lines = append(lines, "COMMIT;")
	if err := r.db.Exec(ctx, script(lines...)); err != nil {
		return nil, fmt.Errorf("replace outgoing server relationships: %w", err)
	}

	return r.ListByServer(ctx, trimmedSourceID)
}

func IsSymmetricType(relationshipType string) bool {
	switch strings.TrimSpace(relationshipType) {
	case TypeReplica, TypeSharedIngress:
		return true
	default:
		return false
	}
}

func normalizeReplaceInputs(sourceServerID string, inputs []ReplaceInput) ([]ReplaceInput, *ValidationError) {
	validationErr := &ValidationError{}
	if sourceServerID == "" {
		validationErr.add("server_id", "is required")
	}

	normalized := make([]ReplaceInput, 0, len(inputs))
	seen := map[string]struct{}{}
	for index, input := range inputs {
		relationshipType := strings.TrimSpace(input.Type)
		targetServerID := strings.TrimSpace(input.TargetServerID)
		typeField := fmt.Sprintf("relationships[%d].type", index)
		targetField := fmt.Sprintf("relationships[%d].target_server_id", index)

		if relationshipType == "" {
			validationErr.add(typeField, "is required")
		} else if _, ok := allowedTypes[relationshipType]; !ok {
			validationErr.add(typeField, "must be one of: parent_child, depends_on, replica, route_through, shared_ingress")
		}

		if targetServerID == "" {
			validationErr.add(targetField, "is required")
		} else if targetServerID == sourceServerID {
			validationErr.add(targetField, "must reference a different server")
		}

		key := relationshipType + "|" + targetServerID
		if relationshipType != "" && targetServerID != "" {
			if _, exists := seen[key]; exists {
				validationErr.add(targetField, "duplicate relationship in request")
			} else {
				seen[key] = struct{}{}
			}
		}

		normalized = append(normalized, ReplaceInput{
			Type:           relationshipType,
			TargetServerID: targetServerID,
		})
	}

	if validationErr.empty() {
		return normalized, nil
	}

	return nil, validationErr
}

func (r *Repository) validateTargets(ctx context.Context, sourceServerID string, inputs []ReplaceInput) error {
	validationErr := &ValidationError{}
	for index, input := range inputs {
		targetField := fmt.Sprintf("relationships[%d].target_server_id", index)

		exists, err := r.serverExists(ctx, input.TargetServerID)
		if err != nil {
			return err
		}
		if !exists {
			validationErr.add(targetField, "server not found")
			continue
		}

		if !IsSymmetricType(input.Type) {
			continue
		}

		hasReverse, err := r.hasReverseSymmetricRelationship(ctx, sourceServerID, input.TargetServerID, input.Type)
		if err != nil {
			return err
		}
		if hasReverse {
			validationErr.add(targetField, "peer server already declares this shared relationship")
		}
	}

	if validationErr.empty() {
		return nil
	}

	return validationErr
}

func (r *Repository) hasReverseSymmetricRelationship(ctx context.Context, sourceServerID, targetServerID, relationshipType string) (bool, error) {
	var rows []struct {
		Count int `json:"count"`
	}

	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@source_server_id", sourceServerID),
		setParam("@target_server_id", targetServerID),
		setParam("@relation_type", relationshipType),
		`SELECT COUNT(1) AS count
		 FROM server_relationships
		 WHERE relation_type = @relation_type
		   AND source_server_id = @target_server_id
		   AND target_server_id = @source_server_id;`,
	), &rows); err != nil {
		return false, fmt.Errorf("check reverse server relationship: %w", err)
	}

	return len(rows) > 0 && rows[0].Count > 0, nil
}

func (r *Repository) serverExists(ctx context.Context, serverID string) (bool, error) {
	var rows []struct {
		Count int `json:"count"`
	}

	if err := r.db.Query(ctx, script(
		parameterInit(),
		setParam("@server_id", strings.TrimSpace(serverID)),
		`SELECT COUNT(1) AS count
		 FROM servers
		 WHERE id = @server_id;`,
	), &rows); err != nil {
		return false, fmt.Errorf("check server existence: %w", err)
	}

	return len(rows) > 0 && rows[0].Count > 0, nil
}

func mapRows(rows []relationRow) ([]Relation, error) {
	relations := make([]Relation, 0, len(rows))
	for _, row := range rows {
		relation, err := row.toRelation()
		if err != nil {
			return nil, err
		}
		relations = append(relations, relation)
	}
	return relations, nil
}

func (r relationRow) toRelation() (Relation, error) {
	createdAt, err := time.Parse(time.RFC3339Nano, r.CreatedAt)
	if err != nil {
		return Relation{}, fmt.Errorf("parse created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if err != nil {
		return Relation{}, fmt.Errorf("parse updated_at: %w", err)
	}

	return Relation{
		ID:             r.ID,
		Type:           r.Type,
		SourceServerID: r.SourceServerID,
		TargetServerID: r.TargetServerID,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
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

func paramName(prefix string, index int) string {
	return fmt.Sprintf("%s_%d", prefix, index)
}

func mustID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(fmt.Sprintf("generate id: %v", err))
	}
	return hex.EncodeToString(buffer)
}
