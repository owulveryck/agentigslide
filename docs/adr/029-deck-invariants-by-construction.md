# ADR 029 — Invariants de deck déterministes appliqués par construction

## Statut

Accepté

## Contexte

La trace `edito-trace-v3.json` montre 3 itérations de review (364 s, 46 % du run) qui n'ont **jamais convergé**, en grande partie sur deux règles structurelles : « le premier slide doit être la couverture officielle (SLIDE 1) » et « le dernier slide doit être la conclusion officielle ». Ces règles ne vivaient que dans la mémoire apprise du reviewer (`REVIEWER_MEMORY.md`) — par ailleurs empoisonnée, cf. ADR 028 :

- le reviewer (LLM) les *jugeait* à chaque passe, à coût Opus ;
- le correcteur (writers de contenu) n'avait **aucun actuateur** pour restructurer le deck → l'issue revenait à chaque itération, le pipeline finissait par livrer le défaut ;
- la conclusion configurée (`CLOSING_SLIDE=325`, ADR 018) entrait en conflit avec la croyance mémoire du reviewer (« conclusion = SLIDE 250 »), produisant des corrections destructrices.

Asymétrie issue ↔ actuateur : un agent ne doit jamais émettre un finding que le système ne sait pas corriger.

## Décision

1. **Les invariants de deck deviennent de la configuration template** : fichiers `COVER_SLIDE`, `CLOSING_SLIDE` (existant), `SUMMARY_SLIDE` (optionnel), un numéro de slide chacun. Chargés par `plan.LoadDeckInvariants` → `agent.DeckInvariants`.
2. **Application par construction, pas par revue** (`orchestrator`) :
   - `enforceDeckInvariants` : après validation de la sélection, tout besoin typé `cover` est **forcé** sur le template de couverture officiel — le writer remplit alors naturellement ses champs (titre, sous-titre) avec le contenu prévu par l'outline ;
   - `assemble` : si l'outline n'a produit aucun besoin `cover`, la couverture officielle est **préfixée** avec le titre de la présentation dans son champ titre principal (identifié depuis le catalogue) ; la conclusion officielle reste appendue en dernier.
3. **Vérification déterministe en pre-review** : `ValidateDeckInvariants` (premier = couverture, dernier = conclusion, conclusion nulle part ailleurs, sommaire présent si configuré). Ces issues portent `SlideIndex=-1` et le type `deck_invariant` : elles ne sont jamais routées vers les writers — une violation signale un bug d'enforcement, pas un problème de contenu.
4. **Exemption des slides invariants** des checks `wrong_template`/`missing_content` du gate pre-review : la couverture et la conclusion officielles peuvent être décoratives (absentes du catalogue compact, sans modification).
5. Le template edito est configuré : `COVER_SLIDE=1`, `CLOSING_SLIDE=325`.

## Principe généralisable

*Symétrie issue ↔ actuateur, et règles structurelles dans la configuration, pas dans le jugement.* Une règle invariante (calculable, binaire) jugée par un LLM coûte des tokens à chaque passe, produit des faux positifs, et — si le correcteur n'a pas le pouvoir d'agir dessus — condamne la boucle à ne jamais converger. Les invariants se déclarent en configuration, s'appliquent par construction, et se vérifient par code. Le LLM ne juge que ce qui ne se calcule pas.

## Conséquences

- La classe d'issues « couverture/conclusion » disparaît du périmètre du reviewer (3 itérations gaspillées sur la trace v3).
- La mémoire du reviewer n'a plus besoin de règles structurelles à numéros de slides — elles sont d'ailleurs rejetées par la validation mémoire (ADR 028).
- Le cross-check des findings (ADR 030) droppe toute issue du reviewer visant les slides invariants.

## Critères de succès

- 0 itération de review motivée par la structure du deck (3 sur la trace v3).
- Le deck généré commence par SLIDE 1 et finit par SLIDE 325 sans intervention du reviewer.
- `ValidateDeckInvariants` ne se déclenche jamais en fonctionnement nominal (sa présence est une défense en profondeur).
