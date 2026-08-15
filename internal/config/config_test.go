package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "etc-vpn")
	return &Store{
		Dir:      dir,
		Path:     filepath.Join(dir, "config.json"),
		OwnerUID: os.Geteuid(),
		GroupGID: os.Getegid(),
	}
}

func TestDefaultSystemConfig(t *testing.T) {
	cfg := Default()
	if cfg.Schema != SystemSchema || cfg.Network != "mainnet" ||
		cfg.PruneSize != 25 || cfg.DbCache != 512 ||
		cfg.P2PMode != "tor" || cfg.AutoUnlock || cfg.SyncthingEnabled {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if !cfg.HasLND() {
		t.Fatal("LND must be part of the base installation")
	}
}

func TestSystemConfigContainsOnlyDesiredNonSecretFields(t *testing.T) {
	data, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw) != len(systemFields) {
		t.Fatalf("serialized %d fields, want %d: %s", len(raw), len(systemFields), data)
	}
	for name := range systemFields {
		if _, ok := raw[name]; !ok {
			t.Errorf("missing desired field %q", name)
		}
	}
	for _, forbidden := range []string{
		"components", "lnd_installed", "wallet_created",
		"syncthing_installed", "syncthing_password",
		"syncthing_devices", "theme", "ssh_ports",
		"key_verification_pending", "ssh_password_auth_disabled",
	} {
		if _, ok := raw[forbidden]; ok {
			t.Errorf("authority-mixed field %q remains", forbidden)
		}
	}
}

func TestDecodeSystemStrict(t *testing.T) {
	valid := `{"schema":1,"network":"testnet4","prune_size_gb":25,"db_cache_mb":1024,"p2p_mode":"hybrid","auto_unlock_enabled":true,"syncthing_enabled":false}`
	cfg, err := decodeSystem([]byte(valid))
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if cfg.Network != "testnet4" || cfg.DbCache != 1024 ||
		cfg.P2PMode != "hybrid" || !cfg.AutoUnlock || cfg.SyncthingEnabled {
		t.Fatalf("decoded wrong values: %+v", cfg)
	}

	cases := map[string]string{
		"malformed":          `{`,
		"not object":         `[]`,
		"unknown":            strings.TrimSuffix(valid, `}`) + `,"extra":1}`,
		"duplicate":          strings.Replace(valid, `"schema":1`, `"schema":1,"schema":1`, 1),
		"missing":            strings.Replace(valid, `"network":"testnet4",`, ``, 1),
		"future schema":      strings.Replace(valid, `"schema":1`, `"schema":2`, 1),
		"bad network":        strings.Replace(valid, `"testnet4"`, `"signet"`, 1),
		"bad p2p":            strings.Replace(valid, `"hybrid"`, `"clearnet"`, 1),
		"bad db cache":       strings.Replace(valid, `1024`, `999`, 1),
		"zero prune":         strings.Replace(valid, `"prune_size_gb":25`, `"prune_size_gb":0`, 1),
		"trailing value":     valid + ` {}`,
		"wrong boolean type": strings.Replace(valid, `"syncthing_enabled":false`, `"syncthing_enabled":"false"`, 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeSystem([]byte(input)); err == nil {
				t.Fatalf("accepted %s", input)
			}
		})
	}
}

func TestDecodeSystemAcceptsPublicSignetProfile(t *testing.T) {
	data := []byte(`{"schema":1,"network":"public-signet","prune_size_gb":25,"db_cache_mb":512,"p2p_mode":"tor","auto_unlock_enabled":false,"syncthing_enabled":false}`)
	cfg, err := decodeSystem(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Network != NetworkPublicSignet {
		t.Fatalf("network = %q", cfg.Network)
	}
}

func TestStoreSaveLoadAndMetadata(t *testing.T) {
	store := testStore(t)
	cfg := Default()
	cfg.Network = "testnet4"
	cfg.DbCache = 2048
	cfg.SyncthingEnabled = true
	if err := store.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	for path, want := range map[string]os.FileMode{
		store.Dir: 0o750, store.Path: 0o640,
	} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Errorf("%s mode %04o, want %04o", path, info.Mode().Perm(), want)
		}
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Network != "testnet4" || loaded.DbCache != 2048 || !loaded.SyncthingEnabled {
		t.Fatalf("loaded wrong config: %+v", loaded)
	}
}

func TestStoreAtomicallyReplacesConfig(t *testing.T) {
	store := testStore(t)
	if err := store.Save(Default()); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.P2PMode = "hybrid"
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("config inode was modified in place instead of replaced")
	}
	loaded, err := store.Load()
	if err != nil || loaded.P2PMode != "hybrid" {
		t.Fatalf("replacement not readable: %+v, %v", loaded, err)
	}
}

func TestStoreRefusesUnsafeObjects(t *testing.T) {
	t.Run("directory symlink", func(t *testing.T) {
		base := t.TempDir()
		realDir := filepath.Join(base, "real")
		if err := os.Mkdir(realDir, 0o750); err != nil {
			t.Fatal(err)
		}
		store := &Store{Dir: filepath.Join(base, "link"), Path: filepath.Join(base, "link", "config.json"), OwnerUID: os.Geteuid(), GroupGID: os.Getegid()}
		if err := os.Symlink(realDir, store.Dir); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(Default()); err == nil {
			t.Fatal("saved through directory symlink")
		}
	})

	t.Run("destination symlink", func(t *testing.T) {
		store := testStore(t)
		if err := os.Mkdir(store.Dir, 0o750); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte("protected"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, store.Path); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(Default()); err == nil {
			t.Fatal("replaced destination symlink")
		}
		data, _ := os.ReadFile(target)
		if string(data) != "protected" {
			t.Fatal("symlink target changed")
		}
	})

	t.Run("load wrong mode", func(t *testing.T) {
		store := testStore(t)
		if err := store.Save(Default()); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(store.Path, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatal("loaded config with wrong mode")
		}
	})
}

func TestStoreRefusesInvalidConfigBeforeMutation(t *testing.T) {
	store := testStore(t)
	cfg := Default()
	cfg.P2PMode = "bogus"
	if err := store.Save(cfg); err == nil {
		t.Fatal("saved invalid config")
	}
	if _, err := os.Lstat(store.Dir); !os.IsNotExist(err) {
		t.Fatalf("invalid save mutated filesystem: %v", err)
	}
}

func TestNetworkConfigRouting(t *testing.T) {
	mainnet := Default()
	mainProfile, err := mainnet.NetworkConfig()
	if err != nil {
		t.Fatal(err)
	}
	if mainProfile.RPCPort != 8332 {
		t.Fatalf("mainnet RPC port %d", mainProfile.RPCPort)
	}
	testnet := Default()
	testnet.Network = NetworkTestnet4
	testProfile, err := testnet.NetworkConfig()
	if err != nil {
		t.Fatal(err)
	}
	if testProfile.RPCPort != 48332 {
		t.Fatalf("testnet4 RPC port %d", testProfile.RPCPort)
	}
}
