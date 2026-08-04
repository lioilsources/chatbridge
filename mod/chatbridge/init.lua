-- chatbridge — Luanti side.
--
-- Luanti Lua is sandboxed: no sockets, HTTP only, and only when the mod is
-- whitelisted in secure.http_mods. So the mod POLLS the bridge's GET /events;
-- the bridge never calls us.

-- request_http_api() MUST be called here, at load time, on the top level.
-- Called later it returns nil and there is no second chance.
local http = core.request_http_api()
if not http then
	core.log("error", "[chatbridge] HTTP API unavailable — add 'chatbridge' to "
		.. "secure.http_mods in minetest.conf (with secure.enable_security = true)")
	return
end

local BRIDGE_URL = core.settings:get("chatbridge_url") or "http://127.0.0.1:8093"
BRIDGE_URL = (BRIDGE_URL:gsub("/+$", "")) -- trim trailing slash

local POLL_INTERVAL = 1.0 -- seconds between polls
local CHECK_INTERVAL = 0.2 -- how often to check an in-flight fetch
local FETCH_TIMEOUT = 5
local MAX_EVENTS = 50
local SPAWN_R_MIN, SPAWN_R_MAX = 5, 15 -- spawn ring around the player
local SPAWN_COUNT_CAP = 10 -- hard cap regardless of params.count

-- Optional fixed spawn point used ONLY when no players are online (showcase /
-- testing). Empty = skip spawns when the world is empty. Format "x,y,z".
local SPAWN_FALLBACK
do
	local raw = core.settings:get("chatbridge_spawn_fallback")
	if raw then
		local x, y, z = raw:match("^%s*(-?%d+)%s*,%s*(-?%d+)%s*,%s*(-?%d+)%s*$")
		if x then
			SPAWN_FALLBACK = { x = tonumber(x), y = tonumber(y), z = tonumber(z) }
		end
	end
end

local last_seq = 0 -- highest event seq we have applied

---------------------------------------------------------------------------
-- Event handlers (phase 4: spawn only; weather/effect come later)
---------------------------------------------------------------------------

local function handle_spawn(ev)
	local params = ev.params or {}
	local entity = params.entity
	if type(entity) ~= "string" then
		core.log("warning", "[chatbridge] spawn '" .. tostring(ev.action)
			.. "' has no params.entity")
		return
	end
	local players = core.get_connected_players()
	if #players == 0 and not SPAWN_FALLBACK then
		return -- nobody online and no fallback point
	end
	local count = math.max(1, math.min(tonumber(params.count) or 1, SPAWN_COUNT_CAP))
	for _ = 1, count do
		local base
		if #players > 0 then
			base = players[math.random(#players)]:get_pos()
		else
			base = SPAWN_FALLBACK
		end
		local ang = math.random() * math.pi * 2
		local r = SPAWN_R_MIN + math.random() * (SPAWN_R_MAX - SPAWN_R_MIN)
		local pos = {
			x = base.x + math.cos(ang) * r,
			y = base.y + 1,
			z = base.z + math.sin(ang) * r,
		}
		if not core.add_entity(pos, entity) then
			core.log("warning", "[chatbridge] add_entity failed for " .. entity)
		end
	end
end

local handlers = {
	spawn = handle_spawn,
}

local function apply_event(ev)
	if type(ev) ~= "table" or type(ev.type) ~= "string" then
		return
	end
	local h = handlers[ev.type]
	if h then
		h(ev)
	end
	-- unknown types are ignored on purpose (forward-compatible)
end

---------------------------------------------------------------------------
-- Poll loop: one in-flight fetch at a time, checked on later ticks so the
-- server thread never blocks on the network.
---------------------------------------------------------------------------

local pending -- in-flight fetch handle, or nil

local function process(res)
	if not res.succeeded or res.code ~= 200 then
		core.log("warning", "[chatbridge] poll failed code=" .. tostring(res.code))
		return
	end
	local data = core.parse_json(res.data or "")
	if type(data) ~= "table" then
		core.log("warning", "[chatbridge] bad JSON from bridge")
		return
	end
	if data.reset then
		-- We fell outside the retained window: resync to latest, don't replay.
		last_seq = tonumber(data.latest) or last_seq
		core.log("action", "[chatbridge] resynced to seq " .. last_seq)
		return
	end
	for _, ev in ipairs(data.events or {}) do
		apply_event(ev)
		local s = tonumber(ev.seq)
		if s and s > last_seq then
			last_seq = s
		end
	end
end

local poll -- forward declaration

local function check_pending()
	local res = http.fetch_async_get(pending)
	if not res.completed then
		core.after(CHECK_INTERVAL, check_pending) -- still in flight
		return
	end
	pending = nil
	process(res)
	core.after(POLL_INTERVAL, poll) -- next cycle
end

poll = function()
	local url = BRIDGE_URL .. "/events?since=" .. last_seq .. "&max=" .. MAX_EVENTS
	pending = http.fetch_async({ url = url, timeout = FETCH_TIMEOUT, method = "GET" })
	core.after(CHECK_INTERVAL, check_pending)
end

core.after(POLL_INTERVAL, poll)
core.log("action", "[chatbridge] polling " .. BRIDGE_URL .. " every " .. POLL_INTERVAL .. "s")
