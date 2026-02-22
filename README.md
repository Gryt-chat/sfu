<div align="center">
  <img src="https://raw.githubusercontent.com/Gryt-chat/client/main/public/logo.svg" width="80" alt="Gryt logo" />
  <h1>Gryt SFU</h1>
  <p>Selective Forwarding Unit for the <a href="https://github.com/Gryt-chat/gryt">Gryt</a> voice chat platform.<br />High-performance Go media server built with <a href="https://github.com/pion/webrtc">Pion WebRTC</a>.</p>
</div>

<br />

## Quick Start

```bash
cp env.example .env
go run ./cmd/sfu
```

Starts on **http://localhost:5005**.

## Configuration

See `env.example` for all options. Key variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `5005` | HTTP/WebSocket port |
| `STUN_SERVERS` | `stun:stun.l.google.com:19302` | Comma-separated STUN servers |
| `ICE_UDP_PORT_MIN` | — | Min UDP port for WebRTC media |
| `ICE_UDP_PORT_MAX` | — | Max UDP port for WebRTC media |
| `ICE_ADVERTISE_IP` | — | Advertised IP for NAT traversal |

## Documentation

Full docs at **[docs.gryt.chat/docs/sfu](https://docs.gryt.chat/docs/sfu)**:

- [SFU Overview](https://docs.gryt.chat/docs/sfu) — architecture, track management, connection states
- [Voice Debugging](https://docs.gryt.chat/docs/sfu/voice-debugging) — troubleshooting audio issues
- [Deployment](https://docs.gryt.chat/docs/deployment) — Docker Compose, Kubernetes

## License

[AGPL-3.0](https://github.com/Gryt-chat/gryt/blob/main/LICENSE) — Part of [Gryt](https://github.com/Gryt-chat/gryt)
