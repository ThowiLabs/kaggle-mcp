# Corrección de dashboard y persistencia

Implementado y verificado con Go tests/vet, notebook simulado, build Linux y Chrome headless (desktop/500px). Métricas Linux y cookies en un túnel Kaggle real por comprobar: tarea 20 pendiente.

- Corregir CSS .ring conflictivo y coherencia de porcentajes.
- Portar monitor CPU/RAM/almacenamiento real del template Axolotl, sin cifras de simulación.
- Rutas HTML protegidas independientes (dashboard, recursos, proyectos, actividad, integraciones, skills, seguridad).
- Sesión con cookie persistente y almacenamiento de hash y CSRF privados, expiración y revocación.
- Pruebas de datos, rutas, seguridad, notebook, build Linux y revisión visual si hay navegador.
