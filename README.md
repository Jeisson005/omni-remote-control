# Omni Remote Control

Sistema centralizado para el control y automatización programática de dispositivos cliente multiplataforma (Windows, Linux, Android, entre otros).

## 🚀 Descripción General

**Omni Remote Control** permite gestionar y operar dispositivos de forma remota y programática a través de una arquitectura cliente-servidor:

- **Clientes Multiplataforma**: Agentes ligeros instalados en dispositivos cliente (Windows, Linux, Android, etc.) que mantienen conexión persistente con el servidor central.
- **Servidor Central**: Orquestador principal encargado de gestionar conexiones, autenticación, telemetría y ejecución de comandos o tareas.
- **API REST / WebSockets**: Interfaz para consultar estado y despachar acciones de forma programática.
- **Servidor MCP (Model Context Protocol)**: Exposición de herramientas y recursos bajo el estándar MCP para permitir la interacción directa e inteligente desde agentes y modelos de IA (LLMs).

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
