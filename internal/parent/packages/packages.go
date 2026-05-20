package packages

import (
	"database/sql"
	"fmt"
	"time"
)

// Package represents a hosting plan template.
type Package struct {
	ID             int       `json:"id"`
	Name           string    `json:"name"`
	DiskMB         int       `json:"disk_mb"`
	BandwidthMB    int       `json:"bandwidth_mb"`
	MaxDB          int       `json:"max_db"`
	MaxEmail       int       `json:"max_email"`
	MaxFTP         int       `json:"max_ftp"`
	MaxDomains     int       `json:"max_domains"`
	MaxSubdomains  int       `json:"max_subdomains"`
	SSHAccess      bool      `json:"ssh_access"`
	BackupEnabled  bool      `json:"backup_enabled"`
	IsDefault      bool      `json:"is_default"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreatePackageRequest struct {
	Name          string `json:"name"`
	DiskMB        int    `json:"disk_mb"`
	BandwidthMB   int    `json:"bandwidth_mb"`
	MaxDB         int    `json:"max_db"`
	MaxEmail      int    `json:"max_email"`
	MaxFTP        int    `json:"max_ftp"`
	MaxDomains    int    `json:"max_domains"`
	MaxSubdomains int    `json:"max_subdomains"`
	SSHAccess     bool   `json:"ssh_access"`
	BackupEnabled *bool  `json:"backup_enabled"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) List() ([]Package, error) {
	rows, err := s.db.Query(`SELECT id, name, disk_mb, bandwidth_mb, max_db, max_email, max_ftp,
		max_domains, max_subdomains, ssh_access, backup_enabled, is_default, created_at, updated_at
		FROM packages ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	defer rows.Close()

	var pkgs []Package
	for rows.Next() {
		var p Package
		if err := rows.Scan(&p.ID, &p.Name, &p.DiskMB, &p.BandwidthMB, &p.MaxDB,
			&p.MaxEmail, &p.MaxFTP, &p.MaxDomains, &p.MaxSubdomains, &p.SSHAccess,
			&p.BackupEnabled, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan package: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, rows.Err()
}

func (s *Store) GetByID(id int) (*Package, error) {
	var p Package
	err := s.db.QueryRow(`SELECT id, name, disk_mb, bandwidth_mb, max_db, max_email, max_ftp,
		max_domains, max_subdomains, ssh_access, backup_enabled, is_default, created_at, updated_at
		FROM packages WHERE id = ?`, id).Scan(
		&p.ID, &p.Name, &p.DiskMB, &p.BandwidthMB, &p.MaxDB,
		&p.MaxEmail, &p.MaxFTP, &p.MaxDomains, &p.MaxSubdomains, &p.SSHAccess,
		&p.BackupEnabled, &p.IsDefault, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get package: %w", err)
	}
	return &p, nil
}

func (s *Store) Create(req *CreatePackageRequest) (*Package, error) {
	backupEnabled := true
	if req.BackupEnabled != nil {
		backupEnabled = *req.BackupEnabled
	}
	result, err := s.db.Exec(`INSERT INTO packages (name, disk_mb, bandwidth_mb, max_db, max_email,
		max_ftp, max_domains, max_subdomains, ssh_access, backup_enabled)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.Name, req.DiskMB, req.BandwidthMB, req.MaxDB, req.MaxEmail,
		req.MaxFTP, req.MaxDomains, req.MaxSubdomains, req.SSHAccess, backupEnabled)
	if err != nil {
		return nil, fmt.Errorf("create package: %w", err)
	}
	id, _ := result.LastInsertId()
	return s.GetByID(int(id))
}

func (s *Store) Update(id int, req *CreatePackageRequest) (*Package, error) {
	_, err := s.db.Exec(`UPDATE packages SET name=?, disk_mb=?, bandwidth_mb=?, max_db=?,
		max_email=?, max_ftp=?, max_domains=?, max_subdomains=?, ssh_access=?,
		backup_enabled=? WHERE id=?`,
		req.Name, req.DiskMB, req.BandwidthMB, req.MaxDB, req.MaxEmail,
		req.MaxFTP, req.MaxDomains, req.MaxSubdomains, req.SSHAccess, req.BackupEnabled, id)
	if err != nil {
		return nil, fmt.Errorf("update package: %w", err)
	}
	return s.GetByID(id)
}

func (s *Store) Delete(id int) error {
	_, err := s.db.Exec("DELETE FROM packages WHERE id = ?", id)
	return err
}
