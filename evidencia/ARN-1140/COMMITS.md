# ARN-1140 — mensajes de commit

E2 tiene `git commit` denegado en `../eco`: deja **todo staged** y los
mensos acá. El arquitecto commitea, en el orden de abajo, con
convencionales y sin atribución de IA.

Estado del árbol al momento de escribir esto (rama `ARN-1140`, base
`aa0141f`):

```
 M cmd/eco/cli_test.go
 M cmd/eco/main.go
 M httpdoor/acl_other.go
 M httpdoor/acl_windows.go
 D store/helpers_windows_test.go
 M store/promote_test.go
?? cmd/eco/version.go
?? cmd/eco/version_test.go
?? store/helpers_test.go
?? store/junction_other_test.go
?? store/junction_windows_test.go
?? store/promote_cancel_test.go
?? tools/release/...
?? tools/goproxy/...
?? evidencia/ARN-1140/...
```

## 1 — la versión vive en el binario

```
feat: la version del binario sale de debug.ReadBuildInfo

eco --version y eco version imprimen eco <version> (<revision>, dirty),
tomados de debug.ReadBuildInfo: Main.Version para go install m@v, y
vcs.revision/vcs.modified para builds desde el arbol. Sin build info
imprime eco (devel). eco doctor suma version y revision a su salida,
y --help lista el verbo version.

La version no viene de un -X que haya que recordar: asi el binario de
go install y el del tarball de npm dicen lo mismo.
```

Archivos: `cmd/eco/version.go` (nuevo), `cmd/eco/version_test.go`
(nuevo), `cmd/eco/main.go`, `cmd/eco/cli_test.go`.

## 2 — H8: store compila fuera de Windows

```
fix: store compila y sus tests compilan fuera de Windows

helpers_windows_test.go estaba sin build tag, asi que los helpers que
definia (makeJunction, errorsIs, dirEntriesOf, resolve) no existian en
linux ni en darwin, y los tests sin tag que los usan no compilaban:
GOOS=linux go vet ./... caia con undefined: makeJunction.

Ahora los helpers que son de todos los SO viven en helpers_test.go, y
makeJunction se parte en junction_windows_test.go (mklink /J) y
junction_other_test.go (os.Symlink, el equivalente mas cercano del
reparse point).

httpdoor: PortFileACL deja de ejecutar stat -c y usa os.Stat con los
bits de modo: en Linux no dependia de GNU stat.
```

Archivos: `store/helpers_test.go` (nuevo), `store/junction_windows_test.go`
(nuevo), `store/junction_other_test.go` (nuevo),
`store/helpers_windows_test.go` (borrado), `httpdoor/acl_other.go`.

## 3 — HR3: restrictFileToOwner era código muerto

```
refactor: borra restrictFileToOwner de acl_windows.go

En Windows no tenia llamadores: createPrivateTemp usa ownerOnlyAttributes.
El cuerpo leia la DACL y la volvia a aplicar con PROTECTED_DACL, que
corta la herencia pero no restringe a dueno: el nombre mentia. En el
_resto de SO si se usa, y hace chmod 0600, que si restringe; queda.
```

Archivos: `httpdoor/acl_windows.go`.

## 4 — HR1: el test de cancelación entra al lote

```
test: Promote se cancela adentro del lote, con la transaccion abierta

El test anterior retenia user.db con BEGIN EXCLUSIVE y NoBusyTimeout:
Promote nunca entraba al lote, la cancelacion caia en withRetry. Se
renombra a ...WhileWaitingForTheLock..., que es lo que de verdad prueba.

El nuevo usa un driver/conn envuelto sobre modernc.org/sqlite que cuenta
BEGIN e INSERT y dispara cancel() en el tercer INSERT: la cancelacion cae
con la transaccion abierta y el lote a medias. Asserta
errors.Is(err, context.Canceled), 0 filas en Z2, y que hubo INSERT y
BEGIN antes de la cancelacion. Contra Promote mutado a una transaccion
por fila queda en rojo con 2 filas huerfanas en Z2: el defecto HR1.
```

Archivos: `store/promote_cancel_test.go` (nuevo), `store/promote_test.go`.

## 5 — el release sin red

```
feat: tools/release compila los tres binarios y arma los cuatro paquetes npm

go run ./tools/release --version v0.1.0 --out dist/ (sin flag, la version
sale de git describe --tags --exact-match y falla si no hay tag exacto):
compila cmd/eco para windows/amd64, linux/amd64 y darwin/arm64 con
-trimpath -ldflags "-s -w", deja SHA256SUMS, arma cuatro directorios de
paquete con package.json desde plantillas y corre npm pack sobre los
cuatro. Sin red, sin goreleaser, sin CI.

El principal (eco) tiene bin: bin/eco.js, optionalDependencies por
nombre y version exacta (nunca file:, que se rompe al desempacar),
engines node >= 20 y ningun script. Los de plataforma llevan os, cpu,
bin y files. Los nombres de plataforma usan los valores de Node
(win32/x64, linux/x64, darwin/arm64), no los de Go: npm compara
process.platform y process.arch.
```

Archivos: `tools/release/main.go`, `tools/release/packages.go`,
`tools/release/release_test.go`,
`tools/release/templates/{package.json,platform.json,eco.js,README.md,platform-README.md}`.

## 6 — el tag del módulo por un proxy de archivos

```
feat: tools/goproxy publica el tag en un GOPROXY de archivos

go run ./tools/goproxy --repo . --out <proxy> --tag v0.1.0 genera
github.com/!javi!css/eco/@v/{list,v0.1.0.info,v0.1.0.mod,v0.1.0.zip},
con el zip prefixed en github.com/JaviCss/eco@v0.1.0/ y sin evidencia/,
poc/, dist/, npm/ ni tools/. Se niega a publicar un go.mod con replace:
un modulo publicado con replace no es instalable (C2).

Con eso, go install github.com/JaviCss/eco/cmd/eco@v0.1.0 con
GOPROXY=file:// sale 0 e imprime eco v0.1.0, y un consumidor sin replace
compila contra port.
```

Archivos: `tools/goproxy/main.go`, `tools/goproxy/goproxy_test.go`.

## 7 — la evidencia

```
test: la evidencia de ARN-1140

RESULTADOS.md con una linea por criterio y la seccion "Para publicar"
(los comandos exactos que corre Javier: remoto, tag, npm publish de los
cuatro en orden plataforma -> principal, licencia, quitar el replace de
base/go.mod). COMMITS.md con estos mensajes. Y la evidencia cruda: la
suite completa, el cruce linux/darwin, el rojo de HR1 contra la
mutacion, el release con sus tamanos, las cuatro pruebas npm con sus
controles negativos, y el proxy de archivos con go install y el
consumidor de juguete.
```

Archivos: `evidencia/ARN-1140/` (todos).

## Lo que el arquitecto tiene que decidir, ademas de commitear

- Si `go mod tidy` se aplica o no: removió una línea huérfana del
  `go.sum` y E2 la restauró. Está anotado en
  `evidencia/ARN-1140/c10-c11-nada-sale-y-reglas.txt`.
- Si `+dirty` en los binarios de `dist/` importa: con el árbol limpio
  (ya commiteado) hay que re-correr el release antes de `npm publish`.
- El `LICENSE`: sin él, el campo `license` de los paquetes se omite.