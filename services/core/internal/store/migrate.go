package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	connString := pool.Config().ConnString()
	db, err := sql.Open("pgx/v5", connString)
	if err != nil {
		return fmt.Errorf("open sql for migrations: %w", err)
	}
	defer db.Close()
	var schemaName string
	if err := db.QueryRow("SHOW search_path").Scan(&schemaName); err != nil {
		return fmt.Errorf("resolve schema: %w", err)
	}
	schemaName = strings.TrimSpace(schemaName)
	if schemaName != "public" && schemaName != `"$user", public` {
		if _, err := db.Exec(fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, schemaName)); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	} else {
		schemaName = "public"
	}
	driver, err := postgres.WithInstance(db, &postgres.Config{SchemaName: schemaName})
	if err != nil {
		return fmt.Errorf("migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate init: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
