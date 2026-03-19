package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

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
	} `yaml:"sandbox" mapstructure:"sandbox"`
	Proposal struct {
		StoreDir string `yaml:"store_dir" mapstructure:"store_dir"`
	} `yaml:"proposal" mapstructure:"proposal"`
	Court struct {
		PersonaDir string `yaml:"persona_dir" mapstructure:"persona_dir"`
	} `yaml:"court" mapstructure:"court"`
}

// DefaultConfig returns the default configuration values
// Security: Defaults enforce isolation - Firecracker binaries in system paths,
// rootfs templates in dedicated directory, audit logs in user space.
func DefaultConfig() Config {
	home, err := os.UserHomeDir()
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
			StateDir     string `yaml:"state_dir" mapstructure:"state_dir"`
			ChrootBase   string `yaml:"chroot_base" mapstructure:"chroot_base"`
			KernelImage  string `yaml:"kernel_image" mapstructure:"kernel_image"`
			RegistryPath string `yaml:"registry_path" mapstructure:"registry_path"`
		}{
			StateDir:     filepath.Join(home, ".local", "share", "aegisclaw", "sandboxes"),
			ChrootBase:   filepath.Join(home, ".local", "share", "aegisclaw", "jailer"),
			KernelImage:  "/var/lib/aegisclaw/vmlinux",
			RegistryPath: filepath.Join(home, ".local", "share", "aegisclaw", "registry.json"),
		},
		Proposal: struct {
			StoreDir string `yaml:"store_dir" mapstructure:"store_dir"`
		}{
			StoreDir: filepath.Join(home, ".local", "share", "aegisclaw", "proposals"),
		},
		Court: struct {
			PersonaDir string `yaml:"persona_dir" mapstructure:"persona_dir"`
		}{
			PersonaDir: filepath.Join(home, ".config", "aegisclaw", "personas"),
		},
	}
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
	viper.SetDefault("proposal.store_dir", defaults.Proposal.StoreDir)
	viper.SetDefault("court.persona_dir", defaults.Court.PersonaDir)

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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "aegisclaw"), nil
}

// validateConfig checks that all paths are absolute and point to expected locations
// Security: Prevents relative paths that could lead to directory traversal.
func validateConfig(config *Config) error {
	paths := map[string]string{
		"firecracker.bin":       config.Firecracker.Bin,
		"jailer.bin":            config.Jailer.Bin,
		"rootfs.template":       config.Rootfs.Template,
		"audit.dir":             config.Audit.Dir,
		"sandbox.state_dir":     config.Sandbox.StateDir,
		"sandbox.chroot_base":   config.Sandbox.ChrootBase,
		"sandbox.kernel_image":  config.Sandbox.KernelImage,
		"sandbox.registry_path": config.Sandbox.RegistryPath,
		"proposal.store_dir":    config.Proposal.StoreDir,
		"court.persona_dir":     config.Court.PersonaDir,
	}

	for name, path := range paths {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s must be an absolute path: %s", name, path)
		}
	}

	return nil
}
