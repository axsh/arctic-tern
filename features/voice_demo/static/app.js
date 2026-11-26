// Plain Example front-end for features/voice_demo.
//
// Two layers (keep them visually separate in the UI):
//   1) OpenAI Realtime = low-latency conversation audio (mic/speaker).
//   2) Tern CAWA       = Coding Agent execution (messages / interrupt).
//
// Why CAWA interrupt stays immediate even in "natural" mode:
//   natural only softens how assistant *audio* stops. Execution steering
//   must still call Interrupt as soon as a busy-turn utterance is finalized.

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
  mode: document.querySelector('[data-testid="bargein-mode"]'),
  stopLabel: document.querySelector('[data-testid="bargein-stop-label"]'),
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

els.vadResponsive.addEventListener("click", () => setVADInputs(500, 0.5, 300));
els.vadPatient.addEventListener("click", () => setVADInputs(1200, 0.5, 300));

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

async function refreshCawaStatus() {
  const resp = await fetch("/api/status");
  const data = await resp.json();
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
}

async function sendUtterance(text) {
  const trimmed = (text || "").trim();
  if (!trimmed) return;
  logLine("you> " + trimmed);
  const data = await postJSON("/api/utterance", { text: trimmed });
  logLine("cawa> action=" + data.action + " summary=" + (data.summary || ""));
  await refreshCawaStatus();
}

// Soften assistant playback on barge-in without pausing the <audio> element.
// pause() leaves the element stopped; later Realtime audio never becomes audible
// until play() runs again — which we do in ensureRemoteAudioPlaying.
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

function ensureRemoteAudioPlaying() {
  if (!remoteAudio) return;
  remoteAudio.volume = 1;
  const p = remoteAudio.play();
  if (p && typeof p.catch === "function") {
    p.catch(() => {});
  }
  logLine("realtime> resume playback");
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
  });
  if (wrapped.barge_in) {
    fadeMs = wrapped.barge_in.fade_ms || 0;
    els.stopLabel.textContent = wrapped.barge_in.stop_label || els.stopLabel.textContent;
  }
  if (wrapped.vad) {
    showAppliedVAD(wrapped.vad);
  }
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

  dc = pc.createDataChannel("oai-events");
  dc.onmessage = (ev) => {
    let msg;
    try {
      msg = JSON.parse(ev.data);
    } catch {
      return;
    }
    if (msg.type === "input_audio_buffer.speech_started") {
      // Soften assistant playback on barge-in (natural fades; abrupt mutes).
      // Do not pause() — that permanently silences later assistant audio.
      softenRemoteAudioOnBargeIn();
      setRealtimeStatus("connected");
    }
    if (msg.type === "conversation.item.input_audio_transcription.completed") {
      const transcript = msg.transcript || "";
      sendUtterance(transcript).catch((err) => logLine("error> " + err.message));
    }
    if (msg.type === "response.created" || msg.type === "output_audio_buffer.started") {
      ensureRemoteAudioPlaying();
      setRealtimeStatus("speaking");
    }
    if (msg.type === "response.done" || msg.type === "output_audio_buffer.stopped") {
      if (els.realtime.textContent === "speaking") setRealtimeStatus("connected");
    }
    if (msg.type === "error") {
      logLine("realtime-error> " + JSON.stringify(msg.error || msg));
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
}

els.start.addEventListener("click", async () => {
  els.start.disabled = true;
  try {
    await postJSON("/api/call/start", {});
    callActive = true;
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
  await refreshCawaStatus();
});

els.sample.addEventListener("click", async () => {
  try {
    await sendUtterance(LONG_TASK_SAMPLE);
  } catch (err) {
    logLine("error> " + err.message);
  }
});
