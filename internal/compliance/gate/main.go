package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/0xfe10/aicli/internal/compliance"
)

func main() {
	findingsPath := flag.String("findings", "", "path to govulncheck -json output")
	allowlistPath := flag.String("allowlist", "", "path to govulncheck allowlist markdown")
	flag.Parse()
	if strings.TrimSpace(*findingsPath) == "" || strings.TrimSpace(*allowlistPath) == "" {
		fmt.Fprintln(os.Stderr, "usage: gate -findings FILE -allowlist FILE")
		os.Exit(2)
	}
	findingsFile, err := os.Open(*findingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open findings: %v\n", err)
		os.Exit(1)
	}
	defer findingsFile.Close()
	found, err := compliance.CollectFindingIDs(findingsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	allowed, err := compliance.LoadAllowlistIDs(*allowlistPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load allowlist: %v\n", err)
		os.Exit(1)
	}
	if bad := compliance.UnapprovedFindings(found, allowed); len(bad) > 0 {
		fmt.Fprintf(os.Stderr, "unapproved govulncheck findings: %s\n", strings.Join(bad, ", "))
		os.Exit(1)
	}
	fmt.Printf("govulncheck gate passed (%d findings, %d allowed)\n", len(found), len(allowed))
}
