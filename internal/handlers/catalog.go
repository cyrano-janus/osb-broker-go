package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/cyrano-janus/osb-broker-go/internal/definition"
)

// GetCatalog handles GET /v2/catalog.
//
// Der Katalog ist genau das, was die Engine aus den ServiceDefinitions kennt -
// unveraendert, ohne zweite Uebersetzung. Der Handler hat den Katalog einmal
// nachgebaut, mit fest verdrahtetem `"free": true` und
// `"plan_updateable": true`: der Broker bewarb damit jeden Plan als kostenlos
// und versprach jedem Marktplatz einen Planwechsel, den er fuer keinen
// Operator nachgewiesen hatte. Zwei Quellen fuer dieselbe Aussage laufen
// auseinander, und die Aussage, die nach aussen geht, ist die falsche.
//
// Der Katalog ist auch das Einzige, was ein Marktplatz vom Broker sieht, bevor
// er ihn benutzt: eine Faehigkeit, die hier fehlt, lehnt die Plattform ab,
// bevor der Broker gefragt wird.
func (h *Handlers) GetCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"services": h.definitionServices()})
}

// definitionServices liefert den Katalog der Engine. Nie nil: ein Broker ohne
// Definitionen hat einen leeren Katalog, keinen fehlenden.
func (h *Handlers) definitionServices() []definition.CatalogEntry {
	if h.engine == nil || h.engine.Engine == nil {
		return []definition.CatalogEntry{}
	}
	return h.engine.Engine.Catalog()
}
