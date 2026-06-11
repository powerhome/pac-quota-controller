package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/powerhome/pac-quota-controller/pkg/config"
)

func TestValidateLeaderElectionTiming(t *testing.T) {
	cases := []struct {
		name        string
		cfg         config.Config
		expectError bool
	}{
		{
			name: "happy: lease > renew > retry, all positive",
			cfg: config.Config{
				LeaderElectionLeaseDuration: 15,
				LeaderElectionRenewDeadline: 10,
				LeaderElectionRetryPeriod:   2,
			},
		},
		{
			name: "lease == renew is invalid",
			cfg: config.Config{
				LeaderElectionLeaseDuration: 10,
				LeaderElectionRenewDeadline: 10,
				LeaderElectionRetryPeriod:   2,
			},
			expectError: true,
		},
		{
			name: "renew == retry is invalid",
			cfg: config.Config{
				LeaderElectionLeaseDuration: 15,
				LeaderElectionRenewDeadline: 5,
				LeaderElectionRetryPeriod:   5,
			},
			expectError: true,
		},
		{
			name: "lease < renew is invalid (would flap leadership)",
			cfg: config.Config{
				LeaderElectionLeaseDuration: 5,
				LeaderElectionRenewDeadline: 10,
				LeaderElectionRetryPeriod:   2,
			},
			expectError: true,
		},
		{
			name: "zero lease is invalid",
			cfg: config.Config{
				LeaderElectionLeaseDuration: 0,
				LeaderElectionRenewDeadline: 10,
				LeaderElectionRetryPeriod:   2,
			},
			expectError: true,
		},
		{
			name: "negative retry is invalid",
			cfg: config.Config{
				LeaderElectionLeaseDuration: 15,
				LeaderElectionRenewDeadline: 10,
				LeaderElectionRetryPeriod:   -1,
			},
			expectError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateLeaderElectionTiming(&tc.cfg)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
