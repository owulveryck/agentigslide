# ADR 018 : Ameliorations du pipeline issues de l'analyse de trace

- **Date** : 2026-06-11
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

L'analyse de la trace `edito-trace.json` (generation de 27 slides depuis `edito.md`) a revele 6 problemes recurrents dans le pipeline multi-agent. Le reviewer n'a jamais approuve la presentation en 3 iterations, et 13 des 40 appels writer etaient des retries inutiles causes par des problemes structurels que le writer ne peut pas corriger.

Les 5 corrections ci-dessous ont ete implementees apres analyse du fichier de trace produit par ADR 017.

## Decisions

### 1. Slide de conclusion automatique (configurable par template)

**Probleme** : Le reviewer reclamait a chaque iteration une slide de conclusion absente. L'outliner ne la genere pas et le writer ne peut pas en ajouter.

**Solution** : `plan.LoadClosingSlide(templateDir)` lit un fichier `CLOSING_SLIDE` dans le repertoire du template contenant le numero de slide a ajouter en fin de presentation. L'orchestrateur l'ajoute dans `assemble()` sans passer par l'outliner ni le writer. Chaque template declare sa propre slide de conclusion — le code du pipeline reste generique.

**Fichiers** : `internal/plan/plan.go` (LoadClosingSlide), `internal/agent/orchestrator/orchestrator.go` (ClosingSlide field + assemble), `cmd/slidegen/agent.go` (wiring), `template/*/CLOSING_SLIDE` (config par template).

### 2. Selection du template sommaire via role des champs

**Probleme** : Le selector choisissait un template a cartes (slide 48) pour le sommaire au lieu du template dedie (slide 2) dont les champs ont le role `sommaire`. Le systeme de types n'a pas de `slideType: "sommaire"`.

**Solution** : Ajout du critere 10 dans le prompt selector : si le SlideNeed evoque un sommaire/agenda/structure, chercher en priorite les templates avec des champs role `sommaire`. Pas de nouveau slideType — le selector utilise la semantique des roles de champs existants.

**Fichiers** : `internal/agent/selector/prompt_selector.txt` (critere 10).

### 3. Numerotation coherente des section_dividers

**Probleme** : Les writers paralleles produisaient des formats incoherents ("01" vs "II") pour les numeros de section. Le reviewer le signalait a chaque iteration sans convergence.

**Solution** : Pre-calcul deterministe de la sequence de section_dividers dans `writeSlides()`. Chaque writer recoit un `ContentItem` `section_number=N` injecte avant les autres items. Le format de presentation (ex: "01", "02") est delegue au prompt writer (regle 9) et aux instructions specifiques du template (`PROMPT.md`). Le code du pipeline ne fait que passer le numero de sequence.

**Fichiers** : `internal/agent/orchestrator/orchestrator.go` (sectionDividerSeq map + injection), `internal/agent/writer/prompt_writer.txt` (regle 9 etendue), `template/*/PROMPT.md` (convention de format par template).

### 4. Classification structurelle des issues reviewer

**Probleme** : Le reviewer signalait des issues non-corrigeables par le writer (template trop petit, slide manquante, index hors bornes), declenchant des boucles de retry inutiles (13 retries pour 27 slides).

**Solution** : `handleReviewIssuesReturn()` filtre desormais 3 categories d'issues structurelles :
- `issue.SlideIndex` hors bornes du plan → skip avec warning
- `issue.IssueType == "wrong_template"` → deja filtre (existant)
- `issue.IssueType == "missing_content"` sans champ specifique (`Field == ""`) → skip comme structurel

Ces issues sont loguees mais ne declenchent pas de rewrite.

**Fichiers** : `internal/agent/orchestrator/orchestrator.go` (handleReviewIssuesReturn).

### 5. Validation maxChars des card titles dans le selector

**Probleme** : Le template 66 a des card titles de ~27-29 chars. Le mot "pollinisation" etait tronque a "pollinisati" car le champ etait trop petit. Aucune validation n'empechait cette selection.

**Solution** : `ValidateSelection()` verifie que quand `MaxItemLength > 0`, aucun champ du template selectionne n'a un `maxChars < 40` ET `maxChars < maxItemLength/2`. C'est un warning (pas une erreur bloquante) car le writer peut parfois reformuler. Le critere 11 est ajoute au prompt selector pour que le LLM fasse cette verification en amont.

**Fichiers** : `internal/agent/validate.go` (guard dans ValidateSelection), `internal/agent/selector/prompt_selector.txt` (critere 11).

## Consequences

- Le reviewer ne reclamera plus la slide de conclusion (eliminant 1 des 2 issues irresolubles)
- Les retries inutiles sur des problemes structurels sont evites, reduisant le nombre d'appels writer
- La numerotation des intercalaires est deterministe et coherente sans intervention du reviewer
- La selection de templates est mieux guidee pour les cas sommaire et les petits champs titre
- Aucune de ces corrections ne necessite de modification des types ou du schema JSON
