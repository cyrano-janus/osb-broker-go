// Command osb-gate ist das blockierende Konformitaets-Gate dieses Repos: es
// prueft den hier gebauten Broker gegen OSB 2.17 und laesst den Build nur
// durch, wenn keine Pruefung anschlaegt.
//
// Es ist bewusst nicht dasselbe wie die Zweitmeinung. Zur Abgrenzung:
//
//	osb-gate                          dieses Repo, blockiert den Build,
//	                                  prueft bei jedem Push DIESEN Broker
//	github.com/cyrano-janus/osb-checker  eigenes Repo, oeffentlich, MIT,
//	                                  prueft JEDEN OSB-Broker
//
// Beide blockieren in der CI. Widersprechen sie sich, gewinnt die
// Spezifikation, nicht das Werkzeug - und eine Zusammenfuehrung ist
// ausdruecklich nicht gewollt: zwei unabhaengig geschriebene Pruefer finden
// zusammen mehr als einer, der sich selbst bestaetigt.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/example/osb-broker/cmd/osb-gate/checks"
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
	serviceID := flag.String("service-id", "", "OSB service_id to audit; empty = first non-demo service in the catalog")
	planID := flag.String("plan-id", "", "OSB plan_id to audit; empty = first plan of the chosen service")
	timeout := flag.Int64("timeout", 0, "per-request timeout in seconds (0 = 30s)")
	asyncTimeout := flag.Int64("async-timeout", 0, "deadline for an asynchronous operation in seconds (0 = 240s)")
	flag.Parse()

	if *insecure {
		fmt.Fprintln(os.Stderr, "osb-gate: WARNING --insecure disables broker certificate verification; never use this in CI")
	}

	cfg := checks.Config{
		BaseURL: *url, User: *user, Pass: *pass,
		IDPrefix:     *prefix,
		CACert:       *caCert,
		ClientCert:   *clientCert,
		ClientKey:    *clientKey,
		Insecure:     *insecure,
		ServiceID:    *serviceID,
		PlanID:       *planID,
		Timeout:      *timeout,
		AsyncTimeout: *asyncTimeout,
	}
	failures := checks.Run(cfg)
	if failures > 0 {
		fmt.Fprintf(os.Stderr, "osb-gate: %d failure(s)\n", failures)
		os.Exit(1)
	}
}
