package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crm-prospect-simulator/backend/internal/auth/model"
	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
)

type MySQLRepository struct {
	db *sql.DB
}

func NewMySQLRepository(db *sql.DB) *MySQLRepository {
	return &MySQLRepository{db: db}
}

func (r *MySQLRepository) FindByEmail(ctx context.Context, email string) (model.User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx, `
		SELECT id, email, password_hash, full_name, role, status,
		       token_version, last_login_at, created_at, updated_at
		FROM users WHERE email = ?`, email))
}

func (r *MySQLRepository) FindUserByID(ctx context.Context, id uuid.UUID) (model.User, error) {
	return r.scanUser(r.db.QueryRowContext(ctx, `
		SELECT id, email, password_hash, full_name, role, status,
		       token_version, last_login_at, created_at, updated_at
		FROM users WHERE id = ?`, id.String()))
}

func (r *MySQLRepository) scanUser(row *sql.Row) (model.User, error) {
	var user model.User
	var idStr string
	var lastLoginAt sql.NullTime
	err := row.Scan(&idStr, &user.Email, &user.PasswordHash, &user.FullName, &user.Role,
		&user.Status, &user.TokenVersion, &lastLoginAt, &user.CreatedAt, &user.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("scan user: %w", err)
	}
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return model.User{}, fmt.Errorf("parse user id from database: %w", err)
	}
	user.ID = parsedID
	if lastLoginAt.Valid {
		user.LastLoginAt = &lastLoginAt.Time
	}
	return user, nil
}

func (r *MySQLRepository) RecordLogin(ctx context.Context, userID uuid.UUID, at time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?`, at, at, userID.String())
	if err != nil {
		return fmt.Errorf("record login: %w", err)
	}
	return nil
}

func (r *MySQLRepository) UpsertSeed(ctx context.Context, user model.User) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, full_name, role, status, token_version)
		VALUES (?, ?, ?, ?, ?, ?, 1)
		ON DUPLICATE KEY UPDATE
			password_hash = VALUES(password_hash),
			full_name = VALUES(full_name),
			role = VALUES(role),
			status = VALUES(status),
			updated_at = UTC_TIMESTAMP(6)`,
		user.ID.String(), user.Email, user.PasswordHash, user.FullName, user.Role, user.Status)
	if err != nil {
		return fmt.Errorf("seed user: %w", err)
	}
	return nil
}

func (r *MySQLRepository) Create(ctx context.Context, session model.RefreshSession) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO refresh_sessions
			(id, user_id, token_hash, user_agent, ip_address, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID.String(), session.UserID.String(),
		session.TokenHash, session.UserAgent, session.IPAddress, session.ExpiresAt)
	return mysqlError("create refresh session", err)
}

func (r *MySQLRepository) FindSessionByID(ctx context.Context, id uuid.UUID) (model.RefreshSession, error) {
	var session model.RefreshSession
	var idStr, userIDStr string
	var revokedAt sql.NullTime
	var replacedBy sql.NullString

	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, user_agent, ip_address, expires_at,
		       revoked_at, COALESCE(revoke_reason, ''), replaced_by_session_id, created_at
		FROM refresh_sessions WHERE id = ?`, id.String()).Scan(
		&idStr, &userIDStr, &session.TokenHash, &session.UserAgent,
		&session.IPAddress, &session.ExpiresAt, &revokedAt,
		&session.RevokeReason, &replacedBy, &session.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.RefreshSession{}, ErrNotFound
	}
	if err != nil {
		return model.RefreshSession{}, fmt.Errorf("find refresh session: %w", err)
	}
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		return model.RefreshSession{}, fmt.Errorf("parse session id from database: %w", err)
	}
	parsedUserID, err := uuid.Parse(userIDStr)
	if err != nil {
		return model.RefreshSession{}, fmt.Errorf("parse session user id from database: %w", err)
	}
	session.ID = parsedID
	session.UserID = parsedUserID
	if revokedAt.Valid {
		session.RevokedAt = &revokedAt.Time
	}
	if replacedBy.Valid {
		parsedReplaced, err := uuid.Parse(replacedBy.String)
		if err != nil {
			return model.RefreshSession{}, fmt.Errorf("parse replaced_by_session_id from database: %w", err)
		}
		session.ReplacedBySessionID = &parsedReplaced
	}
	return session, nil
}

func (r *MySQLRepository) Rotate(ctx context.Context, oldID uuid.UUID, replacement model.RefreshSession, at time.Time) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin session rotation: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE refresh_sessions
		SET revoked_at = ?, revoke_reason = 'ROTATED', replaced_by_session_id = ?
		WHERE id = ? AND revoked_at IS NULL AND expires_at > ?`,
		at, replacement.ID.String(), oldID.String(), at)
	if err != nil {
		return fmt.Errorf("revoke rotated session: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check rotated session: %w", err)
	}
	if affected != 1 {
		return ErrConflict
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO refresh_sessions
			(id, user_id, token_hash, user_agent, ip_address, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		replacement.ID.String(), replacement.UserID.String(),
		replacement.TokenHash, replacement.UserAgent, replacement.IPAddress, replacement.ExpiresAt)
	if err != nil {
		return mysqlError("insert rotated session", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session rotation: %w", err)
	}
	return nil
}

func (r *MySQLRepository) Revoke(ctx context.Context, id uuid.UUID, reason string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE refresh_sessions SET revoked_at = COALESCE(revoked_at, ?), revoke_reason = ?
		WHERE id = ?`, at, reason, id.String())
	return mysqlError("revoke refresh session", err)
}

func (r *MySQLRepository) RevokeAllForUser(ctx context.Context, userID uuid.UUID, reason string, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE refresh_sessions SET revoked_at = ?, revoke_reason = ?
		WHERE user_id = ? AND revoked_at IS NULL`, at, reason, userID.String())
	return mysqlError("revoke user sessions", err)
}

func mysqlError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return ErrConflict
	}
	return fmt.Errorf("%s: %w", operation, err)
}
