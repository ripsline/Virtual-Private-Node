// Package config manages the persistent node configuration.
// The config file is written once at install time and read
// on every login to display the correct welcome message.
package config

import (
    "encoding/json"
    "os"
)

const configDir = "/etc/rlvpn"
const configPath = "/etc/rlvpn/config.json"

// AppConfig stores the choices made during installation.
type AppConfig struct {
    Network    string `json:"network"`     // "mainnet" or "testnet4"
    Components string `json:"components"`  // "bitcoin" or "bitcoin+lnd"
    PruneSize  int    `json:"prune_size"`  // in GB
    P2PMode    string `json:"p2p_mode"`    // "tor" or "hybrid"
    AutoUnlock bool   `json:"auto_unlock"`
    SSHPort    int    `json:"ssh_port"`
}

// Default returns a config with sensible defaults.
func Default() *AppConfig {
    return &AppConfig{
        Network:    "testnet4",
        Components: "bitcoin+lnd",
        PruneSize:  25,
        P2PMode:    "tor",
        SSHPort:    22,
    }
}

// Load reads the config from disk.
func Load() (*AppConfig, error) {
    data, err := os.ReadFile(configPath)
    if err != nil {
        return nil, err
    }

    var cfg AppConfig
    if err := json.Unmarshal(data, &cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}

// Save writes the config to disk.
func Save(cfg *AppConfig) error {
    if err := os.MkdirAll(configDir, 0755); err != nil {
        return err
    }

    data, err := json.MarshalIndent(cfg, "", "  ")
    if err != nil {
        return err
    }

    return os.WriteFile(configPath, data, 0644)
}

// HasLND returns true if LND was installed.
func (c *AppConfig) HasLND() bool {
    return c.Components == "bitcoin+lnd"
}

// IsMainnet returns true if running on mainnet.
func (c *AppConfig) IsMainnet() bool {
    return c.Network == "mainnet"
}