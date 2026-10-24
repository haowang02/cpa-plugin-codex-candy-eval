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
// Just enough of a node tree for keyed list rendering; nodes differ by markup, so a changed row is swapped whole.
const fakeNode = html => ({
  html, nodeName: html, parentNode: null,
  isEqualNode(other) { return other.html === html; },
  replaceWith(node) { const siblings = this.parentNode.childNodes; siblings[siblings.indexOf(this)] = node; node.parentNode = this.parentNode; this.parentNode = null; },
  get nextSibling() { const siblings = this.parentNode?.childNodes || []; return siblings[siblings.indexOf(this) + 1] ?? null; },
  get isConnected() { return !!this.parentNode; },
  remove() { if (this.parentNode) this.parentNode.childNodes.splice(this.parentNode.childNodes.indexOf(this), 1); this.parentNode = null; },
});
function element(id) {
  if (!elements.has(id)) elements.set(id, {
    value: '', innerHTML: '', hidden: true, dataset: {}, childNodes: [],
    get firstChild() { return this.childNodes[0] ?? null; },
    insertBefore(node, ref) { node.remove(); const i = this.childNodes.indexOf(ref); this.childNodes.splice(i < 0 ? this.childNodes.length : i, 0, node); node.parentNode = this; },
    setAttribute(name, value) { this[name] = value; }, querySelector(selector) { return element(id + ' ' + selector); }, listeners: {}, addEventListener(name, fn) { this.listeners[name] = fn; },
    contains() { return false; }, focus() {}, showModal() { this.open = true; },
  });
  return elements.get(id);
}
const context = vm.createContext({
  TextEncoder, TextDecoder, Uint8Array, Uint32Array, DataView, atob, console, AbortController, AbortSignal, URLSearchParams,
  location: { host: 'cpa.test' }, navigator: { userAgent: 'test-agent' },
  addEventListener() {},
  Date: class extends Date { static now() { return now; } },
  setTimeout(callback, delay) { const id = ++timerID; noticeTimers.set(id, { callback, delay }); return id; },
  clearTimeout(id) { noticeTimers.delete(id); },
  window: { crypto: {} }, // Verify the HTTP fallback, not just SubtleCrypto.
  document: { compatMode: 'CSS1Compat', getElementById: element, querySelectorAll: () => [], createElement: () => ({ set innerHTML(html) { this.content = { firstElementChild: fakeNode(html) }; } }) },
  sessionStorage: { getItem: () => null },
  localStorage: {
    getItem: key => browserStorage.get(key) || null,
    setItem(key, value) { if (storageBlocked) throw new Error('Storage unavailable'); storageWrites++; browserStorage.set(key, value); },
  },
});
let source = ['vendor/markdown-it.min.js', 'vendor/markdown-it-cjk-friendly.min.js', 'vendor/texmath.js', 'vendor/temml.min.js', 'credentials.js', 'catalog.js', 'components.js', 'candy.js', 'fingerprint.js', 'pelican.js', 'app.js']
  .map(file => webFile(file).split('// Events and initialization.')[0]).join('\n');
source = source.replace('/*FINGERPRINT_CONFIG*/{}', JSON.stringify({requests:3,aliases:{'gpt-6.1-sol':'gpt-6-astra'}}));
source = source.replace('/*PELICAN_VARIANTS*/[]', JSON.stringify([{id:'original',label:'原版',prompt:'p'},{id:'all-in-one',label:'多合一版',prompt:'q'}]));
vm.runInContext(source, context);
const run = code => vm.runInContext(code, context);
const plain = value => JSON.parse(JSON.stringify(value));
const filter = change => run(`Object.assign(credentialFilter, ${JSON.stringify(change)})`);
const visible = () => plain(run(`visibleCredentials().map(a => a.id)`));
const stableID = (kind, parts) => kind + ':' + crypto.createHash('sha256').update(kind + parts.map(x => '\0' + (x || '').trim()).join('')).digest('hex').slice(0, 12);

(async () => {
  let finishAPI;
  context.fetch = () => new Promise(resolve => { finishAPI = resolve; });
  run(`key='old-login'`);
  const previousLogin = run(`api('/test')`);
  run(`key='new-login'`);
  finishAPI({status:401,ok:false,text:async () => '{"error":"Expired login"}'});
  await assert.rejects(previousLogin, error => !(error instanceof run(`AuthError`)) && error.status === 401, 'a stale key must not sign out a newer login');
  delete context.fetch;
  for (const input of ['', 'abc', '中文配置', 'a'.repeat(1000)]) {
    assert.equal(await run(`sha256Hex(${JSON.stringify(input)})`), crypto.createHash('sha256').update(input).digest('hex'));
  }
  const panelAuth = (version, managementKey) => {
    const secret = Buffer.from('cli-proxy-api-webui::secure-storage|' + (version === 2 ? 'v2|cpa.test' : 'cpa.test|test-agent'));
    const data = Buffer.from(JSON.stringify({ state: { managementKey } }));
    return `enc::v${version}::` + data.map((byte, i) => byte ^ secret[i % secret.length]).toString('base64');
  };
  for (const version of [1, 2]) {
    browserStorage.set('cli-proxy-auth', panelAuth(version, ' panel-key '));
    assert.equal(run(`panelKey()`), 'panel-key', `the panel login stored as enc::v${version}:: must be reused`);
  }
  browserStorage.delete('cli-proxy-auth');
  const key = 'synthetic-private-api-key';
  const config = {
    'codex-api-key': [{ 'api-key': key, prefix: 'prefix', headers: { Z: 'z', A: 'a' }, weight: 0, priority: 99 }, { 'api-key': key, prefix: 'prefix', headers: { A: 'a', Z: 'z' }, 'excluded-models': ['gpt-test', ' * '] }],
    'claude-api-key': [{ 'base-url': 'https://example.test' }],
    'meta-api-key': [{ 'api-key': 'meta-test-key' }],
    'vertex-api-key': [{ 'api-key': 'vertex-key', 'excluded-models': ['*'], priority: -3 }],
    'openai-compatibility': [{ name: 'Demo', disabled: true, 'api-key-entries': [{ 'api-key': key }] }, { name: 'Demo', priority: 7, 'api-key-entries': [{ 'api-key': key, priority: 100 }] }, { name: 'No-key', priority: 9, 'base-url': 'https://example.test' }],
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
  assert.deepEqual(codex.map(x => x.priority), [99, 0]);
  assert.equal(credentials.find(x => x.provider === 'vertex').priority, -3);
  assert.equal(credentials.find(x => x.provider === 'openai-compatible-demo').priority, 7, 'OpenAI-compatible keys inherit the provider priority');
  assert.equal(credentials.find(x => x.provider === 'openai-compatible-no-key').priority, 9);
  assert.equal(credentials.find(x => x.provider === 'claude').id, stableID('claude:apikey', ['', 'https://example.test', '', '', '']));
  assert.equal(credentials.find(x => x.provider === 'openai-compatible-demo').id, stableID('openai-compatibility:demo', [key, '', '']));
  assert.equal(credentials.find(x => x.provider === 'openai-compatible-no-key').id, stableID('openai-compatibility:no-key', ['https://example.test']));
  assert.equal(credentials.find(x => x.provider === 'vertex').disabled, true);
  assert(!JSON.stringify(credentials).includes(key));
  assert.equal(credentials.find(x => x.provider === 'claude').base_url, 'https://example.test');
  assert.equal(credentials.find(x => x.provider === 'openai-compatible-demo').provider_name, 'Demo');
  for (const [field, family] of [['gemini-api-key','gemini'],['claude-api-key','claude'],['meta-api-key','meta'],['vertex-api-key','vertex']]) {
    const runtime = {[field]:[{'api-key':key,'base-url':'https://a.test'},{'api-key':key,'base-url':'https://a.test'}]};
    const groups = {[family]:[{name:' First ','base-url':'https://a.test',keys:[{'api-key':key}]},{name:'Second','base-url':'https://a.test',keys:[{'api-key':key}]}]};
    const named = plain(await run(`configuredCredentials(${JSON.stringify(runtime)}, ${JSON.stringify(groups)})`));
    assert.deepEqual(named.map(a => a.id), plain(await run(`configuredCredentials(${JSON.stringify(runtime)})`)).map(a => a.id), 'names must not change CPA identities');
    assert.deepEqual(named.map(a => a.provider_name), ['First','Second'], family + ' names must follow source order');
  }
  const groupNames = (runtime, groups) => run(`configuredCredentials(${JSON.stringify(runtime)}, ${JSON.stringify(groups)})`).then(list => plain(list.map(a => a.provider_name)));
  assert.deepEqual(await groupNames({'meta-api-key':[{'api-key':'k','base-url':'https://default.test'}]}, {meta:[{name:'Default',keys:[{'api-key':'k'}]}]}), ['Default'], 'URLs CPA fills in must keep the group name');
  assert.deepEqual(await groupNames({'gemini-api-key':[{'api-key':'k',prefix:'a',headers:{A:'a'}},{'api-key':'k',prefix:'b',headers:{B:'b'}}]}, {gemini:[
    {name:'First',prefix:'/a/',headers:{' A ':' a ','':'x'},keys:[{'api-key':'k',headers:null}]},
    {name:'Duplicate',prefix:'a',headers:{A:'a'},keys:[{'api-key':'k'}]},
    {name:'Override',prefix:'group',keys:[{'api-key':'k',prefix:'b',headers:{B:'b'}}]},
  ]}), ['First','Override'], 'deduplicated keys and key overrides must not shift names');

  const configuredView = run(`credentialView({id:'c',source:'ai_providers',provider:'claude',provider_name:'DeepSeek',name:'synthe…-key',base_url:'https://api.example.test/anthropic'}, false)`);
  assert(configuredView.includes('<span class="name">api.example.test/anthropic</span>'));
  assert(configuredView.includes('<div class="api-key name mono">synthe…-key</div>'));
  assert(configuredView.includes('<span>DeepSeek</span>'));
  assert(run(`credentialView({id:'c',source:'ai_providers',provider:'gemini',name:'k'}, false)`).includes('默认地址'));
  run(`credentials = [{id:'a',source:'ai_providers',provider:'openai-compatible-demo'},{id:'b',source:'ai_providers',provider:'openai-compatibility'}]; renderCredentialFilters()`);
  assert.equal(element('candy-credential-type').innerHTML.match(/value="ai_providers:openai-compatibility"/g).length, 1, 'OpenAI-compatible providers share one type');
  filter({type:'ai_providers:openai-compatibility'});
  assert.equal(visible().length, 2);
  filter({type:'auth_files:codex'});
  run(`credentials = []`);

  assert.equal(run(`HIDDEN_MODEL('vendor/IMAGE-preview')`), true);
  run(`initializeCandy(); catalogCache.models = {time:Date.now(),ids:['prefix/model','gpt-5.6-sol','claude-sonnet','gpt-6.1-sol']}; fillModels()`);
  assert.equal(element('candy-model').value, 'gpt-6.1-sol');
  assert.equal(element('candy-effort').value, 'low');
  assert.equal(element('candy-model').innerHTML, '<option value="gpt-6.1-sol">gpt-6.1-sol</option><option value="gpt-5.6-sol">gpt-5.6-sol</option><option value="claude-sonnet">claude-sonnet</option><option value="prefix/model">prefix/model</option>');
  assert.equal(element('fingerprint-model').innerHTML, element('candy-model').innerHTML);
  const sortedModels = ['gpt-6.1-astra','gpt-6.1-sol','gpt-6.1-terra','gpt-6.1-luna','gpt-6-astra','gpt-6-sol','gpt-6-terra','gpt-6-luna','gpt-5.6-sol','codex-auto-review(high)','codex-auto-review','claude-sonnet-4.6','claude-opus-4.6','claude-opus-4.5','gemini-pro','qwen'];
  assert.deepEqual(plain(run(`${JSON.stringify([...sortedModels].reverse())}.sort(compareModels)`)), sortedModels);
  assert.deepEqual(plain(run(`['gpt-6.9-sol','gpt-6.10-sol','gpt-7-sol'].sort(compareModels)`)), ['gpt-7-sol','gpt-6.10-sol','gpt-6.9-sol']);
  assert.deepEqual(plain(run(`['gpt-6-luna','vendor/gpt-6.1-sol','gpt-6-astra','vendor/claude-sonnet','other'].sort(compareModels)`)), ['vendor/gpt-6.1-sol','gpt-6-astra','gpt-6-luna','vendor/claude-sonnet','other']);
  element('candy-model').value = '';
  run(`savePreference('candy-model', 'gpt-5.6-sol'); savePreference('candy-effort', 'high'); fillModels(); initializeCandy()`);
  assert.equal(element('candy-model').value, 'gpt-5.6-sol', 'saved selections survive the new default');
  assert.equal(element('candy-effort').value, 'high');
  run(`savePreference('candy-effort', 7); initializeCandy()`);
  assert.equal(element('candy-effort').value, 'low', 'saved values of the wrong type fall back');
  assert.equal(run(`availableCredential({running:{test:'fingerprint'}})`), false);
  assert.equal(run(`fmtPercent(0.12345)`), '12.3%');
  assert.equal(run(`fingerprintConcurrency()`), 3);
  element('fingerprint-concurrency').value = '10';
  assert.equal(run(`fingerprintConcurrency()`), 3, 'concurrency is capped at the request count');
  assert(run(`fingerprintOutcome({status:'partial',attribution:{prediction:'gpt-test',prediction_name:'<script>',family_prediction_name:'GPT',used_outputs:2}})`).includes('&lt;script&gt;'));
  assert(run(`fingerprintOutcome({status:'cancelled'})`).includes('测试已停止'));
  assert(!run(`fingerprintOutcome({status:'failed',error:'<error>'})`).includes('&lt;error&gt;'), 'raw failures belong in the detail dialog, not the result table');
  assert(run(`fingerprintOutcome({status:'completed',attribution:{prediction:'gpt-test',prediction_name:'gpt-test',family_prediction_name:'GPT',used_outputs:3,probability:0.834}})`).includes('83.4%'));
  assert.equal(run(`fingerprintComparison({status:'completed',model:'openai/GPT-6.1-sol',attribution:{prediction:'gpt-6-astra'}}).tone`), 'ok', 'gpt-6.1-sol shares the gpt-6-astra candidate');
  assert.equal(run(`fingerprintComparison({status:'completed',model:'constructor',attribution:{prediction:'other-model'}}).tone`), 'warn');
  assert.equal(run(`fingerprintComparison({status:'cancelled',model:'gpt-test',attribution:{prediction:'gpt-test'}}).tone`), 'neutral');
  const notModel = run(`fingerprintOutcome({status:'completed',model:'gpt-test',attribution:{decision:'not_model',prediction:'',probability:null,used_outputs:3}})`);
  assert(notModel.includes('不像语言模型回答') && !notModel.includes('fingerprint-probability'));
  const partial = run(`fingerprintResultHTML({status:'partial',model:'gpt-6.1-sol',attribution:{decision:'partial',prediction:'gpt-6-astra',prediction_name:'gpt-6-astra/6.1-sol',probability:null,family_prediction_name:'GPT',used_outputs:2,results:[{model:'gpt-6-astra',display_name:'gpt-6-astra/6.1-sol',probability:null,score:2.1}]}})`);
  assert(partial.includes('不计算归因概率') && !partial.includes('NaN'));
  run(`refreshing = true; renderPageActions()`);
  assert.equal(element('refresh').disabled, true);
  assert.equal(element('refresh')['aria-busy'], true);
  run(`refreshing = false; renderPageActions()`);
  assert.equal(element('refresh').disabled, false);
  run(`var detailPaths = []; api = async (path) => { detailPaths.push(path); return {id:'failed',time:'2026-09-28T00:00:00Z',status:'failed',model:'<model>',error:'<request failed>',duration_ms:0}; }`);
  await run(`openResultDetail('fingerprint', 'cred/1', 'failed', showFingerprintDetail)`);
  assert.equal(element('fingerprint-detail').open, true);
  assert.deepEqual(plain(run(`detailPaths`)), [run(`BASE`) + '/fingerprint/record?credential_id=cred%2F1&id=failed'], 'details load on demand');
  assert.equal(element('fingerprint-detail-body').innerHTML.match(/&lt;request failed&gt;/g).length, 1);
  assert(!element('fingerprint-detail-body').innerHTML.includes('<model>'));
  assert(run(`renderFingerprintRow({id:'progress',name:'Test',source:'auth_files',provider:'codex',running:{test:'fingerprint',model:'gpt-test',done:2,total:3},fingerprints:[]})`).includes('2/3'));
  const historyRecord = {id:'record',time:'2026-09-28T00:00:00Z',model:'<model>',effort:'low',status:'correct',answer:'21',duration_ms:1200,input_tokens:1234,output_tokens:567,reasoning_tokens:89,attribution:{prediction:'<model>',prediction_name:'<model>',probability:0.9}};
  const answer = run(`answerHTML({answer:String.raw\`**答案：**21 <img src=x onerror=alert(1)> [a](javascript:alert(1)) ![b](https://example.com/b.png) \\(r\\le8\\) \\(\\href{javascript:alert(1)}{x}\\)\`})`);
  assert(answer.includes('<strong>答案：</strong>21 &lt;img') && answer.includes('<math') && !/<(img|a)\b|\shref=/.test(answer), 'answers render Markdown and TeX, never HTML, links or images');
  const frame = run(`pelicanFrame('<script>top.location = "x"</script>')`);
  assert(frame.startsWith('<iframe sandbox="allow-scripts" srcdoc="&lt;script&gt;top.location = &quot;x&quot;'), 'model pages run in an opaque origin, inside their srcdoc');
  assert(run(`pelicanHistory({id:'cred'}, {id:'drawn',time:'2026-09-28T00:00:00Z',model:'m',variant:'all-in-one',status:'failed',error:'e'})`).includes('aria-label="版本 多合一版"'), 'pelican records show their variant');
  const fingerprintRecord = {...historyRecord,id:'fingerprint',status:'completed'};
  const historyCredential = {id:'history',name:'Test',source:'auth_files',provider:'codex',candies:[historyRecord,{...historyRecord,id:'second'}],fingerprints:[fingerprintRecord,{...fingerprintRecord,id:'second'}],pelicans:[]};
  run(`candyExpanded.add('history'); fingerprintExpanded.add('history')`);
  for (const renderer of ['renderCandyRow','renderFingerprintRow']) {
    const markup = run(`${renderer}(${JSON.stringify(historyCredential)})`);
    assert.equal((markup.match(/<article class="history-card">/g) || []).length, 2);
    assert(markup.includes('aria-label="推理 Token 89"') && !markup.includes('<model>'));
    assert(run(`${renderer}(${JSON.stringify({...historyCredential,candies:[],fingerprints:[]})})`).includes('暂无记录'));
  }
  const historyMarkup = (r) => run(`historyCard(${JSON.stringify(r)}, '')`);
  assert(historyMarkup({duration_ms:1200,input_tokens:null}).includes('输入 Token —'));
  const zeroCard = historyMarkup({duration_ms:0,input_tokens:0,output_tokens:0,reasoning_tokens:0});
  assert(zeroCard.includes('输入 Token 0') && !zeroCard.includes('推理 Token') && !zeroCard.includes('重试'));
  assert(historyMarkup({duration_ms:0,attempts:3}).includes('aria-label="重试 2 次"'));
  const stopping = run(`renderFingerprintRow(${JSON.stringify({...historyCredential,running:{test:'fingerprint',cancelling:true,model:'test',done:2,total:3}})})`);
  assert(stopping.includes('正在停止') && /data-fingerprint-cancel="history"[^>]*disabled/.test(stopping));
  assert(!run(`renderCandyRow(${JSON.stringify({...historyCredential,id:'skipped',candies:[{...historyRecord,status:'skipped'}]})})`).includes('输入 Token'));
  run(`switchTab('fingerprint')`);
  assert.equal(element('candy-panel').hidden, true);
  run(`switchTab('candy')`);
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
  filter({type:'all'});
  run(`renderCredentialFilters()`);
  assert.deepEqual([...element('candy-credential-plan').innerHTML.matchAll(/value="([^"]*)"/g)].map(m => m[1]), ['all','Pro ALL','Pro 500','Pro 200','Pro 100','Plus','Go','Business Premium','Business','Free','k12']);
  assert.equal(element('candy-plan-filter').hidden, false);
  assert.deepEqual(visible(), ['max','pro','lite','plus','go','premium','team','free','k12','codex:apikey:1']);
  filter({plan:'Pro 200'});
  assert.deepEqual(visible(), ['pro']);
  filter({plan:'Pro ALL'});
  assert.deepEqual(visible(), ['max','pro','lite']);
  run(`credentials = credentials.filter(a => a.id !== 'pro' && a.id !== 'max')`);
  assert(!run(`planChoices('all')`).includes('Pro ALL'), 'a single Pro plan needs no group');
  filter({type:'ai_providers:codex', plan:'all'});
  run(`renderCredentialFilters()`);
  assert.equal(element('candy-plan-filter').hidden, true, 'lists without Codex files have no plan filter');
  filter({type:'all'});
  const view = (id) => run(`credentialView(credentials.find(a => a.id === '${id}'), false)`);
  assert(view('lite').includes('plan-premium') && !view('k12').includes('plan-') && !view('codex:apikey:1').includes('plan-'));
  assert(view('codex:apikey:1').includes('<span>P0</span>'), 'priority defaults to 0');
  const cooling = {id:'cooling',source:'auth_files',provider:'codex',plan_type:'promax',unavailable:true,status_message:'quota exhausted',next_retry_after:'2026-10-01T08:00:00Z'};
  const coolingView = run(`credentialView(${JSON.stringify(cooling)}, false)`);
  assert(coolingView.includes('<span class="tag plan-elite"><span>Pro 500'));
  assert(/class="tag warn" data-tip="quota exhausted\n预计 \d\d-\d\d \d\d:\d\d 恢复"><span>不可用/.test(coolingView));
  assert.equal(run(`availableCredential(${JSON.stringify(cooling)})`), false, 'unavailable credentials cannot be tested');
  const enableSwitchInput = (a) => run(`enableSwitch(${JSON.stringify(a)})`).match(/<input[^>]*>/)[0];
  assert.match(enableSwitchInput({id:'on',source:'auth_files',provider:'codex'}), /checked(?! disabled)/);
  assert.doesNotMatch(enableSwitchInput({id:'off',source:'auth_files',provider:'codex',disabled:true}), /checked|disabled/, 'disabled files must be re-enabled');
  assert.match(enableSwitchInput({id:'busy',source:'auth_files',provider:'codex',running:{test:'fingerprint'}}), /disabled/);
  assert.equal(run(`unavailableReason({status_message:'{"error":{"message":"The usage limit has been reached"}}'})`), 'The usage limit has been reached', 'upstream JSON errors show only their message');
  assert.doesNotMatch(enableSwitchInput({id:'claude:apikey:1',source:'ai_providers',provider:'claude'}), /disabled/, 'config API keys are switchable');
  assert.match(enableSwitchInput({id:'openai-compatibility:demo:1',source:'ai_providers',provider:'openai-compatible-demo'}), /disabled/);
  for (const [renderer, rows, expanded] of [['renderCandyRow','candy-rows','candyExpanded'], ['renderFingerprintRow','fingerprint-rows','fingerprintExpanded']]) {
    assert(run(`${renderer}({...credentials[0], candies:[], fingerprints:[], pelicans:[]})`).includes('data-enable="free"'));
    if (rows === 'fingerprint-rows') run(`initializeFingerprint()`);
    element(rows).listeners.click({target:{closest: (selector) => selector === 'label' ? {} : selector === '[data-row]' ? {dataset:{row:'free'}} : null}});
    assert.equal(run(`${expanded}.has('free')`), false, 'row controls must not expand history');
  }
  filter({type:'auth_files:codex'});
  run(`credentials = [
    {id:'codex',source:'auth_files',provider:'codex'},
    {id:'claude',source:'ai_providers',provider:'claude'},
    {id:'config',source:'ai_providers',provider:'codex'}
  ].map(a => ({...a, candies:[], fingerprints:[], pelicans:[]})); renderCredentialFilters(); candySelected.add('claude'); candySelected.add('codex')`);
  assert(element('candy-credential-type').innerHTML.startsWith('<option value="all">全部凭证'));
  assert.equal(element('candy-credential-type').value, 'auth_files:codex');
  assert.deepEqual(plain(run(`selectedCredentials(candySelected).map(a => a.id)`)), ['codex']);
  filter({type:'all'});
  assert.deepEqual(visible(), ['codex', 'claude', 'config']);
  run(`fingerprintSelected.add('codex'); setCredentialFilter({type:'ai_providers:codex', enabledOnly:true})`);
  assert.deepEqual(visible(), ['config']);
  for (const test of ['candy', 'fingerprint', 'pelican']) {
    assert.equal(element(test + '-credential-type').value, 'ai_providers:codex', 'every tab shows the shared filter');
    assert.equal(element(test + '-enabled-only').checked, true);
  }
  assert.equal(run(`candySelected.size + fingerprintSelected.size`), 0, 'a filter change clears every selection');
  assert.equal(run(`preference('credential-filter.type', '')`), 'ai_providers:codex');
  run(`var listedCredentials = credentials; credentials = []; renderCredentialFilters()`);
  assert.equal(element('fingerprint-credential-type').value, 'ai_providers:codex', 'a chosen type stays listed before credentials load');
  run(`credentials = listedCredentials; setCredentialFilter({type:'auth_files:codex', enabledOnly:false})`);
  const filterCredentials = [
    {id:'enabled',source:'auth_files',provider:'codex',plan_type:'plus'},
    {id:'disabled',source:'auth_files',provider:'codex',plan_type:'plus',disabled:true},
    {id:'cooling',source:'auth_files',provider:'codex',plan_type:'plus',unavailable:true},
    {id:'other-plan',source:'auth_files',provider:'codex',plan_type:'free'},
    {id:'provider',source:'ai_providers',provider:'codex'},
  ];
  run(`var unfilteredCredentials = credentials; credentials = ${JSON.stringify(filterCredentials)}`);
  filter({type:'all'});
  assert.equal(visible().length, 5);
  filter({enabledOnly:true});
  assert.deepEqual(visible(), ['enabled','cooling','other-plan','provider']);
  filter({type:'auth_files:codex', plan:'Plus'});
  assert.deepEqual(visible(), ['enabled','cooling'], 'enabled filter combines with type and plan without hiding cooldowns');
  filter({enabledOnly:false});
  assert.deepEqual(visible(), ['enabled','disabled','cooling']);
  filter({plan:'all'});
  run(`credentials = unfilteredCredentials`);
  filter({type:'all'});
  run(`candySelected.clear(); candySelected.add('codex'); credentials[0].running = {test:'candy',done:0,total:1}`);
  assert.deepEqual(plain(run(`batchCredentials(candySelected)`)), []);
  run(`renderSelection('candy', candySelected, '测试')`);
  assert.equal(element('candy-run-batch').disabled, true);
  assert(element('candy-run-batch').innerHTML.includes('测试所选 (0)'));
  assert.equal(element('candy-select-all').checked, false);
  run(`candySelected.clear()`);
  assert.deepEqual(plain(run(`batchCredentials(candySelected).map(a => a.id)`)), ['claude', 'config']);
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

  run(`resetCatalogCache(); api = async path => {
    if (path.endsWith('/config/api-keys')) throw Object.assign(new Error('Not found'), {status:404});
    if (path.endsWith('/v1/models')) return {data:[{id:'alias'}]};
    return {};
  }`);
  await run(`refreshCatalog()`);
  assert.deepEqual(plain(run(`catalogCache.models.ids`)), ['alias'], 'CPA without v8 groups must still load the catalog');

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
  await assert.rejects(staleRefresh, /列表已更新/);
  assert.equal(run(`catalogCache.models`), null, 'a late response must not overwrite a reset catalog');
  run(`setNotice('flash', 'Read failed')`);
  assert.equal(element('flash').hidden, false);
  assert.equal(element('flash').role, 'alert');
  run(`hideNotice('flash'); setNotice('flash', 'Read failed')`);
  assert.equal(element('flash').hidden, true, 'polling must not reopen a dismissed notice');
  run(`setNotice('flash', 'Different error')`);
  [...noticeTimers.values()][0].callback();
  run(`setNotice('flash', 'Different error')`);
  assert.equal(element('flash').hidden, true, 'nor an expired one');
  run(`setNotice('flash', ''); setNotice('flash', 'Different error')`);
  assert.equal(element('flash').hidden, false);
  run(`setNotice('flash', 'Started', 'info')`);
  assert.equal(element('flash').role, 'status');

  const validState = {credentials:[{id:'valid-state',name:'Test',source:'auth_files',provider:'codex',candies:[],fingerprints:[],pelicans:[]}],storage_error:''};
  run(`resetCatalogCache(); let finishModels, finishSync, modelsStarted, syncStarted;
    const modelsWaiting = new Promise(resolve => {modelsStarted=resolve});
    const syncWaiting = new Promise(resolve => {syncStarted=resolve});
    api = async path => {
      if (path.endsWith('/config') || path.endsWith('/config/api-keys')) return {};
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
  for (const invalid of [{credentials:null},{credentials:[null]},{credentials:[{id:'broken',candies:null,fingerprints:[],pelicans:[]}]},{credentials:[{...validState.credentials[0],candies:[null]}]}]) {
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
  run(`finishState({credentials:[]})`);
  await outdatedState;
  assert.equal(run(`credentials[0].id`), 'valid-state', 'an aborted refresh must not overwrite newer data');
  run(`catalogCache.models = {time:Date.now(),ids:['available-model']}; let submitted = false;
    api = async (path, options) => { if (path.endsWith('/run')) submitted = true; return ${JSON.stringify(validState)}; }`);
  await run(`startTest('fingerprint', {credential_ids:['valid-state'],model:'removed-model'})`);
  assert.equal(run(`submitted`), false);
  assert(element('flash .notice-message').textContent.includes('重新选择'));
  assert.equal(run(`pending`), false);
  run(`var statusServer = ${JSON.stringify(validState)}, statusCalls = [];
    api = async (path, options = {}) => {
      if (path.endsWith('/state')) return JSON.parse(JSON.stringify(statusServer));
      statusCalls.push([path, options.method, options.body]);
      if (!options.body.disabled) throw new Error('Denied');
      statusServer.credentials[0].disabled = true;
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
    : new Promise(resolve => { finishStatus = () => { statusServer.credentials[0].disabled = false; resolve({status:'ok'}); }; })`);
  const enabling = run(`setCredentialEnabled('valid-state', true)`);
  assert.equal(run(`pending`), false, 'switching one credential must not lock the page');
  await run(`load()`);
  assert.equal(run(`credentials[0].disabled`), false, 'reloads keep a requested state until CPA applies it');
  assert.match(run(`enableSwitch(credentials[0])`), /aria-busy="true"[^>]*disabled/);
  run(`finishStatus()`);
  await enabling;
  assert.equal(run(`credentials[0].disabled || switching.has('valid-state')`), false);
  element('app').hidden = true;
  assert.equal(run(`fmtPercent(undefined)`), '—');
  assert(!run(`fingerprintResultHTML({status:'completed',model:'test',attribution:{prediction:'test',prediction_name:'test',probability:0.5,used_outputs:1,results:null}})`).includes('NaN'));
  run(`candySelected.add('a'); fingerprintSelected.add('a'); fingerprintExpanded.add('a'); showLogin('Expired')`);
  assert.equal(run(`credentials.length + candySelected.size + fingerprintSelected.size + fingerprintExpanded.size`), 0);
  assert.equal(element('app').hidden, true);
  assert.equal(element('login-error').textContent, 'Expired');
  assert(!element('fingerprint-rows').innerHTML.includes('data-fingerprint-run'));
  console.log('UI checks passed: identities, filters, plans, enable switches, catalog recovery, fingerprint detection results and controls, refresh, login reset, notices, metrics.');
})().catch(err => { console.error(err); process.exitCode = 1; });
