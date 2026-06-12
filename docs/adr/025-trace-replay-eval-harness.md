# ADR 025 : Harness d'evaluation par rejeu de traces (traceeval)

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Les ADR 019 a 024 modifient le comportement du pipeline sur ses trois axes (cout, vitesse, qualite). Sans instrument de mesure, deux risques :

1. **Pas de critere d'acceptation objectif** : « la passe visuelle trouve moins de defauts » n'est verifiable que si on compte les defauts de la meme maniere d'un run a l'autre.
2. **Regressions invisibles** : la demotion du reviewer (ADR 022) ou l'auto-memoire (ADR 024) peuvent degrader la qualite sans bruit — il faut un garde-fou qui compare chaque nouveau run au run de reference.

Le mode trace (ADR 017) capture deja toutes les donnees necessaires. Il manque l'outil qui les transforme en KPI comparables — l'artefact de mesure academique du projet.

## Decision

Nouveau CLI `cmd/traceeval` :

```bash
go run cmd/traceeval/main.go edito-trace.json                    # KPI d'une trace
go run cmd/traceeval/main.go edito-trace.json edito-trace-v2.json # + delta vs baseline
```

### KPI extraits par trace

- **Vitesse** : duree totale, duree par phase (ADR 019), **taux d'attribution** (Σ phases / total — cible ≥ 95 %).
- **Fiabilite** : tentatives outliner/selector, echecs de validation, detection de run **SANITIZED** (toutes les tentatives selector en echec), erreurs pipeline.
- **Qualite** : troncatures (`Enforcement`), iterations de review et issues par type, findings visuels par passe et **non resolus en derniere passe**, issues formatter par passe.
- **Cout** : tokens in/out par etage et cout estime via `metrics.LookupPricing` (modeles lus dans la config tracee).

### Rejeu deterministe

Le harness rejoue les verifications deterministes possibles sur les sorties LLM enregistrees — sans aucun appel API. Premiere verification implementee : tout `CharCount > MaxChars` non suivi d'un enforcement est signale comme desaccord (« le budget aurait du etre applique »). Le perimetre s'etendra avec les besoins (la trace ne contenant pas le catalogue compact, la revalidation complete de la selection n'est pas rejouable hors ligne — limitation documentee).

### Usage en gate de regression

`edito-trace.json` est le **golden run** de reference. Procedure apres chaque phase d'amelioration :

```bash
go run cmd/slidegen/main.go --file edito.md --trace edito-trace-vN.json
go run cmd/traceeval/main.go edito-trace.json edito-trace-vN.json
```

Le delta affiche (duree, cout, echecs selector, troncatures, defauts visuels non resolus) donne le verdict de la phase contre ses criteres d'acceptation (sections « Mesure » des ADR 019-024).

### Verification sur la trace de reference

Le harness, execute sur `edito-trace.json`, confirme les constats fondateurs du plan :

```
Durée totale                    6757.0s
Attribution par phases          0%            ← avant ADR 019
Outliner / Selector tentatives  1 / 3 (3 échecs, SANITIZED)
Writers                         29 appels, 1 troncatures
Review                          2 itérations, issues={duplicate, incoherence, overflow, wrong_template}
Visual review                   2 passes, 50 issues par passe, non résolus=50
Coût LLM estimé                 $1.049
```

## Conséquences

### Positives

- Chaque ADR a desormais un verdict mesurable et reproductible ; les regressions de qualite des ADR « economes » (022, 024) sont detectables hors ligne, a cout API nul.
- L'outil sert directement le volet academique : series temporelles de KPI par version du systeme.

### Negatives / risques

- Le cout estime ignore le cache (lecture/ecriture) — c'est une borne haute coherente entre traces, suffisante pour les deltas.
- Les KPI dependent de la version du format de trace ; le champ `version` du fichier permet de gerer les evolutions.

## Fichiers Concernés

- `cmd/traceeval/main.go` — nouveau CLI
- `internal/trace/types.go` — format consomme (inchange, enrichi par l'ADR 019)
