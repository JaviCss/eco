# ARN-1120 — mensajes de commit propuestos

`git commit` esta denegado para E2 en este repo: los cambios quedan staged y
aca esta el texto, en orden, para que el arquitecto los commitee. Convencionales,
sin atribucion de IA. La rama es `ARN-1120` desde `main` (`983f339`).

## 1 — el puerto gana Get y el escaneo de Promote desaparece

```
feat: el puerto gana Get por id y Promote deja de escanear Z3

port.Port suma Get(ctx, scope, axis, ids) con las reglas de la card: el orden
pedido se respeta, un id ausente hace fallar todo con ErrNotFound sin
resultado parcial, el lote vacio y el que pasa de port.MaxBatch dan
ErrInvalidEntry, el eje fuera de alcance da ErrAxisNotInScope y la caida
da ErrUnavailable. Lo implementan Fake y Store.

Conformance crece con ocho subtests de Get (orden, ausente, lote vacio, techo,
alcance, validacion antes que ctx, ctx antes que caida, caida) y pasa contra
Fake y Store sin SKIP.

store.Promote se reescribe sobre Get: lee el lote por id de cualquier Port y
escribe en un *Store, que es el unico que puede estampar las claves reservadas.
Un destino que no sea *Store devuelve ErrUnavailable con el motivo. Se van el
escaneo interno, scanCeiling y el context.Background de planPromotion: el ctx
del llamador gobierna todo el lote.

H7: lo promovido conserva la procedencia de la fila de origen en
Attrs["source_origin"], nueva clave reservada que el cliente no puede poner,
ademas de promoted_from y del origin del proceso que promueve.

H2: sameFile pasa a os.SameFile sobre os.Stat de los dos paths cuando ambos
existen, y a directorio padre resuelto con EvalSymlinks mas nombre base con
EqualFold cuando alguno no existe. Un hard link o un nombre corto 8.3 de la
misma base devuelven ErrUnavailable sin crear ni modificar archivos.
```

Archivos: `port/port.go`, `port/fake.go`, `port/conformance.go`,
`store/promote.go`, `store/store.go`, `store/promote_test.go`,
`store/promote_matrix_test.go`, `store/store_test.go`,
`store/samefile_test.go`, `store/samefile_windows_test.go`.

## 2 — el perfil, aplicado por el Store

```
feat: el perfil por tipo vive en el Store y el cero es el estrecho

store.Config suma Profile con ProfileAgent (lee Z2, Z3, Y, X; anexa Z3, Y, X),
ProfileHuman (lo mismo mas Promote) y ProfileRuntime (todo, incluidos R, V y
Manifest). El valor cero es ProfileAgent: un Config sin Profile abre angosto y
el Store se rechaza a si mismo con port.ErrForbidden, que es un error nuevo y
distinto de ErrAxisNotInScope.

Promote con un Store de ProfileAgent devuelve ErrForbidden sin escribir. La
libreria no cambia de contrato: el runtime abre con ProfileRuntime explicito y
los tests que hacen de runtime lo declaran.
```

Archivos: `store/profile.go`, `store/profile_test.go`, `store/store.go`.

## 3 — la puerta MCP por stdio

```
feat: eco mcp habla MCP por stdio con perfil de agente y Z2 solo-lectura

mcpdoor expone tools/list con exactamente cuatro herramientas: eco_read,
eco_search, eco_append y eco_probe. El enum de axis es {Z2,Z3,Y,X} en read y
search y {Z3,Y,X} en append, de modo que Z2 en append y R en cualquier lado los
rechaza el esquema del SDK antes de llegar al Store. No hay herramienta de
promote ni de get, y ninguna nombra R, V o Manifest.

El transporte HTTP streamable del SDK no se registra: solo stdio. tools/list
pesa 1142 bytes compactos, con el techo en 2048.

El adaptador recibe una interfaz estrecha (Read, Search, Append, Probe) y el
Store detras ya filtra por perfil: dos capas, para que un error de una no abra
la otra. Los errores del Store salen como isError con el nombre del sentinel y
nunca con su texto crudo, que puede traer el path de la base. eco_probe
devuelve un codigo tipado: ok, forbidden o unavailable.

Las respuestas se acotan en MaxToolResultBytes (32 KiB) recortando entradas
enteras desde el final, con truncated y el conteo de omitidas; ninguna entrada
se parte.
```

Archivos: `mcpdoor/door.go`, `mcpdoor/server.go`, `mcpdoor/door_test.go`,
`mcpdoor/server_test.go`, `cmd/eco/mcp.go`.

## 4 — la puerta HTTP en loopback

```
feat: eco serve abre HTTP solo en loopback, con token por proceso y dies with the father

httpdoor levanta el listener en 127.0.0.1 o ::1 con puerto 0 y el constructor
rechaza con error tipado cualquier direccion que no sea loopback. Rutas POST
/v1/read, /v1/search, /v1/get, /v1/append, /v1/promote y GET /v1/probe, con
JSON plano y los nombres de port.Entry.

El orden de las cabeceras es fijo: Host se valida contra la direccion y el
puerto reales del listener y otro Host da 421; cualquier Origin presente da
403; el bearer es obligatorio en las seis rutas, porque Z2 es dato del usuario,
y falta o no coincide da 401; el Content-Type tiene que ser application/json
o da 415; el cuerpo se acota con MaxBytesReader y excederlo da 413.

El archivo de puerto es JSON con pid, port, nonce y token, se escribe por
rename (el temporal no sobrevive) y en Windows queda con una DACL que lista
solo al usuario actual, medida con icacls en el test. probe devuelve el nonce
y eco probe --port-file lo valida contra el archivo: si no coincide es otro
proceso en el mismo puerto y sale tipado.

eco serve exige --parent-pid, abre el proceso con OpenProcess(SYNCHRONIZE) y
espera el handle: cuando el padre termina, cierra el listener, borra el archivo
de puerto y sale 0.
```

Archivos: `httpdoor/httpdoor.go`, `httpdoor/portfile.go`,
`httpdoor/acl_windows.go`, `httpdoor/acl_other.go`,
`httpdoor/httpdoor_test.go`, `httpdoor/acl_windows_test.go`,
`cmd/eco/serve.go`, `cmd/eco/parent_windows.go`, `cmd/eco/parent_other.go`.

## 5 — la CLI de verbos

```
feat: eco gana read, search, get, append, promote y probe con codigos de salida fijos

Los verbos de la CLI abren el Store con ProfileHuman y Origin cli, escriben
JSON por stdout y los errores tipados por stderr con el codigo de salida fijo
por error: 2 uso, 3 unavailable, 4 not found, 5 forbidden, 6 invalid. Ningun
mensaje de error lleva el path de una base: sale el nombre del sentinel.

eco --help lista exactamente los diez verbos. eco doctor no cambia.
```

Archivos: `cmd/eco/main.go`, `cmd/eco/verbs.go`, `cmd/eco/cli_test.go`,
`cmd/eco/cli_windows_test.go`, `cmd/eco/main_test.go`.

## 6 — dependencias y evidencia

```
chore: el SDK de MCP entra como dependencia directa y la evidencia de la card

go.mod suma github.com/modelcontextprotocol/go-sdk v1.8.0 y
golang.org/x/sys v0.48.0 como requires directos: el primero lo usa mcpdoor y
el segundo la DACL del archivo de puerto. Ninguna dependencia nueva entra al
arbol; x/sys ya venia como indirecta de modernc.org/sqlite.

evidencia/ARN-1120/ deja el detalle de los quince criterios de aceptacion, con
la salida cruda de cada corrida.
```

Archivos: `go.mod`, `go.sum`, `evidencia/ARN-1120/`.