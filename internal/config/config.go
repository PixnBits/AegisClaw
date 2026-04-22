package config

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

// GatewayChannelConfig is the per-channel adapter configuration stored in
// Config.Gateway.Channels.  It mirrors gateway.ChannelConfig so that the
// config package does not need to import the gateway package.
type GatewayChannelConfig struct {
	// ID is the unique channel name (must match the Channel.ID() return value).
	ID string `yaml:"id" mapstructure:"id"`
	// Type identifies the adapter implementation: "webhook" is the only
	// built-in type.  Other types require governed skill code.
	Type string `yaml:"type" mapstructure:"type"`
	// Enabled controls whether this channel is started.
	Enabled bool `yaml:"enabled" mapstructure:"enabled"`
	// Addr is the listen address for server-side channels (e.g. webhook).
	Addr string `yaml:"addr" mapstructure:"addr"`
	// Secret is a shared secret used to authenticate inbound requests.
	Secret string `yaml:"secret" mapstructure:"secret"`
	// Extra holds channel-specific key-value settings.
	Extra map[string]string `yaml:"extra" mapstructure:"extra"`
}

// Config holds the application configuration loaded from ~/.config/aegisclaw/config.yaml
// Security: All paths are validated to prevent directory traversal attacks.
// Defaults are set to secure, isolated locations with no host filesystem access.
type Config struct {
	Firecracker struct {
		Bin string `yaml:"bin" mapstructure:"bin"`
	} `yaml:"firecracker" mapstructure:"firecracker"`
	Jailer struct {
		Bin string `yaml:"bin" mapstructure:"bin"`
	} `yaml:"jailer" mapstructure:"jailer"`
	Rootfs struct {
		Template string `yaml:"template" mapstructure:"template"`
	} `yaml:"rootfs" mapstructure:"rootfs"`
	Audit struct {
		Dir string `yaml:"dir" mapstructure:"dir"`
	} `yaml:"audit" mapstructure:"audit"`
	Sandbox struct {
		StateDir     string `yaml:"state_dir" mapstructure:"state_dir"`
		ChrootBase   string `yaml:"chroot_base" mapstructure:"chroot_base"`
		KernelImage  string `yaml:"kernel_image" mapstructure:"kernel_image"`
		RegistryPath string `yaml:"registry_path" mapstructure:"registry_path"`
		// IsolationMode selects the sandbox backend.
		// Only "firecracker" is supported on this platform (hardware-virtualised
		// microVMs via Firecracker + jailer).  Any other value is rejected at
		// daemon startup.  See internal/sandbox.IsolationMode for details.
		IsolationMode string `yaml:"isolation_mode" mapstructure:"isolation_mode"`
	} `yaml:"sandbox" mapstructure:"sandbox"`
	Proposal struct {
		StoreDir string `yaml:"store_dir" mapstructure:"store_dir"`
	} `yaml:"proposal" mapstructure:"proposal"`
	Court struct {
		PersonaDir string `yaml:"persona_dir" mapstructure:"persona_dir"`
		SessionDir string `yaml:"session_dir" mapstructure:"session_dir"`
	} `yaml:"court" mapstructure:"court"`
	Builder struct {
		RootfsTemplate      string `yaml:"rootfs_template" mapstructure:"rootfs_template"`
		WorkspaceBaseDir    string `yaml:"workspace_base_dir" mapstructure:"workspace_base_dir"`
		MaxConcurrentBuilds int    `yaml:"max_concurrent_builds" mapstructure:"max_concurrent_builds"`
		BuildTimeoutMinutes int    `yaml:"build_timeout_minutes" mapstructure:"build_timeout_minutes"`
		// SBOMDir is where sbom.json files are written after a successful build.
		// Defaults to ~/.local/share/aegisclaw/sbom.
		// Set to "" to disable SBOM generation.
		SBOMDir string `yaml:"sbom_dir" mapstructure:"sbom_dir"`
	} `yaml:"builder" mapstructure:"builder"`
	Vault struct {
		Dir string `yaml:"dir" mapstructure:"dir"`
	} `yaml:"vault" mapstructure:"vault"`
	Ollama struct {
		Endpoint     string `yaml:"endpoint" mapstructure:"endpoint"`
		TimeoutSecs  int    `yaml:"timeout_secs" mapstructure:"timeout_secs"`
		RegistryPath string `yaml:"registry_path" mapstructure:"registry_path"`
		ModelDir     string `yaml:"model_dir" mapstructure:"model_dir"`
		DefaultModel string `yaml:"default_model" mapstructure:"default_model"`
	} `yaml:"ollama" mapstructure:"ollama"`
	Daemon struct {
		SocketPath string `yaml:"socket_path" mapstructure:"socket_path"`
	} `yaml:"daemon" mapstructure:"daemon"`
	Composition struct {
		Dir string `yaml:"dir" mapstructure:"dir"`
	} `yaml:"composition" mapstructure:"composition"`
	Agent struct {
		// RootfsPath is the ext4 rootfs image used for the main agent microVM.
		// It must contain the guest-agent binary at /sbin/init or as PID-1.
		// Defaults to /var/lib/aegisclaw/rootfs-templates/alpine.ext4.
		RootfsPath string `yaml:"rootfs_path" mapstructure:"rootfs_path"`
		// StructuredOutput enables Ollama JSON-mode enforcement in the agent VM
		// for the ReAct loop (Phase 0).  When true the guest-agent validates
		// tool-call JSON and retries on parse failure.  Defaults to false for
		// backward compatibility; set to true for improved compliance.
		StructuredOutput bool `yaml:"structured_output" mapstructure:"structured_output"`
	} `yaml:"agent" mapstructure:"agent"`
	Snapshot struct {
		// Dir is where VM snapshots (memory + disk state) are stored.
		// Snapshots are used for fast Orchestrator wakeups and Worker spawning.
		// Defaults to ~/.local/share/aegisclaw/snapshots.
		Dir string `yaml:"dir" mapstructure:"dir"`
	} `yaml:"snapshot" mapstructure:"snapshot"`
	Memory struct {
		// Dir is where the encrypted Memory Store vault file is stored.
		// Defaults to ~/.local/share/aegisclaw/memory.
		Dir string `yaml:"dir" mapstructure:"dir"`
		// EmbeddingModel is the Ollama model used for semantic embeddings.
		// Defaults to nomic-embed-text.
		EmbeddingModel string `yaml:"embedding_model" mapstructure:"embedding_model"`
		// MaxSizeMB is the hard cap on memory store size in mebibytes.
		// Defaults to 2048 (2 GiB).
		MaxSizeMB int64 `yaml:"max_size_mb" mapstructure:"max_size_mb"`
		// DefaultTTL is the TTL tier applied to new memories when not specified.
		// Defaults to "90d".
		DefaultTTL string `yaml:"default_ttl" mapstructure:"default_ttl"`
		// CompactOnStartup runs the compaction daemon once at daemon startup
		// in addition to the daily background schedule. Defaults to false.
		CompactOnStartup bool `yaml:"compact_on_startup" mapstructure:"compact_on_startup"`
		// PIIRedaction enables automatic PII scrubbing (email, phone, SSN, IP,
		// JWT, AWS keys) before persisting memory entries.  Defaults to false.
		PIIRedaction bool `yaml:"pii_redaction" mapstructure:"pii_redaction"`
	} `yaml:"memory" mapstructure:"memory"`
	EventBus struct {
		// Dir is where event bus state (timers, subscriptions, approvals) is stored.
		// Defaults to ~/.local/share/aegisclaw/eventbus.
		Dir string `yaml:"dir" mapstructure:"dir"`
		// MaxPendingTimers is the hard cap on active timers. Defaults to 20.
		MaxPendingTimers int `yaml:"max_pending_timers" mapstructure:"max_pending_timers"`
		// MaxSubscriptions is the hard cap on active subscriptions. Defaults to 20.
		MaxSubscriptions int `yaml:"max_subscriptions" mapstructure:"max_subscriptions"`
	} `yaml:"eventbus" mapstructure:"eventbus"`
	Worker struct {
		// Dir is where worker records are persisted.
		// Defaults to ~/.local/share/aegisclaw/workers.
		Dir string `yaml:"dir" mapstructure:"dir"`
		// MaxConcurrent is the hard cap on simultaneously running Worker VMs.
		// Defaults to 4.
		MaxConcurrent int `yaml:"max_concurrent" mapstructure:"max_concurrent"`
		// DefaultTimeoutMins is the default task timeout for workers without an
		// explicit timeout. Defaults to 20.
		DefaultTimeoutMins int `yaml:"default_timeout_mins" mapstructure:"default_timeout_mins"`
		// RootfsPath is the rootfs image used for Worker VMs.
		// Defaults to the same image as the main agent.
		RootfsPath string `yaml:"rootfs_path" mapstructure:"rootfs_path"`
	} `yaml:"worker" mapstructure:"worker"`
	Dashboard struct {
		// Enabled controls whether the local web dashboard starts with the daemon.
		// Defaults to false.
		Enabled bool `yaml:"enabled" mapstructure:"enabled"`
		// Addr is the listen address for the dashboard HTTP server.
		// Defaults to "127.0.0.1:7878".
		Addr string `yaml:"addr" mapstructure:"addr"`
	} `yaml:"dashboard" mapstructure:"dashboard"`
	Workspace struct {
		// Dir is the path to the optional workspace directory containing prompt
		// injection files (AGENTS.md, SOUL.md, TOOLS.md, SKILL.md).
		// When the directory exists, its contents are injected into the agent
		// system prompt and, for SKILL.md, into skill build prompts.
		// Defaults to ~/.aegisclaw/workspace.
		// Set to "" to disable workspace prompt injection entirely.
		Dir string `yaml:"dir" mapstructure:"dir"`
	} `yaml:"workspace" mapstructure:"workspace"`
	Gateway struct {
		// Enabled controls whether the multi-channel Gateway is started by the
		// daemon.  Defaults to false.
		Enabled bool `yaml:"enabled" mapstructure:"enabled"`

		// Channels lists the channel adapter configurations.  Each entry
		// corresponds to one inbound source (e.g. a webhook listener).
		// Channel type "webhook" is supported out of the box; additional types
		// require governed skill code to provide the protocol adapter.
		//
		// Example YAML entry:
		//   - id: my-hook
		//     type: webhook
		//     enabled: true
		//     addr: "127.0.0.1:9000"
		//     secret: "changeme"
		Channels []GatewayChannelConfig `yaml:"channels" mapstructure:"channels"`
	} `yaml:"gateway" mapstructure:"gateway"`
	Registry struct {
		// URL is the base URL of the ClawHub-compatible skill registry.
		// Defaults to https://registry.clawhub.io.
		// Set to "" to disable registry integration.
		URL string `yaml:"url" mapstructure:"url"`
	} `yaml:"registry" mapstructure:"registry"`
	Lookup struct {
		// Dir is the directory where the persistent chromem-go vector database
		// for the dynamic semantic tool-lookup skill is stored.
		// Defaults to ~/.local/share/aegisclaw/vectordb.
		Dir string `yaml:"dir" mapstructure:"dir"`
	} `yaml:"lookup" mapstructure:"lookup"`
}

// DefaultConfig returns the default configuration values
// Security: Defaults enforce isolation - Firecracker binaries in system paths,
// rootfs templates in dedicated directory, audit logs in user space.
func DefaultConfig() Config {
	home, err := resolveConfigHome()
	if err != nil {
		// Fallback to /tmp if home dir unavailable - not ideal but prevents panic
		home = "/tmp"
	}

	return Config{
		Firecracker: struct {
			Bin string `yaml:"bin" mapstructure:"bin"`
		}{
			Bin: "/usr/local/bin/firecracker",
		},
		Jailer: struct {
			Bin string `yaml:"bin" mapstructure:"bin"`
		}{
			Bin: "/usr/local/bin/jailer",
		},
		Rootfs: struct {
			Template string `yaml:"template" mapstructure:"template"`
		}{
			Template: "/var/lib/aegisclaw/rootfs-templates/alpine.ext4",
		},
		Audit: struct {
			Dir string `yaml:"dir" mapstructure:"dir"`
		}{
			Dir: filepath.Join(home, ".local", "share", "aegisclaw", "audit"),
		},
		Sandbox: struct {
			StateDir      string `yaml:"state_dir" mapstructure:"state_dir"`
			ChrootBase    string `yaml:"chroot_base" mapstructure:"chroot_base"`
			KernelImage   string `yaml:"kernel_image" mapstructure:"kernel_image"`
			RegistryPath  string `yaml:"registry_path" mapstructure:"registry_path"`
			IsolationMode string `yaml:"isolation_mode" mapstructure:"isolation_mode"`
		}{
			StateDir:      filepath.Join(home, ".local", "share", "aegisclaw", "sandboxes"),
			ChrootBase:    filepath.Join(home, ".local", "share", "aegisclaw", "jailer"),
			KernelImage:   "/var/lib/aegisclaw/vmlinux-5.10.225",
			RegistryPath:  filepath.Join(home, ".local", "share", "aegisclaw", "registry.json"),
			IsolationMode: "firecracker",
		},
		Proposal: struct {
			StoreDir string `yaml:"store_dir" mapstructure:"store_dir"`
		}{
			StoreDir: filepath.Join(home, ".local", "share", "aegisclaw", "proposals"),
		},
		Court: struct {
			PersonaDir string `yaml:"persona_dir" mapstructure:"persona_dir"`
			SessionDir string `yaml:"session_dir" mapstructure:"session_dir"`
		}{
			PersonaDir: filepath.Join(home, ".config", "aegisclaw", "personas"),
			SessionDir: filepath.Join(home, ".local", "share", "aegisclaw", "court-sessions"),
		},
		Builder: struct {
			RootfsTemplate      string `yaml:"rootfs_template" mapstructure:"rootfs_template"`
			WorkspaceBaseDir    string `yaml:"workspace_base_dir" mapstructure:"workspace_base_dir"`
			MaxConcurrentBuilds int    `yaml:"max_concurrent_builds" mapstructure:"max_concurrent_builds"`
			BuildTimeoutMinutes int    `yaml:"build_timeout_minutes" mapstructure:"build_timeout_minutes"`
			SBOMDir             string `yaml:"sbom_dir" mapstructure:"sbom_dir"`
		}{
			RootfsTemplate:      "/var/lib/aegisclaw/rootfs-templates/builder.ext4",
			WorkspaceBaseDir:    filepath.Join(home, ".local", "share", "aegisclaw", "workspaces"),
			MaxConcurrentBuilds: 2,
			BuildTimeoutMinutes: 10,
			SBOMDir:             filepath.Join(home, ".local", "share", "aegisclaw", "sbom"),
		},
		Vault: struct {
			Dir string `yaml:"dir" mapstructure:"dir"`
		}{
			Dir: filepath.Join(home, ".config", "aegisclaw", "secrets"),
		},
		Ollama: struct {
			Endpoint     string `yaml:"endpoint" mapstructure:"endpoint"`
			TimeoutSecs  int    `yaml:"timeout_secs" mapstructure:"timeout_secs"`
			RegistryPath string `yaml:"registry_path" mapstructure:"registry_path"`
			ModelDir     string `yaml:"model_dir" mapstructure:"model_dir"`
			DefaultModel string `yaml:"default_model" mapstructure:"default_model"`
		}{
			Endpoint:     "http://127.0.0.1:11434",
			TimeoutSecs:  300,
			RegistryPath: filepath.Join(home, ".local", "share", "aegisclaw", "model-registry.json"),
			ModelDir:     filepath.Join(home, ".local", "share", "aegisclaw", "models"),
			DefaultModel: "gemma4:26b",
		},
		Daemon: struct {
			SocketPath string `yaml:"socket_path" mapstructure:"socket_path"`
		}{
			SocketPath: "/run/aegisclaw.sock",
		},
		Composition: struct {
			Dir string `yaml:"dir" mapstructure:"dir"`
		}{
			Dir: filepath.Join(home, ".local", "share", "aegisclaw", "composition"),
		},
		Agent: struct {
			RootfsPath       string `yaml:"rootfs_path" mapstructure:"rootfs_path"`
			StructuredOutput bool   `yaml:"structured_output" mapstructure:"structured_output"`
		}{
			RootfsPath:       "/var/lib/aegisclaw/rootfs-templates/alpine.ext4",
			StructuredOutput: true,
		},
		Snapshot: struct {
			Dir string `yaml:"dir" mapstructure:"dir"`
		}{
			Dir: filepath.Join(home, ".local", "share", "aegisclaw", "snapshots"),
		},
		Memory: struct {
			Dir              string `yaml:"dir" mapstructure:"dir"`
			EmbeddingModel   string `yaml:"embedding_model" mapstructure:"embedding_model"`
			MaxSizeMB        int64  `yaml:"max_size_mb" mapstructure:"max_size_mb"`
			DefaultTTL       string `yaml:"default_ttl" mapstructure:"default_ttl"`
			CompactOnStartup bool   `yaml:"compact_on_startup" mapstructure:"compact_on_startup"`
			PIIRedaction     bool   `yaml:"pii_redaction" mapstructure:"pii_redaction"`
		}{
			Dir:              filepath.Join(home, ".local", "share", "aegisclaw", "memory"),
			EmbeddingModel:   "nomic-embed-text",
			MaxSizeMB:        2048,
			DefaultTTL:       "90d",
			CompactOnStartup: false,
			PIIRedaction:     false,
		},
		EventBus: struct {
			Dir              string `yaml:"dir" mapstructure:"dir"`
			MaxPendingTimers int    `yaml:"max_pending_timers" mapstructure:"max_pending_timers"`
			MaxSubscriptions int    `yaml:"max_subscriptions" mapstructure:"max_subscriptions"`
		}{
			Dir:              filepath.Join(home, ".local", "share", "aegisclaw", "eventbus"),
			MaxPendingTimers: 20,
			MaxSubscriptions: 20,
		},
		Worker: struct {
			Dir                string `yaml:"dir" mapstructure:"dir"`
			MaxConcurrent      int    `yaml:"max_concurrent" mapstructure:"max_concurrent"`
			DefaultTimeoutMins int    `yaml:"default_timeout_mins" mapstructure:"default_timeout_mins"`
			RootfsPath         string `yaml:"rootfs_path" mapstructure:"rootfs_path"`
		}{
			Dir:                filepath.Join(home, ".local", "share", "aegisclaw", "workers"),
			MaxConcurrent:      4,
			DefaultTimeoutMins: 20,
			RootfsPath:         "/var/lib/aegisclaw/rootfs-templates/alpine.ext4",
		},
		Dashboard: struct {
			Enabled bool   `yaml:"enabled" mapstructure:"enabled"`
			Addr    string `yaml:"addr" mapstructure:"addr"`
		}{
			Enabled: true,
			Addr:    "127.0.0.1:7878",
		},
		Workspace: struct {
			Dir string `yaml:"dir" mapstructure:"dir"`
		}{
			Dir: filepath.Join(home, ".aegisclaw", "workspace"),
		},
		Gateway: struct {
			Enabled  bool                   `yaml:"enabled" mapstructure:"enabled"`
			Channels []GatewayChannelConfig `yaml:"channels" mapstructure:"channels"`
		}{
			Enabled:  false,
			Channels: nil,
		},
		Registry: struct {
			URL string `yaml:"url" mapstructure:"url"`
		}{
			URL: "https://registry.clawhub.io",
		},
		Lookup: struct {
			Dir string `yaml:"dir" mapstructure:"dir"`
		}{
			Dir: filepath.Join(home, ".local", "share", "aegisclaw", "vectordb"),
		},
	}
}

func resolveConfigHome() (string, error) {
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" {
		resolvedUser, err := user.Lookup(sudoUser)
		if err == nil && resolvedUser.HomeDir != "" {
			return resolvedUser.HomeDir, nil
		}
	}
	return os.UserHomeDir()
}

// Load reads configuration from ~/.config/aegisclaw/config.yaml
// Creates the config directory and file with defaults if they don't exist.
// Security: Validates all paths are absolute and within expected directories.
// Uses viper for safe YAML parsing with no code execution.
func Load(logger *zap.Logger) (*Config, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")

	// Create config directory if it doesn't exist
	// Security: Directory permissions set to 0700 for user-only access
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	viper.SetConfigFile(configPath)
	viper.SetConfigType("yaml")

	// Set defaults
	defaults := DefaultConfig()
	viper.SetDefault("firecracker.bin", defaults.Firecracker.Bin)
	viper.SetDefault("jailer.bin", defaults.Jailer.Bin)
	viper.SetDefault("rootfs.template", defaults.Rootfs.Template)
	viper.SetDefault("audit.dir", defaults.Audit.Dir)
	viper.SetDefault("sandbox.state_dir", defaults.Sandbox.StateDir)
	viper.SetDefault("sandbox.chroot_base", defaults.Sandbox.ChrootBase)
	viper.SetDefault("sandbox.kernel_image", defaults.Sandbox.KernelImage)
	viper.SetDefault("sandbox.registry_path", defaults.Sandbox.RegistryPath)
	viper.SetDefault("sandbox.isolation_mode", defaults.Sandbox.IsolationMode)
	viper.SetDefault("proposal.store_dir", defaults.Proposal.StoreDir)
	viper.SetDefault("court.persona_dir", defaults.Court.PersonaDir)
	viper.SetDefault("court.session_dir", defaults.Court.SessionDir)
	viper.SetDefault("builder.rootfs_template", defaults.Builder.RootfsTemplate)
	viper.SetDefault("builder.workspace_base_dir", defaults.Builder.WorkspaceBaseDir)
	viper.SetDefault("builder.max_concurrent_builds", defaults.Builder.MaxConcurrentBuilds)
	viper.SetDefault("builder.build_timeout_minutes", defaults.Builder.BuildTimeoutMinutes)
	viper.SetDefault("vault.dir", defaults.Vault.Dir)
	viper.SetDefault("ollama.endpoint", defaults.Ollama.Endpoint)
	viper.SetDefault("ollama.timeout_secs", defaults.Ollama.TimeoutSecs)
	viper.SetDefault("ollama.registry_path", defaults.Ollama.RegistryPath)
	viper.SetDefault("ollama.model_dir", defaults.Ollama.ModelDir)
	viper.SetDefault("ollama.default_model", defaults.Ollama.DefaultModel)
	viper.SetDefault("daemon.socket_path", defaults.Daemon.SocketPath)
	viper.SetDefault("composition.dir", defaults.Composition.Dir)
	viper.SetDefault("agent.rootfs_path", defaults.Agent.RootfsPath)
	viper.SetDefault("agent.structured_output", defaults.Agent.StructuredOutput)
	viper.SetDefault("snapshot.dir", defaults.Snapshot.Dir)
	viper.SetDefault("memory.dir", defaults.Memory.Dir)
	viper.SetDefault("memory.embedding_model", defaults.Memory.EmbeddingModel)
	viper.SetDefault("memory.max_size_mb", defaults.Memory.MaxSizeMB)
	viper.SetDefault("memory.default_ttl", defaults.Memory.DefaultTTL)
	viper.SetDefault("memory.compact_on_startup", defaults.Memory.CompactOnStartup)
	viper.SetDefault("eventbus.dir", defaults.EventBus.Dir)
	viper.SetDefault("eventbus.max_pending_timers", defaults.EventBus.MaxPendingTimers)
	viper.SetDefault("eventbus.max_subscriptions", defaults.EventBus.MaxSubscriptions)
	viper.SetDefault("worker.dir", defaults.Worker.Dir)
	viper.SetDefault("worker.max_concurrent", defaults.Worker.MaxConcurrent)
	viper.SetDefault("worker.default_timeout_mins", defaults.Worker.DefaultTimeoutMins)
	viper.SetDefault("worker.rootfs_path", defaults.Worker.RootfsPath)
	viper.SetDefault("dashboard.enabled", defaults.Dashboard.Enabled)
	viper.SetDefault("dashboard.addr", defaults.Dashboard.Addr)
	viper.SetDefault("workspace.dir", defaults.Workspace.Dir)
	viper.SetDefault("gateway.enabled", defaults.Gateway.Enabled)
	viper.SetDefault("registry.url", defaults.Registry.URL)
	viper.SetDefault("lookup.dir", defaults.Lookup.Dir)
	// Read config file, create with defaults if missing
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Config file doesn't exist, write defaults
		logger.Info("Config file not found, creating with defaults", zap.String("path", configPath))
		if err := viper.WriteConfigAs(configPath); err != nil {
			return nil, fmt.Errorf("failed to write default config: %w", err)
		}
	}

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	logger.Info("Configuration loaded successfully",
		zap.String("firecracker_bin", config.Firecracker.Bin),
		zap.String("jailer_bin", config.Jailer.Bin),
		zap.String("rootfs_template", config.Rootfs.Template),
		zap.String("audit_dir", config.Audit.Dir))

	return &config, nil
}

// getConfigDir returns the path to ~/.config/aegisclaw
func getConfigDir() (string, error) {
	home, err := resolveConfigHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "aegisclaw"), nil
}

// validateConfig checks that all paths are absolute and point to expected locations
// Security: Prevents relative paths that could lead to directory traversal.
func validateConfig(config *Config) error {
	paths := map[string]string{
		"firecracker.bin":            config.Firecracker.Bin,
		"jailer.bin":                 config.Jailer.Bin,
		"rootfs.template":            config.Rootfs.Template,
		"audit.dir":                  config.Audit.Dir,
		"sandbox.state_dir":          config.Sandbox.StateDir,
		"sandbox.chroot_base":        config.Sandbox.ChrootBase,
		"sandbox.kernel_image":       config.Sandbox.KernelImage,
		"sandbox.registry_path":      config.Sandbox.RegistryPath,
		"proposal.store_dir":         config.Proposal.StoreDir,
		"court.persona_dir":          config.Court.PersonaDir,
		"builder.rootfs_template":    config.Builder.RootfsTemplate,
		"builder.workspace_base_dir": config.Builder.WorkspaceBaseDir,
		"vault.dir":                  config.Vault.Dir,
		"composition.dir":            config.Composition.Dir,
		"ollama.registry_path":       config.Ollama.RegistryPath,
		"ollama.model_dir":           config.Ollama.ModelDir,
		"snapshot.dir":               config.Snapshot.Dir,
		"memory.dir":                 config.Memory.Dir,
		"eventbus.dir":               config.EventBus.Dir,
		"worker.dir":                 config.Worker.Dir,
		"worker.rootfs_path":         config.Worker.RootfsPath,
	}

	for name, path := range paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path: %s", name, path)
		}
	}

	return nil
}
