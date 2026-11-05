const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/plugin/web/theme.js'), 'utf8');
function host({ theme, mode, cpamp = false, tokens = {} } = {}) {
  const attributes = new Set(cpamp ? ['data-cpamp-plugin-host'] : []);
  const root = {
    dataset: { ...(theme ? { theme } : {}), ...(mode ? { themeMode: mode } : {}) },
    hasAttribute: (name) => attributes.has(name),
    attributes, tokens,
  };
  root.ownerDocument = {
    fonts: [],
    defaultView: { getComputedStyle: () => ({ getPropertyValue: (name) => tokens[name] || '' }) },
  };
  return root;
}
function setup({ ancestors = [], dark = false, saved, crossOrigin = false, cpamp = false, initialTheme } = {}) {
  const root = host({ cpamp, theme: initialTheme });
  const values = new Map(), fonts = new Set(), listeners = {}, observations = [];
  root.style = { setProperty: (k, v) => values.set(k, v), removeProperty: (k) => values.delete(k) };
  const win = {};
  let frame = win;
  for (const ancestor of ancestors) {
    const parent = { document: { documentElement: ancestor } };
    frame.parent = parent;
    frame = parent;
  }
  frame.parent = frame;
  if (crossOrigin) Object.defineProperty(frame, 'parent', { get() { throw new Error('cross-origin'); } });
  const media = { matches: dark, addEventListener: (name, fn) => { listeners.media = fn; } };
  let sync;
  const state = { saved };
  vm.runInNewContext(source, {
    window: win,
    document: { documentElement: root, fonts: { add: (face) => fonts.add(face) } },
    panelValue: (name) => {
      assert.equal(name, 'cli-proxy-theme', 'appearance must not read auth or unrelated storage');
      return { state: { theme: state.saved } };
    },
    matchMedia: () => media,
    addEventListener: (name, fn) => { listeners[name] = fn; },
    MutationObserver: class {
      constructor(fn) { sync = fn; }
      observe(el, options) { observations.push({ el, options }); }
    },
  });
  return { root, values, fonts, listeners, observations, state, media, sync };
}

for (const theme of ['light', 'white', 'dark']) {
  const page = setup({ saved: theme });
  assert.equal(page.root.dataset.pluginPanel, 'cpamc');
  assert.equal(page.root.dataset.theme, theme, 'preserve CPAMC paper, white and dark modes');
  assert.equal(page.values.size, 0);
}
const standalone = setup();
assert.equal(standalone.root.dataset.theme, 'white');
standalone.media.matches = true;
standalone.listeners.media();
assert.equal(standalone.root.dataset.theme, 'dark', 'standalone follows system changes');
standalone.state.saved = 'light';
standalone.listeners.storage();
assert.equal(standalone.root.dataset.theme, 'light', 'preserve existing saved-theme events');
const initial = setup({ initialTheme: 'dark' });
assert.equal(initial.root.dataset.theme, 'dark', 'preserve an injected initial mode');
const amc = host({ theme: 'white' });
const embedded = setup({ ancestors: [amc], saved: 'dark' });
assert.equal(embedded.root.dataset.theme, 'white', 'the current host takes precedence over stale storage');
amc.dataset.theme = 'light';
embedded.sync();
assert.equal(embedded.root.dataset.theme, 'light');
for (const theme of ['light', 'dark']) {
  const page = setup({ cpamp: true, saved: theme });
  assert.equal(page.root.dataset.pluginPanel, 'cpamp');
  assert.equal(page.root.dataset.theme, theme);
}
const amp = host({ theme: 'dark', cpamp: true });
const ampPage = setup({ ancestors: [amp] });
assert.equal(ampPage.root.dataset.pluginPanel, 'cpamp', 'also accept a CPAMP marker on an ancestor');
amp.attributes.delete('data-cpamp-plugin-host');
ampPage.sync();
assert.equal(ampPage.root.dataset.pluginPanel, 'cpamc', 'panel detection can change without reloading');
const late = setup();
late.root.attributes.add('data-cpamp-plugin-host');
late.sync();
assert.equal(late.root.dataset.pluginPanel, 'cpamp', 'recognize a marker injected after initialization');

const palette = { '--bg': '#121214', '--fg': '#f4f4f6', '--font-ui': 'Custom Mono', '--accent': '#6ee7a8' };
const omc = host({ theme: 'custom-palette', mode: 'dark', tokens: palette });
const face = { family: 'Custom Mono' };
omc.ownerDocument.fonts.push(face);
const shell = host({ theme: 'light' });
const omcPage = setup({ ancestors: [shell, omc], saved: 'white', cpamp: true });
assert.equal(omcPage.root.dataset.pluginPanel, 'omc', 'the OMC palette takes precedence over compatibility markers');
assert.equal(omcPage.root.dataset.theme, 'dark', 'read resolved mode beyond the intermediate shell');
assert.equal(omcPage.values.get('--panel-bg'), '#121214');
assert.equal(omcPage.values.get('--panel-accent'), '#6ee7a8');
assert.equal(omcPage.values.get('--panel-font-ui'), 'Custom Mono');
assert(omcPage.fonts.has(face));
assert.equal(omcPage.observations.length, 3, 'observe both frames and the local CPAMP marker');
assert.deepEqual(Array.from(omcPage.observations.at(-1).options.attributeFilter), ['data-cpamp-plugin-host'], 'own theme and style writes must not trigger the observer');
omc.dataset.themeMode = 'light';
palette['--bg'] = '#f8f3e8';
palette['--accent'] = '#0f766e';
omcPage.sync();
assert.equal(omcPage.root.dataset.theme, 'light');
assert.equal(omcPage.values.get('--panel-bg'), '#f8f3e8');
assert.equal(omcPage.values.get('--panel-accent'), '#0f766e');
delete palette['--accent'];
omcPage.sync();
assert(!omcPage.values.has('--panel-accent'), 'removed palette tokens fall back to CSS');
delete omc.dataset.themeMode;
omcPage.sync();
assert.equal(omcPage.root.dataset.pluginPanel, 'cpamp');
assert.equal(omcPage.values.size, 0, 'leaving OMC removes all copied appearance tokens');
assert.equal(setup({ ancestors: [host({ mode: 'dark' })] }).root.dataset.pluginPanel, 'cpamc', 'a mode alone does not identify OMC');
assert.equal(setup({ crossOrigin: true, dark: true }).root.dataset.theme, 'dark');
const boundary = setup({ ancestors: [host({ theme: 'light' })], crossOrigin: true, dark: true });
assert.equal(boundary.root.dataset.theme, 'light', 'an inaccessible outer frame must not discard the accessible shell');
const moved = host({ theme: 'dark' });
const movedPage = setup({ ancestors: [moved] });
moved.ownerDocument.defaultView.getComputedStyle = () => { throw new Error('navigated frame'); };
assert.doesNotThrow(movedPage.sync);
assert.equal(movedPage.root.dataset.theme, 'white');
console.log('Theme checks passed: CPAMC, CPAMP, nested OMC, live palette/font changes, storage/system modes and origin boundaries.');
