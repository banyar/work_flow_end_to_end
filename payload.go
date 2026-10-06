package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Payload is the ticket JSON the RT Web UI sends to the RT External API.
// It is kept as a generic map so every field RT provides is forwarded as-is;
// only id and run_id are normalized before sending.
type Payload struct {
	fields map[string]any
}

func LoadPayload(r io.Reader) (*Payload, error) {
	var fields map[string]any
	dec := json.NewDecoder(r)
	dec.UseNumber() // keep ticket ids exact
	if err := dec.Decode(&fields); err != nil {
		return nil, fmt.Errorf("invalid payload JSON: %w", err)
	}
	if _, ok := fields["custom_fields"].(map[string]any); !ok {
		if fields["custom_fields"] != nil {
			return nil, fmt.Errorf(`invalid payload: "custom_fields" must be an object`)
		}
		fields["custom_fields"] = map[string]any{}
	}
	return &Payload{fields: fields}, nil
}

// Set overrides one field: "queue=…" for a top-level field or
// "custom_fields.service_type=…" for a custom field.
func (p *Payload) Set(assignment string) error {
	key, value, ok := strings.Cut(assignment, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" {
		return fmt.Errorf("--set %q: expected key=value", assignment)
	}
	if name, isCF := strings.CutPrefix(key, "custom_fields."); isCF {
		p.customFields()[name] = value
		return nil
	}
	p.fields[key] = value
	return nil
}

// TicketID accepts the id as a JSON string or number.
func (p *Payload) TicketID() (uint64, error) {
	raw := strings.TrimSpace(stringify(p.fields["id"]))
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf(`invalid payload: "id" must be a positive ticket number, got %q`, raw)
	}
	return id, nil
}

func (p *Payload) Field(key string) string {
	return strings.TrimSpace(stringify(p.fields[key]))
}

func (p *Payload) CustomField(key string) string {
	return strings.TrimSpace(stringify(p.customFields()[key]))
}

// Body is the request sent to the RT External API: the payload as given,
// with id as a string (the API's model is a string) and run_id set.
func (p *Payload) Body(ticketID uint64, runID string) ([]byte, error) {
	out := make(map[string]any, len(p.fields)+1)
	for k, v := range p.fields {
		out[k] = v
	}
	out["id"] = strconv.FormatUint(ticketID, 10)
	out["run_id"] = runID
	return json.Marshal(out)
}

func (p *Payload) customFields() map[string]any {
	return p.fields["custom_fields"].(map[string]any)
}

// stringify renders a JSON value for checks and DB columns; arrays (tags)
// become comma-separated.
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(stringify(item)); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(t)
	}
}
