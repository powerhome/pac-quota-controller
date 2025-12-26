package logger

import (
	"os"
	"strings"
	"sync"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	globalLogger *zap.Logger
	once         sync.Once
)

// Initialize configures the global zap logger based on provided configuration
func Initialize(cfg *config.Config) {
	once.Do(func() {
		globalLogger = SetupLogger(cfg)
	})
}

// L returns the global logger instance. If not initialized, it returns a Nop logger.
func L() *zap.Logger {
	if globalLogger == nil {
		return zap.NewNop()
	}
	return globalLogger
}

// InitTest initializes the global logger for testing with a development configuration.
func InitTest() *zap.Logger {
	globalLogger = zap.Must(zap.NewDevelopment())
	return globalLogger
}

// SetupLogger configures a zap logger based on provided configuration (for non-global use if needed)
func SetupLogger(cfg *config.Config) *zap.Logger {
	// ... existing implementation remains mostly same but renamed to SetupLogger for consistency
	// Set the log level
	var level zapcore.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var encoder zapcore.Encoder
	if cfg.LogFormat == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)
	return zap.New(core)
}
