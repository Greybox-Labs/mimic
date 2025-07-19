package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Mode      string                 `mapstructure:"mode"` // Global mode: "record", "mock", or "replay"
	Server    ServerConfig           `mapstructure:"server"`
	Proxies   map[string]ProxyConfig `mapstructure:"proxies"`
	Database  DatabaseConfig         `mapstructure:"database"`
	Recording RecordingConfig        `mapstructure:"recording"`
	Mock      MockConfig             `mapstructure:"mock"`
	Replay    ReplayConfig           `mapstructure:"replay"`
	GRPC      GRPCConfig             `mapstructure:"grpc"`
	Export    ExportConfig           `mapstructure:"export"`
}

type ServerConfig struct {
	ListenHost string `mapstructure:"listen_host"`
	ListenPort int    `mapstructure:"listen_port"`
	GRPCPort   int    `mapstructure:"grpc_port"` // Port for gRPC server (defaults to listen_port + 1000)
}

type ProxyConfig struct {
	TargetHost  string `mapstructure:"target_host"`
	TargetPort  int    `mapstructure:"target_port"`
	Protocol    string `mapstructure:"protocol"`
	SessionName string `mapstructure:"session_name"`
	// gRPC routing patterns (optional)
	ServicePattern string `mapstructure:"service_pattern"` // Regex pattern for service names
	MethodPattern  string `mapstructure:"method_pattern"`  // Regex pattern for method names
	IsDefault      bool   `mapstructure:"is_default"`      // Whether this is the default/fallback route
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

type ReplayConfig struct {
	TargetHost         string `mapstructure:"target_host"`          // Target server to replay against
	TargetPort         int    `mapstructure:"target_port"`          // Target server port
	Protocol           string `mapstructure:"protocol"`             // http, https, or grpc
	SessionName        string `mapstructure:"session_name"`         // Session to replay
	MatchingStrategy   string `mapstructure:"matching_strategy"`    // How to compare responses: exact, fuzzy, status_code
	FailFast           bool   `mapstructure:"fail_fast"`            // Exit on first mismatch or collect all errors
	TimeoutSeconds     int    `mapstructure:"timeout_seconds"`      // Request timeout in seconds
	MaxConcurrency     int    `mapstructure:"max_concurrency"`      // Max concurrent requests (0 = sequential)
	IgnoreTimestamps   bool   `mapstructure:"ignore_timestamps"`    // Skip timing-based replay, fire all at once
	InsecureSkipVerify bool   `mapstructure:"insecure_skip_verify"` // Skip TLS verification for HTTPS/gRPC
	// gRPC-specific settings
	GRPCMaxMessageSize int  `mapstructure:"grpc_max_message_size"` // Max gRPC message size in bytes
	GRPCMaxHeaderSize  int  `mapstructure:"grpc_max_header_size"`  // Max gRPC header size in bytes
	GRPCInsecure       bool `mapstructure:"grpc_insecure"`         // Use insecure gRPC connection
}

type GRPCConfig struct {
	ProtoPaths        []string `mapstructure:"proto_paths"`
	ReflectionEnabled bool     `mapstructure:"reflection_enabled"`
	MaxMessageSize    int      `mapstructure:"max_message_size"` // Max message size in bytes
	MaxHeaderSize     int      `mapstructure:"max_header_size"`  // Max header list size in bytes
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

	viper.SetDefault("mode", "record")

	viper.SetDefault("server.listen_host", "0.0.0.0")
	viper.SetDefault("server.listen_port", 8080)
	viper.SetDefault("server.grpc_port", 9080) // Default to 9080

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

	viper.SetDefault("replay.protocol", "https")
	viper.SetDefault("replay.matching_strategy", "exact")
	viper.SetDefault("replay.fail_fast", false)
	viper.SetDefault("replay.timeout_seconds", 30)
	viper.SetDefault("replay.max_concurrency", 0)
	viper.SetDefault("replay.ignore_timestamps", false)
	viper.SetDefault("replay.insecure_skip_verify", false)
	viper.SetDefault("replay.grpc_max_message_size", 256*1024*1024) // 256MB
	viper.SetDefault("replay.grpc_max_header_size", 16*1024*1024)   // 16MB
	viper.SetDefault("replay.grpc_insecure", false)

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
		Mode: "record",
		Server: ServerConfig{
			ListenHost: "0.0.0.0",
			ListenPort: 8080,
			GRPCPort:   9080,
		},
		Proxies: map[string]ProxyConfig{
			"default": {
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
		Replay: ReplayConfig{
			Protocol:           "https",
			MatchingStrategy:   "exact",
			FailFast:           false,
			TimeoutSeconds:     30,
			MaxConcurrency:     0,
			IgnoreTimestamps:   false,
			InsecureSkipVerify: false,
			GRPCMaxMessageSize: 256 * 1024 * 1024, // 256MB
			GRPCMaxHeaderSize:  16 * 1024 * 1024,  // 16MB
			GRPCInsecure:       false,
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
	// Validate global mode
	if c.Mode != "record" && c.Mode != "mock" && c.Mode != "replay" {
		return fmt.Errorf("invalid mode: %s (must be 'record', 'mock', or 'replay')", c.Mode)
	}

	// Validate server config
	if c.Server.ListenPort <= 0 || c.Server.ListenPort > 65535 {
		return fmt.Errorf("invalid server listen_port: %d", c.Server.ListenPort)
	}

	// Set default gRPC port if not configured
	if c.Server.GRPCPort == 0 {
		c.Server.GRPCPort = c.Server.ListenPort + 1000
	}

	if c.Server.GRPCPort <= 0 || c.Server.GRPCPort > 65535 {
		return fmt.Errorf("invalid server grpc_port: %d", c.Server.GRPCPort)
	}

	if len(c.Proxies) == 0 {
		return fmt.Errorf("at least one proxy must be configured")
	}

	// Validate proxy configs
	for name, proxy := range c.Proxies {
		if c.Mode == "record" && (proxy.TargetHost == "" || proxy.TargetPort == 0) {
			return fmt.Errorf("target_host and target_port are required in record mode for proxy '%s'", name)
		}

		if proxy.SessionName == "" {
			return fmt.Errorf("session_name is required for proxy '%s'", name)
		}
	}

	// Validate replay config
	if c.Mode == "replay" {
		if c.Replay.TargetHost == "" {
			return fmt.Errorf("target_host is required in replay mode")
		}
		if c.Replay.TargetPort == 0 {
			return fmt.Errorf("target_port is required in replay mode")
		}
		if c.Replay.SessionName == "" {
			return fmt.Errorf("session_name is required in replay mode")
		}
		if c.Replay.Protocol != "http" && c.Replay.Protocol != "https" && c.Replay.Protocol != "grpc" {
			return fmt.Errorf("invalid replay protocol: %s (must be 'http', 'https', or 'grpc')", c.Replay.Protocol)
		}
		if c.Replay.GRPCMaxMessageSize <= 0 {
			c.Replay.GRPCMaxMessageSize = 256 * 1024 * 1024 // 256MB default
		}
		if c.Replay.GRPCMaxHeaderSize <= 0 {
			c.Replay.GRPCMaxHeaderSize = 16 * 1024 * 1024 // 16MB default
		}
		if c.Replay.MatchingStrategy != "exact" && c.Replay.MatchingStrategy != "fuzzy" && c.Replay.MatchingStrategy != "status_code" {
			return fmt.Errorf("invalid replay matching strategy: %s (must be 'exact', 'fuzzy', or 'status_code')", c.Replay.MatchingStrategy)
		}
	}

	if c.Database.Path == "" {
		return fmt.Errorf("database path cannot be empty")
	}

	return nil
}

func SaveConfig(config *Config, path string) error {
	viper.Set("mode", config.Mode)
	viper.Set("server", config.Server)
	viper.Set("proxies", config.Proxies)
	viper.Set("database", config.Database)
	viper.Set("recording", config.Recording)
	viper.Set("mock", config.Mock)
	viper.Set("replay", config.Replay)
	viper.Set("grpc", config.GRPC)
	viper.Set("export", config.Export)

	if path == "" {
		path = "config.yaml"
	}

	return viper.WriteConfigAs(path)
}
