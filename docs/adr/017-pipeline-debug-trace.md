# ADR 017 : Mode trace structuree pour le pipeline de generation

- **Date** : 2026-06-11
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le pipeline multi-agent (Outliner → Selector → Writers → Reviewer → ExecutePlan → Formatter → VisualReview) produit des presentations a travers de nombreuses etapes et decisions intermediaires. Quand le resultat final presente des problemes (debordement de texte, police incorrecte, entites HTML visibles, layouts casses), il est extremement difficile de diagnostiquer la cause :

- Le texte deborde-t-il parce que le Writer a genere trop de contenu, ou parce que le template a un champ trop petit ?
- La police Arial vient-elle du template (baseStyle) ou d'un defaut d'application de style ?
- Le Reviewer a-t-il vu et ignore le probleme, ou ne l'a-t-il pas detecte ?
- L'EnforceMaxChars a-t-il tronque du contenu, et si oui, lequel ?

Les logs `slog` existants fournissent un flux d'evenements non structure qui ne capture pas les donnees intermediaires (outline produit, selections, contenu genere, styles appliques). Ils ne sont pas exploitables par un systeme automatise pour diagnostiquer les problemes.

## Decision

Ajouter un flag `--trace <fichier.json>` au CLI `slidegen` qui produit un fichier JSON auto-suffisant capturant chaque etape du pipeline avec ses entrees, sorties et decisions.

### Structure du fichier trace

```json
{
  "version": "1.0",
  "generatedAt": "...",
  "durationMs": 42000,
  "config": { "outlinerModel": "...", "writerModel": "...", ... },
  "userRequest": "...",
  "outline": { "attempts": [...], "finalSections": [...] },
  "selection": { "attempts": [...], "final": [...] },
  "writers": [{ "slideIndex": 0, "input": {...}, "output": {...}, "enforcement": [...] }],
  "review": { "iterations": [...] },
  "execution": { "perSlide": [{ "baseStyles": {...}, "textInsertions": [...] }] },
  "formatter": [{ "pass": 1, "issues": [...], "corrections": [...] }],
  "visualReview": [{ "attempt": 0, "findings": [...] }],
  "errors": []
}
```

### Implementation

- **Package `internal/trace`** : types JSON-serializables (`TraceFile`, `ConfigTrace`, `OutlineTrace`, `SelectionTrace`, `WriterTrace`, `ExecutionTrace`, etc.) et struct `Tracer` thread-safe (mutex) avec methodes nil-receiver safe.
- **Pattern nil-receiver** : toutes les methodes `Record*` commencent par `if t == nil { return }`. Le tracer est `nil` quand `--trace` n'est pas specifie, eliminant tout overhead en fonctionnement normal.
- **Functional options** : `orchestrator.WithTracer(t)` et `pipeline.WithExecTracer(t)` pour passer le tracer sans modifier les signatures existantes.
- **Snapshot-diff pour EnforceMaxChars** : le contenu est capture avant et apres `EnforceMaxChars` pour detecter les troncatures sans modifier `validate.go`.

### Points d'instrumentation

| Etape | Donnees capturees |
|-------|-------------------|
| Config | Modeles, retries, template ID, flags |
| Outliner | Tentatives (tokens, duree, erreurs validation), sections finales |
| Selector | Tentatives, selections finales avec champs template (variable, role, maxChars) |
| Writers | Entree (intent, contentItems, champs), sortie (modifications), troncatures EnforceMaxChars, feedback reviewer |
| Reviewer | Iterations (approuve/rejete, issues par slide, corrections) |
| ExecutePlan | Par slide : baseStyles (font, taille, couleur), element map, insertions texte |
| Formatter | Par passe : issues detectees, corrections appliquees |
| VisualReview | Par tentative : findings par page, issues visuelles |

## Alternatives rejetees

### Logs slog en mode debug

Augmenter le niveau de log a `debug` et ajouter des logs detailles. Rejete car :
- Non structure : un flux textuel n'est pas exploitable par une IA pour diagnostic
- Pas auto-suffisant : les logs sont entremeles avec d'autres informations systeme
- Pas reproductible : le format n'est pas garanti entre versions

### Fichiers intermediaires multiples (un par etape)

Ecrire un fichier par etape (`outline.json`, `selection.json`, etc.). Rejete car :
- Plus complexe a correler : il faut ouvrir N fichiers
- Pas auto-suffisant : le contexte est reparti entre fichiers
- Plus complexe a gerer : nettoyage, nommage, repertoire de sortie

### Trace OpenTelemetry

Utiliser le standard OpenTelemetry pour la trace. Rejete car :
- Infrastructure lourde : necessite un collecteur, un backend, une UI
- Inadapte aux donnees metier : les spans OTel sont concu pour la latence reseau, pas pour capturer des outlines de presentation ou des styles de police
- Overhead pour un outil CLI : disproportionne par rapport au besoin

## Consequences

- **Nouveau package** `internal/trace` avec types et tracer
- **Nouveau flag** `--trace` sur `cmd/slidegen`
- **Modifications** de l'orchestrateur et d'ExecutePlan via functional options (retrocompatible)
- **Zero overhead** en fonctionnement normal (nil-receiver pattern)
- Le fichier trace permet a un utilisateur ou une IA de diagnostiquer des problemes visuels en tracant le chemin complet : requete utilisateur → outline → selection template → contenu genere → contraintes appliquees → styles de base → corrections formatter
