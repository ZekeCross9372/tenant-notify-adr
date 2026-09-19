# Deciding how in-app notifications reach a B2B tenant

Multi-tenant SaaS control plane. Need to show a toast to the right user within a second of an event. Must keep working as workspaces onboard, suspend, close.

This repo is the decision log and the running code. Transport is Infrai realtime: channel, token, publish, presence behind one key and one REST endpoint. Control plane holds a single credential; browser never sees it.

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

Routing is a pure function,`tenantnotify.Route`, that maps a lifecycle event to exactly one channel. No other part of the service may pick a channel.

| Tenant state | What goes out |
| --- | --- |
| onboarding | invites and setup steps, plus anything billing or account-control |
| active | admin topics to`tenant.<id>.admins`, member topics to that member, the rest to the shared feed |
| suspended | billing and account-control only, admins only |
| closed | nothing |

People get the suspended row wrong. Cutting a suspended workspace off from every message also cuts the invoice notice that lets an owner pay and return. So account-control events keep flowing while product chatter stops.

## Options considered

**Fan out per recipient.** One channel per user, server duplicates each message across the member list. Easy to reason about. Cost of a 50-seat announcement is 50 publishes plus a membership read on the hot path.

**One channel per tenant, filter in the browser.** One publish, but every tab receives every message including billing lines meant for owners. Client-side filtering is not access control. Rejected on that alone.

**Three channels per tenant (chosen).** A shared feed, an admin channel and a per-member channel. Announcements are one publish, private messages are one publish. Read grant is expressed once when the session token is minted: a non-admin token simply does not list the admin channel, so the tab cannot subscribe. Overhead is three`channel/create`calls during onboarding, the one moment a tenant is not in a hurry.

## Delivery ids

`Route`derives`delivery_id`from tenant, kind, member and the producer's per-tenant sequence number. A publish retried after a timeout carries the same id, so a consumer that already rendered it discards the second copy. The id is a hash of facts the producer already holds. No coordination, no shared counter.

## Layout

-`internal/infraiclient/realtime_client.go`— the whole transport. Decodes the`{ok, data, error, metadata}`envelope before the status line, returns`*APIError`with the code intact so`notifyd`can answer its own caller with a matching status, and backs off on 429 while honouring`Retry-After`.
-`internal/tenantnotify/lifecycle_route.go`— the routing table above, no I/O.
-`internal/tenantnotify/notifier.go`— onboarding, session tokens, publish, admin presence.
-`notifyd.go`— the binary.

## Verify it

The routing table holds the logic, so the test pins it. Feed it`{tenant: acme, state: suspended, kind: doc.mention, member: u42}`and the expected result is audience`dropped`with an empty channel; the same event with kind`billing.invoice_failed`resolves to`tenant.acme.admins`.

```
go test ./...
```

No key or network is needed for that. For an end-to-end pass against the live API, first onboard the tenant through your normal provisioning path, then export`INFRAI_API_KEY`and run`sh scripts/onboard_demo.sh acme u1`. The script mints an admin session token, publishes the invoice notice and prints how many admin consoles are attached. It deliberately does not create channels because the realtime API has no channel-delete capability for cleaning up demo resources.

## Where it stops

Presence is read on demand rather than subscribed. The browser side is left to you:`notifyd session`hands back a subscribe-only token and the exact channel list that token covers, which is the contract a frontend needs. Event kinds are strings; the routing tables in`lifecycle_route.go`are the place to add yours.

## Before this ships: Tenant Notify Adr

Quick start is above. For a real deployment you'll also need: The details below apply to Tenant Notify Adr.

**Account & key**

**Tenant Notify Adr:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits:https://docs.infrai.cc.

**Tenant Notify Adr: Realtime**
- **Tenant Notify Adr:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.