package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
)

type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelCritical
	LevelOff
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

func LevelFromString(s string) Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	case "CRITICAL", "FATAL":
		return LevelCritical
	case "OFF":
		return LevelOff
	default:
		return LevelInfo
	}
}

type Fields map[string]interface{}

type Entry struct {
	Time      string      `json:"time"`
	Level     string      `json:"level"`
	Message   string      `json:"message"`
	RequestID string      `json:"request_id,omitempty"`
	AccountID int         `json:"account_id,omitempty"`
	ActorID   int         `json:"actor_id,omitempty"`
	ActorType string      `json:"actor_type,omitempty"`
	ErrorCode string      `json:"error_code,omitempty"`
	Module    string      `json:"module,omitempty"`
	Operation string      `json:"operation,omitempty"`
	Duration  string      `json:"duration,omitempty"`
	Fields    Fields      `json:"fields,omitempty"`
	Stack     []string    `json:"stack,omitempty"`
	Extra     interface{} `json:"extra,omitempty"`
}

type Formatter interface {
	Format(entry *Entry) ([]byte, error)
}

type JSONFormatter struct {
	Pretty bool
}

func (f *JSONFormatter) Format(entry *Entry) ([]byte, error) {
	if f.Pretty {
		return json.MarshalIndent(entry, "", "  ")
	}
	return json.Marshal(entry)
}

type TextFormatter struct {
	Colored bool
}

func (f *TextFormatter) Format(entry *Entry) ([]byte, error) {
	msg := fmt.Sprintf("%s [%s] %s", entry.Time, entry.Level, entry.Message)
	if entry.RequestID != "" {
		msg = fmt.Sprintf("%s [req=%s]", msg, entry.RequestID)
	}
	if entry.Module != "" {
		msg = fmt.Sprintf("%s [mod=%s]", msg, entry.Module)
	}
	if entry.Duration != "" {
		msg = fmt.Sprintf("%s [dur=%s]", msg, entry.Duration)
	}
	if entry.ErrorCode != "" {
		msg = fmt.Sprintf("%s [code=%s]", msg, entry.ErrorCode)
	}
	if len(entry.Stack) > 0 {
		msg = fmt.Sprintf("%s\n%s", msg, strings.Join(entry.Stack, "\n"))
	}
	return []byte(msg + "\n"), nil
}

type Logger struct {
	mu          sync.Mutex
	level       Level
	formatter   Formatter
	out         io.Writer
	errOut      io.Writer
	module      string
	requestID   string
	accountID   int
	actorID     int
	actorType   string
	fields      Fields
}

type LoggerConfig struct {
	Level     string
	Format    string
	Output    io.Writer
	ErrorOut  io.Writer
	Module    string
	RequestID string
	AccountID int
	ActorID   int
	ActorType string
	Pretty    bool
}

func New(cfg LoggerConfig) *Logger {
	level := LevelInfo
	if cfg.Level != "" {
		level = LevelFromString(cfg.Level)
	}

	out := cfg.Output
	if out == nil {
		out = os.Stdout
	}
	errOut := cfg.ErrorOut
	if errOut == nil {
		errOut = os.Stderr
	}

	var formatter Formatter
	switch strings.ToLower(cfg.Format) {
	case "text":
		formatter = &TextFormatter{}
	default:
		formatter = &JSONFormatter{Pretty: cfg.Pretty}
	}

	return &Logger{
		level:     level,
		formatter: formatter,
		out:       out,
		errOut:    errOut,
		module:    cfg.Module,
		requestID: cfg.RequestID,
		accountID: cfg.AccountID,
		actorID:   cfg.ActorID,
		actorType: cfg.ActorType,
	}
}

func NewDefault(module string) *Logger {
	return New(LoggerConfig{
		Level:  "info",
		Format: "json",
		Module: module,
	})
}

func (l *Logger) clone() *Logger {
	return &Logger{
		level:     l.level,
		formatter: l.formatter,
		out:       l.out,
		errOut:    l.errOut,
		module:    l.module,
		requestID: l.requestID,
		accountID: l.accountID,
		actorID:   l.actorID,
		actorType: l.actorType,
		fields:    l.cloneWithFields(),
	}
}

func (l *Logger) WithModule(module string) *Logger {
	c := l.clone()
	c.module = module
	return c
}

func (l *Logger) WithRequestID(id string) *Logger {
	c := l.clone()
	c.requestID = id
	return c
}

func (l *Logger) WithAccountID(id int) *Logger {
	c := l.clone()
	c.accountID = id
	return c
}

func (l *Logger) WithActor(id int, actorType string) *Logger {
	c := l.clone()
	c.actorID = id
	c.actorType = actorType
	return c
}

func (l *Logger) WithFields(fields Fields) *Logger {
	c := l.clone()
	c.fields = fields
	return c
}

func (l *Logger) cloneWithFields() Fields {
	if l.fields == nil {
		return nil
	}
	f := make(Fields, len(l.fields))
	for k, v := range l.fields {
		f[k] = v
	}
	return f
}

func (l *Logger) log(level Level, msg string, extra Fields) {
	if level < l.level {
		return
	}

	merged := l.fields
	if len(extra) > 0 {
		merged = l.cloneWithFields()
		if merged == nil {
			merged = make(Fields)
		}
		for k, v := range extra {
			merged[k] = v
		}
	}

	entry := &Entry{
		Time:      time.Now().Format(time.RFC3339Nano),
		Level:     level.String(),
		Message:   msg,
		RequestID: l.requestID,
		AccountID: l.accountID,
		ActorID:   l.actorID,
		ActorType: l.actorType,
		Module:    l.module,
		Fields:    merged,
	}

	data, err := l.formatter.Format(entry)
	if err != nil {
		log.Printf("failed to format log entry: %v", err)
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if level >= LevelError {
		l.errOut.Write(data)
	} else {
		l.out.Write(data)
	}
}

func (l *Logger) Debug(msg string, extra Fields)  { l.log(LevelDebug, msg, extra) }
func (l *Logger) Info(msg string, extra Fields)   { l.log(LevelInfo, msg, extra) }
func (l *Logger) Warn(msg string, extra Fields)   { l.log(LevelWarn, msg, extra) }
func (l *Logger) Error(msg string, extra Fields)  { l.log(LevelError, msg, extra) }
func (l *Logger) Critical(msg string, extra Fields) { l.log(LevelCritical, msg, extra) }

func (l *Logger) Debugf(format string, args ...interface{})  { l.Debug(fmt.Sprintf(format, args...), nil) }
func (l *Logger) Infof(format string, args ...interface{})   { l.Info(fmt.Sprintf(format, args...), nil) }
func (l *Logger) Warnf(format string, args ...interface{})   { l.Warn(fmt.Sprintf(format, args...), nil) }
func (l *Logger) Errorf(format string, args ...interface{})  { l.Error(fmt.Sprintf(format, args...), nil) }

func (l *Logger) LogAppError(appErr *errors.AppError, extra Fields) {
	if extra == nil {
		extra = make(Fields)
	}
	if appErr.Context != nil {
		for k, v := range appErr.Context {
			extra[k] = v
		}
	}
	if appErr.Err != nil {
		extra["cause"] = appErr.Err.Error()
	}
	if len(appErr.Stack) > 0 {
		extra["stack"] = appErr.Stack
	}
	if appErr.Details != nil {
		extra["details"] = appErr.Details
	}
	if len(appErr.Fields) > 0 {
		extra["fields"] = appErr.Fields
	}

	entry := &Entry{
		Time:      time.Now().Format(time.RFC3339Nano),
		Level:     string(appErr.Severity),
		Message:   appErr.Message,
		RequestID: l.requestID,
		AccountID: l.accountID,
		ActorID:   l.actorID,
		ActorType: l.actorType,
		Module:    l.module,
		ErrorCode: string(appErr.Code),
		Extra:     extra,
		Stack:     appErr.Stack,
	}

	data, err := l.formatter.Format(entry)
	if err != nil {
		log.Printf("failed to format log entry: %v", err)
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	severity := LevelFromString(string(appErr.Severity))
	if severity >= LevelError {
		l.errOut.Write(data)
	} else {
		l.out.Write(data)
	}
}

func (l *Logger) LogError(err error, extra Fields) {
	if extra == nil {
		extra = make(Fields)
	}

	if appErr, ok := err.(*errors.AppError); ok {
		l.LogAppError(appErr, extra)
		return
	}

	entry := &Entry{
		Time:      time.Now().Format(time.RFC3339Nano),
		Level:     "ERROR",
		Message:   err.Error(),
		RequestID: l.requestID,
		AccountID: l.accountID,
		ActorID:   l.actorID,
		ActorType: l.actorType,
		Module:    l.module,
		Extra:     extra,
		Stack:     captureStack(2),
	}

	data, _ := l.formatter.Format(entry)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.errOut.Write(data)
}

func captureStack(skip int) []string {
	var st []string
	for i := skip; i < skip+15; i++ {
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

type contextKey string

const LoggerKey contextKey = "logger"

func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(LoggerKey).(*Logger); ok {
		return l
	}
	return NewDefault("unknown")
}

func WithLogger(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, LoggerKey, l)
}
