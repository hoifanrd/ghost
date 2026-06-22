package helpers

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/zinc-sig/ghost/cmd/config"
	contextparser "github.com/zinc-sig/ghost/internal/context"
	"github.com/zinc-sig/ghost/internal/webhook"
)

// RecordWebhookChanges records which direct webhook-* flags were explicitly set,
// so BuildWebhookConfig can honour flag-over-config precedence. Call from PreRunE.
// Flags().Visit only visits flags that were changed, so the names need not be
// duplicated here.
func RecordWebhookChanges(cmd *cobra.Command, cfg *config.WebhookConfig) {
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if strings.HasPrefix(f.Name, "webhook-") {
			if cfg.Changed == nil {
				cfg.Changed = map[string]bool{}
			}
			cfg.Changed[f.Name] = true
		}
	})
}

// Default webhook configuration constants
const (
	DefaultWebhookTimeout    = "30s"
	DefaultWebhookRetryDelay = "1s"
	DefaultWebhookRetries    = 3
	DefaultWebhookMethod     = "POST"
	DefaultWebhookAuthType   = "none"
	WebhookRetryMultiplier   = 2.0
)

// WebhookMaxRetryDelay is the maximum delay between retry attempts in exponential backoff
var WebhookMaxRetryDelay = 30 * time.Second

// BuildWebhookConfig builds webhook configuration from all sources
func BuildWebhookConfig(cfg *config.WebhookConfig) (map[string]any, error) {
	// Use the generic builder with all configuration sources
	// Precedence: env < file < json < kv < direct flags
	result, err := contextparser.BuildContextWithPrefix(
		"GHOST_WEBHOOK",
		cfg.Config,     // JSON string configuration
		cfg.ConfigKV,   // Key-value pairs
		cfg.ConfigFile, // Config file path
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build webhook config: %w", err)
	}

	// If no config from any source, create empty map
	if result == nil {
		result = make(map[string]any)
	}

	webhookConf, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("webhook config must be an object/map")
	}

	// Explicit flags win (highest precedence), keyed off cfg.Changed so a flag
	// set to its default value still overrides config. URL/AuthToken have no
	// default, so a non-empty value also counts as set.
	if cfg.URL != "" {
		webhookConf["url"] = cfg.URL
	}
	if cfg.Changed["webhook-method"] {
		webhookConf["method"] = cfg.Method
	}
	if cfg.Changed["webhook-auth-type"] {
		webhookConf["auth_type"] = cfg.AuthType
	}
	if cfg.Changed["webhook-auth-token"] || cfg.AuthToken != "" {
		webhookConf["auth_token"] = cfg.AuthToken
	}
	if cfg.Changed["webhook-timeout"] {
		webhookConf["timeout"] = cfg.Timeout
	}
	if cfg.Changed["webhook-retries"] {
		webhookConf["retries"] = cfg.Retries
	}
	if cfg.Changed["webhook-retry-delay"] {
		webhookConf["retry_delay"] = cfg.RetryDelay
	}

	return webhookConf, nil
}

// ParseWebhookConfigToInternal converts built webhook config map to internal webhook structures
func ParseWebhookConfigToInternal(cfg *config.WebhookConfig) (*webhook.Config, *webhook.RetryConfig, error) {
	// Build the consolidated configuration from all sources
	configMap, err := BuildWebhookConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	// Check if webhook is configured
	url, _ := configMap["url"].(string)
	if url == "" {
		return nil, nil, nil // No webhook configured
	}

	// Parse webhook timeout
	defaultTimeout, _ := time.ParseDuration(DefaultWebhookTimeout)
	var webhookTimeoutDur = defaultTimeout
	if timeout, ok := configMap["timeout"].(string); ok && timeout != "" {
		webhookTimeoutDur, err = time.ParseDuration(timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook timeout duration: %w", err)
		}
	}

	// Parse retry delay
	defaultRetryDelay, _ := time.ParseDuration(DefaultWebhookRetryDelay)
	var retryDelay = defaultRetryDelay
	if delay, ok := configMap["retry_delay"].(string); ok && delay != "" {
		retryDelay, err = time.ParseDuration(delay)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid webhook retry delay: %w", err)
		}
	}

	// Get HTTP method (default to POST)
	method, _ := configMap["method"].(string)
	if method == "" {
		method = DefaultWebhookMethod
	}

	// Get auth settings
	authType, _ := configMap["auth_type"].(string)
	if authType == "" {
		authType = DefaultWebhookAuthType
	}
	authToken, _ := configMap["auth_token"].(string)

	// Get retries (handle both int and float64 from JSON)
	maxRetries := DefaultWebhookRetries
	if r, ok := configMap["retries"].(int); ok {
		maxRetries = r
	} else if r, ok := configMap["retries"].(float64); ok {
		maxRetries = int(r)
	}

	webhookConfig := &webhook.Config{
		URL:       url,
		Method:    method,
		Timeout:   webhookTimeoutDur,
		AuthType:  authType,
		AuthToken: authToken,
	}

	retryConfig := &webhook.RetryConfig{
		MaxRetries:   maxRetries,
		InitialDelay: retryDelay,
		MaxDelay:     WebhookMaxRetryDelay,
		Multiplier:   WebhookRetryMultiplier,
	}

	return webhookConfig, retryConfig, nil
}
