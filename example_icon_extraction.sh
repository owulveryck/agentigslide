#!/bin/bash
# Exemple d'extraction des icônes de la slide 20

TEMPLATE_ID="${SLIDES_PREFORMATES_ID:?SLIDES_PREFORMATES_ID must be set}"

echo "=== EXTRACTION DES ICÔNES - SLIDE 20 ==="
echo ""
echo "📊 Total d'éléments visuels:"
jq '.visualElements | length' "template/$TEMPLATE_ID/20/analysis.json"
echo ""

echo "🎨 Icônes RSE (première colonne):"
jq -r '.visualElements[] | select(.type == "icon") | select(.description | contains("écologique") or contains("environnement") or contains("globe") or contains("recyclage") or contains("carbone") or contains("égalité") or contains("planète")) | "  - \(.objectId): \(.description | split(" - ")[1])"' "template/$TEMPLATE_ID/20/analysis.json" | head -12
echo ""

echo "💼 Icônes Business (deuxième colonne):"
jq -r '.visualElements[] | select(.type == "icon") | select(.description | contains("business") or contains("euro") or contains("dollar") or contains("pièces") or contains("graphique") or contains("cible") or contains("diamant") or contains("documents")) | "  - \(.objectId): \(.description | split(" - ")[1])"' "template/$TEMPLATE_ID/20/analysis.json" | head -12
echo ""

echo "🚌 Icônes Transport (troisième colonne):"
jq -r '.visualElements[] | select(.type == "icon") | select(.description | contains("transport") or contains("bus") or contains("train") or contains("avion") or contains("voiture") or contains("métro") or contains("vélo") or contains("camion") or contains("voilier")) | "  - \(.objectId): \(.description | split(" - ")[1])"' "template/$TEMPLATE_ID/20/analysis.json" | head -12
