# chatbridge

Twitch chat → Go bridge → Luanti mod. Streamer neinstaluje nic.

## Build & test
go build ./... && go vet ./... && go test -race ./...

## Architektura
Mod POLLUJE bridge přes GET /events?since=N. Luanti Lua nemá sockety.
core.request_http_api() se MUSÍ volat na top-levelu init.lua.

## Nesahat
- Formát eventu a HTTP API v §2 plánu jsou zafixované.
- Do herního světa nikdy nevkládat text ani přezdívky z chatu.
- Žádný Twitch OAuth. Anonymní justinfan IRC.

## Konvence
- Chyby wrapovat: fmt.Errorf("...: %w", err)
- Logování log/slog, strukturovaně
- Testy nesmí sahat na síť ani time.Now — čas injektovat
- Jedna fáze plánu = jeden commit; další fáze až po splnění akceptačního kritéria
