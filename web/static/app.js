"use strict";

// ---- config -----------------------------------------------------------
const LANES = ["Priority", "Active", "Backburner", "Archived"];
const CHANNELS = ["LinkedIn", "Referral", "Company site", "Recruiter", "Email", "Event", "Other"];
const STATUSES = ["Draft", "Applied", "Screening", "Interviewing", "Offer", "Rejected", "Ghosted"];

let mode = "application";      // "application" | "networking"
let entries = [];             // entries for the current mode
let query = "";               // free-text filter
let dueOnly = false;          // only entries whose follow-up has come due
let sortKey = "";             // "" => newest first (id desc), the server order
let sortAsc = true;

// ---- tiny DOM helpers -------------------------------------------------
const $ = (id) => document.getElementById(id);
const api = async (path, opts = {}) => {
  const res = await fetch(path, { credentials: "same-origin", ...opts });
  if (res.status === 401) { showLogin(); throw new Error("unauthorized"); }
  return res;
};
function toast(msg, isErr = false) {
  const t = $("toast");
  t.textContent = msg;
  t.className = "toast" + (isErr ? " err" : "");
  setTimeout(() => (t.className = "toast hidden"), 2500);
}
function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, (c) =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}
function fillSelect(el, values, selected) {
  el.innerHTML = '<option value=""></option>' +
    values.map((v) => `<option${v === selected ? " selected" : ""}>${esc(v)}</option>`).join("");
}

// ---- session / auth ---------------------------------------------------
async function boot() {
  const res = await fetch("/api/session", { credentials: "same-origin" });
  const s = await res.json();
  if (s.authRequired && !s.authed) { showLogin(); return; }
  if (s.authRequired) $("btn-logout").classList.remove("hidden");
  showApp();
  await load();
  await prefillFromQuery();
}

// ---- capture prefill ---------------------------------------------------
// The bookmarklet (see /capture.html) opens this app with the posting it
// scraped as query params. A top-level navigation still sends the SameSite=Lax
// session cookie, which is why this needs no CORS and no API token.
//
// Values arrive from whatever page the user was on, so they are untrusted:
// they only ever reach input .value (never innerHTML), and nothing is saved
// until the user reviews the form and clicks Save.
const PREFILL_TEXT = ["entity", "context", "comp", "link", "notes"];
const PREFILL_DATES = ["date", "followUp"];
const PREFILL_SELECTS = { lane: LANES, channel: CHANNELS, status: STATUSES };
const PREFILL_MAX = 4000;

async function prefillFromQuery() {
  const q = new URLSearchParams(location.search);
  const kind = q.get("add");
  if (kind !== "application" && kind !== "networking") return;

  if (kind !== mode) {
    mode = kind;
    document.querySelectorAll(".mode").forEach((b) =>
      b.classList.toggle("active", b.dataset.mode === mode));
    await load();
  }

  openEditor(0);
  for (const f of PREFILL_TEXT) {
    const v = q.get(f);
    if (v) $("f-" + f).value = v.slice(0, PREFILL_MAX);
  }
  for (const f of PREFILL_DATES) {
    const v = q.get(f);
    // A date input silently rejects anything that is not yyyy-mm-dd.
    if (v && /^\d{4}-\d{2}-\d{2}$/.test(v)) $("f-" + f).value = v;
  }
  for (const [f, allowed] of Object.entries(PREFILL_SELECTS)) {
    const v = q.get(f);
    if (v && allowed.includes(v)) $("f-" + f).value = v;
  }

  await warnIfDuplicate(q.get("link"));

  // Drop the params so a refresh does not reopen the editor with stale data.
  history.replaceState(null, "", location.pathname);
}
function showLogin() { $("app").classList.add("hidden"); $("login").classList.remove("hidden"); }
function showApp() { $("login").classList.add("hidden"); $("app").classList.remove("hidden"); }

$("login-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const res = await fetch("/api/login", {
    method: "POST", credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password: $("login-password").value }),
  });
  if (res.status === 204) { $("login-error").textContent = ""; boot(); }
  else $("login-error").textContent = "Incorrect password.";
});
$("btn-logout").addEventListener("click", async () => {
  await api("/api/logout", { method: "POST" });
  showLogin();
});

// ---- data load + render ----------------------------------------------
async function load() {
  const res = await api("/api/entries?kind=" + mode);
  entries = await res.json();
  render();
}
// Local midnight, so "due today" means today in the user's timezone rather
// than in UTC. Dates are stored as plain yyyy-mm-dd with no zone.
function today() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

// Due means "on or before today" — a follow-up dated today is due now, not
// tomorrow. Entries with no follow-up date are never due.
function isDue(e) {
  return !!e.followUp && e.followUp <= today();
}

const SEARCH_FIELDS = ["entity", "context", "notes", "status", "lane", "channel", "comp"];

function visibleEntries() {
  let out = entries;

  if (query) {
    const q = query.toLowerCase();
    out = out.filter((e) => SEARCH_FIELDS.some((f) => (e[f] || "").toLowerCase().includes(q)));
  }
  if (dueOnly) out = out.filter(isDue);

  if (sortKey) {
    out = out.slice().sort((a, b) => {
      const av = (a[sortKey] || "").toString().toLowerCase();
      const bv = (b[sortKey] || "").toString().toLowerCase();
      // Blanks sort last in both directions — an empty follow-up date is not
      // "earliest", it is "not set", and it should never top the list.
      if (!av && !bv) return 0;
      if (!av) return 1;
      if (!bv) return -1;
      return sortAsc ? av.localeCompare(bv) : bv.localeCompare(av);
    });
  }
  return out;
}

function render() {
  const tbody = $("rows");
  const rows = visibleEntries();
  const filtering = !!query || dueOnly;

  $("empty").classList.toggle("hidden", entries.length > 0);
  $("no-match").classList.toggle("hidden", !(entries.length > 0 && rows.length === 0 && filtering));

  document.querySelectorAll("th[data-sort]").forEach((th) => {
    const active = th.dataset.sort === sortKey;
    th.classList.toggle("sorted", active);
    th.dataset.dir = active ? (sortAsc ? "asc" : "desc") : "";
  });

  tbody.innerHTML = rows.map(rowHTML).join("");
  tbody.querySelectorAll("[data-edit]").forEach((b) =>
    b.addEventListener("click", () => openEditor(Number(b.dataset.edit))));
  tbody.querySelectorAll("[data-del]").forEach((b) =>
    b.addEventListener("click", () => del(Number(b.dataset.del))));
}
function rowHTML(e) {
  const link = e.link
    ? `<a href="${esc(e.link)}" target="_blank" rel="noopener">open</a>` : "";
  const due = isDue(e);
  return `<tr class="${due ? "due" : ""}">
    <td>${e.lane ? `<span class="pill">${esc(e.lane)}</span>` : ""}</td>
    <td class="col-kindlabel">${mode === "application" ? "Application" : "Contact"}</td>
    <td>${esc(e.entity)}</td>
    <td>${esc(e.context)}</td>
    <td>${esc(e.date)}</td>
    <td>${esc(e.channel)}</td>
    <td>${esc(e.comp)}</td>
    <td>${e.followUp ? `<span class="${due ? "due-pill" : ""}" ${due ? 'title="Follow-up is due"' : ""}>${esc(e.followUp)}</span>` : ""}</td>
    <td>${e.status ? `<span class="pill">${esc(e.status)}</span>` : ""}</td>
    <td>${link}</td>
    <td><div class="notes">${esc(e.notes)}</div></td>
    <td class="row-actions">
      <button data-edit="${e.id}">Edit</button>
      <button data-del="${e.id}">Del</button>
    </td>
  </tr>`;
}

// ---- editor -----------------------------------------------------------
function openEditor(id) {
  const e = id ? entries.find((x) => x.id === id) : {};
  $("editor-title").textContent = id ? "Edit entry" : "New entry";
  $("f-id").value = id || "";
  $("f-entity").value = e.entity || "";
  $("f-context").value = e.context || "";
  $("f-date").value = e.date || "";
  $("f-comp").value = e.comp || "";
  $("f-followUp").value = e.followUp || "";
  $("f-link").value = e.link || "";
  $("f-notes").value = e.notes || "";
  fillSelect($("f-lane"), LANES, e.lane);
  fillSelect($("f-channel"), CHANNELS, e.channel);
  fillSelect($("f-status"), STATUSES, e.status);
  $("editor").classList.remove("hidden");
}
function closeEditor() {
  $("editor").classList.add("hidden");
  $("dupe-warn").classList.add("hidden");
}

// Capturing the same posting twice is easy to do and silently creates a second
// row. Warn rather than block — re-applying to a role, or tracking a second
// round, is legitimate.
async function warnIfDuplicate(link) {
  const warn = $("dupe-warn");
  warn.classList.add("hidden");
  if (!link) return;
  let all = [];
  try {
    all = await (await api("/api/entries")).json();
  } catch (e) {
    return; // never let a failed check get in the way of saving
  }
  const hit = all.find((e) => e.link && e.link === link);
  if (!hit) return;
  warn.textContent =
    `Heads up: you already have an entry for this link — ` +
    `${hit.entity || "(no company)"}${hit.context ? " · " + hit.context : ""}` +
    `${hit.status ? " · " + hit.status : ""}. Saving will add a second one.`;
  warn.classList.remove("hidden");
}

$("entry-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  const id = $("f-id").value;
  const payload = {
    kind: mode,
    lane: $("f-lane").value, entity: $("f-entity").value, context: $("f-context").value,
    date: $("f-date").value, channel: $("f-channel").value, comp: $("f-comp").value,
    followUp: $("f-followUp").value, status: $("f-status").value,
    link: $("f-link").value, notes: $("f-notes").value,
  };
  const res = await api(id ? "/api/entries/" + id : "/api/entries", {
    method: id ? "PUT" : "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  if (res.ok) { closeEditor(); await load(); toast(id ? "Updated" : "Added"); }
  else { const j = await res.json().catch(() => ({})); toast(j.error || "Save failed", true); }
});
// ---- filtering & sorting ----------------------------------------------
$("search").addEventListener("input", (e) => { query = e.target.value.trim(); render(); });
$("due-only").addEventListener("change", (e) => { dueOnly = e.target.checked; render(); });
$("clear-filters").addEventListener("click", (e) => {
  e.preventDefault();
  query = ""; dueOnly = false;
  $("search").value = ""; $("due-only").checked = false;
  render();
});
document.querySelectorAll("th[data-sort]").forEach((th) =>
  th.addEventListener("click", () => {
    const key = th.dataset.sort;
    if (sortKey === key && sortAsc) sortAsc = false;
    else if (sortKey === key) { sortKey = ""; sortAsc = true; }  // third click: back to newest-first
    else { sortKey = key; sortAsc = true; }
    render();
  }));

$("btn-cancel").addEventListener("click", closeEditor);
$("btn-new").addEventListener("click", () => openEditor(0));

async function del(id) {
  if (!confirm("Delete this entry?")) return;
  const res = await api("/api/entries/" + id, { method: "DELETE" });
  if (res.ok) { await load(); toast("Deleted"); }
}

// ---- mode switch ------------------------------------------------------
document.querySelectorAll(".mode").forEach((b) =>
  b.addEventListener("click", async () => {
    document.querySelectorAll(".mode").forEach((x) => x.classList.remove("active"));
    b.classList.add("active");
    mode = b.dataset.mode;
    await load();
  }));

// ---- data menu (export / import / clear) ------------------------------
$("btn-data").addEventListener("click", (e) => {
  e.stopPropagation();
  $("data-menu").classList.toggle("hidden");
});
document.addEventListener("click", () => $("data-menu").classList.add("hidden"));

let pendingImportFormat = null;
$("data-menu").addEventListener("click", async (e) => {
  const act = e.target.dataset.act;
  if (!act) return;
  e.preventDefault();
  if (act === "export-json") download("/api/export?format=json");
  else if (act === "export-csv") download("/api/export?format=csv");
  else if (act === "import-json") { pendingImportFormat = "json"; $("file-input").click(); }
  else if (act === "import-csv") { pendingImportFormat = "csv"; $("file-input").click(); }
  else if (act === "clear") clearView();
});
function download(url) { window.location.href = url; }

// Import wipes both modes, so the warning has to count both — not just the
// entries currently on screen.
async function countAllEntries() {
  try {
    const res = await api("/api/entries");
    return (await res.json()).length;
  } catch (e) {
    return 0; // never block the import on a failed count
  }
}

$("file-input").addEventListener("change", async (ev) => {
  const file = ev.target.files[0];
  if (!file) return;
  const text = await file.text();

  // Import is replace-all across BOTH modes — the server does DELETE FROM
  // entries before inserting. That is fine as a restore-a-snapshot design, but
  // it must never happen because someone expected "import" to mean "add".
  const total = await countAllEntries();
  const warning = total > 0
    ? `Replace ALL data with "${file.name}"?\n\n` +
      `This deletes your ${total} existing ${total === 1 ? "entry" : "entries"} ` +
      `across BOTH Applications and Networking, then loads the file.\n\n` +
      `This cannot be undone. Export a backup first if you are unsure.`
    : `Load "${file.name}" into the tracker?`;
  if (!confirm(warning)) { ev.target.value = ""; return; }

  const res = await api("/api/import?format=" + pendingImportFormat, {
    method: "POST",
    headers: { "Content-Type": pendingImportFormat === "csv" ? "text/csv" : "application/json" },
    body: text,
  });
  ev.target.value = "";
  if (res.ok) { const j = await res.json(); await load(); toast(`Imported ${j.imported} entries`); }
  else { const j = await res.json().catch(() => ({})); toast(j.error || "Import failed", true); }
});

async function clearView() {
  if (!confirm(`Clear all ${mode} entries? This cannot be undone.`)) return;
  const res = await api("/api/clear?kind=" + mode, { method: "POST" });
  if (res.ok) { await load(); toast("Cleared"); }
}

boot();
