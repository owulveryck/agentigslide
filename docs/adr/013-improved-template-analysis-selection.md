# ADR 013 : Amelioration de l'analyse et de la selection de templates

- **Date** : 2026-05-21
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le pipeline multi-agent de generation de presentations (ADR 001) presente deux problemes recurrents :

1. **Repetitivite des selections** : Le Selector choisit systematiquement les memes slides template, produisant des presentations visuellement monotones. Sur un catalogue de ~298 slides non exclues, seule une fraction est utilisee en pratique.

2. **Erreurs recurrentes** : Certaines slides produisent des champs mal mappes (texte dans le mauvais champ) ou des templates inadaptes au contenu demande.

### Causes racines identifiees

- **Matching purement structurel** : Le Selector decide uniquement sur le nombre de champs, leur capacite en caracteres et le type de slide. Deux slides avec la meme structure (ex: "1 titre, 3 contenu") sont indifferenciables pour lui.

- **Metadonnees semantiques gaspillees** : Les keywords extraites dans `template_index.json` ne sont jamais exposees au Selector dans le compact catalog. Les descriptions sont tronquees a 150 caracteres, perdant le detail discriminant.

- **Contrainte de diversite faible** : `ValidateSelectionGlobal()` n'emet qu'un `slog.Warn` quand un template est utilise 3+ fois, sans declencher de retry.

- **Absence de classification** : Le prompt d'analyse (`prompt_analyze.txt`) ne demande ni categorie semantique, ni tags d'usage, ni style visuel. Des slides structurellement similaires mais destinees a des usages differents (ex: "presentation d'equipe" vs "citation") ne peuvent pas etre distinguees.

- **VariableNames ambigus** : Le prompt d'analyse n'insiste pas assez sur l'unicite et la semantique des noms de variables, produisant parfois des noms generiques (`text1`, `content2`) qui causent des mappings incorrects par le Writer.

## Decision

Ameliorer l'analyse et la selection en deux phases incrementales :

### Phase 1 : Quick wins sur la selection (sans re-analyse)

Modifications a impact immediat sur 3 fichiers :

1. **Exposer les keywords existantes au Selector** via une ligne `tags:` dans le compact catalog
2. **Ajouter un critere de correspondance semantique** dans le prompt du Selector (critere 0, avant les criteres structurels)
3. **Renforcer le prompt de diversite** pour que le Selector explore activement les alternatives avant de reutiliser un template, sans bloquer si c'est vraiment le meilleur choix
4. **Augmenter la troncature des descriptions** de 150 a 250 caracteres

### Phase 2 : Enrichissement de l'analyse

Ajout de metadonnees semantiques structurees dans l'analyse :

1. **`category`** : classification parmi un ensemble fixe (couverture, intercalaire, contenu_texte, contenu_illustre, donnees_tableau, etc.)
2. **`useCaseTags`** : 3-5 tags decrivant quand un presentateur choisirait cette slide
3. **`visualStyle`** : style visuel (minimal, illustre, data, pleine_image, split)
4. **Renforcement des variableNames** : instructions explicites contre les noms generiques

## Choix Technologiques

### Compact catalog enrichi

Le compact catalog (texte injecte dans le prompt du Selector) est enrichi avec la categorie dans le header et les tags d'usage :

```
SLIDE 42 (contenu_illustre) [1 titre, 2 contenu]: Slide de contenu avec illustration
  description: Description tronquee a 250 caracteres...
  disposition: 2 colonnes, 2 zones de contenu
  tags: presentation d'equipe, staffing, organigramme
  champs: titleMainShape (titre ~40) | content1Shape (contenu ~200)
```

Budget tokens supplementaires : ~7K tokens (+22%), bien dans les limites du context window de 200K.

Quand les `useCaseTags` sont disponibles (slides re-analysees), ils remplacent les keywords extraites automatiquement. Les keywords sont conservees en fallback pour la retrocompatibilite.

### Encouragement a la diversite

La diversite est encouragee dans le prompt du Selector (exploration active des alternatives avant reutilisation) mais n'est pas une contrainte bloquante. Si un template est vraiment le meilleur choix pour un contenu donne, sa reutilisation est acceptable. `ValidateSelectionGlobal()` emet un warning a 3+ utilisations pour la tracabilite, sans bloquer.

### Nouveaux champs dans les modeles

Trois champs optionnels ajoutes a `SlideAnalysis`, `VisionResponse` et `TemplateSlide` avec `omitempty` pour la retrocompatibilite JSON :

```go
Category    string   `json:"category,omitempty"`
UseCaseTags []string `json:"useCaseTags,omitempty"`
VisualStyle string   `json:"visualStyle,omitempty"`
```

### Categories predefinies

Ensemble fixe pour garantir la coherence :
- `couverture`, `intercalaire`, `contenu_texte`, `contenu_illustre`
- `donnees_tableau`, `donnees_graphique`, `citation`, `equipe`
- `timeline`, `diagramme`, `conclusion`, `question`

## Consequences

### Positives

- **Diversite accrue** : La contrainte stricte et le matching semantique forcent des selections plus variees
- **Meilleure adequation contenu/template** : Les tags d'usage permettent au Selector de distinguer des slides structurellement identiques mais semantiquement differentes
- **Moins d'erreurs de mapping** : Des variableNames plus explicites reduisent les confusions du Writer
- **Retrocompatibilite** : Les nouveaux champs sont optionnels (`omitempty`), les analysis.json existantes continuent de fonctionner
- **Incrementalite** : La Phase 1 apporte un gain immediat sans re-analyse ; la Phase 2 peut etre deployee progressivement slide par slide

### Negatives

- **Cout de re-analyse** : Les slides existantes doivent etre re-analysees pour beneficier des nouveaux champs (Phase 2). Cout API proportionnel au nombre de slides ciblees.
- **Augmentation du prompt Selector** : ~7K tokens supplementaires dans le compact catalog. Impact negligeable sur le cout mais augmente legerement la latence.
- **Diversite non garantie** : L'encouragement dans le prompt ne bloque pas la reutilisation. Si le catalogue manque de variete pour certaines structures, les memes templates seront reutilises — mais c'est acceptable car c'est alors le meilleur choix disponible.

### Backward Compatibility

- Les `analysis.json` existantes (sans `category`/`useCaseTags`/`visualStyle`) restent valides et sont lues sans erreur grace a `omitempty`
- Le compact catalog utilise les keywords en fallback quand les `useCaseTags` ne sont pas disponibles
- La categorie n'apparait dans le header que quand elle est renseignee
- Le `buildTemplateIndex` propage les nouveaux champs quand ils existent, sans impact sur les slides non re-analysees

## Fichiers Concernes

### Modifies

| Fichier | Modification |
|---------|-------------|
| `internal/plan/plan.go` | `BuildCompactIndex` : ajout tags, categorie dans header, troncature 250 chars |
| `internal/plan/plan.go` | `truncateDescription` : limite 150 -> 250 |
| `internal/agent/validate.go` | `ValidateSelectionGlobal` : log enrichi pour la tracabilite de la reutilisation |
| `internal/agent/selector/prompt_selector.txt` | Ajout critere 0 (correspondance semantique) |
| `analyzeSlides/prompt_analyze.txt` | Ajout category/useCaseTags/visualStyle + regles variableNames |
| `internal/model/analysis.go` | Ajout champs Category, UseCaseTags, VisualStyle a SlideAnalysis et VisionResponse |
| `internal/model/template.go` | Ajout champs Category, UseCaseTags, VisualStyle a TemplateSlide |
| `buildTemplateIndex/build_template_index.go` | Propagation des nouveaux champs |
