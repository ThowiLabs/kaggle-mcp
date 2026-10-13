# Tarea 13 — Corregir login PIN a través de Gradio

Estado: corregido y probado localmente; falta ensayo web real desde Kaggle.
Fecha: 2026-10-09.

- [x] Identificar respuesta `solicitud inválida` originada por coincidencia estricta de `Origin` y `h.Public`.
- [x] Evitar confiar en cabecera Origin reescrita por FRP/Cloudflare.
- [x] Exigir siempre cookie de login segura y token CSRF de formulario.
- [x] Mantener sesión autenticada y token CSRF para todas las acciones posteriores.
- [x] Retener rate limits, PIN y revocación de OAuth.
- [x] Mostrar errores legibles que permitan reintentar login desde la interfaz.
- [x] Pruebas de Origin ausente/distinto, formularios sin cookie, CSRF inválido y error parseando formulario.
- [ ] Confirmar desde navegador real con URL pública Kaggle / Gradio.

Cambios solo dentro de `C:\Users\Admin\Documents\GitHub\kagssh-go`.
