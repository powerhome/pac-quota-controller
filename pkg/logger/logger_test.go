package logger

import (
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/powerhome/pac-quota-controller/pkg/config"
)

func TestSetupLoggerLevels(t *testing.T) {
	cases := []struct {
		level   string
		enabled zapcore.Level
		blocked zapcore.Level
	}{
		{"debug", zapcore.DebugLevel, 0},
		{"info", zapcore.InfoLevel, zapcore.DebugLevel},
		{"warn", zapcore.WarnLevel, zapcore.InfoLevel},
		{"error", zapcore.ErrorLevel, zapcore.WarnLevel},
		{"unknown-defaults-to-info", zapcore.InfoLevel, zapcore.DebugLevel},
	}
	for _, tc := range cases {
		t.Run(tc.level, func(t *testing.T) {
			lg := SetupLogger(&config.Config{LogLevel: tc.level, LogFormat: "json"})
			if lg == nil {
				t.Fatal("SetupLogger returned nil")
			}
			if !lg.Core().Enabled(tc.enabled) {
				t.Errorf("expected level %v to be enabled", tc.enabled)
			}
			if tc.blocked != 0 && lg.Core().Enabled(tc.blocked) {
				t.Errorf("expected level %v to be disabled", tc.blocked)
			}
		})
	}
}

func TestSetupLoggerConsoleFormat(t *testing.T) {
	if SetupLogger(&config.Config{LogLevel: "info", LogFormat: "console"}) == nil {
		t.Fatal("SetupLogger returned nil for console format")
	}
}

func TestLAlwaysReturnsLogger(t *testing.T) {
	if L() == nil {
		t.Fatal("L() must never return nil")
	}
}

func TestInitTestSetsGlobalLogger(t *testing.T) {
	lg := InitTest()
	if lg == nil {
		t.Fatal("InitTest returned nil")
	}
	if L() != lg {
		t.Fatal("L() should return the logger set by InitTest")
	}
}
