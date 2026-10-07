# ARN-1080 — el centro de Eco: resultados (ronda 2)

Una linea por criterio de aceptacion: que se esperaba, que salio, y de que
archivo de evidencia sale. Go 1.26.1 windows/amd64, `modernc.org/sqlite`
v1.60.1, fecha 2026-10-07, **ronda 2 sobre el commit `82e9544`**. Todo el
codigo vive en `../eco`, rama `ARN-1080` desde `main`.

Esta ronda cierra los dos bloqueantes del gate r1 (B1 y B2) y los cinco
hallazgos de codigo pequeno que el arquitecto metio (H1, H3, H4, H5, H10).
H2, H6, H7 y H8 quedan declarados y sin tocar.

| # | criterio | esperado | salio | evidencia |
|---|---|---|---|---|
| 1 | `go build`, `go vet`, `go test -race` | 0 | 0, 0, 0; la suite completa con `-race` tarda 179 s en `store` | `c1-build-vet-race.txt` |
| 2 | `Conformance` de ARN-1090 contra `Store`, cero `SKIP`, `port/` sin cambios | verde, sin `SKIP` | **21 de 21 subtests verdes** (no 22: el conteo de la ronda 1 estaba mal), cero `SKIP`, `git diff main -- port/` vacio | `c2-conformance.txt`, `c2-skip.txt` |
| 3 | control negativo por mutacion | caen los subtests nombrados | mutacion 1 (sin `ON CONFLICT DO NOTHING`): caen exactamente `append_same_key_twice_stores_one_entry` y `append_same_key_twice_returns_the_same_entry`; mutacion 2 (`Search` sin resultados devuelve `ErrUnavailable`): caen exactamente `search_without_match_returns_not_found` y `not_found_and_unavailable_stay_distinct` | `c3-mut1-rojo.txt`, `c3-mut2-rojo.txt`, `c3-verde.txt` |
| 4 | `Promote`: (a) lote falla entero, (b) Z2 caida, (c) corte y reintento convergen, (d) id ocupado con otro cuerpo | los cuatro, **contra los dos dobles** | los cuatro verdes contra el fake **y** contra el `Store` (8 subtests de matriz); mas el caso de Z3 de 250 (ver B2) | `c4-promote.txt` |
| 5 | procedencia y limites | `origin` del `Store`, reservadas rechazadas, 64 KiB + 1, 33 claves, limits 0/-1/201 | todos `ErrInvalidEntry` donde corresponde | `c5-procedencia.txt` |
| 6 | apertura segura: `application_id` distinto, `user_version` mayor, junction, paths iguales | `ErrUnavailable` con motivo y sin tocar disco | los cuatro **con la base mala en `UserDB` y con la base mala en `ProjectDB`**, directorio comparado antes y despues en los ocho casos, ningun archivo nuevo | `c6-apertura.txt` |
| 7 | busqueda: `untime` encuentra `runtime`, sintaxis como texto, corta vacia | literal | los cuatro casos de sintaxis literales y el trigram pasan; vacia y de 2 caracteres dan `ErrNotFound` | `c7-busqueda.txt` |
| 8 | dos procesos x 4 goroutines x 2000 `Append`, y control negativo | cero `SQLITE_BUSY` al llamador; el control los muestra | positivo 3 de 3: `busy_leaked=0` en los dos procesos de cada corrida, 16000 filas, `retries=12` en un proceso, `max_append_ms` 1034/1235, 1033/2837, 1637/2238. Control: **7430 y 7002 `SQLITE_BUSY` al llamador**, con el dsn sin `busy_timeout` y **sin `_txlock=immediate`** | `c8-positiva-1.txt`, `c8-positiva-2.txt`, `c8-positiva-3.txt`, `c8-control-negativo.txt` |
| 9 | `eco doctor` y `--help` | version, `user_version`, tamaño, `journal_mode=wal`; aviso de carpeta sincronizada; `--help` solo `doctor` | `sqlite_version 3.53.4`, `user_version 1`, `application_id 1162037553`, `journal_mode wal`, `size_bytes 36864`; avisa de OneDrive; y **sobre bases que no existen sale 1 y deja el directorio vacio** | `c9-doctor.txt`, `c9-doctor-onedrive.txt`, `c9-help.txt`, `c9-doctor-inexistente.txt` |
| 10 | sin llamadas de red | lista cerrada | `go list -deps` da exactamente `{net, net/netip, net/url}` (los arrastra `modernc`); ningun `.go` fuera de `poc/` importa `net/http` | `c10-net.txt` |
| 11 | rendimiento, sin techo | medido | seed de 10000 en 3589 ms; 1000 `Append` en 349 ms (349.3 us promedio); 100 `Search` en 1996 ms (19968 us promedio, 1000 hits); `Promote` de 100 desde un Z3 de 10000 en 65 ms | `c11-rendimiento.txt` |
| 12 | sin comentarios y una sola dependencia directa | 0 comentarios, 1 `require` | **0 comentarios en 25 archivos** (verificado con `poc/c9`); `go.mod` con un unico `require` directo | `c12-comentarios.txt`, `c12-go-mod.txt`, `c12-modulos.txt` |
| 13 | este archivo | tabla de una linea por criterio | esta tabla | este archivo |

Ademas, la suite completa de la ronda: **119 tests y subtests `PASS`, 0
`SKIP`, 0 `FAIL`** (`r2-green.txt`), y el rojo de la ronda 2 contra el codigo
sin tocar (`r2-red.txt`).

## B1 — `Open` en dos fases: inspecciona las dos bases antes de tocar una

El defecto que el gate reprodujo era real y grave: `Open` abria y creaba
`user.db` **antes** de mirar `project.db`, asi que una base mala en
`project.db` dejaba `user.db` creada, estampada y migrada (36864 bytes) en el
disco. Los cuatro tests de la ronda 1 ponian siempre la base mala en `UserDB`,
y por eso el defecto no se veia.

Ahora `Open` hace: (1) valida la configuracion — `origin`, los dos paths, los
reparse points de cada componente y que no sean el mismo archivo; (2)
**inspecciona las dos bases** (encabezado, `application_id`, `user_version`)
sin abrir ni crear ninguna; recien ahi (3) abre o crea las dos.

Los tests son espejo: cada caso del criterio 6 corre con la base mala en
`ProjectDB` **y** con `UserDB` inexistente, y compara el directorio antes y
despues. Los ocho casos pasan sin un solo archivo nuevo
(`c6-apertura.txt`). El caso de `same_path` ahora devuelve `ErrUnavailable`
(H1) y el test mira el centinela, no solo el texto.

Un caso mas, que la ronda 1 no tenia: con **una base buena ya en disco y la
otra inexistente**, `Open` crea la que falta y `Probe` da verde
(`TestOpenCreatesBothBasesWhenBothAreMissing`): la dos fases no rompio el camino
feliz.

## B2 — el camino por puerto: que hace y que no hace

**Lo que hace.** Los cuatro comportamientos del criterio 4 se sostienen
contra los dos dobles, y eso esta medido en matriz: 4 casos x 2 dobles
(`fake` y `Store`), ocho subtests, en `c4-promote.txt`. Los cuatro ya
funcionaban en la ronda 1; lo que faltaba era la segunda columna de la matriz,
y ahora esta.

**Lo que no hace, y por que.** El puerto topa `Read` en `MaxLimit` (200) y no
pagina. `scanFor` hacia **un solo** `Read(200)`, y con un Z3 de 250 y un lote
que pide un id fuera de esa pagina devolvia `ErrNotFound` —un id que si existe
— y el llamador se lo creia. Ahora `scanFor` sabe si la pagina se lleno y
`planPromotion` convierte ese caso en un error **tipado y con el motivo**:

```
eco: Promote: eco: unavailable: [z3-000] not in the 200 rows the port returns;
Z3 excede la pagina del puerto; Promote requiere el Store
```

Es `ErrUnavailable`, nunca `ErrNotFound`: `ErrNotFound` es un juicio sobre los
datos y se podria reintentar sin cambiar nada; `ErrUnavailable` dice "no lo
puedo saber por este camino". Medido con un Z3 de 250 y un lote de dos ids, uno
dentro de la pagina y otro fuera (`TestPromoteWithAZ3BiggerThanThePortPage`):
contra el `Store` promueve los dos; contra el fake falla fuerte con el motivo y
**no escribe nada en Z2**.

Cuando el origen es un `*Store`, `Promote` sigue usando el escaneo interno sin
el tope (`scanCeiling`), y ahi si resuelve el lote de 10000. Es transitorio y
esta declarado: el fix real es `Get(ids)` o paginacion en el puerto, y va en la
card de las puertas.

## Los cinco hallazgos de codigo que entraron

- **H1** — paths iguales: `ErrUnavailable` (antes `ErrInvalidEntry`), con el
  test mirando el centinela. Razon: no es una entrada invalida, es una base que
  no se puede abrir.
- **H3** — el mismo id en Z2 y R. **Elegi `ErrInvalidEntry` con motivo**, no la
  PK `(axis, id)`. Razon de la eleccion: cambiar la PK exige reconstruir la
  tabla, y la tabla esta acoplada a la virtual FTS5 por `rowid` (los triggers
  de delete/insert dependen de el), asi que el rebuild no es codigo pequeno y
  esta es la ultima ronda del presupuesto. Lo que estaba mal no era solo el
  esquema: era que un `ErrUnavailable: no rows` **no se cura reintentando**, y
  el llamador no tenia ninguna forma de saber que el id estaba ocupado por otro
  eje. Ahora el error dice `id %q is already used by <scope>/<axis>`. **La
  PK `(axis, id)` queda declarada** para la card que migre el esquema; el fake
  de `port` ya admite el mismo id en dos ejes, asi que la divergencia entre
  fake y `Store` es real y esta escrita.
- **H4** — `Search` mapeaba los fallos de `QueryContext`, del `Scan` y de
  `rows.Err()` a `ErrNotFound`: un corte de I/O se disfrazaba de "no hay
  nada". Ahora los tres son `ErrUnavailable`, y el "no hay resultados" sigue
  siendo `ErrNotFound`. El test cierra el handle por abajo de la base que
  tiene el `Store` y pide el centinela deUnavailable, y ademas que **no** sea
  `ErrNotFound`.
- **H5** — `eco doctor` abria con `store.Open`, que crea y migra: no era
  read-only. Ahora `Open` acepta `ReadOnly: true`, que (a) exige que las dos
  bases **existan** antes de abrir nada y (b) abre con
  `_pragma=query_only(1)`, sin `journal_mode` ni `synchronous`. Si falta una,
  `ErrUnavailable` con el motivo. Los tests del binario: sobre un directorio
  vacio `doctor` sale 1 y **el directorio sigue vacio**, y sobre bases ya
  existentes el directorio queda con los mismos dos archivos y los mismos
  bytes antes y despues.
- **H10** — `_txlock=immediate` no hacia nada, porque `Append` escribia en
  autocommit y el flag solo aplica a un `BEGIN` explicito. Ahora **toda
  escritura va en `BeginTx`/`Commit`**, asi que el flag actua. El control
  negativo del criterio 8 apaga las dos cosas de verdad: el dsn que imprime en
  `HELPER_CONFIG` **no tiene `busy_timeout` ni `_txlock=immediate`**, y con el
  `BEGIN` diferido cuelga 7430 y 7002 `SQLITE_BUSY` al llamador mientras el
  positivo no deja pasar ninguno. Ademas hay un test que mide el modo de la
  transaccion en el handle de escritura: con `_txlock=immediate` la
  transaccion del `Store` **toma el lock de escritura antes de la primera
  sentencia** y una segunda conexion no puede ni hacer `BEGIN IMMEDIATE`
  (`wantHeld=true`); con el modo diferido la segunda conexion entra y escribe
  (`wantHeld=false`).

## Lo que queda declarado, sin tocar en esta ronda

- **H2** — `sameFile` compara lexico: un alias 8.3 o un hard link a la misma
  base pasa el chequeo. No se reprodujo en la maquina; el fix (identidad de
  archivo por `FileIndex`/`st_dev`+`st_ino`, o `os.SameFile`) va a la card de
  las puertas o del harness.
- **H6** — `scanCeiling` (200000) es silencioso: si el Z3 lo supera, el escaneo
  interno simplemente no ve el resto. El techo esta declarado en `planning/13`;
  hacerlo ruidoso es codigo de la card de las puertas.
- **H7** — `Promote` con dos puertos y el `origin` del Z3: hoy el `origin` de la
  entrada promovida se borra y solo queda `promoted_from`. El papel va en la
  card de las puertas.
- **H8** — los tests de `store/` no compilan fuera de Windows
  (`makeJunction` y `syscall.Win32FileAttributeData`): va a la card de
  empaquetado.

## Desviaciones, declaradas

1. **`At` del cliente se conserva, no se sobrescribe.** Firmada por el
   arquitecto en el gate r1: el subtest `at_is_stored_and_returned_as_utc` del
   juez lo exige y el juez gana. Un `At` cero lo estampa el `Store`. La
   procedencia queda en `Attrs["origin"]`, del `Store`, que rechaza las claves
   reservadas del cliente.
2. **H3 se resuelve con `ErrInvalidEntry`, no con la PK `(axis, id)`.**
   Declarado arriba, con la razon y con la card que lo recoge.
3. **`Promote` escanea Z3 por dentro del `Store`.** Transitorio, declarado por
   el arquitecto; el camino por puerto ya no miente (ver B2).
4. **La busqueda diverge del puerto en el limite de longitud.** FTS5 con
   trigram cubre substring de 3+ caracteres, no substring puro; una consulta de
   menos de tres caracteres devuelve `ErrNotFound`.
5. **`synchronous=NORMAL` puede perder el ultimo commit en un corte de
   energia.** Riesgo aceptado por la card.

## Lo que esta card NO cubre

Las tres puertas (MCP, HTTP, CLI de verbos), el empaquetado npm y el tag del
modulo Go: son cards hermanas. Tampoco el perfil por puerta, que la card de las
puertas tiene que llevar (el bloque de seguridad ya esta perfilado ahi).