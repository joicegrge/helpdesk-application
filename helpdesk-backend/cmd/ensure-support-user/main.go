package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const (
	supportName  = "Helpdesk Support"
	supportEmail = "support@helpdesk.local"
)

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fatal("Could not load backend .env", err)
	}
	fmt.Printf("Set password for %s (input hidden): ", supportEmail)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		fatal("Could not read password", err)
	}
	if len(password) == 0 {
		fatal("Password cannot be empty", nil)
	}
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		fatal("Could not create bcrypt hash", err)
	}

	cfg := mysql.Config{
		User: env("MYSQL_USER", "root"), Passwd: os.Getenv("MYSQL_PASSWORD"),
		AllowNativePasswords: true, Net: "tcp",
		Addr:   net.JoinHostPort(env("MYSQL_HOST", "127.0.0.1"), env("MYSQL_PORT", "3306")),
		DBName: env("MYSQL_DATABASE", "helpdesk_db"), Params: map[string]string{"charset": "utf8mb4"},
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		fatal("Could not configure MariaDB connection", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fatal("Could not connect to configured MariaDB", err)
	}

	var id int64
	var role, existingHash string
	err = db.QueryRowContext(ctx, "SELECT id, role, `password` FROM users WHERE email = ?", supportEmail).Scan(&id, &role, &existingHash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		result, insertErr := db.ExecContext(ctx, "INSERT INTO users (full_name, email, `password`, role) VALUES (?, ?, ?, 'SUPPORT')", supportName, supportEmail, string(hash))
		if insertErr != nil {
			fatal("Could not create support account", insertErr)
		}
		id, _ = result.LastInsertId()
		fmt.Printf("Created SUPPORT account %s (user ID %d) with a bcrypt password hash.\n", supportEmail, id)
	case err != nil:
		fatal("Could not check for existing support account", err)
	case role != "SUPPORT":
		fmt.Printf("Refusing to change %s: it already exists with role %s (user ID %d). No account was modified.\n", supportEmail, role, id)
		os.Exit(1)
	case bcrypt.CompareHashAndPassword([]byte(existingHash), password) == nil:
		fmt.Printf("SUPPORT account %s (user ID %d) already exists and the supplied password is valid; no change was needed.\n", supportEmail, id)
	default:
		if _, err := db.ExecContext(ctx, "UPDATE users SET `password` = ? WHERE id = ? AND role = 'SUPPORT'", string(hash), id); err != nil {
			fatal("Could not update existing support account password", err)
		}
		fmt.Printf("Updated bcrypt password for existing SUPPORT account %s (user ID %d).\n", supportEmail, id)
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func fatal(message string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", message, err)
	} else {
		fmt.Fprintln(os.Stderr, message)
	}
	os.Exit(1)
}
