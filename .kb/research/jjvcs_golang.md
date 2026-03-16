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

