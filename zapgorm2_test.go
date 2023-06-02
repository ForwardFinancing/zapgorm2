package zapgorm2_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"moul.io/zapgorm2"
)

func Example() {
	logger := zapgorm2.New(zap.L())
	logger.SetAsDefault() // optional: configure gorm to use this zapgorm.Logger for callbacks
	db, _ := gorm.Open(nil, &gorm.Config{Logger: logger})

	// do stuff normally
	var _ = db // avoid "unused variable" warn
}

func setupLogsCapture() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	return zap.New(core), logs
}

func TestContextFunc(t *testing.T) {
	zaplogger, logs := setupLogsCapture()
	logger := zapgorm2.New(zaplogger)

	type ctxKey string
	key1 := ctxKey("Key")
	key2 := ctxKey("Key2")

	value1 := "Value"
	value2 := "Value2"

	ctx := context.WithValue(context.Background(), key1, value1)
	ctx = context.WithValue(ctx, key2, value2)
	logger.Context = func(ctx context.Context) []zapcore.Field {
		ctxValue, ok := (ctx.Value(key1)).(string)
		require.True(t, ok)
		ctxValue2, ok := (ctx.Value(key2)).(string)
		require.True(t, ok)
		return []zapcore.Field{zap.String(string(key1), ctxValue), zap.String(string(key2), ctxValue2)}
	}

	db, err := gorm.Open(nil, &gorm.Config{Logger: logger})
	require.NoError(t, err)

	db.Logger.Error(ctx, "test")
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	require.Equal(t, zap.ErrorLevel, entry.Level)
	require.Equal(t, "test", entry.Message)
	require.Equal(t, value1, entry.ContextMap()[string(key1)])
	require.Equal(t, value2, entry.ContextMap()[string(key2)])

}

func TestTraceFuncs(t *testing.T) {
	zaplogger, logs := setupLogsCapture()
	logger := zapgorm2.New(zaplogger)
	const sql = "select * from users"
	const rows = 35364 // random number

	cases := []struct {
		dur   int
		err   error
		msg   string
		level gormlogger.LogLevel
	}{
		{0, nil, "Trace", gormlogger.Info},                      // not slow and no error
		{0, errors.New("Gorm error"), "Error", gormlogger.Warn}, // not slow with error
		{10, nil, "Slow", gormlogger.Warn},                      // slow with no error
	}

	// test default trace message
	for _, c := range cases {
		logger.LogMode(c.level).Trace(context.Background(), time.Now().Add(time.Duration(-c.dur)*time.Second), func() (string, int64) { return sql, rows }, c.err)
		require.Equal(t, 1, logs.Len())
		testLog := logs.TakeAll()[0]
		require.Equal(t, testLog.Message, "trace")
	}

	// test trace message functions
	logger.TraceSlowQueryMsg = func(sql string, rows int64, duration time.Duration, file string, err error) string {
		return fmt.Sprintf("Slow %s %d %s %s %s", sql, rows, duration.Round(time.Second), file, err)
	}
	logger.TraceErrorMsg = func(sql string, rows int64, duration time.Duration, file string, err error) string {
		return fmt.Sprintf("Error %s %d %s %s %s", sql, rows, duration.Round(time.Second), file, err)
	}
	logger.TraceQueryMsg = func(sql string, rows int64, duration time.Duration, file string, err error) string {
		return fmt.Sprintf("Trace %s %d %s %s %s", sql, rows, duration.Round(time.Second), file, err)
	}

	for _, c := range cases {
		logger.LogMode(c.level).Trace(context.Background(), time.Now().Add(time.Duration(-c.dur)*time.Second), func() (string, int64) { return sql, rows }, c.err)
		require.Equal(t, 1, logs.Len())
		testLog := logs.TakeAll()[0]

		// check that the log message contains the expected values
		require.Contains(t, testLog.Message, sql)
		require.Contains(t, testLog.Message, strconv.Itoa(rows))
		require.Contains(t, testLog.Message, (time.Duration(c.dur) * time.Second).String())
		require.Contains(t, testLog.Message, c.msg)
		require.Contains(t, testLog.Message, "zapgorm2_test.go")
	}
}

func TestLogging(t *testing.T) {
	zaplogger, logs := setupLogsCapture()
	logger := zapgorm2.New(zaplogger)

	// test logger with level high enough to generate output
	cases := []struct {
		f func(ctx context.Context, str string, args ...interface{})
	}{
		{logger.LogMode(gormlogger.Info).Info},
		{logger.LogMode(gormlogger.Warn).Warn},
		{logger.LogMode(gormlogger.Error).Error},
	}
	for _, c := range cases {
		c.f(context.Background(), "test %d", 1)
		require.Equal(t, 1, logs.Len())
		testLog := logs.TakeAll()[0]
		require.Equal(t, testLog.Message, "test 1")
	}

	// test logger with level too low to generate output
	cases = []struct {
		f func(ctx context.Context, str string, args ...interface{})
	}{
		{logger.LogMode(gormlogger.Warn).Info},
		{logger.LogMode(gormlogger.Error).Warn},
		{logger.LogMode(gormlogger.Silent).Error},
	}
	for _, c := range cases {
		c.f(context.Background(), "test %d", 1)
		require.Equal(t, 0, logs.Len())
	}
}
