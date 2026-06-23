package main

import "testing"

func TestNewRootCommand(t *testing.T) {
	cmd := newRootCommand()

	if cmd.Use != "controller-manager" {
		t.Errorf("unexpected Use %q", cmd.Use)
	}

	hasVersion := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "version" {
			hasVersion = true
		}
	}
	if !hasVersion {
		t.Error("version subcommand not registered")
	}

	for _, flag := range []string{"leader-elect", "log-level", "webhook-port", "events-enable"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("flag %q not registered", flag)
		}
	}
}

// Executing the version subcommand exercises the command wiring without starting
// the manager (which would require a real cluster).
func TestRootCommandRunsVersionSubcommand(t *testing.T) {
	cmd := newRootCommand()
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("executing version subcommand: %v", err)
	}
}
