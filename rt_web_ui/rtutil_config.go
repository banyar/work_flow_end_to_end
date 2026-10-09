package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// rtutil reads one JSON config (--config). MODE picks which file in the
// rtutil folder make up starts the consumer with.
var rtutilConfigFiles = map[string]string{
	"local": ".rtutil_local.json",
	"sit":   ".rtutil.json",
	"qa":    ".rtutil_qa.json",
}

const defaultMode = "local"

// rtutilRootCauseFields is the order rtutil expects rt.ticket.rootcauses in
// (see GetRootCauseData): custom field IDs, not names.
var rtutilRootCauseFields = []string{"Suspected Area of Issue", "Root Cause Category", "Service Root Cause", "Root Cause", "Ticket Problem"}

// rtutilRequiredKeys are what the Kafka consumer needs; listed when a mode's file is missing.
const rtutilRequiredKeys = "rt.database.{host,port,name,username,password}, rt.api.{base_url,token}, " +
	"rt.ticket.rootcauses (5 custom field IDs), kafka.config.consumer.{brokers,topic,group_id,username,password}, kafka.config.cpems_api.base_url"

func modeNames() string {
	names := make([]string, 0, len(rtutilConfigFiles))
	for m := range rtutilConfigFiles {
		names = append(names, m)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// rtutilConfigPath returns the config file for mode, or override when set.
func rtutilConfigPath(rtutilDir, mode, override string) (string, error) {
	if override != "" {
		return filepath.Abs(override)
	}
	name, ok := rtutilConfigFiles[mode]
	if !ok {
		return "", fmt.Errorf("unknown MODE %q (use %s)", mode, modeNames())
	}
	return filepath.Abs(filepath.Join(rtutilDir, name))
}

type rtutilConfig struct {
	RT struct {
		Database struct {
			Host string `json:"host"`
			Port any    `json:"port"`
			Name string `json:"name"`
		} `json:"database"`
		Ticket struct {
			RootCauses []any `json:"rootcauses"`
		} `json:"ticket"`
	} `json:"rt"`
}

func loadRtutilConfig(path string) (rtutilConfig, error) {
	var c rtutilConfig
	raw, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return c, nil
}

// checkRtutilConfig catches the two mismatches that leave runs stuck or
// rejected: rtutil writing pipeline state to another DB than rt_web_ui
// (runs stay at PUBLISHED), and rootcauses IDs that are not the expected
// custom fields in that DB ("Invalid Suspected Area of Issue").
func checkRtutilConfig(ctx context.Context, mode, path string, cfg Config, cfgErr error) checkResult {
	r := checkResult{Name: "rtutil config"}
	label := mode + " · " + filepath.Base(path)
	if _, err := os.Stat(path); err != nil {
		r.Level, r.Detail = levelFail, label+" not found"
		r.Fix = filepath.Base(path) + " ကို .rtutil.json မှ copy ပြီး ဖြည့်ပါ: " + rtutilRequiredKeys
		return r
	}
	rc, err := loadRtutilConfig(path)
	if err != nil {
		r.Level, r.Detail, r.Fix = levelFail, label+" — "+err.Error(), "JSON ကို ပြင်ပါ"
		return r
	}
	db := rc.RT.Database
	target := fmt.Sprintf("%s:%v/%s", db.Host, db.Port, db.Name)
	if cfgErr != nil {
		r.Level, r.Detail = levelSkip, label+" · DB "+target+" (rt_web_ui .env not ready)"
		return r
	}
	if !sameHost(db.Host, cfg.DB.Host) || fmt.Sprint(db.Port) != cfg.DB.Port || db.Name != cfg.DB.Name {
		r.Level = levelFail
		r.Detail = fmt.Sprintf("%s · DB %s ≠ rt_web_ui DB %s:%s/%s", label, target, cfg.DB.Host, cfg.DB.Port, cfg.DB.Name)
		r.Fix = "runs stay at PUBLISHED · MODE ကို ပြောင်းပါ (သို့) DB setting ကို တူအောင် ထားပါ"
		return r
	}
	if msg := checkRootCauseIDs(ctx, cfg.DB, rc.RT.Ticket.RootCauses); msg != "" {
		r.Level, r.Detail = levelFail, label+" · rootcauses "+msg
		r.Fix = "rt.ticket.rootcauses ကို ဒီ DB ၏ custom field ID များဖြင့် ပြင်ပါ (" + strings.Join(rtutilRootCauseFields, ", ") + ")"
		return r
	}
	r.Level, r.Detail = levelOK, label+" · DB "+target+" · rootcauses ✓"
	return r
}

// checkRootCauseIDs returns "" when every ID names the expected custom field.
func checkRootCauseIDs(ctx context.Context, dbCfg DBConfig, ids []any) string {
	if len(ids) != len(rtutilRootCauseFields) {
		return fmt.Sprintf("has %d entries, want %d IDs", len(ids), len(rtutilRootCauseFields))
	}
	db, err := gorm.Open(mysql.Open(dbCfg.DSN()+"&timeout=3s"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return "not checked (" + firstLine(err) + ")"
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	var bad []string
	for i, v := range ids {
		id, ok := v.(float64)
		if !ok {
			bad = append(bad, fmt.Sprintf("#%d %v is not an ID", i+1, v))
			continue
		}
		var name string
		db.WithContext(ctx).Raw("SELECT Name FROM CustomFields WHERE id = ?", int(id)).Scan(&name)
		if name != rtutilRootCauseFields[i] {
			if name == "" {
				name = "missing"
			}
			bad = append(bad, fmt.Sprintf("%d=%s (want %s)", int(id), name, rtutilRootCauseFields[i]))
		}
	}
	return strings.Join(bad, "; ")
}

func sameHost(a, b string) bool {
	norm := func(h string) string {
		if h == "localhost" {
			return "127.0.0.1"
		}
		return h
	}
	return norm(a) == norm(b)
}
