// Plain Example front-end for features/voice_demo.
//
// Split of roles:
//   1) OpenAI Realtime (WebRTC in the browser) = low-latency conversation audio.
//   2) This demo's Go server + Tern CAWA = Coding Agent execution authority.
// When you finish speaking, we POST the transcript to /api/utterance.
// The server chooses messages (idle) or interrupt (busy). It must not cancel.

const LONG_TASK_SAMPLE =
  "Please run a long shell command that sleeps for about 60 seconds, then create a file named long_task_done.txt with the text done. Narrate briefly only.";

const els = {
  start: document.querySelector('[data-testid="btn-start"]'),
  end: document.querySelector('[data-testid="btn-end"]'),
  sample: document.querySelector('[data-testid="btn-sample-long"]'),
  busy: document.querySelector('[data-testid="status-busy"]'),
  action: document.querySelector('[data-testid="last-action"]'),
  mode: document.querySelector('[data-testid="interrupt-mode"]'),
  summary: document.querySelector('[data-testid="status-summary"]'),
  transcript: document.querySelector('[data-testid="transcript"]'),
};

let pc = null;
let dc = null;
let statusTimer = null;
let localStream = null;

function logLine(line) {
  els.transcript.textContent += line + "\n";
  els.transcript.scrollTop = els.transcript.scrollHeight;
}

async function postJSON(path, body) {
  const resp = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body || {}),
  });
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) {
    throw new Error(data.error || path + " failed HTTP " + resp.status);
  }
  return data;
}

async function refreshStatus() {
  const resp = await fetch("/api/status");
  const data = await resp.json();
  els.busy.textContent = data.busy ? "yes" : "no";
  els.action.textContent = data.last_action || "—";
  els.mode.textContent = data.interrupt_mode || "—";
  els.summary.textContent = data.last_summary || "—";
}

async function sendUtterance(text) {
  const trimmed = (text || "").trim();
  if (!trimmed) return;
  logLine("you> " + trimmed);
  const data = await postJSON("/api/utterance", { text: trimmed });
  logLine("cawa> action=" + data.action + " summary=" + (data.summary || ""));
  await refreshStatus();
}

// OpenAI Realtime WebRTC with an ephemeral key from our server.
// Official pattern: mint session server-side, then POST the browser SDP offer
// to api.openai.com with Authorization: Bearer <ephemeral>.
async function connectRealtime() {
  const session = await postJSON("/api/realtime/session", {});
  const ephemeral =
    (session.client_secret && session.client_secret.value) ||
    session.value ||
    "";
  if (!ephemeral) {
    throw new Error("realtime session response missing client_secret.value");
  }
  const model = session.model || "gpt-4o-realtime-preview";

  pc = new RTCPeerConnection();
  localStream = await navigator.mediaDevices.getUserMedia({ audio: true });
  for (const track of localStream.getAudioTracks()) {
    pc.addTrack(track, localStream);
  }

  // Remote audio (Realtime speaking back) — attach to a hidden audio element.
  const audioEl = document.createElement("audio");
  audioEl.autoplay = true;
  document.body.appendChild(audioEl);
  pc.ontrack = (ev) => {
    audioEl.srcObject = ev.streams[0];
  };

  dc = pc.createDataChannel("oai-events");
  dc.onmessage = (ev) => {
    let msg;
    try {
      msg = JSON.parse(ev.data);
    } catch {
      return;
    }
    // Final user transcript → hand to CAWA policy on the demo server.
    if (msg.type === "conversation.item.input_audio_transcription.completed") {
      const transcript = msg.transcript || "";
      sendUtterance(transcript).catch((err) => logLine("error> " + err.message));
    }
    if (msg.type === "error") {
      logLine("realtime-error> " + JSON.stringify(msg.error || msg));
    }
  };

  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);

  const baseUrl = "https://api.openai.com/v1/realtime";
  const sdpResp = await fetch(baseUrl + "?model=" + encodeURIComponent(model), {
    method: "POST",
    body: offer.sdp,
    headers: {
      Authorization: "Bearer " + ephemeral,
      "Content-Type": "application/sdp",
      "OpenAI-Beta": "realtime=v1",
    },
  });
  if (!sdpResp.ok) {
    const errText = await sdpResp.text();
    throw new Error("OpenAI SDP exchange failed: " + sdpResp.status + " " + errText);
  }
  const answer = await sdpResp.text();
  await pc.setRemoteDescription({ type: "answer", sdp: answer });
  logLine("system> realtime connected");
}

function stopRealtime() {
  if (statusTimer) {
    clearInterval(statusTimer);
    statusTimer = null;
  }
  if (dc) {
    try { dc.close(); } catch (_) {}
    dc = null;
  }
  if (pc) {
    try { pc.close(); } catch (_) {}
    pc = null;
  }
  if (localStream) {
    for (const t of localStream.getTracks()) t.stop();
    localStream = null;
  }
}

els.start.addEventListener("click", async () => {
  els.start.disabled = true;
  try {
    await postJSON("/api/call/start", {});
    logLine("system> CAWA session started");
    // Realtime needs OPENAI_API_KEY on the server. If missing, you can still
    // exercise interrupt via the sample long-task button.
    try {
      await connectRealtime();
    } catch (err) {
      logLine("system> realtime unavailable: " + err.message);
      logLine("system> use the long-task button to drive CAWA without mic");
    }
    els.end.disabled = false;
    els.sample.disabled = false;
    statusTimer = setInterval(() => {
      refreshStatus().catch(() => {});
    }, 1000);
    await refreshStatus();
  } catch (err) {
    logLine("error> " + err.message);
    els.start.disabled = false;
  }
});

els.end.addEventListener("click", async () => {
  stopRealtime();
  try {
    await postJSON("/api/call/end", {});
    logLine("system> call ended");
  } catch (err) {
    logLine("error> " + err.message);
  }
  els.start.disabled = false;
  els.end.disabled = true;
  els.sample.disabled = true;
  await refreshStatus();
});

els.sample.addEventListener("click", async () => {
  try {
    await sendUtterance(LONG_TASK_SAMPLE);
  } catch (err) {
    logLine("error> " + err.message);
  }
});
