# Validation programmatique

Entre chaque étape du pipeline, des validations programmatiques vérifient la cohérence des sorties d'agents avant de les transmettre à l'étape suivante. Les fonctions sont dans [`internal/agent/validate.go`](../internal/agent/validate.go).

## ValidateOutline

Vérifie la structure de la sortie de l'Outliner :

- `PresentationTitle` non vide
- Au moins une section
- Chaque section a au moins un `SlideNeed`
- `ItemCount` correspond à `len(ContentItems)` pour chaque besoin

## ValidateSelection

Vérifie la sortie du Selector contre l'outline et le catalogue :

- Le nombre de sélections correspond au nombre total de `SlideNeeds`
- Chaque `OutlineIndex` est dans la plage valide
- Pour les slides non-diagramme : le `SourceSlide` existe dans le catalogue
- Vérifications de capacité du template :
  - Un template sans champs ne peut pas avoir de `ItemCount > 0`
  - Les champs texte doivent exister pour le contenu demandé
  - Warning si `ItemCount > textFields * 2` (ratio trop élevé)

## ValidateSelectionGlobal

Contraintes inter-sélections :

- **Erreur** : tous les slides `section_divider` doivent utiliser le même template
- **Warning** : templates utilisés 3+ fois (informatif, non bloquant)

## Erreurs vs Warnings

Les validations distinguent les erreurs bloquantes (le pipeline s'arrête ou retry) des warnings (le pipeline continue avec un log). Les warnings sont typiquement des cas où le Writer peut s'adapter (ex: plus de contenu que de champs — le Writer fusionne).

## Retries

Quand `ValidateSelection` retourne une erreur, l'orchestrateur relance le Selector avec les erreurs dans le prompt, jusqu'à `MaxSelectorRetries` tentatives (défaut : 2).

## Voir aussi

- [Structured output](./structured-output.md) — le parsing qui précède la validation
- [Boucle de feedback](./review-feedback-loop.md) — validation de qualité par le Reviewer (complémentaire)
