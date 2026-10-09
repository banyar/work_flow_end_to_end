package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
)

// up starts the systems `check` finds down that can be started locally:
// the MySQL and RT docker containers (docker start), the criteria mock
// (docker run -d), noc_automation, the rtutil consumer, the criteria UI and
// pipeline_report (built from source into the run folder and started in the
// background). Everything
// already running is left alone. down stops only what up started.

const (
	defaultRunDir     = ".run"
	defaultMySQLCtr   = "rt_local_dev_mariadb_10.4-local"
	defaultRTCtr      = "rt_local_dev_rt_5.0"
	mockContainer     = "noc-criteria-mock"
	mockImage         = "mockoon/cli:latest"
	startWait         = 30 * time.Second
	rtutilSettleDelay = 3 * time.Second
)

type upOptions struct {
	envPath, nocEnvPath, nocDir, rtutilDir string
	mockPort, mockData, runDir             string
	uiAddr, criteriaDir, selfDir           string
	reportDir                              string
	mode, rtutilConfigFlag, rtutilConfig   string
	mysqlContainer, rtContainer            string
}

// background is a process up starts and down stops.
type background struct {
	name    string // also the binary, pid and log file name in runDir
	srcDir  string // go build runs here
	pkg     string // package to build
	workDir string // the process runs here
	args    []string
	env     []string                   // extra environment for build and run
	stamp   string                     // recorded in runDir/<name>.config; a different stamp on the next up restarts it
	configs []string                   // files read only at startup; editing one after the start restarts it
	ready   func(context.Context) bool // nil: alive after a short settle delay is enough
	readyAs string                     // what ready means, for the output line
}

func upFlags(name string, stderr io.Writer) (*flag.FlagSet, *upOptions) {
	o := &upOptions{}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&o.envPath, "env", ".env", "")
	fs.StringVar(&o.nocEnvPath, "noc-env", defaultNOCEnv, "")
	fs.StringVar(&o.nocDir, "noc-dir", "", "")
	fs.StringVar(&o.rtutilDir, "rtutil-dir", "", "")
	fs.StringVar(&o.mockPort, "mock-port", defaultMockPort, "")
	fs.StringVar(&o.mockData, "mock-data", "criteria/mockoon-criteria.json", "")
	fs.StringVar(&o.runDir, "run-dir", defaultRunDir, "")
	fs.StringVar(&o.uiAddr, "ui-addr", defaultServeAddr, "")
	fs.StringVar(&o.criteriaDir, "criteria", defaultCriteriaDir, "")
	fs.StringVar(&o.reportDir, "report-dir", defaultReportDir, "")
	fs.StringVar(&o.mode, "mode", defaultMode, "")
	fs.StringVar(&o.rtutilConfigFlag, "rtutil-config", "", "")
	fs.StringVar(&o.mysqlContainer, "mysql-container", defaultMySQLCtr, "")
	fs.StringVar(&o.rtContainer, "rt-container", defaultRTCtr, "")
	fs.Usage = func() { fmt.Fprintln(stderr, usage) }
	return fs, o
}

func (o *upOptions) services(cfg Config, cfgErr error) []background {
	return []background{
		{name: "noc_automation", srcDir: o.nocDir, pkg: "./frontiir", workDir: o.nocDir, configs: o.nocConfigs(),
			ready:   func(c context.Context) bool { return checkAPI(c, cfg, cfgErr).Level == levelOK },
			readyAs: "healthcheck OK"},
		{name: "rtutil", srcDir: o.rtutilDir, pkg: ".", workDir: o.rtutilDir,
			args: []string{"--config", o.rtutilConfig, "ticket", "kafka-remote-resolve"}, stamp: o.rtutilConfig,
			configs: []string{o.rtutilConfig}},
		{name: "criteria_ui", srcDir: o.selfDir, pkg: ".", workDir: o.selfDir,
			args:    []string{"serve", "--env", o.envPath, "--addr", o.uiAddr, "--criteria", o.criteriaDir},
			configs: []string{o.envPath},
			ready:   func(c context.Context) bool { return checkUI(c, o.uiAddr).Level == levelOK },
			readyAs: "http://" + o.uiAddr + " answering"},
		{name: "pipeline_report", srcDir: o.reportDir, pkg: ".", workDir: o.reportDir,
			args:    []string{"--env", filepath.Join(o.reportDir, ".env")},
			configs: []string{filepath.Join(o.reportDir, ".env")},
			env:     []string{"GOWORK=off"}, // standalone module, not in any go.work
			ready:   func(c context.Context) bool { return checkReport(c, o.reportDir).Level == levelOK },
			readyAs: "http://" + reportAddr(o.reportDir) + " answering"},
	}
}

func runUp(args []string, stdout, stderr io.Writer) int {
	fs, o := upFlags("up", stderr)
	if err := fs.Parse(args); err != nil {
		return 3
	}
	if err := o.absolutize(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 3
	}

	fmt.Fprintf(stdout, "rt_web_ui up   MODE=%s\n  noc_automation %s\n  rtutil         %s\n  rtutil config  %s\n  run folder     %s\n\n",
		o.mode, o.nocDir, o.rtutilDir, o.rtutilConfig, o.runDir)

	cfg, cfgErr := LoadConfig(o.envPath)
	noc, nocErr := godotenv.Read(o.nocEnvPath)
	svc := o.services(cfg, cfgErr)
	ctx := context.Background()
	probe := func(c checkFunc) checkResult {
		cctx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		return c(cctx)
	}

	steps := []struct {
		name  string
		check checkFunc
		start func() (string, error)
	}{
		{"MySQL", func(c context.Context) checkResult { return checkMySQL(c, cfg, cfgErr) },
			func() (string, error) { return o.startContainer(o.mysqlContainer) }},
		{"RT REST2", func(c context.Context) checkResult { return checkRTREST(c, noc, nocErr, o.nocEnvPath) },
			func() (string, error) { return o.startContainer(o.rtContainer) }},
		{"Node-RED mock", func(c context.Context) checkResult { return checkMock(c, o.mockPort) },
			o.startMock},
		{"noc_automation", func(c context.Context) checkResult { return checkAPI(c, cfg, cfgErr) },
			func() (string, error) { return o.startBackground(svc[0]) }},
		{"rtutil consumer", checkRtutil,
			func() (string, error) { return o.startBackground(svc[1]) }},
		{"Criteria UI", func(c context.Context) checkResult { return checkUI(c, o.uiAddr) },
			func() (string, error) { return o.startBackground(svc[2]) }},
		{"pipeline_report", func(c context.Context) checkResult { return checkReport(c, o.reportDir) },
			func() (string, error) { return o.startBackground(svc[3]) }},
	}
	// A process up started is restarted when it runs with another config
	// (MODE) or a file it reads only at startup changed since.
	for _, b := range svc {
		if reason := o.restartReason(b); reason != "" {
			fmt.Fprintf(stdout, " ↻ %-17s %s → restarting\n", b.name, reason)
			o.stopBackground(b)
		}
	}

	failed := 0
	for _, s := range steps {
		r := probe(s.check)
		switch r.Level {
		case levelOK:
			fmt.Fprintf(stdout, " ✔ %-17s already running\n", s.name)
			continue
		case levelSkip: // config needed to probe it is missing; starting blind could be wrong
			fmt.Fprintf(stdout, " - %-17s %s\n", s.name, r.Detail)
			continue
		}
		fmt.Fprintf(stdout, " … %-17s starting\n", s.name)
		detail, err := s.start()
		if err != nil {
			failed++
			fmt.Fprintf(stdout, " ✘ %-17s %v\n", s.name, err)
			continue
		}
		fmt.Fprintf(stdout, " ▶ %-17s %s\n", s.name, detail)
	}
	fmt.Fprintf(stdout, " - %-17s cannot be started from here (remote brokers)\n", "Kafka")

	fmt.Fprintln(stdout)
	code := runCheck([]string{"--env", o.envPath, "--noc-env", o.nocEnvPath, "--mock-port", o.mockPort,
		"--ui-addr", o.uiAddr, "--report-dir", o.reportDir,
		"--mode", o.mode, "--rtutil-dir", o.rtutilDir, "--rtutil-config", o.rtutilConfig, "--run-dir", o.runDir}, stdout, stderr)
	if failed > 0 && code == 0 {
		code = 1
	}
	return code
}

func runDown(args []string, stdout, stderr io.Writer) int {
	fs, o := upFlags("down", stderr)
	if err := fs.Parse(args); err != nil {
		return 3
	}
	if err := o.absolutize(); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 3
	}
	fmt.Fprintln(stdout, "rt_web_ui down (only what make up started)")
	for _, b := range o.services(Config{}, nil) {
		if detail := o.stopBackground(b); detail != "" {
			fmt.Fprintf(stdout, " ■ %-17s %s\n", b.name, detail)
		} else {
			fmt.Fprintf(stdout, " - %-17s not started by make up\n", b.name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "stop", mockContainer).CombinedOutput()
	switch {
	case err == nil:
		fmt.Fprintf(stdout, " ■ %-17s stopped (docker %s)\n", "Node-RED mock", mockContainer)
	case strings.Contains(string(out), "No such container"):
		fmt.Fprintf(stdout, " - %-17s not running\n", "Node-RED mock")
	default:
		fmt.Fprintf(stdout, " ✘ %-17s %s\n", "Node-RED mock", strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(stdout, " - %-17s left running (shared docker containers)\n", "MySQL / RT")
	return 0
}

// absolutize resolves every path against the current folder so the
// background processes, which run elsewhere, see the same files.
func (o *upOptions) absolutize() error {
	if o.nocDir == "" {
		o.nocDir = filepath.Dir(o.nocEnvPath)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	o.selfDir = wd
	for _, p := range []*string{&o.envPath, &o.nocEnvPath, &o.nocDir, &o.rtutilDir, &o.mockData, &o.runDir, &o.criteriaDir, &o.reportDir} {
		if *p == "" {
			continue
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			return err
		}
		*p = abs
	}
	if o.rtutilDir == "" {
		o.rtutilDir = filepath.Join(filepath.Dir(o.nocDir), "remote-resolved-queue-transfer")
	}
	cfgPath, err := rtutilConfigPath(o.rtutilDir, o.mode, o.rtutilConfigFlag)
	if err != nil {
		return err
	}
	o.rtutilConfig = cfgPath
	return os.MkdirAll(o.runDir, 0o755)
}

func (o *upOptions) startContainer(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", name).Output()
	if err != nil {
		return "", fmt.Errorf("docker container %s not found → docker container ကို ကိုယ်တိုင် စစ်ပါ", name)
	}
	if s := strings.TrimSpace(string(state)); s == "running" {
		return "", fmt.Errorf("container %s is running but not answering → docker logs %s", name, name)
	}
	if out, err := exec.CommandContext(ctx, "docker", "start", name).CombinedOutput(); err != nil {
		return "", fmt.Errorf("docker start %s: %s", name, strings.TrimSpace(string(out)))
	}
	return "started (docker start " + name + ")", nil
}

func (o *upOptions) startMock() (string, error) {
	if _, err := os.Stat(o.mockData); err != nil {
		return "", fmt.Errorf("%s not found", o.mockData)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute) // first run pulls the image
	defer cancel()
	if state, err := exec.CommandContext(ctx, "docker", "inspect", "-f", "{{.State.Status}}", mockContainer).Output(); err == nil {
		return "", fmt.Errorf("container %s exists (%s) but does not answer on :%s → make criteria-mock-stop",
			mockContainer, strings.TrimSpace(string(state)), o.mockPort)
	}
	out, err := exec.CommandContext(ctx, "docker", "run", "-d", "--rm", "--name", mockContainer,
		"-p", o.mockPort+":3002", "-v", o.mockData+":/data/mock.json:ro",
		mockImage, "--data", "/data/mock.json", "--port", "3002").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker run: %s", lastLine(out))
	}
	waited, err := waitFor(func(c context.Context) bool { return checkMock(c, o.mockPort).Level == levelOK })
	if err != nil {
		return "", fmt.Errorf("started but CRIT-20 did not answer within %s → docker logs %s", startWait, mockContainer)
	}
	return fmt.Sprintf("started (docker %s) · answering after %s", mockContainer, waited), nil
}

func (o *upOptions) startBackground(b background) (string, error) {
	if st, err := os.Stat(b.srcDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("%s not found → NOC_ROOT=<NocAutomationCodeMerge path>", b.srcDir)
	}
	if b.stamp != "" {
		if _, err := os.Stat(b.stamp); err != nil {
			return "", fmt.Errorf("%s not found → .rtutil.json မှ copy ပြီး ဖြည့်ပါ: %s", b.stamp, rtutilRequiredKeys)
		}
	}
	if pid, ok := o.livePID(b); ok {
		return "", fmt.Errorf("already started by make up (pid %d) but not ready → tail -f %s", pid, o.logPath(b))
	}
	bin := filepath.Join(o.runDir, b.name)
	build := exec.Command("go", "build", "-o", bin, b.pkg)
	build.Dir = b.srcDir
	build.Env = append(os.Environ(), b.env...)
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build failed in %s:\n%s", b.srcDir, indent(out))
	}

	logFile, err := os.OpenFile(o.logPath(b), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "\n=== %s started by make up at %s ===\n", b.name, time.Now().Format(time.RFC3339))
	cmd := exec.Command(bin, b.args...)
	cmd.Dir = b.workDir
	cmd.Env = append(os.Environ(), b.env...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // keep running after make exits
	if err := cmd.Start(); err != nil {
		return "", err
	}
	pid := cmd.Process.Pid
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	if err := os.WriteFile(o.pidPath(b), []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return "", err
	}
	if b.stamp != "" {
		os.WriteFile(o.stampPath(b), []byte(b.stamp), 0o644)
	}

	ready, settle := b.ready, time.Duration(0)
	if ready == nil {
		ready, settle = func(context.Context) bool { return true }, rtutilSettleDelay
	}
	select {
	case <-time.After(settle):
	case err := <-exited:
		os.Remove(o.pidPath(b))
		os.Remove(o.stampPath(b))
		return "", fmt.Errorf("exited right away (%v) → %s:\n%s", err, o.logPath(b), indent(tail(o.logPath(b), 8)))
	}
	waited, err := waitFor(func(c context.Context) bool {
		select {
		case <-exited:
			return false
		default:
			return ready(c)
		}
	})
	if err != nil {
		return "", fmt.Errorf("pid %d not ready within %s → %s:\n%s", pid, startWait, o.logPath(b), indent(tail(o.logPath(b), 8)))
	}
	if b.readyAs != "" {
		return fmt.Sprintf("started · pid %d · %s after %s · log %s", pid, b.readyAs, waited, o.relLog(b)), nil
	}
	return fmt.Sprintf("started · pid %d · log %s", pid, o.relLog(b)), nil
}

// stopBackground stops a process only if its pid file points at the binary
// up built, so a process the user started by hand is never touched.
func (o *upOptions) stopBackground(b background) string {
	pid, ok := o.livePID(b)
	if !ok {
		os.Remove(o.pidPath(b))
		return ""
	}
	p, _ := os.FindProcess(pid)
	p.Signal(syscall.SIGTERM)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && p.Signal(syscall.Signal(0)) == nil {
		time.Sleep(200 * time.Millisecond)
	}
	how := "stopped"
	if p.Signal(syscall.Signal(0)) == nil {
		p.Signal(syscall.SIGKILL)
		how = "killed (did not stop within 5s)"
	}
	os.Remove(o.pidPath(b))
	os.Remove(o.stampPath(b))
	return fmt.Sprintf("%s · pid %d", how, pid)
}

// livePID reads the pid file and confirms the process is still the binary
// up started (pids get reused).
func (o *upOptions) livePID(b background) (int, bool) {
	raw, err := os.ReadFile(o.pidPath(b))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, false
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil || strings.TrimSuffix(exe, " (deleted)") != filepath.Join(o.runDir, b.name) {
		return 0, false
	}
	return pid, true
}

func (o *upOptions) pidPath(b background) string   { return filepath.Join(o.runDir, b.name+".pid") }
func (o *upOptions) logPath(b background) string   { return filepath.Join(o.runDir, b.name+".log") }
func (o *upOptions) stampPath(b background) string { return filepath.Join(o.runDir, b.name+".config") }

// checkRestartNeeded lists processes make up started whose startup config
// changed (or MODE differs) since they started.
func checkRestartNeeded(envPath, nocEnvPath, reportDir, rtutilConfig, runDir string, cfg Config, cfgErr error) checkResult {
	r := checkResult{Name: "Restart needed"}
	o := &upOptions{envPath: envPath, nocEnvPath: nocEnvPath, reportDir: reportDir, runDir: runDir, rtutilConfig: rtutilConfig}
	for _, p := range []*string{&o.envPath, &o.nocEnvPath, &o.reportDir, &o.runDir} {
		if abs, err := filepath.Abs(*p); err == nil {
			*p = abs
		}
	}
	o.nocDir = filepath.Dir(o.nocEnvPath)
	var reasons []string
	for _, b := range o.services(cfg, cfgErr) {
		if reason := o.restartReason(b); reason != "" {
			reasons = append(reasons, b.name+": "+reason)
		}
	}
	if len(reasons) > 0 {
		r.Level, r.Detail, r.Fix = levelWarn, strings.Join(reasons, " · "), "make up (ပြောင်းထားတာကို restart လုပ်ပေးမည်)"
		return r
	}
	r.Level, r.Detail = levelOK, "none"
	return r
}

// nocConfigs are the files noc_automation reads once at startup: its .env
// and the workflow engine config named there (relative to its folder).
func (o *upOptions) nocConfigs() []string {
	files := []string{o.nocEnvPath}
	if env, err := godotenv.Read(o.nocEnvPath); err == nil && env["WORKFLOW_ENGINE_CONFIG_PATH"] != "" {
		p := env["WORKFLOW_ENGINE_CONFIG_PATH"]
		if !filepath.IsAbs(p) {
			p = filepath.Join(o.nocDir, p)
		}
		files = append(files, p)
	}
	return files
}

// restartReason says why a process up started must be restarted, or "".
// The pid file is written when the process starts, so its mtime is the
// start time.
func (o *upOptions) restartReason(b background) string {
	if _, ok := o.livePID(b); !ok {
		return ""
	}
	if b.stamp != "" {
		if running := o.readStamp(b); running != b.stamp {
			was := "default config"
			if running != "" {
				was = filepath.Base(running)
			}
			return "running with " + was + ", MODE wants " + filepath.Base(b.stamp)
		}
	}
	started, err := os.Stat(o.pidPath(b))
	if err != nil {
		return ""
	}
	for _, f := range b.configs {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(started.ModTime()) {
			return fmt.Sprintf("%s changed after start (%s > %s)", filepath.Base(f),
				st.ModTime().Format("01-02 15:04"), started.ModTime().Format("01-02 15:04"))
		}
	}
	return ""
}

func (o *upOptions) readStamp(b background) string {
	raw, _ := os.ReadFile(o.stampPath(b))
	return strings.TrimSpace(string(raw))
}

func (o *upOptions) relLog(b background) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, o.logPath(b)); err == nil {
			return rel
		}
	}
	return o.logPath(b)
}

// waitFor polls ready every 500ms for up to startWait.
func waitFor(ready func(context.Context) bool) (time.Duration, error) {
	start := time.Now()
	for time.Since(start) < startWait {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		ok := ready(ctx)
		cancel()
		if ok {
			return time.Since(start).Round(time.Second), nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return 0, errors.New("timeout")
}

func tail(path string, n int) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return bytes.Join(lines, []byte("\n"))
}

func indent(b []byte) string {
	s := strings.TrimRight(string(b), "\n")
	return "      " + strings.ReplaceAll(s, "\n", "\n      ")
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}
