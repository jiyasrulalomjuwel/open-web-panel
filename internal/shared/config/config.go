package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Paths    PathsConfig    `yaml:"paths"`
	Email    EmailConfig    `yaml:"email"`
}

type ServerConfig struct {
	ListenAddr string `yaml:"listen_addr"` // e.g., :9000
	Domain     string `yaml:"domain"`      // e.g., panel.example.com
}

type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Name     string `yaml:"name"`
}

type AuthConfig struct {
	JWTSecret        string `yaml:"jwt_secret"`
	AccessTokenTTL   int    `yaml:"access_token_ttl"`   // seconds, default 900
	RefreshTokenTTL  int    `yaml:"refresh_token_ttl"`  // seconds, default 604800
	BcryptCost       int    `yaml:"bcrypt_cost"`        // default 12
}

type PathsConfig struct {
	HomeDirBase    string `yaml:"home_dir_base"`    // /home
	VhostDir       string `yaml:"vhost_dir"`        // /etc/nginx/sites-available
	VhostEnabled   string `yaml:"vhost_enabled"`    // /etc/nginx/sites-enabled
	BackupDir      string `yaml:"backup_dir"`       // /backup
	ChildConfigDir string `yaml:"child_config_dir"` // .owp
}

type EmailConfig struct {
	SMTPHost string `yaml:"smtp_host"`
	SMTPPort int    `yaml:"smtp_port"`
	From     string `yaml:"from"`
}

func Default() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr: ":9000",
			Domain:     "localhost",
		},
		Database: DatabaseConfig{
			Host: "127.0.0.1",
			Port: 3306,
			User: "root",
			Name: "openwebpanel_parent",
		},
		Auth: AuthConfig{
			JWTSecret:       "",
			AccessTokenTTL:  900,
			RefreshTokenTTL: 604800,
			BcryptCost:      12,
		},
		Paths: PathsConfig{
			HomeDirBase:    "/home",
			VhostDir:       "/etc/nginx/sites-available",
			VhostEnabled:   "/etc/nginx/sites-enabled",
			BackupDir:      "/backup",
			ChildConfigDir: ".owp",
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
