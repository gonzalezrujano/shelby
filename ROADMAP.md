Próximos pasos

- vars/production.yml — soporte para variable overrides por entorno (shelby viz serve --vars vars/production.yml)
- shelby viz init --from-pipeline my-slug — scaffold automático que lee el output de un pipeline y propone widgets
- shelby viz build dashboard.yml — empaqueta todo en un único archivo HTML estático (incluye JS del motor, CSS, templates).
- disabled: true - para desactivar pasos del pipeline (show as disabled in UI and don't run)
- refresh daemon when pipelines change (add/update/remove pipeline)
- Soporte de filtros/cohorts dentro de la UI del dashboard
- desplegar imagen a Docker Hub con GitHub Actions (tests, amd64, arm64)
- Agregar soporte para scripts de JavaScript
- alias shell para CLI
- n8n? -> para poder utilizarlo en otros sistemas y compartir flujo
- agregar comando export para exportar resultados de un pipeline a un archivo