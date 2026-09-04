package errors

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
)

type ErrorCode string

const (
	ErrNotFound            ErrorCode = "NOT_FOUND"
	ErrValidation          ErrorCode = "VALIDATION_ERROR"
	ErrUnauthorized        ErrorCode = "UNAUTHORIZED"
	ErrForbidden           ErrorCode = "FORBIDDEN"
	ErrInternal            ErrorCode = "INTERNAL_ERROR"
	ErrDatabase            ErrorCode = "DATABASE_ERROR"
	ErrDocker              ErrorCode = "DOCKER_ERROR"
	ErrFilesystem          ErrorCode = "FILESYSTEM_ERROR"
	ErrRateLimit           ErrorCode = "RATE_LIMIT"
	ErrConflict            ErrorCode = "CONFLICT"
	ErrTimeout             ErrorCode = "TIMEOUT"
	ErrDuplicate           ErrorCode = "DUPLICATE_ENTRY"
	ErrInvalidInput        ErrorCode = "INVALID_INPUT"
	ErrExternalAPI         ErrorCode = "EXTERNAL_API_ERROR"
	ErrNotImplemented      ErrorCode = "NOT_IMPLEMENTED"
	ErrServiceUnavailable  ErrorCode = "SERVICE_UNAVAILABLE"
	ErrBadGateway          ErrorCode = "BAD_GATEWAY"
	ErrCaptcha             ErrorCode = "CAPTCHA_ERROR"
	ErrSMTP                ErrorCode = "SMTP_ERROR"
	ErrACME                ErrorCode = "ACME_ERROR"
	ErrBackup              ErrorCode = "BACKUP_ERROR"
	ErrCMS                 ErrorCode = "CMS_ERROR"
	ErrBadRequest          ErrorCode = "BAD_REQUEST"
	ErrUpload              ErrorCode = "UPLOAD_ERROR"
	ErrQuotaExceeded       ErrorCode = "QUOTA_EXCEEDED"
	ErrMaintenance         ErrorCode = "MAINTENANCE_MODE"
	ErrTokenExpired        ErrorCode = "TOKEN_EXPIRED"
	ErrTokenInvalid        ErrorCode = "TOKEN_INVALID"
)

var httpStatusMap = map[ErrorCode]int{
	ErrNotFound:           404,
	ErrValidation:         422,
	ErrUnauthorized:       401,
	ErrForbidden:          403,
	ErrInternal:           500,
	ErrDatabase:           500,
	ErrDocker:             502,
	ErrFilesystem:         500,
	ErrRateLimit:          429,
	ErrConflict:           409,
	ErrTimeout:            504,
	ErrDuplicate:          409,
	ErrInvalidInput:       400,
	ErrExternalAPI:        502,
	ErrNotImplemented:     501,
	ErrServiceUnavailable: 503,
	ErrBadGateway:         502,
	ErrCaptcha:            400,
	ErrSMTP:               502,
	ErrACME:               502,
	ErrBackup:             500,
	ErrCMS:                500,
	ErrBadRequest:         400,
	ErrUpload:             500,
	ErrQuotaExceeded:      413,
	ErrMaintenance:        503,
	ErrTokenExpired:       401,
	ErrTokenInvalid:       401,
}

func HTTPStatus(code ErrorCode) int {
	if s, ok := httpStatusMap[code]; ok {
		return s
	}
	return 500
}

type Severity string

const (
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

type FieldError struct {
	Field  string      `json:"field"`
	Reason string      `json:"reason"`
	Value  interface{} `json:"value,omitempty"`
}

type AppError struct {
	Code      ErrorCode              `json:"code"`
	Message   string                 `json:"message"`
	Details   interface{}            `json:"details,omitempty"`
	Fields    []FieldError           `json:"fields,omitempty"`
	Severity  Severity               `json:"-"`
	Err       error                  `json:"-"`
	Stack     []string               `json:"-"`
	Retryable bool                   `json:"-"`
	Context   map[string]interface{} `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func (e *AppError) WithContext(key string, value interface{}) *AppError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

func (e *AppError) WithField(field, reason string, value interface{}) *AppError {
	e.Fields = append(e.Fields, FieldError{Field: field, Reason: reason, Value: value})
	return e
}

func (e *AppError) AsRetryable() *AppError {
	e.Retryable = true
	return e
}

func (e *AppError) MarshalJSON() ([]byte, error) {
	type Alias AppError
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(e),
	})
}

func captureStack(skip int) []string {
	var st []string
	for i := skip; i < skip+20; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		fn := runtime.FuncForPC(pc)
		if fn == nil {
			break
		}
		if strings.Contains(file, "runtime/") {
			continue
		}
		st = append(st, fmt.Sprintf("%s:%d %s", file, line, fn.Name()))
	}
	return st
}

func NotFound(resource string, id interface{}) *AppError {
	return &AppError{
		Code:     ErrNotFound,
		Message:  fmt.Sprintf("%s not found", resource),
		Details:  map[string]interface{}{"resource": resource, "id": id},
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}

func Validation(message string, fields ...FieldError) *AppError {
	return &AppError{
		Code:     ErrValidation,
		Message:  message,
		Fields:   fields,
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}

func Unauthorized(message string) *AppError {
	if message == "" {
		message = "authentication required"
	}
	return &AppError{
		Code:     ErrUnauthorized,
		Message:  message,
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}

func Forbidden(message string) *AppError {
	if message == "" {
		message = "insufficient permissions"
	}
	return &AppError{
		Code:     ErrForbidden,
		Message:  message,
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}

func Internal(message string, err error) *AppError {
	return &AppError{
		Code:      ErrInternal,
		Message:   message,
		Err:       err,
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func Database(err error) *AppError {
	return &AppError{
		Code:      ErrDatabase,
		Message:   "database operation failed",
		Err:       err,
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func Docker(err error) *AppError {
	return &AppError{
		Code:      ErrDocker,
		Message:   "container operation failed",
		Err:       err,
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func Filesystem(err error) *AppError {
	return &AppError{
		Code:      ErrFilesystem,
		Message:   "file system operation failed",
		Err:       err,
		Severity:  SeverityError,
		Retryable: false,
		Stack:     captureStack(2),
	}
}

func Conflict(message string) *AppError {
	return &AppError{
		Code:     ErrConflict,
		Message:  message,
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}

func Duplicate(resource, value string) *AppError {
	return &AppError{
		Code:    ErrDuplicate,
		Message: fmt.Sprintf("%s '%s' already exists", resource, value),
		Details: map[string]string{"resource": resource, "value": value},
		Severity: SeverityWarning,
		Stack:   captureStack(2),
	}
}

func RateLimit(retryAfter int) *AppError {
	return &AppError{
		Code:      ErrRateLimit,
		Message:   "too many requests",
		Details:   map[string]int{"retry_after_seconds": retryAfter},
		Severity:  SeverityWarning,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func Timeout(operation string) *AppError {
	return &AppError{
		Code:      ErrTimeout,
		Message:   fmt.Sprintf("operation timed out: %s", operation),
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func BadRequest(message string) *AppError {
	return &AppError{
		Code:     ErrBadRequest,
		Message:  message,
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}

func ServiceUnavailable(message string) *AppError {
	return &AppError{
		Code:      ErrServiceUnavailable,
		Message:   message,
		Severity:  SeverityCritical,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func ExternalAPI(service string, err error) *AppError {
	return &AppError{
		Code:      ErrExternalAPI,
		Message:   fmt.Sprintf("external service '%s' failed", service),
		Details:   map[string]string{"service": service},
		Err:       err,
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func SMTP(err error) *AppError {
	return &AppError{
		Code:      ErrSMTP,
		Message:   "email delivery failed",
		Err:       err,
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func ACME(err error) *AppError {
	return &AppError{
		Code:      ErrACME,
		Message:   "SSL certificate operation failed",
		Err:       err,
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func Backup(err error) *AppError {
	return &AppError{
		Code:      ErrBackup,
		Message:   "backup operation failed",
		Err:       err,
		Severity:  SeverityError,
		Retryable: true,
		Stack:     captureStack(2),
	}
}

func CMS(err error) *AppError {
	return &AppError{
		Code:      ErrCMS,
		Message:   "CMS operation failed",
		Err:       err,
		Severity:  SeverityError,
		Retryable: false,
		Stack:     captureStack(2),
	}
}

func QuotaExceeded(resource string, limit int64) *AppError {
	return &AppError{
		Code:     ErrQuotaExceeded,
		Message:  fmt.Sprintf("%s quota exceeded (limit: %d)", resource, limit),
		Details:  map[string]interface{}{"resource": resource, "limit": limit},
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}

func TokenExpired() *AppError {
	return &AppError{
		Code:     ErrTokenExpired,
		Message:  "token has expired",
		Severity: SeverityInfo,
		Stack:    captureStack(2),
	}
}

func TokenInvalid(message string) *AppError {
	if message == "" {
		message = "token is invalid"
	}
	return &AppError{
		Code:     ErrTokenInvalid,
		Message:  message,
		Severity: SeverityWarning,
		Stack:    captureStack(2),
	}
}
