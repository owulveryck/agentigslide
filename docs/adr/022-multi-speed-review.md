# ADR 022 : Revue a plusieurs vitesses gouvernee par les gates deterministes

- **Date** : 2026-06-12
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Sur le run `edito-trace.json`, la premiere iteration du Reviewer (Opus, thinking 5120) a coute **227 s et ~13 K tokens de sortie** (majoritairement du thinking) — le premier poste de cout du pipeline (~0,40 $ l'appel). Sur ses 4 issues :

- 2 etaient **deterministiquement detectables** (sourceSlide hors catalogue, titre duplique entre slides adjacents) et sont desormais couvertes par le gate `PreReviewValidation` (branche courante) et l'enum du selector (ADR 020) ;
- 2 etaient editoriales (incoherence sous-titre/template, formulation de titre) — detectables par un modele moins cher.

Avec les ADR 020/021, les classes d'erreurs structurelles n'atteignent plus le Reviewer. Maintenir Opus + thinking systematiquement revient a payer le tarif « risque eleve » sur des plans deja garantis structurellement sains.

Principe directeur : **cout proportionne au risque** — l'effort de verification LLM est dimensionne par le verdict des gates deterministes, qui sont gratuits et fiables.

## Decision

### Politique de tiering de la revue complete

Avant la boucle de revue, l'orchestrateur choisit le modele :

| Condition | Modele | Thinking |
|---|---|---|
| Gates propres (`PreReviewValidation` = 0 issue **et** pas de sanitisation selector) **et** deck ≤ `REVIEWER_TIER_THRESHOLD` (defaut 20) | `ReviewerSubsetModel` (Sonnet) | 0 |
| Gates en echec, selection sanitizee, grand deck, ou `AGENT_REVIEWER_FORCE_OPUS=true` | `ReviewerModel` (Opus) | `REVIEWER_THINKING_BUDGET` |

Nouvelles variables : `AGENT_REVIEWER_TIER_THRESHOLD` (defaut 20, 0 = toujours Opus) et `AGENT_REVIEWER_FORCE_OPUS` (defaut false).

Les re-revues de sous-ensemble (apres corrections) restent sur `ReviewerSubsetModel`, comme avant.

### Thinking

Le budget de thinking est desactive (0) sur le tier econome : les issues residuelles attendues (editoriales, par-slide) n'exigent pas de raisonnement etendu. Le chemin Opus conserve son budget configurable. Le passage du `budget_tokens` fige au thinking adaptatif de la famille 4.6 est une evolution de `internal/vertex` a instruire separement — hors perimetre de cet ADR.

## Choix Technologiques

- La decision de tier est **deterministe et tracee** (log + modele visible dans les `AgentRow` des metriques) : l'historique `--cost-history` montre directement quel tier a servi.
- `runReviewer` parametre par (modele, thinking) plutot que duplique : un seul chemin de code de revue.
- Le critere « sanitisation » reutilise l'evenement introduit par l'ADR 020 — les ADR se composent.

## Conséquences

### Positives

- Cout de la ligne reviewer : **−40 % a −80 %** sur les runs propres (Sonnet sans thinking vs Opus + thinking), soit le premier poste de cout du pipeline.
- Vitesse : la revue passe d'environ 227 s a un ordre de grandeur de 30-60 s sur le chemin econome.
- Le chemin Opus n'est jamais supprime : tout signal de risque le reactive automatiquement.

### Negatives / risques

- **R3 du plan** : Sonnet peut manquer des incoherences narratives inter-slides qu'Opus aurait vues. Garde-fous : (1) le seuil de taille de deck limite l'exposition ; (2) le harness de rejeu (ADR 025) mesure les defauts livres par tier ; (3) `AGENT_REVIEWER_FORCE_OPUS` permet le retour arriere immediat.
- Le tiering ajoute un parametre de configuration de plus — documente dans CLAUDE.md / README.

### Mesure (benchmark edito.md)

- Cout reviewer par run (metrics history) : baisse cible de ~40 %.
- Defauts livres (visual review + escalades) : stables ou en baisse — verifie par le harness de rejeu avant de generaliser.

## Fichiers Concernés

- `internal/agent/config.go` — `ReviewerTierThreshold`, `ReviewerForceOpus`
- `internal/agent/orchestrator/orchestrator.go` — decision de tier avant la boucle de revue, `runReviewer(ctx, state, model, thinkingBudget)`
