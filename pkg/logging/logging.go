package logging

import (
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger is the global logger instance
var (
	Logger     *zap.Logger
	loggerOnce sync.Once
)

// InitLogger initializes the logger with the specified level
func InitLogger(level string) error {
	// Configure encoder
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Configure level
	var logLevel zapcore.Level
	if err := logLevel.UnmarshalText([]byte(level)); err != nil {
		logLevel = zapcore.InfoLevel
	}

	// Create core
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		logLevel,
	)

	// Create logger
	Logger = zap.New(core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)

	return nil
}

// Sync flushes any buffered log entries
func Sync() error {
	return Logger.Sync()
}

// NewLogger creates a logger instance that respects PAC_QUOTA_CONTROLLER_LOG_LEVEL
func NewLogger() *zap.Logger {
	// Initialize the global logger only once
	loggerOnce.Do(func() {
		// Get log level from environment variable
		logLevel := os.Getenv("PAC_QUOTA_CONTROLLER_LOG_LEVEL")
		if logLevel == "" {
			logLevel = "info" // Default log level
		}

		// Convert to lowercase for case-insensitive comparison
		logLevel = strings.ToLower(logLevel)

		// Initialize the global logger
		if err := InitLogger(logLevel); err != nil {
			// Log the error to stderr as we can't use the logger yet
			os.Stderr.WriteString("Error initializing logger: " + err.Error() + "\n")
			os.Exit(1)
		}
	})

	// If global logger is available, use it
	if Logger != nil {
		return Logger
	}

	// Fallback if something went wrong with global logger initialization
	loggerConfig := zap.NewProductionConfig()

	// Try to get log level from environment
	logLevel := os.Getenv("PAC_QUOTA_CONTROLLER_LOG_LEVEL")
	if logLevel != "" {
		var zapLevel zapcore.Level
		if err := zapLevel.UnmarshalText([]byte(logLevel)); err == nil {
			loggerConfig.Level = zap.NewAtomicLevelAt(zapLevel)
		}
	}

	logger, err := loggerConfig.Build()
	if err != nil {
		// If there's an error building the logger, use a simple production logger
		logger, _ = zap.NewProduction()
	}

	return logger
}
