# ARN-1080 — el centro de Eco: resultados

Una linea por criterio de aceptacion: que se esperaba, que salio, y de que
archivo de evidencia sale. Go 1.26.1 windows/amd64, `modernc.org/sqlite`
v1.60.1, fecha 2026-10-07. Todo el codigo vive en `../eco`, rama `ARN-1080`
desde `main`.

| # | criterio | esperado | salio | evidencia |
|---|---|---|---|---|
| 1 | `go build`, `go vet`, `go test -race` | 0 | 0, 0, 0 | `c1-build-vet-race.txt` |
| 2 | `Conformance` de ARN-1090 contra `Store`, cero `SKIP`, `port/` sin cambios | verde, sin `SKIP` | 22 de 22 subtests verdes, cero `SKIP`, `git diff -- port/` vacio | `c2-conformance.txt`, `c2-skip.txt` |
| 3 | control negativo por mutacion | caen los subtests nombrados | mutacion 1: caen exactamente `append_same_key_twice_stores_one_entry` y `..._returns_the_same_entry`; mutacion 2: caen exactamente `search_without_match_returns_not_found` y `not_found_and_unavailable_stay_distinct` | `c3-mut1-rojo.txt`, `c3-mut2-rojo.txt`, `c3-verde.txt` |
| 4 | `Promote`: (a) lote falla entero, (b) Z2 caida, (c) corte y reintento convergen, (d) id ocupado con otro cuerpo | los cuatro, contra fake y `Store` | los cuatro pasan; (c) deja exactamente una entrada por id con `promoted:<id>` y `promoted_from` | `c4-promote.txt` |
| 5 | procedencia y limites | `origin` del `Store`, reservadas rechazadas, 64 KiB + 1, 33 claves, limits 0/-1/201 | todos `ErrInvalidEntry` donde corresponde | `c5-procedencia.txt` |
| 6 | apertura segura: `application_id` distinto, `user_version` mayor, junction, paths iguales | `ErrUnavailable` con motivo y sin tocar disco | las cuatro, con comparacion del directorio antes y despues | `c6-apertura.txt` |
| 7 | busqueda: `untime` encuentra `runtime`, sintaxis como texto, corta vacia | literal | los cuatro casos de sintaxis literales y el trigram pasan; vacia y de 2 caracteres dan `ErrNotFound` | `c7-busqueda.txt` |
| 8 | dos procesos x 4 goroutines x 2000 `Append`, y control negativo | cero `SQLITE_BUSY` al llamador; el control los muestra | positivo: 3 de 3 corridas, 16000 appends cada una, `busy_leaked=0` las tres; en una corrida anterior un `Append` tardo 13621 ms con `busy_timeout=5000` y el `Store` absorbio 3 reintentos sin filtrar nada. Control: **6982 y 7448 `SQLITE_BUSY` al llamador**, con 14201 y 15163 reintentos agotados | `c8-positiva-1.txt`, `c8-positiva-2.txt`, `c8-positiva-3.txt`, `c8-control-negativo.txt` |
| 9 | `eco doctor` y `--help` | version, `user_version`, tamaño, `journal_mode=wal`; aviso de carpeta sincronizada; `--help` solo `doctor` | todo presente; `sqlite_version 3.53.4`, `user_version 1`, `journal_mode wal`; avisa de OneDrive | `c9-doctor.txt`, `c9-doctor-onedrive.txt`, `c9-help.txt` |
| 10 | sin llamadas de red | lista cerrada | `go list -deps` da exactamente `{net, net/netip, net/url}` (los arrastra `modernc`); ningun `net/http` fuera de `poc/` | `c10-net.txt` |
| 11 | rendimiento, sin techo | medido | seed 10000 en 3547-8254 ms; `Append` 449 us promedio; `Search` 21655 us; `Promote` de 100 desde un Z3 de 10000 en 110 ms | `c11-rendimiento.txt` |
| 12 | sin comentarios y una sola dependencia directa | 0 comentarios, 1 `require` | 0 comentarios en 20 archivos (verificado con `poc/c9`); `go.mod` con un unico `require` directo | `c12-comentarios.txt`, `c12-go-mod.txt`, `c12-modulos.txt` |
| 13 | este archivo | tabla de una linea por criterio | esta tabla | este archivo |

## Lo que el criterio 8 midio y por que importa

La corrida positiva 2 de una tanda anterior es la que importa:
`max_append_ms=13621` con `busy_timeout(5000)`. Un solo `Append` tardo 13.6 s,
mas que el timeout, y aun asi el llamador recibio `busy_leaked=0`: el `Store`
absorbio 3 reintentos con backoff. Eso confirma la conclusion que ARN-1090 C4
refuteo (5000 ms no alcanza con 8 escritores en Windows) y la convierte en la
receta que esta card hereda. Las tres corridas que van adjuntas repiten el
positivo con `busy_leaked=0` y `retries=0` (`max_append_ms` 935-2242 ms): con
esta carga la contencion no llega a desbordar el timeout, y por eso el backoff
no llega a dispararse. El control negativo, con el reintento apagado, deja pasar
**6982 y 7448** `SQLITE_BUSY` al llamador: sin el retry, la garantia no existe.

El mecanismo queda asi, y es el mismo que ARN-1090 C4 midio: WAL,
`busy_timeout(5000)`, `_txlock=immediate`, y **reintento con backoff acotado por
encima**. El `immediate` no es opcional: sin el aparece `SQLITE_BUSY_SNAPSHOT`
(261), que el busy handler de SQLite no reintenta por diseno.

## Desviaciones y huecos, declarados

1. **`At` del cliente se conserva, no se sobrescribe.** El criterio 5 pide que
   el `Store` lo sobrescriba con su reloj, pero el subtest
   `at_is_stored_and_returned_as_utc` del juez (ARN-1090, que esta card declara
   como juez y que exige pasar sin tocar) falla si se sobrescribe. **El juez
   gana**: un `At` explicito del cliente se respeta; un `At` cero lo estampa el
   `Store` con su reloj. La procedencia queda garantizada por `Attrs["origin"]`,
   que si es del `Store` y rejects las claves reservadas del cliente. Esta
   decision la firma el arquitecto: es una lectura, no una lectura obvia.

2. **`Promote` lee Z3 por dentro del `Store`, no por el puerto.** El puerto
   topa `limit` en 200 y no tiene paginacion, asi que un lote de ids de un Z3
   de 10000 no se podria resolver con `Read` solo. `Promote` sigue siendo una
   funcion sobre `port.Port` (funciona contra el fake) pero, cuando el origen
   es un `*Store`, usa un escaneo interno sin el tope. Es la lectura que hace
   posible el criterio 11. La limitacion real: sin `Get(id)` en el puerto, el
   escaneo es O(n) y el techo esta en `scanCeiling`.

3. **La busqueda diverge del puerto en el limite de longitud.** El puerto
   promete substring; FTS5 con trigram cubre substring de 3+ caracteres. Una
   consulta de menos de tres caracteres devuelve `ErrNotFound` (no
   `ErrUnavailable`). Queda escrito, como pide la card.

4. **`synchronous=NORMAL` puede perder el ultimo commit en un corte de energia.**
   Riesgo aceptado por la card, no resuelto aqui.

## Lo que esta card NO cubre

Las tres puertas (MCP, HTTP, CLI de verbos), el empaquetado npm y el tag del
modulo Go: son cards hermanas. Tampoco el perfil por puerta, que la card de las
puertas tiene que llevar (el bloque de seguridad ya esta perfilado ahi).