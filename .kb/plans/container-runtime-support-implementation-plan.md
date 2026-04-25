# Plan de implementación: soporte de ejecución y acceso a ficheros en contenedores para Pando

## Resumen ejecutivo
Pando ya tiene tres puntos de abstracción útiles para introducir contenedores sin reescribir toda la arquitectura: 1) el `bash` tool ya separa ejecución local y ejecución remota vía ACP (`internal/llm/tools/bash.go`), 2) `view` y `write` ya soportan una ruta alternativa mediante `ACPClientConnection` (`internal/llm/tools/view.go`, `internal/llm/tools/write.go`), y 3) Mesnada ACP ya implementa validación de workspace, capacidades y lifecycle para terminales y archivos (`internal/mesnada/acp/client.go`, `internal/mesnada/acp/client_connection.go`, `internal/mesnada/acp/security_test.go`).

La oportunidad es introducir un contrato común de runtime/FS para que `bash`, `view`, `write`, `edit` y `patch` puedan ejecutarse sobre: host actual, Docker, Podman y en una fase posterior un runtime embebido propio. La estrategia recomendada es empezar con bind-mount del workspace dentro del contenedor para minimizar impacto en historial, permisos, locking, LSP y rutas, y solo después evolucionar hacia un runtime OCI nativo con pull/cache desde registry.

Además, la capacidad de ejecución en contenedores debe poder configurarse de forma homogénea desde CLI/config file, TUI y Web UI/API, con autodetección de Docker, Podman o runtime embebido nativo de Pando, y soporte explícito tanto para ejecución de contenedores como para uso de imágenes Docker/OCI y descarga desde registry.

## Hallazgos del codebase
- `internal/llm/tools/bash.go`: ejecuta shell persistente local mediante `shell.GetPersistentShell(config.WorkingDirectory())`; en ACP usa `CreateTerminal`, `WaitForTerminalExit` y `TerminalOutput`.
- `internal/llm/tools/shell/shell.go`: mantiene una sesión persistente de shell a nivel de proceso, con cwd mutable, timeout y cancelación.
- `internal/llm/tools/view.go`: lectura local directa con `os.Stat`/`os.Open`/`os.ReadFile`; alternativa ACP con `ReadTextFile`.
- `internal/llm/tools/write.go`: escritura local con validación de `modTime`, historial (`internal/history`), permisos (`internal/permission`) y diagnósticos LSP; alternativa ACP con `WriteTextFile`.
- `internal/llm/tools/edit.go` y `internal/llm/tools/patch.go`: dependen fuertemente de acceso a FS local, timestamps, locks y contenido actual del workspace.
- `internal/mesnada/acp/client.go`: ya define boundary de workspace, capacidades de terminal y file access, y tests de seguridad reutilizables como patrón.
- `internal/config/config.go`: hoy solo existen `ShellConfig` y `BashConfig`; no hay configuración de runtimes de contenedor.
- `internal/api/handlers_config.go`: ya existe superficie API para configuración; es un buen punto para exponer settings de runtime hacia Web UI.
- `internal/llm/agent/tools.go`: el wiring actual de tools permite inyectar nuevas implementaciones sin cambiar el contrato externo del agente.

## Decisiones arquitectónicas recomendadas
1. **No acoplar tools directamente a Docker/Podman**. Crear una abstracción `ContainerRuntime`/`WorkspaceFS` y que los tools dependan de ella.
2. **Mantener `host` como runtime por defecto** para no romper sesiones actuales.
3. **Usar bind mount del workspace como primer paso** para que los paths sigan siendo reales en host y funcionen historial/LSP/permissions con cambios mínimos.
4. **Separar ejecución de comandos y acceso a ficheros**: un runtime puede soportar exec pero no FS virtual, o viceversa.
5. **Persistencia por sesión/proyecto**: el bash tool necesita semántica persistente; conviene un contenedor persistente por sesión, no uno por comando, al menos para bash.
6. **Network y privilegios mínimos por defecto**: `network=none`, usuario no-root, rootfs read-only y mount RW solo del workspace cuando sea viable.
7. **Autodetección primero, selección explícita después**: Pando debe descubrir qué backends están instalados y luego permitir al usuario elegir `docker`, `podman`, `embedded` o `host` desde config, TUI y Web UI.
8. **Runtime embebido después del contrato estable**: primero estabilizar la interfaz con Docker/Podman y luego implementar backend propio.

## Riesgos principales
- `shell/shell.go` asume un único shell persistente global; para contenedores hará falta indexarlo por sesión+runtime+workspace.
- `write/edit/patch` hoy dependen de `os.Stat`, `os.ReadFile`, `os.WriteFile` y modtimes reales del host; si el FS deja de ser bind-mounted, habrá que rediseñar la coherencia.
- `bash.go` en ACP hace un parseo simple con `strings.Fields`; para contenedores esto puede ser insuficiente si se traslada el comando a una API `exec` con argv.
- Docker y Podman no exponen exactamente la misma experiencia: Docker suele usar daemon/socket; Podman suele ser rootless y vía REST socket. Conviene normalizar capacidades, no APIs.
- Un runtime embebido con pull desde registry implica resolver image store, formatos OCI, unpack, GC, autenticación, verificación y aislamiento del host: es una iniciativa claramente posterior.
- La UX puede quedar fragmentada si config/API/TUI/Web UI no comparten el mismo modelo de runtime, imagen, autodetección y fallback.

## Fases

### Fase 1 — Descubrimiento y abstracción base
**fact_id:** 4

Objetivo: introducir las interfaces, autodetección de engines y el modelo de configuración sin cambiar todavía el comportamiento por defecto.

Entregables:
- Nuevas interfaces, por ejemplo:
  - `CommandRuntime` o `ExecutionRuntime` para `Exec`, `StartSession`, `StopSession`, `Output`, `Kill`
  - `WorkspaceFS` para `ReadFile`, `WriteFile`, `Stat`, `MkdirAll`, `Remove`, `List`
  - `RuntimeResolver` para seleccionar `host|docker|podman|embedded`
- Configuración nueva en `internal/config/config.go` y persistencia en fichero de configuración (`.toml` y la representación usada por API/Web UI; si existe variante `.js`, mapearla también) para runtime, imagen, política de pull, socket/endpoint, mounts, red y recursos.
- Modelo de autodetección de engines disponibles: detectar Docker, Podman o runtime embebido nativo de Pando y exponer esa capacidad al resto del sistema.
- Refactor mínimo en `bash/view/write` para depender del resolved runtime/FS.
- Mantener `host` como implementación por defecto usando la lógica actual.

Cambios sugeridos:
- Crear paquete nuevo, por ejemplo `internal/runtime` o `internal/containers`.
- Mover la lógica del shell persistente local a una implementación `hostRuntime`.
- Añadir claves de contexto/metadata para saber qué runtime ha servido cada tool call.
- Añadir un servicio de discovery/capabilities para detectar sockets/binarios/configuración válida de Docker, Podman y runtime embebido.

Criterios de salida:
- Ningún cambio visible para usuarios cuando runtime=host.
- Tools compilando sobre interfaces nuevas.
- El sistema puede informar qué runtimes están instalados o disponibles.

### Fase 2 — Soporte Docker/Podman para comandos shell
**fact_id:** 5

Objetivo: ejecutar `bash` dentro de contenedores Docker y Podman con persistencia de sesión.

Entregables:
- Adaptador Docker con SDK Go del engine.
- Adaptador Podman con bindings/socket REST Go.
- Gestión de imagen: comprobar disponibilidad local, resolver imágenes Docker/OCI y hacer pull opcional según policy.
- Gestión de contenedor persistente por sesión/proyecto para emular el shell actual.
- Timeout, cancelación, obtención de stdout/stderr y cleanup.

Recomendación técnica:
- Docker: usar SDK Go oficial/compatible (`client`, `ContainerCreate`, `ContainerStart`, `ContainerExecCreate/Attach` o flujo equivalente, `ImagePull`, `CopyToContainer`/`CopyFromContainer` cuando haga falta).
- Podman: usar bindings Go (`go.podman.io/podman/.../bindings`, `images.Pull`, `containers.CreateWithSpec`, `containers.Start`, `containers.Exec...`) conectando al socket rootless/rootful.
- Preferir una imagen base configurable, minimalista, con shell explícito; no asumir `/bin/bash` siempre.

Criterios de salida:
- `bash` soporta host, docker y podman sin cambiar la API externa del tool.
- La persistencia del cwd y estado del shell queda resuelta por sesión.
- Los permisos muestran runtime e imagen solicitada.

### Fase 3 — Soporte contenedorizado para view/write/edit/patch
**fact_id:** 6

Objetivo: hacer que las operaciones de ficheros funcionen dentro del mismo workspace contenedorizado.

Estrategia recomendada:
- **Primera iteración**: bind-mount del workspace host en el contenedor. Así `view/write/edit/patch` pueden seguir operando sobre rutas del host mientras `bash` corre aislado.
- **Segunda iteración opcional**: introducir `WorkspaceFS` real para operaciones puramente dentro del contenedor usando copy/archive APIs.

Entregables:
- `WorkspaceFS` host-backed y container-backed.
- Refactor de `view.go`, `write.go`, `edit.go`, `patch.go` para usar `WorkspaceFS` y no llamadas directas a `os.*` donde corresponda.
- Alineación de `recordFileRead`/`recordFileWrite`, `withFileLock`, historial y modtimes.
- Definición de qué significa “leer antes de escribir” si el runtime no es host directo.
- Soporte consistente para imágenes base y sincronización workspace↔container cuando se active un modo no bind-mounted.

Criterios de salida:
- `view/write/edit/patch` siguen respetando seguridad, locking e historial.
- El workspace visible por shell y file tools es consistente.

### Fase 4 — Integración transversal, seguridad y UX
**fact_id:** 7

Objetivo: hacer la funcionalidad operable y segura en CLI/TUI/API/Web UI.

Entregables:
- Configuración expuesta en API, TUI, Web UI/settings y fichero de configuración persistente.
- Selector de runtime e imagen con autodetección visual de disponibilidad de Docker, Podman y runtime embebido.
- Policies de seguridad por runtime e imagen.
- Logs/observabilidad de lifecycle de contenedores.
- Tests unitarios/e2e equivalentes a ACP security tests.
- Documentación de uso, fallback behavior y prioridades de selección automática/manual.

Políticas mínimas recomendadas:
- rootless cuando el runtime lo permita
- `network=none` por defecto
- CPU/mem/pids limits configurables
- root filesystem read-only salvo mounts necesarios
- usuario no-root dentro del contenedor
- allowlist de mounts y variables de entorno
- no propagar credenciales del host por defecto

Criterios de salida:
- Usuario puede elegir runtime por proyecto/sesión/config global desde TUI, Web UI/API y fichero de configuración.
- El sistema muestra si Docker/Podman están instalados, configurables o no disponibles.
- Fallos de socket, pull o imagen ausente producen errores claros.
- Cobertura de seguridad y regresión aceptable.

### Fase 5 — Runtime embebido propio y descarga desde registry
**fact_id:** 8

Objetivo: añadir un backend propio desacoplado de Docker/Podman para ejecutar y gestionar imágenes OCI/Docker descargadas desde registry.

Alcance recomendado del MVP:
- Resolver referencias de imagen y hacer pull desde registry.
- Cache local de blobs/manifests/layers con GC básico.
- Verificación de digest y metadata de imagen.
- Unpack de rootfs y montaje de workspace.
- Exec aislado con namespaces/cgroups si el entorno lo permite, con fallback controlado si no.
- Integración del runtime embebido como opción de primera clase en config, TUI y Web UI, junto a Docker y Podman.

Bibliotecas/estándares a evaluar:
- `go-containerregistry` para pull/parseo de imágenes OCI/Docker.
- image-spec / distribution-spec / OCI layout para store local.
- `containerd`/`nerdctl`-style primitives si se decide no implementar demasiado bajo nivel desde cero.
- Solo construir aislamiento propio completo si el scope del proyecto lo justifica; si no, un “embedded image manager + delegated executor” puede ser suficiente.

Riesgos específicos:
- Complejidad alta en aislamiento seguro, mounts, cgroups y compatibilidad Linux.
- Necesidad de privilegios/capacidades del host.
- Mantenimiento considerable frente a reutilizar Docker/Podman/containerd.

Criterios de salida:
- Runtime embebido reutiliza el mismo contrato `ContainerRuntime`.
- Pull desde registry, uso de imágenes Docker/OCI y cache local funcionan sin romper Docker/Podman.
- Existe fallback limpio a runtimes externos.

## Orden recomendado de implementación
1. Contratos + discovery + config (`host` default)
2. Bash sobre Docker/Podman
3. WorkspaceFS y file tools
4. Seguridad/UX/config surfaces
5. Runtime embebido + registry

## Recomendaciones concretas de librerías Go
- **Docker**: cliente del engine Docker/Moby para `ImagePull`, `ContainerCreate/Start`, `Exec`, logs, copy/archive.
- **Podman**: bindings Go oficiales de Podman sobre socket REST.
- **Registry/OCI para fase 5**: `go-containerregistry` como base para pull, manifests, layers y auth; evaluar complementarlo con primitives OCI/containerd en vez de inventar un runtime completo desde cero desde el día uno.

## Nota final
La mejor ruta técnica es tratar Docker y Podman como primeros backends de un contrato común, compartir el mismo modelo de configuración entre fichero, TUI y Web UI/API, y usar ACP como referencia de seguridad/capabilities. El runtime embebido debe ser una fase posterior, centrada primero en image distribution/cache y solo después en aislamiento completo, para no bloquear el valor inmediato del soporte en contenedores.
