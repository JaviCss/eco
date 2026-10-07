# ARN-1090 — las seis dudas del mecanismo, medidas

Cada fila dice qué se midió, con qué control, y qué salió. La salida cruda
completa de cada corrida está en `out/cN.txt`; acá va el recorte con el número
que decide. Fecha de la corrida: 2026-10-07. Go 1.26.1 windows/amd64,
`modernc.org/sqlite` v1.60.1, node v24.14.0, npm 11.9.0.

| | duda | resultado medido | control |
|---|---|---|---|
| C1 | `internal` cruzado | **no compila**, contra el `base` real | control que separa `internal` de "otra causa" |
| C2 | `go install` con `replace` | **falla**: el go.mod con `replace` no se puede instalar | mismo árbol sin el `replace` instala en 0 |
| C3 | FTS5 trigram en `modernc.org/sqlite` | funciona; `untime` encuentra 4 filas solo con trigram | la misma consulta en la tabla sin trigram: 0 filas |
| C4 | dos procesos escribiendo | **`busy_timeout(5000)` NO da cero `SQLITE_BUSY`**: 1–2 de 16000 | control negativo sin timeout ni `immediate`: 15778 de 16000 |
| C5 | bytes de `tools/list` | 843 bytes compactos, 1582 indentados, 210.75 tokens (bytes/4) | el mismo resultado con el SDK crudo, `Out` sin tipar |
| C6 | `npm i` de dos tarballs | el `bin` resuelve con los dos paquetes | solo el principal: el `bin` queda sin binario |

## C1 — `internal` cruzado

Un módulo del repo `eco` que importa `github.com/JaviCss/arn-v2/base/internal/eco`
no compila, y ningún `replace` lo arregla. Medido contra el `base` real, con
`replace github.com/JaviCss/arn-v2/base => .../arn-v2/base` en el `go.mod` del
PoC.

```
main.go:3:8: use of internal package github.com/JaviCss/arn-v2/base/internal/eco not allowed
```

Controles, para que el error sea por `internal` y no por otra cosa:

- el paquete público `github.com/JaviCss/arn-v2/base` compila con el mismo `go.mod`;
- `go build ./publico` sale 0 mientras `go build .` sale 1;
- un `replace` sobre el path del paquete `internal` tampoco lo arregla.

## C2 — `go install` con `replace`

`go install m@v` no lee el `go.mod` del árbol de trabajo. Con un módulo publicado
que trae `replace` en su `go.mod`:

```
The go.mod file for the module providing named packages contains one or more
replace directives. It must not contain directives that would cause it to be
interpreted differently than if it were the main module.
```

Control: **el mismo árbol** sin el `replace` instala en 0 por el mismo camino
(`@v1.1.0`), y `go install golang.org/x/tools/cmd/goimports@latest` instala bien.
O sea: el fallo es el `replace`, no el modo de módulo ni el proxy
(`-mod=mod`, `-mod=readonly` y `GO111MODULE=off` dan el mismo error). De ahí la
decisión de que el `replace` viva en el consumidor.

## C3 — FTS5 en `modernc.org/sqlite`

```
sqlite_version = 3.53.4
CREATE VIRTUAL TABLE con_trigram USING fts5(body, tokenize='trigram');
CREATE VIRTUAL TABLE con_default  USING fts5(body);

SELECT count(*) FROM con_trigram WHERE con_trigram MATCH 'untime';  -> 4
SELECT count(*) FROM con_default  WHERE con_default  MATCH 'untime';  -> 0
```

Diferencia medida: **4 filas**. `untime` encuentra `runtime` solo con trigram;
sin trigram, cero. Los cinco filas son las mismas en las dos tablas.

`bm25(con_trigram)` existe y da scores negativos (más negativo = mejor):

```
rowid=3 score=-1.5984703632887192e-06 body=runtime runtime runtime
rowid=1 score=-1.0731707317073174e-06 body=the runtime fell over
rowid=5 score=-9.9642431466031e-07   body=runtime is not a boundary
rowid=2 score=-8.9989235737352e-07   body=a runtime boundary needs a seam
```

Ninguna sentencia falló: ni el `CREATE VIRTUAL TABLE`, ni los inserts, ni `bm25`.

## C4 — dos procesos escribiendo (la hipótesis de la card NO se sostiene)

Dos procesos, 4 goroutines × 2000 `INSERT` cada uno = 16000 operaciones, sobre el
mismo archivo, WAL.

**POSITIVA, `busy_timeout(5000)` + `_txlock=immediate`, tres corridas:**

| corrida | ok | SQLITE_BUSY | filas | `max_insert_ms` | `integrity_check` |
|---|---|---|---|---|---|
| 1 | 15999 | **1** | 15999 | 5030 | ok |
| 2 | 15998 | **2** | 15998 | 5031 | ok |
| 3 | 15999 | **1** | 15999 | 5027 | ok |

```
first_busy_raw="database is locked (5) (SQLITE_BUSY)"
```

La card pedía **cero** `SQLITE_BUSY`. Medido: no es cero. Y `max_insert_ms` de
5030 con un timeout de 5000 dice que el busy handler **sí** se respeta: la espera
los 5 s completos y recién ahí falla. Es decir, con 8 escritores en Windows la
cola de lock excede 5 s.

**SONDA de caracterización, `busy_timeout(20000)` + `immediate`:**

```
PARENT ok=16000 busy=0 rows=16000 expected=16000 integrity_check=ok
```

Cero `SQLITE_BUSY` y las 16000 filas. Confunde lo que se iba a medir: el número
mágico era el timeout, no el mecanismo.

**CONTROL NEGATIVO, `busy_timeout=0` sin `immediate`** (tenía que mostrar
`SQLITE_BUSY`):

```
PARENT ok=222 busy=15778 rows=222 expected=16000 integrity_check=ok
child 0 first_busy_raw="database is locked (261)"     <- 261 = SQLITE_BUSY_SNAPSHOT
child 1 first_busy_raw="database is locked (5) (SQLITE_BUSY)"
```

El control falla como debe: 15778 de 16000. El `261` importa: `SQLITE_BUSY_SNAPSHOT`
no lo reintenta el busy handler por diseño, y por eso `_txlock=immediate` no es
opcional.

**Lo que queda para el centro (no es de esta card):** con `busy_timeout(5000)`
hace falta un retry loop con backoff por encima del DSN, o un timeout
dimensionado contra la carga real. 20000 ms alcanza para esta carga y es un
número empírico de una máquina, no una constante para el producto.

## C5 — bytes de `tools/list` del SDK oficial

Cinco herramientas de esquema mínimo (`inputSchema` a mano, `OutputSchema` nil,
sin `Out` tipado), SDK `github.com/modelcontextprotocol/go-sdk` v1.8.0,
sesión cliente-servidor en memoria.

```
tools_list_bytes_compact: 843
tools_list_bytes_indented: 1582
tools_list_tokens_est_bytes_div_4: 210.75
herramientas: 5
```

El JSON íntegro está en `out/c5-tools-list.json`. Ojo: la respuesta trae campos
que no son de la spec (`resultType`, `_meta.serverInfo`, `ttlMs`, `cacheScope`),
así que parte de esos 843 bytes es overhead del SDK, no del payload de
`tools/list`. Para registrar una herramienta sin `Out` tipado hizo falta el
método crudo `(*mcp.Server).AddTool(t *mcp.Tool, h mcp.ToolHandler)`; el
genérico `mcp.AddTool[In, Out]` sí lo exigiría.

## C6 — `npm i` de dos tarballs

`@eco-poc/principal` con `bin: {eco}` y `optionalDependencies` a
`@eco-poc/plataforma-win`, que trae `bin: {eco-win}`. Los dos con `npm pack`, los
dos instalados en un temporal con `npm i a.tgz b.tgz`, flags
`--no-audit --no-fund --install-strategy=hoisted`.

**POSITIVA:**

```
added 2 packages in 1s
node_modules/.bin: eco, eco.cmd, eco.ps1, eco-win, eco-win.cmd, eco-win.ps1
$ node_modules\.bin\eco.cmd
eco-win ok                                     (exit 0)
$ node_modules\.bin\eco-win.cmd
eco-win ok                                     (exit 0)
```

**CONTROL NEGATIVO (solo el principal):**

```
added 1 package in 1s
node_modules/.bin: eco, eco.cmd, eco.ps1        <- eco-win NO aparece
$ node_modules\.bin\eco.cmd
eco: platform binary missing: eco-win.cmd      (exit 2)
npm ls --all:
`-- UNMET OPTIONAL DEPENDENCY @eco-poc/plataforma-win@file:../plataforma-win-0.0.1.tgz
```

El control falla como debe: el principal instala, el `bin` queda apuntando a un
binario que no está, y el fallo aparece en **runtime**, no en la instalación.
Lo más importante para el mecanismo: **npm avisa con un mensaje engañoso** —
`npm warn tarball tarball data for @eco-poc/plataforma-win@file:... (null) seems
to be corrupted. Trying again.` (4 veces), exit code 0, y nunca dice "optional
omitida". El diagnóstico real (`UNMET OPTIONAL DEPENDENCY`) solo sale si corrés
`npm ls --all` a mano. Instalar sin mirar el árbol es indistinguishable de
instalar bien.

Ruido conocido del experimento: `npm ls --all` marca la opcional como `invalid`
en la positiva, porque el spec `file:../plataforma-win-0.0.1.tgz` queda roto al
desempacar el paquete. No afecta la ejecución del `bin`; sí significa que el
spec de una opcional tiene que ser resoluble desde donde se desempaca.

## Cómo reproducir

Cada PoC tiene su `go.mod` propio (o sus `package.json`), así que ninguno toca
el `go.mod` del producto:

```
cd poc/c1-interno && go build ./...
cd poc/c3-fts5-trigram && go run .
cd poc/c4-dos-procesos && go build -o c4.exe . && ./c4.exe -mode parent -db <scratch>/x.db -busy 5000 -immediate true
cd poc/c5-mcp-tools-list && go run .
cd poc/c6-npm-tarballs && npm pack en cada paquete y npm i de los dos .tgz
```