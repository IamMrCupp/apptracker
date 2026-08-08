"use strict";

// ---- config -----------------------------------------------------------
const LANES = ["Priority", "Active", "Backburner", "Archived"];
const CHANNELS = ["LinkedIn", "Referral", "Company site", "Recruiter", "Email", "Event", "Other"];
const STATUSES = ["Draft", "Applied", "Screening", "Interviewing", "Offer", "Rejected", "Ghosted"];

let mode = "application";      // "application" | "networking"
let entries = [];             // entries for the current mode

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
function render() {
  const tbody = $("rows");
  $("empty").classList.toggle("hidden", entries.length > 0);
  tbody.innerHTML = entries.map(rowHTML).join("");
  tbody.querySelectorAll("[data-edit]").forEach((b) =>
    b.addEventListener("click", () => openEditor(Number(b.dataset.edit))));
  tbody.querySelectorAll("[data-del]").forEach((b) =>
    b.addEventListener("click", () => del(Number(b.dataset.del))));
}
function rowHTML(e) {
  const link = e.link
    ? `<a href="${esc(e.link)}" target="_blank" rel="noopener">open</a>` : "";
  return `<tr>
    <td>${e.lane ? `<span class="pill">${esc(e.lane)}</span>` : ""}</td>
    <td class="col-kindlabel">${mode === "application" ? "Application" : "Contact"}</td>
    <td>${esc(e.entity)}</td>
    <td>${esc(e.context)}</td>
    <td>${esc(e.date)}</td>
    <td>${esc(e.channel)}</td>
    <td>${esc(e.comp)}</td>
    <td>${esc(e.followUp)}</td>
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
function closeEditor() { $("editor").classList.add("hidden"); }

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

$("file-input").addEventListener("change", async (ev) => {
  const file = ev.target.files[0];
  if (!file) return;
  const text = await file.text();
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
