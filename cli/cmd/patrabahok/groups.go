package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/audit"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

func cmdGroup(ctx context.Context, store *mailbox.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: patrabahok group <create|list|show|update|delete|add|remove|role> ...")
	}
	action, rest := args[0], args[1:]
	switch action {
	case "create", "update":
		var name, description, post string
		var external, noExternal bool
		positional := parseFlags(rest,
			map[string]*string{"--name": &name, "--description": &description, "--post": &post},
			map[string]*bool{"--external": &external, "--no-external": &noExternal})
		email, err := arg(positional, 0, "group@domain")
		if err != nil {
			return err
		}
		settings := mailbox.GroupSettings{Name: name, Description: description, PostPolicy: post, AllowExternal: external}
		if action == "update" {
			g, err := store.GroupGet(ctx, email)
			if err != nil {
				return err
			}
			if name == "" {
				settings.Name = g.Name
			}
			if description == "" {
				settings.Description = g.Description
			}
			if post == "" {
				settings.PostPolicy = g.PostPolicy
			}
			settings.AllowExternal = g.AllowExternal
			if external {
				settings.AllowExternal = true
			}
			if noExternal {
				settings.AllowExternal = false
			}
			if err := store.GroupUpdate(ctx, email, settings); err != nil {
				return err
			}
			ok("Group updated: %s", email)
			return nil
		}
		if settings.Name == "" {
			settings.Name, _, _ = strings.Cut(email, "@")
		}
		if settings.PostPolicy == "" {
			settings.PostPolicy = mailbox.PostOrganization
		}
		if err := store.GroupCreate(ctx, email, settings); err != nil {
			return err
		}
		ok("Group created: %s (posting: %s)", strings.ToLower(email), settings.PostPolicy)
		return nil
	case "list":
		domain := ""
		if len(rest) > 0 {
			domain = rest[0]
		}
		list, err := store.GroupList(ctx, domain, "")
		if err != nil {
			return err
		}
		for _, g := range list {
			fmt.Printf("%s\tmembers=%d\tpost=%s\texternal=%v\tname=%q\n", g.Email, g.MemberCount, g.PostPolicy, g.AllowExternal, g.Name)
		}
		return nil
	case "show":
		email, err := arg(rest, 0, "group@domain")
		if err != nil {
			return err
		}
		g, err := store.GroupGet(ctx, email)
		if err != nil {
			return err
		}
		fmt.Printf("%s  %q\nposting: %s  external members: %v\n", g.Email, g.Name, g.PostPolicy, g.AllowExternal)
		if g.Description != "" {
			fmt.Println(g.Description)
		}
		members, err := store.GroupMembers(ctx, email)
		if err != nil {
			return err
		}
		fmt.Printf("\n%d member(s):\n", len(members))
		for _, m := range members {
			fmt.Printf("  %s\t%s\t%s\n", m.Address, m.Role, m.Kind)
		}
		return nil
	case "delete":
		email, err := arg(rest, 0, "group@domain")
		if err != nil {
			return err
		}
		if err := store.GroupDelete(ctx, email); err != nil {
			return err
		}
		ok("Group deleted: %s", email)
		return nil
	case "add":
		var owner bool
		positional := parseFlags(rest, nil, map[string]*bool{"--owner": &owner})
		email, err := arg(positional, 0, "group@domain")
		if err != nil {
			return err
		}
		if len(positional) < 2 {
			return errors.New("usage: patrabahok group add <group@domain> <address>... [--owner]")
		}
		role := mailbox.RoleMember
		if owner {
			role = mailbox.RoleOwner
		}
		results, err := store.GroupAddMembers(ctx, email, positional[1:], role)
		if err != nil {
			return err
		}
		failed := 0
		for _, r := range results {
			if r.Added {
				ok("Added %s as %s", r.Address, role)
			} else {
				failed++
				fmt.Printf("\033[0;31m[FAIL]\033[0m %s: %s\n", r.Address, r.Error)
			}
		}
		if failed > 0 {
			return fmt.Errorf("%d address(es) could not be added", failed)
		}
		return nil
	case "remove":
		email, err := arg(rest, 0, "group@domain")
		if err != nil {
			return err
		}
		member, err := arg(rest, 1, "member address")
		if err != nil {
			return err
		}
		if err := store.GroupRemoveMember(ctx, email, member); err != nil {
			return err
		}
		ok("Removed %s from %s", member, email)
		return nil
	case "role":
		email, err := arg(rest, 0, "group@domain")
		if err != nil {
			return err
		}
		member, err := arg(rest, 1, "member address")
		if err != nil {
			return err
		}
		role, err := arg(rest, 2, "role (member or owner)")
		if err != nil {
			return err
		}
		if err := store.GroupSetMemberRole(ctx, email, member, role); err != nil {
			return err
		}
		ok("%s is now a %s of %s", member, role, email)
		return nil
	default:
		return fmt.Errorf("unknown group action: %s", action)
	}
}

// cmdMailboxForward implements "mailbox forward <user@domain> <destination>|off [--no-copy]".
func cmdMailboxForward(ctx context.Context, store *mailbox.Store, args []string) error {
	var noCopy bool
	positional := parseFlags(args, nil, map[string]*bool{"--no-copy": &noCopy})
	email, err := arg(positional, 0, "user@domain")
	if err != nil {
		return err
	}
	dest, err := arg(positional, 1, "destination address (or 'off')")
	if err != nil {
		return err
	}
	if dest == "off" {
		if err := store.ForwardClear(ctx, email); err != nil {
			return err
		}
		ok("Forwarding turned off for %s", email)
		return nil
	}
	if err := store.ForwardSet(ctx, email, dest, !noCopy); err != nil {
		return err
	}
	copyNote := "keeping a copy"
	if noCopy {
		copyNote = "without keeping a copy"
	}
	ok("%s now forwards to %s (%s)", email, dest, copyNote)
	return nil
}

// cmdDomainCatchAll implements "domain catchall <domain> <destination>|off".
func cmdDomainCatchAll(ctx context.Context, store *mailbox.Store, args []string) error {
	domain, err := arg(args, 0, "domain")
	if err != nil {
		return err
	}
	if len(args) < 2 {
		dest, err := store.CatchAllGet(ctx, domain)
		if err != nil {
			return err
		}
		if dest == "" {
			fmt.Println("off")
		} else {
			fmt.Println(dest)
		}
		return nil
	}
	dest := args[1]
	if dest == "off" {
		dest = ""
	}
	if err := store.CatchAllSet(ctx, domain, dest); err != nil {
		return err
	}
	if dest == "" {
		ok("Catch-all turned off for %s", domain)
	} else {
		ok("Mail for nonexistent addresses at %s now goes to %s", domain, dest)
	}
	return nil
}

func cmdAudit(ctx context.Context, log *audit.Store, args []string) error {
	var query, limit string
	parseFlags(args, map[string]*string{"--search": &query, "--limit": &limit}, nil)
	n := 50
	if limit != "" {
		if _, err := fmt.Sscanf(limit, "%d", &n); err != nil || n <= 0 {
			return fmt.Errorf("invalid --limit: %s", limit)
		}
	}
	entries, _, err := log.List(ctx, audit.Filter{Query: query, Limit: n})
	if err != nil {
		return err
	}
	for _, e := range entries {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", e.At.Format("2006-01-02 15:04:05"), e.Actor, e.Action, e.Target, e.Detail)
	}
	return nil
}

// auditCLI records a successful mutating CLI command. Read-only commands aren't
// recorded, and neither are flag values, which can carry passwords.
func auditCLI(ctx context.Context, log *audit.Store, args []string) {
	readOnly := map[string]bool{"list": true, "show": true, "status": true, "dns": true, "audit": true, "-h": true, "--help": true}
	if len(args) == 0 || readOnly[args[0]] {
		return
	}
	parts := []string{args[0]}
	rest := args[1:]
	if args[0] == "api" && len(rest) > 0 {
		parts = append(parts, rest[0])
		rest = rest[1:]
	}
	if len(rest) == 0 || readOnly[rest[0]] {
		return
	}
	if args[0] == "domain" && rest[0] == "catchall" && len(rest) < 3 {
		return // only reading the current setting
	}
	parts = append(parts, rest[0])
	// The target is the first positional argument. Every value-taking flag's value is
	// skipped with it, so e.g. "--password X" can never be recorded as the target.
	boolFlags := map[string]bool{"--force": true, "--owner": true, "--external": true, "--no-external": true, "--no-copy": true}
	target := ""
	tail := rest[1:]
	for i := 0; i < len(tail); i++ {
		a := tail[i]
		if strings.HasPrefix(a, "--") {
			if !strings.Contains(a, "=") && !boolFlags[a] {
				i++
			}
			continue
		}
		target = a
		break
	}
	log.Record(ctx, "cli", strings.Join(parts, "."), target, "", "")
}
