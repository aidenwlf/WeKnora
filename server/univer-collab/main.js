// WeKnora Univer Collaboration Server.
//
// Hosts the Univer Server SDK collaboration endpoint (HTTP + WebSocket) and a
// small internal API that lets the WeKnora Go backend read and edit units
// through a headless Univer runtime, so edits flow through the same OT pipeline
// as browser edits.
//
// Units belong to a knowledge base of type "univer" and come in four kinds:
// sheet (spreadsheet), doc (document), base (multi-dimensional table) and
// slide (slide deck).
import { createHmac, randomUUID, timingSafeEqual } from "node:crypto";
import { createServer } from "node:http";
import express from "express";
import pg from "pg";
import { MemoryDatabaseAdapter } from "@univerjs-pro/collaboration-database-memory";
import { PostgresDatabaseAdapter } from "@univerjs-pro/collaboration-database-postgres";
import { UniverCollabEndpoint } from "@univerjs-pro/collaboration-endpoint";
import { CollabError, UniverCollabService } from "@univerjs-pro/collaboration-service";
import { createNodeTransport } from "@univerjs-pro/collaboration-transport-node";
import { ErrorCode, UniverType } from "@univerjs/protocol";
import {
  LocaleType,
  getBasesEmptySnapshot,
  getDocsEmptySnapshot,
} from "@univerjs/core";
import { getSlidesEmptySnapshot } from "@univerjs-pro/slides";
import { createStandardHeadlessUniverFactory } from "@univer-cli/headless-univer";
import { createUniverCollaborationRuntimeFactory } from "@univer-cli/univer-collaboration-runtime";

const PORT = Number(process.env.UNIVER_PORT || 3010);
const HOST = process.env.UNIVER_HOST || "127.0.0.1";
const SHARED_SECRET = process.env.UNIVER_SHARED_SECRET || "";
const DB_MODE = (process.env.UNIVER_DB || "memory").toLowerCase();
const DATABASE_URL = process.env.DATABASE_URL || "";
const COOKIE_NAME = process.env.UNIVER_COOKIE_NAME || "weknora_univer";

// Protocol UniverType and core UniverInstanceType agree on these values.
const KIND_TYPES = {
  doc: UniverType.UNIVER_DOC,
  sheet: UniverType.UNIVER_SHEET,
  slide: UniverType.UNIVER_SLIDE,
  base: UniverType.UNIVER_BASE,
};
const TYPE_KINDS = Object.fromEntries(
  Object.entries(KIND_TYPES).map(([kind, type]) => [String(type), kind]),
);
const DEFAULT_READ_RANGE = "A1:Z200";

if (!SHARED_SECRET) {
  console.warn("[univer] UNIVER_SHARED_SECRET is empty: internal API is unauthenticated");
}

// ---------------------------------------------------------------------------
// Identity: the browser carries a cookie minted by the WeKnora Go backend; the
// Go backend itself calls the internal API with a shared-secret header.
// ---------------------------------------------------------------------------

// The signature covers the raw JSON bytes, not the base64url text: the Go
// backend mints these cookies and must agree byte for byte.
function sign(payload) {
  const raw = Buffer.from(JSON.stringify(payload));
  const mac = createHmac("sha256", SHARED_SECRET).update(raw).digest("base64url");
  return `${raw.toString("base64url")}.${mac}`;
}

function verifyToken(token) {
  if (!token || !token.includes(".")) return null;
  const [body, mac] = token.split(".");
  const raw = Buffer.from(body, "base64url");
  const expected = createHmac("sha256", SHARED_SECRET).update(raw).digest("base64url");
  const a = Buffer.from(mac);
  const b = Buffer.from(expected);
  if (a.length !== b.length || !timingSafeEqual(a, b)) return null;
  const payload = JSON.parse(raw.toString("utf8"));
  if (!payload.exp || payload.exp * 1000 < Date.now()) return null;
  return payload;
}

function parseCookies(header) {
  const out = {};
  if (!header) return out;
  for (const part of header.split(";")) {
    const idx = part.indexOf("=");
    if (idx < 0) continue;
    out[part.slice(0, idx).trim()] = decodeURIComponent(part.slice(idx + 1).trim());
  }
  return out;
}

function identityFromRequest(req) {
  const secret = req.headers["x-weknora-secret"];
  if (SHARED_SECRET && typeof secret === "string" && secret === SHARED_SECRET) {
    const userID = String(req.headers["x-weknora-user"] || "weknora-service");
    return { userID, internal: true };
  }
  // Local development without WeKnora: trust a plain user header.
  if (!SHARED_SECRET && typeof req.headers["x-weknora-user"] === "string") {
    return { userID: req.headers["x-weknora-user"], internal: true };
  }
  const cookies = parseCookies(req.headers.cookie);
  const payload = verifyToken(cookies[COOKIE_NAME]);
  if (payload) return { userID: payload.uid, internal: false };
  return null;
}

// ---------------------------------------------------------------------------
// Database + service assembly
// ---------------------------------------------------------------------------

let database;
async function createDatabase() {
  if (DB_MODE === "postgres") {
    // A DATABASE_URL is used when present; otherwise node-postgres reads the
    // discrete PG* variables, which avoids URL-encoding passwords that contain
    // reserved characters.
    const pool = DATABASE_URL ? new pg.Pool({ connectionString: DATABASE_URL }) : new pg.Pool();
    database = await PostgresDatabaseAdapter.create({ pool });
    database.__pool = pool;
  } else {
    database = new MemoryDatabaseAdapter();
  }
  return database;
}

await createDatabase();

const service = new UniverCollabService({ dbAdapter: database });
const endpoint = new UniverCollabEndpoint(service);
const transport = createNodeTransport();

const headlessFactory = createStandardHeadlessUniverFactory({
  license: process.env.UNIVER_LICENSE || "",
});

// ---------------------------------------------------------------------------
// WeKnora ACL bridge
// ---------------------------------------------------------------------------

const WEKNORA_INTERNAL = process.env.WEKNORA_INTERNAL_URL || "http://127.0.0.1:8080";

async function weknoraAllowed(userID, unitID, action) {
  if (!SHARED_SECRET) return true;
  try {
    const resp = await fetch(`${WEKNORA_INTERNAL}/api/internal/univer/acl`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        "x-weknora-secret": SHARED_SECRET,
      },
      body: JSON.stringify({ user_id: userID, unit_id: unitID, action }),
    });
    if (!resp.ok) return false;
    const body = await resp.json();
    return body.allowed === true;
  } catch (err) {
    console.error("[univer] ACL bridge failed:", err.message);
    return false;
  }
}

// ---------------------------------------------------------------------------
// Collaboration transport
// ---------------------------------------------------------------------------

transport.use(async (ctx, next) => {
  const identity = identityFromRequest(ctx.incomingMessage);
  if (!identity) {
    ctx.response.statusCode = 401;
    ctx.response.end("Unauthorized");
    return;
  }
  ctx.userID = identity.userID;
  await next();
});

service.use("readUnitData", async (ctx, next) => {
  // Calls made by this process on behalf of the Go backend were already
  // authorized there; only browser traffic goes through the ACL bridge.
  if (ctx.customData?.internal === true) return next();
  const allowed = await weknoraAllowed(ctx.userID, ctx.request.unitID, "read");
  if (!allowed) throw new CollabError("PERMISSION_DENIED", "Unit is not readable");
  await next();
});

service.use("submitChangeset", async (ctx, next) => {
  if (ctx.customData?.internal === true) return next();
  const allowed = await weknoraAllowed(ctx.userID, ctx.request.changeset.unitID, "write");
  if (!allowed) throw new CollabError("PERMISSION_DENIED", "Unit is read-only");
  await next();
});

endpoint.use("connect", async (ctx, next) => {
  ctx.member.name = ctx.session.userID;
  await next();
});

endpoint.use("joinUnit", async (ctx, next) => {
  const allowed = await weknoraAllowed(ctx.session.userID, ctx.unitID, "read");
  if (!allowed) throw new CollabError("PERMISSION_DENIED", "Unit is not readable");
  await next();
});

transport.register(endpoint);

// ---------------------------------------------------------------------------
// Headless runtime: read/write through the same OT path browsers use
// ---------------------------------------------------------------------------

async function unitType(unitID, userID) {
  const rec = await service.dbAdapter.getUnit(
    { userID, customData: { internal: true } },
    unitID,
  );
  if (!rec) throw new Error(`UNIT_NOT_FOUND: ${unitID}`);
  return rec.type;
}

function directBackend(userID) {
  const memberID = `agent-${randomUUID().slice(0, 8)}`;
  return {
    async open({ unitId }) {
      const ctx = { userID, customData: { memberID, internal: true } };
      let type = KIND_TYPES.sheet;
      const headRevision = async () => {
        const rec = await service.dbAdapter.getUnit(ctx, unitId);
        if (!rec) throw new Error(`UNIT_NOT_FOUND: ${unitId}`);
        type = rec.type;
        return rec.headRevision;
      };
      return {
        format: "snapshot",
        getConnectionState: () => "online",
        async getUnitOnRev(revision) {
          const rev = revision && revision > 0 ? revision : await headRevision();
          const load = await service.getUnitLoadDataWithBlocks(
            { unitID: unitId, type, revision: rev },
            ctx,
          );
          // Return the stored base snapshot plus the changesets that follow it.
          // Materialising here would emit sheet blocks this adapter never stored.
          return { error: undefined, snapshot: load.snapshot, changesets: load.changesets };
        },
        async getSheetBlock(blockID) {
          const r = await service.getSheetBlock({ unitID: unitId, type, blockID }, ctx);
          return { error: undefined, block: r.block };
        },
        async fetchChangesets(from, to) {
          const head = await headRevision();
          const upper = to ?? head;
          if (upper <= from) return { error: undefined, changesets: [], latestRevision: head };
          const r = await service.getChangesets(
            { unitID: unitId, type, from, to: upper },
            ctx,
          );
          return { error: undefined, changesets: r.changesets, latestRevision: head };
        },
        async submitChangeset(draft) {
          try {
            const r = await service.submitChangeset(
              {
                changeset: {
                  unitID: unitId,
                  type,
                  baseRev: draft.baseRev,
                  revision: draft.baseRev + 1,
                  userID,
                  memberID,
                  mutations: draft.mutations,
                  sid: draft.sid,
                  reqId: draft.reqId,
                  additionalFields: draft.additionalFields,
                },
              },
              // Mark the call as already authorized by the Go backend so the
              // ACL middleware does not bounce it as browser traffic would be.
              { userID, memberID, customData: { internal: true } },
            );
            if (r.status === "committed" || r.status === "already-committed") {
              return {
                status: "confirmed",
                precedingChangesets: [],
                acceptedChangeset: r.changeset,
              };
            }
            if (r.status === "retry") {
              return { status: "retry", code: r.error?.code, message: r.error?.message };
            }
            return {
              status: "rejected",
              reason: "invalid-changeset",
              message: r.error?.message ?? "changeset rejected",
            };
          } catch (err) {
            if (err?.code === "PERMISSION_DENIED") {
              return { status: "rejected", reason: "permission-denied", message: err.message };
            }
            if (err?.code === "REVISION_MISMATCH" || err?.code === "OT_CONFLICT") {
              return { status: "retry", message: err.message };
            }
            return {
              status: "rejected",
              reason: "invalid-changeset",
              message: err?.message ?? String(err),
            };
          }
        },
        async close() {},
      };
    },
  };
}

function runtimeFactory(userID) {
  return createUniverCollaborationRuntimeFactory({
    backend: directBackend(userID),
    createUniver: headlessFactory,
  });
}

async function runWithRuntime(userID, unitID, fn, { commit = true } = {}) {
  const type = await unitType(unitID, userID);
  const factory = runtimeFactory(userID);
  const runtime = await factory.load(unitID, type);
  try {
    const pull = await runtime.pull();
    if (pull.status === "conflict") {
      throw new Error(`pull conflict: ${pull.conflict?.message ?? "unknown"}`);
    }
    const result = await fn(runtime);
    if (!commit) return result;
    let committed = await runtime.commit();
    if (committed.status === "pull-required") {
      const again = await runtime.pull();
      if (again.status === "conflict") {
        throw new Error(`pull conflict: ${again.conflict?.message ?? "unknown"}`);
      }
      committed = await runtime.commit();
    }
    if (
      committed.status === "conflict" ||
      committed.status === "retry" ||
      committed.status === "unknown"
    ) {
      const detail = JSON.stringify(committed);
      console.error("[univer] commit failed:", detail);
      throw new Error(`commit ${committed.status}: ${detail}`);
    }
    return result;
  } finally {
    await runtime.close();
  }
}

// ---------------------------------------------------------------------------
// Facade snippets. The headless runtime exposes `univerAPI`; nothing else is
// bound in the evaluation context. Every snippet ends with `return`.
// ---------------------------------------------------------------------------

function readCode(kind, range) {
  switch (kind) {
    case "sheet": {
      const a1 = String(range || "").trim() || DEFAULT_READ_RANGE;
      return `return univerAPI.getActiveWorkbook().getActiveSheet().getRange(${JSON.stringify(a1)}).getValues();`;
    }
    case "doc":
      return `const d = univerAPI.getActiveDocument(); return d ? { title: d.getName(), stream: d.getBody().dataStream } : null;`;
    case "base":
      return `const b = univerAPI.getActiveBase(); if (!b) return null; return b.getTables().map((t) => ({ id: t.getId(), name: t.getName(), fields: t.getFields().map((f) => ({ id: f.getId(), name: f.getName(), type: f.getType() })), records: t.getRecords().map((r) => r.getValues()) }));`;
    case "slide":
      return `const p = univerAPI.getActivePresentation(); if (!p) return null; return p.getSlides().map((s) => ({ id: s.getId(), name: s.getName() }));`;
    default:
      throw new Error(`unsupported kind: ${kind}`);
  }
}

function metaCode(kind) {
  switch (kind) {
    case "sheet":
      return "return univerAPI.getActiveWorkbook().getSheets().map((s) => ({ id: s.getSheetId(), name: s.getSheetName(), row_count: s.getMaxRows(), column_count: s.getMaxColumns() }));";
    case "doc":
      return `const d = univerAPI.getActiveDocument(); return d ? [{ id: d.getId(), name: d.getName() }] : [];`;
    case "base":
      return `const b = univerAPI.getActiveBase(); if (!b) return []; return b.getTables().map((t) => ({ id: t.getId(), name: t.getName(), fields: t.getFields().length, records: t.getRecords().length }));`;
    case "slide":
      return `const p = univerAPI.getActivePresentation(); if (!p) return []; return p.getSlides().map((s) => ({ id: s.getId(), name: s.getName() }));`;
    default:
      throw new Error(`unsupported kind: ${kind}`);
  }
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

function renderCsv(values) {
  return (values || [])
    .map((row) => row.map((cell) => renderCell(cell)).join(","))
    .join("\n");
}

function renderCell(cell) {
  if (cell === null || cell === undefined) return "";
  if (typeof cell === "string") {
    return /[",\n]/.test(cell) ? `"${cell.replace(/"/g, '""')}"` : cell;
  }
  if (typeof cell === "number") {
    return Number.isInteger(cell) ? String(cell) : String(cell);
  }
  if (typeof cell === "boolean") return cell ? "TRUE" : "FALSE";
  return JSON.stringify(cell);
}

// Documents keep text in a data stream where \r separates paragraphs.
function streamToText(stream) {
  return String(stream || "")
    .replace(/\u0000/g, "")
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .replace(/\n+$/g, "");
}

// ---------------------------------------------------------------------------
// A1 helpers
// ---------------------------------------------------------------------------

const cellRef = /^[A-Za-z]{1,3}[0-9]+$/;

function normalizeRange(raw) {
  const trimmed = String(raw || "").trim().replace(/\$/g, "");
  if (!trimmed) return DEFAULT_READ_RANGE;
  const parts = trimmed.split(":");
  if (parts.length > 2 || !parts.every((p) => cellRef.test(p))) return DEFAULT_READ_RANGE;
  return trimmed.toUpperCase();
}

// ---------------------------------------------------------------------------
// HTTP: collaboration routes + authz + internal API
// ---------------------------------------------------------------------------

const app = express();
app.use(express.json({ limit: "8mb" }));

app.get("/healthz", (_req, res) => res.json({ ok: true, db: DB_MODE }));

// The collaboration client asks for object-action permissions in bulk.
app.post("/universer-api/authz/-/object/-/batch_allowed", async (req, res) => {
  const identity = identityFromRequest(req);
  if (!identity) {
    return res.status(401).json({ error: { code: ErrorCode.UNAUTHENTICATED, message: "" } });
  }
  const requests = req.body?.requests ?? [];
  const objectActions = [];
  for (const item of requests) {
    const allowed = await weknoraAllowed(identity.userID, item.unitID, "write");
    objectActions.push({
      unitID: item.unitID,
      objectID: item.objectID,
      actions: (item.actions ?? []).map((action) => ({ action, allowed })),
    });
  }
  res.json({ error: { code: ErrorCode.OK, message: "" }, objectActions });
});

function requireInternal(req, res) {
  const identity = identityFromRequest(req);
  if (!identity || !identity.internal) {
    res.status(401).json({ error: "unauthorized" });
    return null;
  }
  return identity;
}

function initialData(kind, unitID, name) {
  switch (kind) {
    case "doc":
      return { ...getDocsEmptySnapshot(unitID), rev: 1, title: name };
    case "slide":
      return { ...getSlidesEmptySnapshot(unitID, LocaleType.ZH_CN, name), rev: 1 };
    case "base":
      return { ...getBasesEmptySnapshot(unitID), rev: 1, name };
    case "sheet":
    default:
      return {
        id: unitID,
        rev: 1,
        name,
        appVersion: "",
        locale: LocaleType.ZH_CN,
        styles: {},
        resources: [],
        sheetOrder: ["sheet-1"],
        sheets: {
          "sheet-1": {
            id: "sheet-1",
            name,
            rowCount: 500,
            columnCount: 40,
            cellData: {},
            defaultColumnWidth: 100,
            defaultRowHeight: 24,
          },
        },
      };
  }
}

// Create a unit of any supported kind.
app.post("/internal/units", async (req, res) => {
  const identity = requireInternal(req, res);
  if (!identity) return;
  const body = req.body ?? {};
  const unitID = String(body.unit_id || "");
  const kind = String(body.kind || "sheet").toLowerCase();
  const name = String(body.name || unitID);
  if (!unitID) return res.status(400).json({ error: "unit_id is required" });
  if (!(kind in KIND_TYPES)) return res.status(400).json({ error: `unsupported kind: ${kind}` });
  try {
    const result = await service.createUnitFromData(
      { type: KIND_TYPES[kind], data: initialData(kind, unitID, name) },
      { userID: identity.userID },
    );
    res.json({ unit_id: result.unitID, kind, head_revision: result.headRevision, status: result.status });
  } catch (err) {
    console.error("[univer] create unit failed:", err);
    res.status(500).json({ error: err?.message ?? String(err) });
  }
});

// Structure of one unit, shaped by its kind.
app.get("/internal/units/:unitID/meta", async (req, res) => {
  const identity = requireInternal(req, res);
  if (!identity) return;
  const unitID = req.params.unitID;
  try {
    const type = await unitType(unitID, identity.userID);
    const kind = TYPE_KINDS[String(type)] || "sheet";
    const rec = await service.dbAdapter.getUnit(
      { userID: identity.userID, customData: { internal: true } },
      unitID,
    );
    const result = await runWithRuntime(
      identity.userID,
      unitID,
      (runtime) => runtime.execute({ mode: "read", code: metaCode(kind) }),
      { commit: false },
    );
    const items = result.value ?? [];
    const payload = { unit_id: unitID, kind, head_revision: rec.headRevision };
    if (kind === "sheet") payload.sheets = items;
    else if (kind === "base") payload.tables = items;
    else if (kind === "slide") payload.slides = items;
    else payload.title = items?.[0]?.name ?? "";
    res.json(payload);
  } catch (err) {
    res.status(500).json({ error: err?.message ?? String(err) });
  }
});

// Read one unit as text.
app.get("/internal/units/:unitID/content", async (req, res) => {
  const identity = requireInternal(req, res);
  if (!identity) return;
  const unitID = req.params.unitID;
  const range = normalizeRange(req.query.range);
  try {
    const type = await unitType(unitID, identity.userID);
    const kind = TYPE_KINDS[String(type)] || "sheet";
    const rec = await service.dbAdapter.getUnit(
      { userID: identity.userID, customData: { internal: true } },
      unitID,
    );
    const result = await runWithRuntime(
      identity.userID,
      unitID,
      (runtime) => runtime.execute({ mode: "read", code: readCode(kind, range) }),
      { commit: false },
    );
    let text;
    if (kind === "sheet") text = renderCsv(result.value);
    else if (kind === "doc") text = streamToText(result.value?.stream);
    else text = JSON.stringify(result.value, null, 2);
    res.json({ unit_id: unitID, kind, head_revision: rec.headRevision, text });
  } catch (err) {
    res.status(500).json({ error: err?.message ?? String(err) });
  }
});

// Spreadsheet writes replace a range; document writes append text.
app.put("/internal/units/:unitID/content", async (req, res) => {
  const identity = requireInternal(req, res);
  if (!identity) return;
  const unitID = req.params.unitID;
  const { range, values } = req.body ?? {};
  if (!range || !Array.isArray(values)) {
    return res.status(400).json({ error: "range and values are required" });
  }
  try {
    const type = await unitType(unitID, identity.userID);
    if (TYPE_KINDS[String(type)] !== "sheet") {
      return res.status(400).json({ error: "range writes apply to spreadsheets only" });
    }
    const code =
      `univerAPI.getActiveWorkbook().getActiveSheet().getRange(${JSON.stringify(range)})` +
      `.setValues(${JSON.stringify(values)});`;
    await runWithRuntime(identity.userID, unitID, (runtime) =>
      runtime.execute({ mode: "write", code }),
    );
    const rec = await service.dbAdapter.getUnit(
      { userID: identity.userID, customData: { internal: true } },
      unitID,
    );
    res.json({ unit_id: unitID, head_revision: rec?.headRevision ?? 0 });
  } catch (err) {
    res.status(500).json({ error: err?.message ?? String(err) });
  }
});

// Append text to a document unit.
app.post("/internal/units/:unitID/content", async (req, res) => {
  const identity = requireInternal(req, res);
  if (!identity) return;
  const unitID = req.params.unitID;
  const text = String((req.body ?? {}).text || "");
  if (!text) return res.status(400).json({ error: "text is required" });
  try {
    const type = await unitType(unitID, identity.userID);
    if (TYPE_KINDS[String(type)] !== "doc") {
      return res.status(400).json({ error: "text appends apply to documents only" });
    }
    // An empty document still carries one empty paragraph; writing at offset 0
    // fills it instead of leaving a blank line above the text. Later appends go
    // before the trailing paragraph break the stream keeps at the end.
    const code =
      `const d = univerAPI.getActiveDocument();` +
      `const body = d.getBody();` +
      `const empty = String(body.dataStream || "").trim().length === 0;` +
      `const at = empty ? 0 : Math.max(0, body.dataStream.length - 1);` +
      `d.insertText(at, ${JSON.stringify(text)});`;
    await runWithRuntime(identity.userID, unitID, (runtime) =>
      runtime.execute({ mode: "write", code }),
    );
    const rec = await service.dbAdapter.getUnit(
      { userID: identity.userID, customData: { internal: true } },
      unitID,
    );
    res.json({ unit_id: unitID, head_revision: rec?.headRevision ?? 0 });
  } catch (err) {
    res.status(500).json({ error: err?.message ?? String(err) });
  }
});

const server = createServer(app);
transport.attach(server);

server.listen(PORT, HOST, () => {
  console.log(`[univer] collaboration server on http://${HOST}:${PORT} (db=${DB_MODE})`);
});

async function shutdown() {
  try {
    await transport.dispose();
    await service.dispose();
    await database.dispose?.();
    await database.__pool?.end?.();
  } finally {
    process.exit(0);
  }
}
process.on("SIGTERM", shutdown);
process.on("SIGINT", shutdown);
