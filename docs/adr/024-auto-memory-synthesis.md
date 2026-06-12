# ADR 024 : Synthese memoire automatique gouvernee par la litigiosite

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

L'apprentissage par memoire d'agents (ADR 015) souffrait de deux defauts symetriques sur le run `edito-trace.json` :

1. **Aveugle aux defauts dominants** : l'issueLog n'etait alimente que par selector/pre-review/reviewer. Les 21+15 findings de la visual review, les 22 corrections du formatter et les troncatures `EnforceMaxChars` n'y figuraient pas — la synthese memoire n'apprenait rien des defauts les plus visibles du produit final.
2. **Ceremonie humaine systematique** : chaque proposition de guideline bloquait sur un prompt `[o/N]`, meme purement additive et issue d'evenements deterministes. C'est l'inverse de l'objectif « le systeme s'ameliore a chaque etape, l'humain n'arbitre que le litigieux ».

Principe directeur : **boucle fermee** — chaque run produit les donnees qui ameliorent le suivant ; la friction humaine est reservee aux decisions reellement risquees.

## Decision

### 1. issueLog elargi a toutes les sources de defauts

| Source | Agent enregistre | Type d'issue |
|---|---|---|
| Validation selector (existant) | `selector` | `validation_error` |
| Sanitisation selector (ADR 020) | `selector` | `sanitized_selection` |
| Pre-review gate (existant) | `pre-review` | types deterministes |
| Reviewer (existant) | `reviewer` | types editoriaux |
| **Troncatures persistantes apres re-ask** (ADR 021) | `writer` | `truncated_text` |
| **Corrections formatter** | `formatter` | `formatting_<regle>` |
| **Findings visuels non resolus** (ADR 023) | `visual-reviewer` | types visuels |

Les enregistrements issus des goroutines writers sont collectes par slide puis enregistres apres la barriere (`IssueLog` n'est pas goroutine-safe).

### 2. Classification de litigiosite des propositions memoire

`runMemorySynthesis` classe chaque proposition :

- **Auto-appliquee** (sans prompt) : la proposition est **additive** (`IsAdditiveUpdate` — toutes les lignes existantes restent presentes verbatim) **et** le run n'a pas d'evenement litigieux pour cet agent (`IssueLog.HasLitigiousIssues` — sanitisation).
- **Litigieuse** (prompt `[o/N]` conserve) : suppression ou reecriture de guidelines existantes, ou proposition derivee d'un run degrade (sanitize). Une guideline qui disparait silencieusement est aussi dangereuse qu'une mauvaise qui apparait.

### 3. Reversibilite par git

Les fichiers `*_MEMORY.md` vivent dans le repertoire template versionne : toute guideline auto-appliquee est diffable et revertable (`git diff template/`). C'est le filet de securite qui rend l'auto-application acceptable (risque R4 du plan).

## Conséquences

### Positives

- La memoire apprend enfin des classes de defauts qui coutent le plus (visuel, troncatures, style) — chaque run reduit la probabilite de recurrence du suivant.
- Zero friction humaine dans le cas nominal ; l'humain n'est sollicite que pour les suppressions/reecritures et les runs degrades.

### Negatives / risques

- Une guideline additive de mauvaise qualite peut s'accumuler — bornee par la limite de 10-15 guidelines du prompt de synthese et par la revue git.
- La detection « additive » est ligne-a-ligne verbatim : une reformulation mineure d'une guideline existante est classee litigieuse (faux positif acceptable — le cout est un prompt, pas une perte).

### Mesure

- Part des syntheses memoire appliquees sans prompt : cible ≥ 80 % des runs.
- Recurrence des types d'issues dans l'issueLog run-over-run (via traces) : tendance a la baisse.

## Fichiers Concernés

- `internal/agent/memory.go` — `IsAdditiveUpdate`, `IssueLog.HasLitigiousIssues`
- `internal/agent/orchestrator/orchestrator.go` — troncatures dans l'issueLog (apres barriere writers)
- `cmd/slidegen/main.go` — formatter et visual review dans l'issueLog, classification et auto-application dans `runMemorySynthesis`
