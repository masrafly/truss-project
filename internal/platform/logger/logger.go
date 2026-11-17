// Package logger membuat logger terstruktur (log/slog).
//
// development -> teks berwarna-sederhana, mudah dibaca di terminal
// production  -> JSON, mudah diproses oleh log aggregator
//
// ATURAN: jangan pernah mencatat password, hash password, token JWT,
// header Authorization, atau body request.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

func New(env, level string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var h slog.Handler
	if strings.EqualFold(env, "production") {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(h).With("service", "truss")
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
