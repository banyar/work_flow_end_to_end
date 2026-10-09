package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// check reports which systems the send flow depends on are not ready. It is
// read-only: no ticket is sent and nothing is written to the DB or Kafka.
// Exit 1 when a required system fails; warnings alone exit 0.

const (
	checkTimeout     = 3 * time.Second
	defaultNOCEnv    = "../../NocAutomationCodeMerge/noc_automation/.env"
	defaultMockPort  = "3002"
	defaultReportDir = "../../pipeline_report"
	defaultRtutilDir = "../../NocAutomationCodeMerge/remote-resolved-queue-transfer"
)

type checkLevel int

const (
	levelOK checkLevel = iota
	levelSkip
	levelWarn
	levelFail
)

var levelMark = map[checkLevel]string{levelOK: "✔", levelSkip: "-", levelWarn: "!", levelFail: "✘"}

type checkResult struct {
	Name   string
	Level  checkLevel
	Detail string
	Fix    string // shown as "→ …" when the check did not pass
}

type checkFunc func(ctx context.Context) checkResult

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	envPath := fs.String("env", ".env", "")
	nocEnvPath := fs.String("noc-env", defaultNOCEnv, "")
	mockPort := fs.String("mock-port", defaultMockPort, "")
	uiAddr := fs.String("ui-addr", defaultServeAddr, "")
	reportDir := fs.String("report-dir", defaultReportDir, "")
	mode := fs.String("mode", defaultMode, "")
	rtutilDir := fs.String("rtutil-dir", defaultRtutilDir, "")
	rtutilConfigFlag := fs.String("rtutil-config", "", "")
	runDir := fs.String("run-dir", defaultRunDir, "")
	fs.Usage = func() { fmt.Fprintln(stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 3
	}

	cfg, cfgErr := LoadConfig(*envPath)
	rtutilConfig, rtutilConfigErr := rtutilConfigPath(*rtutilDir, *mode, *rtutilConfigFlag)
	noc, nocErr := godotenv.Read(*nocEnvPath)

	checks := []checkFunc{
		checkGo,
		func(context.Context) checkResult { return checkEnv(*envPath, cfgErr) },
		func(ctx context.Context) checkResult { return checkMySQL(ctx, cfg, cfgErr) },
		func(ctx context.Context) checkResult { return checkAPI(ctx, cfg, cfgErr) },
		checkDocker,
		func(ctx context.Context) checkResult { return checkMock(ctx, *mockPort) },
		func(context.Context) checkResult { return checkNodeRedURL(noc, nocErr, *nocEnvPath, *mockPort) },
		func(ctx context.Context) checkResult { return checkKafka(ctx, noc, nocErr, *nocEnvPath) },
		func(ctx context.Context) checkResult { return checkRTREST(ctx, noc, nocErr, *nocEnvPath) },
		checkRtutil,
		func(ctx context.Context) checkResult { return checkUI(ctx, *uiAddr) },
		func(ctx context.Context) checkResult {
			if rtutilConfigErr != nil {
				return checkResult{Name: "rtutil config", Level: levelFail, Detail: rtutilConfigErr.Error()}
			}
			return checkRtutilConfig(ctx, *mode, rtutilConfig, cfg, cfgErr)
		},
		func(context.Context) checkResult {
			return checkRestartNeeded(*envPath, *nocEnvPath, *reportDir, rtutilConfig, *runDir, cfg, cfgErr)
		},
		func(ctx context.Context) checkResult { return checkReport(ctx, *reportDir) },
		checkJQ,
	}

	// Run in parallel, print in a fixed order.
	results := make([]checkResult, len(checks))
	var wg sync.WaitGroup
	for i, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
			defer cancel()
			results[i] = c(ctx)
		}()
	}
	wg.Wait()

	fmt.Fprintf(stdout, "rt_web_ui system check   MODE=%s   (env %s · noc env %s)\n\n", *mode, *envPath, *nocEnvPath)
	fails, warns := 0, 0
	for _, r := range results {
		fmt.Fprintf(stdout, " %s %-19s %s\n", levelMark[r.Level], r.Name, r.Detail)
		if r.Level >= levelWarn && r.Fix != "" {
			fmt.Fprintf(stdout, "   %-19s → %s\n", "", r.Fix)
		}
		switch r.Level {
		case levelFail:
			fails++
		case levelWarn:
			warns++
		}
	}
	fmt.Fprintf(stdout, "\nNot ready: %d · Warnings: %d\n", fails, warns)
	if fails > 0 {
		return 1
	}
	return 0
}

func checkGo(context.Context) checkResult {
	r := checkResult{Name: "Go", Level: levelOK, Detail: runtime.Version()}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == "git.frontiir.net/sa-dev/rtdatacore" {
				r.Detail += " · rtdatacore " + dep.Version
			}
		}
	}
	return r
}

func checkEnv(envPath string, cfgErr error) checkResult {
	r := checkResult{Name: "rt_web_ui .env"}
	if _, err := os.Stat(envPath); err != nil {
		r.Level, r.Detail, r.Fix = levelFail, envPath+" not found", "ENV=<file> နဲ့ env file ကို ညွှန်ပါ"
		return r
	}
	if cfgErr != nil {
		r.Level, r.Detail, r.Fix = levelFail, cfgErr.Error(), envPath+" ထဲ ထည့်ပါ"
		return r
	}
	r.Level, r.Detail = levelOK, "all required keys set"
	return r
}

func checkMySQL(ctx context.Context, cfg Config, cfgErr error) checkResult {
	r := checkResult{Name: "MySQL"}
	if cfgErr != nil {
		r.Level, r.Detail = levelSkip, "skipped (.env not ready)"
		return r
	}
	target := fmt.Sprintf("%s:%s/%s", cfg.DB.Host, cfg.DB.Port, cfg.DB.Name)
	db, err := gorm.Open(mysql.Open(cfg.DB.DSN()+"&timeout=3s"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		r.Level, r.Detail, r.Fix = levelFail, target+" — "+firstLine(err), "MySQL run နေလား၊ MYSQL_DB_* မှန်လား စစ်ပါ"
		return r
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}
	var missing []string
	for _, table := range []string{"pipeline_runs", "pipeline_run_events"} {
		if !db.WithContext(ctx).Migrator().HasTable(table) {
			missing = append(missing, table)
		}
	}
	if len(missing) > 0 {
		r.Level, r.Detail = levelFail, target+" · missing table "+strings.Join(missing, ", ")
		r.Fix = "rtdatacore ၏ pipeline table migration ကို run ပါ"
		return r
	}
	r.Level, r.Detail = levelOK, target+" · pipeline_runs ✓ pipeline_run_events ✓"
	return r
}

func checkAPI(ctx context.Context, cfg Config, cfgErr error) checkResult {
	r := checkResult{Name: "RT External API"}
	if cfgErr != nil {
		r.Level, r.Detail = levelSkip, "skipped (.env not ready)"
		return r
	}
	u, err := url.Parse(cfg.APIURL)
	if err != nil || u.Host == "" {
		r.Level, r.Detail, r.Fix = levelFail, fmt.Sprintf("bad RT_WEB_UI_API_URL %q", cfg.APIURL), "URL ကို ပြင်ပါ"
		return r
	}
	health := u.Scheme + "://" + u.Host + "/healthcheck"
	status, err := httpGet(ctx, health)
	switch {
	case err != nil:
		r.Level, r.Detail, r.Fix = levelFail, health+" — "+err.Error(), "noc_automation ကို start ပါ"
	case status != http.StatusOK:
		r.Level, r.Detail, r.Fix = levelFail, fmt.Sprintf("%s — HTTP %d", health, status), "noc_automation log ကို စစ်ပါ"
	default:
		r.Level, r.Detail = levelOK, health+" — 200"
	}
	return r
}

func checkDocker(ctx context.Context) checkResult {
	r := checkResult{Name: "Docker"}
	if _, err := exec.LookPath("docker"); err != nil {
		r.Level, r.Detail, r.Fix = levelWarn, "not installed", "criteria mock အတွက် Docker ထည့်ပါ"
		return r
	}
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Output()
	if err != nil {
		r.Level, r.Detail, r.Fix = levelWarn, "daemon not reachable", "sudo systemctl start docker (သို့) docker group permission စစ်ပါ"
		return r
	}
	r.Level, r.Detail = levelOK, "daemon running · "+strings.TrimSpace(string(out))
	return r
}

func checkMock(ctx context.Context, port string) checkResult {
	r := checkResult{Name: "Node-RED mock"}
	endpoint := fmt.Sprintf("http://localhost:%s/api/v1/onus/CRIT-20/status", port)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.Level, r.Detail, r.Fix = levelWarn, "localhost:"+port+" not answering", "make criteria-mock"
		return r
	}
	defer resp.Body.Close()
	var body struct {
		Data struct {
			OLT struct {
				Status string `json:"status"`
			} `json:"olt"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Data.OLT.Status != "Offline" {
		r.Level, r.Detail = levelWarn, "localhost:"+port+" answers, but CRIT-20 is not the criteria mock"
		r.Fix = "port " + port + " ကို တခြား program သုံးနေသလား စစ်ပါ"
		return r
	}
	r.Level, r.Detail = levelOK, "localhost:"+port+" · CRIT-20 → olt Offline"
	return r
}

func checkNodeRedURL(noc map[string]string, nocErr error, nocEnvPath, mockPort string) checkResult {
	r := checkResult{Name: "NODE_RED_BASE_URL"}
	if nocErr != nil {
		r.Level, r.Detail, r.Fix = levelSkip, "skipped ("+nocEnvPath+" not found)", "NOC_ENV=<noc_automation .env>"
		return r
	}
	raw := noc["NODE_RED_BASE_URL"]
	u, err := url.Parse(raw)
	if raw == "" || err != nil {
		r.Level, r.Detail = levelWarn, "not set in "+nocEnvPath
		r.Fix = "NODE_RED_BASE_URL=http://localhost:" + mockPort + "/"
		return r
	}
	host := u.Hostname()
	if (host == "localhost" || host == "127.0.0.1") && u.Port() == mockPort {
		r.Level, r.Detail = levelOK, raw+" (criteria mock)"
		return r
	}
	r.Level, r.Detail = levelWarn, raw+" (mock :"+mockPort+" မဟုတ်)"
	r.Fix = "criteria 10–36 result မမှန်နိုင်ပါ · mock သုံးရင် NODE_RED_BASE_URL=http://localhost:" + mockPort + "/ ပြီး noc_automation restart"
	return r
}

func checkKafka(ctx context.Context, noc map[string]string, nocErr error, nocEnvPath string) checkResult {
	r := checkResult{Name: "Kafka"}
	if nocErr != nil {
		r.Level, r.Detail = levelSkip, "skipped ("+nocEnvPath+" not found)"
		return r
	}
	brokers := splitList(noc["KAFKA_BROKERS"])
	if len(brokers) == 0 {
		r.Level, r.Detail, r.Fix = levelWarn, "KAFKA_BROKERS not set", nocEnvPath+" ထဲ ထည့်ပါ"
		return r
	}
	var down []string
	for _, b := range brokers {
		if err := tcpDial(ctx, b); err != nil {
			down = append(down, b)
		}
	}
	if len(down) > 0 {
		r.Level = levelWarn
		r.Detail = fmt.Sprintf("broker %d/%d reachable · down: %s", len(brokers)-len(down), len(brokers), strings.Join(down, ", "))
		r.Fix = "Kafka broker / VPN ကို စစ်ပါ"
		return r
	}
	r.Level, r.Detail = levelOK, fmt.Sprintf("broker %d/%d reachable", len(brokers), len(brokers))
	return r
}

func checkRTREST(ctx context.Context, noc map[string]string, nocErr error, nocEnvPath string) checkResult {
	r := checkResult{Name: "RT REST2"}
	if nocErr != nil {
		r.Level, r.Detail = levelSkip, "skipped ("+nocEnvPath+" not found)"
		return r
	}
	base := noc["RT_BASE_URL"]
	if base == "" {
		r.Level, r.Detail, r.Fix = levelWarn, "RT_BASE_URL not set", nocEnvPath+" ထဲ ထည့်ပါ"
		return r
	}
	// Any HTTP answer means RT is up; auth is not checked here.
	if _, err := httpGet(ctx, base); err != nil {
		r.Level, r.Detail, r.Fix = levelWarn, hostOf(base)+" — "+err.Error(), "RT server / VPN ကို စစ်ပါ"
		return r
	}
	r.Level, r.Detail = levelOK, hostOf(base)+" reachable"
	return r
}

func checkRtutil(context.Context) checkResult {
	r := checkResult{Name: "rtutil consumer"}
	pids := rtutilConsumerPIDs()
	if len(pids) == 0 {
		r.Level, r.Detail = levelWarn, "local process not found"
		r.Fix = "make up (သို့) rtutil ticket kafka-remote-resolve ကို start ပါ (docker/k8s မှာ run ရင် ignore)"
		return r
	}
	r.Level, r.Detail = levelOK, "running · pid "+strings.Join(pids, ", ")
	return r
}

// rtutilConsumerPIDs finds processes whose arguments are exactly
// "… ticket kafka-remote-resolve". Matching whole arguments (not pgrep -f)
// keeps shells, greps and editors that merely mention the name out.
func rtutilConsumerPIDs() []string {
	entries, _ := os.ReadDir("/proc")
	var pids []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		raw, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		for i := 1; i+1 < len(argv); i++ {
			if argv[i] == "ticket" && argv[i+1] == "kafka-remote-resolve" {
				pids = append(pids, e.Name())
				break
			}
		}
	}
	return pids
}

func checkUI(ctx context.Context, addr string) checkResult {
	r := checkResult{Name: "Criteria UI"}
	base := "http://" + addr
	status, err := httpGet(ctx, base+"/api/info")
	switch {
	case err != nil:
		r.Level, r.Detail, r.Fix = levelWarn, base+" — "+err.Error(), "make up (သို့) make criteria-ui"
	case status != http.StatusOK:
		r.Level, r.Detail, r.Fix = levelWarn, fmt.Sprintf("%s — HTTP %d", base, status), "port "+addr+" ကို တခြား program သုံးနေသလား စစ်ပါ"
	default:
		r.Level, r.Detail = levelOK, base
	}
	return r
}

// reportAddr is pipeline_report's REPORT_HTTP_ADDR (default :8090) as a
// dialable host:port.
func reportAddr(dir string) string {
	addr := ":8090"
	if env, err := godotenv.Read(filepath.Join(dir, ".env")); err == nil && env["REPORT_HTTP_ADDR"] != "" {
		addr = env["REPORT_HTTP_ADDR"]
	}
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	return addr
}

func checkReport(ctx context.Context, dir string) checkResult {
	r := checkResult{Name: "pipeline_report"}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		r.Level, r.Detail, r.Fix = levelWarn, dir+" not found", "REPORT_DIR=<pipeline_report path>"
		return r
	}
	base := "http://" + reportAddr(dir)
	status, err := httpGet(ctx, base+"/healthz")
	switch {
	case err != nil:
		r.Level, r.Detail, r.Fix = levelWarn, base+" — "+err.Error(), "make up (သို့) cd "+dir+" && make run"
	case status != http.StatusOK:
		r.Level, r.Detail, r.Fix = levelWarn, fmt.Sprintf("%s/healthz — HTTP %d", base, status), "pipeline_report log / DB ကို စစ်ပါ"
	default:
		r.Level, r.Detail = levelOK, base
	}
	return r
}

func checkJQ(ctx context.Context) checkResult {
	r := checkResult{Name: "jq"}
	if _, err := exec.LookPath("jq"); err != nil {
		r.Level, r.Detail, r.Fix = levelWarn, "not installed", "sudo apt install jq (make send-sample အတွက်သာ)"
		return r
	}
	out, _ := exec.CommandContext(ctx, "jq", "--version").Output()
	r.Level, r.Detail = levelOK, strings.TrimSpace(string(out))
	return r
}

func httpGet(ctx context.Context, rawURL string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, shortNetErr(err)
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func tcpDial(ctx context.Context, addr string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

// shortNetErr turns "Get \"…\": dial tcp …: connect: connection refused" into "connection refused".
func shortNetErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("timeout")
	}
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		msg = msg[i+2:]
	}
	return errors.New(msg)
}

func hostOf(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return rawURL
}

func firstLine(err error) string {
	msg, _, _ := strings.Cut(err.Error(), "\n")
	return msg
}
