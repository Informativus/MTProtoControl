package serverrelationships

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"mtproxy-control/apps/api/internal/database"
	"mtproxy-control/apps/api/internal/inventory"
	"mtproxy-control/apps/api/internal/migrations"
)

func TestReplaceOutgoingAndListByServer(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := NewRepository(db)
	servers := inventory.NewRepository(db)

	primary := mustCreateServer(t, ctx, servers, "primary", "203.0.113.10")
	child := mustCreateServer(t, ctx, servers, "child", "203.0.113.11")
	ingress := mustCreateServer(t, ctx, servers, "ingress", "203.0.113.12")

	relations, err := repo.ReplaceOutgoing(ctx, primary.ID, []ReplaceInput{
		{Type: TypeParentChild, TargetServerID: child.ID},
		{Type: TypeSharedIngress, TargetServerID: ingress.ID},
	})
	if err != nil {
		t.Fatalf("replace outgoing relationships: %v", err)
	}
	if len(relations) != 2 {
		t.Fatalf("expected 2 attached relationships for primary, got %d", len(relations))
	}

	childRelations, err := repo.ListByServer(ctx, child.ID)
	if err != nil {
		t.Fatalf("list child relationships: %v", err)
	}
	if len(childRelations) != 1 {
		t.Fatalf("expected 1 child relationship, got %d", len(childRelations))
	}
	if childRelations[0].Type != TypeParentChild || childRelations[0].SourceServerID != primary.ID || childRelations[0].TargetServerID != child.ID {
		t.Fatalf("unexpected child relationship: %#v", childRelations[0])
	}

	allRelations, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all relationships: %v", err)
	}
	if len(allRelations) != 2 {
		t.Fatalf("expected 2 global relationships, got %d", len(allRelations))
	}

	relations, err = repo.ReplaceOutgoing(ctx, primary.ID, []ReplaceInput{{Type: TypeRouteThrough, TargetServerID: ingress.ID}})
	if err != nil {
		t.Fatalf("replace outgoing relationships second pass: %v", err)
	}
	if len(relations) != 1 || relations[0].Type != TypeRouteThrough {
		t.Fatalf("expected only route_through after replace, got %#v", relations)
	}

	allRelations, err = repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("list all relationships after replace: %v", err)
	}
	if len(allRelations) != 1 || allRelations[0].Type != TypeRouteThrough {
		t.Fatalf("unexpected relationships after replace: %#v", allRelations)
	}
}

func TestReplaceOutgoingRejectsInvalidTargetsAndReverseSharedDuplicate(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	repo := NewRepository(db)
	servers := inventory.NewRepository(db)

	left := mustCreateServer(t, ctx, servers, "left", "203.0.113.20")
	right := mustCreateServer(t, ctx, servers, "right", "203.0.113.21")

	if _, err := repo.ReplaceOutgoing(ctx, right.ID, []ReplaceInput{{Type: TypeSharedIngress, TargetServerID: left.ID}}); err != nil {
		t.Fatalf("seed reverse shared relationship: %v", err)
	}

	_, err := repo.ReplaceOutgoing(ctx, left.ID, []ReplaceInput{
		{Type: TypeDependsOn, TargetServerID: "missing-server"},
		{Type: TypeSharedIngress, TargetServerID: right.ID},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	validationErr, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if validationErr.Fields["relationships[0].target_server_id"] != "server not found" {
		t.Fatalf("unexpected missing target error: %#v", validationErr.Fields)
	}
	if validationErr.Fields["relationships[1].target_server_id"] != "peer server already declares this shared relationship" {
		t.Fatalf("unexpected reverse shared relationship error: %#v", validationErr.Fields)
	}

	_, err = repo.ReplaceOutgoing(ctx, left.ID, []ReplaceInput{{Type: TypeParentChild, TargetServerID: left.ID}})
	if err == nil {
		t.Fatal("expected self-reference validation error")
	}

	validationErr, ok = err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError for self reference, got %T", err)
	}
	if validationErr.Fields["relationships[0].target_server_id"] != "must reference a different server" {
		t.Fatalf("unexpected self relationship error: %#v", validationErr.Fields)
	}
}

func newTestDB(t *testing.T) *database.DB {
	t.Helper()

	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 binary not available")
	}

	tempDir := t.TempDir()
	db, err := database.Open(filepath.Join(tempDir, "panel.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_ = os.Remove(db.Path)
	})

	if err := migrations.Up(context.Background(), db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return db
}

func mustCreateServer(t *testing.T, ctx context.Context, repo *inventory.Repository, name, host string) inventory.Server {
	t.Helper()

	server, err := repo.CreateServer(ctx, inventory.CreateInput{
		Name:       name,
		Host:       host,
		SSHUser:    "operator",
		PublicHost: stringPointer(name + ".example.com"),
	})
	if err != nil {
		t.Fatalf("create server %s: %v", name, err)
	}

	return server
}

func stringPointer(value string) *string {
	return &value
}
