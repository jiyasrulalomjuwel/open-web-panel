package audit

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

type ActorType string

const (
	ActorAdmin    ActorType = "admin"
	ActorReseller ActorType = "reseller"
	ActorAccount  ActorType = "account"
	ActorSystem   ActorType = "system"
)

type Logger struct {
	db *sql.DB
}

func New(db *sql.DB) *Logger {
	return &Logger{db: db}
}

func (l *Logger) Log(actorType ActorType, actorID int, action, targetType string, targetID int, details interface{}, ipAddress string) {
	detailsJSON := "{}"
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			detailsJSON = string(b)
		}
	}

	_, err := l.db.Exec(`INSERT INTO audit_log (actor_type, actor_id, action, target_type, target_id, details, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		string(actorType), actorID, action, targetType, targetID, detailsJSON, ipAddress)
	if err != nil {
		log.Printf("[AUDIT] Failed to log: %v", err)
	}
}

func (l *Logger) LogString(actorType ActorType, actorID int, action, target string, details interface{}, ipAddress string) {
	l.Log(actorType, actorID, action, target, 0, details, ipAddress)
}

func (l *Logger) Cleanup(olderThan time.Duration) {
	cutoff := time.Now().Add(-olderThan).Format("2006-01-02 15:04:05")
	result, err := l.db.Exec("DELETE FROM audit_log WHERE created_at < ?", cutoff)
	if err == nil {
		if n, _ := result.RowsAffected(); n > 0 {
			log.Printf("[AUDIT] Cleaned up %d old log entries", n)
		}
	}
}
