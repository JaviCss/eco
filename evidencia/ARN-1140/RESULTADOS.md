# ARN-1140 — Eco empaquetado: una línea por criterio

Fecha: 2026-10-07. Ejecutor: E2. Repo del código: `../eco`, rama `ARN-1140`
desde `main` `aa0141f`. **Nada se publicó: no hay remoto y el tag `v0.1.0`
es local.** Toda la evidencia de este directorio está en UTF-8 sin BOM y
cada número sale de una corrida pegada acá al lado.

| # | Criterio | Veredicto | Evidencia |
|---|---|---|---|
| 1 | `go build`, `go vet`, `go test -race` en verde; cruzado para linux y darwin | **PASS** | `c1-build-vet-race.txt` (7 paquetes, los 7 `ok`), `c1-cruzado-linux-darwin.txt` (16 exit=0: vet + `go test -c` de 7 paquetes en cada plataforma) |
| 2 | Version en el binario, `doctor`, once verbos | **PASS con una salvedad declarada** | `c2-version.txt`, `c9-go-install-proxy.txt` |
| 3 | H8: helpers partidos, `PortFileACL` sin `stat` | **PASS** | `c3-h8-hr3.txt` |
| 4 | HR3: `restrictFileToOwner` solo en `_other`, httpdoor verde | **PASS** | `c3-h8-hr3.txt`, `c4-hr3-httpdoor-verde.txt` |
| 5 | HR1: cancel **adentro** del lote, rojo contra `aa0141f` | **PASS** | `c5-hr1-lote-transaccion-rojo.txt`, y el verde en `c1-build-vet-race.txt` |
| 6 | Release sin red: 3 binarios, `SHA256SUMS`, 4 `.tgz` | **PASS** | `c6-release.txt`, `c6-release-tamanos.txt`, `c6-release-control-negativo.txt` |
| 7 | npm positiva: `added 2`, `--version`, sin `invalid`, exit 5 | **PASS** | `c7-npm-positiva.txt` |
| 8 | npm negativa: solo el principal, linux en windows, `--ignore-scripts` | **PASS** | `c8-npm-negativos.txt`, `c8-npm-negativo2-linux-en-windows.txt` |
| 9 | Go por proxy local: `list`/`.info`/`.mod`/`.zip`, `go install`, consumidor, tidy, verify | **PASS con dos salvedades declaradas** | `c9-proxy.zip.txt`, `c9-go-install-proxy.txt`, `c9-consumidor.txt`, `c9-version-inexistente.txt`, `c10-c11-nada-sale-y-reglas.txt` |
| 10 | El tag no sale | **PASS** | `c10-c11-nada-sale-y-reglas.txt` |
| 11 | Sin comentarios en `.go`, launcher sin terceros, tres `require` directos | **PASS** | `c10-c11-nada-sale-y-reglas.txt` |
| 12 | Este archivo, `COMMITS.md` y "Para publicar" | **PASS** | acá |

## Lo que se midió, en corto

**La versión viaja dentro del binario y sale de `debug.ReadBuildInfo`.**
`eco --version` y `eco version` imprimen `eco <version> (<revision>, dirty)`.
`eco doctor` agrega `version:` y `revision:` a su salida. `--help` lista
once verbos (el nuevo es `version`).

Las tres rutas de instalación, lado a lado:

```
tarball de npm  : eco v0.1.0+dirty (aa0141f52b0d, dirty)
dist/ (release) : eco v0.1.0+dirty (aa0141f52b0d, dirty)
go install/proxy: eco v0.1.0
```

**Las tres salidas fueron,virtualmente la última la del tag limpio; la
primera y la segunda llevan `+dirty` porque el árbol de `../eco` tiene
cambios sin commitear** (E2 tiene `git commit` denegado ahí; el arquitecto
commitea). Cuando el commit exista y el árbol esté limpio, las tres
imprimen `eco v0.1.0 (aa0141f52b0d)`. Es un hecho del árbol, no del
código: por eso `+dirty` está en `Main.Version` y no lo pone el programa.

## Tres cosas que esta card encontró y que no estaban escritas

1. **npm compara `os`/`cpu` contra `process.platform`/`process.arch`**, o
   sea `win32`/`x64`, `linux`/`x64`, `darwin`/`arm64`. Con los nombres de
   Go (`windows`/`amd64`) el `npm install` aborta con `EBADPLATFORM` y no
   instala nada. La primera corrida de la positiva falló exactamente así:
   la salida real, con el `npm error notsup` y el `wanted {"os":"windows",
   "cpu":"amd64"} (current: {"os":"win32","cpu":"x64"})`, está pegada en
   `c7-nombres-de-plataforma-rojo.txt`. El mismo archivo deja ver que un
   `npx` sin paquete local se va **a la red** a buscar el `eco` de otro
   proyecto del registry (`1.1.0-rc-3`) y ejecuta ese: por eso toda la
   evidencia de la instalación usa `--no-install` o el launcher por
   `node`.
2. **Un `optionalDependencies` con spec `file:` no sirve.** Con nombre y
   versión exacta, `npm ls --all` sale sin `invalid`; la positiva lo
   muestra. Es lo que C6 de ARN-1090 ya habia avisado y ahora queda
  _round-trip_ con la versión exacta, no por `file:`.
3. **Con npm 11, un tarball de plataforma incompatible aborta toda la
   instalación** (`EBADPLATFORM`, exit 1, árbol vacío), no la omite en
   silencio. El "omite por `os`/`cpu`" de la card es lo que hace la
   resolución de la opcional cuando la plataforma no coincide; instalar
   el `.tgz` equivocado a mano es un error duro. El control negativo 2
   documenta las dos mitades.

## H8, HR3, HR1

- **H8**: `store/helpers_windows_test.go` se partió en tres archivos:
  `helpers_test.go` (sin tag: `errorsIs`, `dirEntriesOf`, `resolve`),
  `junction_windows_test.go` (`mklink /J`) y `junction_other_test.go`
  (`os.Symlink`). `httpdoor/acl_other.go` `PortFileACL` ya no ejecuta
  `stat`: usa `os.Stat` y los bits de modo. Los pares de Windows y de
  otros SO ya estaban bien; lo que estaba roto era esto.
- **HR3**: `restrictFileToOwner` borrado de `acl_windows.go`. Queda solo
  en `acl_other.go`, que sí lo usa (`chmod 0600`). La suite de `httpdoor`
  verde.
- **HR1**: test nuevo `TestPromoteCancelledInsideTheBatchRollsBackEveryRow`.
  Un driver/conn envuelto sobre `modernc.org/sqlite` cuenta `BEGIN` e
  `INSERT` y dispara `cancel()` en el **tercer `INSERT`**: el `cancel`
  ocurre con la transacción abierta y el lote a medias. El test prueba
  `errors.Is(err, context.Canceled)`, 0 filas en Z2 y que hubo `INSERT`
  y `BEGIN` antes de la cancelación. Contra `aa0141f` mutado a **una
  transacción por fila**, el test cae con **2 filas huérfanas en Z2**
  (pegado en `c5-hr1-lote-transaccion-rojo.txt`): ese es el defecto que
  HR1 describe. El test viejo (`cancelado esperando el candado`) se queda,
  renombrado a `...WhileWaitingForTheLock...`.

## Para publicar (lo que corre Javier, en orden)

Esto es lo que falta. E2 no publica nada: no hay remoto, no hay `LICENSE`,
no hay scope definitivo.

1. **Crear el remoto y subir el tag** (el tag local no sale solo):
   ```
   cd ../eco
   git remote add origin <url-del-repo>
   git push origin ARN-1140        # la rama de la card
   git push origin v0.1.0          # el tag, anotado
   ```
2. **Elegir el scope** (hoy el código usa `@javicss` como constante y el
   `name` de cada `package.json`; el `launcher` lo lee de una sola
   constante, `SCOPE`). Si el scope final es otro, se cambia en
   `tools/release/packages.go` y se vuelve a correr el release.
3. **Publicar en npm, en este orden**: las tres de plataforma primero,
   el principal al final (el principal depende de las tres por
   `optionalDependencies`):
   ```
   cd dist/npm/eco-win32-x64  && npm publish
   cd ../eco-linux-x64       && npm publish
   cd ../eco-darwin-arm64    && npm publish
   cd ../eco                 && npm publish
   ```
   (el `npm publish` usa el `dist/` que dejó el release; si el árbol
   estaba sucio, re-correr `go run ./tools/release --out dist/` con el
   árbol limpio para que los tres binarios lleven `eco v0.1.0` sin
   `dirty`).
4. **La licencia.** No hay `LICENSE` en `../eco`; el campo `license` de
   los paquetes se **omite** hasta que se decida. Cuando exista, el
   release lo lee del `LICENSE` y lo pone solo.
5. **Quitar el `replace` de `base/go.mod`** en `arn-v2` y borrar el
   junction `arn-v2-worktrees/eco`, que existe solo por ese `replace`.
   Esto lo hace el arquitecto/Javier, no esta card.

Lo que **no** se publica y no hay que hacer: `npm publish` de los `.tgz`
de prueba, nada de `git push` de otra cosa, y `npm provenance`/firmas que
no entran en esta card.

## Lo que queda declarado y no resuelto

- **La `vcs.revision` no puede coincidir entre las dos rutas.** El
  binario de `go install` por proxy **no trae** `vcs.revision` (el zip del
  módulo no lleva `.git`), mientras el de `dist/` sí. Lo que **sí**
  coincide es `Main.Version = v0.1.0`. Declarado, no resuelto: la card
  (criterio 2) pedía "la misma `vcs.revision`" y el instrumento no la da
  en la ruta del proxy.
- **Los tests no se corren en Linux ni en macOS**, no hay máquina: se
  compilan cruzado (`go test -c`, 16 exit=0) y se declara.
- **`+dirty` en los binarios de `dist/`** porque `../eco` tiene cambios
  sin commitear (E2 no commitea). Re-corriendo el release con el árbol
  limpio desaparece.
- **`go mod tidy` removió una línea huérfana del `go.sum`** (una de
  `cloud.google.com/go/compute/metadata`). Se restauró el `go.sum` con
  `git checkout` (la card no pide tocarlo); queda anotado para el
  arquitecto.
- **`GOFLAGS=-mod=mod`**: el `go` de la máquina lo tiene en el entorno;
  se usó en todas las corridas para que los `tools/` compilen.