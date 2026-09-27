const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

const webFile = name => fs.readFileSync(path.join(__dirname, '../internal/plugin/web', name), 'utf8');
const elements = new Map();
const noticeTimers = new Map();
const browserStorage = new Map();
let now = Date.now(), storageBlocked = false, storageWrites = 0;
let timerID = 0;
function element(id) {
  if (!elements.has(id)) elements.set(id, { value: id.endsWith('credential-type') ? 'auth_files:codex' : '', innerHTML: '', hidden: true, dataset: {}, setAttribute(name, value) { this[name] = value; }, querySelector(selector) { return element(id + ' ' + selector); }, addEventListener() {} });
  return elements.get(id);
}
const context = vm.createContext({
  TextEncoder, Uint8Array, Uint32Array, DataView, console,
  Date: class extends Date { static now() { return now; } },
  setTimeout(callback, delay) { const id = ++timerID; noticeTimers.set(id, { callback, delay }); return id; },
  clearTimeout(id) { noticeTimers.delete(id); },
  window: { crypto: {} }, // Verify the HTTP fallback, not just SubtleCrypto.
  document: { getElementById: element },
  sessionStorage: { getItem: () => null },
  localStorage: {
    getItem: key => browserStorage.get(key) || null,
    setItem(key, value) { if (storageBlocked) throw new Error('Storage unavailable'); storageWrites++; browserStorage.set(key, value); },
  },
});
let source = webFile('credentials.js') + '\n' + webFile('catalog.js') + '\n' + webFile('app.js').split('// Events and initialization.')[0];
source = source.replace('/*FINGERPRINT_CONFIG*/{}', JSON.stringify({ modes: [{ id: 'quick', name: '快速', cells: 4, samples_per_cell: 15 }], default_concurrency: 2, max_concurrency: 6 }));
vm.runInContext(source, context);
const run = code => vm.runInContext(code, context);
const plain = value => JSON.parse(JSON.stringify(value));
const stableID = (kind, parts) => kind + ':' + crypto.createHash('sha256').update(kind + parts.map(x => '\0' + (x || '').trim()).join('')).digest('hex').slice(0, 12);

(async () => {
  for (const input of ['', 'abc', '中文配置', 'a'.repeat(1000)]) {
    assert.equal(await run(`sha256Hex(${JSON.stringify(input)})`), crypto.createHash('sha256').update(input).digest('hex'));
  }
  const key = 'synthetic-private-api-key';
  const config = {
    'codex-api-key': [{ 'api-key': key, prefix: 'prefix', headers: { Z: 'z', A: 'a' } }, { 'api-key': key, prefix: 'prefix', headers: { A: 'a', Z: 'z' }, weight: 0 }],
    'claude-api-key': [{ 'base-url': 'https://example.test' }],
    'meta-api-key': [{ 'api-key': 'meta-test-key' }],
    'vertex-api-key': [{ 'api-key': 'vertex-key', disabled: true }],
    'openai-compatibility': [{ name: 'Demo', disabled: true, 'api-key-entries': [{ 'api-key': key }] }, { name: 'Demo', 'api-key-entries': [{ 'api-key': key }] }, { name: 'No-key', 'base-url': 'https://example.test' }],
  };
  const credentials = plain(await run(`configuredCredentials(${JSON.stringify(config)})`));
  const codexID = stableID('codex:apikey', [key, '', '', 'prefix', 'A\0a\0Z\0z\0']);
  const codex = credentials.filter(x => x.provider === 'codex');
  assert.equal(codex[0].id, codexID);
  assert.equal(codex[0].name, 'synthe…-key');
  assert.equal(run(`previewCredential('short-key')`), '*********');
  assert.equal(run(`previewCredential('  1234567890123  ')`), '123456…0123');
  assert.equal(codex[1].id, codexID + '-1');
  assert.equal(codex[1].disabled, true);
  assert.equal(credentials.find(x => x.provider === 'claude').id, stableID('claude:apikey', ['', 'https://example.test', '', '', '']));
  assert.equal(credentials.find(x => x.provider === 'openai-compatible-demo').id, stableID('openai-compatibility:demo', [key, '', '']));
  assert.equal(credentials.find(x => x.provider === 'openai-compatible-no-key').id, stableID('openai-compatibility:no-key', ['https://example.test']));
  assert.equal(credentials.find(x => x.provider === 'vertex').disabled, true);
  assert(!JSON.stringify(credentials).includes(key));
  assert(!JSON.stringify(credentials).includes('https://'));

  assert.equal(run(`HIDDEN_MODEL('vendor/IMAGE-preview')`), true);
  assert.equal(run(`HIDDEN_MODEL('codex-auto-review')`), true);
  assert.equal(run(`HIDDEN_MODEL('provider/codex-auto-review(high)')`), true);
  assert.equal(run(`HIDDEN_MODEL('claude-sonnet')`), false);
  run(`initializeControls(); catalogCache.models = {time:Date.now(),ids:['prefix/model','gpt-5.6-sol','claude-sonnet']}; fillModels()`);
  assert.equal(element('model').value, 'gpt-5.6-sol');
  assert.equal(element('model').innerHTML, '<option value="claude-sonnet">claude-sonnet</option><option value="gpt-5.6-sol">gpt-5.6-sol</option><option value="prefix/model">prefix/model</option>');
  assert.equal(element('fp-model').innerHTML, element('model').innerHTML);
  assert.equal(element('effort').value, 'low');
  run(`fillSelect('effort', DEFAULT_EFFORTS, 'none', DEFAULT_EFFORT)`);
  assert.equal(element('effort').value, 'none');
  run(`credentials = [
    {id:'codex',source:'auth_files',provider:'codex'},
    {id:'claude',source:'ai_providers',provider:'claude'},
    {id:'config',source:'ai_providers',provider:'codex'}
  ]; fillCredentialTypes(); candySelected.add('claude'); candySelected.add('codex')`);
  assert(element('credential-type').innerHTML.startsWith('<option value="all">全部凭证'));
  assert.equal(element('credential-type').value, 'auth_files:codex');
  assert.deepEqual(plain(run(`selectedCredentials('', candySelected).map(a => a.id)`)), ['codex']);
  element('credential-type').value = 'all';
  assert.deepEqual(plain(run(`visibleCredentials('').map(a => a.id)`)), ['codex', 'claude', 'config']);
  element('credential-type').value = 'ai_providers:codex';
  assert.deepEqual(plain(run(`visibleCredentials('').map(a => a.id)`)), ['config']);
  assert.equal(element('fp-credential-type').value, 'auth_files:codex');
  assert.equal(run(`kind({skipped:true,error:'unsupported'})`), 'skip');
  element('credential-type').value = 'all';
  run(`candySelected.clear(); candySelected.add('codex'); credentials[0].running = {done:0,total:1}`);
  assert.deepEqual(plain(run(`batchCredentials('', candySelected)`)), []);
  run(`renderSelection('', candySelected, '测试')`);
  assert.equal(element('run-batch').disabled, true);
  assert(element('run-batch').innerHTML.includes('测试所选 (0)'));
  assert.equal(element('select-all').checked, false);
  run(`candySelected.clear()`);
  assert.deepEqual(plain(run(`batchCredentials('', candySelected).map(a => a.id)`)), ['claude', 'config']);
  for (const malformed of [[], {'codex-api-key': [null]}, {'openai-compatibility': [{'api-key-entries': [null]}]}]) {
    await assert.rejects(run(`configuredCredentials(${JSON.stringify(malformed)})`), /CPA 凭证配置格式无效/);
  }

  run(`api = async (path) => {
    if (path.endsWith('unknown')) throw new Error('unavailable');
    if (path.endsWith('invalid')) return {models: null};
    return {models: path.endsWith('empty') ? [] : [{id:'prefix/alias'}]};
  }`);
  const catalog = plain(await run(`modelCatalog(['supported','empty','unknown','invalid'])`));
  assert.deepEqual(catalog, {supported:['prefix/alias'], empty:[]});

  run(`key = 'synthetic-management-key'; credentials = ['a','b'].map(id => ({id,source:'auth_files',provider:'codex'})); pruneCredentialCatalog()`);
  await run(`openCatalogCache(key)`);
  run(`let lookupCalls = 0; api = async () => { lookupCalls++; return {models:[{id:'alias'}]}; }`);
  await Promise.all([run(`modelCatalog(['a','a','b'])`), run(`modelCatalog(['a'])`)]);
  assert.equal(run(`lookupCalls`), 2, 'duplicate and concurrent lookups should share requests');
  const writesBeforeReload = storageWrites;
  await run(`openCatalogCache(key)`);
  assert.equal(storageWrites - writesBeforeReload, 1, 'restoring the cache should not write an empty intermediate state');
  await run(`modelCatalog(['a','b'])`);
  assert.equal(run(`lookupCalls`), 2, 'reload should reuse the persisted directory');
  assert(![...browserStorage.values()].join('').includes('synthetic-management-key'));
  run(`credentials[0].disabled = true; pruneCredentialCatalog()`);
  await run(`modelCatalog(['a','b'])`);
  assert.equal(run(`lookupCalls`), 3, 'credential changes should invalidate only that entry');

  now += 5 * 60000;
  run(`api = async () => { lookupCalls++; throw new Error('Temporary failure'); }`);
  assert.deepEqual(plain(await run(`modelCatalog(['a'])`)), {}, 'expired support must not survive a failed lookup');
  assert.deepEqual(plain(await run(`modelCatalog(['a'])`)), {});
  assert.equal(run(`lookupCalls`), 4, 'failed lookups should briefly back off');
  now += 15000;
  run(`api = async () => { lookupCalls++; return {models:[]}; }`);
  assert.deepEqual(plain(await run(`modelCatalog(['a'])`)), {a:[]});
  assert.deepEqual(plain(await run(`modelCatalog(['a'])`)), {a:[]});
  assert.equal(run(`lookupCalls`), 5, 'confirmed empty directories should be cached');

  run(`let finishLookup, lookupStarted; const lookupWaiting = new Promise(resolve => {lookupStarted=resolve});
    api = () => new Promise(resolve => { finishLookup=resolve; lookupStarted(); })`);
  const interrupted = run(`modelCatalog(['a','b'])`);
  await run(`lookupWaiting`);
  run(`resetCatalogCache(); finishLookup({models:[{id:'stale'}]})`);
  assert.deepEqual(plain(await interrupted), {}, 'manual refresh must discard both cached and in-flight results from the old batch');
  run(`api = async () => { throw new AuthError('Expired login'); }`);
  await assert.rejects(run(`modelCatalog(['a'])`), /Expired login/);
  run(`api = async () => { lookupCalls++; return {models:[{id:'alias'}]}; }`);
  storageBlocked = true;
  await run(`modelCatalog(['a'])`);
  await run(`modelCatalog(['a'])`);
  storageBlocked = false;
  assert.equal(run(`lookupCalls`), 6, 'memory cache should work when storage is blocked');

  run(`catalogRefreshedAt = 0; let configVersion = 1, modelVersion = 'alias', configCalls = 0, modelCalls = 0;
    api = async path => {
      if (path.endsWith('/config')) { configCalls++; return {version:configVersion,'api-keys':['synthetic-client-key']}; }
      if (path.endsWith('/sync')) return {synced:0};
      if (path.endsWith('/v1/models')) { modelCalls++; return {data:[{id:modelVersion},{id:'hidden-image'}]}; }
      return {models:[{id:modelVersion}]};
    }`);
  await run(`refreshCatalog()`);
  await run(`modelCatalog(['a'])`);
  await run(`refreshCatalog()`);
  assert.equal(run(`configCalls`), 1);
  assert.equal(run(`modelCalls`), 1);
  assert.deepEqual(plain(run(`catalogCache.models.ids`)), ['alias']);
  await run(`openCatalogCache(key)`);
  await run(`refreshCatalog({force:true})`);
  assert.equal(run(`modelCalls`), 1, 'a fresh global model list should survive reload');
  assert(run(`!!catalogCache.credentials.a`));
  run(`configVersion++`);
  await run(`refreshCatalog({force:true})`);
  assert.equal(run(`catalogCache.credentials.a`), undefined, 'config changes must invalidate support');
  await run(`modelCatalog(['a'])`);
  now += 60000;
  await run(`openCatalogCache(key)`);
  await run(`modelVersion='new-model'; refreshCatalog({force:true})`);
  assert.equal(run(`catalogCache.credentials.a`), undefined, 'model list changes must invalidate support after reload');
  assert(![...browserStorage.values()].join('').includes('synthetic-client-key'));
  await run(`modelCatalog(['a'])`);
  await run(`key='different-management-key'; openCatalogCache(key)`);
  assert.equal(run(`catalogCache.credentials.a`), undefined, 'logins must not share directories');
  browserStorage.set(run(`CATALOG_STORE`), '{invalid');
  await run(`openCatalogCache(key)`);
  assert.equal(run(`catalogCache.models`), null);
  run(`let finishCatalog, catalogStarted; const catalogWaiting = new Promise(resolve => {catalogStarted=resolve});
    api = async path => {
      if (path.endsWith('/v1/models')) return new Promise(resolve => {finishCatalog=resolve; catalogStarted();});
      return {};
    }`);
  const staleRefresh = run(`refreshCatalog({force:true})`);
  await run(`catalogWaiting`);
  run(`resetCatalogCache(); finishCatalog({data:[{id:'outdated-model'}]})`);
  await assert.rejects(staleRefresh, /目录已更新/);
  assert.equal(run(`catalogCache.models`), null, 'a late response must not overwrite a reset catalog');
  run(`setNotice('flash', 'Read failed')`);
  assert.equal(element('flash').hidden, false);
  assert.equal(element('flash').role, 'alert');
  assert.equal(element('flash .notice-message').textContent, 'Read failed');
  assert.equal([...noticeTimers.values()][0].delay, 8000);
  run(`hideNotice('flash'); setNotice('flash', 'Read failed')`);
  assert.equal(element('flash').hidden, true);
  assert.equal(noticeTimers.size, 0);
  run(`setNotice('flash', 'Different error')`);
  assert.equal(element('flash').hidden, false);
  [...noticeTimers.values()][0].callback();
  assert.equal(element('flash').hidden, true);
  run(`setNotice('flash', 'Different error')`);
  assert.equal(element('flash').hidden, true);
  run(`setNotice('flash', ''); setNotice('flash', 'Different error')`);
  assert.equal(element('flash').hidden, false);
  run(`setNotice('flash', 'Started', 'info')`);
  assert.equal(element('flash').role, 'status');
  assert.equal(noticeTimers.size, 1);
  assert.equal(run(`metrics({skipped:true, duration_ms:0, input_tokens:0, output_tokens:0, reasoning_tokens:0})`), '');
  assert(!run(`metrics({duration_ms:100, input_tokens:5, output_tokens:10, reasoning_tokens:0})`).includes('推理 tokens'));
  assert(run(`metrics({duration_ms:100, input_tokens:5, output_tokens:10, reasoning_tokens:2})`).includes('推理 tokens'));
  console.log('UI checks passed: identities, filters, preflight, cache reuse/invalidation/recovery, notices, metrics.');
})().catch(err => { console.error(err); process.exitCode = 1; });
