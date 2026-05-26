Cambio 1: dejar de anunciar tools como `available_commands`
En `sendAvailableCommandsUpdate(...)` no debería usarse `ListAvailableTools()` directamente para poblar commands ACP.

Los commands ACP deberían ser una lista propia, algo como:
- `goal`
- `goal-status`
- `goal-cancel`
- `compact`
- `summarize`

### Cambio 2: crear un registro real de slash commands ACP
En vez de tener solo `parseSlashCommand(...)` hardcoded, debería haber una tabla/registro de comandos ACP con:
- nombre
- descripción
- uso
- handler

### Cambio 3: implementar compactación manual en ACP
Aprovechando la lógica ya existente del agente:
- `/compact`
- o `/summarize`

### Cambio 4: decidir si tools deben exponerse como tools, no como commands
Si el cliente ACP ya sabe renderizar tools/llamadas de herramienta, no hace falta venderlas como slash commands.
