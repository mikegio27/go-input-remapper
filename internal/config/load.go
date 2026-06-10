package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikegio27/nereus/internal/device"
	"github.com/mikegio27/nereus/internal/paths"
	"github.com/pelletier/go-toml/v2"
)

// Load reads the global config.toml and every profiles/*.toml under dir. A
// missing config directory or missing config.toml is not an error: it yields a
// default, empty config so a first run can start and the TUI can populate it.
// Each profile is keyed by its filename stem; a profile whose name field is
// empty inherits the stem.
func Load(dir string) (*Config, error) {
	cfg := &Config{
		VirtualPrefix: device.DefaultVirtualPrefix,
		Profiles:      map[string]*Profile{},
	}

	if data, err := os.ReadFile(paths.ConfigFile(dir)); err == nil {
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", paths.ConfigFile(dir), err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", paths.ConfigFile(dir), err)
	}
	if cfg.VirtualPrefix == "" {
		cfg.VirtualPrefix = device.DefaultVirtualPrefix
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]*Profile{}
	}

	files, err := filepath.Glob(filepath.Join(paths.ProfilesDir(dir), "*.toml"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		p, err := loadProfile(f)
		if err != nil {
			return nil, err
		}
		key := strings.TrimSuffix(filepath.Base(f), ".toml")
		if p.Name == "" {
			p.Name = key
		}
		cfg.Profiles[key] = p
	}
	return cfg, nil
}

// loadProfile reads and parses a single profile file.
func loadProfile(path string) (*Profile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var p Profile
	if err := toml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &p, nil
}

// ActiveProfile returns the profile named by ActiveProfile, or nil if unset or
// not found.
func (c *Config) ActiveProfileOrNil() *Profile {
	if c.ActiveProfile == "" {
		return nil
	}
	return c.Profiles[c.ActiveProfile]
}
