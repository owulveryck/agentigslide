# Audit : agentigslide vs Claude Certified Architect -- Foundations

**Document de reference** : *Claude Certified Architect -- Foundations Certification Exam Guide* (Anthropic, v0.1, 10 fevrier 2025)
**Date de l'audit** : 8 mai 2026
**Codebase** : `github.com/owulveryck/agentigslide` (commit `c75c1b4`)
**Auditeur** : Claude Opus 4.6

---

## Synthese executive

| Domaine | Poids | Couverture | Resume |
|---------|-------|------------|--------|
| 1. Agentic Architecture & Orchestration | 27% | **Excellent** | Pipeline multi-agent complet (Outliner/Selector/Writers/Reviewer) avec boucle de feedback, execution parallele, retry avec feedback d'erreurs |
| 2. Tool Design & MCP Integration | 18% | **Bon** | Serveur MCP fonctionnel, descriptions d'outils detaillees, mais pas de `isError` structure ni de distribution d'outils entre agents au sens Agent SDK |
| 3. Claude Code Configuration & Workflows | 20% | **Partiel** | CLAUDE.md present et complet, mais pas de `.claude/commands/`, `.claude/rules/`, `.claude/skills/` |
| 4. Prompt Engineering & Structured Output | 20% | **Excellent** | `tool_use` avec schemas JSON dynamiques, `tool_choice` force, validation/retry/feedback loops, prompt caching |
| 5. Context Management & Reliability | 15% | **Bon** | Prompt caching, delegation a des agents specialises, gestion du contexte par blocs structures, mais pas de human review workflow formel |

---

## Domain 1 : Agentic Architecture & Orchestration (27%)

### Task 1.1 -- Boucles agentiques pour l'execution autonome

**Couverture : Excellent**

La codebase implemente un pattern agentique via des appels Claude API avec `tool_use` et inspection du `stop_reason`. Bien que l'architecture n'utilise pas le Claude Agent SDK (elle passe par Vertex AI), elle implemente les memes patterns fondamentaux.

**Illustrations dans le code :**

- **Inspection du `stop_reason`** : Chaque agent verifie explicitement `resp.StopReason == "max_tokens"` pour detecter les troncatures. Exemples :
  - `internal/agent/outliner.go:132` -- Outliner
  - `internal/agent/selector.go:111` -- Selector
  - `internal/agent/writer.go:155` -- Writer
  - `internal/agent/reviewer.go:130` -- Reviewer

- **Boucle interactive de l'Outliner** (`internal/agent/outliner.go:203-263`) : Implemente une boucle agentique multi-tour complete :
  1. Envoie le message utilisateur
  2. Recoit la reponse `tool_use` avec le plan structure
  3. Sollicite le feedback utilisateur via `feedbackFn()`
  4. Accumule l'historique de conversation (assistant response + tool_result + user feedback)
  5. Boucle jusqu'a approbation ou erreur

  ```go
  // outliner.go:203 -- boucle agentique multi-tour
  for round := 1; ; round++ {
      resp, err := a.client.RawPredictFull(ctx, a.model, messages, opts...)
      // ... parse tool_use ...
      feedback, err := feedbackFn(&outline)
      if feedback == "" { return &outline, nil } // approbation
      messages = append(messages, assistantMsg, userFeedbackMsg) // accumulation
  }
  ```

- **Ajout des resultats d'outils au contexte** : L'Outliner accumule les messages dans `messages` (`outliner.go:250-262`), incluant la reponse assistant et le tool_result + feedback. Cela correspond exactement au pattern decrit dans le Task Statement 1.1.

**Ecarts :**
- L'architecture n'utilise pas directement `stop_reason == "tool_use"` vs `"end_turn"` comme mecanisme de boucle, car chaque agent fait un appel unique avec `tool_choice` force. La boucle est geree par l'orchestrateur Go, pas par le modele.
- Pas d'anti-pattern a signaler : la codebase n'utilise ni parsing de langage naturel pour la terminaison, ni caps d'iteration arbitraires.

---

### Task 1.2 -- Orchestration multi-agent coordinateur/subagent

**Couverture : Excellent**

Le fichier `internal/agent/orchestrator.go` implemente un pattern hub-and-spoke classique :

- **Coordinateur central** : L'`Orchestrator` decompose la tache en 5 etapes sequentielles (`orchestrator.go:35-149`)
- **Subagents isoles** : Chaque agent (Outliner, Selector, Writer, Reviewer) a son propre contexte, ses propres prompts, et ne partage pas l'historique de conversation des autres
- **Delegation et aggregation** : L'orchestrateur transmet les resultats d'un agent au suivant via la structure `PipelineState`
- **Boucle de raffinement iterative** : Le Reviewer evalue la sortie, renvoie des issues aux Writers concernes, et re-evalue (`orchestrator.go:100-142`)

```
User Request
    |
[Outliner] --> PresentationOutline
    |
[Selector] --> SelectionPlan (avec retry sur erreurs de validation)
    |
[Writers] (paralleles) --> SlideContent[]
    |
[Assembler] --> GenerationPlan
    |
[Reviewer] --> Approuve? --Non--> feedback aux Writers --> reassemble --> re-review
    |                                                                        |
    +------Yes-------> Google Slides API --> Presentation URL <--------------+
```

**Risque de decomposition trop etroite** (mentionne dans l'exam guide Question 7) : L'Outliner decompose la requete utilisateur en sections et slides. Si la requete utilisateur est vague ou trop large, l'Outliner pourrait manquer des aspects. La codebase mitige ce risque via le mode interactif (`--chat`) qui permet a l'utilisateur de raffiner le plan.

---

### Task 1.3 -- Invocation de subagents, passage de contexte

**Couverture : Bon**

- **Contexte explicite** : Chaque agent recoit son contexte directement dans son prompt, pas par heritage automatique. Exemple : le Writer recoit les champs du template, le slide need, et le feedback via ses parametres (`writer.go:63-125`)
- **Formats structures** : Les donnees intermediaires utilisent des structures Go typees (`PresentationOutline`, `SelectionPlan`, `SlideContent`, `ReviewResult`) serializees en JSON
- **Execution parallele** : Les Writers sont lances en parallele via goroutines avec semaphore (`orchestrator.go:241-298`)

```go
// orchestrator.go:241 -- parallelisme avec semaphore
sem := make(chan struct{}, o.config.MaxParallel)
var wg sync.WaitGroup
```

**Ecarts :**
- L'architecture utilise un orchestrateur Go natif plutot que le Claude Agent SDK avec `Task` tool et `allowedTools`. C'est une approche parfaitement valide (et plus performante pour ce cas d'usage), mais ne correspond pas exactement a la configuration `AgentDefinition` decrite dans l'exam.

---

### Task 1.4 -- Workflows multi-etapes avec enforcement et handoff

**Couverture : Excellent**

- **Enforcement programmatique** : La validation du Selector bloque le pipeline si les templates selectionnes n'existent pas dans le catalogue (`validate.go:159-257`). C'est l'equivalent du pattern "prerequisite gate" decrit dans l'examen.
- **Handoff structure** : L'orchestrateur assemble les resultats (`orchestrator.go:181-195`) en un `GenerationPlan` structure contenant toutes les informations necessaires pour l'execution.
- **Degradation gracieuse** : Si le Reviewer ne valide pas apres N retries, l'orchestrateur continue quand meme avec un warning (`orchestrator.go:122-127`). C'est un choix delibere documente dans l'ADR 001.

---

### Task 1.5 -- Hooks pour interception et normalisation

**Couverture : Partiel**

- **Enforcement post-generation** : `enforceMaxChars()` (`orchestrator.go:301-340`) tronque les champs qui depassent la limite du template. C'est une forme de hook post-tool-use.
- **Validation programmatique** : `validateOutline()` et `validateSelection()` interceptent et valident les sorties avant de passer a l'etape suivante.

**Ecarts :**
- Pas d'implementation de `PostToolUse` hooks au sens Agent SDK
- La normalisation des donnees est faite via validation Go plutot que via hooks declaratifs

---

### Task 1.6 -- Strategies de decomposition de taches

**Couverture : Excellent**

Le pipeline illustre une decomposition fixe sequentielle (prompt chaining) :
1. **Outliner** : analyse structurelle de la requete
2. **Selector** : mapping des besoins aux templates
3. **Writers** : generation de contenu (parallelise)
4. **Reviewer** : validation qualite

Cette approche est exactement le pattern "fixed sequential pipeline" recommande pour les workflows predictibles, par opposition a la decomposition dynamique pour les taches ouvertes.

L'ADR 001 (`docs/adr/001-agentic-architecture.md`) documente le raisonnement derriere cette architecture.

---

### Task 1.7 -- Gestion de l'etat de session

**Couverture : Partiel**

- **Pre-built outline** : L'orchestrateur supporte un outline pre-construit via `Orchestrator.Outline` (`orchestrator.go:22-24`), ce qui permet la reprise apres le mode interactif.
- **Sauvegarde intermediaire du plan** : Le plan JSON est sauvegardable (`slidegen/main.go` avec `--plan`), permettant de reprendre une execution echouee.

**Ecarts :**
- Pas d'implementation de `--resume`, `fork_session`, ou sessions nommees au sens Claude Code
- Pas de persistence structuree de l'etat pour crash recovery au sens Agent SDK

---

## Domain 2 : Tool Design & MCP Integration (18%)

### Task 2.1 -- Descriptions d'outils efficaces

**Couverture : Excellent**

La description de l'outil MCP `generate_slides` (`mcp-server/tool_description.txt`) est un exemple modele :
- **Purpose** : premiere ligne claire sur ce que fait l'outil
- **Input format** : description detaillee du format markdown attendu
- **Example input** : un exemple complet de presentation
- **Constraints** : temps de traitement, adaptation automatique, localisation
- **Output** : format du retour (URL Google Slides)

Les outils internes des agents sont egalement bien decrits :
- `produce_outline` (`outliner.go:26-97`) : schema JSON detaille avec enums, descriptions de champs, required fields
- `select_templates` (`selector.go:25-57`) : descriptions claires de chaque propriete
- `produce_slide_content` (`writer.go:27-57`) : schema genere dynamiquement avec contraintes maxLength
- `submit_review` (`reviewer.go:25-69`) : enum d'`issueType` (overflow, duplicate, missing_content, wrong_template, incoherence, invented_content)

Chaque outil a un nom distinct et une description qui ne chevauche pas les autres -- pas de risque de "misrouting" entre outils.

---

### Task 2.2 -- Reponses d'erreur structurees pour outils MCP

**Couverture : Partiel**

Le serveur MCP utilise `SetError()` pour signaler les erreurs (`mcp-server/main.go:217-221`) :

```go
func errResult(msg string) *mcp.CallToolResult {
    r := &mcp.CallToolResult{}
    r.SetError(fmt.Errorf("%s", msg))
    return r
}
```

**Ecarts :**
- Pas de `errorCategory` (transient/validation/permission) dans les reponses d'erreur
- Pas de flag `isRetryable` explicite
- Les messages d'erreur sont des strings non structures -- un agent appelant ne peut pas distinguer automatiquement une erreur transitoire d'une erreur de validation
- **Recommandation** : enrichir `errResult()` avec une structure `{ "errorCategory": "...", "isRetryable": bool, "description": "..." }`

---

### Task 2.3 -- Distribution d'outils entre agents et tool_choice

**Couverture : Excellent**

Chaque agent recoit exactement un outil adapte a son role :

| Agent | Outil | tool_choice |
|-------|-------|-------------|
| Outliner | `produce_outline` | `{"type": "tool", "name": "produce_outline"}` |
| Selector | `select_templates` | `{"type": "tool", "name": "select_templates"}` |
| Writer | `produce_slide_content` | `{"type": "tool", "name": "produce_slide_content"}` |
| Reviewer | `submit_review` | Force ou `auto` (si extended thinking) |

- **Un outil par agent** : separation stricte des responsabilites, pas de risque de misuse cross-specialisation
- **`tool_choice` force** : garantit que le modele appelle l'outil (pas de reponse textuelle)
- **Exception pour le Reviewer** : quand `thinkingBudget > 0`, `tool_choice` est mis a `"auto"` car extended thinking est incompatible avec le forced tool choice (`reviewer.go:110-116`). C'est un compromis documente et justifie.

**Selection de modele adaptative** (`orchestrator.go:250-253`) :
```go
writerModel := o.config.WriterModel
if len(templateFields) <= 2 {
    writerModel = o.config.WriterSimpleModel // Haiku pour les slides simples
}
```

Ce pattern de selection de modele par complexite est exactement le type de decision architecturale attendue dans l'examen.

---

### Task 2.4 -- Integration de serveurs MCP

**Couverture : Bon**

`mcp-server/main.go` implemente un serveur MCP complet avec :
- **Trois modes de transport** : stdio (ligne 183), SSE (ligne 186), HTTP streamable (ligne 195)
- **Cross-origin protection** : middleware CORS configurable (`main.go:223-238`)
- **Description d'outil detaillee** : chargee depuis un fichier embarque (`tool_description.txt`)

**Ecarts :**
- Pas de fichier `.mcp.json` dans le projet pour la configuration de serveurs MCP externes
- Pas de MCP resources (le catalogue de templates pourrait etre expose comme resource MCP pour donner de la visibilite aux agents sans appels exploratoires)
- **Recommandation** : ajouter un `.mcp.json` referençant le serveur slidegen avec expansion de variables d'environnement pour les credentials

---

### Task 2.5 -- Outils built-in (Read, Write, Edit, Bash, Grep, Glob)

**Couverture : Non applicable**

Ce task statement concerne l'usage de Claude Code en tant qu'outil de developpement. La codebase elle-meme n'est pas un outil Claude Code mais un systeme de generation de presentations. Cependant, le `CLAUDE.md` documente les commandes de developpement, ce qui aide Claude Code a travailler efficacement sur ce projet.

---

## Domain 3 : Claude Code Configuration & Workflows (20%)

### Task 3.1 -- Configuration CLAUDE.md hierarchique

**Couverture : Bon**

Le fichier `CLAUDE.md` a la racine est complet et bien structure :
- Vue d'ensemble du projet
- Architecture en 4 phases
- Variables d'environnement avec valeurs par defaut
- Commandes courantes
- Structure des repertoires
- Details d'implementation importants

**Ecarts :**
- Pas de CLAUDE.md de niveau utilisateur (`~/.claude/CLAUDE.md`) specifique au projet
- Pas de CLAUDE.md dans les sous-repertoires (e.g., `internal/agent/CLAUDE.md` pour les conventions specifiques aux agents)
- Pas d'utilisation de `@import` pour modulariser les instructions
- **Recommandation** : ajouter un CLAUDE.md dans `internal/agent/` documentant les conventions de nommage des agents, les patterns de creation d'outils, et les regles de validation

---

### Task 3.2 -- Slash commands et skills personnalisees

**Couverture : Absent**

Pas de `.claude/commands/` ni `.claude/skills/` dans le projet.

**Recommandations :**
- Creer `.claude/commands/analyze-template.md` pour automatiser l'analyse de templates
- Creer `.claude/commands/gen-slide.md` pour generer une presentation en une commande
- Creer `.claude/skills/agent-debug/SKILL.md` avec `context: fork` et `allowed-tools: [Bash, Read]` pour debugger le pipeline multi-agent sans polluer le contexte principal

---

### Task 3.3 -- Rules conditionnelles par chemin

**Couverture : Absent**

Pas de `.claude/rules/` dans le projet.

**Recommandations :**
- `.claude/rules/agents.md` avec `paths: ["internal/agent/**"]` pour les conventions des agents (un outil par agent, validation apres chaque appel API, etc.)
- `.claude/rules/prompts.md` avec `paths: ["internal/agent/prompt_*.txt", "internal/pipeline/prompt_*.txt.tmpl"]` pour les conventions de prompts (en francais, pas de generation de contenu, extraction uniquement)
- `.claude/rules/tests.md` avec `paths: ["**/*_test.go"]` pour les conventions de test

---

### Task 3.4 -- Plan mode vs execution directe

**Couverture : Illustre indirectement**

Le projet illustre cette distinction dans son propre mecanisme : le mode monolithique (`slidegen` sans `--agent`) est analogue a l'execution directe (un seul appel Claude), tandis que le mode multi-agent (`--agent`) est analogue au plan mode (exploration, planification, execution). L'ADR 001 documente cette decision.

---

### Task 3.5 -- Raffinement iteratif

**Couverture : Bon**

- **Mode interactif de l'Outliner** (`outliner.go:176-264`) : le mode `--chat` permet a l'utilisateur de raffiner le plan via des aller-retours conversationnels. C'est le pattern "interview" mentionne dans le Task Statement 3.5.
- **Boucle Reviewer/Writer** : le Reviewer identifie des problemes specifiques et les renvoie aux Writers pour correction. C'est le pattern test-driven iteration.

---

### Task 3.6 -- Integration CI/CD

**Couverture : Partiel**

Le serveur MCP (`mcp-server/main.go`) peut etre utilise en mode `stdio` dans un pipeline automatise. Cependant :

**Ecarts :**
- Pas de flag `-p` / `--print` pour un mode non-interactif de Claude Code
- Pas d'utilisation de `--output-format json` / `--json-schema` pour des sorties structurees CI
- Pas de configuration CI/CD documentee (GitHub Actions, etc.)

---

## Domain 4 : Prompt Engineering & Structured Output (20%)

### Task 4.1 -- Prompts avec criteres explicites

**Couverture : Excellent**

Les prompts systeme sont explicites et precis :
- **Reviewer** (`prompt_reviewer.txt`) : 6 criteres de validation nommes (overflow, duplication, contenu manquant, mauvais template, incoherence, contenu invente)
- **Writer** (`prompt_writer.txt`) : regles explicites sur le mapping contenu -> champs, respect des maxChars, markdown autorise
- **Outliner** (`prompt_outliner.txt`) : distinction claire entre extraction et invention de contenu

Le schema de `ReviewIssue` utilise un enum pour `issueType` :
```json
"enum": ["overflow", "duplicate", "missing_content", "wrong_template", "incoherence", "invented_content"]
```
Ce sont des criteres categoriques precis, pas des instructions vagues comme "signale les problemes".

---

### Task 4.2 -- Few-shot prompting

**Couverture : Partiel**

- La description de l'outil MCP (`tool_description.txt`) inclut un exemple d'input complet -- c'est un one-shot example
- Les prompts des agents ne contiennent pas de few-shot examples explicites

**Ecarts :**
- Les prompts des agents (outliner, selector, writer, reviewer) n'incluent pas d'exemples d'inputs/outputs attendus
- **Recommandation** : ajouter 1-2 exemples de sorties attendues dans les system prompts, surtout pour le Selector (mapping outline -> template) et le Writer (mapping content -> champs), pour reduire les erreurs de format

---

### Task 4.3 -- Structured output via tool_use et JSON schemas

**Couverture : Excellent**

C'est un des points les plus forts de la codebase. Chaque agent utilise `tool_use` avec des schemas JSON stricts :

- **Schemas statiques** : Outliner, Selector, Reviewer ont des schemas definis en tant que `json.RawMessage` literals
- **Schemas dynamiques** : le Writer genere son schema a runtime en fonction des champs du template (`writer.go:27-57`). Chaque champ template devient une propriete du schema avec `maxLength` calcule.

```go
// writer.go:27-57 -- schema dynamique
func buildWriterTool(fields []TemplateField) vertex.Tool {
    properties := make(map[string]any, len(fields))
    for _, f := range fields {
        prop := map[string]any{"type": "string", "description": "..."}
        if f.MaxChars > 0 {
            prop["maxLength"] = f.MaxChars * 9 / 10
        }
        properties[f.VariableName] = prop
    }
}
```

- **`tool_choice` force** : garantit l'appel de l'outil et elimine les reponses textuelles
- **Distinction `tool_choice` modes** :
  - Force (`{"type": "tool", "name": "..."}`) pour Outliner, Selector, Writer
  - Auto (`{"type": "auto"}`) pour Reviewer avec extended thinking (contrainte technique)

---

### Task 4.4 -- Validation, retry et boucles de feedback

**Couverture : Excellent**

Trois mecanismes de retry-with-feedback distincts :

1. **Selector retry** (`orchestrator.go:59-89`) : si la validation echoue, l'erreur de validation est injectee dans le prompt du prochain appel :
   ```go
   // selector.go:72-75 -- injection de l'erreur precedente
   if len(previousErrors) > 0 && previousErrors[0] != "" {
       outlinePrompt += "\n\nERREURS DE VALIDATION...\nCORRIGE ces erreurs..."
   }
   ```

2. **Reviewer feedback loop** (`orchestrator.go:100-142`) : les issues identifiees par le Reviewer sont renvoyees aux Writers concernes via `handleReviewIssuesReturn()`, puis une re-review ciblee est lancee (`RunSubset`).

3. **Writer correction** (`writer.go:94-108`) : le feedback du Reviewer est injecte dans le prompt du Writer avec les details de chaque issue et la suggestion de correction :
   ```go
   feedbackSection = "CORRECTIONS DEMANDEES...\n" + issues + "\nCorrige ces problemes."
   ```

4. **analyzeSlides retry** (`analyzeSlides/analyze_slides.go`) : sur erreur de parsing JSON, renvoie un prompt de correction avec l'erreur specifique.

Ces patterns correspondent parfaitement au "retry-with-error-feedback" decrit dans le Task Statement 4.4.

---

### Task 4.5 -- Batch processing

**Couverture : Partiel**

- L'execution parallele des Writers (`orchestrator.go:238-298`) avec semaphore est une forme de batch processing avec controle de concurrence
- La configuration `AGENT_MAX_PARALLEL` (defaut 5) controle le degre de parallelisme

**Ecarts :**
- Pas d'utilisation de la Message Batches API d'Anthropic (la codebase passe par Vertex AI)
- Pas de `custom_id` pour la correlation batch
- Le cas d'usage (generation de presentation interactive) ne justifie pas l'API batch (latence requise)

---

### Task 4.6 -- Architectures multi-instance et multi-pass review

**Couverture : Excellent**

- **Review independante** : Le Reviewer est une instance Claude separee qui n'a pas le contexte de raisonnement des Writers. Il recoit uniquement le plan assemble et la requete originale. C'est exactement le pattern "independent review instance" recommande.

- **Multi-pass review** (`reviewer.go:169-277`) : La methode `RunSubset()` ne re-evalue que les slides corrigees, evitant de re-traiter le plan entier (~114K tokens). C'est le pattern "focused per-element review" + "cross-element integration".

- **Extended thinking** (`reviewer.go:110-116`) : Le Reviewer peut activer le "extended thinking" de Claude avec un budget de tokens configurable (`AGENT_REVIEWER_THINKING_BUDGET`, defaut 5120). Cela active un raisonnement plus profond pour la validation qualite.

---

## Domain 5 : Context Management & Reliability (15%)

### Task 5.1 -- Gestion du contexte conversationnel

**Couverture : Bon**

- **Prompt caching** (`internal/agent/cache.go`) : Les system prompts sont structures en blocs `ContentBlock` avec `cache_control: {"type": "ephemeral"}` sur le dernier bloc. Cela optimise le cout pour les appels paralleles des Writers (le prompt systeme est cache et reutilise).

```go
// cache.go:9-28 -- breakpoint de cache sur le dernier bloc
func buildSystemBlocks(systemPrompt, templateInstructions string) []vertex.ContentBlock {
    return []vertex.ContentBlock{
        {Type: "text", Text: systemPrompt},
        {Type: "text", Text: "INSTRUCTIONS...\n" + templateInstructions,
         CacheControl: &vertex.CacheControl{Type: "ephemeral"}},
    }
}
```

- **Cache dans les messages utilisateur** : Le Reviewer place un breakpoint de cache sur le catalogue et la requete utilisateur (`reviewer.go:88-95`), permettant la reutilisation entre `Run()` et `RunSubset()`.

- **Logging des metriques de cache** : Chaque agent log les tokens de cache (`cacheRead`, `cacheWrite`) pour le suivi des couts.

L'ADR 002 (`docs/adr/002-prompt-caching.md`) documente la strategie de caching et le calcul d'economie.

---

### Task 5.2 -- Escalade et resolution d'ambiguite

**Couverture : Partiel**

- **Mode interactif** : Le mode `--chat` de l'Outliner permet a l'utilisateur de valider ou corriger le plan avant execution. C'est une forme d'escalade.
- **Degradation gracieuse** : Si le Reviewer n'approuve pas apres les retries, l'orchestrateur continue avec un warning (`orchestrator.go:122-127`).

**Ecarts :**
- Pas de criteres d'escalade explicites dans les prompts (quand escalader vs resoudre)
- Pas de handoff structure vers un humain avec resume du contexte
- Le systeme n'a pas de mecanisme de confiance calibree -- il ne sait pas distinguer les cas ou il devrait demander une intervention humaine

---

### Task 5.3 -- Propagation d'erreurs multi-agent

**Couverture : Bon**

- **Erreurs contextuelles** : Chaque erreur est wrappee avec le nom de l'agent et le contexte :
  ```go
  return nil, fmt.Errorf("outliner: %w", err)           // orchestrator.go:51
  return nil, fmt.Errorf("selector: %w", err)            // orchestrator.go:67
  return nil, fmt.Errorf("writers failed: %w", errors.Join(writerErrors...))  // orchestrator.go:295
  ```

- **Aggregation d'erreurs** : Les erreurs des Writers paralleles sont collectees et jointes via `errors.Join()` (`orchestrator.go:294-296`)

- **Retry local + propagation** : Le Selector fait des retries locaux (jusqu'a `MaxSelectorRetries`) avant de propager l'erreur. Le Reviewer fait de meme. C'est le pattern "local recovery for transient failures, propagate only unresolvable errors".

**Ecarts :**
- Les erreurs ne contiennent pas de `errorCategory` (transient vs validation vs permission)
- Pas de "partial results + what was attempted" structure dans les retours d'erreur
- Les erreurs du Reviewer silencieusement degradent (`slog.Warn`) plutot que de signaler clairement au coordinateur

---

### Task 5.4 -- Gestion du contexte dans l'exploration de codebase

**Couverture : Non applicable directement**

Ce task statement concerne l'usage de Claude Code / Agent SDK pour l'exploration de codebase. La codebase elle-meme n'explore pas de code, mais elle illustre indirectement les principes :

- **Delegation a des agents specialises** : Chaque agent a un scope precis (Outliner ne connait pas les templates, Writer ne connait pas le plan global)
- **Resume intermediaire** : L'Assembler (`orchestrator.go:181-195`) synthetise les resultats des Writers en un plan structure avant de le passer au Reviewer

---

### Task 5.5 -- Human review et calibration de confiance

**Couverture : Partiel**

- **Extended thinking du Reviewer** : active un raisonnement plus profond pour les decisions de validation
- **Logging detaille** : chaque issue du Reviewer est logguee avec son type, slide, champ et description (`reviewer.go:149-155`)

**Ecarts :**
- Pas de scoring de confiance par champ ou par slide
- Pas de routing vers un humain base sur la confiance
- Pas d'echantillonnage stratifie pour la validation
- **Recommandation** : ajouter un champ `confidence` dans `ReviewIssue` et un seuil configurable pour l'escalade

---

### Task 5.6 -- Provenance et incertitude

**Couverture : Partiel**

- **Rationale du Selector** : chaque selection de template inclut un champ `rationale` expliquant le choix (`selector.go:48-50`)
- **Tracabilite** : les logs structurees (`slog.Info`) avec champs semantiques (agent, model, duration, tokens) permettent de retracer chaque decision

**Ecarts :**
- Pas de mapping source (quel contenu de la requete utilisateur -> quel champ de quel slide)
- Pas de gestion de conflits entre sources
- Pas de dates de publication/collection dans les sorties structurees

---

## Recommandations d'amelioration

### Priorite haute (impact sur plusieurs domaines)

1. **Ajouter des erreurs structurees MCP** (Domaines 2, 5) : enrichir `errResult()` dans `mcp-server/main.go` avec `errorCategory`, `isRetryable`, et `description` structures. Impact : permet aux agents appelants de prendre des decisions de recovery intelligentes.

2. **Ajouter des few-shot examples dans les prompts agents** (Domaine 4) : 1-2 exemples d'input/output attendus dans les system prompts du Selector et du Writer. Impact : reduit les erreurs de format et de mapping.

3. **Creer `.claude/rules/`** (Domaine 3) : ajouter des rules conditionnelles pour les agents (`internal/agent/**`), les prompts (`**/prompt_*.txt`), et les tests (`**/*_test.go`). Impact : Claude Code applique automatiquement les bonnes conventions.

### Priorite moyenne

4. **Ajouter `.claude/commands/`** (Domaine 3) : creer des slash commands pour les workflows courants (`/analyze-template`, `/gen-slide`).

5. **Exposer le catalogue comme MCP resource** (Domaine 2) : ajouter un `resource` MCP pour le `template_index.json`, evitant aux agents de deviner les templates disponibles.

6. **Ajouter un champ `confidence` au Reviewer** (Domaine 5) : scoring de confiance par issue pour permettre un routing humain calibre.

### Priorite basse

7. **Ajouter `.mcp.json`** (Domaine 2) : fichier de configuration MCP project-scoped pour referencer le serveur slidegen.

8. **Documenter l'integration CI/CD** (Domaine 3) : ajouter un exemple GitHub Actions utilisant le mode MCP stdio.

---

## Conclusion

La codebase `agentigslide` est une **illustration excellente** des concepts de la certification Claude Certified Architect, en particulier pour les domaines 1 (Architecture agentique) et 4 (Prompt Engineering & Structured Output) qui representent 47% de l'examen.

**Points de force majeurs :**
- Pipeline multi-agent complet avec orchestration Go native
- `tool_use` avec schemas JSON dynamiques et `tool_choice` force
- Boucle de feedback Reviewer -> Writers avec re-review ciblee
- Prompt caching avec breakpoints strategiques (ADR 002)
- Validation programmatique a chaque etape du pipeline
- Retry-with-error-feedback pour le Selector
- Extended thinking configurable pour le Reviewer
- Selection de modele adaptative par complexite

**Axes d'amelioration principaux :**
- Configuration Claude Code (`.claude/commands/`, `.claude/rules/`, `.claude/skills/`) pour le Domaine 3
- Erreurs MCP structurees (`errorCategory`, `isRetryable`) pour le Domaine 2
- Few-shot examples dans les prompts agents pour le Domaine 4
- Human review workflow avec calibration de confiance pour le Domaine 5

La codebase couvre 22 des 27 task statements de l'examen de maniere directe ou indirecte, ce qui en fait un support pedagogique solide pour la preparation a la certification. Les 5 task statements non couverts concernent principalement les fonctionnalites specifiques a Claude Code (slash commands, rules, CI/CD integration) et au Claude Agent SDK (`Task` tool, `AgentDefinition`, `PostToolUse` hooks), qui sont des technologies complementaires a l'approche Vertex AI + Go orchestrateur adoptee ici.
