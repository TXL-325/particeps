package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen              string  `yaml:"listen"`
	SessionCookieSecure bool    `yaml:"session_cookie_secure"`
	DataDir             string  `yaml:"data_dir"`
	IncusSocket         string  `yaml:"incus_socket"`
	IncusProject        string  `yaml:"incus_project"`
	StoragePool         string  `yaml:"storage_pool"`
	Network             string  `yaml:"network"`
	PrivateIPv4CIDR     string  `yaml:"private_ipv4_cidr"`
	TaskConcurrency     int     `yaml:"task_concurrency"`
	CPUCapCores         float64 `yaml:"cpu_cap_cores"`
	PortPoolStart       int     `yaml:"port_pool_start"`
	PortPoolEnd         int     `yaml:"port_pool_end"`
	PortsPerGuest       int     `yaml:"ports_per_guest"`
	SourceIPLimit       int     `yaml:"source_ip_limit"`
	SampleSeconds       int     `yaml:"sample_seconds"`
}

func Defaults() Config {
	return Config{
		Listen:              "0.0.0.0:8792",
		SessionCookieSecure: false,
		DataDir:             "/var/lib/particeps",
		IncusSocket:         "/var/lib/incus/unix.socket",
		IncusProject:        "particeps",
		StoragePool:         "particeps-pool",
		Network:             "particepsbr0",
		PrivateIPv4CIDR:     "10.80.0.0/24",
		TaskConcurrency:     2,
		CPUCapCores:         0, // 0 = 75% of host CPUs at start
		PortPoolStart:       20000,
		PortPoolEnd:         59999,
		PortsPerGuest:       20,
		SourceIPLimit:       0,
		SampleSeconds:       5,
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	if cfg.TaskConcurrency < 1 {
		cfg.TaskConcurrency = 1
	}
	if cfg.SampleSeconds < 1 {
		cfg.SampleSeconds = 5
	}
	return cfg, nil
}

func (c Config) StateDB() string   { return filepath.Join(c.DataDir, "state.db") }
func (c Config) MetricsDB() string { return filepath.Join(c.DataDir, "metrics.db") }
func (c Config) Bootstrap() string { return filepath.Join(c.DataDir, "admin-bootstrap.txt") }
