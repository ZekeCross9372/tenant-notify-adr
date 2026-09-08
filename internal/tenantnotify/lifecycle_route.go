// Package tenantnotify turns B2B tenant lifecycle events into realtime
// deliveries. Routing is a pure function so it can be tested without a network.
package tenantnotify

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// TenantState is the account lifecycle position of a workspace.
type TenantState string

const (
	StateOnboarding TenantState = "onboarding"
	StateActive     TenantState = "active"
	StateSuspended  TenantState = "suspended"
	StateClosed     TenantState = "closed"
)

// Audience decides who a notification is addressed to.
type Audience string

const (
	AudienceAdmins  Audience = "admins"
	AudienceMember  Audience = "member"
	AudienceTenant  Audience = "tenant"
	AudienceDropped Audience = "dropped"
)

// Event is one thing that happened inside a tenant.
type Event struct {
	TenantID string
	State    TenantState
	Kind     string // e.g. "billing.invoice_failed", "seat.invited", "doc.mention"
	MemberID string // set when the event concerns one person
	Seq      int64  // monotonic per tenant; makes the delivery id stable
}

// Delivery is the resolved instruction handed to the realtime transport.
type Delivery struct {
	Channel        string
	Event          string
	Audience       Audience
	IdempotencyKey string
}

// adminOnly kinds carry billing or account-control meaning: they reach the
// admin channel even while the workspace is suspended, because that is exactly
// when an owner needs to see them.
var adminOnly = map[string]bool{
	"billing.invoice_failed": true,
	"billing.plan_changed":   true,
	"account.suspended":      true,
	"account.reinstated":     true,
	"seat.limit_reached":     true,
	"security.sso_changed":   true,
}

// onboardingVisible kinds are the only ones worth sending before a workspace
// has finished setup; everything else would land in an empty room.
var onboardingVisible = map[string]bool{
	"seat.invited":        true,
	"setup.step_complete": true,
	"setup.finished":      true,
}

// Route resolves an event to a single delivery. It returns Audience
// AudienceDropped when the event should not be sent at all.
func Route(ev Event) (Delivery, error) {
	if ev.TenantID == "" || ev.Kind == "" {
		return Delivery{}, errors.New("tenantnotify: event needs a tenant id and a kind")
	}

	d := Delivery{Event: ev.Kind, IdempotencyKey: deliveryKey(ev)}

	switch ev.State {
	case StateClosed:
		d.Audience = AudienceDropped
		return d, nil
	case StateSuspended:
		if !adminOnly[ev.Kind] {
			d.Audience = AudienceDropped
			return d, nil
		}
		d.Audience = AudienceAdmins
		d.Channel = AdminChannel(ev.TenantID)
		return d, nil
	case StateOnboarding:
		if !adminOnly[ev.Kind] && !onboardingVisible[ev.Kind] {
			d.Audience = AudienceDropped
			return d, nil
		}
	}

	switch {
	case adminOnly[ev.Kind]:
		d.Audience = AudienceAdmins
		d.Channel = AdminChannel(ev.TenantID)
	case ev.MemberID != "":
		d.Audience = AudienceMember
		d.Channel = MemberChannel(ev.TenantID, ev.MemberID)
	default:
		d.Audience = AudienceTenant
		d.Channel = TenantChannel(ev.TenantID)
	}
	return d, nil
}

func AdminChannel(tenantID string) string  { return "tenant." + tenantID + ".admins" }
func TenantChannel(tenantID string) string { return "tenant." + tenantID + ".feed" }
func MemberChannel(tenantID, memberID string) string {
	return "tenant." + tenantID + ".member." + memberID
}

// deliveryKey is derived only from facts the producer already knows, so a
// retried publish reuses the same key and applies once.
func deliveryKey(ev Event) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", ev.TenantID, ev.Kind, ev.MemberID, ev.Seq)))
	return "dlv_" + hex.EncodeToString(sum[:8])
}

// SubscribableChannels lists what a browser session for this member may read.
// Admins additionally get the admin channel; nobody gets another tenant's.
func SubscribableChannels(tenantID, memberID string, isAdmin bool) []string {
	out := []string{TenantChannel(tenantID), MemberChannel(tenantID, memberID)}
	if isAdmin {
		out = append(out, AdminChannel(tenantID))
	}
	sort.Strings(out)
	return out
}

// NormalizeKind trims and lowercases an incoming event kind so that routing
// tables do not depend on how a caller typed it.
func NormalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}
