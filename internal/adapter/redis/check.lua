-- Atomic check-and-consume for one gateway call.
-- KEYS[1] rate-limit bucket (per key + API)
-- KEYS[2] quota counter (per subscription + period)
-- KEYS[3] upstream bucket (per API, shared by all customers)
-- KEYS[4] prepaid balance (per org, micro-rupees)
-- ARGV: now_ms, rate_rps, quota_limit, quota_ttl_s, upstream_rps, price_micro, prepaid
-- Returns {code, quota_remaining, balance}
--   code: 0 OK, 1 RATE_LIMITED, 2 QUOTA_EXCEEDED, 3 UPSTREAM_LIMIT,
--         4 INSUFFICIENT_BALANCE, 5 BALANCE_UNAVAILABLE

local now = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local quota = tonumber(ARGV[3])
local quota_ttl = tonumber(ARGV[4])
local up_rate = tonumber(ARGV[5])
local price = tonumber(ARGV[6])
local prepaid = tonumber(ARGV[7])

-- Token bucket with capacity = max(rate, 1). Returns allowed, tokens_after.
local function take(key, r)
  if r <= 0 then return true, -1 end
  local burst = math.max(r, 1)
  local v = redis.call('HMGET', key, 't', 'ts')
  local tokens = tonumber(v[1])
  local ts = tonumber(v[2])
  if tokens == nil then tokens = burst; ts = now end
  tokens = math.min(burst, tokens + math.max(0, now - ts) * r / 1000)
  if tokens < 1 then return false, tokens end
  return true, tokens - 1
end

local function store(key, r, tokens)
  if r <= 0 then return end
  redis.call('HSET', key, 't', tokens, 'ts', now)
  redis.call('PEXPIRE', key, math.ceil(math.max(r, 1) / r * 1000) + 60000)
end

-- 1. plan rate limit
local ok_rl, rl_tokens = take(KEYS[1], rate)
if not ok_rl then return {1, -1, -1} end

-- 2. period quota
local used = 0
if quota > 0 then
  used = tonumber(redis.call('GET', KEYS[2]) or '0')
  if used >= quota then return {2, 0, -1} end
end

-- 3. provider upstream limit
local ok_up, up_tokens = take(KEYS[3], up_rate)
if not ok_up then return {3, -1, -1} end

-- 4. prepaid balance (fail closed if unknown)
local balance = -1
if prepaid == 1 then
  local b = redis.call('GET', KEYS[4])
  if b == false then return {5, -1, -1} end
  balance = tonumber(b)
  if balance < price then return {4, -1, balance} end
end

-- All passed: consume.
store(KEYS[1], rate, rl_tokens)
store(KEYS[3], up_rate, up_tokens)
local remaining = -1
if quota > 0 then
  used = redis.call('INCR', KEYS[2])
  redis.call('EXPIRE', KEYS[2], quota_ttl)
  remaining = quota - used
end
if prepaid == 1 and price > 0 then
  balance = redis.call('DECRBY', KEYS[4], price)
end
return {0, remaining, balance}
