# ADR 007 : Architecture A2A (Agent-to-Agent) pour agentigslide

- **Date** : 2026-05-09
- **Statut** : RFC
- **Decideurs** : Olivier Wulveryck

## Contexte et motivation

### L'architecture actuelle : un pipeline Go linéaire

Aujourd'hui, agentigslide est un **pipeline monolithique orchestré par du code Go pur**. Les agents (Outliner, Selector, Writers, Reviewer) sont des fonctions Go appelées séquentiellement par un orchestrateur central. La communication inter-agents est un passage de structures Go en mémoire, dans un processus unique.

```plantuml
@startuml
!include https://raw.githubusercontent.com/plantuml-stdlib/C4-PlantUML/master/C4_Context.puml

title Contexte actuel — agentigslide comme outil CLI/MCP

Person(consultant, "Consultant", "Rédige un brief markdown\nValide les slides générées")
Person(commteam, "Equipe communication", "Maintient le catalogue\nGoogle Slides natif")

System(agentigslide, "agentigslide", "Génère des présentations Google Slides\nà partir d'un brief markdown")
System_Ext(googleslides, "Google Slides / Drive API", "Production des slides\nStockage du catalogue")
System_Ext(vertexai, "Vertex AI / Claude", "Inférence LLM\nOpus / Sonnet / Haiku")
System_Ext(mcpclient, "Client MCP", "Claude Code ou autre\nclient LLM compatible")

Rel(consultant, agentigslide, "Brief markdown", "CLI / MCP")
Rel(mcpclient, agentigslide, "generate_slides()", "MCP stdio/SSE")
Rel(commteam, googleslides, "Crée et maintient\nle catalogue", "Google Slides natif")
Rel(agentigslide, googleslides, "Lit le catalogue\nProduit la présentation", "REST API")
Rel(agentigslide, vertexai, "Appels LLM\npar agent", "REST API")

@enduml
```

```plantuml
@startuml
!include https://raw.githubusercontent.com/plantuml-stdlib/C4-PlantUML/master/C4_Container.puml

title Architecture actuelle — Conteneurs agentigslide

Person(consultant, "Consultant")
Person_Ext(mcpclient, "Client MCP")

System_Boundary(agentigslide, "agentigslide") {
  Container(cli, "CLI / MCP Server", "Go binary", "Point d'entrée — reçoit le brief\nexpose generate_slides via MCP")
  Container(orchestrator, "Orchestrateur Go", "Go — orchestrator.go", "Coordinateur déterministe\nAppelle les agents en séquence\nGère les retries et le feedback")
  Container(outliner, "Outliner", "Go + Claude Sonnet", "Analyse le brief\nProduit PresentationOutline")
  Container(selector, "Selector", "Go + Claude Sonnet", "Mappe les besoins\naux templates du catalogue")
  Container(writers, "Writers", "Go + Haiku/Sonnet\nGoroutines parallèles", "Génère le contenu\nde chaque slide")
  Container(reviewer, "Reviewer", "Go + Claude Opus\n+ extended thinking", "Valide la cohérence\ndu plan assemblé")
  Container(executor, "Executor", "Go — pipeline.go", "Applique le plan\nvia Google APIs")
  Container(fixfonts, "FixFonts", "Go + Claude Opus", "Post-production\ncorrection formatage")
  ContainerDb(templateindex, "Index sémantique", "JSON — template_index.json", "Catalogue indexé\nObjectIDs, capacités, rôles")
}

System_Ext(googleapi, "Google Slides / Drive API", "Production")
System_Ext(vertexai, "Vertex AI", "Inférence LLM")

Rel(consultant, cli, "Brief markdown", "CLI flag --file")
Rel(mcpclient, cli, "generate_slides()", "MCP")
Rel(cli, orchestrator, "Lance le pipeline", "Go func call")
Rel(orchestrator, outliner, "userRequest", "Go func call")
Rel(orchestrator, selector, "PresentationOutline", "Go func call")
Rel(orchestrator, writers, "SelectionPlan\n(fan-out parallèle)", "Go goroutines")
Rel(orchestrator, reviewer, "GenerationPlan", "Go func call")
Rel(orchestrator, executor, "PresentationPlan", "Go func call")
Rel(executor, fixfonts, "URL présentation", "Go func call")
Rel(outliner, templateindex, "Charge l'index", "filesystem read")
Rel(selector, templateindex, "Cherche templates", "filesystem read")
Rel(outliner, vertexai, "Appel Claude Sonnet", "REST")
Rel(selector, vertexai, "Appel Claude Sonnet", "REST")
Rel(writers, vertexai, "Appels Haiku/Sonnet\n(parallèles)", "REST")
Rel(reviewer, vertexai, "Appel Claude Opus\n+ extended thinking", "REST")
Rel(fixfonts, vertexai, "Appel Claude Opus", "REST")
Rel(executor, googleapi, "DuplicateObject\nBatchUpdate\nDeleteObject", "REST")
Rel(fixfonts, googleapi, "Export PDF\nBatchUpdate", "REST")

@enduml
```

### Les limites structurelles du pipeline Go

Le pipeline Go actuel a trois limites fondamentales qui ne peuvent pas être résolues sans changer le paradigme architectural :

**Limite 1 — Les agents ne peuvent pas orchestrer d'autres agents.**
Le Selector ne peut pas décider dynamiquement d'appeler un agent de layout puis un agent de design. Il peut seulement retourner un résultat à l'orchestrateur, qui décide ensuite. Toute logique de branchement doit être codée dans l'orchestrateur — ce qui couple l'orchestrateur à la logique métier de chaque agent.

**Limite 2 — Le pipeline est fermé à l'extension externe.**
Ajouter un nouvel agent (agent de recherche pour le Writer, agent de validation visuelle pour le Selector) requiert de modifier le binaire Go, de recompiler, de redéployer. Il n'y a pas de mécanisme de découverte ou de composition dynamique.

**Limite 3 — L'unité de déploiement est le binaire entier.**
Si le Reviewer doit être mis à jour (nouveau modèle, nouveau prompt, nouveau comportement), c'est tout le binaire qui est recompilé et redéployé. Il n'y a pas de cycle de vie indépendant par agent.

---

## Le changement de paradigme : de pipeline à réseau d'agents

### Ce qu'est A2A

Le protocole **Agent-to-Agent (A2A)** est un standard émergent (Google, mai 2025) qui définit comment des agents LLM s'exposent, se découvrent, et se composent. Chaque agent expose une **Agent Card** (capacités, schémas d'entrée/sortie, endpoint) et accepte des **Tasks** via une API REST standardisée.

```plantuml
@startuml
!include https://raw.githubusercontent.com/plantuml-stdlib/C4-PlantUML/master/C4_Container.puml

title Protocole A2A — Structure d'un agent

System_Boundary(agent, "Agent A2A") {
  Container(agentcard, "Agent Card", "JSON — /.well-known/agent.json", "Nom, description, capacités\nSchémas entrée/sortie\nEndpoint, auth")
  Container(taskhandler, "Task Handler", "REST endpoint /tasks", "Reçoit les Tasks\nRetourne TaskResult\nSupporte streaming SSE")
  Container(llm, "LLM Core", "Claude / GPT / Gemini...", "Le modèle sous-jacent\nRemplaçable indépendamment")
  Container(tools, "Tools / Sub-agents", "MCP / A2A calls", "Outils et sous-agents\nque cet agent peut appeler")
}

System_Ext(orchestrator, "Orchestrateur\n(client A2A)", "Découvre et appelle\nles agents via leur card")

Rel(orchestrator, agentcard, "GET /.well-known/agent.json", "HTTP")
Rel(orchestrator, taskhandler, "POST /tasks\n{input, context}", "HTTP / SSE")
Rel(taskhandler, llm, "Inference", "SDK")
Rel(taskhandler, tools, "Tool calls", "MCP / A2A")

@enduml
```

### La nouvelle topologie : deux niveaux d'orchestration

Avec A2A, agentigslide adopte une **architecture hiérarchique** :

- **Niveau 1** : l'orchestrateur Go reste — déterministe, prévisible — mais il appelle des agents via A2A plutôt que des fonctions Go.
- **Niveau 2** : chaque agent principal peut lui-même orchestrer des sous-agents A2A pour accomplir sa tâche.

```plantuml
@startuml
!include https://raw.githubusercontent.com/plantuml-stdlib/C4-PlantUML/master/C4_Container.puml

title Architecture cible — agentigslide avec A2A

Person(consultant, "Consultant")
Person_Ext(mcpclient, "Client MCP externe\n(Claude Code, etc.)")
Person(commteam, "Equipe communication")

System_Boundary(agentigslide, "agentigslide — réseau d'agents") {

  Container(orchestrator, "Orchestrateur Go", "Go — orchestrator.go", "Coordinateur déterministe\nNiveau 1 — appelle les agents\nvia protocole A2A")

  Container(mcpserver, "Serveur MCP/A2A", "Go", "Façade unifiée\nExpose generate_slides\nDécouvre les agents via Agent Cards")

  System_Boundary(agents_n1, "Agents Niveau 1") {
    Container(outliner, "Agent Outliner", "Go + A2A server\nClaude Sonnet", "Analyse structurelle\ndu brief")
    Container(selector, "Agent Selector", "Go + A2A server\nClaude Sonnet", "Matching besoins/templates\nOU déclenche fallback créatif")
    Container(writers, "Agent Writers", "Go + A2A server\nHaiku / Sonnet", "Génération de contenu\npar slide")
    Container(reviewer, "Agent Reviewer", "Go + A2A server\nClaude Opus + thinking", "Validation cohérence\nglobale du plan")
  }

  System_Boundary(agents_n2_fallback, "Agents Niveau 2 — Fallback créatif\n(orchestrés par Selector)") {
    Container(layoutagent, "Agent Layout", "Go + A2A server\nClaude Sonnet", "Structure spatiale\nzones et proportions")
    Container(designagent, "Agent Design", "Go + A2A server\nClaude Sonnet + Vision", "Instancie formes\ncoleurs, typographies")
    Container(visualvalidator, "Agent Validation\nVisuelle", "Go + A2A server\nClaude Opus Vision", "Vérifie le respect\nde la charte OCTO")
  }

  System_Boundary(agents_n2_writers, "Agents Niveau 2 — Writers enrichis\n(orchestrés par Writers)") {
    Container(researchagent, "Agent Recherche", "Go + A2A server", "Enrichit le contenu\navec données externes")
    Container(reformulagent, "Agent Reformulation", "Go + A2A server\nClaude Haiku", "Reformule selon\ncontraintes de style")
  }

  ContainerDb(templateindex, "Index sémantique", "JSON + API REST", "Slides, primitives\ncharte, règles")
  ContainerDb(charterepo, "Charte formalisée", "JSON versionné", "Règles layout\ntypographies, palettes")
  ContainerDb(primitives, "Primitives de design", "Google Slides IDs", "Formes, icônes\nzones réutilisables")
}

System_Ext(googleapi, "Google Slides / Drive API", "Production")
System_Ext(vertexai, "Vertex AI / Claude", "Inférence LLM")
System_Ext(extcatalogue, "Catalogues externes\n(autres cabinets)", "Agents Selector\nexternes via A2A")

Rel(consultant, mcpserver, "Brief markdown", "MCP / HTTP")
Rel(mcpclient, mcpserver, "generate_slides()", "MCP")
Rel(mcpserver, orchestrator, "Lance le pipeline", "Go func / A2A")
Rel(commteam, charterepo, "Maintient la charte", "Git / API")
Rel(commteam, primitives, "Maintient les primitives", "Google Slides")

Rel(orchestrator, outliner, "Task: analyser le brief", "A2A")
Rel(orchestrator, selector, "Task: sélectionner templates", "A2A")
Rel(orchestrator, writers, "Task: générer contenu slide N", "A2A (fan-out)")
Rel(orchestrator, reviewer, "Task: valider le plan", "A2A")

Rel(selector, templateindex, "Cherche templates", "REST")
Rel(selector, layoutagent, "Task: définir layout", "A2A")
Rel(selector, designagent, "Task: instancier formes", "A2A")
Rel(selector, visualvalidator, "Task: valider charte", "A2A")

Rel(writers, researchagent, "Task: enrichir contenu", "A2A")
Rel(writers, reformulagent, "Task: reformuler", "A2A")

Rel(designagent, charterepo, "Lit les règles", "REST")
Rel(designagent, primitives, "Sélectionne primitives", "REST")
Rel(designagent, googleapi, "Crée les formes", "REST")
Rel(layoutagent, charterepo, "Lit les règles layout", "REST")
Rel(visualvalidator, googleapi, "Export PNG pour vision", "REST")

Rel(outliner, vertexai, "Appel Claude", "REST")
Rel(selector, vertexai, "Appel Claude", "REST")
Rel(writers, vertexai, "Appel Claude", "REST")
Rel(reviewer, vertexai, "Appel Claude Opus", "REST")
Rel(layoutagent, vertexai, "Appel Claude", "REST")
Rel(designagent, vertexai, "Appel Claude Vision", "REST")
Rel(visualvalidator, vertexai, "Appel Claude Vision", "REST")

Rel(orchestrator, googleapi, "Produit la présentation", "REST")
Rel(selector, extcatalogue, "Fallback: catalogue externe", "A2A")

@enduml
```

---

## Le cas déclencheur : le fallback créatif du Selector

### Aujourd'hui : le Selector est borné par le catalogue

```plantuml
@startuml
title Séquence actuelle — Selector sans fallback

participant Orchestrateur as O
participant Selector as S
database "Index sémantique" as IDX
participant "Claude Sonnet" as LLM

O -> S : SelectionPlan(outline, catalogue)
S -> IDX : Cherche templates matching
IDX --> S : Templates disponibles
S -> LLM : Mappe besoins → templates
LLM --> S : SelectionPlan (best effort)
note right of S
  Si aucun template ne convient :
  Claude choisit "le moins mauvais"
  Le Reviewer peut détecter le problème
  mais ne peut pas y remédier
end note
S --> O : SelectionPlan (potentiellement dégradé)
O -> O : Reviewer détecte "template inadéquat"
O -> S : Retry avec feedback
S -> LLM : Retry (même catalogue, même résultat)
LLM --> S : Même SelectionPlan dégradé
S --> O : SelectionPlan dégradé (max retries atteint)

@enduml
```

### Avec A2A : le Selector orchestre un fallback créatif

```plantuml
@startuml
title Séquence cible — Selector avec fallback créatif via A2A

participant Orchestrateur as O
participant "Agent Selector\n(A2A)" as S
database "Index sémantique" as IDX
participant "Agent Layout\n(A2A)" as AL
participant "Agent Design\n(A2A)" as AD
participant "Agent Validation\nVisuelle (A2A)" as AV
database "Charte formalisée" as CH
database "Primitives" as PR
participant "Google Slides API" as GS
participant "Boucle enrichissement" as BE

O -> S : Task{outline, catalogue}
S -> IDX : Cherche templates matching
IDX --> S : Aucun template satisfaisant
note right of S
  Score de confiance < seuil
  Le Selector décide de créer
  sans remonter à l'orchestrateur
end note

S -> AL : Task{SlideNeed, charte_url}
AL -> CH : GET /rules/layout
CH --> AL : Règles layout (grilles, zones)
AL --> S : LayoutSpec{zones, proportions}

S -> AD : Task{LayoutSpec, charte_url, primitives_url}
AD -> CH : GET /rules/design
AD -> PR : GET /primitives?type=icon,text,shape
PR --> AD : Primitives disponibles
AD -> GS : CreateShape, InsertTextBox...
GS --> AD : ObjectIDs créés
AD --> S : SlideSpec{objectIds, screenshot_url}

S -> AV : Task{SlideSpec, charte_url}
AV -> GS : Export PNG slide
GS --> AV : Image PNG
AV -> AV : Vision — vérifie la charte
alt Charte respectée
  AV --> S : ValidationResult{approved: true}
  S -> BE : Signal{slideSpec, gap_detected: true}
  BE -> BE : Propose à l'équipe communication
else Charte non respectée
  AV --> S : ValidationResult{approved: false, issues: [...]}
  S -> AD : Task{LayoutSpec, issues} [correction]
  AD --> S : SlideSpec corrigée
  S -> AV : Task{SlideSpec corrigée}
  AV --> S : ValidationResult{approved: true}
end

S --> O : SelectionPlan{sourceSlide: "created", objectIds: [...]}

@enduml
```

---

## Impact sur les composants existants

### L'index sémantique doit évoluer

Aujourd'hui l'index référence uniquement des slides complètes. Avec A2A et le fallback créatif, il doit référencer trois types d'actifs :

```plantuml
@startuml
title Structure cible de l'index sémantique

package "Index sémantique v2" {
  class SlideEntry {
    slideNumber: int
    slideId: string
    intention: string
    keywords: []string
    editableFields: []Field
    confidence: float
  }

  class PrimitiveEntry {
    primitiveId: string
    type: icon | shape | textbox | image
    objectId: string
    tags: []string
    charteCompliant: bool
  }

  class CharteRule {
    ruleId: string
    category: layout | typography | color | spacing
    constraint: string
    validValues: []string
    testable: bool
  }

  class TemplateIndex {
    templateId: string
    version: string
    slides: []SlideEntry
    primitives: []PrimitiveEntry
    charteRules: []CharteRule
  }

  TemplateIndex "1" *-- "N" SlideEntry
  TemplateIndex "1" *-- "N" PrimitiveEntry
  TemplateIndex "1" *-- "N" CharteRule
}

@enduml
```

### La charte doit devenir explicite

C'est le chantier le plus critique — et le plus sous-estimé. Aujourd'hui la charte OCTO est implicite dans les slides produites par l'équipe communication. Pour que l'Agent Design puisse la respecter, elle doit être formalisée :

```plantuml
@startuml
title Charte visuelle formalisée — structure cible

package "Charte OCTO v1.0" {

  package "Typographie" {
    class FontRule {
      family: "Nunito Sans" | "Roboto Mono"
      usage: title | body | code | caption
      sizes: []int
      weights: []string
    }
  }

  package "Couleurs" {
    class Palette {
      primary: "#E74C3C"
      secondary: "#3498DB"
      accent: "#27AE60"
      neutral: "#95A5A6"
      background: "#FFFFFF" | "#2C3E50"
    }
    class ColorRule {
      element: title | body | background | icon
      allowedColors: []string
      forbiddenCombinations: []string
    }
  }

  package "Layout" {
    class GridRule {
      columns: int
      margins: Margins
      gutters: int
    }
    class ZoneRule {
      name: title | body | visual | footer
      position: TopLeft | Center | ...
      minSize: Dimensions
      maxSize: Dimensions
    }
  }

  package "Composants" {
    class ComponentRule {
      type: bullet | table | icon | diagram
      maxItems: int
      spacing: int
      allowedInZones: []string
    }
  }
}

@enduml
```

---

## Les évolutions potentielles rendues possibles par A2A

### Evolution 1 : Selector multi-catalogue (12 mois)

Avec A2A, le Selector peut interroger des catalogues externes — d'autres cabinets, d'autres domaines — via leur Agent Card. C'est la manœuvre two-sided-market à l'échelle de l'industrie.

```plantuml
@startuml
title Evolution 1 — Selector multi-catalogue

participant "Agent Selector\nOCTO" as S
participant "Agent Selector\nMcKinsey" as SM
participant "Agent Selector\nAccenture" as SA
database "Catalogue OCTO" as CO
database "Catalogue McKinsey" as CM
database "Catalogue Accenture" as CA

note over S, CA
  Le brief identifie un contexte "banque de détail"
  Le Selector OCTO cherche dans son catalogue
  puis élargit à des catalogues partenaires
end note

S -> CO : Cherche slides "banque détail"
CO --> S : 2 slides (faible couverture)
S -> SM : Task{SlideNeed, context: "banque détail"}
SM -> CM : Cherche slides "banque détail"
CM --> SM : 5 slides (bonne couverture)
SM --> S : SelectionProposal{slides, confidence: 0.85}
S -> SA : Task{SlideNeed, context: "banque détail"}
SA -> CA : Cherche slides "banque détail"
CA --> SA : 3 slides
SA --> S : SelectionProposal{slides, confidence: 0.72}
S -> S : Compare les propositions\nChoisit la meilleure couverture
S --> S : Sélectionne McKinsey pour ce besoin

@enduml
```

### Evolution 2 : Writer comme chef d'orchestre (6-12 mois)

Le Writer peut appeler un agent de recherche pour enrichir le contenu avec des données réelles, un agent de reformulation pour respecter les contraintes de style.

```plantuml
@startuml
title Evolution 2 — Writer enrichi par sous-agents

participant "Agent Writer\n(complexe)" as W
participant "Agent Recherche" as AR
participant "Agent Reformulation" as AF
participant "Claude Sonnet" as LLM
database "Sources externes" as SRC

W -> W : Reçoit SlideNeed{intention: "ROI cloud migration"}
W -> AR : Task{query: "ROI cloud migration 2025\nchiffres cabinet conseil"}
AR -> SRC : Recherche données
SRC --> AR : Études, benchmarks, chiffres
AR --> W : ResearchResult{data: [...], sources: [...]}
W -> LLM : Génère contenu avec données réelles
LLM --> W : Contenu brut (trop long, 850 chars / 600 max)
W -> AF : Task{content: "...", maxChars: 600, style: "assertif"}
AF -> LLM : Reformule dans la contrainte
LLM --> AF : Contenu reformulé (580 chars)
AF --> W : ReformulationResult{content: "...", chars: 580}
W -> W : Valide maxChars et style
W --> W : SlideContent{modifications: [...]}

@enduml
```

### Evolution 3 : Reviewer comme service transverse (18 mois)

Le Reviewer (Opus + extended thinking) est coûteux à construire et générique dans sa nature. En A2A, il peut être mutualisé entre plusieurs pipelines.

```plantuml
@startuml
title Evolution 3 — Reviewer comme service transverse

participant "agentigslide\nOrchestrator" as AG
participant "Pipeline\nRapports" as RP
participant "Pipeline\nPropositions\ncommerciales" as PP
participant "Agent Reviewer\n(service partagé)\nClaude Opus + thinking" as RV

note over AG, RV
  Le Reviewer valide la cohérence de n'importe quel plan structuré
  Il n'est pas spécifique aux slides — il valide des plans
end note

AG -> RV : Task{plan: GenerationPlan, type: "slides", rules: [...]}
RP -> RV : Task{plan: ReportPlan, type: "report", rules: [...]}
PP -> RV : Task{plan: ProposalPlan, type: "proposal", rules: [...]}

RV -> RV : Extended thinking\nAnalyse chaque plan\nindépendamment

RV --> AG : ReviewResult{approved, issues: [...]}
RV --> RP : ReviewResult{approved, issues: [...]}
RV --> PP : ReviewResult{approved, issues: [...]}

@enduml
```

### Evolution 4 : boucle d'enrichissement automatique du catalogue (18-24 mois)

Chaque slide créée ex nihilo et validée génère un signal vers l'équipe communication. Avec A2A, cette boucle devient un agent autonome.

```plantuml
@startuml
title Evolution 4 — Boucle d'enrichissement du catalogue

participant "Agent Validation\nVisuelle" as AV
participant "Agent Enrichissement\nCatalogue" as AE
participant "Equipe communication" as EC
database "Catalogue OCTO" as CAT
database "Gap tracker" as GT

AV -> AE : Signal{slideSpec, gap_detected: true, context: "banque détail"}
AE -> GT : Enregistre le gap
GT --> AE : Gap count pour ce contexte: 7
note right of AE
  Seuil atteint : 5 occurrences
  Le gap est récurrent et justifie
  l'intégration au catalogue officiel
end note
AE -> AE : Génère la fiche de proposition\n(slide + contexte + fréquence)
AE -> EC : Notification{proposal: SlideProposal, priority: HIGH}
EC -> EC : Revoit la slide créée\nAjuste si nécessaire\nValide la proposition
EC -> CAT : Intègre la slide au catalogue officiel
CAT -> CAT : Rebuilt index sémantique
CAT --> AE : Confirmation intégration
AE -> GT : Clôture le gap

@enduml
```

---

## Decision

### Ce qui est décidé

Implémenter A2A de façon **progressive et séquentielle**, en trois phases :

**Phase 1 — Exposition (0-6 mois) :** Chaque agent existant (Outliner, Selector, Writers, Reviewer) expose une Agent Card et un endpoint `/tasks`. L'orchestrateur Go appelle les agents via A2A plutôt que via des fonctions Go. L'interface externe ne change pas. C'est un refactoring architectural, pas une évolution fonctionnelle.

**Phase 2 — Fallback créatif (6-12 mois) :** Le Selector implémente le fallback créatif — il orchestre Agent Layout, Agent Design, Agent Validation Visuelle via A2A quand aucun template ne convient. La charte formalisée et les primitives de design sont préconditions de cette phase.

**Phase 3 — Écosystème (12-24 mois) :** Writers enrichis (Agent Recherche, Agent Reformulation), Reviewer comme service transverse, Selector multi-catalogue, boucle d'enrichissement automatique.

### Ce qui n'est pas décidé

- L'abandon de l'orchestrateur Go pur — il reste le coordinateur de Niveau 1.
- Le fournisseur A2A — le protocole Google est le candidat naturel mais il est encore en Genesis. Une abstraction d'interface est nécessaire pour ne pas dépendre d'une implémentation unique.
- Le modèle économique d'un catalogue multi-tenant.

---

## Conséquences

### Positives

- **Composabilité** : les agents peuvent orchestrer des sous-agents sans modifier l'orchestrateur central.
- **Cycles de vie indépendants** : chaque agent peut être mis à jour, remplacé, ou redéployé sans impact sur les autres.
- **Substitution de modèles** : le modèle LLM sous-jacent de chaque agent est remplaçable indépendamment.
- **Couverture totale** : le fallback créatif élimine les zones blanches du catalogue.
- **Écosystème** : agentigslide devient un nœud dans un réseau d'agents, pas un outil isolé.
- **Mutualisation** : le Reviewer peut devenir un service transverse, partagé entre plusieurs pipelines.

### Négatives

- **Complexité opérationnelle** : un réseau d'agents distribués est plus complexe à déployer, monitorer et déboguer qu'un binaire Go monolithique.
- **Latence** : les appels A2A via réseau ajoutent de la latence par rapport aux appels de fonctions Go en mémoire.
- **Dépendance sur un protocole instable** : A2A est encore en Genesis — construire dessus avant stabilisation introduit un risque de breaking changes.
- **Charte implicite** : le fallback créatif ne peut pas fonctionner sans une charte formalisée explicite — c'est un chantier préalable non trivial pour l'équipe communication.
- **Indéterminisme** : deux niveaux d'orchestration introduisent des comportements émergents plus difficiles à tester et à auditer.

### Risques et mitigations

| Risque | Probabilité | Impact | Mitigation |
|--------|-------------|--------|------------|
| A2A breaking changes | Haute | Moyen | Abstraire derrière une interface Go — changer l'implémentation sans changer les agents |
| Charte non formalisée | Haute | Haut | Bloquer la Phase 2 jusqu'à formalisation complète — ne pas tenter le fallback créatif sans charte explicite |
| Latence réseau | Moyenne | Moyen | Mode local (in-process) pour le développement, A2A pour la production — l'interface est la même |
| Complexité opérationnelle | Haute | Moyen | Observabilité dès Phase 1 — traces distribuées, métriques par agent, logs corrélés |
| Indéterminisme niveau 2 | Moyenne | Haut | Tests de bout en bout avec snapshots des plans générés — détecter les régressions avant production |
