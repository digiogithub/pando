---
title: Plan de Implementación de Interfaz Web y Desktop para Pando
description: Análisis y estrategia de implementación para Pando inspirado en la arquitectura Web (SolidJS) + Desktop de OpenCode, adaptado a las funcionalidades de Pando como MCP, Remembrances y Mesnada.
fases: 4
---

# Pando Desktop / Web Interface Implementation Plan

After analyzing OpenCode `desktop/` and `app/`, we identified that it works as a wrapper around the CLI. We will implement an equivalent topology in Pando:

## 1. Recommended Topology

- **Engine Server:** We will use the `cmd/acp.go` infrastructure and fully implement the TCP/HTTP REST/SSE server in Go, similar to the `--serve` command in OpenCode (*See Phase 1*).
- **Web Frontend:** We will create a decoupled client (Single Page Application). Recommended in SolidJS just like OpenCode (for its speed, minimalism, and native similarity with React), packageable and initially hosted by Vite (*See Phase 2*).
- **Desktop Host:** For packaging, instead of forcing the use of a Sidecar binary through Tauri (as in OpenCode, combining Rust and Go sidecar), we recommend **Wails**, which allows packaging pure Go (where Pando runs) with a SolidJS frontend, achieving a more compact and native single binary (*See Phase 3*). If exact architectural compatibility with OpenCode is preferred, **Tauri v2** + a `pando` sidecar can be used.
- **Competitive Advantages:** Interactive visual integration with the "Mesnada" system (Subagent Generator) and the "Code of Remembrances" (Visual Exploration) (*See Phase 4*).

## Phases and Additional Details stored (Remembrances Facts):

- [Phase 1: Engine HTTP API](desktop-web-ui-phase-1.md)
- [Phase 2: Frontend SolidJS UI](desktop-web-ui-phase-2.md)
- [Phase 3: Native/Desktop Wrapper](desktop-web-ui-phase-3.md)
- [Phase 4: Advanced Pando Features](desktop-web-ui-phase-4.md)

Suggested next step: Develop or unify `pando serve` or finish the ACP server over HTTP transport from Phase 1 in `acp.go`.
