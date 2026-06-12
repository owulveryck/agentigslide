# ADR 020 : Selection contrainte par le catalogue et escalade sur sanitisation

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

La trace `edito-trace.json` montre le Selector echouant ses **3 tentatives** de validation, puis le pipeline continuant avec une selection « sanitizee » silencieusement :

- Tentative 1 : `sourceSlide 131 has no title field but needsTitle=true`.
- Tentative 2 : `sourceSlide 130` — **meme classe d'erreur**, slide adjacent : le feedback de validation n'a pas aide le modele (tokensIn identiques entre tentatives).
- Tentative 3 : `27 selections vs 26 slide needs` (mismatch de cardinalite).
- Apres epuisement : `SanitizeSelection` a corrige/supprime des entrees **sans laisser de trace dans l'issueLog**, et le Reviewer Opus (227 s, ~0,40 $) a ensuite detecte « sourceSlide 325 n'existe pas au catalogue » — un fait deterministe connu des la selection.

Trois defauts structurels identifies dans le code :

1. **Le schema du tool acceptait n'importe quel entier** pour `sourceSlide` : l'hallucination « slide 325 » etait permise au niveau API.
2. **Incoherence eligibilite/validation** : `NeedConstraintsWithCatalog` proposait comme eligibles les slides avec `Titles > 0 || Subtitles > 0`, mais `ValidateSelection` rejette si `Titles == 0`. Le selector pouvait donc choisir un slide presente comme eligible et echouer en validation — c'est exactement la boucle 131 → 130 observee.
3. **Retry non cible** : chaque retry regenerait les 26+ selections pour corriger 1 ou 2 entrees, avec le simple texte d'erreur en feedback.

Principe directeur : **shift-left deterministe** — toute erreur detectable par du code doit etre rendue impossible (schema contraint) ou corrigee au plus pres de sa source, jamais rattrapee par un LLM cher en aval. Et **echec bruyant, jamais silencieux** : une degradation deterministe du plan est un evenement enregistre.

## Decision

### 1. Schema de tool contraint par enum

`selectorTool(catalog)` construit dynamiquement le schema JSON avec `"sourceSlide": {"enum": [-1, <numeros du catalogue>]}`. Une selection hors catalogue devient une erreur de decodage cote API — la classe « slide 325 » disparait par construction.

### 2. Eligibilite calculee par les memes regles que la validation

- Nouvelle fonction `slideCompatible(need, counts, fields)` : implementation unique des verifications deterministes (titre requis, zones de texte, ratio items/champs, step-groups), utilisee a la fois par l'eligibilite et coherente avec `ValidateSelection`.
- `EligibleSlidesForNeed(need, catalog)` retourne les slides passant toutes les verifications.
- `NeedConstraintsWithCatalog` reecrit sur cette base : liste les eligibles (jusqu'a 40), ou le complement ineligible quand il est plus court (« tous les slides du catalogue SAUF : ... »). L'ancien plafond de 20 candidats et l'incoherence titre/sous-titre sont supprimes.
- `CatalogInfo` enrichi de `FieldDetailsBySlide` pour eviter de re-parser le catalogue par besoin.

### 3. Retry cible (partial retry)

- `ValidateSelectionDetailed` retourne des `SelectionIssue{SelectionIndex, OutlineIndex, Reason}` par entree invalide (le mismatch de cardinalite reste une erreur globale, irreparable entree par entree).
- `Selector.RunPartial` re-demande **uniquement** les entrees en echec, chacune presentee avec sa raison d'echec et sa liste d'eligibles, puis fusionne les corrections dans le plan courant. Fallback automatique sur le retry complet si le partiel echoue.
- L'orchestrateur choisit : tentative 0 = run complet ; tentatives suivantes = partiel si les issues sont par-entree, complet sinon.

### 4. Sanitisation = evenement litigieux

Quand `SanitizeSelection` s'execute apres epuisement des retries, l'orchestrateur enregistre une issue `sanitized_selection` dans l'issueLog (nombre d'entrees touchees + erreurs residuelles). Cet evenement :

- alimente la synthese memoire (ADR 015/024) ;
- constitue un critere d'escalade humaine (ADR 026).

## Choix Technologiques

- Enum JSON Schema plutot que post-validation : la contrainte est appliquee par le decodeur de tool-use du modele, cout nul.
- Le retry partiel reutilise le meme tool `select_templates` et le meme bloc catalogue cache (`cache_control: ephemeral`) — le cache prefix reste chaud entre run complet et partiel.
- `MaxTokens` du retry partiel reduit a 4096 (quelques selections, pas 26).

## Conséquences

### Positives

- La classe d'erreur dominante du run analyse (hors-catalogue, titre manquant) devient impossible ou auto-reparable en un retry cible.
- Cout des retries reduit : on regenere N entrees en echec, pas 26.
- Le Reviewer n'a plus a depenser de l'Opus sur des faits deterministes.
- La sanitisation est visible, apprise (memoire) et escaladable.

### Negatives / risques

- Le schema enum varie par template : a verifier que le hit-rate du cache prompt ne baisse pas (le tool est rendu avant les blocs caches) — risque R2 du plan, mesure via `--cost-history`.
- Une liste d'eligibles longue (~40 numeros par besoin) allonge legerement le prompt du selector — negligeable devant le catalogue de 50-200 KB.

### Mesure (benchmark edito.md)

- Tentatives selector jusqu'a validite : **≤ 1 retry** (3 echecs sur le run de reference).
- Sanitisations : **0** (1 sur le run de reference).
- Issues reviewer de type `wrong_template` hors-catalogue : **0**.

## Fichiers Concernés

- `internal/agent/selector/selector.go` — `selectorTool(catalog)` avec enum, `RunPartial`
- `internal/agent/validate.go` — `slideCompatible`, `EligibleSlidesForNeed`, `NeedConstraintsWithCatalog` reecrit, `SelectionIssue`, `ValidateSelectionDetailed`, `CatalogInfo.FieldDetailsBySlide`
- `internal/agent/orchestrator/orchestrator.go` — boucle de retry (partiel/complet), enregistrement `sanitized_selection`
- `internal/agent/validate_test.go` — tests de coherence eligibilite/validation et de la validation detaillee
