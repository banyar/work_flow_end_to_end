package main

import (
	"fmt"
	"strings"
)

// GateFailure is a failed final.html section 1 check ("No Process").
type GateFailure struct {
	Gate   string // which check failed, stored in the event detail
	Event  string // pipeline_run_events.event
	Reason string // pipeline_runs.last_state_reason, shown to the user
}

// Event names for the gates (component rt_web_ui).
const (
	EventNotInNOCQueue            = "not_in_noc_queue"
	EventTicketStatusNotAllowed   = "ticket_status_not_allowed"
	EventCPEOrLocalServiceMissing = "cpe_or_local_service_id_missing"
	EventOPICustomerExcluded      = "opi_customer_excluded"
	EventServiceTypeNotEligible   = "service_type_not_eligible"
)

// CheckEligibility runs the section 1 gates in diagram order and returns the
// first failure, or nil when the ticket may be sent to the RT External API.
//
//	step 1  the ticket is in the NOC queue, in an allowed status
//	        (new, in_progress, re-open … re-open-5)
//	step 2  CPE ID and Local Service ID are included
//	step 3  the OPI-side custom field is empty
//	step 4  Service Type is MNet, MNet Plus, G2 Net or G2 Plus
func CheckEligibility(cfg Config, p *Payload) *GateFailure {
	if queue := p.Field("queue"); !strings.EqualFold(queue, cfg.NOCQueue) {
		return &GateFailure{Gate: "noc_queue", Event: EventNotInNOCQueue,
			Reason: fmt.Sprintf("ticket queue %q is not %q", queue, cfg.NOCQueue)}
	}

	if status := p.Field("status"); !isAllowedTicketStatus(status, cfg.AllowedTicketStatuses) {
		return &GateFailure{Gate: "ticket_status", Event: EventTicketStatusNotAllowed,
			Reason: fmt.Sprintf("ticket status %q is not one of %s", status, strings.Join(cfg.AllowedTicketStatuses, ", "))}
	}

	var missing []string
	for _, key := range []string{"cpe_id", "local_service_id"} {
		if p.CustomField(key) == "" {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return &GateFailure{Gate: "cpe_and_local_service_id", Event: EventCPEOrLocalServiceMissing,
			Reason: "CPE ID and Local Service ID are required (missing: " + strings.Join(missing, ", ") + ")"}
	}

	for _, key := range cfg.OPIFields {
		if v := p.CustomField(key); v != "" {
			return &GateFailure{Gate: "opi", Event: EventOPICustomerExcluded,
				Reason: fmt.Sprintf("OPI customer (%s=%q) is excluded from automation", key, v)}
		}
	}

	serviceType := p.CustomField("service_type")
	if !isEligibleServiceType(serviceType, cfg.EligibleServiceTypes) {
		return &GateFailure{Gate: "service_type", Event: EventServiceTypeNotEligible,
			Reason: fmt.Sprintf("Service Type %q is not one of %s", serviceType, strings.Join(cfg.EligibleServiceTypes, ", "))}
	}
	return nil
}

// isEligibleServiceType compares ignoring case, spaces, '-' and '_', so the
// diagram's "G2net" / "Mnet" match the AC's "G2 Net" / "MNet".
func isEligibleServiceType(value string, eligible []string) bool {
	v := normalizeServiceType(value)
	if v == "" {
		return false
	}
	for _, e := range eligible {
		if normalizeServiceType(e) == v {
			return true
		}
	}
	return false
}

func normalizeServiceType(s string) string {
	return strings.NewReplacer(" ", "", "-", "", "_", "").Replace(strings.ToLower(strings.TrimSpace(s)))
}

// isAllowedTicketStatus compares ignoring case and treating spaces as '_',
// since RT sends lifecycle names such as "New", "Re-Open" or "In_Progress".
func isAllowedTicketStatus(value string, allowed []string) bool {
	v := normalizeTicketStatus(value)
	if v == "" {
		return false
	}
	for _, a := range allowed {
		if normalizeTicketStatus(a) == v {
			return true
		}
	}
	return false
}

func normalizeTicketStatus(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "_")
}
