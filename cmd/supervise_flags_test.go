package cmd

import (
	"testing"

	"github.com/zinc-sig/ghost/cmd/config"
)

func TestSuperviseFlagValidation(t *testing.T) {
	tests := []struct {
		name          string
		setup         func()
		wantErr       bool
		errorContains string
	}{
		{
			name: "supervise with webhook-url is rejected",
			setup: func() {
				runFlags = config.CommonFlags{Supervise: true}
				runWebhookConfig.URL = "http://example.com"
			},
			wantErr:       true,
			errorContains: "--supervise is incompatible with --webhook-url",
		},
		{
			name: "supervise with upload-provider is rejected",
			setup: func() {
				runFlags = config.CommonFlags{Supervise: true}
				runUploadConfig.Provider = "minio"
			},
			wantErr:       true,
			errorContains: "--supervise is incompatible with --upload-provider",
		},
		{
			name: "supervise with dry-run is rejected",
			setup: func() {
				runFlags = config.CommonFlags{Supervise: true, DryRun: true}
			},
			wantErr:       true,
			errorContains: "--supervise is incompatible with --dry-run",
		},
		{
			name: "supervise alone is accepted",
			setup: func() {
				runFlags = config.CommonFlags{Supervise: true}
			},
			wantErr: false,
		},
		{
			name: "supervise with sandbox and max-pids is accepted",
			setup: func() {
				runFlags = config.CommonFlags{Supervise: true, Sandbox: true, MaxPids: 33}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			savedFlags := runFlags
			savedWebhook := runWebhookConfig
			savedUpload := runUploadConfig
			defer func() {
				runFlags = savedFlags
				runWebhookConfig = savedWebhook
				runUploadConfig = savedUpload
			}()
			runWebhookConfig.URL = ""
			runUploadConfig.Provider = ""

			tt.setup()

			err := validateSuperviseFlags()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorContains)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
