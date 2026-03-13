---
title: Plan de Implementación de Interfaz Web y Desktop para Pando
description: Análisis y estrategia de implementación para Pando inspirado en la arquitectura Web (SolidJS) + Desktop de OpenCode, adaptado a las funcionalidades de Pando como MCP, Remembrances y Mesnada.
fases: 4
---

# Plan de Implementación de la Interfaz Pando Desktop / Web

Tras el análisis de OpenCode `desktop/` y `app/`, identificamos que este funciona como un envoltorio de la CLI. Implementaremos una topología equivalente en Pando:

## 1. Topología Recomendada

- **Engine Server:** Utilizaremos la infraestructura de `cmd/acp.go` e implementaremos por completo el servidor TCP/HTTP REST/SSE en Go, similar al comando `--serve` en OpenCode (*Ver Fase 1*).
- **Web Frontend:** Se optará por crear un cliente desacoplado (Single Page Application). Recomendado en SolidJS tal cual lo hace OpenCode (por su velocidad, minimalismo y similitud nativa con React), empaquetable y hosteado inicialmente por Vite (*Ver Fase 2*).
- **Desktop Host:** Para empaquetar, en vez de obligar al uso de un binario Sidecar a través de Tauri (como en OpenCode, combinando Rust y Go sidecar), recomendamos **Wails**, que permite empaquetar Go puro (donde corre Pando) con frontend SolidJS, logrando un binario único más compacto y natural (*Ver Fase 3*). Si se prefiere compatibilidad exacta arquitectónica con OpenCode, se puede usar **Tauri v2** + un sidecar `pando`.
- **Ventajas Competitivas:** Integración visual interactiva al sistema "Mesnada" (Engendrador de subagentes) y la "Code of Remembrances" (Exploración Visual) (*Ver Fase 4*).

## Fases y Detalles Adicionales almacenados (Facts de Remembrances):

- [Fase 1: Engine HTTP API](desktop-web-ui-phase-1.md)
- [Fase 2: Frontend SolidJS UI](desktop-web-ui-phase-2.md)
- [Fase 3: Wrapper Native/Desktop](desktop-web-ui-phase-3.md)
- [Fase 4: Funcionalidades Avanzadas Pando](desktop-web-ui-phase-4.md)

Paso sugerido a continuación: Desarrollar o unificar `pando serve` o terminar el servidor ACP sobre transporte HTTP de la Fase 1 en `acp.go`.
