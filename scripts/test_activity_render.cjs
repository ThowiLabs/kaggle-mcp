// Regression test for the browser's activity renderer without external packages.
// This feeds the same lowercase JSON names emitted by projectstate.Event.
"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");

class FakeNode {
  constructor(tag = "") {
    this.tag = tag;
    this.children = [];
    this.textContent = "";
    this.className = "";
    this.attributes = {};
    this.classList = { add() {}, remove() {} };
  }
  append(child) { this.children.push(child); }
  replaceChildren(fragment) { this.children = fragment.children || []; }
  addEventListener() {}
  setAttribute(name, value) { this.attributes[name] = value; }
}
const ids = new Map([
  "activityEntries", "activityPagination", "activityLiveStatus",
  "activityTotal", "activityChecked", "activityPageInfo",
  "liveLabel", "connectionStatus", "liveIndicator",
].map(id => [id, new FakeNode("div")]));
const json = {
  items: [{
    project: "demo",
    description: "Compilación terminada",
    command: "go test ./...",
    status: "ok",
    when: "2026-10-11T21:05:00Z",
  }],
  page: 1, page_size: 20, total: 1, total_pages: 1, has_prev: false,
  has_next: false, page_numbers: [1], checked_at: "2026-10-11T21:05:01Z",
};
let fetchedUrl = "";
const context = {
  document: {
    getElementById(id) { return ids.get(id) || null; },
    createElement(tag) { return new FakeNode(tag); },
    createDocumentFragment() { return new FakeNode("fragment"); },
    querySelector() { return new FakeNode(); },
    addEventListener() {},
    hidden: false,
  },
  window: {
    location: { search: "", origin: "https://example.test", assign() { throw new Error("Unexpected login"); } },
    history: { replaceState() {}, pushState() {} },
    addEventListener() {},
  },
  fetch: async url => {
    fetchedUrl = url;
    return { ok: true, status: 200, redirected: false, json: async () => json };
  },
  setInterval(fn, ms) { assert.equal(ms, 5000); return 1; },
  URLSearchParams, URL, Date, Intl, JSON, Number, String, Array,
};
const source = fs.readFileSync(path.join(__dirname, "../internal/dashboard/ui/activity.js"), "utf8");
context.console = console;
vm.runInNewContext(source, context, { filename: "activity.js" });

// The initial fetch and JSON parsing are async. Flush the microtask queue.
setImmediate(() => {
  try {
    assert.equal(fetchedUrl, "/api/activity?page=1");
    const rows = ids.get("activityEntries").children;
    assert.equal(rows.length, 1);
    const row = rows[0];
    assert.equal(row.children[0].textContent, "ok");
    assert.equal(row.children[1].children[0].textContent, "Compilación terminada");
    assert.match(row.children[1].children[1].textContent, /demo/);
    assert.doesNotMatch(row.children[1].children[1].textContent, /Fecha no disponible/);
    assert.equal(row.children[1].children[2].textContent, "go test ./...");
    assert.equal(ids.get("activityTotal").textContent, "1 registro");
    assert.equal(ids.get("activityLiveStatus").textContent, "En vivo · cada 5 s");
    console.log("PASS: activity.js renders actual lowercase JSON fields");
  } catch (err) {
    console.error(err);
    process.exitCode = 1;
  }
});
