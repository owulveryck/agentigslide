# ADR 027 — La géométrie calculée est l'unique source de vérité des capacités texte

## Statut

Accepté

## Contexte

L'analyse de `edito-trace-v3.json` (premier run après les ADR 019-026) montre que le gate anti-overflow (`OverLimitFields`, ADR 021) est resté inopérant sur 5 issues d'overflow découvertes par le reviewer Opus en passe 1 (248 s). Cause racine : **deux sources de vérité pour la capacité d'un champ**.

- Le writer du slide 21 a reçu `card2contentShape maxChars=343` depuis l'index persistant.
- Le reviewer a raisonné sur une capacité différente (~121 caractères, « 4L×39C ») et a produit des findings d'overflow que le gate ne pouvait pas intercepter puisqu'il budgétait sur 343.

Deux mécanismes alimentent cette divergence :

1. **Index stale.** `template_index.json` persiste des `maxChars` calculés par une version antérieure du code d'estimation (voire estimés par Claude Vision avant l'ADR 021). Rien ne détecte qu'index et code ont divergé. Le `-check` introduit ici a immédiatement révélé 17 champs en dérive.
2. **Rebuild non déterministe.** `ExtractPredominantFont` départageait les familles de polices ex æquo par ordre d'itération de map : deux rebuilds successifs produisaient des capacités différentes pour les mêmes champs (ex. `step1titleShape` 10↔12), rendant tout gate de cohérence impossible.

## Décision

1. **`DerivedMaxChars(charsPerLine, lines)` devient l'unique fonction de dérivation** (`internal/templateindex/dimensions.go`). `wrapEfficiency` reste privé au package : aucun consommateur ne peut recalculer une capacité par un autre chemin.
2. **Normalisation au chargement.** `plan.LoadTemplateIndex` appelle `NormalizeIndexGeometry` : tout champ porteur d'une géométrie (`charsPerLine`, `lines`) voit son `MaxChars` réécrit par `DerivedMaxChars`. Writer, selector, reviewer (catalogue compact) et gates (`OverLimitFields`, `EnforceMaxChars`) consomment mécaniquement la même valeur. Les dérives sont loggées (signal d'index stale) ; les champs sans géométrie sont conservés tels quels et disparaissent au rebuild suivant.
3. **Garde anti-stale en CI.** `cmd/buildindex -check` recalcule l'index depuis les `analysis.json` et sort en code non-zéro si les capacités ou la géométrie persistées diffèrent. Le rebuild est rendu déterministe (tie-break lexicographique des familles de polices) pour que ce gate soit fiable.
4. L'index du template edito est reconstruit et versionné avec cette décision.

## Principe généralisable

*Une seule source de vérité par fait partagé.* Quand deux agents d'un pipeline voient deux valeurs différentes pour la même grandeur (ici la capacité d'un champ), chaque boucle de correction devient un conflit insoluble : l'un produit du « légal », l'autre le rejette, indéfiniment. Toute donnée partagée doit avoir une projection unique, dérivée par une seule fonction, consommée par tous — et un gate déterministe doit détecter la dérive entre la donnée persistée et le code qui la dérive.

## Conséquences

- Les findings d'overflow du reviewer deviennent vérifiables par code contre la même valeur que celle du writer (prérequis du cross-check de l'ADR 030).
- Les budgets writer peuvent *baisser* après normalisation (capacités vision optimistes écrasées) : davantage de re-asks « raccourcir » à court terme — c'est le but, le défaut est intercepté avant la review.
- `go run ./cmd/buildindex -check` est exécutable en CI (aucun appel LLM).

## Critères de succès (mesurés par traceeval sur le prochain run edito)

- Issues `overflow` au reviewer : 0 (5 sur la trace v3).
- `OverLimitOutputs` (rejeu déterministe traceeval) : 0.
- `buildindex -check` : vert sur deux exécutions consécutives.
