# 1. Sauvegarder votre fichier actuel
cp ~/.config/gcloud/application_default_credentials.json ~/.config/gcloud/application_default_credentials.json.backup

# 2. Générer les nouveaux credentials pour Slides
gcloud auth application-default login --scopes=https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/presentations.readonly,https://www.googleapis.com/auth/drive.readonly

# 3. Copier vers un fichier dédié pour les slides
cp ~/.config/gcloud/application_default_credentials.json ~/.config/gcloud/slides-credentials.json

# 4. Restaurer votre fichier original
mv ~/.config/gcloud/application_default_credentials.json.backup ~/.config/gcloud/application_default_credentials.json

# 5. Ajouter dans votre .envrc
export GOOGLE_APPLICATION_CREDENTIALS=~/.config/gcloud/slides-credentials.json
