// Command rt_web_ui plays the RT Web UI part of the NOC automation
// (final.html section 1, steps 1-5): it checks a NOC ticket against the
// eligibility gates, sends its JSON to the RT External API and records the
// run's state and events in pipeline_runs / pipeline_run_events.
//
//	go run ./rt_web_ui send --file ticket.json [--env .env] [--run-id UUID] [--set key=value ...]
//	go run ./rt_web_ui export [--env .env] [--queue 43] [--out samples/tickets.json] ...
//
// export reads open tickets from the RT DB into a sample JSON array (see export.go).
//
// Exit codes: 0 accepted, 1 not eligible, 2 rejected or unreachable,
// 3 usage / configuration / database error.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"git.frontiir.net/sa-dev/rtdatacore/pkg/driven/rtdb"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const usage = `usage: rt_web_ui send --file <ticket.json|-> [--env <file>] [--run-id <uuid>] [--set key=value ...]
       rt_web_ui export [--env <file>] [--queue <id>] [--statuses a,b] [--ids 1,2] [--limit N] [--out <file|->] [--all-fields]

  --file    ticket JSON as the RT Web UI sends it ("-" = stdin)
  --env     config file (default .env); noc_automation/.env works as-is
  --run-id  use this run_id instead of a new UUID
  --set     override a field, e.g. --set queue=... or --set custom_fields.service_type=MNet (repeatable)
  --all-fields  export: include every custom field of the queue, "" when the ticket has no value`

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type setFlags []string

func (s *setFlags) String() string     { return strings.Join(*s, ",") }
func (s *setFlags) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "export" {
		return runExport(args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "send" {
		fmt.Fprintln(stderr, usage)
		return 3
	}
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("file", "", "")
	envPath := fs.String("env", ".env", "")
	runID := fs.String("run-id", "", "")
	var sets setFlags
	fs.Var(&sets, "set", "")
	fs.Usage = func() { fmt.Fprintln(stderr, usage) }
	if err := fs.Parse(args[1:]); err != nil {
		return 3
	}
	if *file == "" {
		fmt.Fprintln(stderr, usage)
		return 3
	}

	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "error: "+format+"\n", a...)
		return 3
	}

	payload, err := readPayload(*file)
	if err != nil {
		return fail("%v", err)
	}
	for _, s := range sets {
		if err := payload.Set(s); err != nil {
			return fail("%v", err)
		}
	}
	if *runID == "" {
		*runID = newRunID()
	} else if !uuidPattern.MatchString(*runID) {
		return fail("--run-id %q is not a UUID", *runID)
	}

	cfg, err := LoadConfig(*envPath)
	if err != nil {
		return fail("%v", err)
	}
	// Log the loaded config with secrets masked.
	logCfg := cfg
	logCfg.APIToken = mask(logCfg.APIToken)
	logCfg.DB.Password = mask(logCfg.DB.Password)
	fmt.Fprintf(stderr, "config (from %s): %+v\n", *envPath, logCfg)

	db, err := openDB(cfg.DB)
	if err != nil {
		return fail("%v", err)
	}

	tracker := NewRepoTracker(rtdb.NewPipelineRunRepository(db))
	api := NewAPIClient(cfg.APIURL, cfg.APIToken, cfg.APITimeout)

	res, err := Process(context.Background(), cfg, payload, *runID, tracker, api)
	if err != nil {
		return fail("run %s: %v", *runID, err)
	}

	fmt.Fprintf(stdout, "%s — ticket %d, run_id %s, state %s\n", res.Message, res.TicketID, res.RunID, res.State)
	out, _ := json.Marshal(res)
	fmt.Fprintln(stdout, string(out))

	switch res.Outcome {
	case OutcomeAccepted:
		return 0
	case OutcomeNotEligible:
		return 1
	default:
		return 2
	}
}

func openDB(cfg DBConfig) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to MySQL %s:%s/%s: %w", cfg.Host, cfg.Port, cfg.Name, err)
	}
	return db, nil
}

// mask hides a secret for logging, keeping only whether it is set.
func mask(s string) string {
	if s == "" {
		return ""
	}
	return "****"
}

func readPayload(path string) (*Payload, error) {
	if path == "-" {
		return LoadPayload(os.Stdin)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadPayload(f)
}

// newRunID returns a random UUIDv4.
func newRunID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
