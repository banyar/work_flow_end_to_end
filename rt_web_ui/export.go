package main

// export reads tickets from the RT DB (read-only) and writes them as one JSON
// array whose elements are in the format `send` accepts, so real tickets can
// be replayed as samples:
//
//	SELECT * FROM Tickets WHERE Status IN (...) AND Queue = 43
//
// plus the queue name, the creator's name and the ticket's custom fields.

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (
	defaultExportQueue    = 43 // Network Operation Center (NOC)
	defaultExportStatuses = "re-open,re-open-1,re-open-2,re-open-3,re-open-4,re-open-5,new,in_progress"
	defaultExportOut      = "samples/tickets.json"
	rtTimeLayout          = "2006-01-02 15:04:05"
	// ticket ids per custom field query, to keep the IN list bounded
	cfQueryBatch = 500
)

// cfKeyOverrides is for RT custom field names that FormatCustomFieldName's
// rule (noc_automation/frontiir/formatter) turns into an unusable key.
// Every other name follows the rule.
var cfKeyOverrides = map[string]string{
	"Ticket No.":             "ticket_no",
	"CaseID (POI Reference)": "case_id",
}

// arrayCFKeys are always written as a JSON array, even with one value.
var arrayCFKeys = map[string]bool{"tags": true}

// ExportTicket is one element of the output, in the send payload format.
type ExportTicket struct {
	ID           uint64         `json:"id"`
	RunID        string         `json:"run_id"`
	Status       string         `json:"status"`
	Queue        string         `json:"queue"`
	QueueID      string         `json:"queue_id"`
	Creator      string         `json:"creator"`
	Type         string         `json:"type"`
	Created      string         `json:"created"`
	Started      string         `json:"started"`
	Resolved     string         `json:"resolved"`
	CustomFields map[string]any `json:"custom_fields"`
}

type exportOptions struct {
	Queue    int
	Statuses []string
	IDs      []uint64
	Limit    int
	// AllFields adds every custom field the queue can have, with "" (or []
	// for arrayCFKeys) when the ticket has no value, so each ticket has the
	// same complete set of keys.
	AllFields bool
}

// cfValueRow is one ObjectCustomFieldValues row of a ticket.
type cfValueRow struct {
	TicketID uint64
	Name     string
	Content  string
	Large    []byte
	Encoding string
}

func runExport(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	envPath := fs.String("env", ".env", "")
	queue := fs.Int("queue", defaultExportQueue, "")
	statuses := fs.String("statuses", defaultExportStatuses, "")
	ids := fs.String("ids", "", "")
	limit := fs.Int("limit", 0, "")
	out := fs.String("out", defaultExportOut, "")
	allFields := fs.Bool("all-fields", false, "")
	fs.Usage = func() { fmt.Fprintln(stderr, usage) }
	if err := fs.Parse(args); err != nil {
		return 3
	}

	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "error: "+format+"\n", a...)
		return 3
	}

	opts := exportOptions{Queue: *queue, Statuses: splitList(*statuses), Limit: *limit, AllFields: *allFields}
	if len(opts.Statuses) == 0 {
		return fail("--statuses is empty")
	}
	if opts.Limit < 0 {
		return fail("--limit must be >= 0")
	}
	for _, raw := range splitList(*ids) {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return fail("--ids: %q is not a ticket id", raw)
		}
		opts.IDs = append(opts.IDs, id)
	}

	dbCfg, err := LoadDBConfig(*envPath)
	if err != nil {
		return fail("%v", err)
	}
	db, err := openDB(dbCfg)
	if err != nil {
		return fail("%v", err)
	}

	tickets, err := exportTickets(db, opts)
	if err != nil {
		return fail("%v", err)
	}

	data, err := json.MarshalIndent(tickets, "", "    ")
	if err != nil {
		return fail("%v", err)
	}
	data = append(data, '\n')

	if *out == "-" {
		_, _ = stdout.Write(data)
	} else {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			return fail("%v", err)
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			return fail("%v", err)
		}
	}
	fmt.Fprintf(stderr, "exported %d ticket(s) from queue %d (status %s) to %s\n",
		len(tickets), opts.Queue, strings.Join(opts.Statuses, ","), *out)
	return 0
}

// exportTickets runs the ticket query and attaches each ticket's custom fields.
func exportTickets(db *gorm.DB, opts exportOptions) ([]ExportTicket, error) {
	query := `SELECT t.id, IFNULL(t.Status, ''), t.Queue, IFNULL(q.Name, ''), IFNULL(u.Name, ''),
		IFNULL(t.Type, ''), t.Created, t.Started, t.Resolved
		FROM Tickets t
		LEFT JOIN Queues q ON q.id = t.Queue
		LEFT JOIN Users u ON u.id = t.Creator
		WHERE t.Status IN ? AND t.Queue = ?`
	args := []any{opts.Statuses, opts.Queue}
	if len(opts.IDs) > 0 {
		query += " AND t.id IN ?"
		args = append(args, opts.IDs)
	}
	query += " ORDER BY t.id"
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query tickets: %w", err)
	}
	defer rows.Close()

	tickets := []ExportTicket{} // "[]", not "null", when nothing matches
	for rows.Next() {
		var (
			t                          ExportTicket
			queueID                    int64
			created, started, resolved sql.NullTime
		)
		if err := rows.Scan(&t.ID, &t.Status, &queueID, &t.Queue, &t.Creator, &t.Type,
			&created, &started, &resolved); err != nil {
			return nil, fmt.Errorf("read ticket row: %w", err)
		}
		t.QueueID = strconv.FormatInt(queueID, 10)
		t.Created, t.Started, t.Resolved = formatRTTime(created), formatRTTime(started), formatRTTime(resolved)
		tickets = append(tickets, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read tickets: %w", err)
	}

	cfRows, err := loadCustomFieldValues(db, tickets)
	if err != nil {
		return nil, err
	}
	byTicket := buildCustomFields(cfRows)
	var queueFields []string
	if opts.AllFields && len(tickets) > 0 {
		if queueFields, err = loadQueueCustomFields(db, opts.Queue); err != nil {
			return nil, err
		}
	}
	for i := range tickets {
		if cf := byTicket[tickets[i].ID]; cf != nil {
			tickets[i].CustomFields = cf
		} else {
			tickets[i].CustomFields = map[string]any{}
		}
		fillMissingCustomFields(tickets[i].CustomFields, queueFields)
	}
	return tickets, nil
}

// loadQueueCustomFields returns the names of the enabled ticket custom
// fields applied globally (ObjectId 0) or to queue.
func loadQueueCustomFields(db *gorm.DB, queue int) ([]string, error) {
	var names []string
	err := db.Raw(`SELECT DISTINCT cf.Name
		FROM CustomFields cf
		JOIN ObjectCustomFields ocf ON ocf.CustomField = cf.id
		WHERE cf.LookupType = 'RT::Queue-RT::Ticket' AND cf.Disabled = 0 AND ocf.ObjectId IN (0, ?)
		ORDER BY cf.Name`, queue).Scan(&names).Error
	if err != nil {
		return nil, fmt.Errorf("query queue custom fields: %w", err)
	}
	return names, nil
}

// fillMissingCustomFields adds each field in names that cf lacks, keyed like
// buildCustomFields. Names RT has twice (e.g. two "Township" fields) map to
// one key; a value the ticket already has is never replaced.
func fillMissingCustomFields(cf map[string]any, names []string) {
	for _, name := range names {
		key := cfKey(name)
		if _, ok := cf[key]; ok || key == "" {
			continue
		}
		if arrayCFKeys[key] {
			cf[key] = []string{}
		} else {
			cf[key] = ""
		}
	}
}

func loadCustomFieldValues(db *gorm.DB, tickets []ExportTicket) ([]cfValueRow, error) {
	var out []cfValueRow
	for start := 0; start < len(tickets); start += cfQueryBatch {
		end := min(start+cfQueryBatch, len(tickets))
		ids := make([]uint64, 0, end-start)
		for _, t := range tickets[start:end] {
			ids = append(ids, t.ID)
		}
		rows, err := db.Raw(`SELECT o.ObjectId, cf.Name, IFNULL(o.Content, ''), o.LargeContent, IFNULL(o.ContentEncoding, '')
			FROM ObjectCustomFieldValues o
			JOIN CustomFields cf ON cf.id = o.CustomField
			WHERE o.ObjectType = 'RT::Ticket' AND o.Disabled = 0 AND o.ObjectId IN ?
			ORDER BY o.ObjectId, o.CustomField, o.id`, ids).Rows()
		if err != nil {
			return nil, fmt.Errorf("query custom fields: %w", err)
		}
		for rows.Next() {
			var r cfValueRow
			if err := rows.Scan(&r.TicketID, &r.Name, &r.Content, &r.Large, &r.Encoding); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read custom field row: %w", err)
			}
			out = append(out, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, fmt.Errorf("read custom fields: %w", err)
		}
	}
	return out, nil
}

// buildCustomFields groups rows per ticket under their payload key. A field
// with several values (or an arrayCFKeys field) becomes an array.
func buildCustomFields(rows []cfValueRow) map[uint64]map[string]any {
	values := map[uint64]map[string][]string{}
	for _, r := range rows {
		if values[r.TicketID] == nil {
			values[r.TicketID] = map[string][]string{}
		}
		key := cfKey(r.Name)
		values[r.TicketID][key] = append(values[r.TicketID][key], cfValue(r))
	}

	out := make(map[uint64]map[string]any, len(values))
	for id, fields := range values {
		cf := make(map[string]any, len(fields))
		for key, vals := range fields {
			if len(vals) == 1 && !arrayCFKeys[key] {
				cf[key] = vals[0]
			} else {
				cf[key] = vals
			}
		}
		out[id] = cf
	}
	return out
}

// cfKey turns an RT custom field name into its payload key, e.g.
// "Fiber CA1 Status" -> "fiber_ca1_status".
func cfKey(name string) string {
	if k, ok := cfKeyOverrides[strings.TrimSpace(name)]; ok {
		return k
	}
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "-", "_")
	return strings.Join(strings.Fields(name), "_")
}

// cfValue is Content, or LargeContent when RT moved a long value there.
func cfValue(r cfValueRow) string {
	if r.Content != "" || len(r.Large) == 0 {
		return r.Content
	}
	if strings.EqualFold(r.Encoding, "base64") {
		if decoded, err := base64.StdEncoding.DecodeString(string(r.Large)); err == nil {
			return string(decoded)
		}
	}
	return string(r.Large)
}

// formatRTTime renders an RT DATETIME as stored; RT's "not set" is 1970-01-01.
func formatRTTime(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(rtTimeLayout)
}
