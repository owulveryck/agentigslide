# ADR 021 : Budgets de texte calibres sur la geometrie des zones

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

La visual review du run `edito-trace.json` a trouve **21 defauts en passe 1, puis encore 15 en passe 2** — livres tels quels (limite de retries atteinte). Types dominants : `text_truncated` et `text_overflow`. La chaine causale est entierement amont :

1. `EstimateMaxChars` calculait la capacite par **surface brute** (`charsPerLine × lines`), en ignorant la perte de word-wrap : les fins de lignes irregulieres gaspillent 20-30 % d'une zone multi-lignes. Les budgets etaient donc systematiquement optimistes.
2. Le writer ne voyait qu'un budget **scalaire** (`~N` caracteres) sans la geometrie de la zone : un titre de carte de 27 caracteres recevait « pollinisation » (13 caracteres insecables sur une ligne de ~10).
3. Quand le texte depassait, `EnforceMaxChars` **tronquait avec une ellipse** — fabriquant exactement le defaut (`Le shift-left déplace la…`) que la visual review re-detectait ensuite, deux passes durant, au prix de screenshots + appels Vision.

Principe directeur : **corriger la cause, pas le symptome**. Un defaut detecte en aval doit devenir une contrainte amont ; sinon on paie sa detection a chaque run.

## Decision

### 1. Facteur d'efficacite de word-wrap

`EstimateMaxChars` applique desormais `wrapEfficiency = 0.78` aux zones multi-lignes (`charsPerLine × lines × 0.78`). Les zones d'une seule ligne ne subissent aucune decote (rien n'y est perdu au wrap). La valeur 0,78 est la calibration initiale ; elle sera ajustee par famille de police en confrontant les budgets aux verdicts de la visual review du golden run (mini-boucle fermee, risque R5 du plan).

### 2. Geometrie exposee de bout en bout

- `EstimateLineGeometry(widthPt, heightPt, font)` retourne `(charsPerLine, lines)`.
- `EditableFieldSummary` (index) gagne `charsPerLine` et `lines` ; `cmd/buildindex` les calcule (**l'index doit etre regenere** pour en beneficier).
- Le catalogue compact rend la geometrie : `bodyShape (texte ~240 6Lx40C)` ; `ParseCatalog`/`ParseSlideFields` la parsent (retro-compatible : champs optionnels du regex).
- Le prompt du writer affiche « zone de N lignes de ~C caracteres — evite les mots de plus de C caracteres ».

### 3. Re-ask avant troncature

Dans `writeSlides`, apres le retour du writer :

1. `OverLimitFields(content, fields)` detecte les champs au-dela de la **meme limite effective** que `EnforceMaxChars` (100 % du max, 90 % pour les titres — logique unifiee dans `effectiveFieldLimit`).
2. Si depassement : **une** relance ciblee du meme writer (`reaskShorter`) avec un feedback `overflow` par champ (« reformule en ≤ N caracteres, phrase complete, pas d'ellipse »). Cout : un appel Haiku/Sonnet de ~2 s, sur le meme prefixe cache.
3. La sortie raccourcie n'est retenue que si elle reduit strictement le nombre de depassements. `EnforceMaxChars` reste l'ultime garde-fou.

Les troncatures restantes sont deja tracees (`EnforcementAction`) : la metrique de succes est gratuite.

## Choix Technologiques

- La decote s'applique au moment du build d'index (valeurs stockees), pas au runtime : un seul endroit de verite, et les schemas du writer (`maxLength = maxChars × 4/5`) en heritent automatiquement.
- Le format `NLxC C` dans le catalogue est concis (~8 caracteres par champ) pour ne pas gonfler les 50-200 KB du catalogue.
- Le re-ask reutilise `WriteSlide` et son mecanisme de feedback existant — pas de nouveau chemin de code LLM.

## Conséquences

### Positives

- La cause racine des defauts visuels dominants est traitee a trois niveaux : budget realiste (decote), texte adapte a la forme de la zone (geometrie dans le prompt), correction par reformulation plutot que par mutilation (re-ask).
- La passe 2 de visual review devrait approuver — moins de boucles de correction, moins de screenshots, moins d'appels Vision.

### Negatives / risques

- Budgets ~22 % plus stricts : les writers produiront des textes legerement plus courts. C'est voulu (un texte coupe est pire qu'un texte concis).
- Le re-ask ajoute un appel LLM par slide en depassement — surcout faible et borne (1 re-ask max), largement compense par la disparition des boucles visuelles.
- Tant que l'index n'est pas regenere (`go run cmd/buildindex/build_template_index.go`), la geometrie est absente : le systeme degrade proprement (champs optionnels).

### Mesure (benchmark edito.md)

- `Enforcement` (troncatures) dans la trace : **0** (objectif), contre plusieurs sur le run de reference.
- Findings visuels passe 1 : **−50 %** ; passe 2 : **0** non resolu.

## Fichiers Concernés

- `internal/templateindex/dimensions.go` — `wrapEfficiency`, `EstimateLineGeometry`, `EstimateMaxChars`
- `internal/model/template.go` — `EditableFieldSummary.CharsPerLine/Lines`
- `cmd/buildindex/build_template_index.go` — calcul de la geometrie
- `internal/plan/plan.go` — rendu `NLxC C` dans le catalogue compact
- `internal/agent/types.go` — `TemplateField.Lines/CharsPerLine`
- `internal/agent/validate.go` — parsing geometrie, `effectiveFieldLimit`, `FieldOverrun`, `OverLimitFields`
- `internal/agent/writer/writer.go` — geometrie dans le prompt
- `internal/agent/orchestrator/orchestrator.go` — `reaskShorter` avant `EnforceMaxChars`
- Tests : `internal/agent/validate_test.go`, `internal/templateindex/dimensions_test.go`
