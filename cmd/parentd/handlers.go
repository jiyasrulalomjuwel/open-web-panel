package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/audit"
	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/middleware"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/retry"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/validator"
)

func writeAppError(w http.ResponseWriter, r *http.Request, appErr error) {
	var ae *apperrors.AppError
	var ok bool
	if ae, ok = appErr.(*apperrors.AppError); !ok {
		ae = apperrors.Internal("an unexpected error occurred", appErr)
	}
	l := logging.FromContext(r.Context())
	if l != nil {
		l.LogAppError(ae, nil)
	}
	middleware.WriteError(w, r, ae)
}

func decodeJSON(r *http.Request, v interface{}) *apperrors.AppError {
	if r.Body == nil {
		return apperrors.BadRequest("request body is required")
	}
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		if syntaxErr, ok := err.(*json.SyntaxError); ok {
			return apperrors.Validation(fmt.Sprintf("invalid JSON at position %d", syntaxErr.Offset))
		}
		return apperrors.BadRequest("invalid request body: " + err.Error())
	}
	return nil
}

func decodeJSONStrict(r *http.Request, v interface{}) *apperrors.AppError {
	if r.Body == nil {
		return apperrors.BadRequest("request body is required")
	}
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperrors.Validation("invalid request body: " + err.Error())
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return apperrors.Validation("unexpected fields in request body")
	}
	return nil
}

func paramInt(r *http.Request, name string) (int, error) {
	val := chi.URLParam(r, name)
	if val == "" {
		return 0, apperrors.Validation(fmt.Sprintf("path parameter '%s' is required", name))
	}
	id, err := strconv.Atoi(val)
	if err != nil {
		return 0, apperrors.Validation(fmt.Sprintf("'%s' must be a valid integer", name))
	}
	return id, nil
}

func queryInt(r *http.Request, name string, defaultVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	id, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return id
}

func queryString(r *http.Request, name string, defaultVal string) string {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	return val
}

func validateRequest(r *http.Request, data map[string]interface{}, spec *validator.ValidationSpec) (*apperrors.AppError, map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			return apperrors.BadRequest("invalid request body"), nil
		}
		defer r.Body.Close()
	}
	if err := spec.Validate(data); err != nil {
		return err, nil
	}
	return nil, data
}

func withDBRetry(operation func() error) error {
	return retry.WithDefaultDBRetry(operation)
}

func withTransaction(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return apperrors.Database(err)
	}
	defer tx.Rollback()

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func withTransactionRetry(db *sql.DB, fn func(tx *sql.Tx) error) error {
	return retry.WithDefaultDBRetry(func() error {
		return withTransaction(db, fn)
	})
}

func auditLog(db *sql.DB, r *http.Request, action string, details interface{}) {
	ip := getClientIP(r)
	detailsJSON := "{}"
	if details != nil {
		if b, err := json.Marshal(details); err == nil {
			detailsJSON = string(b)
		}
	}
	targetType := strings.SplitN(action, ".", 2)[0]
	if targetType == "" {
		targetType = action
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	actorType := audit.ActorAdmin
	actorID := getAdminID(r)
	if c := getClaims(r); c != nil && c.Scope == "child" {
		actorType = audit.ActorAccount
		actorID = c.AccountID
	}
	if _, err := db.Exec(`INSERT INTO audit_log (actor_type, actor_id, action, target_type, target_id, details, ip_address, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(actorType), actorID, action, targetType, 0, detailsJSON, ip, now); err != nil {
		l := logging.FromContext(r.Context())
		if l != nil {
			l.Errorf("failed to write audit log: %v", err)
		}
	}
}

func getAdminID(r *http.Request) int {
	c := getClaims(r)
	if c != nil {
		return c.UserID
	}
	return 0
}

func getChildAccountID(r *http.Request) int {
	c := getClaims(r)
	if c != nil {
		return c.AccountID
	}
	return 0
}

func validateAndDecode(r *http.Request, req interface{}, spec *validator.ValidationSpec) *apperrors.AppError {
	if err := decodeJSON(r, req); err != nil {
		return err
	}
	data, err := toMap(req)
	if err != nil {
		return apperrors.Internal("failed to process request data", err)
	}
	return spec.Validate(data)
}

func toMap(v interface{}) (map[string]interface{}, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func handleAppErr(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if appErr, ok := err.(*apperrors.AppError); ok {
		writeAppError(w, r, appErr)
		return true
	}
	writeAppError(w, r, apperrors.Internal("an unexpected error occurred", err))
	return true
}

func getLogger(r *http.Request) *logging.Logger {
	l := logging.FromContext(r.Context())
	if l == nil {
		return logging.NewDefault("parentd")
	}
	return l
}

func addField(fields []apperrors.FieldError, field string, reason string) []apperrors.FieldError {
	return append(fields, apperrors.FieldError{Field: field, Reason: reason})
}

func getRAMLimit(db *sql.DB, accountID int) int {
	var pkgRAM, accRAM int
	db.QueryRow("SELECT COALESCE(ram_limit_mb, 0) FROM packages p JOIN accounts a ON a.package_id = p.id WHERE a.id = ?", accountID).Scan(&pkgRAM)
	db.QueryRow("SELECT COALESCE(ram_limit_mb, 0) FROM accounts WHERE id = ?", accountID).Scan(&accRAM)
	if accRAM > 0 {
		return accRAM
	}
	return pkgRAM
}
