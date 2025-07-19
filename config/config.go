package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Server    ServerConfig           `mapstructure:"server"`
	Proxies   map[string]ProxyConfig `mapstructure:"proxies"`
	Database  DatabaseConfig         `mapstructure:"database"`
	Recording RecordingConfig        `mapstructure:"recording"`
	Mock      MockConfig             `mapstructure:"mock"`
	GRPC      GRPCConfig             `mapstructure:"grpc"`
	Export    ExportConfig           `mapstructure:"export"`
}

type ServerConfig struct {
	ListenHost string `mapstructure:"listen_host"`
	ListenPort int    `mapstructure:"listen_port"`
}

type ProxyConfig struct {
	Mode        string `mapstructure:"mode"`
	TargetHost  string `mapstructure:"target_host"`
	TargetPort  int    `mapstructure:"target_port"`
	Protocol    string `mapstructure:"protocol"`
	SessionName string `mapstructure:"session_name"`
}

type DatabaseConfig struct {
	Path               string `mapstructure:"path"`
	ConnectionPoolSize int    `mapstructure:"connection_pool_size"`
}

type RecordingConfig struct {
	SessionName    string   `mapstructure:"session_name"`
	CaptureHeaders bool     `mapstructure:"capture_headers"`
	CaptureBody    bool     `mapstructure:"capture_body"`
	RedactPatterns []string `mapstructure:"redact_patterns"`
}

type MockConfig struct {
	MatchingStrategy string                 `mapstructure:"matching_strategy"`
	SequenceMode     string                 `mapstructure:"sequence_mode"`
	NotFoundResponse NotFoundResponseConfig `mapstructure:"not_found_response"`
}

type NotFoundResponseConfig struct {
	Status int                    `mapstructure:"status"`
	Body   map[string]interface{} `mapstructure:"body"`
}

type GRPCConfig struct {
	ProtoPaths        []string `mapstructure:"proto_paths"`
	ReflectionEnabled bool     `mapstructure:"reflection_enabled"`
	MaxMessageSize    int      `mapstructure:"max_message_size"`    // Max message size in bytes
	MaxHeaderSize     int      `mapstructure:"max_header_size"`     // Max header list size in bytes
}

type ExportConfig struct {
	Format      string `mapstructure:"format"`
	PrettyPrint bool   `mapstructure:"pretty_print"`
	Compress    bool   `mapstructure:"compress"`
}

func LoadConfig(configPath string) (*Config, error) {
	// Ensure ~/.mimic directory exists
	if err := ensureMimicDirectory(); err != nil {
		return nil, fmt.Errorf("failed to create mimic directory: %w", err)
	}

	if configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME/.mimic")
	}

	setDefaults()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			return getDefaultConfig(), nil
		}
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %w", err)
	}

	return &config, nil
}

func ensureMimicDirectory() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	mimicDir := filepath.Join(homeDir, ".mimic")
	if err := os.MkdirAll(mimicDir, 0755); err != nil {
		return fmt.Errorf("failed to create mimic directory: %w", err)
	}

	return nil
}

func setDefaults() {
	homeDir, _ := os.UserHomeDir()
	defaultDBPath := filepath.Join(homeDir, ".mimic", "recordings.db")

	viper.SetDefault("server.listen_host", "0.0.0.0")
	viper.SetDefault("server.listen_port", 8080)

	viper.SetDefault("database.path", defaultDBPath)
	viper.SetDefault("database.connection_pool_size", 10)

	viper.SetDefault("recording.session_name", "default")
	viper.SetDefault("recording.capture_headers", true)
	viper.SetDefault("recording.capture_body", true)

	viper.SetDefault("mock.matching_strategy", "exact")
	viper.SetDefault("mock.sequence_mode", "ordered")
	viper.SetDefault("mock.not_found_response.status", 404)
	viper.SetDefault("mock.not_found_response.body", map[string]interface{}{
		"error": "Recording not found",
	})

	viper.SetDefault("grpc.reflection_enabled", true)
	viper.SetDefault("grpc.max_message_size", 64*1024*1024) // 64MB
	viper.SetDefault("grpc.max_header_size", 64*1024*1024)  // 64MB

	viper.SetDefault("export.format", "json")
	viper.SetDefault("export.pretty_print", true)
	viper.SetDefault("export.compress", false)
}

func getDefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	defaultDBPath := filepath.Join(homeDir, ".mimic", "recordings.db")

	return &Config{
		Server: ServerConfig{
			ListenHost: "0.0.0.0",
			ListenPort: 8080,
		},
		Proxies: map[string]ProxyConfig{
			"default": {
				Mode:        "record",
				Protocol:    "http",
				SessionName: "default",
			},
		},
		Database: DatabaseConfig{
			Path:               defaultDBPath,
			ConnectionPoolSize: 10,
		},
		Recording: RecordingConfig{
			SessionName:    "default",
			CaptureHeaders: true,
			CaptureBody:    true,
			RedactPatterns: []string{},
		},
		Mock: MockConfig{
			MatchingStrategy: "exact",
			SequenceMode:     "ordered",
			NotFoundResponse: NotFoundResponseConfig{
				Status: 404,
				Body:   map[string]interface{}{"error": "Recording not found"},
			},
		},
		GRPC: GRPCConfig{
			ProtoPaths:        []string{},
			ReflectionEnabled: true,
			MaxMessageSize:    64 * 1024 * 1024, // 64MB
			MaxHeaderSize:     64 * 1024 * 1024, // 64MB
		},
		Export: ExportConfig{
			Format:      "json",
			PrettyPrint: true,
			Compress:    false,
		},
	}
}

func (c *Config) Validate() error {
	if c.Server.ListenPort <= 0 || c.Server.ListenPort > 65535 {
		return fmt.Errorf("invalid server listen_port: %d", c.Server.ListenPort)
	}

	if len(c.Proxies) == 0 {
		return fmt.Errorf("at least one proxy must be configured")
	}

	for name, proxy := range c.Proxies {
		if proxy.Mode != "record" && proxy.Mode != "mock" {
			return fmt.Errorf("invalid proxy mode for '%s': %s (must be 'record' or 'mock')", name, proxy.Mode)
		}

		if proxy.Mode == "record" && (proxy.TargetHost == "" || proxy.TargetPort == 0) {
			return fmt.Errorf("target_host and target_port are required in record mode for proxy '%s'", name)
		}

		if proxy.SessionName == "" {
			return fmt.Errorf("session_name is required for proxy '%s'", name)
		}
	}

	if c.Database.Path == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	return nil
}

func SaveConfig(config *Config, path string) error {
	viper.Set("server", config.Server)
	viper.Set("proxies", config.Proxies)
	viper.Set("database", config.Database)
	viper.Set("recording", config.Recording)
	viper.Set("mock", config.Mock)
	viper.Set("grpc", config.GRPC)
	viper.Set("export", config.Export)

	if path == "" {
		path = "config.yaml"
	}

	return viper.WriteConfigAs(path)
}
