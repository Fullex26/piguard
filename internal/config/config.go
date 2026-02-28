package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

const DefaultConfigPath = "/etc/piguard/config.yaml"

type Config struct {
	Notifications NotificationConfig `yaml:"notifications"`
	Ports         PortConfig         `yaml:"ports"`
	Firewall      FirewallConfig     `yaml:"firewall"`
	System        SystemConfig       `yaml:"system"`
	Alerts          AlertConfig          `yaml:"alerts"`
	Baseline        BaselineConfig       `yaml:"baseline"`
	Docker          DockerConfig         `yaml:"docker"`
	FileIntegrity   FileIntegrityConfig  `yaml:"file_integrity"`
	SecurityTools   SecurityToolsConfig  `yaml:"security_tools"`
	Network         NetworkConfig        `yaml:"network"`
}

type NotificationConfig struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Ntfy     NtfyConfig     `yaml:"ntfy"`
	Discord  DiscordConfig  `yaml:"discord"`
	Webhook  WebhookConfig  `yaml:"webhook"`
}

type TelegramConfig struct {
	Enabled     bool   `yaml:"enabled"`
	BotToken    string `yaml:"bot_token"`
	ChatID      string `yaml:"chat_id"`
	Interactive bool   `yaml:"interactive"` // Enable two-way command handling
}

type NtfyConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Topic    string `yaml:"topic"`
	Server   string `yaml:"server"`
	Token    string `yaml:"token"`
}

type DiscordConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
}

type WebhookConfig struct {
	Enabled bool   `yaml:"enabled"`
	URL     string `yaml:"url"`
	Method  string `yaml:"method"`
}

type PortConfig struct {
	Enabled  bool        `yaml:"enabled"`
	Ignore   []string    `yaml:"ignore"`
	Known    []KnownPort `yaml:"known"`
	Cooldown string      `yaml:"cooldown"`
}

type KnownPort struct {
	Addr  string `yaml:"addr"`
	Label string `yaml:"label"`
	Risk  string `yaml:"risk"`
}

type FirewallConfig struct {
	Enabled       bool           `yaml:"enabled"`
	Chains        []ChainConfig  `yaml:"chains"`
	CheckInterval string         `yaml:"check_interval"`
}

type ChainConfig struct {
	Table        string `yaml:"table"`
	Chain        string `yaml:"chain"`
	ExpectPolicy string `yaml:"expect_policy"`
	ExpectRule   string `yaml:"expect_rule"`
}

type SystemConfig struct {
	DiskThreshold   int `yaml:"disk_threshold"`
	MemoryThreshold int `yaml:"memory_threshold"`
	TempThreshold   int `yaml:"temperature_threshold"`
}

type AlertConfig struct {
	MinSeverity  string     `yaml:"min_severity"`
	DailySummary string     `yaml:"daily_summary"`
	QuietHours   QuietHours `yaml:"quiet_hours"`
}

type QuietHours struct {
	Start string `yaml:"start"`
	End   string `yaml:"end"`
}

type BaselineConfig struct {
	Mode             string `yaml:"mode"`
	LearningDuration string `yaml:"learning_duration"`
}

type DockerConfig struct {
	Enabled      bool   `yaml:"enabled"`
	PollInterval string `yaml:"poll_interval"` // default: "10s"
	AlertOnStop  bool   `yaml:"alert_on_stop"` // alert on graceful stop (default: false)
}

type FileIntegrityConfig struct {
	Enabled  bool        `yaml:"enabled"`
	Paths    []WatchPath `yaml:"paths"`
	Cooldown string      `yaml:"cooldown"`
}

type WatchPath struct {
	Path        string `yaml:"path"`
	Description string `yaml:"description"`
	Severity    string `yaml:"severity"` // "warning" or "critical"
}

type SecurityToolsConfig struct {
	Enabled      bool   `yaml:"enabled"`
	ClamAVLog    string `yaml:"clamav_log"`    // default: /var/log/clamav/clamav.log
	RKHunterLog  string `yaml:"rkhunter_log"`  // default: /var/log/rkhunter.log
	PollInterval string `yaml:"poll_interval"` // default: 30s
}

type NetworkConfig struct {
	Enabled      bool     `yaml:"enabled"`
	PollInterval string   `yaml:"poll_interval"` // default: "5m"
	AlertOnLeave bool     `yaml:"alert_on_leave"` // alert when known device leaves
	IgnoreMACs   []string `yaml:"ignore_macs"`    // MACs to never alert on
}

// Load reads and parses the config file, expanding env vars
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	// Expand environment variables in config
	expanded := os.ExpandEnv(string(data))

	cfg := DefaultConfig()
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// DefaultConfig returns sane defaults
func DefaultConfig() *Config {
	return &Config{
		Ports: PortConfig{
			Enabled:  true,
			Cooldown: "15m",
			Ignore:   []string{"127.0.0.1:*", "::1:*"},
		},
		Firewall: FirewallConfig{
			Enabled:       true,
			CheckInterval: "60s",
			Chains: []ChainConfig{
				{Table: "filter", Chain: "INPUT", ExpectPolicy: "DROP"},
				{Table: "filter", Chain: "DOCKER-USER", ExpectRule: "DROP.*0.0.0.0/0"},
			},
		},
		System: SystemConfig{
			DiskThreshold:   80,
			MemoryThreshold: 90,
			TempThreshold:   75,
		},
		Alerts: AlertConfig{
			MinSeverity:  "warning",
			DailySummary: "08:00",
			QuietHours: QuietHours{
				Start: "23:00",
				End:   "07:00",
			},
		},
		Baseline: BaselineConfig{
			Mode:             "enforcing",
			LearningDuration: "7d",
		},
		Docker: DockerConfig{
			Enabled:      true,
			PollInterval: "10s",
			AlertOnStop:  false,
		},
		FileIntegrity: FileIntegrityConfig{
			Enabled:  true,
			Cooldown: "5m",
			Paths: []WatchPath{
				{Path: "/etc/passwd", Description: "User accounts", Severity: "critical"},
				{Path: "/etc/shadow", Description: "Password hashes", Severity: "critical"},
				{Path: "/etc/sudoers", Description: "Sudo rules", Severity: "critical"},
				{Path: "/etc/ssh/sshd_config", Description: "SSH daemon config", Severity: "critical"},
				{Path: "/etc/hosts", Description: "Host resolution", Severity: "warning"},
				{Path: "/etc/crontab", Description: "System cron", Severity: "warning"},
				{Path: "/etc/cron.d", Description: "Cron job directory", Severity: "warning"},
			},
		},
		SecurityTools: SecurityToolsConfig{
			Enabled:      false,
			ClamAVLog:    "/var/log/clamav/clamav.log",
			RKHunterLog:  "/var/log/rkhunter.log",
			PollInterval: "30s",
		},
		Network: NetworkConfig{
			Enabled:      false,
			PollInterval: "5m",
			AlertOnLeave: false,
		},
	}
}

// Validate checks the config for errors
func (c *Config) Validate() error {
	hasNotifier := c.Notifications.Telegram.Enabled ||
		c.Notifications.Ntfy.Enabled ||
		c.Notifications.Discord.Enabled ||
		c.Notifications.Webhook.Enabled

	if !hasNotifier {
		return fmt.Errorf("at least one notification channel must be enabled")
	}

	if c.Notifications.Telegram.Enabled {
		if c.Notifications.Telegram.BotToken == "" {
			return fmt.Errorf("telegram bot_token is required when telegram is enabled")
		}
		if c.Notifications.Telegram.ChatID == "" {
			return fmt.Errorf("telegram chat_id is required when telegram is enabled")
		}
	}

	if c.Notifications.Ntfy.Enabled && c.Notifications.Ntfy.Topic == "" {
		return fmt.Errorf("ntfy topic is required when ntfy is enabled")
	}

	validSeverities := map[string]bool{"info": true, "warning": true, "critical": true}
	if !validSeverities[strings.ToLower(c.Alerts.MinSeverity)] {
		return fmt.Errorf("invalid min_severity: %s (must be info, warning, or critical)", c.Alerts.MinSeverity)
	}

	return nil
}

// HasNotifier returns whether at least one notifier is configured
func (c *Config) HasNotifier() bool {
	return c.Notifications.Telegram.Enabled ||
		c.Notifications.Ntfy.Enabled ||
		c.Notifications.Discord.Enabled ||
		c.Notifications.Webhook.Enabled
}
