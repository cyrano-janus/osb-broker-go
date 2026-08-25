// Package main implements the osb-checker conformance runner.
package main

import (
	"flag"
	"fmt"
	"os"

	checker "github.com/example/osb-broker/cmd/osb-checker/checks"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "broker base URL")
	user := flag.String("user", "", "basic auth user")
	pass := flag.String("pass", "", "basic auth password")
	prefix := flag.String("id-prefix", "conformance", "instance/binding id prefix")
	flag.Parse()

	cfg := checker.Config{
		BaseURL: *url, User: *user, Pass: *pass,
		IDPrefix: *prefix,
	}
	failures := checker.Run(cfg)
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "osb-checker: %d failure(s)\n", failures)
		os.Exit(1)
	}
}
