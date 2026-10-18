package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	fmt.Print("Password to hash (input hidden): ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not read password:", err)
		os.Exit(1)
	}
	if len(password) == 0 {
		fmt.Fprintln(os.Stderr, "Password cannot be empty.")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not hash password:", err)
		os.Exit(1)
	}

	fmt.Printf("bcrypt hash (paste into the SQL insert):\n%s\n", hash)
}
