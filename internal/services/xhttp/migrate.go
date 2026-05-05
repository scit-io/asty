// internal/services/xhttp/migrate.go
package xhttp

import "database/sql"

func Migrate(db *sql.DB) error {
	_, err := db.Exec(`
		SELECT pg_advisory_lock(hashtext('xhttp_migrate'));

		CREATE TABLE IF NOT EXISTS xhttp (
			id         BIGSERIAL    PRIMARY KEY,
			name       TEXT         NOT NULL,
			value      TEXT         NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
		);

		SELECT pg_advisory_unlock(hashtext('xhttp_migrate'));
	`)
	return err
}
