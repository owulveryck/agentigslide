# ADR 016 : FormatAgent — verification structurelle de coherence et remplacement de fixfonts

- **Date** : 2026-05-30
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Le pipeline multi-agent (ADR 001) produit des presentations via une chaine Outliner → Selector → Writers → Reviewer. Apres l'execution des modifications sur Google Slides, une etape post-production `fixfonts` detecte et corrige les problemes de formatage.

`fixfonts` souffre de quatre limitations :

1. **Isolation architecturale** : c'est une etape hors pipeline, sans participation au feedback loop, sans memoire (ADR 015), sans orchestration. Son code vit dans `internal/fixfonts/`, decouple du pattern agent.

2. **Extraction structurelle incomplete** : `ExtractStructure()` extrait polices, tailles, espacement et bounding boxes, mais ignore :
   - Couleurs de texte (ForegroundColor)
   - Couleurs de fond des shapes (ShapeBackgroundFill)
   - Alignement de paragraphe (START, CENTER, END, JUSTIFIED)
   - Indentation (start, end, first line)
   - Underline, strikethrough
   - Outline de shape (couleur, epaisseur)
   - ContentAlignment de shape (TOP, MIDDLE, BOTTOM)

3. **Absence de verification de coherence** : fixfonts traite chaque element individuellement avec Claude Vision. Il ne verifie pas la coherence inter-slides (memes polices par role, palette de couleurs unifiee, hierarchie de tailles respectee). Toute l'intelligence est deleguee au LLM, ce qui rend le resultat non-deterministe et couteux.

4. **Dependance systematique a Claude Vision** : chaque execution necessite l'export PDF complet + un appel Claude Opus, meme quand les problemes sont detectables par simple inspection de la structure API.

Par ailleurs, la Google Slides API expose en lecture toutes les proprietes de style des elements de maniere structuree. Une analyse deterministe de ces donnees peut detecter la majorite des incoherences de formatage sans appel LLM.

## Decision

### Remplacement de fixfonts par un agent Formatter

Creer un agent `Formatter` integre au pipeline d'agents, suivant le meme pattern que Outliner, Selector, Writer, Reviewer et Designer. Cet agent remplace `fixfonts` dans les deux pipelines (creation et edition).

### Extraction structurelle enrichie

Etendre les types d'extraction pour capturer toutes les proprietes de style disponibles via l'API Google Slides :

```go
type TextRunInfo struct {
    StartIndex      int       `json:"startIndex"`
    EndIndex        int       `json:"endIndex"`
    Content         string    `json:"content"`
    FontFamily      string    `json:"fontFamily,omitempty"`
    FontSizePt      float64   `json:"fontSizePt,omitempty"`
    Bold            bool      `json:"bold,omitempty"`
    Italic          bool      `json:"italic,omitempty"`
    Underline       bool      `json:"underline,omitempty"`
    Strikethrough   bool      `json:"strikethrough,omitempty"`
    ForegroundColor *RGBColor `json:"foregroundColor,omitempty"`
}

type RGBColor struct {
    Red   float64 `json:"red"`
    Green float64 `json:"green"`
    Blue  float64 `json:"blue"`
}

type ParagraphInfo struct {
    StartIndex    int     `json:"startIndex"`
    EndIndex      int     `json:"endIndex"`
    LineSpacing   float64 `json:"lineSpacing,omitempty"`
    SpaceAbovePt  float64 `json:"spaceAbovePt,omitempty"`
    SpaceBelowPt  float64 `json:"spaceBelowPt,omitempty"`
    Alignment     string  `json:"alignment,omitempty"`
    IndentStartPt float64 `json:"indentStartPt,omitempty"`
    IndentEndPt   float64 `json:"indentEndPt,omitempty"`
    IndentFirstPt float64 `json:"indentFirstPt,omitempty"`
}

type ElementInfo struct {
    ObjectID         string          `json:"objectId"`
    ShapeType        string          `json:"shapeType,omitempty"`
    PlaceholderType  string          `json:"placeholderType,omitempty"`
    BoundingBox      BoundingBox     `json:"boundingBox"`
    TextRuns         []TextRunInfo   `json:"textRuns"`
    Paragraphs       []ParagraphInfo `json:"paragraphs"`
    CellLocation     *CellRef        `json:"cellLocation,omitempty"`
    BackgroundColor  *RGBColor       `json:"backgroundColor,omitempty"`
    ContentAlignment string          `json:"contentAlignment,omitempty"`
    OutlineColor     *RGBColor       `json:"outlineColor,omitempty"`
    OutlineWeightPt  float64         `json:"outlineWeightPt,omitempty"`
}
```

### Verification de coherence deterministe

Le Formatter execute des regles de coherence purement deterministes (aucun appel LLM) sur la structure extraite :

| Regle | Description | Entree |
|-------|-------------|--------|
| `font_family_by_role` | Tous les elements de meme PlaceholderType (TITLE, BODY, SUBTITLE) utilisent la meme famille de police | TextRunInfo.FontFamily x PlaceholderType |
| `font_size_by_role` | Taille de police coherente par role au sein de la presentation | TextRunInfo.FontSizePt x PlaceholderType |
| `size_hierarchy` | Hierarchie respectee : TITLE >= SUBTITLE >= BODY | Tailles medianes par role |
| `color_palette` | Couleurs de texte limitees a un ensemble coherent (pas de couleurs orphelines utilisees sur un seul slide) | TextRunInfo.ForegroundColor sur toute la presentation |
| `background_consistency` | Couleurs de fond coherentes pour les shapes de meme role | ElementInfo.BackgroundColor x PlaceholderType |
| `paragraph_spacing` | Espacement de paragraphe consistant par role | ParagraphInfo.LineSpacing, SpaceAbove, SpaceBelow x PlaceholderType |
| `alignment_by_role` | Alignement coherent par role (ex: tous les titres CENTER, tous les corps START) | ParagraphInfo.Alignment x PlaceholderType |
| `emphasis_coherence` | Bold/italic utilises de maniere coherente par role (ex: tous les titres bold, aucun corps bold) | TextRunInfo.Bold, Italic x PlaceholderType |
| `outline_consistency` | Outline de shape coherent par type de shape | ElementInfo.OutlineColor, OutlineWeightPt x ShapeType |

Chaque regle produit des `ConsistencyIssue` :

```go
type ConsistencyIssue struct {
    Rule       string `json:"rule"`
    SlideIndex int    `json:"slideIndex"`
    ObjectID   string `json:"objectId"`
    Expected   string `json:"expected"`
    Actual     string `json:"actual"`
    Severity   string `json:"severity"` // "error" | "warning"
}
```

La valeur "expected" est calculee par majorite : la valeur la plus frequente pour un role donne est consideree comme la reference. Les elements qui devient sont signales.

### Integration au pipeline comme agent

Le Formatter suit le pattern agent standard :

- **Package** : `internal/agent/formatter/`
- **Role** : extraire la structure, verifier la coherence, produire des corrections
- **Entree** : acces a `slides.Service` pour lire la presentation generee
- **Sortie** : `[]Correction` (meme type que fixfonts, etendu avec couleurs/alignement)
- **Memoire** : fichier `template/{TEMPLATE_ID}/FORMATTER_MEMORY.md` (ADR 015)
- **Orchestration** : appele par l'Orchestrator apres l'execution des slides, avant toute review visuelle

**Pipeline de l'agent :**

1. **Extraction** — Recuperer la presentation via `Presentations.Get()`, extraire la structure enrichie
2. **Analyse de coherence** — Appliquer les regles deterministes, produire la liste de `ConsistencyIssue`
3. **Generation de corrections** — Transformer les issues en `[]Correction` (BatchUpdate requests)
4. **Application** — Executer les corrections via `BatchUpdate`

L'etape 2 est entierement deterministe. Pas de PDF, pas de Claude Vision, pas de LLM.

### Remplacement de fixfonts

Le package `internal/fixfonts/` est remplace par `internal/agent/formatter/`. Les elements reutilises :

- `ExtractStructure()` / `ExtractStructureForPages()` → etendus avec les nouveaux champs
- `ValidateCorrections()` → reutilise tel quel
- `BuildCorrections()` → etendu pour les nouveaux types de correction (couleurs, alignement)
- `ApplyCorrections()` → reutilise tel quel
- `ExportPDF()` → supprime (plus de dependance au PDF)

Les types structurels (`SlideInfo`, `ElementInfo`, `TextRunInfo`, etc.) migrent vers `internal/agent/formatter/`.

### Positionnement dans le pipeline

```
Outliner → Selector → Writers → Assembler → Reviewer → Execute → Formatter → [Visual Review]
```

Le Formatter s'execute :
- Apres `ExecuteSlides()` / `ExecuteEditPlan()` (les slides sont dans Google Slides)
- Avant la review visuelle (thumbnails) si elle existe
- La review visuelle reste optionnelle pour les cas que la structure ne peut pas capturer (overlaps visuels, rendu typographique, equilibre esthetique)

## Alternatives evaluees

### 1. Etendre fixfonts avec un pre-traitement deterministe

Ajouter une phase de coherence dans `fixfonts` tout en conservant Claude Vision pour la confirmation. Rejete car :
- Maintient l'isolation architecturale de fixfonts hors du pipeline
- Pas de memoire d'agent, pas de feedback loop
- L'architecture a deux phases (deterministe + vision) dans un meme module rend le code plus complexe sans gain d'uniformite

### 2. Module separe `structcheck` + fixfonts conserve

Creer un nouveau package `internal/structcheck/` qui alimente fixfonts en donnees. Rejete car :
- Multiplie les modules de post-traitement (structcheck + fixfonts + visual review)
- Le rapport de coherence et les corrections sont produits par deux systemes differents
- Ne resout pas le probleme d'isolation de fixfonts

### 3. Integrer la coherence dans le Reviewer existant

Etendre le Reviewer pour qu'il verifie aussi la coherence de formatage. Rejete car :
- Le Reviewer travaille sur le `GenerationPlan` (texte), pas sur la presentation Google Slides rendue
- Il faudrait lui donner acces aux APIs Google Slides, ce qui casse sa separation de responsabilites
- La verification de formatage est un concern distinct de la review de contenu

## Consequences

### Positives

- **Determinisme** : les regles de coherence produisent des resultats reproductibles et previsibles
- **Cout reduit** : pas d'appel LLM ni d'export PDF pour les verifications structurelles
- **Vitesse** : un appel API Google Slides + traitement Go vs export PDF + Claude Vision
- **Uniformite architecturale** : meme pattern que les autres agents (config, memoire, orchestration)
- **Couverture elargie** : verifie les couleurs, l'alignement, et la coherence inter-slides — des dimensions que fixfonts ignorait
- **Memoire** : capitalise sur les patterns d'erreur par template (ADR 015)

### Negatives

- **Perte de la confirmation visuelle** : le Formatter ne "voit" pas le rendu final. Certains problemes (overlaps, rendu typographique, troncature visuelle) ne sont detectables que par vision. La review visuelle (thumbnails) reste necessaire pour ces cas.
- **Effort de migration** : les appels a `fixfonts.Run()` et `fixfonts.RunForSlides()` dans `cmd/slidegen/main.go` et `cmd/slidegen/edit.go` doivent etre remplaces. Le CLI `cmd/fixfonts/` doit etre adapte ou supprime.
- **Maintenance des regles** : les regles deterministes doivent evoluer avec les templates. Le mecanisme de "majorite" attenue ce risque mais des faux positifs sont possibles sur des presentations avec un style volontairement varie.

## Configuration

```bash
# Modele : non applicable (pas de LLM dans la phase deterministe)
# Si une phase optionnelle de confirmation visuelle est ajoutee plus tard :
export AGENT_FORMATTER_MODEL="claude-sonnet-4-6"           # default, si vision optionnelle
export AGENT_FORMATTER_ENABLED=true                         # default
export AGENT_FORMATTER_MEMORY_ENABLED=true                  # default, suit ADR 015
```

## Fichiers concernes

### Nouveaux fichiers

| Fichier | Description |
|---------|-------------|
| `internal/agent/formatter/formatter.go` | Agent principal : extraction, coherence, corrections |
| `internal/agent/formatter/extract.go` | Extraction structurelle enrichie (migre+etendu de fixfonts) |
| `internal/agent/formatter/consistency.go` | Regles de coherence deterministes |
| `internal/agent/formatter/types.go` | Types etendus (SlideInfo, ElementInfo, RGBColor, ConsistencyIssue) |

### Fichiers modifies

| Fichier | Modification |
|---------|-------------|
| `internal/agent/config.go` | Ajout config Formatter (FormatterEnabled, FormatterModel) |
| `internal/agent/orchestrator/orchestrator.go` | Appel du Formatter apres execution des slides |
| `cmd/slidegen/main.go` | Remplacer appels `fixfonts.Run()` par orchestration Formatter |
| `cmd/slidegen/edit.go` | Remplacer appels `fixfonts.RunForSlides()` par orchestration Formatter |
| `docs/glossary.md` | Ajouter entree pour l'agent Formatter |

### Fichiers supprimes (ou deprecies)

| Fichier | Raison |
|---------|--------|
| `internal/fixfonts/` | Remplace par `internal/agent/formatter/` |
| `cmd/fixfonts/` | A evaluer : conserver comme CLI standalone utilisant le nouveau package, ou supprimer |
