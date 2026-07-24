# The Complete System Design & HLD Interview Guide
### From Absolute Beginner to Senior Engineer Interview-Ready

> This document is your single resource. Written as if a Staff Engineer mentored you for months. Every concept is introduced through a real problem — understood, not memorized.

---

## Table of Contents

1. [Part 1 — How Systems Evolve](#part-1)
2. [Part 2 — How the Internet Works](#part-2)
3. [Part 3 — Databases from First Principles](#part-3)
4. [Part 4 — Caching](#part-4)
5. [Part 5 — Application Scaling](#part-5)
6. [Part 6 — Distributed Systems](#part-6)
7. [Part 7 — Messaging Systems](#part-7)
8. [Part 8 — Data at Scale](#part-8)
9. [Part 9 — Core Building Blocks](#part-9)
10. [Part 10 — Capacity Estimation Masterclass](#part-10)
11. [Part 11 — The Universal HLD Interview Framework](#part-11)
12. [Part 12 — Complete Mock Interviews (15 Systems)](#part-12)
13. [Part 13 — Senior Engineer Thinking](#part-13)

---

<a name="part-1"></a>
# Part 1 — How Systems Evolve: From One Server to a Hundred Million Users

## The Beginning: You Have an Idea

Let's say you build a social photo-sharing app called "Snapgram." Users sign up, upload photos, follow others. You write Node.js code, connect it to MySQL, deploy it on a $5/month DigitalOcean server. A few friends sign up.

Every company you know started exactly this way. Instagram. Twitter. Airbnb.

Your architecture:
```
[ User's Browser ] ——HTTP——> [ Your Server (App + DB on same machine) ]
```

Simple. Clean. You can SSH in, look at logs, query the database directly. Life is good.

But you've already made tradeoffs — and as you grow, they will each surface in a predictable order, causing very specific failures.

---

## Stage 1: 1–100 Users — Enjoy the Simplicity

At 100 users, you're using maybe 2% of server capacity. Nothing breaks.

The lesson interviewers want to see: **start simple, scale when needed, know exactly what to scale and why.** Engineers who build complex infrastructure for 100 users waste months on zero benefit. Premature optimization is a real career mistake.

---

## Stage 2: 1,000 Users — The Database Feels It

A Product Hunt mention brings a thousand signups. Pages are a bit slower. You look at MySQL — it's doing full table scans on your feed query:

```sql
SELECT photos.*, users.username FROM photos
JOIN users ON photos.user_id = users.id
WHERE photos.user_id IN (
  SELECT following_id FROM follows WHERE follower_id = ?
)
ORDER BY photos.created_at DESC LIMIT 20;
```

MySQL reads every row in `follows` to find matches — an O(n) scan.

### Indexes: The Most Important Database Concept

A database index is a B-Tree data structure storing column values in sorted order with pointers back to rows. Without an index: read every row (O(n)). With an index: jump directly via O(log n) tree traversal.

```sql
CREATE INDEX idx_follows_follower ON follows(follower_id);
CREATE INDEX idx_photos_user_created ON photos(user_id, created_at DESC);
```

Feed query drops from 800ms to 15ms. No application code changed.

**Interview signal:** When you propose a table schema, also name the indexes needed for your access patterns. It shows you've thought about execution, not just structure.

Indexes have costs: every INSERT/UPDATE/DELETE must also update every index. More indexes = slower writes. Add them for measured query patterns, not speculatively.

---

## Stage 3: 10,000 Users — Single Server Becomes a Liability

80% CPU regularly. Occasional timeouts at peak hours. Worse: **zero redundancy** — the server dies, both your app and your database die simultaneously.

Two types of work compete for the same machine:
- **Application work:** CPU-intensive (running code, handling HTTP)
- **Database work:** CPU + disk I/O intensive (executing queries, reading/writing storage)

**Solution: Separate app and database onto two servers.**

```
[ App Server (Node.js) ] ——SQL——> [ DB Server (MySQL) ]
```

Each server is now optimized for one job. DB server gets lots of RAM so MySQL can buffer its working set in memory. App server gets more CPU for concurrent requests.

New tradeoff: **network latency** between app and DB. Before: queries over localhost ≈ 0ms. Now: 0.5–2ms per query. A request making 20 queries adds 10–40ms. This is why minimizing round trips matters.

---

## Stage 4: 100,000 Users — Database Becomes the Bottleneck

Symptoms:
- Queries that took 20ms now take 200–500ms
- MySQL CPU at 95%, disk I/O spiking
- `Too many connections` errors in logs
- Users see 3–5 second page loads

Root cause: **massive read/write imbalance.** Uploading a photo is 1 write. That photo being seen by 1,000 followers checking feeds twice a day = 2,000 reads. The ratio is often 100:1 or 1,000:1 reads to writes.

### Read Replicas — The First True Scaling Architecture

Keep one MySQL **primary** for all writes. It streams a **binary log (binlog)** of every change to one or more **replicas**. Replicas apply those changes to their own copies. Route read queries to replicas; route writes to the primary.

```
                [App Server]
               /             \
         (Reads)               (Writes)
        /                           \
[Read Replica 1]  [Read Replica 2]  [Primary MySQL]
```

Scale reads by adding more replicas.

**The first distributed systems problem: replication lag.**

After a user uploads a photo, the write hits the primary. The primary acknowledges and returns. A few milliseconds (sometimes seconds under load) later, replicas get the update.

If that user's next request reads from a replica, they may not see their own photo. They retry, creating a duplicate. This is **read-after-write inconsistency**.

Solutions:
1. After a write by user X, route X's reads to the primary for a short window
2. Track last-write timestamp per user; read from primary until replication catches up
3. Accept eventual consistency — for social feeds, a few seconds of delay is often fine

Which you choose depends on product requirements. This exact nuance is what interviewers probe in senior interviews.

---

## Stage 5: 1 Million Users — The Architecture Grows Up

Multiple new problems emerge together:

**Peak traffic:** Evenings hit 10x average. You need to handle peaks without crashing, without paying for 10x capacity 24/7. Solution: horizontal scaling + load balancer.

**Static asset latency:** Photos served from Mumbai to London = 200ms+ just from the network. Solution: CDN (content delivery network) — serve assets from servers geographically near the user.

**Cache:** 90% of reads are for 10% of the data. Repeatedly reading the same rows from MySQL is wasteful. Solution: Redis cache layer to serve repeated reads from memory.

```
[ Users ]
    |
    v
[ Load Balancer ]
   /     |     \
[App 1] [App 2] [App 3]
         |
    [ Redis Cache ]
         |
    [ MySQL Primary + Replicas ]
         |
    [ CDN (photos/videos) ]
```

---

## Stage 6: 10 Million Users — Breaking Apart the Monolith

One codebase handles user auth, photo uploads, feed generation, notifications, DMs, search, analytics. A bug anywhere can take down everything. Deploying any change requires deploying everything.

**Microservices:** Separate applications for separate domains. Each can be deployed, scaled, and failed independently.

```
User Service    → Users DB
Photo Service   → Photos DB
Feed Service    → Feed Cache
Notification    → Notifications DB
Search Service  → Elasticsearch
```

Microservices are not free: every service-to-service call is now a network call. Distributed transactions become very hard. Observability (logging, tracing) becomes much more complex. Operations burden multiplies.

Move to microservices when the pain of a monolith (deployment coupling, scaling independence) outweighs the pain of a distributed system. Not before.

---

## Stage 7: 100 Million Users — Industry-Scale

- 5,000–50,000 requests per second at peak
- Petabytes of data
- Global deployment, multiple continents
- 99.99% uptime = ~1 hour downtime per year
- Real-time delivery requirements
- Data residency compliance (GDPR, etc.)

At this scale: every design decision has outsized consequences. A 1% performance improvement saves millions annually. A single architectural mistake = multi-hour outage for millions of users.

---

## The Core Mindset

Systems don't start complex. They become complex. Every component at every big company — every Kafka cluster, every Redis node, every CDN — exists because a specific, painful problem made it necessary.

The wrong interview approach: immediately propose the "final architecture" with all the pieces.

The right approach: start with requirements, identify constraints, start simple, evolve as each bottleneck appears. Show *why* each piece exists. The interviewer wants to see your reasoning, not your memorized patterns.

---

## Summary

- Bottlenecks appear predictably: DB connections → CPU → disk I/O → network → geography
- Indexes turn O(n) table scans into O(log n) seeks — critical for any query optimization
- Read replicas scale reads but introduce replication lag and read-after-write consistency problems
- Monolith first; microservices when deployment coupling and scaling independence become genuinely painful
- Every piece of infrastructure exists to solve a specific problem — know what problem it solves

## Common Interview Questions

- "Walk me through how architecture evolves from 1,000 to 10 million users."
- "What are the tradeoffs between vertical and horizontal scaling?"
- "How do read replicas help, and what new problems do they introduce?"
- "When would you choose microservices over a monolith?"

## Common Mistakes

- Jumping to microservices for small systems — adds complexity for zero benefit
- Treating replicas as always having fresh data — forgetting replication lag
- Discussing only the happy path — not mentioning failure scenarios
- Premature optimization at low scale

## Real-World Examples

- Instagram was a surprisingly small codebase at large scale — disciplined caching and query optimization let them scale without over-engineering
- Twitter's "Fail Whale" was a real database bottleneck problem that lasted years during high-traffic events
- GitHub ran a well-scaled monolith for years before selectively extracting services

## Senior Engineer Perspective

"Given our current scale, what is the actual bottleneck? What is the cheapest fix that buys the most runway? What are we trading off?" Senior engineers resist adding infrastructure until there is a clear, measured need. Complexity is a liability.

## Revision Notes

- Single server → separate app + DB → read replicas → cache + load balancer → CDN → microservices
- B-tree index: O(log n) vs full scan O(n)
- Replication lag = reads may not reflect recent writes
- Reads outweigh writes 100:1 to 1000:1 in social apps
- Microservices: deployment independence + scale independence, at the cost of distributed complexity

---

<a name="part-2"></a>
# Part 2 — How the Internet Works: From Your Browser to the Server and Back

## Why This Matters for System Design

Every system design decision is constrained by how the internet works. When you say "put a CDN in front of images," you need to understand what a CDN actually does at the network level. When you say "use WebSockets for real-time messaging," you need to understand why HTTP can't do that. When you discuss latency, you need to understand where it physically comes from.

This section gives you the mental model of a complete request journey — from pressing Enter to pixels on screen.

---

## What Happens When You Type `google.com` and Press Enter

### Step 1: DNS — Translating a Name to an IP Address

Computers communicate using **IP addresses** like `142.250.195.46`. Before your browser can connect to Google, it must resolve `google.com` into an IP address. DNS (Domain Name System) is the internet's phone book — not one central server, but a **distributed hierarchical** system.

```
Browser → Local Cache → OS Cache → ISP Resolver (e.g. 8.8.8.8)
                                          |
                                    Root Name Server
                                    ("Who handles .com?")
                                          |
                                    .com TLD Server
                                    ("Who handles google.com?")
                                          |
                                    Google's Authoritative NS
                                          |
                                    "142.250.195.46, TTL: 300s"
```

Each level caches the result for the TTL (Time To Live) duration, so most lookups hit a cache and take < 5ms.

**System design implication — TTL matters for failover:** When you update a DNS record (new server IP after a migration), old caches continue pointing to the old server for the full TTL duration. A 1-hour TTL = up to 1 hour before all users see the new server. Production systems use **low TTLs (60–300 seconds)** on critical records so failovers propagate quickly. CDNs intercept traffic by updating DNS: you point `images.example.com` to the CDN's IP, and the CDN serves cached content from nearby edge servers.

---

### Step 2: TCP — Reliable Delivery Over an Unreliable Network

The internet routes data in **packets** (~1,500 bytes each). Different packets may take different physical routes. Some are dropped or corrupted. **TCP** (Transmission Control Protocol) makes this reliable through sequencing, acknowledgments, and retransmission.

Before any data flows, TCP performs a **3-way handshake**:

```
Client                            Server
  |                                  |
  |——— SYN (seq: 100) ————————————>  |  "I want to connect"
  |<—— SYN-ACK (seq: 200, ack: 101) —|  "Acknowledged"
  |——— ACK (ack: 201) ————————————>  |  "Connection ready"
  |                                  |
  |     [Data can now flow]          |
```

This costs **one full round trip** before a single byte of useful data moves. Mumbai ↔ Virginia ≈ 150ms round-trip. That's 150ms of pure latency before the browser even sends the HTTP request.

**System design implication:** This is why CDNs terminate TCP connections near users (5ms RTT instead of 150ms), why HTTP keep-alive reuses the same TCP connection across multiple requests, and why HTTP/2 multiplexes many requests over one connection — each avoids paying the handshake cost repeatedly.

---

### Step 3: TLS — Encrypting the Connection

After TCP, HTTPS adds a **TLS handshake**. TLS (Transport Layer Security) provides:
- **Confidentiality:** Intercepted packets are encrypted and unreadable
- **Integrity:** Tampering with data in transit is detectable
- **Authentication:** Certificate proves you're talking to the real server (signed by a trusted Certificate Authority)

TLS 1.3 (current standard) adds **1 round trip** after TCP. Older TLS 1.2 added 2. With "0-RTT resumption," reconnecting to a known server can skip the handshake entirely.

**System design implication:** TLS handshakes involve expensive asymmetric cryptography. High-traffic systems perform **TLS termination** at the load balancer or CDN edge — the cryptographic work happens there, and internal traffic uses faster unencrypted (or more cheaply encrypted) channels. This lets you scale TLS capacity independently from application capacity.

---

### Step 4: HTTP — The Language of Web Communication

With TCP + TLS established, the browser sends an HTTP request:

```
GET /feed HTTP/2
Host: api.snapgram.com
Authorization: Bearer eyJhbGciOiJIUzI1Ni...
Accept: application/json
```

The server responds:

```
HTTP/2 200 OK
Content-Type: application/json
Cache-Control: private, max-age=30

{"posts": [...]}
```

### HTTP Methods and REST

REST (Representational State Transfer) uses HTTP methods semantically:

| Method | Meaning | Idempotent? | Example |
|--------|---------|-------------|---------|
| GET | Retrieve resource | Yes | `GET /users/42` |
| POST | Create resource | No | `POST /photos` |
| PUT | Replace resource entirely | Yes | `PUT /users/42` |
| PATCH | Partial update | No | `PATCH /users/42` |
| DELETE | Remove resource | Yes | `DELETE /photos/99` |

**Idempotent** means calling it N times has the same effect as calling it once. `DELETE /photos/99` twice: first call deletes it, second call finds nothing. Same final state. POST is not idempotent — two POSTs create two resources.

### Status Codes You Must Know Cold

| Code | Meaning | When it appears |
|------|---------|----------------|
| 200 | OK | Successful GET/PUT/PATCH |
| 201 | Created | Successful POST |
| 204 | No Content | Successful DELETE |
| 400 | Bad Request | Client sent invalid data |
| 401 | Unauthorized | Missing or invalid authentication |
| 403 | Forbidden | Authenticated but lacks permission |
| 404 | Not Found | Resource doesn't exist |
| 409 | Conflict | Duplicate, or version conflict |
| 429 | Too Many Requests | Rate limit exceeded |
| 500 | Internal Server Error | Bug in your server |
| 502 | Bad Gateway | Upstream service returned garbage |
| 503 | Service Unavailable | Overloaded or in maintenance |
| 504 | Gateway Timeout | Upstream service timed out |

**429, 502, 503, 504** appear in almost every system design discussion. Know them cold.

---

### The Complete Production Request Flow

```
User: api.snapgram.com/feed
       |
       v DNS lookup → CDN IP (104.22.XX.XX)
       |
       v TCP + TLS to nearby CDN edge (Mumbai PoP)
  ┌──────────────────────────────────────────┐
  │  CDN Edge Server                          │
  │  Static asset? YES → serve from cache     │
  │               NO  → forward to origin     │
  └─────────────────────┬────────────────────┘
                        |
                        v
  ┌──────────────────────────────────────────┐
  │  Load Balancer                            │
  │  Health check → route to healthy App Svr  │
  └─────────────────────┬────────────────────┘
                        |
                        v
  ┌──────────────────────────────────────────┐
  │  API Gateway                              │
  │  - Validate JWT                           │
  │  - Rate limit check                       │
  │  - Route to Feed Service                  │
  └─────────────────────┬────────────────────┘
                        |
                        v
  ┌──────────────────────────────────────────┐
  │  Feed Service                             │
  │  Check Redis → HIT: return (~1ms)         │
  │              → MISS: query MySQL,         │
  │                cache result, return       │
  └──────────────────────────────────────────┘
```

Every box here is a system we'll understand deeply in this guide.

---

## WebSockets: When HTTP Isn't Enough

HTTP is **request-response**: client asks, server answers. The server cannot push data unless asked. This breaks for:

- **Chat apps** — messages must arrive without the recipient polling
- **Collaborative editing** — others' cursor movements appear instantly
- **Live dashboards** — metrics update in real time
- **Multiplayer games** — positions update 60 times per second

**WebSockets** fix this by upgrading an HTTP connection to a persistent, full-duplex channel:

```
Browser                              Server
   |—— HTTP Upgrade: websocket ——————>|
   |<—— 101 Switching Protocols ——————|
   |                                  |
   |—— "Register for messages" ————>  |
   |<—— "New message from Alice" ——————|  ← server pushes unsolicited
   |<—— "Bob is typing..." ————————————|
```

**Critical system design consequence:** WebSocket connections are **stateful** — a client is permanently connected to one specific server. With 10 app servers:
- Client A is connected to Server 1
- Client B is connected to Server 3
- A sends B a message → arrives at Server 1
- B's connection is on Server 3 — Server 1 can't reach it directly

Solution: a **shared message broker** (Redis Pub/Sub or Kafka) between servers. Server 1 publishes the message; Server 3 is subscribed and delivers it to Client B. We'll cover this in detail in Part 7.

**Long Polling** (older approach): client sends a request, server holds it open until data is available or timeout expires. Simulates push but wastes connection resources and adds latency.

**Server-Sent Events (SSE)**: one-way server → browser push over HTTP. Simpler than WebSockets for notification streams or activity feeds.

---

## REST vs. GraphQL vs. gRPC

**REST** — HTTP-based, resource-oriented, cacheable, widely understood. Best for public-facing APIs.

**GraphQL** — Client declares exactly what data it needs in one request. Eliminates over-fetching and under-fetching. Ideal for products with many different clients (web, iOS, Android) that need different data shapes from the same underlying data. Tradeoffs: per-query nature makes HTTP caching hard; N+1 query problems are common without a dataloader; adds server complexity.

**gRPC** — Binary Protocol Buffers encoding (smaller + faster than JSON), strongly typed, auto-generated client/server code, bidirectional streaming. Best for **internal service-to-service communication**. Browsers don't natively support gRPC, so it's not suitable for public APIs. Rule of thumb: **REST/GraphQL externally, gRPC internally**.

---

## Summary

- DNS resolves names to IPs through a distributed hierarchy; TTL controls cache duration and failover speed
- TCP handshake = one round trip before data flows; CDNs and keep-alive reduce this cost
- TLS adds 1 round trip; terminated at load balancers/CDN for scalability
- HTTP is request-response; REST uses HTTP methods semantically; status codes matter
- WebSockets = persistent, bidirectional, but stateful — requires a shared message broker across servers
- REST (external/public), gRPC (internal/services), GraphQL (flexible multi-client querying)

## Common Interview Questions

- "Explain every step when you type a URL and press Enter."
- "Why use WebSockets for chat instead of HTTP? What new problems does it introduce?"
- "What is the difference between REST and gRPC? When would you choose each?"
- "Why does DNS TTL matter for system design?"

## Common Mistakes

- Saying "HTTPS is secure" without explaining TLS encryption and certificate authentication
- Not recognizing that WebSockets are stateful — failing to discuss the message broker requirement
- Proposing gRPC for a public API — browsers don't support it
- Not knowing 429/502/503/504 — these codes appear in nearly every design discussion

## Senior Engineer Perspective

Senior engineers think in **latency budgets**. Users expect a page in under 2 seconds. DNS (~10ms) + TCP (~100ms) + TLS (~100ms) + processing (~200ms) + transfer (~50ms) ≈ 460ms for infrastructure alone. The application must fit in the remaining ~1.5s. CDNs reduce DNS + TCP latency. HTTP/2 reduces handshake overhead. Caching eliminates processing time. Every architectural decision either saves from this budget or spends it.

## Revision Notes

- Order always: DNS → TCP → TLS → HTTP
- Low TTL = faster failover; high TTL = better cache hit rate
- WebSocket: stateful, bidirectional, requires message broker between servers
- REST (external, cacheable), gRPC (internal, binary, fast), GraphQL (flexible)
- RTT between client and nearest server is the primary driver of perceived latency

---

<a name="part-3"></a>
# Part 3 — Databases from First Principles

## Starting Simple: We Need to Store Users

You're building Snapgram. Users sign up. They need to log in later, so you must remember them. Where do you store that?

**First instinct: a file.**

```json
[
  {"id": 1, "email": "alice@example.com", "password_hash": "abc123"},
  {"id": 2, "email": "bob@example.com", "password_hash": "def456"}
]
```

Works for 2 users. Falls apart fast:
- **O(n) reads:** Find Bob by email? Read the entire file from start to finish.
- **Race conditions on writes:** Two users sign up simultaneously — both read the file, both append, second write overwrites first. One user disappears.
- **No durability:** Server crashes mid-write → corrupted file → all data lost.
- **No querying:** "Find all users who signed up last week" requires reading every record.

These problems apply to any naive storage mechanism. Engineers recognized them decades ago and built databases to solve them.

---

## Why Relational Databases Were Invented

In the 1970s, IBM researcher Edgar Codd proposed the **relational model**: organize data into tables with rows and columns, enforce relationships and constraints, and use a declarative query language (SQL). You describe *what* data you want; the database figures out *how* to retrieve it efficiently.

### ACID — The Four Guarantees That Make Databases Trustworthy

**A — Atomicity:** A transaction either fully completes or fully rolls back. Transferring $100 from Account A to Account B: if the server crashes between debiting A and crediting B, the debit is rolled back. Money doesn't disappear into the void.

**C — Consistency:** The database enforces constraints always and everywhere. If emails must be unique, the database rejects duplicates even under concurrent load — guaranteed by the database engine, not by application code.

**I — Isolation:** Concurrent transactions don't interfere with each other. Two users signing up simultaneously each see the database as if they're the only one using it. Neither sees the other's partial write.

**D — Durability:** Once a transaction is committed, it's permanent — even if the power dies a millisecond after the commit. Accomplished through **Write-Ahead Logging (WAL)**: every change is written to a sequential log on disk *before* being applied to data files. On crash recovery, the database replays the log to restore state.

These guarantees are what you're paying for with a relational database. They're hard to implement correctly — don't take them for granted.

---

## How Indexes Actually Work: B-Trees

An index is a separate **B-Tree** (Balanced Tree) data structure that stores a column's values in sorted order with pointers back to the actual rows.

**Without an index on `email`:**
```
SELECT * FROM users WHERE email = 'alice@example.com';
→ MySQL reads every row in the table, checks if email matches
→ With 1M users: 1,000,000 reads (full table scan, O(n))
```

**With an index on `email`:**
```
→ MySQL traverses the B-tree: ~20 comparisons for 1M values (O(log n))
→ Jumps directly to Alice's row via the stored pointer
→ Returns result in ~1ms instead of ~500ms
```

**Composite indexes** (multiple columns) are even more powerful:
```sql
CREATE INDEX idx_photos_user_time ON photos(user_id, created_at DESC);
```
This index is sorted first by `user_id`, then by `created_at` within each user. The query "get all photos by user 42, newest first" can jump to user 42's section and walk it in order — no separate sort step, no reading other users' data.

**When indexes hurt:**
- Every INSERT/UPDATE/DELETE must also update all indexes on that table → more indexes = slower writes
- Indexes consume disk space and memory (can exceed the size of the data itself on heavily indexed tables)
- Add indexes for measured query patterns, not speculatively

**Covering indexes:** When all columns needed by a query are in the index itself, MySQL can answer the query entirely from the index without reading the actual row. This is called an "index-only scan" and is extremely fast.

---

## SQL Joins: Querying Related Data in One Shot

Relational databases let you query across related tables in a single statement:

```sql
SELECT p.id, p.image_url, u.username, COUNT(l.id) AS like_count
FROM photos p
JOIN users u ON p.user_id = u.id
LEFT JOIN likes l ON l.photo_id = p.id
WHERE p.user_id IN (
    SELECT following_id FROM follows WHERE follower_id = 42
)
GROUP BY p.id, u.username, p.created_at
ORDER BY p.created_at DESC
LIMIT 20;
```

This single query joins four tables, filters by followed users, counts likes, sorts, and paginates. The database's **query optimizer** figures out the most efficient execution plan (which table to start from, which indexes to use, how to execute the join). You declare *what* you want; it figures out *how*.

This declarative power is one of SQL's greatest strengths. And it's why losing it (when you move to NoSQL or sharding) is a real tradeoff, not a free upgrade.

---

## The Limits of Relational Databases

### Vertical Scaling Wall

A single MySQL server handles tens of thousands of queries per second with good indexing. But you can't scale a single server arbitrarily — the largest cloud instances have limits. And all your write traffic must go through one primary. Beyond a certain point you need to distribute data across multiple servers, which SQL databases weren't originally designed for.

### Schema Rigidity

Relational databases require a fixed schema defined upfront. Adding a column to a 500-million-row table requires `ALTER TABLE` — which can lock the table for minutes or hours. Changing data types is worse. For fast-moving products, this creates friction.

### The Join Problem at Scale

JOINs are powerful — but they require data to live on the same server (or at least in the same database). Once you shard data across multiple database servers (split user 1-10M on server A, user 10M-20M on server B), cross-shard JOINs become extremely expensive or impossible. This forces you to denormalize data and handle relationships in application code.

---

## NoSQL: A Family of Alternatives

"NoSQL" doesn't mean one thing. It's a family of database types, each designed for a specific problem that relational databases handle poorly.

### Document Databases: MongoDB

**The problem it solves:** Flexible, hierarchical data without a fixed schema.

Imagine a product catalog. Electronics have voltage specs, battery life, wireless standards. Clothing has sizes, colors, materials. Books have ISBNs, authors, page counts. In MySQL, you'd need separate tables per category, or a generic key-value attributes table (clunky to query), or 100+ nullable columns. None are elegant.

MongoDB stores data as **documents** — flexible JSON-like objects:

```json
{
  "_id": "64f1a2b3c4d5e6f7",
  "name": "Sony WH-1000XM5",
  "category": "electronics",
  "price": 349.99,
  "specs": {
    "battery_hours": 30,
    "noise_cancellation": true,
    "weight_grams": 250
  },
  "colors": ["black", "silver"]
}
```

Same collection, different shapes — a book document can have `isbn` and `pages` while a clothing document has `sizes` and `material`. No schema change required. This is **schema-on-read**: structure is interpreted when reading, not enforced on writing.

**Strengths:**
- Flexible schema — add new fields without migrations
- Documents map naturally to application objects (no ORM impedance mismatch)
- Excellent for document-level reads: load a complete user profile in one query
- Built-in horizontal scaling via sharding

**Weaknesses:**
- No server-side JOINs — cross-document relationships must be handled in application code
- No referential integrity — nothing stops you from referencing a user that doesn't exist
- Transactions across documents exist in modern MongoDB but are weaker than PostgreSQL's
- Schema-on-read means data quality is entirely the application's responsibility

**When to use:** Content management, product catalogs, user profiles with variable optional fields, rapid prototyping.

**When not to use:** Financial transactions, highly relational data, when data integrity must be enforced at the database level.

---

### Wide-Column Databases: Cassandra

**The problem it solves:** Massive write throughput, time-series data, near-linear horizontal scalability.

Imagine an IoT platform: 10,000 sensors each writing a temperature reading every second = 10,000 writes per second, continuously, forever. After a year: hundreds of billions of rows. You need to answer: "Give me all readings from sensor 5421 between 2:00 PM and 3:00 PM on July 15th."

MySQL struggles: at this scale even indexed tables slow down, and all writes still go through one primary.

Cassandra was designed for exactly this. Its key architectural properties:

**Masterless (peer-to-peer):** Every node can accept writes for any data. No single primary to be a write bottleneck. Add nodes to scale writes linearly.

**Write-optimized storage:** Writes go to an in-memory structure (memtable) + a sequential append-only log. Sequential disk writes are orders of magnitude faster than random writes. Data is compacted to disk in the background.

**Data model — partition key + clustering key:**
```sql
CREATE TABLE sensor_readings (
    sensor_id    UUID,
    reading_time TIMESTAMP,
    temperature  FLOAT,
    humidity     FLOAT,
    PRIMARY KEY (sensor_id, reading_time)
) WITH CLUSTERING ORDER BY (reading_time DESC);
```

`sensor_id` is the **partition key**: all readings for a sensor land on the same set of nodes (hashed to a location in the cluster). `reading_time` is the **clustering key**: within each sensor's partition, readings are stored sorted by time on disk. The query "get readings for sensor 5421 between 2PM–3PM" jumps directly to that sensor's partition and walks the time-sorted rows — extremely efficient.

**Tunable consistency:** Cassandra lets you configure how many replica nodes must acknowledge a read or write before it's considered successful:
- `CONSISTENCY ONE` — one node confirms → fast, may read stale data
- `CONSISTENCY QUORUM` — majority of replicas confirm → balanced
- `CONSISTENCY ALL` — all replicas confirm → strong consistency, but any failure = unavailable

This is **tunable consistency** — you trade between consistency and availability per query.

**CAP tradeoff:** Cassandra favors **Availability + Partition Tolerance** over **Consistency**. If some nodes are unreachable, Cassandra continues accepting writes (on available nodes) rather than refusing them. Replicas catch up when nodes reconnect (eventual consistency). We'll cover CAP theorem deeply in Part 8.

**When to use:** IoT/time-series data, event logging, analytics events, write-heavy workloads at high volume, global multi-region with active-active writes.

**When not to use:** Complex queries with multiple filter conditions (queries must align with data model), ad-hoc querying (access patterns must be known upfront), strong ACID transactions.

---

### Key-Value / Document at Scale: Amazon DynamoDB

DynamoDB is Amazon's fully managed database. The value proposition: **single-digit millisecond performance at any scale, zero operational overhead**.

It's a key-value and document store. Every item has a **partition key** (determines which partition holds it) and an optional **sort key** (enables range queries within a partition):

```
Table: UserSessions
Partition Key: user_id
Sort Key: session_start

user_id=42, session_start=2025-07-15T14:00:00 → {device: "iPhone", ip: "..."}
user_id=42, session_start=2025-07-16T09:00:00 → {device: "MacBook", ip: "..."}
user_id=99, session_start=2025-07-15T11:00:00 → {device: "Android", ip: "..."}
```

**Strengths:** Fully managed (no servers, no replication config), guaranteed sub-10ms reads, auto-scaling, high availability across 3 AZs.

**Limitations:** Query flexibility is limited to partition key (+ sort key). Secondary indexes (GSI/LSI) add flexibility but cost money. Joins don't exist — relationships handled in application code.

**When to use:** Session storage, shopping carts, gaming leaderboards, user preferences, any workload with predictable, key-based access patterns.

---

### Search Databases: Elasticsearch

Sometimes you need full-text search: "find all photos with captions containing 'sunset beach'," or "find users whose names start with 'Var'." Relational databases can do this with `LIKE '%sunset beach%'`, but it's slow (no index can help with leading wildcards) and doesn't support relevance ranking.

**Elasticsearch** is built specifically for full-text search. It uses **inverted indexes**: instead of storing documents and their content, it stores words and which documents contain them. "sunset" → [doc_42, doc_87, doc_1204]. When you search for "sunset beach," Elasticsearch finds documents containing either word and ranks by relevance.

Elasticsearch is not a primary database — it's a search layer. You typically write to your primary database (MySQL/MongoDB), then asynchronously index into Elasticsearch for search queries.

---

## Choosing the Right Database

Don't pick a database based on trend. Walk through these questions:

**1. What are my primary access patterns?** How will data be read most of the time? A key-value lookup? A range scan? A full-text search? A complex JOIN?

**2. What consistency do I actually need?** Financial transactions → strong ACID. Social feed → eventual consistency is fine.

**3. What is the read/write ratio and volume?** Read-heavy, moderate volume → MySQL with replicas is excellent. Write-heavy, massive volume → Cassandra or DynamoDB.

**4. Is the schema fixed or evolving?** Fixed, relational → PostgreSQL. Flexible, hierarchical → MongoDB.

**5. What does the team know how to operate?** A small team might prefer managed services (RDS, DynamoDB) over self-managing a Cassandra cluster. Operational burden is a real cost.

**A good interview answer sounds like:**
> "For user profiles and follow relationships I'd use PostgreSQL — the data is relational, consistency matters, and the read/write volume at our initial scale is well within what a primary + replicas can handle. For the event stream (likes, views, shares) I'd use Cassandra — it's write-heavy, append-only, and we need to query by user ID in time order. That pattern fits Cassandra's data model perfectly. For search I'd add Elasticsearch as a secondary index, fed asynchronously from the primary databases."

---

## Summary

- Files fail: O(n) reads, race conditions on writes, no durability, no querying
- Relational databases provide ACID guarantees, B-tree indexes (O(log n) lookups), and powerful SQL JOINs
- Schema rigidity and vertical scale limits motivate NoSQL
- **MongoDB:** flexible schema, document model, no JOINs, weaker cross-document transactions
- **Cassandra:** massive write throughput, time-series/event data, masterless, tunable consistency (AP in CAP)
- **DynamoDB:** fully managed key-value/document, sub-10ms, partition key + sort key model
- **Elasticsearch:** full-text search via inverted indexes; used as a secondary search layer, not a primary store
- Choose based on access patterns, consistency needs, scale, and operational capability

## Common Interview Questions

- "What is ACID? Why does it matter for a payment system?"
- "When would you choose MongoDB over MySQL? What do you give up?"
- "How does Cassandra achieve high write throughput?"
- "What is a B-tree index and how does it improve query performance?"
- "How would you design a database schema for a ride-sharing app?"

## Common Mistakes

- Choosing NoSQL because it "scales better" — it makes different tradeoffs, not uniformly better tradeoffs
- Not thinking about indexes when designing schemas — every query needs a supported index
- Not discussing access patterns before choosing a database
- Treating eventual consistency as equivalent to strong consistency

## Real-World Examples

- Facebook uses MySQL at massive scale — they've contributed heavily to MySQL's scalability through projects like MyRocks (RocksDB storage engine) and Vitess (MySQL sharding proxy)
- Netflix uses Cassandra as their primary database for viewing history and user preferences — write-heavy, append-only, global scale
- Instagram uses PostgreSQL for user data and relationships, with extensive caching on top
- Amazon DynamoDB was built because Amazon's retail platform needed key-value lookups at massive scale with guaranteed performance SLAs

## Senior Engineer Perspective

Senior engineers don't choose databases based on what's popular. They start with: "What queries will we run 99% of the time? What consistency does the business actually require — would a user notice a 1-second read lag? What happens to this database at 10x current scale?" They also think about **operations**: managing a Cassandra cluster requires deep expertise. A small team might choose RDS (managed PostgreSQL) even if Cassandra would be theoretically more performant, because the operational burden of Cassandra is a real engineering cost that delays feature development.

## Revision Notes

- ACID: Atomicity (all-or-nothing), Consistency (constraints always enforced), Isolation (transactions don't interfere), Durability (committed = permanent, via WAL)
- B-tree index: O(log n) lookup, O(n) without — critical for production query performance
- MySQL/PostgreSQL: ACID, relational, strong consistency, vertical scale limits, JOINs
- MongoDB: flexible schema, document model, schema-on-read, no referential integrity
- Cassandra: masterless, write-optimized (memtable + sequential log), partition key + clustering key, tunable consistency, CAP=AP
- DynamoDB: managed, sub-10ms, partition + sort key, no JOINs
- Elasticsearch: inverted index for full-text search, secondary layer not primary store

---

<a name="part-4"></a>
# Part 4 — Caching: Making Systems Fast Without Breaking Your Database

## The Problem: Your Database Can't Keep Up

It's 8 PM on a weekday — peak usage. 50,000 users scroll their feeds. Every feed load triggers a complex SQL JOIN across four tables. Your database CPU is at 95%. Queries that took 20ms now take 800ms. Users abandon the app.

You look at your access logs and notice something: @celebrity_user has 2 million followers. Every time she posts, all 2 million followers' feeds include her photo. Your database retrieves the same photo record from MySQL **2 million times**. The data doesn't change between those reads — the photo exists, it's the same photo. You're doing millions of identical reads of static data. Enormously wasteful.

The insight: **once you've read a row from MySQL, why throw it away? Store it somewhere fast and serve the next million reads from there.**

This is caching.

---

## Why Memory Is So Much Faster Than Disk

The physics of storage explain why caching is transformative:

| Storage | Latency |
|---------|---------|
| CPU L1 Cache | ~0.5 nanoseconds |
| RAM | ~100 nanoseconds |
| NVMe SSD | ~100 microseconds |
| Network (same datacenter) | ~500 microseconds |
| SATA SSD | ~1 millisecond |

RAM is **1,000× faster than an SSD**. A cache is a purpose-built in-memory store that holds your working data for extremely fast retrieval — eliminating the disk I/O and query parsing overhead of a full database read.

---

## Introducing Redis

Redis (Remote Dictionary Server) is an **in-memory data structure store**. Unlike a database that stores data on disk and reads it into memory as needed, Redis keeps all data in RAM. This makes it extraordinarily fast: **100,000–1,000,000 operations per second** on a single server.

Think of Redis as a massive, fast dictionary accessible over the network. But it's far more than a simple key-value store — it supports rich data structures:

| Structure | Use Cases |
|-----------|-----------|
| **Strings** | Session tokens, counters, simple key-value |
| **Hashes** | User profile cache, field-level updates |
| **Lists** | Activity feeds, task queues (LPUSH/RPOP) |
| **Sets** | "Who liked this photo?" (unique members) |
| **Sorted Sets** | Leaderboards (score-ranked), rate limiting (timestamps) |
| **Pub/Sub** | Real-time notifications, WebSocket message fanout |

---

## The Cache-Aside Pattern (Most Common)

```
User requests feed for user_42
    |
    v
App checks Redis: GET "feed:42"
    |
    +---> CACHE HIT: Return immediately (~1ms). Done.
    |
    +---> CACHE MISS:
              Query MySQL → 200ms
              Store in Redis: SET "feed:42" <data> EX 60  (TTL=60s)
              Return to user → 210ms
              Next 60 seconds: all requests for feed:42 → Redis (~1ms)
```

```python
def get_user_feed(user_id):
    cache_key = f"feed:{user_id}"
    
    cached = redis.get(cache_key)
    if cached:
        return json.loads(cached)          # Cache hit — ~1ms
    
    feed = db.query_feed(user_id)          # Cache miss — ~200ms
    redis.setex(cache_key, 60, json.dumps(feed))  # Store with 60s TTL
    return feed
```

The first request after a miss pays full database cost. For the next 60 seconds, all requests for that feed serve from Redis. If @celebrity_user's feed is requested 10,000 times per minute, you go from 10,000 MySQL queries/minute to ~1.

**Why "cache-aside":** The cache sits "off to the side." The application explicitly decides what to cache, when, and with what TTL. The database remains the source of truth — if Redis fails, the app falls back to MySQL.

---

## Write Strategies

Cache-aside handles reads. When data changes, how do you handle the cache?

### Write-Around (Most Common)
On write, update the database and **invalidate (delete)** the cache entry. The next read re-populates from the database.

```python
def update_username(user_id, new_username):
    db.execute("UPDATE users SET username=? WHERE id=?", new_username, user_id)
    redis.delete(f"user:{user_id}")   # Invalidate — next read re-fetches
```

**Pro:** Simple, always correct — stale data is never served after an update.
**Con:** The first read after any write pays full DB cost (cold miss).

### Write-Through
Write to database and cache simultaneously.

```python
def update_username(user_id, new_username):
    db.execute("UPDATE users SET username=? WHERE id=?", new_username, user_id)
    redis.hset(f"user:{user_id}", "username", new_username)   # Keep cache warm
```

**Pro:** Cache is always fresh. No cold-miss after writes.
**Con:** Every write pays both DB and cache cost. Writes that are never read still populate the cache (wasted memory).

### Write-Back (Write-Behind)
Write to cache immediately; flush to database asynchronously in the background.

```python
def add_like(user_id, photo_id):
    redis.incr(f"likes:{photo_id}")           # Instant response (~1ms)
    background_queue.push({"like": photo_id}) # DB write happens later
```

**Pro:** Write response is near-instant — great for write-heavy counters.
**Con:** **Data loss risk** — if Redis crashes before the background flush, those writes are gone. Use only for loss-tolerant workloads (analytics counters, approximate view counts).

**Rule of thumb:** Cache-aside + write-around for most web applications. Write-through when strong consistency after writes is needed. Write-back only for truly write-heavy, loss-tolerant data.

---

## Cache Invalidation: The Hardest Problem in Computer Science

Phil Karlton famously said: "There are only two hard things in Computer Science: cache invalidation and naming things."

**The problem:** Your cache holds a copy of data. The source of truth (the database) changes. Now your cache has stale data.

Scenario: User's profile is cached with `username: "alice_photos"`. She changes it to `"alice_designs"` in MySQL. If you don't invalidate the cache, everyone who reads her profile sees the old name until the TTL expires.

**Strategies:**

**1. TTL-based expiration:** Every cache entry has a TTL. After it expires, the next read re-fetches from DB. Simple — but accepts that data can be stale for up to TTL duration. Acceptable for feeds, product listings, counts.

**2. Event-based invalidation:** Explicitly delete the cache key on any write that changes that data. Zero staleness — but you must invalidate *everywhere* data changes: in the main API, in background jobs, in admin tools. Missing one invalidation path causes silent stale data bugs that are very hard to track down.

**3. Versioned keys:** Include a version number in cache keys: `user:42:v7`. When you update user 42, increment to v8. Old keys naturally stop being used and eventually evict. Avoids race conditions but requires tracking versions.

**The real danger of cache invalidation:** It's not technically hard — it's operationally hard. A year after you write the invalidation logic, a new engineer adds an admin script to bulk-update usernames and forgets to invalidate the cache. Suddenly thousands of users see wrong usernames for an hour. These bugs are subtle and often only appear in production.

---

## Cache Eviction Policies

Redis stores data in RAM. RAM is finite. When Redis runs out of memory, it evicts keys according to a configured policy:

| Policy | Behavior | Best For |
|--------|----------|----------|
| `allkeys-lru` | Evict least recently used | General web caches |
| `allkeys-lfu` | Evict least frequently used | "Keep popular items" |
| `volatile-ttl` | Evict key with shortest remaining TTL | TTL-based caches |
| `volatile-lru` | LRU but only keys with TTL set | Mixed permanent + cached data |
| `noeviction` | Return error when full | Redis as primary store |

For most web application caches: **`allkeys-lru`** is the right default. Frequently accessed data stays in RAM; rarely accessed data naturally ages out.

---

## Advanced Cache Problems

### Cache Stampede (Thundering Herd)

Scenario: @celebrity_user's feed is cached with a 60-second TTL. At 8:00:00 PM exactly, the TTL expires for 100,000 concurrent users' caches. All 100,000 users' next requests simultaneously miss the cache and fire database queries. Your database goes from near-zero load (100% cache hits) to 100,000 simultaneous queries in one second. It crashes.

**Solutions:**

1. **TTL Jitter:** `TTL = 60 + random(0, 10)` seconds. Staggers expirations so they don't all expire at the same moment.

2. **Probabilistic early refresh (PER):** Before the TTL expires, begin recomputing the value with increasing probability as expiry approaches. One request recomputes while others still serve the old value.

3. **Mutex/Distributed Lock:** On cache miss, acquire a Redis lock before querying the DB. First process to acquire the lock computes and repopulates the cache; others wait and then read the freshly cached value.

### Cache Penetration

Scenario: An attacker (or buggy client) repeatedly requests `GET /users/999999999` — a user ID that doesn't exist. Every request: cache miss (nothing to cache) → database query (returns empty) → no caching → repeat. The database gets hammered with useless queries for non-existent data.

**Solutions:**

1. **Cache negative results:** If the database returns empty, cache that: `redis.setex("user:999999999", 30, "NOT_FOUND")`. Next 30 seconds of requests for that ID get "not found" from Redis without touching MySQL.

2. **Bloom Filter:** A Bloom filter is a space-efficient probabilistic data structure. It can tell you with certainty that a key *definitely does not exist*, or that it *probably exists* (with a tunable false-positive rate). Before hitting the cache or database, check the Bloom filter. If it says "definitely doesn't exist," return 404 immediately. Used at scale by systems like HBase and Cassandra.

### Cache Avalanche

Scenario: Your Redis cluster crashes entirely. Every request that was being served from cache now hits the database — a sudden 50x–100x load spike your database was never sized for.

**Solutions:**

1. **Redis high availability:** Run Redis with Sentinel (automatic failover) or Redis Cluster. A single node failure doesn't take down the entire cache.

2. **Circuit breaker:** Application detects cache failure and throttles database requests rather than passing all traffic through.

3. **Never assume 100% cache hit rate in DB capacity planning.** Your database must survive a cold-cache scenario — size it to handle a significant fraction of total traffic.

---

## Redis Architecture Essentials

### Redis Persistence (How It Survives Restarts)

Redis is in-memory but supports persistence:

**RDB (Redis Database) — Snapshots:** At configurable intervals, Redis forks and writes the entire in-memory dataset to a `.rdb` file on disk. Fast to restore (load the file, done). Data loss risk: up to the interval since the last snapshot (minutes).

**AOF (Append-Only File):** Every write command is appended to a log file. On restart, Redis replays the log. More durable — data loss is at most the last second of operations (configurable). Slower to restore than RDB (must replay every write command).

**No persistence:** Redis is a pure cache — data is intentionally ephemeral. Fastest, but cache starts cold after restart.

For a cache, **RDB or no persistence** is typical. For Redis used as a primary store (e.g., session storage), **AOF** is appropriate.

### Redis Cluster

A single Redis server tops out around 100GB RAM and ~1M ops/second. For larger scales, **Redis Cluster** shards data across multiple Redis nodes using consistent hashing — each node owns a portion of the keyspace. Keys are automatically routed to the correct node. Cluster provides both horizontal scaling and high availability (each shard has replicas).

---

## When to Use Redis vs. In-Process Cache

**In-process cache (e.g., a HashMap in your app server's memory):**
- Fastest possible (no network hop)
- Dies when the process restarts
- Inconsistent across multiple app servers — Server 1 and Server 2 have separate caches; an invalidation on one doesn't affect the other

**Redis (shared remote cache):**
- Tiny network overhead (~0.5ms)
- Survives app restarts
- **Consistent across all app servers** — one Redis instance serves all of them; an invalidation immediately affects all servers

The moment you have more than one app server, you need a shared cache (Redis). In-process caching becomes a liability — different servers serve different data, leading to inconsistencies users notice.

---

## Summary

- Caching stores frequently-read data in memory to avoid repeated, expensive database reads
- Redis supports strings, hashes, lists, sets, sorted sets, pub/sub — not just simple key-value
- **Cache-aside** (most common): check cache first; on miss, fetch DB, store in cache
- **Write-around**: invalidate cache on writes; next read repopulates
- **Write-through**: write DB and cache together; always fresh, but wastes cache space for write-only data
- **Write-back**: write cache first, flush DB async; fast but risk of data loss
- Cache invalidation is operationally hard — missing one invalidation path causes subtle stale data bugs
- TTL jitter prevents cache stampedes; bloom filters prevent cache penetration; Redis HA prevents cache avalanche
- Redis Cluster shards across nodes for horizontal scale and HA

## Common Interview Questions

- "What is cache-aside and how does it work?"
- "What are the tradeoffs between write-through and write-back caching?"
- "What is a cache stampede and how do you prevent it?"
- "What happens to your system if Redis goes down?"
- "How would you cache a user's social feed? What are the invalidation challenges?"

## Common Mistakes

- Saying "add Redis" without explaining which pattern (cache-aside? write-through?) and what TTL
- Not discussing cache invalidation — it's harder than populating the cache
- Forgetting that in-process caches become inconsistent with multiple app servers
- Treating Redis as a reliable persistent store without discussing persistence configuration and its tradeoffs

## Real-World Examples

- Twitter uses a massive Redis cluster to cache timelines — the "home timeline" for each user is precomputed and stored in Redis, so feed loads are essentially a Redis read
- Facebook uses Memcached (a simpler in-memory cache) at extraordinary scale — they've written papers about operating it at tens of millions of requests/second
- Instagram's PostgreSQL database handles a fraction of its theoretical load because the vast majority of reads hit their caching layer first

## Senior Engineer Perspective

The most important insight about caching: **caches don't eliminate consistency problems — they defer them**. When you add a cache, you now have two copies of every piece of data (DB and cache). Keeping them synchronized is a distributed systems problem. Senior engineers think about: "What is the maximum acceptable staleness for this data? What are the consequences of serving stale data? What are all the code paths that modify this data — and does each one correctly invalidate the cache?" A cache that's 99% correct and 1% stale is sometimes worse than no cache at all, because the bugs are hard to reproduce and the impact is subtle.

## Revision Notes

- Cache hit → Redis (~1ms); Cache miss → DB (~200ms) then store in Redis
- Cache-aside: most common, app manages cache explicitly, DB is source of truth
- Write-around: invalidate on write (simple, always correct, cold miss after write)
- Write-through: update both on write (fresh, but wastes space)
- Write-back: cache-first then async DB (fast writes, risk of data loss)
- TTL jitter prevents stampede; bloom filter prevents penetration; Redis HA prevents avalanche
- LRU eviction: least recently used is evicted when memory full
- RDB = periodic snapshot; AOF = append-every-write log; both for durability

---

<a name="part-5"></a>
# Part 5 — Application Scaling: Running More Than One of Everything

## The Problem: One App Server Isn't Enough

Your Snapgram app server is a single Node.js process running on one machine. It handles incoming HTTP requests one at a time (or with concurrency, up to its CPU/memory limits). As traffic grows, two problems emerge:

1. **Capacity:** The server can only handle so many simultaneous requests. During peak hours, the queue grows, latency spikes, and requests time out.

2. **Single point of failure:** If the server crashes — a bug, a memory leak, a hardware failure — your entire application is down. Every user gets errors. There's no backup.

Both problems have the same solution: run more than one app server.

---

## Vertical Scaling vs. Horizontal Scaling

**Vertical Scaling (Scale Up):** Buy a bigger server. Replace your 2-core/4GB RAM machine with an 8-core/32GB RAM machine.

- **Pros:** Zero architectural changes. No new failure modes. Simple.
- **Cons:** Has a hard limit (biggest available instance). Doesn't solve single-point-of-failure. Gets exponentially expensive. A single 64-core server costs far more than 8 × 8-core servers.

**Horizontal Scaling (Scale Out):** Run more servers of the same size. Add a second, third, fourth server identical to the first.

- **Pros:** Near-linear scaling (add servers for more capacity), redundancy (one server fails, others absorb load), cheaper per unit.
- **Cons:** Requires a load balancer in front. Application must be stateless (more on this shortly).

**In practice:** Vertical scaling is your first move — it's fast and requires no architectural changes. Horizontal scaling is your long-term strategy.

---

## Load Balancers

When you have multiple app servers, you need something in front to distribute incoming traffic. That's a **load balancer**.

```
              Incoming requests
                    |
                    v
            [ Load Balancer ]
            /       |        \
       [App 1]  [App 2]  [App 3]
            \       |        /
                    v
             [ Database ]
```

A load balancer's job:
1. Receive incoming requests
2. Pick a healthy backend server
3. Forward the request
4. Return the response to the client

### Load Balancing Algorithms

**Round Robin:** Distribute requests to servers in rotation — first request to App 1, second to App 2, third to App 3, fourth to App 1, and so on. Simple, works well when all requests take roughly the same time.

**Least Connections:** Route to the server with the fewest active connections. Better for workloads where some requests take much longer than others (e.g., file uploads vs. profile reads). Avoids overloading one server while others are idle.

**IP Hashing:** Hash the client's IP address and always route that client to the same server. Useful when you have sticky session requirements (discussed next). Consistent for a given client as long as the server pool doesn't change.

**Weighted Round Robin:** Assign weights to servers — more powerful servers receive proportionally more traffic.

### Health Checks

Load balancers continuously probe each backend server (e.g., `GET /health` every 5 seconds). If a server fails to respond or returns an error, the load balancer removes it from rotation. Traffic automatically routes to healthy servers. When the server recovers, it's added back.

This is what makes horizontal scaling fault-tolerant: one server dying doesn't mean downtime.

### Layer 4 vs. Layer 7 Load Balancing

**Layer 4 (Transport Layer):** Routes based on IP address and TCP/UDP port. Fast — doesn't inspect packet content. Suitable for raw TCP load balancing.

**Layer 7 (Application Layer):** Inspects the HTTP content of requests. Can route based on URL path (`/api/*` → backend service A, `/web/*` → backend service B), HTTP headers, cookies, or body content. More flexible but slightly slower due to inspection overhead.

Most modern web apps use **Layer 7** load balancers (AWS ALB, NGINX, HAProxy).

---

## Stateless vs. Stateful Services

This is a critical concept. Here's the problem.

Suppose you add a login feature. When a user logs in, you create a session and store it in memory:

```
Server 1's memory: { session_id: "abc123" → user_id: 42 }
```

The user makes a request. Load balancer sends it to Server 1. Server 1 checks its memory — session found. OK, user 42.

User makes another request. Load balancer sends it to Server 2 (round-robin). Server 2's memory has no such session. User appears logged out. 

This is a **stateful server** — server behavior depends on data stored locally in its memory. Stateful servers break horizontal scaling because the load balancer routes requests to any server, but only one server has the relevant state.

**Solutions:**

**1. Sticky Sessions (Session Affinity):** Configure the load balancer to always route a given user to the same server (using IP hashing or a cookie). This "fixes" the statefulness problem — but it's fragile. If that server crashes, the session is lost. If you're adding servers during a traffic spike, some users' sessions suddenly can't be served.

**2. Centralized Session Store:** Store sessions in Redis instead of in-process memory. Every app server reads from the same Redis. Any server can handle any request.

```
App Server 1 \
App Server 2  ----> [ Redis (shared session store) ] → session_id: abc123 → user_id: 42
App Server 3 /
```

**This is the key to stateless services:** By moving all state out of application server memory into shared stores (Redis, databases), your app servers become **interchangeable**. Any server can handle any request. This is what makes horizontal scaling clean and fault-tolerant.

**Stateless services** store no per-request or per-user data locally. All state is in the database, cache, or object store. The servers themselves are disposable — you can add or remove them at any time.

Modern authentication via **JWT (JSON Web Tokens)** is another approach: the authentication state is encoded in the token itself (signed, not secret), which the client presents on every request. No server-side session storage needed at all.

---

## Service Discovery

When you have many app servers, how does the load balancer know their addresses? When you spin up a new server, how does it register itself? When a server crashes, how does the load balancer know to stop routing to it?

This is **service discovery** — the mechanism by which services find and track each other in a dynamic environment.

**Simple approach (static configuration):** List all server IPs in the load balancer config. Works for small, stable deployments. Terrible for dynamic environments (containers, auto-scaling) where servers start and stop constantly.

**DNS-based service discovery:** Services register themselves with a DNS server. Load balancers look up the current set of servers via DNS. Works with low TTL. Simple.

**Dedicated service registry (Consul, etcd, ZooKeeper):** Services register themselves on startup and deregister on shutdown. The registry performs health checks. Other services query the registry to discover healthy instances. More sophisticated, used in microservices architectures.

**Cloud-native (AWS ECS/EKS, Kubernetes):** The cloud orchestration platform manages service discovery automatically. Services are identified by name; the platform handles routing to healthy instances. This is the modern standard for containerized microservices.

---

## Auto-Scaling: Paying Only for What You Need

One of the greatest benefits of horizontal scaling with stateless services is **auto-scaling** — automatically adding or removing servers based on current load.

At 2 AM, Snapgram has 1,000 active users. You run 2 app servers. At 8 PM peak, 50,000 active users. You need 20 app servers.

Without auto-scaling: you run 20 servers 24/7 to handle peak load. At 2 AM, 18 servers are mostly idle. Waste.

With auto-scaling: at 2 AM, run 2 servers. As traffic rises through the day, automatically spin up more servers. As traffic falls at night, terminate the extras. You pay only for capacity you use.

Cloud providers (AWS, GCP, Azure) offer auto-scaling groups that monitor CPU/request rate and scale up/down automatically. Combined with stateless services (so new servers can immediately serve any request), this is extremely effective.

---

## The Connection Pool Problem

When you have 20 app servers each making database queries, and your database allows 500 simultaneous connections, you need to be careful. If each app server maintains 30 connections to the database, 20 servers × 30 = 600 connections — exceeding MySQL's limit.

**Connection pooling** — maintaining a fixed pool of reusable database connections rather than opening/closing one per request — is standard practice. Each app server has a connection pool of (say) 20 connections. Requests borrow a connection, use it, and return it to the pool.

For very large-scale deployments, a dedicated **connection pooler** like PgBouncer (for PostgreSQL) sits between app servers and the database, multiplexing thousands of app-side connections onto a small number of actual database connections.

---

## Summary

- **Vertical scaling** (bigger server): fast, simple, no architecture changes — but has a ceiling and no redundancy
- **Horizontal scaling** (more servers): near-linear capacity increase, redundancy — requires load balancer and stateless services
- Load balancers: distribute traffic, perform health checks, remove failed servers automatically
- **Round robin** for uniform requests; **least connections** for variable-length requests
- **Stateless services**: move all state (sessions, caches) to shared external stores (Redis, DB)
- Sessions in Redis (or JWT) rather than in-process memory is mandatory for multi-server deployments
- Auto-scaling + stateless services = pay only for what you use
- Connection pooling prevents overwhelming the database with connections

## Common Interview Questions

- "What is the difference between vertical and horizontal scaling? When would you choose each?"
- "What does it mean for a service to be stateless? Why does it matter for scaling?"
- "How do sticky sessions work? What are their tradeoffs?"
- "What happens if a load balancer goes down? How do you make it highly available?"

## Common Mistakes

- Not mentioning stateless design when proposing to add more servers — stateful services break with multiple servers
- Forgetting that the load balancer itself can be a single point of failure (answer: run load balancers in pairs / use cloud-managed ones)
- Not discussing health checks — how does the load balancer know a server is down?
- Confusing Layer 4 and Layer 7 load balancing

## Real-World Examples

- Netflix uses auto-scaling extensively — their fleet of application servers scales up dramatically during peak evening hours and scales down overnight
- Amazon runs stateless services almost universally — this is one of the key architectural principles that enables them to scale services independently

## Senior Engineer Perspective

The shift to stateless services is one of the most important architectural decisions a team can make. Senior engineers insist on it early — not because you need 20 servers today, but because retrofitting a stateful service into a stateless one later is painful. Sessions embedded in application memory, local file system writes, in-process caches — these are all technical debt that must be paid when you need to scale. Design stateless from the start, and scaling becomes a simple matter of adding more identical servers.

## Revision Notes

- Vertical = bigger server (fast, limited, no HA); Horizontal = more servers (scalable, HA, needs LB + stateless)
- Load balancer algorithms: round-robin (uniform), least-connections (variable), IP hash (sticky)
- Stateless = no per-user data in app server memory; all state in Redis/DB
- JWT = token carries auth state; no server-side session needed
- Auto-scaling = add/remove servers based on load; requires stateless services
- Connection pool = reuse DB connections; prevents connection exhaustion

---

<a name="part-6"></a>
# Part 6 — Distributed Systems: The Pandora's Box You Open When You Scale

## The Monolith: A Love Letter

Before talking about distributed systems, let's appreciate what you give up when you leave a monolith.

A monolith is a single deployable unit containing all your application code. When Snapgram is a monolith:

- **Calling user service from photo service** is a function call in memory. It takes nanoseconds. It never fails. It's never slow unless your code is slow.
- **Transactions across features** are trivial — everything shares one database, one transaction context.
- **Debugging** means following a single call stack. One log file. One deployment. One rollback.
- **Testing** means running one test suite. Integration tests are easy because everything is co-located.
- **Operations** means deploying one thing, monitoring one thing, scaling one thing.

The monolith is beautiful in its simplicity. And at small to medium scale, it's often the right choice.

---

## Why Monoliths Eventually Cause Pain

At scale, the monolith's strengths become its weaknesses.

**Deployment coupling:** A bug in the notification service requires deploying the entire app — including the photo service, user service, feed service — to fix it. Deployments become high-risk events that could break anything.

**Scaling inflexibility:** Your feed generation algorithm is CPU-intensive. Your notification delivery is I/O-bound. Your photo storage is disk-bound. But in a monolith, everything runs on the same machines. You can't scale the feed service independently of the notification service.

**Team scaling:** As your engineering team grows from 5 to 50 to 500 engineers, they all commit to the same codebase. Merge conflicts multiply. Build times balloon. A change in one team's area can unexpectedly break another's.

**Technology lock-in:** The entire monolith uses the same language, framework, and database. Need Elasticsearch for search? You must integrate it at the language level. Want to try a new language for a performance-critical component? You can't without rewriting everything.

---

## Microservices: The Promise

Microservices decompose a large application into small, independently deployable services, each responsible for one domain:

```
Monolith:
┌──────────────────────────────────────────┐
│ Users │ Photos │ Feed │ Notifications │ DM │
└──────────────────────────────────────────┘
          One process, one DB, one deployment

Microservices:
┌──────┐  ┌───────┐  ┌──────┐  ┌──────────────┐  ┌──────┐
│Users │  │Photos │  │ Feed │  │Notifications │  │  DM  │
└──┬───┘  └───┬───┘  └──┬───┘  └──────┬───────┘  └──┬───┘
   │          │          │              │              │
 Users DB   Photos DB  Feed Cache  Notif DB       Messages DB
```

Each service:
- Deployed independently (update notifications without touching photos)
- Scaled independently (scale the feed service × 10 without scaling notifications)
- Has its own database (choice of database technology per service's needs)
- Can be written in any language
- Owned by one small team

This sounds ideal. And when it works, it's transformative. But the distributed system problems that come with it are severe.

---

## The Cost of Distribution: What Goes Wrong

When service A calls service B across a network instead of across function call boundaries, everything becomes harder.

### Network Calls Fail in Ways Function Calls Don't

A function call either returns a value, throws an exception, or loops forever. A network call can:

- Return successfully
- Return an error
- Time out (server received and is processing, but response takes too long)
- **Partially succeed** — the server received the request and executed it, but the response was lost. Did it succeed? You don't know.
- Drop silently due to a network blip

The last two cases are especially insidious. If you send a payment request and don't get a response — did it go through? If you retry and it actually did go through, you've charged the user twice.

This is the **partial failure problem**, and it doesn't exist in monoliths.

### Latency Adds Up

In a monolith: `user_service.get_user(42)` → 0.001ms (function call in memory)

In microservices: `HTTP GET api.user-service/users/42` → 1–10ms (network + serialization + deserialization)

If feed generation calls 5 different services (user service, photo service, like count service, etc.), and each call takes 5ms, that's 25ms added to every feed request — just in service-call overhead, not including actual database queries.

If those 5 calls happen **sequentially**, you wait for each before making the next. If they happen in **parallel** (which requires more complex code and error handling), you wait for the slowest one.

At scale, the tail latency problem is severe: even if 99% of calls take 5ms, 1% take 500ms. When a single request depends on 5 service calls, the probability of hitting at least one slow call is much higher than 1%.

### Distributed Transactions: A Nightmare

In a monolith with one database, transferring money is simple:
```sql
BEGIN TRANSACTION;
UPDATE accounts SET balance = balance - 100 WHERE id = 1;
UPDATE accounts SET balance = balance + 100 WHERE id = 2;
COMMIT;
```

Atomic. Either both updates happen, or neither. ACID guarantees this.

In microservices, User Account Service and Payment History Service have different databases. How do you atomically update both?

You can't use a SQL transaction across different databases. The solutions are all complex:

**Two-Phase Commit (2PC):** A coordinator asks all participants to "prepare" (lock their resources), then asks them to "commit." If any participant fails during prepare, all roll back. Very slow (requires multiple round trips), and the coordinator can become a bottleneck. Rarely used in practice.

**Saga Pattern:** Break the transaction into a sequence of local transactions, each with a compensating action that undoes it if a later step fails. Debit account (local) → Record transaction (local) → if any step fails, run compensating actions to reverse prior steps. Complex to implement correctly. No isolation — intermediate states are visible.

**Eventual consistency:** Accept that data will be inconsistent for a short window. Design your system to handle it gracefully. Most large-scale systems use this approach for most operations.

---

## Service Communication Patterns

### Synchronous (Request-Response)

Service A calls Service B and waits for a response before continuing:

```
Feed Service ——REST/gRPC——> User Service ——> returns user data ——> Feed Service continues
```

Simple to reason about. But Service A is blocked while B processes. If B is slow or unavailable, A is also slow or unavailable. This **propagates failures** across the system — one slow service can slow down every service that depends on it.

**Timeouts and retries** are critical: always set a timeout on outbound calls. If B takes more than 500ms, fail the request rather than blocking indefinitely. But retries can cause problems if the request was already executed (idempotency required).

### Circuit Breaker Pattern

If Service B is consistently failing (returning errors or timing out), continuing to send it requests is counterproductive — it wastes resources on requests that won't succeed, adds latency to every request, and can overwhelm B's recovery.

A **circuit breaker** wraps Service B calls. It tracks the success/failure rate:
- **Closed:** Normal operation, calls pass through
- **Open:** Too many failures detected — calls immediately fail with an error (no network call attempted), giving B time to recover
- **Half-Open:** After a timeout, allow one test request. If it succeeds, close the circuit. If it fails, stay open.

```
Normal:    A → [CLOSED] → B  (calls pass through)
B failing: A → [OPEN]   ✗   (calls immediately fail, no load added to B)
Recovery:  A → [HALF-OPEN] → B  (one test call allowed)
```

Circuit breakers prevent **cascading failures** — where one unhealthy service causes all dependent services to queue up requests, exhaust their connection pools, and fail too.

### Asynchronous (Event-Driven)

Service A publishes an event. Service B (and C and D) subscribe to and process that event, independently and at their own pace:

```
Photo Service publishes: "photo_uploaded" event
    ↓
[Message Queue / Event Stream (Kafka)]
    ↓              ↓                ↓
Feed Service   Notification   Analytics
(adds to feeds) (sends push) (logs analytics)
```

Service A doesn't wait for B, C, or D to finish. They process independently. This provides:
- **Decoupling:** Photo Service doesn't know about Feed Service. Feed Service just subscribes to events.
- **Resilience:** If Notification Service is down, events queue up and are processed when it recovers. Photo Service is unaffected.
- **Scalability:** Add more Feed Service instances to consume events faster.

The cost: eventual consistency. The feed isn't updated *while* the photo is uploaded — it's updated *shortly after*. And complex flows become harder to trace and debug (an event triggers B, which triggers C, which triggers D — tracking this through distributed logs requires correlation IDs and distributed tracing).

We'll cover Kafka and message queues in depth in Part 7.

---

## Service Boundaries: The Most Important Decision

Getting microservice boundaries wrong is one of the most common architectural mistakes teams make. The temptation is to create many fine-grained services. The result is a **distributed monolith** — services so tightly coupled that deploying one requires deploying all others, but you now have all the operational complexity of microservices with none of the benefits.

**How to define good service boundaries:**

**Domain-Driven Design (DDD):** Group services around **bounded contexts** — well-defined business domains with clear ownership. "Users" is a domain. "Payments" is a domain. "Content" (photos, videos) is a domain. Each domain owns its data and logic.

**The Single Responsibility Principle at service level:** A service should have one reason to change. If you find yourself deploying Service A every time you change Service B's business rules, they're probably the same service.

**Follow your team structure:** Conway's Law states that organizations design systems that mirror their communication structure. If you have a "User Team" and a "Payments Team," you'll have a User Service and a Payments Service. This is actually useful guidance — design service boundaries that map to team ownership.

---

## Summary

- Monoliths are simple, fast (function calls vs. network calls), easy to test and deploy — the right choice until scale genuinely requires otherwise
- Microservices provide deployment independence, independent scaling, polyglot freedom — at the cost of distributed systems complexity
- Network calls fail in ways function calls don't: timeouts, partial failures, unknown states
- Distributed transactions are hard: 2PC is slow and fragile; sagas are complex; eventual consistency is the practical default
- Circuit breakers prevent cascading failures when services are unhealthy
- Asynchronous event-driven communication (Kafka) decouples services and increases resilience
- Service boundaries should reflect business domains — fine-grained services with tight coupling create distributed monoliths

## Common Interview Questions

- "What are the tradeoffs between a monolith and microservices?"
- "How would you handle a distributed transaction across multiple services?"
- "What is a circuit breaker and when would you use one?"
- "How do you handle retries safely when service calls can fail?"

## Common Mistakes

- Recommending microservices for a small team or early-stage product without discussing the operational cost
- Not discussing idempotency when talking about retries — retrying a non-idempotent call (like a payment) without safeguards causes duplicates
- Not mentioning circuit breakers when discussing service dependencies — a single slow service can cascade
- Designing microservices that share a database — this defeats the point of service isolation

## Senior Engineer Perspective

Senior engineers are often *more* conservative about microservices than junior engineers. They've seen the operational cost: distributed tracing, service meshes, increased deployment complexity, harder debugging, distributed transaction headaches. They ask: "Does the benefit of deployment independence and independent scaling actually outweigh the cost at our current scale and team size?" Often the answer at < 50 engineers is no. The right answer is: "Start with a well-structured modular monolith. Extract services when there's a demonstrated need — usually when deployment independence or independent scaling becomes genuinely painful."

## Revision Notes

- Monolith: function calls (fast, no failure modes), one DB, one deployment, one codebase
- Microservices: network calls (slow, fail in multiple ways), independent DBs, independent deployment
- Circuit breaker: Closed → Open (on failure threshold) → Half-Open (test call) → Closed (on success)
- Partial failure = network call may have executed but response lost; requires idempotency
- Saga = sequence of local transactions with compensating actions for rollback
- Async events (Kafka) = decoupling + resilience at the cost of eventual consistency
- Good service boundaries = bounded contexts (business domains), not fine-grained technical layers

---

<a name="part-7"></a>
# Part 7 — Messaging Systems: Kafka, Queues, and the Art of Asynchronous Work

## The Problem: Synchronous Processing Doesn't Scale

You've just built Snapgram's photo upload feature. Here's what happens when a user uploads a photo:

1. Save the original image to object storage (S3)
2. Generate 3 thumbnail sizes (small, medium, large)
3. Run the image through a content moderation AI model (detects NSFW content)
4. Update the user's photo count in the database
5. Create feed entries for all the user's followers
6. Send push notifications to followers

Steps 2–6 are done synchronously, in the upload request handler. The user's browser waits for all of them to complete before getting a response.

**The problem:** Step 5 — creating feed entries for all followers — is brutal if the user has 1 million followers. Creating 1 million database rows takes minutes. The user's upload request is held open for minutes. Their browser times out. The upload appears to fail. They try again. You now have 2 million feed entries from one photo.

Even for a user with 1,000 followers, this could take 1–2 seconds. The user waits for a 1–2 second response just to upload a photo. Bad experience.

The insight: **the user doesn't need all these things to happen before they get a confirmation.** They just need to know the photo was saved. Everything else can happen afterward.

This is **asynchronous processing** — and it's where message queues and event streams come in.

---

## Queues vs. Event Streams: The Mental Model

Before diving into specific technologies, understand the two fundamental models:

**Message Queue (point-to-point):**
- Producer puts a message in a queue
- One consumer picks it up and processes it
- The message is **removed from the queue** after processing
- If multiple consumers exist, each message goes to exactly one of them
- Example: a background job queue for sending emails — each email should be sent exactly once

**Event Stream (publish-subscribe):**
- Producer publishes an event to a topic
- **Multiple consumer groups** can independently read and process the event
- Events are **retained** for a configurable period (days, weeks) even after being consumed
- Each consumer group has its own pointer into the stream (its own "offset")
- Example: "photo uploaded" event — feed service, notification service, and analytics service all need to independently process it

---

## Starting with a Simple Queue: RabbitMQ

Before Kafka existed, teams used message queues like RabbitMQ to decouple producers and consumers.

Here's the photo upload problem solved with a queue:

```
User uploads photo
    |
    v
Photo Service:
  1. Save image to S3                    (synchronous — user waits)
  2. Write photo record to DB            (synchronous — user waits)
  3. Push "process_photo" message to queue   (fast — just a queue write)
  4. Return "upload successful" to user  (user gets response in ~200ms)
    |
    | (asynchronously, in background)
    v
Worker Process consuming from queue:
  - Generate thumbnails
  - Run content moderation
  - Create feed entries
  - Send push notifications
```

The user gets a fast response. Background workers process the remaining steps asynchronously. This is the **producer-consumer pattern**.

**RabbitMQ** implements the AMQP protocol. Messages are pushed to **queues**. Workers subscribe to queues and process messages as they arrive. RabbitMQ handles message routing, persistence (messages survive restarts), and acknowledgments (worker must ACK a message to confirm processing; if the worker crashes before ACKing, RabbitMQ redelivers to another worker).

**Where RabbitMQ shines:** Task queues, job scheduling, work distribution. One message → one worker processes it.

**Where RabbitMQ struggles:** When multiple independent systems need to process the same event, or when you need to replay events (e.g., "reprocess all photos uploaded last week"). Once consumed, messages are gone.

---

## Kafka: The Event Stream That Changed Everything

Apache Kafka was built at LinkedIn around 2010 to solve a problem that queues couldn't: **multiple teams needed to independently process the same stream of events in real time, and they needed to be able to replay historical events**.

LinkedIn had hundreds of engineers building dozens of services. Every service produced events (page views, connection requests, job applications) and many services needed to consume those same events. With point-to-point queues, every producer would need to know about every consumer and push to separate queues. Adding a new consumer meant modifying the producer. This was a mess.

Kafka's insight: **make events a persistent, replayable, ordered log. Any consumer can read any event, at any time, independently of other consumers.**

### Kafka Architecture from First Principles

**Topics:** Events are organized into named **topics**. A topic is like a category. "photo_uploads" is a topic. "user_signups" is a topic. "payment_completed" is a topic.

**Partitions:** Each topic is divided into **partitions** — ordered, immutable sequences of records. Each partition is stored on disk as an append-only log. New records are appended to the end. Older records are never overwritten (just eventually deleted after the retention period).

```
Topic: "photo_uploads" (3 partitions)

Partition 0: [event_1] [event_4] [event_7] [event_10] ...
Partition 1: [event_2] [event_5] [event_8] [event_11] ...
Partition 2: [event_3] [event_6] [event_9] [event_12] ...
```

**Offsets:** Every record within a partition has a unique, monotonically increasing **offset**. The first record has offset 0, the second offset 1, and so on. When a consumer reads a record, it commits the offset it last processed. If it crashes and restarts, it resumes from the last committed offset — no message is lost.

**Producers:** Producers write events to topics. The producer decides which partition to write to — either by providing an explicit partition key (all events with the same key go to the same partition, preserving order for that key) or via round-robin.

**Consumers and Consumer Groups:** Consumers read from partitions. **Consumer groups** are the key abstraction for parallel consumption. Each consumer group has exactly one consumer reading from each partition. All consumers in the group together read the entire topic, but each partition is owned by exactly one consumer in the group.

```
Topic "photo_uploads" (3 partitions)

Feed Service Consumer Group:
  Consumer A → reads Partition 0
  Consumer B → reads Partition 1
  Consumer C → reads Partition 2

Notification Consumer Group (independent):
  Consumer D → reads Partition 0
  Consumer E → reads Partition 1
  Consumer F → reads Partition 2
```

Feed Service and Notification Service each read every event — independently. Adding a new consumer group (e.g., Analytics) doesn't affect existing groups at all. The producer doesn't need to know about consumers.

### Why Partitions Matter for Throughput and Ordering

Kafka's throughput is proportional to the number of partitions. More partitions → more consumers can read in parallel → higher throughput. A well-provisioned Kafka cluster can handle millions of messages per second.

But partitions affect ordering: **Kafka only guarantees ordering within a partition, not across partitions.** If you care about processing events for a specific entity (e.g., all events for user_42) in order, you must ensure they all go to the same partition. You do this via a **partition key** — Kafka hashes the key to determine the partition. If you use `user_id` as the partition key, all events for user_42 always go to the same partition and are processed in order.

### Replication: Making Kafka Fault-Tolerant

Each partition is replicated across multiple **brokers** (Kafka servers). One broker is the **leader** for that partition — all reads and writes go to the leader. Other brokers hold **replicas** that stay synchronized with the leader.

If the leader broker crashes, one of the replicas is automatically elected as the new leader. This happens within seconds. Producers and consumers reconnect to the new leader seamlessly. No data is lost (assuming the replica was up-to-date).

Replication factor 3 (1 leader + 2 replicas) is standard for production Kafka deployments.

### Kafka's Durability: It's a Log on Disk

Unlike in-memory queues, Kafka persists every message to disk. This means:
- Messages survive broker restarts
- Messages can be replayed: "give me all photo upload events from the last 7 days"
- Messages are retained for a configurable period (days, weeks, or indefinitely)
- If a downstream service has a bug and processes events incorrectly, you can fix the bug and replay from the beginning

This replay capability is one of Kafka's most powerful features — you can rebuild derived data stores from scratch by replaying the event log.

### Kafka: The Complete Picture

```
Producers                 Kafka Cluster                Consumers
                                                        
Photo Service ──────────> [Topic: photo_uploads]
                          [Partition 0] ──────────────> Feed Service
                          [Partition 1] ──────────────> Notification Service
                          [Partition 2] ──────────────> Analytics Service

User Service ───────────> [Topic: user_signups]
                          [Partition 0] ──────────────> Email Service
                          [Partition 1] ──────────────> Onboarding Service

Payment Service ────────> [Topic: payment_events]
                          [Partition 0] ──────────────> Ledger Service
                          [Partition 1] ──────────────> Fraud Detection
```

Each service produces and consumes independently. Adding a new consumer is zero impact on producers. Replaying events is trivial.

---

## Dead-Letter Queues (DLQ)

What happens when a consumer fails to process a message? For example, the thumbnail generation service receives a "process photo" message, but the image is corrupted — any attempt to process it throws an exception.

If you keep retrying, it loops forever, blocking the processing of all subsequent messages on that partition. This is called a **poison pill** message.

**Dead-Letter Queue:** After N failed retries, move the problematic message to a separate "dead-letter" topic or queue. The DLQ stores messages that couldn't be processed. Engineers can inspect them, fix the bug, and reprocess them manually — or alert on them, or discard them with logging.

DLQs are critical for production systems. Without them, one bad message can permanently stall a consumer.

---

## Ordering, Idempotency, and At-Least-Once Delivery

Kafka provides **at-least-once delivery** by default — each message is delivered at least once, but network issues or consumer crashes may cause a message to be delivered more than once. If a consumer processes a message and crashes before committing the offset, it will reprocess the same message after restarting.

This means consumer logic must be **idempotent**: processing the same message twice must produce the same result as processing it once.

For a feed update: "add photo_id=99 to feed for user_42" — if this is processed twice, you just try to add the same photo to the feed twice. If your feed query deduplicates by photo_id, the second processing is a no-op. Idempotent.

For a payment: "charge user $10" — processing this twice charges the user $20. Not idempotent. You must include an idempotency key (a unique transaction ID) and check whether it's already been processed before charging.

**Exactly-once delivery** (process each message exactly once) is possible in Kafka using transactions, but it's significantly more complex and has performance overhead. Most systems use at-least-once + idempotent consumers.

---

## Amazon SQS: A Simpler Queue for Simpler Needs

AWS SQS (Simple Queue Service) is a fully managed message queue service. It provides simple point-to-point queuing without Kafka's complexity.

**Standard Queue:** At-least-once delivery, best-effort ordering. Messages might be delivered multiple times, might be out of order. Simple and extremely scalable.

**FIFO Queue:** Exactly-once processing, strict ordering within a message group. Slower (limited to ~300 transactions/second per queue).

SQS is excellent when you need a simple background job queue and don't need Kafka's event stream semantics (replay, multiple consumer groups, high throughput). Many teams use SQS for simple task queues (email sending, PDF generation) and Kafka for high-volume event streaming.

---

## When to Use What

| Technology | Use When |
|------------|----------|
| **Kafka** | High-volume events, multiple independent consumers, need replay, ordering requirements, event sourcing |
| **RabbitMQ** | Task queues, complex routing rules, moderate volume, strong delivery guarantees needed |
| **SQS** | AWS ecosystem, simple job queues, fully managed, no replay needed |
| **Redis Pub/Sub** | Real-time notifications within a single datacenter, ephemeral (no persistence), simple fan-out |

---

## Summary

- Synchronous processing creates tight coupling and blocks users waiting for background work
- **Message queues** (RabbitMQ, SQS): point-to-point, one consumer per message, message removed after processing
- **Event streams** (Kafka): multiple consumer groups read independently, events retained and replayable
- Kafka's core abstractions: **topics** (categories), **partitions** (ordered logs, enable parallelism), **offsets** (consumer position), **consumer groups** (parallel, independent consumption)
- Partitioning key ensures ordering for a specific entity; same key → same partition → ordered delivery
- Replication factor 3 standard for fault tolerance
- At-least-once delivery requires idempotent consumer logic
- Dead-letter queues prevent poison pill messages from stalling consumers

## Common Interview Questions

- "Why would you use Kafka instead of a simple database table for event processing?"
- "Explain Kafka partitions and how they affect ordering."
- "What is a consumer group and how do consumer groups enable parallel processing?"
- "What is at-least-once delivery and what does it require of consumers?"
- "How would you design a notification system for a social network using Kafka?"

## Common Mistakes

- Saying "Kafka is a message queue" — it's an event stream (different semantics: retention, replay, multiple consumer groups)
- Not discussing consumer groups when explaining Kafka scalability
- Assuming Kafka guarantees global ordering — it only guarantees ordering within a partition
- Forgetting idempotency — retries are a fact of life in distributed systems

## Real-World Examples

- LinkedIn (Kafka's birthplace) uses Kafka for activity data, metrics, and operational data pipelines — trillions of messages per day
- Uber uses Kafka for matching events, trip updates, and supply/demand adjustments in real time
- Netflix uses Kafka for their operational event pipeline connecting hundreds of internal services

## Senior Engineer Perspective

The most important question when introducing a message queue or Kafka: "What are the delivery semantics, and is our consumer logic idempotent?" At-least-once delivery is almost universal. That means consumers will see duplicates. A senior engineer ensures every consumer either naturally handles duplicates (idempotent operations) or explicitly checks for them (idempotency keys). The second question: "What happens when this queue/topic gets behind?" If consumers are slower than producers, the queue grows. Eventually it hits capacity limits or latency targets. You need a plan: add consumer instances, throttle producers, or drop non-critical messages.

## Revision Notes

- Queue: point-to-point, one consumer per message, message deleted after ACK
- Stream (Kafka): multiple consumer groups, messages retained, replay possible
- Topic → Partitions → Records with offsets
- Partition key → same key → same partition → ordered for that key
- Consumer group: each partition owned by exactly one consumer in the group; groups are independent
- At-least-once: consumer may see duplicates → must be idempotent
- DLQ: message that fails N retries moved to dead-letter topic for manual inspection
- Replication: leader + N replicas; auto-failover if leader crashes

---

<a name="part-8"></a>
# Part 8 — Data at Scale: Replication, Sharding, CAP Theorem, and Consistency

## The Problem: One Database Can't Hold It All

Snapgram has grown to 100 million users. Your MySQL database now contains:
- 100 million user records
- 10 billion photos
- 500 billion likes
- 200 billion follows

A single MySQL server — even the largest available — can hold and serve this data. But barely. And as you grow, it can't scale further. You need to distribute data across multiple database servers.

There are two ways to distribute data: **replication** and **partitioning (sharding)**. They solve different problems.

---

## Replication: Multiple Copies for Reads and Fault Tolerance

We introduced replication in Part 1. Let's go deeper.

**Replication** means maintaining multiple identical copies of your data across multiple servers. The canonical pattern: one **primary** (master) accepts all writes, and multiple **replicas** (secondaries/slaves) receive a stream of changes from the primary and apply them to their own copies.

### What Replication Solves

1. **Read scaling:** Route read queries to replicas. Add more replicas for more read capacity.
2. **Fault tolerance:** If the primary dies, promote a replica to become the new primary. Minimal downtime.
3. **Geographic distribution:** Put replicas in different data centers/regions. Serve reads to users from a nearby replica.

### Replication Lag: The Core Problem

After a write to the primary, replicas lag behind by some amount — typically milliseconds to seconds, depending on network and load. This is **replication lag**. As we discussed, this causes **read-after-write inconsistency**.

But there's a deeper problem: what if the primary crashes and you promote a replica that was significantly behind?

Scenario:
- User writes post at 10:00:00.000
- Primary acknowledges
- Replica lag: 2 seconds — replica is at 09:59:58
- Primary crashes at 10:00:01
- Replica promoted to primary
- User's post is gone (not yet replicated)

This is **data loss on failover**. To prevent it, you can use **synchronous replication**: the primary waits for at least one replica to confirm the write before acknowledging to the client. This eliminates data loss on failover, but adds latency to every write (must wait for network round trip to replica).

**Semi-synchronous replication** (MySQL's default): the primary waits for *at least one* replica to confirm. Balances data safety and write latency.

### Leader Election and Split-Brain

When the primary crashes, replicas must elect a new leader. This is done by consensus protocols (or tools like Patroni for PostgreSQL, MHA for MySQL). 

The dangerous failure mode: **split-brain**. If a network partition causes replicas to think the primary is dead (but it's still running, just unreachable), two nodes may both believe they are the primary. Both accept writes. The data diverges. When the partition heals, you have two conflicting histories.

Preventing split-brain requires **fencing** (ensuring the old primary can't accept writes) and **quorum** (requiring a majority of nodes to agree on the new leader). This is a hard problem in distributed systems.

---

## Sharding: Distributing Data to Scale Writes

Replication scales reads. But all writes still go to one primary. Once your write volume exceeds what one server handles, you need to scale writes — and that means **sharding** (also called **partitioning**).

Sharding splits your data across multiple database servers, each responsible for a **subset** of the data. Instead of one MySQL server with all 100 million users, you might have:
- Shard 0: users with IDs 1–25,000,000
- Shard 1: users with IDs 25,000,001–50,000,000
- Shard 2: users with IDs 50,000,001–75,000,000
- Shard 3: users with IDs 75,000,001–100,000,000

Each shard is a full MySQL server (with its own replicas for redundancy). Reads and writes for a user go to their shard only.

### Sharding Strategies

**Range-based sharding:** Shard by value range (user IDs 1–25M on Shard 0). Simple to reason about. But creates **hot spots** — if most new users have high IDs, Shard 3 gets all new user writes while Shards 0–2 are idle.

**Hash-based sharding:** Hash the shard key and take modulo the number of shards. `shard = hash(user_id) % num_shards`. Distributes data evenly. But adding a shard requires remapping all data — expensive. Solved by consistent hashing.

**Consistent hashing:** A technique that minimizes data movement when adding or removing shards. Place both data and servers on a virtual "ring" of hash values. Each server owns a range of the ring. Adding a server means it takes over only a portion of one existing server's range — most data doesn't move.

```
Hash Ring (0 to 2^32):

         Shard A
        /
[0 -------- Shard B -------- Shard C -------- Shard D ----] 2^32
        \
         user_id hashes to this position → served by Shard A
```

Adding Shard E: only the data that maps to E's range on the ring moves. Everything else stays put.

### The Shard Key Problem

Choosing the right **shard key** (the field you shard on) is one of the most critical and irreversible decisions in sharding. A bad shard key creates hot spots and makes certain queries impossible.

**Bad shard key: creation time.** All new data goes to the most recent shard. That shard is hot; older shards are cold.

**Bad shard key for social apps: follower count.** Sharding by follower count means shards with celebrities are overwhelmed.

**Good shard key for users: user_id.** Distributes users evenly. Queries for a specific user go to exactly one shard.

**The join problem:** Once you shard, you can no longer JOIN across shards efficiently. If User A (on Shard 0) follows User B (on Shard 2), getting their relationship requires querying two different servers. This is why sharded systems often denormalize data — copying information onto both sides of a relationship — to avoid cross-shard queries.

### Re-sharding: The Nightmare

What happens when your data grows beyond the capacity of your current shards? You need to re-shard — redistribute data across more shards. This is a massive undertaking:

1. Add new shard servers
2. Migrate data from existing shards to new ones (while the system is live)
3. Update routing logic to reflect new shard assignments
4. Verify data integrity

Without consistent hashing, this might mean moving 50–75% of all your data. Even with consistent hashing, re-sharding requires careful coordination and creates temporary inconsistency windows. Many teams avoid sharding as long as possible precisely because re-sharding is so painful.

---

## CAP Theorem: The Fundamental Constraint

In 2000, computer scientist Eric Brewer proposed what became known as the **CAP theorem**:

> In a distributed system, you can only guarantee two of the following three properties simultaneously: **Consistency**, **Availability**, and **Partition Tolerance**.

Let's understand what each means.

**Consistency (C):** Every read returns the most recent write, or an error. All nodes see the same data at the same time. If I write a value on Node A, any subsequent read from Node B returns that value. (Note: this is "linearizability" in CAP — stronger than ACID's consistency.)

**Availability (A):** Every request receives a response (not an error). The system may not return the most recent data, but it responds. No timeouts, no "service unavailable."

**Partition Tolerance (P):** The system continues operating even when network partitions occur (some nodes can't communicate with others). Messages are dropped or delayed between nodes.

**Why can't you have all three?** Consider a network partition: Node A and Node B can't communicate.

- A write comes in to Node A: "user_42's username = 'alice_new'"
- A read comes to Node B for user_42's username

What does Node B return?

If it returns `'alice_new'` → it would have to wait for A to communicate it, but the network is partitioned. Node B blocks until partition heals (sacrificing Availability).

If it returns immediately → it might return the old value `'alice_old'` (sacrificing Consistency).

If it returns an error → it's not Available.

You can't return a fresh value immediately if you can't communicate with the node that has the latest write. **During a network partition, you must choose between Consistency and Availability.**

**Why P is not optional:** Network partitions happen. Networks are unreliable. Hardware fails. Software crashes. A distributed system that can't operate during partitions is not useful in practice. So **P must always be tolerated** — which means the real choice is between **CP** and **AP**.

**CP systems** (Consistent + Partition-tolerant): When a partition occurs, the system stops accepting writes (or becomes unavailable) to avoid serving stale data. Strongly consistent. Example: HBase, ZooKeeper, Spanner.

**AP systems** (Available + Partition-tolerant): When a partition occurs, nodes continue accepting reads and writes. Different nodes may have different data temporarily. Highly available. Eventual consistency: data converges when partition heals. Example: Cassandra, CouchDB, DynamoDB (default).

**A more nuanced view:** CAP is a binary model, but real systems have a spectrum. You can choose where to trade off based on specific operations. Cassandra lets you choose consistency level per query — `CONSISTENCY ALL` is effectively CP behavior; `CONSISTENCY ONE` is AP behavior.

---

## Consistency Models

"Consistency" in distributed systems isn't binary — it's a spectrum from weakest to strongest:

### Eventual Consistency (Weakest)

If no new updates are made, all replicas will eventually converge to the same value. After a write to Node A, there's a window where Node B returns stale data. But eventually, the update propagates and Node B has the new value.

Best for: social feeds, like counts, product view counts, anything where slight delays are acceptable. Most NoSQL systems (Cassandra, DynamoDB default) provide eventual consistency.

### Read-Your-Own-Writes

After you write a value, you will always read back that same value from subsequent reads — even if other users don't yet see it. This is a more specific guarantee: the *writer* sees their own writes immediately.

Implemented by: routing reads by the same user to the same replica as the write, or routing reads to the primary for a short window after writes.

### Monotonic Read Consistency

Once you read a value at version V, you will never read an older version. Reads never go "backward." This prevents the confusing experience of seeing a post, refreshing, and not seeing it.

### Sequential Consistency

Operations appear to execute in the same order on all nodes — though not necessarily in real-time order. All nodes agree on the same sequence of events, but there may be delays.

### Linearizability (Strongest — "Strong Consistency")

Every operation appears to take effect at a single point in time between its invocation and its completion. The system behaves as if it's a single machine with a single copy of data. Any read returns the most recent committed write, globally.

This is what most people mean when they say "strongly consistent." It's the most intuitive model but the most expensive to implement in a distributed system. Achieved by systems like Google Spanner using synchronized clocks and consensus protocols.

---

## Consistency vs. Availability: Business Decision

Which consistency model you choose depends on **business requirements**, not technical preference.

**Strong consistency is required when:**
- Financial transactions (debit account must be immediately visible to prevent double-spending)
- Inventory management (sell only what you have — eventual consistency leads to overselling)
- Configuration updates (all servers must use new config simultaneously)

**Eventual consistency is acceptable when:**
- Social media feeds (if a post takes 1–2 seconds to appear for followers, nobody suffers)
- Like/view counts (showing 1,001 likes vs. 1,000 is not business-critical)
- Search indexes (if a product takes 10 seconds to appear in search after being added, acceptable)
- User profiles in non-critical contexts (avatar change might take 30 seconds to propagate globally)

The mistake is assuming you need strong consistency everywhere. Strong consistency is expensive — it increases latency, reduces availability, and limits scalability. Use it only where the business genuinely requires it.

---

## Summary

- **Replication**: multiple copies of data for reads, fault tolerance, and geographic distribution. Primary + replicas. Introduces replication lag (read-after-write inconsistency) and failover complexity (split-brain risk).
- **Sharding**: split data across multiple servers to scale writes. Each server owns a subset. Introduces shard key problem (hot spots), loss of JOINs, and painful re-sharding.
- **CAP Theorem**: must tolerate partition (it's inevitable) → real choice is CP (consistent under partition, may become unavailable) vs. AP (available under partition, may serve stale data).
- **Consistency spectrum**: eventual → read-your-own-writes → monotonic read → sequential → linearizable (strongest)
- Choose consistency level based on business requirements: financial data needs strong consistency; social feeds tolerate eventual

## Common Interview Questions

- "What is CAP theorem? Can you give an example of a CP system and an AP system?"
- "What is the difference between replication and sharding?"
- "How would you shard a user table? What shard key would you choose and why?"
- "What does eventual consistency mean? When is it acceptable?"
- "What is split-brain and how do you prevent it?"

## Common Mistakes

- Saying a system is "CA" (consistent + available) — this violates CAP; P is non-negotiable in distributed systems
- Confusing replication lag with sharding — they're different problems
- Choosing a shard key that creates hot spots
- Assuming strong consistency is always needed — most social/content applications are fine with eventual consistency

## Real-World Examples

- Google Spanner is a globally distributed CP database — it uses TrueTime (synchronized atomic clocks) to achieve external consistency at global scale
- Cassandra is a canonical AP system — designed for high availability even during network partitions; replicas catch up when partition heals
- Amazon's DynamoDB defaults to eventual consistency for reads but offers optional strongly-consistent reads at higher cost and latency
- Twitter shards user data by user_id — all data for a user lives on the same shard; cross-user operations require cross-shard coordination

## Senior Engineer Perspective

The CAP theorem is often misunderstood in interviews. Senior engineers know that the real practical choice is consistency versus latency, not consistency versus availability. A system can be "always available" and "strongly consistent" when there are no partitions — which is most of the time. The issue is: what do you do *during* the rare partition? Do you refuse writes (CP) or accept writes that might diverge (AP)? The answer should be driven by business impact. For most social media operations, AP is correct. For financial ledgers, CP is non-negotiable.

## Revision Notes

- Replication: primary (writes) + replicas (reads); lag causes stale reads; synchronous replication adds write latency but prevents data loss on failover
- Sharding: distribute data across servers by shard key; hash-based is most common; consistent hashing minimizes data movement on resharding
- CAP: C (strong consistency) + A (availability) + P (partition tolerance) — can guarantee only 2; P is required → choose CP or AP
- CP examples: HBase, ZooKeeper; AP examples: Cassandra, DynamoDB (default)
- Consistency levels: eventual < read-your-own-writes < monotonic read < sequential < linearizable
- Hot spot: shard that receives disproportionate traffic due to bad shard key choice

---

<a name="part-9"></a>
# Part 9 — Core System Design Building Blocks

## CDN (Content Delivery Network)

### The Problem

Snapgram serves photos and videos. Your origin server is in Mumbai. A user in London requests a photo. The bytes travel Mumbai → London — approximately 7,000 km. At the speed of light through fiber (~200,000 km/s), that's ~35ms one-way, ~70ms round trip. Add TCP handshake (~70ms) and TLS (~70ms) — you're at ~200ms before the first byte of the photo arrives. Then the actual photo data transfer. For a 500KB photo, at a typical connection speed, that's another 100ms+. A user in London waits 300–400ms for a single photo.

Multiply this across a page showing 20 photos. Users in London have a terrible experience.

### How a CDN Works

A CDN is a global network of **edge servers** (Points of Presence / PoPs) placed in data centers worldwide — London, Frankfurt, New York, Singapore, São Paulo, Mumbai. When a user in London requests a photo, instead of going to your Mumbai origin server, they go to the London or Frankfurt PoP — perhaps 10–20ms away.

**First request (cache miss at edge):**
```
London user → CDN PoP (London) → "Don't have this photo yet"
              → Fetches from origin (Mumbai) → 200ms
              → Stores copy at London PoP
              → Returns photo to user → 200ms (first time)
```

**Subsequent requests (cache hit at edge):**
```
London user → CDN PoP (London) → "Have it!" → Returns in ~15ms
```

Every subsequent London user gets the photo from the London PoP. Only the first London request pays the full origin round trip.

### What CDNs Cache

CDNs cache **static assets** — content that doesn't change per user or changes infrequently:
- Images and photos
- Videos
- JavaScript bundles
- CSS files
- Fonts
- HTML pages (for static sites)

CDNs do **not** typically cache dynamic, personalized API responses (your feed, your notifications). Those must always come from origin servers because they depend on who the user is.

Cache behavior is controlled by the **`Cache-Control`** HTTP response header:
```
Cache-Control: public, max-age=31536000  → Cache for 1 year (static assets with version in URL)
Cache-Control: private, max-age=0        → Don't cache (personalized API responses)
Cache-Control: public, max-age=300       → Cache for 5 minutes (semi-dynamic content)
```

### CDN Internals

Modern CDNs like Cloudflare, Akamai, and AWS CloudFront have thousands of PoPs. They use **anycast routing** — multiple CDN PoPs advertise the same IP address. Your DNS lookup resolves to the same IP regardless of location, but the internet's routing infrastructure (BGP) directs your request to the nearest PoP automatically.

CDNs also terminate TCP and TLS connections at the edge. This means the expensive handshake latency is paid to a nearby server (10ms RTT) rather than your distant origin (150ms RTT).

**CDN for video streaming** (YouTube, Netflix): Video is broken into small segments (2–10 seconds each). CDNs cache these segments. Popular videos are cached at many PoPs worldwide. Less popular content is cached at fewer PoPs or fetched from origin on demand.

**Interview signal:** When asked "how would you design YouTube/Netflix/Snapgram," mentioning CDN for media delivery with `Cache-Control` configuration and discussing cache miss behavior (and origin fetch patterns) demonstrates real understanding.

---

## API Gateway

### The Problem

You have 15 microservices. Every service needs authentication. Every service needs rate limiting. Every service needs logging and tracing. Every service needs SSL termination. If every service implements these independently, you have duplicated code, inconsistent behavior, and 15 different places to update when your auth logic changes.

### What an API Gateway Is

An **API Gateway** sits in front of all your microservices. It's the single entry point for all external requests. It handles:

- **Authentication:** Validate JWTs or API keys before requests reach services
- **Authorization:** Check permissions (is this user allowed to access this resource?)
- **Rate limiting:** Throttle abusive clients before they hit backend services
- **Request routing:** Route `/users/*` to the User Service, `/photos/*` to the Photo Service
- **SSL termination:** Handle TLS at the gateway; internal traffic uses plain HTTP
- **Request/Response transformation:** Add headers, strip headers, transform payloads
- **Circuit breaking:** Stop forwarding requests to unhealthy services
- **Logging and distributed tracing:** Generate request IDs, log all requests centrally

```
Mobile App ──────────────────> [ API Gateway ]
Web App   ──────────────────>      |
3rd Party API ───────────────>     |   Auth check
                                   |   Rate limit
                                   |   Route request
                                   |
                   ┌───────────────┼───────────────┐
                   v               v               v
            User Service    Photo Service   Feed Service
```

**Common API Gateway implementations:** AWS API Gateway (fully managed), Kong (open source, self-hosted), Nginx (can be configured as a gateway), Netflix Zuul, Spring Cloud Gateway.

**The BFF pattern (Backend for Frontend):** For complex products with multiple frontends (mobile app, web app, third-party API), instead of one gateway, you create separate gateways ("BFFs") per client type. The mobile BFF returns mobile-optimized responses; the web BFF returns web-optimized responses. Each BFF aggregates calls to multiple backend services and shapes the response for its client.

---

## Rate Limiter

### The Problem

Without rate limiting, a single malfunctioning client can send 10,000 requests per second to your API — overwhelming your servers and starving legitimate users. Malicious actors can use your API to enumerate user data, brute-force passwords, or conduct denial-of-service attacks. You need to throttle excessive requests.

### Common Rate Limiting Algorithms

**Token Bucket:**
Each user has a "bucket" with a maximum capacity of N tokens. Tokens are added at a fixed rate (e.g., 10 per second). Each request consumes one token. If the bucket is empty, the request is rejected (HTTP 429). This allows short bursts (up to bucket capacity) while enforcing an average rate.

```
Bucket: max 100 tokens, refill 10/second

User makes 50 requests quickly → 50 tokens consumed → bucket has 50 left → allowed
User makes 60 more quickly → 50 consumed, 10 rejected (bucket empty) → 429
Every second: 10 tokens added back
```

**Sliding Window Counter:**
Track request timestamps in a sliding time window (e.g., last 60 seconds). Count how many requests fall within the window. If count > limit, reject. More accurate than fixed windows (no "reset spike" at window boundaries).

**Fixed Window Counter:**
Divide time into fixed windows (0–60 seconds, 60–120 seconds). Count requests per window. Simple but has edge case: a user can make N requests in the last second of window 1 and N more in the first second of window 2 — 2N requests in 2 seconds.

### Implementing Rate Limiting with Redis

Redis Sorted Sets make a clean sliding window implementation:

```python
def is_rate_limited(user_id, limit=100, window_seconds=60):
    key = f"ratelimit:{user_id}"
    now = time.time()
    window_start = now - window_seconds
    
    pipe = redis.pipeline()
    # Remove requests older than the window
    pipe.zremrangebyscore(key, 0, window_start)
    # Count requests in window
    pipe.zcard(key)
    # Add current request
    pipe.zadd(key, {str(now): now})
    # Set TTL to clean up old keys
    pipe.expire(key, window_seconds)
    results = pipe.execute()
    
    request_count = results[1]
    return request_count >= limit  # True = rate limited
```

**Rate limiting at what granularity?**
- Per IP address: blocks anonymous attacks
- Per user/API key: prevents authenticated users from abusing
- Per endpoint: different limits for different operations (100 reads/min, 10 writes/min)
- Per service: total throughput limit for downstream service protection

---

## Object Storage (S3 / File Storage)

### The Problem

Users upload photos and videos to Snapgram. Where do these files live? Your database stores metadata (filename, uploader, timestamp) but not the binary file content — databases aren't designed for large binary blobs. Your application servers are stateless; you can't write to local disk (the file would only be on one server).

### What Object Storage Is

**Object storage** (S3, Google Cloud Storage, Azure Blob Storage) stores arbitrary binary data (objects) identified by a key. Think of it as an infinitely large key-value store for files.

Each object is stored with:
- A **key** (unique identifier, like a file path): `photos/user_42/photo_99.jpg`
- The **data** (the actual bytes)
- **Metadata** (content type, size, custom attributes)
- **Access control** (public, private, signed URL)

**Properties:**
- Effectively unlimited storage (petabytes, exabytes)
- Extremely durable: objects are replicated across multiple availability zones (typically 99.999999999% durability — "eleven nines")
- Globally accessible via HTTP
- Scales to millions of requests per second
- Cheap: ~$0.02–0.03 per GB/month vs. ~$0.10–0.20 per GB for SSDs

### Typical Flow: Photo Upload

```
1. Client requests upload URL: POST /photos/upload-url
   → Server generates a pre-signed S3 URL (time-limited, unique)
   → Returns URL to client

2. Client uploads directly to S3: PUT <presigned-url> (binary data)
   → Photo goes directly from client to S3 (bypasses your servers)
   → S3 returns success

3. Client confirms: POST /photos { "s3_key": "photos/user_42/photo_99.jpg" }
   → Server saves metadata to database
   → Server publishes "photo_uploaded" event to Kafka
   → Background workers: generate thumbnails, run moderation
```

Why upload directly to S3 instead of through your server? Your servers don't have to handle the bandwidth for large file uploads. S3 handles the upload natively and efficiently. Your servers are not blocked by slow uploads.

**Signed URLs:** S3 supports generating time-limited, cryptographically signed URLs for specific operations (PUT for upload, GET for download). These let clients interact directly with S3 without needing AWS credentials, and the URL expires after (say) 5 minutes so it can't be shared.

---

## Distributed Locking

### The Problem

You have multiple instances of your Photo Service. A user initiates a photo upload. You need to generate a unique photo ID. Two instances run simultaneously, both check "what's the next photo ID?", both get 1000, both create photo 1000. Duplicate IDs. Data corruption.

Or: you're implementing a rate limiter. You read the current count (99), increment it (100), write back 100. But another instance reads the count (99) simultaneously, also increments, also writes 100. You've allowed 2 requests but only incremented the counter by 1.

These are **race conditions** in distributed systems. Unlike single-process race conditions (fixed by a mutex), here multiple processes on different servers need to coordinate.

### Redis SETNX: Simple Distributed Lock

```python
def acquire_lock(redis, lock_key, ttl=10):
    # SET key value NX EX ttl
    # NX = only set if key doesn't exist (atomic)
    # EX ttl = expire in ttl seconds (auto-release if process dies)
    return redis.set(lock_key, "locked", nx=True, ex=ttl)

def release_lock(redis, lock_key):
    redis.delete(lock_key)

# Usage
if acquire_lock(redis, "lock:photo_id_gen"):
    try:
        # Critical section — only one process executes this
        photo_id = get_next_photo_id()
        create_photo(photo_id)
    finally:
        release_lock(redis, "lock:photo_id_gen")
else:
    # Another process has the lock — retry or fail
    raise LockNotAcquiredError()
```

The `NX` flag makes the SET atomic — either you get the lock or you don't. The TTL ensures the lock is released even if the process dies (preventing deadlock).

**The Fencing Token Problem:** A subtle issue: if Process A holds the lock, gets paused (GC pause, network delay), its TTL expires, Process B acquires the lock and starts working. Then Process A resumes — it still thinks it has the lock. Now both A and B are in the critical section simultaneously.

The solution is a **fencing token**: a monotonically increasing number given when the lock is acquired. Any resource that the lock protects checks the fencing token and rejects operations with outdated tokens.

**Redlock:** A more robust distributed locking algorithm by Redis's creator. Requires acquiring locks on N/2+1 Redis nodes (quorum) for the lock to be valid. Even if some Redis nodes fail, the lock remains valid. More complex but provides stronger guarantees in distributed environments.

---

## Feed Generation Systems

### The Problem

How does Snapgram generate each user's home feed? The feed shows the most recent posts from everyone you follow, ranked by recency (and potentially by relevance scores).

### Approach 1: Pull on Read (Fan-out on Read)

When a user opens their feed, query the database:

```sql
SELECT p.* FROM photos p
JOIN follows f ON p.user_id = f.following_id
WHERE f.follower_id = ?
ORDER BY p.created_at DESC
LIMIT 20;
```

**Pros:** Always fresh data. No precomputation needed.
**Cons:** At 100M users, this query joins the massive `follows` table with the massive `photos` table. For a user following 500 people, this might scan millions of rows. At 50,000 requests/second, your database dies.

### Approach 2: Push on Write (Fan-out on Write)

When a user uploads a photo, immediately write a feed entry for *every* follower:

```python
def photo_uploaded(user_id, photo_id):
    followers = db.get_followers(user_id)
    for follower_id in followers:
        feed_cache.lpush(f"feed:{follower_id}", photo_id)
        feed_cache.ltrim(f"feed:{follower_id}", 0, 999)  # Keep last 1000 entries
```

The feed for each user is precomputed and stored in Redis. Feed reads are a single Redis list read — O(1), extremely fast.

**Pros:** Feed reads are near-instant.
**Cons:** Writing is expensive. A celebrity with 10 million followers uploads a photo — you must write 10 million feed entries. That takes significant time and resources. This is the **celebrity problem** or **hot user problem**.

### Approach 3: Hybrid (Real Systems)

Use fan-out on write for regular users (say, < 10,000 followers). For celebrities (> 10,000 followers), use fan-out on read — but cache the celebrity's recent posts and merge them at read time.

```python
def get_user_feed(user_id):
    # 1. Get precomputed feed from Redis (fan-out-on-write users)
    feed_items = redis.lrange(f"feed:{user_id}", 0, 19)
    
    # 2. Get followed celebrities and merge their latest posts
    celebrities = get_followed_celebrities(user_id)
    celebrity_posts = [redis.lrange(f"posts:{c}", 0, 19) for c in celebrities]
    
    # 3. Merge and rank
    all_items = merge_and_sort(feed_items, celebrity_posts)
    return all_items[:20]
```

This is essentially the architecture used by Twitter (before they moved to more sophisticated ML ranking).

---

## Notification Systems

### The Problem

When someone likes your photo, you want to receive a notification on your phone. This must be delivered within seconds, reliably, across multiple channels (push notification, email, SMS, in-app).

### Architecture

```
Event Source (e.g., like created)
    |
    v
Kafka Topic: "notification_events"
    |
    v
Notification Service (consumer)
    |
    +──> Push Notification (FCM/APNs) ──> User's phone
    +──> Email Service (SendGrid) ──> User's inbox
    +──> In-app notification (WebSocket) ──> Active browser
    +──> SMS (Twilio) ──> User's phone (for critical alerts)
```

**Fan-out across channels:** Each user has preferences — notify via push only, or email + push, or in-app only. The notification service reads user preferences and routes accordingly.

**Push notification services:** Firebase Cloud Messaging (FCM) for Android, Apple Push Notification Service (APNs) for iOS. Your servers send a notification payload to FCM/APNs, which deliver it to the device. You don't maintain a persistent connection to every device — FCM/APNs do.

**Notification aggregation:** If you get 100 likes in one minute, you don't want 100 notifications. Aggregate: "Alice and 99 others liked your photo." This requires a short delay (collect notifications for 30 seconds, then send the aggregated version) and deduplication.

---

## Summary

- **CDN:** Edge servers near users cache static assets, eliminating origin round trips. Controlled by `Cache-Control` headers.
- **API Gateway:** Single entry point; handles auth, rate limiting, routing, TLS termination, logging for all microservices.
- **Rate Limiter:** Token bucket (allows bursts), sliding window (accurate), fixed window (simple). Implemented with Redis atomic operations. Granularity: per IP, per user, per endpoint.
- **Object Storage (S3):** Infinitely scalable binary file storage. Pre-signed URLs for direct client uploads. 11-nines durability.
- **Distributed Locking:** Redis SETNX + TTL for basic locks. Fencing tokens to prevent stale lock holders. Redlock for multi-node quorum.
- **Feed Generation:** Fan-out on write (precompute, fast reads, expensive for celebrities) vs. fan-out on read (always fresh, expensive query). Hybrid: fan-out on write for regular users, on-read merge for celebrities.
- **Notifications:** Event-driven via Kafka → Notification Service → per-channel delivery (FCM/APNs, Email, SMS, WebSocket). Aggregation prevents notification flooding.

## Common Interview Questions

- "How does a CDN work? When would you not use a CDN?"
- "What is an API gateway and what does it handle?"
- "How would you implement a rate limiter? Walk me through the algorithm."
- "How would you design a notification system that delivers to millions of users?"
- "Compare fan-out on write vs. fan-out on read for a social feed. Which would you use?"

## Common Mistakes

- Saying "put a CDN in front" without explaining what gets cached and what doesn't
- Not discussing the celebrity problem with fan-out-on-write
- Not discussing the aggregation/deduplication requirement for notifications
- Proposing in-memory distributed locks without discussing TTL or the fencing token problem

## Revision Notes

- CDN: PoPs worldwide → edge cache → origin on miss; controlled by Cache-Control header
- API Gateway: auth + rate limit + route + log; one entry point for all external traffic
- Rate limiter: token bucket allows bursts; sliding window is most accurate; Redis ZADD for implementation
- S3: key-value for binary blobs; pre-signed URLs for direct uploads; 11-nines durability
- Distributed lock: Redis SET NX EX; TTL prevents deadlock; fencing token prevents stale holders
- Feed: fan-out-on-write = precomputed (fast reads, expensive celebrity writes); hybrid = write for regular, read-merge for celebrities

---

<a name="part-10"></a>
# Part 10 — Capacity Estimation Masterclass

## Why Capacity Estimation Matters

In a system design interview, you'll be asked to design a system for "100 million users" or "500 million DAU." Without estimation, you can't make principled architectural decisions:

- Do we need one database server or fifty?
- Does our cache need 16GB RAM or 2TB?
- Do we need 10 servers or 1,000?
- Is our bandwidth cost $100/month or $100,000/month?

Interviewers don't expect exact answers. They want to see that you can **reason quantitatively** and **use numbers to drive design decisions**. A candidate who says "we'll add more servers if needed" without knowing whether that's 2 more or 2,000 more is not demonstrating senior-level thinking.

---

## Key Vocabulary

**DAU (Daily Active Users):** Number of unique users who use the app on a given day.

**MAU (Monthly Active Users):** Unique users in a month. Typically DAU ≈ MAU × 0.1 to 0.3 (depends on product retention).

**QPS (Queries Per Second):** How many requests hit the database or service per second. Note: "query" here is generic — one HTTP request may result in multiple DB queries.

**RPS (Requests Per Second):** HTTP requests hitting your API per second. One RPS from a user might generate 5 QPS to your database.

**Throughput:** Amount of data transferred per second (bytes/second, MB/s, GB/s).

**Latency:** Time for a single operation to complete (milliseconds).

**Bandwidth:** Maximum data transfer rate of a network connection (bits per second).

---

## The Core Mental Model: Back-of-Envelope Math

### Power of Ten Reference

| Quantity | Approximate value |
|----------|-----------------|
| 1 million | 10^6 |
| 1 billion | 10^9 |
| 1 trillion | 10^12 |
| 1 KB | 10^3 bytes |
| 1 MB | 10^6 bytes |
| 1 GB | 10^9 bytes |
| 1 TB | 10^12 bytes |
| 1 PB | 10^15 bytes |
| Seconds in a day | ~86,400 ≈ 10^5 |
| Seconds in a year | ~31,500,000 ≈ 3 × 10^7 |

### Converting DAU to QPS

```
Daily Active Users → Requests Per Day → Requests Per Second

Formula:
QPS = (DAU × requests_per_user_per_day) / seconds_in_day
    = (DAU × requests_per_user_per_day) / 86,400

Peak QPS ≈ Average QPS × 3 to 5
(traffic is bursty: peak hours can be 3-5× the daily average)
```

**Example: Snapgram with 10M DAU**
- Assume each user makes ~20 requests/day (open app 5 times, each session makes ~4 API calls)
- Average QPS = (10,000,000 × 20) / 86,400 = 200,000,000 / 86,400 ≈ **2,300 QPS**
- Peak QPS ≈ 2,300 × 3 ≈ **7,000 QPS**

---

## Worked Example 1: TinyURL

**Requirements:** 100M DAU, 1% create short URLs, 99% use short URLs. Average 1 read per URL per day.

### Read/Write QPS

```
New URLs created per day:
= 100M DAU × 1% = 1M new URLs/day
= 1,000,000 / 86,400 ≈ 12 write QPS
Peak write QPS ≈ 12 × 3 ≈ 36 write QPS

URL reads per day:
= 1M new URLs × 1 read each = 1M reads/day... but URLs accumulate over years
= Let's say 100M total URLs exist, each read 0.01 times per day = 1M reads/day
= 1,000,000 / 86,400 ≈ 12 read QPS
Peak read QPS ≈ 36 read QPS
```

Very modest QPS — a single MySQL server handles thousands of QPS. For TinyURL's scale, database sharding isn't needed; caching is the key optimization.

### Storage

```
Per URL stored:
- Short URL key: 7 characters = 7 bytes
- Long URL: avg 100 characters = 100 bytes
- Metadata (user_id, created_at, expiry): ~50 bytes
- Total per URL: ~160 bytes

5 years of new URLs:
= 12 write/sec × 86,400 sec/day × 365 days/year × 5 years
= 12 × 86,400 × 1,825
≈ 12 × 86,400 × 1,800  (rounding)
≈ 1.9 billion URLs

Storage = 1.9 billion × 160 bytes ≈ 300 GB
```

300 GB easily fits on a single database server. No sharding needed.

### Cache

```
Cache the 20% most popular URLs (80/20 rule: 20% of URLs → 80% of traffic)

Daily reads: 1M reads × 80% = 800K reads from top 20% of URLs
Top 20% of URLs = 0.2 × 1.9B = 380M URLs

But most traffic is concentrated in recently created URLs.
Cache the last day's URLs: 1M URLs × 160 bytes = 160MB
160MB fits comfortably in a small Redis instance.
```

### Bandwidth

```
Read bandwidth:
= 12 read QPS × 200 bytes (response) ≈ 2.4 KB/s — negligible
```

TinyURL is a trivially small system from a capacity perspective. The interesting challenge is URL generation (avoiding duplicates, ensuring uniqueness at scale).

---

## Worked Example 2: Instagram-like Photo App (100M DAU)

### Read/Write QPS

```
Photo uploads:
- 100M DAU, assume 5% upload a photo daily = 5M photos/day
- 5,000,000 / 86,400 ≈ 58 photo uploads/sec
- Peak: 58 × 3 ≈ 175 uploads/sec

Feed reads:
- 100M DAU, each opens feed 5 times/day = 500M feed loads/day
- 500,000,000 / 86,400 ≈ 5,800 feed QPS
- Peak: 5,800 × 3 ≈ 17,500 feed QPS
```

### Storage — Photos

```
Per photo:
- Original: 3MB average
- 3 thumbnails (small/medium/large): 200KB + 500KB + 1MB = 1.7MB
- Total per photo: ~4.7MB ≈ 5MB

Daily storage:
= 5M photos/day × 5MB = 25TB/day = 25,000 GB/day

Over 5 years:
= 25TB/day × 365 × 5 = 45,625 TB ≈ 46 PB

This requires object storage (S3) — no traditional file system handles this.
```

### Storage — Metadata

```
Per photo metadata row:
- photo_id: 8 bytes
- user_id: 8 bytes
- s3_key: 50 bytes
- caption: 300 bytes
- created_at: 8 bytes
- like_count, comment_count: 8 bytes each
- Total: ~400 bytes

5M photos/day × 400 bytes ≈ 2 GB of metadata/day
5 years: 2 GB/day × 365 × 5 ≈ 3.65 TB of metadata

Fits on a sharded MySQL cluster — no single server holds 3.65TB,
but distributed across 4-8 shards it's very manageable.
```

### Bandwidth — Incoming (Uploads)

```
175 uploads/sec × 3MB (original only) = 525 MB/sec incoming
= ~4.2 Gbps — significant! Requires multi-gigabit network uplink.
```

### Bandwidth — Outgoing (Reads)

```
17,500 feed QPS, each returning 20 photos (thumbnails only: 200KB each)
= 17,500 × 20 × 200KB = 17,500 × 4MB = 70 GB/sec ???

That can't be right. Why? Because CDN serves most photo reads.
CDN cache hit rate for popular photos: ~95%

CDN serves: 0.95 × 70 GB/sec = 66.5 GB/sec — CDN handles this.
Origin sees: 0.05 × 70 GB/sec = 3.5 GB/sec — manageable.
```

This illustrates why CDN is essential at scale — without it, origin bandwidth requirements would be impossible.

### Servers Needed

```
Each app server handles ~1,000 RPS (a reasonable estimate for I/O-bound Node.js)
Peak feed QPS: 17,500
Servers needed: 17,500 / 1,000 ≈ 18 servers

Plus overhead: add 50% → ~27 app servers
With auto-scaling: set min=10, max=30, auto-scale based on CPU
```

---

## Worked Example 3: WhatsApp (Messages at Scale)

### User Behavior

```
1 billion users, 65% DAU = 650M DAU
Average 40 messages sent per day per user
Average message size: 50 bytes (text) — photos are separate
```

### Message QPS

```
Messages sent per day = 650M × 40 = 26 billion messages/day
= 26,000,000,000 / 86,400 ≈ 300,000 messages/second
Peak: 300,000 × 3 ≈ 900,000 messages/second

This is 300K writes/second — far beyond a single database.
Cassandra (masterless, write-optimized) handles this naturally.
Shard by (sender_id, chat_id) to distribute load.
```

### Message Storage

```
Per message:
- message_id: 16 bytes (UUID)
- sender_id: 8 bytes
- chat_id: 8 bytes
- content: 50 bytes avg
- created_at: 8 bytes
- status (sent/delivered/read): 1 byte
- Total: ~91 bytes ≈ 100 bytes

Storage per day:
= 26 billion messages × 100 bytes = 2.6 TB/day

5 years: 2.6 TB/day × 365 × 5 ≈ 4.75 PB

WhatsApp doesn't permanently store messages server-side (end-to-end encrypted,
stored on device). Server stores messages only until delivered.
If average delivery time is 1 minute and messages are deleted after delivery:
Active storage = 26B/day / (24 × 60) minutes/day × 1 minute = 18M messages at any time
= 18M × 100 bytes = 1.8 GB — trivially small!
```

---

## Estimation Framework: Step by Step

For any system design question, follow this sequence:

```
1. Identify user behavior:
   - How many DAU?
   - What actions do users perform?
   - How many times per day per user?

2. Calculate average QPS:
   QPS = (DAU × actions/day) / 86,400

3. Calculate peak QPS:
   Peak QPS = Average QPS × 3 (conservative) to 5 (aggressive peak)

4. Separate read vs. write QPS:
   (Most systems: reads >> writes)

5. Calculate storage:
   Storage per day = writes/day × bytes/write
   Total storage = Storage per day × retention period

6. Calculate bandwidth:
   Incoming = write QPS × bytes per write
   Outgoing = read QPS × bytes per read
   (Then apply CDN cache hit rate for outgoing)

7. Derive infrastructure:
   Servers = Peak QPS / QPS per server
   Cache size = hot data fraction × storage size
   DB shards = Peak write QPS / DB write capacity
```

---

## Numbers to Memorize

| Benchmark | Approximate Value |
|-----------|------------------|
| MySQL reads/sec (with cache) | 10,000–50,000 QPS |
| MySQL writes/sec | 1,000–5,000 QPS |
| Redis ops/sec | 100,000–1,000,000 |
| Cassandra writes/sec/node | 10,000–50,000 |
| Single SSD throughput | 500 MB/s |
| 1 Gbps network | 125 MB/s |
| 10 Gbps network | 1.25 GB/s |
| App server (Node.js) RPS | 1,000–5,000 RPS |
| App server (Go/Java) RPS | 5,000–50,000 RPS |
| Average image size | 100 KB thumbnail, 3 MB original |
| Average tweet/message | 50–280 bytes |

These are rough industry estimates, not specifications. Present them as ballpark figures.

---

## Summary

- Capacity estimation converts user behavior into concrete infrastructure requirements
- Core formula: QPS = (DAU × actions/day) / 86,400; Peak QPS = Average × 3–5
- Separate read QPS, write QPS, bandwidth, and storage estimations
- Always account for CDN reducing origin bandwidth for media-heavy apps
- Use back-of-envelope math — round aggressively, state your assumptions
- Estimation drives design: if write QPS is 300K, you need Cassandra or DynamoDB, not MySQL

## Common Interview Questions

- "Given 100M DAU each sending 10 messages per day, how many messages per second does your system handle?"
- "How much storage does your system need for 5 years?"
- "How much bandwidth do you need to serve images?"
- "How many app servers do you need for this traffic?"

## Common Mistakes

- Forgetting to account for peak vs. average traffic (peak can be 3–5× average)
- Not separating read QPS from write QPS — they have very different infrastructure implications
- Forgetting the CDN when estimating bandwidth for media content
- Being paralyzed by uncertainty — make reasonable assumptions, state them, and proceed

## Revision Notes

- Seconds in a day: ~86,400 ≈ 10^5
- Average QPS = (DAU × actions) / 86,400; peak = average × 3–5
- Storage: writes/day × bytes/write × days retained
- Bandwidth: QPS × bytes/response (then apply CDN hit rate for outgoing media)
- MySQL: ~10K-50K reads/sec; Cassandra: ~10K-50K writes/sec/node; Redis: 100K-1M ops/sec

---

<a name="part-11"></a>
# Part 11 — The Universal HLD Interview Framework

## How System Design Interviews Work

A system design interview is typically 45–60 minutes. You'll be asked to design a well-known system: "Design Twitter," "Design Uber," "Design Netflix." The interviewer wants to see how you *think*, not just what you know.

The worst thing you can do: start drawing boxes immediately. You'll design the wrong system. You'll miss constraints. You'll make assumptions that change everything.

The best approach: follow a structured framework. Every single time.

Here's the framework. Internalize it. Practice it. Use it for every system you design.

---

## The 11-Step Framework

### Step 1: Clarify Requirements (3–5 minutes)

Before writing a single word, ask clarifying questions. You're trying to understand the **scope** of what you're building. Different requirements lead to completely different architectures.

**Questions to ask:**
- "Who are the users of this system?" (consumers, businesses, developers?)
- "What are the core features we need to support?" (be specific)
- "What is the scale?" (DAU, number of posts, etc.)
- "Are there any specific performance requirements?" (latency SLAs?)
- "Should I focus on a specific part of the system or the whole thing?"

**Example for "Design Twitter":**
> "Before I start, let me clarify requirements. Are we focusing on the feed/timeline, the posting mechanism, or search? For scale — are we talking about current Twitter scale (~400M MAU, ~350M DAU-ish) or should I assume a specific number? Should I include features like video, ads, and spaces, or focus on the core tweet/follow/feed functionality?"

The interviewer's answers shape everything.

### Step 2: Functional Requirements

List the specific features you'll design. Keep it to the core — don't overscope.

**Example: Twitter**
- Users can post tweets (up to 280 characters)
- Users can follow other users
- Users can see a chronological (or ranked) timeline of tweets from followed users
- Users can like and retweet
- Out of scope: DMs, ads, analytics, spaces

### Step 3: Non-Functional Requirements

These are quality attributes — how the system should behave, not what it should do.

Always cover:
- **Availability:** "The system should be highly available — 99.99% uptime"
- **Latency:** "Feed loads should complete in < 200ms P99"
- **Consistency:** "What consistency do we need? Eventual is acceptable for feeds; posting a tweet should be immediately visible to the poster (read-your-own-writes)"
- **Durability:** "No tweet should ever be lost once confirmed"
- **Scale:** "Should handle N DAU, peak X QPS"
- **Fault tolerance:** "System should continue operating if one data center goes down"

Prioritizing these non-functional requirements drives key decisions. Strong consistency requirement → PostgreSQL with synchronous replication. Eventual consistency acceptable → Cassandra. Low latency → aggressive caching. High availability → multi-region.

### Step 4: Capacity Estimation

Do a quick back-of-envelope calculation (2–3 minutes). Don't over-engineer this.

- **QPS:** Write QPS and read QPS
- **Storage:** Per year or over 5 years
- **Bandwidth:** Incoming and outgoing

Estimation should **directly inform decisions**: "300K write QPS means we can't use MySQL alone — we need Cassandra or a sharded setup."

### Step 5: APIs

Define the key API endpoints. This forces clarity about what the system does.

Use REST conventions:

```
POST   /tweets              { content: string }     → 201 Created
GET    /tweets/:id          →  tweet object
DELETE /tweets/:id          → 204 No Content
GET    /users/:id/timeline  ?cursor=...  → [tweet]
POST   /follows             { target_user_id }
DELETE /follows/:id
GET    /users/:id/followers ?cursor=...  → [user]
```

Mention pagination strategy: cursor-based (using last seen ID) for feeds, not offset-based (which is slow at scale because OFFSET N requires scanning N rows).

### Step 6: Data Model

Design the schema. Show tables, fields, and indexes.

```
Users:
  user_id        BIGINT PRIMARY KEY
  username       VARCHAR(50) UNIQUE
  email          VARCHAR(255) UNIQUE
  created_at     TIMESTAMP
  INDEX (username)

Tweets:
  tweet_id       BIGINT PRIMARY KEY
  user_id        BIGINT (FK → users.user_id)
  content        VARCHAR(280)
  created_at     TIMESTAMP
  INDEX (user_id, created_at DESC)

Follows:
  follower_id    BIGINT
  following_id   BIGINT
  created_at     TIMESTAMP
  PRIMARY KEY (follower_id, following_id)
  INDEX (following_id)
```

Point out tradeoffs: "For tweets, I'm using a relational DB for user and tweet data, but I'd consider Cassandra for the timeline/feed data since it's append-only and read by user_id in time order — that fits Cassandra's data model perfectly."

### Step 7: High-Level Design

Now draw the architecture. Start with the simplest design that works, then evolve it.

```
Client → CDN → Load Balancer → API Gateway → Services → Databases
```

Walk through the main flows:

1. **Write path:** User posts a tweet → Tweet Service → Write to DB → Publish to Kafka → Feed workers fan-out to followers' timelines
2. **Read path:** User opens timeline → Check Redis cache → Cache hit: return → Cache miss: query DB, cache, return

Use a diagram. Label the components. Explain why each piece is there.

### Step 8: Deep Dives

After the high-level design, the interviewer will ask about specific components. Prepare to go deep on:

- The **feed generation algorithm** (fan-out on write vs. read, celebrity problem)
- The **database choices** and why
- The **caching strategy** (what's cached, TTL, invalidation)
- The **Kafka** setup (topics, partitions, consumer groups)
- **Failure scenarios** (what happens if the DB goes down? if Redis goes down? if Kafka falls behind?)

**Pro tip:** Don't wait for the interviewer to ask. Proactively say: "Let me deep dive into the feed generation because it's the most interesting part of this system."

### Step 9: Bottlenecks and Scaling

Identify where the system will struggle first and how you'd address it.

- "At our current scale, the timeline DB read is the bottleneck. Redis caching addresses this."
- "At 10× scale, the feed fan-out for celebrities is the bottleneck. The hybrid fan-out approach addresses this."
- "At 100× scale, the DB write layer for tweets becomes a bottleneck. We'd shard by user_id."

### Step 10: Fault Tolerance

How does the system behave when components fail?

- **Database failure:** Read replicas continue serving reads. Writes fail until primary recovers or replica is promoted. Automated failover via Patroni/MHA.
- **Redis failure:** Fall through to database. Slower but correct. Redis HA (Sentinel) prevents this.
- **Kafka falling behind:** Backpressure: throttle producers or add more consumers. DLQ for poison pills.
- **Data center outage:** Multi-region replication. DNS failover. Active-passive or active-active depending on consistency requirements.

### Step 11: Monitoring and Tradeoffs

**Monitoring:** What metrics matter?
- Latency (P50, P95, P99 per endpoint)
- Error rate (4xx, 5xx)
- Queue lag (how far behind are Kafka consumers?)
- Cache hit rate
- Database connection pool utilization
- Disk usage on databases

**Tradeoffs summary:** Every design decision has a tradeoff. Be explicit:
- "I chose fan-out on write for the feed — this means writes are more expensive and celebrities are a special case, but reads are fast and feed latency is excellent."
- "I chose eventual consistency for the timeline — users might see a tweet from a celebrity a few seconds late. This is acceptable for a social feed; I would use strong consistency for the tweet creation acknowledgment itself."

---

## How to Communicate During the Interview

### Opening the Interview

> "Thanks for the question. Before I start designing, I want to make sure I understand the requirements. Let me ask a few clarifying questions..."

**Spend 3–5 minutes here. This is not wasted time — it's the most important time.**

### Signaling Your Thinking

Don't just draw boxes silently. Narrate:

> "I'm going to start with a simple design that handles the basic requirements, then we can identify bottlenecks and evolve it."

> "For the timeline, I'm thinking about two approaches: fan-out on write and fan-out on read. Let me walk through the tradeoffs..."

> "This is a write-heavy workload at 300K writes/sec, which means MySQL alone won't cut it. I'm going to propose Cassandra here because..."

### When You Don't Know Something

Be honest, then reason from first principles:

> "I haven't worked with Kafka in this specific configuration before, but thinking about the requirements — we need multiple consumer groups reading independently and we need event replay — a message queue like RabbitMQ wouldn't give us replay, so Kafka's log-based model is the right fit."

### The "Magic Word" Formula

When making every architectural decision, follow this pattern:

> "I'm choosing [technology/approach] because [specific problem it solves] and the tradeoff is [what you give up]."

Never just say "I'd use Redis here." Always say why.

---

## A Sample Interaction Pattern

**Interviewer:** "Design Twitter's tweet and timeline system."

**You:** "Great question. Before I start, let me make sure I understand what we're building. Are we focusing on the core timeline/feed experience, or should we include search, DMs, and trending as well? And for scale — should I design for Twitter's current scale of ~300-400 million monthly active users, or a specific number?"

**Interviewer:** "Focus on core tweet posting and home timeline. Assume 100M DAU."

**You:** "Perfect. Let me define the functional requirements I'll address: posting tweets, following users, and reading a home timeline showing tweets from followed users in reverse chronological order. Out of scope: search, DMs, ads. Is that right?"

**Interviewer:** "Yes."

**You:** "For non-functional requirements, I'll assume: high availability (99.99%), eventual consistency is acceptable for the timeline (a tweet can take a few seconds to propagate), strong consistency for the tweet creation acknowledgment (user must see their own tweet immediately), and P99 timeline load under 200ms. 

Let me do a quick capacity estimate: 100M DAU, ~20 tweets/day per DAU would be a lot. Let's say 5% of users tweet, each posts 2 tweets/day = 10M tweets/day = ~115 write QPS, peak ~350 QPS. Timeline reads: each user reads 5 times/day = 500M reads/day = ~5,800 read QPS, peak ~17,500. This is very read-heavy.

For the APIs: POST /tweets, GET /users/:id/timeline (cursor-paginated), POST /follows, GET /users/:id/followers..."

[Continue through schema design → high-level architecture → deep dives]

---

## Summary

The framework:
1. **Clarify** — ask questions before designing anything
2. **Functional requirements** — what the system does
3. **Non-functional requirements** — how it behaves (availability, latency, consistency)
4. **Capacity estimation** — QPS, storage, bandwidth → drives technology choices
5. **APIs** — defines the interface
6. **Data model** — schema, indexes, DB choice rationale
7. **High-level design** — components, write path, read path
8. **Deep dives** — go deep on interesting components
9. **Bottlenecks** — what breaks first, how to scale
10. **Fault tolerance** — what happens when things fail
11. **Monitoring + tradeoffs** — explicit tradeoff discussion

Every decision: "I chose X because [problem Y], tradeoff is [Z]."

---

<a name="part-12"></a>
# Part 12 — Complete Mock Interviews: 15 Real Systems

---

## Mock Interview 1: TinyURL (URL Shortener)

### Requirements Clarification

**Functional:**
- Given a long URL, generate a short URL (e.g., `tiny.url/abc1234`)
- Redirect users who access the short URL to the original long URL
- Optional: custom short URLs, expiry dates, click analytics

**Non-Functional:**
- High availability (redirection must be extremely reliable — even if creation is down)
- Low latency (<10ms for redirect)
- Eventual consistency acceptable
- 1 billion URLs stored, 100:1 read/write ratio

**Out of scope:** User management, analytics dashboard

### Capacity Estimation

```
Assumptions: 10M new URLs/day, 1B total URLs after ~3 years

Write QPS: 10M / 86,400 ≈ 115 QPS; peak ~350 QPS
Read QPS: 115 × 100 = 11,500 QPS; peak ~35,000 QPS

Storage per URL: ~500 bytes (URL + metadata)
Total: 1B × 500 bytes = 500 GB — fits on a single server with SSD,
or a small sharded cluster.

Cache: Top 20% of URLs → 200M URLs × 500 bytes = 100 GB Redis cache
```

### APIs

```
POST /urls                    { long_url, custom_alias?, expiry? }  → { short_url }
GET  /:short_code             → 301 Redirect to long URL
DELETE /urls/:short_code      → 204 No Content
```

### Data Model

```sql
CREATE TABLE urls (
    short_code   CHAR(7)    PRIMARY KEY,  -- 7-char base62 encoded
    long_url     TEXT       NOT NULL,
    user_id      BIGINT,
    created_at   TIMESTAMP  DEFAULT NOW(),
    expires_at   TIMESTAMP,
    click_count  BIGINT     DEFAULT 0,
    INDEX (user_id),
    INDEX (expires_at)
);
```

### Core Design Challenge: Generating Unique Short Codes

The most interesting problem in TinyURL is generating unique 7-character short codes (base62 = 62^7 ≈ 3.5 trillion possible codes — enough for decades).

**Option 1: MD5/Hash the long URL**
Take MD5 of long URL, take first 7 characters. Problem: collisions (different URLs can produce the same 7-char prefix). Also, same URL maps to same short code — intentional? What if the user wants two different short URLs for the same long URL?

**Option 2: Auto-increment ID → Base62 encode**
Use a database auto-increment ID: 1, 2, 3, ... encode as base62: 1→"0000001", 100→"0000001z", etc.

Problem: single DB is a bottleneck for ID generation. At 350 write QPS this is fine. At 100K QPS, the ID generator becomes a bottleneck.

**Option 3: Pre-generate IDs (ID pool)**
A background process pre-generates millions of unique IDs and stores them in a "key DB." URL creation service picks an unused ID from the pool. Needs atomic "claim" operation (Redis RPOP from a list of pre-generated IDs).

**Option 4: Range-based ID allocation**
Multiple URL creation servers. Each server claims a range of IDs (1–1M, 1M–2M, etc.) from a central coordinator. Within their range, servers generate IDs locally without coordination. Highly scalable.

### High-Level Architecture

```
Client → CDN → Load Balancer
                    |
              ┌─────┴──────┐
              v            v
        [URL Create]  [URL Redirect]   ← separate services!
              |            |
        [MySQL Write]   [Redis Cache]
                            |
                        [MySQL Read Replicas]
```

**Why separate creation and redirect services?** Redirect is extremely latency-sensitive (users are waiting for a web page to load). Creation is not. You can scale them independently and ensure redirect SLAs are met even if creation is under load.

**Redirect flow:**
1. User hits `tiny.url/abc1234`
2. CDN checks if cached (short-lived cache, since URLs can expire): usually not cached
3. Redirect service checks Redis: `GET url:abc1234`
4. Cache hit → 301 Redirect (< 5ms)
5. Cache miss → MySQL query → 301 Redirect + cache the result

**Why 301 vs 302 redirect?**
- 301 (Permanent): Browser caches the redirect forever. Next time user visits `tiny.url/abc1234`, browser goes directly to the long URL without hitting your server. Saves bandwidth. But: click count tracking doesn't work, and if you change the long URL, browsers will still use the cached one.
- 302 (Temporary): Browser always asks your server. You can track clicks, change the destination. Higher server load.

For analytics tracking → 302. For maximum performance → 301.

### Failure Handling

- **Redis down:** Fall through to MySQL replicas. Latency degrades from 5ms to ~20ms. Acceptable.
- **MySQL primary down:** Reads continue from replicas. Writes (URL creation) fail. Cache continues serving existing URLs.
- **What if we want to support expiration?** Background job scans MySQL for expired URLs (WHERE expires_at < NOW()), deletes them, and calls `redis.delete("url:short_code")`.

### Interviewer Follow-up Questions

**Q: How do you handle custom aliases (user wants tiny.url/my-brand)?**
Same URL table, just store the user-provided code. Validate uniqueness at write time with a unique index. Rate limit to prevent namespace squatting.

**Q: How do you prevent abuse (malicious URLs)?**
Integrate with a URL reputation service (Google Safe Browsing API) at creation time. Flag or block URLs on known malware/phishing lists.

**Q: How would you scale to 10× traffic (350K read QPS)?**
Increase Redis cluster size. Add more redirect service instances. MySQL read replicas scale reads. 350K QPS from Redis is trivial (Redis handles millions of ops/sec on a single node; a cluster handles much more).

---

## Mock Interview 2: WhatsApp Chat Application

### Requirements Clarification

**Functional:**
- 1:1 messaging (real-time text messages)
- Group messaging (up to 256 members)
- Message delivery status: sent ✓, delivered ✓✓, read ✓✓ (blue)
- Media sharing (images, documents) — optional scope
- Online/offline/last-seen status

**Non-Functional:**
- Messages delivered within 100ms for online users
- Messages must never be lost
- End-to-end encryption (don't need to design E2E crypto — just note it's handled client-side)
- 1 billion users, 65% DAU, ~40 messages/user/day

### Capacity Estimation

```
DAU: 650M
Messages/day: 650M × 40 = 26 billion
Write QPS: 26B / 86,400 ≈ 300,000 QPS
Peak: ~1M QPS

Per message: ~100 bytes
Messages stored (server-side, until delivered): 
  If avg delivery time = 30 seconds, messages in flight = 300K × 30s = 9M messages
  = 9M × 100 bytes = 900 MB — trivially small

Persistent storage (7-day retention): 26B × 100 bytes × 7 = 18.2 TB/week
```

### Data Model

```
Messages Table (Cassandra):
  chat_id        UUID           PARTITION KEY
  message_id     TIMEUUID       CLUSTERING KEY (time-ordered)
  sender_id      UUID
  content        TEXT
  message_type   ENUM(text/image/doc)
  created_at     TIMESTAMP
  status         ENUM(sent/delivered/read)

Users Table (PostgreSQL):
  user_id        UUID           PRIMARY KEY
  phone_number   VARCHAR(20)    UNIQUE
  username       VARCHAR(50)
  last_seen      TIMESTAMP
  status         ENUM(online/offline)
```

**Why Cassandra for messages?** Append-only writes at 300K QPS, queried by chat_id in time order — perfect Cassandra partition key + clustering key pattern. Masterless handles massive write throughput without a single-write-bottleneck.

**Why PostgreSQL for users?** Low write volume, relational (users ↔ contacts ↔ groups), ACID important for phone number uniqueness.

### Architecture: The Real-Time Messaging Challenge

```
[User A Phone]                              [User B Phone]
     |                                           |
     | WebSocket                                 | WebSocket
     v                                           v
[Message Server 1]                      [Message Server 2]
         |                                       |
         | A sends msg to B                      |
         v                                       |
  [Kafka: chat_messages topic]                   |
         |                                       |
         v                                       |
  [Message Delivery Service]                     |
         |                                       |
         +--- B is connected to Server 2 ------->|
         |    publish to Redis Pub/Sub:           | receives message
         |    channel "user:B:messages"           | → push to B via WebSocket
         |                                       |
         +--- Write to Cassandra (persist)       |
```

**How does Server 1 know B is on Server 2?**
- All servers subscribe to Redis Pub/Sub for channels corresponding to their connected users
- Server 1 publishes to `user:B:messages` channel
- Server 2 is subscribed (because B is connected to it) → receives and delivers
- User-to-server mapping stored in Redis: `user:B → server_2_id`

**Offline delivery:**
- If B is offline, message is stored in Cassandra
- When B comes online and connects, server sends all pending undelivered messages from Cassandra
- B's app sends delivery receipts for each

**Delivery receipts:**
- When B's device receives a message → server updates message status to `delivered`
- When B opens the chat → app sends `read` receipt → server updates to `read`
- A's app polls (or receives push update) to update the ✓✓ status

### Group Messaging

Group messages are sent to a group. The server fans out to all group members.

```
A sends to Group(256 members):
  1. Store in Cassandra (partition key: group_id)
  2. Read all 256 member IDs from group_members table
  3. For each online member: deliver via their WebSocket connection server
  4. For each offline member: message waits in Cassandra

Fan-out: 256 members × 300K messages/sec = up to 76.8M delivery operations/sec
→ Must be async (via Kafka workers), not synchronous
```

### Failure Handling

- **Message Server crashes:** Client reconnects to another server via load balancer. Pending messages re-delivered from Cassandra.
- **Cassandra node failure:** Replication factor 3; with QUORUM consistency, one node failure doesn't affect availability.
- **Network partition:** Message may be acknowledged (stored in Cassandra on server's side) but client doesn't receive the ACK. Client retries with idempotency key (message UUID). Server deduplicates.

### Interviewer Follow-ups

**Q: How do you implement end-to-end encryption?**
This is handled entirely client-side (Signal Protocol). The server stores and forwards encrypted blobs — it never has the decryption keys.

**Q: How do you handle message ordering?**
Cassandra TIMEUUID clustering key ensures messages within a chat are ordered by time. For group messages, use vector clocks or Lamport timestamps if strict ordering across senders is required.

**Q: How do you show last-seen / online status?**
- When a user connects: set `user:user_id:status = online` in Redis with TTL 60s
- Client sends a heartbeat every 30s; server updates TTL
- When connection drops (TTL expires or explicit disconnect): set offline, update `last_seen` timestamp in PostgreSQL
- Other users query Redis for online status; Redis falls back to PostgreSQL for last_seen

---

## Mock Interview 3: Instagram (Photo Sharing)

### Requirements

**Functional:**
- Upload photos with captions
- Follow other users
- View home timeline (recent photos from followed users)
- Like and comment on photos

**Non-Functional:**
- 100M DAU, 5% upload photos (5M photos/day)
- Feed latency < 200ms
- Photo availability: 99.99%
- Eventual consistency for timeline acceptable

### Key Systems

**Photo Upload Flow:**
```
Client → POST /photos/upload-url → server generates pre-signed S3 URL
Client → PUT <presigned_url> (upload directly to S3)
Client → POST /photos (confirm: { s3_key, caption })
Server → Write to PostgreSQL (metadata)
Server → Publish to Kafka: "photo_uploaded"
Workers → Generate thumbnails (store back to S3)
Workers → Fan-out to followers' feeds (Redis lists)
Workers → Update search index (Elasticsearch)
```

**Feed Generation (Hybrid Fan-out):**
- Regular users (<10K followers): fan-out on write → write photo_id to every follower's Redis feed list
- Celebrity users (>10K followers): fan-out on read → follower's feed merges precomputed feed + celebrity's recent posts at read time
- Redis feed list: sorted list of photo IDs; load photo metadata from Redis/DB at read time

**Database Sharding:**
- `users` table: shard by `user_id` (consistent hashing, 4 MySQL shards initially)
- `photos` table: shard by `user_id` (photo owner — queries for a user's profile page hit one shard)
- `follows` table: shard by `follower_id` (for "who does user X follow?") AND denormalize index by `following_id` (for "who follows user X?")
- `likes` table: Cassandra (high write volume, append-only, partition key: photo_id)

**Why denormalize follows?** A follow has two owners: the follower and the following. "Get all users I follow" → query by follower_id. "Get all my followers" → query by following_id. These are on different shards. Solution: store two rows per follow (one indexed by follower_id, one by following_id). Trades storage for query performance.

---

## Mock Interview 4: Netflix (Video Streaming)

### Requirements

**Functional:**
- Browse and search catalog (movies, TV shows)
- Stream video with adaptive bitrate (adjust quality based on connection)
- Continue watching from where you left off
- Recommendations

**Non-Functional:**
- 200M subscribers, 70M DAU
- Video streaming must be extremely low latency and highly reliable
- Global distribution
- 4K video for premium subscribers

### The Video Storage and Delivery Architecture

This is the core challenge. Netflix's content library is many petabytes of video.

**Transcoding:**
When content is ingested, it's transcoded into dozens of formats:
- Multiple resolutions: 480p, 720p, 1080p, 4K
- Multiple codecs: H.264, H.265 (HEVC), AV1
- Multiple bitrates: 500Kbps (mobile) to 25Mbps (4K HDR)

Each 2-hour movie transcoded into ~20 formats = ~20× the original size. This is done once by a transcoding pipeline (massive parallel Spark/Flink jobs on AWS). Result: hundreds of GB per title.

**Adaptive Bitrate Streaming (ABR):**
Video is split into small segments (2–4 seconds each). The player downloads a manifest file (HLS/DASH) listing available bitrates. As the user watches, the player monitors bandwidth and automatically switches between quality levels — higher quality when bandwidth is good, lower when bandwidth is poor.

```
Manifest file (DASH/HLS):
- /video/123/1080p/segment_001.mp4
- /video/123/1080p/segment_002.mp4
...
- /video/123/480p/segment_001.mp4
...

Player downloads segments slightly ahead (buffer of ~30 seconds)
If bandwidth drops: switch to lower quality segments
If bandwidth improves: switch to higher quality
```

**CDN for video:**
Video segments are served from CDN. Netflix uses their own CDN (Open Connect) — boxes co-located at ISPs worldwide. Popular movies are pre-positioned on every CDN box globally. When you click "play," your player gets the manifest and downloads segments from a CDN node likely within your ISP.

```
User plays movie → Player fetches manifest from Netflix API
→ Player fetches segment_001 from nearest CDN box (Open Connect)
→ CDN box has it (pre-positioned) → delivers in < 50ms
```

**Playback position / Continue Watching:**
```
As user watches: every 30 seconds, app saves position:
  POST /playback { user_id, title_id, position_seconds, device }

Stored in Cassandra: partition key = (user_id), clustering key = (last_watched_at)
  → "get all recently watched titles for user X, sorted by recency"

On resume: query Cassandra for last position → seek to that point in video player
```

**Recommendations:**
Netflix's recommendation system is complex ML — involving collaborative filtering, content-based filtering, viewing history analysis. At a high level:
- Events (views, ratings, completions) stream to Kafka
- Hadoop/Spark batch jobs compute recommendations daily (offline model training)
- Precomputed recommendations stored in Cassandra: `user_id → [title_id1, title_id2, ...]`
- Real-time ranking adjusts based on current context (time of day, device)

---

## Mock Interview 5: Uber (Ride-Sharing)

### Requirements

**Functional:**
- Rider requests a ride
- Nearby drivers matched to rider
- Real-time driver location tracking
- Trip tracking (start, in-progress, complete)
- Pricing (surge pricing)

**Non-Functional:**
- Low matching latency (<2 seconds to find a driver)
- Driver location updated every 4 seconds
- High availability for matching service
- Geospatial queries at scale (find drivers within 5km radius)

### The Core Problem: Real-Time Location at Scale

Uber has ~5 million daily drivers. Each sends their GPS location every 4 seconds.

```
Location updates/sec = 5M drivers / 4 = 1.25M location updates/sec
```

This is a write-intensive time-series workload. Normal databases can't handle 1.25M location writes/sec.

**Storage:**
- **Cassandra** for historical location data (partition key: driver_id, clustering key: timestamp)
- **Redis Sorted Sets** for current driver locations (for real-time matching query)

**Real-time matching using geospatial indexing:**

How do you find "all drivers within 5km of rider's location"? You can't do `WHERE distance(lat, long, driver_lat, driver_long) < 5000` in SQL efficiently — it's O(n) scan.

**Geohash** is the solution. Geohash divides the Earth's surface into a grid of cells, encoding each cell as a string. Nearby locations share common prefixes. By querying all geohash cells within a radius, you find nearby drivers efficiently.

```
Driver locations stored in Redis as Sorted Set:
  Key: geohash_cell_prefix (e.g., "s2f7" = ~5km cell in Mumbai)
  Members: driver IDs
  Score: timestamp (for cleanup of stale locations)

Rider requests ride at (19.0760°N, 72.8777°E):
1. Calculate geohash of rider's location: "s2f7..."
2. Query all driver IDs in surrounding geohash cells (current cell + 8 neighbors)
3. Filter by availability status
4. Calculate actual distance for filtered drivers
5. Assign nearest available driver
```

Redis's built-in `GEOAGG` command actually handles this natively.

### Matching Service

```
Rider requests → Matching Service
  1. Get rider's geohash
  2. Query Redis for drivers in surrounding cells → [driver_1, driver_3, driver_7, ...]
  3. Filter: driver.status == "available"
  4. Calculate ETA for each (using routing service or haversine distance estimate)
  5. Rank by ETA
  6. Send offer to top driver → driver accepts/rejects → confirm to rider
  7. Update driver status = "on_trip" in Redis
```

**Trip state machine:**
```
States: 
  REQUESTED → ACCEPTED → DRIVER_ARRIVED → IN_PROGRESS → COMPLETED | CANCELLED

Transitions triggered by:
  REQUESTED: rider submits request
  ACCEPTED: driver accepts match offer
  DRIVER_ARRIVED: driver presses "Arrived" button
  IN_PROGRESS: driver presses "Start Trip"
  COMPLETED/CANCELLED: driver/rider action
```

Stored in PostgreSQL (low write volume, needs ACID for payment processing at completion).

### Surge Pricing

Surge pricing is a demand/supply balance calculation:

```
Surge multiplier = f(demand / supply) per geohash cell

Demand = requests in last 5 minutes in cell
Supply = available drivers in cell

Computed in real-time using Kafka streams:
  Kafka receives all ride requests and driver location updates
  → Flink/Spark streaming job aggregates demand and supply per geohash cell
  → Publishes surge multiplier to Redis: "surge:s2f7 = 1.8x"
  → Pricing service reads surge from Redis at fare calculation time
```

---

## Mock Interview 6: BookMyShow (Ticket Booking System)

### Requirements

**Functional:**
- Browse movies and shows
- View available showtimes and seats
- Select and book seats
- Payment processing
- Booking confirmation

**Non-Functional:**
- No double booking (critical — must not oversell)
- High concurrency for popular shows (IPL final, Coldplay concert)
- Eventual consistency OK for browsing; strong consistency for booking
- 10M DAU, 500K bookings/day

### The Core Problem: Concurrent Seat Selection

This is the most interesting system design challenge. When a blockbuster releases, thousands of people simultaneously try to book the same seat. Without proper concurrency control, the same seat gets booked by multiple people.

**Naive approach (broken):**
```
1. User A and User B both query: "is seat 12C available?" → both see "available"
2. Both proceed to book
3. Both insert into bookings table
4. Both booked seat 12C. Double booking!
```

**Database locking approach:**
```sql
BEGIN TRANSACTION;
SELECT * FROM seats WHERE show_id = 1 AND seat_id = '12C' FOR UPDATE;  -- row lock
-- If seat is available:
UPDATE seats SET status = 'RESERVED', user_id = 42 WHERE seat_id = '12C';
INSERT INTO bookings (user_id, seat_id, show_id) VALUES (42, '12C', 1);
COMMIT;
```

`SELECT FOR UPDATE` places an exclusive row lock. User A gets the lock; User B blocks until A commits or rolls back. If A books successfully, B sees `status = 'RESERVED'` and fails gracefully.

**Problem:** At high concurrency (thousands competing for same seat), lock contention causes massive queuing and timeouts.

**Better approach: Optimistic locking + reservation timeout**

1. Show seat availability from Redis (cached)
2. User selects seat → "hold" in Redis with 10-minute TTL
3. During hold: seat shows as "pending" — no other user can select it
4. User completes payment within 10 minutes → write to DB (confirm booking)
5. If user abandons: Redis TTL expires → seat becomes available again

```python
def hold_seat(show_id, seat_id, user_id, ttl=600):
    key = f"seat_hold:{show_id}:{seat_id}"
    # Atomic: only set if key doesn't exist (another user didn't hold it)
    success = redis.set(key, user_id, nx=True, ex=ttl)
    if success:
        return {"status": "held", "expires_in": ttl}
    else:
        holder = redis.get(key)
        return {"status": "unavailable", "held_by": holder}
```

This is essentially a distributed lock with a natural TTL for seat holds.

**Why is this better?** Only the person who got the seat hold (the Redis SET NX succeeded) goes through to payment. Everyone else gets immediate "unavailable" without database contention.

### Architecture for High-Concurrency Show Sales

```
Popular show goes on sale (thousands of concurrent users):

[Users] → [API Gateway with Rate Limiter]
                |
                v
         [Virtual Queue]  ← Users are queued
                |
        [Ticket Service] — processes users one by one (FIFO from queue)
                |
        [Redis: seat map + holds]
                |
        [MySQL: confirmed bookings, payments]
```

The **virtual queue** prevents stampede: instead of thousands of users simultaneously hitting the booking service, they're placed in a fair queue and served in order. This is how Ticketmaster's "waiting room" works.

---

## Mock Interview 7: Google Drive / Dropbox

### Requirements

**Functional:**
- Upload, download, and organize files/folders
- Sync files across multiple devices
- Share files/folders with other users
- Version history (restore previous versions)

**Non-Functional:**
- Strong consistency for file operations (you must see your own uploads immediately)
- High durability (zero data loss)
- Handle large files (up to 5GB)
- 500M users, 100M DAU

### The Core Challenge: File Sync

How do you sync files across devices efficiently? When a user edits a document on laptop A, device B must get the update. The naive approach — upload the entire file on every change — is bandwidth-wasteful for large files.

**Chunked file storage:**
Files are split into fixed-size chunks (4MB each). Each chunk is hashed (SHA-256). Chunks are stored by their hash as the key in S3. This gives you:

1. **Deduplication:** Two users uploading the same file → same content → same hash → stored once. Saves storage.
2. **Delta sync:** When a file changes, only modified chunks need uploading. A 1GB document with a 100-byte edit? Only the 1–2 chunks containing that edit are re-uploaded.

**File metadata storage:**
```
Files table (PostgreSQL):
  file_id       UUID         PRIMARY KEY
  user_id       UUID
  file_name     VARCHAR(255)
  parent_folder_id UUID
  current_version INTEGER
  created_at    TIMESTAMP
  modified_at   TIMESTAMP

FileVersions table:
  version_id    UUID         PRIMARY KEY
  file_id       UUID
  version_num   INTEGER
  chunk_ids     TEXT[]       -- ordered list of chunk SHA-256 hashes
  created_at    TIMESTAMP
  size_bytes    BIGINT

Chunks table (conceptually — actually stored in S3):
  chunk_hash    CHAR(64)     PRIMARY KEY  -- SHA-256
  s3_key        VARCHAR(255)
  size_bytes    INTEGER
```

**Sync protocol:**
```
Client maintains a local metadata DB (SQLite on device).

When a file changes:
1. Client chunks the file, hashes each chunk
2. Client sends metadata to server: "file X now has these chunk hashes: [h1, h2, h3]"
3. Server checks which chunks it doesn't have: "I'm missing h2"
4. Client uploads only missing chunks to S3 (via pre-signed URL)
5. Server updates FileVersions record
6. Server notifies other devices via WebSocket: "file X has been updated"
7. Other devices download changed version: server tells them which chunks to download
8. Devices download only changed chunks

This is a "sync" protocol, not "full upload every time."
```

**Conflict resolution:**
If two devices edit the same file simultaneously (offline then reconnect), you have a conflict. Google Drive creates a "conflicting copy" with a timestamp suffix. Dropbox does the same. Simple and transparent — let the user decide which version to keep.

### Sharing

```
Shares table:
  share_id    UUID
  resource_id UUID      -- file or folder
  shared_with UUID      -- user or email
  permission  ENUM(view, comment, edit)
  created_by  UUID
  created_at  TIMESTAMP
```

A user viewing a shared file → permission check → if they have access → serve from S3 (via pre-signed URL or access-controlled CDN).

---

## Mock Interview 8: Distributed Rate Limiter

### Requirements

**Functional:**
- Limit requests per API key to N requests per minute
- Multiple rate limiting rules (different limits for different endpoints)
- Return appropriate headers (X-RateLimit-Remaining, Retry-After)

**Non-Functional:**
- <5ms additional latency per request
- Distributed — must work correctly across many API gateway instances
- Graceful degradation — if rate limiter fails, should fail open (allow requests) rather than block all traffic

### Algorithm Choice: Sliding Window with Redis

**Token Bucket** (allow bursts up to bucket capacity):
```python
def check_rate_limit_token_bucket(api_key, capacity=100, refill_rate=10):
    # Redis Lua script for atomicity
    lua_script = """
    local key = KEYS[1]
    local capacity = tonumber(ARGV[1])
    local refill_rate = tonumber(ARGV[2])
    local now = tonumber(ARGV[3])
    
    local bucket = redis.call('HMGET', key, 'tokens', 'last_refill')
    local tokens = tonumber(bucket[1]) or capacity
    local last_refill = tonumber(bucket[2]) or now
    
    -- Add tokens for time elapsed
    local elapsed = now - last_refill
    tokens = math.min(capacity, tokens + elapsed * refill_rate)
    
    if tokens >= 1 then
        tokens = tokens - 1
        redis.call('HMSET', key, 'tokens', tokens, 'last_refill', now)
        redis.call('EXPIRE', key, 60)
        return 1  -- allowed
    else
        return 0  -- rejected
    end
    """
    result = redis.eval(lua_script, 1, f"ratelimit:{api_key}", capacity, refill_rate, time.time())
    return result == 1
```

**Why Lua scripts?** Redis executes Lua scripts atomically — the entire script runs as a single operation with no interleaving from other clients. This prevents race conditions in the token calculation.

### Distributed Consistency Challenge

With 10 API gateway instances all checking rate limits, they all share the same Redis. A request coming to any gateway instance checks the same Redis key for that API key. This works correctly because:

1. Redis is single-threaded for command execution (no race conditions within Redis)
2. The Lua script ensures the read-modify-write is atomic
3. All gateways share the same centralized state

**What if Redis is slow?** Use a local in-memory pre-check as a fast path:
- Each server maintains a rough local counter (reset every 1 second)
- If the local count is way under the limit, skip Redis and allow immediately
- If local count is near the limit, check Redis for accurate count
- This reduces Redis calls by 80-90% for normal traffic

**Graceful degradation:** If Redis is unreachable (timeout after 2ms), fail open — allow the request. The alternative (block all requests) is a denial of service you caused yourself.

---

## Mock Interview 9: Notification System

### Requirements

**Functional:**
- Send notifications to users across multiple channels: push (FCM/APNs), email, SMS
- Support different notification types: transactional (OTP, order confirmed) vs. marketing
- User preferences (which channels are enabled, when to send)
- Notification aggregation (batch similar events)

**Non-Functional:**
- Transactional notifications: < 5 second delivery
- Marketing notifications: batch delivery over hours (can be slower)
- High reliability for transactional (OTP must arrive — it's blocking)
- Deduplication (same notification must not be sent twice)

### Architecture

```
Events (from all services)
    |
    v
Kafka: "notification_events" topic
    |
    v
Notification Orchestrator Service
  - Reads user notification preferences
  - Determines which channels to use
  - Creates notification jobs per channel
    |
    +──────────────────┬─────────────────┐
    v                  v                 v
[Push Worker]    [Email Worker]    [SMS Worker]
    |                  |                 |
   FCM/APNs         SendGrid           Twilio
    |                  |                 |
  User Phone      User Inbox        User Phone
```

**Idempotency:** Every notification has a unique `notification_id`. Workers check: `redis.set("sent:notification_id", 1, nx=True, ex=86400)`. If the key already exists (already sent), skip. This prevents duplicates from Kafka at-least-once delivery.

**User preferences:**
```
UserNotificationPreferences:
  user_id         UUID
  channel         ENUM(push, email, sms)
  enabled         BOOLEAN
  quiet_hours_start TIME  -- don't send between 10PM and 8AM
  quiet_hours_end   TIME
  frequency_cap   INTEGER -- max N marketing notifications per day
```

**Priority queues:** Transactional notifications (OTP, password reset, order confirmed) go to a high-priority Kafka topic with dedicated workers. Marketing notifications go to a lower-priority topic processed more slowly.

**Aggregation (batching):**
```
"Alice liked your photo" + "Bob liked your photo" within 60 seconds
→ Don't send two notifications
→ Aggregate: "Alice, Bob, and 3 others liked your photo"

Implementation:
  When notification created: add to an aggregation buffer in Redis (TTL: 60s)
  After 60s: read all buffered notifications, merge similar ones, send aggregated version
```

---

## Mock Interview 10: Distributed Cache (Design Redis)

### Requirements

**Functional:**
- GET key → value
- SET key value EX seconds (with optional TTL)
- DELETE key
- Support string, hash, list, set, sorted set data structures

**Non-Functional:**
- < 1ms read/write latency
- In-memory storage
- Persistence (survive restarts)
- Replication (replica servers)
- Clustering (horizontal scaling)

### Core Architecture

**Single-threaded event loop:** Redis processes all commands in a single thread. No locking needed — commands are atomic because only one command executes at a time. This is key to Redis's simplicity and correctness.

**Memory management:**
- All data in RAM. Redis tracks memory usage.
- When memory exceeds `maxmemory`, eviction policy kicks in (LRU, LFU, etc.)
- Each value has an optional TTL stored alongside it
- A background process periodically scans for expired keys and removes them (lazy deletion + active expiry)

**Persistence:**
- **RDB:** Fork the process, write snapshot to disk. Zero impact on main thread (fork is copy-on-write). Periodic — configurable interval.
- **AOF:** Every write command appended to log. On restart: replay log. Can be fsync'd every second (safe) or every write (slow).

**Replication:**
- Replica connects to primary, sends `PSYNC` command
- Primary sends full RDB snapshot, then streams live commands
- Replica applies commands in same order → eventually consistent copy
- Primary-replica relationship: primary can have many replicas; replica can have sub-replicas

**Redis Cluster:**
- Data sharded across 16,384 hash slots
- Each node owns a range of hash slots
- `CLUSTER KEYSLOT key` → which slot → which node
- Clients connect to any node; node redirects to correct one (or client caches the mapping)
- Each shard has primary + replicas; automatic failover if primary dies

---

## Remaining Mock Interviews (Condensed)

### Twitter / X

**Core challenge:** The timeline/feed at scale. 300M+ active users, each following hundreds of others.

**Key decisions:**
- **Hybrid fan-out:** Fan-out on write for regular users; fan-out on read for celebrities (accounts with >10K followers). Store timelines in Redis (sorted sets, score = timestamp).
- **Sharding:** `users` by `user_id`. `tweets` by `user_id`. `follows` denormalized (by follower AND following).
- **Search:** Elasticsearch for tweet search (inverted index on tweet text).
- **Trending:** Count tweet occurrences per hashtag per time window using Kafka streaming + Redis sorted set (score = count).

### YouTube

**Core challenge:** Video upload, transcoding, and global delivery at scale.

**Key decisions:**
- **Transcoding pipeline:** Upload to S3 → event triggers transcoding job (parallel workers for each resolution) → store segments to CDN-backed S3.
- **Adaptive bitrate (ABR):** HLS/DASH manifest; player selects quality based on measured bandwidth.
- **CDN:** YouTube videos served from Google's global CDN. Popular videos cached at thousands of PoPs.
- **View counts:** Approximate count using Redis INCR (fast) → batch persist to DB every minute.
- **Recommendations:** Pre-computed by offline ML pipeline, stored in Cassandra by user_id.

### Food Delivery Platform

**Core challenge:** Real-time order tracking, restaurant and delivery matching.

**Key decisions:**
- **Order state machine:** PLACED → ACCEPTED (by restaurant) → PREPARING → READY → PICKED_UP → DELIVERED/CANCELLED
- **Real-time tracking:** Driver app sends GPS every 5 seconds → stored in Redis (current location) + Cassandra (history). Rider app subscribes via WebSocket to driver's location updates.
- **Restaurant matching:** When order placed → push notification to restaurant app. Restaurant acknowledges within N minutes or order reassigned.
- **ETA calculation:** Route distance from driver location to restaurant + restaurant prep time + restaurant to customer. Dynamically updated as driver moves.

---

## Summary

The 15 mock interviews cover common patterns that repeat across systems:

| Pattern | Systems |
|---------|---------|
| Fan-out on write/read | Twitter, Instagram, WhatsApp |
| WebSocket + message broker | WhatsApp, Uber, Food Delivery |
| Geospatial indexing | Uber, Food Delivery |
| Chunked file storage + delta sync | Google Drive, Dropbox |
| Adaptive bitrate streaming | Netflix, YouTube |
| Distributed locking | BookMyShow, Rate Limiter |
| Event-driven notification fanout | Notification System, WhatsApp |

Recognize these patterns and you can design any unfamiliar system by composing them.

---

<a name="part-13"></a>
# Part 13 — Senior Engineer Thinking

## The Difference Between Junior and Senior System Design

This is the section that separates candidates who pass senior-level interviews from those who don't. It's not about knowing more facts. It's about a fundamentally different way of thinking.

**A junior engineer** walks into a system design interview thinking: "What components do I need? What are the technologies involved? Let me draw a diagram."

**A senior engineer** walks in thinking: "What are the constraints? What breaks first? What happens at 10× scale? What are the failure modes? What tradeoffs am I making? What don't I know yet?"

Here's how to develop and demonstrate senior thinking.

---

## The Senior Engineer's Mental Checklist

Before accepting any architectural decision, a senior engineer mentally runs through:

### 1. "What is the actual bottleneck?"

Not theoretical bottlenecks. Measured ones.

**Wrong:** "We should shard the database because it might become a bottleneck."
**Right:** "Our write QPS estimate is 15K writes/sec. MySQL comfortably handles 5K writes/sec with good hardware. We need sharding or a write-optimized DB like Cassandra."

Systems usually have one dominant bottleneck at any given scale. Identify it. Fix it. Then find the next one.

### 2. "What fails first under load?"

Every system has a weakest link. Under unexpected load (traffic spike, viral event, DDoS), something fails first.

Think through: CPU? Memory? Disk I/O? Network bandwidth? Database connections? External API rate limits? A smart senior engineer knows which component will give out first and has a plan for it.

### 3. "What happens at 10× traffic? 100× traffic?"

Every architecture works at current scale. Design with growth in mind.

At 1× — your current architecture works.
At 10× — what breaks? Which service gets overwhelmed? Which database hits its limits?
At 100× — what requires fundamental re-architecture?

Don't over-engineer for 100× on day one. But know the path. "At 10× we'd need to add read replicas. At 100× we'd need to shard. Right now we're fine with a single primary."

### 4. "What if [dependency X] fails?"

For each dependency, ask: what happens to our system if this is unavailable?

- What if the database is down for 5 minutes?
- What if Redis is unreachable?
- What if the CDN has an outage?
- What if our third-party payment processor has downtime?

For each: Can we degrade gracefully? Can we serve stale data? Can we queue requests for later? Or do we completely fail?

**Graceful degradation** is a senior-level concept: under partial failures, provide a reduced but functional experience rather than a complete outage.

### 5. "Why this database? Why not another?"

Never choose a database because it's popular. Choose it because its properties match your requirements.

Be able to justify: "I'm choosing PostgreSQL over MongoDB because our data is relational, we need ACID transactions for order processing, and our write volume is low enough that sharding isn't needed yet. MongoDB would give us schema flexibility but we'd lose referential integrity and cross-document transactions, which matter for this use case."

### 6. "What consistency guarantees do we actually need?"

For each data type and operation:
- If a user sees data that's 2 seconds stale, is that acceptable?
- If a user's own action is not immediately reflected, is that acceptable?
- If two simultaneous users get conflicting information, what's the worst case?

Financial operations: strong consistency non-negotiable. Social feeds: eventual consistency is fine. Knowing which is which — and designing accordingly — is senior-level thinking.

### 7. "What are the operational costs?"

A technically superior solution that requires a team of 5 engineers to operate is often worse than a technically adequate solution that runs itself.

- How complex is this to deploy and monitor?
- What does it take to recover from failure?
- How do we upgrade it without downtime?
- What is the blast radius if something goes wrong?

This is why managed services (RDS, DynamoDB, managed Kafka) are often the right choice: the operational burden is on the provider.

### 8. "What are we not handling?"

Every design has edge cases. A senior engineer explicitly acknowledges them:

- "We're not handling the case where a user follows more than 10,000 celebrities — our hybrid fan-out threshold might need adjusting."
- "We're not addressing GDPR data deletion — if a user requests deletion, we'd need a deletion pipeline that cascades through Cassandra."

Acknowledging what you haven't solved — and having a sense of how you'd approach it — is more impressive than pretending your design is complete.

---

## How Senior Engineers Make Technology Tradeoffs

Best framework: **COGS** — Correctness, Operations, Growth, Speed.

**Correctness:** Does this guarantee the consistency and reliability our product requires?
**Operations:** How hard to operate at scale? Recovery time?
**Growth:** How far does this take us before re-architecture?
**Speed:** Does this give us the latency users need?

Example: MySQL vs. Cassandra for messages at 300K writes/sec:

| Factor | MySQL | Cassandra |
|--------|-------|-----------|
| Correctness | ACID, strong | Tunable, eventual default |
| Operations | Mature, managed options | Complex multi-node expertise |
| Growth | ~10K writes/sec, then shard | ~100K+ writes/sec horizontal |
| Speed | 1–5ms with indexing | ~1ms write-optimized |

For 300K writes/sec: Cassandra wins. For 500 writes/sec at a small startup: MySQL wins on simplicity.

---

## The "Walk Backwards from Failure" Technique

Start from failure and reason backward to resilient design.

**Step 1:** Imagine the system is completely down. Why?
**Step 2:** What could have caused that?
**Step 3:** What prevents each of those causes?
**Step 4:** Build those prevention mechanisms into the design.

Example — payment system:

"Imagine a payment is not recorded. Why?
- Database write failed → synchronous replication + retry with idempotency key
- Service crashed after DB write but before responding → client retries → idempotency key prevents double-charge
- Kafka message lost → acks=all (all replicas confirm before acknowledging)
- Third-party processor down → queue payment attempts with retry logic + exponential backoff"

Walking backward from failure ensures you don't miss failure modes.

---

## What Interviewers Are Actually Measuring

When an interviewer asks "design Twitter," they're evaluating:

**Structured thinking:** Can you approach a complex, ambiguous problem systematically? Do you clarify before designing? Do you estimate before choosing?

**Depth of understanding:** Can you explain *why* technologies work as they do? Do you understand tradeoffs, not just patterns?

**Tradeoff awareness:** Do you acknowledge what you're giving up? Do you choose based on requirements?

**Communication:** Can you walk a non-expert through a complex design?

**Adaptability:** When the interviewer adds a constraint ("now design it for 10× scale"), can you evolve intelligently?

**Intellectual honesty:** Do you acknowledge when you don't know something? Do you reason from first principles when facing unfamiliar territory?

---

## Interview Anti-Patterns to Avoid

**The "Kitchen Sink" architect:** Kafka, Redis, Cassandra, Elasticsearch, Kubernetes, GraphQL, gRPC — all in one design, most unnecessary. Shows memorization, not problem-solving.

**The "It depends" non-answerer:** Never commits to a decision. Make reasoned decisions. Defend them.

**The "Jump to solution" engineer:** Starts drawing without asking requirements, scale, or constraints. Designs the right solution to the wrong problem.

**The "Happy path only" thinker:** Never mentions what happens when the database is down or there's a traffic spike.

**The "Jargon without depth" candidate:** Uses all the right words but can't explain what they mean or when to use them.

---

## The Questions Senior Engineers Ask First

1. **"What's the read/write ratio?"** — Determines whether to optimize for reads or writes.
2. **"What are the latency requirements?"** — P50 is easy. P99 requires thought.
3. **"Is the data relational or hierarchical?"** — SQL vs. document vs. graph.
4. **"What's the consistency requirement per operation?"** — Financial: strong. Social: eventual.
5. **"What's the expected data growth rate?"** — Determines architecture runway.
6. **"What are the most common query patterns?"** — Determines indexes, shard key, cache strategy.
7. **"Who are the downstream consumers of this data?"** — Pull API or push events?

---

## Final Advice: The Mindset That Wins Interviews

The best system designers don't memorize architectures. They reason from first principles:

"Here's the requirement. Here's the constraint it imposes. Here's the naive solution and why it fails. Here's the evolved solution and what it trades off. Here's when I'd choose something different."

This document has given you:
- The evolution mindset (Part 1)
- Networking fundamentals (Part 2)
- Database internals (Part 3)
- Caching patterns (Part 4)
- Scaling strategies (Parts 5–6)
- Messaging systems (Part 7)
- Distributed systems theory (Part 8)
- Core building blocks (Part 9)
- Estimation framework (Part 10)
- Interview framework (Part 11)
- 10+ mock interviews (Part 12)
- Senior thinking (Part 13)

The final step is practice. Design systems out loud. Explain your reasoning. Welcome pushback — it makes you better.

---

## Summary — The Senior Engineer's Core Principles

1. **Start with requirements.** Every design decision flows from requirements.
2. **Estimate before choosing.** Let numbers drive technology decisions.
3. **Start simple, evolve to complex.** Simplest design today; know the path to scale tomorrow.
4. **Every decision has a tradeoff.** Naming the tradeoff is more important than naming the technology.
5. **Design for failure.** Every component will eventually fail. Design for graceful degradation.
6. **Measure, don't guess.** Bottlenecks must be measured. Premature optimization adds unnecessary complexity.
7. **Operations matter as much as architecture.** A brilliant architecture that's impossible to operate is a bad architecture.
8. **Be explicit about what you don't know.** Intellectual honesty about unknowns is a strength, not a weakness.

---

*This document covers the complete landscape of Senior Software Engineer (SDE-2) High Level Design interviews. From single-server beginnings to globally distributed architectures, from B-tree indexes to Kafka partitions, from cache invalidation to CAP theorem — you now have the foundation to walk into any HLD interview and think like a senior engineer.*

---

**End of Document**
