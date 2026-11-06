// Package config manages local, user-editable application settings.
package config

import (
	"cmp"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ModelURL           string `json:"model_url"`
	ModelName          string `json:"model_name"`
	ModelKey           string `json:"model_key"`
	Kubeconfig         string `json:"kubeconfig"`
	Context            string `json:"context"`
	Remediation        bool   `json:"remediation"`
	InspectionInterval string `json:"inspection_interval"`
}

func Defaults() Config {
	return Config{
		ModelURL: cmp.Or(os.Getenv("MODEL_BASE_URL"), "http://localhost:11434/v1"), ModelName: cmp.Or(os.Getenv("MODEL_NAME"), "qwen2.5:7b"), ModelKey: cmp.Or(os.Getenv("MODEL_API_KEY"), "ollama"),
		Kubeconfig: os.Getenv("KUBECONFIG"), Remediation: os.Getenv("KUBEPILOT_ENABLE_REMEDIATION") == "true", InspectionInterval: os.Getenv("KUBEPILOT_INSPECTION_INTERVAL"),
	}
}
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "kubepilot", "config.json"), nil
}
func Load(path string) (Config, error) {
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("read settings: %w", err)
	}
	return cfg, nil
}
func (c Config) Save(path string) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".config-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(append(data, '\n'))
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}

type Field struct {
	Label, Description string
	Secret, Toggle     bool
}

var Fields = []Field{
	{"Model endpoint", "OpenAI-compatible URL", false, false},
	{"Model name", "Model with tool calling support", false, false},
	{"Model API key", "Stored locally; hidden in the interface", true, false},
	{"Kubeconfig", "Paths separated by the OS path separator; blank uses the default", false, false},
	{"Allow remediation", "Requires review and confirmation for every plan", false, true},
	{"Inspection interval", "Blank disables; otherwise at least 1m", false, false},
	{"Kubernetes context", "Choose a context from the active kubeconfig", false, false},
}

func (c Config) Value(i int) string {
	switch i {
	case 0:
		return c.ModelURL
	case 1:
		return c.ModelName
	case 2:
		return c.ModelKey
	case 3:
		return c.Kubeconfig
	case 4:
		return strconv.FormatBool(c.Remediation)
	case 5:
		return c.InspectionInterval
	case 6:
		return c.Context
	}
	return ""
}
func (c *Config) Set(i int, value string) error {
	if i != 2 {
		value = strings.TrimSpace(value)
	}
	switch i {
	case 0:
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("enter a valid http(s) URL without embedded credentials")
		}
		c.ModelURL = value
	case 1:
		if value == "" {
			return errors.New("model name cannot be empty")
		}
		c.ModelName = value
	case 2:
		c.ModelKey = value
	case 3:
		c.Kubeconfig = value
	case 4:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		c.Remediation = v
	case 5:
		if value != "" {
			d, err := time.ParseDuration(value)
			if err != nil || d < time.Minute {
				return errors.New("use a duration of at least 1m, or clear to disable")
			}
		}
		c.InspectionInterval = value

	default:
		return errors.New("unknown setting")
	}
	return nil
}
