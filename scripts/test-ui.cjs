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
// Just enough of a node tree for keyed list rendering.
const fakeNode = html => ({
  html, parentNode: null,
  get nextSibling() { const siblings = this.parentNode?.childNodes || []; return siblings[siblings.indexOf(this) + 1] ?? null; },
  get isConnected() { return !!this.parentNode; },
  remove() { if (this.parentNode) this.parentNode.childNodes.splice(this.parentNode.childNodes.indexOf(this), 1); this.parentNode = null; },
});
function element(id) {
  if (!elements.has(id)) elements.set(id, {
    value: id.endsWith('credential-type') ? 'auth_files:codex' : id.endsWith('credential-plan') ? 'all' : '', innerHTML: '', hidden: true, dataset: {}, childNodes: [],
    get firstChild() { return this.childNodes[0] ?? null; },
    insertBefore(node, ref) { node.remove(); const i = this.childNodes.indexOf(ref); this.childNodes.splice(i < 0 ? this.childNodes.length : i, 0, node); node.parentNode = this; },
    setAttribute(name, value) { this[name] = value; }, querySelector(selector) { return element(id + ' ' + selector); }, listeners: {}, addEventListener(name, fn) { this.listeners[name] = fn; },
    contains() { return false; }, focus() {}, showModal() { this.open = true; },
  });
  return elements.get(id);
}
const context = vm.createContext({
  TextEncoder, Uint8Array, Uint32Array, DataView, console, AbortController, AbortSignal, URLSearchParams,
  addEventListener() {},
  Date: class extends Date { static now() { return now; } },
  setTimeout(callback, delay) { const id = ++timerID; noticeTimers.set(id, { callback, delay }); return id; },
  clearTimeout(id) { noticeTimers.delete(id); },
  window: { crypto: {} }, // Verify the HTTP fallback, not just SubtleCrypto.
  document: { getElementById: element, querySelectorAll: () => [], createElement: () => ({ set innerHTML(html) { this.content = { firstElementChild: fakeNode(html) }; } }) },
  sessionStorage: { getItem: () => null },
  localStorage: {
    getItem: key => browserStorage.get(key) || null,
    setItem(key, value) { if (storageBlocked) throw new Error('Storage unavailable'); storageWrites++; browserStorage.set(key, value); },
  },
});
let source = ['credentials.js', 'catalog.js', 'components.js', 'candy.js', 'fingerprint.js', 'modeltrace.js', 'app.js']
  .map(file => webFile(file).split('// Events and initialization.')[0]).join('\n');
source = source.replace('/*FINGERPRINT_CONFIG*/{}', JSON.stringify({ modes: [{ id: 'quick', name: '快速', cells: 4, samples_per_cell: 15 }], default_concurrency: 2, max_concurrency: 6 }));
source = source.replace('/*MODELTRACE_CONFIG*/{}', JSON.stringify({requests:3,default_concurrency:3,max_concurrency:3}));
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
    'codex-api-key': [{ 'api-key': key, prefix: 'prefix', headers: { Z: 'z', A: 'a' }, weight: 0 }, { 'api-key': key, prefix: 'prefix', headers: { A: 'a', Z: 'z' }, 'excluded-models': ['gpt-test', ' * '] }],
    'claude-api-key': [{ 'base-url': 'https://example.test' }],
    'meta-api-key': [{ 'api-key': 'meta-test-key' }],
    'vertex-api-key': [{ 'api-key': 'vertex-key', 'excluded-models': ['*'] }],
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
  assert.deepEqual(codex.map(x => x.disabled), [false, true], 'only the exclude-all rule disables a config key');
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
  run(`initializeCandy(); initializeFingerprint(); catalogCache.models = {time:Date.now(),ids:['prefix/model','gpt-5.6-sol','claude-sonnet','gpt-6.1-sol']}; fillModels()`);
  assert.equal(element('model').value, 'gpt-6.1-sol');
  assert.equal(element('effort').value, 'low');
  assert.equal(element('model').innerHTML, '<option value="gpt-6.1-sol">gpt-6.1-sol</option><option value="gpt-5.6-sol">gpt-5.6-sol</option><option value="claude-sonnet">claude-sonnet</option><option value="prefix/model">prefix/model</option>');
  assert.equal(element('fp-model').innerHTML, element('model').innerHTML);
  assert.equal(element('mt-model').innerHTML, element('model').innerHTML);
  const sortedModels = ['gpt-6.1-astra','gpt-6.1-sol','gpt-6.1-terra','gpt-6.1-luna','gpt-6-astra','gpt-6-sol','gpt-6-terra','gpt-6-luna','gpt-5.6-sol','claude-sonnet-4.6','claude-opus-4.6','claude-opus-4.5','gemini-pro','qwen'];
  assert.deepEqual(plain(run(`${JSON.stringify([...sortedModels].reverse())}.sort(compareModels)`)), sortedModels);
  assert.deepEqual(plain(run(`['gpt-6-luna','vendor/gpt-6.1-sol','gpt-6-astra','vendor/claude-sonnet','other'].sort(compareModels)`)), ['vendor/gpt-6.1-sol','gpt-6-astra','gpt-6-luna','vendor/claude-sonnet','other']);
  element('model').value = 'gpt-5.6-sol';
  element('effort').value = 'high';
  run(`candySavePrefs(); fillModels(); initializeCandy()`);
  assert.equal(element('model').value, 'gpt-5.6-sol', 'explicit saved selections should survive the new default');
  assert.equal(element('effort').value, 'high');
  element('effort').value = 'low';
  run(`candySavePrefs()`);
  assert.equal(run(`availableCredential({modeltrace_running:{}})`), false);
  assert.equal(run(`mtPercent(0.12345)`), '12.3%');
  assert.equal(run(`mtConcurrency()`), 3);
  element('mt-concurrency').value = '2';
  assert.equal(run(`mtConcurrency()`), 2);
  element('mt-concurrency').value = '10';
  assert.equal(run(`mtConcurrency()`), 3);
  run(`mtSavePrefs()`);
  assert.equal(run(`stored(PREF_STORE + '.modeltrace').concurrency`), 3);
  assert.equal(run(`stored(PREF_STORE + '.modeltrace').temperature`), undefined);
  assert(run(`mtOutcome({status:'partial',attribution:{prediction:'<script>',family_prediction_name:'GPT',used_outputs:2}})`).includes('&lt;script&gt;'));
  assert(run(`mtOutcome({status:'cancelled'})`).includes('测试已停止'));
  const failedOutcome = run(`mtOutcome({status:'failed',error:'<error>'})`);
  assert(failedOutcome.includes('测试失败'));
  assert(!failedOutcome.includes('&lt;error&gt;'), 'raw failures belong in the detail dialog, not the result table');
  assert(run(`mtOutcome({status:'completed',attribution:{prediction:'gpt-test',family_prediction_name:'GPT',used_outputs:3,probability:0.834}})`).includes('<span>gpt-test</span><span class="mt-probability mono">83.4%</span>'));
  assert.equal(run(`mtComparison({status:'completed',model:'provider/GPT-TEST',attribution:{prediction:'gpt-test'}}).tone`), 'ok');
  assert.equal(run(`mtComparison({status:'completed',model:'gpt-test',attribution:{prediction:'other-model'}}).tone`), 'warn');
  assert.equal(run(`mtComparison({status:'cancelled',model:'gpt-test',attribution:{prediction:'gpt-test'}}).tone`), 'neutral');
  assert(run(`mtOutcome({status:'partial',model:'gpt-test',attribution:{prediction:'other-model'}})`).includes('与测试模型不一致 · 部分结果'));
  for (const [status, label] of [['cancelled','采集已停止'], ['failed','采集失败'], ['skipped','已跳过'], ['completed','与所选模型一致']]) {
    assert(run(`fpOutcome({status:${JSON.stringify(status)},attribution:{status:'consistent'}})`).includes(label));
  }
  run(`showFingerprintDetail({id:'skipped',status:'skipped',model:'test',error:'不支持的模型'})`);
  assert.equal(element('fp-detail-body').innerHTML.match(/不支持的模型/g).length, 1);
  assert(!element('fp-detail-body').innerHTML.includes('基准比对'));
  run(`loadController = {}; renderRefresh()`);
  assert.equal(element('refresh').disabled, true);
  assert.equal(element('refresh')['aria-busy'], true);
  run(`loadController = null; pending = true; renderRefresh()`);
  assert.equal(element('refresh').disabled, true);
  assert.equal(element('refresh')['aria-busy'], false);
  run(`pending = false; renderRefresh()`);
  assert.equal(element('refresh').disabled, false);
  run(`var detailPaths = []; api = async (path) => { detailPaths.push(path); return {id:'failed',time:'2026-09-28T00:00:00Z',status:'failed',model:'<model>',error:'<request failed>',duration_ms:0}; }`);
  await run(`openResultDetail('mt', 'modeltrace', 'cred/1', 'failed', showModelTraceDetail)`);
  assert.equal(element('mt-detail').open, true);
  assert.deepEqual(plain(run(`detailPaths`)), [run(`BASE`) + '/modeltrace/record?auth_id=cred%2F1&id=failed'], 'details load on demand');
  assert.equal(element('mt-detail-body').innerHTML.match(/&lt;request failed&gt;/g).length, 1);
  assert(!element('mt-detail-body').innerHTML.includes('<model>'));
  const progress = run(`renderModelTraceRow({id:'progress',name:'Test',source:'auth_files',provider:'codex',modeltrace_running:{model:'gpt-test',phase:'collecting',done:2},modeltraces:[]})`);
  assert(progress.includes('2/3'));
  assert(!progress.includes('份回答'));
  const historyRecord = {id:'record',time:'2026-09-28T00:00:00Z',model:'<model>',effort:'low',mode:'quick',status:'completed',ok:true,answer:'21',duration_ms:1200,input_tokens:1234,output_tokens:567,reasoning_tokens:89,attribution:{status:'consistent',prediction:'<model>',probability:0.9}};
  const historyCredential = {id:'history',name:'Test',source:'auth_files',provider:'codex',results:[historyRecord,historyRecord],fingerprints:[historyRecord,historyRecord],modeltraces:[historyRecord,historyRecord]};
  run(`candyExpanded.add('history'); fpExpanded.add('history'); mtExpanded.add('history')`);
  for (const renderer of ['renderCandyRow','renderFingerprintRow','renderModelTraceRow']) {
    const markup = run(`${renderer}(${JSON.stringify(historyCredential)})`);
    assert.equal((markup.match(/class="history-title"/g) || []).length, 1);
    assert.equal((markup.match(/<article class="history-card">/g) || []).length, 2);
    assert.equal((markup.match(/class="history-card-head"/g) || []).length, 2);
    assert(markup.includes('data-row="history"'));
    for (const head of markup.matchAll(/class="history-card-head">(.*?)<div class="history-card-body">/gs)) {
      const labels = [...head[1].matchAll(/aria-label="(耗时|输入 tokens|输出 tokens|推理 tokens) ([^"]+)"/g)].map(match => match[1] + ' ' + match[2]);
      assert.deepEqual(labels, ['耗时 1.2s', '输入 tokens 1,234', '输出 tokens 567', '推理 tokens 89']);
      assert(head[1].includes(run(`ICONS['square-arrow-right-enter']`)));
      assert(head[1].includes(run(`ICONS['square-arrow-right-exit']`)));
    }
    assert(!markup.includes('<model>'));
    const empty = run(`${renderer}(${JSON.stringify({...historyCredential,results:[],fingerprints:[],modeltraces:[]})})`);
    assert(empty.includes('<div class="empty">暂无记录</div>'));
  }
  const unknownCard = run(`historyCard({duration_ms:1200,input_tokens:null,output_tokens:null}, '')`);
  assert(unknownCard.includes('输入 tokens —'));
  assert(unknownCard.includes('输出 tokens —'));
  assert(!unknownCard.includes('推理 tokens'));
  const zeroCard = run(`historyCard({duration_ms:0,input_tokens:0,output_tokens:0,reasoning_tokens:0,status:'skipped'}, '')`);
  assert(zeroCard.includes('耗时 0.0s'));
  assert(zeroCard.includes('输入 tokens 0'));
  assert(zeroCard.includes('输出 tokens 0'));
  assert(!zeroCard.includes('推理 tokens'));
  for (const [renderer, runningKey, type] of [['renderFingerprintRow','fingerprint_running','fp'],['renderModelTraceRow','modeltrace_running','mt']]) {
    const markup = run(`${renderer}(${JSON.stringify({...historyCredential,[runningKey]:{phase:'cancelling',model:'test',done:2,total:3}})})`);
    assert(markup.includes('等待当前请求结束'));
    assert(markup.includes('已停止后续请求，当前请求返回后保存结果'));
    assert(new RegExp(`data-${type}-cancel="history"[^>]*disabled`).test(markup));
  }
  const skippedLatest = run(`renderCandyRow(${JSON.stringify({...historyCredential,id:'skipped',results:[{...historyRecord,skipped:true}]})})`);
  assert(!skippedLatest.includes('输入 tokens'));
  run(`switchTab('modeltrace')`);
  assert.equal(element('modeltrace-panel').hidden, false);
  assert.equal(element('candy-panel').hidden, true);
  assert.equal(element('fingerprint-panel').hidden, true);
  run(`switchTab('candy')`);
  assert.equal(element('effort').value, 'low');
  run(`store(PREF_STORE, {effort:'max'}); initializeCandy()`);
  assert.equal(element('effort').value, 'max');
  run(`store(PREF_STORE, {}); initializeCandy()`);
  assert.equal(element('effort').value, 'low');
  run(`fillSelect('effort', DEFAULT_EFFORTS, 'none', DEFAULT_EFFORT)`);
  assert.equal(element('effort').value, 'none');
  run(`credentials = ${JSON.stringify([
    {id:'free',source:'auth_files',provider:'codex',plan_type:'free'},
    {id:'premium',source:'auth_files',provider:'codex',plan_type:'self_serve_business_prolite'},
    {id:'plus',source:'auth_files',provider:'codex',plan_type:' PLUS '},
    {id:'pro',source:'auth_files',provider:'codex',plan_type:'pro'},
    {id:'lite',source:'auth_files',provider:'codex',plan_type:'prolite'},
    {id:'max',source:'auth_files',provider:'codex',plan_type:'promax'},
    {id:'team',source:'auth_files',provider:'codex',plan_type:'team'},
    {id:'go',source:'auth_files',provider:'codex',plan_type:'go'},
    {id:'k12',source:'auth_files',provider:'codex',plan_type:'k12'},
    {id:'codex:apikey:1',source:'ai_providers',provider:'codex'},
  ])}`);
  element('credential-type').value = 'all';
  run(`fillPlanFilter('')`);
  assert.deepEqual([...element('credential-plan').innerHTML.matchAll(/value="([^"]*)"/g)].map(m => m[1]), ['all','Pro ALL','Pro 500','Pro 200','Pro 100','Plus','Go','Business Premium','Business','Free','k12']);
  assert.equal(element('plan-filter').hidden, false);
  assert.deepEqual(plain(run(`visibleCredentials('').map(a => a.id)`)), ['max','pro','lite','plus','go','premium','team','free','k12','codex:apikey:1']);
  element('credential-plan').value = 'Pro 200';
  assert.deepEqual(plain(run(`visibleCredentials('').map(a => a.id)`)), ['pro']);
  element('credential-plan').value = 'Pro ALL';
  assert.deepEqual(plain(run(`visibleCredentials('').map(a => a.id)`)), ['max','pro','lite']);
  run(`credentials = credentials.filter(a => a.id !== 'pro' && a.id !== 'max'); fillPlanFilter('')`);
  assert(!element('credential-plan').innerHTML.includes('Pro ALL'), 'a single Pro plan needs no group');
  assert.equal(element('credential-plan').value, 'all');
  element('credential-type').value = 'ai_providers:codex';
  run(`fillPlanFilter('')`);
  assert.equal(element('plan-filter').hidden, true, 'lists without Codex files have no plan filter');
  assert.equal(element('credential-plan').value, 'all');
  element('credential-type').value = 'all';
  const planTag = (id) => run(`credentialView(credentials.find(a => a.id === '${id}'), false)`).match(/<span class="tag ([^"]*)"><span>([^<]*)/g).at(-1);
  assert.equal(planTag('lite'), '<span class="tag plan-premium"><span>Pro 100');
  assert.equal(planTag('k12'), '<span class="tag "><span>k12');
  assert(!run(`credentialView(credentials.at(-1), false)`).includes('plan-'));
  const cooling = {id:'cooling',source:'auth_files',provider:'codex',plan_type:'promax',unavailable:true,status_message:'quota exhausted',next_retry_after:'2026-10-01T08:00:00Z'};
  const coolingView = run(`credentialView(${JSON.stringify(cooling)}, false)`);
  assert(coolingView.includes('<span class="tag plan-elite"><span>Pro 500'));
  assert(/class="tag warn" title="quota exhausted · 预计 \d\d-\d\d \d\d:\d\d 恢复"><span>不可用/.test(coolingView));
  assert.equal(run(`availableCredential(${JSON.stringify(cooling)})`), false, 'unavailable credentials cannot be tested');
  const enableSwitchInput = (a) => run(`enableSwitch(${JSON.stringify(a)})`).match(/<input[^>]*>/)[0];
  assert.match(enableSwitchInput({id:'on',source:'auth_files',provider:'codex'}), /checked(?! disabled)/);
  assert.doesNotMatch(enableSwitchInput({id:'off',source:'auth_files',provider:'codex',disabled:true}), /checked|disabled/, 'disabled files must be re-enabled');
  assert.match(enableSwitchInput({id:'busy',source:'auth_files',provider:'codex',fingerprint_running:{}}), /disabled/);
  assert.doesNotMatch(enableSwitchInput({id:'claude:apikey:1',source:'ai_providers',provider:'claude'}), /disabled/, 'config API keys are switchable');
  assert.match(enableSwitchInput({id:'openai-compatibility:demo:1',source:'ai_providers',provider:'openai-compatible-demo'}), /disabled/);
  for (const [renderer, rows, expanded] of [['renderCandyRow','rows','candyExpanded'], ['renderFingerprintRow','fp-rows','fpExpanded'], ['renderModelTraceRow','mt-rows','mtExpanded']]) {
    assert(run(`${renderer}({...credentials[0], results:[], fingerprints:[], modeltraces:[]})`).includes('data-enable="free"'));
    if (rows === 'mt-rows') run(`initializeModelTrace()`);
    element(rows).listeners.click({target:{closest: (selector) => selector === 'label' ? {} : selector === '[data-row]' ? {dataset:{row:'free'}} : null}});
    assert.equal(run(`${expanded}.has('free')`), false, 'row controls must not expand history');
  }
  element('credential-type').value = 'auth_files:codex';
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
  assert.equal(element('mt-credential-type').value, 'auth_files:codex');
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

  const validState = {auths:[{id:'valid-state',name:'Test',source:'auth_files',provider:'codex',results:[],fingerprints:[],modeltraces:[]}],storage_error:''};
  run(`resetCatalogCache(); let finishModels, finishSync, modelsStarted, syncStarted;
    const modelsWaiting = new Promise(resolve => {modelsStarted=resolve});
    const syncWaiting = new Promise(resolve => {syncStarted=resolve});
    api = async path => {
      if (path.endsWith('/config')) return {};
      if (path.endsWith('/v1/models')) return new Promise(resolve => {finishModels=resolve; modelsStarted();});
      if (path.endsWith('/sync')) return new Promise(resolve => {finishSync=resolve; syncStarted();});
      throw new Error('Unexpected endpoint');
    }`);
  const parallelCatalog = run(`refreshCatalog({force:true})`);
  await Promise.all([run(`modelsWaiting`), run(`syncWaiting`)]);
  run(`finishModels({data:[{id:'test-model'}]})`);
  await new Promise(setImmediate);
  assert.equal(run(`catalogCache.models`), null, 'a directory is ready only after credential sync');
  run(`finishSync({synced:0})`);
  await parallelCatalog;
  assert.deepEqual(plain(run(`catalogCache.models.ids`)), ['test-model']);
  run(`credentials = []; refreshCatalog = async () => {throw new Error('Directory unavailable')}; api = async () => (${JSON.stringify(validState)})`);
  await run(`load()`);
  assert.equal(run(`credentials[0].id`), 'valid-state', 'a directory failure should still allow viewing history');
  assert.equal(element('catalog-error').hidden, false);
  run(`refreshCatalog = async () => {}; api = async () => (${JSON.stringify(validState)})`);
  await run(`load()`);
  for (const invalid of [{auths:null},{auths:[null]},{auths:[{id:'broken',results:null,fingerprints:[],modeltraces:[]}]},{auths:[{...validState.auths[0],results:[null]}]}]) {
    run(`api = async () => (${JSON.stringify(invalid)})`);
    await run(`load()`);
    assert.equal(run(`credentials[0].id`), 'valid-state', 'invalid responses must not replace the last valid state');
    assert.equal(element('load-error').hidden, false);
    assert.equal(element('refresh')['aria-busy'], false);
    assert(run(`pollTimer > 0`), 'polling must recover after a bad response');
  }
  run(`api = async () => (${JSON.stringify(validState)})`);
  await run(`load()`);
  assert.equal(element('load-error').hidden, true);
  run(`let finishState, stateStarted; const stateWaiting = new Promise(resolve => {stateStarted=resolve});
    api = () => new Promise(resolve => {finishState=resolve;stateStarted();})`);
  const outdatedState = run(`load()`);
  await run(`stateWaiting`);
  run(`api = async () => (${JSON.stringify(validState)})`);
  await run(`load()`);
  run(`finishState({auths:[]})`);
  await outdatedState;
  assert.equal(run(`credentials[0].id`), 'valid-state', 'an aborted refresh must not overwrite newer data');
  run(`catalogCache.models = {time:Date.now(),ids:['available-model']}; let submitted = false;
    api = async (path, options) => { if (path.endsWith('/run')) submitted = true; return ${JSON.stringify(validState)}; }`);
  await run(`update('/modeltrace/run', {method:'POST',body:{auth_ids:['valid-state'],model:'removed-model'}}, '开始测试失败')`);
  assert.equal(run(`submitted`), false);
  assert(element('flash .notice-message').textContent.includes('重新选择'));
  assert.equal(run(`pending`), false);
  run(`var statusServer = ${JSON.stringify(validState)}, statusCalls = [];
    api = async (path, options = {}) => {
      if (path.endsWith('/state')) return JSON.parse(JSON.stringify(statusServer));
      statusCalls.push([path, options.method, options.body]);
      if (!options.body.disabled) throw new Error('Denied');
      statusServer.auths[0].disabled = true;
      return {status:'ok'};
    }`);
  element('app').hidden = false;
  await run(`setCredentialEnabled('valid-state', false)`);
  assert.deepEqual(plain(run(`statusCalls`)), [['/v0/management/auth-files/status','PATCH',{name:'valid-state',disabled:true}]]);
  assert.equal(run(`credentials[0].disabled`), true);
  await run(`setCredentialEnabled('valid-state', true)`);
  assert.equal(run(`credentials[0].disabled`), true, 'a rejected change must reload the CPA state');
  assert(element('flash .notice-message').textContent.includes('启用凭证失败：Denied'));
  run(`let finishStatus; api = (path) => path.endsWith('/state') ? Promise.resolve(JSON.parse(JSON.stringify(statusServer)))
    : new Promise(resolve => { finishStatus = () => { statusServer.auths[0].disabled = false; resolve({status:'ok'}); }; })`);
  const enabling = run(`setCredentialEnabled('valid-state', true)`);
  assert.equal(run(`pending`), false, 'switching one credential must not lock the page');
  await run(`load()`);
  assert.equal(run(`credentials[0].disabled`), false, 'reloads keep a requested state until CPA applies it');
  assert.match(run(`enableSwitch(credentials[0])`), /aria-busy="true"[^>]*disabled/);
  run(`finishStatus()`);
  await enabling;
  assert.equal(run(`credentials[0].disabled || switching.has('valid-state')`), false);
  element('app').hidden = true;
  assert.equal(run(`mtPercent(undefined)`), '—');
  assert(!run(`mtResultHTML({status:'completed',model:'test',attribution:{prediction:'test',probability:0.5,used_outputs:1,family_probabilities:null,results:null}})`).includes('NaN'));
  run(`candySelected.add('a'); fpSelected.add('a'); mtSelected.add('a'); mtExpanded.add('a'); showLogin('Expired')`);
  assert.equal(run(`credentials.length + candySelected.size + fpSelected.size + mtSelected.size + mtExpanded.size`), 0);
  assert.equal(element('app').hidden, true);
  assert.equal(element('login-error').textContent, 'Expired');
  assert(!element('mt-rows').innerHTML.includes('data-mt-run'));
  console.log('UI checks passed: identities, filters, plans, enable switches, catalog recovery, ModelTrace results and controls, refresh, login reset, notices, metrics.');
})().catch(err => { console.error(err); process.exitCode = 1; });
