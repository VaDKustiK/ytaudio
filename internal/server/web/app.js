const ytaudioToken = document.querySelector('meta[name="ytaudio-token"]').content;
const setupMessage = document.querySelector("#setup-message");
const setupButton = document.querySelector("#setup-button");
const setupProgress = document.querySelector("#setup-progress");
const setupDetails = document.querySelector("#setup-details");
const diagnostics = document.querySelector("#diagnostics");
const form = document.querySelector("#download-form");
const downloadMessage = document.querySelector("#download-message");
const downloadProgress = document.querySelector("#download-progress");
const cancelButton = document.querySelector("#cancel-button");
const historyList = document.querySelector("#history");
let activeJob;
let events;

async function getDiagnostics() {
  const response = await fetch("/api/diagnostics");
  if (!response.ok) throw new Error("Unable to read diagnostics");
  return response.json();
}

function render(data) {
  setupMessage.textContent = data.setup.error || data.setup.message || "Dependencies are ready.";
  setupButton.disabled = data.setup.running;
  setupProgress.value = data.setup.percent || 0;
  setupDetails.textContent = data.setup.total
    ? `${formatBytes(data.setup.bytes)} of ${formatBytes(data.setup.total)}`
    : "";
  diagnostics.textContent = JSON.stringify(data, null, 2);
}

function formatBytes(bytes) {
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

async function refresh() {
  try {
    render(await getDiagnostics());
    const historyResponse = await fetch("/api/history");
    const history = await historyResponse.json();
    historyList.replaceChildren();
    if (!history.length) {
      const empty = document.createElement("li");
      empty.className = "muted";
      empty.textContent = "No completed downloads.";
      historyList.append(empty);
    } else {
      history.slice().reverse().forEach((item) => {
        const entry = document.createElement("li");
        entry.textContent = `${item.status}: ${item.url} (${item.format})`;
        historyList.append(entry);
      });
    }
  } catch (error) {
    setupMessage.textContent = error.message;
  }
}

setupButton.addEventListener("click", async () => {
  setupButton.disabled = true;
  setupMessage.textContent = "Starting dependency setup...";
  try {
    const response = await fetch("/api/setup", {
      method: "POST",
      headers: {"Origin": window.location.origin, "X-Ytaudio-Token": ytaudioToken}
    });
    if (!response.ok) throw new Error(await response.text());
    await refresh();
  } catch (error) {
    setupMessage.textContent = error.message || "Unable to start dependency setup.";
    setupButton.disabled = false;
  }
});
document.querySelector("#refresh-button").addEventListener("click", refresh);
document.querySelector("#copy-button").addEventListener("click", async () => {
  try {
    await navigator.clipboard.writeText(diagnostics.textContent);
  } catch {
    setupMessage.textContent = "Clipboard access is unavailable.";
  }
});
form.addEventListener("submit", async (event) => {
  event.preventDefault();
  await startDownload(false);
});
async function startDownload(confirmPlaylist) {
  downloadMessage.textContent = "Starting download...";
  const response = await fetch("/api/download", {
    method: "POST",
    headers: {"Content-Type": "application/json", "Origin": window.location.origin, "X-Ytaudio-Token": ytaudioToken},
    body: JSON.stringify({url: form.url.value, format: form.format.value, confirm_playlist: confirmPlaylist})
  });
  if (!response.ok) {
    const message = await response.text();
    if (response.status === 409 && message.includes("playlist") && !confirmPlaylist && window.confirm(`${message}\nContinue?`)) {
      await startDownload(true);
      return;
    }
    downloadMessage.textContent = message;
    return;
  }
  activeJob = (await response.json()).id;
  downloadProgress.hidden = false;
  cancelButton.hidden = false;
  events = new EventSource(`/api/jobs/${activeJob}`);
  events.addEventListener("progress", (message) => {
    const progress = JSON.parse(message.data);
    downloadProgress.value = progress.percent;
    downloadMessage.textContent = `${progress.stage}: ${progress.percent}% ${progress.speed || ""} ${progress.eta || ""}`;
  });
  events.addEventListener("complete", () => finish("Download complete."));
  events.addEventListener("error", (message) => {
    if (message.data) finish(JSON.parse(message.data).error || "Download failed.");
  });
}
cancelButton.addEventListener("click", async () => {
  if (!activeJob) return;
  await fetch(`/api/jobs/${activeJob}`, {method: "DELETE", headers: {"Origin": window.location.origin, "X-Ytaudio-Token": ytaudioToken}});
  finish("Download cancelled.");
});
function finish(message) {
  downloadMessage.textContent = message;
  cancelButton.hidden = true;
  if (events) events.close();
  activeJob = undefined;
}
refresh();
setInterval(refresh, 2000);
