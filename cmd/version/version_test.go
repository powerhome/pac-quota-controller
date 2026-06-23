package version

import "testing"

func TestNewVersionCmd(t *testing.T) {
	cmd := NewVersionCmd()
	if cmd.Use != "version" {
		t.Errorf("unexpected Use %q", cmd.Use)
	}
	if cmd.Run == nil {
		t.Error("version command has no Run function")
	}
}

func TestPrintInfo(t *testing.T) {
	// Smoke test: PrintInfo writes build info to stdout and must not panic.
	PrintInfo()
}
