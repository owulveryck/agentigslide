# Formatter (post-production déterministe)

Le Formatter est un agent de post-production qui vérifie et corrige la cohérence de mise en forme des slides générées. Contrairement aux autres agents du pipeline, il n'utilise pas de LLM — son fonctionnement est entièrement déterministe. L'implémentation est dans [`internal/agent/formatter/`](../internal/agent/formatter/).

## Pipeline

```
Google Slides API
      │
      ▼
 ExtractStructure(pres) → []SlideInfo
      │
      ▼
 CheckConsistency(structure) → []ConsistencyIssue
      │
      ▼
 GenerateCorrections(issues, structure) → []Correction
      │
      ▼
 ValidateCorrections(plan, structure) → plan filtré
      │
      ▼
 ApplyCorrections(ctx, ...) → BatchUpdate API
```

1. **Extract** : lit la présentation via l'API Google Slides et extrait les styles de texte (police, taille, couleur, alignement, espacement) de chaque élément
2. **Check** : applique des règles déterministes de cohérence pour détecter les écarts
3. **Generate** : transforme chaque issue en une correction concrète (requête BatchUpdate)
4. **Validate** : filtre les corrections invalides ou redondantes
5. **Apply** : envoie les corrections via l'API Google Slides

## Règles de cohérence

| Règle | Description |
|-------|-------------|
| `FontFamilyByRole` | Les éléments de même rôle doivent utiliser la même police (vote majoritaire) |
| `FontSizeByRole` | Taille de police cohérente par rôle |
| `AlignmentByRole` | Alignement cohérent par rôle |
| `EmphasisCoherence` | Gras/italique cohérent pour les éléments de même rôle |
| `ParagraphSpacing` | Espacement inter-lignes, espace avant/après cohérent |
| `BackgroundConsistency` | Couleur de fond cohérente |
| `OutlineConsistency` | Couleur et épaisseur de contour cohérentes |
| `SizeHierarchy` | Hiérarchie de tailles respectée (titre > sous-titre > corps) |
| `ColorPalette` | Palette de couleurs cohérente |

Le principe est le **vote majoritaire** : la valeur la plus fréquente pour un rôle donné devient la référence, et les écarts sont corrigés.

## Modes d'exécution

- **`Run()`** : traite toute la présentation
- **`RunForPages()`** : traite uniquement un sous-ensemble de pages (utilisé après édition de slides spécifiques)

## Voir aussi

- [Boucle de feedback](./review-feedback-loop.md) — validation de qualité par le Reviewer (avant le Formatter)
- [Métriques](./metrics.md) — le Formatter n'apparaît pas dans les métriques LLM (pas d'appel API Claude)
