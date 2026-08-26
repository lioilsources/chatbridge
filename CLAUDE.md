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

## Zjištěno při fázi 5

- `secure.http_mods` NELZE nastavit z minetest.conf hry — Luanti hlásí
  "Secure setting ... isn't allowed" a ignoruje to. Musí být v configu hráče
  nebo serveru. Hra si nesmí sama povolit síť, a je to tak správně.
- Mod bez `chatbridge_url` schválně spí (ship-uje se uvnitř DoggioWars, takže
  většina hráčů ho nikdy nepoužije a nemá jim zaplevelovat log).
- Chráněná zóna počítá 3D vzdálenost. Vodorovná by chránila nekonečný sloup,
  což by v létací hře znamenalo celý vzdušný prostor nad spawnem.
- `chatbridge.register_on_spawn(fn)` je háček pro hru: bridge umí jen
  "spawni entitu X". DoggioWars přes něj monstru přiřadí ostrov, bez kterého
  se do vteřiny samo smaže. Žádná strana na té druhé nezávisí.
- Mod se vendoruje do DoggioWars přes `tools/sync-to-doggiowars.sh`.
