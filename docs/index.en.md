---
layout: home

hero:
  name: Courier
  text: Universal Game SDK
  tagline: A universal SDK for integrating common game services across engines and platforms —— providing Unity, Unreal, Cocos, Godot, and other engines with unified account, authentication, player, messaging, announcements, support, payments, real name, remote config, branding, and diagnostics capability interfaces
  actions:
    - theme: brand
      text: Contract Constitution
      link: /contract/
    - theme: alt
      text: Architecture
      link: /architecture
    - theme: alt
      text: Roadmap
      link: /roadmap

features:
  - title: Account & Session
    details: Registration/login (including third-party OAuth login, optional), token rotation, device binding; one account logs into all games
  - title: Player
    details: Account ↔ game character mapping and profile interfaces; character data belongs to each game
  - title: Messaging & Announcements
    details: Real-time push plus pull/subscribe for announcements, events, and maintenance notices; channel is swappable and can be disabled
  - title: Support
    details: Ticket submission/query, FAQ retrieval; the panel is an optional UI package
  - title: Real Name (optional, compliance)
    details: Provider hot-pluggable (degradation chain + circuit breaker); off by default, enabling it explicitly states the data scope
  - title: Remote Config & Branding
    details: Matched by game_id/env/platform/version/region; brand asset delivery, consumed by the UI package
  - title: Assistant
    details: FAQ retrieval Q&A (optional); answers come from verbatim snapshots without generation, and guides to human support when unable to answer
  - title: Diagnostics (optional)
    details: Crashes, OTel Trace, performance metrics, telemetry; all off by default, no-op when not configured
  - title: Payments (contract + sandbox channel)
    details: Order/Purchase/Receipt; built-in sandbox channel (teller) fully implemented across the chain, real channels ship with the deployment surface; four forms optional
---

[English](./index.en.md) | [中文](./index.md)
