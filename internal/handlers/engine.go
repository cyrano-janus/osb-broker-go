package handlers

import (
	"context"
	"log"
	"os"

	"github.com/example/osb-broker/internal/broker"
	"github.com/example/osb-broker/internal/definition"
	"github.com/example/osb-broker/internal/store"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// EngineHolder verdrahtet die Generic Engine (Phase 2) mit dem Handler-Layer.
// Wenn DEFINITIONS_DIR gesetzt ist, werden ServiceDefinitions geladen und der
// Katalog um definitionsbasierte Services erweitert; Provision/Bind dieser
// Services laufen über den OperatorClient statt über die Fake-Logik.
type EngineHolder struct {
	Engine *definition.Engine
	Op     *definition.OperatorClient
}

// NewEngineHolder lädt Definitionen aus dir und baut den Operator-Client.
// dir == "" ergibt eine Engine ohne Definitionen.
func NewEngineHolder(dir, brokerNamespace string, stateStore broker.StateStore) (*EngineHolder, error) {
	defs, err := definition.LoadFromDir(dir)
	if err != nil {
		return nil, err
	}
	if len(defs) == 0 {
		log.Printf("No service definitions loaded from %q (definition-based services disabled)", dir)
		return &EngineHolder{}, nil
	}

	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	k8sClient, err := broker.NewK8sClient(cfg)
	if err != nil {
		return nil, err
	}
	op := broker.NewOperatorClient(k8sClient)
	engine := definition.NewEngine(op, defs...)
	if stateStore != nil {
		engine.SetInstanceRegistry(&stateStoreRegistry{store: stateStore})
	}
	log.Printf("Loaded %d service definition(s) from %q", len(defs), dir)
	return &EngineHolder{Engine: engine, Op: op}, nil
}

var _ = store.NewInMemoryStore // reference to keep import stable if wiring changes
var _ = context.Background
var _ = os.Getenv
