package accounts

import (
	"database/sql"
	"fmt"
	"time"
)

// Status represents the lifecycle state of a hosting account.
type Status string

const (
	StatusPending    Status = "pending"
	StatusActive     Status = "active"
	StatusSuspended  Status = "suspended"
	StatusTerminated Status = "terminated"
)

// Account represents a hosting account (one child panel instance).
type Account struct {
	ID               int       `json:"id"`
	Username         string    `json:"username"`
	Domain           string    `json:"domain"`
	Email            string    `json:"email"`
	PasswordHash     string    `json:"-"` // never serialize
	PackageID        int       `json:"package_id"`
	PackageName      string    `json:"package_name,omitempty"`
	ResellerID       *int      `json:"reseller_id"`
	Status           Status    `json:"status"`
	HomeDir          string    `json:"home_dir"`
	IPAddress        string    `json:"ip_address,omitempty"`
	DiskUsedMB       int       `json:"disk_used_mb"`
	BandwidthUsedMB  int       `json:"bandwidth_used_mb"`
	RamUsedMB        int       `json:"ram_used_mb"`
	SuspendedReason  string    `json:"suspended_reason,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CreateAccountRequest struct {
	Username    string `json:"username"`
	Domain      string `json:"domain"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	PackageID   int    `json:"package_id"`
	ResellerID  *int   `json:"reseller_id"`
	IPAddress   string `json:"ip_address"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) List(status string, offset, limit int) ([]Account, int, error) {
	var total int
	countQuery := "SELECT COUNT(*) FROM accounts"
	args := []interface{}{}
	if status != "" {
		countQuery += " WHERE status = ?"
		args = append(args, status)
	}
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count accounts: %w", err)
	}

	query := `SELECT a.id, a.username, a.domain, a.email, a.password_hash, a.package_id,
		p.name, a.reseller_id, a.status, a.home_dir, a.ip_address,
		a.disk_used_mb, a.bandwidth_used_mb, a.ram_used_mb, a.suspended_reason, a.created_at, a.updated_at
		FROM accounts a JOIN packages p ON a.package_id = p.id`
	queryArgs := []interface{}{}
	if status != "" {
		query += " WHERE a.status = ?"
		queryArgs = append(queryArgs, status)
	}
	query += " ORDER BY a.id DESC LIMIT ? OFFSET ?"
	queryArgs = append(queryArgs, limit, offset)

	rows, err := s.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		var reason sql.NullString
		var ip sql.NullString
		var rid sql.NullInt64
		if err := rows.Scan(&a.ID, &a.Username, &a.Domain, &a.Email, &a.PasswordHash,
			&a.PackageID, &a.PackageName, &rid, &a.Status, &a.HomeDir, &ip,
			&a.DiskUsedMB, &a.BandwidthUsedMB, &a.RamUsedMB, &reason, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan account: %w", err)
		}
		if reason.Valid {
			a.SuspendedReason = reason.String
		}
		if ip.Valid {
			a.IPAddress = ip.String
		}
		if rid.Valid {
			v := int(rid.Int64)
			a.ResellerID = &v
		}
		accounts = append(accounts, a)
	}
	return accounts, total, rows.Err()
}

func (s *Store) GetByID(id int) (*Account, error) {
	var a Account
	var reason sql.NullString
	var ip sql.NullString
	var rid sql.NullInt64
	err := s.db.QueryRow(`SELECT a.id, a.username, a.domain, a.email, a.password_hash, a.package_id,
		p.name, a.reseller_id, a.status, a.home_dir, a.ip_address,
		a.disk_used_mb, a.bandwidth_used_mb, a.ram_used_mb, a.suspended_reason, a.created_at, a.updated_at
		FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.id = ?`, id).Scan(
		&a.ID, &a.Username, &a.Domain, &a.Email, &a.PasswordHash,
		&a.PackageID, &a.PackageName, &rid, &a.Status, &a.HomeDir, &ip,
		&a.DiskUsedMB, &a.BandwidthUsedMB, &a.RamUsedMB, &reason, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	if reason.Valid {
		a.SuspendedReason = reason.String
	}
	if ip.Valid {
		a.IPAddress = ip.String
	}
	if rid.Valid {
		v := int(rid.Int64)
		a.ResellerID = &v
	}
	return &a, nil
}

func (s *Store) GetByUsername(username string) (*Account, error) {
	var a Account
	var reason sql.NullString
	var ip sql.NullString
	var rid sql.NullInt64
	err := s.db.QueryRow(`SELECT a.id, a.username, a.domain, a.email, a.password_hash, a.package_id,
		p.name, a.reseller_id, a.status, a.home_dir, a.ip_address,
		a.disk_used_mb, a.bandwidth_used_mb, a.ram_used_mb, a.suspended_reason, a.created_at, a.updated_at
		FROM accounts a JOIN packages p ON a.package_id = p.id WHERE a.username = ?`, username).Scan(
		&a.ID, &a.Username, &a.Domain, &a.Email, &a.PasswordHash,
		&a.PackageID, &a.PackageName, &rid, &a.Status, &a.HomeDir, &ip,
		&a.DiskUsedMB, &a.BandwidthUsedMB, &a.RamUsedMB, &reason, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get account by username: %w", err)
	}
	if reason.Valid {
		a.SuspendedReason = reason.String
	}
	if ip.Valid {
		a.IPAddress = ip.String
	}
	if rid.Valid {
		v := int(rid.Int64)
		a.ResellerID = &v
	}
	return &a, nil
}

func (s *Store) Create(req *CreateAccountRequest, passwordHash, homeDir string) (*Account, error) {
	result, err := s.db.Exec(`INSERT INTO accounts (username, domain, email, password_hash,
		package_id, reseller_id, home_dir, ip_address, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
		req.Username, req.Domain, req.Email, passwordHash, req.PackageID,
		req.ResellerID, homeDir, req.IPAddress)
	if err != nil {
		return nil, fmt.Errorf("create account: %w", err)
	}
	id, _ := result.LastInsertId()
	return s.GetByID(int(id))
}

func (s *Store) UpdateStatus(id int, status Status, reason string) error {
	_, err := s.db.Exec("UPDATE accounts SET status = ?, suspended_reason = ? WHERE id = ?",
		string(status), reason, id)
	return err
}

func (s *Store) UpdatePassword(id int, passwordHash string) error {
	_, err := s.db.Exec("UPDATE accounts SET password_hash = ? WHERE id = ?", passwordHash, id)
	return err
}

func (s *Store) UpdateDiskUsage(id int, mb int) error {
	_, err := s.db.Exec("UPDATE accounts SET disk_used_mb = ? WHERE id = ?", mb, id)
	return err
}

func (s *Store) UpdateBandwidthUsage(id int, mb int) error {
	_, err := s.db.Exec("UPDATE accounts SET bandwidth_used_mb = ? WHERE id = ?", mb, id)
	return err
}

func (s *Store) UpdateRAMUsage(id int, mb int) error {
	_, err := s.db.Exec("UPDATE accounts SET ram_used_mb = ? WHERE id = ?", mb, id)
	return err
}

func (s *Store) Delete(id int) error {
	_, err := s.db.Exec("DELETE FROM accounts WHERE id = ?", id)
	return err
}

// GetStats returns overview statistics for the dashboard.
func (s *Store) GetStats() (map[string]int, error) {
	stats := make(map[string]int)
	var count int

	if err := s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'active'").Scan(&count); err != nil {
		return nil, err
	}
	stats["active_accounts"] = count

	if err := s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'suspended'").Scan(&count); err != nil {
		return nil, err
	}
	stats["suspended_accounts"] = count

	if err := s.db.QueryRow("SELECT COUNT(*) FROM accounts WHERE status = 'pending'").Scan(&count); err != nil {
		return nil, err
	}
	stats["pending_accounts"] = count

	if err := s.db.QueryRow("SELECT COALESCE(SUM(disk_used_mb), 0) FROM accounts").Scan(&count); err != nil {
		return nil, err
	}
	stats["total_disk_used_mb"] = count

	if err := s.db.QueryRow("SELECT COALESCE(SUM(bandwidth_used_mb), 0) FROM accounts").Scan(&count); err != nil {
		return nil, err
	}
	stats["total_bandwidth_used_mb"] = count

	if err := s.db.QueryRow("SELECT COALESCE(SUM(ram_used_mb), 0) FROM accounts").Scan(&count); err != nil {
		return nil, err
	}
	stats["total_ram_used_mb"] = count

	return stats, nil
}
