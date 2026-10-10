package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Admin    AdminConfig    `yaml:"admin"`
	Database DatabaseConfig `yaml:"database"`
	Logging  LoggingConfig  `yaml:"logging"`
	Printer  PrinterConfig  `yaml:"printer"`
	Paths    PathsConfig    `yaml:"paths"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
	// OpenBrowser opens the kiosk start page on launch.
	// nil = default (true on Windows, false elsewhere).
	OpenBrowser *bool `yaml:"open_browser"`
}

func (c *Config) ShouldOpenBrowser(goos string) bool {
	if c.Server.OpenBrowser != nil {
		return *c.Server.OpenBrowser
	}
	return goos == "windows"
}

type AdminConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type LoggingConfig struct {
	Path  string `yaml:"path"`
	Level string `yaml:"level"`
}

type PrinterConfig struct {
	// Name of Windows printer queue. Empty = system default.
	Name string `yaml:"name"`
	// Optional path to SumatraPDF.exe for silent PDF printing on Windows.
	Sumatra string `yaml:"sumatra"`
	// DryRun skips real printing (useful while developing without a printer).
	DryRun bool `yaml:"dry_run"`
}

type PathsConfig struct {
	LibreOffice string `yaml:"libreoffice"`
	// LibreOfficeAutoInstall installs LibreOffice on Windows when missing.
	// nil = default true on Windows.
	LibreOfficeAutoInstall *bool  `yaml:"libreoffice_auto_install"`
	LibreOfficeMSIURL      string `yaml:"libreoffice_msi_url"`
	PrintJobs              string `yaml:"print_jobs"`
}

func (c *Config) ShouldAutoInstallLibreOffice(goos string) bool {
	if c.Paths.LibreOfficeAutoInstall != nil {
		return *c.Paths.LibreOfficeAutoInstall
	}
	return goos == "windows"
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Database.Path == "" {
		c.Database.Path = "data/kiosk.db"
	}
	if c.Logging.Path == "" {
		c.Logging.Path = "logs/kiosk.log"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Paths.LibreOffice == "" {
		c.Paths.LibreOffice = "soffice"
	}
	if c.Paths.PrintJobs == "" {
		c.Paths.PrintJobs = "data/print-jobs"
	}
}

// DataRoot is the parent directory of print jobs (usually "data").
func (c *Config) DataRoot() string {
	dir := filepath.Dir(c.Paths.PrintJobs)
	if dir == "" || dir == "." {
		return "data"
	}
	return dir
}

func (c *Config) ScanJobsDir() string {
	return filepath.Join(c.DataRoot(), "scan-jobs")
}

func (c *Config) EmailInboxDir() string {
	return filepath.Join(c.DataRoot(), "email-inbox")
}

func (c *Config) validate() error {
	if c.Admin.Username == "" {
		return fmt.Errorf("admin.username is required")
	}
	if c.Admin.Password == "" {
		return fmt.Errorf("admin.password is required")
	}
	return nil
}
