-- Give back the quota unit and reserved price for a non-billable call.
-- KEYS[1] quota counter, KEYS[2] prepaid balance
-- ARGV: quota_limit, price_micro, prepaid
local quota = tonumber(ARGV[1])
local price = tonumber(ARGV[2])
local prepaid = tonumber(ARGV[3])

if quota > 0 then
  local used = tonumber(redis.call('GET', KEYS[1]) or '0')
  if used > 0 then redis.call('DECR', KEYS[1]) end
end
if prepaid == 1 and price > 0 and redis.call('EXISTS', KEYS[2]) == 1 then
  redis.call('INCRBY', KEYS[2], price)
end
return 1
