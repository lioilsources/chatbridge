-- Stub harness for the chatbridge Luanti mod. Mocks the `core` API, loads
-- init.lua, and drives the core.after queue to exercise the poll state machine
-- without a running server. Run: luajit mod/chatbridge/tests/harness.lua
--
-- unpack shim (LuaJIT is 5.1: global unpack; newer Lua: table.unpack)
local unpack = unpack or table.unpack

local INIT = (arg and arg[0] or ""):gsub("tests/harness%.lua$", "init.lua")
if INIT == "" or INIT == (arg and arg[0]) then INIT = "../init.lua" end

local scheduled, added, fetches, fake_result, canned
local passed, failed = 0, 0

local function check(cond, msg)
	if cond then passed = passed + 1 else
		failed = failed + 1
		print("  FAIL: " .. msg)
	end
end

-- Build a fresh mocked `core` and (re)load the mod. opts.settings is a table
-- of setting values; opts.players overrides the connected-players list.
local settings_values, players
local function load_mod(opts)
	opts = opts or {}
	settings_values = opts.settings or {}
	players = opts.players
	if players == nil then
		players = { { get_pos = function() return { x = 0, y = 10, z = 0 } end } }
	end
	scheduled, added, fetches, fake_result, canned = {}, {}, {}, nil, nil
	math.randomseed(1)
	_G.core = {
		request_http_api = function()
			return {
				fetch_async = function(req)
					fetches[#fetches + 1] = req
					return { id = #fetches }
				end,
				fetch_async_get = function() return fake_result end,
			}
		end,
		after = function(d, fn, ...)
			scheduled[#scheduled + 1] = { fn = fn, args = { ... } }
		end,
		log = function() end,
		settings = { get = function(_, k) return settings_values[k] end },
		get_connected_players = function() return players end,
		add_entity = function(pos, name)
			added[#added + 1] = { pos = pos, name = name }
			return {} -- non-nil = success
		end,
		parse_json = function() return canned end,
	}
	dofile(INIT)
end

-- Run the next scheduled callback (FIFO).
local function step()
	local job = table.remove(scheduled, 1)
	if not job then return false end
	job.fn(unpack(job.args))
	return true
end

local function complete_ok()
	fake_result = { completed = true, succeeded = true, code = 200, data = "{}" }
end

---------------------------------------------------------------------------
print("scenario: spawn event applied, last_seq advances")
load_mod()
check(#fetches == 0 and #scheduled == 1, "load schedules exactly one poll")
step() -- poll(): starts fetch #1 with since=0, schedules check_pending
check(#fetches == 1, "poll starts one fetch")
check(fetches[1].url:find("since=0"), "first fetch uses since=0")
canned = {
	events = {
		{ seq = 1, type = "spawn", action = "chicken",
			params = { entity = "test:chicken", count = 2 }, ttl = 60 },
	},
	latest = 1,
}
complete_ok()
step() -- check_pending(): completed → process → spawn ×2 → schedule poll
check(#added == 2, "count=2 spawned two entities (got " .. #added .. ")")
check(added[1].name == "test:chicken", "spawned the requested entity")
do
	local dx, dz = added[1].pos.x, added[1].pos.z
	local d = math.sqrt(dx * dx + dz * dz)
	check(d >= 5 and d <= 15, "spawn distance within [5,15] (got " .. string.format("%.1f", d) .. ")")
	check(added[1].pos.y == 11, "spawn y = player.y + 1")
end
step() -- next poll(): should use since=1 now
check(fetches[2] and fetches[2].url:find("since=1"), "second fetch uses since=1 (last_seq advanced)")

---------------------------------------------------------------------------
print("scenario: reset jumps to latest, no spawns")
load_mod()
step() -- poll #1 (since=0)
canned = { reset = true, latest = 42 }
complete_ok()
step() -- check_pending → process → reset
check(#added == 0, "reset applies no events")
step() -- next poll
check(fetches[2] and fetches[2].url:find("since=42"), "after reset, fetch uses since=latest (42)")

---------------------------------------------------------------------------
print("scenario: fallback spawn point used when no players online")
load_mod({ settings = { chatbridge_spawn_fallback = "0,8,0" }, players = {} })
step() -- poll
canned = {
	events = {
		{ seq = 1, type = "spawn", action = "rocks",
			params = { entity = "test:rock", count = 2 }, ttl = 10 },
	},
	latest = 1,
}
complete_ok()
step() -- process → spawn at fallback
check(#added == 2, "empty world + fallback spawns 2 (got " .. #added .. ")")
do
	local d = math.sqrt(added[1].pos.x ^ 2 + added[1].pos.z ^ 2)
	check(d >= 5 and d <= 15, "fallback spawn distance within [5,15]")
	check(added[1].pos.y == 9, "fallback spawn y = 8 + 1")
end

---------------------------------------------------------------------------
print("scenario: no players and no fallback → nothing spawns")
load_mod({ players = {} })
step()
canned = {
	events = {
		{ seq = 1, type = "spawn", action = "rocks",
			params = { entity = "test:rock", count = 2 }, ttl = 10 },
	},
	latest = 1,
}
complete_ok()
step()
check(#added == 0, "empty world without a fallback spawns nothing")

---------------------------------------------------------------------------
print("scenario: HTTP error is logged, not applied")
load_mod()
step()
fake_result = { completed = true, succeeded = false, code = 502, data = "" }
canned = nil
step() -- process: non-200 → no spawn, no crash
check(#added == 0, "failed poll spawns nothing")
step()
check(fetches[2] ~= nil, "poll loop continues after a failed fetch")

---------------------------------------------------------------------------
print(string.format("\n%d passed, %d failed", passed, failed))
os.exit(failed == 0 and 0 or 1)
