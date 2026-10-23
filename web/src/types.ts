/**
 * Wire protocol shared with the Go backend (internal/signaling/messages.go).
 * Keep both sides in sync: an event renamed here must be renamed there.
 */

export type CallStatus =
  | 'idle'
  | 'requesting-media'
  | 'waiting'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'ended'
  | 'error';

export interface Envelope {
  event: string;
  data?: unknown;
  req_id?: string;
}

export interface IceServerConfig {
  urls: string | string[];
  username?: string;
  credential?: string;
}

export interface ServerHello {
  user_id: string;
  protocol_version: string;
  ice_servers: IceServerConfig[];
  server_time: string;
  queue_length: number;
  limits: {
    max_chat_runes: number;
    max_message_bytes: number;
  };
}

export type SessionMode = 'solo' | 'group';

export interface QueueJoinPayload {
  mode?: SessionMode;
  max_participants?: number;
}

export interface MatchFound {
  session_id: string;
  peers: string[];
  mode: SessionMode;
  max_participants: number;
  peer_id?: string;
  role?: 'offerer' | 'answerer';
  started_at: string;
}

export interface PeerJoined {
  session_id: string;
  peer_id: string;
}

export interface PeerLeft {
  session_id: string;
  peer_id: string;
  reason: string;
  members: number;
}

export interface QueueWaiting {
  position: number;
  since: string;
}

export interface SessionEnded {
  session_id: string;
  reason: 'next' | 'left' | 'peer_disconnected' | 'reported' | 'blocked' | 'requeue' | 'server_shutdown';
  duration_seconds: number;
  can_requeue: boolean;
}

export interface SDPPayload {
  type: 'offer' | 'answer';
  sdp: string;
  to?: string;
  from?: string;
}

export interface ICEPayload {
  candidate: string;
  sdpMid?: string | null;
  sdpMLineIndex?: number | null;
  usernameFragment?: string | null;
  to?: string;
  from?: string;
}

export interface IncomingChatMessage {
  id: string;
  text: string;
  from: 'peer';
  at: string;
}

export interface ServerErrorPayload {
  code: string;
  message: string;
  event?: string;
}

export interface InviteCreated {
  token: string;
  mode: SessionMode;
  max_participants: number;
}

export interface InviteRevoked {
  session_id: string;
}

export interface SessionUpdated {
  session_id: string;
  mode: SessionMode;
  max_participants: number;
}

export interface InvitePreview {
  mode: SessionMode;
  members: number;
  max_participants: number;
}

export interface ReportAck {
  report_id: string;
  blocked: boolean;
  suspended: boolean;
}

/** Client side events, mirrored from the server constants. */
export const C2S = {
  queueJoin: 'queue.join',
  queueCancel: 'queue.cancel',
  sessionNext: 'session.next',
  sessionLeave: 'session.leave',
  sessionAdd: 'session.add',
  sessionJoin: 'session.join',
  sessionUpdate: 'session.update',
  inviteCreate: 'invite.create',
  inviteRevoke: 'invite.revoke',
  rtcOffer: 'rtc.offer',
  rtcAnswer: 'rtc.answer',
  rtcIce: 'rtc.ice',
  chatSend: 'chat.send',
  reportCreate: 'report.create',
  blockCreate: 'block.create',
} as const;

/** Server to client events. */
export const S2C = {
  hello: 'server.hello',
  error: 'server.error',
  queueWaiting: 'queue.waiting',
  queueCanceled: 'queue.canceled',
  queueTimeout: 'queue.timeout',
  matchFound: 'match.found',
  sessionEnded: 'session.ended',
  sessionUpdated: 'session.updated',
  inviteCreated: 'invite.created',
  inviteRevoked: 'invite.revoked',
  peerJoined: 'peer.joined',
  peerLeft: 'peer.left',
  chatMessage: 'chat.message',
  chatAck: 'chat.ack',
  reportAck: 'report.ack',
  blockAck: 'block.ack',
  rtcOffer: 'rtc.offer',
  rtcAnswer: 'rtc.answer',
  rtcIce: 'rtc.ice',
  state: 'session.state',
  connectionOpen: '__open',
  connectionClose: '__close',
} as const;

export interface ChatMessage {
  id: string;
  from: 'me' | 'peer' | 'system';
  text: string;
  at: number;
  pending?: boolean;
}

export interface PeerStats {
  candidateType: 'host' | 'srflx' | 'prflx' | 'relay' | 'unknown';
  rttMs: number | null;
  jitterMs: number | null;
  packetsLost: number;
  bitrateKbps: number | null;
  framesPerSecond: number | null;
}

export interface PublicStats {
  online: number;
  active_sessions: number;
  queue_length: number;
  matches_total: number;
}

export interface Toast {
  id: string;
  tone: 'info' | 'success' | 'warn' | 'error';
  title: string;
  description?: string;
}

export const REPORT_CATEGORIES = [
  { value: 'nudity', label: 'Nudité ou contenu sexuel' },
  { value: 'harassment', label: 'Harcèlement ou insultes' },
  { value: 'hate', label: 'Discours haineux' },
  { value: 'violence', label: 'Violence ou menace' },
  { value: 'spam', label: 'Spam ou publicité' },
  { value: 'minor', label: 'Personne mineure' },
  { value: 'other', label: 'Autre' },
] as const;
