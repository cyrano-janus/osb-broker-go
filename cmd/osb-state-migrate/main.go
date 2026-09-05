// Command osb-state-migrate uebertraegt den Broker-Zustand aus der
// abgeloesten State-ConfigMap in die CRDs aus Phase 5.
//
// Der Umstieg ist ein harter Schnitt: der Broker liest die ConfigMap nicht
// mehr. Wer bestehende Instanzen hat und diesen Schritt auslaesst, hat sie
// danach fuer den Broker verloren - Cloud Foundry wuesste noch von ihnen, und
// die angelegten Datenbanken blieben als Waisen im Cluster stehen.
//
//	osb-state-migrate --namespace osb --dry-run
//	osb-state-migrate --namespace osb
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/migrate"
	k8sconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	namespace := flag.String("namespace", "", "Namespace der alten State-ConfigMap (Pflicht)")
	configMap := flag.String("configmap", migrate.DefaultConfigMapName, "Name der alten State-ConfigMap")
	dryRun := flag.Bool("dry-run", false, "nur zaehlen, nichts schreiben")
	flag.Parse()

	if *namespace == "" {
		fmt.Fprintln(os.Stderr, "osb-state-migrate: --namespace fehlt")
		flag.Usage()
		os.Exit(2)
	}

	cfg, err := k8sconfig.GetConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "osb-state-migrate: kubeconfig: %v\n", err)
		os.Exit(1)
	}
	c, err := broker.NewK8sClient(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "osb-state-migrate: Client: %v\n", err)
		os.Exit(1)
	}

	report, err := migrate.Run(context.Background(), c, *namespace, *configMap, *dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "osb-state-migrate: %v\n", err)
		os.Exit(1)
	}

	switch {
	case report.SourceMissing:
		fmt.Printf("Keine alte ConfigMap %s/%s gefunden - nichts zu tun.\n", *namespace, *configMap)
	case report.DryRun:
		fmt.Printf("Trockenlauf: %d Instanzen und %d Bindings wuerden uebertragen.\n",
			report.Instances, report.Bindings)
	default:
		fmt.Printf("%d Instanzen und %d Bindings uebertragen.\n", report.Instances, report.Bindings)
		fmt.Printf("Die ConfigMap %s/%s steht weiterhin - sie ist die Rueckfallebene.\n",
			*namespace, *configMap)
		fmt.Println("Nach einer erfolgreichen Pruefung loeschen:")
		fmt.Printf("  kubectl -n %s delete configmap %s\n", *namespace, *configMap)
	}
}
