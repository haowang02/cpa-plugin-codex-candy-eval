"use strict";

async function sha256Hex(value) {
  const bytes = new TextEncoder().encode(value);
  if (window.crypto?.subtle) {
    const digest = await crypto.subtle.digest("SHA-256", bytes);
    return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
  }
  // Public HTTP origins lack SubtleCrypto; identity hashes must still match.
  const constants = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01,
    0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
    0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
    0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070, 0x19a4c116, 0x1e376c08,
    0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
    0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2
  ];
  const hash = new Uint32Array([0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19]);
  const padded = new Uint8Array(Math.ceil((bytes.length + 9) / 64) * 64);
  padded.set(bytes);
  padded[bytes.length] = 0x80;
  const data = new DataView(padded.buffer);
  const bitLength = bytes.length * 8;
  data.setUint32(padded.length - 8, Math.floor(bitLength / 0x100000000));
  data.setUint32(padded.length - 4, bitLength >>> 0);
  const words = new Uint32Array(64);
  const rotate = (word, bits) => (word >>> bits) | (word << (32 - bits));
  for (let offset = 0; offset < padded.length; offset += 64) {
    for (let i = 0; i < 16; i++) words[i] = data.getUint32(offset + i * 4);
    for (let i = 16; i < 64; i++) {
      const x = words[i - 15], y = words[i - 2];
      const s0 = rotate(x, 7) ^ rotate(x, 18) ^ (x >>> 3);
      const s1 = rotate(y, 17) ^ rotate(y, 19) ^ (y >>> 10);
      words[i] = words[i - 16] + s0 + words[i - 7] + s1;
    }
    let [a, b, c, d, e, f, g, h] = hash;
    for (let i = 0; i < 64; i++) {
      const sum1 = rotate(e, 6) ^ rotate(e, 11) ^ rotate(e, 25);
      const choice = (e & f) ^ (~e & g);
      const t1 = (h + sum1 + choice + constants[i] + words[i]) >>> 0;
      const sum0 = rotate(a, 2) ^ rotate(a, 13) ^ rotate(a, 22);
      const majority = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (sum0 + majority) >>> 0;
      h = g;
      g = f;
      f = e;
      e = (d + t1) >>> 0;
      d = c;
      c = b;
      b = a;
      a = (t1 + t2) >>> 0;
    }
    const block = [a, b, c, d, e, f, g, h];
    for (let i = 0; i < 8; i++) hash[i] += block[i];
  }
  return Array.from(hash, (word) => word.toString(16).padStart(8, "0")).join("");
}

function previewCredential(value) {
  value = String(value || "").trim();
  if (value.length <= 12) return "*".repeat(value.length);
  return value.slice(0, 6) + "…" + value.slice(-4);
}

// CPA config identities match internal/watcher/synthesizer (including collisions).
async function configuredCredentials(config, providerGroups) {
  const record = (value) => !!value && typeof value === "object" && !Array.isArray(value);
  if (!record(config)) throw new Error("CPA 凭证配置格式无效");
  if (providerGroups != null && !record(providerGroups)) throw new Error("CPA 凭证分组格式无效");
  const credentials = [], counters = new Map();
  const text = (v) => String(v || "").trim();
  // CPA and CPAMC disable a config API key by excluding every model.
  const excludesAll = (entry) => Array.isArray(entry["excluded-models"]) && entry["excluded-models"].some((model) => text(model) === "*");
  const headers = (entry) => Object.keys(entry.headers || {}).sort().map((key) => key + "\0" + entry.headers[key] + "\0").join("");
  const entries = (field, value = config[field]) => {
    if (value == null) return [];
    if (!Array.isArray(value) || !value.every(record)) throw new Error("CPA 凭证配置格式无效：" + field);
    return value;
  };
  // v0 lists each provider's effective keys without their v8 group names. Pair keys by CPA's
  // normalized dedup identity, so keys CPA dropped or deduplicated cannot shift the names.
  const identity = (entry) => {
    const prefix = text(entry.prefix).replace(/^\/+|\/+$/g, "");
    const headerPairs = Object.entries(entry.headers || {}).map(([key, value]) => [text(key), text(value)]).filter(([key, value]) => key && value).sort();
    return JSON.stringify([text(entry["api-key"]), text(entry["proxy-url"]), prefix.includes("/") ? "" : prefix, headerPairs]);
  };
  // v8 keeps each "<family>-api-key" list as named groups under api-keys.<family>.
  function groupKeys(field) {
    const family = field.replace(/-api-key$/, "");
    return entries("api-keys." + family, providerGroups?.[family]).flatMap((group) =>
      entries(`api-keys.${family}.keys`, group.keys).map((key) => ({
        // Keys inherit the group's fields unless they set their own.
        identity: identity({ ...group, ...Object.fromEntries(Object.entries(key).filter(([, value]) => value !== null)) }),
        baseURL: text(group["base-url"]),
        name: text(group.name),
      })));
  }
  function groupName(keys, entry) {
    const id = identity(entry), baseURL = text(entry["base-url"]);
    let index = keys.findIndex((key) => key.identity === id && key.baseURL === baseURL);
    // CPA fills in some providers' default URL when a group has none.
    if (index < 0) index = keys.findIndex((key) => key.identity === id && !key.baseURL);
    return index < 0 ? "" : keys.splice(index, 1)[0].name;
  }
  async function add(kind, parts, provider, disabled, apiKey, baseURL, providerName) {
    const digest = await sha256Hex(kind + parts.map((part) => "\0" + text(part)).join(""));
    const base = kind + ":" + digest.slice(0, 12), collision = counters.get(base) || 0;
    counters.set(base, collision + 1);
    const id = collision ? base + "-" + collision : base;
    credentials.push({ id, provider, name: previewCredential(apiKey) || provider + " · " + id.slice(kind.length + 1), base_url: text(baseURL), provider_name: text(providerName), disabled });
  }
  for (const [field, provider] of [
    ["gemini-api-key", "gemini"], ["interactions-api-key", "gemini-interactions"],
    ["claude-api-key", "claude"], ["codex-api-key", "codex"], ["xai-api-key", "xai"], ["meta-api-key", "meta"],
  ]) {
    const groups = groupKeys(field);
    for (const entry of entries(field)) {
      if (!text(entry["api-key"]) && !text(entry["base-url"])) continue;
      await add(provider + ":apikey", [entry["api-key"], entry["base-url"], entry["proxy-url"], entry.prefix, headers(entry)], provider, excludesAll(entry), entry["api-key"], entry["base-url"], groupName(groups, entry));
    }
  }
  for (const entry of entries("openai-compatibility")) {
    // Disabled parents are skipped by CPA before allocating stable IDs.
    if (entry.disabled) continue;
    const name = text(entry.name).toLowerCase() || "openai-compatibility";
    const provider = name === "openai-compatibility" || name.startsWith("openai-compatible-") ? name : "openai-compatible-" + name;
    const keys = entries("api-key-entries", entry["api-key-entries"]);
    if (!keys.length) await add("openai-compatibility:" + name, [entry["base-url"]], provider, false, "", entry["base-url"], entry.name);
    for (const entryKey of keys) await add("openai-compatibility:" + name, [entryKey["api-key"], entry["base-url"], entryKey["proxy-url"]], provider, false, entryKey["api-key"], entry["base-url"], entry.name);
  }
  const vertexGroups = groupKeys("vertex-api-key");
  for (const entry of entries("vertex-api-key")) {
    await add("vertex:apikey", [entry["api-key"], entry["base-url"], entry["proxy-url"]], "vertex", excludesAll(entry), entry["api-key"], entry["base-url"], groupName(vertexGroups, entry));
  }
  return credentials;
}
