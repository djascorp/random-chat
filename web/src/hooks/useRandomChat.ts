import { getT } from '../i18n';
import { useCallback, useEffect, useRef, useState } from 'react';
import {
  C2S,
  S2C,
  type CallStatus,
  type ChatMessage,
  type ICEPayload,
  type IncomingChatMessage,
  type InviteCreated,
  type InviteRevoked,
  type MatchFound,
  type PeerJoined,
  type PeerLeft,
  type PeerStats,
  type PublicStats,
  type ServerErrorPayload,
  type ServerHello,
  type SessionEnded,
  type SessionMode,
  type SessionUpdated,
  type SDPPayload,
  type Toast,
} from '../types';

export interface RemotePeer {
  id: string;
  stream: MediaStream | null;
}
import { SignalingClient, persistentClientId, signalingUrl } from '../lib/signaling';
import { RtcPeer } from '../lib/webrtc';

const STATS_POLL_MS = 3000;
const AUTO_REQUEUE_DELAY_MS = 1400;
const CONSENT_KEY = 'random-chat.consent-v1';

interface DeviceLists {
  cameras: MediaDeviceInfo[];
  microphones: MediaDeviceInfo[];
}

/**
 * Owns the whole client-side session: media capture, the WebRTC peer, the
 * signaling socket and the status machine the UI renders from.
 *
 * The mutable pieces live in refs because the socket handlers are registered
 * once per connection while the React closures change on every render.
 */
export function useRandomChat() {
  const t = getT();
  const [status, setStatus] = useState<CallStatus>('idle');
  const [statusDetail, setStatusDetail] = useState('');
  const [consentAccepted, setConsentAccepted] = useState<boolean>(() => readConsent());
  const [hello, setHello] = useState<ServerHello | null>(null);
  const [publicStats, setPublicStats] = useState<PublicStats | null>(null);
  const [localStream, setLocalStream] = useState<MediaStream | null>(null);
  // One entry per remote participant, in server join order minus ourselves.
  const [remotePeers, setRemotePeers] = useState<RemotePeer[]>([]);
  const [myId, setMyId] = useState('');
  const [members, setMembers] = useState<string[]>([]);
  const [sessionMode, setSessionMode] = useState<SessionMode>('solo');
  const [sessionMax, setSessionMax] = useState(0);
  // Search prefs chosen on the landing screen; reused by Next.
  const [searchMode, setSearchMode] = useState<SessionMode>('solo');
  const [searchMax, setSearchMax] = useState(0);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [micEnabled, setMicEnabled] = useState(true);
  const [camEnabled, setCamEnabled] = useState(true);
  const [devices, setDevices] = useState<DeviceLists>({ cameras: [], microphones: [] });
  const [selectedCamera, setSelectedCamera] = useState<string | null>(null);
  const [selectedMicrophone, setSelectedMicrophone] = useState<string | null>(null);
  const [quality, setQuality] = useState<PeerStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [sessionSeconds, setSessionSeconds] = useState(0);
  const [queuePosition, setQueuePosition] = useState(0);
  const [chatOpen, setChatOpenState] = useState(false);
  const [unread, setUnread] = useState(0);
  // Last invitation link minted for the current session, if any.
  const [invite, setInvite] = useState<InviteCreated | null>(null);
  // True once the session link is known dead (revoked): share dialogs disable.
  const [inviteRevoked, setInviteRevoked] = useState(false);
  // Link-join failure shown on the invitation screen (not a toast only).
  const [joinError, setJoinError] = useState<string | null>(null);

  const signalingRef = useRef<SignalingClient | null>(null);
  const connectingRef = useRef<Promise<SignalingClient> | null>(null);
  // Full mesh: one RTCPeerConnection per remote participant.
  const peersRef = useRef(new Map<string, RtcPeer>());
  const connectedRef = useRef(new Set<string>());
  const localStreamRef = useRef<MediaStream | null>(null);
  const sessionRef = useRef<string>('');
  const myIdRef = useRef('');
  const sessionModeRef = useRef<SessionMode>('solo');
  const prefsRef = useRef<{ mode: SessionMode; max: number }>({ mode: 'solo', max: 0 });
  const statusRef = useRef<CallStatus>('idle');
  const chatOpenRef = useRef(false);
  const iceServersRef = useRef<RTCIceServer[]>([]);
  const camEnabledRef = useRef(true);
  const selectedCameraRef = useRef<string | null>(null);
  const selectedMicrophoneRef = useRef<string | null>(null);
  const pollRef = useRef<number | null>(null);
  const tickRef = useRef<number | null>(null);
  const requeueRef = useRef<number | null>(null);
  const restartAttemptsRef = useRef(0);
  const helloReceivedRef = useRef(false);
  const handlersRef = useRef<Handlers>({} as Handlers);

  chatOpenRef.current = chatOpen;
  camEnabledRef.current = camEnabled;

  const applyStatus = useCallback((next: CallStatus, detail = '') => {
    statusRef.current = next;
    setStatus(next);
    setStatusDetail(detail);
  }, []);

  const pushToast = useCallback((tone: Toast['tone'], title: string, description?: string) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    setToasts((previous) => [...previous.slice(-3), { id, tone, title, description }]);
    window.setTimeout(() => {
      setToasts((previous) => previous.filter((toast) => toast.id !== id));
    }, 5200);
  }, []);

  const dismissToast = useCallback((id: string) => {
    setToasts((previous) => previous.filter((toast) => toast.id !== id));
  }, []);

  const pushSystemMessage = useCallback((text: string) => {
    setMessages((previous) => [
      ...previous,
      { id: `system-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`, from: 'system', text, at: Date.now() },
    ]);
  }, []);

  const clearTimers = useCallback(() => {
    if (pollRef.current !== null) {
      window.clearInterval(pollRef.current);
      pollRef.current = null;
    }
    if (tickRef.current !== null) {
      window.clearInterval(tickRef.current);
      tickRef.current = null;
    }
    if (requeueRef.current !== null) {
      window.clearTimeout(requeueRef.current);
      requeueRef.current = null;
    }
  }, []);

  const teardownAllPeers = useCallback(() => {
    clearTimers();
    for (const peer of peersRef.current.values()) peer.close();
    peersRef.current.clear();
    connectedRef.current.clear();
    sessionRef.current = '';
    restartAttemptsRef.current = 0;
    setRemotePeers([]);
    setMembers([]);
    setQuality(null);
    setSessionSeconds(0);
    // The link dies with the session: a stale token must never be reshown.
    setInvite(null);
    setInviteRevoked(false);
  }, [clearTimers]);

  const setPeerStream = useCallback((peerId: string, stream: MediaStream | null) => {
    setRemotePeers((previous) => {
      if (!previous.some((p) => p.id === peerId)) return previous;
      return previous.map((p) => (p.id === peerId ? { ...p, stream } : p));
    });
  }, []);

  const dropPeerConnection = useCallback(
    (peerId: string) => {
      peersRef.current.get(peerId)?.close();
      peersRef.current.delete(peerId);
      connectedRef.current.delete(peerId);
      setRemotePeers((previous) => previous.filter((p) => p.id !== peerId));
      setMembers((previous) => previous.filter((id) => id !== peerId));
    },
    [],
  );

  /** Creates the connection to one remote peer and attaches local tracks. */
  const ensurePeerConnection = useCallback(
    (peerId: string): RtcPeer => {
      const existing = peersRef.current.get(peerId);
      if (existing) return existing;
      const peer = new RtcPeer({
        iceServers: iceServersRef.current,
        onIceCandidate: (candidate) => {
          signalingRef.current?.send(C2S.rtcIce, { ...candidate, to: peerId });
        },
        onRemoteStream: (stream) => setPeerStream(peerId, stream),
        onConnectionState: (state) => handlersRef.current.onConnectionState(state, peerId),
        onIceConnectionState: (state) => {
          if (state === 'failed') handlersRef.current.onIceFailed(peerId);
        },
      });
      if (localStreamRef.current) peer.attachLocalStream(localStreamRef.current);
      peersRef.current.set(peerId, peer);
      setRemotePeers((previous) =>
        previous.some((p) => p.id === peerId) ? previous : [...previous, { id: peerId, stream: null }],
      );
      return peer;
    },
    [setPeerStream],
  );

  const startSessionClocks = useCallback(() => {
    const startedAt = Date.now();
    if (tickRef.current === null) {
      tickRef.current = window.setInterval(() => {
        setSessionSeconds(Math.floor((Date.now() - startedAt) / 1000));
      }, 1000);
    }
    if (pollRef.current === null) {
      pollRef.current = window.setInterval(() => {
        void handlersRef.current.sampleQuality();
      }, STATS_POLL_MS);
    }
  }, []);

  const refreshDevices = useCallback(async () => {
    if (!navigator.mediaDevices?.enumerateDevices) return;
    try {
      const all = await navigator.mediaDevices.enumerateDevices();
      const cameras = all.filter((device) => device.kind === 'videoinput');
      const microphones = all.filter((device) => device.kind === 'audioinput');
      setDevices({ cameras, microphones });

      const videoSettings = localStreamRef.current?.getVideoTracks()[0]?.getSettings();
      const audioSettings = localStreamRef.current?.getAudioTracks()[0]?.getSettings();
      if (videoSettings?.deviceId) {
        selectedCameraRef.current = videoSettings.deviceId;
        setSelectedCamera(videoSettings.deviceId);
      }
      if (audioSettings?.deviceId) {
        selectedMicrophoneRef.current = audioSettings.deviceId;
        setSelectedMicrophone(audioSettings.deviceId);
      }
    } catch {
      // Enumeration requires permission in some browsers; ignore until granted.
    }
  }, []);

  const applySessionShape = useCallback((mode: SessionMode, max: number) => {
    sessionModeRef.current = mode;
    setSessionMode(mode);
    setSessionMax(max);
  }, []);

  const wireSignaling = useCallback((client: SignalingClient) => {
    client.on(S2C.hello, (data: ServerHello) => handlersRef.current.onHello(data));
    client.on(S2C.queueWaiting, () => handlersRef.current.onQueueWaiting());
    client.on(S2C.queueCanceled, () => handlersRef.current.onQueueCanceled());
    client.on(S2C.queueTimeout, () => handlersRef.current.onQueueTimeout());
    client.on(S2C.matchFound, (data: MatchFound) => handlersRef.current.onMatchFound(data));
    client.on(S2C.sessionUpdated, (data: SessionUpdated) => handlersRef.current.onSessionUpdated(data));
    client.on(S2C.inviteCreated, (data: InviteCreated) => handlersRef.current.onInviteCreated(data));
    client.on(S2C.inviteRevoked, (data: InviteRevoked) => handlersRef.current.onInviteRevoked(data));
    client.on(S2C.sessionEnded, (data: SessionEnded) => handlersRef.current.onSessionEnded(data));
    client.on(S2C.peerJoined, (data: PeerJoined) => handlersRef.current.onPeerJoined(data));
    client.on(S2C.peerLeft, (data: PeerLeft) => handlersRef.current.onPeerLeft(data));
    client.on(S2C.chatMessage, (data: IncomingChatMessage) => handlersRef.current.onChatMessage(data));
    client.on(S2C.chatAck, () => handlersRef.current.onChatAck());
    client.on(S2C.reportAck, () => handlersRef.current.onReportAck());
    client.on(S2C.blockAck, () => handlersRef.current.onBlockAck());
    client.on(S2C.error, (data: ServerErrorPayload) => handlersRef.current.onError(data));
    client.on(S2C.rtcOffer, (data: SDPPayload) => handlersRef.current.onOffer(data));
    client.on(S2C.rtcAnswer, (data: SDPPayload) => handlersRef.current.onAnswer(data));
    client.on(S2C.rtcIce, (data: ICEPayload) => handlersRef.current.onIce(data));
    client.onClose((event) => handlersRef.current.onClose(event));
  }, []);

  /** Opens the socket once, even when several actions race to do it. */
  const ensureSignaling = useCallback(async (): Promise<SignalingClient> => {
    if (signalingRef.current?.isOpen) return signalingRef.current;
    if (connectingRef.current) return connectingRef.current;

    const promise = (async () => {
      const client = new SignalingClient(signalingUrl(persistentClientId()));
      signalingRef.current = client;
      wireSignaling(client);
      const helloPromise = client.waitFor<ServerHello>(S2C.hello, 10000);
      client.connect();
      try {
        const payload = await helloPromise;
        helloReceivedRef.current = true;
        iceServersRef.current = payload.ice_servers.map((server) => ({
          urls: server.urls,
          username: server.username,
          credential: server.credential,
        }));
        setHello(payload);
      } catch (err) {
        client.close();
        signalingRef.current = null;
        throw err;
      }
      return client;
    })();

    connectingRef.current = promise.finally(() => {
      connectingRef.current = null;
    });
    return connectingRef.current;
  }, [wireSignaling]);

  const joinQueue = useCallback(async () => {
    clearTimers();
    try {
      const client = await ensureSignaling();
      const prefs = prefsRef.current;
      const payload: { mode: SessionMode; max_participants: number } =
        prefs.mode === 'group'
          ? { mode: prefs.mode, max_participants: prefs.max }
          : { mode: prefs.mode, max_participants: 0 };
      if (!client.send(C2S.queueJoin, payload)) throw new Error(t('status.signalUnavailable'));
      setQueuePosition(0);
      applyStatus(
        'waiting',
        prefs.mode === 'group' ? t('status.searching.group') : t('status.searching.solo'),
      );
      if (navigator.mediaDevices) void refreshDevices();
    } catch (err) {
      // The close handler has already explained a refused handshake; keep its
      // message rather than replacing it with a generic transport error.
      if (statusRef.current === 'error') return;
      const message = err instanceof Error ? err.message : t('status.serverUnreachable');
      setError(message);
      applyStatus('error', message);
      pushToast('error', t('toast.connectFailed.title'), message);
    }
  }, [applyStatus, clearTimers, ensureSignaling, pushToast, refreshDevices]);

  const chooseSearchPrefs = useCallback((mode: SessionMode, max: number) => {
    const safeMax = mode === 'group' && max >= 2 ? Math.floor(max) : 0;
    prefsRef.current = { mode, max: safeMax };
    setSearchMode(mode);
    setSearchMax(safeMax);
  }, []);

  /** Captures camera and microphone; shared by queue search and link joins. */
  const acquireMedia = useCallback(async (): Promise<boolean> => {
    if (!navigator.mediaDevices?.getUserMedia) {
      const message = t('toast.cameraDenied.body');
      setError(message);
      applyStatus('error', message);
      return false;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: { width: { ideal: 1280 }, height: { ideal: 720 }, facingMode: 'user' },
        audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
      });
      localStreamRef.current = stream;
      setLocalStream(stream);
      setMicEnabled(true);
      setCamEnabled(true);
      for (const track of stream.getAudioTracks()) track.enabled = true;
      for (const track of stream.getVideoTracks()) track.enabled = true;
      await refreshDevices();
      return true;
    } catch (err) {
      const message = describeMediaError(err);
      setError(message);
      applyStatus('error', message);
      pushToast('error', t('toast.cameraDenied.title'), message);
      return false;
    }
  }, [applyStatus, pushToast, refreshDevices]);

  const start = useCallback(
    async (mode: SessionMode = 'solo', max = 0) => {
      chooseSearchPrefs(mode, max);
      setError(null);
      setMessages([]);
      setUnread(0);
      applyStatus('requesting-media', t('status.cameraPrompt'));

      if (!(await acquireMedia())) return;
      await joinQueue();
    },
    [acquireMedia, applyStatus, chooseSearchPrefs, joinQueue],
  );

  /**
   * Enters a precise session through its invitation link: same media path as
   * start(), then session.join — without touching the user's own search
   * preferences, so Next keeps using them afterwards.
   */
  const joinByToken = useCallback(
    async (token: string) => {
      setError(null);
      setJoinError(null);
      setMessages([]);
      setUnread(0);
      applyStatus('requesting-media', t('status.cameraPrompt'));

      if (!(await acquireMedia())) return;
      clearTimers();
      try {
        const client = await ensureSignaling();
        if (!client.send(C2S.sessionJoin, { token })) throw new Error(t('status.signalUnavailable'));
        setQueuePosition(0);
        applyStatus('waiting', t('invite.joining'));
        if (navigator.mediaDevices) void refreshDevices();
      } catch (err) {
        if (statusRef.current === 'error') return;
        const message = err instanceof Error ? err.message : t('status.serverUnreachable');
        setError(message);
        setJoinError(message);
        applyStatus('error', message);
        pushToast('error', t('toast.connectFailed.title'), message);
      }
    },
    [acquireMedia, applyStatus, clearTimers, ensureSignaling, pushToast, refreshDevices],
  );

  const dismissJoinError = useCallback(() => setJoinError(null), []);

  const next = useCallback(() => {
    const inCall =
      statusRef.current === 'connected' ||
      statusRef.current === 'connecting' ||
      statusRef.current === 'reconnecting';
    const client = signalingRef.current;
    clearTimers();

    if (!client?.isOpen) {
      void joinQueue();
      return;
    }
    const prefs = prefsRef.current;
    const waitingText =
      prefs.mode === 'group' ? t('status.searching.group') : t('status.searching.solo');
    if (inCall && sessionRef.current) {
      // The current search shape travels with Suivant so a setting changed
      // mid-call is already in effect for the next search.
      client.send(C2S.sessionNext, { mode: prefs.mode, max_participants: prefs.max });
      teardownAllPeers();
      // A new peer means a new conversation: stale bubbles and the previous
      // "session ended" notice must not leak into it.
      setMessages([]);
      setUnread(0);
      applyStatus('waiting', waitingText);
      return;
    }
    client.send(C2S.queueJoin, { mode: prefs.mode, max_participants: prefs.max });
    applyStatus('waiting', waitingText);
  }, [applyStatus, clearTimers, joinQueue, teardownAllPeers]);

  const stop = useCallback(() => {
    signalingRef.current?.send(C2S.sessionLeave);
    clearTimers();
    teardownAllPeers();
    signalingRef.current?.close(1000, 'user_left');
    signalingRef.current = null;
    helloReceivedRef.current = false;
    localStreamRef.current?.getTracks().forEach((track) => track.stop());
    localStreamRef.current = null;
    setLocalStream(null);
    setMessages([]);
    setUnread(0);
    setSessionSeconds(0);
    applyStatus('idle');
  }, [applyStatus, clearTimers, teardownAllPeers]);

  const toggleMic = useCallback(() => {
    const stream = localStreamRef.current;
    if (!stream) return;
    const tracks = stream.getAudioTracks();
    if (tracks.length === 0) return;
    const nextEnabled = !tracks.every((track) => track.enabled);
    for (const track of tracks) track.enabled = nextEnabled;
    setMicEnabled(nextEnabled);
  }, []);

  const toggleCam = useCallback(() => {
    const stream = localStreamRef.current;
    if (!stream) return;
    const tracks = stream.getVideoTracks();
    if (tracks.length === 0) return;
    const nextEnabled = !tracks.every((track) => track.enabled);
    for (const track of tracks) track.enabled = nextEnabled;
    setCamEnabled(nextEnabled);
  }, []);

  /** Rebuilds the local MediaStream so React sees a new object reference. */
  const rebuildLocalStream = useCallback((kind: 'audio' | 'video', track: MediaStreamTrack) => {
    const current = localStreamRef.current;
    const audio = kind === 'video' ? (current?.getAudioTracks() ?? []) : [track];
    const video = kind === 'video' ? [track] : (current?.getVideoTracks() ?? []);
    const rebuilt = new MediaStream([...audio, ...video]);
    localStreamRef.current = rebuilt;
    setLocalStream(rebuilt);
  }, []);

  const switchCamera = useCallback(
    async (deviceId: string) => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { deviceId: { exact: deviceId }, width: { ideal: 1280 }, height: { ideal: 720 } },
          audio: false,
        });
        const [track] = stream.getVideoTracks();
        if (!track) return;
        track.enabled = camEnabledRef.current;
        localStreamRef.current?.getVideoTracks().forEach((existing) => existing.stop());
        for (const peer of peersRef.current.values()) {
          await peer.replaceTrack('video', track);
        }
        rebuildLocalStream('video', track);
        selectedCameraRef.current = deviceId;
        setSelectedCamera(deviceId);
        setCamEnabled(track.enabled);
      } catch (err) {
        pushToast('error', t('toast.cameraSwitchFailed'), describeMediaError(err));
      }
    },
    [pushToast, rebuildLocalStream],
  );

  const switchMicrophone = useCallback(
    async (deviceId: string) => {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          audio: { deviceId: { exact: deviceId }, echoCancellation: true, noiseSuppression: true },
        });
        const [track] = stream.getAudioTracks();
        if (!track) return;
        track.enabled = micEnabled;
        localStreamRef.current?.getAudioTracks().forEach((existing) => existing.stop());
        for (const peer of peersRef.current.values()) {
          await peer.replaceTrack('audio', track);
        }
        rebuildLocalStream('audio', track);
        selectedMicrophoneRef.current = deviceId;
        setSelectedMicrophone(deviceId);
      } catch (err) {
        pushToast('error', t('toast.micSwitchFailed'), describeMediaError(err));
      }
    },
    [micEnabled, pushToast, rebuildLocalStream],
  );

  const sendMessage = useCallback(
    (raw: string) => {
      const text = raw.trim();
      if (!text) return;
      const client = signalingRef.current;
      if (!client?.isOpen || !sessionRef.current) {
        pushToast('warn', t('toast.noPeer.title'), t('toast.noPeer.send'));
        return;
      }
      setMessages((previous) => [
        ...previous,
        { id: `local-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`, from: 'me', text, at: Date.now(), pending: true },
      ]);
      client.send(C2S.chatSend, { text });
    },
    [pushToast],
  );

  const blockPeer = useCallback(
    (targetId?: string) => {
      const client = signalingRef.current;
      if (!client?.isOpen || !sessionRef.current) {
        pushToast('warn', t('toast.noPeer.title'), t('toast.noPeer.block'));
        return;
      }
      client.send(C2S.blockCreate, targetId ? { target: targetId } : {});
      pushToast('success', t('toast.blocked.title'), t('toast.blocked.body'));
    },
    [pushToast],
  );

  const submitReport = useCallback(
    (category: string, reason: string, block: boolean, targetId?: string) => {
      const client = signalingRef.current;
      if (!client?.isOpen || !sessionRef.current) {
        pushToast('warn', 'Signalement impossible', 'Aucune session active.');
        return;
      }
      client.send(
        C2S.reportCreate,
        targetId ? { category, reason, details: '', block, target: targetId } : { category, reason, details: '', block },
      );
      pushToast('success', t('toast.reportSent.title'), t('toast.reportSent.body'));
    },
    [pushToast],
  );

  const addParticipant = useCallback(() => {
    const client = signalingRef.current;
    if (!client?.isOpen || !sessionRef.current) {
      pushToast('warn', t('toast.noGroup.title'), t('toast.noGroup.body'));
      return;
    }
    client.send(C2S.sessionAdd);
  }, [pushToast]);

  /**
   * Asks the server for the session's invitation link. The answer arrives as
   * invite.created and lands in `invite`; an optional cap is applied to the
   * session first, and rotate revokes the previous link.
   */
  const createInvite = useCallback(
    (max?: number, rotate = false) => {
      const client = signalingRef.current;
      if (!client?.isOpen || !sessionRef.current) {
        pushToast('warn', t('toast.noSession.title'), t('toast.noSession.body'));
        return;
      }
      setInviteRevoked(false);
      client.send(
        C2S.inviteCreate,
        max === undefined ? { rotate } : { max_participants: max, rotate },
      );
    },
    [pushToast],
  );

  const revokeInvite = useCallback(() => {
    const client = signalingRef.current;
    if (!client?.isOpen || !sessionRef.current) return;
    client.send(C2S.inviteRevoke);
  }, []);

  /** Changes the member cap of the current session (0 lifts it). */
  const updateSessionCap = useCallback(
    (max: number) => {
      const client = signalingRef.current;
      if (!client?.isOpen || !sessionRef.current) {
        pushToast('warn', t('toast.noSession.title'), t('toast.noSession.body'));
        return;
      }
      client.send(C2S.sessionUpdate, { max_participants: max });
    },
    [pushToast],
  );

  const acceptConsent = useCallback(() => {
    try {
      window.localStorage.setItem(CONSENT_KEY, 'true');
    } catch {
      // Storage may be unavailable; the consent simply won't persist.
    }
    setConsentAccepted(true);
  }, []);

  const setChatOpen = useCallback((open: boolean) => {
    chatOpenRef.current = open;
    setChatOpenState(open);
    if (open) setUnread(0);
  }, []);

  const dismissError = useCallback(() => setError(null), []);

  // Socket handlers always call the newest closures through this ref.
  handlersRef.current = {
    onHello: (payload) => {
      setHello(payload);
      myIdRef.current = payload.user_id;
      setMyId(payload.user_id);
      iceServersRef.current = payload.ice_servers.map((server) => ({
        urls: server.urls,
        username: server.username,
        credential: server.credential,
      }));
    },

    onQueueWaiting: () => {
      // Join already announced the right shape; only fill in when the server
      // is the first to speak (requeue path).
      if (statusRef.current !== 'waiting') {
        applyStatus(
          'waiting',
          prefsRef.current.mode === 'group'
            ? t('status.searching.group')
            : t('status.searching.solo'),
        );
      }
    },

    onQueueCanceled: () => {
      applyStatus('idle');
    },

    onQueueTimeout: () => {
      applyStatus('idle');
      pushToast('warn', t('toast.nobodyOnline.title'), t('toast.nobodyOnline.body'));
    },

    onSessionUpdated: (payload) => {
      if (payload.mode === 'group' && sessionModeRef.current === 'solo') {
        pushSystemMessage(t('chat.promoted'));
      }
      applySessionShape(payload.mode === 'group' ? 'group' : 'solo', payload.max_participants ?? 0);
    },

    onInviteCreated: (payload) => {
      setInviteRevoked(false);
      setInvite(payload);
    },

    onInviteRevoked: () => {
      setInvite(null);
      setInviteRevoked(true);
      pushToast('info', t('invite.revoked.title'), t('invite.revoked.body'));
    },

    onMatchFound: (payload) => {
      sessionRef.current = payload.session_id;
      restartAttemptsRef.current = 0;
      setJoinError(null);
      applySessionShape(payload.mode === 'group' ? 'group' : 'solo', payload.max_participants ?? 0);
      const others = (payload.peers ?? []).filter((id) => id !== myIdRef.current);
      setMembers(payload.peers ?? []);
      setRemotePeers(others.map((id) => ({ id, stream: null })));
      // The auto-requeue path lands here with the previous "session ended"
      // notice still in the list; a fresh session starts with a clean chat.
      setMessages([]);
      setUnread(0);
      applyStatus('connecting', t('status.negotiating'));
      // One SDP offer per pair, no coordination round trip: the longest
      // waiter offers in solo mode (server role), while conference members
      // compare identifiers and the smaller one offers.
      const offerTo = (peerId: string) => {
        const peer = ensurePeerConnection(peerId);
        void (async () => {
          try {
            const offer = await peer.createOffer();
            signalingRef.current?.send(C2S.rtcOffer, { ...offer, to: peerId });
          } catch (err) {
            pushToast('error', t('toast.negotiationFailed'), describeMediaError(err));
          }
        })();
      };
      if (payload.role === 'offerer' && payload.peer_id) {
        offerTo(payload.peer_id);
      } else if (payload.role !== 'answerer') {
        for (const peerId of others) {
          if (myIdRef.current < peerId) offerTo(peerId);
        }
      }
      for (const peerId of others) ensurePeerConnection(peerId);
    },

    onOffer: (sdp) => {
      const from = sdp.from;
      if (!from) return;
      const peer = ensurePeerConnection(from);
      void (async () => {
        try {
          const answer = await peer.acceptOffer(sdp);
          signalingRef.current?.send(C2S.rtcAnswer, { ...answer, to: from });
        } catch (err) {
          pushToast('error', t('toast.negotiationFailed'), describeMediaError(err));
        }
      })();
    },

    onAnswer: (sdp) => {
      const peer = sdp.from ? peersRef.current.get(sdp.from) : undefined;
      void peer?.acceptAnswer(sdp);
    },

    onIce: (candidate) => {
      const peer = candidate.from ? peersRef.current.get(candidate.from) : undefined;
      void peer?.addIceCandidate(candidate);
    },

    onPeerJoined: (payload) => {
      setMembers((previous) =>
        previous.includes(payload.peer_id) ? previous : [...previous, payload.peer_id],
      );
      // The newcomer offers to everyone: the room only waits for the offer.
      ensurePeerConnection(payload.peer_id);
      pushSystemMessage(t('chat.peerJoined'));
    },

    onPeerLeft: (payload) => {
      dropPeerConnection(payload.peer_id);
      pushSystemMessage(
        payload.members > 1
          ? t('chat.peerLeftCount', { n: payload.members })
          : t('chat.peerLeft.alone'),
      );
    },

    onChatMessage: (payload) => {
      setMessages((previous) => [
        ...previous,
        { id: payload.id, from: 'peer', text: payload.text, at: Date.now() },
      ]);
      if (!chatOpenRef.current) setUnread((count) => count + 1);
    },

    onChatAck: () => {
      setMessages((previous) => {
        const index = [...previous].reverse().findIndex((m) => m.from === 'me' && m.pending);
        if (index === -1) return previous;
        const target = previous.length - 1 - index;
        const copy = [...previous];
        copy[target] = { ...copy[target], pending: false };
        return copy;
      });
    },

    onReportAck: () => {
      // The server ends the session right after, which drives the UI transition.
    },

    onBlockAck: () => {
      // Same as reports: session.ended follows.
    },

    onSessionEnded: (payload) => {
      teardownAllPeers();
      pushSystemMessage(describeSessionEnd(payload.reason));

      if (payload.reason === 'reported' || payload.reason === 'blocked') {
        applyStatus('idle');
        return;
      }
      applyStatus('ended', 'Recherche d’un nouvel interlocuteur');
      if (payload.can_requeue) {
        requeueRef.current = window.setTimeout(() => {
          requeueRef.current = null;
          void joinQueue();
        }, AUTO_REQUEUE_DELAY_MS);
      }
    },

    onError: (payload) => {
      if (payload.code === 'replaced') {
        applyStatus('error', t('server.replaced'));
        return;
      }
      if (payload.code === 'rate_limited') {
        pushToast('warn', t('toast.tooMany.title'), t('server.rate_limited'));
        return;
      }
      if (payload.code === 'suspended') {
        setError(payload.message);
        applyStatus('error', payload.message);
        return;
      }
      if (payload.code === 'empty_queue') {
        pushToast('info', t('server.empty_queue.title'), t('server.empty_queue.body'));
        return;
      }
      if (payload.code === 'session_full') {
        pushToast('warn', t('toast.addFailed.title'), payload.message);
        setJoinError(t('invite.full'));
        return;
      }
      if (payload.code === 'invalid_token') {
        setJoinError(t('invite.invalid'));
        pushToast('warn', t('invite.invalid.title'), t('invite.invalid.body'));
        return;
      }
      if (payload.code === 'blocked_from_session') {
        setJoinError(t('invite.blocked'));
        pushToast('warn', t('invite.blocked.title'), t('invite.blocked.body'));
        return;
      }
      if (payload.code === 'already_in_session') {
        setJoinError(t('invite.alreadyIn'));
        return;
      }
      if (payload.code === 'invalid_limit') {
        pushToast('warn', t('toast.invalidLimit.title'), t('toast.invalidLimit.body'));
        return;
      }
      if (payload.code === 'no_peer' || payload.code === 'not_waiting' || payload.code === 'no_session') {
        return;
      }
      pushToast('error', t('toast.protocolError.title'), payload.message);
    },

    onConnectionState: (state, peerId) => {
      if (state === 'connected') {
        restartAttemptsRef.current = 0;
        connectedRef.current.add(peerId);
        applyStatus('connected');
        startSessionClocks();
        return;
      }
      if (state === 'connecting') {
        if (statusRef.current !== 'connected') applyStatus('connecting', t('status.negotiating'));
        return;
      }
      if (state === 'disconnected') {
        connectedRef.current.delete(peerId);
        // A conference survives one peer dropping; only the last link down
        // reads as a reconnection.
        if (connectedRef.current.size === 0) {
          applyStatus('reconnecting', t('status.unstable'));
        }
        return;
      }
      if (state === 'failed') {
        handlersRef.current.onIceFailed(peerId);
      }
    },

    onIceFailed: (peerId) => {
      const peer = peersRef.current.get(peerId);
      if (peersRef.current.size > 1) {
        // One mesh link died while others live: drop the tile, keep the call.
        dropPeerConnection(peerId);
        pushToast('warn', t('toast.peerLost.title'), t('toast.peerLost.body'));
        return;
      }
      if (restartAttemptsRef.current < 1 && peer) {
        restartAttemptsRef.current += 1;
        applyStatus('reconnecting', 'Tentative de reprise de la connexion');
        peer.restartIce();
        return;
      }
      pushToast('warn', t('toast.connectFailed.title'), t('toast.iceFailed.body'));
      applyStatus('idle');
      if (signalingRef.current?.isOpen) {
        signalingRef.current.send(C2S.sessionNext);
      }
    },

    sampleQuality: async () => {
      const first = peersRef.current.values().next();
      if (first.done) return;
      try {
        setQuality(await first.value.sample());
      } catch {
        // Sampling failures are not actionable for the user.
      }
    },

    onClose: (event) => {
      const client = signalingRef.current;
      if (client?.wasClosedByUs || event.code === 1000) return;
      const active = statusRef.current;
      if (active === 'idle' || active === 'error') return;
      teardownAllPeers();
      signalingRef.current = null;

      // Distinguish "we lost the server" from "the server never let us in",
      // because the second one is a configuration problem, not a network blip.
      const established = helloReceivedRef.current;
      const message = established ? t('status.serverLost') : t('status.signalRefused');
      setError(message);
      applyStatus('error', message);
      pushToast(
        'error',
        t('toast.serverDown.title'),
        established ? t('error.checkNetwork') : t('error.checkOrigins'),
      );
    },
  };

  // Device changes (plugging a headset, for example) must refresh the pickers.
  useEffect(() => {
    const media = navigator.mediaDevices;
    if (!media?.addEventListener) return;
    const onChange = () => {
      void refreshDevices();
    };
    media.addEventListener('devicechange', onChange);
    return () => media.removeEventListener('devicechange', onChange);
  }, [refreshDevices]);

  // Aggregate counters for the landing page.
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const response = await fetch('/api/stats', { headers: { Accept: 'application/json' } });
        if (!response.ok) return;
        const payload = (await response.json()) as PublicStats;
        if (!cancelled) setPublicStats(payload);
      } catch {
        // Counters are decorative; failures are silent.
      }
    };
    void load();
    const timer = window.setInterval(load, 15000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  // Release the camera and the socket when the page goes away.
  useEffect(() => {
    const release = () => {
      clearTimers();
      for (const peer of peersRef.current.values()) peer.close();
      peersRef.current.clear();
      localStreamRef.current?.getTracks().forEach((track) => track.stop());
      localStreamRef.current = null;
      signalingRef.current?.close(1000, 'page_hidden');
    };
    window.addEventListener('pagehide', release);
    return () => {
      window.removeEventListener('pagehide', release);
      release();
    };
  }, [clearTimers]);

  return {
    status,
    statusDetail,
    consentAccepted,
    hello,
    publicStats,
    localStream,
    remotePeers,
    myId,
    members,
    sessionMode,
    sessionMax,
    searchMode,
    searchMax,
    messages,
    micEnabled,
    camEnabled,
    devices,
    selectedCamera,
    selectedMicrophone,
    quality,
    error,
    toasts,
    sessionSeconds,
    queuePosition,
    chatOpen,
    unread,
    invite,
    inviteRevoked,
    joinError,
    start,
    next,
    joinByToken,
    dismissJoinError,
    changeSearchPrefs: chooseSearchPrefs,
    stop,
    toggleMic,
    toggleCam,
    switchCamera,
    switchMicrophone,
    sendMessage,
    addParticipant,
    createInvite,
    revokeInvite,
    updateSessionCap,
    submitReport,
    blockPeer,
    acceptConsent,
    setChatOpen,
    dismissToast,
    dismissError,
  };
}

interface Handlers {
  onHello(payload: ServerHello): void;
  onQueueWaiting(): void;
  onQueueCanceled(): void;
  onQueueTimeout(): void;
  onMatchFound(payload: MatchFound): void;
  onSessionUpdated(payload: SessionUpdated): void;
  onInviteCreated(payload: InviteCreated): void;
  onInviteRevoked(payload: InviteRevoked): void;
  onOffer(sdp: SDPPayload): void;
  onAnswer(sdp: SDPPayload): void;
  onIce(candidate: ICEPayload): void;
  onPeerJoined(payload: PeerJoined): void;
  onPeerLeft(payload: PeerLeft): void;
  onChatMessage(payload: IncomingChatMessage): void;
  onChatAck(): void;
  onReportAck(): void;
  onBlockAck(): void;
  onSessionEnded(payload: SessionEnded): void;
  onError(payload: ServerErrorPayload): void;
  onConnectionState(state: RTCPeerConnectionState, peerId: string): void;
  onIceFailed(peerId: string): void;
  sampleQuality(): Promise<void>;
  onClose(event: CloseEvent): void;
}

function describeSessionEnd(reason: SessionEnded['reason']): string {
  switch (reason) {
    case 'peer_disconnected':
      return getT()('error.peerDisconnected');
    case 'next':
      return getT()('error.peerSkipped');
    case 'left':
      return getT()('error.peerLeft');
    case 'reported':
      return getT()('error.closedByReport');
    case 'blocked':
      return getT()('error.closedByBlock');
    case 'server_shutdown':
      return getT()('error.sessionShutdown');
    default:
      return getT()('error.sessionEnded');
  }
}

function describeMediaError(err: unknown): string {
  if (err instanceof DOMException) {
    switch (err.name) {
      case 'NotAllowedError':
        return getT()('media.notAllowed');
      case 'NotFoundError':
        return getT()('media.notFound');
      case 'NotReadableError':
        return getT()('media.notReadable');
      case 'OverconstrainedError':
        return getT()('media.overconstrained');
      case 'SecurityError':
        return getT()('media.insecure');
      default:
        return getT()('media.error', { name: err.name });
    }
  }
  if (err instanceof Error) return err.message;
  return getT()('media.generic');
}

function readConsent(): boolean {
  try {
    return window.localStorage.getItem(CONSENT_KEY) === 'true';
  } catch {
    return false;
  }
}
