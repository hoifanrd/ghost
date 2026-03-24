package cmd

import (
	"testing"

	"github.com/zinc-sig/ghost/cmd/config"
)

func TestExecFlagValidation(t *testing.T) {
	tests := []struct {
		name          string
		setup         func()
		wantErr       bool
		errorContains string
	}{
		{
			name: "exec with webhook-url is rejected",
			setup: func() {
				runFlags = config.CommonFlags{Exec: true}
				runWebhookConfig.URL = "http://example.com"
				runUploadConfig.Provider = ""
			},
			wantErr:       true,
			errorContains: "--exec is incompatible with --webhook-url",
		},
		{
			name: "exec with upload-provider is rejected",
			setup: func() {
				runFlags = config.CommonFlags{Exec: true}
				runWebhookConfig.URL = ""
				runUploadConfig.Provider = "minio"
			},
			wantErr:       true,
			errorContains: "--exec is incompatible with --upload-provider",
		},
		{
			name: "exec with dry-run is rejected",
			setup: func() {
				runFlags = config.CommonFlags{Exec: true, DryRun: true}
				runWebhookConfig.URL = ""
				runUploadConfig.Provider = ""
			},
			wantErr:       true,
			errorContains: "--exec is incompatible with --dry-run",
		},
		{
			name: "exec alone is accepted",
			setup: func() {
				runFlags = config.CommonFlags{Exec: true}
				runWebhookConfig.URL = ""
				runUploadConfig.Provider = ""
			},
			wantErr: false,
		},
		{
			name: "sandbox without exec is accepted",
			setup: func() {
				runFlags = config.CommonFlags{Sandbox: true}
				runWebhookConfig.URL = ""
				runUploadConfig.Provider = ""
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore globals
			savedFlags := runFlags
			savedWebhook := runWebhookConfig
			savedUpload := runUploadConfig
			defer func() {
				runFlags = savedFlags
				runWebhookConfig = savedWebhook
				runUploadConfig = savedUpload
			}()

			tt.setup()

			// Call the PreRunE validation logic directly
			err := validateExecFlags()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
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
