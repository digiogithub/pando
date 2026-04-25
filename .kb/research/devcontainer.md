<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# quiero hacer compatible mi agente ai con devcontainer, por un lado que pueda ejecutarse dentro de uno aislado y por otro que pueda lanzar un subagente dentro de un contenedor aislado, buscame opciones y posibilidades de hacerlo con librerías Golang y como podría añadir esta funcionalidad en mi agente en golang

Tu agente AI en Go puede soportar DevContainers en dos modos: **ejecutarse dentro de un devcontainer existente** consumiendo su configuración, y **orquestar subagentes aislados** lanzando contenedores programáticamente. Para ello, la base fundamental es el **Docker SDK for Go**, complementado con parsers de `devcontainer.json`.

## Librerías Go relevantes

| Librería | Propósito | Lenguaje | Relevancia |
| :-- | :-- | :-- | :-- |
| `github.com/docker/docker` | Cliente Docker nativo en Go | Go | Esencial para crear, lanzar y gestionar contenedores [^1_1] |
| `github.com/colony-2/devcontainer-go` | Interpreta `devcontainer.json` y genera configs `docker run` | Go | Prometedora para parsear configuraciones de devcontainer [^1_2] |
| `github.com/ar90n/code-code-server/devcontainer` | Parser básico de `devcontainer.json` | Go | Referencia mínima con structs [^1_3] |
| `github.com/kontainment/devcontainers-go` | Tipos/structs Go del spec | Go | Útil para serializar/deserializar configs [^1_4] |
| `devcontainers/cli` (oficial) | CLI de referencia del spec | TypeScript | No es Go nativo, pero define el estándar [^1_5] |
| `github.com/testcontainers/testcontainers-go` | Testing con contenedores | Go | Ideal para tests de integración del agente |

Para tu caso de producción, recomiendo **combinar el Docker SDK oficial con un parser propio o `colony-2/devcontainer-go`** para leer `devcontainer.json`.

## Modo 1: Tu agente ejecutándose dentro de un DevContainer

Cuando tu agente corre dentro de un devcontainer, necesita detectar que está en ese entorno y adaptar su comportamiento:

- **Detección**: verifica la existencia de variables como `DEVCONTAINER` o `.devcontainer` en rutas estándar. Muchas imágenes oficiales (como `mcr.microsoft.com/devcontainers/go`) inyectan metadatos de entorno [^1_6][^1_7].
- **Workspace**: la carpeta de trabajo suele montarse en `/workspaces/<proyecto>`. Tu agente debe usar `workspaceFolder` como directorio base.
- **Servicios vinculados**: si el proyecto usa `dockerComposeFile`, tu agente debe comunicarse con el Docker daemon del host (accesible típicamente vía socket bind).
- **Puertos y Features**: el agente puede leer `forwardPorts` y `features` de `devcontainer.json` para saber qué servicios auxiliares están disponibles [^1_8][^1_9].


## Modo 2: Lanzar subagentes en contenedores aislados

Aquí usas el Docker SDK para crear contenedores dinámicamente. La clave está en traducir la configuración de `devcontainer.json` a parámetros de `container.Config` y `container.HostConfig`:

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/docker/docker/api/types/container"
    "github.com/docker/docker/api/types/mount"
    "github.com/docker/docker/client"
)

func launchSubagent(ctx context.Context, image, workspace string) (string, error) {
    cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
    if err != nil {
        return "", err
    }

    // Configuración del contenedor (equivalente a devcontainer.json)
    config := &container.Config{
        Image: image,
        Env:   []string{"AGENT_MODE=subagent", "WORKSPACE=" + workspace},
        // Puedes añadir entrypoint/cmd del subagente
    }

    // HostConfig con aislamiento fuerte
    hostConfig := &container.HostConfig{
        CapDrop:         []string{"ALL"}, // Eliminar todas las capabilities
        CapAdd:          []string{"CHOWN", "SETGID", "SETUID"}, // Mínimas necesarias
        ReadonlyRootfs:  true,           // Filesystem solo lectura
        SecurityOpt:     []string{"no-new-privileges:true", "seccomp=default.json"},
        NetworkMode:     container.NetworkMode("bridge"),
        PidMode:         "",
        Resources: container.Resources{
            Memory:   512 * 1024 * 1024,  // 512MB límite
            CpuQuota: 50000,              // Medio core
        },
        Mounts: []mount.Mount{
            {
                Type:   mount.TypeBind,
                Source: workspace,
                Target: "/workspace",
                ReadOnly: true,
            },
            {
                Type:   mount.TypeTmpfs,
                Target: "/tmp",
                // tmpfs para escritura efímera
            },
        },
    }

    resp, err := cli.ContainerCreate(ctx, config, hostConfig, nil, nil, "")
    if err != nil {
        return "", err
    }

    if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
        return "", err
    }

    return resp.ID, nil
}
```


## Opciones de aislamiento disponibles en Go

Puedes implementar estas técnicas de hardening mediante el Docker SDK:


| Aislamiento | Flag Go (`HostConfig`) | Impacto |
| :-- | :-- | :-- |
| Drop de capabilities | `CapDrop: []string{"ALL"}` | Alto — elimina permisos del kernel [^1_10] |
| Filesystem read-only | `ReadonlyRootfs: true` | Alto — previene modificaciones [^1_10] |
| Tmpfs para escritura | `Tmpfs: map[string]string{...}` | Medio-Alto — datos volátiles |
| Seccomp personalizado | `SecurityOpt: []string{"seccomp=..."}` | Alto — filtra syscalls [^1_10] |
| Limites de recursos | `Resources.Memory`, `Resources.CpuQuota` | Medio — contiene DoS |
| Aislamiento de red | `NetworkMode: "none"` o bridge | Medio — controla conectividad |
| Sin privilegios | `SecurityOpt: []string{"no-new-privileges:true"}` | Alto — evita escalada [^1_11] |

## Arquitectura recomendada para tu agente

### 1. Capa de parsing

Implementa un parser de `devcontainer.json` que mapee a structs Go. El spec utiliza **JSON with Comments (jsonc)**, por lo que necesitarás un parser que soporte comentarios (puedes usar `github.com/tidwall/gjson` con preprocesado o `github.com/colony-2/devcontainer-go` si está maduro).

### 2. Capa de orquestación

Un `ContainerManager` que exponga métodos como:

- `RunInDevContainer(configPath string) error` — para modo auto-contenedor.
- `SpawnSubAgent(image string, spec DevContainerSpec, task Task) (AgentInstance, error)` — para subagentes.
- `StopSubAgent(id string) error` — lifecycle management.


### 3. Comunicación agente ↔ subagente

Para que el agente principal se comunique con subagentes aislados, considera:

- **gRPC/HTTP sobre Unix socket** montado como `bind` en el contenedor (más seguro que TCP expuesto).
- **Redis/NATS** en un contenedor bridge si necesitas pub/sub.
- **Volúmenes compartidos** para intercambio de resultados (en combinación con read-only).


### 4. DinD (Docker-in-Docker) si el subagente necesita Docker

Si el subagente aislado también necesita lanzar contenedores, monta `/var/run/docker.sock` con cuidado. Considera usar **rootless Docker** dentro del contenedor o **Docker-out-of-Docker** con permisos restringidos para evitar escapes [^1_12].

## Ejemplo de integración en tu agente

```go
type DevContainerRuntime struct {
    docker *client.Client
    spec   DevContainerSpec
}

func (r *DevContainerRuntime) ExecuteSubAgent(ctx context.Context, task Task) (Result, error) {
    // 1. Construir/verificar imagen según spec.Image o spec.Build
    // 2. Aplicar overrides de seguridad
    // 3. Crear y arrancar contenedor
    // 4. Exec del binario del subagente
    // 5. Capturar stdout/stderr vía ContainerExecAttach
    // 6. Cleanup automático con defer
}
```

Si tu agente necesita operar con **GitHub Codespaces** o **GitLab Workspaces**, simplemente adapta el parsing: estos servicios consumen el mismo `devcontainer.json` que el spec estándar [^1_9].

¿Tu agente necesitará que los subagentes aislados tengan acceso a Docker también (Docker-in-Docker), o es suficiente con aislamiento de filesystem y red?
<span style="display:none">[^1_13][^1_14][^1_15][^1_16][^1_17][^1_18][^1_19][^1_20][^1_21][^1_22][^1_23][^1_24][^1_25][^1_26][^1_27][^1_28][^1_29][^1_30][^1_31][^1_32][^1_33][^1_34][^1_35][^1_36][^1_37][^1_38]</span>

<div align="center">⁂</div>

[^1_1]: https://www.vitaliihonchar.com/insights/manage-docker-containers-go

[^1_2]: https://github.com/colony-2/devcontainer-go

[^1_3]: https://pkg.go.dev/github.com/ar90n/code-code-server/devcontainer

[^1_4]: https://pkg.go.dev/github.com/kontainment/devcontainers-go/pkg/devcontainers

[^1_5]: https://github.com/devcontainers/cli

[^1_6]: https://github.com/microsoft/vscode-remote-try-go/blob/main/.devcontainer/devcontainer.json

[^1_7]: https://marianarp754.github.io/comp423-course-notes/tutorials/go-setup/

[^1_8]: https://devcontainers.github.io/implementors/json_reference/

[^1_9]: https://containers.dev/implementors/spec/

[^1_10]: https://mountainss.wordpress.com/2024/11/14/enhancing-security-with-docker-container-isolation/

[^1_11]: https://docs.docker.com/security/faqs/containers/

[^1_12]: https://docs.docker.com/enterprise/security/hardened-desktop/enhanced-container-isolation/config/

[^1_13]: https://github.com/microsoft/vscode-dev-containers/blob/main/script-library/docs/go.md

[^1_14]: https://github.com/devcontainers/spec

[^1_15]: https://containers.dev/features

[^1_16]: https://github.com/devmeireles/go-devcontainer

[^1_17]: https://www.jetbrains.com/help/go/dev-container-cli.html

[^1_18]: https://pkg.go.dev/github.com/umermansoor/golang-devcontainer

[^1_19]: https://danielbautista7799.github.io/comp423-course-notes/tutorials/go-setup/

[^1_20]: https://hub.docker.com/r/microsoft/devcontainers-go

[^1_21]: https://code.visualstudio.com/docs/devcontainers/devcontainer-cli

[^1_22]: https://github.com/microsoft/vscode-remote-try-go

[^1_23]: https://docs.openhands.dev/sdk/guides/agent-server/docker-sandbox

[^1_24]: https://github.com/sarg3nt/go-dev-container

[^1_25]: https://github.com/WillFantom/devcontainer-go

[^1_26]: https://github.com/nlsantos/brig

[^1_27]: https://github.com/stuartleeks/devcontainer-cli

[^1_28]: https://www.youtube.com/watch?v=pN0t8EABsxc

[^1_29]: https://devcontainers.github.io/implementors/reference/

[^1_30]: https://docs.openhands.dev/openhands/usage/run-openhands/local-setup

[^1_31]: https://github.com/OpenHands/OpenHands/blob/main/Development.md

[^1_32]: https://docs.docker.com/enterprise/security/hardened-desktop/enhanced-container-isolation/

[^1_33]: https://code.googlesource.com/gocloud/+/refs/heads/main/.devcontainer/devcontainer.json

[^1_34]: https://bamurtaugh.github.io/dev-container-spec/implementors/json_reference/

[^1_35]: https://github.com/moby/moby/issues/47772

[^1_36]: https://github.com/qdm12/godevcontainer

[^1_37]: https://github.com/devcontainers/spec/blob/main/docs/specs/devcontainer-reference.md

[^1_38]: https://github.com/qdm12/godevcontainer/blob/master/.devcontainer/devcontainer.json


---

# hay librerías Golang para hacerlo con podman o crun o runc? podría embeber runc o el soporte de contenedores en un binario Golang?

Sí, tienes varias opciones Go nativas para alternativas a Docker, y embeber runc en un binario Go es totalmente factible.

## Librerías Go para Podman, containerd y runtimes alternativos

| Runtime | Librería Go | Tipo de integración | Ideal para |
| :-- | :-- | :-- | :-- |
| **Podman** | `github.com/containers/podman/pkg/bindings` | REST API client (gRPC/Unix socket) | Sistemas sin daemon Docker, rootless containers [^2_1][^2_2] |
| **libpod** | `github.com/containers/libpod/libpod` | Librería core de Podman (más bajo nivel) | Control granular del lifecycle [^2_3][^2_4] |
| **containerd** | `github.com/containerd/containerd` | Cliente Go nativo (gRPC al daemon) | Producción, Kubernetes, ligereza [^2_5][^2_6] |
| **runc (libcontainer)** | `github.com/opencontainers/runc/libcontainer` | Librería Go nativa sin daemon | Embeber directamente, máximo control [^2_7] |
| **nerdctl** | `github.com/containerd/nerdctl/v2/pkg/api/types` | Tipos y helpers (más CLI-oriented) | Docker-compatible sobre containerd [^2_8][^2_9] |
| **crun** | No hay librería Go nativa | Es C, se embeber como binario | Rendimiento crítico, poca memoria [^2_10] |

## Opción 1: Usar `libcontainer` de runc como librería embebida (recomendado)

`libcontainer` es el paquete Go subyacente de runc. Permite crear contenedores directamente desde Go **sin necesidad de un binario externo ni daemon** [^2_7].

```go
package main

import (
    "os"
    "github.com/opencontainers/runc/libcontainer"
    "github.com/opencontainers/runc/libcontainer/configs"
    "github.com/opencontainers/runc/libcontainer/specconv"
)

func runIsolatedSubagent() error {
    // 1. Configurar la factory con root de containers
    factory, err := libcontainer.New("/var/lib/myagent/containers", 
        libcontainer.Cgroupfs, 
        libcontainer.InitArgs(os.Args[^2_0], "init"),
    )
    if err != nil {
        return err
    }

    // 2. Crear configuración OCI-like
    config := &configs.Config{
        Rootfs: "/path/to/rootfs", // El filesystem del subagente
        Namespaces: configs.Namespaces{
            {Type: configs.NEWNS},   // Mount namespace
            {Type: configs.NEWUTS},  // UTS
            {Type: configs.NEWPID},  // PID
            {Type: configs.NEWNET},  // Network
            {Type: configs.NEWUSER}, // User namespace (rootless)
        },
        Cgroups: &configs.Cgroup{
            Resources: &configs.Resources{
                Memory: 512 * 1024 * 1024,
                CpuShares: 512,
            },
        },
        Capabilities: &configs.Capabilities{
            Bounding: []string{"CAP_CHOWN", "CAP_SETUID", "CAP_SETGID"},
            Effective: []string{"CAP_CHOWN", "CAP_SETUID", "CAP_SETGID"},
            Permitted: []string{"CAP_CHOWN", "CAP_SETUID", "CAP_SETGID"},
        },
        Readonlyfs: true,
        Mounts: []*configs.Mount{
            {
                Source:      "proc",
                Destination: "/proc",
                Device:      "proc",
                Flags:       unix.MS_NOEXEC | unix.MS_NOSUID | unix.MS_NODEV,
            },
            {
                Source:      "/workspace",
                Destination: "/workspace",
                Device:      "bind",
                Flags:       unix.MS_RDONLY | unix.MS_BIND,
            },
        },
    }

    // 3. Crear el container
    container, err := factory.Create("subagent-001", config)
    if err != nil {
        return err
    }
    defer container.Destroy()

    // 4. Ejecutar el proceso del subagente
    process := &libcontainer.Process{
        Args:   []string{"/usr/bin/my-subagent"},
        Env:    []string{"AGENT_MODE=subagent"},
        Stdin:  os.Stdin,
        Stdout: os.Stdout,
        Stderr: os.Stderr,
        Init:   true,
    }

    err = container.Run(process)
    if err != nil {
        return err
    }

    // Esperar y cleanup
    _, err = process.Wait()
    return err
}
```


## Opción 2: Embeber runc/crun como binario con `//go:embed`

Si prefieres usar el binario de runc/crun directamente, Go 1.16+ permite embeber archivos binarios en el ejecutable final [^2_11][^2_12][^2_13].

```go
package main

import (
    _ "embed"
    "os"
    "os/exec"
    "io/ioutil"
    "github.com/opencontainers/runtime-spec/specs-go"
)

//go:embed runc
var runcBinary []byte

//go:embed crun
var crunBinary []byte

func extractAndRun(runtime string, spec specs.Spec) error {
    var binary []byte
    var path string
    
    switch runtime {
    case "runc":
        binary = runcBinary
        path = "/tmp/.myagent/runc"
    case "crun":
        binary = crunBinary
        path = "/tmp/.myagent/crun"
    }
    
    // Extraer a disco (necesario para exec)
    os.MkdirAll("/tmp/.myagent", 0755)
    if err := os.WriteFile(path, binary, 0755); err != nil {
        return err
    }
    
    // Escribir config.json OCI
    if err := writeOCISpec("/tmp/.myagent/subagent", &spec); err != nil {
        return err
    }
    
    // Ejecutar runc/crun
    cmd := exec.Command(path, "run", "-b", "/tmp/.myagent/subagent", "subagent-001")
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}
```


## Opción 3: Usar containerd como backend (recomendado para producción)

Si necesitas gestión de imágenes, snapshots y redes, containerd es más completo que runc puro [^2_5][^2_6]:

```go
import (
    "context"
    "github.com/containerd/containerd"
    "github.com/containerd/containerd/cio"
    "github.com/containerd/containerd/namespaces"
    "github.com/containerd/containerd/oci"
)

func runWithContainerd(ctx context.Context, imageRef string) error {
    client, err := containerd.New("/run/containerd/containerd.sock")
    if err != nil {
        return err
    }
    defer client.Close()
    
    ctx = namespaces.WithNamespace(ctx, "myagent")
    
    image, err := client.Pull(ctx, imageRef, containerd.WithPullUnpack)
    if err != nil {
        return err
    }
    
    container, err := client.NewContainer(ctx, "subagent-001",
        containerd.WithImage(image),
        containerd.WithNewSnapshot("subagent-snap", image),
        containerd.WithNewSpec(
            oci.WithImageConfig(image),
            oci.WithProcessArgs("/usr/bin/my-subagent"),
            // Aislamiento adicional
            oci.WithPrivileged(false),
            oci.WithHostNamespace(specs.NetworkNamespace), // o sin red
        ),
    )
    if err != nil {
        return err
    }
    defer container.Delete(ctx, containerd.WithSnapshotCleanup)
    
    task, err := container.NewTask(ctx, cio.NewCreator(cio.WithStdio))
    if err != nil {
        return err
    }
    
    return task.Start(ctx)
}
```


## Opción 4: Podman bindings (para rootless sin daemon)

La ventaja de Podman es que no requiere daemon corriendo como root [^2_1][^2_14]:

```go
import (
    "context"
    "github.com/containers/podman/v4/pkg/bindings"
    "github.com/containers/podman/v4/pkg/bindings/containers"
    "github.com/containers/podman/v4/pkg/specgen"
)

func runWithPodman(ctx context.Context) error {
    conn, err := bindings.NewConnection(ctx, "unix:///run/user/1000/podman/podman.sock")
    if err != nil {
        return err
    }
    
    s := specgen.NewSpecGenerator("my-subagent-image:latest", false)
    s.CapDrop = []string{"ALL"}
    s.CapAdd = []string{"CHOWN", "SETGID", "SETUID"}
    s.ReadOnlyFilesystem = true
    s.ResourceLimits = &specs.LinuxResources{
        Memory: &specs.LinuxMemory{Limit: proto.Int64(512 * 1024 * 1024)},
    }
    
    createResponse, err := containers.CreateWithSpec(conn, s, nil)
    if err != nil {
        return err
    }
    
    return containers.Start(conn, createResponse.ID, nil)
}
```


## Cuándo usar cada opción

| Escenario | Mejor opción | Razón |
| :-- | :-- | :-- |
| Binario único autocontenido, sin dependencias externas | `libcontainer` embebido | Zero runtime deps [^2_7] |
| Necesitas pull de imágenes, snapshots, redes complejas | `containerd` | Feature-complete [^2_6] |
| Rootless, sin daemon, desarrollo local | `Podman` bindings | Sin privilegios root [^2_1] |
| Rendimiento máximo, overhead mínimo | `crun` embebido + `//go:embed` | C rápido, poca memoria [^2_10] |
| Docker-compatible, migración sencilla | `containerd` + `nerdctl` | Drop-in replacement [^2_9] |

Para tu agente AI, si buscas **distribuir un único binario Go que pueda lanzar contenedores aislados sin requerir Docker instalado**, la combinación más potente es embeber `libcontainer` de runc para creación nativa de containers, o embeber el binario de `crun` vía `//go:embed` para máximo rendimiento.

¿Qué nivel de aislamiento necesitas para los subagentes? ¿Requieren acceso a red y posibilidad de construir imágenes nuevas, o solo ejecutar procesos aislados con filesystem readonly?
<span style="display:none">[^2_15][^2_16][^2_17][^2_18][^2_19][^2_20][^2_21][^2_22][^2_23][^2_24][^2_25][^2_26][^2_27][^2_28][^2_29][^2_30]</span>

<div align="center">⁂</div>

[^2_1]: https://github.com/containers/podman/blob/main/pkg/bindings/README.md

[^2_2]: https://pkg.go.dev/github.com/containers/libpod/v2/pkg/bindings

[^2_3]: https://pkg.go.dev/github.com/containers/libpod

[^2_4]: https://pkg.go.dev/github.com/containers/libpod/libpod

[^2_5]: https://pkg.go.dev/github.com/containerd/containerd

[^2_6]: https://containerd.io/docs/2.2/getting-started/

[^2_7]: https://pkg.go.dev/github.com/opencontainers/runc/libcontainer

[^2_8]: https://pkg.go.dev/github.com/containerd/nerdctl/v2/pkg/api/types

[^2_9]: https://dev.to/lovestaco/nerdctl-a-docker-compatible-cli-for-containerd-4i2l

[^2_10]: https://www.redhat.com/en/blog/introduction-crun

[^2_11]: https://gobyexample.com/embed-directive

[^2_12]: https://www.reddit.com/r/golang/comments/llv8da/go_116_embed_and_execute_binary_files/

[^2_13]: https://dathan.github.io/blog/posts/embed-executables-inside-go-programs/

[^2_14]: https://podman.io/blogs/2020/08/10/podman-go-bindings.html

[^2_15]: https://www.reddit.com/r/podman/comments/110j7i3/running_containers_from_go_program_rest_api_or_lib/

[^2_16]: https://podman.io/blogs/2020/08/10/podman-go-bindings

[^2_17]: https://www.baeldung.com/java-golang-invoke-function

[^2_18]: https://insujang.github.io/2020-11-09/building-container-image-inside-container-using-buildah/

[^2_19]: https://pkg.go.dev/github.com/opencontainers/runtime-tools

[^2_20]: https://docs.podman.io/en/v4.3/Tutorials.html

[^2_21]: https://pkg.go.dev/github.com/containers/buildah

[^2_22]: https://github.com/containerd/containerd/blob/main/docs/client-opts.md

[^2_23]: https://www.youtube.com/watch?v=C5fYUnnQ5ZA

[^2_24]: https://blog.mobyproject.org/getting-started-with-containerd-a81fa090982f

[^2_25]: https://github.com/opencontainers/runc/blob/master/libcontainer/container_linux.go

[^2_26]: https://github.com/containerd/containerd/issues/3593

[^2_27]: https://pkg.go.dev/github.com/opencontainers/runc

[^2_28]: https://stackoverflow.com/questions/44841488/embed-executable-binary-with-libraries-to-a-golang-app-and-run-it

[^2_29]: https://stackoverflow.com/questions/53528416/docker-golang-client-within-docker-container

[^2_30]: https://pkg.go.dev/codeberg.org/msantos/embedexe


---

# puedo hacer o soportar con libcontainer el pull de imágenes de repositorios públicos y privados? dame ejemplos

No. `libcontainer` de runc **no sabe nada de imágenes ni registries** — solo ejecuta contenedores a partir de un `rootfs` ya desempaquetado en disco [^3_1][^3_2]. Para hacer pull desde Docker Hub, registries privados o cualquier registry, necesitas combinar `libcontainer` con una librería de imagen.

La solución más coherente con tu enfoque de no-dependencias-daemon es usar **`github.com/containers/image/v5`** + **`github.com/containers/storage`**. Son las mismas librerías que usa Podman/Buildah y están diseñadas para trabajar sin Docker [^3_3][^3_4].

## Stack completo: pull de imagen → rootfs → libcontainer

```
Registry → containers/image → containers/storage (layers + mount) → libcontainer (runtime OCI)
```


## Ejemplo 1: Pull desde registry público (Docker Hub)

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/containers/image/v5/copy"
    "github.com/containers/image/v5/signature"
    "github.com/containers/image/v5/storage"
    transport "github.com/containers/image/v5/transports/alltransports"
    imagetypes "github.com/containers/image/v5/types"
    containerstorage "github.com/containers/storage"
    "github.com/containers/storage/pkg/reexec"
)

func main() {
    // reexec trick necesario para storage
    if reexec.Init() {
        return
    }

    ctx := context.Background()

    // 1. Crear storage store
    storeOptions, err := containerstorage.DefaultStoreOptions(false, 0)
    if err != nil {
        panic(err)
    }
    // Personalizar paths si quieres todo embebido:
    // storeOptions.GraphRoot = "/var/lib/myagent/storage"
    // storeOptions.RunRoot = "/var/run/myagent/storage"
    
    store, err := containerstorage.GetStore(storeOptions)
    if err != nil {
        panic(err)
    }

    // 2. Parsear imagen fuente
    imageName := "docker://alpine:latest"
    srcRef, err := transport.ParseImageName(imageName)
    if err != nil {
        panic(err)
    }

    // 3. Referencia destino en storage local
    dstName := imageName
    if srcRef.DockerReference() != nil {
        dstName = srcRef.DockerReference().String()
    }
    dstRef, err := storage.Transport.ParseStoreReference(store, dstName)
    if err != nil {
        panic(err)
    }

    // 4. Política de firma (permissiva para demo)
    systemCtx := &imagetypes.SystemContext{}
    policy, err := signature.DefaultPolicy(systemCtx)
    if err != nil {
        panic(err)
    }
    policyCtx, err := signature.NewPolicyContext(policy)
    if err != nil {
        panic(err)
    }
    defer policyCtx.Destroy()

    // 5. Pull!
    copyOpts := &copy.Options{
        ReportWriter: os.Stdout,
    }
    manifest, err := copy.Image(ctx, policyCtx, dstRef, srcRef, copyOpts)
    if err != nil {
        panic(err)
    }

    // 6. Crear contenedor writable a partir de la imagen
    images, err := store.Images()
    if err != nil {
        panic(err)
    }
    
    var imageID string
    for _, img := range images {
        // Buscar la imagen que acabamos de pull
        imageID = img.ID
    }

    container, err := store.CreateContainer("", nil, imageID, "", "", nil)
    if err != nil {
        panic(err)
    }

    // 7. Mount → obtienes el rootfs para libcontainer
    mountPoint, err := store.Mount(container.ID)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Rootfs montado en: %s\n", mountPoint)
    fmt.Printf("Usa %s como Rootfs en tu config libcontainer\n", mountPoint)

    // Aquí pasas mountPoint a libcontainer...
    // runLibcontainer(mountPoint)

    // Cleanup al final
    store.Unmount(container.ID)
    store.DeleteContainer(container.ID)
}
```


## Ejemplo 2: Pull desde registry privado con autenticación

Para registries privados (GitHub Container Registry, GitLab, Amazon ECR, Azure ACR, tu propio Harbor), usas `SystemContext.DockerAuthConfig` o `DockerCertPath` [^3_5][^3_6]:

```go
package main

import (
    "context"
    "fmt"
    "os"

    "github.com/containers/image/v5/copy"
    "github.com/containers/image/v5/signature"
    "github.com/containers/image/v5/storage"
    transport "github.com/containers/image/v5/transports/alltransports"
    imagetypes "github.com/containers/image/v5/types"
    containerstorage "github.com/containers/storage"
    "github.com/containers/storage/pkg/reexec"
)

func main() {
    if reexec.Init() {
        return
    }

    ctx := context.Background()

    // Configurar storage
    storeOptions, _ := containerstorage.DefaultStoreOptions(false, 0)
    store, _ := containerstorage.GetStore(storeOptions)

    // === AUTHENTICACIÓN PARA REGISTRY PRIVADO ===
    systemCtx := &imagetypes.SystemContext{
        // Opción A: Basic auth directo (usuario + token/password)
        DockerAuthConfig: &imagetypes.DockerAuthConfig{
            Username: "tu-usuario",
            Password: "tu-token-o-password", // o personal access token
        },

        // Opción B: Para registries con certificados TLS propios
        // DockerCertPath: "/etc/myagent/certs/registry.example.com",
        // TLSVerify:    imagetypes.NewOptionalBool(false), // solo para dev/testing

        // Opción C: Docker config.json (lee ~/.docker/config.json)
        // AuthFilePath: "/root/.docker/config.json",

        // Opción D: Registry mirror / configuración especial
        // RegistriesDirPath: "/etc/containers/registries.d",
    }

    // Imagen privada
    imageName := "docker://registry.example.com:5000/mi-proyecto/subagent:v1.2"
    srcRef, err := transport.ParseImageName(imageName)
    if err != nil {
        panic(err)
    }

    dstName := srcRef.DockerReference().String()
    dstRef, _ := storage.Transport.ParseStoreReference(store, dstName)

    policy, _ := signature.DefaultPolicy(systemCtx)
    policyCtx, _ := signature.NewPolicyContext(policy)
    defer policyCtx.Destroy()

    copyOpts := &copy.Options{
        ReportWriter: os.Stdout,
    }

    manifest, err := copy.Image(ctx, policyCtx, dstRef, srcRef, copyOpts)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Pull completado, manifest: %s\n", string(manifest))
}
```


## Ejemplo 3: Usar `containers-auth.json` (múltiples registries)

Para manejar múltiples credenciales de forma elegante, usa el archivo `containers-auth.json` que `containers/image` lee automáticamente [^3_5]:

```go
// ~/.config/containers/auth.json
// {
//   "auths": {
//     "docker.io": { "auth": "base64(user:pass)" },
//     "ghcr.io":   { "auth": "base64(user:token)" },
//     "registry.gitlab.com": { "auth": "..." }
//   }
// }

// En tu código, solo apuntas al auth file:
systemCtx := &imagetypes.SystemContext{
    AuthFilePath: "/root/.config/containers/auth.json",
    // o usa el default: /run/user/$UID/containers/auth.json
}
```


## Alternativa más moderna: `go-containerregistry` (Google)

Si prefieres una API más limpia y moderna, `github.com/google/go-containerregistry` soporta pull y push con autenticación nativa [^3_7][^3_8][^3_9]:

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/google/go-containerregistry/pkg/authn"
    "github.com/google/go-containerregistry/pkg/crane"
    "github.com/google/go-containerregistry/pkg/v1/remote"
)

func main() {
    imageRef := "registry.example.com/mi-proyecto/subagent:v1.2"

    // Opción A: Keychain que lee ~/.docker/config.json automáticamente
    img, err := crane.Pull(imageRef, crane.WithAuthFromKeychain(authn.DefaultKeychain))
    if err != nil {
        panic(err)
    }

    // Opción B: Auth directo
    auth := &authn.Basic{
        Username: "usuario",
        Password: "token",
    }
    img, err = crane.Pull(imageRef, crane.WithAuth(auth))
    if err != nil {
        panic(err)
    }

    // Guardar como tarball para extraer capas
    if err := crane.Save(img, imageRef, "/tmp/subagent.tar"); err != nil {
        panic(err)
    }

    fmt.Println("Imagen descargada. Extraer capas para usar con libcontainer...")

    // Extraer el tarball a un directorio rootfs (puedes usar archive/tar o exec tar)
    // El directorio resultante es tu Rootfs para libcontainer
}
```


## Flujo completo para tu agente

```
┌─────────────────────────────────────────┐
│  1. Parse devcontainer.json             │
│     → obtener image: "ghcr.io/..."       │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│  2. Pull con containers/image           │
│     → SystemContext + auth              │
│     → almacena en containers/storage    │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│  3. Mount con storage → rootfs path     │
│     → /var/lib/containers/.../merged    │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│  4. libcontainer.New() + .Create()    │
│     → Rootfs: mountPoint                │
│     → CapDrop ALL, Readonlyfs true      │
│     → Cgroups limits                    │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│  5. container.Run(process)               │
│     → subagent aislado corriendo        │
└─────────────────────────────────────────┘
```


## Autenticación por registry type

| Registry | Tipo de credencial | Ejemplo |
| :-- | :-- | :-- |
| Docker Hub | Username + password o PAT | `DockerAuthConfig{Username, Password}` [^3_5] |
| GitHub GHCR | Token de `GITHUB_TOKEN` | `Password: ghp_...` |
| GitLab | Deploy token o PAT | `DockerAuthConfig{Username, Password}` |
| Amazon ECR | Token temporal | `authn.DefaultKeychain` detecta helper [^3_9] |
| Azure ACR | Service principal o admin | `DockerAuthConfig` |
| Self-signed TLS | `DockerCertPath` + `TLSVerify` | `SystemContext{DockerCertPath}` |
| Sin TLS (dev) | `TLSVerify: OptionalBool(false)` | Solo para testing [^3_10] |

La ventaja de `containers/image` es que soporta **múltiples transports** (`docker://`, `containers-storage://`, `dir://`, `oci://`, `docker-archive://`) y es el mismo código que usa `podman pull` y `skopeo copy` [^3_4][^3_11]. Si tu agente embebe este stack, tiene pull nativo de cualquier registry sin dependencias externas.

¿Quieres que prepare un ejemplo integrado que combine pull de imagen + libcontainer runtime en una sola función con cleanup automático?
<span style="display:none">[^3_12][^3_13][^3_14][^3_15][^3_16][^3_17][^3_18][^3_19][^3_20][^3_21][^3_22][^3_23][^3_24][^3_25][^3_26][^3_27][^3_28]</span>

<div align="center">⁂</div>

[^3_1]: https://echorand.me/posts/libcontainer-runc-fedora-24/

[^3_2]: https://labs.iximiuz.com/challenges/start-container-with-runc

[^3_3]: https://iximiuz.com/en/posts/working-with-container-images-in-go/

[^3_4]: https://pkg.go.dev/github.com/containers/image/v5

[^3_5]: https://github.com/containers/image/blob/master/docs/containers-auth.json.5.md

[^3_6]: https://gist.github.com/developer-guy/2664e217d080f9a3ba34a52e3ed67425

[^3_7]: https://github.com/google/go-containerregistry/blob/main/cmd/crane/doc/crane.md

[^3_8]: https://godocs.io/github.com/google/go-containerregistry/pkg/crane

[^3_9]: https://github.com/google/go-containerregistry/blob/main/pkg/authn/README.md

[^3_10]: https://access.redhat.com/solutions/5750401

[^3_11]: https://github.com/paralin/containers-image

[^3_12]: https://stackoverflow.com/questions/75493423/pull-a-docker-image-to-local-registry-using-golang

[^3_13]: https://golang.testcontainers.org/features/docker_auth/

[^3_14]: https://github.com/moby/moby/issues/34503

[^3_15]: https://blog.csdn.net/guoguangwu/article/details/125690700

[^3_16]: https://manpages.debian.org/experimental/golang-github-containers-image/containers-signature.5.en.html

[^3_17]: https://tech.michaelaltfield.net/2024/09/03/container-download-curl-wget/

[^3_18]: https://www.reddit.com/r/golang/comments/achq6u/my_first_go_project_roots_pull_containers_from/

[^3_19]: https://benjamintoll.com/2022/01/18/on-runc/

[^3_20]: https://github.com/containers/image/blob/main/copy/copy.go

[^3_21]: https://pkg.go.dev/github.com/containers/image/v5/types

[^3_22]: https://pkg.go.dev/github.com/containers/image/v5/copy

[^3_23]: https://github.com/google/go-containerregistry/issues/1976

[^3_24]: https://github.com/containers/image/blob/main/copy/single.go

[^3_25]: https://developers.plane.so/self-hosting/methods/clone-docker-images

[^3_26]: https://github.com/moby/moby/issues/37882

[^3_27]: https://manpages.debian.org/experimental/golang-github-containers-image/containers-registries.conf.5.en.html

[^3_28]: https://gist.github.com/imjasonh/5f0053abe38349067935cd7bf59fd4b1


---

# prepara un ejemplo

Aquí tienes el ejemplo completo listo para compilar y ejecutar. He generado un binario Go autocontenido que hace pull de imágenes y lanza subagentes aislados con `libcontainer` .

## Cómo funciona el ejemplo

El flujo completo en un solo binario:

```
Registry → containers/image (pull + auth) → containers/storage (overlay mount) → libcontainer (runtime aislado)
```


## Archivos generados

| Archivo | Descripción |
| :-- | :-- |
| `devcontainer_libcontainer_example.go` | Código fuente completo del agente |
| `go.mod.example` | Dependencias necesarias |
| `README.md` | Guía de uso y build |

## Código principal

### `devcontainer_libcontainer_example.go`

```go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/storage"
	transport "github.com/containers/image/v5/transports/alltransports"
	imagetypes "github.com/containers/image/v5/types"
	containerstorage "github.com/containers/storage"
	"github.com/containers/storage/pkg/reexec"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"golang.org/x/sys/unix"
)

// DevContainerRuntime gestiona pull de imágenes y ejecución con libcontainer
type DevContainerRuntime struct {
	store   containerstorage.Store
	factory libcontainer.Factory
	ctx     context.Context
}

// NewDevContainerRuntime inicializa storage y libcontainer factory
func NewDevContainerRuntime(ctx context.Context, storageRoot string) (*DevContainerRuntime, error) {
	// reexec trick necesario para containers/storage
	if reexec.Init() {
		os.Exit(0)
	}

	// Storage con overlay driver
	storeOptions := containerstorage.StoreOptions{
		GraphRoot:         storageRoot + "/storage",
		RunRoot:           storageRoot + "/run",
		GraphDriverName:   "overlay",
		GraphDriverOptions: []string{"overlay.mountopt=nodev"},
	}

	store, err := containerstorage.GetStore(storeOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to get store: %w", err)
	}

	// Factory libcontainer con cgroupfs
	factory, err := libcontainer.New(
		storageRoot+"/libcontainer",
		libcontainer.Cgroupfs,
		libcontainer.InitArgs(os.Args[^4_0], "init"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create factory: %w", err)
	}

	return &DevContainerRuntime{
		store:   store,
		factory: factory,
		ctx:     ctx,
	}, nil
}

// PullImage descarga imagen del registry al storage local
func (r *DevContainerRuntime) PullImage(imageName string, auth *imagetypes.DockerAuthConfig) error {
	srcRef, err := transport.ParseImageName("docker://" + imageName)
	if err != nil {
		return fmt.Errorf("parse image: %w", err)
	}

	dstRef, err := storage.Transport.ParseStoreReference(r.store, imageName)
	if err != nil {
		return fmt.Errorf("parse store ref: %w", err)
	}

	systemCtx := &imagetypes.SystemContext{}
	if auth != nil {
		systemCtx.DockerAuthConfig = auth
	}
	// systemCtx.AuthFilePath = os.Getenv("HOME") + "/.config/containers/auth.json"

	policy, _ := signature.DefaultPolicy(systemCtx)
	policyCtx, _ := signature.NewPolicyContext(policy)
	defer policyCtx.Destroy()

	_, err = copy.Image(r.ctx, policyCtx, dstRef, srcRef, &copy.Options{ReportWriter: os.Stdout})
	if err != nil {
		return fmt.Errorf("copy image: %w", err)
	}
	return nil
}

// RunSubAgent ejecuta un subagente aislado desde imagen ya pulleada
func (r *DevContainerRuntime) RunSubAgent(imageName, containerID string, cmd []string, stdin, stdout, stderr *os.File) (*libcontainer.Process, error) {
	// Buscar imagen en storage local
	images, err := r.store.Images()
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	var targetImage *containerstorage.Image
	for i := range images {
		names, _ := r.store.Names(images[i].ID)
		for _, name := range names {
			if name == imageName || images[i].ID == imageName {
				targetImage = &images[i]
				break
			}
		}
	}
	if targetImage == nil {
		return nil, fmt.Errorf("image %s not found locally", imageName)
	}

	// Crear contenedor writable desde la imagen
	c, err := r.store.CreateContainer(containerID, nil, targetImage.ID, "", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	// Mount overlay → rootfs merged
	rootfs, err := r.store.Mount(c.ID)
	if err != nil {
		r.store.DeleteContainer(c.ID)
		return nil, fmt.Errorf("mount: %w", err)
	}
	defer r.store.Unmount(c.ID)

	// Configuración libcontainer con aislamiento fuerte
	config := &configs.Config{
		Rootfs: rootfs,
		Namespaces: configs.Namespaces{
			{Type: configs.NEWNS},
			{Type: configs.NEWUTS},
			{Type: configs.NEWPID},
			{Type: configs.NEWNET},
			{Type: configs.NEWUSER},
			{Type: configs.NEWIPC},
		},
		Cgroups: &configs.Cgroup{
			Name:   containerID,
			Parent: "system",
			Resources: &configs.Resources{
				Memory:     256 * 1024 * 1024,
				MemorySwap: 256 * 1024 * 1024,
				CpuShares:  512,
				PidsLimit:  64,
			},
		},
		Capabilities: &configs.Capabilities{
			Bounding:  []string{"CAP_CHOWN", "CAP_SETGID", "CAP_SETUID", "CAP_KILL"},
			Effective: []string{"CAP_CHOWN", "CAP_SETGID", "CAP_SETUID", "CAP_KILL"},
			Permitted: []string{"CAP_CHOWN", "CAP_SETGID", "CAP_SETUID", "CAP_KILL"},
			Ambient:   []string{},
		},
		Readonlyfs: true,
		Mounts: []*configs.Mount{
			{
				Source: "proc", Destination: "/proc", Device: "proc",
				Flags: unix.MS_NOEXEC | unix.MS_NOSUID | unix.MS_NODEV,
			},
			{
				Source: "tmpfs", Destination: "/tmp", Device: "tmpfs",
				Flags: unix.MS_NOSUID | unix.MS_NODEV,
				Data:  "size=64m,mode=1777",
			},
			{
				Source: "tmpfs", Destination: "/var/tmp", Device: "tmpfs",
				Flags: unix.MS_NOSUID | unix.MS_NODEV,
				Data:  "size=32m,mode=1777",
			},
			{
				Source: "tmpfs", Destination: "/run", Device: "tmpfs",
				Flags: unix.MS_NOSUID | unix.MS_NODEV,
				Data:  "size=32m,mode=755",
			},
		},
		Hostname:        containerID,
		RootlessEUID:    os.Geteuid() != 0,
		RootlessCgroups: os.Geteuid() != 0,
	}

	lc, err := r.factory.Create(containerID, config)
	if err != nil {
		return nil, fmt.Errorf("libcontainer create: %w", err)
	}
	defer lc.Destroy()

	process := &libcontainer.Process{
		Args:   cmd,
		Env:    []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		Init:   true,
	}

	if err := lc.Run(process); err != nil {
		return nil, fmt.Errorf("run: %w", err)
	}
	return process, nil
}

func init() {
	if len(os.Args) > 1 && os.Args[^4_1] == "init" {
		factory, _ := libcontainer.New("", libcontainer.Cgroupfs)
		if err := factory.StartInitialization(); err != nil {
			fmt.Fprintf(os.Stderr, "init error: %v\n", err)
			os.Exit(1)
		}
		panic("unexpected init exit")
	}
}

func main() {
	ctx := context.Background()

	rt, err := NewDevContainerRuntime(ctx, "/var/lib/myagent")
	if err != nil {
		panic(err)
	}

	// Pull público
	rt.PullImage("alpine:latest", nil)

	// Pull privado (descomenta según necesidad)
	// rt.PullImage("ghcr.io/org/subagent:v1", &imagetypes.DockerAuthConfig{
	//     Username: "myuser", Password: "ghp_...",
	// })

	// Ejecutar subagente aislado
	proc, err := rt.RunSubAgent(
		"alpine:latest",
		"subagent-001",
		[]string{"/bin/sh", "-c", "echo Hello isolated subagent; uname -a; id"},
		nil, os.Stdout, os.Stderr,
	)
	if err != nil {
		panic(err)
	}

	ps, _ := proc.Wait()
	fmt.Printf("Exit code: %d\n", ps.ExitCode())
}
```


## Build y ejecución

```bash
# Crear módulo
cp output/go.mod.example go.mod
go mod tidy

# Compilar
go build -o agent output/devcontainer_libcontainer_example.go

# Ejecutar (requiere root o subuid/subgid configurado para rootless)
sudo ./agent
```


## Autenticación soportada

| Método | Cómo implementarlo |
| :-- | :-- |
| Auth directo | `DockerAuthConfig{Username, Password}` |
| `auth.json` | `SystemContext.AuthFilePath = "/path/to/auth.json"` [^4_1] |
| Docker config | Apunta a `~/.docker/config.json` |
| Certificado TLS | `DockerCertPath` + `TLSVerify` |

## Puntos clave del aislamiento

- **CapDrop ALL** + solo 4 capabilities: el subagente no puede hacer casi nada al nivel del kernel
- **Readonly rootfs**: la imagen es inmutable; solo puede escribir en tmpfs
- **Cgroups v1/v2**: límite de RAM (256MB), CPU (512 shares), PIDs (64)
- **6 namespaces**: aísla filesystem, hostname, procesos, red, usuarios e IPC
- **Overlay storage**: las capas de imagen son de solo lectura, el container layer es el único writable

Para adaptar esto a tu `devcontainer.json`, necesitarás parsear `image`, `runArgs` (mapear a `config.Capabilities` y `config.Mounts`) y `features` (instalar tools en el rootfs antes de lanzar). ¿Quieres que añada un parser de `devcontainer.json` integrado en este mismo ejemplo?
<span style="display:none">[^4_10][^4_11][^4_12][^4_13][^4_14][^4_15][^4_16][^4_2][^4_3][^4_4][^4_5][^4_6][^4_7][^4_8][^4_9]</span>

<div align="center">⁂</div>

[^4_1]: https://github.com/containers/image/blob/master/docs/containers-auth.json.5.md

[^4_2]: https://github.com/containers/storage/blob/main/drivers/overlay/overlay.go

[^4_3]: https://github.com/containers/buildah/issues/1831

[^4_4]: https://stackoverflow.com/questions/79016337/podman-volume-overlay-mount-for-root-of-filesystem

[^4_5]: https://oneuptime.com/blog/post/2026-03-18-use-overlay-storage-driver-podman/view

[^4_6]: https://gvisor.dev/blog/2023/05/08/rootfs-overlay/

[^4_7]: https://pkg.go.dev/github.com/ericchiang/kubernetes@v1.1.2/Godeps/_workspace/src/github.com/docker/libcontainer

[^4_8]: https://iximiuz.com/en/posts/working-with-container-images-in-go/

[^4_9]: https://terenceli.github.io/技术/2021/12/23/runc-internals-2

[^4_10]: https://docs.docker.com/engine/storage/drivers/overlayfs-driver/

[^4_11]: https://pkg.go.dev/github.com/docker/libcontainer

[^4_12]: https://github.com/efrecon/docker-image-extract

[^4_13]: https://github.com/opencontainers/runc/blob/v1.0.0-rc3/libcontainer/container_linux.go

[^4_14]: https://people.redhat.com/vgoyal/papers-presentations/vault-2017/vivek-overlayfs-and-containers-presentation-valult-2017.pdf

[^4_15]: https://godocs.io/github.com/docker/libnetwork/Godeps/_workspace/src/github.com/opencontainers/runc/libcontainer

[^4_16]: https://pkg.go.dev/github.com/containers/image/v5/copy


---

# añade el parser

Aquí tienes el ejemplo completo con parser de `devcontainer.json` integrado .

## Archivos generados

| Archivo | Propósito |
| :-- | :-- |
| `devcontainer_complete_example.go` | Código fuente completo: parser + pull + libcontainer |
| `devcontainer.json.example` | Ejemplo de `devcontainer.json` que el parser consume |

## Qué parsea el código

El parser soporta las propiedades principales del spec [^5_1][^5_2]:


| Propiedad | Mapeo a libcontainer |
| :-- | :-- |
| `image` | Imagen a descargar vía `containers/image` |
| `build.dockerfile` | No implementado en este ejemplo (requiere build) |
| `containerEnv` | Variables de entorno del proceso |
| `containerUser` / `remoteUser` | User del proceso libcontainer |
| `workspaceFolder` | Directorio de trabajo por defecto |
| `workspaceMount` | Mount bind del workspace |
| `mounts[]` | Mounts adicionales parseados |
| `runArgs` | `--cap-add`, `--cap-drop`, `--security-opt`, `--read-only`, `--memory`, `--cpus`, `--network`, `--mount`, `-v` |
| `postCreateCommand` | Comando a ejecutar tras crear el container |
| `forwardPorts` | Parseado pero sin implementar red puente en este ejemplo |
| `features` | Parseado pero sin instalar en este ejemplo |

## Build y ejecución

```bash
# 1. Crear módulo Go
mkdir agent-devcontainer && cd agent-devcontainer
go mod init github.com/josefran/agent-devcontainer

# 2. Copiar archivos
cp output/devcontainer_complete_example.go main.go
cp output/devcontainer.json.example .devcontainer/devcontainer.json

# 3. Dependencias
go mod tidy

# 4. Compilar
go build -o agent .

# 5. Ejecutar (root o rootless con subuid)
sudo ./agent
```


## Cómo funciona el parser

### 1. `ParseDevContainerJSON(path)`

```go
devConfig, err := ParseDevContainerJSON(".devcontainer/devcontainer.json")
```

- Lee el archivo
- **Elimina comentarios JSONC** (`//` línea y `/* */` bloque)
- Deserializa en `DevContainerConfig`
- Aplica defaults (`workspaceFolder = "/workspace"`)


### 2. `ApplyDevContainer(cfg, workspaceRoot)`

```go
parsed, err := ApplyDevContainer(devConfig, "/home/josefran/projects/my-project")
```

Resuelve:

- **`ResolvedImage`**: la imagen a descargar
- **Capabilities**: traduce `--cap-add=CHOWN` → `CAP_CHOWN`
- **Cgroups**: traduce `--memory=256m` → `256*1024*1024` bytes
- **Mounts**: parsea `--mount type=bind,source=...,target=...` y `-v /host:/container:ro`
- **Workspace mount**: por defecto o el `workspaceMount` explícito
- **Post create command**: string → `[]string{"/bin/sh", "-c", cmd}` o array directo
- **Variables**: interpolación básica `${localEnv:VAR}`


### 3. `buildLibcontainerConfig(parsed, rootfs, containerID)`

Genera `configs.Config` con:

- 6 namespaces activados
- Capabilities aplicadas desde `runArgs`
- Cgroups limits desde `runArgs`
- Mounts: proc + tmpfs + workspace + extras
- `Readonlyfs` si `--read-only`
- `NoNewPrivileges` si `security-opt no-new-privileges:true`


### 4. Ejecución

```go
process, err := rt.RunDevContainer(parsed, "subagent-001", cmd, nil, os.Stdout, os.Stderr)
exitCode, err := rt.WaitSubAgent(process)
```


## Ejemplo de devcontainer.json soportado

```json
{
    "name": "My AI Agent DevContainer",
    "image": "alpine:latest",
    "workspaceFolder": "/workspace",
    "workspaceMount": "source=${localWorkspaceFolder},target=/workspace,type=bind",
    "containerEnv": {
        "MY_VAR": "hello-from-devcontainer",
        "AGENT_MODE": "subagent"
    },
    "remoteUser": "vscode",
    "runArgs": [
        "--cap-drop=ALL",
        "--cap-add=CHOWN",
        "--cap-add=SETGID",
        "--cap-add=SETUID",
        "--security-opt", "no-new-privileges:true",
        "--read-only",
        "--memory=256m",
        "--cpus=0.5"
    ],
    "mounts": [
        "source=/tmp/agent-cache,target=/cache,type=bind,readonly"
    ],
    "postCreateCommand": "echo 'Container ready' && id"
}
```


## Flujo completo del agente

```
┌─────────────────┐    ┌─────────────────────┐    ┌──────────────────┐
│ devcontainer.json│───▶│ ParseDevContainerJSON│───▶│ ApplyDevContainer│
│ (.devcontainer/) │    │ (strip JSONC comments) │    │ (resolve mounts, │
└─────────────────┘    └─────────────────────┘    │ env, caps, cgroups)│
                                                    └────────┬─────────┘
                                                             │
┌────────────────────────────────────────────────────────────┘
│
▼
┌─────────────────────┐    ┌──────────────────┐    ┌─────────────┐
│ PullImage(resolved) │───▶│ storage.Create   │───▶│ storage.Mount│
│ (containers/image)   │    │ + libcontainer   │    │ (overlay    │
│                      │    │   .Create+Run      │    │  rootfs)     │
└─────────────────────┘    └──────────────────┘    └──────┬──────┘
                                                          │
                                                          ▼
                                                   ┌──────────────┐
                                                   │ Subagent     │
                                                   │ Aislado      │
                                                   └──────────────┘
```


## Features no implementados en este ejemplo (para extender)

- **`build.dockerfile`**: requiere `containers/buildah` o `docker build` para compilar la imagen antes del pull
- **`features`**: requiere descargar scripts de `ghcr.io/devcontainers/features/...` y ejecutarlos en el rootfs antes de `RunDevContainer`
- **`dockerComposeFile`**: requiere orquestar múltiples containers (fuera de alcance de libcontainer puro)
- **`forwardPorts`**: requiere configurar port forwarding desde el namespace de red del container
- **`overrideFeatureInstallOrder`**: requiere topología de dependencias entre features
- **Seccomp profiles personalizados**: requiere cargar JSON de syscall filter

¿Quieres que implemente alguna de estas extensiones? El más útil para un agente AI sería probablemente **Features** (instalar herramientas automáticamente) o **forwardPorts** (exponer servicios del subagente).
<span style="display:none">[^5_10][^5_11][^5_12][^5_13][^5_14][^5_15][^5_3][^5_4][^5_5][^5_6][^5_7][^5_8][^5_9]</span>

<div align="center">⁂</div>

[^5_1]: https://bamurtaugh.github.io/dev-container-spec/implementors/json_reference/

[^5_2]: https://containers.dev/implementors/json_reference/

[^5_3]: https://github.com/devcontainers/spec/blob/main/schemas/devContainer.schema.json

[^5_4]: https://containers.dev/implementors/json_schema/

[^5_5]: https://evuzv.shop/custom/97fca9b1/tara/devcontainers/spec/blob/main/docs/specs/devcontainerjson-reference.md

[^5_6]: https://stackoverflow.com/revisions/70219806/5

[^5_7]: https://github.com/devcontainers/spec/blob/main/docs/specs/devcontainer-features.md

[^5_8]: https://stackoverflow.com/questions/69577685/vscode-extension-remote-container-does-not-allow-me-use-workspacemount

[^5_9]: https://cocalc.com/github/microsoft/vscode/blob/main/extensions/configuration-editing/schemas/devContainer.vscode.schema.json

[^5_10]: https://developer.harness.io/docs/cloud-development-environments/develop-using-cde/run-args/

[^5_11]: https://code.visualstudio.com/blogs/2022/09/15/dev-container-features

[^5_12]: https://devcontainers.github.io/implementors/json_reference/

[^5_13]: https://navigation.ros.org/development_guides/devcontainer_docs/devcontainer_guide.html

[^5_14]: https://github.com/devcontainers/cli/issues/857

[^5_15]: https://github.com/devcontainers/features


---

# añade el parser

El parser ya está **integrado** en el archivo `devcontainer_complete_example.go` que generé en la respuesta anterior . Aquí te muestro las secciones específicas del parser:

## Secciones del parser incluidas

### 1. Estructura de datos (`DevContainerConfig`)

```go
type DevContainerConfig struct {
    Image           string            `json:"image,omitempty"`
    Build           *DevContainerBuild `json:"build,omitempty"`
    ContainerEnv    map[string]string `json:"containerEnv,omitempty"`
    ContainerUser   string            `json:"containerUser,omitempty"`
    RemoteUser      string            `json:"remoteUser,omitempty"`
    WorkspaceFolder string            `json:"workspaceFolder,omitempty"`
    WorkspaceMount  string            `json:"workspaceMount,omitempty"`
    ForwardPorts    interface{}       `json:"forwardPorts,omitempty"`
    PostCreateCommand interface{}      `json:"postCreateCommand,omitempty"`
    PostStartCommand  interface{}      `json:"postStartCommand,omitempty"`
    Mounts          []string          `json:"mounts,omitempty"`
    RunArgs         []string          `json:"runArgs,omitempty"`
    Features        map[string]interface{} `json:"features,omitempty"`
    ShutdownAction  string            `json:"shutdownAction,omitempty"`
}
```


### 2. Parseo de JSON con comentarios (`ParseDevContainerJSON`)

```go
func ParseDevContainerJSON(path string) (*DevContainerConfig, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read file: %w", err)
    }

    // Strip JSONC comments (// line and /* */ block)
    clean := stripJSONC(string(data))

    var cfg DevContainerConfig
    if err := json.Unmarshal([]byte(clean), &cfg); err != nil {
        return nil, fmt.Errorf("unmarshal: %w", err)
    }

    // Defaults
    if cfg.WorkspaceFolder == "" {
        cfg.WorkspaceFolder = "/workspace"
    }
    return &cfg, nil
}
```


### 3. Traducción de `runArgs` a libcontainer (`TranslateRunArgs`)

```go
func TranslateRunArgs(runArgs []string) *ParsedDevContainer {
    parsed := &ParsedDevContainer{...}
    for i := 0; i < len(runArgs); i++ {
        switch arg {
        case "--cap-add":
            parsed.CapAdd = append(parsed.CapAdd, "CAP_"+strings.ToUpper(runArgs[i+1]))
        case "--cap-drop":
            parsed.CapDrop = append(parsed.CapDrop, "CAP_"+strings.ToUpper(runArgs[i+1]))
        case "--security-opt":
            parsed.SecurityOpts = append(parsed.SecurityOpts, runArgs[i+1])
        case "--read-only":
            parsed.RootfsReadonly = true
        case "--memory", "-m":
            parsed.CgroupLimits.Memory = parseMemory(runArgs[i+1])
        case "--cpus":
            parsed.CgroupLimits.CpuQuota = int64(parseFloat(runArgs[i+1]) * 100000)
        case "--network":
            parsed.NetworkMode = runArgs[i+1]
        case "--mount":
            parsed.ExtraMounts = append(parsed.ExtraMounts, parseDockerMount(runArgs[i+1]))
        case "-v", "--volume":
            parsed.ExtraMounts = append(parsed.ExtraMounts, parseDockerVolume(runArgs[i+1]))
        }
    }
    return parsed
}
```


### 4. Aplicación completa del config (`ApplyDevContainer`)

```go
func ApplyDevContainer(cfg *DevContainerConfig, workspaceRoot string) (*ParsedDevContainer, error) {
    parsed := TranslateRunArgs(cfg.RunArgs)
    parsed.Config = cfg
    parsed.ResolvedImage = cfg.Image
    parsed.EnvVars = cfg.ContainerEnv
    parsed.User = cfg.ContainerUser
    if parsed.User == "" {
        parsed.User = cfg.RemoteUser
    }
    // Workspace mount (default o custom)
    // Mounts adicionales
    // Post create command como []string
    return parsed, nil
}
```


### 5. Generador de config libcontainer (`buildLibcontainerConfig`)

```go
func buildLibcontainerConfig(parsed *ParsedDevContainer, rootfs, containerID string) *configs.Config {
    cfg := &configs.Config{
        Rootfs: rootfs,
        Namespaces: configs.Namespaces{{Type: configs.NEWNS}, ...},
        Cgroups: &configs.Cgroup{
            Resources: &configs.Resources{
                Memory: 256*1024*1024, MemorySwap: 256*1024*1024,
                CpuShares: 512, PidsLimit: 64,
            },
        },
    }
    // Aplica capabilities de runArgs
    // Aplica mounts del devcontainer.json
    // Aplica security-opts (no-new-privileges, seccomp, apparmor)
    // Aplica cgroups overrides
    return cfg
}
```


## Flujo completo en `main()`

```go
func main() {
    // 1. Parsear
    devConfig, _ := ParseDevContainerJSON(".devcontainer/devcontainer.json")
    
    // 2. Interpretar/Resolver
    parsed, _ := ApplyDevContainer(devConfig, "/home/josefran/projects/my-project")
    
    // 3. Inicializar runtime
    rt, _ := NewDevContainerRuntime(ctx, "/var/lib/myagent")
    
    // 4. Pull imagen
    rt.PullImage(parsed.ResolvedImage, nil)
    
    // 5. Ejecutar subagente con config del devcontainer.json
    process, _ := rt.RunDevContainer(parsed, "subagent-001", cmd, nil, os.Stdout, os.Stderr)
    
    // 6. Esperar y cleanup
    rt.WaitSubAgent(process)
    rt.CleanupContainer("subagent-001")
}
```


## `runArgs` soportados

| Flag | Traducción |
| :-- | :-- |
| `--cap-add CHOWN` | `CAP_CHOWN` en bounding/effective/permitted |
| `--cap-drop ALL` | Vacía capabilities, solo las de `--cap-add` |
| `--security-opt no-new-privileges:true` | `NoNewPrivileges = true` |
| `--read-only` | `Readonlyfs = true` |
| `--memory 256m` | `Cgroups.Resources.Memory = 256MB` |
| `--cpus 0.5` | `Cgroups.Resources.CpuQuota = 50000` |
| `--network none` | Namespace de red aislado |
| `--mount type=bind,...` | Mount adicional en config |
| `-v /host:/container:ro` | Mount bind con readonly |
| `--privileged` | `Privileged = true` |

## `mounts[]` soportados

```json
"mounts": [
    "source=/tmp/agent-cache,target=/cache,type=bind,readonly",
    "source=my-volume,target=/data,type=volume"
]
```

Se parsean como `configs.Mount` con flags `MS_BIND`, `MS_RDONLY`, etc.

El archivo está listo para compilar. Solo necesitas:

```bash
cp output/devcontainer_complete_example.go main.go
cp output/devcontainer.json.example .devcontainer/devcontainer.json
go mod init ... && go mod tidy
go build -o agent .
sudo ./agent
```

¿Hay alguna propiedad específica del `devcontainer.json` que necesites que el parser maneje de forma diferente?

