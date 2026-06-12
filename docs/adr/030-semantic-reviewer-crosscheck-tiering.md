# ADR 030 — Reviewer sémantique : cross-check déterministe des findings et tiering Sonnet d'abord, Opus en escalade

## Statut

Accepté

## Contexte

Sur la trace `edito-trace-v3.json`, la phase review représente 46 % du temps total (364 s / 792 s) pour un run de 24 slides. La passe 1 (Opus + thinking, 248 s, 14,5k tokens de sortie) a produit 11 issues, dont :

- **5 issues `overflow` calculables** — qui auraient dû être interceptées par le gate `OverLimitFields` (inopérant à cause de la double source de capacités, ADR 027) ;
- **1 faux positif factuel** : « SLIDE 325 n'existe pas dans le catalogue (qui va jusqu'au SLIDE 250) » — le slide 325 existe et est la conclusion configurée. Ce faux positif, nourri par la mémoire empoisonnée (ADR 028), a déclenché une correction destructrice (remplacement de la conclusion légitime par un slide générique, ensuite flaggé « contenu inventé » par le même reviewer) ;
- des issues structurelles (couverture/conclusion) désormais traitées par construction (ADR 029).

Le tiering de l'ADR 022 était inopérant : 24 slides > seuil de 20 → Opus systématique. Et aucun mécanisme ne vérifiait les findings du juge avant d'engager des corrections.

## Décision

1. **Cross-check déterministe des findings** (`agent.CrossCheckReviewIssues`) : avant toute correction, chaque finding de type calculable est re-vérifié par code contre la vérité terrain —
   - `overflow` : recomptage du texte réel vs la capacité dérivée de la géométrie (ADR 027) ;
   - `wrong_template` : vérification de l'appartenance réelle au catalogue ;
   - tout finding visant un slide invariant (couverture/conclusion configurées, ADR 029) est écarté.
   Les faux positifs sont droppés, loggés, enregistrés dans l'issueLog (`reviewer_false_positive` — la mémoire apprend ainsi dans le bon sens) et tracés (`droppedIssues`). Si tous les findings d'une passe sont des faux positifs, le plan est **réputé approuvé**.
2. **Reviewer purement sémantique** (`prompt_reviewer.txt`) : les critères calculables (OVERFLOW, TEMPLATE INADAPTÉ, DENSITÉ, LISTES À PUCES) sortent du prompt et deviennent des gates déterministes pre-review (`CheckTextHeuristics` : puces dans champs inadaptés, lignes nécessaires vs géométrie de la boîte, remplissage > 90 % avec sauts de ligne). Le reviewer ne juge plus que : duplication sémantique, couverture de la demande, cohérence, invention, qualité éditoriale, topologie des diagrammes. Le prompt lui interdit explicitement de juger ce que le système vérifie.
3. **Tiering Sonnet d'abord, Opus en escalade** : gates propres → review complète sur le modèle économe sans thinking, **sans condition de taille de deck** (`AGENT_REVIEWER_TIER_THRESHOLD` est déprécié). Le modèle cher n'intervient que sur signal déterministe : gates sales, sélection sanitizée, `REVIEWER_FORCE_OPUS`, ou **non-progrès** — un fingerprint d'issue (slide, champ, type) qui réapparaît après une correction escalade la passe suivante vers Opus + thinking (seconde opinion).

## Principes généralisables

1. *Cross-check des jugements du LLM-judge.* Un juge LLM hallucine des faits. Tout finding portant sur un fait calculable doit être re-vérifié par code avant d'engager une action — sinon les faux positifs déclenchent des corrections destructrices et contaminent la mémoire apprise.
2. *Le LLM ne juge que ce qui ne se calcule pas.* Chaque critère déplacé du prompt vers un gate est une classe de coût, de latence et de faux positifs supprimée définitivement.
3. *Économie multi-vitesses gouvernée par des signaux déterministes.* Le tier du modèle n'est pas un choix statique mais une réaction à des signaux mesurables (gates, progrès de la boucle). Le modèle cher est un escalier, pas un défaut.

## Conséquences

- Phase review attendue < 90 s (vs 364 s) : passe 1 sur Sonnet sans thinking, périmètre réduit au sémantique.
- Les corrections destructrices sur faux positifs deviennent impossibles (droppées avant routage).
- `AGENT_REVIEWER_TIER_THRESHOLD` est ignoré (déprécié, documenté dans la desc envconfig).

## Critères de succès (traceeval, prochain run edito)

- 0 finding de type calculable engagé en correction (6 sur la trace v3 : 5 overflow + 1 wrong_template).
- `droppedIssues` visibles dans la trace pour tout faux positif.
- Phase review < 90 s, coût reviewer divisé par ≥ 3.
- Pas de hausse des défauts sémantiques livrés (vérification humaine du deck).
