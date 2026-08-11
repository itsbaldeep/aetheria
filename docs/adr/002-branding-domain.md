# ADR-002: Branding & Domain Config

Status: accepted (2026-08-11)

## Context
The brief says "Aetheria" is a working title and the game will eventually move
from deployden.tech subdomains to a dedicated domain.

## Decision
All user-facing strings (game title, portal title, news footer) come from
`shared/branding.json`. All service URLs come from `deploy/env`
(`AETHERIA_PUBLIC_BASE`, `AETHERIA_WS_URL`, etc.). Nothing hardcodes "Aetheria"
or "deployden.tech" in source. Changing the name/domain = edit config + rebuild
client, no code changes.

MVP domains (human approved 2026-08-11):
- portal:  `https://aetheria.apps.deployden.tech`
- api:     `https://api.aetheria.apps.deployden.tech`
- admin:   `https://admin.aetheria.apps.deployden.tech`
- play:    `wss://play.aetheria.apps.deployden.tech/ws`

## Consequences
- Migration to a custom domain later is a config-only change (ADR-001 keep).
- All 4 subdomains fall under the existing `*.apps.deployden.tech` wildcard, so
  DNS + on-demand TLS work out of the box.
