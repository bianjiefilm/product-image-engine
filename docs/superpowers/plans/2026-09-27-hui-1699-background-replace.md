# HUI-1699 AI 背景替换实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在产品图工程里加上保真优先的背景替换报价、幂等任务和诚实结果，不把生产出图或计费写成已经通过。

**Architecture:** 纯领域判定在 `server/internal/bgreplace`，SQLite 保存逻辑任务，HTTP 在生成开关关闭或任务未知时停止。Web 面板用限定样式的 Button/Card/Badge 展示「计费待确认」。

**Tech Stack:** Go、SQLite、Next.js、Vitest

**Spec:** `docs/superpowers/specs/2026-09-27-hui-1699-background-replace-design.md`

## Global Constraints

- `production_generation_passed` 恒为 false
- `billing_passed` 恒为 false
- 没有报价金额时文案为「计费待确认」
- `FEATURE_BG_REPLACE` 默认关闭，关闭时路由 404
- 创意模式默认不可用
- 同指纹不第二次调用平台任务
- 质量 `pass` 在没有保真证据时拒绝

## 执行说明

任务共享同一套表、接口和面板，按顺序在同一工作区实现，并在结束后做整支复审。

- [x] 领域判定与失败测试
- [x] 存储、配置开关与 HTTP
- [x] 工程页面板与 BFF
