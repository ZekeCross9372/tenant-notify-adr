package tenantnotify

import (
	"fmt"

	"tenant-notify/internal/infraiclient"
)

// Notifier binds the routing rules to the realtime transport.
type Notifier struct {
	API *infraiclient.Client
}

// OnboardTenant creates the three channels a workspace needs before its first
// admin signs in: the shared feed, the admin channel, and the owner's own.
func (n *Notifier) OnboardTenant(tenantID, ownerID string) ([]string, error) {
	channels := []string{
		TenantChannel(tenantID),
		AdminChannel(tenantID),
		MemberChannel(tenantID, ownerID),
	}
	for _, ch := range channels {
		if err := n.API.CreateChannel(infraiclient.ChannelCreateInput{
			Channel: ch,
			Type:    "presence",
		}); err != nil {
			return nil, fmt.Errorf("create %s: %w", ch, err)
		}
	}
	return channels, nil
}

// SessionToken mints a short-lived, subscribe-only token for one browser tab.
// The server key stays on the server; the tab connects with this.
func (n *Notifier) SessionToken(tenantID, memberID string, isAdmin bool) (string, error) {
	tok, err := n.API.IssueToken(infraiclient.TokenIssueInput{
		ClientID:     tenantID + ":" + memberID,
		Channels:     SubscribableChannels(tenantID, memberID, isAdmin),
		Capabilities: []string{"subscribe", "presence"},
		TTLSeconds:   900,
	})
	if err != nil {
		return "", err
	}
	return tok.Token, nil
}

// Deliver routes an event and publishes it. A dropped event is a normal
// outcome: the delivery is returned with Audience AudienceDropped and nothing
// is sent.
func (n *Notifier) Deliver(ev Event, payload map[string]any) (Delivery, error) {
	ev.Kind = NormalizeKind(ev.Kind)
	d, err := Route(ev)
	if err != nil || d.Audience == AudienceDropped {
		return d, err
	}

	data := map[string]any{"delivery_id": d.IdempotencyKey, "tenant_id": ev.TenantID}
	for k, v := range payload {
		data[k] = v
	}

	if err := n.API.Publish(infraiclient.PublishInput{
		Channel: d.Channel,
		Event:   d.Event,
		Data:    data,
	}); err != nil {
		return d, err
	}
	return d, nil
}

// AdminsOnline reports how many admin consoles are attached right now, which
// is what an operator wants to know before pushing a maintenance notice.
func (n *Notifier) AdminsOnline(tenantID string) (int, error) {
	p, err := n.API.Presence(AdminChannel(tenantID))
	if err != nil {
		return 0, err
	}
	return len(p.Members), nil
}
