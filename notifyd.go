// Command notifyd is the single binary a B2B SaaS control plane runs to push
// in-app notifications: onboard a tenant, mint a browser session token, emit a
// lifecycle event, or check who is watching the admin console.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"tenant-notify/internal/infraiclient"
	"tenant-notify/internal/tenantnotify"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		var apiErr *infraiclient.APIError
		if errors.As(err, &apiErr) {
			fmt.Fprintf(os.Stderr, "rejected: %s (%s)\n", apiErr.Message, apiErr.Code)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: notifyd <onboard|session|emit|presence> [flags]")
	}

	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		return errors.New("set INFRAI_API_KEY (one key covers realtime and every other capability on the same bill)")
	}
	n := &tenantnotify.Notifier{API: infraiclient.New(key)}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "onboard":
		fs := flag.NewFlagSet("onboard", flag.ExitOnError)
		tenant := fs.String("tenant", "", "tenant id")
		owner := fs.String("owner", "", "owner member id")
		fs.Parse(rest)
		channels, err := n.OnboardTenant(*tenant, *owner)
		if err != nil {
			return err
		}
		return emit(map[string]any{"tenant": *tenant, "channels": channels})

	case "session":
		fs := flag.NewFlagSet("session", flag.ExitOnError)
		tenant := fs.String("tenant", "", "tenant id")
		member := fs.String("member", "", "member id")
		admin := fs.Bool("admin", false, "member holds the admin role")
		fs.Parse(rest)
		token, err := n.SessionToken(*tenant, *member, *admin)
		if err != nil {
			return err
		}
		return emit(map[string]any{
			"client_id": *tenant + ":" + *member,
			"channels":  tenantnotify.SubscribableChannels(*tenant, *member, *admin),
			"token":     token,
		})

	case "emit":
		fs := flag.NewFlagSet("emit", flag.ExitOnError)
		tenant := fs.String("tenant", "", "tenant id")
		state := fs.String("state", "active", "onboarding|active|suspended|closed")
		kind := fs.String("kind", "", "event kind, e.g. billing.invoice_failed")
		member := fs.String("member", "", "member id, when the event concerns one person")
		seq := fs.Int64("seq", 1, "monotonic sequence number for this tenant")
		title := fs.String("title", "", "human-readable line shown in the tray")
		fs.Parse(rest)

		d, err := n.Deliver(tenantnotify.Event{
			TenantID: *tenant,
			State:    tenantnotify.TenantState(*state),
			Kind:     *kind,
			MemberID: *member,
			Seq:      *seq,
		}, map[string]any{"title": *title})
		if err != nil {
			return err
		}
		return emit(map[string]any{
			"audience":    d.Audience,
			"channel":     d.Channel,
			"event":       d.Event,
			"delivery_id": d.IdempotencyKey,
		})

	case "presence":
		fs := flag.NewFlagSet("presence", flag.ExitOnError)
		tenant := fs.String("tenant", "", "tenant id")
		fs.Parse(rest)
		count, err := n.AdminsOnline(*tenant)
		if err != nil {
			return err
		}
		return emit(map[string]any{"channel": tenantnotify.AdminChannel(*tenant), "admins_online": count})

	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func emit(v map[string]any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
