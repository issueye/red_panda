import { normalizeGatewayBase } from './format.js';

function toWebSocketUrl(baseUrl, path = '/api/v1/ws') {
  const base = normalizeGatewayBase(baseUrl);
  const url = base.startsWith('/')
    ? new URL(`${base}${path.startsWith('/') ? path : `/${path}`}`, window.location.origin)
    : new URL(path, `${base}/`);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

export class GatewayWebSocketClient {
  constructor({
    baseUrl,
    token,
    onEvent,
    onStatus,
    onError,
    getResumeCursors,
    reconnectBaseDelay = 500,
    reconnectMaxDelay = 5000,
  }) {
    this.baseUrl = baseUrl;
    this.token = token || '';
    this.onEvent = onEvent;
    this.onStatus = onStatus;
    this.onError = onError;
    this.getResumeCursors = getResumeCursors;
    this.reconnectBaseDelay = reconnectBaseDelay;
    this.reconnectMaxDelay = reconnectMaxDelay;
    this.socket = null;
    this.pending = new Map();
    this.counter = 0;
    this.lastSeen = new Map();
    this.reconnectAttempt = 0;
    this.reconnectTimer = null;
    this.resumeSignature = '';
    this.shouldReconnect = false;
    this.ready = false;
  }

  connect() {
    this.shouldReconnect = true;
    this.clearReconnectTimer();
    this.disposeSocket(new Error('websocket reconnecting'));
    this.openSocket();
  }

  reconnect() {
    this.connect();
  }

  openSocket() {
    this.onStatus?.('connecting');
    const socket = new WebSocket(toWebSocketUrl(this.baseUrl));
    this.socket = socket;
    this.ready = false;
    this.resumeSignature = '';

    socket.addEventListener('open', () => {
      if (this.socket !== socket) return;
      this.send({
        type: 'auth',
        payload: {
          token: this.token,
          client: { kind: 'desktop', name: 'red_panda', version: '0.2.0' },
        },
      })
        .then(() => {
          if (this.socket !== socket) return;
          this.ready = true;
          this.reconnectAttempt = 0;
          this.onStatus?.('connected');
          this.resume().catch((error) => this.onError?.(error));
        })
        .catch((error) => {
          this.onError?.(error);
          socket.close();
        });
    });

    socket.addEventListener('message', (event) => {
      if (this.socket === socket) {
        this.handleMessage(event.data);
      }
    });
    socket.addEventListener('close', () => {
      if (this.socket !== socket) return;
      this.socket = null;
      this.ready = false;
      this.rejectPending(new Error('websocket closed'));
      this.onStatus?.('disconnected');
      this.scheduleReconnect();
    });
    socket.addEventListener('error', () => {
      if (this.socket === socket) {
        this.onStatus?.('error');
      }
    });
  }

  close() {
    this.shouldReconnect = false;
    this.clearReconnectTimer();
    this.disposeSocket(new Error('websocket closed'));
  }

  disposeSocket(error) {
    const socket = this.socket;
    this.socket = null;
    this.ready = false;
    if (socket) {
      socket.close();
    }
    this.rejectPending(error);
  }

  rejectPending(error) {
    for (const item of this.pending.values()) {
      item.reject(error);
    }
    this.pending.clear();
  }

  clearReconnectTimer() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  scheduleReconnect() {
    if (!this.shouldReconnect || this.reconnectTimer) {
      return;
    }
    const delay = Math.min(
      this.reconnectBaseDelay * (2 ** this.reconnectAttempt),
      this.reconnectMaxDelay,
    );
    this.reconnectAttempt += 1;
    this.onStatus?.('reconnecting');
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.shouldReconnect) {
        this.openSocket();
      }
    }, delay);
  }

  send(message) {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      return Promise.reject(new Error('websocket is not connected'));
    }

    const id = message.id || `msg_${Date.now()}_${++this.counter}`;
    const envelope = { ...message, id };

    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      this.socket.send(JSON.stringify(envelope));
    });
  }

  request(method, payload) {
    return this.send({ type: 'request', method, payload });
  }

  resume() {
    if (!this.ready) {
      return Promise.resolve(null);
    }
    const cursors = { ...(this.getResumeCursors?.() || {}) };
    for (const [runId, runSeq] of this.lastSeen.entries()) {
      cursors[runId] = Math.max(Number(cursors[runId]) || 0, runSeq);
    }
    const lastSeen = Object.fromEntries(
      Object.entries(cursors)
        .filter(([runId]) => Boolean(runId))
        .map(([runId, runSeq]) => [runId, Number(runSeq) || 0])
        .sort(([left], [right]) => left.localeCompare(right)),
    );
    if (Object.keys(lastSeen).length === 0) {
      return Promise.resolve(null);
    }
    const signature = JSON.stringify(lastSeen);
    if (signature === this.resumeSignature) {
      return Promise.resolve(null);
    }
    this.resumeSignature = signature;
    return this.request('run.resume', { last_seen: lastSeen }).catch((error) => {
      this.resumeSignature = '';
      throw error;
    });
  }

  handleMessage(raw) {
    let message;
    try {
      message = JSON.parse(raw);
    } catch (error) {
      this.onError?.(error);
      return;
    }

    if (message.type === 'event') {
      const runId = message.payload?.run_id || message.meta?.run_id;
      const runSeq = Number(message.payload?.run_seq || message.meta?.run_seq || 0);
      if (runId && runSeq > 0) {
        this.lastSeen.set(runId, Math.max(this.lastSeen.get(runId) || 0, runSeq));
      }
      this.onEvent?.(message);
      return;
    }

    const pending = message.id ? this.pending.get(message.id) : null;
    if (pending) {
      this.pending.delete(message.id);
      if (message.type === 'error' || message.ok === false) {
        pending.reject(new Error(message.error?.message || 'gateway error'));
      } else {
        pending.resolve(message.payload ?? null);
      }
    }
  }
}
