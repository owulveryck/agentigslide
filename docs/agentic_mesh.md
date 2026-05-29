# Prompt de reconstruction : Architecture Agentique — Convictions

Ce document decrit un poster single-page (format paysage, A4) qui presente 4 convictions sur les architectures agentiques. Il est suffisamment detaille pour reconstruire le visuel.

---

## Structure generale

- **Format** : page unique, paysage, pleine largeur/hauteur de l'ecran
- **Layout** : grille CSS 2x2 avec header en haut et footer en bas
- **Palette** : bichrome OCTO (Marine #0E2356 + Turquoise #00D2DD)
- **Police** : sans-serif systeme (Segoe UI / system-ui)

### Header
- Titre : **"Architecture Agentique"** (gras, marine)
- Sous-titre : *"Convictions pour concevoir des systemes a base d'agents"* (italique, gris)
- Filet sous le header : degarde marine → turquoise

### Footer
- Legende avec icones SVG inline :
  - Hexagone turquoise = Agent (decide)
  - Rectangle marine 60% = Tool (execute, idempotent)
  - Rectangle arrondi marine = Orchestrateur (coordonne)
  - Trait plein marine = Delegue (tache)
  - Trait pointille turquoise = Retourne (decision)
- A droite : "Fondation → Plateforme → Domaines → Maillage"

---

## Langage visuel (coherent sur les 4 cadrans)

| Forme | Couleur | Signification |
|-------|---------|---------------|
| Hexagone | Turquoise #00D2DD | Agent (decideur, non-deterministe) |
| Rectangle | Marine 60% #6E7B9A | Outil (executeur, idempotent) |
| Rectangle arrondi | Marine #0E2356 | Orchestrateur (coordinateur) |
| Rectangle pointille | Marine, stroke-dasharray | Perimetre de domaine |
| Cercle blanc avec "H" | Blanc sur marine | Humain dans un processus |
| Hexagone avec "A" | Turquoise sur marine | Agent dans un processus |

### Regles de contraste OCTO
- Texte dans hexagones turquoise : marine (pas blanc)
- Texte dans rectangles marine : blanc
- Texte sur fond blanc/clair : marine ou marine 80% (#3E4F78)
- Jamais de texte turquoise sur fond clair

---

## Cadran 1 — Agent vs Tool

**Position** : haut-gauche
**Fond** : turquoise 10% (#EBFAFB), bordure haute turquoise
**Titre** : "Agent vs Tool" — *"La **distinction fondatrice**"*

### Contenu visuel

Deux colonnes cote a cote, separees par une ligne verticale pointillee avec "vs" en dessous.

**Colonne gauche (Agent)** :
- Fond : rectangle arrondi turquoise a 12% d'opacite, bordure turquoise
- Icone : hexagone turquoise avec lettre "A" en marine
- Titre : **AGENT** (marine, gras, 14pt)
- Sous-titre : "Non-deterministe" (marine 80%)
- Corps :
  - **"Travaille et decide en autonomie"** (gras)
  - "sur la base d'une **intention**"
  - "en fonction d'un **contexte**"

**Colonne droite (Tool)** :
- Fond : rectangle arrondi marine 60% a 12% d'opacite, bordure marine 60%
- Icone : rectangle marine 60% avec lettre "T" en blanc
- Titre : **TOOL** (marine, gras, 14pt)
- Sous-titre : "Deterministe" (marine 80%)
- Corps :
  - **"Utilisation programmatique"** (gras)
  - "API ou MCP"
  - **"Idempotent"** (gras)
  - "meme appel = meme resultat"

**Phrase cle** (bas, italique, 2 lignes) :
*"L'outil est idempotent. L'agent ne l'est pas : son resultat est statistique et depend du contexte d'execution."*

---

## Cadran 2 — Fondations du socle

**Position** : haut-droite
**Fond** : marine 10% (#E7E9EE), bordure haute marine
**Titre** : "Fondations du socle" — *"**MVP** = le socle . **POC** = une application agentique qui valide les concepts"*

### Contenu visuel (de haut en bas)

**1. Encadre "Application agentique"** (rectangle turquoise pointille, coins arrondis) :
- Label en haut : "APPLICATION AGENTIQUE" (turquoise, petit)
- **Orchestrateur** : rectangle arrondi marine au centre-haut, label "Orchestrateur / coordonne"
- Lignes fines de l'orchestrateur vers 3 agents
- Label : "decident et agissent"
- **3 agents** (hexagones turquoise) : Ag. A, Ag. B, Ag. C en ligne

**2. Outils** (en dehors de l'encadre) :
- 3 petits rectangles marine 60% labelles "outils" sous chaque agent
- Connecteurs vers les capacites, labels "lit" (gauche) et "ecrit" (droite)

**3. Capacites exposees** (2 rectangles cote a cote) :
- **LECTURE** : "Comprendre . Ressources . Contexte"
- **ECRITURE** : "Agir . Actions . Modifications"
- Label entre les deux : *"decouplage fonctionnel"* (italique, gras)

**4. Plateforme digitale** (barre marine pleine largeur, coins arrondis) :
- Texte blanc : **"PLATEFORME DIGITALE"**
- Sous-texte marine 30% : "SI existant . APIs . Donnees . Services metier"

**Phrase cle** : *"D'abord cadrer la plateforme. Exposer lecture et ecriture. Ensuite, les agents decident."*

---

## Cadran 3 — Decouplage & Domaines metier

**Position** : bas-gauche
**Fond** : degrade marine 10% → turquoise 10%, bordure haute marine 60%
**Titre** : "Decouplage & Domaines metier" — *"Rapprocher chaque agent de son **contexte metier**"*

### Contenu visuel : 3 panels evolutifs de gauche a droite

**Panel 1 — Monolithe** (reduit, ~20% de la largeur) :
- Encadre plein (marine, trait epais) label "Application monolithique"
- A l'interieur : rectangle marine "Orch." au-dessus, fleche vers hexagone turquoise "Agent"
- Labels sous le panel :
  - **"Monolithe"** (gras)
  - "Dans le monolithe / avec l'orchestrateur"
  - **"Couple . Synchrone"**

**Fleche 1→2** : trait marine avec label *"decouplage"* au-dessus

**Panel 2 — Agent expose** (~30% de la largeur) :
- Rectangle marine "Orch." a l'exterieur en haut
- Badge **"API"** (marine 60%) entre l'orchestrateur et l'agent
- Encadre pointille contenant le hexagone turquoise "Agent"
- Label dans l'encadre : "l'agent est utile"
- Labels sous le panel :
  - **"Agent expose"** (gras)
  - "Appel couple via API / l'agent est utile"
  - **"Prend en compte le contexte metier"**

**Fleche 2→3** : trait marine avec label *"autonomisation"* au-dessus

**Panel 3 — Agent-produit** (agrandi, ~50% de la largeur) :
- **Domaine B** (centre) : encadre turquoise pointille contenant le hexagone turquoise "Agent"
- **Domaine A** (haut-gauche) : petit encadre marine pointille contenant un rectangle "Orch." → ligne vers l'agent central
- **Domaine C** (haut-droite) : petit encadre contenant un hexagone agent → ligne vers l'agent central
- **Domaine D** (droite) : petit encadre contenant un hexagone agent → ligne vers l'agent central
- Badges **"A2A"** et **"MCP"** (turquoise) sous le domaine B
- "ownership dans ce domaine"
- Labels sous le panel :
  - **"Agent-produit"** (gras)
  - "Pense comme un produit / ownership dans un autre domaine"
  - **"Strategie . Product management"**

---

## Cadran 4 — Agent Mesh

**Position** : bas-droite
**Fond** : turquoise 10%, bordure haute turquoise
**Titre** : "Agent Mesh" — *"4 piliers pour un **maillage a valeur**"*

### Contenu visuel (de haut en bas)

**1. Processus metier** (barre marine pleine largeur) :
- Titre blanc : **"PROCESSUS METIER"**
- 3 groupes d'icones montrant humains (H) et agents (A) qui collaborent :
  - Groupe gauche : H → A → H → A
  - Groupe centre : A → A → H
  - Groupe droite : H → A → H → A
- Humains = cercles blancs avec "H" en marine
- Agents = hexagones turquoise avec "A" en marine
- Connexions = lignes blanches entre chaque acteur

**2. Trois domaines** (3 rectangles cote a cote, pointilles marine) :
- **DOMAINE A** : 2 hexagones turquoise (agents 1 et 2), label "decide dans le cadre du domaine"
- **DOMAINE B** : 2 hexagones turquoise (agents 3 et 4), meme label
- **DOMAINE C** : 2 hexagones turquoise (agents 5 et 6), meme label
- **Connexions mesh** : lignes turquoise entre agents de domaines differents (maillage inter-domaines)
- **Connexions plateforme** : lignes fines marine 60% de chaque agent vers la plateforme en bas

**3. Gouvernance** (barre turquoise 10%, bordure turquoise) :
- Texte marine gras : **"GOUVERNANCE . Enablement : cadrer et faciliter l'utilisation des agents par les agents . Ownership par domaine"**

**4. Plateforme digitale** (barre marine, meme style que cadran 2) :
- Texte blanc : **"PLATEFORME DIGITALE"**
- Sous-texte : "SI existant . APIs . Donnees . Services metier . Lecture & Ecriture"

**Phrase cle** : *"Les agents sont les points de communication entre domaines. La valeur nait du maillage."*

---

## Palette OCTO complete

### Couleurs principales
| Role | Hex |
|------|-----|
| Marine (principale) | #0E2356 |
| Turquoise (secondaire) | #00D2DD |
| Blanc | #FFFFFF |

### Declinaisons utilisees
| Alias | Hex | Usage |
|-------|-----|-------|
| Marine 80% | #3E4F78 | Texte secondaire, italique |
| Marine 60% | #6E7B9A | Rectangles outils, separateurs |
| Marine 30% | #B7BDCC | Sous-texte sur fond marine |
| Marine 10% | #E7E9EE | Fond cadrans B et C |
| Turquoise 10% | #EBFAFB | Fond cadrans A et D |
| Turquoise 20% | #DAF6F9 | Non utilise |

### Associations de fond de cadran
- Cadran 1 (Agent vs Tool) : turquoise 10%, bordure turquoise
- Cadran 2 (Fondations) : marine 10%, bordure marine
- Cadran 3 (Decouplage) : degrade marine 10% → turquoise 10%, bordure marine 60%
- Cadran 4 (Mesh) : turquoise 10%, bordure turquoise

---

## Messages cles (les 4 convictions)

1. **Agent ≠ Tool** — L'agent decide sur intention + contexte (non-idempotent). L'outil execute sur appel programmatique (idempotent).
2. **Fondations du socle** — D'abord cadrer la plateforme digitale et exposer lecture/ecriture. Le MVP est le socle, le POC est une application agentique.
3. **Decouplage & Domaines** — Faire evoluer les agents du monolithe (couple) vers l'agent-produit (autonome, avec ownership dans un domaine, pense avec strategie et product management).
4. **Agent Mesh** — Humains et agents collaborent dans les processus metier. Les agents decident dans le cadre de leur domaine. La gouvernance est un enablement. La valeur nait du maillage inter-domaines.
