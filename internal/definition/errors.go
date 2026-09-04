package definition

import (
	"errors"
	"fmt"
)

// Die Fehler, die die Engine nach aussen gibt, sind Werte und keine Texte.
//
// Vorher entschied die HTTP-Schicht per strings.Contains, was ein Fehler
// bedeutet. Das ging so lange gut, wie sich die Formulierungen nicht aehnelten:
// "plan not found" und "instance not found" trugen beide "not found" und
// wurden auf einem DELETE beide zu 410 Gone - ein unbekannter Plan sah aus wie
// eine bereits geloeschte Instanz. Die Plattform leitet aus dem Statuscode ihr
// Retry-Verhalten ab; sie darf nicht von einer Wortwahl abhaengen.
//
// ErrNotFound bleibt die Oberkategorie, damit ein Aufrufer weiter nach
// "irgendetwas war nicht da" fragen kann. Die spezifischen Werte darunter
// erlauben der HTTP-Schicht, den richtigen Code zu waehlen.
var (
	// ErrNotFound ist die Oberkategorie aller Fehler, bei denen etwas nicht
	// gefunden wurde. errors.Is(ErrServiceUnknown, ErrNotFound) ist wahr.
	ErrNotFound = errors.New("not found")

	// ErrServiceUnknown: fuer diese service_id gibt es keine Definition.
	// Ein Katalogfehler des Aufrufers, also 400.
	ErrServiceUnknown = fmt.Errorf("%w: unknown service", ErrNotFound)

	// ErrPlanUnknown: die plan_id gehoert nicht zu diesem Service. Ebenfalls
	// ein Katalogfehler des Aufrufers, also 400 - und ausdruecklich kein 410,
	// auch nicht auf einem DELETE.
	ErrPlanUnknown = fmt.Errorf("%w: unknown plan", ErrNotFound)

	// ErrResourceGone: der Datensatz verweist auf ein Objekt, das es nicht
	// (mehr) gibt. Auf einem DELETE ist das 410 Gone, sonst 404.
	ErrResourceGone = fmt.Errorf("%w: resource gone", ErrNotFound)

	// ErrParameterNotAllowed: ein Parameter steht nicht in allowedParameters.
	// Bewusst NICHT unter ErrNotFound - der Parameter wurde ja gefunden, er
	// ist nur nicht erlaubt. Frueher lief er unter ErrNotFound und wurde auf
	// einem DELETE zu 410.
	ErrParameterNotAllowed = errors.New("parameter not allowed")
)
