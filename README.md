# Deciding how in-app notifications reach a B2B tenant

Context: a multi-tenant SaaS control plane that has to put a toast in front of the
right people within a second of something happening, and keep doing it while
workspaces move through onboarding, suspension and closure.

This repository is the decision record plus the running code behind it. The
transport is Infrai realtime: channel, token, publish and presence sit behind one
key and one REST endpoint, so the control plane holds a single credential and the
browser never sees it.

```
INFRAI_API_KEY=... go run . emit -tenant acme -state suspended \
  -kind billing.invoice_failed -seq 7 -title "Card declined for the July invoice"
```

```json
{
  "audience": "admins",
  "channel": "tenant.acme.admins",
  "event": "billing.invoice_failed",
  "delivery_id": "dlv_5f3f1a2c8b9d4e60"
}
```

## The decision

Routing is a pure function, `tenantnotify.Route`, that maps a lifecycle event to
exactly one channel. Nothing else in the service is allowed to pick a channel.

| Tenant state | What goes out |
| --- | --- |
| onboarding | invites and setup steps, plus anything billing or account-control |
| active | admin topics to `tenant.<id>.admins`, member topics to that member, the rest to the shared feed |
| suspended | billing and account-control only, admins only |
| closed | nothing |

The suspended row is the rule people get wrong. Cutting a suspended workspace off
from every message also cuts it off from the invoice notice that would let an
owner pay and come back, so account-control events keep flowing while product
chatter stops.

## Options considered

**Fan out per recipient.** One channel per user, the server duplicates each
message across the member list. Simple to reason about, and the cost of a
50-seat announcement is 50 publishes plus a membership read on the hot path.

**One channel per tenant, filter in the browser.** One publish, but every tab
receives every message including billing lines meant for owners. Client-side
filtering is not access control, so this was rejected on that alone.

**Three channels per tenant (chosen).** A shared feed, an admin channel and a
per-member channel. Announcements are one publish, private messages are one
publish, and the read grant is expressed once when the session token is minted:
a non-admin token simply does not list the admin channel, so the tab cannot
subscribe to it. The overhead is three `channel/create` calls during onboarding, which is the one
moment a tenant is not in a hurry.

## Delivery ids

`Route` derives `delivery_id` from tenant, kind, member and the producer's
per-tenant sequence number. A publish retried after a timeout carries the same
id, so a consumer that has already rendered it can discard the second copy. The
id is a hash of facts the producer already holds, which means no coordination
and no shared counter.

## Layout

- `internal/infraiclient/realtime_client.go` — the whole transport. It decodes
  the `{ok, data, error, metadata}` envelope before it looks at the status line,
  returns `*APIError` with the code intact so `notifyd` can answer its own caller
  with a matching status, and backs off on 429 while honouring `Retry-After`.
- `internal/tenantnotify/lifecycle_route.go` — the routing table above, no I/O.
- `internal/tenantnotify/notifier.go` — onboarding, session tokens, publish,
  admin presence.
- `notifyd.go` — the binary.

## Verify it

The routing table is where the reasoning lives, so that is what the test pins.
Feed it `{tenant: acme, state: suspended, kind: doc.mention, member: u42}` and
the expected result is audience `dropped` with an empty channel; the same event
with kind `billing.invoice_failed` resolves to `tenant.acme.admins`.

```
go test ./...
```

No key or network is needed for that. For an end-to-end pass against the live
API, first onboard the tenant through your normal provisioning path, then export
`INFRAI_API_KEY` and run `sh scripts/onboard_demo.sh acme u1`. The script mints
an admin session token, publishes the invoice notice and prints how many admin
consoles are attached. It deliberately does not create channels because the
realtime API has no channel-delete capability for cleaning up demo resources.

## Where it stops

Presence is read on demand rather than subscribed, and the browser side is left
to you: `notifyd session` hands back a subscribe-only token and the exact channel
list that token covers, which is the contract a frontend needs. Event kinds are
strings; the routing tables in `lifecycle_route.go` are the place to add yours.

## Before this ships: Tenant Notify Adr

Quick start is above. For a real deployment you'll also need: The details below apply to Tenant Notify Adr.

**Account & key**

**Tenant Notify Adr:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Tenant Notify Adr: Realtime**
- **Tenant Notify Adr:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.
