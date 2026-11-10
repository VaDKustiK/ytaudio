"use strict";
const $ = (selector) => document.querySelector(selector);
const token = $('meta[name="ytaudio-token"]').content;
let settings, diagnostics, releases, submitting = false, polling = false, selectedPage = "downloader";
const rows = new Map();
const descriptions = {
  mp3: "Best-quality VBR MP3. Plays almost everywhere; converting adds a lossy encoding step.",
  wav: "Uncompressed audio. No further lossy encoding, but much larger files. Does not restore quality YouTube removed.",
  flac: "Lossless encoding of YouTube’s decoded audio. Larger files; no added quality over the YouTube source.",
  ogg: "High-quality OGG Vorbis audio. A lossy conversion, with broad support in open-source players."
};
async function api(path, method = "GET", body) {
  const options = {method, headers: {"X-Ytaudio-Token": token}};
  if (body !== undefined) { options.headers["Content-Type"] = "application/json"; options.body = JSON.stringify(body); }
  const response = await fetch(path, options);
  if (!response.ok) throw new Error((await response.text()).trim() || "The operation failed. Try again.");
  return response.status === 204 ? null : response.json();
}
function feedback(selector, message, error = false) { const node = $(selector); node.textContent = message; node.classList.toggle("error", error); }
function showPage(page) {
  if (!["downloader", "settings", "info"].includes(page)) page = "downloader";
  selectedPage = page;
  document.querySelectorAll(".page").forEach(node => { node.hidden = node.id !== `page-${page}`; });
  document.querySelectorAll("[data-page]").forEach(button => {
    const active = button.dataset.page === page;
    button.classList.toggle("active", active);
    if (active) button.setAttribute("aria-current", "page"); else button.removeAttribute("aria-current");
  });
}
document.querySelectorAll("[data-page]").forEach(button => button.addEventListener("click", () => showPage(button.dataset.page)));
document.querySelectorAll("[data-goto]").forEach(button => button.addEventListener("click", () => showPage(button.dataset.goto)));
$(".brand").addEventListener("click", event => { event.preventDefault(); showPage("downloader"); });
function formatHelp() { $("#format-help").textContent = descriptions[$("#format").value]; }
$("#format").addEventListener("change", formatHelp);
$("#paste-url").addEventListener("click", async () => {
  const button = $("#paste-url"), field = $("#url");
  button.disabled = true;
  try {
    const text = window.runtime?.ClipboardGetText
      ? await window.runtime.ClipboardGetText()
      : await navigator.clipboard.readText();
    if (!text.trim()) { feedback("#download-message", "Clipboard is empty.", true); return; }
    field.value = text.trim();
    field.dispatchEvent(new Event("input", {bubbles: true}));
    feedback("#download-message", "");
  } catch {
    feedback("#download-message", "Clipboard access is unavailable. Paste into the link field using your keyboard.", true);
  } finally {
    button.disabled = false;
    field.focus();
  }
});
function applySettings(data) {
  settings = data;
  $("#output-folder").value = data.output_folder;
  $("#default-format").value = data.format;
  $("#proxy").value = data.proxy;
  $("#daily-cap").value = data.daily_cap;
  $("#format").value = data.format;
  $("#destination").textContent = data.output_folder;
  formatHelp();
}
$("#settings-form").addEventListener("submit", async event => {
  event.preventDefault(); $("#save-settings").disabled = true;
  try {
    const data = await api("/api/settings", "PUT", {...settings, output_folder: $("#output-folder").value.trim(), format: $("#default-format").value, proxy: $("#proxy").value.trim(), daily_cap: Number($("#daily-cap").value)});
    applySettings(data); feedback("#settings-message", "Preferences saved.");
  } catch (error) { feedback("#settings-message", error.message, true); }
  finally { $("#save-settings").disabled = false; }
});
$("#choose-folder").addEventListener("click", async () => {
  $("#choose-folder").disabled = true;
  try { const data = await api("/api/pick-folder", "POST"); if (data.folder) $("#output-folder").value = data.folder; }
  catch (error) { feedback("#settings-message", error.message, true); }
  finally { $("#choose-folder").disabled = false; }
});
$("#open-folder").addEventListener("click", async () => {
  try { await api("/api/open-folder", "POST"); } catch (error) { feedback("#download-message", error.message, true); }
});
function renderDiagnostics(data) {
  diagnostics = data;
  $("#app-version").textContent = data.version === "dev" ? "Development build" : data.version;
  $("#diagnostics").textContent = JSON.stringify(data, null, 2);
  $("#setup-message").textContent = data.setup.error || data.setup.message;
  $("#setup-message").classList.toggle("error", Boolean(data.setup.error));
  $("#setup-progress").hidden = !data.setup.running;
  if (data.setup.percent > 0) $("#setup-progress").value = data.setup.percent; else $("#setup-progress").removeAttribute("value");
  $("#tools-badge").textContent = data.setup.running ? "Working…" : data.ready ? "Ready" : "Setup needed";
  $("#setup-button").disabled = data.setup.running;
  $("#update-tools").disabled = data.setup.running || !data.ready;
  $("#download-button").disabled = submitting || data.setup.running || !data.ready;
  $("#setup-banner").hidden = data.ready && !data.setup.running;
  $("#setup-banner-text").textContent = data.setup.running ? data.setup.message : "A quick setup is needed before your first download.";
  const list = $("#dependencies"); list.replaceChildren();
  for (const dep of data.dependencies || []) {
    const li = document.createElement("li"), name = document.createElement("strong"), version = document.createElement("span");
    name.textContent = dep.name;
    version.textContent = dep.ready ? dep.version : dep.error;
    li.append(name, version); list.append(li);
  }
}
function renderJobs(jobs) {
  $("#queue-count").textContent = jobs.filter(j => ["queued", "running"].includes(j.state)).length;
  const ids = new Set(jobs.map(j => j.id));
  for (const [id, row] of rows) if (!ids.has(id)) { row.remove(); rows.delete(id); }
  $("#jobs .empty-state")?.remove();
  if (!jobs.length) {
    const empty = document.createElement("div"); empty.className = "empty-state";
    const icon = document.createElement("span"); icon.className = "empty-icon"; icon.textContent = "♫";
    const text = document.createElement("p"); text.textContent = "Your next listen starts here.";
    const help = document.createElement("span"); help.className = "small muted"; help.textContent = "Downloads will appear here while this app is open.";
    empty.append(icon, text, help); $("#jobs").append(empty);
  }
  for (const job of jobs) {
    let row = rows.get(job.id);
    if (!row) {
      row = document.createElement("article");
      const icon = document.createElement("span"); icon.className = "job-icon";
      const body = document.createElement("div"); body.className = "job-body";
      const title = document.createElement("p"); title.className = "job-title";
      const message = document.createElement("p"); message.className = "job-message";
      const progress = document.createElement("progress"); progress.max = 100; progress.setAttribute("aria-label", "Download progress");
      const cancel = document.createElement("button"); cancel.className = "text-button";
      cancel.addEventListener("click", async () => {
        cancel.disabled = true;
        try { await api(`/api/jobs/${job.id}`, "DELETE"); await refresh(); }
        catch (error) { feedback("#download-message", error.message, true); }
        finally { cancel.disabled = false; }
      });
      body.append(title, message, progress); row.append(icon, body, cancel); rows.set(job.id, row); $("#jobs").append(row);
    }
    row.className = `job ${job.state}`;
    row.querySelector(".job-icon").textContent = job.format.toUpperCase();
    row.querySelector(".job-title").textContent = job.progress.file || job.url;
    const active = ["queued", "running"].includes(job.state);
    const detail = job.progress.stage === "downloading" ? ` · ${Math.round(job.progress.percent)}%${job.progress.speed ? ` · ${job.progress.speed}` : ""}${job.progress.eta ? ` · ${job.progress.eta} left` : ""}` : "";
    row.querySelector(".job-message").textContent = job.message + (active ? detail : "");
    const progress = row.querySelector("progress"); progress.hidden = job.state !== "running";
    if (job.progress.stage === "downloading" || job.progress.stage === "saved") progress.value = job.progress.percent; else progress.removeAttribute("value");
    row.querySelector("button").textContent = active ? "Cancel" : "Dismiss";
    row.querySelector("button").setAttribute("aria-label", `${active ? "Cancel" : "Dismiss"} ${job.format} download`);
  }
}
async function refresh() {
  if (polling) return; polling = true;
  try {
    const [data, jobs] = await Promise.all([api("/api/diagnostics"), api("/api/jobs")]);
    renderDiagnostics(data); renderJobs(jobs);
    if (!releases || releases.checking) renderReleases(await api("/api/app-update"));
  } catch { feedback("#download-message", "Connection to the app was interrupted. Reconnecting…", true); }
  finally { polling = false; }
}
async function prepareTools(updateOnly) {
  $("#setup-button").disabled = true; $("#update-tools").disabled = true;
  try { await api(updateOnly ? "/api/check-updates" : "/api/setup", "POST"); await refresh(); }
  catch (error) { feedback("#setup-message", error.message, true); $("#setup-button").disabled = false; $("#update-tools").disabled = false; }
}
$("#setup-button").addEventListener("click", () => prepareTools(false));
$("#update-tools").addEventListener("click", () => prepareTools(true));
async function confirmPlaylist() {
  const dialog = $("#playlist-dialog"); dialog.showModal();
  return new Promise(resolve => {
    const done = result => { dialog.close(); $("#playlist-yes").onclick = null; $("#playlist-no").onclick = null; dialog.oncancel = null; resolve(result); };
    $("#playlist-yes").onclick = () => done(true); $("#playlist-no").onclick = () => done(false); dialog.oncancel = event => { event.preventDefault(); done(false); };
  });
}
$("#download-form").addEventListener("submit", async event => {
  event.preventDefault(); if (submitting) return; submitting = true; $("#download-button").disabled = true;
  try {
    const body = {url: $("#url").value.trim(), format: $("#format").value, confirm_playlist: false};
    if (new URL(body.url).searchParams.has("list")) { if (!(await confirmPlaylist())) return; body.confirm_playlist = true; }
    await api("/api/download", "POST", body); $("#url").value = ""; feedback("#download-message", "Added to your download queue."); await refresh();
  } catch (error) { feedback("#download-message", error.message, true); }
  finally { submitting = false; $("#download-button").disabled = !diagnostics?.ready || diagnostics?.setup.running; }
});
function toggleNotifications(open) {
  $("#notifications").hidden = !open; $("#bell").setAttribute("aria-expanded", String(open));
  if (open && releases?.latest) { try { localStorage.setItem("seen-release", releases.latest); } catch {} $("#notification-count").hidden = true; }
}
$("#bell").addEventListener("click", () => toggleNotifications($("#notifications").hidden));
$("#close-notifications").addEventListener("click", () => { toggleNotifications(false); $("#bell").focus(); });
document.addEventListener("click", event => { if (!event.target.closest(".notifications-wrap")) toggleNotifications(false); });
document.addEventListener("keydown", event => { if (event.key === "Escape" && !$("#notifications").hidden) { toggleNotifications(false); $("#bell").focus(); } });
async function openRelease(url) {
  try { await api("/api/open-release", "POST", {url}); }
  catch { window.open(url, "_blank", "noopener,noreferrer"); }
}
function renderReleases(data) {
  releases = data;
  $("#check-app-update").hidden = data.current === "dev";
  const message = data.checking ? "Checking releases…" : data.error || (data.ready ? `Version ${data.latest} is available.` : data.current === "dev" ? "Development build. App update checks are available in release builds." : data.latest ? "You’re up to date." : "No published releases yet.");
  $("#app-update-message").textContent = message;
  $("#latest-release").hidden = !data.url;
  $("#latest-release").onclick = () => openRelease(data.url);
  const list = $("#notification-list"); list.replaceChildren();
  if (!data.notifications?.length) { const text = document.createElement("p"); text.className = "muted"; text.textContent = message; list.append(text); }
  for (const item of data.notifications || []) {
    const button = document.createElement("button"); button.className = "notification-item";
    const title = document.createElement("strong"), date = document.createElement("span"), notes = document.createElement("span");
    title.textContent = item.title; date.textContent = `${item.version} · ${item.date ? new Date(item.date).toLocaleDateString() : "New release"}`;
    notes.textContent = item.notes || "See what’s new and download the updated app.";
    button.append(title, date, notes); button.addEventListener("click", () => openRelease(item.url)); list.append(button);
  }
  let seen; try { seen = localStorage.getItem("seen-release"); } catch {}
  $("#notification-count").textContent = data.notifications?.length || 0;
  $("#notification-count").hidden = !data.ready || seen === data.latest;
}
$("#check-app-update").addEventListener("click", async () => {
  $("#check-app-update").disabled = true;
  try { renderReleases(await api("/api/app-update", "POST")); }
  catch (error) { $("#app-update-message").textContent = error.message; }
  finally { $("#check-app-update").disabled = false; }
});
$("#copy-diagnostics").addEventListener("click", async () => {
  try {
    const text = $("#diagnostics").textContent;
    if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text);
    else { const field = document.createElement("textarea"); field.value = text; field.className = "clipboard-helper"; document.body.append(field); field.select(); const copied = document.execCommand("copy"); field.remove(); if (!copied) throw new Error(); }
    feedback("#copy-message", "Copied");
  } catch { feedback("#copy-message", "Clipboard unavailable. Open Technical details and copy the text.", true); }
});
async function init() {
  formatHelp();
  try { applySettings(await api("/api/settings")); }
  catch (error) { feedback("#download-message", error.message, true); }
  await refresh(); setInterval(refresh, 1000);
}
init();
