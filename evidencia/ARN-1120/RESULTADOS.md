# ARN-1120 — las tres puertas de Eco: resultados

Rama `ARN-1120` desde `main` (`983f339`). Todo el codigo vive en `../eco`;
este repo (arn-v2) solo recibio papel. `git commit` esta denegado para E2 en
este repo: el trabajo queda **staged** y los mensajes propuestos estan en
`COMMITS.md`.

Reservas del ejecutor: ruta directa, riesgo alto, presupuesto 2 corridas de
evidencia y 2 rondas de gate. Todo lo de abajo salio de corridas propias; los
testes nuevos nacieron en rojo antes de existir la implementacion
(`c2-get-rojo.txt`, `c3-promote-rojo.txt`, y los rojos que cada subagente
reporto al escribir `mcpdoor/` y `httpdoor/`).

## Criterio por criterio

| # | Que se esperaba | Que salio | Evidencia |
|---|---|---|---|
| 1 | `go build`, `go vet` y `go test -race` en 0 | los tres en 0; 5 paquetes en verde, `store` 189 s con `-race` | `c1-build-vet-race.txt` |
| 2 | `Conformance` con los subtests de `Get` contra `Fake` y `Store`, sin `SKIP`, y el diff de `port/` solo con `Get` y `ErrForbidden` | 8 subtests nuevos de `Get` en verde contra las dos implementaciones; el diff de `port/` son 4 lineas de interfaz mas `ErrForbidden`, `MaxBatch` y `Fake.Get` | `c2-conformance.txt`, `c2-port-diff.txt` |
| 3 | `Promote` lee el lote por id del puerto, sin escaneo ni `context.Background`; destino que no es `*Store` -> `ErrUnavailable`; `ctx` cancelado -> `context.Canceled` y Z2 intacta | `scanCeiling`, `scanForPromotion` y `promotionScanner` no existen (rg, exit 1); `context.Background()` no aparece en `promote.go` (rg, exit 1); Z3 de 250 promueve por puerto en las dos filas de la matriz; destino `Fake` -> `ErrUnavailable` con motivo; `ctx` cancelado -> `context.Canceled` | `c3-promote-sin-escaneo.txt` |
| 4 | H7: lo promovido conserva `source_origin`, `promoted_from` y `origin`; el cliente no puede poner `source_origin` | con un origen `distiller` y un destino `cli`: `source_origin=distiller`, `promoted_from=distilled`, `origin=cli`; `Append` con `source_origin` del cliente -> `ErrInvalidEntry` | `c4-h7-procedencia.txt` |
| 5 | H2: hard link y nombre corto 8.3 de la misma base -> `ErrUnavailable` sin crear ni modificar archivos | los tres tests en verde; el nombre corto real medido es `USER-B~1.DB` y el listado del directorio no cambia | `c5-h2-paths.txt` |
| 6 | perfil por tipo con el cero estrecho; `Append` Z2 y R con agente -> `ErrForbidden`; `Promote` con agente -> `ErrForbidden` sin escribir; runtime escribe R; control negativo | el cero es `ProfileAgent`; el control negativo (mutar el cero a `ProfileRuntime`) deja en rojo **exactamente** `TestProfileZeroIsTheNarrowOne` y nada mas en los cinco paquetes | `c6-perfil.txt` |
| 7 | `tools/list` por stdio contra el propio binario con 4 herramientas; enum `{Z2,Z3,Y,X}` en read/search y `{Z3,Y,X}` en append; compacto <= 2048 bytes; `append` en Z2 -> `isError` y Z2 intacta; `axis: R` rechazado por esquema; HTTP streamable no registrado | `tools=[eco_append eco_probe eco_read eco_search]`, **1142 bytes** compactos; `axis: Z2` y `axis: R` en append los rechaza el enum del SDK; el `Fake`/Store no llega a escribir; rg sin coincidencias de `StreamableHTTP` en produccion (la unica aparicion del repo es la asercion negativa del test) | `c7-mcp-tools.txt` |
| 8 | con 500 entradas de 1 KiB en Z3: `truncated: true`, conteo de omitidas, ninguna entrada partida, <= 32 KiB | MCP: 28 entradas enteras, 172 omitidas, **31712 bytes** por `tools/call`; HTTP: 29 conservadas, 471 omitidas, **32699 bytes** | `c8-mcp-truncado.txt` |
| 9 | constructor rechaza `0.0.0.0` y `::`; servidor en `127.0.0.1:0` y `[::1]:0`; `Host` ajeno 421, `Origin` 403, `Content-Type` 415, cuerpo excedido 413 | los cuatro codigos salen en los tests y quedan logueados; bind v4 y v6 medidos | `c9-http-cabeceras.txt` |
| 10 | sin `Authorization` 401 en las seis rutas; token ajeno 401; token correcto 200; archivo de puerto con `pid`, `port`, `nonce`, `token`, escrito por rename y con ACL solo-usuario; `eco probe --port-file` valida el nonce | la tabla de seis rutas da 401 y el token correcto da 200; `icacls` del archivo de puerto lista una sola entrada, `DESKTOP-PH3954H\Javier Css:(F)`, SID `S-1-5-21-...-1000`; `eco probe --port-file` responde `{"nonce":"f5da678f...","pid":17168,"port":52104,"status":"ok"}` y con el nonce alterado sale `eco: unavailable` | `c10-http-token-puerto.txt` |
| 11 | `eco serve` muere con el padre en menos de 2 s y el archivo de puerto no esta | el helper lanzador pid 35936, el padre muere, el hijo no existe y el archivo no esta dentro de la ventana de 2 s | `c11-muere-con-el-padre.txt` |
| 12 | `eco --help` lista exactamente los diez verbos; `append --axis Z2` sale 5; `promote` con id ausente sale 4 y Z2 no cambia; `promote` real promueve y `get` lo devuelve con `source_origin`; `read` sin `--project-db` sale 2 | los cinco casos en verde; el `get` del promovido trae `source_origin` y `promoted_from` | `c12-cli.txt` |
| 13 | ningun mensaje de error de MCP, HTTP ni CLI contiene el path de una base; `net/http` y el SDK solo en `cmd/eco`, `mcpdoor/` y `httpdoor/` | los tres tests de fuga en verde (el port sembrado con un error que contiene `C:/secret/base.db` no lo deja pasar); `go list -deps ./... \| rg '^net'` pegado; los unicos archivos de produccion que tocan red o el SDK son `httpdoor/httpdoor.go`, `mcpdoor/server.go`, `mcpdoor/door.go`, `cmd/eco/serve.go` y el PoC `poc/c5-mcp-tools-list` | `c13-sin-fugas.txt` |
| 14 | ningun `.go` con comentarios salvo `//go:build`; `go.mod` con dos `require` directos; `go list -m all` pegado | 52 archivos `.go` revisados con `go/ast`, **0** comentarios fuera de `//go:build`; `go list -m all` pegado. **Desvio declarado**: hay **tres** `require` directos, porque `httpdoor` usa `golang.org/x/sys/windows` para la DACL del archivo de puerto (ya estaba en el arbol como indirecta de `modernc.org/sqlite`; no entra ninguna dependencia nueva) | `c14-comentarios.txt` |
| 15 | `RESULTADOS.md` con una linea por criterio y `COMMITS.md` con los mensajes propuestos | este archivo y `COMMITS.md` | este archivo |

## Lo que se midio y quedo como numero

- `tools/list` compacto: **1142 bytes** (techo 2048). El PoC C5 de ARN-1090
  media 843 bytes con cinco herramientas de un campo; con cuatroTools de Eco
  y esquemas con enum el numero sube, y sigue bajo el techo.
- Resultado de `eco_read` con 500 entradas de 1 KiB: **31712 bytes** por MCP y
  **32699 bytes** por HTTP (techo 32768). Las dos puertas recortan por entrada
  entera y no parten ninguna.
- `go test -race ./...` completo: 5 paquetes en verde, `store` 189 s.

## Desvios, huecos y decisiones que el arquitecto tiene que ver

1. **`go.mod` con tres `require` directos, no dos** (criterio 14). La DACL
   solo-usuario del archivo de puerto (criterio 10) se implementa con
   `golang.org/x/sys/windows`: el paquete `syscall` de la biblioteca estandar
   no expone `SetNamedSecurityInfo` ni iteradores de ACE. `x/sys v0.48.0` ya
   estaba en el arbol como dependencia indirecta de `modernc.org/sqlite`, asi
   que no entra ninguna dependencia nueva, pero el marcador `// indirect` de
   `go mod tidy` no aplica y el `require` queda directo. La alternativa
   (SDDL a mano con `syscall.NewLazyDLL`) se puede escribir, pero cuesta una
   capa de `unsafe` que no vale una linea de `go.mod`.
2. **El texto del rechazo de esquema del SDK no pasa por el mapeo a sentinel.**
   Cuando el enum rechaza `axis: "R"` o `axis: "Z2"`, el SDK responde
   `isError: true` con su propio texto (`validating "arguments": ... enum: R
   does not equal any of: [Z3 Y X]`). Todo error que viene del `Store` si
   pasa por el mapeo y sale como nombre de sentinel; el texto del SDK no
   filtra paths ni stacks, asi que el criterio 13 se sostiene, pero el
   criterio 7 pide "error de esquema", y eso es exactamente lo que hay.
3. **La costura de `promote` en HTTP.** `store.Promote` exige que el destino
   sea un `*Store` y que su perfil pueda promover, asi que la puerta recibe
   el cliente por la interfaz `port.Port` y ademas busca, si el cliente la
   tiene, una `PromoteSource() port.Port`. En `eco serve` el cliente es el
   mismo `Store` de las dos bases, asi que el origen de Z3 y el destino de Z2
   son el mismo proceso. El fallback (usar el propio cliente) esta probado.
4. **`gofmt -l` lista casi todo el repo, antes y despues de esta card.** La
   causa es una sola: los archivos del repo no terminan en newline y gofmt
   quiere ponerlo. `gofmt -d` sobre `port/port.go` y `cmd/eco/verbs.go` solo
   muestra ese cambio final, asi que el codigo nuevo esta formateado y la
   convencion del repo (sin newline final) se respeta.
5. **H8 sigue declarado**: `store/helpers_windows_test.go` (y ahora
   `store/samefile_windows_test.go`, `httpdoor/acl_windows.go` y
   `cmd/eco/parent_windows.go`) no compilan fuera de Windows. La ACL y el
   nombre corto se declararon en la card de empaquetado (ARN-1130), no aqui.
6. **Lo que esta card declara y no verifica**, igual quefirmo el
   presupuesto: empaquetado npm y tag Go (ARN-1130); descubrimiento del
   archivo de puerto, daemon y autoarranque (modulo 5 y `layout`);
   paginacion por cursor, ranking, embeddings, dedup, W2, H11, H3; N1 a N4.
   Y **el rendimiento de las puertas no se midio**: ningun numero de esta
   card es de throughput.
7. **La suite corre en Windows** (GOOS=windows/amd64). `acl_other.go` y
   `parent_other.go` compilan pero no se ejercitaron: sin una maquina POSIX
   no se afirma nada sobre ellos.
---

# ARN-1120 ronda 2 — el gate de la ronda 1 dio FAIL con un bloqueante (B1)

HEAD de partida: `11cb303`, misma rama `ARN-1120` de `../eco`. Los cinco
arreglos de la "Direccion de ronda 2" con su rojo por comportamiento antes del
verde. `git commit` sigue denegado para E2: el trabajo queda **staged** y los
mensajes estan en `COMMITS.md`.

| # | Que se esperaba | Que salio | Evidencia |
|---|---|---|---|
| 16 (B1) | el lote entero de Z2 en **una sola transaccion**; un `ctx` cancelado a mitad del lote devuelve `context.Canceled` **sin envolver** y Z2 con cero filas; rojo contra `11cb303` | contra `11cb303` el lote dejaba **12 filas** en Z2 cuando fallaba en la fila trece, y la cancelacion salia como `eco: Append: eco: unavailable: context canceled` con `errors.Is` falso (los dos casos, 15 ms y 40 ms). Ahora `store.writePromotedBatch` abre **una** transaccion (`db.BeginTx` con el `_txlock=immediate` del DSN, el mismo BEGIN IMMEDIATE que usa `Append`), inserta las N filas, las relee y hace `Commit` al final: `TestPromoteIntoZ2IsAllOrNothing` deja **0** filas y los dos casos de cancelacion dan `errors.Is(err, context.Canceled)` con **0** filas | `c16-b1-lote-una-transaccion-rojo.txt`, `c16-b1-lote-una-transaccion-verde.txt` |
| 17 (H1) | `Host: localhost:<cualquier puerto>` deja de pasar el atajo de `hostAllowed`; `Host: localhost:9` -> 421 | contra `11cb303` el mismo `Host` recibia **200** con el cuerpo de la lectura; ahora `hostAllowed` solo compara contra la lista derivada del listener y `localhost:9` da **421** `host mismatch` | `c17-h1-host-localhost-rojo.txt`, `c17-h1-host-localhost-verde.txt` |
| 18 (H2) | la DACL del usuario va sobre el temporal **vacio**, antes del primer `Write` | contra `11cb303`, mientras se escribia el token el temporal llevaba **cuatro** ACE (`...-1004`, `...-174881868`, `SY`, `BA`, `...-1000`): la ACL heredada del directorio. Ahora el archivo **nace** con la DACL: `windows.CreateFile` con `SECURITY_ATTRIBUTOS` (`httpdoor.createPrivateTemp`) y no hay ventana entre crear y proteger. Con el token de 8 MiB, el observador que mira el temporal mientras se escribe ve **un solo ACE**, el SID del usuario | `c18-h2-dacl-antes-del-write-rojo.txt`, `c18-h2-dacl-antes-del-write-verde.txt` |
| 19 (H4) | el tipo y el perfil del destino se chequean **antes** de `source.Get`; el test HTTP deja de aceptar "503 o 404" y exige 503 | contra `11cb303` un destino `Fake` con id ausente daba **404** `not found` (el `Get` corria primero); ahora `Promote` resuelve destino y perfil antes de planificar, y el test exige **503** `unavailable` | `c19-h4-destino-antes-de-get-rojo.txt`, `c19-h4-destino-antes-de-get-verde.txt` |
| 20 (H5) | `eco mcp` sin `--user-db`/`--project-db` sale **2** | contra `11cb303` salia **6** con `eco open: eco: invalid entry`; ahora `mcpVerb` rechaza los flags faltantes con `refuse` y sale **2** con `eco mcp: --user-db and --project-db are required` | `c20-h5-mcp-sin-flags-rojo.txt`, `c20-h5-mcp-sin-flags-verde.txt` |
| 21 | suite entera `-race` verde, sin regresion | `go build ./...`, `go vet ./...` y `go test -race -count=1 ./...` en 0; los cinco paquetes en verde (`store` 164.8 s) | `c21-suite-race-verde.txt` |
| 13 y 14 | los criterios de fugas y de comentarios siguen sostenidos | `go list -deps ./... \| rg '^net'` igual; `net/http` y el SDK solo en `cmd/eco`, `httpdoor/`, `mcpdoor/`; contador AST: **0** comentarios fuera de `//go:build` en 46 archivos; `port/` sin cambios de la ronda 2; `promote.go` sin `scanCeiling`, `scanForPromotion` ni `context.Background` | `c22-verificaciones.txt` |

## Invariante que B1 compra, y por que un lote parcial es imposible

Una transaccion por lote sobre `user.db`: `BEGIN IMMEDIATE` (el `_txlock=immediate`
del DSN del `Store`, el mismo que usa `Append`), los N `INSERT` de `sqlInsert`
(`ON CONFLICT DO NOTHING`), la relectura de cada fila con `sqlGet` **dentro** de la
transaccion para comparar el cuerpo, y un unico `Commit` al final. Cualquier
salida —error de `checkEntry`, conflicto de cuerpo, `SQLITE_BUSY` que agota los
reintentos, `ctx` cancelado— pasa por `tx.Rollback()`. Como no hay `COMMIT`
intermedio, el archivo de Z2 no puede quedar con un prefijo del lote: lo que se
commitio es el lote entero o nada. `withRetry` reintenta el **lote entero** desde
la primera fila, nunca desde el punto en que se quedo.

El `ctx` del llamador gobierna todo: si se cancela mientras la transaccion esta
abierta, `Rollback` corre sobre la conexion y el lote no deja filas. Para que
`errors.Is(err, context.Canceled)` sea verdadero, `Promote` devuelve `ctx.Err()`
sin envolver en cuanto el contexto esta hecho (`unwrapContext`), y `withRetry`
hace lo mismo cuando la operacion que fallo fue cancelada y no un `BUSY`.

## Lo que la ronda 2 **no** toca

Los hallazgos H3 (una DACL por SID no le niegue la lectura a un proceso de
integridad baja del mismo usuario), H6, H7, H8, H9, H10 y H11 quedan como los
dejo el gate: H3 ya fue enviado por el arquitecto a la card del tier
AppContainer del modulo 5, y el resto no era bloqueante. El cambio de la ronda 2
es cinco arreglos sobre `11cb303`, sin tocar `port/`, `mcpdoor/` ni los schemas.