# ADR 0009: Der Broker läuft im Cluster der Operatoren

> [English](../../en/adr/0009-deployment-model.md) · Führende Fassung: deutsch

**Status:** angenommen · **Betrifft:** Betriebsmodell, Zertifikatsvertrauen, und damit die Bauform des Reconcile-Loops

## Kontext

Der Broker läuft heute als Kubernetes-Deployment neben den Operatoren, die er
steuert, und wird von Korifi über eine HTTPS-URL angesprochen. Für ein
produktives Cloud Foundry oder Tanzu TAS ist damit nicht entschieden, **wo** er
läuft — und diese Frage blockiert mehr, als ihr anzusehen ist: ein
Reconcile-Loop (C2) sieht völlig anders aus, je nachdem, ob der Broker ein
Kubernetes-Controller sein darf oder eine Anwendung, die von außen pollt.

**Was der Broker bereits kann**, und was deshalb *nicht* zur Entscheidung
steht:

- **Er terminiert TLS selbst.** `internal/server` bedient einen
  `http.Server` mit `ServeTLS`; das Zertifikat kommt über die Callbacks eines
  Reloaders, damit eine Erneuerung durch cert-manager ohne Neustart wirkt.
  mTLS mit Allowlist auf CN, DNS-Name und URI ist derselbe Pfad.
- **Er findet seinen Kubernetes-Zugang in beiden Lagen.** `main.go` benutzt
  `k8sconfig.GetConfig()`: in einem Pod das ServiceAccount-Token, außerhalb
  `$KUBECONFIG` oder `~/.kube/config`. Ein Betrieb außerhalb des Clusters
  scheitert also nicht am Code.

**Was offen ist**, sind drei Fragen, und keine davon ist eine Codefrage:

1. **Wem gehört das Zertifikat?** Eine Plattform prüft das Zertifikat des
   Brokers gegen ihren eigenen Vertrauensanker. Korifi vertraut der CA, die
   dieser Cluster ausstellt; ein fremdes Cloud Foundry tut das nicht.
2. **Woher kommt der Kubernetes-Zugang?** Läuft der Broker außerhalb des
   Clusters, braucht er ein Token, das nicht abläuft — oder eines, das
   nachgeladen wird.
3. **Wer betreibt das Ding?** Ein Kubernetes-Deployment und eine CF-App haben
   verschiedene Betreiber, verschiedene Protokollwege und verschiedene
   Neustart-Semantik.

## Entscheidung

**Der Broker läuft als Kubernetes-Deployment im Cluster der Operatoren.** Eine
konsumierende Plattform — Korifi, produktives Cloud Foundry, TAS oder ein
anderer Marktplatz — erreicht ihn über eine Netzwerkadresse und kennt ihn
ausschließlich über die OSB-API.

**Damit ist auch entschieden: der Broker darf ein Kubernetes-Controller sein.**
Ein Reconcile-Loop mit controller-runtime, Watches auf die CRs der Operatoren
und Leader-Election ist eine zulässige Bauform. Das war die eigentlich
blockierte Frage.

### Warum nicht als CF-Anwendung

Der Broker als App auf der konsumierenden Plattform löst Frage 1 von selbst:
eine CF-Route trägt ein Zertifikat, dem die Plattform ohnehin traut. Sie
tauscht damit aber ein gelöstes Problem gegen ein ungelöstes ein.

Ein Broker außerhalb des Clusters braucht ein Kubernetes-Token. Ein literales
`token:` in einer Kubeconfig läuft ab und wird nicht nachgeladen — client-go
liest nur `tokenFile:` periodisch neu (`transport.NewCachedFileTokenSource`).
Wer also die App-Variante will, muss beantworten, wer diese Datei in einem
CF-Container erneuert. Im Pod stellt sich die Frage nicht: das projizierte
Token rotiert, und derselbe Mechanismus liest es nach.

Dazu kommt, dass der Broker dann zwei Netze gleichzeitig sehen muss — das der
Plattform und das der Kubernetes-API. Als Deployment sieht er eines und wird
aus dem anderen erreicht.

### Warum kein BOSH-Release

Der TAS-eigene Weg, und der aufwendigste. Er zahlt auf keine der drei Fragen
ein, die hier offen sind, und bindet den Broker an eine Plattform, während
[ADR 0006](0006-platform-independence.md) ihn ausdrücklich von allen lösen will.

## Was daraus folgt

**Das Zertifikatsvertrauen ist eine Betriebsaufgabe, keine Codeaufgabe.** Drei
Wege, in aufsteigendem Aufwand:

1. **Ein Zertifikat von einer CA, der die Plattform bereits traut** — die
   Unternehmens-PKI oder ein öffentlicher Aussteller. Nichts weiter zu tun.
2. **Die CA des Clusters in den Vertrauensspeicher der Plattform.** Bei TAS
   ist das ein Feld im Ops Manager, bei Cloud Foundry ein Eintrag in den
   BOSH-Trusted-Certificates. Kein Code, aber eine Absprache.
3. **mTLS in beide Richtungen**, wenn die Plattform Client-Zertifikate
   ausstellt. Der Broker kann es bereits; ob eine Plattform es benutzt, ist
   ihre Entscheidung.

Welcher Weg gilt, entscheidet der Betreiber des Zielsystems. Der Broker
verlangt keinen bestimmten.

**Der Kubernetes-Zugang ist das in-cluster ServiceAccount-Token.** Es rotiert
von selbst, und client-go lädt es nach. Ein RBAC-Umfang je Definition liegt
bereits im Chart und wird durch einen Test gegen die ausgelieferten CRDs
gehalten.

## Folgen

**Gewinn:**

- Die schwierigste Frage — ein Token, das nicht abläuft — stellt sich nicht.
- Der Reconcile-Loop darf die Werkzeuge benutzen, die für diese Aufgabe
  gebaut sind: Watches statt Polling, Leader-Election statt einer Annahme über
  die Zahl der Replikate.
- Der Aufbau, der hier läuft und geprüft wird, ist derselbe wie der auf dem
  Zielsystem. Was sich ändert, ist der Konsument, nicht die Bauform — und
  genau das ist der Sinn von [ADR 0006](0006-platform-independence.md).

**Preis:**

- **Der Broker setzt einen Kubernetes-Cluster voraus.** Das gilt bereits: er
  provisioniert Custom Resources. Ausgeschlossen wird damit eine Zukunft, in
  der er Backends ohne Kubernetes steuert.
- **Ein Betreiber muss zwei Plattformen bedienen.** Der Broker gehört dem
  Kubernetes-Team, die Registrierung dem Cloud-Foundry-Team. Die Absprache
  über den Vertrauensanker liegt dazwischen und hat keinen natürlichen
  Eigentümer.
- **Die Netzwerkstrecke muss existieren.** Cloud Foundry muss den Cluster
  erreichen können. Auf einem abgeschotteten TAS ist das eine Freigabe, keine
  Selbstverständlichkeit.

## Abgrenzung

Dies entscheidet **nicht**, welcher Vertrauensanker gilt — das kann nur der
Betreiber des Zielsystems, und die drei Wege oben stehen gleichberechtigt.

Dies entscheidet **nicht**, dass der Reconcile-Loop gebaut wird, sondern nur,
dass er ein Controller sein *darf*. Was er tun soll, steht in
[known-issues.md](../known-issues.md).

Unberührt bleibt [ADR 0004](0004-tls-and-mtls-no-oauth2.md): TLS und mTLS
bleiben die Authentisierung gegenüber der Plattform, OAuth2 bleibt draußen.
