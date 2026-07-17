package api

import (
	"database/sql"
	"fmt"
	"time"
)

type Admin struct {
	ID           int        `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	TOTPSecret   *string    `json:"-"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

type AdminStore struct {
	db *sql.DB
}

func NewAdminStore(db *sql.DB) *AdminStore {
	return &AdminStore{db: db}
}

func (s *AdminStore) GetByUsername(username string) (*Admin, error) {
	var a Admin
	var totp sql.NullString
	var lastLogin sql.NullTime
	err := s.db.QueryRow(`SELECT id, username, password_hash, role, totp_secret, last_login_at, created_at
		FROM admins WHERE username = ?`, username).Scan(
		&a.ID, &a.Username, &a.PasswordHash, &a.Role, &totp, &lastLogin, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get admin: %w", err)
	}
	if totp.Valid {
		a.TOTPSecret = &totp.String
	}
	if lastLogin.Valid {
		a.LastLoginAt = &lastLogin.Time
	}
	return &a, nil
}

func (s *AdminStore) UpdateLastLogin(id int) error {
	_, err := s.db.Exec("UPDATE admins SET last_login_at = ? WHERE id = ?", time.Now().Format("2006-01-02 15:04:05"), id)
	return err
}
