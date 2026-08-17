package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexQFMM2/mhed-platform/api/db/migrations"
	"github.com/AlexQFMM2/mhed-platform/api/internal/auth"
	"github.com/AlexQFMM2/mhed-platform/api/internal/config"
	"github.com/AlexQFMM2/mhed-platform/api/internal/database"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

func main() {
	if len(os.Args) < 2 {
		fatal("usage: mhedctl <migrate|bootstrap-super-admin|create-local-user|import-game-data>")
	}
	switch os.Args[1] {
	case "migrate":
		migrate(os.Args[2:])
	case "bootstrap-super-admin":
		bootstrap(os.Args[2:])
	case "create-local-user":
		createLocalUser(os.Args[2:])
	case "import-game-data":
		importGameData(os.Args[2:])
	default:
		fatal("unknown command: " + os.Args[1])
	}
}

func createLocalUser(arguments []string) {
	flags := flag.NewFlagSet("create-local-user", flag.ExitOnError)
	username := flags.String("username", "plaza_tester", "ASCII login username")
	passwordFile := flags.String("password-file", "./test-user-password", "one-time local credential file")
	_ = flags.Parse(arguments)
	if !validUsername(*username) {
		fatal("username must match [A-Za-z0-9_]{3,32}")
	}
	cfg, err := config.Load()
	if err != nil {
		fatal(err.Error())
	}
	pool, err := database.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM users WHERE lower(username)=lower($1))`, *username).Scan(&exists); err != nil {
		fatal("query user: " + err.Error())
	}
	if exists {
		fmt.Fprintf(os.Stdout, "local user %s already exists; credentials unchanged\n", *username)
		return
	}
	password, err := auth.RandomSecret(24)
	if err != nil {
		fatal("generate password: " + err.Error())
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		fatal("hash password: " + err.Error())
	}
	absPath, err := filepath.Abs(*passwordFile)
	if err != nil {
		fatal(err.Error())
	}
	credential := fmt.Sprintf("username=%s\npassword=%s\ncreated_at=%s\nmust_change_password=true\nroles=\n",
		*username, password, time.Now().UTC().Format(time.RFC3339))
	file, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fatal("create password file: " + err.Error())
	}
	if _, err = file.WriteString(credential); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		_ = os.Remove(absPath)
		fatal("write password file: " + err.Error())
	}
	if _, err = pool.Exec(context.Background(), `INSERT INTO users(username,password_hash,must_change_password) VALUES($1,$2,true)`, *username, hash); err != nil {
		_ = os.Remove(absPath)
		fatal("create local user: " + err.Error())
	}
	fmt.Fprintf(os.Stdout, "created local user %s without roles; credentials written to %s\n", *username, absPath)
}

func migrate(arguments []string) {
	flags := flag.NewFlagSet("migrate", flag.ExitOnError)
	_ = flags.Parse(arguments)
	action := "up"
	if flags.NArg() > 0 {
		action = flags.Arg(0)
	}
	cfg, err := config.Load()
	if err != nil {
		fatal(err.Error())
	}
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer db.Close()
	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("postgres"); err != nil {
		fatal(err.Error())
	}
	switch action {
	case "up":
		err = goose.Up(db, ".")
	case "down":
		err = goose.Down(db, ".")
	case "status":
		err = goose.Status(db, ".")
	default:
		fatal("migrate action must be up, down, or status")
	}
	if err != nil {
		fatal(err.Error())
	}
}

func bootstrap(arguments []string) {
	flags := flag.NewFlagSet("bootstrap-super-admin", flag.ExitOnError)
	username := flags.String("username", "alex_admin", "ASCII login username")
	passwordFile := flags.String("password-file", "./test-password", "one-time local credential file")
	_ = flags.Parse(arguments)
	if !validUsername(*username) {
		fatal("username must match [A-Za-z0-9_]{3,32}")
	}
	cfg, err := config.Load()
	if err != nil {
		fatal(err.Error())
	}
	pool, err := database.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fatal(err.Error())
	}
	defer pool.Close()
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM users WHERE lower(username)=lower($1))`, *username).Scan(&exists); err != nil {
		fatal("query user: " + err.Error())
	}
	if exists {
		fmt.Fprintf(os.Stdout, "super administrator %s already exists; credentials unchanged\n", *username)
		return
	}
	password, err := auth.RandomSecret(24)
	if err != nil {
		fatal("generate password: " + err.Error())
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		fatal("hash password: " + err.Error())
	}
	absPath, err := filepath.Abs(*passwordFile)
	if err != nil {
		fatal(err.Error())
	}
	credential := fmt.Sprintf("username=%s\npassword=%s\ncreated_at=%s\nmust_change_password=true\n",
		*username, password, time.Now().UTC().Format(time.RFC3339))
	file, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		fatal("create password file: " + err.Error())
	}
	if _, err = file.WriteString(credential); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		_ = os.Remove(absPath)
		fatal("write password file: " + err.Error())
	}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		_ = os.Remove(absPath)
		fatal(err.Error())
	}
	defer tx.Rollback(context.Background())
	var userID string
	err = tx.QueryRow(context.Background(), `INSERT INTO users(username,password_hash,must_change_password)
		VALUES($1,$2,true) RETURNING id`, *username, hash).Scan(&userID)
	if err == nil {
		_, err = tx.Exec(context.Background(), `INSERT INTO user_roles(user_id,role_id)
			SELECT $1,id FROM roles WHERE key='super_admin'`, userID)
	}
	if err == nil {
		_, err = tx.Exec(context.Background(), `INSERT INTO admin_audit_logs(actor_user_id,action,target_type,target_id,metadata)
			VALUES($1::uuid,'super_admin.bootstrap','user',($1::uuid)::text,jsonb_build_object('username',$2::text))`, userID, *username)
	}
	if err == nil {
		err = tx.Commit(context.Background())
	}
	if err != nil {
		_ = os.Remove(absPath)
		fatal("bootstrap super administrator: " + err.Error())
	}
	fmt.Fprintf(os.Stdout, "created super administrator %s; credentials written to %s\n", *username, absPath)
}

func validUsername(value string) bool {
	if len(value) < 3 || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if !((character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '_') {
			return false
		}
	}
	return true
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func importGameData(arguments []string) {
	flags := flag.NewFlagSet("import-game-data", flag.ExitOnError)
	source := flags.String("source", "../mh3u-se/data/mh3g.sqlite", "source SQLite database")
	manifest := flags.String("manifest", "../mh3u-se/data/manifest.json", "source manifest")
	destination := flags.String("destination", "./game-data/runtime", "runtime data directory")
	_ = flags.Parse(arguments)
	if err := copyVerifiedGameData(*source, *manifest, *destination); err != nil {
		fatal(err.Error())
	}
	fmt.Fprintf(os.Stdout, "verified MH3G game data imported into %s\n", *destination)
}

var errInvalidManifest = errors.New("manifest does not match the MH3G database")

func normalizePath(value string) string { return strings.TrimSpace(value) }
