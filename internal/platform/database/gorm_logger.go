package database

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const slowQueryThreshold = 200 * time.Millisecond

// gormSlogLogger menyalurkan log GORM ke slog.
// Hanya mencatat error dan query lambat; "record not found" diabaikan karena
// itu kasus normal (mis. cek email sebelum register).
// ParamsFilter (di bawah) membuat nilai (mis. password hash) tidak pernah
// muncul di SQL yang dicatat.
type gormSlogLogger struct {
	log   *slog.Logger
	level gormlogger.LogLevel
}

func newGormLogger(log *slog.Logger) gormlogger.Interface {
	return &gormSlogLogger{log: log.With("component", "gorm"), level: gormlogger.Warn}
}

// ParamsFilter dipanggil GORM sebelum SQL dicatat. Dengan membuang params,
// log hanya berisi placeholder ($1, $2, ...) sehingga nilai seperti password
// hash tidak pernah bocor ke log.
func (l *gormSlogLogger) ParamsFilter(ctx context.Context, sql string, params ...any) (string, []any) {
	return sql, nil
}

func (l *gormSlogLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	c := *l
	c.level = level
	return &c
}

func (l *gormSlogLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Info {
		l.log.InfoContext(ctx, msg)
	}
}

func (l *gormSlogLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Warn {
		l.log.WarnContext(ctx, msg)
	}
}

func (l *gormSlogLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= gormlogger.Error {
		l.log.ErrorContext(ctx, msg)
	}
}

func (l *gormSlogLogger) Trace(
	ctx context.Context,
	begin time.Time,
	fc func() (string, int64),
	err error,
) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)

	switch {
	case err != nil &&
		!errors.Is(err, gorm.ErrRecordNotFound) &&
		l.level >= gormlogger.Error:
		sql, rows := fc()
		l.log.ErrorContext(ctx, "query database gagal",
			"error", err, "sql", sql, "rows", rows, "elapsed_ms", elapsed.Milliseconds())

	case elapsed > slowQueryThreshold && l.level >= gormlogger.Warn:
		sql, rows := fc()
		l.log.WarnContext(ctx, "query database lambat",
			"sql", sql, "rows", rows, "elapsed_ms", elapsed.Milliseconds())

	case l.level >= gormlogger.Info:
		sql, rows := fc()
		l.log.DebugContext(ctx, "query database",
			"sql", sql, "rows", rows, "elapsed_ms", elapsed.Milliseconds())
	}
}
