package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config holds saved profiles for diffwatch.
type Config struct {
	Profiles map[string][]string `json:"profiles"`
}

// configPath returns the path to the config file.
func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "diffwatch", "config.json")
}

// loadConfig reads the config from disk, returning an empty config if it doesn't exist.
func loadConfig() (*Config, error) {
	data, err := os.ReadFile(configPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Profiles: make(map[string][]string)}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string][]string)
	}
	return &cfg, nil
}

// saveConfig writes the config to disk.
func saveConfig(cfg *Config) error {
	path := configPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// expandPath expands ~ to the home directory.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// listProfiles prints all saved profiles.
func listProfiles() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	if len(cfg.Profiles) == 0 {
		fmt.Println("No saved profiles. Use --save <name> <path>... to create one.")
		return
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		paths := cfg.Profiles[name]
		fmt.Printf("  %s: %s\n", name, strings.Join(paths, " "))
	}
}

// saveProfile saves a named profile with the given paths.
func saveProfile(name string, paths []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Store paths with ~ for home dir to keep them portable
	home, _ := os.UserHomeDir()
	storedPaths := make([]string, len(paths))
	for i, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if strings.HasPrefix(abs, home+string(os.PathSeparator)) {
			storedPaths[i] = "~/" + abs[len(home)+1:]
		} else {
			storedPaths[i] = abs
		}
	}

	cfg.Profiles[name] = storedPaths
	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Saved profile '%s': %s\n", name, strings.Join(storedPaths, " "))
}

// deleteProfile removes a saved profile.
func deleteProfile(name string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	if _, ok := cfg.Profiles[name]; !ok {
		fmt.Fprintf(os.Stderr, "Profile '%s' not found.\n", name)
		os.Exit(1)
	}
	delete(cfg.Profiles, name)
	if err := saveConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deleted profile '%s'.\n", name)
}

// resolveProfile checks if a single arg matches a profile name and returns expanded paths.
// Returns nil if no profile matches.
func resolveProfile(name string) []string {
	cfg, err := loadConfig()
	if err != nil {
		return nil
	}
	paths, ok := cfg.Profiles[name]
	if !ok {
		return nil
	}
	expanded := make([]string, len(paths))
	for i, p := range paths {
		expanded[i] = expandPath(p)
	}
	return expanded
}
