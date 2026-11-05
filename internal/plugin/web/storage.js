"use strict";
const KEY_STORE = "cpa-codex-candy-eval.key";
const VIEW_STORE = "cpa-codex-candy-eval.view.v1.";

function stored(name) {
  try { return JSON.parse(localStorage.getItem(name)); } catch (_) { return null; }
}
function store(name, value) {
  try { localStorage.setItem(name, JSON.stringify(value)); } catch (_) {}
}
function preference(name, fallback) {
  const value = stored(VIEW_STORE + name);
  return value !== null && typeof value === typeof fallback ? value : fallback;
}
function savePreference(name, value) {
  store(VIEW_STORE + name, value);
}
