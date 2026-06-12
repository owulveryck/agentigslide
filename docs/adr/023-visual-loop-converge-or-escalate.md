# ADR 023 : Boucle visuelle ciblee — converger ou escalader, jamais degrader en silence

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Sur le run `edito-trace.json`, la visual review a trouve **21 defauts en passe 1 et encore 15 en passe 2** ; `MaxVisualRetries=1` etant atteint, les 15 defauts ont ete **livres tels quels**, signales uniquement par une ligne de log. Trois problemes :

1. **Budget de convergence insuffisant** : une seule iteration de correction pour des defauts qui demandent parfois deux reformulations.
2. **Ciblage duplique et incomplet** : le `correctedSet` (slides a re-reviewer) etait reconstruit dans `main.go` a partir d'un filtre de types (`text_overflow`/`text_truncated`) duplique de `HandleVisualFeedback` — les deux pouvaient diverger, et `empty_field` (pourtant corrigible par reecriture) n'etait pas traite.
3. **Degradation silencieuse** : les findings restants n'etaient ni resumes pour l'humain, ni enregistres dans l'issueLog — la synthese memoire (ADR 015) n'apprenait rien des defauts les plus visibles du produit final.

Principe directeur : **echec bruyant, jamais silencieux** — la boucle converge, ou elle escalade avec un constat actionnable ; et chaque defaut nourrit l'apprentissage.

## Decision

### 1. Budget de convergence

`AGENT_MAX_VISUAL_RETRIES` passe de 1 a **2** par defaut. Le cout marginal est borne car la re-review est ciblee (point 2) : seuls les slides corriges sont re-screenshotes et re-evalues.

### 2. Re-review derivee des corrections reelles

Le `correctedSet` est desormais derive **des operations effectivement corrigees** (`correctedOps[].SlideIndex` → pageID via `buildPlanIndexToPageID`), au lieu d'un filtre de types duplique. Toute extension future des types corrigibles dans `HandleVisualFeedback` cible automatiquement la re-review. `empty_field` rejoint les types corrigibles par reecriture (le repositionnement et les polices restent hors de portee d'un writer).

### 3. Sortie de boucle : constat + apprentissage

`reportUnresolvedVisualFindings` s'execute a chaque sortie de boucle avec defauts restants (limite atteinte **ou** plus rien de corrigible) :

- **Synthese une page** sur stderr : `[type] slide : description → suggestion` — l'humain sait exactement avec quoi la presentation est livree.
- **Enregistrement dans l'issueLog** (`agent: visual-reviewer`) : la synthese memoire apprend des defauts visuels (ADR 024), et la politique d'escalade (ADR 026) dispose de l'evenement.

### Differe : prefetch concurrent des thumbnails

Le prefetch des thumbnails en amont des appels Vision est **differe** : la review est deja parallelisee par slide (`maxParallel`), et la Phase 0 (ADR 019) fournit desormais `ThumbnailFetchMs`/`ReviewMs` par slide — la decision sera prise sur ces mesures, pas sur une intuition (risque R1 du plan d'amelioration).

## Conséquences

### Positives

- Plus aucun defaut visuel livre silencieusement : corrige, ou expose a l'humain et appris.
- Le ciblage exact reduit le cout des passes 2/3 a proportion des defauts restants.
- Avec l'ADR 021 (budgets geometriques) en amont, la boucle devrait converger en 1 passe dans le cas nominal — la passe 2 est une assurance, pas un cout systematique.

### Negatives / risques

- Une passe potentielle de plus quand l'ADR 021 ne suffit pas : borne par le ciblage et par `AGENT_VISUAL_REVIEW_TIMEOUT` (ADR 019).
- Les defauts non corrigibles par texte (misalignment, font_issue) sortent de la boucle des la premiere passe — c'est voulu : ils relevent du Formatter ou de l'humain, pas d'une reecriture.

### Mesure (benchmark edito.md)

- Defauts livres sans acquittement : **0** (15 sur le run de reference).
- Findings non resolus presents dans l'issueLog et la synthese stderr : 100 % des restants.

## Fichiers Concernés

- `internal/agent/config.go` — defaut `MaxVisualRetries` 1 → 2
- `internal/agent/editorchestrator/editorchestrator.go` — `empty_field` corrigible
- `cmd/slidegen/main.go` — `buildPlanIndexToPageID`, `correctedSet` derive des ops corrigees, `reportUnresolvedVisualFindings`, issueLog passe a `runVisualReview`
