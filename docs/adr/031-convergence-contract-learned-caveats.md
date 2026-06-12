# ADR 031 — Contrat de convergence des boucles de revue : fingerprints, classification par actuateur, annotation du template

## Statut

Accepté

## Contexte

La boucle visuelle de `edito-trace-v3.json` **diverge** : 3 passes (47/28/26 s de vision), slides non approuvés 13 → 11 → 10, corrections 42/29/29, et **29 findings livrés non résolus**. Analyse des findings :

- une grande partie décrit des problèmes de **géométrie du template** (« zone de texte trop étroite forçant des retours à la ligne artificiels », textes fragmentés) — aucune réécriture de contenu ne peut les corriger ; la boucle les re-découvre à chaque passe ;
- d'autres sont **subjectifs** (asymétries d'alignement, préférences de mise en page) — un correcteur de contenu ne peut pas les satisfaire de façon fiable ;
- seuls les vrais problèmes de contenu (débordement, champ vide) sont corrigeables par le canal existant.

La boucle traitait ces trois classes identiquement, sans mesure de progression : elle itérait « pour voir », brûlait des tokens de vision et s'arrêtait sur épuisement du compteur. La boucle éditoriale souffrait du même défaut (3 itérations sur la même issue de couverture en v3).

## Décision

1. **Fingerprints d'issues** (`internal/agent/convergence.go`) : une issue est identifiée par `(slide|champ|type)` (review éditoriale) ou `(pageID|type)` (review visuelle). Entre deux passes, chaque issue est *résolue*, *répétée* ou *nouvelle*.
2. **Contrat de convergence** (`ConvergenceTracker.StrictProgress`) : une passe fait un progrès strict si elle résout au moins une issue ET n'en crée pas plus qu'elle n'en résout. Dès que le progrès strict cesse, la boucle **s'arrête et constate** (escalade consolidée, ADR 026/032) au lieu d'itérer. Appliqué aux deux boucles : review éditoriale (orchestrateur) et review visuelle (`runVisualReview`). Sur la trace v3, la passe visuelle 3 n'aurait pas eu lieu.
3. **Classification par actuateur** (`ClassifyVisualIssue`) :
   - *corrigeable par le contenu* (`text_overflow`, `text_truncated`, `empty_field` sans marqueur géométrique) → routé vers la boucle de correction ;
   - *géométrie du template* (marqueurs : « zone trop étroite », « fragmenté », « élargir la zone »…) → **jamais retenté** ; persisté en caveat structuré ;
   - *subjectif* (`misalignment`, `font_issue`, `layout_broken` non géométrique) → acquitté, jamais retenté.
   Classification conservatrice : au doute, corrigeable (un vrai problème de contenu garde toujours sa chance).
4. **Annotation du template** (`learned_caveats.json`) : les findings de géométrie deviennent des caveats du slide template source — overlay JSON versionné git, séparé de `template_index.json` (survit aux rebuilds), mergé dans `VisualCaveats` au chargement, donc rendu en « contraintes: » dans le catalogue compact **vu par le selector et le reviewer**. C'est la mémoire structurée et vérifiable qui remplace les règles texte-libre à numéros de slides interdites par l'ADR 028. Borné à 5 caveats/slide.

## Principes généralisables

1. *Boucles bornées par un critère de convergence mesuré, pas par un compteur.* Un budget de retries sans mesure de progrès produit soit des itérations gaspillées (le défaut est incorrigeable), soit un arrêt prématuré (le défaut allait être corrigé). Le signal d'arrêt correct est « cette passe a-t-elle strictement progressé ? ».
2. *Classification par actuateur avant correction.* Chaque finding est routé vers l'acteur qui peut agir : correcteur de contenu, base de connaissance du template, ou acquittement humain. Re-soumettre au correcteur un défaut hors de son pouvoir garantit la divergence.
3. *L'apprentissage va dans les structures de données, pas dans les prompts.* Un fait appris sur le monde (cette zone fragmente le texte) est plus utile — et plus sûr — comme donnée structurée vérifiable injectée dans le catalogue que comme règle en langage naturel dans une mémoire d'agent.

## Conséquences

- La boucle visuelle s'arrête dès qu'elle cesse de progresser ; les passes économisées sont des appels vision (les plus chers du pipeline).
- Les templates problématiques s'auto-documentent : au fil des runs, le selector évite les zones fragiles signalées en « contraintes: ».
- Les findings subjectifs ne polluent plus la boucle de correction ; ils restent visibles dans le rapport d'acquittement final.

## Critères de succès (traceeval, prochain run edito)

- Boucle visuelle ≤ 2 passes, arrêt motivé (« no strict progress ») visible dans les logs si non convergence.
- `learned_caveats.json` créé avec les slides à zones étroites identifiés en v3 ; « contraintes: » visibles dans le catalogue compact au run suivant.
- Baisse des findings visuels de passe 1 sur les runs suivants (le selector évite les templates annotés).
