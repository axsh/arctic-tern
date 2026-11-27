// Plain Example front-end for features/voice_demo.
//
// Two layers (keep them visually separate in the UI):
//   1) OpenAI Realtime = low-latency conversation audio (mic/speaker).
//   2) Tern CAWA       = Coding Agent execution (messages / interrupt).
//
// Routing (auto/always/never) decides whether a transcript goes to CAWA.
// Context bridge injects short CAWA summaries into the Realtime conversation.
// On turn complete, inject a reconsider note (A)+(C) and run (B); do not force
// a spoken readout of agent status. (D) answers the human only when (B) chooses.

const LONG_TASK_SAMPLE =
  "Please run a long shell command that sleeps for about 60 seconds, then create a file named long_task_done.txt with the text done. Narrate briefly only.";

const MODE_PARAMS = {
  natural: { fade_ms: 180, stop_label: "soft_vad+fade" },
  abrupt: { fade_ms: 0, stop_label: "abrupt_cut" },
};

const els = {
  start: document.querySelector('[data-testid="btn-start"]'),
  end: document.querySelector('[data-testid="btn-end"]'),
  sample: document.querySelector('[data-testid="btn-sample-long"]'),
  sendTern: document.querySelector('[data-testid="btn-send-tern"]'),
  mode: document.querySelector('[data-testid="bargein-mode"]'),
  stopLabel: document.querySelector('[data-testid="bargein-stop-label"]'),
  routeMode: document.querySelector('[data-testid="cawa-route-mode"]'),
  bridge: document.querySelector('[data-testid="context-bridge"]'),
  voice: document.querySelector('[data-testid="realtime-voice"]'),
  speed: document.querySelector('[data-testid="realtime-speed"]'),
  outputApplied: document.querySelector('[data-testid="realtime-output-applied"]'),
  speedSlow: document.querySelector('[data-testid="speed-preset-slow"]'),
  speedNormal: document.querySelector('[data-testid="speed-preset-normal"]'),
  speedFast: document.querySelector('[data-testid="speed-preset-fast"]'),
  vadSilence: document.querySelector('[data-testid="vad-silence-ms"]'),
  vadThreshold: document.querySelector('[data-testid="vad-threshold"]'),
  vadPrefix: document.querySelector('[data-testid="vad-prefix-ms"]'),
  vadApplied: document.querySelector('[data-testid="vad-applied"]'),
  vadResponsive: document.querySelector('[data-testid="vad-preset-responsive"]'),
  vadPatient: document.querySelector('[data-testid="vad-preset-patient"]'),
  realtime: document.querySelector('[data-testid="status-realtime"]'),
  cawa: document.querySelector('[data-testid="status-cawa"]'),
  action: document.querySelector('[data-testid="last-action"]'),
  interruptMode: document.querySelector('[data-testid="interrupt-mode"]'),
  summary: document.querySelector('[data-testid="status-summary"]'),
  transcript: document.querySelector('[data-testid="transcript"]'),
};

let pc = null;
let dc = null;
let statusTimer = null;
let localStream = null;
let remoteAudio = null;
let fadeMs = MODE_PARAMS.natural.fade_ms;
let callActive = false;
let lastYouLine = "";
let lastBridgedSummary = "";
let lastCawaBusy = false;
let realtimeResponseActive = false;
let resumeLoggedForResponse = false;

function logLine(line) {
  els.transcript.textContent += line + "\n";
  els.transcript.scrollTop = els.transcript.scrollHeight;
}

function setRealtimeStatus(s) {
  els.realtime.textContent = s;
}

function setCawaStatus(s) {
  els.cawa.textContent = s;
}

function syncModeLabel() {
  const mode = els.mode.value || "natural";
  const p = MODE_PARAMS[mode] || MODE_PARAMS.natural;
  fadeMs = p.fade_ms;
  els.stopLabel.textContent = p.stop_label;
}

els.mode.addEventListener("change", syncModeLabel);
syncModeLabel();

function readVADFromUI() {
  return {
    silence_duration_ms: Number(els.vadSilence.value),
    threshold: Number(els.vadThreshold.value),
    prefix_padding_ms: Number(els.vadPrefix.value),
  };
}

function setVADInputs(silenceMs, threshold, prefixMs) {
  els.vadSilence.value = String(silenceMs);
  els.vadThreshold.value = String(threshold);
  els.vadPrefix.value = String(prefixMs);
}

function showAppliedVAD(vad) {
  if (!vad) return;
  const line =
    "silence=" + vad.silence_duration_ms +
    "ms threshold=" + vad.threshold +
    " prefix=" + vad.prefix_padding_ms + "ms";
  els.vadApplied.textContent = line;
  logLine("realtime> vad " + line);
}

function showAppliedOutput(out) {
  if (!out) return;
  const line = "voice=" + out.voice + " speed=" + out.speed;
  els.outputApplied.textContent = line;
  logLine("realtime> output " + line);
}

els.vadResponsive.addEventListener("click", () => setVADInputs(500, 0.5, 300));
els.vadPatient.addEventListener("click", () => setVADInputs(1200, 0.5, 300));
els.speedSlow.addEventListener("click", () => { els.speed.value = "0.8"; });
els.speedNormal.addEventListener("click", () => { els.speed.value = "1"; });
els.speedFast.addEventListener("click", () => { els.speed.value = "1.25"; });

async function loadVoices() {
  try {
    const resp = await fetch("/api/realtime/voices");
    const data = await resp.json();
    if (!data.ok || !Array.isArray(data.voices)) return;
    const cur = els.voice.value || data.default || "marin";
    els.voice.innerHTML = "";
    for (const v of data.voices) {
      const opt = document.createElement("option");
      opt.value = v.id;
      opt.textContent = v.recommended ? v.label + " (recommended)" : v.label;
      if (v.id === cur) opt.selected = true;
      els.voice.appendChild(opt);
    }
  } catch (_) {
    // keep fallback marin option from HTML
  }
}

loadVoices();

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

function requestRealtimeReconsider() {
  if (!dc || dc.readyState !== "open") return;
  // Start a Realtime turn so (B) can choose speak / silence / more Tern work.
  // This is not a forced readout of coding-agent status.
  cancelActiveRealtimeResponse();
  try {
    dc.send(JSON.stringify({ type: "response.create" }));
    logLine("bridge> reconsider (turn done)");
  } catch (err) {
    logLine("bridge-error> " + err.message);
  }
}

// Inject a Tern note into Realtime. reconsider=true only when a CAWA turn has
// just finished (busy→idle); mid-turn progress stays silent by design.
function maybeBridgeSummary(summary, bridgeNote, opts) {
  if (!els.bridge.checked || !dc || dc.readyState !== "open") return;
  const s = (summary || "").trim();
  if (!s) return;
  const reconsider = !!(opts && opts.reconsider);
  const isNew = s !== lastBridgedSummary;
  if (!isNew && !reconsider) return;

  if (isNew || reconsider) {
    lastBridgedSummary = s;
    const note = (bridgeNote && String(bridgeNote).trim()) ||
      ("Tern coding agent status: " + s);
    const ev = {
      type: "conversation.item.create",
      item: {
        type: "message",
        role: "user",
        content: [{ type: "input_text", text: note }],
      },
    };
    try {
      dc.send(JSON.stringify(ev));
      logLine(reconsider ? "bridge> reconsider note" : "bridge> tern status: " + s);
    } catch (err) {
      logLine("bridge-error> " + err.message);
      return;
    }
  }
  if (reconsider) requestRealtimeReconsider();
}

async function refreshCawaStatus() {
  const resp = await fetch("/api/status");
  const data = await resp.json();
  const wasBusy = lastCawaBusy;
  const nowBusy = !!data.busy;
  lastCawaBusy = nowBusy;

  if (!callActive && !data.session_id) {
    setCawaStatus("no session");
  } else if (data.busy) {
    setCawaStatus("busy");
  } else if (data.session_id) {
    setCawaStatus("idle");
  } else {
    setCawaStatus("no session");
  }
  els.action.textContent = data.last_action || "—";
  els.interruptMode.textContent = data.interrupt_mode || "—";
  els.summary.textContent = data.last_summary || "—";
  // Reconsider only on busy→idle (turn completed), not on every status poll.
  const turnJustFinished = wasBusy && !nowBusy && !!data.session_id;
  if (data.last_summary) {
    if (turnJustFinished) {
      maybeBridgeSummary(
        data.last_summary,
        data.reconsider_note || data.bridge_note,
        { reconsider: true }
      );
    } else {
      maybeBridgeSummary(data.last_summary, data.bridge_note, { reconsider: false });
    }
  }
}

async function sendUtterance(text, routeOverride) {
  const trimmed = (text || "").trim();
  if (!trimmed) return;
  lastYouLine = trimmed;
  logLine("you> " + trimmed);
  const route_mode = routeOverride || els.routeMode.value || "auto";
  const data = await postJSON("/api/utterance", { text: trimmed, route_mode });
  if (data.sent === false) {
    logLine("route> skip cawa (" + (data.reason || "skip") + ")");
  } else {
    logLine("cawa> action=" + data.action + " summary=" + (data.summary || "") + " reason=" + (data.reason || ""));
    maybeBridgeSummary(data.summary || data.last_summary, data.bridge_note, { reconsider: false });
  }
  await refreshCawaStatus();
}

function softenRemoteAudioOnBargeIn() {
  if (!remoteAudio) return;
  if (!fadeMs || fadeMs <= 0) {
    remoteAudio.volume = 0;
    return;
  }
  const start = remoteAudio.volume || 1;
  const steps = 8;
  const stepMs = Math.max(16, Math.floor(fadeMs / steps));
  let i = 0;
  const timer = setInterval(() => {
    i += 1;
    remoteAudio.volume = Math.max(0, start * (1 - i / steps));
    if (i >= steps) clearInterval(timer);
  }, stepMs);
}

function ensureRemoteAudioPlaying(logResume) {
  if (!remoteAudio) return;
  remoteAudio.volume = 1;
  const p = remoteAudio.play();
  if (p && typeof p.catch === "function") {
    p.catch(() => {});
  }
  if (logResume && !resumeLoggedForResponse) {
    resumeLoggedForResponse = true;
    logLine("realtime> resume playback");
  }
}

function cancelActiveRealtimeResponse() {
  if (!dc || dc.readyState !== "open") return;
  if (!realtimeResponseActive) return;
  try {
    dc.send(JSON.stringify({ type: "response.cancel" }));
  } catch (_) {}
}

async function connectRealtime() {
  setRealtimeStatus("connecting");
  const mode = els.mode.value || "natural";
  const vadReq = readVADFromUI();
  const wrapped = await postJSON("/api/realtime/session", {
    barge_in_mode: mode,
    silence_duration_ms: vadReq.silence_duration_ms,
    threshold: vadReq.threshold,
    prefix_padding_ms: vadReq.prefix_padding_ms,
    voice: els.voice.value || "marin",
    speed: Number(els.speed.value) || 1,
  });
  if (wrapped.barge_in) {
    fadeMs = wrapped.barge_in.fade_ms || 0;
    els.stopLabel.textContent = wrapped.barge_in.stop_label || els.stopLabel.textContent;
  }
  if (wrapped.vad) showAppliedVAD(wrapped.vad);
  if (wrapped.output) showAppliedOutput(wrapped.output);
  const secret = wrapped.openai || wrapped;
  const ephemeral =
    secret.value ||
    (secret.client_secret && secret.client_secret.value) ||
    "";
  if (!ephemeral) {
    throw new Error("realtime client_secrets response missing value (ek_...)");
  }

  pc = new RTCPeerConnection();
  localStream = await navigator.mediaDevices.getUserMedia({ audio: true });
  for (const track of localStream.getAudioTracks()) {
    pc.addTrack(track, localStream);
  }

  remoteAudio = document.createElement("audio");
  remoteAudio.autoplay = true;
  document.body.appendChild(remoteAudio);
  pc.ontrack = (ev) => {
    remoteAudio.srcObject = ev.streams[0];
    setRealtimeStatus("speaking");
  };

  realtimeResponseActive = false;
  resumeLoggedForResponse = false;
  dc = pc.createDataChannel("oai-events");
  dc.onmessage = (ev) => {
    let msg;
    try {
      msg = JSON.parse(ev.data);
    } catch {
      return;
    }
    if (msg.type === "input_audio_buffer.speech_started") {
      softenRemoteAudioOnBargeIn();
      cancelActiveRealtimeResponse();
      setRealtimeStatus("connected");
    }
    if (msg.type === "conversation.item.input_audio_transcription.completed") {
      const transcript = msg.transcript || "";
      sendUtterance(transcript).catch((err) => logLine("error> " + err.message));
    }
    if (msg.type === "response.created") {
      realtimeResponseActive = true;
      resumeLoggedForResponse = false;
      ensureRemoteAudioPlaying(true);
      setRealtimeStatus("speaking");
    }
    if (msg.type === "output_audio_buffer.started") {
      ensureRemoteAudioPlaying(false);
      setRealtimeStatus("speaking");
    }
    if (
      msg.type === "response.done" ||
      msg.type === "response.cancelled" ||
      msg.type === "output_audio_buffer.stopped"
    ) {
      if (msg.type === "response.done" || msg.type === "response.cancelled") {
        realtimeResponseActive = false;
      }
      if (els.realtime.textContent === "speaking") setRealtimeStatus("connected");
    }
    if (msg.type === "error") {
      const err = msg.error || msg;
      logLine("realtime-error> " + JSON.stringify(err));
      if (err && err.code === "conversation_already_has_active_response") {
        cancelActiveRealtimeResponse();
        maybeBridgeSummary("Realtime: active response conflict");
      }
    }
  };

  const offer = await pc.createOffer();
  await pc.setLocalDescription(offer);

  const sdpResp = await fetch("https://api.openai.com/v1/realtime/calls", {
    method: "POST",
    body: offer.sdp,
    headers: {
      Authorization: "Bearer " + ephemeral,
      "Content-Type": "application/sdp",
    },
  });
  if (!sdpResp.ok) {
    const errText = await sdpResp.text();
    throw new Error("OpenAI SDP exchange failed: " + sdpResp.status + " " + errText);
  }
  const answer = await sdpResp.text();
  await pc.setRemoteDescription({ type: "answer", sdp: answer });
  setRealtimeStatus("connected");
  logLine("realtime> connected (mode=" + mode + ")");
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
  if (remoteAudio) {
    try { remoteAudio.pause(); } catch (_) {}
    remoteAudio.remove();
    remoteAudio = null;
  }
  realtimeResponseActive = false;
}

els.start.addEventListener("click", async () => {
  els.start.disabled = true;
  try {
    await postJSON("/api/call/start", {});
    callActive = true;
    lastBridgedSummary = "";
    lastCawaBusy = false;
    setCawaStatus("idle");
    logLine("cawa> session started");
    try {
      await connectRealtime();
    } catch (err) {
      setRealtimeStatus("unavailable");
      logLine("realtime> unavailable: " + err.message);
      logLine("system> use the long-task button to drive CAWA without mic");
    }
    els.end.disabled = false;
    els.sample.disabled = false;
    els.sendTern.disabled = false;
    statusTimer = setInterval(() => {
      refreshCawaStatus().catch(() => {});
    }, 1000);
    await refreshCawaStatus();
  } catch (err) {
    logLine("error> " + err.message);
    setCawaStatus("no session");
    els.start.disabled = false;
  }
});

els.end.addEventListener("click", async () => {
  stopRealtime();
  callActive = false;
  try {
    await postJSON("/api/call/end", {});
    logLine("cawa> call ended");
  } catch (err) {
    logLine("error> " + err.message);
  }
  setRealtimeStatus("disconnected");
  setCawaStatus("no session");
  els.start.disabled = false;
  els.end.disabled = true;
  els.sample.disabled = true;
  els.sendTern.disabled = true;
  await refreshCawaStatus();
});

els.sample.addEventListener("click", async () => {
  try {
    await sendUtterance(LONG_TASK_SAMPLE, "always");
  } catch (err) {
    logLine("error> " + err.message);
  }
});

els.sendTern.addEventListener("click", async () => {
  try {
    if (!lastYouLine) {
      logLine("system> no last user line to send");
      return;
    }
    await sendUtterance(lastYouLine, "always");
  } catch (err) {
    logLine("error> " + err.message);
  }
});
