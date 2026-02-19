package obs

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

type Logger struct {
	mu sync.Mutex
	l  *log.Logger
}

type LogEvent struct {
	TS        string `json:"ts"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Handler   string `json:"handler,omitempty"`
	Result    string `json:"result,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

var defaultLogger = NewLogger(os.Stdout)

func NewLogger(w io.Writer) *Logger {
	return &Logger{l: log.New(w, "", 0)}
}

func DefaultLogger() *Logger {
	return defaultLogger
}

func (l *Logger) InfoHandler(handler string, result string, requestID string) {
	l.log(LogEvent{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		Level:     "info",
		Message:   "handler_request",
		Handler:   handler,
		Result:    result,
		RequestID: requestID,
	})
}

func (l *Logger) ErrorHandler(handler string, result string, requestID string, err string) {
	l.log(LogEvent{
		TS:        time.Now().UTC().Format(time.RFC3339Nano),
		Level:     "error",
		Message:   "handler_request",
		Handler:   handler,
		Result:    result,
		RequestID: requestID,
		Error:     err,
	})
}

func (l *Logger) log(event LogEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()

	payload, err := json.Marshal(event)
	if err != nil {
		l.l.Printf("{\"level\":\"error\",\"message\":\"logger_marshal_failed\"}")
		return
	}
	l.l.Print(string(payload))
}
