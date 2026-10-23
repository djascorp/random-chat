import type { ICEPayload, PeerStats, SDPPayload } from '../types';

export interface RtcPeerOptions {
  iceServers: RTCIceServer[];
  onIceCandidate: (candidate: ICEPayload) => void;
  onRemoteStream: (stream: MediaStream) => void;
  onConnectionState: (state: RTCPeerConnectionState) => void;
  onIceConnectionState: (state: RTCIceConnectionState) => void;
}

/**
 * Minimal shape of a candidate stats entry. Declared locally because
 * `RTCIceCandidateStats` is missing from some TypeScript DOM lib versions.
 */
interface IceCandidateEntry {
  id: string;
  candidateType?: string;
}

/**
 * Thin, stateful wrapper around RTCPeerConnection.
 *
 * Tracks are added exactly once (at session start) and never removed: toggling
 * the microphone or camera flips `track.enabled`, and switching device uses
 * `replaceTrack`. Neither operation requires renegotiation, which keeps the
 * signaling path free of glare handling.
 */
export class RtcPeer {
  private readonly pc: RTCPeerConnection;
  private readonly pendingCandidates: ICEPayload[] = [];
  private hasRemoteDescription = false;

  private lastSample: { bytes: number; at: number } | null = null;

  constructor(options: RtcPeerOptions) {
    this.pc = new RTCPeerConnection({
      iceServers: options.iceServers,
      bundlePolicy: 'max-bundle',
      rtcpMuxPolicy: 'require',
    });

    this.pc.onicecandidate = (event) => {
      if (event.candidate) {
        options.onIceCandidate(event.candidate.toJSON() as ICEPayload);
      }
    };

    this.pc.ontrack = (event) => {
      const [stream] = event.streams;
      if (stream) options.onRemoteStream(stream);
    };

    this.pc.onconnectionstatechange = () => options.onConnectionState(this.pc.connectionState);
    this.pc.oniceconnectionstatechange = () => options.onIceConnectionState(this.pc.iceConnectionState);
  }

  get connectionState(): RTCPeerConnectionState {
    return this.pc.connectionState;
  }

  get signalingState(): RTCSignalingState {
    return this.pc.signalingState;
  }

  /**
   * Adds the local media tracks to the connection. Called once, at session
   * start: tracks are never removed afterwards, which avoids renegotiation.
   */
  attachLocalStream(stream: MediaStream): void {
    for (const track of stream.getTracks()) {
      this.pc.addTrack(track, stream);
    }
  }

  async createOffer(): Promise<SDPPayload> {
    const offer = await this.pc.createOffer();
    await this.pc.setLocalDescription(offer);
    return { type: 'offer', sdp: offer.sdp ?? '' };
  }

  async acceptOffer(offer: SDPPayload): Promise<SDPPayload> {
    // Defensive rollback: an offer should only arrive while we are stable, but a
    // simultaneous device change on both sides could produce glare.
    if (this.pc.signalingState !== 'stable' && this.pc.signalingState !== 'have-remote-offer') {
      await this.pc.setLocalDescription({ type: 'rollback' } as RTCLocalSessionDescriptionInit);
    }
    await this.pc.setRemoteDescription({ type: 'offer', sdp: offer.sdp });
    this.hasRemoteDescription = true;
    await this.flushCandidates();

    const answer = await this.pc.createAnswer();
    await this.pc.setLocalDescription(answer);
    return { type: 'answer', sdp: answer.sdp ?? '' };
  }

  async acceptAnswer(answer: SDPPayload): Promise<void> {
    if (this.pc.signalingState !== 'have-local-offer') return;
    await this.pc.setRemoteDescription({ type: 'answer', sdp: answer.sdp });
    this.hasRemoteDescription = true;
    await this.flushCandidates();
  }

  /** Queues candidates received before the remote description was applied. */
  async addIceCandidate(candidate: ICEPayload): Promise<void> {
    if (!this.hasRemoteDescription) {
      this.pendingCandidates.push(candidate);
      return;
    }
    try {
      await this.pc.addIceCandidate(candidate as RTCIceCandidateInit);
    } catch {
      // A stale candidate after a renegotiation is harmless.
    }
  }

  private async flushCandidates(): Promise<void> {
    const queued = this.pendingCandidates.splice(0, this.pendingCandidates.length);
    for (const candidate of queued) {
      try {
        await this.pc.addIceCandidate(candidate as RTCIceCandidateInit);
      } catch {
        // ignore
      }
    }
  }

  /**
   * Swaps the outgoing track for one of the same kind. Same-kind replacement
   * does not require renegotiation, so no new offer/answer round trip is needed.
   * Returns false when there is no sender to update yet.
   */
  async replaceTrack(kind: 'audio' | 'video', track: MediaStreamTrack): Promise<boolean> {
    const sender = this.pc.getSenders().find((s) => s.track?.kind === kind);
    if (!sender) return false;
    await sender.replaceTrack(track);
    return true;
  }

  /** Attempts an ICE restart after a transient network failure. */
  restartIce(): void {
    try {
      this.pc.restartIce();
    } catch {
      // Older browsers may not support it; the caller falls back to a new search.
    }
  }

  /** Samples the connection and returns normalized quality figures. */
  async sample(): Promise<PeerStats> {
    const report = await this.pc.getStats();
    const stats: PeerStats = {
      candidateType: 'unknown',
      rttMs: null,
      jitterMs: null,
      packetsLost: 0,
      bitrateKbps: null,
      framesPerSecond: null,
    };

    let selectedPairId: string | null = null;
    const candidates = new Map<string, IceCandidateEntry>();

    report.forEach((entry) => {
      if (entry.type === 'candidate-pair') {
        const pair = entry as RTCIceCandidatePairStats;
        if (pair.state === 'succeeded' && (pair.nominated || selectedPairId === null)) {
          selectedPairId = pair.id as string;
        }
      }
      if (entry.type === 'local-candidate' || entry.type === 'remote-candidate') {
        candidates.set(entry.id, entry as unknown as IceCandidateEntry);
      }
    });

    report.forEach((entry) => {
      if (entry.type === 'candidate-pair' && (entry as RTCIceCandidatePairStats).id === selectedPairId) {
        const pair = entry as RTCIceCandidatePairStats;
        if (typeof pair.currentRoundTripTime === 'number') {
          stats.rttMs = Math.round(pair.currentRoundTripTime * 1000);
        }
        const local = pair.localCandidateId ? candidates.get(pair.localCandidateId) : undefined;
        if (local?.candidateType) {
          stats.candidateType = local.candidateType as PeerStats['candidateType'];
        }
        const remote = pair.remoteCandidateId ? candidates.get(pair.remoteCandidateId) : undefined;
        if (stats.candidateType === 'unknown' && remote?.candidateType) {
          stats.candidateType = remote.candidateType as PeerStats['candidateType'];
        }
      }

      if (entry.type === 'inbound-rtp') {
        const inbound = entry as RTCInboundRtpStreamStats;
        if (inbound.kind !== 'video') return;
        if (typeof inbound.jitter === 'number') {
          stats.jitterMs = Math.round(inbound.jitter * 1000);
        }
        stats.packetsLost += inbound.packetsLost ?? 0;
        stats.framesPerSecond = inbound.framesPerSecond ?? stats.framesPerSecond;
        const bytes = inbound.bytesReceived ?? 0;
        const now = performance.now();
        if (this.lastSample) {
          const seconds = (now - this.lastSample.at) / 1000;
          if (seconds > 0.5) {
            const delta = bytes - this.lastSample.bytes;
            stats.bitrateKbps = Math.max(0, Math.round((delta * 8) / seconds / 1000));
            this.lastSample = { bytes, at: now };
          }
        } else {
          this.lastSample = { bytes, at: now };
        }
      }
    });

    return stats;
  }

  close(): void {
    for (const sender of this.pc.getSenders()) {
      try {
        sender.replaceTrack(null);
      } catch {
        // ignore
      }
    }
    this.pc.onicecandidate = null;
    this.pc.ontrack = null;
    this.pc.onconnectionstatechange = null;
    this.pc.oniceconnectionstatechange = null;
    this.pc.close();
  }
}

/** Classifies a connection for the quality badge. */
export function describeConnection(stats: PeerStats | null): {
  label: string;
  tone: 'ok' | 'warn' | 'bad';
} {
  if (!stats) return { label: 'connecting', tone: 'warn' };
  if (stats.candidateType === 'relay') {
    return { label: 'relay', tone: 'warn' };
  }
  const rtt = stats.rttMs;
  if (rtt === null) return { label: 'direct', tone: 'ok' };
  if (rtt < 150) return { label: `directMs:${rtt}`, tone: 'ok' };
  if (rtt < 350) return { label: `fairMs:${rtt}`, tone: 'warn' };
  return { label: `slowMs:${rtt}`, tone: 'bad' };
}
