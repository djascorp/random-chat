import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

export type Lang = 'en' | 'fr';

const STORAGE_KEY = 'random-chat.lang';
// English is the default language; the choice persists across visits.
const DEFAULT_LANG: Lang = 'en';

type Entry = { en: string; fr: string };

/**
 * Single source of truth for every user-facing string (UI copy, toasts,
 * status lines, a11y labels). `{name}` placeholders are filled by translate().
 * Server-origin messages are keyed by stable signalling codes so the client
 * can localise them regardless of the server's own language.
 */
const STRINGS = {
  // ─────────────────────────────── topbar / shell
  'topbar.online': { en: '{n} online', fr: '{n} en ligne' },
  'topbar.offline': { en: 'offline', fr: 'hors ligne' },

  // ─────────────────────────────── landing — hero
  'landing.badge.connecting': { en: 'Connecting…', fr: 'Connexion…' },
  'landing.title': { en: 'Meet someone new,', fr: 'Rencontrez quelqu’un,' },
  'landing.title.soft': { en: 'on video, right now.', fr: 'en vidéo, maintenant.' },
  'landing.lead': {
    en: 'One click, a live video conversation with someone picked at random. No account, no profile, no public history. You stay in control: mic, camera and Skip at any moment.',
    fr: 'Un clic, une conversation vidéo en direct avec une personne choisie au hasard. Sans compte, sans profil, sans historique public. Vous gardez le contrôle : micro, caméra et changement d’interlocuteur à tout moment.',
  },
  'landing.cta.solo': { en: 'Start a conversation', fr: 'Démarrer une conversation' },
  'landing.cta.group': { en: 'Join a group call', fr: 'Rejoindre une conférence' },
  'landing.cta.groupMax': { en: 'Join a group call (max {n})', fr: 'Rejoindre une conférence (max {n})' },
  'landing.cta.preparing': { en: 'Getting ready…', fr: 'Préparation…' },
  'landing.hint.turn': { en: 'TURN relay configured for restrictive networks', fr: 'Relais TURN configuré pour les réseaux restrictifs' },
  'landing.hint.p2p': { en: 'Optimised for networks that allow direct connections', fr: 'Optimisé pour les réseaux autorisant les connexions directes' },

  // ─────────────────────────────── landing — metrics
  'metric.online': { en: 'Online', fr: 'En ligne' },
  'metric.active': { en: 'Active conversations', fr: 'Conversations actives' },
  'metric.matches': { en: 'Total matches', fr: 'Mises en relation' },

  // ─────────────────────────────── landing — steps
  'steps.title': { en: 'How it works', fr: 'Comment ça marche' },
  'steps.1.title': { en: 'Allow your camera', fr: 'Autorisez la caméra' },
  'steps.1.text': { en: 'One click, no sign-up. Your browser asks for microphone and camera access.', fr: 'Un clic, aucune inscription. Votre navigateur demande l’accès au micro et à la caméra.' },
  'steps.2.title': { en: 'We match you instantly', fr: 'Nous vous mettons en relation' },
  'steps.2.text': { en: 'You join the queue and are connected with an available person.', fr: 'Vous rejoignez la file d’attente et êtes connecté à une personne disponible.' },
  'steps.3.title': { en: 'Skip whenever you like', fr: 'Passez au suivant quand vous voulez' },
  'steps.3.text': { en: 'Not the right match? One click ends the session and starts a new search.', fr: 'La conversation ne vous convient pas ? Un clic met fin à la session et relance la recherche.' },

  // ─────────────────────────────── landing — safety
  'safety.title': { en: 'Safety & moderation', fr: 'Sécurité et modération' },
  'safety.report.title': { en: 'Report & block', fr: 'Signalement et blocage' },
  'safety.report.text': { en: 'Report inappropriate behaviour in one click. Blocking prevents any future match with the same person.', fr: 'Signalez un comportement inapproprié en un clic. Le blocage empêche toute mise en relation ultérieure avec la même personne.' },
  'safety.chat.title': { en: 'Text chat fallback', fr: 'Chat texte de secours' },
  'safety.chat.text': { en: 'If audio fails, type instead: the chat stays available throughout the session.', fr: 'Si le son ne fonctionne pas, écrivez : le chat reste disponible pendant toute la session.' },
  'safety.p2p.title': { en: 'Peer-to-peer video', fr: 'Appel pair à pair' },
  'safety.p2p.text': { en: 'Browsers negotiate the connection directly with each other. The server only handles signalling — it never sees your video.', fr: 'Les navigateurs négocient la connexion directement entre eux. Le serveur ne traite que la signalisation, jamais votre flux vidéo.' },

  // ─────────────────────────────── consent gate
  'consent.title': { en: 'Before you start', fr: 'Avant de commencer' },
  'consent.text': { en: 'This service connects you with people you don’t know, live and without automated content moderation. Please confirm the following.', fr: 'Cet espace met en relation des personnes qui ne se connaissent pas, en direct et sans modération automatique du contenu. Merci de confirmer les points suivants.' },
  'consent.adult': { en: 'I am 18 or older.', fr: 'J’ai 18 ans ou plus.' },
  'consent.terms': { en: 'I accept the rules: no nudity, no harassment, no illegal content, and I understand that reports are recorded.', fr: 'J’accepte les règles : pas de nudité, pas de harcèlement, pas de contenu illégal, et je comprends que tout signalement est enregistré.' },
  'consent.cta': { en: 'Got it, continue', fr: 'J’ai compris, continuer' },

  // ─────────────────────────────── search preferences
  'prefs.groupLabel': { en: 'Conversation type', fr: 'Type de conversation' },
  'prefs.maxLabel': { en: 'Maximum participants', fr: 'Participants maximum' },
  'prefs.solo': { en: 'Solo', fr: 'Solo' },
  'prefs.solo.desc': { en: 'One-to-one video call.', fr: 'Rencontre individuelle, en tête-à-tête.' },
  'prefs.group': { en: 'Group call', fr: 'Conférence' },
  'prefs.group.desc': { en: 'A room with several participants; invite people while the call is running.', fr: 'Salon à plusieurs participants ; ajoutez des personnes en cours d’appel.' },
  'prefs.max.decrease': { en: 'Decrease the maximum number of participants', fr: 'Réduire le nombre maximum de participants' },
  'prefs.max.increase': { en: 'Increase the maximum number of participants', fr: 'Augmenter le nombre maximum de participants' },
  'prefs.max.unlimited': { en: 'No limit', fr: 'Sans limite' },
  'prefs.max.short': { en: '{n} max', fr: '{n} max' },
  'prefs.menu.label': { en: 'Search type: {mode}. Applied on the next match.', fr: 'Type de recherche : {mode}. Appliqué au prochain changement.' },
  'prefs.menu.title': { en: 'Search type (applied on Skip)', fr: 'Type de recherche (appliqué au suivant)' },
  'prefs.menu.group': { en: 'Group', fr: 'Conf.' },
  'prefs.menu.groupMax': { en: 'Group {n}', fr: 'Conf. {n}' },

  // ─────────────────────────────── call stage
  'stage.remoteVideo': { en: 'Your caller’s video', fr: 'Flux vidéo de votre interlocuteur' },
  'stage.remoteVideoLarge': { en: 'Caller’s video, enlarged', fr: 'Flux de l’appelant en grand' },
  'stage.selfVideo': { en: 'Your camera', fr: 'Votre caméra' },
  'stage.selfVideoLarge': { en: 'Your camera, enlarged', fr: 'Votre caméra en grand' },
  'stage.selfVideoPip': { en: 'Your camera (thumbnail)', fr: 'Votre caméra (miniature)' },
  'stage.waitingStream': { en: 'Waiting for video…', fr: 'En attente du flux vidéo…' },
  'stage.cameraOff': { en: 'Camera off', fr: 'Caméra coupée' },
  'stage.selfCameraOff': { en: 'Your camera is off', fr: 'Votre caméra est coupée' },
  'stage.you': { en: 'You', fr: 'Vous' },
  'stage.caller': { en: 'Caller', fr: 'Appelant' },
  'stage.searching': { en: 'Looking for someone…', fr: 'Recherche d’un interlocuteur…' },
  'stage.connecting': { en: 'Connecting…', fr: 'Connexion en cours…' },
  'stage.connectingPeer': { en: 'Connecting to your match…', fr: 'Connexion à l’interlocuteur…' },
  'stage.cameraPrompt': { en: 'Camera', fr: 'Caméra' },
  'stage.inConversation': { en: 'In conversation', fr: 'En conversation' },
  'stage.inConference': { en: 'In group call', fr: 'En conférence' },
  'stage.reconnecting': { en: 'Reconnecting…', fr: 'Reconnexion…' },
  'stage.waitingParticipants': { en: 'Waiting for participants…', fr: 'En attente de participants…' },
  'stage.waitingParticipantsHint': { en: 'Use Add to bring someone in, or wait for others to join.', fr: 'Utilisez le bouton Ajouter pour faire entrer quelqu’un, ou attendez qu’on vous rejoigne.' },
  'stage.peerConnecting': { en: 'Connecting…', fr: 'Connexion…' },
  'stage.participant': { en: 'Participant {n}', fr: 'Participant {n}' },
  'stage.participants': { en: 'Group call participants', fr: 'Participants de la conférence' },
  'stage.otherParticipants': { en: 'Other participants', fr: 'Autres participants' },
  'stage.pin': { en: 'Enlarge {name}', fr: 'Agrandir {name}' },
  'stage.unpin': { en: 'Back to grid', fr: 'Revenir à la grille' },
  'stage.swapHint': { en: 'Tap to enlarge · drag to move', fr: 'Appuyez pour agrandir · glissez pour déplacer' },
  'stage.dragHint': { en: 'Drag to move', fr: 'Glissez pour déplacer' },
  'stage.swapAria': { en: 'Swap with the main view', fr: 'Échanger avec la grande vue' },
  'stage.fullscreenHint': { en: 'Double-click: fullscreen · F fullscreen · S swap views', fr: 'Double-clic : plein écran · F plein écran · S échanger les vues' },
  'stage.reportPeer': { en: 'Report {name}', fr: 'Signaler {name}' },
  'stage.blockPeer': { en: 'Block {name}', fr: 'Bloquer {name}' },
  'stage.quality.turn': { en: 'Relayed through TURN: quality may be reduced on this network', fr: 'Appel relayé via TURN : la qualité peut être réduite sur ce réseau' },
  'stage.quality.rtt': { en: 'Network round-trip: {n} ms', fr: 'Aller-retour réseau : {n} ms' },
  'stage.quality.measuring': { en: 'Measuring connection quality', fr: 'Mesure de la qualité de connexion en cours' },
  'stage.quality.connecting': { en: 'Connecting…', fr: 'Connexion…' },
  'stage.quality.relay': { en: 'TURN relay', fr: 'Relais TURN' },
  'stage.quality.direct': { en: 'Direct', fr: 'Direct' },
  'stage.quality.directMs': { en: 'Direct {n} ms', fr: 'Direct {n} ms' },
  'stage.quality.fairMs': { en: 'Fair {n} ms', fr: 'Moyen {n} ms' },
  'stage.quality.slowMs': { en: 'Slow {n} ms', fr: 'Lent {n} ms' },
  'stage.bitrate': { en: 'Incoming video bitrate', fr: 'Débit vidéo reçu' },
  'stage.browserPip': { en: 'Show video in a floating window (Picture-in-Picture)', fr: 'Afficher la vidéo en surimpression (Picture-in-Picture)' },
  'stage.pipAction': { en: 'Pop-out', fr: 'Surimpression' },
  'stage.maxMembers': { en: 'Group call: {n} of {max} max', fr: 'Conférence : {n} participants sur {max} maximum' },
  'stage.maxMembersUnlimited': { en: 'Group call: {n} participants, no limit', fr: 'Conférence : {n} participants, sans limite' },
  'stage.error.title': { en: 'The session has ended', fr: 'La session s’est arrêtée' },
  'stage.error.fallback': { en: 'An unexpected error occurred.', fr: 'Une erreur inattendue est survenue.' },
  'stage.error.retry': { en: 'Try again', fr: 'Réessayer' },
  'stage.error.back': { en: 'Back to home', fr: 'Retour à l’accueil' },

  // ─────────────────────────────── control bar
  'ctl.mic.off': { en: 'Mute mic (M)', fr: 'Couper le micro (M)' },
  'ctl.mic.on': { en: 'Unmute mic (M)', fr: 'Activer le micro (M)' },
  'ctl.cam.off': { en: 'Turn camera off (C)', fr: 'Couper la caméra (C)' },
  'ctl.cam.on': { en: 'Turn camera on (C)', fr: 'Activer la caméra (C)' },
  'ctl.next': { en: 'Skip to the next person (N)', fr: 'Passer à l’interlocuteur suivant (N)' },
  'ctl.stop': { en: 'Leave the conversation (Q)', fr: 'Quitter la conversation (Q)' },
  'ctl.add': { en: 'Add a participant (A)', fr: 'Ajouter un participant (A)' },
  'ctl.add.full': { en: 'The group call is full', fr: 'La conférence est complète' },
  'ctl.chat': { en: 'Open chat (T)', fr: 'Ouvrir le chat (T)' },
  'ctl.volume': { en: 'Caller volume', fr: 'Volume de l’appelant' },
  'ctl.volume.level': { en: 'Volume level', fr: 'Niveau du volume' },
  'ctl.devices': { en: 'Devices', fr: 'Périphériques' },

  // ─────────────────────────────── devices menu
  'devices.camera': { en: 'Camera', fr: 'Caméra' },
  'devices.microphone': { en: 'Microphone', fr: 'Microphone' },
  'devices.hotswap': { en: 'Switching applies instantly, without ending the call.', fr: 'Le changement de périphérique est appliqué instantanément, sans interrompre l’appel.' },
  'devices.needPermission': { en: 'Allow camera access to list your devices.', fr: 'Autorisez la caméra pour voir la liste des périphériques.' },

  // ─────────────────────────────── session menu
  'session.more': { en: 'More session actions', fr: 'Plus d’actions de session' },
  'session.moreHint': { en: 'Report, block…', fr: 'Signaler, bloquer…' },
  'session.report': { en: 'Report this person', fr: 'Signaler cette personne' },
  'session.block': { en: 'Block this person', fr: 'Bloquer cette personne' },
  'session.blockConfirm': { en: 'Confirm permanent block', fr: 'Confirmer le blocage définitif' },

  // ─────────────────────────────── chat
  'chat.panel': { en: 'Text chat', fr: 'Chat texte' },
  'chat.title': { en: 'Chat', fr: 'Chat' },
  'chat.connected': { en: 'Connected to your match', fr: 'Connecté à l’interlocuteur' },
  'chat.standby': { en: 'Available during the conversation', fr: 'Disponible pendant la conversation' },
  'chat.close': { en: 'Close chat', fr: 'Fermer le chat' },
  'chat.messages': { en: 'Messages', fr: 'Messages' },
  'chat.system': { en: 'System message', fr: 'Message système' },
  'chat.pending': { en: ' · sending…', fr: ' · envoi…' },
  'chat.sent': { en: ' · sent', fr: ' · envoyé' },
  'chat.placeholder.live': { en: 'Type a message…', fr: 'Écrivez un message…' },
  'chat.placeholder.standby': { en: 'Chat becomes available during the conversation', fr: 'Le chat sera disponible pendant la conversation' },
  'chat.input': { en: 'Your message', fr: 'Votre message' },
  'chat.send': { en: 'Send', fr: 'Envoyer' },
  'chat.peerJoined': { en: 'A participant joined the call.', fr: 'Un participant a rejoint la conférence.' },
  'chat.peerLeft.alone': { en: 'A participant left — you are alone now.', fr: 'Un participant est parti, vous êtes seul.' },
  'chat.peerLeft': { en: 'A participant left the call.', fr: 'Un participant a quitté la conférence.' },

  // ─────────────────────────────── report dialog
  'report.title': { en: 'Report a participant', fr: 'Signaler un participant' },
  'report.close': { en: 'Close', fr: 'Fermer' },
  'report.target': { en: 'Who are you reporting?', fr: 'Participant concerné' },
  'report.category': { en: 'Main reason', fr: 'Motif principal' },
  'report.details': { en: 'Details (optional)', fr: 'Précisions (optionnel)' },
  'report.details.placeholder': { en: 'Briefly describe what happened', fr: 'Décrivez brièvement ce qui s’est passé' },
  'report.block': { en: 'Also block this person so we never match you again', fr: 'Bloquer cette personne pour ne plus jamais lui être associé' },
  'report.submit': { en: 'Send report', fr: 'Envoyer le signalement' },
  'report.cat.nudity': { en: 'Nudity or sexual content', fr: 'Nudité ou contenu sexuel' },
  'report.cat.harassment': { en: 'Harassment or insults', fr: 'Harcèlement ou insultes' },
  'report.cat.hate': { en: 'Hate speech', fr: 'Discours haineux' },
  'report.cat.violence': { en: 'Violence or threats', fr: 'Violence ou menace' },
  'report.cat.spam': { en: 'Spam or advertising', fr: 'Spam ou publicité' },
  'report.cat.minor': { en: 'Underage person', fr: 'Personne mineure' },
  'report.cat.other': { en: 'Other', fr: 'Autre' },

  // ─────────────────────────────── toasts
  'toast.close': { en: 'Dismiss notification', fr: 'Fermer la notification' },

  // ─────────────────────────────── toasts raised by the client
  'toast.connectFailed.title': { en: 'Connection failed', fr: 'Connexion impossible' },
  'toast.cameraDenied.title': { en: 'Camera access denied', fr: 'Accès caméra refusé' },
  'toast.cameraDenied.body': { en: 'This browser cannot access the camera (HTTPS required).', fr: 'Ce navigateur ne donne pas accès à la caméra (HTTPS requis).' },
  'toast.cameraSwitchFailed': { en: 'Could not switch camera', fr: 'Changement de caméra impossible' },
  'toast.micSwitchFailed': { en: 'Could not switch microphone', fr: 'Changement de micro impossible' },
  'toast.noPeer.title': { en: 'No one to talk to', fr: 'Aucun interlocuteur' },
  'toast.noPeer.send': { en: 'The message could not be sent.', fr: 'Le message n’a pas pu être envoyé.' },
  'toast.noPeer.block': { en: 'Nothing to block right now.', fr: 'Rien à bloquer pour le moment.' },
  'toast.blocked.title': { en: 'User blocked', fr: 'Utilisateur bloqué' },
  'toast.blocked.body': { en: 'You will not be matched with this person again.', fr: 'Vous ne serez plus mis en relation avec cette personne.' },
  'toast.reportSent.title': { en: 'Report sent', fr: 'Signalement envoyé' },
  'toast.reportSent.body': { en: 'Thank you — our moderation team will review it.', fr: 'Merci, notre équipe de modération va l’examiner.' },
  'toast.noGroup.title': { en: 'No group call', fr: 'Aucune conférence' },
  'toast.noGroup.body': { en: 'Join a group call first.', fr: 'Rejoignez d’abord une conférence.' },
  'toast.nobodyOnline.title': { en: 'Nobody online', fr: 'Personne en ligne' },
  'toast.nobodyOnline.body': { en: 'No match found — try again in a moment.', fr: 'Aucun interlocuteur trouvé, réessayez dans un instant.' },
  'toast.negotiationFailed': { en: 'Connection negotiation failed', fr: 'Négociation impossible' },

  // ─────────────────────────────── status lines (hook fallbacks)
  'status.searching.group': { en: 'Looking for a group call…', fr: 'Recherche d’une conférence' },
  'status.searching.solo': { en: 'Looking for someone…', fr: 'Recherche d’un interlocuteur' },
  'status.cameraPrompt': { en: 'Allowing camera and microphone', fr: 'Autorisation caméra et micro' },
  'status.negotiating': { en: 'Negotiating the connection', fr: 'Négociation de la connexion' },
  'status.signalUnavailable': { en: 'signalling service unavailable', fr: 'socket de signalisation indisponible' },
  'status.serverUnreachable': { en: 'cannot reach the server', fr: 'connexion au serveur impossible' },

  // ─────────────────────────────── server signalling codes → localised copy
  'server.unknown_event': { en: 'Unknown event', fr: 'Événement inconnu' },
  'server.suspended': { en: 'Access temporarily suspended', fr: 'Accès temporairement suspendu' },
  'server.no_session': { en: 'No active group call', fr: 'Aucune conférence active' },
  'server.solo_session': { en: 'Adding participants is only available in a group call', fr: 'L’ajout n’est possible qu’en conférence' },
  'server.session_full': { en: 'The group call is full', fr: 'La conférence est complète' },
  'server.rate_limited': { en: 'Too many messages, slow down', fr: 'Signalisation trop fréquente' },
  'server.invalid_sdp': { en: 'Connection description rejected', fr: 'Description SDP rejetée' },
  'server.invalid_candidate': { en: 'Network candidate rejected', fr: 'Candidat ICE rejeté' },
  'server.block_failed': { en: 'The block could not be saved', fr: 'Le blocage n’a pas pu être enregistré' },
  'server.no_target': { en: 'Choose who this applies to', fr: 'Choisissez l’interlocuteur concerné' },
  'server.no_peer': { en: 'That person has disconnected', fr: 'Interlocuteur déconnecté' },
  'server.replaced': { en: 'This connection was taken over in another tab.', fr: 'Connexion reprise dans un autre onglet.' },
  'server.empty_queue.title': { en: 'Nobody waiting', fr: 'Personne en attente' },
  'server.empty_queue.body': { en: 'No one is available right now.', fr: 'Aucun interlocuteur disponible pour le moment.' },
  'toast.tooMany.title': { en: 'Too many requests', fr: 'Trop de requêtes' },
  'toast.addFailed.title': { en: 'Could not add participant', fr: 'Ajout impossible' },
  'status.unstable': { en: 'Connection unstable, reconnecting…', fr: 'Connexion instable, reprise en cours' },
  'toast.protocolError.title': { en: 'Protocol error', fr: 'Erreur de protocole' },
  'toast.peerLost.title': { en: 'Participant lost', fr: 'Participant perdu' },
  'toast.peerLost.body': { en: 'The connection with a participant was dropped.', fr: 'La connexion avec un participant a été coupée.' },
  'toast.iceFailed.body': { en: 'Network too restrictive: looking for someone new.', fr: 'Réseau trop restrictif : recherche d’un nouvel interlocuteur.' },
  'error.peerDisconnected': { en: 'The other person disconnected.', fr: 'L’interlocuteur s’est déconnecté.' },
  'error.peerSkipped': { en: 'The other person moved on.', fr: 'L’interlocuteur est passé au suivant.' },
  'error.peerLeft': { en: 'The other person left the conversation.', fr: 'L’interlocuteur a quitté la conversation.' },
  'error.closedByReport': { en: 'The session was closed after your report.', fr: 'La session a été fermée après votre signalement.' },
  'error.closedByBlock': { en: 'The session was closed and that person was blocked.', fr: 'La session a été fermée et cette personne est bloquée.' },
  'media.notAllowed': { en: 'You denied camera or microphone access. Allow it and try again.', fr: 'Vous avez refusé l’accès à la caméra ou au micro. Autorisez-le puis réessayez.' },
  'media.notFound': { en: 'No camera or microphone detected on this device.', fr: 'Aucune caméra ou aucun micro détecté sur cet appareil.' },
  'media.notReadable': { en: 'Your camera is already in use by another application.', fr: 'Votre caméra est déjà utilisée par une autre application.' },
  'media.generic': { en: 'Your camera or microphone could not be started.', fr: 'Impossible de démarrer votre caméra ou votre micro.' },
  'status.signalRefused': { en: 'The server refused the signalling connection.', fr: 'Le serveur a refusé la connexion de signalisation.' },
  'error.checkNetwork': { en: 'Check your connection and try again.', fr: 'Vérifiez votre connexion puis réessayez.' },
  'error.checkOrigins': { en: 'Fix ALLOWED_ORIGINS on the server, then try again.', fr: 'Vérifiez ALLOWED_ORIGINS côté serveur, puis réessayez.' },
  'stage.cameraPromptHint': { en: 'Allow access in your browser: your picture appears below before matching.', fr: 'Autorisez l’accès dans votre navigateur : votre image apparaît ci-dessous avant la mise en relation.' },
  'stage.waitingMatch': { en: 'You will be connected as soon as someone is available.', fr: 'Vous serez mis en relation dès qu’une personne sera disponible.' },
  'stage.negotiationNote': { en: 'Audio and video may take a few seconds to appear.', fr: 'Le son et l’image peuvent mettre quelques secondes à apparaître.' },
  'stage.duration': { en: 'Current session duration', fr: 'Durée de la session en cours' },
  'chat.peerLeftCount': { en: 'A participant left ({n} remaining).', fr: 'Un participant est parti ({n} restants).' },
  'chat.promoted': { en: 'This call just became a group call — use Add to bring more people in.', fr: 'Cet appel vient de devenir une conférence — utilisez Ajouter pour faire entrer d’autres personnes.' },

  // ─────────────────────────────── invitations & share
  'invite.joining': { en: 'Joining the discussion…', fr: 'Entrée dans la discussion…' },
  'invite.full': { en: 'This discussion is full.', fr: 'Cette discussion est complète.' },
  'invite.invalid': { en: 'This invitation link is invalid or expired.', fr: 'Ce lien d’invitation est invalide ou expiré.' },
  'invite.invalid.title': { en: 'Could not join', fr: 'Entrée impossible' },
  'invite.invalid.body': { en: 'The link is invalid, revoked, or the discussion has ended.', fr: 'Le lien est invalide, révoqué, ou la discussion est terminée.' },
  'invite.blocked': { en: 'You cannot join this discussion.', fr: 'Vous ne pouvez pas rejoindre cette discussion.' },
  'invite.blocked.title': { en: 'Access refused', fr: 'Accès refusé' },
  'invite.blocked.body': { en: 'A member of this discussion has blocked you.', fr: 'Un membre de cette discussion vous a bloqué.' },
  'invite.alreadyIn': { en: 'You are already in a session.', fr: 'Vous êtes déjà en session.' },
  'invite.revoked.title': { en: 'Invitation link revoked', fr: 'Lien d’invitation révoqué' },
  'invite.revoked.body': { en: 'Nobody else can enter with the old link.', fr: 'Personne d’autre ne pourra entrer avec l’ancien lien.' },
  'toast.noSession.title': { en: 'No active session', fr: 'Aucune session active' },
  'toast.noSession.body': { en: 'Start or join a conversation first.', fr: 'Démarrez ou rejoignez d’abord une conversation.' },
  'toast.invalidLimit.title': { en: 'Invalid limit', fr: 'Limite invalide' },
  'toast.invalidLimit.body': { en: 'Use 0 for no limit, or at least 2.', fr: 'Utilisez 0 pour aucune limite, ou au moins 2.' },

  // ─────────────────────────────── invite entry point
  'ctl.invite': { en: 'Invite (I)', fr: 'Inviter (I)' },
  'session.invite': { en: 'Invitation link…', fr: 'Lien d’invitation…' },
  'session.cap': { en: 'Participant limit', fr: 'Plafond de participants' },
  'session.cap.hint': { en: '0 means no limit. Lowering it never ejects anyone.', fr: '0 signifie sans limite. Le baisser n’éjecte personne.' },
  'session.cap.apply': { en: 'Apply', fr: 'Appliquer' },

  // ─────────────────────────────── share dialog
  'share.title': { en: 'Invite with a link', fr: 'Inviter par lien' },
  'share.lead': { en: 'Anyone with the link enters directly, without knocking.', fr: 'Toute personne avec le lien entre directement, sans frapper.' },
  'share.generating': { en: 'Generating the link…', fr: 'Génération du lien…' },
  'share.copy': { en: 'Copy', fr: 'Copier' },
  'share.copied': { en: 'Copied!', fr: 'Copié !' },
  'share.share': { en: 'Share…', fr: 'Partager…' },
  'share.revoke': { en: 'Revoke', fr: 'Révoquer' },
  'share.regenerate': { en: 'Regenerate', fr: 'Régénérer' },
  'share.capLabel': { en: 'Limit applied right away', fr: 'Limite appliquée aussitôt' },
  'share.capHint': { en: 'Sets the room limit immediately.', fr: 'Fixe aussitôt la limite du salon.' },
  'share.dead': { en: 'This link was revoked. Generate a new one to keep inviting.', fr: 'Ce lien a été révoqué. Générez-en un nouveau pour continuer d’inviter.' },
  'share.hint': { en: 'One link per discussion, shared by every member. Revoking blocks future arrivals without ejecting anyone.', fr: 'Un seul lien par discussion, partagé par tous les membres. Révoquer bloque les arrivées futures sans éjecter personne.' },

  // ─────────────────────────────── join screen
  'join.loading': { en: 'Loading the invitation…', fr: 'Chargement de l’invitation…' },
  'join.title': { en: 'You have been invited', fr: 'On vous invite' },
  'join.solo': { en: 'One-to-one call', fr: 'Tête-à-tête' },
  'join.group': { en: 'Group call', fr: 'Conférence' },
  'join.solo.hint': { en: 'Joining turns it into a group call.', fr: 'Y entrer la transforme en conférence.' },
  'join.members': { en: '{n} in the discussion', fr: '{n} dans la discussion' },
  'join.maxUnlimited': { en: 'no limit', fr: 'sans limite' },
  'join.max': { en: 'limit: {n}', fr: 'limite : {n}' },
  'join.cta': { en: 'Join the discussion', fr: 'Rejoindre la discussion' },
  'join.back': { en: 'Back to home', fr: 'Retour à l’accueil' },
  'join.dead.title': { en: 'This link no longer works', fr: 'Ce lien ne fonctionne plus' },
  'join.dead.body': { en: 'It was revoked, or the discussion has ended.', fr: 'Il a été révoqué, ou la discussion est terminée.' },
  'landing.preset': { en: 'Search preset from the shared link.', fr: 'Recherche pré-réglée par le lien partagé.' },
  'status.serverLost': { en: 'Server connection lost.', fr: 'Connexion au serveur perdue.' },
  'toast.serverDown.title': { en: 'Server unreachable', fr: 'Serveur injoignable' },
  'error.sessionShutdown': { en: 'The service is restarting — the session was interrupted.', fr: 'Le service redémarre, la session a été interrompue.' },
  'error.sessionEnded': { en: 'Session ended.', fr: 'Session terminée.' },
  'media.overconstrained': { en: 'The requested camera is not available.', fr: 'La caméra demandée n’est pas disponible.' },
  'media.insecure': { en: 'Media access requires a secure connection (HTTPS).', fr: 'L’accès aux médias exige une connexion sécurisée (HTTPS).' },
  'media.error': { en: 'Media error: {name}', fr: 'Erreur média : {name}' },
  'signal.closedBefore': { en: 'connection closed before “{event}” (code {code}) — check ALLOWED_ORIGINS on the server', fr: 'connexion fermée avant « {event} » (code {code}) : vérifiez ALLOWED_ORIGINS côté serveur' },
} as const;

export type StringKey = keyof typeof STRINGS;

export function translate(lang: Lang, key: StringKey, vars?: Record<string, string | number>): string {
  const entry = STRINGS[key] as Entry;
  let text: string = entry[lang] ?? entry.en;
  if (vars) {
    for (const [name, value] of Object.entries(vars)) {
      text = text.replaceAll(`{${name}}`, String(value));
    }
  }
  return text;
}

let activeLang: Lang = DEFAULT_LANG;

/** Stable translator reading the ACTIVE language at call time — safe inside
 *  memoised callbacks and refs (no stale closure on language switch). */
export function getT() {
  return (key: StringKey, vars?: Record<string, string | number>): string =>
    translate(activeLang, key, vars);
}

function readStoredLang(): Lang {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY);
    return raw === 'fr' ? 'fr' : raw === 'en' ? 'en' : DEFAULT_LANG;
  } catch {
    return DEFAULT_LANG;
  }
}

interface LangContextValue {
  lang: Lang;
  setLang: (lang: Lang) => void;
}

const LangContext = createContext<LangContextValue>({ lang: DEFAULT_LANG, setLang: () => {} });

export function LangProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(readStoredLang);

  useEffect(() => {
    activeLang = lang;
    document.documentElement.lang = lang;
  }, [lang]);

  const setLang = useCallback((next: Lang) => {
    setLangState(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      /* private mode — the choice just won't persist */
    }
  }, []);

  const value = useMemo(() => ({ lang, setLang }), [lang, setLang]);
  return <LangContext.Provider value={value}>{children}</LangContext.Provider>;
}

export function useLang(): LangContextValue {
  return useContext(LangContext);
}

/** Returns a translate function bound to the active language. */
export function useT() {
  const { lang } = useLang();
  return useCallback(
    (key: StringKey, vars?: Record<string, string | number>) => translate(lang, key, vars),
    [lang],
  );
}

/** Segmented EN/FR toggle, shown in the top bar on every screen. */
export function LangSwitcher() {
  const { lang, setLang } = useLang();
  return (
    <div className="lang-switch" role="group" aria-label="Language / Langue">
      {(['en', 'fr'] as const).map((code) => (
        <button
          key={code}
          type="button"
          className={`lang-switch__option ${lang === code ? 'is-active' : ''}`}
          onClick={() => setLang(code)}
          aria-pressed={lang === code}
          aria-label={code === 'en' ? 'English' : 'Français'}
          title={code === 'en' ? 'English' : 'Français'}
        >
          {code.toUpperCase()}
        </button>
      ))}
    </div>
  );
}
