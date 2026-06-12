# ADR 032 — Observabilité LLM exhaustive : coût réel par appel, escalade consolidée, synthèse mémoire asynchrone, gates de régression sur traces

## Statut

Accepté

## Contexte

Sur `edito-trace-v3.json`, le coût estimé par traceeval ($1.013) **sous-comptait** : ni le visual review (3 passes de vision sur 13+ slides — parmi les appels les plus chers du pipeline), ni la synthèse mémoire, ni le designer n'avaient leurs tokens dans la trace. Aucun KPI de cache. La synthèse mémoire (57 s) bloquait la fin du run. Les escalades humaines (ADR 026) étaient dispersées le long du pipeline — chaque constat interrompait potentiellement l'humain séparément. Et rien ne permettait de détecter une régression de coût/qualité en CI sans relancer le pipeline (donc sans payer d'appels API).

On ne peut pas améliorer ce qu'on ne mesure pas — et on ne peut pas garantir une amélioration sans gate de régression.

## Décision

1. **Ledger LLM exhaustif** (`TraceFile.AgentCalls`) : en fin de run, le contenu du `metrics.Collector` (un enregistrement par appel : agent, modèle réel, tokens in/out, cache read/write, durée) est dumpé dans la trace. Le visual review remonte désormais son `Usage` par slide (`EditVisualFinding.Usage/Model`), la synthèse mémoire passe par `RawPredictFull` et enregistre le sien. Le ledger est la source autoritaire du coût ; les champs de tokens par phase restent pour la lisibilité.
2. **Coût réel et KPI cache dans traceeval** : quand le ledger est présent, le coût est recalculé par appel sur 4 composantes (input, output, cache read ×0,1, cache write ×1,25) via `metrics.LookupPricing`, avec affichage du `Cache hit ratio`. Fallback sur l'ancien calcul partiel pour les traces antérieures (marqué « partiel »).
3. **Escalade consolidée** (`escalation.Collector`, raffine ADR 026) : les constats *advisoires* (boucles non convergées — ADR 031, défauts visuels non résolus, issues persistantes) sont **collectés** via `orchestrator.WithNotification` et présentés en **un seul écran d'acquittement** en fin de run. Les décisions *bloquantes* (sélection sanitizée, écriture mémoire litigieuse) gardent leur `Ask` immédiat : leur issue change le comportement du pipeline.
4. **Synthèse mémoire recouvrante** : l'appel LLM de synthèse part en goroutine juste après formatter-2 et s'exécute **pendant** l'acquittement humain consolidé ; l'application (validation ADR 028, classification, écriture, décision litigieuse éventuelle) n'a lieu qu'après — les deux ne se disputent jamais stdin. Jointure bornée à 2 min.
5. **Gates de régression CI** (`traceeval -gate baseline.json new.json`) : sortie non-zéro si invariants absolus violés (dépassements non corrigés, rejeux déterministes en désaccord, erreurs pipeline, sélection sanitizée) ou régression relative (coût > baseline +15 % paramétrable, findings visuels non résolus en hausse, itérations de review > baseline+1). `traces/golden/` versionne les traces de référence. Avec `buildindex -check` (ADR 027), la CI vérifie le pipeline **à coût API nul**.

## Principes généralisables

1. *Observabilité exhaustive = condition de l'amélioration.* Tout appel LLM non mesuré est un angle mort d'optimisation ; le coût « estimé » qui sous-compte oriente les efforts au mauvais endroit (ici, le visual review invisible était un poste majeur).
2. *L'humain en un point, pas en pointillé.* Les constats qui n'exigent pas de décision se consolident en un acquittement unique ; seules les vraies bifurcations interrompent. Et le temps de réflexion humain est du temps machine gratuit (la synthèse tourne pendant l'acquittement).
3. *L'évaluation hors ligne sur traces enregistrées.* Un pipeline agentique doit pouvoir prouver ses non-régressions sans s'exécuter : les traces sont des fixtures, les KPIs des assertions, le tout exécutable en CI à coût nul.

## Conséquences

- Le coût affiché devient le coût réel ; le prochain run mesurera enfin le poids du visual review.
- Fin de run : ~1 min de synthèse mémoire masquée par l'acquittement ; un seul écran d'interruption.
- `go run ./cmd/traceeval -gate traces/golden/<baseline>.json <new>.json` en CI ; après le premier run post-ADRs-027-032, la nouvelle trace devient le baseline de référence.

## Critères de succès

- Trace du prochain run : `agentCalls` non vide couvrant outliner/selector/writers/reviewer/designer/visual-reviewer/memory-synthesis ; coût « réel (ledger) » affiché par traceeval avec cache hit ratio.
- Un seul écran d'acquittement par run.
- `traceeval -gate` échoue sur une trace artificiellement dégradée, passe sur la trace identique (vérifié).
