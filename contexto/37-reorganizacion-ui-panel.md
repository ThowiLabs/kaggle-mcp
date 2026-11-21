# Reorganización de la UI del panel: tarjetas + ventanas como thowilabs-mcp (2026-11-20)

- Fecha: 2026-11-20
- Objetivo: que la ESTRUCTURA del panel de kaggle-mcp sea como la de thowilabs-mcp (rejilla de tarjetas por integración, cada una con icono, nombre, descripción y estado, que abre su propia ventana con el formulario de esa integración), sin cambiar el diseño visual Axolotl actual. El usuario lo pidió así: "Integraciones tiene sus iconos y sus apartados con ventanas y todo, no un formulario de todo en una sola página. Debe ser igual de pulido que thowilabs-mcp".
- Tarea: tareas/37-reorganizacion-ui-panel-en-proceso.md
- Depende de: tarea 36 (túnel SSH en Integraciones), SIN mergear. Rama base: tarea-36-tunel-ssh-inverso (commit e31da9e). La 37 se hace sobre esa rama para que la sección SSH exista y se reorganice también.

## Decisiones

- Estructura, no diseño: se mantienen los colores, la tipografía y el CSS Axolotl (axolotl.css, dashboard.css). Solo cambia cómo se organizan las secciones: tarjetas + ventanas (modales) en vez de formularios apilados en una sola página.
- Referencia: thowilabs-mcp, rama hybrid: internal/dashboard/templates/pages/integrations.html (rejilla de tarjetas, una por integración, con icono SVG, nombre, descripción y estado, cada una enlaza a su propia página) y components/panel_modals.html (ventanas modales compartidas con formulario dentro). En kaggle-mcp se replica el patrón con modales en la misma página (sin rutas nuevas): cada tarjeta abre una ventana modal con el formulario de ESA integración dentro. Sin rutas ni valores de Page nuevos: menos superficie de cambio y mismos endpoints.
- Sin romper nada: mismos endpoints POST, mismos nombres de campo, misma sesión y CSRF. Los formularios se mueven tal cual dentro de los modales; no se cambia ningún campo, acción ni validación.
- Integraciones: 3 tarjetas (GitHub, Hugging Face, Túnel SSH) con icono, nombre, descripción corta y estado (Activo/Inactivo; el túnel muestra Activo/Conectando…/Deshabilitado/Sin configurar). El subtítulo de la página en pageDescriptions pasa de "Tokens de GitHub y Hugging Face" a "GitHub, Hugging Face y túnel SSH".
- Seguridad: la página tenía un solo formulario largo (cambiar PIN). Se divide en 2 tarjetas (Cambiar PIN, con botón que abre la ventana con el formulario; Sesión activa, con la información de la sesión) más la advertencia del túnel SSH como nota al pie.
- Proyectos: la tarjeta "Nuevo proyecto" deja de mostrar el formulario y pasa a tener un botón que abre la ventana con el formulario. El resto de Proyectos (proyectos guardados, delegación de agentes) no cambia.
- Dashboard, Recursos, Actividad y Skills ya usan tarjetas o listas y no se tocan.
- Modales: divs .modal-lite con .modal-card dentro, role="dialog" aria-modal="true", cerrados por botón, clic en el fondo y tecla Escape; al cerrar devuelven el foco al botón que los abrió. JS nuevo en dashboard.js (openModal/closeModal), sin dependencias. CSS nuevo al final de dashboard.css con la misma paleta del archivo (sin tocar axolotl.css).
- La skill skills/mobile-uiux-pro-max de thowilabs-mcp se leyó como referencia: es una skill de diseño de apps móviles (Material 3, Apple HIG, WCAG 2.2); no aporta nada específico a un panel web, así que solo se aplican sus principios generales ya presentes (objetivos táctiles grandes, estados accesibles, foco visible). No hay ninguna skill de la tienda que instalar para esto: se hace con el código directamente.
- El cuaderno notebooks/kaggle_mcp.ipynb NO cambia nada (regla firme del usuario).

## Arquitectura

- internal/dashboard/ui/page.html: la sección Integraciones pasa de 2 tarjetas apiladas con todos los formularios a una rejilla .integration-grid con 3 tarjetas-botón (data-open-modal) + 3 ventanas .modal-lite (modal-github, modal-huggingface, modal-ssh) con los formularios originales dentro, sin cambios. Seguridad pasa a 2 tarjetas + ventana modal-security con el formulario de cambiar PIN. Proyectos: la tarjeta "Nuevo proyecto" abre la ventana modal-project con el formulario.
- internal/dashboard/ui/dashboard.css: estilos nuevos al final (.integration-grid, .integration-card, .modal-lite, .modal-card, .modal-head, .modal-close), con la paleta del propio archivo.
- internal/dashboard/ui/dashboard.js: lógica de ventanas al final del IIFE (openModal, closeModal, data-open-modal / data-close-modal, clic en fondo, Escape, retorno de foco).
- internal/dashboard/dashboard.go: solo el subtítulo de /integrations en pageDescriptions.
- Sin cambios en internal/mcp, internal/sshtunnel ni en ningún endpoint.

## Librerías

- Sin dependencias nuevas. Solo HTML, CSS y JS del propio panel.

## Archivos

- Creados: contexto/37-reorganizacion-ui-panel.md, tareas/37-reorganizacion-ui-panel-en-proceso.md, internal/dashboard/integrations_test.go.
- Modificados: internal/dashboard/ui/page.html (Integraciones, Seguridad y Proyectos), internal/dashboard/ui/dashboard.css (estilos de tarjetas y ventanas), internal/dashboard/ui/dashboard.js (lógica de ventanas), internal/dashboard/dashboard.go (subtítulo de /integrations).

## Problemas

- La subida de archivos al sandbox por heredoc añade un salto de línea extra al final de cada archivo (el helper del MCP no expone write_file): los sha256 no coincidían con los locales. Se detectó comparando tamaños (1 byte de más en cada archivo) y no afectó al contenido.
- gofmt marcó el test nuevo (salto de línea final) y git diff --check marcó una línea en blanco al final de dashboard.css tras añadir los estilos.

## Soluciones

- Contenido verificado: el splice comprobó en el sandbox que cada formulario aparece una sola vez y que todas las ventanas existen; el byte extra es solo un salto de línea final, inocuo en HTML, CSS, JS, Python y Go.
- gofmt -w sobre el test y rstrip de saltos de línea finales en dashboard.css; después gofmt y diff-check limpios.

## Tests

- internal/dashboard/integrations_test.go: la página /integrations contiene la rejilla y las 3 ventanas con sus formularios (acciones /github, /huggingface, /ssh-tunnel); /security contiene la ventana modal-security con el formulario /change-pin; /projects contiene la ventana modal-project con el formulario /project.
- gofmt, go vet ./..., go test ./internal/dashboard/ y ./internal/sshtunnel/ en verde, git diff --check limpio, node --check dashboard.js.

## Seguridad

- Sin cambios en autenticación, sesión, CSRF ni endpoints. Los formularios se mueven tal cual: mismos campos, mismas acciones, misma validación en el servidor. Las ventanas son solo presentación; no añaden rutas ni exponen datos nuevos. La contraseña SSH y el PIN siguen sin mostrarse ni registrarse.

## Pendientes

- Validación real en Kaggle (tarea pendiente-20 del repo): abrir el panel con PIN propio y comprobar las ventanas de Integraciones, Seguridad y Proyectos en el navegador.
- Merge de la 36 a main (pendiente de aprobación del usuario) y después merge de la 37: la 37 depende de la 36 sin mergear.

## Próximos pasos

- Presentar el resultado al usuario y, si lo aprueba, merge de la 36 y luego de la 37 a main y push.
- El usuario también quiere portar más características de thowilabs-mcp a kaggle-mcp (secretos con URL, perfiles SSH/ssh_*, pizarra/chat, identidades, backup GitHub, Telegram): pedirle que priorice tras esta tarea.
