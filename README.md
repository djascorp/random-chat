# RandomChat — chat vidéo aléatoire

Application de chat vidéo aléatoire inspirée d'Omegle : on arrive, on autorise sa caméra,
on est mis en relation avec une personne disponible, on discute, puis on passe au suivant.

Ce dépôt contient le MVP décrit dans le plan d'implémentation : **Go + WebRTC natif + React**,
avec une architecture modulaire pensée pour grandir (multi-instances, SFU) sans réécriture.

---

## 1. Ce qui fonctionne

| Domaine | État |
| --- | --- |
| Mise en relation aléatoire (file d'attente, `Suivant`, annulation, expiration) | ✅ |
| Appel vidéo 1:1 pair à pair (WebRTC, STUN, repli TURN) | ✅ |
| Signalisation WebSocket validée côté serveur | ✅ |
| Chat texte pendant la session | ✅ |
| Signalement, blocage, suspensions automatiques | ✅ |
| Limitation de débit par utilisateur et par IP | ✅ |
| Métriques (`/metrics`, `/api/stats`) et journal d'audit | ✅ |
| API d'administration protégée par jeton | ✅ |
| PostgreSQL (persistance) + repli mémoire | ✅ |
| Interface responsive, thème sombre, mobile inclus | ✅ |
| Tests unitaires, tests d'intégration WebSocket, `-race` | ✅ |

Non inclus volontairement dans ce MVP : comptes sociaux, cadeaux virtuels, filtres avancés,
enregistrement des sessions, SFU.

---

## 2. Architecture

```
Navigateur A  ⇄  serveur Go (signalisation, matching, modération)  ⇄  Navigateur B
      └───────────────────── flux média WebRTC direct ──────────────────────┘
                     (relayé par TURN uniquement si nécessaire)
```

- **Le serveur ne voit jamais la vidéo.** Il échange des offres/réponses SDP et des
  candidats ICE, choisit les interlocuteurs, applique les règles et ferme les sessions.
- **Le matching vit en mémoire**, sérialisé par un seul verrou : c'est ce qui garantit
  qu'un utilisateur ne peut appartenir qu'à une seule session, même quand deux clics
  arrivent au même instant.
- **PostgreSQL est hors du chemin chaud** : les écritures d'audit partent dans une
  goroutine, la signalisation ne bloque jamais sur la base.
- **Redis n'est pas nécessaire au MVP.** Le jour où plusieurs instances Go tournent en
  parallèle, la file d'attente et les sessions deviennent les deux seuls composants à
  déporter derrière une interface.

### Pile technique

| Couche | Choix |
| --- | --- |
| Frontend | React 18 + TypeScript + Vite |
| Vidéo | WebRTC natif (`RTCPeerConnection`) |
| Signalisation | WebSocket (`gorilla/websocket`) |
| Backend | Go (bibliothèque standard, `net/http`) |
| Persistance | PostgreSQL via `pgx`, repli mémoire |
| Réseau média | STUN public + TURN (coturn) |
| Déploiement | Docker multi-étapes, une seule image |

---

## 3. Démarrage

### Prérequis

- Go 1.24+ (testé avec 1.27)
- Node.js 20+ (testé avec 24)
- Optionnel : Docker + Docker Compose, PostgreSQL 14+

### Développement en deux terminaux

```bash
make deps            # modules Go + paquets npm

# terminal 1 — backend sur :8080
make dev-api

# terminal 2 — frontend sur :5173, proxy /api et /ws vers :8080
make dev-web
```

Ouvrez <http://localhost:5173>. Deux onglets (ou deux navigateurs) suffisent pour tester une
mise en relation complète.

> La caméra n'est accessible qu'en `https://` ou sur `localhost`. Pour tester depuis un
> téléphone sur le réseau local, il faut un tunnel HTTPS (Caddy, ngrok, Cloudflare Tunnel).

### Mode production en une commande

```bash
make build           # bundle React + binaire Go
make run             # sert l'API et le SPA sur :8080
```

### Docker

```bash
cp .env.example .env          # renseignez ADMIN_TOKEN et, en prod, TURN_*
make docker-up                # API + PostgreSQL
make docker-up-turn           # ajoute le relais coturn
```

---

## 4. Configuration

Toutes les variables ont une valeur par défaut utilisable en local. Voir `.env.example`
pour la liste complète ; les plus importantes :

| Variable | Défaut | Rôle |
| --- | --- | --- |
| `ADDR` | `:8080` | Adresse d'écoute |
| `ALLOWED_ORIGINS` | `localhost:5173`, `localhost:8080` | Origines **supplémentaires** autorisées à ouvrir le WebSocket. Le *same-origin* est toujours accepté, donc un déploiement qui sert le SPA depuis ce même serveur fonctionne sans rien configurer. N'y listez que les autres origines (serveur de dev Vite, domaine séparé). **Jamais `*` en production.** |
| `DATABASE_URL` | *(vide)* | Vide = stockage mémoire. |
| `ICE_SERVERS` | STUN Google | Tableau JSON au format `RTCIceServer`. |
| `TURN_URLS` / `TURN_USERNAME` / `TURN_CREDENTIAL` | *(vide)* | Raccourci qui ajoute un serveur TURN à la liste ICE. |
| `ADMIN_TOKEN` | *(vide)* | Vide = API d'administration désactivée (404). |
| `QUEUE_TIMEOUT` | `90s` | Durée maximale d'attente avant `queue.timeout`. |
| `AUTO_SUSPEND_AFTER` | `3` | Nombre de signalements distincts déclenchant une suspension. |
| `WEB_DIR` | `web/dist` | Dossier du SPA compilé. |

### Activer TURN (indispensable hors réseau ouvert)

1. Déployez coturn avec `deploy/coturn/turnserver.conf` en remplaçant le secret.
2. Renseignez côté API :
   ```bash
   TURN_URLS="turn:turn.exemple.net:3478?transport=udp,turn:turn.exemple.net:3478?transport=tcp"
   TURN_USERNAME=randomchat
   TURN_CREDENTIAL=<le secret>
   ```
3. Vérifiez que `GET /api/config` renvoie bien `features.turn_relay: true`.

Sans TURN, les appels échouent derrière un NAT symétrique ou un pare-feu d'entreprise —
c'est la cause numéro un d'échec de connexion en production.

---

## 5. Protocole WebSocket

Endpoint : `GET /ws?cid=<identifiant-client>`

L'identifiant `cid` est un jeton opaque stocké dans le `localStorage` (format
`^[a-z0-9-]{8,64}$`). Il sert à faire survivre blocages et suspensions à un rechargement de
page sans imposer de compte. Le serveur le traite comme **non fiable** : il le valide, et
applique la limitation de débit sur l'origine réseau.

Chaque trame est une enveloppe JSON : `{ "event": "...", "data": { ... } }`.

### Client → serveur

| Événement | Rôle |
| --- | --- |
| `queue.join` | Demander une mise en relation |
| `queue.cancel` | Annuler la recherche |
| `session.next` | Fermer la session et relancer la recherche |
| `session.leave` | Fermer la session sans rechercher |
| `rtc.offer` / `rtc.answer` | Transmettre une description SDP au pair uniquement |
| `rtc.ice` | Transmettre un candidat ICE au pair uniquement |
| `chat.send` | Envoyer un message texte |
| `report.create` | Signaler (et éventuellement bloquer) l'interlocuteur |
| `block.create` | Bloquer l'interlocuteur |

### Serveur → client

| Événement | Rôle |
| --- | --- |
| `server.hello` | Identifiant, version de protocole, serveurs ICE, limites |
| `queue.waiting` / `queue.canceled` / `queue.timeout` | État de la file d'attente |
| `match.found` | Session créée, avec le rôle (`offerer` / `answerer`) |
| `session.ended` | Fin de session avec le motif |
| `session.state` | État courant (`idle`, `waiting`, …) |
| `chat.message` / `chat.ack` | Message reçu / accusé d'envoi |
| `report.ack` / `block.ack` | Confirmation |
| `server.error` | Erreur de protocole (`code` stable, message français) |

### Règles appliquées côté serveur

1. Seuls les événements de la liste sont acceptés ; tout le reste est rejeté explicitement.
2. La signalisation n'est relayée que si l'émetteur et le destinataire sont **le même
   couple de la même session active**. Un client ne peut jamais adresser une autre session.
3. Taille bornée : enveloppe (64 Ko par défaut), SDP (100 Ko), candidat ICE (2 Ko),
   message de chat (1000 caractères), signalement (500 + 2000 caractères).
4. `rtc.offer` doit porter `type: "offer"` et `rtc.answer` `type: "answer"` : aucun blob
   arbitraire n'est retransmis.
5. Chaque famille d'événements a son propre seau à jetons par utilisateur
   (`queue.join`, `chat.send`, `report.create`, signalisation).
6. Une écriture concurrente par connexion : chaque socket a une unique goroutine
   d'écriture alimentée par un canal tamponné. Un client trop lent est déconnecté plutôt
   que de bloquer le serveur.

---

## 6. Structure du projet

```
random-chat/
├── cmd/api/main.go              # composition de l'application, arrêt gracieux
├── internal/
│   ├── config/                  # configuration par variables d'environnement
│   ├── httpapi/                 # routes REST, middlewares, admin, service du SPA
│   ├── matching/                # état, file d'attente, moteur de mise en relation
│   ├── metrics/                 # compteurs et histogrammes (format Prometheus)
│   ├── moderation/              # limites de débit, signalements, blocages, suspensions
│   ├── sessions/                # cycle de vie des sessions, identifiants aléatoires
│   ├── signaling/               # protocole, connexions WebSocket, routage des événements
│   └── storage/                 # port de persistance : mémoire et PostgreSQL
├── migrations/                  # schéma SQL idempotent
├── web/                         # application React (Vite + TypeScript)
│   └── src/
│       ├── components/          # interface
│       ├── hooks/useRandomChat  # machine à états de la session
│       ├── lib/                 # client de signalisation, enveloppe WebRTC
│       ├── styles/              # thème sombre, design system
│       └── types.ts             # contrat de protocole partagé
├── deploy/
│   ├── Dockerfile               # image unique : SPA + binaire
│   ├── compose.yaml             # API + PostgreSQL + coturn
│   └── coturn/turnserver.conf
├── Makefile
└── .env.example
```

### Invariants du backend

- **Une connexion = un propriétaire d'écriture.** Aucune écriture concurrente possible.
- **Un participant = au plus une session active**, garanti par le verrou du moteur de
  matching et par la table `byPeer` du gestionnaire de sessions.
- **Identifiants imprévisibles** : 128 bits aléatoires (`crypto/rand`) pour les sessions.
- **Toute session terminée libère ses ressources** et sort ses participants de la file.
- **Rien venant du navigateur n'est fiable** : taille, type d'événement, état de session,
  appartenance au couple, tout est revérifié.

---

## 7. Tests

```bash
make test         # suite complète
make test-race    # avec le détecteur de courses
make check        # vet + tests + typecheck TypeScript
```

Couverture fonctionnelle des tests :

- **Matching** : appariement, non-appariement des utilisateurs bloqués, annulation,
  expiration, et surtout `TestConcurrentJoinsNeverDoubleAssign` qui lance 200 connexions
  simultanées et vérifie qu'aucune session ne contient autre chose que deux pairs.
- **WebSocket (bout en bout, sur un vrai serveur `httptest`)** : `match.found` symétrique,
  relais SDP/ICE/chat, `Suivant` qui libère le partenaire, déconnexion qui ferme la session,
  signalement, rejet des trames invalides et des événements inconnus, cohérence de la file
  après un « suivant » massif.
- **Modération** : seaux à jetons, indépendance des clés, nettoyage, escalade des
  violations vers une suspension d'IP, symétrie des blocages.
- **Stockage mémoire** : collections bornées, et surtout qu'une **suspension n'est jamais
  évincée** (une sanction perdue remettrait un abuseur dans la file).

### Contrôle de mise en page

```bash
make dev-api                       # dans un terminal
cd web && npm run check:layout     # dans un autre
```

Le script lance deux instances Chrome headless avec une caméra synthétique, provoque une
vraie mise en relation entre elles, puis mesure la mise en page à six largeurs d'écran
(1440, 1180, 834, 390, 320 et paysage 740×360). Il vérifie que la page ne défile pas, que
la barre de contrôle ne recouvre pas la vidéo, que la vignette « Vous » reste dans le cadre
et qu'aucun élément ne masque le champ de saisie du chat. Les captures sont écrites dans
`web/.layout-shots/`.

Il existe parce qu'un simple coup d'œil ne suffit pas : la première version de l'interface
faisait passer la barre de contrôle sur **trois lignes** à 1180×700 — un élément en
`position: fixed` avec `left: 50%` mais sans `width` voit sa largeur plafonnée à la moitié
de l'écran. Aucune relecture du CSS ne l'avait révélé ; les mesures, si.

---

## 8. Administration et modération

Activez l'API avec `ADMIN_TOKEN`. Toutes les routes exigent
`Authorization: Bearer <token>` (comparaison à temps constant) ; sans jeton configuré,
elles répondent 404.

```bash
TOKEN=... ; HOST=http://localhost:8080

curl -H "Authorization: Bearer $TOKEN" $HOST/api/admin/stats
curl -H "Authorization: Bearer $TOKEN" "$HOST/api/admin/reports?status=open&limit=50"
curl -H "Authorization: Bearer $TOKEN" -X POST $HOST/api/admin/reports/<id>/status \
     -d '{"status":"actioned"}'
curl -H "Authorization: Bearer $TOKEN" -X POST $HOST/api/admin/users/<id>/suspend \
     -d '{"duration_minutes":1440,"reason":"nudité"}'
curl -H "Authorization: Bearer $TOKEN" $HOST/api/admin/actions     # journal d'audit
```

Automatismes en place :

- `AUTO_SUSPEND_AFTER` signalements distincts sur une même cible ⇒ suspension automatique.
- Violations répétées des limites de débit ⇒ suspension de l'IP sur `SUSPEND_DURATION`.
- Blocage ⇒ plus jamais appariés (vérifié par le moteur, pas par le client).
- Toute session signalée est fermée immédiatement et son motif est journalisé.

### Protection des mineurs

Un écran de consentement bloque l'accès caméra avant toute mise en relation : l'utilisateur
confirme être majeur et accepter les règles. Le bouton « Signaler » propose une catégorie
dédiée (`minor`). **Ce n'est qu'une barrière d'entrée** : une mise en production réelle
exige une vérification d'âge, une politique de conservation des données et une astreinte
de modération humaine.

---

## 9. Observabilité

| Endpoint | Usage |
| --- | --- |
| `GET /api/health` | Sonde de vivacité (connexions, sessions, file) |
| `GET /api/stats` | Compteurs agrégés publics, utilisés par la page d'accueil |
| `GET /metrics` | Format Prometheus, sans authentification |

Indicateurs à surveiller en priorité (voir §8 du plan) :

- **Matching** : `randomchat_match_wait_p95_ms` — le temps d'attente est le premier motif
  d'abandon.
- **Signalisation** : `randomchat_connections_active`, latence de routage.
- **Media** : taux de connexions `relay` face à `host`/`srflx` ; une part élevée de relais
  signifie un coût TURN élevé. La qualité affichée dans l'interface provient de
  `RTCPeerConnection.getStats()` (RTT, jitter, pertes, débit, type de candidat).
- **Sécurité** : `randomchat_reports_total`, `randomchat_violations_total`,
  `randomchat_suspensions_total`.

---

## 10. Feuille de route

Correspondance avec les phases du plan d'implémentation :

| Phase | Contenu | État |
| --- | --- | --- |
| 01 — Fondations | Dépôt, configuration, Docker, interfaces API/WS | ✅ |
| 02 — Moteur de mise en relation | États, file concurrente, `Suivant`, expirations | ✅ |
| 03 — Appel vidéo WebRTC | SDP/ICE, STUN/TURN, reprise réseau, libération des pistes | ✅ |
| 04 — Interface | Accueil, scène d'appel, chat, responsive | ✅ |
| 05 — Sécurité et administration | Signalements, blocages, limites, suspensions, audit | ✅ |
| 06 — Tests, optimisation, déploiement | Tests de charge, métriques, pilote | 🔶 base posée, à instrumenter en réel |

### Étapes suivantes naturelles

1. **Redis** pour la file d'attente et les sessions, afin d'autoriser plusieurs instances Go
   (l'interface `sessions.Store` et la file sont déjà isolées).
2. **Identifiants TURN éphémères** (HMAC avec expiration) servis par `/api/config`.
3. **Vérification d'âge** et politique de conservation des données par juridiction.
4. **SFU (LiveKit)** si le produit évolue vers du multi-participants ou de l'enregistrement.
5. **Tests de charge** sur le matching et les WebSockets avant d'annoncer un chiffre de
   capacité — un serveur vidéo coûte d'abord en bande passante TURN, pas en CPU Go.

---

## 11. Limites connues

- **Le blocage est lié au `cid` du navigateur**, effaçable en vidant le stockage local.
  Le passage à un compte léger (e-mail ou OAuth) est la seule vraie parade.
- **Le SPA et l'API partagent une seule image** : simple à opérer, à séparer (CDN) si le
  trafic statique devient significatif.
- **Pas de reprise de session après rechargement** : la session WebRTC est perdue, il faut
  relancer une recherche.
- **Le chat n'est pas modéré automatiquement.** Les messages transitent sans être stockés ;
  seuls les signalements sont persistés.
