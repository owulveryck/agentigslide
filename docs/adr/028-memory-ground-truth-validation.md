# ADR 028 — Validation des mémoires synthétisées contre la vérité terrain (config > code > mémoire)

## Statut

Accepté

## Contexte

L'analyse de `edito-trace-v3.json` a révélé la cause racine n°1 de la non-convergence de la review : **le mécanisme d'auto-amélioration s'est retourné contre le système**. `REVIEWER_MEMORY.md`, produit par la synthèse mémoire (ADR 015/024) d'un run antérieur, contenait des croyances fausses :

- « le catalogue ne va que jusqu'au SLIDE 250 ; SLIDE 325 = slide fantôme à supprimer » — alors que le SLIDE 325 existe dans le catalogue **et** est la conclusion configurée (`CLOSING_SLIDE=325`) ;
- « exiger SLIDE 1 en couverture et SLIDE 250 en conclusion » — des invariants structurels appris en texte libre, entrant en conflit direct avec la configuration.

Conséquences mesurées sur le run v3 : le reviewer a fait supprimer la conclusion légitime (remplacée par un slide générique qu'il a ensuite flaggé « contenu inventé »), a exigé une couverture jamais satisfaite (3 itérations, jamais approuvé), et la mémoire fausse s'auto-renforçait à chaque synthèse. Aucune validation des mémoires n'existait : `IsAdditiveUpdate` vérifiait la *forme* (additive ou non), jamais le *fond*.

C'est le phénomène de **belief drift** : un apprentissage non vérifié dérive, s'auto-renforce, et finit par combattre la configuration déterministe.

## Décision

1. **Hiérarchie de connaissance explicite : configuration déterministe > validation code > mémoire apprise.** En cas de conflit, la configuration gagne, et le conflit est journalisé.
2. **Validation factuelle des mémoires** (`agent.ValidateMemory`, `internal/agent/memory_validate.go`) contre une `GroundTruth` (numéros de slides du catalogue ∪ configuration), ligne par ligne :
   - toute règle mentionnant un slide **absent** du catalogue et de la config est un fait inventé → rejetée ;
   - toute règle mentionnant un slide et portant une **assertion structurelle** (« fantôme », « premier/dernier slide », « couverture/conclusion officielle »…) est un invariant de deck déguisé → rejetée : les invariants vivent dans la config (ADR 029), pas dans la mémoire.
   - les règles non falsifiables (éditoriales, sans numéros) passent verbatim.
3. **Double barrière** :
   - *à la synthèse* (`runMemorySynthesis`) : les propositions du LLM sont validées avant la classification additive/litigieuse — la règle « SLIDE 325 fantôme » n'aurait jamais été écrite ;
   - *au chargement* (`pipeline.LoadValidatedAgentMemories`) : les mémoires existantes sont assainies — les lignes rejetées sont déplacées dans `template/<id>/MEMORY_QUARANTINE.md` (audit versionné git) et les fichiers `*_MEMORY.md` sont réécrits sans le poison. La mémoire empoisonnée actuelle est nettoyée automatiquement au prochain run.
4. **Subordination dans le prompt** (`BuildSystemBlocks`) : le bloc mémoire est préfixé d'une consigne de subordination au catalogue/config — filet sémantique en plus du filtrage déterministe.

## Principe généralisable

*L'auto-amélioration doit être harnachée comme le reste.* Un système qui apprend de ses propres erreurs sans valider ses apprentissages contre une vérité terrain encode tôt ou tard une croyance fausse — qui s'auto-renforce car elle influence les runs suivants, donc les synthèses suivantes. Toute connaissance apprise doit être (a) falsifiable, (b) validée contre les faits vérifiables avant application, (c) subordonnée à la configuration, (d) auditable et réversible (quarantaine versionnée). Les faits structurels appris ont leur place dans des structures de données vérifiables (cf. caveats de l'ADR 031), jamais dans des règles en texte libre.

## Conséquences

- La classe « croyance mémoire vs config » disparaît : c'était la cause des 3 itérations de review stériles de la trace v3.
- Des règles utiles citant des slides existants restent autorisées tant qu'elles ne sont pas structurelles (ex. « le SLIDE 35 convient aux présentations institutionnelles ») ; les faits de géométrie migreront vers `learned_caveats.json` (ADR 031).
- `MEMORY_QUARANTINE.md` donne l'historique auditable de tout ce que le système a « cru » à tort.

## Critères de succès

- Au prochain run : les 3+ règles empoisonnées de `REVIEWER_MEMORY.md` sont en quarantaine, plus aucune issue de review motivée par « SLIDE 250 » ou « SLIDE 325 fantôme ».
- 0 proposition de synthèse contenant un numéro de slide inconnu écrite sur disque.
- Tests : `TestValidateMemory_PoisonedRulesFromTraceV3` rejoue les règles réelles de la trace v3.
