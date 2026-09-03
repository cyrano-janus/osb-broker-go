// Package store liefert einen fest verdrahteten Demo-Katalog mit den beiden
// Angeboten service-1 und database-service.
//
// Er wird bei jedem GET /v2/catalog den Services aus den ServiceDefinitions
// vorangestellt, und es gibt keinen Schalter dagegen. Das hat zwei Folgen, die
// man kennen muss: die Demo-Angebote erscheinen in jedem Produktivkatalog, und
// die eigene Konformitaetssuite prueft ausgerechnet sie, weil sie den ersten
// Service aus dem Katalog nimmt.
//
// Das Paket entfaellt mit dem Umbau aus
// docs/de/adr/0003-replace-http-layer.md.
package store
