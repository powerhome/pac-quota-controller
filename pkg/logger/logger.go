package logger

import (
	"os"
	"strings"

	"github.com/powerhome/pac-quota-controller/pkg/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// SetupLogger configures the zap logger based on provided configuration
func SetupLogger(config *config.Config) *zap.Logger {
	// Set the log level
	var level zapcore.Level
	switch strings.ToLower(config.LogLevel) {
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

	// Create encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Configure the encoder based on the format
	var encoder zapcore.Encoder
	if config.LogFormat == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// Create the core
	core := zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level)

	// Create the logger
	return zap.New(core)
}

// ConfigureControllerRuntime sets up the controller-runtime logger to use our zap configuration
func ConfigureControllerRuntime(zapLogger *zap.Logger) {
	// Convert uber/zap logger to controller-runtime logger
	encoderConfig := zap.NewProductionEncoderConfig()
	encoder := zapcore.NewJSONEncoder(encoderConfig)
	ctrlLogger := ctrlzap.New(ctrlzap.UseDevMode(false), ctrlzap.Encoder(encoder))
	ctrl.SetLogger(ctrlLogger)
}
