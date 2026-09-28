"use strict";

const CATALOG_STORE = "cpa-codex-candy-eval.catalog.v1";
const MODEL_CATALOG_TTL = 60000;
const CREDENTIAL_CATALOG_TTL = 5 * 60000;
const CATALOG_RETRY_DELAY = 15000;
let catalogCache = { owner: "", revision: "", models: null, credentials: Object.create(null) };
let catalogRefreshedAt = 0;
const catalogRequests = new Map();
const catalogFailures = new Map();
let credentialSignatures = new Map();

function freshCatalog(entry, ttl) {
  const age = Date.now() - entry?.time;
  return Number.isFinite(entry?.time) && age >= 0 && age < ttl && Array.isArray(entry.ids) && entry.ids.every((id) => typeof id === "string" && id.trim());
}

function saveCatalogCache() {
  if (catalogCache.owner) store(CATALOG_STORE, catalogCache);
}

function resetCatalogCache(next = { owner: catalogCache.owner, revision: "", models: null, credentials: Object.create(null) }) {
  catalogCache = next;
  catalogRefreshedAt = 0;
  catalogRequests.clear();
  catalogFailures.clear();
  saveCatalogCache();
}

async function openCatalogCache(managementKey) {
  const owner = await sha256Hex(CATALOG_STORE + "\0" + managementKey);
  if (key !== managementKey) return;
  const saved = stored(CATALOG_STORE);
  const next = { owner, revision: "", models: null, credentials: Object.create(null) };
  if (saved?.owner === owner && typeof saved.revision === "string") {
    next.revision = saved.revision;
    if (freshCatalog(saved.models, CREDENTIAL_CATALOG_TTL)) next.models = { time: saved.models.time, ids: saved.models.ids };
    for (const [id, entry] of Object.entries(saved.credentials || {})) {
      if (freshCatalog(entry, CREDENTIAL_CATALOG_TTL) && typeof entry.signature === "string") next.credentials[id] = { time: entry.time, ids: entry.ids, signature: entry.signature };
    }
  }
  resetCatalogCache(next);
}

function credentialSignature(id) {
  return credentialSignatures.get(id) || "";
}

function pruneCredentialCatalog() {
  credentialSignatures = new Map(credentials.map((entry) => [entry.id, JSON.stringify([entry.source, entry.provider, entry.plan_type, entry.disabled])]));
  let changed = false;
  for (const [id, entry] of Object.entries(catalogCache.credentials)) {
    if (!credentialSignatures.has(id) || !freshCatalog(entry, CREDENTIAL_CATALOG_TTL) || entry.signature !== credentialSignature(id)) {
      delete catalogCache.credentials[id];
      catalogFailures.delete(id);
      changed = true;
    }
  }
  for (const [id, expires] of catalogFailures) if (!credentialSignatures.has(id) || expires <= Date.now()) catalogFailures.delete(id);
  if (changed) saveCatalogCache();
}

function modelIDs(entries) {
  if (!Array.isArray(entries) || entries.some((entry) => typeof entry?.id !== "string" || !entry.id.trim())) throw new Error("CPA 模型目录格式无效");
  return [...new Set(entries.map((entry) => entry.id))].sort();
}

async function refreshCatalog({ signal, force = false } = {}) {
  const age = Date.now() - catalogRefreshedAt;
  if (!force && age >= 0 && age < MODEL_CATALOG_TTL) return;
  catalogRefreshedAt = 0;
  const cache = catalogCache;
  const checkCurrent = () => {
    signal?.throwIfAborted();
    if (cache !== catalogCache) throw new Error("凭证目录已更新，请重试。");
  };
  const config = await api("/v0/management/config", { signal });
  const inventory = await configuredCredentials(config);
  // Detect routing and alias changes without storing the configuration or keys.
  const revision = await sha256Hex(JSON.stringify(config));
  checkCurrent();
  await api(BASE + "/credentials/sync", { method: "POST", body: { credentials: inventory }, signal });
  checkCurrent();
  let models = cache.models;
  if (revision !== cache.revision || !freshCatalog(models, MODEL_CATALOG_TTL)) {
    const keys = Array.isArray(config["api-keys"]) ? config["api-keys"] : [];
    const data = await api("/v1/models", { apiKey: keys[0] || "", signal });
    models = { time: Date.now(), ids: modelIDs(data.data).filter((id) => !HIDDEN_MODEL(id)) };
  }
  checkCurrent();
  if (revision !== cache.revision || JSON.stringify(models.ids) !== JSON.stringify(cache.models?.ids)) {
    resetCatalogCache({ owner: cache.owner, revision, models, credentials: Object.create(null) });
  } else {
    cache.models = models;
    saveCatalogCache();
  }
  catalogRefreshedAt = Date.now();
}

async function credentialModels(id) {
  const cache = catalogCache, signature = credentialSignature(id);
  const entry = cache.credentials[id];
  if (freshCatalog(entry, CREDENTIAL_CATALOG_TTL) && entry.signature === signature) return entry.ids;
  if (catalogFailures.get(id) > Date.now()) return null;
  if (catalogRequests.has(id)) return catalogRequests.get(id);
  const request = (async () => {
    try {
      const data = await api("/v0/management/auth-files/models?name=" + encodeURIComponent(id));
      const ids = modelIDs(data.models);
      if (cache !== catalogCache || signature !== credentialSignature(id)) return null;
      cache.credentials[id] = { time: Date.now(), ids, signature };
      catalogFailures.delete(id);
      return ids;
    } catch (err) {
      if (cache !== catalogCache || signature !== credentialSignature(id)) return null;
      if (err instanceof AuthError) throw err;
      // A failed lookup is unknown, never an empty (unsupported) catalog.
      catalogFailures.set(id, Date.now() + CATALOG_RETRY_DELAY);
      return null;
    } finally {
      if (catalogRequests.get(id) === request) catalogRequests.delete(id);
    }
  })();
  catalogRequests.set(id, request);
  return request;
}

async function modelCatalog(ids) {
  const cache = catalogCache;
  const catalog = Object.create(null), queue = [...new Set(ids)];
  try {
    await Promise.all(Array.from({ length: Math.min(6, queue.length) }, async () => {
      while (queue.length && cache === catalogCache) {
        const id = queue.shift(), models = await credentialModels(id);
        if (models !== null) catalog[id] = models;
      }
    }));
  } catch (err) {
    queue.length = 0;
    throw err;
  } finally {
    saveCatalogCache();
  }
  if (cache !== catalogCache) return Object.create(null);
  return catalog;
}
