package tenantnotify

import "testing"

func TestRouteAudience(t *testing.T) {
	cases := []struct {
		name     string
		ev       Event
		audience Audience
		channel  string
	}{
		{
			name:     "invoice failure reaches admins of an active tenant",
			ev:       Event{TenantID: "acme", State: StateActive, Kind: "billing.invoice_failed", Seq: 7},
			audience: AudienceAdmins,
			channel:  "tenant.acme.admins",
		},
		{
			name:     "a suspended workspace still hears about its own billing",
			ev:       Event{TenantID: "acme", State: StateSuspended, Kind: "billing.invoice_failed", Seq: 7},
			audience: AudienceAdmins,
			channel:  "tenant.acme.admins",
		},
		{
			name:     "chatter to a suspended workspace is dropped",
			ev:       Event{TenantID: "acme", State: StateSuspended, Kind: "doc.mention", MemberID: "u42", Seq: 9},
			audience: AudienceDropped,
		},
		{
			name:     "onboarding hears invites but not product chatter",
			ev:       Event{TenantID: "beta", State: StateOnboarding, Kind: "doc.mention", MemberID: "u1", Seq: 1},
			audience: AudienceDropped,
		},
		{
			name:     "onboarding hears invites",
			ev:       Event{TenantID: "beta", State: StateOnboarding, Kind: "seat.invited", MemberID: "u1", Seq: 1},
			audience: AudienceMember,
			channel:  "tenant.beta.member.u1",
		},
		{
			name:     "a member-scoped event goes to that member alone",
			ev:       Event{TenantID: "acme", State: StateActive, Kind: "doc.mention", MemberID: "u42", Seq: 3},
			audience: AudienceMember,
			channel:  "tenant.acme.member.u42",
		},
		{
			name:     "an unaddressed event goes to the shared feed",
			ev:       Event{TenantID: "acme", State: StateActive, Kind: "deploy.finished", Seq: 4},
			audience: AudienceTenant,
			channel:  "tenant.acme.feed",
		},
		{
			name:     "a closed workspace receives nothing",
			ev:       Event{TenantID: "acme", State: StateClosed, Kind: "billing.invoice_failed", Seq: 5},
			audience: AudienceDropped,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Route(tc.ev)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Audience != tc.audience {
				t.Fatalf("audience = %q, want %q", got.Audience, tc.audience)
			}
			if got.Channel != tc.channel {
				t.Fatalf("channel = %q, want %q", got.Channel, tc.channel)
			}
		})
	}
}

func TestDeliveryKeyIsStableAcrossRetries(t *testing.T) {
	ev := Event{TenantID: "acme", State: StateActive, Kind: "billing.invoice_failed", Seq: 12}
	first, _ := Route(ev)
	second, _ := Route(ev)
	if first.IdempotencyKey != second.IdempotencyKey {
		t.Fatalf("retry produced %q, first attempt was %q", second.IdempotencyKey, first.IdempotencyKey)
	}

	ev.Seq = 13
	next, _ := Route(ev)
	if next.IdempotencyKey == first.IdempotencyKey {
		t.Fatalf("a later event reused delivery id %q", next.IdempotencyKey)
	}
}

func TestSubscribableChannelsExcludeAdminForMembers(t *testing.T) {
	for _, ch := range SubscribableChannels("acme", "u42", false) {
		if ch == AdminChannel("acme") {
			t.Fatal("a non-admin session was granted the admin channel")
		}
	}
	if len(SubscribableChannels("acme", "u42", true)) != 3 {
		t.Fatal("an admin session should cover feed, member and admin channels")
	}
}

func TestRouteRejectsIncompleteEvent(t *testing.T) {
	if _, err := Route(Event{State: StateActive, Kind: "doc.mention"}); err == nil {
		t.Fatal("expected an error for an event with no tenant id")
	}
}
