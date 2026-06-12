# ADR 026 : Politique d'escalade humaine

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Avant cet ADR, l'intervention humaine etait soit systematique (prompt `[o/N]` a chaque synthese memoire, validation interactive de l'outline en mode chat), soit absente la ou elle aurait compte (selection sanitizee, defauts visuels livres, issues de revue persistantes — tous traverses en silence avec un simple log).

L'objectif du projet est explicite : **le systeme s'ameliore et tourne seul ; une validation humaine est demandee si besoin, et uniquement si besoin, dans les cas litigieux.** Il faut donc une definition fermee de « litigieux » et un mecanisme unique, non bloquant, pour solliciter l'humain.

## Decision

### 1. Mecanisme unique : `internal/escalation`

`escalation.Ask(Request{Reason, Details, Question, Default, Timeout})` :

- Affiche un constat une-page (`Details`) puis une question oui/non sur stderr.
- **Jamais bloquant** : sans terminal interactif (stdin non-TTY : CI, cron, dashboard web), au timeout (60 s par defaut), ou sur erreur de lecture, la **decision par defaut** s'applique.
- Chaque escalade est journalisee via `slog` (raison, decision) — le handler du monitor la **miroite automatiquement sur le dashboard SSE** (`--web`), et elle reste visible en post-mortem.

L'orchestrateur recoit le callback par injection (`orchestrator.WithEscalation`) : la librairie ne depend pas de stdin, seul le CLI cable l'interactivite.

### 2. Criteres d'escalade (liste fermee)

| Evenement | Lieu | Question | Defaut |
|---|---|---|---|
| **Selection sanitizee** (ADR 020) | orchestrateur, apres `SanitizeSelection` | Continuer avec le plan degrade ? | **Oui** (proceed) — refuser abandonne le run |
| **Issues de revue persistantes** (vues 3+ fois, exclues du rewrite) | orchestrateur, boucle de revue | Continuer ? | **Oui** (proceed) |
| **Defauts visuels non resolus** apres la passe finale (ADR 023) | CLI, sortie de boucle visuelle | Acquitter ? (presentation deja creee) | **Oui** (acquitte) |
| **Memoire litigieuse** (suppression/reecriture de guidelines, run degrade — ADR 024) | CLI, synthese memoire | Ecrire ces guidelines ? | **Non** (timeout 120 s) |

Tout le reste — retries, corrections, re-asks, memoire additive — passe **sans solliciter l'humain**.

### 3. Logique des defauts

- Les trois premiers cas degradent la *qualite d'un artefact deja largement produit* : l'interet de bloquer un run autonome est inferieur au cout ; le defaut est « proceed » mais l'evenement est trace, journalise dans l'issueLog et appris.
- La memoire litigieuse modifie le *comportement de tous les runs futurs* : le defaut est conservateur (« non »).

## Conséquences

### Positives

- Le contrat « humain uniquement si litigieux » est code, pas implicite : la liste des criteres est exhaustive et auditable.
- Les runs non surveilles (CI, web, cron) ne bloquent jamais et gardent un comportement deterministe documente.
- Un seul point d'implementation pour etendre la politique (futur : escalade interactive via le dashboard SSE plutot que stderr).

### Negatives / risques

- Un defaut « proceed » peut laisser passer un deck degrade sans humain dans la boucle — c'est le compromis assume de l'autonomie ; la triple trace (stderr, issueLog, dashboard) le rend toujours visible a posteriori.
- Le prompt stderr peut interferer avec le lecteur interactif du mode chat — les escalades surviennent apres la phase de chat (pipeline puis post-production), le conflit est theorique.

## Fichiers Concernés

- `internal/escalation/escalation.go` — mecanisme unique
- `internal/agent/orchestrator/orchestrator.go` — `WithEscalation`, points sanitize et stale issues
- `cmd/slidegen/agent.go` — cablage du callback
- `cmd/slidegen/main.go` — escalades defauts visuels et memoire litigieuse
