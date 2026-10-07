# Eco

Eco es la memoria de los agentes de ARN y de cualquier harness que quiera
usarla. Es un binario Go con SQLite adentro (`modernc.org/sqlite`, sin CGO)
y tres puertas sobre un mismo puerto: MCP por stdio para el modelo, HTTP en
loopback para los programas y una CLI de verbos para el humano. El runtime
de ARN v2, que también es Go, la importa como librería en el mismo proceso.
Se publica dos veces desde el mismo código: `@arn-harness/eco` en npm, con el
binario por plataforma adentro, y `github.com/JaviCss/eco` como módulo Go
(tag `v0.1.0`, MIT).

## Qué clase de memoria es

Vocabulario, en una línea cada término:

- **Capa**: un nivel de la memoria según qué guarda, quién lo escribe y
  cuánto dura. Viene de la memoria 4D de Tellar (`diseno/01-particion.md` §7).
- **Eje** (`port.Axis`): el nombre que el código le da a cada capa. Cada eje
  pertenece a exactamente un alcance; pedirlo en el otro da `ErrAxisNotInScope`.
- **Alcance** (`port.Scope`): `user` o `project`. Son dos bases SQLite
  separadas, nunca unidas con `ATTACH` (ADR-0014 p3); lo que cruza alcances
  se fusiona en el cliente con `port.Merge`.
- **Entrada** (`port.Entry`): id, eje, alcance, instante en UTC, cuerpo de
  hasta 64 KiB y hasta 32 atributos texto. El id es la clave de idempotencia:
  la pone el cliente o la deriva el adaptador con `port.DefaultID`
  (SHA-256 de alcance, eje y cuerpo). Repetir un id no duplica.
- **Solo-anexar**: el puerto no tiene operación de editar ni de borrar. Lo
  que entra queda; R lo exige (ADR-0015 p5) y los demás ejes lo heredan.
- **Promoción supervisada**: pasar una entrada de Z3 (proyecto) a Z2
  (usuario). La dispara el runtime al cerrar un ciclo verificable, nunca el
  agente que hizo el trabajo (ADR-0014 p4). Eco aporta el verbo `Promote`;
  cuándo llamarlo lo decide quien lo importa.
- **Perfil** (`store.Profile`): lo que cada puerta puede leer y escribir,
  fijado por subcomando y no por flag (ADR-0014 p6 enmendado).
- **Composición**: el base fija sus capas y cada kit suma la forma de Y y
  los tipos de Z3 (ADR-0002 p3). En v0.1.0 es diseño, no código.

```
base del usuario (una por máquina)          base del proyecto (una por repo)
+-------------------------------------+     +----------------------------------+
| Z2  destilado compartido            |     | Z3  destilado del proyecto       |
|     lee: agente, humano, runtime    | <-- |     escribe: agente, humano      |
|     escribe: runtime; humano solo   |     | Y   episodios, bitácora          |
|     por Promote desde Z3            |     | X   la sesión viva               |
| R   telemetría del runtime          |     |     escribe: agente, humano      |
| V   validación (Arconte con firma)  |     +----------------------------------+
| Manifest  lo que armó el Compositor |
|     R, V, Manifest: solo runtime    |
+-------------------------------------+
```

## Qué capas cubre hoy y qué está planificado

| Capa o mecanismo | v0.1.0 | Fuente |
|---|---|---|
| Ejes Z2, R, V, Manifest en la base del usuario | implementado: tabla eje→alcance en `port/port.go` | ADR-0014 p2, ADR-0015, ADR-0017 |
| Ejes Z3, Y, X en la base del proyecto | implementado | ADR-0014 p2 |
| Dos bases sin `ATTACH`; rechazo si son el mismo archivo | implementado en `store.Open` | ADR-0014 p3 |
| Operaciones `Get`, `Read`, `Append`, `Search`, `Probe` | implementado; suite `port.Conformance` contra `Fake` y `Store` | ADR-0031 |
| `Promote` Z3→Z2, lote todo o nada, sellos `promoted_from` y `source_origin` | implementado en `store/promote.go`; el disparador es del runtime | ADR-0014 p4 |
| Búsqueda de texto: FTS5 trigram con `bm25`, frase literal, mínimo 3 caracteres | implementado | `store/schema.go`, `poc/RESULTADOS.md` C3 |
| Perfiles agent, human, runtime | implementado en `store/profile.go` | ADR-0014 p6 |
| Procedencia: atributo `origin` sellado por el `Store`; claves reservadas rechazadas al cliente | implementado | `evidencia/ARN-1120` |
| Apertura segura: `application_id`, `user_version`, reparse points, carpetas sincronizadas | implementado; `eco doctor` lo reporta | `evidencia/ARN-1080` |
| N procesos sobre la misma base: WAL, `busy_timeout`, `BEGIN IMMEDIATE`, reintento con backoff | implementado y medido | `evidencia/ARN-1080` c8 |
| Capas Z1, W1, W2 | planificado; fuera del puerto hasta que exista el Compositor | ADR-0031 |
| Tipos de Z3 y forma de Y por kit | planificado | ADR-0002 p3 |
| Decaimiento de Y; retención y techo de R | planificado | ADR-0015 abiertos |
| Congelar Z3 huérfano al desinstalar un kit | planificado | ADR-0014 p7 |
| Recuperación híbrida con cercanía temporal | planificado; hoy `bm25` y luego instante | ADR-0002 p5 |
| Paginación por cursor; cola durable de escrituras | planificado | ADR-0031, `planning/10` |
| Migración de esquema entre versiones | esquema v1 con `migrate`; el contrato entre versiones es planificado | ADR-0014 abiertos |

## Cómo se integra

| Puerta | Arranque | Quién entra | Perfil | Operaciones |
|---|---|---|---|---|
| MCP por stdio | `eco mcp` | el modelo | agent | `eco_read`, `eco_search`, `eco_append` (Z3, Y, X), `eco_probe` |
| HTTP en loopback | `eco serve` | hooks, scripts | human | `/v1/read`, `/v1/search`, `/v1/get`, `/v1/append`, `/v1/promote`, `/v1/probe` |
| CLI | `eco <verbo>` | el humano | human | `read`, `search`, `get`, `append`, `promote`, `probe`, `doctor` |
| Librería Go | `store.Open` | el runtime de ARN | el que fije `Config.Profile` | todo el puerto, `Promote`, y R, V, Manifest con `ProfileRuntime` |

Todas las puertas piden las dos bases: `--user-db` y `--project-db`. Ningún
mensaje de error lleva la ruta de una base; los errores salen como nombre
de sentinel (`eco: not found`, `eco: unavailable`, `eco: forbidden`,
`eco: invalid entry`, `eco: axis not in scope`, `eco: invalid scope`).

### MCP por stdio

Un proceso por cliente. Expone cuatro herramientas; `Z2` se lee y se busca
pero no se anexa, y `Promote` no existe en `tools/list`. Las respuestas se
recortan a 32 KiB por entradas enteras, con `truncated` y `omitted`.

```json
{
  "mcpServers": {
    "eco": {
      "command": "eco",
      "args": ["mcp", "--user-db", "<user.db>", "--project-db", "<project.db>"]
    }
  }
}
```

```
claude mcp add eco -- eco mcp --user-db <user.db> --project-db <project.db>
```

### HTTP en loopback

`eco serve` escucha en `127.0.0.1:0` salvo `--addr`, genera un token de 32
bytes y un nonce por proceso, y escribe un archivo de puerto solo legible
por el usuario actual (DACL con un único SID en Windows, `0600` en el
resto). Muere con el proceso que lo lanzó (`--parent-pid`) y borra el
archivo al salir.

```
eco serve --user-db <user.db> --project-db <project.db> --parent-pid <pid> --port-file <eco.port>
```

```json
{"pid":17168,"port":52104,"nonce":"<hex>","token":"<hex>"}
```

Las seis rutas exigen `Authorization: Bearer <token>`, incluidas las
lecturas. Antes del token se comprueban, en este orden: `Host` igual al del
listener (421), ausencia de `Origin` (403), bearer (401), `Content-Type:
application/json` exacto en los `POST` (415) y cuerpo acotado (413).

```
POST http://127.0.0.1:52104/v1/read
Authorization: Bearer <token>
Content-Type: application/json

{"scope":"project","axis":"Z3","limit":10}
```

```json
{"entries":[{"ID":"<id>","Axis":"Z3","Scope":"project","At":"2026-10-07T12:00:00Z","Body":"...","Attrs":{"origin":"mcp"}}],"truncated":false,"omitted":0}
```

`eco probe --port-file <eco.port>` lee el archivo, llama a `/v1/probe` y
compara el nonce: otro proceso en el mismo puerto se detecta.

### CLI

JSON por `stdout`, error tipado por `stderr`, código de salida fijo: 2 uso,
3 unavailable, 4 not found, 5 forbidden, 6 invalid.

```
eco append --user-db <user.db> --project-db <project.db> --axis Z3 --body "texto" --attr tool=verifier
eco search --user-db <user.db> --project-db <project.db> --axis Z3 --query "runtime" --limit 5
eco promote --user-db <user.db> --project-db <project.db> --ids <id,id>
eco doctor --user-db <user.db> --project-db <project.db>
```

`append` sin `--id` usa `port.DefaultID`. `promote` lee el lote de `Z3` por
id y lo escribe en `Z2` con id `promoted:<id>`; un id ausente hace fallar
el lote entero sin resultado parcial.

### Librería Go

```
go get github.com/JaviCss/eco@v0.1.0
```

El puerto neutral al transporte vive en `github.com/JaviCss/eco/port`
(`Port`, `Entry`, `Fake`, `Conformance`, `Merge`, `DefaultID`); el centro,
en `github.com/JaviCss/eco/store` (`Open`, `Config`, `Profile`, `Promote`).
`port/` y `store/` no importan `net/http` ni el SDK de MCP.

```go
s, err := store.Open(store.Config{
	UserDB:    userDB,
	ProjectDB: projectDB,
	Origin:    "runtime",
	Profile:   store.ProfileRuntime,
})
if err != nil {
	return err
}
defer s.Close()
stored, err := s.Append(ctx, port.ScopeUser, port.AxisR, port.Entry{ID: key, Body: body})
```

Para probar un adaptador propio contra el contrato:

```go
port.Conformance(t,
	func() port.Port { return port.NewFake() },
	func() port.Outage { return port.NewFake() },
)
```

`arn-v2/base` consume exactamente esto: `base/go.mod` requiere
`github.com/JaviCss/eco v0.1.0` sin `replace`, `base/internal/eco` reexporta
los tipos de `port`, y el módulo de telemetría recibe el `Port` por
parámetro y anexa en `User/R`.

## Instalación

```
npm i @arn-harness/eco
eco version
```

`@arn-harness/eco` es un launcher de Node (`>= 20`) sin dependencias, sin
`postinstall` y sin red: resuelve `@arn-harness/eco-<platform>-<arch>/bin/eco`,
lo ejecuta heredando stdio y sale con su mismo código. El binario llega
por `optionalDependencies` con versión exacta: `eco-win32-x64`,
`eco-linux-x64`, `eco-darwin-arm64`. Si falta el paquete de la plataforma
(`--omit=optional`, os/cpu distinto), el launcher sale con código 2 y lo dice.

Como módulo Go, `go install github.com/JaviCss/eco/cmd/eco@v0.1.0`. La
versión viaja en el binario (`debug.ReadBuildInfo`) y `eco version` la
imprime igual por las dos rutas. Los binarios se compilan con
`CGO_ENABLED=0`, `-trimpath` y `-ldflags "-s -w"`; `tools/release` deja
`dist/SHA256SUMS`. La suite corre en Windows; linux y darwin se compilan
cruzado (`evidencia/ARN-1140`).

## Dónde viven los datos

El binario no fija rutas: cada verbo recibe `--user-db` y `--project-db`, y
`store.Open` crea lo que falte salvo en modo solo lectura (`doctor`), que
nunca crea una base. El diseño ubica la base del proyecto en el `.arn/` del
proyecto, compartida por todos sus worktrees (ADR-0014 p5), y la del usuario
en el estado global de la máquina, fuera de lo que pisa un release
(ADR-0014, ADR-0021). Resolver esas rutas y pasarlas es tarea del runtime
o del harness que lanza a Eco.

`Open` rechaza reparse points en la ruta, rechaza que las dos bases sean el
mismo archivo, y no toca el disco si una de las dos tiene un `application_id`
ajeno o un `user_version` más nuevo que el binario. `eco doctor` avisa si una
base está en OneDrive, Dropbox, Google Drive o una ruta de red, donde WAL no
funciona.

## Decisiones

Viven en `../arn-v2/documentacion/adr/`.

- **ADR-0002**: la memoria es un producto aparte con tres puertas; enmendado
  el 2026-10-07: se escribe en Go y se publica en npm y como módulo Go.
- **ADR-0014**: el genoma queda en git; dos bases (usuario y proyecto) sin
  `ATTACH`; promoción Z3→Z2 en dos pasos a cargo del runtime; Z2 solo lectura
  por MCP; perfil por puerta.
- **ADR-0015**: la telemetría es el eje R, en la base del usuario, solo-anexar
  y escrito solo por el runtime.
- **ADR-0017**: la memoria de validación es el eje V, escrito solo por Arconte
  con firma humana.
- **ADR-0018**: Eco no es un módulo del base; es una dependencia con versión.
- **ADR-0031** (propuesto): el puerto lo tiene el cliente, la fusión de
  alcances vive en el cliente, `ErrNotFound` es "el dato no está" y
  `ErrUnavailable` es "Eco no está".

## Linaje

Eco reescribe el diseño de la libreta (`../libreta`, TypeScript, SQLite con
FTS5 y verbos por MCP): sus ejes, verbos y el modelo de mempalace entran
rediseñados; su código no. La forma de binario Go con SQLite adentro y
tres puertas sigue el precedente de engram, la memoria de Gentleman-Programming.

## Licencia

MIT. Ver `LICENSE`.
