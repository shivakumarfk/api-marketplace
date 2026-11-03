# API Marketplace — AWS Cost Estimate (Phase 1, v0.1)

_Rough order-of-magnitude estimate, 2026-11-03. On-demand list prices in USD, AWS Mumbai (ap-south-1) unless noted, before 18% GST (reclaimable as input credit if GST-registered). Several line items are estimates, not quoted rates — confirm in the AWS Pricing Calculator before budgeting._

## Traffic assumptions
- Peak 2,000 RPS, average ~500 RPS → **~1.3 billion calls/month**.
- **Average payload ~5 KB per call** (≈1 KB request + 4 KB response). _This is the biggest unknown; network cost scales linearly with it._
- Architecture as in `mvp-architecture.md`, with SigNoz Cloud (India region) for observability.

## Production (Mumbai) — at full launch-peak traffic

| Item | Sizing | ~USD / month |
|---|---|---|
| EKS control plane | 1 cluster | 75 |
| EC2 worker nodes (Graviton) | ~8 × m7g.xlarge (4 vCPU/16 GB) at ~$0.117/hr: gateway ×3–4 across 3 AZs, control plane + consumers ×4 | 680 |
| Application Load Balancer | 1 ALB, LCU charges at this RPS (estimate) | 150 |
| RDS PostgreSQL | db.r7g.large Multi-AZ (~$199/mo single-AZ ×2) + ~200 GB gp3 | 450 |
| ElastiCache Redis | primary + replica, r7g.large class (estimate) | 350 |
| Event bus | Kinesis Data Streams on-demand, ~600 GB/month of usage events (estimate; MSK Serverless would be higher) | 150 |
| ClickHouse Cloud (AWS Mumbai) | Scale tier, 2 replicas × 8 GiB (~$0.30/unit-hr) + <0.5 TB storage (~$25/TB) | 500 |
| NAT gateways | 3 (one per AZ) + ~6.5 TB processed to/from providers (~$0.056/GB + ~$40/gateway) | 480 |
| Data transfer out to internet | ~6.5 TB (responses to customers + requests to providers) at ~$0.109/GB | 710 |
| Cross-AZ traffic | ALB → pods, pods → Redis/RDS (estimate) | 150 |
| KMS, S3, CloudFront, Route 53, SES, ECR, CloudWatch (minimal) | | 150 |
| AWS WAF | **on console/catalog only** (see cost levers) | 50 |
| **Production subtotal** | | **≈ 3,900** |

## Other environments and services

| Item | ~USD / month |
|---|---|
| SigNoz Cloud (India) — ~5% trace sampling, app logs (not customer call logs), infra metrics; $0.30/GB logs & traces, $0.10 per million metric samples | 400–700 |
| DR warm standby (Hyderabad): RDS cross-region replica, minimal EKS + nodes, ClickHouse backups | 500 |
| Staging environment (scaled down, switched off at night) | 600–800 |
| **Total (on-demand)** | **≈ 5,500–6,000** |

**≈ ₹4.7–5.2 lakh/month** at full launch-peak traffic (at ~₹85–88 per USD), before GST.

## How it changes with traffic
- **Fixed base** (clusters, databases, Redis, ClickHouse, NAT hourly, DR, staging, SigNoz minimum): ≈ **$2,500–3,000/month** even at low traffic. Early months after launch should sit near this.
- **Variable**: mostly network. At 5 KB/call, each **million calls ≈ 5 GB ≈ $0.85** in NAT + egress, plus a small amount of compute, Kinesis and ClickHouse. If the average payload is 20 KB instead of 5 KB, network cost roughly ×4 (~$4,700/month instead of ~$1,200).
- Use this for pricing: the platform margin per call must comfortably cover ~$1–1.5 per million calls in variable infrastructure cost at 5 KB payloads.

## Cost levers (in order of impact)
1. **Don't put AWS Secrets Manager on every provider credential.** At ~$0.40/secret/month, 2,000 APIs ≈ $800/month. Store provider credentials in PostgreSQL encrypted with KMS (envelope encryption) and keep Secrets Manager for platform secrets only.
2. **Don't put AWS WAF in front of the gateway's API traffic.** WAF bills per million requests (~$0.60/M), so 1.3B calls ≈ $780/month. Keep it on the console/catalog; protect the gateway with key auth, rate limits and AWS Shield Standard (free).
3. **Commit once traffic is steady:** 1-year Savings Plans / reserved instances on EC2, RDS and ElastiCache cut those lines ~25–35% (≈ $400–500/month saved).
4. **Spot instances** for consumers and batch jobs (never the gateway).
5. **NAT costs:** if provider traffic is heavy, consider gateway nodes in public subnets with tight security groups, or NAT instances, instead of managed NAT gateways.
6. **Turn staging off** outside working hours; scale DR to the minimum.
7. **Startup credits** (AWS Activate) can cover most of the first year — apply early.

## Not included
- AWS Support plan (Business support is a % of the bill — budget ~10% if wanted).
- Payment gateway fees (per transaction), transactional email beyond SES, domain/TLS, third-party tools (CI, error tracking).
- Payments to API providers (that's cost of goods, covered by pricing).
- Engineering salaries.
