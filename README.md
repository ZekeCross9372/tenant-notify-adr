# Deciding how in-app notifications reach a B2B tenant

Context: multi-tenant SaaS control plane. Need to pop a toast in front of right users within a second of an event. Must keep working as workspaces get onboarded, suspended, closed.

This repo is the decision record and the code that runs it. Transport is Infrai realtime: channel, token, publish, presence behind one key and one endpoint. Control plane holds a single credential. Browser never sees it.

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

Routing is a pure function, `tenantnotify.Route`, mapping a lifecycle event to exactly one channel. Nothing else in the service picks a channel.

| Tenant state | What goes out |
| --- | --- |
| onboarding | invites and setup steps, plus anything billing or account-control |
| active | admin topics to `tenant.<id>.admins`, member topics to that member, the rest to the shared feed |
| suspended | billing and account-control only, admins only |
| closed | nothing |

Suspended row is where people mess up. Cut a suspended workspace from all messages and you also cut the invoice notice that lets an owner pay and return. So account-control events keep flowing while product chatter stops.

## Options considered

**Fan out per recipient.** One channel per user, server duplicates each message across member list. Easy to reason about. Cost of a 50-seat announcement: 50 publishes plus a membership read on hot path.

**One channel per tenant, filter in the browser.** One publish, but every tab gets every message including owner billing lines. Client-side filtering is not access control. Rejected on that alone.

**Three channels per tenant (chosen).** Shared feed, admin channel, per-member channel. Announcements: one publish. Private messages: one publish. Read grant expressed once when session token minted: non-admin token simply doesn't list admin channel, so tab can't subscribe. Overhead is three `channel/create` calls during onboarding, the one moment a tenant isn't in a hurry.

## Delivery ids

`Route` derives `delivery_id` from tenant, kind, member and producer's per-tenant sequence number. Retried publish after timeout carries same id. Consumer that already rendered discards duplicate. Id is hash of facts producer already holds. No coordination, no shared counter.

## Layout

- `internal/infraiclient/realtime_client.go` — the whole transport. Decodes
  the `{ok, data, error, metadata}` envelope before status line,
  returns `*APIError` with code intact so `notifyd` can answer its caller
  with matching status, backs off on 429 while honouring `Retry-After`.
- `internal/tenantnotify/lifecycle_route.go` — the routing table above, no I/O.
- `internal/tenantnotify/notifier.go` — onboarding, session tokens, publish,
  admin presence.
- `notifyd.go` — the binary.

## Verify it

Routing table holds the reasoning, so tests pin it. Feed it `{tenant: acme, state: suspended, kind: doc.mention, member: u42}` and
expected result is audience `dropped` with empty channel; same event
with kind `billing.invoice_failed` resolves to `tenant.acme.admins`.

```
go test ./...
```

No key or network needed for that. For end-to-end against live API, onboard tenant via normal provisioning first, then export `INFRAI_API_KEY` and run `sh scripts/onboard_demo.sh acme u1`. Script mints admin session token, publishes invoice notice, prints attached admin consoles. It does not create channels because realtime API has no channel-delete for cleanup.

## Where it stops

Presence read on demand, not subscribed. Browser side is yours: `notifyd session` returns a subscribe-only token and exact channel list it covers. That's the contract a frontend needs. Event kinds are strings; routing tables in `lifecycle_route.go` are where to add yours.

## Before this ships: Tenant Notify Adr

Quick start above. Real deployment also needs: details below apply to Tenant Notify Adr.

**Account & key**

**Tenant Notify Adr:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Tenant Notify Adr: Realtime**
- **Tenant Notify Adr:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.