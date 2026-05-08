# ADR 006 : Mode Agent + Chat Interactif par Defaut

- **Date** : 2026-05-08
- **Statut** : Accepte
- **Decideurs** : Olivier Wulveryck

## Contexte

Historiquement, `slidegen` proposait deux modes de generation :

1. **Mode monolithique** (defaut) : un seul appel a Claude via Vertex AI, recevant le catalogue complet et la demande utilisateur. Simple mais sans boucle de correction (cf. ADR 001, section "Limites identifiees").
2. **Mode multi-agent** (`--agent`) : pipeline en 5 etapes (Outliner, Selector, Writers, Reviewer), avec boucle de feedback et parallelisme.

Le mode interactif (`--chat`, ADR 005) a ensuite ete ajoute comme option supplementaire, permettant de raffiner l'outline avant le pipeline.

### Constats

- **Le mode monolithique est devenu obsolete** : le pipeline multi-agent produit systematiquement de meilleurs resultats grace a la boucle de review et a la specialisation des agents. Aucun utilisateur n'a de raison de preferer le mode monolithique.
- **Le mode chat est le flux naturel** : lancer `slidegen` sans argument devrait proposer une interaction plutot qu'afficher l'aide et quitter.
- **Trois flags mutuellement dependants** (`--agent`, `--chat`, `--web`) complexifiaient l'interface CLI pour un choix qui devrait etre automatique.

## Decision

1. **Le mode multi-agent est le seul mode de generation** (hors amend/recovery). Le mode monolithique (`generateMode`) est supprime.
2. **Le mode interactif est le comportement par defaut** : quand aucun fichier n'est fourni (`--file` ou stdin pipe), slidegen entre en mode chat.
3. **Les flags `--agent`, `--chat` et `--prompt` sont supprimes**. Le flag `--web` reste pour le dashboard.

### Logique resultante

```
slidegen                     → agent + chat interactif
slidegen --file request.md   → agent sans chat (pipeline direct)
cat req.md | slidegen        → agent sans chat (stdin pipe)
slidegen --web               → agent + dashboard web
slidegen --plan plan.json    → recovery (inchange)
slidegen --plan p --file a   → amend (inchange)
```

La detection est automatique via `hasUserRequest()` qui verifie `filePath != ""` ou `stdin is pipe`.

## Choix Techniques

### Suppression du mode monolithique

La fonction `generateMode()` et les flags associes (`--prompt`, `--dump` en mode generation) sont supprimes. Le flag `--dump` est conserve uniquement pour le mode amend (`--plan + --file`).

La variable d'environnement `SLIDEGEN_AGENT_MODE` est supprimee. `SLIDEGEN_MODEL` est conservee pour le mode amend qui utilise encore `pipeline.SendPrompt()`.

### Detection automatique du mode chat

```go
useChat := !hasUserRequest(*filePath) && !useWeb
```

- Terminal interactif, pas de fichier → chat
- Fichier fourni ou stdin pipe → pipeline direct
- Mode web → dashboard (pas de chat terminal)

### Suppression des flags redondants

| Flag supprime | Raison |
|---------------|--------|
| `--agent` | Agent est toujours actif |
| `--chat` | Chat est automatique quand pas d'input |
| `--prompt` | Utilise uniquement par `generateMode` |

## Consequences

### Positives

- **UX simplifiee** : `slidegen` sans argument fait quelque chose d'utile (mode interactif) au lieu d'afficher l'aide.
- **Moins de flags** : 3 flags supprimes, interface CLI plus claire.
- **Un seul chemin de generation** : pas d'ambiguite sur quel mode utiliser.
- **Code simplifie** : suppression de `generateMode()` (~75 lignes) et de la logique de routage multi-mode.

### Negatives

- **Breaking change** : les scripts utilisant `--agent` ou `--chat` casseront. Mitigation : ces flags etaient recents (ADR 001 et 005, mai 2026).
- **Pas de mode "simple"** : pour un test rapide sans pipeline complet, il faut desormais passer par les 5 agents. En pratique, la latence additionnelle est compensee par la qualite.
- **Mode amend reste monolithique** : le mode `--plan + --file` utilise encore un seul appel Claude (`pipeline.SendPrompt`). Evolution future possible.

## Fichiers Concernes

### Modifies

| Fichier | Modification |
|---------|-------------|
| `slidegen/main.go` | Suppression de `generateMode()`, des flags `--agent`/`--chat`/`--prompt`, de `AgentMode` dans la config. Nouvelle logique de routage avec agent+chat par defaut. |

### Supprimes (code mort)

| Element | Raison |
|---------|--------|
| `generateMode()` | Mode monolithique supprime |
| Flag `--agent` | Agent est toujours actif |
| Flag `--chat` | Chat est automatique |
| Flag `--prompt` | Dependait de `generateMode` |
| `SLIDEGEN_AGENT_MODE` | Plus de choix de mode |
