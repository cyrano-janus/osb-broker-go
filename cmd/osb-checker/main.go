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
	caCert := flag.String("ca-cert", "", "PEM CA bundle used to verify the broker certificate")
	clientCert := flag.String("client-cert", "", "client certificate for mTLS")
	clientKey := flag.String("client-key", "", "client private key for mTLS")
	insecure := flag.Bool("insecure", false, "skip broker certificate verification (testing only)")
	flag.Parse()

	if *insecure {
		fmt.Fprintln(os.Stderr, "osb-checker: WARNING --insecure disables broker certificate verification; never use this in CI")
	}

	cfg := checker.Config{
		BaseURL: *url, User: *user, Pass: *pass,
		IDPrefix:   *prefix,
		CACert:     *caCert,
		ClientCert: *clientCert,
		ClientKey:  *clientKey,
		Insecure:   *insecure,
	}
	failures := checker.Run(cfg)
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "osb-checker: %d failure(s)\n", failures)
		os.Exit(1)
	}
}
