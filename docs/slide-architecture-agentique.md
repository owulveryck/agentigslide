# Architecture Agentique : Convictions

Brief conceptuel pour une slide dense de principes.
Destinataires : decideurs techniques.

---

## Titre de la slide

**ARCHITECTURE AGENTIQUE : CONVICTIONS**

---

## Layout general

Grille 2x2 avec titre en bandeau. Lecture en Z : Agent vs Tool (fondation) -> Orchestration (pattern) -> Autonomie progressive (methode) -> Agent Mesh (vision).

```
+================================================================+
|           ARCHITECTURE AGENTIQUE : CONVICTIONS                  |
+================================+===============================+
|                                |                                |
|  A. AGENT vs TOOL              |  B. ORCHESTRATEUR              |
|  La distinction fondatrice     |  Deleguer des taches,          |
|                                |  pas des instructions           |
|                                |                                |
+================================+===============================+
|                                |                                |
|  C. CONCEVOIR POUR             |  D. AGENT MESH                 |
|  L'AUTONOMIE                   |  La puissance nait             |
|  Le contexte s'elargit         |  de l'ecosysteme               |
|                                |                                |
+================================+===============================+
```

---

## ZONE A -- Agent vs Tool

### Message

Un agent prend des decisions. Un outil execute des actions. Cette distinction est structurante pour toute architecture agentique.

### Schema ASCII

```
  +------------------+          +------------------+
  |    🧠 AGENT      |          |    ⚙️  TOOL       |
  |                  |          |                  |
  |  Non-deterministe|          |  Deterministe    |
  |  Non-idempotent  |          |  Idempotent      |
  |                  |          |                  |
  |  DECIDE          |          |  EXECUTE         |
  |  en fonction     |          |  en fonction     |
  |  du contexte     |          |  des parametres  |
  |  d'execution     |          |  d'entree        |
  +------------------+          +------------------+
         |                              |
   Meme entree                   Meme entree
   = resultat                    = meme resultat
     different                     garanti
```

### Textes exacts a placer

**Colonne Agent (gauche, fond orange #E8734A) :**
- Titre : `AGENT`
- Ligne 1 : `Non-deterministe`
- Ligne 2 : `Non-idempotent`
- Ligne 3 : `Decide en fonction du contexte d'execution`

**Colonne Tool (droite, fond gris #95A5A6) :**
- Titre : `TOOL`
- Ligne 1 : `Deterministe`
- Ligne 2 : `Idempotent`
- Ligne 3 : `Execute en fonction des parametres d'entree`

**Phrase cle (en bas de zone, italique) :**

> Meme entree, meme outil = meme resultat.
> Meme entree, meme agent = resultat different selon le contexte.

---

## ZONE B -- Orchestrateur et Delegation

### Message

L'orchestrateur est le systeme cognitif centralise. Il delegue des taches a des agents specialises qui prennent des sous-decisions dans leur perimetre. Chaque agent recoit un morceau de contexte, pas un mode d'emploi.

### Schema ASCII

```
                 +---------------------+
                 |   ORCHESTRATEUR     |
                 |  Systeme cognitif   |
                 |  Decide du workflow |
                 |  Coordonne          |
                 +-----+-----+--------+
                 tache |     | tache
               + ctxt  |     | + ctxt
                /      |      \
               v       v       v
         +-------+ +-------+ +-------+
         | Agent | | Agent | | Agent |
         |   A   | |   B   | |   C   |
         +---+---+ +---+---+ +---+---+
             |         |         |
         +---+---+ +---+---+ +---+---+
         | Tools | | Tools | | Tools |
         +-------+ +-------+ +-------+
              ^         ^         ^
        deterministe  deterministe  deterministe
        idempotent    idempotent    idempotent
```

### Textes exacts a placer

**Rectangle central (fond teal #1A5276) :**
- `ORCHESTRATEUR`
- `Systeme cognitif centralise`
- `Decide du workflow, coordonne, valide`

**Hexagones satellites (fond orange #E8734A) :**
- `Agent A`, `Agent B`, `Agent C`
- Sous chaque hexagone : perimetre de decision

**Fleches descendantes :**
- Label : `Delegue une tache + contexte`

**Fleches montantes :**
- Label : `Retourne une decision`

**Rectangles en bas (fond gris #95A5A6) :**
- `Tools` sous chaque agent
- Label : `Ressources & actions deterministes`

**Phrase cle (en bas de zone, italique) :**

> Deleguer des taches, pas des instructions.
> L'agent decide comment accomplir la tache dans son perimetre.

---

## ZONE C -- Concevoir pour l'autonomie

### Message

Concevoir chaque agent comme s'il deviendra un agent autonome. Comme les architectures composables (MACH), le decouplage est un investissement. Commencer par cadrer le perimetre d'action, puis elargir le contexte progressivement.

### Schema ASCII

```
  Contexte etroit         Contexte elargi          Contexte ouvert
  (sous-agent)            (agent decouple)         (agent autonome)

  +- - - - - - -+        +- - - - - - - - -+      +- - - - - - - - - - -+
  :   parent     :        :                  :      :                      :
  :  +--------+  :        :  +-----------+   :      :  +------------+      :
  :  | Agent  |  :        :  |  Agent    |   :      :  |   Agent    |      :
  :  |        |  :        :  |           |   :      :  |            |      :
  :  +--------+  :        :  +-----+-----+   :      :  +-----+------+     :
  :       ^      :        :    ^       ^     :      :    ^    ^    ^       :
  :       |      :        :    |       |     :      :    |    |    |       :
  :   contexte   :        :  proto-  autres  :      :  orch. orch. events  :
  :   du parent  :        :  cole    sources :      :   A     B            :
  +- - - - - - -+        +- - - - - - - - -+      +- - - - - - - - - - -+

    Synchrone                Protocole               Evenements
    In-process               A2A / API                Multi-domaine
```

### Textes exacts a placer

**Cercle 1 (petit, opacite forte) :**
- Titre : `Sous-agent contraint`
- `Contexte alimente par le parent`
- `Perimetre d'action cadre`
- `Communication synchrone`

**Cercle 2 (moyen, opacite moyenne) :**
- Titre : `Agent decouple`
- `Entrees/sorties via protocole`
- `Decouple mais dans le domaine`
- `Communication asynchrone`

**Cercle 3 (large, opacite faible) :**
- Titre : `Agent autonome`
- `Contexte multi-domaine`
- `Sert plusieurs orchestrateurs`
- `Nouvelles informations = nouvelles decisions`

**Phrase cle (en bas de zone, italique) :**

> Commencer par emprisonner l'agent dans un contexte etroit.
> Elargir progressivement. L'autonomie se gagne, elle ne se decrete pas.

---

## ZONE D -- Agent Mesh

### Message

La valeur est decuplee quand les agents forment un maillage au service de plusieurs orchestrateurs. Pour eviter le plat de spaghettis, la communication passe par des evenements. Un registry interne permet la decouverte dynamique.

### Schema ASCII

```
    Orch. X          Orch. Y          Orch. Z
      \                |                /
       \               |               /
        v              v              v
  +------------------------------------------+
  |              AGENT REGISTRY               |
  |      Decouverte dynamique des agents      |
  +------------------------------------------+
        |         |         |         |
        v         v         v         v
     +-----+  +-----+  +-----+  +-----+
     |Ag. 1|--|Ag. 2|--|Ag. 3|--|Ag. 4|
     +--+--+  +--+--+  +--+--+  +--+--+
        |  \  /  |  \  /  |  \  /  |
        +---\/---+---\/---+---\/---+
             EVENEMENTS
        Communication decouple
        (pas de plat de spaghettis)
```

### Textes exacts a placer

**Rectangles en haut (fond teal #1A5276) :**
- `Orchestrateur X`, `Orchestrateur Y`, `Orchestrateur Z`

**Barre centrale (fond blanc, bordure teal) :**
- `AGENT REGISTRY`
- `Decouverte dynamique des agents`

**Hexagones en maillage (fond orange #E8734A) :**
- `Agent 1`, `Agent 2`, `Agent 3`, `Agent 4`
- Lignes de connexion entre agents = partage de contexte

**Bande inferieure :**
- `EVENEMENTS` -- communication decouple

**Phrase cle (en bas de zone, italique) :**

> Un agent interne a de la valeur.
> Un agent dans un mesh en a dix fois plus.
> La puissance nait de l'ecosysteme.

---

## Palette couleurs

| Element | Hex | Usage |
|---------|-----|-------|
| Agent | `#E8734A` | Hexagones, decisions, intelligence |
| Tool | `#95A5A6` | Rectangles, execution, determinisme |
| Orchestrateur | `#1A5276` | Rectangles arrondis, coordination |
| Contexte | Degradee d'opacite sur `#E8734A` | Expansion progressive |
| Mesh/Liens | `#E8734A` a 50% opacite | Connexions ecosysteme |
| Fond slide | `#FFFFFF` | Lisibilite |
| Texte principal | `#2C3E50` | Titres et labels |
| Texte secondaire | `#7F8C8D` | Annotations, phrases cles |

## Langage visuel

| Forme | Signification |
|-------|---------------|
| Hexagone | Agent (decideur) |
| Rectangle | Tool (executeur) |
| Rectangle arrondi | Orchestrateur (coordinateur) |
| Cercle en pointille | Perimetre de contexte |
| Fleche pleine | Delegation de tache |
| Fleche pointillee | Evolution future / decouplage |
| Ligne fine entre hexagones | Partage de contexte (mesh) |

## Les 4 takeaways

1. **Agent ≠ Tool** -- Un agent decide. Un outil execute. Ne pas confondre.
2. **Deleguer des taches** -- L'orchestrateur partage du contexte, pas des instructions. L'agent decide dans son perimetre.
3. **Concevoir pour l'autonomie** -- Cadrer le contexte etroit, elargir progressivement. Comme MACH : le decouplage est un investissement.
4. **Agent Mesh** -- La valeur est decuplee quand les agents servent un ecosysteme, pas un seul orchestrateur. La puissance nait du maillage.
