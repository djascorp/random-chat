import { getT } from '../i18n';
import { S2C, type Envelope } from '../types';

type Handler = (data: any, envelope: Envelope) => void;
type OpenHandler = () => void;
type CloseHandler = (event: CloseEvent) => void;

/**
 * Minimal typed WebSocket client for the signaling protocol.
 *
 * Reconnection is deliberately not automatic: the hook owns the call lifecycle
 * and decides whether a dropped socket should resume a search or end the call.
 */
export class SignalingClient {
  private socket: WebSocket | null = null;
  private readonly handlers = new Map<string, Set<Handler>>();
  private readonly openHandlers = new Set<OpenHandler>();
  private readonly closeHandlers = new Set<CloseHandler>();
  private closedByUs = false;

  constructor(private readonly url: string) {}

  get isOpen(): boolean {
    return this.socket?.readyState === WebSocket.OPEN;
  }

  connect(): void {
    if (this.socket && this.socket.readyState <= WebSocket.OPEN) {
      return;
    }
    this.closedByUs = false;
    const socket = new WebSocket(this.url);
    this.socket = socket;

    socket.onopen = () => {
      for (const handler of this.openHandlers) handler();
    };

    socket.onmessage = (message: MessageEvent<string>) => {
      let envelope: Envelope;
      try {
        envelope = JSON.parse(message.data) as Envelope;
      } catch {
        return;
      }
      if (!envelope || typeof envelope.event !== 'string') return;
      this.dispatch(envelope.event, envelope.data, envelope);
    };

    socket.onclose = (event: CloseEvent) => {
      this.socket = null;
      for (const handler of this.closeHandlers) handler(event);
    };

    socket.onerror = () => {
      // The close handler always follows, so recovery is handled in one place.
    };
  }

  /** Sends an event, silently dropping it when the socket is not writable. */
  send(event: string, data?: unknown): boolean {
    if (!this.isOpen) return false;
    const payload: Envelope = data === undefined ? { event } : { event, data };
    try {
      this.socket!.send(JSON.stringify(payload));
      return true;
    } catch {
      return false;
    }
  }

  /** Registers a handler and returns an unsubscribe function. */
  on(event: string, handler: Handler): () => void {
    let set = this.handlers.get(event);
    if (!set) {
      set = new Set();
      this.handlers.set(event, set);
    }
    set.add(handler);
    return () => set!.delete(handler);
  }

  onOpen(handler: OpenHandler): () => void {
    this.openHandlers.add(handler);
    return () => this.openHandlers.delete(handler);
  }

  onClose(handler: CloseHandler): () => void {
    this.closeHandlers.add(handler);
    return () => this.closeHandlers.delete(handler);
  }

  /**
   * Waits for the next occurrence of an event.
   *
   * Also rejects as soon as the socket closes: without that, a handshake the
   * server refused (403 on a rejected origin, for instance) would look like a
   * silent timeout ten seconds later.
   */
  waitFor<T = any>(event: string, timeoutMs = 8000): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      let timer = 0;
      const finish = (settle: () => void) => {
        window.clearTimeout(timer);
        unsubscribe();
        unsubscribeClose();
        settle();
      };

      const unsubscribe = this.on(event, (data: T) => finish(() => resolve(data)));
      const unsubscribeClose = this.onClose((closeEvent) => {
        finish(() =>
          reject(
            new Error(
              getT()('signal.closedBefore', { event, code: closeEvent.code || 1006 }),
            ),
          ),
        );
      });
      timer = window.setTimeout(
        () => finish(() => reject(new Error(`délai dépassé en attendant « ${event} »`))),
        timeoutMs,
      );
    });
  }

  close(code = 1000, reason = 'client_close'): void {
    this.closedByUs = true;
    const socket = this.socket;
    this.socket = null;
    if (socket && socket.readyState <= WebSocket.OPEN) {
      socket.close(code, reason);
    }
  }

  get wasClosedByUs(): boolean {
    return this.closedByUs;
  }

  private dispatch(event: string, data: unknown, envelope: Envelope): void {
    const set = this.handlers.get(event);
    if (set) {
      for (const handler of [...set]) handler(data, envelope);
    }
    // Protocol errors are surfaced as toasts, so notify catch-all listeners too.
    if (event === S2C.error) {
      const wildcard = this.handlers.get('*');
      if (wildcard) {
        for (const handler of [...wildcard]) handler(data, envelope);
      }
    }
  }
}

/** Builds the signaling URL for the current page, honouring a Vite proxy. */
export function signalingUrl(clientId: string): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  const host = window.location.host;
  return `${protocol}//${host}/ws?cid=${encodeURIComponent(clientId)}`;
}

const CLIENT_ID_KEY = 'random-chat.client-id';
const CLIENT_ID_PATTERN = /^[a-z0-9-]{8,64}$/;

/**
 * Returns the browser-persisted pseudo-anonymous identifier.
 *
 * The identifier is what makes blocks and suspensions survive a reload without
 * asking the visitor to create an account. It is client-controlled, so the
 * server treats it as untrusted and rate limits on the network origin as well.
 */
export function persistentClientId(): string {
  try {
    const stored = window.localStorage.getItem(CLIENT_ID_KEY);
    if (stored && CLIENT_ID_PATTERN.test(stored)) return stored;
    const generated = `guest-${randomToken(24)}`;
    window.localStorage.setItem(CLIENT_ID_KEY, generated);
    return generated;
  } catch {
    // Private browsing may deny storage access; fall back to a per-page id.
    return `guest-${randomToken(24)}`;
  }
}

function randomToken(bytes: number): string {
  const buffer = new Uint8Array(bytes);
  crypto.getRandomValues(buffer);
  return Array.from(buffer, (b) => (b % 36).toString(36)).join('');
}
