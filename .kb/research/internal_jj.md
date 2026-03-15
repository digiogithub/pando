<img src="https://r2cdn.perplexity.ai/pplx-full-logo-primary-dark%402x.png" style="height:64px;margin-right:32px"/>

# uso últimamente con jj vcs . Tiene una funcionalidad de establecer puntos de cambio antes de iniciar cambios y trackea todo mejor que git que debes de añadir ficheros que no existían. Cómo podría añadir una funcionalidad similar a jj vcs en una aplicación golang sin dependencias externas?

Sí: la idea clave de **jj** no es solo “trackear mejor”, sino modelar el trabajo como una *working copy snapshot* asociada a un cambio activo, con auto-snapshot de archivos nuevos y borrados, en vez de depender de un staging explícito como Git.  En Go, sin dependencias externas, puedes replicar bastante bien esa experiencia si diseñas tu app alrededor de “cambios abiertos” + snapshot automático del árbol de trabajo.[^1_1][^1_2]

## Qué copiar de jj

En jj, la copia de trabajo pertenece a un “working-copy commit”, y la herramienta suele crear un nuevo snapshot automáticamente cuando detecta cambios; además, los archivos nuevos se rastrean implícitamente por defecto y los borrados pasan a reflejarse sin `add` manual.  También existe la idea de “abrir un nuevo cambio” para separar el trabajo siguiente, en lugar de ir acumulando todo en el mismo estado.[^1_2][^1_1]

Eso se puede traducir a tu app como:

- Un cambio activo actual.
- Un comando tipo `new` o `begin` que crea un punto base antes de editar.[^1_1]
- Un escaneo del workspace que detecta altas, modificaciones y bajas automáticamente.[^1_1]
- Reglas de ignore para no meter temporales o builds por accidente.[^1_1]


## Diseño mínimo

La arquitectura más simple es guardar un metadirectorio, por ejemplo `.myvcs/`, con:

- `HEAD`: id del cambio activo.
- `changes/<id>.json`: metadata del cambio, padres, timestamps, mensaje.
- `objects/`: blobs por hash de contenido.
- `snapshots/<id>.json`: mapa `path -> blobHash | tombstone`.
- `index-ignore`: reglas tipo `.gitignore` simplificadas.

Cada “change” puede ser mutable, y cada snapshot nuevo reemplaza el snapshot previo del cambio activo, igual que jj reemplaza la revisión de working copy por otra más reciente cuando detecta cambios.  Si quieres acercarte todavía más a jj, separa `change ID` estable de `snapshot ID` inmutable.[^1_3][^1_1]

## Flujo recomendado

El flujo sería:

1. `begin "mensaje"` crea un cambio vacío con padre = snapshot actual.[^1_1]
2. El usuario edita archivos libremente.
3. Cualquier comando relevante (`status`, `diff`, `save`, `log`) primero ejecuta `snapshotWorkingCopy()`.[^1_1]
4. Ese snapshot compara disco vs snapshot anterior del cambio activo y actualiza automáticamente altas, bajas y modificaciones.[^1_1]

Ejemplo de comportamiento:

- Creas `foo.go` y no llamas a `add`.
- Ejecutas `status`.
- Tu app hace escaneo, calcula hash del archivo y lo mete en el snapshot activo automáticamente, como hace jj con archivos nuevos.[^1_2][^1_1]


## Implementación en Go

Puedes hacerlo solo con librería estándar: `os`, `io`, `path/filepath`, `crypto/sha256`, `encoding/json`, `bufio`, `strings`, `time`. La pieza central es construir un manifiesto del árbol de trabajo y compararlo contra el snapshot anterior.

Estructuras simples:

```go
type Change struct {
	ID         string            `json:"id"`
	ParentID   string            `json:"parent_id"`
	Message    string            `json:"message"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	SnapshotID string            `json:"snapshot_id"`
}

type Snapshot struct {
	ID        string            `json:"id"`
	ChangeID  string            `json:"change_id"`
	Files     map[string]string `json:"files"` // path -> blob hash, "" => deleted
	CreatedAt time.Time         `json:"created_at"`
}
```

Pseudocódigo del snapshot:

```go
func snapshotWorkingCopy(root string, current Snapshot, ignore func(string) bool) (Snapshot, error) {
	next := Snapshot{
		ID:        newID(),
		ChangeID:  current.ChangeID,
		Files:     map[string]string{},
		CreatedAt: time.Now(),
	}

	seen := map[string]bool{}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == ".myvcs" || strings.HasPrefix(rel, ".myvcs"+string(os.PathSeparator)) {
			return filepath.SkipDir
		}
		if ignore(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		hash, err := hashFile(path)
		if err != nil {
			return err
		}
		next.Files[filepath.ToSlash(rel)] = hash
		seen[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}

	for p := range current.Files {
		if !seen[p] {
			next.Files[p] = "" // tombstone
		}
	}

	return next, nil
}
```

El hash del blob puede ser SHA-256, y si el blob no existe aún en `objects/`, lo escribes una sola vez. Eso te da deduplicación básica sin ninguna dependencia externa.

## Decisiones importantes

Si quieres que se sienta realmente como jj, te recomiendo estas decisiones:

- Haz auto-snapshot antes de casi cualquier comando, no solo en `commit`, porque jj actualiza el estado de working copy al ejecutar muchos comandos.[^1_1]
- Considera nuevos archivos como tracked por defecto, salvo ignore match.[^1_2][^1_1]
- Modela borrados como tombstones en el snapshot, no como ausencia silenciosa, para poder calcular diffs bien.
- Añade `untrack <path>` para excluir un archivo manteniéndolo en disco, parecido al comportamiento documentado por jj.[^1_1]

Y si quieres algo pragmático para una app de negocio, no hace falta implementar un VCS completo:

- Mantén un único cambio activo por workspace.
- Guarda snapshots enteros por simplicidad.
- Genera diffs al vuelo comparando dos mapas `path -> hash`.
- Deja merge/rebase para una fase posterior.

Si quieres, te puedo preparar a continuación un prototipo funcional en Go, sin dependencias externas, con comandos `init`, `begin`, `status`, `save` y auto-tracking de archivos nuevos.
<span style="display:none">[^1_10][^1_11][^1_12][^1_13][^1_14][^1_15][^1_4][^1_5][^1_6][^1_7][^1_8][^1_9]</span>

<div align="center">⁂</div>

[^1_1]: https://gist.github.com/christianromney/27fd1fca9e5f24ef24d9ed6c9eddda50

[^1_2]: https://swiftwithmajid.com/2025/10/22/introducing-jujutsu-vcs-edit-workflow/

[^1_3]: https://kubamartin.com/posts/introduction-to-the-jujutsu-vcs/

[^1_4]: https://www.reddit.com/r/programming/comments/1hentx8/tech_notes_the_jujutsu_version_control_system/

[^1_5]: https://danverbraganza.com/writings/most-frequent-jj-commands

[^1_6]: https://jj-vcs.github.io/jj/v0.17.0/working-copy/

[^1_7]: https://stackoverflow.com/questions/17078727/what-is-the-most-effective-way-to-lock-down-external-dependency-versions-in-go

[^1_8]: https://github.com/jj-vcs/jj

[^1_9]: https://docs.jj-vcs.dev/latest/working-copy/

[^1_10]: https://news.ycombinator.com/item?id=43021515

[^1_11]: https://swiftwithmajid.com/2025/10/15/introducing-jujutsu-vcs/

[^1_12]: https://github.com/martinvonz/jj/blob/main/docs/working-copy.md

[^1_13]: https://www.x-cmd.com/install/jj/

[^1_14]: https://docs.jj-vcs.dev/latest/config/

[^1_15]: https://www.jj-vcs.dev/v0.15.1/working-copy/


---

# cómo podría tener mi propia implementación de jj para que cada sesión de AI trabaje una rama o versión diferente de ficheros que luego puedo incorporar por defecto, o descartar los cambio, dame el comienzo de código

Sí: para tu caso no necesitas clonar todo jj, sino construir un motor de **workspaces efímeros** donde cada sesión de AI tenga su propio cambio mutable sobre una base común, y luego puedas hacer `apply` o `discard` sobre esa sesión. En jj, la working copy es un snapshot mutable que se autoactualiza y los cambios se organizan como revisiones editables; esa idea encaja muy bien con “una sesión AI = una rama/cambio aislado”.[^2_1][^2_2][^2_3]

## Modelo mental

La forma más práctica es pensar en tres capas:

- `base snapshot`: el estado aprobado actual.
- `session snapshot`: los cambios de una sesión AI concreta.
- `overlay materializado`: los ficheros que ve esa sesión en disco o en memoria.[^2_3][^2_1]

En vez de ramas Git, cada sesión puede apuntar a `BaseID + cambios propios`, y cuando termina haces una de dos cosas: incorporar el snapshot de sesión a la base, o abandonarlo. En jj, editar cambios mutables y reubicar trabajo después es parte del flujo normal; aquí usarías el mismo patrón, pero orientado a sesiones de agentes.[^2_4][^2_3]

## Diseño simple

Haz un directorio como `.aijj/` con esta estructura:

- `objects/`: blobs por hash.
- `snapshots/`: mapas `path -> blobHash`.
- `sessions/`: metadata de cada sesión.
- `HEAD`: snapshot base actual.[^2_1]

Tipos mínimos en Go:

```go
package aijj

import "time"

type SnapshotID string
type SessionID string
type BlobID string

type Snapshot struct {
	ID        SnapshotID        `json:"id"`
	Parent    SnapshotID        `json:"parent"`
	Files     map[string]BlobID `json:"files"` // path -> blob
	CreatedAt time.Time         `json:"created_at"`
	Message   string            `json:"message"`
}

type Session struct {
	ID             SessionID  `json:"id"`
	Name           string     `json:"name"`
	BaseSnapshotID SnapshotID `json:"base_snapshot_id"`
	HeadSnapshotID SnapshotID `json:"head_snapshot_id"`
	Status         string     `json:"status"` // open, applied, discarded
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
```

La decisión importante es que una sesión no “posee” ficheros completos, sino un `HeadSnapshotID` que representa su versión actual. Así puedes crear muchas sesiones desde la misma base sin duplicar demasiado estado.[^2_5][^2_6]

## Flujo de sesiones

El flujo mínimo sería este:

1. `Init(repoRoot)` crea `.aijj/` y un snapshot base vacío o inicial.
2. `NewSession(name)` crea una sesión nueva apuntando al `HEAD` actual.
3. La AI escribe archivos en un workspace de sesión o mediante API.
4. `SaveSession(sessionID)` reescanea archivos y genera un nuevo snapshot.
5. `ApplySession(sessionID)` convierte ese snapshot en la nueva base.
6. `DiscardSession(sessionID)` marca la sesión como descartada.[^2_2][^2_1]

Si varias sesiones parten de la misma base, puedes empezar con una política simple: solo permitir `apply` si la base no ha cambiado desde que nació la sesión. Si cambió, devuelves conflicto y más adelante implementas merge de tres vías. Esa restricción te simplifica muchísimo el comienzo.[^2_7]

## Comienzo de código

Abajo tienes una base funcional de arranque, sin dependencias externas, centrada en crear sesiones, guardar snapshots y aplicar o descartar. Está pensada para evolucionar luego hacia diff, merge y workspaces materializados.

```go
package aijj

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SnapshotID string
type SessionID string
type BlobID string

type Snapshot struct {
	ID        SnapshotID        `json:"id"`
	Parent    SnapshotID        `json:"parent"`
	Files     map[string]BlobID `json:"files"`
	CreatedAt time.Time         `json:"created_at"`
	Message   string            `json:"message"`
}

type Session struct {
	ID             SessionID  `json:"id"`
	Name           string     `json:"name"`
	BaseSnapshotID SnapshotID `json:"base_snapshot_id"`
	HeadSnapshotID SnapshotID `json:"head_snapshot_id"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Repo struct {
	Root string
	Meta string
}

func Init(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	dirs := []string{
		meta,
		filepath.Join(meta, "objects"),
		filepath.Join(meta, "snapshots"),
		filepath.Join(meta, "sessions"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	r := &Repo{Root: root, Meta: meta}

	if _, err := os.Stat(filepath.Join(meta, "HEAD")); errors.Is(err, os.ErrNotExist) {
		snap := Snapshot{
			ID:        SnapshotID(newID("snap")),
			Parent:    "",
			Files:     map[string]BlobID{},
			CreatedAt: time.Now().UTC(),
			Message:   "initial",
		}
		if err := r.saveSnapshot(snap); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(meta, "HEAD"), []byte(snap.ID), 0o644); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func Open(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	if _, err := os.Stat(meta); err != nil {
		return nil, err
	}
	return &Repo{Root: root, Meta: meta}, nil
}

func (r *Repo) NewSession(name string) (Session, error) {
	head, err := r.Head()
	if err != nil {
		return Session{}, err
	}

	now := time.Now().UTC()
	s := Session{
		ID:             SessionID(newID("sess")),
		Name:           name,
		BaseSnapshotID: head.ID,
		HeadSnapshotID: head.ID,
		Status:         "open",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.saveSession(s); err != nil {
		return Session{}, err
	}
	return s, nil
}

func (r *Repo) Head() (Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(r.Meta, "HEAD"))
	if err != nil {
		return Snapshot{}, err
	}
	return r.loadSnapshot(SnapshotID(strings.TrimSpace(string(b))))
}

func (r *Repo) LoadSession(id SessionID) (Session, error) {
	var s Session
	err := readJSON(filepath.Join(r.Meta, "sessions", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) SaveSessionFromDir(id SessionID, dir string, message string) (Snapshot, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	if sess.Status != "open" {
		return Snapshot{}, errors.New("session is not open")
	}

	parent, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return Snapshot{}, err
	}

	files, err := r.captureDir(dir)
	if err != nil {
		return Snapshot{}, err
	}

	if sameFiles(parent.Files, files) {
		return parent, nil
	}

	next := Snapshot{
		ID:        SnapshotID(newID("snap")),
		Parent:    parent.ID,
		Files:     files,
		CreatedAt: time.Now().UTC(),
		Message:   message,
	}
	if err := r.saveSnapshot(next); err != nil {
		return Snapshot{}, err
	}

	sess.HeadSnapshotID = next.ID
	sess.UpdatedAt = time.Now().UTC()
	if err := r.saveSession(sess); err != nil {
		return Snapshot{}, err
	}

	return next, nil
}

func (r *Repo) ApplySession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}

	currentHead, err := r.Head()
	if err != nil {
		return err
	}
	if currentHead.ID != sess.BaseSnapshotID {
		return errors.New("base changed; merge required")
	}

	if err := os.WriteFile(filepath.Join(r.Meta, "HEAD"), []byte(sess.HeadSnapshotID), 0o644); err != nil {
		return err
	}

	sess.Status = "applied"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) DiscardSession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}
	sess.Status = "discarded"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) MaterializeSnapshot(id SnapshotID, dst string) error {
	snap, err := r.loadSnapshot(id)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for path, blob := range snap.Files {
		target := filepath.Join(dst, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(r.Meta, "objects", string(blob)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) captureDir(dir string) (map[string]BlobID, error) {
	files := map[string]BlobID{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}
		if rel == ".aijj" || strings.HasPrefix(rel, ".aijj/") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}

		blob, err := r.storeBlob(path)
		if err != nil {
			return err
		}
		files[rel] = blob
		return nil
	})

	return files, err
}

func (r *Repo) storeBlob(path string) (BlobID, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	blob := BlobID(sum)

	objPath := filepath.Join(r.Meta, "objects", sum)
	if _, err := os.Stat(objPath); errors.Is(err, os.ErrNotExist) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(objPath, data, 0o644); err != nil {
			return "", err
		}
	}
	return blob, nil
}

func (r *Repo) saveSnapshot(s Snapshot) error {
	return writeJSON(filepath.Join(r.Meta, "snapshots", string(s.ID)+".json"), s)
}

func (r *Repo) loadSnapshot(id SnapshotID) (Snapshot, error) {
	var s Snapshot
	err := readJSON(filepath.Join(r.Meta, "snapshots", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) saveSession(s Session) error {
	return writeJSON(filepath.Join(r.Meta, "sessions", string(s.ID)+".json"), s)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func sameFiles(a, b map[string]BlobID) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}

func newID(prefix string) string {
	now := time.Now().UTC().Format("20060102T150405.000000000")
	return prefix + "_" + now
}

func (r *Repo) DiffSession(id SessionID) ([]string, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return nil, err
	}
	base, err := r.loadSnapshot(sess.BaseSnapshotID)
	if err != nil {
		return nil, err
	}
	head, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return nil, err
	}

	pathsMap := map[string]struct{}{}
	for p := range base.Files {
		pathsMap[p] = struct{}{}
	}
	for p := range head.Files {
		pathsMap[p] = struct{}{}
	}

	paths := make([]string, 0, len(pathsMap))
	for p := range pathsMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []string
	for _, p := range paths {
		bv, bok := base.Files[p]
		hv, hok := head.Files[p]
		switch {
		case !bok && hok:
			out = append(out, "A "+p)
		case bok && !hok:
			out = append(out, "D "+p)
		case bok && hok && bv != hv:
			out = append(out, "M "+p)
		}
	}
	return out, nil
}
```


## Cómo usarlo

La forma más limpia es que cada sesión AI trabaje en un directorio materializado propio, por ejemplo:

- `sessions/agent-1/`
- `sessions/agent-2/`
- `sessions/reviewer/`

Creas la sesión, materializas su snapshot, dejas que el agente escriba ahí, y luego llamas a `SaveSessionFromDir()`. Si el resultado te gusta, haces `ApplySession()`; si no, `DiscardSession()`. Esto imita bastante bien la idea de cambios editables y snapshots automáticos de jj, pero simplificado a tu caso.[^2_2][^2_3][^2_1]

Lo siguiente que yo implementaría sería:

- `ignore patterns`
- `three-way merge`
- `diff de contenido`
- `checkout de sesión`
- `locks` para concurrencia entre agentes

Si quieres, en el siguiente mensaje te preparo un **CLI completo en Go** (`init`, `session new`, `session save`, `session diff`, `session apply`, `session discard`) sobre esta base.
<span style="display:none">[^2_10][^2_11][^2_12][^2_13][^2_14][^2_15][^2_8][^2_9]</span>

<div align="center">⁂</div>

[^2_1]: https://docs.jj-vcs.dev/latest/working-copy/

[^2_2]: https://man.archlinux.org/man/extra/jujutsu/jj-file-show.1.en

[^2_3]: https://kubamartin.com/posts/introduction-to-the-jujutsu-vcs/

[^2_4]: https://swiftwithmajid.com/2025/10/22/introducing-jujutsu-vcs-edit-workflow/

[^2_5]: https://neugierig.org/software/blog/2024/12/jujutsu.html

[^2_6]: https://www.stavros.io/posts/switch-to-jujutsu-already-a-tutorial/

[^2_7]: https://docs.jj-vcs.dev/v0.16.0/branches/

[^2_8]: https://github.com/jj-vcs/jj/issues/2876

[^2_9]: https://github.com/jj-vcs/jj/discussions/2425

[^2_10]: https://danverbraganza.com/writings/most-frequent-jj-commands

[^2_11]: https://docs.jj-vcs.dev/latest/cli-reference/

[^2_12]: https://news.ycombinator.com/item?id=43020180

[^2_13]: https://www.youtube.com/watch?v=ou4ZNRFXkO0

[^2_14]: https://www.reddit.com/r/git/comments/1nf74iz/another_jujutsu_post/

[^2_15]: https://gist.github.com/christianromney/27fd1fca9e5f24ef24d9ed6c9eddda50


---

# adelante, prepara un cli completo en go

Sí: abajo te dejo un CLI completo en Go, sin dependencias externas, con subcomandos usando `flag.FlagSet`, que es el enfoque estándar para CLI con subcommands en la librería estándar.  La idea sigue el modelo de jj de trabajar sobre una working copy/snapshot mutable, pero adaptado a “sesiones AI” que puedes aplicar o descartar.[^3_1][^3_2][^3_3][^3_4]

## Estructura

Te propongo este layout mínimo:

- `go.mod`
- `main.go`
- `aijj/repo.go`
- `aijj/cli.go`

Con esto tendrás:

- `aijj init`
- `aijj session new`
- `aijj session list`
- `aijj session materialize`
- `aijj session save`
- `aijj session diff`
- `aijj session apply`
- `aijj session discard`
- `aijj head`[^3_5][^3_1]


## go.mod

```go
module github.com/tuusuario/aijj

go 1.22
```


## aijj/repo.go

```go
package aijj

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SnapshotID string
type SessionID string
type BlobID string

type Snapshot struct {
	ID        SnapshotID        `json:"id"`
	Parent    SnapshotID        `json:"parent"`
	Files     map[string]BlobID `json:"files"`
	CreatedAt time.Time         `json:"created_at"`
	Message   string            `json:"message"`
}

type Session struct {
	ID             SessionID  `json:"id"`
	Name           string     `json:"name"`
	BaseSnapshotID SnapshotID `json:"base_snapshot_id"`
	HeadSnapshotID SnapshotID `json:"head_snapshot_id"`
	Status         string     `json:"status"`
	Workspace      string     `json:"workspace"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type Repo struct {
	Root string
	Meta string
}

func Init(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	dirs := []string{
		meta,
		filepath.Join(meta, "objects"),
		filepath.Join(meta, "snapshots"),
		filepath.Join(meta, "sessions"),
		filepath.Join(meta, "workspaces"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}

	r := &Repo{Root: root, Meta: meta}

	if _, err := os.Stat(filepath.Join(meta, "HEAD")); errors.Is(err, os.ErrNotExist) {
		snap := Snapshot{
			ID:        SnapshotID(newID("snap")),
			Parent:    "",
			Files:     map[string]BlobID{},
			CreatedAt: time.Now().UTC(),
			Message:   "initial",
		}
		if err := r.saveSnapshot(snap); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(meta, "HEAD"), []byte(snap.ID), 0o644); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func Open(root string) (*Repo, error) {
	meta := filepath.Join(root, ".aijj")
	if _, err := os.Stat(meta); err != nil {
		return nil, err
	}
	return &Repo{Root: root, Meta: meta}, nil
}

func (r *Repo) Head() (Snapshot, error) {
	b, err := os.ReadFile(filepath.Join(r.Meta, "HEAD"))
	if err != nil {
		return Snapshot{}, err
	}
	return r.loadSnapshot(SnapshotID(strings.TrimSpace(string(b))))
}

func (r *Repo) NewSession(name string) (Session, error) {
	head, err := r.Head()
	if err != nil {
		return Session{}, err
	}

	now := time.Now().UTC()
	id := SessionID(newID("sess"))
	workspace := filepath.Join(r.Meta, "workspaces", string(id))

	s := Session{
		ID:             id,
		Name:           name,
		BaseSnapshotID: head.ID,
		HeadSnapshotID: head.ID,
		Status:         "open",
		Workspace:      workspace,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.saveSession(s); err != nil {
		return Session{}, err
	}
	return s, nil
}

func (r *Repo) ListSessions() ([]Session, error) {
	dir := filepath.Join(r.Meta, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var out []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var s Session
		if err := readJSON(filepath.Join(dir, e.Name()), &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *Repo) LoadSession(id SessionID) (Session, error) {
	var s Session
	err := readJSON(filepath.Join(r.Meta, "sessions", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) MaterializeSession(id SessionID) (string, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return "", err
	}
	if sess.Status != "open" {
		return "", errors.New("session is not open")
	}

	if err := os.RemoveAll(sess.Workspace); err != nil {
		return "", err
	}
	if err := os.MkdirAll(sess.Workspace, 0o755); err != nil {
		return "", err
	}

	if err := r.MaterializeSnapshot(sess.HeadSnapshotID, sess.Workspace); err != nil {
		return "", err
	}
	return sess.Workspace, nil
}

func (r *Repo) SaveSession(id SessionID, message string) (Snapshot, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	return r.SaveSessionFromDir(sess.ID, sess.Workspace, message)
}

func (r *Repo) SaveSessionFromDir(id SessionID, dir string, message string) (Snapshot, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return Snapshot{}, err
	}
	if sess.Status != "open" {
		return Snapshot{}, errors.New("session is not open")
	}

	parent, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return Snapshot{}, err
	}

	files, err := r.captureDir(dir)
	if err != nil {
		return Snapshot{}, err
	}

	if sameFiles(parent.Files, files) {
		return parent, nil
	}

	next := Snapshot{
		ID:        SnapshotID(newID("snap")),
		Parent:    parent.ID,
		Files:     files,
		CreatedAt: time.Now().UTC(),
		Message:   message,
	}
	if err := r.saveSnapshot(next); err != nil {
		return Snapshot{}, err
	}

	sess.HeadSnapshotID = next.ID
	sess.UpdatedAt = time.Now().UTC()
	if err := r.saveSession(sess); err != nil {
		return Snapshot{}, err
	}
	return next, nil
}

func (r *Repo) ApplySession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}

	currentHead, err := r.Head()
	if err != nil {
		return err
	}
	if currentHead.ID != sess.BaseSnapshotID {
		return errors.New("base changed; merge required")
	}

	if err := os.WriteFile(filepath.Join(r.Meta, "HEAD"), []byte(sess.HeadSnapshotID), 0o644); err != nil {
		return err
	}

	sess.Status = "applied"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) DiscardSession(id SessionID) error {
	sess, err := r.LoadSession(id)
	if err != nil {
		return err
	}
	if sess.Status != "open" {
		return errors.New("session is not open")
	}
	sess.Status = "discarded"
	sess.UpdatedAt = time.Now().UTC()
	return r.saveSession(sess)
}

func (r *Repo) DiffSession(id SessionID) ([]string, error) {
	sess, err := r.LoadSession(id)
	if err != nil {
		return nil, err
	}

	base, err := r.loadSnapshot(sess.BaseSnapshotID)
	if err != nil {
		return nil, err
	}
	head, err := r.loadSnapshot(sess.HeadSnapshotID)
	if err != nil {
		return nil, err
	}

	pathsMap := map[string]struct{}{}
	for p := range base.Files {
		pathsMap[p] = struct{}{}
	}
	for p := range head.Files {
		pathsMap[p] = struct{}{}
	}

	paths := make([]string, 0, len(pathsMap))
	for p := range pathsMap {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []string
	for _, p := range paths {
		bv, bok := base.Files[p]
		hv, hok := head.Files[p]
		switch {
		case !bok && hok:
			out = append(out, "A "+p)
		case bok && !hok:
			out = append(out, "D "+p)
		case bok && hok && bv != hv:
			out = append(out, "M "+p)
		}
	}
	return out, nil
}

func (r *Repo) MaterializeSnapshot(id SnapshotID, dst string) error {
	snap, err := r.loadSnapshot(id)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	for path, blob := range snap.Files {
		target := filepath.Join(dst, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(r.Meta, "objects", string(blob)))
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repo) captureDir(dir string) (map[string]BlobID, error) {
	files := map[string]BlobID{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		blob, err := r.storeBlob(path)
		if err != nil {
			return err
		}
		files[rel] = blob
		return nil
	})

	return files, err
}

func (r *Repo) storeBlob(path string) (BlobID, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	blob := BlobID(sum)

	objPath := filepath.Join(r.Meta, "objects", sum)
	if _, err := os.Stat(objPath); errors.Is(err, os.ErrNotExist) {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(objPath, data, 0o644); err != nil {
			return "", err
		}
	}
	return blob, nil
}

func (r *Repo) saveSnapshot(s Snapshot) error {
	return writeJSON(filepath.Join(r.Meta, "snapshots", string(s.ID)+".json"), s)
}

func (r *Repo) loadSnapshot(id SnapshotID) (Snapshot, error) {
	var s Snapshot
	err := readJSON(filepath.Join(r.Meta, "snapshots", string(id)+".json"), &s)
	return s, err
}

func (r *Repo) saveSession(s Session) error {
	return writeJSON(filepath.Join(r.Meta, "sessions", string(s.ID)+".json"), s)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func sameFiles(a, b map[string]BlobID) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || vb != va {
			return false
		}
	}
	return true
}

func newID(prefix string) string {
	return prefix + "_" + time.Now().UTC().Format("20060102T150405.000000000")
}
```


## aijj/cli.go

```go
package aijj

import (
	"flag"
	"fmt"
	"io"
)

type CLI struct {
	Stdout io.Writer
	Stderr io.Writer
}

func NewCLI(stdout, stderr io.Writer) *CLI {
	return &CLI{Stdout: stdout, Stderr: stderr}
}

func (c *CLI) Run(args []string) int {
	if len(args) == 0 {
		c.usage()
		return 1
	}

	switch args[^3_0] {
	case "init":
		return c.runInit(args[1:])
	case "head":
		return c.runHead(args[1:])
	case "session":
		return c.runSession(args[1:])
	case "help", "-h", "--help":
		c.usage()
		return 0
	default:
		fmt.Fprintf(c.Stderr, "unknown command: %s\n", args[^3_0])
		c.usage()
		return 1
	}
}

func (c *CLI) usage() {
	fmt.Fprintln(c.Stdout, "aijj - snapshots por sesión AI")
	fmt.Fprintln(c.Stdout, "")
	fmt.Fprintln(c.Stdout, "Uso:")
	fmt.Fprintln(c.Stdout, "  aijj init [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj head [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session new -name NAME [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session list [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session materialize -id SESSION_ID [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session save -id SESSION_ID [-m MESSAGE] [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session diff -id SESSION_ID [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session apply -id SESSION_ID [-repo PATH]")
	fmt.Fprintln(c.Stdout, "  aijj session discard -id SESSION_ID [-repo PATH]")
}

func (c *CLI) runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r, err := Init(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "init error: %v\n", err)
		return 1
	}
	fmt.Fprintf(c.Stdout, "repo initialized: %s\n", r.Meta)
	return 0
}

func (c *CLI) runHead(args []string) int {
	fs := flag.NewFlagSet("head", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	head, err := r.Head()
	if err != nil {
		fmt.Fprintf(c.Stderr, "head error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "HEAD: %s\n", head.ID)
	fmt.Fprintf(c.Stdout, "Parent: %s\n", head.Parent)
	fmt.Fprintf(c.Stdout, "Message: %s\n", head.Message)
	fmt.Fprintf(c.Stdout, "Files: %d\n", len(head.Files))
	return 0
}

func (c *CLI) runSession(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(c.Stderr, "missing session subcommand")
		return 1
	}

	switch args[^3_0] {
	case "new":
		return c.runSessionNew(args[1:])
	case "list":
		return c.runSessionList(args[1:])
	case "materialize":
		return c.runSessionMaterialize(args[1:])
	case "save":
		return c.runSessionSave(args[1:])
	case "diff":
		return c.runSessionDiff(args[1:])
	case "apply":
		return c.runSessionApply(args[1:])
	case "discard":
		return c.runSessionDiscard(args[1:])
	default:
		fmt.Fprintf(c.Stderr, "unknown session subcommand: %s\n", args[^3_0])
		return 1
	}
}

func (c *CLI) runSessionNew(args []string) int {
	fs := flag.NewFlagSet("session new", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	name := fs.String("name", "", "nombre de la sesión")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" {
		fmt.Fprintln(c.Stderr, "missing -name")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	s, err := r.NewSession(*name)
	if err != nil {
		fmt.Fprintf(c.Stderr, "new session error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Session: %s\n", s.ID)
	fmt.Fprintf(c.Stdout, "Name: %s\n", s.Name)
	fmt.Fprintf(c.Stdout, "Base: %s\n", s.BaseSnapshotID)
	fmt.Fprintf(c.Stdout, "Workspace: %s\n", s.Workspace)
	return 0
}

func (c *CLI) runSessionList(args []string) int {
	fs := flag.NewFlagSet("session list", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	sessions, err := r.ListSessions()
	if err != nil {
		fmt.Fprintf(c.Stderr, "list sessions error: %v\n", err)
		return 1
	}

	for _, s := range sessions {
		fmt.Fprintf(c.Stdout, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Status, s.Name, s.BaseSnapshotID, s.HeadSnapshotID)
	}
	return 0
}

func (c *CLI) runSessionMaterialize(args []string) int {
	fs := flag.NewFlagSet("session materialize", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	workspace, err := r.MaterializeSession(SessionID(*id))
	if err != nil {
		fmt.Fprintf(c.Stderr, "materialize error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Workspace ready: %s\n", workspace)
	return 0
}

func (c *CLI) runSessionSave(args []string) int {
	fs := flag.NewFlagSet("session save", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	id := fs.String("id", "", "session id")
	msg := fs.String("m", "session save", "mensaje del snapshot")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	snap, err := r.SaveSession(SessionID(*id), *msg)
	if err != nil {
		fmt.Fprintf(c.Stderr, "save error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Saved snapshot: %s\n", snap.ID)
	fmt.Fprintf(c.Stdout, "Parent: %s\n", snap.Parent)
	fmt.Fprintf(c.Stdout, "Files: %d\n", len(snap.Files))
	return 0
}

func (c *CLI) runSessionDiff(args []string) int {
	fs := flag.NewFlagSet("session diff", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	diff, err := r.DiffSession(SessionID(*id))
	if err != nil {
		fmt.Fprintf(c.Stderr, "diff error: %v\n", err)
		return 1
	}

	if len(diff) == 0 {
		fmt.Fprintln(c.Stdout, "No changes")
		return 0
	}

	for _, line := range diff {
		fmt.Fprintln(c.Stdout, line)
	}
	return 0
}

func (c *CLI) runSessionApply(args []string) int {
	fs := flag.NewFlagSet("session apply", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	if err := r.ApplySession(SessionID(*id)); err != nil {
		fmt.Fprintf(c.Stderr, "apply error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Session applied: %s\n", *id)
	return 0
}

func (c *CLI) runSessionDiscard(args []string) int {
	fs := flag.NewFlagSet("session discard", flag.ContinueOnError)
	fs.SetOutput(c.Stderr)

	repoPath := fs.String("repo", ".", "ruta del repositorio")
	id := fs.String("id", "", "session id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(c.Stderr, "missing -id")
		return 1
	}

	r, err := Open(*repoPath)
	if err != nil {
		fmt.Fprintf(c.Stderr, "open error: %v\n", err)
		return 1
	}

	if err := r.DiscardSession(SessionID(*id)); err != nil {
		fmt.Fprintf(c.Stderr, "discard error: %v\n", err)
		return 1
	}

	fmt.Fprintf(c.Stdout, "Session discarded: %s\n", *id)
	return 0
}
```


## main.go

```go
package main

import (
	"os"

	"github.com/tuusuario/aijj/aijj"
)

func main() {
	cli := aijj.NewCLI(os.Stdout, os.Stderr)
	os.Exit(cli.Run(os.Args[1:]))
}
```


## Uso real

Con esto ya tienes un flujo bastante útil para agentes AI que trabajan aislados sobre snapshots, algo conceptualmente cercano a la working copy editable de jj.[^3_3][^3_4]

Ejemplo:

```bash
go build -o aijj .

./aijj init
./aijj session new -name "agent-refactor"
./aijj session list
./aijj session materialize -id sess_20260315T225200.000000000

# la AI escribe en .aijj/workspaces/<session-id>/

./aijj session save -id sess_20260315T225200.000000000 -m "refactor auth middleware"
./aijj session diff -id sess_20260315T225200.000000000
./aijj session apply -id sess_20260315T225200.000000000
```


## Qué mejoraría ya

Para producción, yo añadiría estas piezas primero:

- `.aijjignore`, para no snapshotear binarios, caches o `.git/`.
- Lock de repositorio, para que dos agentes no hagan `apply` a la vez.
- Merge de tres vías, porque ahora `apply` falla si cambió la base entre medias.
- Diff de contenido por archivo, porque ahora solo informa `A/M/D`.
- Un comando `session exec` para lanzar directamente un agente dentro del workspace materializado.[^3_6][^3_1]

Si quieres, el siguiente paso te lo doy ya hecho: un `Makefile`, tests básicos y una mejora para que `session save` detecte automáticamente borrados, ignores y conflictos de aplicación.
<span style="display:none">[^3_10][^3_11][^3_12][^3_13][^3_14][^3_15][^3_7][^3_8][^3_9]</span>

<div align="center">⁂</div>

[^3_1]: https://gobyexample.com/command-line-subcommands

[^3_2]: https://www.digitalocean.com/community/tutorials/how-to-use-the-flag-package-in-go

[^3_3]: https://kristofferbalintona.me/posts/202503270335/

[^3_4]: https://docs.jj-vcs.dev/latest/working-copy/

[^3_5]: https://abhinavg.net/2022/08/13/flag-subcommand/

[^3_6]: https://appliedgo.net/spotlight/subcommands-without-command-package/

[^3_7]: https://www.reddit.com/r/golang/comments/1h18wi4/golang_flags_subcommands_and_sourcefiles_per/

[^3_8]: https://www.bytesizego.com/blog/golang-flag-package

[^3_9]: https://stackoverflow.com/questions/67904765/global-flags-and-subcommands

[^3_10]: https://github.com/arran4/go-subcommand

[^3_11]: https://osinet.fr/go/en/articles/cli-google-subcommands/

[^3_12]: https://docs.jj-vcs.dev/latest/github/

[^3_13]: https://stackoverflow.com/questions/67642066/providing-context-to-cobra-subcommands-without-using-package-globals

[^3_14]: https://github.com/cosiner/flag

[^3_15]: https://github.com/jj-vcs/jj/blob/main/docs/git-comparison.md

