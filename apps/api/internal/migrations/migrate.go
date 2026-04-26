package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"mtproxy-control/apps/api/internal/database"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

type fileMigration struct {
	name string
	body string
}

func Up(ctx context.Context, db *database.DB) error {
	if err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	files, err := loadFiles()
	if err != nil {
		return err
	}

	for _, migration := range files {
		applied, err := isApplied(ctx, db, migration.name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		if err := apply(ctx, db, migration); err != nil {
			return err
		}
	}

	return nil
}

func loadFiles() ([]fileMigration, error) {
	entries, err := fs.ReadDir(migrationFiles, "sql")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	migrations := make([]fileMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		body, err := migrationFiles.ReadFile("sql/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, fileMigration{
			name: entry.Name(),
			body: string(body),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].name < migrations[j].name
	})

	return migrations, nil
}

func isApplied(ctx context.Context, db *database.DB, version string) (bool, error) {
	var rows []struct {
		Count int `json:"count"`
	}

	if err := db.Query(ctx, fmt.Sprintf(`
		.parameter init
		.parameter set @version %s
		SELECT COUNT(1) AS count
		FROM schema_migrations
		WHERE version = @version;
	`, quote(version)), &rows); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}

	if len(rows) == 0 {
		return false, nil
	}

	return rows[0].Count > 0, nil
}

func apply(ctx context.Context, db *database.DB, migration fileMigration) error {
	script := fmt.Sprintf(`
		.parameter init
		.parameter set @version %s
		.parameter set @applied_at %s
		BEGIN;
		%s
		INSERT INTO schema_migrations (version, applied_at)
		VALUES (@version, @applied_at);
		COMMIT;
	`,
		quote(migration.name),
		quote(time.Now().UTC().Format(time.RFC3339Nano)),
		strings.TrimSpace(migration.body),
	)

	if err := db.Exec(ctx, script); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.name, err)
	}

	return nil
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
