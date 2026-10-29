// Admin role changes are restricted to operators with VPS/database access.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"supportcenter/internal/database"
	"time"
)

func main() {
	subject := flag.String("subject", "", "exact Auth0 user ID, e.g. auth0|...")
	role := flag.String("role", "", "user, support, or admin")
	flag.Parse()
	if *subject == "" || *role == "" {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := database.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Database connection failed")
		os.Exit(1)
	}
	defer s.Pool.Close()
	if err = s.SetRole(ctx, *subject, *role); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Role updated.")
}
