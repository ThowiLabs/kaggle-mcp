# Tarea 17 — OAuth correcto, action discovery fallido

Fecha: 2026-10-09.
- [x] Analizar logs del panel con Origin diferente, que son informativos.
- [x] Identificar en /mcp rechazo Origin exacto HTTP 403 previo a OAuth.
- [x] Consultar especificación MCP 2026-07-28 y mantener Origin validado (sin admitir webs arbitrarias).
- [x] Permitir orígenes ChatGPT conocidos y la URL del túnel, además de clientes sin Origin.
- [x] Mantener Bearer válido por petición y validación JSON, método, cabeceras y metadata.
- [x] Incluir versiones admitidas al rechazar versión desconocida.
- [x] Logs de causas de rechazo sin PIN, token, cookies ni valores de Origin.
- [x] Probar OAuth PKCE+tools/list y server/discover a través de proxy real simulado.
- [x] Validar schemas de 42 herramientas y rechazar Origins falsificados.
- [ ] Prueba final con ChatGPT y URL pública real de Kaggle/Gradio.
