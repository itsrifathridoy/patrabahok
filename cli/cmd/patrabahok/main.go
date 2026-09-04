// patrabahok is the mail server management CLI: domains, mailboxes, aliases, DKIM/DNS
// record lookup, mail queue, installer status, and local API token management. Talks
// directly to the database (parameterized queries via internal/mailbox), the same
// business logic patrabahokd exposes over the API.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/itsrifathridoy/patrabahok/cli/internal/adminauth"
	"github.com/itsrifathridoy/patrabahok/cli/internal/authtoken"
	"github.com/itsrifathridoy/patrabahok/cli/internal/db"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "\033[0;31m[FAIL]\033[0m %v\n", err)
		os.Exit(1)
	}
}

func ok(format string, args ...any) {
	fmt.Printf("\033[0;32m[ OK ]\033[0m "+format+"\n", args...)
}

func usage() {
	fmt.Print(`patrabahok — mail server management CLI

Usage:
  patrabahok domain add <domain>
  patrabahok domain list
  patrabahok domain remove <domain> [--force]

  patrabahok mailbox add <user@domain> [--quota 500M] [--password PASS]
  patrabahok mailbox list [domain]
  patrabahok mailbox remove <user@domain> [--force]
  patrabahok mailbox passwd <user@domain> [--password PASS]
  patrabahok mailbox quota <user@domain> <quota>    # e.g. 2G, 500M

  patrabahok alias add <alias@domain> <target@domain>
  patrabahok alias list [domain]
  patrabahok alias remove <alias@domain> <target@domain>

  patrabahok dkim show <domain>
  patrabahok dns show <domain>

  patrabahok queue list
  patrabahok queue flush

  patrabahok status

  patrabahok api token create <name> [--scope domain,mailbox,...]
  patrabahok api token list
  patrabahok api token revoke <name>

  patrabahok webadmin add <username> [--password PASS]
  patrabahok webadmin list
  patrabahok webadmin remove <username>
`)
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("patrabahok must be run as root (it manages mail data, DB credentials, and system files)")
	}
	return nil
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	if err := requireRoot(); err != nil {
		return err
	}

	conn, err := db.Open("")
	if err != nil {
		return err
	}
	defer conn.Close()
	store := mailbox.NewStore(conn)
	tokens := authtoken.NewStore(conn)
	webAdmins := adminauth.NewStore(conn)
	ctx := context.Background()

	group, rest := args[0], args[1:]
	switch group {
	case "domain":
		return cmdDomain(ctx, store, rest)
	case "mailbox":
		return cmdMailbox(ctx, store, rest)
	case "alias":
		return cmdAlias(ctx, store, rest)
	case "dkim":
		return cmdDKIM(rest)
	case "dns":
		return cmdDNS(rest)
	case "queue":
		return cmdQueue(rest)
	case "status":
		return cmdStatus()
	case "api":
		return cmdAPI(ctx, tokens, rest)
	case "webadmin":
		return cmdWebAdmin(ctx, webAdmins, rest)
	case "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", group)
	}
}

func arg(args []string, i int, what string) (string, error) {
	if i >= len(args) {
		return "", fmt.Errorf("%s required", what)
	}
	return args[i], nil
}

// parseFlags does minimal manual flag extraction (--name value / --name=value and a
// couple of boolean flags), returning the remaining positional args.
func parseFlags(args []string, stringFlags map[string]*string, boolFlags map[string]*bool) []string {
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if dst, isBool := boolFlags[a]; isBool {
			*dst = true
			continue
		}
		if strings.Contains(a, "=") && strings.HasPrefix(a, "--") {
			parts := strings.SplitN(a, "=", 2)
			if dst, ok := stringFlags[parts[0]]; ok {
				*dst = parts[1]
				continue
			}
		}
		if dst, ok := stringFlags[a]; ok && i+1 < len(args) {
			i++
			*dst = args[i]
			continue
		}
		positional = append(positional, a)
	}
	return positional
}

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line) != ""
}

func cmdDomain(ctx context.Context, store *mailbox.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: patrabahok domain <add|list|remove> ...")
	}
	action, rest := args[0], args[1:]
	switch action {
	case "add":
		name, err := arg(rest, 0, "domain")
		if err != nil {
			return err
		}
		if err := store.DomainAdd(ctx, name); err != nil {
			return err
		}
		ok("Domain added: %s", name)
		return nil
	case "list":
		list, err := store.DomainList(ctx)
		if err != nil {
			return err
		}
		for _, d := range list {
			fmt.Println(d.Name)
		}
		return nil
	case "remove":
		var force bool
		positional := parseFlags(rest, nil, map[string]*bool{"--force": &force})
		name, err := arg(positional, 0, "domain")
		if err != nil {
			return err
		}
		if !force {
			var confirmName string
			fmt.Fprintf(os.Stderr, "This deletes all mailboxes/aliases for %s. Type the domain name to confirm: ", name)
			reader := bufio.NewReader(os.Stdin)
			line, _ := reader.ReadString('\n')
			confirmName = strings.TrimSpace(line)
			if confirmName != name {
				return errors.New("confirmation did not match, aborted")
			}
		}
		if err := store.DomainRemove(ctx, name); err != nil {
			return err
		}
		ok("Domain removed: %s", name)
		return nil
	default:
		return fmt.Errorf("unknown domain action: %s", action)
	}
}

func cmdMailbox(ctx context.Context, store *mailbox.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: patrabahok mailbox <add|list|remove|passwd> ...")
	}
	action, rest := args[0], args[1:]
	switch action {
	case "add":
		var quota, password string
		positional := parseFlags(rest, map[string]*string{"--quota": &quota, "--password": &password}, nil)
		email, err := arg(positional, 0, "user@domain")
		if err != nil {
			return err
		}
		if password == "" {
			p1, err := readPassword(fmt.Sprintf("Password for %s: ", email))
			if err != nil {
				return err
			}
			p2, err := readPassword("Confirm password: ")
			if err != nil {
				return err
			}
			if p1 != p2 {
				return errors.New("passwords did not match")
			}
			password = p1
		}
		if password == "" {
			return errors.New("password cannot be empty")
		}
		quotaBytes, err := mailbox.ParseQuota(quota)
		if err != nil {
			return err
		}
		if err := store.MailboxAdd(ctx, email, password, quotaBytes); err != nil {
			return err
		}
		ok("Mailbox ready: %s (quota: %d bytes)", email, quotaBytes)
		return nil
	case "list":
		domain := ""
		if len(rest) > 0 {
			domain = rest[0]
		}
		list, err := store.MailboxList(ctx, domain)
		if err != nil {
			return err
		}
		for _, m := range list {
			fmt.Printf("%s\tenabled=%v\tquota_bytes=%d\n", m.Email, m.Enabled, m.QuotaBytes)
		}
		return nil
	case "remove":
		var force bool
		positional := parseFlags(rest, nil, map[string]*bool{"--force": &force})
		email, err := arg(positional, 0, "user@domain")
		if err != nil {
			return err
		}
		if !force && !confirm(fmt.Sprintf("Delete mailbox %s and its mail data? Type the address to confirm:", email)) {
			return errors.New("aborted")
		}
		if err := store.MailboxRemove(ctx, email); err != nil {
			return err
		}
		ok("Mailbox removed: %s", email)
		return nil
	case "passwd":
		var password string
		positional := parseFlags(rest, map[string]*string{"--password": &password}, nil)
		email, err := arg(positional, 0, "user@domain")
		if err != nil {
			return err
		}
		if password == "" {
			p1, err := readPassword(fmt.Sprintf("New password for %s: ", email))
			if err != nil {
				return err
			}
			p2, err := readPassword("Confirm password: ")
			if err != nil {
				return err
			}
			if p1 != p2 {
				return errors.New("passwords did not match")
			}
			password = p1
		}
		if err := store.MailboxPasswd(ctx, email, password); err != nil {
			return err
		}
		ok("Password updated for %s", email)
		return nil
	case "quota":
		email, err := arg(rest, 0, "user@domain")
		if err != nil {
			return err
		}
		quotaStr, err := arg(rest, 1, "quota (e.g. 2G)")
		if err != nil {
			return err
		}
		quotaBytes, err := mailbox.ParseQuota(quotaStr)
		if err != nil {
			return err
		}
		if err := store.MailboxSetQuota(ctx, email, quotaBytes); err != nil {
			return err
		}
		ok("Quota for %s set to %d bytes", email, quotaBytes)
		return nil
	default:
		return fmt.Errorf("unknown mailbox action: %s", action)
	}
}

func cmdAlias(ctx context.Context, store *mailbox.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: patrabahok alias <add|list|remove> ...")
	}
	action, rest := args[0], args[1:]
	switch action {
	case "add":
		source, err := arg(rest, 0, "alias@domain")
		if err != nil {
			return err
		}
		dest, err := arg(rest, 1, "target@domain")
		if err != nil {
			return err
		}
		if err := store.AliasAdd(ctx, source, dest); err != nil {
			return err
		}
		ok("Alias added: %s -> %s", source, dest)
		return nil
	case "list":
		domain := ""
		if len(rest) > 0 {
			domain = rest[0]
		}
		list, err := store.AliasList(ctx, domain)
		if err != nil {
			return err
		}
		for _, a := range list {
			fmt.Printf("%s -> %s\n", a.Source, a.Destination)
		}
		return nil
	case "remove":
		source, err := arg(rest, 0, "alias@domain")
		if err != nil {
			return err
		}
		dest, err := arg(rest, 1, "target@domain")
		if err != nil {
			return err
		}
		if err := store.AliasRemove(ctx, source, dest); err != nil {
			return err
		}
		ok("Alias removed: %s -> %s", source, dest)
		return nil
	default:
		return fmt.Errorf("unknown alias action: %s", action)
	}
}

func cmdDKIM(args []string) error {
	if len(args) < 2 || args[0] != "show" {
		return errors.New("usage: patrabahok dkim show <domain>")
	}
	rec, err := sysinfo.DKIMRecord(args[1])
	if err != nil {
		return err
	}
	fmt.Print(rec)
	return nil
}

func cmdDNS(args []string) error {
	if len(args) < 2 || args[0] != "show" {
		return errors.New("usage: patrabahok dns show <domain>")
	}
	rec, err := sysinfo.DNSRecords(args[1])
	if err != nil {
		return err
	}
	fmt.Print(rec)
	return nil
}

func cmdQueue(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: patrabahok queue <list|flush>")
	}
	switch args[0] {
	case "list":
		out, err := sysinfo.QueueList()
		fmt.Print(out)
		return err
	case "flush":
		out, err := sysinfo.QueueFlush()
		fmt.Print(out)
		if err != nil {
			return err
		}
		ok("Queue flush requested.")
		return nil
	default:
		return fmt.Errorf("unknown queue action: %s", args[0])
	}
}

func cmdStatus() error {
	st, err := sysinfo.InstallerState()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(st)
}

func cmdAPI(ctx context.Context, tokens *authtoken.Store, args []string) error {
	if len(args) < 1 || args[0] != "token" {
		return errors.New("usage: patrabahok api token <create|list|revoke> ...")
	}
	rest := args[1:]
	if len(rest) == 0 {
		return errors.New("usage: patrabahok api token <create|list|revoke> ...")
	}
	action, rest2 := rest[0], rest[1:]
	switch action {
	case "create":
		var scope string
		positional := parseFlags(rest2, map[string]*string{"--scope": &scope}, nil)
		name, err := arg(positional, 0, "token name")
		if err != nil {
			return err
		}
		var scopes []string
		if scope != "" {
			scopes = strings.Split(scope, ",")
		}
		plaintext, err := tokens.Create(ctx, name, scopes)
		if err != nil {
			return err
		}
		fmt.Println(plaintext)
		fmt.Fprintln(os.Stderr, "^ this token is shown once — store it now, it cannot be recovered.")
		return nil
	case "list":
		list, err := tokens.List(ctx)
		if err != nil {
			return err
		}
		for _, t := range list {
			last := "never"
			if t.LastUsedAt != nil {
				last = t.LastUsedAt.Format("2006-01-02T15:04:05Z")
			}
			fmt.Printf("%s\tscopes=%s\tcreated=%s\tlast_used=%s\n",
				t.Name, strings.Join(t.Scopes, ","), t.CreatedAt.Format("2006-01-02T15:04:05Z"), last)
		}
		return nil
	case "revoke":
		name, err := arg(rest2, 0, "token name")
		if err != nil {
			return err
		}
		if err := tokens.Revoke(ctx, name); err != nil {
			return err
		}
		ok("Token revoked: %s", name)
		return nil
	default:
		return fmt.Errorf("unknown api token action: %s", action)
	}
}

func cmdWebAdmin(ctx context.Context, admins *adminauth.Store, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: patrabahok webadmin <add|list|remove> ...")
	}
	action, rest := args[0], args[1:]
	switch action {
	case "add":
		var password string
		positional := parseFlags(rest, map[string]*string{"--password": &password}, nil)
		username, err := arg(positional, 0, "username")
		if err != nil {
			return err
		}
		if password == "" {
			p1, err := readPassword(fmt.Sprintf("Password for dashboard user %s: ", username))
			if err != nil {
				return err
			}
			p2, err := readPassword("Confirm password: ")
			if err != nil {
				return err
			}
			if p1 != p2 {
				return errors.New("passwords did not match")
			}
			password = p1
		}
		if len(password) < 8 {
			return errors.New("password must be at least 8 characters")
		}
		if err := admins.CreateUser(ctx, username, password); err != nil {
			return err
		}
		ok("Dashboard admin created: %s", username)
		return nil
	case "list":
		list, err := admins.ListUsers(ctx)
		if err != nil {
			return err
		}
		for _, u := range list {
			fmt.Println(u.Username)
		}
		return nil
	case "remove":
		username, err := arg(rest, 0, "username")
		if err != nil {
			return err
		}
		if err := admins.DeleteUser(ctx, username); err != nil {
			return err
		}
		ok("Dashboard admin removed: %s", username)
		return nil
	default:
		return fmt.Errorf("unknown webadmin action: %s", action)
	}
}
