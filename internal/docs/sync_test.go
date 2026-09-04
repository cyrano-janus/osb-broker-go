package docs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Die Dokumentation liegt zweisprachig vor: docs/de/ ist die fuehrende Fassung,
// docs/en/ zieht nach. Zwei vollstaendige Baeume driften auseinander, sobald
// jemand nur einen anfasst - und niemand merkt es, weil beide fuer sich
// gelesen stimmig bleiben.
//
// Dieser Waechter vergleicht keine Uebersetzungen. Er prueft nur die Struktur:
// dieselben Dateien, dieselbe Gliederung, keine toten Verweise, und in jedem
// Dokument ein Link auf die Gegensprache. Das faengt den haeufigen Fall - ein
// neues Kapitel nur auf Deutsch geschrieben - und laesst den seltenen Fall -
// eine schlechte Uebersetzung - ungeprueft. Mehr kann ein Test hier nicht
// leisten, ohne falsch Alarm zu schlagen.

const (
	repoRoot = "../.."
	deDir    = "docs/de"
	enDir    = "docs/en"
)

// markdownFiles sammelt alle .md-Dateien unter dir, relativ zu dir.
func markdownFiles(t *testing.T, dir string) []string {
	t.Helper()
	root := filepath.Join(repoRoot, dir)
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	require.NoError(t, err)
	sort.Strings(out)
	return out
}

// headingLevels liefert die Ueberschriftenebenen eines Dokuments in
// Lesereihenfolge, z.B. [1 2 2 3 2]. Fenced Code Blocks werden uebersprungen:
// eine Kommentarzeile "# make verify" in einem Shell-Beispiel ist keine
// Ueberschrift.
func headingLevels(content string) []int {
	var levels []int
	inFence := false
	var fence string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inFence:
			if strings.HasPrefix(trimmed, fence) {
				inFence = false
			}
			continue
		case strings.HasPrefix(trimmed, "```"):
			inFence, fence = true, "```"
			continue
		case strings.HasPrefix(trimmed, "~~~"):
			inFence, fence = true, "~~~"
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
		// "#5 - Titel" ist eine Befundnummer, keine Ueberschrift: nach den
		// Rauten muss ein Leerzeichen stehen.
		if level > 6 || !strings.HasPrefix(trimmed[level:], " ") {
			continue
		}
		levels = append(levels, level)
	}
	return levels
}

var linkPattern = regexp.MustCompile(`\[[^\]]*\]\(([^)]+)\)`)

// stripCode entfernt Code-Bloecke und Inline-Code. Ohne das haelt der
// Link-Test einen regulaeren Ausdruck wie ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ fuer
// einen Markdown-Link auf die Datei "[-a-z0-9]*[a-z0-9]" - Klammer auf,
// Klammer zu, fertig ist der Fehlalarm.
func stripCode(content string) string {
	var b strings.Builder
	inFence := false
	var fence string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inFence:
			if strings.HasPrefix(trimmed, fence) {
				inFence = false
			}
			continue
		case strings.HasPrefix(trimmed, "```"):
			inFence, fence = true, "```"
			continue
		case strings.HasPrefix(trimmed, "~~~"):
			inFence, fence = true, "~~~"
			continue
		}
		// Inline-Code: alles zwischen zwei Backticks faellt weg.
		for {
			i := strings.IndexByte(line, '`')
			if i < 0 {
				break
			}
			j := strings.IndexByte(line[i+1:], '`')
			if j < 0 {
				line = line[:i]
				break
			}
			line = line[:i] + line[i+1+j+1:]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// relativeLinks liefert alle Linkziele, die auf eine Datei im Repo zeigen.
// Externe URLs, Anker und mailto fallen raus - die kann dieser Test nicht
// pruefen, ohne ins Netz zu gehen.
func relativeLinks(content string) []string {
	var out []string
	for _, m := range linkPattern.FindAllStringSubmatch(stripCode(content), -1) {
		target := strings.TrimSpace(m[1])
		if target == "" || strings.HasPrefix(target, "#") {
			continue
		}
		if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		if i := strings.IndexByte(target, '#'); i >= 0 {
			target = target[:i]
		}
		if target == "" {
			continue
		}
		out = append(out, target)
	}
	return out
}

func read(t *testing.T, parts ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{repoRoot}, parts...)...))
	require.NoError(t, err)
	return string(raw)
}

func bothTreesExist(t *testing.T) bool {
	t.Helper()
	for _, d := range []string{deDir, enDir} {
		if _, err := os.Stat(filepath.Join(repoRoot, d)); err != nil {
			t.Skipf("%s existiert noch nicht - der Baum wird gerade aufgebaut", d)
			return false
		}
	}
	return true
}

func TestDocsSync_BeideSprachbaeumeTragenDieselbenDateien(t *testing.T) {
	if !bothTreesExist(t) {
		return
	}
	de := markdownFiles(t, deDir)
	en := markdownFiles(t, enDir)

	assert.Equal(t, de, en,
		"docs/de/ und docs/en/ enthalten nicht dieselben Dateien - ein Dokument wurde nur in einer Sprache angelegt oder umbenannt")
}

func TestDocsSync_GliederungStimmtJeDokumentpaarUeberein(t *testing.T) {
	if !bothTreesExist(t) {
		return
	}
	for _, rel := range markdownFiles(t, deDir) {
		enPath := filepath.Join(repoRoot, enDir, rel)
		if _, err := os.Stat(enPath); err != nil {
			continue // der Dateimengen-Test meldet das bereits
		}
		deLevels := headingLevels(read(t, deDir, rel))
		enLevels := headingLevels(read(t, enDir, rel))

		assert.Equal(t, deLevels, enLevels,
			"docs/de/%s und docs/en/%s haben unterschiedliche Gliederungen - ein Abschnitt fehlt in einer der beiden Fassungen", rel, rel)
	}
}

func TestDocsSync_KeineTotenRelativenVerweise(t *testing.T) {
	roots := []string{"docs"}
	for _, name := range []string{"README.md", "README.de.md", "CONTRIBUTING.md", "CONTRIBUTING.de.md"} {
		if _, err := os.Stat(filepath.Join(repoRoot, name)); err == nil {
			roots = append(roots, name)
		}
	}

	var files []string
	for _, r := range roots {
		info, err := os.Stat(filepath.Join(repoRoot, r))
		if err != nil {
			continue
		}
		if !info.IsDir() {
			files = append(files, r)
			continue
		}
		for _, rel := range markdownFiles(t, r) {
			files = append(files, filepath.Join(r, rel))
		}
	}

	for _, f := range files {
		dir := filepath.Dir(filepath.Join(repoRoot, f))
		for _, link := range relativeLinks(read(t, f)) {
			resolved := filepath.Join(dir, link)
			_, err := os.Stat(resolved)
			assert.NoError(t, err,
				"%s verweist auf %q - das Ziel gibt es nicht", f, link)
		}
	}
}

func TestDocsSync_JedesDokumentVerweistAufDieGegensprache(t *testing.T) {
	if !bothTreesExist(t) {
		return
	}
	for _, pair := range []struct{ from, to string }{{deDir, enDir}, {enDir, deDir}} {
		for _, rel := range markdownFiles(t, pair.from) {
			content := read(t, pair.from, rel)
			// Der Verweis ist ein relativer Pfad in den anderen Baum; wie tief
			// er hinauflaeuft, haengt vom Unterverzeichnis ab - deshalb wird
			// nur auf den Zielbaum und den Dateinamen geprueft.
			want := filepath.ToSlash(filepath.Join(filepath.Base(pair.to), rel))

			assert.Contains(t, content, want,
				"docs/%s/%s enthaelt keinen Verweis auf die Gegensprache (erwartet ein Link, der auf %q endet)", pair.from, rel, want)
		}
	}
}

// Die Dokumentation soll wie eine einmal getroffene Festlegung lesen, nicht wie
// das Protokoll ihrer Entstehung. Ein neuer Leser muss sonst erst lernen, zu
// unterscheiden, was gilt und was einmal galt - und genau das ist die Arbeit,
// die ihm die Doku abnehmen soll. Die Historie steht vollstaendig in `git log`
// und in korifi-platform/FINDINGS.md.
//
// Diese Liste verbietet die Wendungen, mit denen sich Chronologie
// einschleicht. Sie ist bewusst eng: nur Formulierungen, die ohne Ausnahme
// einen zeitlichen Verlauf erzaehlen. Ein Argument geht dabei nie verloren -
// aus "erst A, dann B" wird "B, weil A diese Probleme haette".
var werdegangsWendungen = []string{
	// Phasenarchaeologie
	"seit Phase", "bis Phase", "vor Phase", "ab Phase", "in Phase",
	"Phase 4.5", "Phase 5", "Phase 6",
	"since phase", "until phase", "from phase", "in phase", "pre-phase",
	"phase 4.5", "phase 5", "phase 6",
	// Versionserzaehlung
	"v1:", "v2:", "Status quo v1", "v1 status quo",
	"erste Fassung des Projekts", "first version of the project",
	// Etappen
	"in zwei Stufen", "in two stages",
	"Was sich seit", "What has changed since",
	"Die urspruengliche Empfehlung", "Die ursprüngliche Empfehlung",
	"The original recommendation",
	// Zeitmarker ohne Informationswert
	"ursprünglich", "urspruenglich", "originally",
	"zunächst", "zunaechst", "initially",
	"früher", "frueher", "the earlier", "used to",
	"wie vorher", "as before",
	// Meta-Verweise auf die Doku selbst
	"Stand dieses Dokuments", "As of this document",
	"Dokumentationsstand", "documentation pass",
	// Verdikte ueber die Vergangenheit
	"Altlast",
}

func TestDocsSync_KeineWerdegangsSprache(t *testing.T) {
	var files []string
	for _, name := range []string{"README.md", "README.de.md", "CONTRIBUTING.md", "CONTRIBUTING.de.md"} {
		if _, err := os.Stat(filepath.Join(repoRoot, name)); err == nil {
			files = append(files, name)
		}
	}
	for _, tree := range []string{deDir, enDir} {
		if _, err := os.Stat(filepath.Join(repoRoot, tree)); err != nil {
			continue
		}
		for _, rel := range markdownFiles(t, tree) {
			files = append(files, filepath.Join(tree, rel))
		}
	}

	for _, f := range files {
		// Code-Bloecke und Inline-Code sind ausgenommen: dort steht, was im
		// Repo wirklich so heisst, nicht die Erzaehlung darueber.
		lines := strings.Split(stripCode(read(t, f)), "\n")
		for i, line := range lines {
			for _, phrase := range werdegangsWendungen {
				if strings.Contains(line, phrase) {
					assert.Fail(t, "Werdegang statt Festlegung",
						"%s:%d enthaelt %q\n  %s\n  Umformen: nicht erzaehlen, wie es dazu kam, sondern was gilt und warum.",
						f, i+1, phrase, strings.TrimSpace(line))
				}
			}
		}
	}
}
