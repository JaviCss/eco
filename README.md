# Eco

La memoria de ARN: un producto aparte, con tres puertas, escrito en Go.

- **Qué es**: el centro de memoria de los agentes (capas, promoción
  supervisada, composición) más su SQLite, y encima tres adaptadores sobre
  el mismo puerto: MCP por stdio (la puerta del modelo), HTTP local (la de
  los programas) y CLI (la del humano). Un runtime en Go puede además
  importarlo como librería.
- **De dónde nace**: del diseño de la libreta (`../libreta`, Node), no de su
  código. Los ejes, los verbos y el modelo de mempalace se reescriben acá.
- **Cómo se publica**: dos veces desde el mismo código. En npm, con el
  binario compilado por plataforma adentro del paquete, para que cualquier
  harness lo instale con `npm i`. Y como módulo Go, taggeando este repo,
  para que el runtime de ARN v2 lo importe.
- **Decisiones que lo sostienen**: ADR-0002 (producto aparte, tres puertas;
  enmendado el 2026-10-07: Go), ADR-0014 (dos bases, sin ATTACH, promoción
  Z3→Z2 por el runtime), ADR-0018 (dependencia con versión del base). Viven
  en `../arn-v2/documentacion/adr/`.
- **Precedente**: engram, de Gentleman-Programming — un binario Go con un
  SQLite adentro y las tres puertas encima.

Hoy es un repo vacío con su `go.mod`. La primera card (`arn-v2`, módulo 4
del plan del bloque 5) entrega la interfaz Go neutral al transporte y un
fake en memoria.
