# Omni Remote Control

Sistema centralizado para el control, automatización programática y recolección de telemetría en dispositivos cliente multiplataforma (Windows, Linux, Android, entre otros).

## 🚀 Descripción General

**Omni Remote Control** no solo permite operar y ejecutar acciones de forma remota, sino también monitorear en tiempo real el estado y la telemetría de cada cliente a través de una arquitectura cliente-servidor:

- **Clientes Multiplataforma**: Agentes ligeros instalados en dispositivos cliente (Windows, Linux, Android, etc.) que mantienen conexión persistente con el servidor central, reportando telemetría continua (métricas del sistema, consumo de recursos, estado de red y eventos) y ejecutando tareas asignadas.
- **Servidor Central**: Orquestador principal encargado de gestionar conexiones, autenticación, procesamiento de telemetría y despacho de comandos o tareas.
- **API REST / WebSockets**: Interfaz para consultar estado en tiempo real, métricas históricas y despachar acciones de forma programática.
- **Servidor MCP (Model Context Protocol)**: Exposición de herramientas y recursos bajo el estándar MCP para permitir que modelos de IA (LLMs) y agentes inteligentes consulten la telemetría y operen los dispositivos de manera autónoma.

## 📐 Arquitectura

```
  [ Dispositivos Cliente ]
   (Windows / Linux / Android)
              │
              │ WebSockets / gRPC / HTTPS
              ▼
    ┌──────────────────┐
    │  Servidor Omni   │
    └────┬────────┬────┘
         │        │
         ▼        ▼
     [ API ]   [ MCP Server ]
                  ▲
                  │
          [ Agentes / IA ]
```

## 🛠️ Estado del Proyecto

En etapa inicial de diseño y desarrollo.
