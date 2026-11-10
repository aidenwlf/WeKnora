# Univer collaboration server

Self-hosted real-time collaboration for WeKnora's collaborative knowledge bases, assembled from the
[Univer Server SDK](https://docs.univer.ai/server/collaboration/overview). The process owns
operational transform (OT), revisions and the WebSocket sessions; WeKnora owns users, permissions,
storage and deployment.

A knowledge base of type `univer` holds entries of four kinds:

| Kind | Product | Editor |
| --- | --- | --- |
| `sheet` | Spreadsheet | `@univerjs/preset-sheets-core` |
| `doc` | Document | `@univerjs/preset-docs-core` |
| `base` | Multi-dimensional table | `@univerjs-pro/bases` (plugin mode) |
| `slide` | Slide deck | `@univerjs-pro/slides` (plugin mode) |

WeKnora reaches this process in three ways:

| Traffic | Path | Authenticated by |
| --- | --- | --- |
| Browser collaboration client (HTTP + WebSocket) | `/api/v1/univer/universer-api/*` on the WeKnora origin | WeKnora mints a path-scoped cookie; the Go proxy verifies it and adds `X-WeKnora-User` / `X-WeKnora-Secret` |
| Agent reads and edits | `/internal/units/...` | `X-WeKnora-Secret` |
| ACL questions | WeKnora `POST /api/internal/univer/acl` | `X-WeKnora-Secret` |

## Run

```bash
npm install
UNIVER_SHARED_SECRET=... node main.js
```

| Variable | Purpose |
| --- | --- |
| `UNIVER_PORT` | Listen port (default `3010`). |
| `UNIVER_HOST` | Listen host (default `127.0.0.1`; containers use `0.0.0.0`). |
| `UNIVER_SHARED_SECRET` | Required in production. Signs the browser cookie and guards the internal API. Must equal the value in WeKnora's environment. |
| `UNIVER_DB` | `memory` (default, lost on restart) or `postgres`. |
| `PGHOST` / `PGPORT` / `PGUSER` / `PGPASSWORD` / `PGDATABASE` | PostgreSQL connection; takes precedence over nothing — set `DATABASE_URL` instead if the password is URL-safe. Tables live in a separate `collaboration` schema. |
| `WEKNORA_INTERNAL_URL` | Where ACL checks are sent (default `http://127.0.0.1:8080`). |
| `UNIVER_LICENSE` | Optional Univer Pro key; without it Slides/Bases show a watermark and keep working. |

## Data model

Unit content never touches WeKnora's ordinary tables. The collaboration store holds units,
snapshots, changesets and sheet blocks; WeKnora keeps `univer_units` (id, workspace, knowledge base,
name, kind, unit id, creator) so it can list entries, resolve them by name, and answer ACL
questions.

## Programmatic API

Used by WeKnora's agent tools. Every call is attributed to `X-WeKnora-User`.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/internal/units` | Create a blank unit; `kind` selects the product. |
| `GET` | `/internal/units/:unitID/meta` | Kind-specific structure (sheets, tables, slides, title). |
| `GET` | `/internal/units/:unitID/content?range=A1:C10` | Kind-specific text: CSV for sheets, body text for documents, JSON for tables and decks. |
| `PUT` | `/internal/units/:unitID/content` | Replace a cell range (spreadsheets). |
| `POST` | `/internal/units/:unitID/content` | Append text (documents). |

Writes go through a headless Univer runtime (`@univer-cli/univer-collaboration-runtime`), so they
produce the same mutations a browser produces and are merged by the same OT engine.
