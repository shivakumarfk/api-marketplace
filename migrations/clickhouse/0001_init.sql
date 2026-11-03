-- Customer call logs: metadata only, never bodies. 60-day retention.
-- ReplacingMergeTree on request_id so a replayed usage event is not double counted.
CREATE TABLE IF NOT EXISTS call_logs
(
    ts                   DateTime64(3, 'UTC'),
    request_id           String,
    org_id               String,
    key_id               String,
    env                  LowCardinality(String),
    api_id               String,
    subscription_id      String,
    method               LowCardinality(String),
    path                 String,
    status               UInt16,
    latency_ms           UInt32,
    upstream_latency_ms  UInt32,
    req_bytes            UInt64,
    resp_bytes           UInt64,
    billable             UInt8,
    cost_micro           Int64,
    provider_cost_micro  Int64,
    error_code           LowCardinality(String),
    client_ip            String,
    retry_count          UInt8
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMMDD(ts)
ORDER BY (org_id, ts, request_id)
TTL toDateTime(ts) + INTERVAL 60 DAY;

-- Per-minute rollups for dashboards and alerts.
CREATE TABLE IF NOT EXISTS usage_1m
(
    minute       DateTime('UTC'),
    org_id       String,
    api_id       String,
    key_id       String,
    calls        UInt64,
    errors       UInt64,
    cost_micro   Int64,
    latency      AggregateFunction(quantiles(0.5, 0.95), UInt32)
)
ENGINE = AggregatingMergeTree
ORDER BY (org_id, minute, api_id, key_id)
TTL minute + INTERVAL 60 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS usage_1m_mv TO usage_1m AS
SELECT
    toStartOfMinute(ts)                 AS minute,
    org_id, api_id, key_id,
    count()                             AS calls,
    countIf(status >= 500 OR error_code != '') AS errors,
    sum(cost_micro)                     AS cost_micro,
    quantilesState(0.5, 0.95)(latency_ms) AS latency
FROM call_logs
GROUP BY minute, org_id, api_id, key_id;

CREATE TABLE IF NOT EXISTS audit_events
(
    ts           DateTime64(3, 'UTC'),
    org_id       String,
    actor_id     String,
    actor_type   LowCardinality(String),
    action       LowCardinality(String),
    target_type  LowCardinality(String),
    target_id    String,
    ip           String,
    metadata     String
)
ENGINE = MergeTree
ORDER BY (org_id, ts)
TTL toDateTime(ts) + INTERVAL 60 DAY; -- raise to 365 in Phase 2
