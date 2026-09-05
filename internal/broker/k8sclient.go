package broker

import (
	"errors"

	osbv1 "github.com/cyrano-janus/osb-broker-go/internal/apis/v1alpha1"
	"github.com/cyrano-janus/osb-broker-go/internal/definition"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrNotFound is returned by Get/Delete lookups when the entity does not
// exist in the store.
var ErrNotFound = errors.New("not found")

// NewK8sClient builds a controller-runtime client from the given rest config.
//
// Neben den eingebauten Typen sind die Broker-CRDs registriert; ohne das
// scheitert jeder Zugriff auf den State Store erst zur Laufzeit mit
// "no kind is registered".
func NewK8sClient(cfg *rest.Config) (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := osbv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}

// NewOperatorClient builds an OperatorClient from a controller-runtime client.
func NewOperatorClient(k8sClient client.Client) *definition.OperatorClient {
	return &definition.OperatorClient{Client: k8sClient}
}
