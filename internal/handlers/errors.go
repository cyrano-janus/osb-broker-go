package handlers

import (
	"errors"
	"net/http"

	"github.com/cyrano-janus/osb-broker-go/internal/broker"
	"github.com/cyrano-janus/osb-broker-go/internal/definition"
	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// respondOSBError bildet einen Fehler der Engine auf den OSB-Statuscode ab.
//
// Die Zuordnung haengt am Fehlerwert, nicht mehr am Fehlertext. Vorher
// entschied strings.Contains: jeder Fehler mit "not found" wurde auf einem
// DELETE zu 410 Gone, ein unbekannter Plan also genauso wie eine bereits
// geloeschte Instanz. Die Plattform leitet aus dem Statuscode ihr
// Retry-Verhalten ab - sie darf nicht von einer Wortwahl abhaengen.
func respondOSBError(c *gin.Context, err error) {
	status, name := osbErrorStatus(c.Request.Method, err)
	c.JSON(status, gin.H{"error": name, "description": err.Error()})
}

func osbErrorStatus(method string, err error) (int, string) {
	switch {
	// Katalogfehler des Aufrufers: die Angabe passt nicht zum Katalog. Das
	// ist 400 - auch auf einem DELETE, wo ein 410 behaupten wuerde, die
	// Ressource habe es einmal gegeben.
	case errors.Is(err, definition.ErrServiceUnknown),
		errors.Is(err, definition.ErrPlanUnknown):
		return http.StatusBadRequest, "BadRequest"

	// Der Parameter wurde gefunden, er ist nur nicht erlaubt.
	case errors.Is(err, definition.ErrParameterNotAllowed):
		return http.StatusBadRequest, "BadRequest"

	// Der Datensatz verweist auf ein Objekt, das es nicht mehr gibt. Auf
	// einem DELETE liest die Plattform 410 als "ist schon weg, gut so".
	case errors.Is(err, definition.ErrResourceGone),
		errors.Is(err, broker.ErrNotFound),
		apierrors.IsNotFound(err):
		if method == http.MethodDelete {
			return http.StatusGone, "Gone"
		}
		return http.StatusNotFound, "NotFound"

	// Zwei Schreiber auf demselben Objekt. Die Plattform darf wiederholen.
	case apierrors.IsConflict(err), apierrors.IsAlreadyExists(err):
		return http.StatusConflict, "Conflict"

	// Fehlende RBAC-Rechte sind ein Konfigurationsfehler auf unserer Seite,
	// kein Fehler des Aufrufers. 403 zurueckzugeben wuerde die Plattform
	// glauben lassen, sie selbst sei nicht berechtigt.
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return http.StatusInternalServerError, "InternalServerError"

	default:
		return http.StatusInternalServerError, "InternalServerError"
	}
}
