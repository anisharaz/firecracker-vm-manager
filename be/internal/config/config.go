package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config is the manager-level configuration.
type Config struct {
	ListenAddr         string      `yaml:"listen_addr"`
	BridgeName         string      `yaml:"bridge_name"`
	StateDir           string      `yaml:"state_dir"`
	KernelPath         string      `yaml:"kernel_path"`
	RootfsTemplate     string      `yaml:"rootfs_template"`
	TapPrefix          string      `yaml:"tap_prefix"`
	MacPrefix          string      `yaml:"mac_prefix"`
	DefaultVCPUs       int         `yaml:"default_vcpus"`
	DefaultMemMiB      int         `yaml:"default_mem_mib"`
	DefaultDiskSizeMiB int         `yaml:"default_disk_size_mib"`
	DefaultKernelArgs  string      `yaml:"default_kernel_args"`
	Store              StoreConfig `yaml:"store"`
}

// StoreConfig selects which Store implementation to use.
type StoreConfig struct {
	Type  string           `yaml:"type"` // "json" | "mongo"
	JSON  JSONStoreConfig  `yaml:"json"`
	Mongo MongoStoreConfig `yaml:"mongo"`
}

type JSONStoreConfig struct {
	Path string `yaml:"path"`
}

type MongoStoreConfig struct {
	URI        string `yaml:"uri"`
	Database   string `yaml:"database"`
	Collection string `yaml:"collection"`
}

// Defaults returns a Config with sensible defaults applied.
func Defaults() Config {
	return Config{
		ListenAddr:         "127.0.0.1:8080",
		BridgeName:         "br0",
		StateDir:           "/var/lib/firecracker-manager",
		TapPrefix:          "fcmtap",
		MacPrefix:          "AA:FC:00",
		DefaultVCPUs:       2,
		DefaultMemMiB:      1024,
		DefaultDiskSizeMiB: 2048,
		DefaultKernelArgs:  "console=ttyS0 reboot=k panic=1 pci=off",
		Store: StoreConfig{
			Type: "json",
			JSON: JSONStoreConfig{Path: ""},
		},
	}
}

// Load reads a YAML file and applies env overrides.
// If path is empty, only defaults + env are used.
func Load(path string) (Config, error) {
	cfg := Defaults()

	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(&cfg)

	if cfg.Store.Type == "json" && cfg.Store.JSON.Path == "" {
		cfg.Store.JSON.Path = cfg.StateDir + "/vms.json"
	}

	return cfg, cfg.Validate()
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("MANAGER_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("MANAGER_BRIDGE_NAME"); v != "" {
		cfg.BridgeName = v
	}
	if v := os.Getenv("MANAGER_STATE_DIR"); v != "" {
		cfg.StateDir = v
	}
	if v := os.Getenv("MANAGER_KERNEL_PATH"); v != "" {
		cfg.KernelPath = v
	}
	if v := os.Getenv("MANAGER_ROOTFS_TEMPLATE"); v != "" {
		cfg.RootfsTemplate = v
	}
	if v := os.Getenv("MANAGER_STORE_TYPE"); v != "" {
		cfg.Store.Type = v
	}
	if v := os.Getenv("MANAGER_STORE_JSON_PATH"); v != "" {
		cfg.Store.JSON.Path = v
	}
	if v := os.Getenv("MANAGER_STORE_MONGO_URI"); v != "" {
		cfg.Store.Mongo.URI = v
	}
	if v := os.Getenv("MANAGER_DEFAULT_DISK_SIZE_MIB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.DefaultDiskSizeMiB = n
		}
	}
}

// Validate checks required fields.
func (c Config) Validate() error {
	if c.BridgeName == "" {
		return fmt.Errorf("bridge_name is required")
	}
	if c.StateDir == "" {
		return fmt.Errorf("state_dir is required")
	}
	if c.KernelPath == "" {
		return fmt.Errorf("kernel_path is required")
	}
	if c.RootfsTemplate == "" {
		return fmt.Errorf("rootfs_template is required")
	}
	switch c.Store.Type {
	case "json":
		// path defaulted in Load
	case "mongo":
		if c.Store.Mongo.URI == "" {
			return fmt.Errorf("store.mongo.uri is required when store.type=mongo")
		}
	default:
		return fmt.Errorf("unknown store.type %q (want json|mongo)", c.Store.Type)
	}
	return nil
}
