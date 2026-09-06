/*
 * Injected (at DocumentReady, main world) into the chatgpt.com page the engine
 * keeps loaded in the background. Everything network-facing happens here, in the
 * real page context, so cookies, Cloudflare clearance, TLS trust and the browser
 * User-Agent are handled by Chromium.
 *
 * window.__dictate:
 *   configure(obj)  override endpoint / VAD settings from Python config
 *   start()         begin microphone capture (+ silence detection if enabled)
 *   stop()          stop capture; kicks off the transcription request
 *   poll()          one call for the driver: {phase, events, result}
 *                     events: e.g. ["autostopped"] (drained each poll)
 *                     result: {ok:true,text} / {ok:false,error} once, else null
 *   take()          legacy single result read (used by diagnostics)
 *   state()         status object for diagnostics
 */
(function () {
  "use strict";
  if (window.__dictate && window.__dictate.__v === 3) return;

  var D = {
    __v: 3,
    cfg: {
      origin: location.origin,
      path: "/backend-api/transcribe",
      field: "file",
      filename: "audio.webm",
      responseKey: "text",
      extraFields: {},
      maxSeconds: 120,
      silenceStop: false,
      silenceMs: 1800,
      silenceMinMs: 700,
      silenceThreshold: 0.02,
    },
    _rec: null,
    _chunks: null,
    _stream: null,
    _result: null,
    _busy: false,
    _settled: false,
    _maxTimer: null,
    _gumTimer: null,
    _lastError: null,
    _phase: "idle",
    _events: [],
    _ac: null,
    _vadTimer: null,
    _trace: [],

    _t: function (msg) {
      this._trace.push((Date.now() % 100000) + " " + msg);
      if (this._trace.length > 40) this._trace.shift();
    },

    configure: function (c) {
      if (c && typeof c === "object") {
        for (var k in c) if (Object.prototype.hasOwnProperty.call(c, k)) this.cfg[k] = c[k];
      }
      return this.state();
    },

    state: function () {
      return {
        v: this.__v,
        host: location.hostname,
        phase: this._phase,
        busy: this._busy,
        recState: this._rec ? this._rec.state : null,
        chunks: this._chunks ? this._chunks.length : 0,
        hasResult: this._result !== null,
        lastError: this._lastError,
        trace: this._trace.slice(-20),
        hasMediaDevices: !!(navigator.mediaDevices && navigator.mediaDevices.getUserMedia),
        hasMediaRecorder: !!window.MediaRecorder,
        secureContext: window.isSecureContext,
      };
    },

    _onChatGPT: function () {
      var h = location.hostname;
      return h === "chatgpt.com" || /\.chatgpt\.com$/.test(h) || h === "chat.openai.com";
    },

    _token: function () {
      return fetch("/api/auth/session", { credentials: "include" })
        .then(function (r) {
          if (!r.ok) throw new Error("session-http-" + r.status);
          return r.json();
        })
        .then(function (j) {
          if (!j || !j.accessToken) {
            var e = new Error("reauth");
            e.code = "reauth";
            throw e;
          }
          return j.accessToken;
        });
    },

    start: function () {
      var self = this;
      self._t("start() busy=" + self._busy);
      self._result = null;
      self._lastError = null;
      self._phase = "starting";
      if (!self._onChatGPT()) {
        self._finalize({ ok: false, error: "reauth" });
        return;
      }
      if (self._busy) return;
      if (!navigator.mediaDevices || !navigator.mediaDevices.getUserMedia) {
        self._finalize({ ok: false, error: "no-getusermedia-api" });
        return;
      }
      self._busy = true;
      self._settled = false;
      var t0 = Date.now();

      self._gumTimer = setTimeout(function () {
        if (self._settled) return;
        self._settled = true;
        self._finalize({
          ok: false,
          error: "mic-timeout-" + (Date.now() - t0) + "ms (grant Microphone to the app in " +
                 "System Settings > Privacy & Security > Microphone, or run the .app bundle)",
        });
      }, 10000);

      self._t("calling getUserMedia");
      navigator.mediaDevices.getUserMedia({ audio: true }).then(
        function (stream) {
          self._t("gUM resolved settled=" + self._settled);
          if (self._settled) { stream.getTracks().forEach(function (t) { t.stop(); }); return; }
          self._settled = true;
          clearTimeout(self._gumTimer);
          self._stream = stream;
          self._chunks = [];
          var mt = "audio/webm;codecs=opus";
          if (!window.MediaRecorder || !MediaRecorder.isTypeSupported(mt)) mt = "audio/webm";
          if (window.MediaRecorder && !MediaRecorder.isTypeSupported(mt)) mt = "";
          try {
            self._rec = mt ? new MediaRecorder(stream, { mimeType: mt })
                           : new MediaRecorder(stream);
          } catch (err) {
            self._finalize({ ok: false, error: "recorder-init:" + (err && err.message || err) });
            return;
          }
          self._rec.ondataavailable = function (e) {
            self._t("ondataavailable size=" + (e.data ? e.data.size : "nil"));
            if (e.data && e.data.size) self._chunks.push(e.data);
          };
          self._rec.onstop = function () { self._t("onstop"); self._finish(); };
          self._rec.onerror = function (e) {
            self._t("onerror");
            self._finalize({ ok: false, error: "recorder-error:" + (e && e.error && e.error.name || e) });
          };
          self._phase = "recording";
          self._rec.start();
          self._t("rec.start() state=" + self._rec.state);
          if (self.cfg.silenceStop) self._startVAD(stream);
          if (self.cfg.maxSeconds) {
            self._maxTimer = setTimeout(function () {
              try { self.stop(); } catch (e) {}
            }, self.cfg.maxSeconds * 1000 + 750);
          }
        },
        function (err) {
          if (self._settled) return;
          self._settled = true;
          clearTimeout(self._gumTimer);
          var name = err && err.name;
          var code = name === "NotAllowedError" ? "no-mic-permission"
                   : name === "NotFoundError" ? "no-mic"
                   : name === "NotReadableError" ? "mic-in-use"
                   : "getusermedia:" + (name || (err && err.message) || err);
          self._finalize({ ok: false, error: code });
        }
      );
    },

    // -- silence detection (Web Audio) ------------------------------------
    _startVAD: function (stream) {
      var self = this;
      try {
        var AC = window.AudioContext || window.webkitAudioContext;
        if (!AC) { self._t("no AudioContext"); return; }
        self._ac = new AC();
        if (self._ac.state === "suspended" && self._ac.resume) self._ac.resume();
        var src = self._ac.createMediaStreamSource(stream);
        var an = self._ac.createAnalyser();
        an.fftSize = 512;
        src.connect(an);
        var buf = new Uint8Array(an.fftSize);
        var startedAt = Date.now();
        var lastLoud = startedAt;
        var heardSpeech = false;
        self._t("VAD started thr=" + self.cfg.silenceThreshold + " gap=" + self.cfg.silenceMs);
        self._vadTimer = setInterval(function () {
          if (!self._rec || self._rec.state !== "recording") return;
          an.getByteTimeDomainData(buf);
          var sum = 0;
          for (var i = 0; i < buf.length; i++) {
            var d = (buf[i] - 128) / 128;
            sum += d * d;
          }
          var rms = Math.sqrt(sum / buf.length);
          var now = Date.now();
          if (rms > self.cfg.silenceThreshold) {
            lastLoud = now;
            heardSpeech = true;
          }
          if (heardSpeech &&
              (now - lastLoud) > self.cfg.silenceMs &&
              (now - startedAt) > self.cfg.silenceMinMs) {
            self._t("silence auto-stop (quiet " + (now - lastLoud) + "ms)");
            self._events.push("autostopped");
            self._stopVAD();
            self.stop();
          }
        }, 100);
      } catch (e) {
        self._t("VAD init fail:" + e);
      }
    },

    _stopVAD: function () {
      if (this._vadTimer) { clearInterval(this._vadTimer); this._vadTimer = null; }
      try { if (this._ac && this._ac.close) this._ac.close(); } catch (e) {}
      this._ac = null;
    },

    stop: function () {
      this._t("stop() recState=" + (this._rec ? this._rec.state : "nil") + " busy=" + this._busy);
      this._phase = "stopping";
      this._stopVAD();
      if (this._maxTimer) { clearTimeout(this._maxTimer); this._maxTimer = null; }
      if (this._gumTimer) { clearTimeout(this._gumTimer); this._gumTimer = null; }
      if (this._rec && this._rec.state !== "inactive") {
        this._rec.stop();
      } else if (!this._busy && this._result === null) {
        this._finalize({ ok: false, error: "not-recording" });
      }
    },

    _finalize: function (res) {
      this._t("_finalize ok=" + res.ok + " err=" + (res.error || ""));
      this._busy = false;
      if (!res.ok) this._lastError = res.error;
      this._result = res;
      this._phase = "idle";
      this._cleanup();
    },

    _finish: function () {
      var self = this;
      self._phase = "transcribing";
      var type = (self._chunks && self._chunks[0] && self._chunks[0].type) || "audio/webm";
      var blob = new Blob(self._chunks || [], { type: type });
      self._t("_finish blobSize=" + blob.size + " chunks=" + (self._chunks ? self._chunks.length : "nil"));
      if (!blob.size) { self._finalize({ ok: false, error: "empty-audio" }); return; }

      self._token()
        .then(function (token) {
          self._t("got token, POSTing " + self.cfg.origin + self.cfg.path);
          var fd = new FormData();
          fd.append(self.cfg.field, blob, self.cfg.filename);
          var ef = self.cfg.extraFields || {};
          for (var k in ef) if (Object.prototype.hasOwnProperty.call(ef, k)) fd.append(k, ef[k]);
          return fetch(self.cfg.origin + self.cfg.path, {
            method: "POST",
            credentials: "include",
            headers: { authorization: "Bearer " + token },
            body: fd,
          });
        })
        .then(function (r) {
          self._t("response " + r.status);
          if (r.status === 401 || r.status === 403) {
            self._finalize({ ok: false, error: "reauth" });
            return;
          }
          var ct = r.headers.get("content-type") || "";
          if (!r.ok) {
            return r.text().then(function (t) {
              self._finalize({ ok: false, error: "http-" + r.status + ":" + (t || "").slice(0, 200) });
            });
          }
          if (ct.indexOf("application/json") !== -1) {
            return r.json().then(function (j) {
              var text = j[self.cfg.responseKey];
              if (typeof text !== "string") text = j.text || j.transcript || j.transcription || "";
              if (typeof text !== "string") text = "";
              self._finalize({ ok: true, text: text });
            });
          }
          return r.text().then(function (t) {
            self._finalize({ ok: true, text: t || "" });
          });
        })
        .catch(function (err) {
          var code = (err && err.code) ? err.code : "request:" + (err && err.message || err);
          self._finalize({ ok: false, error: code });
        });
    },

    _cleanup: function () {
      this._stopVAD();
      try {
        if (this._stream) this._stream.getTracks().forEach(function (t) { t.stop(); });
      } catch (e) {}
      this._stream = null;
      this._rec = null;
      this._chunks = null;
    },

    poll: function () {
      var events = this._events;
      this._events = [];
      var result = this._result;
      if (result !== null) this._result = null;
      return { phase: this._phase, events: events, result: result };
    },

    take: function () {
      if (this._result === null) return null;
      var r = this._result;
      this._result = null;
      return r;
    },
  };

  window.__dictate = D;
})();
