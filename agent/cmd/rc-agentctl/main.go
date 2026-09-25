// Command rc-agentctl is a local admin CLI for managing accounts directly
// against rc-agentd's SQLite database. It's meant to be run on the Fedora
// machine itself (e.g. via `sudo rc-agentctl ...`) -- most importantly to
// bootstrap the very first "Vollzugriff" account, since the web portal has
// no self-registration by design.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/term"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/store"
)

func main() {
	dataDir := flag.String("data-dir", "/var/lib/rc-agent", "directory containing agent.db (must match rc-agentd's -data-dir)")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	st, err := store.Open(filepath.Join(*dataDir, "agent.db"))
	if err != nil {
		fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()

	switch args[0] {
	case "create-user":
		cmdCreateUser(ctx, st, args[1:])
	case "list-users":
		cmdListUsers(ctx, st)
	case "delete-user":
		cmdDeleteUser(ctx, st, args[1:])
	case "set-password":
		cmdSetPassword(ctx, st, args[1:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `rc-agentctl [-data-dir DIR] <command> [args]

Commands:
  create-user -username NAME -role full|viewer   Create an account (prompts for password)
  list-users                                      List all accounts
  delete-user -id ID                              Delete an account
  set-password -username NAME                     Change an account's password (prompts)`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func cmdCreateUser(ctx context.Context, st *store.Store, args []string) {
	fs := flag.NewFlagSet("create-user", flag.ExitOnError)
	username := fs.String("username", "", "account username")
	role := fs.String("role", "", `"full" (Vollzugriff) or "viewer" (Betrachter)`)
	fs.Parse(args)

	if *username == "" || (*role != string(model.RoleFull) && *role != string(model.RoleViewer)) {
		fatalf(`usage: create-user -username NAME -role full|viewer`)
	}

	pw := readPasswordTwice()
	u, err := st.CreateUser(ctx, *username, pw, model.Role(*role))
	if err != nil {
		fatalf("create user: %v", err)
	}
	fmt.Printf("Account %q (id=%d, role=%s) angelegt.\n", u.Username, u.ID, u.Role)
}

func cmdListUsers(ctx context.Context, st *store.Store) {
	users, err := st.ListUsers(ctx)
	if err != nil {
		fatalf("list users: %v", err)
	}
	if len(users) == 0 {
		fmt.Println("(keine Accounts)")
		return
	}
	for _, u := range users {
		fmt.Printf("%-4d %-20s %-8s %s\n", u.ID, u.Username, u.Role, u.CreatedAt.Format("2006-01-02 15:04"))
	}
}

func cmdDeleteUser(ctx context.Context, st *store.Store, args []string) {
	fs := flag.NewFlagSet("delete-user", flag.ExitOnError)
	id := fs.Int64("id", 0, "account id (see list-users)")
	fs.Parse(args)
	if *id == 0 {
		fatalf("usage: delete-user -id ID")
	}
	if err := st.DeleteUser(ctx, *id); err != nil {
		fatalf("delete user: %v", err)
	}
	fmt.Println("gelöscht.")
}

func cmdSetPassword(ctx context.Context, st *store.Store, args []string) {
	fs := flag.NewFlagSet("set-password", flag.ExitOnError)
	username := fs.String("username", "", "account username")
	fs.Parse(args)
	if *username == "" {
		fatalf("usage: set-password -username NAME")
	}
	u, err := st.GetUserByUsername(ctx, *username)
	if err != nil {
		fatalf("lookup user: %v", err)
	}
	pw := readPasswordTwice()
	if _, err := st.SetPassword(ctx, u.ID, pw); err != nil {
		fatalf("set password: %v", err)
	}
	fmt.Println("Passwort aktualisiert.")
}

func readPasswordTwice() string {
	fmt.Fprint(os.Stderr, "Passwort: ")
	pw1, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fatalf("read password: %v", err)
	}
	fmt.Fprint(os.Stderr, "Passwort (wiederholen): ")
	pw2, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fatalf("read password: %v", err)
	}
	if string(pw1) != string(pw2) {
		fatalf("Passwörter stimmen nicht überein.")
	}
	if len(pw1) < 8 {
		fatalf("Passwort muss mindestens 8 Zeichen haben.")
	}
	return string(pw1)
}
