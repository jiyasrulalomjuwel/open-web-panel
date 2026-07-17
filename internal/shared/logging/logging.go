package logging

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// Entry is a structured log entry.
type Entry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Extra   map[string]interface{} `json:"extra,omitempty"`
}

// Logger wraps the standard logger with structured output.
type Logger struct {
	*log.Logger
}

// New creates a new structured logger.
func New() *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "", 0),
	}
}

func (l *Logger) log(level, msg string, extra map[string]interface{}) {
	e := Entry{
		Time:    time.Now().Format(time.RFC3339),
		Level:   level,
		Message: msg,
		Extra:   extra,
	}
	data, _ := json.Marshal(e)
	l.Logger.Println(string(data))
}

func (l *Logger) Info(msg string, extra map[string]interface{})  { l.log("info", msg, extra) }
func (l *Logger) Warn(msg string, extra map[string]interface{})  { l.log("warn", msg, extra) }
func (l *Logger) Error(msg string, extra map[string]interface{}) { l.log("error", msg, extra) }
func (l *Logger) Debug(msg string, extra map[string]interface{}) { l.log("debug", msg, extra) }
