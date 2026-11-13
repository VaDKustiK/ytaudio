"use strict";
let selectedAudio = null, analysisState = null, audioSelection = 0, analysisCopy = "", pendingAnalysisID = null;
let selectedAnalysisID = null, requestSelection = null, analysisError = "";
function analysisFailure(message) {
  analysisError = message; feedback("#analysis-message", message, true);
}
function selectAnalysisFile(file, start = false) {
  if (!file) return;
  ++audioSelection; selectedAudio = file; pendingAnalysisID = null; showPage("analyzer");
  selectedAnalysisID = null; requestSelection = null; analysisError = "";
  $("#audio-name").textContent = file.name;
  $("#analysis-results").hidden = true; $("#copy-analysis").hidden = true;
  feedback("#analysis-message", "");
  $("#analyze-audio").disabled = !analysisState?.ready || analysisState?.installing;
  if (start) startAnalysis();
}
async function startAnalysis() {
  if (!selectedAudio) return;
  if (!analysisState?.ready) {
    pendingAnalysisID = selectedAudio.id;
    feedback("#analysis-message", "Install the analyzer to analyze this file.");
    return;
  }
  pendingAnalysisID = null;
  const selection = audioSelection;
  selectedAnalysisID = null; requestSelection = selection; analysisError = "";
  $("#analyze-audio").disabled = true;
  $("#analysis-results").hidden = true; $("#copy-analysis").hidden = true;
  try {
    const state = await api("/api/analysis", "POST", {file_id: selectedAudio.id});
    if (selection === audioSelection) {
      selectedAnalysisID = state.id; requestSelection = null; renderAnalysis(state);
    }
  } catch (error) {
    if (selection === audioSelection) {
      requestSelection = null; analysisFailure(error.message);
      $("#analyze-audio").disabled = !analysisState?.ready;
    }
  }
}
function renderAnalysis(state) {
  analysisState = state;
  if (state.ready && !state.installing && !state.install_error && pendingAnalysisID && pendingAnalysisID === selectedAudio?.id) {
    pendingAnalysisID = null; Promise.resolve().then(startAnalysis);
  }
  $("#install-analyzer").hidden = state.ready && !state.install_error;
  $("#install-analyzer").disabled = state.installing;
  $("#install-analyzer").textContent = state.installing ? "Installing…" : state.ready ? "Repair analyzer" : "Install analyzer";
  const belongs = selectedAudio && state.file_id === selectedAudio.id && state.id === selectedAnalysisID;
  const busy = ["queued", "running"].includes(state.status);
  $("#cancel-analysis").hidden = !busy;
  $("#analyze-audio").disabled = !selectedAudio || !state.ready || state.installing || requestSelection === audioSelection || (belongs && busy);
  if (!state.ready || state.installing || state.install_error) {
    feedback("#analysis-message", analysisError || state.install_error || (state.installing ? "Installing analyzer…" : "Install the analyzer once to analyze audio offline."), Boolean(analysisError || state.install_error));
  }
  // An older request must not replace results for a newly selected file.
  if (!belongs) {
    $("#analysis-progress").hidden = true;
    if (analysisError) feedback("#analysis-message", analysisError, true);
    return;
  }
  feedback("#analysis-message", analysisError || state.install_error || state.error || (state.status === "complete" ? "" : state.stage), Boolean(analysisError || state.install_error || state.error));
  $("#analysis-progress").hidden = !busy; $("#analysis-progress").value = state.percent;
  $("#analysis-results").hidden = !state.result; $("#copy-analysis").hidden = !state.result;
  if (!state.result) return;
  const r = state.result;
  // Prefer the faster interpretation before rounding (62.5 -> 125, not 126).
  const bpm = r.tempo.bpm == null ? null : Math.max(r.tempo.bpm, r.tempo.alternate_bpm ?? r.tempo.bpm);
  const tempo = bpm == null ? (r.tempo.variable ? "Variable" : "Unknown") : `${Math.round(bpm)} BPM`;
  const key = r.key.tonic ? `${r.key.tonic} ${r.key.scale}` : "Unknown";
  const tuning = r.tuning.status === "standard" ? "440 Hz (0 cents)" : r.tuning.status === "detuned"
    ? `${r.tuning.reference_hz.toFixed(1)} Hz (${r.tuning.offset_cents > 0 ? "+" : ""}${r.tuning.offset_cents.toFixed(1)} cents)`
    : r.tuning.status === "mixed" ? "Mixed tuning" : "Unknown";
  $("#analysis-tempo").textContent = tempo;
  $("#analysis-key").textContent = key;
  $("#analysis-tuning").textContent = tuning;
  $("#analysis-key-alt").textContent = r.key.status === "uncertain" ? "Uncertain key" : "";
  $("#analysis-tempo-alt").textContent = r.tempo.variable && bpm != null ? "Tempo varies" : bpm != null && r.tempo.confidence < .7 ? "Uncertain tempo" : "";
  analysisCopy = `Tempo: ${tempo}${r.tempo.bpm != null && r.tempo.variable ? " (tempo varies)" : r.tempo.bpm != null && r.tempo.confidence < .7 ? " (uncertain)" : ""}\nKey: ${key}${r.key.status === "uncertain" ? " (uncertain)" : ""}\nTuning: ${tuning}`;
}
async function refreshAnalysis() {
  try { renderAnalysis(await api("/api/analysis")); }
  catch { if (selectedPage === "analyzer") feedback("#analysis-message", "Unable to reach the analyzer. Try again.", true); }
}
async function chooseAudio() {
  if (!window.runtime) { $("#audio-file").click(); return; }
  try { selectAnalysisFile(await api("/api/analysis/pick", "POST")); }
  catch (error) { analysisFailure(error.message); }
}
async function uploadAudio(file) {
  if (!file) return;
  if (file.size > 256 * 1024 * 1024) { analysisFailure("Browser file limit is 256 MB. Use the desktop app for larger files."); return; }
  const selection = ++audioSelection;
  feedback("#analysis-message", "Reading audio…");
  try {
    const response = await fetch(`/api/analysis/upload?name=${encodeURIComponent(file.name)}`, {method: "POST", headers: {"X-Ytaudio-Token": token}, body: file});
    if (!response.ok) throw new Error((await response.text()).trim());
    const selected = await response.json(); if (selection === audioSelection) selectAnalysisFile(selected);
  } catch (error) { if (selection === audioSelection) analysisFailure(error.message); }
}
$("#choose-audio").addEventListener("click", event => {event.stopPropagation(); chooseAudio();});
$("#audio-drop").addEventListener("click", chooseAudio);
$("#audio-drop").addEventListener("keydown", event => {if (event.target === $("#audio-drop") && ["Enter", " "].includes(event.key)) {event.preventDefault(); chooseAudio();}});
$("#audio-file").addEventListener("change", event => {uploadAudio(event.target.files[0]); event.target.value = "";});
$("#analyze-audio").addEventListener("click", startAnalysis);
$("#cancel-analysis").addEventListener("click", async () => {try {await api("/api/analysis", "DELETE"); await refreshAnalysis();} catch (error) {feedback("#analysis-message", error.message, true);}});
$("#install-analyzer").addEventListener("click", async () => {try {renderAnalysis(await api("/api/analysis/install", "POST"));} catch (error) {feedback("#analysis-message", error.message, true);}});
$("#copy-analysis").addEventListener("click", async () => {
  try { if (window.runtime?.ClipboardSetText) await window.runtime.ClipboardSetText(analysisCopy); else await navigator.clipboard.writeText(analysisCopy); feedback("#analysis-message", "Copied"); }
  catch {feedback("#analysis-message", "Unable to copy results.", true);}
});
const drop = $("#audio-drop");
drop.addEventListener("dragover", event => {event.preventDefault(); drop.classList.add("drag-active");});
drop.addEventListener("dragleave", () => drop.classList.remove("drag-active"));
drop.addEventListener("drop", event => {
  event.preventDefault(); drop.classList.remove("drag-active");
  if (window.runtime) return; // Wails provides native paths without copying audio.
  if (event.dataTransfer.files.length !== 1) {analysisFailure("Drop one audio file at a time."); return;}
  uploadAudio(event.dataTransfer.files[0]);
});
if (window.runtime?.OnFileDrop) {
  window.runtime.OnFileDrop(async (x, y, paths) => {
    showPage("analyzer");
    if (paths.length !== 1) { analysisFailure("Drop one audio file at a time."); return; }
    const selection = ++audioSelection;
    try { const file = await api("/api/analysis/select", "POST", {path: paths[0]}); if (selection === audioSelection) selectAnalysisFile(file); }
    catch (error) { if (selection === audioSelection) analysisFailure(error.message); }
  }, true);
}
