package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikegio27/go-input-remapper/internal/paths"
	"github.com/pelletier/go-toml/v2"
)

// SaveGlobal writes config.toml (global settings only — profiles live in their
// own files). It creates the config directory if needed.
func SaveGlobal(dir string, cfg *Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return writeAtomic(paths.ConfigFile(dir), data)
}

// SaveProfile writes a single profile to profiles/<key>.toml, creating the
// profiles directory if needed.
func SaveProfile(dir, key string, p *Profile) error {
	profDir := paths.ProfilesDir(dir)
	if err := os.MkdirAll(profDir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile %q: %w", key, err)
	}
	return writeAtomic(filepath.Join(profDir, key+".toml"), data)
}

// DeleteProfile removes a profile's file. A missing file is not an error.
func DeleteProfile(dir, key string) error {
	err := os.Remove(filepath.Join(paths.ProfilesDir(dir), key+".toml"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Save writes the global config and every profile. Profile keys come from the
// Profiles map.
func Save(dir string, cfg *Config) error {
	if err := SaveGlobal(dir, cfg); err != nil {
		return err
	}
	for key, p := range cfg.Profiles {
		if err := SaveProfile(dir, key, p); err != nil {
			return err
		}
	}
	return nil
}

// writeAtomic writes data to a temp file in the same directory and renames it
// into place, so a reader (notably the daemon's config watcher) never observes a
// half-written file.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*."+filepath.Base(path))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
