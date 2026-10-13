# Actividad en vivo y sidebar estable (producto comercial)

Implementación comprobada: tests Go de orden y seguridad de API, HTML paginado SSR, actualización cada 5 segundos, enlaces/navegación, protección XSS, Chrome headless de escritorio expandido/contraído, actividad y drawer móvil con botón cerrar; build Linux Go y análisis estático aprobados. Limitación documentada: almacenamiento del historial actual está limitado a 1000 eventos; pruebas con Kaggle real pendientes en tarea 20.

- API privada con paginación inversa y respuesta acotada.
- Vista renderizada servidor de página 1 y autoactualización cada cinco segundos, con controles anteriores/siguientes.
- Revisión de accesibilidad y estabilidad del sidebar colapsado y móvil, persistencia de preferencia de interfaz.
- Tests de paginación/seguridad, browser Chrome headless (estado plegado/expandido y móvil), compilación Linux y documentación.
