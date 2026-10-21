package store

import (
	"context"
	"time"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	Role         string
}

func InsertUser(ctx context.Context, q DBTX, email, passwordHash, role string) (User, error) {
	var user User
	err := q.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role)
		VALUES ($1, $2, $3::user_role)
		RETURNING id::text, email, password_hash, role::text
	`, email, passwordHash, role).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role)
	if isUnique(err) {
		return User{}, errUnique("email")
	}
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func GetUserByEmail(ctx context.Context, q DBTX, email string) (User, error) {
	var user User
	err := q.QueryRow(ctx, `
		SELECT id::text, email, password_hash, role::text
		FROM users
		WHERE email = $1
	`, email).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role)
	if isNoRows(err) {
		return User{}, ErrNotFound
	}
	return user, err
}

func GetUserByID(ctx context.Context, q DBTX, id string) (User, error) {
	var user User
	err := q.QueryRow(ctx, `
		SELECT id::text, email, password_hash, role::text
		FROM users
		WHERE id = $1::uuid
	`, id).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Role)
	if isNoRows(err) {
		return User{}, ErrNotFound
	}
	return user, err
}

func InsertRefresh(ctx context.Context, q DBTX, jti, userID string, expiresAt time.Time) error {
	_, err := q.Exec(ctx, `
		INSERT INTO refresh_tokens (jti, user_id, expires_at)
		VALUES ($1::uuid, $2::uuid, $3)
	`, jti, userID, expiresAt)
	return err
}

func ConsumeRefresh(ctx context.Context, q DBTX, jti, userID string) error {
	var got string
	err := q.QueryRow(ctx, `
		DELETE FROM refresh_tokens
		WHERE jti = $1::uuid AND user_id = $2::uuid AND expires_at > now()
		RETURNING jti::text
	`, jti, userID).Scan(&got)
	if isNoRows(err) {
		return ErrNotFound
	}
	return err
}

type uniqueError struct{ field string }

func (e uniqueError) Error() string { return e.field + " already exists" }

func errUnique(field string) error { return uniqueError{field: field} }

func IsUnique(err error) (string, bool) {
	var target uniqueError
	if err != nil && asUnique(err, &target) {
		return target.field, true
	}
	return "", false
}

func asUnique(err error, target *uniqueError) bool {
	var current uniqueError
	if ok := errorAs(err, &current); ok {
		*target = current
		return true
	}
	return false
}
