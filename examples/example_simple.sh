#!/bin/bash

# Exemple 1: Slide de titre simple
echo "=== Exemple 1: Slide de titre simple ==="
go run ../generate_appscript.go --request "Créer un deck 'Innovation 2026' avec une slide de titre"

echo ""
echo "=== Exemple 2: Modifier le titre et l'année ==="
go run ../generate_appscript.go --request "Créer une présentation 'Audit Digital 2026' avec une slide de titre 'Audit Infrastructure Cloud', modifier l'année en 2027"

echo ""
echo "=== Exemple 3: Présentation avec plusieurs slides ==="
go run ../generate_appscript.go --request "Créer une présentation 'Ma proposition' avec:
- Une slide de titre 'Projet Digital 2026'
- Une slide sommaire
- Une slide avec des pictos business"
