package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	defaultNOCQueue             = "Network Operation Center (NOC)"
	defaultEligibleServiceTypes = "MNet,MNet Plus,G2 Net,G2 Plus"
	defaultOPIFields            = "opi_site_code"
	// RT lifecycle statuses a NOC ticket may be in to be automated.
	defaultAllowedTicketStatuses = "new,in_progress,re-open,re-open-1,re-open-2,re-open-3,re-open-4,re-open-5"
	defaultAPITimeout            = 30 * time.Second
)

// Config is read from an env file; a variable already set in the process
// environment wins over the file. The MYSQL_DB_* / DOMAIN_PORT /
// DEFAULT_TOKEN names match noc_automation's .env, so that file can be used
// directly (--env ../../NocAutomationCodeMerge/noc_automation/.env).
type Config struct {
	APIURL                string
	APIToken              string
	APITimeout            time.Duration
	NOCQueue              string
	AllowedTicketStatuses []string
	EligibleServiceTypes  []string
	OPIFields             []string
	DB                    DBConfig
}

type DBConfig struct {
	Host, Port, User, Password, Name string
}

func (d DBConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Name)
}

// envGetter returns a lookup for envPath where the process environment wins
// over the file; a missing file is not an error.
func envGetter(envPath string) (func(key, fallback string) string, error) {
	file := map[string]string{}
	if envPath != "" {
		values, err := godotenv.Read(envPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", envPath, err)
		}
		if err == nil {
			file = values
		}
	}
	return func(key, fallback string) string {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
		if v := strings.TrimSpace(file[key]); v != "" {
			return v
		}
		return fallback
	}, nil
}

func dbConfig(get func(key, fallback string) string) DBConfig {
	return DBConfig{
		Host:     get("MYSQL_DB_HOST", ""),
		Port:     get("MYSQL_DB_PORT", "3306"),
		User:     get("MYSQL_DB_USERNAME", ""),
		Password: get("MYSQL_DB_PASSWORD", ""),
		Name:     get("MYSQL_DB_DATABASE", ""),
	}
}

// LoadDBConfig reads only the MySQL settings, for commands that do not call
// the RT External API (export).
func LoadDBConfig(envPath string) (DBConfig, error) {
	get, err := envGetter(envPath)
	if err != nil {
		return DBConfig{}, err
	}
	db := dbConfig(get)
	var missing []string
	for _, req := range []struct{ name, value string }{
		{"MYSQL_DB_HOST", db.Host},
		{"MYSQL_DB_USERNAME", db.User},
		{"MYSQL_DB_DATABASE", db.Name},
	} {
		if req.value == "" {
			missing = append(missing, req.name)
		}
	}
	if len(missing) > 0 {
		return DBConfig{}, fmt.Errorf("missing config: %s", strings.Join(missing, ", "))
	}
	return db, nil
}

func LoadConfig(envPath string) (Config, error) {
	get, err := envGetter(envPath)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		APIURL:                get("RT_WEB_UI_API_URL", ""),
		APIToken:              get("RT_WEB_UI_API_TOKEN", get("DEFAULT_TOKEN", "")),
		APITimeout:            defaultAPITimeout,
		NOCQueue:              get("RT_WEB_UI_NOC_QUEUE", defaultNOCQueue),
		AllowedTicketStatuses: splitList(get("RT_WEB_UI_ALLOWED_TICKET_STATUSES", defaultAllowedTicketStatuses)),
		EligibleServiceTypes:  splitList(get("RT_WEB_UI_ELIGIBLE_SERVICE_TYPES", defaultEligibleServiceTypes)),
		OPIFields:             splitList(get("RT_WEB_UI_OPI_FIELDS", defaultOPIFields)),
		DB:                    dbConfig(get),
	}
	if cfg.APIURL == "" {
		if port := get("DOMAIN_PORT", ""); port != "" {
			cfg.APIURL = fmt.Sprintf("http://localhost:%s/api/v1/cpe/remote-resolve", port)
		}
	}
	if raw := get("RT_WEB_UI_API_TIMEOUT", ""); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("RT_WEB_UI_API_TIMEOUT %q: must be a positive duration such as 30s", raw)
		}
		cfg.APITimeout = d
	}

	var missing []string
	for _, req := range []struct{ name, value string }{
		{"RT_WEB_UI_API_URL (or DOMAIN_PORT)", cfg.APIURL},
		{"RT_WEB_UI_API_TOKEN (or DEFAULT_TOKEN)", cfg.APIToken},
		{"MYSQL_DB_HOST", cfg.DB.Host},
		{"MYSQL_DB_USERNAME", cfg.DB.User},
		{"MYSQL_DB_DATABASE", cfg.DB.Name},
	} {
		if req.value == "" {
			missing = append(missing, req.name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing config: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
