# The Complete Practical Guide to Golang Concurrency
### A Staff Engineer's Brain Dump After Years of Building Large-Scale Go Services

---

> This is not a textbook. This is what I wish someone had told me before I pushed a deadlock into production at 3am.
> Read this like we're sitting at a whiteboard together — I'm the senior, you're the curious engineer asking all the right questions.
> Everything here comes from real production experience building services at scale.

---

## Table of Contents

1. [Concurrency vs Parallelism](#1-concurrency-vs-parallelism)
2. [Goroutines](#2-goroutines)
3. [The Go Scheduler — GMP Model](#3-the-go-scheduler-gmp-model)
4. [Channels](#4-channels)
5. [Select](#5-select)
6. [sync.Mutex](#6-syncmutex)
7. [sync.RWMutex](#7-syncrwmutex)
8. [sync.WaitGroup](#8-syncwaitgroup)
9. [Context](#9-context)
10. [sync.Once](#10-synconce)
11. [sync.Pool](#11-syncpool)
12. [Atomic Operations](#12-atomic-operations)
13. [The Go Memory Model](#13-the-go-memory-model)
14. [Race Conditions](#14-race-conditions)
15. [Deadlocks](#15-deadlocks)
16. [Goroutine Leaks](#16-goroutine-leaks)
17. [Worker Pools](#17-worker-pools)
18. [Pipelines](#18-pipelines)
19. [Fan-In / Fan-Out](#19-fan-in--fan-out)
20. [Backpressure](#20-backpressure)
21. [Cancellation Patterns](#21-cancellation-patterns)
22. [Concurrency Debugging](#22-concurrency-debugging)
23. [pprof](#23-pprof)
24. [go tool trace](#24-go-tool-trace)
25. [Production Tuning](#25-production-tuning)
26. [Runtime Internals](#26-runtime-internals)
27. [Interview Masterclass](#27-interview-masterclass)

---

# 1. Concurrency vs Parallelism

## What problem does it solve?

Let's understand why this distinction exists, and why it matters far more than most engineers realize.

Before writing a single line of Go, we need to completely destroy a misconception that lives deep in the brain of most developers — even senior ones. **Concurrency and parallelism are not the same thing.** They are not synonyms. They are not two words for the same idea. They are fundamentally different concepts that operate at different layers of your system, and confusing them leads to real production mistakes.

I've interviewed hundreds of engineers. When I ask "what is the difference between concurrency and parallelism?" most of them pause, then say something like "uh, parallelism is when things run at the same time, and concurrency is... kind of the same but with goroutines?" That answer fails. And if you go into production thinking they're the same, you'll make architectural decisions that don't scale.

Let's fix this once and for all.

## Mental Model

Here's the simplest possible way I can put it:

**Parallelism** is a hardware concept. It means two or more things are happening at the *exact same physical instant* on two or more CPU cores. It requires multiple processors. Without multiple processors, there is no parallelism — period.

```
CPU Core 1: [====== Task A ======]
CPU Core 2: [====== Task B ======]
                ^ Same clock tick. Literally simultaneous.
```

**Concurrency** is a software design concept. It means your program is *structured* to deal with multiple things at once — but they may or may not actually execute simultaneously. It's about the *structure* of your code, not the hardware underneath it.

```
CPU Core 1: [Task A][Task B][Task A][Task B][Task A][Task B]
             ← time →
             ^ Task A and Task B are interleaved. They take turns.
             ^ They are NEVER at the same instant — but the program
               manages both of them "at once" conceptually.
```

Rob Pike — one of Go's creators — gave a famous talk that every Go engineer should watch: "Concurrency is not Parallelism." His summary:

> **Concurrency is about dealing with lots of things at once.**
> **Parallelism is about doing lots of things at once.**

Read that again. "Dealing with" vs "doing." The first is a design property of your program. The second is a physical property of execution.

## How developers usually think about this

Most developers think: "I wrote `go f()`, therefore my code is running in parallel." 

This is wrong in a subtle but important way. Writing `go f()` makes your program *concurrent* — you've structured it to handle `f()` alongside the rest of your code. But whether `f()` actually *runs in parallel* — on a different CPU core at the exact same instant — depends on `GOMAXPROCS` and how many physical cores you have available.

On a single-core machine, a Go program with 1,000 goroutines is purely concurrent — there is zero parallelism. The scheduler gives each goroutine a slice of the single CPU core and switches between them rapidly. It looks like things are running simultaneously because the switches are fast, but they are not.

On an 8-core machine with `GOMAXPROCS=8`, up to 8 goroutines genuinely execute in parallel at any given moment. The rest queue up waiting for a turn.

```
Single-Core Machine (GOMAXPROCS=1):
  G1 [===] G2 [===] G1 [===] G3 [===] G2 [===]
  ← time slices, interleaved, never simultaneous →
  This is CONCURRENCY, not parallelism.

8-Core Machine (GOMAXPROCS=8):
  Core 1: G1  [============]
  Core 2: G2  [============]
  Core 3: G3  [============]
  Core 4: G4  [============]
  Core 5: G5  [============]
  Core 6: G6  [============]
  Core 7: G7  [============]
  Core 8: G8  [============]
           ^ These 8 goroutines run SIMULTANEOUSLY.
           ^ This is both CONCURRENCY and PARALLELISM.
  G9..G1000: waiting in run queues for their turn
```

## Where people get confused

The confusion comes from two places:

**First**: most modern hardware is multi-core, so when engineers add goroutines and observe speedup, they correctly see that parallelism is happening — but they attribute it to the wrong thing. They say "goroutines make things parallel." More precisely: goroutines enable *concurrency*, and the Go runtime *exploits parallelism* by running them across multiple cores. The goroutine itself isn't parallel — the runtime's mapping of goroutines to OS threads is what creates parallelism.

**Second**: people conflate "running fast" with "running in parallel." A single-core program switching rapidly between goroutines can appear to run things "at the same time" from a human perspective, but it is categorically not parallel. The distinction matters for reasoning about CPU bottlenecks vs I/O bottlenecks (more on this in a moment).

## How it actually works in Go

When your Go program starts, the runtime reads `GOMAXPROCS` (default: `runtime.NumCPU()`, i.e., the number of logical CPU cores available). It creates that many **P** (processor) structs — you'll meet these in the GMP section. Each P can run one goroutine at a time. So if `GOMAXPROCS=4`, at most 4 goroutines execute simultaneously.

```
GOMAXPROCS=4, 100 goroutines:

  Executing now (parallel):  G1, G2, G3, G4
  Waiting in queues:         G5 ... G100

  Every few microseconds, the scheduler rotates goroutines in and out.
  At any instant: 4 goroutines are physically running.
  Over time: all 100 get CPU time. That's concurrency.
```

## Real Production Example

Let me give you a concrete scenario to anchor this.

Imagine you're running a service that handles 50,000 requests per second. Each incoming HTTP request:
1. Reads a session token from Redis (~1ms round trip)
2. Queries a user record from Postgres (~5ms round trip)
3. Calls an internal profile service via gRPC (~3ms round trip)
4. Builds and marshals a JSON response (~0.1ms CPU)
5. Writes the response (~0.05ms)

Total wall-clock time per request: ~9ms, but only ~0.15ms of that is CPU work. The other 8.85ms is waiting on network I/O.

On a 4-core machine, if you used OS threads (one per request), you'd need thousands of threads to keep 50K concurrent requests in flight. That's gigabytes of RAM for thread stacks alone.

With goroutines: each request parks its goroutine during the I/O waits. The scheduler immediately runs another goroutine on that core. At 50K RPS with 9ms average latency, you have ~450 goroutines in flight at steady state. Each goroutine uses ~2-8KB of stack. Total: a few megabytes. And CPU parallelism is used efficiently for the actual compute portions.

This is concurrency solving your problem — not parallelism. The I/O wait time is the bottleneck, and goroutines handle that through cooperative scheduling, not through having 450 CPU cores.

Now imagine you add an image processing pipeline — each image gets resized into 6 thumbnail formats. That work is pure CPU: no I/O, just pixel math. Here you *do* want parallelism. You want Go to run thumbnail generation goroutines on all 4 cores simultaneously. That's where `GOMAXPROCS=4` earns its keep.

**The mental checklist**:
- Is your bottleneck I/O (network, disk)? → You need concurrency. Goroutines handle this naturally.
- Is your bottleneck CPU (computation)? → You need parallelism. Set `GOMAXPROCS` to match your core count, and make sure your work is CPU-bound (not locked behind a mutex).

## What happens under the hood

Let me show you how Go maps the concurrency model to actual parallelism.

The Go runtime maintains a set of OS threads (M structs) and a set of logical processors (P structs). The count of Ps is `GOMAXPROCS`. Each P holds a queue of runnable goroutines and runs them one at a time on whichever M it's currently paired with.

```
Runtime internals at GOMAXPROCS=3:

  P0 <──> M0 (OS thread)          P1 <──> M1 (OS thread)
  ┌──────────────────┐            ┌──────────────────┐
  │ Running: G1      │            │ Running: G4      │
  │ LocalQ: G2,G3    │            │ LocalQ: G5,G6    │
  └──────────────────┘            └──────────────────┘

  P2 <──> M2 (OS thread)
  ┌──────────────────┐
  │ Running: G7      │
  │ LocalQ: G8       │
  └──────────────────┘

  Global Queue: G9, G10, G11, G12 ...
  Idle Ms: M3, M4 (parked, no P assigned)

  G1, G4, G7 are PARALLEL — physically running on 3 cores simultaneously.
  G2, G3, G5, G6, G8, G9... are CONCURRENT — queued, waiting for their turn.
```

The OS threads are truly parallel — they run on different CPU cores at the same time. The goroutines *within* a single P are concurrent — they share one OS thread and take turns.

## Performance Implications

Understanding this distinction has direct performance implications:

**CPU-bound work**: More goroutines than `GOMAXPROCS` doesn't help and actually hurts. Each goroutine beyond `GOMAXPROCS` adds scheduler overhead without adding CPU capacity. For a pure CPU-bound computation, the optimal goroutine count equals `GOMAXPROCS`.

```go
// CPU-bound: spawn exactly runtime.NumCPU() workers
numWorkers := runtime.NumCPU()
for i := 0; i < numWorkers; i++ {
    go worker(jobs, results)
}
```

**I/O-bound work**: You can have far more goroutines than `GOMAXPROCS` because goroutines block on I/O and release their P for other goroutines. For I/O-bound work, goroutine count is limited by memory (stack size), not CPU.

```go
// I/O-bound: can have thousands of goroutines
// Each one sleeps most of the time, waiting on network
for _, req := range thousandsOfRequests {
    go func(r Request) { fetchFromDB(r) }(req)
}
```

**Mixed workloads**: Profile first. Identify whether you're CPU-bound or I/O-bound. Tune goroutine count and `GOMAXPROCS` accordingly.

## Common Mistakes

**Mistake 1: Setting GOMAXPROCS=1 "for simplicity."**

Some developers set `GOMAXPROCS=1` because "my program is concurrent, I don't need parallelism." This is almost always wrong. For any real service, you want the runtime to exploit all available CPU cores. The default `GOMAXPROCS=runtime.NumCPU()` is correct for most workloads.

**Mistake 2: Assuming more goroutines always means more throughput.**

Adding goroutines to a CPU-bound computation beyond `GOMAXPROCS` does *not* improve throughput — it adds scheduling overhead and memory pressure. I've seen engineers spawn 10,000 goroutines for a CPU-bound matrix multiplication thinking "more goroutines = more parallel." The result was slower than using 8 goroutines on an 8-core machine.

**Mistake 3: Ignoring GOMAXPROCS in containers.**

In Kubernetes or Docker, your container may have a CPU limit of 0.5 or 2 cores, but it's running on a 64-core host. Go's default `runtime.NumCPU()` returns the *host* CPU count — 64 — not your container's limit. So you get `GOMAXPROCS=64`, meaning 64 OS threads competing for 0.5 or 2 physical cores. This creates massive context switch overhead and latency spikes.

The fix is `go.uber.org/automaxprocs`, which reads the Linux CFS CPU quota and sets `GOMAXPROCS` appropriately:

```go
import _ "go.uber.org/automaxprocs"

func main() {
    // automaxprocs reads /sys/fs/cgroup/cpu/cpu.cfs_quota_us
    // and sets GOMAXPROCS to match the container's actual CPU limit
}
```

This is one of the highest-ROI changes you can make to a containerized Go service.

**Mistake 4: Thinking async/await style and goroutines are the same thing.**

JavaScript's async/await is a single-threaded concurrency model — callbacks on one thread. Go goroutines can be parallel — multiple threads, truly simultaneous. The mental models are different: in JS, you never have two functions genuinely running at the same instant; in Go, you can and do.

## Interview Questions

**Q: Explain the difference between concurrency and parallelism.**

Concurrency is a design property of a program — it is structured to handle multiple independent tasks, potentially interleaved in time. Parallelism is an execution property — multiple tasks physically execute at the same instant on multiple processor cores. A program can be concurrent without being parallel (single-core machine), and you can have non-concurrent parallel computation (SIMD vector instructions doing the same operation on 8 elements simultaneously, but no independent control flow).

**Q: Is Go concurrent or parallel?**

Both, depending on hardware and GOMAXPROCS. Go provides concurrency primitives (goroutines, channels). The Go runtime exploits parallelism by mapping goroutines to OS threads and running those threads on multiple CPU cores. On a multi-core machine with `GOMAXPROCS > 1`, goroutines run in parallel.

**Q: If you have 1,000 goroutines on a 4-core machine, how many run in parallel?**

At most 4 (one per core, assuming `GOMAXPROCS=4`). The other 996 are either blocked (waiting on I/O, channels, mutexes) or runnable (in run queues). At any given instant, exactly `GOMAXPROCS` goroutines execute.

**Q: Can you achieve parallelism without concurrency in Go?**

Technically yes — if you write `GOMAXPROCS` goroutines that all do independent CPU work with no synchronization, they run in parallel and your program has parallelism without meaningful concurrency primitives. But in practice, Go's parallelism always comes through its concurrency model.

## Key Takeaways

- Concurrency is structure; parallelism is execution
- Go gives you concurrency primitives; the runtime provides parallelism
- `GOMAXPROCS` is the parallelism ceiling — goroutines beyond that count are concurrent, not parallel
- I/O-bound services benefit from concurrency; CPU-bound services benefit from parallelism
- In containers, always set `GOMAXPROCS` from container CPU limits — use `automaxprocs`
- Profile before tuning; know whether you're CPU-bound or I/O-bound

---

# 2. Goroutines

## What problem does it solve?

Let's understand why goroutines exist by starting with the problem they replaced.

Traditional servers used the **thread-per-connection** model. When a client connects, the server spawns an OS thread to handle it. That thread lives for the duration of the connection — reading requests, processing them, writing responses — and dies when the connection closes.

This worked fine in the early 2000s when a "busy" server had a few hundred concurrent users. But as the internet scaled, engineers hit what became known as the **C10K problem** — how do you handle 10,000 concurrent connections on a single server?

The answer with threads was: you can't. Here's why:

An OS thread on Linux has a **default stack size of 8MB** (adjustable but with limits). Just the stack memory for 10,000 threads is 80GB. Even if you reduce the default stack to 1MB, you're at 10GB. 

*(Note: While 10GB of RAM is very little today, remember the "C10K problem" was coined in 1999 when a server might only have 512MB of RAM. Today, the benchmark is the **C10M problem** (10 Million connections). With OS threads, that would be 10 Terabytes of RAM. With Goroutines at 2KB each, it is only 20 Gigabytes).*

And that's ignoring the massive overhead of OS-level context switches, which involve saving and restoring full CPU register state in kernel mode at a cost of ~1–5µs each.

The industry tried various workarounds: event loops (Node.js), callback hell, async/await, non-blocking I/O with `epoll`/`kqueue`. These work but make code complex and harder to reason about. You lose the natural sequential flow of "read, then process, then write."

Go's answer is elegant: **goroutines**. They give you the simplicity of thread-per-task code with the efficiency of an event-loop system. You write sequential-looking code, but the runtime schedules it efficiently across a small pool of OS threads.

## Mental Model

Think of goroutines as extremely lightweight tasks that the Go runtime manages for you. The key word is *lightweight*:

- An OS thread starts with **8MB** of stack by default (kernel minimum is 4KB, but practically 8MB on Linux)
- A goroutine starts with **2KB** of stack
- That's a 4000x reduction in memory per concurrent unit

```
OS Threads (traditional server):
  Thread 1:  [=============8MB stack=============]
  Thread 2:  [=============8MB stack=============]
  Thread 3:  [=============8MB stack=============]
  ...
  Thread 10,000: MEMORY EXHAUSTED. System crashes.

Goroutines (Go):
  G1:   [2KB]
  G2:   [2KB]
  G3:   [2KB]
  ...
  G10,000:   20MB total — laughably small.
  G1,000,000: 2GB total — fits on a laptop.
```

And the cost comparison for creation:

```
OS thread creation:  ~10,000ns (10µs) — kernel call, memory mapping, TLS setup
Goroutine creation:  ~300ns  — just a struct allocation and queue push
                     That's 33x faster.
```

This is why Go services can handle hundreds of thousands of concurrent connections without breaking a sweat.

## How it works internally

When you write `go f()`, here is exactly what the Go runtime does:

**Step 1**: The runtime calls `newproc()` in `runtime/proc.go`. This function:
- Allocates a `g` struct (goroutine descriptor) — either from a free list or freshly allocated
- Sets up the goroutine's initial stack
- Sets the PC (program counter) to point at `f`
- Sets up argument passing on the goroutine's stack

**Step 2**: A new stack is allocated. The initial stack size is **2048 bytes (2KB)**. This is defined as `_StackMin = 2048` in `runtime/stack.go`. This 2KB is where local variables, function arguments, and the call stack for the goroutine will live initially.

**Step 3**: The goroutine is placed in the `runnext` slot of the current P's local run queue (priority position — runs before other queued goroutines). If `runnext` is already occupied, it's placed in the P's circular run queue instead.

**Step 4**: The function `go f()` returns immediately in the calling goroutine. The new goroutine is now "runnable" but not yet running.

Here's the goroutine lifecycle as a state machine:

```
                    go f()
                      │
                      ▼
                 [_Grunnable]  ← in a run queue, waiting for a P
                      │
              scheduler picks it up
                      │
                      ▼
                 [_Grunning]   ← executing on an M (OS thread)
                      │
           ┌──────────┴──────────┐
           │                     │
    blocks on I/O            function returns
    channel, mutex              │
    sleep, etc.                 ▼
           │              [_Gdead]   ← recycled into free list
           ▼
       [_Gwaiting]  ← parked, not on any run queue, not burning CPU
           │
    I/O completes / channel ready
    mutex released / timer fires
           │
           ▼
      [_Grunnable]  ← back in run queue
```

The critical insight: a goroutine in `_Gwaiting` consumes **zero CPU**. The OS thread it was running on picks up the next runnable goroutine immediately. The waiting goroutine is just a struct in memory, dormant, until the event it's waiting for occurs.

## What happens under the hood — the goroutine struct

The runtime represents every goroutine as a `g` struct. This is one of the most important data structures in the entire Go runtime. Let me walk you through the key fields:

```go
// Simplified from runtime/runtime2.go
// The actual struct has ~100 fields; these are the most important ones.
type g struct {
    // Stack bounds for this goroutine.
    // The stack grows downward on most architectures.
    // stack.lo <= SP <= stack.hi
    stack stack // offset known to runtime/cgo

    // stackguard0 is compared against the stack pointer during function prologue.
    // If SP falls below this value, the goroutine needs to grow its stack.
    // It's set to stack.lo + _StackGuard (128 bytes of safety margin).
    stackguard0 uintptr // offset known to liblink

    // stackguard1 is used by the C stack growth checks.
    stackguard1 uintptr

    // _panic is a linked list of active panics.
    _panic *_panic

    // _defer is a linked list of deferred calls.
    _defer *_defer

    // m is the current M (OS thread) running this goroutine.
    // nil if the goroutine is not currently running.
    m *m

    // sched stores the goroutine's context when it's NOT running.
    // When the scheduler parks a goroutine, it saves SP and PC here.
    // When the goroutine is resumed, it restores from here.
    sched gobuf

    // atomicstatus stores the current status: _Gidle, _Grunnable,
    // _Grunning, _Gsyscall, _Gwaiting, _Gdead, etc.
    atomicstatus atomic.Uint32

    // goid is the goroutine's unique ID (used in stack traces).
    goid int64

    // schedlink is used to link goroutines in run queues.
    schedlink guintptr

    // waitsince is the timestamp when the goroutine entered Gwaiting.
    // Used for detecting long blocking operations in pprof.
    waitsince int64

    // waitreason explains WHY the goroutine is waiting.
    // Values: waitReasonChanReceive, waitReasonSleep, waitReasonGCMark, etc.
    waitreason waitReason

    // preempt is set to true when the goroutine should be preempted.
    // The goroutine will check this at safe points and yield.
    preempt bool

    // lockedm is set if this goroutine is locked to a specific M.
    // Used by runtime.LockOSThread().
    lockedm muintptr

    // gopc is the PC of the go statement that created this goroutine.
    // Used for stack traces to show where goroutines were started.
    gopc uintptr

    // startpc is the PC of the function passed to go.
    startpc uintptr

    // paniconfault causes a runtime panic (instead of a process fault)
    // if the goroutine accesses an invalid memory address.
    paniconfault bool

    // labels is the set of profiling labels attached to this goroutine.
    labels unsafe.Pointer
}
```

The `gobuf` struct — the goroutine's saved execution context — is particularly important:

```go
type gobuf struct {
    sp   uintptr  // saved stack pointer
    pc   uintptr  // saved program counter (instruction pointer)
    g    guintptr // goroutine that owns this context
    ctxt unsafe.Pointer // closure context
    ret  uintptr  // return value from syscall
    lr   uintptr  // link register (used on ARM/RISC-V)
    bp   uintptr  // saved base pointer (frame pointer, if enabled)
}
```

When a goroutine is preempted or blocks, the scheduler saves its `sp` and `pc` into `gobuf`. When resumed, it restores from `gobuf` and continues exactly where it left off. This is the core of context switching.

## Stack Growth — Contiguous Stacks

This is one of Go's most clever engineering decisions. Let's understand it deeply.

The problem: we start goroutines with 2KB stacks. But a goroutine might call deeply nested functions, or recurse, and need much more stack space. How do we handle this without pre-allocating a large fixed stack (which wastes memory)?

Go uses **contiguous, growable stacks** (introduced in Go 1.4, replacing the earlier segmented stack approach).

Here's how it works:

**At every function call**, the compiler inserts a stack overflow check in the function prologue. This is a comparison between the current stack pointer and `stackguard0`:

```asm
; x86-64 example of what the compiler generates
; at the start of every Go function:
MOVQ (TLS), R14      ; load current goroutine pointer
CMPQ SP, 24(R14)     ; compare SP with g.stackguard0
JBE  grow_stack      ; if SP <= stackguard0, need to grow
; ... function body ...
grow_stack:
    CALL runtime.morestack_noctxt(SB)
    JMP  function_start
```

If the check fails (stack would overflow), `runtime.morestack()` is called:

1. **Allocate a new stack** twice as large as the current one
2. **Copy the entire stack** from old location to new location (bit-for-bit)
3. **Update all pointers** that point into the old stack — this includes saved frame pointers, return addresses, and Go-level pointers (the runtime knows the type of every local variable thanks to the garbage collector's type information)
4. **Resume execution** on the new, larger stack

```
Initial stack (2KB):           After first growth (4KB):
┌───────────┐                  ┌───────────────────────┐
│ frame 1   │                  │ frame 1 (copied)      │
│ frame 2   │    morestack()   │ frame 2 (copied)      │
│ frame 3   │ ──────────────▶  │ frame 3 (copied)      │
│ [LIMIT]   │                  │ frame 4 (new space)   │
└───────────┘                  │ frame 5 (new space)   │
                               └───────────────────────┘
```

**Why not segmented stacks?** Go used to use segmented stacks (stack segments linked together like a list). The problem was the "hot split" issue: if a function that's called in a tight loop needs just a tiny bit more stack and sits right at a segment boundary, it repeatedly allocates and deallocates a stack segment on every iteration — a performance disaster. Contiguous copying stacks have a smoother performance profile.

**Stack shrinking**: Go also shrinks stacks. After a garbage collection, if a goroutine's stack is larger than 4x what it's actually using, the stack is shrunk (copied to a smaller allocation). This prevents one-time-deep-call goroutines from holding 1MB stacks forever.

## Real Production Example

Here's a real scenario that illustrates goroutine power at scale.

We built an event ingestion pipeline that received IoT sensor data. At peak, we ingested 2,000,000 events per second from 500,000 connected devices. Each device maintained a persistent WebSocket connection.

With goroutines: 500,000 goroutines — one per connection. Each goroutine spent 99.9% of its time blocked, waiting for the next event from its device. Memory for stacks: ~1GB. CPU for scheduling: negligible compared to actual processing.

Stack traces from pprof showed the majority of goroutines in `_Gwaiting` with `waitReason: waitReasonChanReceive`. The runtime had perfectly parked them with zero CPU cost.

The alternative (event-loop based) would have required complex state machines to track each connection's state across async callbacks. The goroutine model let us write a simple sequential loop:

```go
func handleDevice(ctx context.Context, conn *websocket.Conn, deviceID string) {
    for {
        msg, err := conn.ReadMessage()
        if err != nil {
            if ctx.Err() != nil { return } // context cancelled, clean shutdown
            log.Printf("device %s disconnected: %v", deviceID, err)
            return
        }

        event, err := parseEvent(msg)
        if err != nil {
            log.Printf("device %s bad event: %v", deviceID, err)
            continue
        }

        if err := processEvent(ctx, event); err != nil {
            log.Printf("device %s process error: %v", deviceID, err)
        }
    }
}

// In main server loop:
go handleDevice(ctx, conn, deviceID)
```

Sequential, readable, obvious. 500,000 of these in flight simultaneously. This is the Go way.

## Common Mistakes

**Mistake 1: Classic loop variable capture — the most common Go bug I see in code reviews.**

```go
// BUG: all goroutines capture the same variable `i`
// By the time any goroutine runs, the loop may have finished
// and i == 5. All goroutines print 5.
for i := 0; i < 5; i++ {
    go func() {
        fmt.Println(i) // captures i by reference, not by value
    }()
}

// Fix 1: pass i as a function argument (creates a new copy)
for i := 0; i < 5; i++ {
    go func(n int) {
        fmt.Println(n) // n is a copy of i at the moment of go statement
    }(i)
}

// Fix 2: create a new variable inside the loop (Go 1.22+ does this automatically)
for i := 0; i < 5; i++ {
    i := i // shadows outer i with a new variable per iteration
    go func() {
        fmt.Println(i)
    }()
}
```

Note: **Go 1.22 changed the semantics** of loop variables — they are now per-iteration, so this bug is eliminated in new code compiled with Go 1.22+. But you'll still encounter it in codebases using older Go versions.

**Mistake 2: Assuming `main` waits for goroutines.**

```go
func main() {
    go fmt.Println("hello from goroutine")
    // main returns here immediately
    // the goroutine may never execute
    // the program exits before the goroutine runs
}
```

When `main()` returns, the Go runtime calls `os.Exit(0)`, which terminates the process immediately. All goroutines — running, blocked, or runnable — are killed without any cleanup. No deferred functions in non-main goroutines run. No goroutine gets a chance to finish.

```go
// Fix: use WaitGroup, channel, or sleep (last resort)
func main() {
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        fmt.Println("hello from goroutine")
    }()
    wg.Wait() // main blocks until goroutine completes
}
```

**Mistake 3: Not monitoring goroutine count in production.**

Goroutine count is a critical health metric. An ever-increasing goroutine count is the classic sign of a goroutine leak. Always export this:

```go
// Prometheus example
goroutineGauge := prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "go_goroutines_total",
    Help: "Number of goroutines currently running",
})

go func() {
    for range time.NewTicker(10 * time.Second).C {
        goroutineGauge.Set(float64(runtime.NumGoroutine()))
    }
}()
```

Alert if this grows unboundedly. A healthy service has a stable goroutine count that rises and falls with traffic load.

**Mistake 4: Thinking goroutines are free.**

They're extremely cheap, but not free. Creating a goroutine involves:
- Memory allocation for the `g` struct (~400 bytes)
- Stack allocation (2KB)
- Enqueueing in the scheduler
- Eventually, GC must collect dead goroutines

At 100,000 goroutine creations/second, you're creating 100K × 2KB = 200MB of stack per second that needs to be GC'd. At this rate, you'll want to use a goroutine pool to reuse goroutines.

**Mistake 5: Using `runtime.Gosched()` as a fix for concurrency bugs.**

```go
// Don't do this — it's a band-aid, not a fix
for {
    doWork()
    runtime.Gosched() // "yield" to other goroutines
}
```

`runtime.Gosched()` hints to the scheduler to yield, but it doesn't provide any synchronization guarantees. It does not make your code safe from data races. If you feel the need to call it, you're probably missing a proper synchronization primitive (channel, mutex, etc.).

## Performance Implications

Let's look at actual numbers.

**Goroutine creation benchmark:**

```go
func BenchmarkGoroutineCreation(b *testing.B) {
    for i := 0; i < b.N; i++ {
        done := make(chan struct{})
        go func() { close(done) }()
        <-done
    }
}
// Result: ~300-500ns per goroutine creation on modern hardware
```

**Memory per goroutine (approximate):**

```
Initial goroutine overhead:
  g struct:       ~400 bytes
  Initial stack:  2048 bytes
  Total:          ~2.4KB per goroutine

1,000 goroutines:   ~2.4MB
100,000 goroutines: ~240MB
1,000,000 goroutines: ~2.4GB  (feasible on a server with 4GB+ RAM)
```

**Context switch cost:**

- Go goroutine context switch: ~100-200ns (user-space, just gobuf save/restore)
- OS thread context switch: ~1,000-5,000ns (kernel mode transition, full register save)
- That's a 5-50x difference

This is why goroutines handle I/O-bound concurrency so efficiently — the overhead of switching between them while waiting for I/O is negligible compared to the I/O latency itself.

## Interview Questions

**Q: What is the difference between a goroutine and an OS thread?**

A goroutine is a user-space lightweight thread managed by the Go runtime. It starts with a 2KB stack (vs 8MB for OS threads), is created in ~300ns (vs ~10µs for OS threads), and is scheduled by the Go runtime's user-space scheduler (vs the OS kernel scheduler). Multiple goroutines multiplex onto a smaller number of OS threads via the GMP scheduler. The goroutine is cheaper to create, faster to switch, and uses far less memory than an OS thread.

**Q: What is the initial stack size of a goroutine, and how does it grow?**

A goroutine starts with a 2KB stack (defined as `_StackMin` in `runtime/stack.go`). The stack grows dynamically using contiguous stack copying: when a function prologue detects that the current stack pointer has fallen below `stackguard0`, `runtime.morestack()` is called, which allocates a new stack twice as large, copies the old stack contents, updates all pointers, and resumes execution. The stack can grow up to 1GB by default (controlled by `runtime/debug.SetMaxStack()`).

**Q: What happens to all goroutines when `main` returns?**

All goroutines are terminated immediately without any cleanup. The runtime calls `runtime.exit()` which calls `os.Exit()`. No deferred functions in non-main goroutines execute. No goroutine gets to finish its work. This is why graceful shutdown requires explicit coordination via `sync.WaitGroup`, channels, or `context.Context`.

**Q: Why do goroutines use so much less memory than OS threads?**

OS threads need large fixed stacks because the OS must pre-allocate enough stack space for the worst case (deep recursion, large local variables). Goroutines start small because Go has a runtime that can grow the stack dynamically by copying it to a new, larger allocation. There's no "worst case reservation" needed.

**Q: What are the goroutine states and what do they mean?**

- `_Gidle`: allocated but not initialized
- `_Grunnable`: on a run queue, ready to run
- `_Grunning`: executing on an M
- `_Gsyscall`: executing a system call (goroutine's M may be handling the syscall)
- `_Gwaiting`: blocked on a channel, mutex, I/O, sleep, GC, etc. — not consuming CPU
- `_Gdead`: finished execution, being recycled or freed

## Key Takeaways

- Goroutines start at 2KB stack, grow dynamically via contiguous copying — this is the key to their memory efficiency
- Goroutine creation costs ~300ns and ~2.4KB — you can create hundreds of thousands
- A goroutine in `_Gwaiting` consumes zero CPU — it's just a struct in memory
- When `main` returns, all goroutines die immediately — always explicitly wait for goroutines you care about
- Monitor goroutine count in production — unbounded growth means a leak
- Goroutine context switches are ~100-200ns (user-space) vs ~1-5µs for OS threads (kernel-space)

---

# 3. The Go Scheduler — GMP Model

## What problem does it solve?

Let's understand why the GMP model exists and why it's designed the way it is.

We have potentially millions of goroutines but only a handful of OS threads. Somehow we need to map many goroutines onto few threads efficiently, without:

1. Creating a new OS thread for every goroutine (too expensive — ~10µs and 8MB per thread)
2. Using a single OS thread for all goroutines (limits parallelism to 1 core)
3. Blocking a thread when a goroutine does I/O (wastes a CPU core waiting on network)
4. Leaving CPU cores idle when there's work to be done
5. Letting one greedy goroutine monopolize a thread forever (starvation)
6. Using a global scheduler lock that all threads must contend for (kills scalability)

The Go scheduler solves all six problems. And it does so with a surprisingly small and elegant design called the **GMP model**.

## Mental Model

GMP stands for three things:

**G — Goroutine**: The unit of concurrent work. A G is the goroutine struct we saw in the previous section — it contains the goroutine's stack, saved program counter, status, and metadata. G is the *what* of the scheduler.

**M — Machine**: An OS thread. The actual executor. An M runs exactly one goroutine at a time. The OS knows about M (it's a real `pthread`), schedules it on a CPU core, and context switches it via the kernel. M is the *who* of the scheduler.

**P — Processor**: A logical CPU context. This is the most interesting and least obvious component. P holds a local run queue of goroutines, a reference to its current M, caches for memory allocation, and other per-CPU state. P is the *bridge* between G and M. P is the *where* of the scheduler.

The key insight: **you need a P to run a goroutine.** An M without a P can't run Go code. A G needs to be scheduled by a P onto an M to execute. `GOMAXPROCS` controls how many Ps exist, which is why it controls parallelism.

```
The GMP relationship:

  G (goroutine) ──── needs a P to run
  P (processor) ──── has a local run queue of Gs, must be paired with an M to execute
  M (OS thread) ──── the actual physical executor, paired with at most one P at a time

  To run Go code: M must have a P, P must have a G.
  P ←──────── M        (M is paired with P)
  │
  └──[G1, G2, G3...]  (P has a local run queue)
      ↑
      G1 is currently running on M via P
```

Full runtime view with GOMAXPROCS=3:

```
┌──────────────────────────────────────────────────────────────────┐
│                        Go Runtime                                 │
│                                                                  │
│   P0                  P1                  P2                    │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │  M0 (thread) │    │  M1 (thread) │    │  M2 (thread) │       │
│  │  running: G1 │    │  running: G5 │    │  running: G8 │       │
│  │  runq:       │    │  runq:       │    │  runq:       │       │
│  │  [G2, G3, G4]│    │  [G6, G7]    │    │  [G9]        │       │
│  └──────────────┘    └──────────────┘    └──────────────┘       │
│                                                                  │
│  Global Run Queue: [G10, G11, G12, G13, G14...]                 │
│                                                                  │
│  Idle Ms (parked): [M3, M4, M5]    (no P, waiting for work)     │
│  Idle Ps (free):   []              (none free right now)         │
└──────────────────────────────────────────────────────────────────┘
```

G1, G5, G8 are executing in **parallel** on 3 OS threads on 3 CPU cores.
G2–G4, G6–G7, G9 are **runnable** in local queues, waiting for G1/G5/G8 to block or be preempted.
G10–G14 are in the **global queue**, waiting for a P to pick them up.

## How it works internally — the P struct

Let's look at what P actually contains (simplified from `runtime/runtime2.go`):

```go
type p struct {
    id          int32
    status      uint32    // Pidle, Prunning, Psyscall, Pgcstop, Pdead
    link        puintptr  // linked list of idle Ps

    m           muintptr  // the M currently running on this P (nil if Pidle)

    // The local run queue — a fixed-size circular buffer.
    // Lock-free! Uses atomic operations on runqhead/runqtail.
    runqhead uint32
    runqtail uint32
    runq     [256]guintptr  // circular buffer, holds up to 256 goroutine pointers

    // runnext is a special slot for the "next" goroutine to run.
    // When a goroutine unblocks another (e.g., by sending on a channel),
    // the newly runnable goroutine goes here — it runs before anything in runq.
    // This is an optimization: the goroutine that was just unblocked often
    // has good cache locality with the unblocking goroutine.
    runnext guintptr

    // Per-P memory allocator cache — avoids malloc contention.
    mcache *mcache

    // sudogcache is a per-P free list of sudog structs (used by channels).
    sudogcache []*sudog

    // Timer heap — P manages timers for goroutines sleeping with time.Sleep,
    // time.After, etc. Having per-P timers avoids a global timer mutex.
    timers     []*timer
    numTimers  atomic.Int32

    // gcBgMarkWorker is the background GC mark worker goroutine for this P.
    gcBgMarkWorker atomic.Pointer[g]

    // Various stats for profiling and debugging
    schedtick  uint32  // incremented on every goroutine schedule
    syscalltick uint32 // incremented on every syscall
    sysmontick sysmontick // last tick observed by sysmon goroutine
}
```

Key design decision: **the local run queue is lock-free**. Because only one M can run on a P at a time, only one thread ever reads/writes the local run queue's head (the consumer side). Other threads can only steal from the tail. This enables using atomic operations instead of a mutex, which is critical for performance.

## The M struct — OS thread details

```go
type m struct {
    g0      *g       // goroutine with scheduling stack
                     // the "special" goroutine that runs scheduler code
    gsignal *g       // signal-handling goroutine

    tls     [tlsSlots]uintptr // thread-local storage
    mstartfn func()   // function to call when thread starts

    curg    *g        // current running goroutine (or nil)
    p       puintptr  // attached P (nil if no P)
    nextp   puintptr  // P that will be attached next (for syscall return)
    oldp    puintptr  // P that was attached before syscall

    id             int64
    mallocing      int32  // are we currently in malloc?
    throwing       throwType
    preemptoff     string // preemption disabled if != ""
    locks          int32  // number of locks held (goroutine must not be preempted)
    dying          int32
    spinning       bool   // is this M spin-waiting for work?
    blocked        bool   // is M blocked on a note?
    newSigstack    bool
    printlock      int8
    incgo          bool   // is M executing a cgo call?
    freeWait       atomic.Uint32
    fastrand       uint64
    needextram     bool
    traceback      uint8
    ncgocall       uint64 // number of cgo calls in total
    ncgo           int32  // number of cgo calls currently in progress
    cgoCallersUse  atomic.Uint32
    cgoCallers     *cgoCallers
    park           note   // for parking M when it has no work
    alllink         *m    // on allm
    schedlink      muintptr
    lockedg        guintptr // G locked to this M (via LockOSThread)
    createstack    [32]uintptr // stack that created this thread
    lockedExt      uint32 // tracking external LockOSThread nesting
    lockedInt      uint32 // tracking internal lockOSThread nesting
    nextwaitm      muintptr // next M waiting for lock
    waitunlockf    func(*g, unsafe.Pointer) bool
    waitlock       unsafe.Pointer
    waittraceev    byte
    waittraceskip  int
    startingtrace  bool
    syscalltick    uint32
    freelink       *m // on sched.freem

    // thread-specific stats
    mOS  // OS-specific thread fields
}
```

The `g0` goroutine is fascinating: every M has a special goroutine called `g0` that has a larger, fixed stack (the system stack). The scheduler itself runs on `g0`. When the scheduler needs to make a decision — which goroutine to run next — it switches to `g0`'s stack, makes the decision, then switches to the chosen goroutine's stack. This separation keeps scheduler code from accidentally growing regular goroutine stacks.

## What happens under the hood — the schedule() loop

Here's what the scheduler does every time a goroutine is scheduled. This is the `schedule()` function in `runtime/proc.go`:

```
schedule():
  top:
    pp := getg().m.p.ptr()   // get current P

    // 1. Check if we should run a GC worker goroutine
    if gp, inheritTime := findRunnable(); gp != nil {
        execute(gp, inheritTime)
    }

findRunnable():
    // Step 1: Every 61 scheduler ticks, check the global run queue.
    // This prevents global queue starvation — if we always prefer local
    // queues, goroutines in the global queue might never run.
    if pp.schedtick % 61 == 0 && sched.runqsize > 0 {
        lock(&sched.lock)
        gp := globrunqget(pp, 1)
        unlock(&sched.lock)
        if gp != nil { return gp, false }
    }

    // Step 2: Check runnext slot (priority — runs before local queue).
    // This goroutine was just made runnable by the current goroutine,
    // so it has good cache locality.
    if gp := pp.runnext.ptr(); gp != nil {
        if pp.runnext.cas(gp, 0) { return gp, true }
    }

    // Step 3: Check P's local run queue (the circular buffer).
    if gp, ok := runqget(pp); ok { return gp, false }

    // Step 4: Check global run queue (needs lock).
    if sched.runqsize != 0 {
        lock(&sched.lock)
        gp := globrunqget(pp, 0)
        unlock(&sched.lock)
        if gp != nil { return gp, false }
    }

    // Step 5: Check the network poller — are any goroutines whose
    // I/O operations have completed ready to run?
    if list, delta := netpoll(0); !list.empty() {
        gp := list.pop()
        injectglist(&list)  // put the rest back
        return gp, false
    }

    // Step 6: Work stealing — try to steal goroutines from other Ps.
    // Try random Ps, steal half their run queue.
    for i := 0; i < 4; i++ {
        otherP := randomP()
        if gp := runqsteal(pp, otherP, true); gp != nil {
            return gp, false
        }
    }

    // Step 7: If still nothing, do a more thorough scan of all Ps.
    // Check global queue, network poller again.
    // If we still find nothing, park this M.
    stopm()  // park M, release P, wait for work signal
```

The **"every 61 ticks check global queue"** is a classic fairness mechanism. Without it, goroutines in the global queue (put there during syscall handoffs) would starve if every P was constantly busy with local work. The number 61 is prime, which helps avoid synchronization patterns between multiple Ps.

## Work Stealing — Deep Dive

Work stealing is the heart of Go's load balancing. Here's exactly how it works:

```go
// runqsteal steals goroutines from victim P's run queue into pp's run queue.
// It returns one goroutine to run immediately.
func runqsteal(pp, victim *p, stealRunNextG bool) *g {
    t := pp.runqtail.Load()
    n := runqgrab(victim, &pp.runq, t, stealRunNextG)
    if n == 0 { return nil }
    n--
    // The last goroutine we grabbed is the one we'll return to run
    gp := pp.runq[(t+n)%uint32(len(pp.runq))].ptr()
    pp.runqtail.Store(t + n)
    return gp
}

// runqgrab grabs a batch of goroutines from victim's queue.
// It grabs up to n/2 goroutines (half the queue).
func runqgrab(victim *p, batch *[256]guintptr, batchHead uint32, stealRunNextG bool) uint32 {
    for {
        h := victim.runqhead.Load()  // load head atomically
        t := victim.runqtail.Load()  // load tail atomically
        n := t - h                   // number of goroutines in victim's queue
        n = n - n/2                  // steal half
        if n == 0 {
            // Try to steal victim's runnext goroutine
            if stealRunNextG {
                if next := victim.runnext.Load(); next != 0 {
                    if !victim.runnext.cas(next, 0) { continue }
                    batch[batchHead%uint32(len(batch))] = next
                    return 1
                }
            }
            return 0
        }
        // ... copy n goroutines from victim's runq into batch
        if victim.runqhead.cas(h, h+n) { // atomic CAS to claim the steal
            return n
        }
        // CAS failed — someone else modified the queue, retry
    }
}
```

The beauty of this: the steal is **lock-free**. It uses atomic compare-and-swap on the queue head. If two Ps try to steal from the same victim simultaneously, one will win the CAS and the other will retry (either finding less work, or trying a different victim).

Work stealing diagram:

```
Before steal:
  P0: runq=[        ] (empty, M0 idle, spinning)
  P1: runq=[G3,G4,G5,G6,G7,G8,G9,G10] (busy)

P0 tries to steal from P1:
  1. Read P1.runqhead (atomic load) = 0
  2. Read P1.runqtail (atomic load) = 8
  3. n = 8, steal = n - n/2 = 4
  4. Copy G3,G4,G5,G6 to P0's run queue
  5. CAS P1.runqhead from 0 to 4 (atomic)

After steal:
  P0: runq=[G3,G4,G5,G6] → runs G3 immediately
  P1: runq=[G7,G8,G9,G10] → continues with remaining work
```

This keeps CPU utilization high even when work is unevenly distributed across Ps.

## Preemption — How Go Stops a Goroutine

### Before Go 1.14: Cooperative Preemption

Originally, Go used cooperative preemption — goroutines yielded to the scheduler only at **function call sites** (and a few other safe points like channel operations and memory allocation).

The compiler inserts a preemption check at the start of every function (via the same mechanism as stack growth checks). If `g.preempt == true`, the goroutine yields.

**Problem**: a goroutine with a tight CPU loop and no function calls (or only inlined function calls) could monopolize a thread indefinitely:

```go
// This would starve all other goroutines in Go < 1.14
func monopolize() {
    x := 0
    for {
        x++ // no function calls, inlined or absent
            // preemption check never happens
            // this goroutine runs forever
    }
}
```

### Go 1.14+: Asynchronous Preemption

Go 1.14 introduced **signal-based preemption** (asynchronous preemption). Here's how it works:

1. The **sysmon goroutine** (a special background goroutine) periodically scans all Ps
2. If a goroutine has been running for more than **~10ms** (10,000µs — one "scheduling tick"), sysmon signals its OS thread with **SIGURG** (on Unix) or uses a special mechanism on Windows
3. The OS thread's signal handler saves the current goroutine's register state
4. The signal handler calls `runtime.asyncPreempt` which marks the goroutine as preemptible
5. At the next safe point (where goroutine stack is in a consistent state), the goroutine is preempted and the scheduler runs

This solves the monopolization problem completely — even tight CPU loops are preempted.

```go
// This is now safe in Go 1.14+ — SIGURG preempts it every ~10ms
func monopolize() {
    for { /* tight loop */ }
}
```

**Why SIGURG specifically?** It's an obscure signal (originally for urgent data on sockets) that most programs don't use, and CGo code handles it as a no-op. This makes it safe to send to any Go thread without interfering with normal signal handling.

## Syscall Handling — The Handoff Protocol

This is one of the most important and subtle aspects of the Go scheduler. Let's trace through exactly what happens when a goroutine makes a blocking syscall.

**Scenario**: G1 is running on P0's M0. G1 calls `read(fd, buf, len)` — a blocking system call.

**Step 1: Enter syscall** — `runtime.entersyscall()` is called (injected by the compiler before every syscall):

```
G1 on P0-M0 about to enter syscall:

  P0.status = Psyscall  (P0 marks itself as in syscall)
  G1.status = Gsyscall  (G1 marks itself as in syscall)
  G1.m = M0             (G1 remembers its thread)
  M0.oldp = P0          (M0 remembers its P for after syscall)
  
  P0 is now "detached" from M0 but not yet handed off.
  If the syscall returns quickly, P0 will reattach to M0.
```

**Step 2: The sysmon goroutine watches** for Ps stuck in `Psyscall`. If a P has been in `Psyscall` for more than ~20µs:

```
sysmon detects P0 has been in syscall too long:

  1. Find an idle M (or create a new one) → M1
  2. P0.status = Pidle
  3. M0.nextp = nil    (M0 no longer owns P0)
  4. M1.nextp = P0    (M1 will pick up P0 when M0's syscall finishes or now)
  5. Signal M1 to start running

Now:
  P0 ──── M1 ──── G2  (other goroutines keep running!)
          M0 ──── G1  (blocked in kernel, no P attached)
```

**Step 3: Syscall returns** — `runtime.exitsyscall()` is called:

```
M0 returns from syscall, G1 is Grunnable again:

  Option A: A P is available (P was idle):
    G1 attaches to that P, continues running immediately.
    
  Option B: No P available (all Ps busy):
    G1.status = Grunnable
    G1 goes to the global run queue
    M0 parks (spins for a bit, then sleeps)
    G1 waits to be picked up by work stealing or global queue check
```

This is the **M:N threading model** in action. Multiple goroutines (N) are multiplexed onto multiple OS threads (M). The ratio N:M can be millions:thousands.

**Why not just create a new OS thread per goroutine?** Thread creation is expensive (~10µs, 8MB memory), and the OS can only efficiently schedule a limited number of threads. Having 100,000 OS threads on a 4-core machine would cause massive context switch overhead and memory pressure.

**Why not have one OS thread total?** A single OS thread can only use one CPU core. No parallelism. Also, blocking syscalls would pause the entire program.

The GMP model threads the needle: a small, bounded set of OS threads (typically `GOMAXPROCS` active + a few more for syscalls in progress), achieving both parallelism and efficient blocking syscall handling.

## The sysmon Goroutine — The Background Watchdog

`sysmon` is a special goroutine in the Go runtime that never runs on a P (it uses the system stack directly). It wakes up periodically (every 20µs initially, backing off to 10ms when the program is quiet) and does:

1. **Preemption checks**: finds goroutines that have been running too long, signals their M with SIGURG
2. **Syscall handoff**: finds Ps stuck in `Psyscall`, triggers handoffs to new Ms
3. **Network poller**: polls for completed I/O and injects ready goroutines into run queues
4. **Timer management**: checks if any timers have expired, makes their goroutines runnable
5. **GC scavenging**: periodically returns memory pages to the OS

`sysmon` is the heartbeat of the runtime. Without it, goroutines would never be preempted, syscall-blocked threads would never hand off their P, and timers would never fire.

## Real Production Example

Here's a scenario where understanding GMP saved us from a production disaster.

We had a Go service doing heavy JSON parsing — CPU-bound work. We were running it in Kubernetes with a 4-CPU limit on a 64-core host. The service had 50 goroutines in its worker pool, all doing CPU work.

**Problem**: `GOMAXPROCS` defaulted to 64 (the host core count). So we had 64 Ps, but only 4 real CPU cores available to our container. Each P was spinning (trying to find work or stealing), and we had 64 OS threads competing for 4 CPUs. The context switch overhead from 64 threads was enormous — p99 latency was 500ms.

**Diagnosis**: `go tool pprof` showed most time in `runtime.schedule` and `runtime.futex` — scheduling overhead, not actual work.

**Fix 1**: `import _ "go.uber.org/automaxprocs"` → `GOMAXPROCS=4`. Now 4 Ps, 4 OS threads, no spinning. p99 dropped to 50ms.

**Fix 2**: Worker pool size = `runtime.NumCPU()` (now returns 4 after automaxprocs). No more over-subscription.

Understanding that P count equals parallelism and that spinning Ps waste CPU was essential. The fix was one import and one number.

## Common Mistakes

**Mistake 1: Not using `automaxprocs` in Kubernetes.**

This is the single highest-ROI configuration change for containerized Go services. Without it, `GOMAXPROCS = host CPU count`, not container CPU limit.

```go
import _ "go.uber.org/automaxprocs"
```

**Mistake 2: `runtime.Gosched()` abuse.**

`runtime.Gosched()` yields the current goroutine, letting other goroutines run. It's occasionally useful in tight CPU loops to ensure fairness, but it's NOT a substitute for proper synchronization and should not be used to "fix" concurrency bugs.

**Mistake 3: `runtime.LockOSThread()` misuse.**

`runtime.LockOSThread()` locks the current goroutine to its current OS thread — the goroutine will only run on that M. This is needed for some CGo operations or OS-thread-specific APIs (like OpenGL). But if you lock goroutines to threads without a clear reason, you break the scheduler's ability to load-balance and may exhaust the thread pool.

**Mistake 4: Blocking the sysmon goroutine.**

Operations like `runtime.GC()` (manual GC trigger) hold the world for sysmon. In production, trust the GC tuner and avoid manual GC calls.

## Interview Questions

**Q: What is the GMP model in Go's scheduler?**

G (Goroutine), M (Machine/OS thread), P (Processor/logical CPU). Ps are the bridge between Gs and Ms. `GOMAXPROCS` controls the number of Ps, which limits parallelism. Each P has a local lock-free run queue of goroutines. Ms execute goroutines by pairing with a P. Work stealing balances load across Ps.

**Q: What is work stealing and why does Go use it?**

When a P's local run queue is empty, it "steals" half the goroutines from another P's run queue using lock-free atomic CAS operations. This keeps all CPU cores busy even when work is unevenly distributed. It's O(1) and doesn't require a global scheduler lock.

**Q: What is the `g0` goroutine?**

Every OS thread (M) has a special goroutine called `g0` that uses the system stack (larger, fixed size). Scheduler code runs on `g0`'s stack. When the scheduler decides which goroutine to run next, it switches to `g0`, makes the decision, then switches to the target goroutine's stack. This separation prevents scheduler code from growing regular goroutine stacks.

**Q: How does Go handle a goroutine that makes a blocking syscall?**

The goroutine's P detaches from its M (the M stays in the kernel for the syscall). The P gets picked up by another M (idle or newly created) and keeps running other goroutines. When the syscall returns, the M tries to find a free P; if none is available, the goroutine goes to the global run queue and the M parks.

**Q: What is `GOMAXPROCS` and what happens if you set it wrong?**

`GOMAXPROCS` sets the number of Ps (logical processors). If too low: you underutilize available CPU cores. If too high (common in containers): more Ps and OS threads than real CPU cores, causing excessive context switch overhead. In containers, use `go.uber.org/automaxprocs` to read the actual CPU limit.

**Q: What changed in Go 1.14 regarding preemption?**

Before 1.14: goroutines were only preempted at function call sites (cooperative preemption). Tight CPU loops could monopolize a thread. After 1.14: asynchronous preemption via OS signals (SIGURG on Linux) — the sysmon goroutine sends SIGURG to any thread running a goroutine for more than ~10ms, forcing preemption even in tight loops.

## Key Takeaways

- GMP = Goroutines mapped onto OS threads via logical processors; Ps limit parallelism
- Local run queues are lock-free circular buffers of 256 slots — the fast path
- Work stealing keeps all cores busy with O(1) atomic CAS, no global lock
- Syscall handoff: P detaches from the blocked M, runs other goroutines on a new M
- `g0` is the scheduler's stack — all scheduling decisions happen there
- `GOMAXPROCS` should match your actual CPU count (use `automaxprocs` in containers)
- Async preemption (Go 1.14+) via SIGURG — no goroutine can monopolize a thread indefinitely
- `sysmon` is the background watchdog: preemption, I/O polling, timer management

---

# 4. Channels

## What problem does it solve?

Let's understand why channels exist, and what problem they're solving at a philosophical level before we look at the mechanics.

The traditional approach to concurrent programming is **shared memory with locks**. You have a piece of data. Multiple goroutines want to read and write it. You put a mutex around it. Everyone locks before accessing, unlocks after. Done.

This works, but it has deep problems:

1. **Implicit ownership**: any goroutine can access the shared data at any time (if they remember to lock). There's no clear owner.
2. **Lock complexity**: as your program grows, tracking which locks protect which data becomes a full-time job.
3. **Deadlock risk**: acquiring multiple locks in the wrong order deadlocks your program.
4. **Race conditions**: forgetting to lock anywhere in the codebase is a silent bug.

The alternative philosophy — Go's philosophy — is **message passing**: instead of sharing memory and adding locks, you transfer ownership of data from one goroutine to another. At any moment, only one goroutine has the data. Nobody fights over it.

> **"Don't communicate by sharing memory; share memory by communicating."**
> — Go's core concurrency proverb

A channel is the mechanism for this. It's a typed conduit through which goroutines pass values. The sender puts data into the channel; the receiver takes data out. Between the send and the receive, the channel owns the data — not either goroutine.

## Mental Model

Imagine a pipe between two people:

```
Sender goroutine                 Channel                 Receiver goroutine
      │                                                         │
      │                    ┌──────────────┐                     │
      │── value ──────────▶│   value      │──────────── value ──▶│
      │                    └──────────────┘                     │
      │                          ↑                              │
      │                    Data lives here                      │
      │                    while in transit                     │
```

**Unbuffered channel** (`make(chan T)`): The pipe has zero capacity. The sender must wait until a receiver is ready to accept. They must both be present at the pipe at the same time. This is a **synchronization rendezvous** — a meeting point.

```
Unbuffered channel — rendezvous:
  Sender waits here ────▶ [    ] ◀──── Receiver waits here
                          ^ pipe has no buffer
                          Both must arrive before either can leave
```

**Buffered channel** (`make(chan T, N)`): The pipe has N slots. The sender can put up to N values in without a receiver present. The receiver can take values out even if the sender is gone (as long as there are buffered values). Producer and consumer are decoupled.

```
Buffered channel (capacity 3):
  Sender:   ──▶ [v1][v2][  ] ◀── Receiver draining values
                 ↑ filled    ↑ empty slot
  Sender can send 1 more value without waiting.
  If full: sender blocks. If empty: receiver blocks.
```

## How it works internally

A channel is implemented as the `hchan` struct in `runtime/chan.go`. Let me walk through every field:

```go
// From runtime/chan.go
type hchan struct {
    // qcount is the number of elements currently queued (in the buffer).
    qcount uint

    // dataqsiz is the total capacity of the circular buffer.
    // 0 for unbuffered channels.
    dataqsiz uint

    // buf is a pointer to the circular ring buffer.
    // Only valid for buffered channels (dataqsiz > 0).
    // The buffer holds dataqsiz elements, each of elemsize bytes.
    buf unsafe.Pointer

    // elemsize is the size of each element in bytes.
    // For chan struct{}: elemsize = 0
    // For chan int64:    elemsize = 8
    // For chan string:   elemsize = 16 (two words: pointer + length)
    elemsize uint16

    // closed is 1 if the channel is closed, 0 otherwise.
    // Set atomically. Read with atomic.Load.
    closed uint32

    // elemtype is the type of elements in the channel.
    // Used by the garbage collector for pointer scanning.
    elemtype *_type

    // sendx is the index in the circular buffer where the next send will go.
    sendx uint

    // recvx is the index in the circular buffer where the next receive will read.
    recvx uint

    // recvq is the list of goroutines blocked waiting to receive from this channel.
    // These goroutines are in _Gwaiting state.
    recvq waitq

    // sendq is the list of goroutines blocked waiting to send to this channel.
    // These goroutines are in _Gwaiting state.
    sendq waitq

    // lock protects all fields in hchan, as well as several fields in sudogs
    // blocked on this channel.
    // Do not change another G's status while holding this lock
    // (in particular, do not ready a G), as this can deadlock
    // with stack shrinking.
    lock mutex
}

// waitq is a linked list of goroutines blocked on a channel.
type waitq struct {
    first *sudog
    last  *sudog
}

// sudog represents a goroutine g waiting in a list
// (e.g. for sending/receiving on a channel).
// A sudog is allocated from a special pool to avoid excessive GC pressure.
type sudog struct {
    g        *g          // the goroutine
    next     *sudog      // next in waitq
    prev     *sudog      // previous in waitq
    elem     unsafe.Pointer // data element being sent/received
    acquiretime int64
    releasetime int64
    ticket   uint32
    parent   *sudog      // semaRoot binary tree
    waitlink *sudog
    waittail *sudog
    c        *hchan      // channel
}
```

Memory layout of a buffered channel (capacity 4, element type `int64`):

```
hchan struct in heap:
┌─────────────────────────────────────────────────────┐
│ qcount:   2    (2 elements currently in buffer)      │
│ dataqsiz: 4    (capacity 4)                          │
│ buf:      ─────────────────────────────────────┐     │
│ elemsize: 8    (int64 = 8 bytes)               │     │
│ closed:   0                                    │     │
│ sendx:    3    (next send goes to index 3)     │     │
│ recvx:    1    (next receive reads index 1)    │     │
│ recvq:    {}   (no waiting receivers)          │     │
│ sendq:    {}   (no waiting senders)            │     │
│ lock:     (unlocked)                           │     │
└────────────────────────────────────────────────┘     │
                                                       │
                  ring buffer (4 slots × 8 bytes):     │
                  ┌──────────┬──────────┬──────────┬──────────┐
                  │  slot 0  │  slot 1  │  slot 2  │  slot 3  │
           buf ──▶│ (old val)│ 42 ◀recv │ 99       │ (empty)  │
                  └──────────┴──────────┴──────────┴──────────┘
                               ↑ recvx=1              ↑ sendx=3
```

## What happens under the hood — every operation in detail

### Sending on an unbuffered channel

```go
ch := make(chan int)
ch <- 42
```

Here's what `runtime.chansend()` does:

**Case 1: A receiver is already waiting** (the fast path — direct transfer)

```
1. Lock hchan.lock
2. Check recvq — is there a waiting receiver? YES
3. Pop the first sudog from recvq
4. The sudog has a pointer to the receiver goroutine's stack slot where it wants the value
5. Copy 42 DIRECTLY from the sender's stack to the receiver's stack slot
   — the value never goes into the channel buffer!
   — this is "direct send" optimization
6. Change receiver's status from _Gwaiting to _Grunnable
7. Put receiver into the current P's runnext slot (priority run)
8. Unlock
9. Return immediately — sender is NOT blocked

Memory movement:
  Sender stack: [42]  ──────direct copy──────▶  Receiver stack: [42]
                              Channel buffer: never touched
```

This direct copy optimization is beautiful. When both goroutines are present, the value goes straight from one stack to another. The channel buffer is bypassed entirely.

**Case 2: No receiver waiting, buffer has space** (buffered channel)

```
1. Lock hchan.lock
2. recvq is empty, but buf has space (qcount < dataqsiz)
3. Copy 42 to buf[sendx]
4. sendx = (sendx + 1) % dataqsiz
5. qcount++
6. Unlock
7. Return immediately — sender is NOT blocked
```

**Case 3: No receiver waiting, buffer is full (or unbuffered)**

```
1. Lock hchan.lock
2. recvq is empty, buf is full (or no buf)
3. Create a sudog for the current goroutine
4. sudog.elem = pointer to 42 (the value to send)
5. Add sudog to sendq
6. Call gopark() — changes goroutine status to _Gwaiting
7. Unlock
8. Goroutine is now parked — NOT running, NOT burning CPU
   It will be woken when a receiver drains the buffer or arrives
```

### Receiving from a channel

```go
v := <-ch
```

Symmetric to sending:

**Case 1: A sender is waiting in sendq** (direct transfer)
- Pop sender's sudog from sendq
- Copy value directly from sender's stack (or sudog.elem) to receiver's local variable
- Wake the sender goroutine

**Case 2: Buffer has data**
- Copy from buf[recvx] to receiver's local variable
- recvx = (recvx + 1) % dataqsiz
- qcount--
- If sendq is non-empty: copy the first waiting sender's value into the now-free buffer slot and wake that sender

**Case 3: Buffer empty, no sender**
- Create a sudog for the receiver goroutine
- Add to recvq
- gopark() — goroutine parked until a sender arrives

### Closing a channel

```go
close(ch)
```

`runtime.closechan()`:

1. Lock hchan.lock
2. If already closed: panic ("close of closed channel")
3. Set hchan.closed = 1
4. Collect all goroutines in recvq into a list
5. Collect all goroutines in sendq into a list
6. Unlock
7. For each goroutine in recvq: set their receive result to (zero value, false), make runnable
8. For each goroutine in sendq: panic them ("send on closed channel" — actually this panics at the send site)

Wait — what happens when a goroutine in sendq is woken after a close? The runtime panics on their behalf because the invariant "you sent to a closed channel" must be reported.

## Channel Behavior Table — The Complete Truth

This table is a **favorite interview question** and every Go engineer must know it cold:

```
Channel State    │  Operation  │  Result
─────────────────┼─────────────┼────────────────────────────────────
nil              │  send       │  blocks forever (deadlock if only goroutine)
nil              │  receive    │  blocks forever (deadlock if only goroutine)
nil              │  close      │  panic: close of nil channel
                 │             │
open, empty      │  send       │  blocks until receiver ready
open, empty      │  receive    │  blocks until sender sends
open, empty      │  close      │  closes the channel successfully
                 │             │
open, with data  │  send       │  may block (if buffer full) or proceed
open, with data  │  receive    │  returns data, ok=true (never blocks if buf non-empty)
open, with data  │  close      │  closes; buffered data still readable
                 │             │
open, full buf   │  send       │  blocks until a receiver drains a slot
open, full buf   │  receive    │  proceeds, returns value, ok=true
open, full buf   │  close      │  closes; all buffered data still readable
                 │             │
closed           │  send       │  PANIC: send on closed channel
closed           │  receive    │  returns (zero value, false) immediately, NEVER blocks
closed           │  close      │  PANIC: close of closed channel
```

The "closed channel receive returns immediately" behavior is crucial for range loops:

```go
for v := range ch {
    // This loop exits when ch is closed AND drained
    // It does NOT exit just because ch is closed — it drains first
    process(v)
}
```

The two-value receive idiom:

```go
v, ok := <-ch
if !ok {
    // channel is closed and empty
    return
}
// ok == true means we got a real value
```

## Nil Channels — The Useful Edge Case

Nil channels are not just an error condition — they're a genuinely useful tool.

A nil channel:
- Send: blocks forever (never proceeds)
- Receive: blocks forever (never proceeds)

In a `select` statement, a case on a nil channel is **never selected**. This lets you dynamically enable and disable select cases:

```go
func mergeChannels(ch1, ch2 <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        // Keep going until both channels are nil (closed and drained)
        for ch1 != nil || ch2 != nil {
            select {
            case v, ok := <-ch1:
                if !ok {
                    ch1 = nil // set to nil to "disable" this case
                    continue
                }
                out <- v
            case v, ok := <-ch2:
                if !ok {
                    ch2 = nil // set to nil to "disable" this case
                    continue
                }
                out <- v
            }
        }
    }()
    return out
}
```

Once `ch1` is set to nil, the `case v, ok := <-ch1` never fires — it blocks forever, so select ignores it. This is one of Go's most elegant idioms.

## Directional Channels

Go supports directional channel types:

```go
chan T      // bidirectional: can send and receive
<-chan T    // receive-only: can only receive from this channel
chan<- T    // send-only: can only send to this channel
```

Directional channels are used in function signatures to express intent and prevent misuse:

```go
// producer only sends — chan<- prevents producer from receiving or closing
func producer(out chan<- Job, ctx context.Context) {
    for _, job := range jobs {
        select {
        case out <- job:
        case <-ctx.Done(): return
        }
    }
    close(out) // producer closes: this is valid, chan<- allows close
}

// consumer only receives — <-chan prevents consumer from sending or closing
func consumer(in <-chan Job, results chan<- Result) {
    for job := range in { // range works on receive-only channel
        results <- process(job)
    }
}
```

The compiler enforces these restrictions at compile time. Attempting to send on a `<-chan T` is a compile error.

## Real Production Example

Here's a real pattern from our event processing service. We needed to fan work out to processors and collect results, with proper backpressure:

```go
type Event struct {
    ID      string
    Payload []byte
}

type Result struct {
    EventID string
    Status  string
    Err     error
}

func processEvents(ctx context.Context, events []Event, concurrency int) []Result {
    // Buffered channel for backpressure — don't overwhelm processors
    jobs := make(chan Event, concurrency*2)
    results := make(chan Result, len(events))

    // Start worker pool
    var wg sync.WaitGroup
    for i := 0; i < concurrency; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case event, ok := <-jobs:
                    if !ok {
                        return // jobs channel closed, worker exits
                    }
                    result := Result{EventID: event.ID}
                    if err := process(ctx, event); err != nil {
                        result.Status = "error"
                        result.Err = err
                    } else {
                        result.Status = "ok"
                    }
                    results <- result
                case <-ctx.Done():
                    return // context cancelled, worker exits
                }
            }
        }()
    }

    // Feed jobs — runs in a separate goroutine so we don't block main
    go func() {
        defer close(jobs) // signal workers to exit when jobs are exhausted
        for _, event := range events {
            select {
            case jobs <- event: // may block if buffer full — backpressure!
            case <-ctx.Done():
                return
            }
        }
    }()

    // Close results when all workers are done
    go func() {
        wg.Wait()
        close(results)
    }()

    // Collect results
    var allResults []Result
    for result := range results {
        allResults = append(allResults, result)
    }
    return allResults
}
```

The buffered `jobs` channel with `concurrency*2` capacity gives us natural backpressure: the feeder can stay ahead of workers by up to `2×concurrency` jobs, but it won't run unboundedly far ahead.

## Common Mistakes

**Mistake 1: Sending on a closed channel — panics.**

The most common channel bug in production. The rule is: **only the goroutine that sends should close the channel.** The receiver should never close.

```go
// Bad: receiver closes the channel
func receiver(ch chan int) {
    v := <-ch
    close(ch) // WRONG — sender might be about to send
}

// Good: sender signals completion by closing
func sender(ch chan int) {
    for _, v := range data {
        ch <- v
    }
    close(ch) // sender closes when done — safe
}

// For multiple senders: use sync.Once or a WaitGroup + separate closer goroutine
func multiSender(ch chan int, senders int) {
    var wg sync.WaitGroup
    for i := 0; i < senders; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            ch <- generateValue()
        }()
    }
    go func() {
        wg.Wait()
        close(ch) // close only after ALL senders are done
    }()
}
```

**Mistake 2: Ranging over a channel that never closes — goroutine leak.**

```go
// BUG: if nobody closes jobs, this goroutine blocks forever
func worker(jobs <-chan Job) {
    for job := range jobs { // range loops until channel closes
        process(job)
    }
}

// Fix: close the jobs channel when done sending
close(jobs) // this makes worker's range loop exit
```

**Mistake 3: Thinking buffered channels prevent deadlocks.**

```go
ch := make(chan int, 3)
ch <- 1
ch <- 2
ch <- 3
ch <- 4  // DEADLOCKS — buffer is full, nobody is reading
```

A buffer only delays the block. If nobody drains the buffer, you still deadlock.

**Mistake 4: Not using the two-value receive to detect channel closure.**

```go
v := <-ch
// If ch is closed and empty, v is the zero value of T
// You can't tell if v==0 was an actual value or a closed-channel zero

v, ok := <-ch
if !ok { /* channel is closed and empty */ }
// ok=false definitively means closed
```

**Mistake 5: Using channels for simple counters or accumulators.**

```go
// Overcomplicated: channel for a counter
counter := make(chan int, 1)
counter <- 0
// ... later
v := <-counter
counter <- v + 1

// Simple and correct: just use atomic
var counter atomic.Int64
counter.Add(1)
```

Channels shine for ownership transfer and signaling. For shared numeric state, use `sync/atomic` or a `Mutex`.

**Mistake 6: Blocking the main goroutine on a channel with no sender.**

```go
func main() {
    ch := make(chan int)
    // Forgot to start a sender goroutine
    v := <-ch // deadlock — runtime panics: "all goroutines are asleep"
}
```

## Performance Implications

Channel operations are not free. Here are approximate costs on modern hardware:

```
Operation                                          │  Approximate cost
───────────────────────────────────────────────────┼──────────────────
Unbuffered send+receive (both goroutines present)  │  ~200-300ns
Buffered send (buffer not full, no waiter)         │  ~50-100ns
Buffered receive (buffer not empty, no waiter)     │  ~50-100ns
Blocked send (goroutine parks and wakes)           │  ~500ns-1µs
Blocked receive (goroutine parks and wakes)        │  ~500ns-1µs
sync/atomic Add (for comparison)                   │  ~5-15ns
sync/Mutex Lock (uncontended)                      │  ~15-25ns
```

The channel operations that require goroutine parking and waking are ~10-20x slower than unblocked operations. If you're doing high-frequency message passing (millions/sec), consider:

1. **Batching**: send slices/arrays instead of individual values
2. **Ring buffers**: lock-free ring buffers like `golang.org/x/sync/semaphore` for ultra-high-throughput
3. **sync/atomic**: for simple signaling (counters, flags) avoid channels entirely
4. **sync.Mutex**: for protecting shared state, often simpler and faster than channels

## Scheduler Integration — How Channel Ops Interact with GMP

When a goroutine blocks on a channel operation:

```
G1 tries to send on full channel ch:
  1. runtime.chansend() is called
  2. ch.lock is acquired
  3. No receiver, buffer full
  4. Create sudog for G1, add to ch.sendq
  5. Call gopark(chanparkcommit, ch, waitReasonChanSend, ...)
     - gopark sets G1.status = _Gwaiting
     - gopark puts G1 on no run queue (it's in ch.sendq instead)
     - gopark calls schedule() on g0 to pick the next goroutine to run
  6. G1 is now _Gwaiting — not consuming any CPU

G2 receives from ch, waking G1:
  1. runtime.chanrecv() finds G1 in ch.sendq
  2. Copies G1's value directly to G2's result variable
  3. Calls goready(G1):
     - Sets G1.status = _Grunnable
     - Puts G1 in the current P's runnext slot
  4. G1 will run as soon as G2 yields or the scheduler picks it
```

The `waitReason` field in the `g` struct captures *why* a goroutine is blocked. When you do `go tool pprof` or look at a goroutine dump, you'll see these reasons:
- `chan receive (nil chan)` — receiving from a nil channel
- `chan send` — blocked on channel send
- `chan receive` — blocked on channel receive
- `select` — blocked in a select statement

## Interview Questions

**Q: What is the difference between a buffered and unbuffered channel?**

An unbuffered channel (`make(chan T)`) has no storage — a send blocks until a receiver is ready, and vice versa. It's a synchronization rendezvous: both goroutines must be present. A buffered channel (`make(chan T, N)`) can hold N values without a receiver. A send only blocks when the buffer is full; a receive only blocks when the buffer is empty. Buffered channels decouple the producer from the consumer.

**Q: What happens when you receive from a closed channel?**

You immediately get the zero value of the channel's element type, and the `ok` value (in a two-value receive) is `false`. A receive from a closed channel never blocks, even if the channel is empty. Buffered data in a closed channel is still readable — you drain the buffer before getting zero values.

**Q: What happens when you send on a closed channel?**

Panic: "send on closed channel." This is immediate and always a bug. The convention is that only the sender(s) close a channel, and they do so only after they've sent everything.

**Q: Why does direct send (bypassing the buffer) happen in Go's channel implementation?**

When a receiver is already waiting in `recvq`, the sender copies the value directly to the receiver's stack. This optimizes cache locality (the sender and receiver may be on the same core or neighboring cores) and avoids the double-copy (sender→buffer→receiver) that would happen otherwise. It also means the value is never in the heap — it goes directly from one stack to another.

**Q: What is the channel behavior table for nil channels?**

Send to nil: blocks forever. Receive from nil: blocks forever. Close nil: panic. This is useful in `select` statements: a nil channel case is never selected, letting you dynamically disable select cases.

**Q: Can multiple goroutines receive from the same channel?**

Yes. When multiple goroutines are waiting to receive from the same channel, they form a queue (`recvq`). When a value is sent, exactly one receiver (the first in the queue) gets it. This is how worker pools work — multiple workers receive from a shared jobs channel.

## Key Takeaways

- Channels implement CSP (Communicating Sequential Processes) — transfer ownership, don't share memory
- Unbuffered = synchronization rendezvous. Buffered = decoupled producer/consumer
- Direct send optimization: when receiver is waiting, value goes directly from sender's stack to receiver's stack (no buffer touch)
- Closing rules: only senders close, close only once, never send after close
- Nil channel in select = disabled case — powerful idiom for dynamic case management
- Channel receive from closed = zero value + ok=false — drain before getting zeros
- Use directional channel types (`<-chan T`, `chan<- T`) in function signatures to express intent
- Channels have non-trivial cost — batch work or use atomics for ultra-high-frequency signaling

---

# 5. Select

## What problem does it solve?

Here's a scenario you'll hit constantly in real services: you have a goroutine that needs to do one of several things — whichever is ready first. Maybe it's waiting on a database response, but also watching for a timeout, and also listening for a cancellation signal. You don't know which will arrive first.

Without `select`, you'd have to pick one thing to wait on. You'd have to write complex logic to poll multiple channels with non-blocking receives. It would be messy, inefficient, and racy.

`select` solves this elegantly. It's Go's multiplexer for channel operations. A `select` statement picks whichever channel case is ready, and blocks if none are.

```
Without select (awkward):
  for {
      // try ch1, if not ready, try ch2, if not ready, loop
      // this burns CPU spinning and has timing issues
  }

With select (clean):
  select {
  case v := <-ch1: ...  // fires when ch1 has data
  case v := <-ch2: ...  // fires when ch2 has data
  case ch3 <- x:   ...  // fires when ch3 can accept
  case <-time.After(5s): // fires after 5 second timeout
  }
  // blocks until exactly one case fires, then continues
```

Every real Go service uses `select` constantly. Timeouts, cancellation, multiplexing inputs, non-blocking operations — all of these use `select`. Master it deeply.

## Mental Model

Think of `select` as a switch statement where each case is a channel operation, and the program picks whichever case is "ready" right now. If multiple cases are ready simultaneously, it picks one at random. If none are ready, it blocks until one becomes ready (or it can fall through to a `default` case if one exists).

```
select {
case <-done:      ← is this channel readable? (closed or has data)
case msg := <-ch: ← is this channel readable?
case out <- val:  ← is this channel writable? (not full)
default:          ← always ready — executes if no other case is ready
}
```

The **randomness** when multiple cases are ready is important and intentional. It prevents starvation — if Go always picked the first ready case, a constantly-ready case would starve all others.

## How it works internally

`select` is compiled into calls to `runtime.selectgo()`. This is a surprisingly complex runtime function. Let's trace through it.

### Compilation

The Go compiler transforms a `select` statement into a call to `runtime.selectgo()` with an array of `scase` structs:

```go
// Each case in a select statement becomes a scase.
// From runtime/select.go:
type scase struct {
    c    *hchan         // the channel for this case
    elem unsafe.Pointer // pointer to send/receive value
}
```

### selectgo() — the full algorithm

```go
// Simplified pseudocode of runtime.selectgo()
func selectgo(cas0 *scase, order0 *uint16, pc0 *uintptr, nsends int, nrecvs int, block bool) (int, bool) {
    // cas0: array of scases (send cases first, then receive cases)
    // order0: scratch space for shuffle and lock ordering
    // nsends: number of send cases
    // nrecvs: number of receive cases  
    // block: false if there's a default case (non-blocking)

    // STEP 1: Create a random permutation of the cases.
    // This is the source of select's pseudorandom case selection.
    // Uses Fisher-Yates shuffle with a per-goroutine pseudorandom source.
    pollorder := order0[:ncases]
    lockorder := order0[ncases:]
    // shuffle pollorder randomly
    for i := range pollorder {
        j := fastrandn(uint32(i + 1))
        pollorder[i] = pollorder[j]
        pollorder[j] = uint16(i)
    }

    // STEP 2: Sort channels by address for consistent lock ordering.
    // This prevents deadlocks when select locks multiple channel mutexes.
    // Always lock channels in the same order, regardless of how select
    // cases are written.
    // (heap sort by channel pointer address)
    for i := range lockorder { ... }

    // STEP 3: Lock all channels in lockorder.
    // We must hold all channel locks while examining their state.
    for _, casei := range lockorder {
        sellock(scases, lockorder)
    }

    // STEP 4: Pass 1 — look for a channel that's immediately ready.
    // Iterate in pollorder (random order) to check each case.
    var casi int
    var cas *scase
    var caseSuccess bool
    var recvOK bool
    for _, casei := range pollorder {
        k := &scases[casei]
        if k.c == nil { continue } // nil channel — skip

        if casei < nsends {
            // This is a send case: check if receiver is waiting or buffer has space
            sg = k.c.recvq.dequeue()
            if sg != nil { goto send } // receiver waiting, do direct send
            if k.c.qcount < k.c.dataqsiz { goto bufsend } // buffer space
        } else {
            // This is a receive case: check if sender is waiting or buffer has data
            sg = k.c.sendq.dequeue()
            if sg != nil { goto recv } // sender waiting, do direct receive
            if k.c.qcount > 0 { goto bufrecv } // data in buffer
            if k.c.closed != 0 { goto rclose } // channel closed
        }
    }
    // Also check default:
    if !block { // has default case
        // No channel is ready, but default is available
        selunlock(scases, lockorder)
        casi = -1 // signal: default case
        goto retc
    }

    // STEP 5: Pass 2 — no case was immediately ready, and no default.
    // Block: enqueue this goroutine as a waiter on ALL channels.
    // Create a sudog for each case and add it to the channel's send/recv queue.
    gp = getg()
    for _, casei := range lockorder {
        k = &scases[casei]
        sg := acquireSudog()
        sg.g = gp
        sg.isSelect = true
        sg.elem = k.elem
        sg.c = k.c
        if casei < nsends {
            k.c.sendq.enqueue(sg)
        } else {
            k.c.recvq.enqueue(sg)
        }
    }

    // Park the goroutine — block until one channel becomes ready.
    // When woken, exactly one of the sudogs will have been "selected"
    // (its sg.g.selectDone will be set).
    gopark(selparkcommit, nil, waitReasonSelect, ...)

    // STEP 6: Goroutine was woken by one of the channels becoming ready.
    // Find which case woke us and dequeue our sudogs from all other channels.
    sg = (*sudog)(gp.param)
    gp.param = nil
    
    // Determine which case fired (the sg whose channel woke us)
    casi = -1
    cas = nil
    caseSuccess = false
    sglist = gp.waiting
    // ... iterate gp.waiting list to find the winner and dequeue losers ...

    // STEP 7: Unlock all channels and return the winning case index.
    selunlock(scases, lockorder)
    goto retc
    // ...
}
```

The key insight from this algorithm:

1. **Pass 1 (optimistic)**: check all channels in random order — if any are immediately ready, do the operation and return. No blocking at all.

2. **Pass 2 (blocking)**: if nothing is immediately ready, enqueue the goroutine as a waiter on *all* channels simultaneously. The goroutine parks (sleeps). When any channel becomes ready, it wakes the goroutine and the goroutine dequeues itself from all other channels.

3. **Lock ordering**: channels are always locked in address order to prevent deadlock between concurrent selects.

### The isSelect flag and atomic winner selection

When a goroutine is waiting in multiple channels' queues (step 2), multiple channels might become ready at the same time. The runtime uses an atomic CAS on `gp.selectDone` to ensure exactly one winner:

```go
// In the channel operation that wakes up a select waiter:
// sg.isSelect is true, so we need to "win" this goroutine atomically.
if sg.isSelect {
    if !sg.g.selectDone.cas(0, 1) {
        // Another channel already claimed this goroutine, skip.
        continue
    }
    // We won the CAS — this goroutine is ours to wake.
}
goready(sg.g, ...)
```

This is the mechanism that ensures that when a goroutine is waiting in a `select`, only one channel "wins" the goroutine, even if multiple channels become ready simultaneously.

## Select Patterns — The Complete Toolkit

### Pattern 1: Timeout

The most common select pattern in production:

```go
select {
case result := <-fetchFromDB(ctx, id):
    return result, nil
case <-time.After(5 * time.Second):
    return nil, errors.New("database query timed out after 5s")
}
```

**Warning**: `time.After()` creates a timer that is not GC'd until it fires. If you use `time.After` inside a tight loop, you leak timers. Prefer `time.NewTimer` with `defer timer.Stop()`:

```go
timer := time.NewTimer(5 * time.Second)
defer timer.Stop()

select {
case result := <-fetchFromDB(ctx, id):
    return result, nil
case <-timer.C:
    return nil, errors.New("timed out")
}
```

### Pattern 2: Non-blocking channel operation (default)

```go
// Try to send, don't block if channel is full
select {
case ch <- value:
    // sent successfully
default:
    // channel full, value dropped (or handle differently)
    droppedCounter.Add(1)
}

// Try to receive, don't block if channel is empty
select {
case msg := <-ch:
    // got a message
default:
    // nothing available right now
}
```

This is used everywhere: non-blocking cache lookups, best-effort event sending, polling patterns.

### Pattern 3: Cancellation/timeout with context

```go
func doWork(ctx context.Context) error {
    for {
        select {
        case item := <-workQueue:
            if err := process(item); err != nil {
                return err
            }
        case <-ctx.Done():
            // context was cancelled or deadline exceeded
            return ctx.Err() // returns context.Canceled or context.DeadlineExceeded
        }
    }
}
```

`ctx.Done()` returns a channel that is closed when the context is cancelled. Receiving from a closed channel always succeeds immediately, which is exactly what we want for propagating cancellation.

### Pattern 4: Multiplexing multiple inputs

```go
func fanIn(ctx context.Context, sources ...<-chan Event) <-chan Event {
    merged := make(chan Event, 100)
    var wg sync.WaitGroup

    for _, src := range sources {
        wg.Add(1)
        go func(ch <-chan Event) {
            defer wg.Done()
            for {
                select {
                case event, ok := <-ch:
                    if !ok { return } // source closed
                    select {
                    case merged <- event:
                    case <-ctx.Done(): return
                    }
                case <-ctx.Done():
                    return
                }
            }
        }(src)
    }

    go func() {
        wg.Wait()
        close(merged)
    }()

    return merged
}
```

### Pattern 5: Rate limiting

```go
limiter := time.NewTicker(time.Second / 100) // 100 ops/sec
defer limiter.Stop()

for _, req := range requests {
    select {
    case <-limiter.C:
        go processRequest(req) // proceed after token received
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

### Pattern 6: Graceful shutdown with done channel

This is the Go pattern for "stop when told to, but finish current work":

```go
func worker(jobs <-chan Job, done <-chan struct{}) {
    for {
        select {
        case job, ok := <-jobs:
            if !ok { return } // jobs channel closed
            process(job)
        case <-done:
            // Drain remaining jobs before stopping (graceful)
            for {
                select {
                case job := <-jobs:
                    process(job)
                default:
                    return // no more jobs, truly done
                }
            }
        }
    }
}
```

### Pattern 7: Select with nil channels for dynamic case disabling

(Covered in the Channels section, but repeated here because it's a select pattern)

```go
func processTwo(ch1, ch2 <-chan int) {
    for ch1 != nil || ch2 != nil {
        select {
        case v, ok := <-ch1:
            if !ok { ch1 = nil; continue }
            handle1(v)
        case v, ok := <-ch2:
            if !ok { ch2 = nil; continue }
            handle2(v)
        }
    }
}
```

### Pattern 8: Non-blocking send with backpressure logging

```go
func publishEvent(ch chan<- Event, event Event) {
    select {
    case ch <- event:
        // published
    default:
        // channel full — this is a backpressure signal
        // Log it so you can detect if this becomes frequent
        metrics.IncCounter("events.dropped")
        log.Warn("event channel full, dropping event", "id", event.ID)
    }
}
```

## Real Production Example

Here's a production pattern we used for a circuit breaker with timeout and backpressure. This select does four things at once:

```go
type CircuitBreaker struct {
    requests  chan Request
    responses chan Response
    failures  chan error
    done      chan struct{}
    timeout   time.Duration
    threshold int
}

func (cb *CircuitBreaker) Execute(ctx context.Context, req Request) (Response, error) {
    timer := time.NewTimer(cb.timeout)
    defer timer.Stop()

    // Try to send request to the executor
    select {
    case cb.requests <- req:
        // request accepted
    case <-timer.C:
        return Response{}, ErrCircuitBreakerTimeout
    case <-ctx.Done():
        return Response{}, ctx.Err()
    case <-cb.done:
        return Response{}, ErrCircuitBreakerOpen
    }

    // Wait for response
    select {
    case resp := <-cb.responses:
        return resp, nil
    case err := <-cb.failures:
        return Response{}, err
    case <-timer.C:
        return Response{}, ErrCircuitBreakerTimeout
    case <-ctx.Done():
        return Response{}, ctx.Err()
    case <-cb.done:
        return Response{}, ErrCircuitBreakerOpen
    }
}
```

This select handles five concurrent conditions simultaneously. Without `select`, this would require complex callback chains or polling loops. With `select`, it's 15 lines.

## Common Mistakes

**Mistake 1: Using `select` with only one case (and no default).**

```go
// This is equivalent to just: v := <-ch
// Select adds overhead for zero benefit here
select {
case v := <-ch:
    process(v)
}
```

**Mistake 2: Forgetting that select is pseudorandom.**

```go
// BUG: if both ch1 and ch2 always have data, select randomly picks between them.
// You might expect ch1 to always be processed first (priority). It isn't.
for {
    select {
    case v := <-ch1: process1(v) // "high priority"
    case v := <-ch2: process2(v) // "low priority"
    }
}

// Fix: check ch1 first with a non-blocking select, then fall through to both
for {
    select {
    case v := <-ch1:
        process1(v) // try high-priority first
        continue
    default:
    }
    // Only reach here if ch1 was empty
    select {
    case v := <-ch1: process1(v)
    case v := <-ch2: process2(v)
    }
}
```

**Mistake 3: `time.After` in a loop — timer leak.**

```go
// BUG: a new timer.After is created every iteration.
// Old timers are not GC'd until they fire (after the deadline).
// Under high load, you can leak thousands of timers.
for {
    select {
    case req := <-requests:
        handle(req)
    case <-time.After(30 * time.Second): // leaks if loop runs fast
        doMaintenance()
    }
}

// Fix: create timer once, reset it on each use
ticker := time.NewTicker(30 * time.Second)
defer ticker.Stop()
for {
    select {
    case req := <-requests:
        handle(req)
    case <-ticker.C:
        doMaintenance()
    }
}
```

**Mistake 4: Empty select blocks forever.**

```go
// This goroutine sleeps forever — intentional in some cases,
// but if you meant something else, it's a bug.
select {}  // blocks the current goroutine forever
           // useful for: keeping main alive while background goroutines run
```

**Mistake 5: Sending to a nil channel in select blocks that case, but other cases still work.**

```go
var out chan int // nil
select {
case out <- 42:   // NEVER fires (nil channel)
case <-done:      // fires when done is closed
}
// This is actually intentional and useful! nil cases are disabled.
```

**Mistake 6: Not handling the ok value in a range-like select pattern.**

```go
// BUG: if ch is closed, this will spin-loop receiving zero values
for {
    select {
    case v := <-ch: // if ch is closed, this ALWAYS fires with zero value
        process(v)
    }
}

// Fix: check ok
for {
    select {
    case v, ok := <-ch:
        if !ok { return } // channel closed, stop
        process(v)
    }
}
```

## Performance Implications

Select is not free. Understanding its cost matters for high-throughput systems:

```
select with 1 case, immediately ready:         ~50-100ns
select with 2 cases, 1 immediately ready:      ~80-150ns
select with 4 cases, 1 immediately ready:      ~100-200ns
select with 8 cases, 1 immediately ready:      ~150-300ns

select with no ready cases (blocks + wakes):   ~500ns-1µs
  (requires goroutine park/unpark cycle)
```

Select's overhead comes from:
1. Creating the `pollorder` random permutation (shuffle)
2. Locking all channel mutexes (in lock order)
3. Checking all cases
4. Unlocking all channel mutexes

For a select with many cases and highly loaded channels, this lock acquisition pattern can become a bottleneck. If you're doing >1M select operations/second, consider:

- Reducing the number of cases in each select
- Using a single aggregated input channel instead of multiple channels
- Using non-blocking selects with `default` and a sleep to avoid constant lock contention

## What happens under the hood — the lock ordering deep dive

Here's why select locks channels in address order. Consider two goroutines with crossed selects:

```go
// Goroutine 1:
select {
case <-ch1: ...   // locks ch1 first, then ch2
case <-ch2: ...
}

// Goroutine 2:
select {
case <-ch2: ...   // if we locked in case order: locks ch2 first, then ch1
case <-ch1: ...   // DEADLOCK: G1 holds ch1 waiting for ch2, G2 holds ch2 waiting for ch1
}
```

If `select` locked channels in the order the cases appear, these two goroutines would deadlock. The solution: always lock channels in the same order, regardless of case order. The Go runtime sorts the channels by their memory address before locking. So both goroutines lock `min(addr(ch1), addr(ch2))` first. Deadlock impossible.

This is a classic lock ordering technique — it guarantees global lock acquisition order, preventing circular wait.

## Interview Questions

**Q: How does select choose between multiple ready cases?**

Pseudorandomly. The runtime creates a random permutation (`pollorder`) of the cases using Fisher-Yates shuffle with the goroutine's fastrand source. It then checks cases in that random order and takes the first ready one. This ensures fairness — no case starves.

**Q: What does `select {}` do?**

Blocks the current goroutine forever. It's a valid idiom for keeping `main` alive while background goroutines do work (though `select {}` in main means the program runs indefinitely and can only be stopped with a signal). More commonly, programs wait on a `done` channel.

**Q: How does Go prevent deadlocks when multiple select statements try to lock the same channels?**

By locking channels in memory address order, not case order. The runtime sorts the channels by address and acquires their locks in that sorted order. This global lock ordering prevents circular wait.

**Q: What is the difference between a nil channel case in select and a closed channel case?**

Nil channel case: never fires (as if the case doesn't exist). Closed channel case: always fires immediately, returning the zero value with ok=false. Nil channel is a powerful tool for disabling cases dynamically.

**Q: Walk through what happens when a goroutine reaches a select with no ready cases.**

The runtime enqueues the goroutine as a waiting sudog on every channel in the select (in all of their send/recv queues), then parks the goroutine. When any one channel becomes ready, it atomically claims the goroutine via CAS on `selectDone`, marks it runnable, and the goroutine then dequeues itself from all other channels it was waiting on.

**Q: How would you implement a priority select (always check ch1 before ch2)?**

Double select pattern: first try a non-blocking select on the high-priority channel; if it has nothing, fall to a blocking select on all channels.

```go
select {
case v := <-highPriority:
    handle(v)
    continue
default:
}
select {
case v := <-highPriority: handle(v)
case v := <-lowPriority:  handle(v)
}
```

## Key Takeaways

- `select` is a channel multiplexer — waits for whichever case is ready first
- When multiple cases are ready: pseudorandom selection (fairness, prevents starvation)
- Internally: optimistic pass (poll all channels), then blocking pass (enqueue on all, park)
- Lock channels in address order to prevent select deadlocks — Go does this automatically
- `default` makes select non-blocking — falls through immediately if no case is ready
- Nil channel case = disabled case — powerful for dynamic case management
- `time.After` in loops leaks timers — use `time.NewTimer` + `Stop()` instead
- Select has real overhead — for ultra-high-throughput, minimize cases and consider alternatives

---

# 6. sync.Mutex

## What problem does it solve?

Channels are great for ownership transfer and communication. But sometimes you genuinely need multiple goroutines to share access to the same data structure — a cache, a counter, a map, a connection pool — without transferring ownership back and forth. This is where a mutex fits.

A mutex (mutual exclusion lock) guarantees that only one goroutine can execute a critical section at a time. It prevents concurrent access to shared state.

Here's the classic problem a mutex solves:

```go
var counter int
for i := 0; i < 1000; i++ {
    go func() {
        counter++ // DATA RACE — read-modify-write is not atomic
    }()
}
// counter will be some unpredictable value < 1000
```

`counter++` is three CPU instructions: load, add, store. Two goroutines interleaving those steps clobber each other's writes. The mutex fixes this:

```go
var (
    counter int
    mu      sync.Mutex
)
for i := 0; i < 1000; i++ {
    go func() {
        mu.Lock()
        counter++
        mu.Unlock()
    }()
}
// counter will always be exactly 1000
```

## Mental Model

Think of a mutex as a single key to a locked room. The room contains your shared data. To enter, you grab the key (`Lock()`). To leave, you put it back (`Unlock()`). Only one goroutine holds the key at a time. Everyone else waits outside.

```
                     [ shared state ]
                           |
goroutine 1:  Lock() ---> [KEY] ---> work ---> Unlock() ---> [KEY back]
goroutine 2:  Lock() ---> [waiting, no key...] ------> eventually gets key
goroutine 3:  Lock() ---> [waiting, no key...] ------> eventually gets key
```

## How it works internally

`sync.Mutex` is just 8 bytes — two 32-bit integers:

```go
// From sync/mutex.go
type Mutex struct {
    state int32  // packed bit-field: locked bit, woken bit, starving bit, waiter count
    sema  uint32 // semaphore for blocking/waking goroutines
}

// state bit layout:
// bit 0  (mutexLocked=1):    is the mutex locked?
// bit 1  (mutexWoken=2):     has a waiter been woken?
// bit 2  (mutexStarving=4):  is the mutex in starvation mode?
// bits 3+:                   count of goroutines waiting (shifted left 3)
```

## What happens under the hood — Lock()

```go
func (m *Mutex) Lock() {
    // FAST PATH: try to acquire with a single atomic CAS.
    // If state == 0 (unlocked, no waiters), set state = 1 (locked).
    // Cost: ~5-25ns. This is the common case (uncontended).
    if atomic.CompareAndSwapInt32(&m.state, 0, mutexLocked) {
        return
    }
    m.lockSlow()
}
```

`lockSlow()` does two things in a loop:

**Phase 1 — Spin**: On a multi-core machine, if the lock is held (not in starvation mode) and we haven't spun too many times (up to 4 iterations, 30 PAUSE instructions each), spin in user-space. The hypothesis: the holder may release the lock in a few hundred nanoseconds. Spinning avoids the expensive park/unpark cycle if the lock is released quickly.

**Phase 2 — Block**: If spinning doesn't win the lock, increment the waiter count in `state`, then block on the semaphore (`runtime_SemacquireMutex`). This parks the goroutine (zero CPU).

## The Two Modes — Normal vs Starvation

This is one of Go's most sophisticated mutex details. Most engineers don't know it exists.

**Normal mode** (default): When the lock is released, a waiter is woken — but it must compete with any new goroutines that arrive and try to spin. New goroutines are already running on a CPU and have a hardware advantage. The woken waiter often loses. This favors **throughput** (goroutines get the lock quickly when CPU is available).

**Starvation mode**: If a waiter has been blocked for more than **1ms**, the mutex switches to starvation mode. In this mode, the lock is transferred **directly** to the head-of-queue waiter when the current holder unlocks. New goroutines don't spin — they go straight to the end of the queue. This favors **fairness** and prevents indefinite starvation.

```
Normal mode (throughput-optimized):
  Holder unlocks ---> Wake W1
                      New goroutine G2 spins ---> GETS LOCK (W1 loses again)
                      After 1ms of W1 waiting: switch to starvation mode

Starvation mode (fairness-guaranteed):
  Holder unlocks ---> Lock goes DIRECTLY to W1 (head of queue)
                      G2 goes to tail of queue, does NOT spin
```

## What happens under the hood — Unlock()

```go
func (m *Mutex) Unlock() {
    // FAST PATH: atomic subtract clears the locked bit.
    // If state becomes 0 (no waiters), we are done. ~5ns.
    new := atomic.AddInt32(&m.state, -mutexLocked)
    if new != 0 {
        m.unlockSlow(new)
    }
}
// unlockSlow: in normal mode, release semaphore to wake one waiter.
// In starvation mode, hand off directly to head waiter (handoff=true).
```

## The defer pattern

```go
// ALWAYS use defer — it handles panics and early returns automatically
func (s *Store) Update(key string, val int) {
    s.mu.Lock()
    defer s.mu.Unlock() // called even if panic occurs

    s.data[key] = val
}

// Without defer: every return path needs explicit Unlock.
// Miss one and the mutex is locked forever -> all callers deadlock.
func (s *Store) UpdateDangerous(key string, val int) error {
    s.mu.Lock()
    if key == "" {
        s.mu.Unlock() // easy to forget
        return errors.New("empty key")
    }
    s.data[key] = val
    s.mu.Unlock() // easy to forget
    return nil
}
```

The one exception: extremely hot paths where the ~5ns `defer` overhead is measurable. Profile before removing `defer`.

## Real Production Example

At a payments service, we maintained an in-memory ledger protected by a single mutex:

```go
type Ledger struct {
    mu       sync.Mutex
    balances map[string]int64
}

func (l *Ledger) Transfer(from, to string, amount int64) error {
    l.mu.Lock()
    defer l.mu.Unlock()

    if l.balances[from] < amount {
        return fmt.Errorf("insufficient funds")
    }
    // Both debit AND credit happen under one lock hold.
    // Critical: another goroutine cannot observe the intermediate state
    // where money is debited but not yet credited.
    l.balances[from] -= amount
    l.balances[to] += amount
    return nil
}
```

The key insight: both the debit and credit are inside the same lock scope. If we had unlocked between them, another goroutine could observe "money gone from `from`, not yet in `to`" — a phantom deduction. The mutex makes the entire transfer an indivisible operation.

## Common Mistakes

**Mistake 1: Copying a mutex — silent corruption.**

```go
type Counter struct {
    mu    sync.Mutex
    value int
}

// BUG: copies the entire struct including the mutex state
func process(c Counter) { // passed by value
    c.mu.Lock()
    // ...
}

// Fix: always pass by pointer
func process(c *Counter) {
    c.mu.Lock()
    // ...
}
```

`go vet ./...` catches this. It's the most common mutex mistake in code reviews.

**Mistake 2: Holding the mutex during slow I/O.**

```go
// BAD: all callers blocked for the ~10ms database round trip
func (s *Service) Save(id string, data Data) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.db.Save(id, data) // network I/O while mutex held!
}

// GOOD: do I/O without lock, then update in-memory state under lock
func (s *Service) Save(id string, data Data) error {
    if err := s.db.Save(id, data); err != nil { // no lock
        return err
    }
    s.mu.Lock()
    s.cache[id] = data // fast, in-memory
    s.mu.Unlock()
    return nil
}
```

**Mistake 3: Recursive locking — Go mutexes are NOT reentrant.**

```go
func (s *Service) A() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.B() // B also tries to lock s.mu -> DEADLOCK
}

func (s *Service) B() {
    s.mu.Lock()         // goroutine already holds this lock -> deadlocks itself
    defer s.mu.Unlock()
}
```

Solution: have internal methods that assume the lock is already held (unexported, no lock/unlock), and have exported methods acquire the lock and call the internal ones.

**Mistake 4: Double unlock.**

```go
mu.Lock()
mu.Unlock()
mu.Unlock() // PANIC: sync: unlock of unlocked mutex
```

**Mistake 5: Not using the race detector.**

```bash
go test -race ./...  # runs tests with race detection enabled
go run -race main.go # runs program with race detection
```

The race detector adds ~5-10x overhead but catches data races precisely. Run it in CI and staging always.

## Mutex Sharding — When One Lock Is Not Enough

For high-throughput maps with many concurrent writers, one mutex is a serialization point. Solution: shard the data and use one mutex per shard.

```go
const numShards = 256

type ShardedMap struct {
    shards [numShards]struct {
        mu sync.Mutex
        m  map[string]string
        // padding to avoid false sharing on CPU cache lines
        _ [64 - unsafe.Sizeof(sync.Mutex{}) - unsafe.Sizeof(map[string]string(nil))]byte
    }
}

func (s *ShardedMap) shard(key string) int {
    h := fnv.New32a()
    h.Write([]byte(key))
    return int(h.Sum32() % numShards)
}

func (s *ShardedMap) Set(key, value string) {
    idx := s.shard(key)
    s.shards[idx].mu.Lock()
    s.shards[idx].m[key] = value
    s.shards[idx].mu.Unlock()
}
```

With 256 shards and uniform key distribution, contention drops by ~256x.

## Performance Implications

```
Mutex operation                     Cost
────────────────────────────────────────────
Lock() uncontended (CAS only)       ~5-25ns
Lock() with spinning (2-4 rounds)   ~50-200ns
Lock() blocks (goroutine parks)     ~500ns-2µs
Unlock() no waiters                 ~5-10ns
Unlock() with waiters               ~50-100ns
sync/atomic.AddInt64 (comparison)   ~5-15ns
```

Under high contention, mutex throughput collapses as goroutines serialize. Profile before optimizing — you may not actually have contention.

## Interview Questions

**Q: What are the two modes of sync.Mutex?**

Normal mode (throughput-oriented): woken waiters compete with spinning new goroutines. Starvation mode (fairness-oriented): triggered when a waiter has waited >1ms; lock is directly handed to the head-of-queue waiter. Switches back to normal when all waiters have waited <1ms.

**Q: Is Go's sync.Mutex reentrant?**

No. If a goroutine holding a mutex tries to lock it again, it deadlocks itself. This is by design — reentrant mutexes hide bugs (methods that accidentally call each other when they shouldn't). The idiomatic fix is unexported helper methods that assume the lock is held.

**Q: What happens if you copy a sync.Mutex?**

Copying a locked mutex creates a second locked mutex. Both the original and copy think they own the lock. Concurrent use leads to undefined behavior. `go vet` detects and reports mutex copies.

**Q: What is the fast path in sync.Mutex.Lock()?**

A single `atomic.CompareAndSwapInt32` — compare `state` to 0, set to 1. This takes ~5-25ns. It's the only path taken when there is no contention.

## Key Takeaways

- `sync.Mutex` is 8 bytes: a packed bit-field state + semaphore
- Fast path: single CAS (~5-25ns). Slow path: spin up to 4x, then park on semaphore
- Two modes: Normal (throughput) and Starvation (fairness, triggered at 1ms wait time)
- Always `defer mu.Unlock()` — handles all exit paths including panics
- Never copy a mutex — pass structs containing mutexes by pointer
- Never hold a mutex during slow I/O — keep critical sections short and fast
- Go mutexes are NOT reentrant — recursive locking deadlocks
- Under high contention, consider mutex sharding or switching to RWMutex

---

# 7. sync.RWMutex

## What problem does it solve?

Consider a configuration store that 10,000 goroutines read every millisecond, but only one goroutine writes to every 30 seconds (configuration reload). With a plain `sync.Mutex`, every single read — even though reads don't conflict with each other — blocks all other readers. You serialize 10,000 concurrent readers into a queue. Throughput craters.

The insight: **reads don't conflict with other reads**. Two goroutines reading a map simultaneously is safe. The problem only arises when a writer interleaves with readers, or two writers run simultaneously. We need a lock that:

- Allows **multiple concurrent readers** (read-read is safe)
- Blocks writers when readers are active (read-write conflict)
- Blocks all readers and other writers when a writer is active (write-write and write-read conflict)

That's exactly `sync.RWMutex`.

```
sync.Mutex:           [R1][R2][R3][R4][R5]  (reads serialized, slow)
sync.RWMutex:         [R1,R2,R3,R4,R5]      (reads concurrent, fast!)
                      Then: [W1]             (writer gets exclusive access)
                      Then: [R6,R7,R8]       (reads concurrent again)
```

## Mental Model

Think of RWMutex as a library reading room:

- **Readers**: anyone can walk in and read at the same time. Many readers, simultaneously. No problem.
- **Writers**: need the room completely empty. They put a "Closed for renovations" sign outside, wait for all current readers to leave, then enter alone. No new readers admitted until they're done.

```
API:
  mu.RLock()    // acquire read lock (shared — many goroutines can hold simultaneously)
  mu.RUnlock()  // release read lock

  mu.Lock()     // acquire write lock (exclusive — only one goroutine can hold)
  mu.Unlock()   // release write lock
```

## How it works internally

```go
// From sync/rwmutex.go
type RWMutex struct {
    w           Mutex        // held if there are pending or active writers
    writerSem   uint32       // semaphore for writers to wait for completing readers
    readerSem   uint32       // semaphore for readers to wait for completing writers
    readerCount atomic.Int32 // number of active readers (negative = writer pending)
    readerWait  atomic.Int32 // number of readers that writer is waiting for
}
```

### RLock() — acquire read lock

```go
func (rw *RWMutex) RLock() {
    // Atomically increment readerCount.
    // If result is negative, a writer is waiting — block.
    // If result is positive, we have the read lock. Done.
    if rw.readerCount.Add(1) < 0 {
        // A writer has decremented readerCount by rwmutexMaxReaders (a large negative number).
        // We need to wait for the writer to finish.
        runtime_SemacquireMutex(&rw.readerSem, false, 0)
    }
}
```

The key trick: `readerCount` starts at 0. Each reader atomically increments it. A positive value means "N active readers, no writer". When a writer arrives, it subtracts `rwmutexMaxReaders` (1<<30 = 1,073,741,824) from `readerCount`, making it massively negative. Any reader that increments after this gets a negative result and knows it must wait.

### RUnlock() — release read lock

```go
func (rw *RWMutex) RUnlock() {
    // Decrement readerCount.
    // If result is negative, a writer is waiting.
    r := rw.readerCount.Add(-1)
    if r < 0 {
        // We were the last reader a writer was waiting for.
        // Decrement readerWait. If it hits 0, wake the writer.
        if rw.readerWait.Add(-1) == 0 {
            runtime_Semrelease(&rw.writerSem, false, 1)
        }
    }
}
```

### Lock() — acquire write lock

```go
func (rw *RWMutex) Lock() {
    // First, acquire the internal mutex — this blocks other writers.
    rw.w.Lock()

    // Announce that a writer is pending by subtracting rwmutexMaxReaders from readerCount.
    // This makes readerCount negative, causing future RLock() callers to block.
    r := rw.readerCount.Add(-rwmutexMaxReaders) + rwmutexMaxReaders
    // r is now the count of ACTIVE readers at this moment.

    // If there are active readers, wait for them to finish.
    if r != 0 && rw.readerWait.Add(r) != 0 {
        runtime_SemacquireMutex(&rw.writerSem, false, 0)
    }
    // Now no readers are active. We have exclusive write access.
}
```

### Unlock() — release write lock

```go
func (rw *RWMutex) Unlock() {
    // Re-enable reading: add rwmutexMaxReaders back to readerCount.
    // This makes readerCount positive again, waking blocked readers.
    r := rw.readerCount.Add(rwmutexMaxReaders)

    // Wake all waiting readers.
    for i := 0; i < int(r); i++ {
        runtime_Semrelease(&rw.readerSem, false, 0)
    }

    // Release the internal writer mutex.
    rw.w.Unlock()
}
```

## Writer starvation prevention

Here's a subtle issue: if readers are constantly arriving (a read-heavy workload), a writer could wait forever — new readers keep arriving before the current ones leave.

Go's RWMutex prevents writer starvation: once a writer calls `Lock()` and subtracts `rwmutexMaxReaders` from `readerCount`, any new RLock() callers see a negative count and block on `readerSem`. They will not be allowed in until the writer finishes. So the writer only needs to wait for **readers already inside** — not the infinite stream of future readers.

```
Timeline:
  t=0: R1 calls RLock() -- allowed in (readerCount=1)
  t=1: R2 calls RLock() -- allowed in (readerCount=2)
  t=2: W1 calls Lock()  -- readerCount becomes -(1<<30)+2, announces writer pending
  t=3: R3 calls RLock() -- blocked! readerCount is negative, goes to readerSem queue
  t=4: R4 calls RLock() -- blocked!
  t=5: R1 calls RUnlock() -- readerCount goes lower, writer counts R1 as done
  t=6: R2 calls RUnlock() -- readerWait hits 0, W1 is woken!
  t=7: W1 executes         -- R3 and R4 still blocked
  t=8: W1 calls Unlock()   -- readerCount restored, R3 and R4 woken
```

## When to use RWMutex vs Mutex

```
Use sync.Mutex when:
  - Write frequency is similar to read frequency
  - Critical sections are very short (< 1µs) — RWMutex overhead may exceed benefit
  - You have only 1-2 goroutines competing

Use sync.RWMutex when:
  - Reads vastly outnumber writes (10:1 or higher ratio)
  - Read-lock hold time is significant (> 1µs)
  - Many goroutines do concurrent reads
  - Write operations are relatively rare (config reload, cache refresh, etc.)

Real examples of RWMutex use cases:
  - In-memory config/feature flags (many readers, rare reloads)
  - Read-heavy caches (many cache lookups, infrequent insertions)
  - Route tables, routing configs
  - Session stores (many lookups, infrequent session creation/expiry)
```

## Real Production Example

We had a service with an in-memory routing table — a map from user IDs to backend service endpoints. 50,000 requests/second read from it; a background goroutine refreshed it every 30 seconds.

```go
type Router struct {
    mu    sync.RWMutex
    routes map[string]string  // userID -> endpoint
}

// Called 50,000 times/second — needs maximum read throughput
func (r *Router) GetRoute(userID string) (string, bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    endpoint, ok := r.routes[userID]
    return endpoint, ok
}

// Called once every 30 seconds — rare exclusive write
func (r *Router) Reload(newRoutes map[string]string) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.routes = newRoutes
}
```

With `sync.Mutex`, 50K concurrent read goroutines serialize. With `sync.RWMutex`, all 50K can read simultaneously — the only blocking occurs during the 30-second reload (a ~1ms window during which reads queue up briefly).

Measured throughput difference in our service:
- `sync.Mutex`: ~800K reads/second (serialized)
- `sync.RWMutex`: ~12M reads/second (concurrent)

That's a 15x improvement from switching one line.

## Common Mistakes

**Mistake 1: Using RLock for a read-then-write operation.**

```go
// BUG: read under RLock, then try to write under Lock
// But between RUnlock and Lock, the state can change!
func (c *Cache) GetOrSet(key string, defaultVal string) string {
    c.mu.RLock()
    v, ok := c.m[key]
    c.mu.RUnlock()

    if !ok {
        c.mu.Lock()
        // BUG: another goroutine may have set this key between RUnlock and Lock
        c.m[key] = defaultVal // may overwrite what another goroutine just wrote
        c.mu.Unlock()
        return defaultVal
    }
    return v
}

// Fix: check-then-act must be under a single write lock
func (c *Cache) GetOrSet(key string, defaultVal string) string {
    c.mu.Lock()
    defer c.mu.Unlock()
    v, ok := c.m[key]
    if !ok {
        c.m[key] = defaultVal
        return defaultVal
    }
    return v
}
```

**Mistake 2: Forgetting that RLock and Lock are incompatible — don't mix them on the same path.**

```go
// BUG: a goroutine holding RLock tries to upgrade to Lock
func (c *Cache) Update(key string) {
    c.mu.RLock()
    // ... check something ...
    c.mu.Lock()    // DEADLOCK: can't upgrade RLock to Lock
    c.m[key] = "" // this will never execute
    c.mu.Unlock()
    c.mu.RUnlock()
}
```

There is no "upgrade" from read lock to write lock in Go. You must release the read lock first, then acquire the write lock. Between the two, state may change.

**Mistake 3: Using RWMutex when writes are as frequent as reads.**

On a single core or with very short critical sections, `sync.RWMutex` is actually **slower** than `sync.Mutex` due to the overhead of managing the reader count atomically. Profile first.

**Mistake 4: Copying an RWMutex.**

Same issue as `sync.Mutex` — contains internal state. Always use by pointer.

## Performance Implications

```
Operation                           Cost
────────────────────────────────────────────────────
RLock() (no writer waiting)         ~15-30ns (atomic Add)
RUnlock() (no writer waiting)       ~15-30ns (atomic Add)
Lock() (no readers)                 ~25-50ns (mutex + atomic)
Unlock() (no waiting readers)       ~25-50ns

RLock() vs Mutex.Lock() speedup:
  - At 8 concurrent readers: ~8x faster (concurrent vs serialized)
  - At 64 concurrent readers: ~64x faster
  - At 1 concurrent reader: ~same speed (or slightly slower)
```

## Interview Questions

**Q: When should you use RWMutex over Mutex?**

When reads greatly outnumber writes (at least 10:1 ratio), hold times are non-trivial (> 1µs), and multiple goroutines do concurrent reads. RWMutex allows concurrent readers, serializes writers, and prevents writer starvation. It adds overhead vs Mutex for write operations and for very short critical sections.

**Q: How does RWMutex prevent writer starvation?**

When a writer calls `Lock()`, it subtracts `rwmutexMaxReaders` from `readerCount`, making it negative. Any subsequent RLock() calls see a negative count and block. This means the writer only waits for readers that were already inside — not future readers. Future readers queue behind the writer.

**Q: Can you upgrade a read lock to a write lock in Go?**

No. You must release the read lock (`RUnlock()`) before acquiring the write lock (`Lock()`). Between the two, state may change, so you must re-validate any assumptions made under the read lock.

**Q: What happens internally when a writer calls Lock() on an RWMutex?**

1. Acquires the internal `Mutex` (blocks other writers).
2. Atomically subtracts `rwmutexMaxReaders` from `readerCount` (making it negative — blocks new readers).
3. Computes the count of currently active readers.
4. If there are active readers, waits on `writerSem` until the last active reader calls `RUnlock()` and wakes the writer.

## Key Takeaways

- RWMutex allows concurrent reads, exclusive writes — ideal for read-heavy workloads
- Writer starvation prevention: once a writer is waiting, new readers block until the writer finishes
- Internally: `readerCount` tracks active readers; writer uses a large negative offset to block new readers atomically
- No lock upgrade: cannot go from RLock to Lock — must release and reacquire
- Never copy an RWMutex — use by pointer
- Benchmark before switching: for very short critical sections or write-heavy workloads, plain Mutex may be faster
- The 15x throughput difference is real — for read-heavy shared state, RWMutex is the correct tool

---

# 8. sync.WaitGroup

## What problem does it solve?

You launch N goroutines. You need to wait until ALL of them finish before continuing. How?

The naive approach — `time.Sleep(1 * time.Second)` — is wrong. Too short and you proceed before work is done. Too long and you waste time. Using N channels works but doesn't scale when N is dynamic.

`sync.WaitGroup` is the idiomatic solution. A counter: increment before launching each goroutine, decrement when each finishes, block until the counter hits zero.

```go
var wg sync.WaitGroup

for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        doWork()
    }()
}

wg.Wait() // blocks until all 100 goroutines call Done()
```

## Mental Model

Think of it as a countdown latch. `Add(n)` sets the count. Every `Done()` decrements it. `Wait()` blocks until the count is zero.

```
wg.Add(3)  -> counter=3
[G1] Done() -> counter=2
[G2] Done() -> counter=1
[G3] Done() -> counter=0  -> Wait() unblocks!
```

## How it works internally

```go
// From sync/waitgroup.go
type WaitGroup struct {
    noCopy noCopy       // makes go vet detect copies
    state  atomic.Uint64 // high 32 bits = counter, low 32 bits = waiter count
    sema   uint32        // semaphore for waking Wait() goroutines
}
```

**Add(delta)**:
- Atomically adds `delta` to the high 32-bit counter via a single 64-bit atomic add
- If delta is negative and counter goes negative: panic ("negative WaitGroup counter")
- If counter reaches zero and there are waiters: releases the semaphore to wake all of them

**Done()**:
- Just calls `Add(-1)` — exactly one atomic operation

**Wait()**:
- Reads state atomically. If counter is already zero, returns immediately.
- Otherwise, atomically increments the waiter count (low 32 bits) and blocks on the semaphore.
- When the semaphore is released (counter reached zero), Wait() returns.

The elegance: the entire state — counter AND waiter count — is packed into one `uint64`. A single atomic operation updates both. No separate mutex needed.

## The Rules — You Must Know These Cold

**Rule 1: `Add()` must be called BEFORE the goroutine is launched.**

```go
// BUG: race between Add and Wait
go func() {
    wg.Add(1)       // goroutine calls Add after being launched
    defer wg.Done()
    doWork()
}()
wg.Wait() // may execute before Add(1) is called, returns immediately, misses the goroutine
```

```go
// CORRECT: Add before go statement
wg.Add(1)
go func() {
    defer wg.Done()
    doWork()
}()
wg.Wait() // always sees counter=1 before blocking
```

**Rule 2: `Done()` must always be called exactly once per `Add(1)`, even on error paths — use `defer`.**

```go
wg.Add(1)
go func() {
    defer wg.Done() // called even if the goroutine panics or returns early
    if err := doWork(); err != nil {
        log.Printf("error: %v", err)
        return // Done() still gets called via defer
    }
}()
```

**Rule 3: Never reuse a WaitGroup while Wait() is still running.**

```go
// BUG: reusing wg while Wait() is still blocking
go func() {
    wg.Add(1) // called while another goroutine is in Wait() -- panic
}()
wg.Wait()
```

This is documented in Go as a misuse. The panic message is: `"sync: WaitGroup misuse: Add called concurrently with Wait"`.

**Rule 4: Don't copy a WaitGroup — it contains internal state.**

Same as Mutex — use by pointer or embed in a struct and pass the struct by pointer.

## Real Production Example

We used WaitGroup to parallelize a batch of API calls, collecting all results before returning:

```go
func fetchAllUsers(ctx context.Context, ids []string) ([]User, []error) {
    users := make([]User, len(ids))
    errs  := make([]error, len(ids))

    var wg sync.WaitGroup
    for i, id := range ids {
        wg.Add(1)
        go func(idx int, userID string) {
            defer wg.Done()
            user, err := fetchUser(ctx, userID)
            users[idx] = user // safe: each goroutine writes to its own index
            errs[idx]  = err
        }(i, id)
    }

    wg.Wait() // block until all fetches complete
    return users, errs
}
```

Key insight: each goroutine writes to a different index in the slice, so no mutex is needed. The WaitGroup just ensures we don't return before all goroutines finish.

## Common Mistakes

**Mistake 1: Calling Add inside the goroutine (classic race).**

Already shown above. Always `Add` before `go`.

**Mistake 2: Forgetting Done on error paths.**

```go
wg.Add(1)
go func() {
    result, err := doWork()
    if err != nil {
        return // BUG: forgot wg.Done() — Wait() blocks forever
    }
    process(result)
    wg.Done()
}()
// Fix: use defer wg.Done() at the top of the goroutine
```

**Mistake 3: Using WaitGroup to wait for one goroutine — overkill.**

```go
// Overkill — just use a channel or simple done channel
var wg sync.WaitGroup
wg.Add(1)
go func() { defer wg.Done(); doWork() }()
wg.Wait()

// Simpler for a single goroutine:
done := make(chan struct{})
go func() { doWork(); close(done) }()
<-done
```

**Mistake 4: Negative counter — calling Done() too many times.**

```go
var wg sync.WaitGroup
wg.Add(1)
wg.Done() // counter = 0
wg.Done() // PANIC: sync: negative WaitGroup counter
```

**Mistake 5: Not scoping WaitGroups properly in loops.**

```go
// BUG: wg is shared across loop iterations that may overlap
var wg sync.WaitGroup
for batch := range batches {
    for _, item := range batch {
        wg.Add(1)
        go process(item, &wg)
    }
    wg.Wait() // waits for current batch, but previous iterations' goroutines
              // might still be running and calling Done() on the same wg
}

// Fix: create a new wg per batch
for batch := range batches {
    var wg sync.WaitGroup
    for _, item := range batch {
        wg.Add(1)
        go process(item, &wg)
    }
    wg.Wait()
}
```

## WaitGroup + errgroup

The standard `sync.WaitGroup` doesn't collect errors. For concurrent work that can fail, use `golang.org/x/sync/errgroup` — it wraps WaitGroup with error collection and context cancellation:

```go
import "golang.org/x/sync/errgroup"

func fetchAllUsers(ctx context.Context, ids []string) ([]User, error) {
    users := make([]User, len(ids))
    g, ctx := errgroup.WithContext(ctx)

    for i, id := range ids {
        i, id := i, id // capture loop vars
        g.Go(func() error {
            user, err := fetchUser(ctx, id)
            if err != nil {
                return err // cancels the context for all other goroutines
            }
            users[i] = user
            return nil
        })
    }

    if err := g.Wait(); err != nil { // blocks until all goroutines done, returns first error
        return nil, err
    }
    return users, nil
}
```

`errgroup.WithContext` also propagates cancellation: if any goroutine returns an error, the shared context is cancelled, signalling all other goroutines to stop early.

## Performance Implications

```
WaitGroup operation        Cost
──────────────────────────────────
Add(1) — no waiters        ~10-20ns (single atomic Add)
Done() — counter > 0       ~10-20ns (same as Add(-1))
Done() — counter hits 0    ~50-100ns (must wake all waiters)
Wait() — counter is 0      ~5-10ns (immediate return)
Wait() — counter > 0       ~500ns-1µs (park + wake cycle)
```

WaitGroup is very efficient — it's built on a single 64-bit atomic and a semaphore. For most use cases, its overhead is negligible compared to the work being done by the goroutines.

## Interview Questions

**Q: What is the correct order of WaitGroup operations?**

`Add(n)` must be called before launching the goroutine (to avoid races with `Wait()`). `Done()` must be called exactly once per `Add(1)`, always via `defer`. `Wait()` blocks until the counter reaches zero.

**Q: What happens if you call Done() more times than Add()?**

Panic: "sync: negative WaitGroup counter". Each `Done()` is `Add(-1)`. Going below zero indicates a bug — more goroutines called `Done()` than were counted.

**Q: How does WaitGroup store its state efficiently?**

It packs the goroutine counter (high 32 bits) and the waiter count (low 32 bits) into a single `uint64`, updated with a single atomic operation. This avoids needing a separate mutex while still being goroutine-safe.

**Q: What's the difference between WaitGroup and errgroup?**

`sync.WaitGroup` just waits — no error propagation. `golang.org/x/sync/errgroup` wraps WaitGroup, collects the first non-nil error from any goroutine, and can propagate cancellation via a shared context.

## Key Takeaways

- Call `Add(n)` before launching goroutines — never inside the goroutine
- Always `defer wg.Done()` at the start of every goroutine body
- `Done()` is `Add(-1)` — going negative panics
- State packed into one `uint64`: high bits = counter, low bits = waiter count
- Never copy a WaitGroup — embed in struct and pass by pointer
- For error-collecting fan-out, use `errgroup` from `golang.org/x/sync`

---

# 9. Context

## What problem does it solve?

In a real server, a single incoming request spawns multiple goroutines: one to query the database, one to call a downstream service, one to log, and so on. Now the client cancels the request (or the request times out). What happens to all those goroutines?

Without a mechanism to propagate cancellation, they keep running. They burn CPU, consume database connections, make network calls — all wasted work for a request nobody cares about anymore. In a high-traffic service, this compounds quickly into a reliability problem.

`context.Context` is Go's answer. It's a standard way to carry:
1. **Cancellation signals** — "stop what you're doing"
2. **Deadlines and timeouts** — "stop by this time"
3. **Request-scoped values** — "here's the request ID, user auth, trace ID"

The context flows down the call stack and goroutine tree. Every function and goroutine that should respect cancellation accepts a `context.Context` as its first argument and watches `ctx.Done()`.

## Mental Model

Think of a context as a tree of linked signals. The root context (usually `context.Background()`) is never cancelled. You derive child contexts from it. Cancelling a parent cancels all children.

```
context.Background()          (never cancels)
    |
    +-- WithCancel()           (cancels when cancel() is called)
    |       |
    |       +-- WithTimeout()  (cancels when timer fires, or when parent cancels)
    |               |
    |               +-- WithValue()  (carries a value, inherits cancellation from parent)
    |
    +-- WithDeadline()         (cancels at absolute time, or when parent cancels)
```

When any context in the tree is cancelled, all its children are cancelled too, automatically.

## The Context interface

```go
// From context/context.go
type Context interface {
    // Deadline returns the time when this context will be cancelled, if any.
    // ok==false if no deadline is set.
    Deadline() (deadline time.Time, ok bool)

    // Done returns a channel that is CLOSED when the context is cancelled.
    // Receiving from a closed channel always succeeds — this is how cancellation propagates.
    // Returns nil for contexts that can never be cancelled (context.Background, context.TODO).
    Done() <-chan struct{}

    // Err returns nil if Done is not yet closed.
    // Returns context.Canceled if the context was cancelled via cancel().
    // Returns context.DeadlineExceeded if the deadline passed.
    Err() error

    // Value returns the value associated with key, or nil.
    // Only for request-scoped data (trace IDs, auth tokens) — not for optional function parameters.
    Value(key any) any
}
```

## The Four Context Constructors

### context.Background() and context.TODO()

```go
// Background is the root of all context trees. Never cancels. Used at program entry points.
ctx := context.Background()

// TODO is a placeholder for when you haven't decided which context to use yet.
// Identical to Background internally; signals intent "I'll fix this later".
ctx := context.TODO()
```

### context.WithCancel()

```go
ctx, cancel := context.WithCancel(parent)
defer cancel() // ALWAYS call cancel — it releases resources even if ctx is never cancelled

// In a goroutine:
go func() {
    select {
    case <-doSomething():
        // work done
    case <-ctx.Done():
        return // cancelled
    }
}()

// Cancel from outside:
cancel() // closes ctx.Done(), all children notified
```

### context.WithTimeout() and context.WithDeadline()

```go
// Timeout: relative duration from now
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
defer cancel()

// Deadline: absolute time
ctx, cancel := context.WithDeadline(parent, time.Now().Add(5*time.Second))
defer cancel()

// Both cancel at the earlier of: the specified time OR the parent being cancelled.
```

### context.WithValue()

```go
type contextKey string

const requestIDKey contextKey = "requestID"

// Set a value
ctx = context.WithValue(ctx, requestIDKey, "req-12345")

// Get a value (anywhere downstream in the call chain)
reqID, ok := ctx.Value(requestIDKey).(string)
```

**Critical rules for WithValue**:
- Always use a **custom unexported type** as the key — never a string or built-in type directly. This prevents key collisions between packages.
- Only store request-scoped data: trace IDs, auth tokens, request IDs. Never store optional function parameters in context — that's an API smell.

## How it works internally

### How cancellation propagates

```go
// From context/context.go — simplified
type cancelCtx struct {
    Context               // parent

    mu       sync.Mutex
    done     atomic.Value // stores chan struct{} lazily
    children map[canceler]struct{} // children contexts that must be cancelled with this one
    err      error
}

func (c *cancelCtx) Done() <-chan struct{} {
    // Lazily create the done channel on first access.
    // Most contexts are never cancelled — the lazy init avoids allocating a channel.
    d := c.done.Load()
    if d != nil {
        return d.(chan struct{})
    }
    c.mu.Lock()
    defer c.mu.Unlock()
    d = c.done.Load()
    if d == nil {
        d = make(chan struct{})
        c.done.Store(d)
    }
    return d.(chan struct{})
}

func (c *cancelCtx) cancel(removeFromParent bool, err error) {
    c.mu.Lock()
    if c.err != nil {
        c.mu.Unlock()
        return // already cancelled
    }
    c.err = err
    // Close the done channel — this wakes all goroutines waiting on ctx.Done()
    close(c.done.Load().(chan struct{}))
    // Recursively cancel all children
    for child := range c.children {
        child.cancel(false, err)
    }
    c.children = nil
    c.mu.Unlock()

    if removeFromParent {
        removeChild(c.Context, c)
    }
}
```

When `cancel()` is called, it closes the `done` channel. Every goroutine waiting on `<-ctx.Done()` immediately unblocks (receiving from a closed channel always succeeds). Then it cancels all child contexts recursively.

### How WithCancel() registers with the parent

```go
func WithCancel(parent Context) (ctx Context, cancel CancelFunc) {
    c := &cancelCtx{}
    c.propagateCancel(parent, c) // register c as a child of parent
    return c, func() { c.cancel(true, Canceled) }
}

func (c *cancelCtx) propagateCancel(parent Context, child canceler) {
    // Walk up the parent chain to find the nearest cancelCtx ancestor.
    // Register child in that ancestor's children map.
    // When the ancestor is cancelled, it will cancel this child too.
    if p, ok := parentCancelCtx(parent); ok {
        p.mu.Lock()
        if p.err != nil {
            // parent is already cancelled — cancel child immediately
            child.cancel(false, p.err)
        } else {
            p.children[child] = struct{}{}
        }
        p.mu.Unlock()
    } else {
        // No cancelCtx ancestor — start a goroutine to watch the parent's Done channel.
        go func() {
            select {
            case <-parent.Done():
                child.cancel(false, parent.Err())
            case <-child.Done():
            }
        }()
    }
}
```

## The Canonical Pattern — Context as First Argument

Every function that can be cancelled or has a deadline should accept `ctx context.Context` as its **first parameter**:

```go
// The standard Go convention:
func (s *Service) ProcessOrder(ctx context.Context, orderID string) error {
    // Check if already cancelled before starting work
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Pass context to every downstream call
    if err := s.db.LockOrder(ctx, orderID); err != nil {
        return err
    }

    if err := s.payment.Charge(ctx, orderID); err != nil {
        return err
    }

    if err := s.inventory.Reserve(ctx, orderID); err != nil {
        return err
    }

    return s.db.CommitOrder(ctx, orderID)
}
```

If `ctx` is cancelled at any point (say, during `s.payment.Charge`), the charge call returns `context.Canceled`, `ProcessOrder` returns that error, and no further work is done. The DB lock may need explicit cleanup — context cancellation does not auto-rollback database transactions.

## Real Production Example — HTTP Request with Timeout

```go
func handler(w http.ResponseWriter, r *http.Request) {
    // Derive a child context with a 3-second timeout for this handler
    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()

    // Fan out three concurrent calls, all sharing the same timeout
    var (
        userCh    = make(chan User, 1)
        productCh = make(chan Product, 1)
        errCh     = make(chan error, 2)
    )

    go func() {
        user, err := s.db.GetUser(ctx, getUserID(r))
        if err != nil { errCh <- err; return }
        userCh <- user
    }()

    go func() {
        product, err := s.catalog.GetProduct(ctx, getProductID(r))
        if err != nil { errCh <- err; return }
        productCh <- product
    }()

    var user User
    var product Product
    for i := 0; i < 2; i++ {
        select {
        case u := <-userCh:     user = u
        case p := <-productCh:  product = p
        case err := <-errCh:
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        case <-ctx.Done():
            // timeout fired — both DB calls will also return with context.DeadlineExceeded
            http.Error(w, "request timed out", http.StatusGatewayTimeout)
            return
        }
    }

    render(w, user, product)
}
```

The 3-second timeout propagates to every downstream call. If the user DB query takes 2.9 seconds, the product catalog call only has 0.1 seconds left — it will time out automatically.

## Common Mistakes

**Mistake 1: Not calling cancel() — resource leak.**

```go
// BUG: cancel() is never called if the function returns early
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
// ... if we return early, cancel() is never called
// The timer goroutine and context children leak until the timeout fires

// Fix: ALWAYS defer cancel()
ctx, cancel := context.WithTimeout(parent, 5*time.Second)
defer cancel() // releases timer and child resources immediately
```

**Mistake 2: Storing context in a struct.**

```go
// BAD: context stored in struct
type Service struct {
    ctx context.Context // wrong — context is per-request, not per-service
}

// GOOD: pass context as a parameter to every method
func (s *Service) DoWork(ctx context.Context) error { ... }
```

Context is request-scoped. It should flow through function parameters, not live in structs.

**Mistake 3: Using string keys for WithValue.**

```go
// BUG: string keys can collide between packages
ctx = context.WithValue(ctx, "userID", userID) // any package can read "userID"

// CORRECT: unexported custom type prevents collisions
type ctxKey string
const userIDKey ctxKey = "userID"
ctx = context.WithValue(ctx, userIDKey, userID) // only this package can read it
```

**Mistake 4: Using context for optional function parameters.**

```go
// BAD: using context to pass function arguments
ctx = context.WithValue(ctx, "maxRetries", 3)
func doWork(ctx context.Context) {
    retries := ctx.Value("maxRetries").(int) // bad pattern
}

// GOOD: pass as explicit parameters
func doWork(ctx context.Context, maxRetries int) { ... }
```

**Mistake 5: Ignoring ctx.Done() in long-running loops.**

```go
// BUG: ignores cancellation — runs until all items processed even after cancel
func processAll(ctx context.Context, items []Item) {
    for _, item := range items {
        process(item) // doesn't check ctx
    }
}

// CORRECT: check ctx.Done() at the start of each iteration
func processAll(ctx context.Context, items []Item) error {
    for _, item := range items {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }
        if err := process(ctx, item); err != nil {
            return err
        }
    }
    return nil
}
```

## Performance Implications

```
context.Background()           essentially zero cost — a singleton
context.WithCancel()           ~200-300ns (struct alloc + parent registration)
context.WithTimeout()          ~300-400ns (timer creation + struct alloc)
ctx.Done() check (closed chan) ~5-10ns (immediate receive)
ctx.Done() check (open chan)   ~50-100ns (non-blocking select + default)
context.WithValue()            ~100-200ns (struct alloc, no timer)
ctx.Value() lookup             O(depth) — walks up the context chain
```

Context chains deeper than ~10 levels can make `Value()` calls measurably slow. For extremely hot paths (millions of calls/second), cache values from context into local variables.

## Interview Questions

**Q: What does context.WithCancel return and what should you always do with it?**

It returns a child context and a `CancelFunc`. You must always call the `CancelFunc` (typically with `defer cancel()`) even if you know the parent will cancel it. Failing to call it leaks the timer goroutine and the child's resources until the parent is cancelled.

**Q: What is the difference between context.Canceled and context.DeadlineExceeded?**

`context.Canceled` is returned by `ctx.Err()` when the context was explicitly cancelled via the cancel function. `context.DeadlineExceeded` is returned when the deadline (from `WithDeadline` or `WithTimeout`) elapsed. Both implement the `error` interface, and callers can check which one they got.

**Q: Why should context keys be unexported custom types?**

To prevent key collisions between packages. If two packages both use `"userID"` as a string key, they'd read each other's values accidentally. A custom unexported type (`type ctxKey string`) is unique to the package, making collisions impossible at the type system level.

**Q: What does ctx.Done() return for context.Background()?**

Nil. `context.Background()` and `context.TODO()` are never cancelled, so their `Done()` method returns a nil channel. A receive from a nil channel blocks forever — so `<-ctx.Done()` on a Background context will block forever, which is correct (it never fires).

**Q: How is cancellation propagated down a context tree?**

Each `cancelCtx` maintains a `children` map. When a parent is cancelled, it locks its mutex, iterates children, and calls `cancel()` on each child recursively. The child's `done` channel is closed, waking all goroutines waiting on `<-ctx.Done()`.

## Key Takeaways

- Context carries cancellation, deadlines, and request-scoped values down the call stack
- `ctx.Done()` returns a channel closed when cancelled — use in select to detect cancellation
- Always `defer cancel()` — even if you don't cancel manually, it frees resources
- Context is per-request: pass as first parameter, never store in structs
- Use unexported custom key types for `WithValue` — prevents cross-package collisions
- `ctx.Err()` returns `context.Canceled` or `context.DeadlineExceeded` — check which for appropriate handling
- Cancelling a parent automatically cancels all children (recursive propagation)
- `context.Background().Done()` returns nil — selecting on it blocks forever (by design)

---

# 10. sync.Once

## What problem does it solve?

You need to initialize something exactly once — lazily, the first time it's needed — in a concurrent program. A singleton database connection. A compiled regex. A config object loaded from disk. A logger instance.

The naive approach:

```go
var instance *DB

func GetDB() *DB {
    if instance == nil {           // CHECK
        instance = newDB()         // INITIALIZE
    }
    return instance
}
```

This has a data race. Two goroutines can both read `instance == nil` simultaneously, both decide to initialize, and you end up with two DB connections — or worse, one goroutine reads a partially-initialized object from another goroutine's in-progress initialization.

You could add a mutex:

```go
var (
    instance *DB
    mu       sync.Mutex
)

func GetDB() *DB {
    mu.Lock()
    defer mu.Unlock()
    if instance == nil {
        instance = newDB()
    }
    return instance
}
```

This works but is slow — every call acquires the mutex, even after initialization is done. In a hot path, that's unnecessary overhead.

`sync.Once` solves this perfectly: the initialization function runs exactly once, ever, and after that, every call to `Do()` is essentially free (a single atomic load).

```go
var (
    instance *DB
    once     sync.Once
)

func GetDB() *DB {
    once.Do(func() {
        instance = newDB()
    })
    return instance
}
```

## Mental Model

`sync.Once` is a one-shot gate. The first goroutine to call `Do(f)` executes `f`. Every subsequent call — from any goroutine, ever — is a no-op. The gate can never be reopened.

```
First call:  once.Do(f) -> executes f, sets "done" flag
All subsequent calls:  once.Do(f) -> sees "done" flag, returns immediately (skips f)

Even if 1000 goroutines call once.Do(f) simultaneously:
  - Exactly 1 goroutine executes f
  - The other 999 wait for f to finish, then return
```

## How it works internally

```go
// From sync/once.go
type Once struct {
    // done indicates whether the action has been performed.
    // It is first in the struct because it is used in the hot path.
    // The hot path is inlined at every call site.
    // Placing done first allows more compact instructions on some architectures.
    done atomic.Uint32  // 0 = not done, 1 = done

    m    Mutex          // protects the slow path
}

func (o *Once) Do(f func()) {
    // FAST PATH: atomic load — if done==1, return immediately.
    // This is the common case after initialization.
    // Cost: ~1-5ns (single atomic load, often inlined by compiler).
    if o.done.Load() == 0 {
        // Slow path — only reached if done==0 (not yet initialized, or in-progress).
        o.doSlow(f)
    }
}

func (o *Once) doSlow(f func()) {
    o.m.Lock()
    defer o.m.Unlock()
    // Double-checked locking: re-check done inside the mutex.
    // Another goroutine may have completed initialization while we were waiting for the mutex.
    if o.done.Load() == 0 {
        defer o.done.Store(1)  // mark done AFTER f() completes (via defer, so even on panic)
        f()
    }
    // If done==1 here, another goroutine already ran f() while we were waiting.
    // We just return without running f() again.
}
```

### Why the `done` field is first in the struct

This is a micro-optimization: placing `done` first means it has offset 0 from the struct pointer. On 32-bit architectures, atomic operations require 64-bit alignment, and being at offset 0 guarantees that alignment. On 64-bit architectures, it enables the compiler to generate more compact instructions for the hot path check.

### The double-checked locking pattern

The implementation uses the classic double-checked locking:

1. **Outer check** (fast path): atomic load of `done`. If 1, return. No lock needed.
2. **Acquire mutex** (slow path): block until no other goroutine is running `f`.
3. **Inner check** (inside mutex): check `done` again. Another goroutine may have set it while we waited.
4. **Run f and mark done**: run `f()`, then set `done = 1` via defer.

The `defer o.done.Store(1)` placement is critical: it runs AFTER `f()` completes. This means:
- While `f()` is running, `done == 0`. Other goroutines trying `doSlow()` will acquire the mutex, block, then wait for `f()` to finish before seeing `done == 1`.
- `done` is only set to 1 once `f()` has fully returned. Callers are guaranteed to see the initialized state.

### What if f() panics?

If `f()` panics, `done` is still set to 1 (via `defer`). Subsequent calls to `Do()` are no-ops — they won't retry `f()`. This means: **if your initialization function panics, the Once is permanently "done" with a broken state**. Plan accordingly: don't panic in the function passed to `Do()`, return errors instead.

```go
var (
    db  *sql.DB
    err error
    once sync.Once
)

func GetDB() (*sql.DB, error) {
    once.Do(func() {
        db, err = sql.Open("postgres", connString)
        if err != nil {
            return // err is captured in the closure
        }
        err = db.Ping()
    })
    return db, err
}
```

## Real Production Example

We used `sync.Once` for lazy initialization of our metrics registry — expensive to create, shared by all request handlers:

```go
type Server struct {
    metricsOnce    sync.Once
    metricsHandler http.Handler
}

func (s *Server) MetricsHandler() http.Handler {
    s.metricsOnce.Do(func() {
        reg := prometheus.NewRegistry()
        reg.MustRegister(
            prometheus.NewGoCollector(),
            prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
        )
        // Register all service-specific metrics
        for _, m := range s.serviceMetrics {
            reg.MustRegister(m)
        }
        s.metricsHandler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
    })
    return s.metricsHandler
}
```

The first call to `MetricsHandler()` initializes the registry (expensive). Every subsequent call returns the cached handler instantly — the `once.Do` fast path costs ~1ns.

## Common Mistakes

**Mistake 1: Passing a different function on each call — Only the first function ever runs.**

```go
var once sync.Once

// BUG: engineer thinks once.Do runs the function each time with different args
once.Do(func() { setup("dev") })   // runs
once.Do(func() { setup("prod") })  // DOES NOT RUN — already done
```

`sync.Once` doesn't care about the function you pass after the first call. It's "once per Once instance", not "once per function".

**Mistake 2: Trying to reset a Once — you can't.**

```go
// There's no Reset() method on sync.Once.
// If you need re-runnable once behavior, you need a new Once instance.
// For truly resettable initialization, use a mutex + bool flag manually.
```

**Mistake 3: Calling once.Do inside itself — deadlock.**

```go
var once sync.Once

once.Do(func() {
    once.Do(func() { // DEADLOCK: inner Do() tries to acquire the mutex, outer already holds it
        // ...
    })
})
```

**Mistake 4: Using Once for things that should be re-initialized on config changes.**

`sync.Once` is a permanent one-shot. If you need to re-initialize (e.g., config reload), use a RWMutex + value pattern instead:

```go
type Reloadable struct {
    mu  sync.RWMutex
    cfg *Config
}

func (r *Reloadable) Get() *Config {
    r.mu.RLock()
    defer r.mu.RUnlock()
    return r.cfg
}

func (r *Reloadable) Reload(newCfg *Config) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.cfg = newCfg
}
```

## The onceFunc helper (Go 1.21+)

Go 1.21 added `sync.OnceFunc`, `sync.OnceValue`, and `sync.OnceValues` — convenience wrappers around sync.Once:

```go
// sync.OnceFunc: runs f at most once, returns a function that calls f on first call
initialize := sync.OnceFunc(func() {
    // expensive setup
})
initialize() // runs the setup
initialize() // no-op

// sync.OnceValue: like OnceFunc but returns a single value
getDB := sync.OnceValue(func() *sql.DB {
    db, _ := sql.Open("postgres", connStr)
    return db
})
db := getDB() // initializes on first call
db  = getDB() // returns cached value

// sync.OnceValues: returns two values (value + error pattern)
getDB := sync.OnceValues(func() (*sql.DB, error) {
    return sql.Open("postgres", connStr)
})
db, err := getDB()
```

## Performance Implications

```
sync.Once operation               Cost
──────────────────────────────────────────
Do() — fast path (done==1)        ~1-5ns (atomic load)
Do() — first call (done==0)       ~100-500ns (mutex + f() + atomic store)
Do() — contended first call       ~500ns-2µs (mutex contention + wait)
```

After the first call, `sync.Once.Do()` is among the cheapest synchronization primitives in Go — just a single atomic load that the compiler can often inline.

## Interview Questions

**Q: What happens if the function passed to sync.Once.Do panics?**

The panic propagates to the caller. However, because `done` is set to 1 via `defer`, subsequent calls to `Do()` are no-ops — the function will NOT be retried. The Once is permanently "done" even though initialization failed. Always handle errors within the function rather than panicking.

**Q: What does sync.Once guarantee about memory visibility?**

Once `Do()` returns to any goroutine, all memory writes made by `f()` are visible to that goroutine. This is a happens-before guarantee: the completion of `f()` in the first goroutine happens-before the return of `Do()` in every subsequent goroutine. This is why it's safe to read `instance` after `once.Do()` without additional synchronization.

**Q: Why is the `done` field checked with an atomic load rather than just inside the mutex?**

For performance. The mutex path (slow path) is taken only on the very first call (or while the first call is in progress). All subsequent calls should be as cheap as possible. An atomic load (~1-5ns) is far cheaper than a mutex acquire (~20-100ns). The atomic load is the "fast path" for the 99.999% of calls that happen after initialization.

**Q: Can you use a sync.Once to implement a singleton safely?**

Yes — that's its primary use case. The once.Do approach is the Go-idiomatic way to implement a lazy singleton. It's safe for concurrent access, guarantees exactly one initialization, and makes subsequent accesses essentially free.

## Key Takeaways

- `sync.Once` ensures a function runs exactly once, ever — across all goroutines, forever
- Fast path: single atomic load (~1-5ns) — essentially free after initialization
- Slow path: mutex-protected double-checked locking — only taken by the first (or concurrent initial) callers
- If `f()` panics, `done` is still set to 1 — no retry on subsequent calls
- No Reset — Once is permanent; for resettable init, use a mutex + bool flag
- Go 1.21+ adds `sync.OnceFunc`, `sync.OnceValue`, `sync.OnceValues` for cleaner API
- Memory guarantee: writes in `f()` are visible to all goroutines after `Do()` returns

---

# 11. sync.Pool

## What problem does it solve?

In high-throughput Go services, object allocation is often the bottleneck — not CPU, not I/O. Every `make([]byte, 4096)` or `new(SomeStruct)` is a heap allocation, which means the garbage collector eventually has to scan and reclaim it. Under high load (say, 100K requests/second), allocating and GCing millions of short-lived objects per second causes GC pauses and high CPU overhead.

`sync.Pool` is a concurrent, GC-aware object pool. You put objects you're done with back in the pool instead of letting them become garbage. The next operation that needs an object takes it from the pool instead of allocating new. Zero allocations in the hot path.

The canonical example is `bytes.Buffer` or byte slices used for JSON serialization:

```go
var bufPool = sync.Pool{
    New: func() any {
        return new(bytes.Buffer)
    },
}

func serializeToJSON(v any) ([]byte, error) {
    buf := bufPool.Get().(*bytes.Buffer)  // get a buffer from pool (or create new)
    buf.Reset()                           // clear any previous content
    defer bufPool.Put(buf)                // return to pool when done

    if err := json.NewEncoder(buf).Encode(v); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
```

Without the pool: every call allocates a new `bytes.Buffer` on the heap. With the pool: buffers are reused across calls — allocation drops to near zero in steady state.

## Mental Model

Think of `sync.Pool` as a bag of pre-used objects. When you need an object, check the bag first. If the bag has one, take it. If not, create a new one (via the `New` function). When you're done with the object, put it back in the bag for the next caller.

```
Hot path (pool has object):
  Get() -> takes from pool (no allocation!) -> use -> Put() -> back in pool

Cold path (pool is empty):
  Get() -> calls New() -> allocates new object -> use -> Put() -> back in pool
```

**Critical caveat**: The GC can empty the pool at any time. Between two GC cycles, pooled objects are available. At a GC, the pool is cleared (not entirely — see below). Never store state in a pool that must survive a GC.

## How it works internally

```go
// From sync/pool.go
type Pool struct {
    noCopy noCopy         // go vet anti-copy

    // local is a fixed-size array of poolLocal, one per P.
    // Each P has its own poolLocal, avoiding cross-P contention in the hot path.
    local     unsafe.Pointer // *[P]poolLocal
    localSize uintptr        // size of the local array

    victim     unsafe.Pointer // *[P]poolLocal — objects rescued from previous GC
    victimSize uintptr

    // New optionally specifies a function to generate a value
    // when Get would otherwise return nil.
    New func() any
}

// poolLocal is the per-P pool cache
type poolLocal struct {
    poolLocalInternal
    // Prevents false sharing (CPU cache line conflicts) by padding to cache line size
    pad [128 - unsafe.Sizeof(poolLocalInternal{})%128]byte
}

type poolLocalInternal struct {
    private any       // single object, can be set and retrieved without CAS
    shared  poolChain // ring buffer of objects (lock-free, accessed by other Ps for stealing)
}
```

### Get() — the full algorithm

```
1. Lock the current goroutine to its P (disable preemption).
2. Check local.private: if non-nil, take it. Zero out private. Return.
   (Zero contention: only the current P touches private)
3. If private was nil, try to pop from local.shared (current P's shared ring buffer).
4. If local.shared is empty, try to STEAL from another P's shared pool (work stealing!).
5. If all Ps' pools are empty, check the victim cache (objects from the previous GC cycle).
6. If victim cache is also empty, call New() to create a fresh object.
7. Unlock P.
```

### Put() — the full algorithm

```
1. Lock the current goroutine to its P.
2. If local.private is nil, store the object there. (fastest path — zero contention)
3. Otherwise, push to local.shared (the ring buffer).
4. Unlock P.
```

### The victim cache mechanism — surviving one GC

Before Go 1.13, the pool was completely emptied at every GC. This caused dramatic latency spikes: the first requests after a GC all had to allocate new objects, causing a burst of allocations and another GC shortly after.

Since Go 1.13, sync.Pool uses a **victim cache** (from CPU cache terminology):

```
Before GC:
  local  = [current pool objects]
  victim = [objects from previous GC cycle]

At GC:
  victim = local  (the current pool becomes the victim)
  local  = empty  (fresh start)

Get() checks:
  1. local.private
  2. local.shared
  3. victim cache  (still available for one GC cycle)
  4. New()
```

This means objects survive through one GC cycle. After a GC, Get() can still find objects in the victim cache for the next ~2 minutes (typical GC frequency under load). Only after a second GC are objects truly gone. This dramatically reduces post-GC allocation spikes.

## Real Production Example

We used `sync.Pool` for HTTP request parsing buffers in our API gateway. At 200K requests/second, each requiring a 32KB scratch buffer for parsing, that's 6.4GB/second of allocation without pooling. With pooling, it's near zero:

```go
var scratchPool = sync.Pool{
    New: func() any {
        b := make([]byte, 32*1024) // 32KB buffer
        return &b
    },
}

func parseRequest(r *http.Request) (*ParsedRequest, error) {
    bufPtr := scratchPool.Get().(*[]byte)
    buf := *bufPtr
    defer scratchPool.Put(bufPtr)

    n, err := io.ReadFull(r.Body, buf)
    if err != nil && err != io.ErrUnexpectedEOF {
        return nil, err
    }

    return parse(buf[:n])
}
```

Before pooling: GC was running every 100ms, causing 5-10ms latency spikes. After pooling: GC runs every 2-3 seconds, latency spikes eliminated.

## Common Mistakes

**Mistake 1: Storing objects with state you haven't reset.**

```go
buf := bufPool.Get().(*bytes.Buffer)
// BUG: forgot to reset — previous content is still there!
buf.WriteString("new content")
// buf now has "old content" + "new content"

// Fix: always reset before use
buf := bufPool.Get().(*bytes.Buffer)
buf.Reset() // clear previous content
defer bufPool.Put(buf)
```

**Mistake 2: Putting a nil back in the pool.**

```go
buf := bufPool.Get().(*bytes.Buffer)
// BUG: buf is nil if New() returns nil or Get() returns nil
bufPool.Put(nil) // this actually works but is wasteful — Put with nil stores nil
// Next Get() returns nil, caller must handle it

// Fix: always ensure Put receives a valid object
if buf != nil {
    bufPool.Put(buf)
}
```

**Mistake 3: Growing a pooled slice and not shrinking it back.**

```go
// BAD: if you append to the buffer and grow it, you return a larger buffer
// but future getters may not expect a grown buffer
b := slicePool.Get().([]byte)
b = append(b, bigData...) // grows b to a new underlying array
defer slicePool.Put(b)    // puts the large slice back — now pool has a big slice
```

Cap the slice before putting it back, or use `(*[]byte)` instead of `[]byte` so you can `*b = (*b)[:0]` to reset length without freeing:

```go
// Better pattern: pool pointers to slices
var pool = sync.Pool{New: func() any {
    b := make([]byte, 0, 4096)
    return &b
}}

b := pool.Get().(*[]byte)
*b = (*b)[:0] // reset length, keep capacity
defer pool.Put(b)
```

**Mistake 4: Using Pool for objects that must not be shared.**

Connections, file handles, mutexes that encode external state — don't pool these. Pools are for stateless value objects (buffers, scratch slices, encoder/decoders that can be Reset).

**Mistake 5: Assuming a specific number of objects in the pool.**

`sync.Pool` gives no size guarantees. The pool might have 0 objects or 1000. You cannot rely on the pool for bounded concurrency (use a buffered channel for that).

## Performance Implications

```
sync.Pool.Get() (pool has object)    ~20-50ns (find object in per-P pool)
sync.Pool.Get() (calls New())        ~100-500ns + allocation cost
sync.Pool.Put()                      ~20-50ns (store in per-P pool)

vs. plain allocation:
  make([]byte, 4096)                 ~50-100ns + GC pressure
  new(SomeStruct)                    ~20-50ns + GC pressure
```

The real benefit isn't the ns saved per call — it's the GC pressure reduction. Fewer heap objects → fewer GC runs → lower tail latency. In high-throughput services, this is often the difference between p99=5ms and p99=50ms.

## Interview Questions

**Q: What guarantee does sync.Pool make about how long an object stays in the pool?**

None. Objects can be evicted at any GC. Since Go 1.13, objects survive one GC cycle via the victim cache, but that's still no guarantee of persistence. Never store anything in a pool that must survive across GCs.

**Q: Why does sync.Pool have a per-P private field?**

To eliminate contention in the hot path. Each P has its own `private` slot — only the current P accesses it, so no CAS or mutex needed. This makes Put/Get in the single-P case purely non-atomic (just a pointer write/read), which is the fastest possible case.

**Q: What is the victim cache and why was it introduced?**

Before Go 1.13, GC completely emptied pools, causing post-GC allocation spikes and cascading latency. The victim cache preserves objects through one GC cycle. After a GC, the previous pool becomes the victim and objects are still available for Get(). Only after a second GC are they truly reclaimed.

**Q: What should you always do to a pooled object before using it?**

Reset it. Pooled objects carry state from their previous use. A `bytes.Buffer` has old content. A slice has old elements. Always reset/clear before using a pooled object.

## Key Takeaways

- `sync.Pool` eliminates allocation hot paths by reusing objects across goroutines
- Per-P private slot: zero-contention fast path for single-P access
- Victim cache (Go 1.13+): objects survive one GC cycle, preventing post-GC allocation spikes
- Always reset pooled objects before use — they carry state from previous users
- No size guarantees — pool may be empty at any time (use buffered channels for bounded concurrency)
- Real benefit: reduced GC pressure → lower tail latency in high-throughput services
- Best for: byte buffers, scratch slices, encoders/decoders, parser state

---

# 12. Atomic Operations

## What problem does it solve?

Mutexes are the right tool for protecting complex shared state — maps, structs with multiple fields, anything requiring more than one operation to update. But mutexes are heavy. For simple counters, flags, and pointers that you update with a single operation, a mutex is massive overkill.

`sync/atomic` provides lock-free, CPU-level atomic operations: read-modify-write operations that the hardware guarantees are indivisible. No goroutine can observe a partial update. No mutex needed.

```go
var counter int64

// Unsafe — data race if called from multiple goroutines
counter++

// Safe — atomic increment, no mutex
atomic.AddInt64(&counter, 1)

// Even better in Go 1.19+ — typed atomic types
var counter atomic.Int64
counter.Add(1)
```

## Mental Model

An atomic operation is a single CPU instruction that reads and writes memory indivisibly. On modern x86/ARM hardware, these are directly supported in silicon:
- `LOCK XADD` — atomic add
- `CMPXCHG` — compare-and-swap
- `XCHG` — atomic swap

The "lock" prefix on x86 instructions signals the memory bus to lock that cache line for the duration of the instruction — no other CPU can read or write that address until the instruction completes.

```
Without atomic:
  Goroutine 1: read counter=5, add 1, write 6
  Goroutine 2: read counter=5, add 1, write 6  (reads before G1 writes!)
  Result: counter=6, one increment lost

With atomic.AddInt64:
  Goroutine 1: LOCK XADD counter, 1  (indivisible hardware instruction)
  Goroutine 2: LOCK XADD counter, 1  (waits for G1's instruction to complete)
  Result: counter=7, both increments counted
```

## The sync/atomic API

### Old-style function API (Go 1.0+)

```go
import "sync/atomic"

// Integer operations
atomic.AddInt32(&val, delta)         // val += delta, returns new value
atomic.AddInt64(&val, delta)
atomic.AddUint32(&val, delta)
atomic.AddUint64(&val, delta)

atomic.LoadInt32(&val)               // read atomically
atomic.LoadInt64(&val)
atomic.LoadUint32(&val)
atomic.LoadUint64(&val)
atomic.LoadPointer(&ptr)

atomic.StoreInt32(&val, new)         // write atomically
atomic.StoreInt64(&val, new)
atomic.StorePointer(&ptr, new)

atomic.SwapInt32(&val, new)          // atomically swap, returns old value
atomic.SwapInt64(&val, new)

// Compare-and-Swap: if *addr == old, set *addr = new, return true
atomic.CompareAndSwapInt32(&val, old, new) bool
atomic.CompareAndSwapInt64(&val, old, new) bool
atomic.CompareAndSwapPointer(&ptr, old, new) bool
```

### New-style typed atomics (Go 1.19+) — PREFER THESE

```go
var counter atomic.Int64
counter.Add(1)              // increment
counter.Load()              // read
counter.Store(42)           // write
counter.Swap(0)             // swap, returns old value
counter.CompareAndSwap(old, new) bool

var flag atomic.Bool
flag.Store(true)
flag.Load()

var ptr atomic.Pointer[T]  // generic, type-safe atomic pointer
ptr.Store(&myValue)
ptr.Load()

var val atomic.Value       // can hold any comparable value
val.Store(someInterface{})
val.Load()
```

The typed atomic types in Go 1.19+ are strongly preferred over the function API because they're:
1. Type-safe — no `(*int64)` casts needed
2. Cleaner API — methods on the type
3. Prevent accidental non-atomic access (you can't accidentally do `counter.val++`)
4. `noCopy` embedded — `go vet` catches copies

## The Compare-And-Swap (CAS) operation — the foundation of lock-free algorithms

CAS is the most powerful atomic operation. It atomically checks if a value matches an expected value and, only if it does, replaces it with a new value. It returns true if the swap happened.

```go
// Atomic equivalent of:
//   if *addr == old { *addr = new; return true }
//   return false
success := atomic.CompareAndSwapInt64(&counter, old, new)
```

CAS is the building block for all lock-free data structures. Here's how to use it to implement a thread-safe increment with custom logic:

```go
// Add delta to counter, but only if current value is > 0 (custom constraint)
func addIfPositive(counter *atomic.Int64, delta int64) bool {
    for {
        old := counter.Load()
        if old <= 0 {
            return false // constraint violated, don't add
        }
        new := old + delta
        if counter.CompareAndSwap(old, new) {
            return true // CAS succeeded, we updated atomically
        }
        // CAS failed: another goroutine changed counter between Load and CAS.
        // Loop and retry with the new current value.
    }
}
```

This "load-compute-CAS-retry" loop is the universal pattern for lock-free updates. It's sometimes called an **optimistic locking** or **spin-on-CAS** pattern.

## Real Production Examples

### Example 1: Request counter

```go
type Server struct {
    activeRequests atomic.Int64
    totalRequests  atomic.Int64
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    s.activeRequests.Add(1)
    s.totalRequests.Add(1)
    defer s.activeRequests.Add(-1)

    handleRequest(w, r)
}

func (s *Server) Stats() (active, total int64) {
    return s.activeRequests.Load(), s.totalRequests.Load()
}
```

### Example 2: Atomic flag for one-time shutdown

```go
type Worker struct {
    stopped atomic.Bool
}

func (w *Worker) Stop() {
    if w.stopped.Swap(true) {
        return // already stopped — idempotent
    }
    // perform actual stop logic
    w.cleanup()
}

func (w *Worker) Run() {
    for !w.stopped.Load() {
        doWork()
    }
}
```

### Example 3: Atomic pointer swap (for live config reloading)

```go
type Config struct {
    MaxRetries int
    Timeout    time.Duration
}

type ConfigHolder struct {
    ptr atomic.Pointer[Config]
}

func (h *ConfigHolder) Get() *Config {
    return h.ptr.Load() // atomic pointer read — no lock needed
}

func (h *ConfigHolder) Reload(newCfg *Config) {
    h.ptr.Store(newCfg) // atomic pointer write — readers see either old or new, never partial
}
```

This is the lock-free config swap pattern. Writers swap the entire pointer atomically. Readers always get a consistent, fully-formed config. No mutex needed.

## Common Mistakes

**Mistake 1: Mixing atomic and non-atomic access to the same variable.**

```go
var counter int64

// Goroutine 1: atomic access
atomic.AddInt64(&counter, 1) // correct

// Goroutine 2: non-atomic access
counter++ // DATA RACE — even though goroutine 1 uses atomic, this is a race
```

All access to an atomically-managed variable must go through atomic operations. One non-atomic access anywhere breaks the guarantee.

**Mistake 2: Using atomic for multi-field updates.**

```go
var (
    count int64
    sum   int64
)

// BUG: these two atomic operations are NOT atomic together!
atomic.AddInt64(&count, 1)
atomic.AddInt64(&sum, value)
// Another goroutine can observe count incremented but sum not yet incremented
```

Atomics only protect individual operations. For multi-field consistency, use a mutex.

**Mistake 3: Alignment issues with 64-bit atomics on 32-bit platforms.**

On 32-bit ARM and x86, 64-bit atomic operations require the variable to be 8-byte aligned. If it's not, the program panics or produces incorrect results. The fix: use the new typed atomics (`atomic.Int64`, `atomic.Uint64`) which handle alignment internally, or ensure your `int64`/`uint64` fields are at the beginning of a struct (which guarantees alignment).

**Mistake 4: Spinning on CAS too aggressively.**

```go
// This can starve other goroutines on single-CPU machines
for {
    old := counter.Load()
    if counter.CompareAndSwap(old, old+1) {
        break
    }
    // no backoff — spins 100% CPU
}

// Better: use runtime.Gosched() to yield occasionally
for i := 0; ; i++ {
    old := counter.Load()
    if counter.CompareAndSwap(old, old+1) {
        break
    }
    if i&7 == 0 {
        runtime.Gosched() // yield to other goroutines periodically
    }
}
```

## Performance Implications

```
Operation                    Cost
──────────────────────────────────────────
atomic.LoadInt64            ~5-15ns (L1 cache hit, no other writers)
atomic.StoreInt64           ~5-15ns
atomic.AddInt64             ~10-25ns
atomic.CompareAndSwapInt64  ~10-25ns (success case)
CAS retry loop (contended)  ~50-200ns per retry

sync.Mutex.Lock (uncontended) ~20-50ns

atomic vs mutex for a counter: atomic is ~3-5x faster under low contention
```

Under high contention, CAS loops can spin significantly. For a heavily contested counter, `atomic.Add` still wins because it has hardware support. For a counter accessed from thousands of goroutines simultaneously, consider per-P/per-goroutine counters summed on read (like expvar's sync.Map approach).

## Interview Questions

**Q: What is the difference between atomic operations and mutex-protected operations?**

Atomic operations are single CPU instructions that are indivisible at the hardware level. They protect a single read-modify-write on a single word. Mutex-protected operations can protect arbitrarily complex operations involving multiple variables and multiple steps. Atomics are faster (~5-25ns vs ~20-100ns for mutex) but limited to single-variable operations.

**Q: What is Compare-And-Swap and when do you use it?**

CAS atomically checks if a variable equals an expected old value and, if so, replaces it with a new value. It returns true if the swap happened. Used for lock-free updates where you compute a new value based on the current value and need to ensure the current value hasn't changed between the read and the write. Pattern: load, compute, CAS, retry on failure.

**Q: Can you mix atomic and non-atomic access to the same variable?**

No. If any goroutine accesses a variable non-atomically while another goroutine accesses it atomically, it's a data race. All accesses must be atomic, or all must be mutex-protected.

**Q: What is the new-style typed atomics API (Go 1.19+) and why prefer it?**

Go 1.19 added `atomic.Int32`, `atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]`, `atomic.Value`. They're type-safe, prevent accidental non-atomic access, have cleaner method-based APIs, and embed `noCopy` for `go vet` copy detection. Always prefer them over the raw function API.

## Key Takeaways

- Atomics are single-instruction read-modify-write operations guaranteed indivisible by hardware
- Use for simple counters, flags, and pointer swaps — not for multi-variable updates
- All access to an atomic variable must be atomic — mixing is a data race
- CAS (Compare-And-Swap) is the foundation of lock-free algorithms: load → compute → CAS → retry
- Go 1.19+ typed atomics (`atomic.Int64`, `atomic.Bool`, etc.) are preferred over raw function API
- ~3-5x faster than mutex for simple counters under low-to-medium contention
- Alignment matters for 64-bit atomics on 32-bit platforms — use typed atomics to avoid this

---

# 13. The Go Memory Model

## What problem does it solve?

Here's a bug that confuses engineers who come from sequential programming:

```go
var data int
var ready bool

// Goroutine 1
data = 42
ready = true

// Goroutine 2
for !ready {}  // spin until ready
fmt.Println(data) // might print 0, not 42!
```

This looks obviously correct. Goroutine 1 sets data before setting ready. So when goroutine 2 sees `ready == true`, surely `data == 42`, right?

**Wrong.** On modern hardware, this is broken. The CPU and compiler are both free to reorder operations. G1 might write `ready = true` before `data = 42` at the hardware level. G2 might read `data` before the write propagates from G1's CPU cache to G2's CPU cache. The result is undefined.

The **Go Memory Model** is the specification that tells you when a memory write in one goroutine is **guaranteed** to be visible to another goroutine. Understanding it lets you write correct concurrent programs without over-synchronizing.

The Go Memory Model is built on one central concept: **happens-before**.

## The Happens-Before Relationship

If event A "happens-before" event B, then the effects of A are guaranteed to be visible when B occurs.

```
If A happens-before B:
  - Any writes that A performed are visible to B
  - The compiler and CPU are not allowed to reorder A and B

If A does NOT happen-before B (and B does not happen-before A):
  - They are "concurrent" (racy)
  - There is no guarantee about what B sees
  - This is a data race if at least one is a write
```

Within a single goroutine, all operations have a natural happens-before order (program order). The problem is establishing happens-before relationships *between* goroutines.

## Synchronization Operations That Establish Happens-Before

The Go Memory Model specifies that the following operations establish happens-before guarantees:

### 1. Goroutine creation

```
go statement happens-before the goroutine's first operation.

x := 42
go func() {
    fmt.Println(x) // SAFE: x=42 is visible here
}()
```

The `go` statement is a synchronization point. The spawning goroutine's writes before the `go` statement are visible to the new goroutine.

### 2. Goroutine destruction (via channel close or WaitGroup)

A goroutine's final operations do NOT automatically happen-before anything in another goroutine. You must explicitly synchronize at goroutine exit:

```go
// Using a channel — close signals completion
done := make(chan struct{})
x := 0
go func() {
    x = 42
    close(done) // close happens-before <-done
}()
<-done
fmt.Println(x) // SAFE: x=42 guaranteed visible
```

### 3. Channel operations

This is the richest set of happens-before guarantees:

```
For unbuffered channel ch:
  A send on ch happens-before the corresponding receive from ch completes.
  The close of ch happens-before a receive that returns the zero value due to the channel being closed.

For buffered channel ch with capacity C:
  The kth receive from ch happens-before the (k+C)th send on ch completes.
  (This enables the semaphore pattern — a send blocks until the kth receiver has freed a slot)
```

```go
var x int
ch := make(chan struct{}, 1)

// Goroutine 1
x = 42
ch <- struct{}{} // send happens-before receive

// Goroutine 2
<-ch
fmt.Println(x) // SAFE: x=42 guaranteed visible (send happens-before receive)
```

### 4. Mutex operations

```
sync.Mutex and sync.RWMutex:
  For any n < m:
  The nth call to Unlock() happens-before the mth call to Lock() returns.

In simpler terms:
  Unlock() happens-before the next goroutine's Lock() returns.
  Every write made while holding the mutex is visible to the next Lock() holder.
```

```go
var x int
var mu sync.Mutex

// Goroutine 1
mu.Lock()
x = 42
mu.Unlock() // Unlock happens-before next Lock returns

// Goroutine 2
mu.Lock()    // this Lock is guaranteed to see the x=42 write
fmt.Println(x) // SAFE: x=42 guaranteed
mu.Unlock()
```

### 5. sync.Once

```
The completion of the f() call in once.Do(f) happens-before any call to once.Do returns.

This is why sync.Once is safe for lazy initialization:
  - The initializer's writes are visible to ALL goroutines that call once.Do
```

### 6. Atomic operations (Go 1.19+)

```
Atomic operations on the same variable are sequentially consistent:
  - They all appear to happen in a total order
  - A goroutine that observes an atomic write also sees all writes that preceded it

This matches C++ memory_order_seq_cst semantics.
```

### 7. sync.WaitGroup

```
wg.Done() (which is wg.Add(-1)) happens-before wg.Wait() returns
  when the counter reaches zero.
```

## The Broken Example — Fixed

Back to the original broken example:

```go
var data int
var ready bool

// BROKEN — no happens-before between goroutines
go func() { data = 42; ready = true }()
for !ready {}
fmt.Println(data) // UB: may print 0
```

Fix using a channel (establishes send happens-before receive):

```go
var data int
ch := make(chan struct{}, 1)

go func() {
    data = 42
    ch <- struct{}{} // SEND — happens-before receive
}()

<-ch // RECEIVE
fmt.Println(data) // SAFE: data=42 guaranteed
```

Fix using a mutex:

```go
var data int
var mu sync.Mutex

go func() {
    mu.Lock()
    data = 42
    mu.Unlock() // Unlock happens-before next Lock
}()

// Wait a bit (BAD!) — still racy without synchronization
// The right fix:
mu.Lock()
fmt.Println(data)
mu.Unlock()
```

## CPU Reordering — Why You Need Memory Barriers

Modern CPUs don't execute instructions in program order for performance. They reorder memory operations to keep execution units busy. Every core has its own cache — writes to main memory may not be immediately visible to other cores.

```
CPU 1 (G1 running):                CPU 2 (G2 running):
  Store data=42  ─┐  (buffered)      Load ready ─── sees true
  Store ready=T  ─┘─▶ propagates     Load data  ─── might see 0 (not yet propagated!)
                       first!
```

On x86, the memory model is "Total Store Order" (TSO) — stores are ordered, but loads can read stale values from local buffers. On ARM, the model is weaker — both loads and stores can be reordered.

A **memory barrier** (or fence) is a CPU instruction that prevents the hardware from reordering around it:

```
MFENCE (x86) — prevents all reordering
SFENCE (x86) — prevents store reordering
LFENCE (x86) — prevents load reordering
```

Go's synchronization primitives (mutexes, channels, atomics) all insert appropriate memory barriers. When you call `mu.Lock()`, the hardware is prevented from moving memory operations across the lock boundary.

## Compiler Reordering

The compiler also reorders code for optimization. Without synchronization:

```go
// Compiler may decide to hoist ready=true before data=42 if it thinks
// they're independent, or to cache ready in a register indefinitely.
data = 42
ready = true // compiler: "I can optimize the order of these independent writes"
```

Synchronization primitives include compiler fences too (via `//go:nosplit` and memory barrier intrinsics), preventing the compiler from reordering across them.

## Real Production Example — The Initialization Bug

One of the nastiest bugs we ever saw in production was this:

```go
var (
    cache   map[string]string
    cacheOK bool
)

func init() {
    // Loaded from disk on startup
    cache = loadCache()
    cacheOK = true
}

// Handler called from HTTP server (multiple goroutines)
func handler(w http.ResponseWriter, r *http.Request) {
    if !cacheOK {
        buildCache()
        return
    }
    // USE cache here — BUG: cacheOK=true visible but cache writes not yet visible!
    val := cache[r.URL.Path]
    // ...
}
```

The `init()` function runs before `main()`, before the HTTP server starts, so there's no concurrent access to `cache` during initialization. This seems safe. But the goroutines spawned by `net/http` to serve requests had no happens-before relationship to the `init()` function beyond "init completes before main starts". In practice this was fine because the Go memory model guarantees that the main goroutine's program start synchronizes with init. But the *handler goroutines* spawned by http.Server had no explicit synchronization with the initialization — they were created later, and the init writes were visible only because of Go's runtime startup guarantees, not explicit happens-before.

The lesson: always establish explicit happens-before. Don't rely on "init must have run by now."

## The Data Race Definition

A **data race** is when:
1. Two goroutines access the same memory location concurrently
2. At least one access is a write
3. They are not synchronized (no happens-before between them)

```go
var x int

go func() { x = 1 }() // Write
go func() { _ = x }() // Read
// These are concurrent, no synchronization → DATA RACE
```

Data races are undefined behavior in Go. The spec makes no guarantee about what the program does. In practice, the race detector catches them. In production, they cause:
- Torn reads (reading half-written values)
- Stale reads (reading old cached values from CPU)
- Memory corruption
- Unpredictable behavior that changes with CPU count, OS scheduling, build flags

## Interview Questions

**Q: What is the happens-before relationship?**

An ordering guarantee between two events A and B such that if A happens-before B, all memory writes by A are guaranteed visible to B. Without a happens-before relationship between two concurrent operations accessing the same memory, the behavior is undefined (data race).

**Q: Name three synchronization operations in Go that establish happens-before.**

Channel send happens-before the corresponding receive. `mu.Unlock()` happens-before the next `mu.Lock()` returns. `wg.Done()` (when counter hits zero) happens-before `wg.Wait()` returns. Also: goroutine creation `go` statement happens-before goroutine's first operation; `once.Do(f)` completion happens-before any `once.Do()` returns.

**Q: Why is `for !ready {}` a broken spin-wait in Go?**

No happens-before is established between the writer and reader. The compiler may cache `ready` in a register and never re-read it (infinite loop). The CPU may see a stale cached value due to cache coherency delays. The compiler may also reorder the writer's operations. The correct fix is to use a channel, mutex, or atomic operation.

**Q: What is a data race?**

Two goroutines accessing the same memory concurrently where at least one is a write, with no synchronization between them. Data races are undefined behavior — they can cause torn reads, stale values, and arbitrary memory corruption. Detected by `go run -race` / `go test -race`.

## Key Takeaways

- The Go Memory Model defines when writes in one goroutine are visible to another
- The core concept: happens-before. Without it, concurrent writes are undefined behavior
- Synchronization primitives that establish happens-before: channels, mutexes, atomic ops, WaitGroup, sync.Once, goroutine creation
- Modern CPUs and compilers freely reorder memory operations — synchronization inserts barriers that prevent reordering
- `for !ready {}` without atomics is broken — compiler caches, CPU reorders
- Data race = concurrent access, at least one write, no synchronization = undefined behavior
- Run `go test -race` in CI always — the race detector catches happens-before violations

---

# 14. Race Conditions

## What problem does it solve?

A race condition is any situation where the correctness of the program depends on the relative timing or ordering of events in concurrent goroutines. The program might work correctly 99.9% of the time and catastrophically fail 0.1% of the time — only under specific scheduling, load, or hardware conditions.

Race conditions are notoriously hard to debug because:
1. They don't reproduce reliably
2. They often disappear when you add logging (which changes timing)
3. They can be latent for months and suddenly manifest under load in production
4. They cause data corruption, crashes, and incorrect behavior with no obvious cause

There are two kinds:
- **Data races**: concurrent access to the same memory with no synchronization, at least one write. This is what `go -race` detects.
- **Logic races**: correct synchronization primitives but incorrect logic — your program is technically race-free but the logic is wrong due to non-atomic multi-step operations.

## Data Race Anatomy

```go
var balance int = 100

// Two goroutines: simultaneous withdrawal
func withdraw(amount int) {
    if balance >= amount { // Read balance
        // <<< scheduler can preempt here >>>
        balance -= amount  // Write balance
    }
}

// G1: withdraw(80) — reads balance=100, sees 100>=80, starts subtracting
// G2: withdraw(80) — reads balance=100 (G1 hasn't written yet!), sees 100>=80, starts subtracting
// G1 writes balance=20
// G2 writes balance=20 (based on old read of 100)
// Final balance: 20 (should be -60, which would've been caught and rejected!)
```

Both withdrawals succeed even though you only have $100. Classic check-then-act race.

## The Race Detector

The most important concurrency tool in Go. Period.

```bash
go test -race ./...       # run tests with race detection
go run -race main.go      # run program with race detection
go build -race -o app .   # build race-detector binary (for staging)
```

When a race is detected, the output looks like this:

```
==================
WARNING: DATA RACE
Write at 0x00c000122090 by goroutine 8:
  main.withdraw()
      /app/main.go:12 +0x44

Previous read at 0x00c000122090 by goroutine 7:
  main.withdraw()
      /app/main.go:9 +0x3c

Goroutine 8 (running) created at:
  main.main()
      /app/main.go:23 +0x88

Goroutine 7 (running) created at:
  main.main()
      /app/main.go:22 +0x6a
==================
```

The race detector tells you:
- The exact memory address that was raced on
- Which goroutine wrote and which goroutine read
- The call stacks of both goroutines
- Where those goroutines were created

### How the race detector works internally

The race detector is based on **ThreadSanitizer (TSan)** from Google. It instruments every memory access at compile time. At runtime, it maintains a "shadow memory" — for every 8 bytes of your program's memory, it tracks the last few reads and writes (goroutine ID + timestamp). When a new access happens, it checks if any previous conflicting access lacks a happens-before relationship.

```
Shadow memory for address X:
  [last write: goroutine 5, epoch 3]
  [last reads: goroutine 7 epoch 2, goroutine 8 epoch 1]

New write by goroutine 9:
  - Check: is there a happens-before between goroutine 9 and goroutine 5's write?
  - Check: is there a happens-before between goroutine 9 and goroutine 7's read?
  - If no: RACE DETECTED
```

Cost: ~5-10x slowdown and ~5-10x memory overhead. Not suitable for production load, but excellent for CI and staging.

## Common Race Condition Patterns

### Pattern 1: Check-Then-Act

```go
// BUG: Read then write is not atomic
if _, exists := m[key]; !exists {
    m[key] = value // another goroutine may have set it between check and write
}

// Fix: use a mutex around the entire check-then-act
mu.Lock()
if _, exists := m[key]; !exists {
    m[key] = value
}
mu.Unlock()

// Or use sync.Map.LoadOrStore for this specific pattern
m.LoadOrStore(key, value)
```

### Pattern 2: Read-Modify-Write

```go
// BUG: three separate operations, not atomic together
counter++        // read, add, write — preemptable between any step

// Fix: atomic operation
atomic.AddInt64(&counter, 1)

// Or: mutex
mu.Lock()
counter++
mu.Unlock()
```

### Pattern 3: Closure Variable Capture in Loop

```go
// BUG: all goroutines capture the same 'i' variable — loop ends before goroutines run
for i := 0; i < 5; i++ {
    go func() {
        fmt.Println(i) // i is shared — all goroutines print 5
    }()
}

// Fix 1: pass as argument (creates a copy)
for i := 0; i < 5; i++ {
    go func(i int) {
        fmt.Println(i)
    }(i)
}

// Fix 2: create a local copy (Go 1.22+ range variables are per-iteration by default)
for i := 0; i < 5; i++ {
    i := i // shadow variable — creates a new i per iteration
    go func() {
        fmt.Println(i)
    }()
}
```

This is by far the most common race condition in Go codebases.

### Pattern 4: Map Concurrent Read/Write

```go
var m = map[string]int{}

// BUG: concurrent map read and write panics in Go (even read+read is fine, but read+write is fatal)
go func() { m["key"] = 1 }()
go func() { _ = m["key"] }()
// FATAL ERROR: concurrent map read and map write

// Fix: sync.Mutex
var mu sync.Mutex
go func() { mu.Lock(); m["key"] = 1; mu.Unlock() }()
go func() { mu.RLock(); _ = m["key"]; mu.RUnlock() }()

// Or: use sync.Map for concurrent access
var sm sync.Map
sm.Store("key", 1)
sm.Load("key")
```

Go's built-in map detects concurrent write at runtime and panics — this is a deliberate design choice to catch races early (unlike C++ which would silently corrupt memory).

### Pattern 5: Struct Field Races

```go
type Stats struct {
    Requests int64
    Errors   int64
}

var stats Stats

// BUG: multiple goroutines increment fields concurrently
go func() { stats.Requests++ }()
go func() { stats.Errors++ }()

// Fix: atomic fields
type Stats struct {
    Requests atomic.Int64
    Errors   atomic.Int64
}
// Or: protect with a mutex for multi-field atomic snapshots
```

### Pattern 6: Interface Value Race

```go
var handler http.Handler = defaultHandler

// BUG: interface value is two words (type pointer + data pointer)
// Assigning a new interface is NOT atomic — another goroutine can read a torn interface
go func() { handler = newHandler }()  // write
go func() { handler.ServeHTTP(w, r) }() // read
// Can read a partially updated interface — wrong type pointer with wrong data pointer

// Fix: atomic.Pointer[http.Handler] or sync.RWMutex
var handler atomic.Pointer[http.Handler]
```

## Logic Races — Beyond Data Races

Logic races are harder to catch because the race detector won't help. The synchronization is correct; the logic is wrong.

```go
var mu sync.Mutex
var balance int = 100

// Thread-safe but logically racy
func SafeWithdraw(amount int) bool {
    mu.Lock()
    hasEnough := balance >= amount
    mu.Unlock()

    if hasEnough { // <<< another goroutine can withdraw here!
        mu.Lock()
        balance -= amount
        mu.Unlock()
        return true
    }
    return false
}
```

No data race — the mutex protects every access. But the check and the deduction are separate critical sections. Between the two lock acquisitions, another goroutine can reduce the balance below `amount`. The fix:

```go
func SafeWithdraw(amount int) bool {
    mu.Lock()
    defer mu.Unlock()
    if balance >= amount {
        balance -= amount
        return true
    }
    return false
}
```

Hold the lock for the entire check-then-act sequence.

## Real Production Example — The Famous Caching Bug

At a high-traffic service, we had an in-memory cache that used a two-step initialization:

```go
var (
    cacheMap map[string]*Item
    cacheMu  sync.RWMutex
)

func GetItem(key string) *Item {
    cacheMu.RLock()
    item := cacheMap[key]
    cacheMu.RUnlock()

    if item == nil {
        item = loadFromDB(key)
        cacheMu.Lock()
        cacheMap[key] = item  // BUG: another goroutine may have loaded the same key
        cacheMu.Unlock()
    }
    return item
}
```

This is a logic race. Two goroutines both see `item == nil`, both call `loadFromDB(key)`, both write to `cacheMap[key]`. Technically safe (no data race), but we make two expensive DB calls for the same key. Worse: if `loadFromDB` has side effects (like incrementing a counter), they run twice.

Fix: check-inside-lock pattern:

```go
func GetItem(key string) *Item {
    // First: cheap read lock check
    cacheMu.RLock()
    item := cacheMap[key]
    cacheMu.RUnlock()

    if item != nil {
        return item
    }

    // Need to load: acquire write lock and re-check
    cacheMu.Lock()
    defer cacheMu.Unlock()
    // Re-check: another goroutine may have loaded it while we waited for the write lock
    if item = cacheMap[key]; item != nil {
        return item
    }
    item = loadFromDB(key)
    cacheMap[key] = item
    return item
}
```

This is the "double-checked locking" pattern for caches. The second check inside the write lock ensures only one goroutine loads from DB.

## Interview Questions

**Q: What is the difference between a data race and a logic race?**

A data race is concurrent access to the same memory with no synchronization, detected by the race detector. A logic race is correctly synchronized code where the program logic is incorrect — multiple individually-atomic operations aren't combined into a single atomic check-then-act, allowing other goroutines to invalidate assumptions between steps.

**Q: How does the Go race detector work?**

It instruments every memory read/write at compile time (via TSan). At runtime, it maintains shadow memory tracking the last few goroutine accesses per memory location, with happens-before vector clocks. When a new access lacks a happens-before with a conflicting prior access, it reports a race. Cost: ~5-10x slowdown, ~5-10x memory.

**Q: Why is concurrent map access a panic in Go, not silent corruption?**

By design. Go's runtime detects concurrent map write and panics immediately. Silent corruption (as in C++) is far harder to debug. A panic with a clear message is better than corrupted data that surfaces as wrong answers hours later.

**Q: What is the most common race condition in Go code?**

Loop variable capture in goroutine closures: `go func() { use(i) }()` in a `for i := range ...` loop. All goroutines share the same `i` variable. By the time they run, the loop has finished and `i` is at its final value. Fix: pass `i` as an argument, or shadow it with `i := i` inside the loop.

## Key Takeaways

- Data races: concurrent access, no synchronization, at least one write — use `go test -race` to catch them
- Logic races: correct synchronization, incorrect logic — check-then-act across separate lock acquisitions
- Race detector (TSan-based): instruments all memory accesses, maintains shadow memory + happens-before vectors
- Most common race: loop variable capture in goroutine closures
- Concurrent map write causes runtime panic in Go (by design)
- Interface value assignment is NOT atomic — use `atomic.Pointer` for concurrent interface swapping
- Always run `go test -race` in CI — many races only manifest under specific timing conditions

---

# 15. Deadlocks

## What problem does it solve?

A deadlock is when two or more goroutines are each waiting for the other to proceed — and none of them ever will. The program hangs forever, consuming no CPU, doing no work, making no progress.

Go's runtime detects the total deadlock case (all goroutines asleep) and panics with:

```
fatal error: all goroutines are asleep - deadlock!
```

Partial deadlocks (a subset of goroutines stuck while others continue) are harder to detect — the program appears to run but some requests hang indefinitely.

## The Four Necessary Conditions (Coffman Conditions)

A deadlock requires ALL four of these simultaneously:

1. **Mutual exclusion** — a resource can only be held by one goroutine at a time (mutexes)
2. **Hold and wait** — a goroutine holds one resource while waiting for another
3. **No preemption** — a held resource cannot be forcibly taken away
4. **Circular wait** — a cycle exists: G1 waits for G2, G2 waits for G1

Breaking any one of these conditions prevents deadlock.

## Deadlock Pattern 1: Mutex Deadlock

The classic two-mutex deadlock:

```go
var (
    mu1 sync.Mutex
    mu2 sync.Mutex
)

// Goroutine 1: locks mu1, then mu2
go func() {
    mu1.Lock()
    time.Sleep(1 * time.Millisecond) // yields CPU, G2 gets scheduled
    mu2.Lock() // BLOCKS: G2 holds mu2
    defer mu2.Unlock()
    defer mu1.Unlock()
    doWork()
}()

// Goroutine 2: locks mu2, then mu1
go func() {
    mu2.Lock()
    time.Sleep(1 * time.Millisecond)
    mu1.Lock() // BLOCKS: G1 holds mu1
    defer mu1.Unlock()
    defer mu2.Unlock()
    doWork()
}()

// G1 holds mu1, waits for mu2
// G2 holds mu2, waits for mu1
// DEADLOCK
```

```
G1: [holds mu1] ──waiting──> [needs mu2] ──held by──> G2
G2: [holds mu2] ──waiting──> [needs mu1] ──held by──> G1
                    CYCLE = DEADLOCK
```

**Fix: consistent lock ordering.** Always acquire multiple locks in the same order everywhere in the codebase:

```go
// RULE: always acquire mu1 before mu2, everywhere
func safeWork() {
    mu1.Lock()
    defer mu1.Unlock()
    mu2.Lock()
    defer mu2.Unlock()
    doWork()
}
// Both goroutines use this order → no circular wait → no deadlock
```

## Deadlock Pattern 2: Channel Deadlock

```go
// DEADLOCK: send on unbuffered channel with no receiver
ch := make(chan int)
ch <- 42    // blocks forever — nobody is receiving
```

```go
// DEADLOCK: receive on unbuffered channel with no sender
ch := make(chan int)
<-ch    // blocks forever — nobody is sending
```

```go
// DEADLOCK: goroutine waiting on itself
ch := make(chan int)
go func() {
    ch <- 1  // send
    ch <- 2  // send again
}()
<-ch         // first receive
// Second send blocks because there's no second receive in main
// But main is busy... wait, actually this one doesn't deadlock.
// THIS deadlocks:
func main() {
    ch := make(chan int)
    ch <- 1  // main goroutine sends — nobody receives — deadlock!
}
```

```go
// DEADLOCK: goroutine loop waiting for itself
done := make(chan struct{})
go func() {
    <-done // waiting for done to be closed
    close(done) // BUG: closes done after receiving — but nothing closes it first!
}()
<-done // main also waiting — deadlock
```

## Deadlock Pattern 3: WaitGroup Deadlock

```go
var wg sync.WaitGroup

// DEADLOCK: Add called after Wait
go func() {
    time.Sleep(10 * time.Millisecond)
    wg.Add(1) // called after Wait — panic + deadlock
}()
wg.Wait() // starts waiting with counter=0, returns immediately before Add(1) is called
// Actually this is a race/misuse, not a deadlock per se
// But this is a deadlock:

wg.Add(1)
go func() {
    // Never calls Done() — Wait blocks forever
    doWork()
    // forgot: defer wg.Done()
}()
wg.Wait() // blocks forever
```

## Deadlock Pattern 4: Context-Channel Interaction

```go
// DEADLOCK: goroutine waits on channel, but sender is blocked on context cancel
ctx, cancel := context.WithCancel(context.Background())

results := make(chan string) // unbuffered

go func() {
    // Does work, then tries to send result
    result := compute()
    results <- result // BLOCKS if nobody is receiving
}()

// Main goroutine checks context first
select {
case <-ctx.Done():
    // Context cancelled — but goroutine is still blocked trying to send!
    // Goroutine leaks here
case r := <-results:
    fmt.Println(r)
}
cancel()
// The goroutine is now permanently stuck on results <- result
```

Fix: use a buffered channel or ensure the goroutine can detect cancellation:

```go
results := make(chan string, 1) // buffer 1 — goroutine never blocks on send
go func() {
    result := compute()
    select {
    case results <- result:
    case <-ctx.Done(): // detect cancellation
    }
}()
```

## Deadlock Pattern 5: Recursive Mutex Lock

```go
var mu sync.Mutex

func outer() {
    mu.Lock()
    defer mu.Unlock()
    inner() // calls inner which also tries to lock mu
}

func inner() {
    mu.Lock() // DEADLOCK: goroutine already holds mu from outer()
    defer mu.Unlock()
    doWork()
}
```

Go mutexes are not reentrant. A goroutine trying to lock a mutex it already holds will deadlock itself.

**Fix**: split into exported (locks) and unexported (assumes lock held) methods:

```go
func (s *Service) Outer() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.inner() // unexported, no lock
}

func (s *Service) inner() { // assumes caller holds s.mu
    doWork()
}
```

## Detecting Deadlocks in Production

### Total deadlock — runtime catches it

```
fatal error: all goroutines are asleep - deadlock!

goroutine 1 [chan receive]:
main.main()
    /app/main.go:12 +0x58

goroutine 6 [chan send]:
main.func1()
    /app/main.go:8 +0x44
```

The runtime dumps all goroutine stacks. Look for goroutines in `[chan send]`, `[chan receive]`, `[semacquire]`, `[sleep]` states that form a cycle.

### Partial deadlock — hung goroutines

For partial deadlocks, use the built-in goroutine dump:

```go
// Hit Ctrl+\ (SIGQUIT) in terminal — dumps all goroutine stacks
// Or programmatically:
import "runtime"
buf := make([]byte, 1<<20)
n := runtime.Stack(buf, true) // true = all goroutines
fmt.Printf("%s", buf[:n])
```

Or use `pprof`:

```bash
# If your server has pprof endpoint:
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

Look for goroutines stuck in:
- `sync.(*Mutex).Lock` — waiting to acquire a mutex
- `sync.(*RWMutex).Lock` — waiting for read/write lock
- `chan receive` / `chan send` — blocked on channel
- `time.Sleep` — normal, not a deadlock
- `net.(*conn).Read` — blocked on network I/O (normal)

## The Dining Philosophers — The Classic Deadlock Example

Five philosophers sit at a table. Each needs two forks to eat. There's one fork between each pair. If all five simultaneously pick up their left fork, nobody can pick up their right fork — deadlock.

```go
var forks [5]sync.Mutex

func philosopher(id int) {
    left  := id
    right := (id + 1) % 5

    // DEADLOCK: all philosophers grab left fork simultaneously
    forks[left].Lock()
    forks[right].Lock()  // right fork held by neighbor — deadlock cycle
    eat(id)
    forks[right].Unlock()
    forks[left].Unlock()
}

// Fix: consistent ordering — philosopher 4 picks up right before left
func philosopher(id int) {
    left  := id
    right := (id + 1) % 5
    first, second := left, right
    if left > right {
        first, second = right, left // philosopher 4 reverses order
    }
    forks[first].Lock()
    forks[second].Lock()
    eat(id)
    forks[second].Unlock()
    forks[first].Unlock()
}
```

This is the canonical solution to the dining philosophers: break the circular wait by making at least one philosopher acquire locks in reverse order.

## Real Production Example — The Cascading Deadlock

At a billing service, we had three mutexes: one for the user account, one for the invoice, and one for the payment method. Different code paths acquired them in different orders depending on which entity was the "primary":

```go
// Payment flow: user -> invoice -> payment
userMu.Lock()
invoiceMu.Lock()
paymentMu.Lock()

// Refund flow: invoice -> user -> payment
invoiceMu.Lock()
userMu.Lock() // DEADLOCK with payment flow holding userMu waiting for invoiceMu
paymentMu.Lock()
```

We discovered this at 2 AM when a batch job ran concurrently with live transactions. The fix was a global lock-ordering document and a code review checklist: every time you acquire multiple locks, they must be in alphabetical order by variable name. Ridiculous? Yes. Did it work? Absolutely.

## Interview Questions

**Q: What are the four Coffman conditions for deadlock?**

Mutual exclusion (resources can't be shared), hold-and-wait (holding one resource while waiting for another), no preemption (resources can't be forcibly taken), and circular wait (cycle of goroutines each waiting for the next). Breaking any one prevents deadlock. The most practical one to break in Go is circular wait — by always acquiring multiple locks in a consistent order.

**Q: How does Go detect a total deadlock?**

The runtime's scheduler detects when all goroutines are blocked (asleep) with no possibility of progress. It checks if the number of sleeping goroutines equals the total number of goroutines. If so, it prints a full goroutine dump and calls `throw("all goroutines are asleep - deadlock!")`.

**Q: How do you diagnose a partial deadlock (hung goroutines) in production?**

Send SIGQUIT to the process (gets a goroutine dump on stderr), use `runtime.Stack(buf, true)` programmatically, or hit the `pprof` goroutine endpoint. Look for goroutines stuck in mutex acquire, channel send/receive. Identify cycles: G1 waiting for mutex held by G2, G2 waiting for mutex held by G1.

**Q: How do you prevent deadlock when you need to hold multiple mutexes?**

Establish a global lock ordering and always acquire multiple locks in that order everywhere in the codebase. If two goroutines always acquire mu1 before mu2, circular wait is impossible. Document the ordering. Enforce it in code review.

## Key Takeaways

- Deadlock requires all four Coffman conditions: mutual exclusion, hold-and-wait, no preemption, circular wait
- Breaking circular wait (consistent lock ordering) is the most practical prevention
- Go runtime detects total deadlock (all goroutines asleep) and panics with a goroutine dump
- Partial deadlocks don't panic — diagnose with SIGQUIT goroutine dump or pprof
- Go mutexes are NOT reentrant — a goroutine locking a mutex it already holds deadlocks
- Channel deadlocks: unbuffered channel with no sender/receiver, goroutine can't detect cancellation
- In production: document and enforce lock acquisition order across the entire codebase

---

# 16. Goroutine Leaks

## What problem does it solve?

A goroutine leak is a goroutine that was launched but will never terminate. It stays alive — blocked, spinning, or sleeping — consuming memory and OS resources indefinitely. Unlike memory leaks from heap allocations, goroutine leaks are invisible to the GC: a blocked goroutine holds its stack (2KB minimum, often much more) and any heap objects it references forever.

In a long-running server, leaked goroutines accumulate over time. You start with 100 goroutines, and after a day of traffic you have 50,000. Memory climbs steadily. Eventually the process OOMs or becomes so slow from GC pressure that it fails. This is one of the most common production reliability issues in Go services.

The insidious part: goroutine leaks don't crash immediately. They degrade performance slowly. They're often only discovered by monitoring goroutine counts and memory over time.

## How to Measure Goroutine Count

```go
import "runtime"

fmt.Println("goroutines:", runtime.NumGoroutine())
```

```go
// Expose via pprof (if you have the pprof handler registered):
// GET /debug/pprof/goroutine?debug=1  -- count by state
// GET /debug/pprof/goroutine?debug=2  -- full stacks of all goroutines
```

If your goroutine count grows linearly with traffic or time and never decreases, you have a leak.

## The Most Common Leak Patterns

### Pattern 1: Goroutine blocked on channel send — nobody receiving

```go
func processRequest(r *http.Request) {
    results := make(chan Result) // unbuffered

    go func() {
        result := heavyCompute(r)
        results <- result // LEAK: if caller returns early (timeout, cancel), nobody receives
    }()

    select {
    case <-time.After(2 * time.Second):
        return // caller exits — goroutine is stuck forever on results <- result
    case r := <-results:
        handle(r)
    }
}
```

Every timeout leaves a goroutine stuck on `results <- result`. After 1000 timeouts: 1000 leaked goroutines.

**Fix: use a buffered channel (capacity 1) so the goroutine can always send and exit:**

```go
results := make(chan Result, 1) // buffer 1 — send never blocks
go func() {
    result := heavyCompute(r)
    results <- result // can always send; nobody reading is fine
}()
```

Or: pass the context and let the goroutine check for cancellation:

```go
go func() {
    result := heavyCompute(r)
    select {
    case results <- result:
    case <-ctx.Done(): // parent context cancelled — goroutine exits cleanly
    }
}()
```

### Pattern 2: Goroutine blocked on channel receive — nobody sending

```go
func worker(jobs <-chan Job) {
    for job := range jobs { // LEAK: blocks forever if jobs channel is never closed
        process(job)
    }
}

go worker(jobs)
// If we never close(jobs), the goroutine blocks forever on range
```

**Fix: always close channels when done sending, or pass a done channel:**

```go
// Option 1: close the channel when done
close(jobs) // worker's range loop exits

// Option 2: context-aware worker
func worker(ctx context.Context, jobs <-chan Job) {
    for {
        select {
        case job, ok := <-jobs:
            if !ok { return }
            process(job)
        case <-ctx.Done():
            return
        }
    }
}
```

### Pattern 3: Goroutine waiting on context that's never cancelled

```go
func startBackgroundWorker(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                return // exits when ctx is cancelled
            case <-time.After(1 * time.Second):
                doPeriodicWork()
            }
        }
    }()
}

// LEAK: if caller passes context.Background() (never cancels),
// the goroutine runs forever — by design in this case.
// But if called per-request and each request gets a background context,
// you get one goroutine per request, all running forever.
startBackgroundWorker(context.Background()) // called on every request — LEAK
```

**Fix: use a service-level context or explicit stop channel:**

```go
type Service struct {
    stopCh chan struct{}
}

func (s *Service) Start() {
    s.stopCh = make(chan struct{})
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-s.stopCh:
                return
            case <-ticker.C:
                doPeriodicWork()
            }
        }
    }()
}

func (s *Service) Stop() {
    close(s.stopCh)
}
```

### Pattern 4: time.After in a goroutine's select loop

```go
func monitor(ch <-chan Event) {
    for {
        select {
        case event := <-ch:
            handle(event)
        case <-time.After(30 * time.Second): // LEAK: creates a new timer each iteration
            checkHealth()
        }
    }
}
```

`time.After(30s)` creates a new `time.Timer` and a new goroutine on every loop iteration. The old timer goroutines live for 30 seconds before expiring. Under load (many iterations per second), you accumulate thousands of timer goroutines.

**Fix: use `time.NewTicker` or `time.NewTimer` outside the loop:**

```go
func monitor(ctx context.Context, ch <-chan Event) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case event := <-ch:
            handle(event)
        case <-ticker.C:
            checkHealth()
        case <-ctx.Done():
            return
        }
    }
}
```

### Pattern 5: Goroutine leak via sync.WaitGroup misuse

```go
var wg sync.WaitGroup
for _, task := range tasks {
    wg.Add(1)
    go func(t Task) {
        // BUG: if process panics, Done() is never called
        process(t) // panic here
        wg.Done()  // never reached
    }(task)
}
wg.Wait() // blocks forever if any goroutine panics without recovering
```

**Fix: always `defer wg.Done()`:**

```go
go func(t Task) {
    defer wg.Done() // called even on panic (before panic unwinds)
    process(t)
}(task)
```

## Detecting Goroutine Leaks in Tests

Use `goleak` — the most popular goroutine leak detector for tests:

```bash
go get go.uber.org/goleak
```

```go
import "go.uber.org/goleak"

func TestMyFunction(t *testing.T) {
    defer goleak.VerifyNone(t) // fails if any goroutines leaked during the test

    // ... run test
}

// Or check at the end of main test:
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

`goleak` works by:
1. Snapshotting goroutines at the start of the test
2. Snapshotting goroutines at the end
3. Comparing — any new goroutines that are still running = leak

It knows about common "expected" goroutines (pprof, runtime gc) and filters them out.

## Detecting Goroutine Leaks in Production

```go
// Register a pprof handler (if not already done by importing _ "net/http/pprof")
import _ "net/http/pprof"

// Hit the endpoint to see all goroutine stacks:
// curl http://localhost:6060/debug/pprof/goroutine?debug=2

// Emit goroutine count as a metric (Prometheus example):
func recordGoroutineCount() {
    for range time.Tick(10 * time.Second) {
        goroutineGauge.Set(float64(runtime.NumGoroutine()))
    }
}
```

Alert on: goroutine count growing faster than request rate, goroutine count not returning to baseline after traffic drops.

## Real Production Example

We had a leak in our event-processing service. Each incoming event spawned a goroutine that called an external HTTP API. We used a 5-second timeout:

```go
func processEvent(event Event) {
    resultCh := make(chan APIResponse)

    go func() {
        resp, err := callAPI(event) // HTTP call — can take up to 5s
        if err == nil {
            resultCh <- resp // LEAK: if caller already timed out, nobody receives
        }
    }()

    select {
    case resp := <-resultCh:
        handle(resp)
    case <-time.After(5 * time.Second):
        log.Warn("API call timed out")
        return // goroutine is stuck on resultCh <- resp until API responds
    }
}
```

The API was flaky — it often took 10-15 seconds to respond. Every timed-out call left a goroutine stuck. After 12 hours of traffic, we had 8,000 leaked goroutines consuming ~16MB of stack space and holding open HTTP connections. The fix was trivial:

```go
resultCh := make(chan APIResponse, 1) // buffer 1 — goroutine can always send and exit
```

## Interview Questions

**Q: What is a goroutine leak and why is it dangerous?**

A goroutine that will never terminate. It holds its stack (2KB+), any referenced heap objects, and possibly OS resources (connections, file handles). In a server, leaked goroutines accumulate over time, causing memory growth, GC pressure, connection pool exhaustion, and eventual OOM.

**Q: What is the most common cause of goroutine leaks?**

A goroutine blocked on a channel send or receive where the other side has exited (caller timed out, context cancelled, channel never closed). The goroutine blocks forever. Fix: use buffered channels of capacity 1, always pass and check context, always close channels when done.

**Q: How do you detect goroutine leaks in tests?**

Use `go.uber.org/goleak`. Add `defer goleak.VerifyNone(t)` to tests or `goleak.VerifyTestMain(m)` to `TestMain`. It compares goroutine snapshots before and after the test and fails if new goroutines are still running at the end.

**Q: What is the problem with `time.After` inside a select loop?**

`time.After(d)` creates a new timer and its associated goroutine on every call. In a loop, the old timers don't get garbage collected until they fire (after duration `d`). Under high iteration rate, thousands of timer goroutines accumulate. Fix: use `time.NewTicker` or `time.NewTimer` outside the loop and reuse them.

## Key Takeaways

- Goroutine leaks are goroutines that block forever — they accumulate silently until OOM
- Most common cause: goroutine blocked on channel with no reader/writer after caller exits
- Fix for send leak: buffer the channel with capacity 1, or let goroutine check `ctx.Done()`
- Fix for receive leak: always close channels, or pass a done/context signal
- Never use `time.After` inside a tight loop — use `time.NewTicker` instead
- Always `defer wg.Done()` to handle panics
- Detect in tests: `go.uber.org/goleak`
- Detect in production: monitor `runtime.NumGoroutine()` as a metric, use pprof goroutine endpoint

---

# 17. Worker Pools

## What problem does it solve?

Naively launching one goroutine per task is simple but dangerous at scale. If you receive 100,000 incoming requests and launch a goroutine for each, you have 100,000 goroutines. Each needs ~2KB of stack minimum. That's 200MB of stacks alone — before counting any work the goroutines do. Worse, if each goroutine makes a DB query, you now have 100,000 concurrent DB connections, which your database will reject.

A **worker pool** bounds the concurrency. You launch a fixed number of goroutines (workers). Tasks are sent to a channel. Workers pick tasks up, process them, and go back to waiting. If all workers are busy, new tasks queue up in the channel (or are rejected if the queue is full — that's backpressure).

```
Without worker pool:
  1000 requests → 1000 goroutines → 1000 DB connections → DB overwhelmed

With worker pool of 20:
  1000 requests → 20 goroutines → 20 DB connections → DB happy
  remaining 980 tasks queued in channel
```

Worker pools are the most fundamental concurrency pattern in production Go services.

## The Basic Worker Pool

```go
func WorkerPool(ctx context.Context, numWorkers int, jobs <-chan Job) <-chan Result {
    results := make(chan Result, numWorkers)

    var wg sync.WaitGroup
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case job, ok := <-jobs:
                    if !ok {
                        return // channel closed, no more jobs
                    }
                    result := process(job)
                    results <- result
                case <-ctx.Done():
                    return // context cancelled
                }
            }
        }()
    }

    // Close results channel when all workers are done
    go func() {
        wg.Wait()
        close(results)
    }()

    return results
}
```

### Usage

```go
jobs := make(chan Job, 100) // buffered job queue
results := WorkerPool(ctx, 20, jobs) // 20 concurrent workers

// Feed jobs
go func() {
    defer close(jobs) // signal workers to stop when done
    for _, item := range bigList {
        select {
        case jobs <- Job{Item: item}:
        case <-ctx.Done():
            return
        }
    }
}()

// Collect results
for result := range results {
    handle(result)
}
```

## The Anatomy of a Production Worker Pool

Let's build a more complete one that handles errors, respects context, and includes metrics:

```go
type WorkerPool struct {
    numWorkers int
    jobs       chan func() error
    wg         sync.WaitGroup
    ctx        context.Context
    cancel     context.CancelFunc
}

func NewWorkerPool(ctx context.Context, numWorkers, queueSize int) *WorkerPool {
    ctx, cancel := context.WithCancel(ctx)
    p := &WorkerPool{
        numWorkers: numWorkers,
        jobs:       make(chan func() error, queueSize),
        ctx:        ctx,
        cancel:     cancel,
    }
    p.start()
    return p
}

func (p *WorkerPool) start() {
    for i := 0; i < p.numWorkers; i++ {
        p.wg.Add(1)
        go func(id int) {
            defer p.wg.Done()
            for {
                select {
                case job, ok := <-p.jobs:
                    if !ok {
                        return // pool shut down
                    }
                    if err := job(); err != nil {
                        log.Printf("worker %d: job error: %v", id, err)
                    }
                case <-p.ctx.Done():
                    return
                }
            }
        }(i)
    }
}

// Submit adds a job to the pool. Returns error if the pool is full or shut down.
func (p *WorkerPool) Submit(job func() error) error {
    select {
    case p.jobs <- job:
        return nil
    case <-p.ctx.Done():
        return fmt.Errorf("pool shut down")
    default:
        return fmt.Errorf("pool queue full")
    }
}

// SubmitWait adds a job, blocking until there's capacity.
func (p *WorkerPool) SubmitWait(job func() error) error {
    select {
    case p.jobs <- job:
        return nil
    case <-p.ctx.Done():
        return fmt.Errorf("pool shut down")
    }
}

// Shutdown waits for all in-flight jobs to finish.
func (p *WorkerPool) Shutdown() {
    close(p.jobs) // signal workers to drain and exit
    p.wg.Wait()
    p.cancel()
}
```

## How Many Workers?

This is the most frequently asked worker pool question in interviews. There's no universal answer, but there are frameworks:

### CPU-bound work

```
numWorkers = runtime.NumCPU()
// or
numWorkers = runtime.NumCPU() - 1  // leave one CPU for other goroutines
```

CPU-bound tasks (compression, encryption, parsing, computation) scale with CPU cores. Having more workers than CPUs just adds context-switching overhead. Rule of thumb: workers = GOMAXPROCS.

### I/O-bound work

```
numWorkers = (target_concurrency_for_downstream)
// Example: DB allows 100 connections
numWorkers = 100
// Example: target 50ms average response at 1000 req/s
// Little's Law: concurrency = throughput * latency = 1000 * 0.05 = 50
numWorkers = 50
```

I/O-bound tasks (DB queries, HTTP calls, file reads) spend most of their time waiting. More goroutines = more concurrent waits = more throughput, up to the downstream system's limit. Use **Little's Law**: L = λW (concurrency = arrival rate × average service time).

### The proper approach: benchmark and profile

```go
for workers := 1; workers <= 256; workers *= 2 {
    b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
        pool := NewWorkerPool(ctx, workers, 1000)
        defer pool.Shutdown()
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            pool.SubmitWait(func() error { return processItem() })
        }
    })
}
```

Benchmark with realistic load. Watch for: throughput plateau (you've hit the downstream bottleneck), latency increase (queue buildup), CPU saturation.

## Real Production Example — Image Processing Pipeline

At a media service, we needed to resize uploaded images to 5 different sizes. Image resizing is CPU-bound. We used a worker pool sized to GOMAXPROCS:

```go
type ResizeJob struct {
    ImageID string
    Data    []byte
    Sizes   []image.Point
}

type ResizeResult struct {
    ImageID string
    Sizes   map[string][]byte // size -> resized image bytes
    Err     error
}

func startImagePool(ctx context.Context) (chan<- ResizeJob, <-chan ResizeResult) {
    jobs    := make(chan ResizeJob, 500)    // queue up to 500 jobs
    results := make(chan ResizeResult, 500)

    numWorkers := runtime.NumCPU()
    var wg sync.WaitGroup

    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case job, ok := <-jobs:
                    if !ok { return }
                    sized := make(map[string][]byte)
                    var err error
                    for _, sz := range job.Sizes {
                        key := fmt.Sprintf("%dx%d", sz.X, sz.Y)
                        sized[key], err = resizeImage(job.Data, sz)
                        if err != nil { break }
                    }
                    select {
                    case results <- ResizeResult{ImageID: job.ImageID, Sizes: sized, Err: err}:
                    case <-ctx.Done():
                        return
                    }
                case <-ctx.Done():
                    return
                }
            }
        }()
    }

    go func() {
        wg.Wait()
        close(results)
    }()

    return jobs, results
}
```

With `numWorkers = runtime.NumCPU()` (8 on our machines), throughput was 8x single-goroutine. Adding more workers beyond the CPU count added overhead without improving throughput.

## Semaphore Pattern — Lightweight Worker Pool Alternative

For cases where you want to limit concurrency without a full worker pool, a buffered channel works as a semaphore:

```go
// Semaphore: buffered channel of capacity = max concurrency
sem := make(chan struct{}, 20) // max 20 concurrent goroutines

for _, item := range items {
    sem <- struct{}{} // acquire (blocks if 20 already running)
    go func(i Item) {
        defer func() { <-sem }() // release
        process(i)
    }(item)
}
// Wait for all to finish (drain the semaphore)
for i := 0; i < cap(sem); i++ {
    sem <- struct{}{}
}
```

This is simpler than a worker pool when you don't need a job queue — each goroutine is still launched per task, but only `cap(sem)` can run simultaneously.

## Common Mistakes

**Mistake 1: Closing the jobs channel from a producer while workers are still running.**

```go
// BUG: multiple producers — any can close the channel, causing panic when others try to send
for _, producer := range producers {
    go func(p Producer) {
        for job := range p.Jobs() {
            jobs <- job
        }
        close(jobs) // BUG: panics if another goroutine also closes or sends to jobs
    }(producer)
}

// Fix: use a WaitGroup to close only after all producers finish
var wg sync.WaitGroup
for _, producer := range producers {
    wg.Add(1)
    go func(p Producer) {
        defer wg.Done()
        for job := range p.Jobs() {
            jobs <- job
        }
    }(producer)
}
go func() {
    wg.Wait()
    close(jobs) // safe: all producers done
}()
```

**Mistake 2: Not respecting context cancellation in workers.**

Workers should check `ctx.Done()` on every iteration, or pass `ctx` to the work function so it can propagate cancellation downstream.

**Mistake 3: Forgetting to drain the results channel.**

If you have a results channel and stop reading it (due to error or early exit), workers block trying to send and the pool jams. Always use buffered results channels sized to the expected result count, or always drain with a goroutine.

## Interview Questions

**Q: How do you size a worker pool?**

CPU-bound: `runtime.NumCPU()`. I/O-bound: use Little's Law (L = λW), or set to the downstream system's max concurrency (connection pool size). Always benchmark under realistic load — the theoretical optimum and actual optimum often differ.

**Q: What is the difference between a worker pool and spawning one goroutine per task?**

Worker pool bounds concurrency to a fixed number, preventing resource exhaustion (memory, connections). One-goroutine-per-task is unbounded — under heavy load it creates thousands of goroutines, overwhelming downstream systems and consuming excessive memory. Worker pools also provide natural backpressure via the bounded job queue.

**Q: How do you gracefully shut down a worker pool?**

Close the jobs channel (signals workers to drain remaining jobs and exit after). Wait on a WaitGroup until all workers have exited. Optionally use context cancellation to interrupt in-flight work that can be cancelled.

## Key Takeaways

- Worker pools bound concurrency — prevent goroutine explosion and resource exhaustion
- Size: CPU-bound → NumCPU; I/O-bound → Little's Law or downstream resource limit
- Pattern: jobs channel (bounded queue) + N goroutine workers + results channel
- Always close jobs channel to signal workers to stop; wait on WaitGroup for graceful shutdown
- Workers must respect context cancellation for clean shutdown under deadline/cancel
- Semaphore pattern (buffered channel) is a lightweight alternative when goroutine-per-task is acceptable but needs bounding
- Buffered results channel prevents worker blocking when consumer is slow

---

# 18. Pipelines

## What problem does it solve?

A pipeline is a series of processing stages connected by channels. Each stage receives data from the previous stage, transforms it, and sends the result to the next stage. Stages run concurrently — while stage 2 is processing item N, stage 1 is already working on item N+1.

This is Go's answer to stream processing. Instead of loading all data into memory and processing it in phases, you process it as a stream: data flows through the pipeline one item at a time (or one batch at a time), keeping memory constant regardless of input size.

```
Stage 1: read      → Stage 2: parse    → Stage 3: enrich  → Stage 4: write
[item1] ──────────▶ [item1] ──────────▶ [item1] ──────────▶ [item1]
[item2] ──────────▶ [item2] ──────────▶ [item2] (items flow concurrently between stages)
```

## The Pipeline Pattern

Each stage is a function that takes an input channel and returns an output channel. The goroutine inside processes items from input and sends results to output:

```go
// Stage 1: generate integers
func generate(ctx context.Context, nums ...int) <-chan int {
    out := make(chan int, len(nums))
    go func() {
        defer close(out)
        for _, n := range nums {
            select {
            case out <- n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

// Stage 2: square each integer
func square(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            select {
            case out <- n * n:
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}

// Stage 3: filter even numbers
func filterEven(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            if n%2 == 0 {
                select {
                case out <- n:
                case <-ctx.Done():
                    return
                }
            }
        }
    }()
    return out
}

// Wire the pipeline: generate → square → filterEven
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    nums := generate(ctx, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10)
    squared := square(ctx, nums)
    evens := filterEven(ctx, squared)

    for n := range evens {
        fmt.Println(n) // prints: 4, 16, 36, 64, 100
    }
}
```

## Real Production Example — Log Processing Pipeline

We used pipelines to process server logs in batch. Log files could be GBs; we couldn't load them into memory:

```go
func buildLogPipeline(ctx context.Context, files []string) <-chan Report {
    // Stage 1: read log lines from files
    lines := readLines(ctx, files)

    // Stage 2: parse each line into a structured LogEntry
    entries := parseEntries(ctx, lines)

    // Stage 3: filter only ERROR entries
    errors := filterErrors(ctx, entries)

    // Stage 4: aggregate into reports
    return aggregate(ctx, errors)
}

func readLines(ctx context.Context, files []string) <-chan string {
    out := make(chan string, 1000)
    go func() {
        defer close(out)
        for _, f := range files {
            file, err := os.Open(f)
            if err != nil { continue }
            scanner := bufio.NewScanner(file)
            for scanner.Scan() {
                select {
                case out <- scanner.Text():
                case <-ctx.Done():
                    file.Close()
                    return
                }
            }
            file.Close()
        }
    }()
    return out
}
```

Memory usage stayed constant regardless of file size because only one line was in the pipeline at any moment.

## Cancellation in Pipelines

A critical requirement: if any stage exits early (context cancelled, error), all upstream and downstream stages must stop too. The pattern:

1. Every stage accepts a context
2. Every send/receive uses `select` with `ctx.Done()`
3. Every stage closes its output channel when it exits (signals downstream)
4. Upstream stages detect downstream closure via `ok` from `range`

```go
// Generic pipeline stage with cancellation
func stageN(ctx context.Context, in <-chan T) <-chan U {
    out := make(chan U)
    go func() {
        defer close(out) // always close — signals downstream stages
        for item := range in { // exits when in is closed (upstream stopped)
            result := transform(item)
            select {
            case out <- result:
            case <-ctx.Done(): // downstream cancelled or timed out
                return
            }
        }
    }()
    return out
}
```

## Key Takeaways

- Pipeline: series of stages connected by channels, each running concurrently
- Each stage: goroutine reading from input channel, writing to output channel
- Close output channel when stage exits — signals downstream stages to stop
- Every stage must respect context cancellation via `select` with `ctx.Done()`
- Memory-efficient: constant memory regardless of input size (streaming)
- Add buffering between stages to absorb speed mismatches without blocking

---

# 19. Fan-In / Fan-Out

## What problem does it solve?

**Fan-out**: take one stream of work and distribute it across multiple goroutines to parallelize processing.

**Fan-in**: take multiple streams of results and merge them into a single channel for the consumer.

These two patterns are often used together: fan-out to parallelize expensive work, fan-in to collect all the results.

```
Fan-Out:
  input ──▶ worker 1 ──▶
            worker 2 ──▶ (merged results)
            worker 3 ──▶

Fan-In:
  source 1 ──▶
  source 2 ──▶ (merged output channel)
  source 3 ──▶
```

## Fan-Out

```go
// Fan-out: distribute input across N workers
func fanOut(ctx context.Context, input <-chan Job, numWorkers int) []<-chan Result {
    outputs := make([]<-chan Result, numWorkers)
    for i := 0; i < numWorkers; i++ {
        outputs[i] = worker(ctx, input) // ALL workers read from the SAME input channel
    }
    return outputs
}

func worker(ctx context.Context, jobs <-chan Job) <-chan Result {
    out := make(chan Result)
    go func() {
        defer close(out)
        for job := range jobs {
            select {
            case out <- process(job):
            case <-ctx.Done():
                return
            }
        }
    }()
    return out
}
```

Multiple workers reading from the same channel is safe — Go channels are safe for concurrent access. Each item is received by exactly one worker (the first goroutine to complete the receive). This gives natural load balancing: busy workers don't receive new jobs until they finish the current one.

## Fan-In (Merge)

```go
// Fan-in: merge multiple channels into one
func fanIn(ctx context.Context, inputs ...<-chan Result) <-chan Result {
    out := make(chan Result)
    var wg sync.WaitGroup

    // One goroutine per input channel — forwards all items to out
    forward := func(ch <-chan Result) {
        defer wg.Done()
        for item := range ch {
            select {
            case out <- item:
            case <-ctx.Done():
                return
            }
        }
    }

    wg.Add(len(inputs))
    for _, ch := range inputs {
        go forward(ch)
    }

    // Close out when all forwarders finish
    go func() {
        wg.Wait()
        close(out)
    }()

    return out
}
```

## Complete Fan-Out / Fan-In Example

```go
func processItems(ctx context.Context, items []Item) []Result {
    // 1. Create input channel
    jobs := make(chan Item, len(items))
    for _, item := range items {
        jobs <- item
    }
    close(jobs)

    // 2. Fan-out: 8 workers all read from jobs
    numWorkers := 8
    workerOutputs := make([]<-chan Result, numWorkers)
    for i := 0; i < numWorkers; i++ {
        workerOutputs[i] = worker(ctx, jobs)
    }

    // 3. Fan-in: merge all worker outputs into a single channel
    merged := fanIn(ctx, workerOutputs...)

    // 4. Collect results
    var results []Result
    for r := range merged {
        results = append(results, r)
    }
    return results
}
```

## Real Production Example — Parallel API Enrichment

We had a list of user IDs and needed to call three APIs for each: profile, permissions, and preferences. We fan-out to three workers (one per API type) and fan-in the results:

```go
type UserData struct {
    ID          string
    Profile     *Profile
    Permissions []string
    Preferences *Preferences
}

func enrichUsers(ctx context.Context, userIDs []string) []UserData {
    type partial struct {
        id   string
        kind string  // "profile", "permissions", "preferences"
        data any
    }

    partials := make(chan partial, len(userIDs)*3)
    var wg sync.WaitGroup

    // Fan-out to three API workers
    for _, id := range userIDs {
        id := id
        wg.Add(3)
        go func() { defer wg.Done(); p, _ := getProfile(ctx, id);     partials <- partial{id, "profile", p} }()
        go func() { defer wg.Done(); p, _ := getPermissions(ctx, id); partials <- partial{id, "permissions", p} }()
        go func() { defer wg.Done(); p, _ := getPreferences(ctx, id); partials <- partial{id, "preferences", p} }()
    }
    go func() { wg.Wait(); close(partials) }()

    // Merge results by user ID
    users := make(map[string]*UserData)
    for p := range partials {
        if _, ok := users[p.id]; !ok {
            users[p.id] = &UserData{ID: p.id}
        }
        switch p.kind {
        case "profile":     users[p.id].Profile = p.data.(*Profile)
        case "permissions": users[p.id].Permissions = p.data.([]string)
        case "preferences": users[p.id].Preferences = p.data.(*Preferences)
        }
    }

    result := make([]UserData, 0, len(users))
    for _, u := range users {
        result = append(result, *u)
    }
    return result
}
```

All three API calls happen concurrently for each user, reducing total latency from 3x(API latency) to 1x(API latency).

## Interview Questions

**Q: What is the difference between fan-in and fan-out?**

Fan-out distributes work from one source to multiple concurrent workers (parallelizes processing). Fan-in merges results from multiple sources into a single channel (aggregates concurrent results). They're complementary: fan-out to parallelize, fan-in to collect.

**Q: How does Go naturally load-balance work across multiple workers reading from one channel?**

Multiple goroutines blocking on the same channel receive compete via Go's channel scheduler. The first goroutine to complete a receive gets the item. Busy workers naturally don't compete until they finish their current task. This gives automatic work-stealing style load balancing with zero additional code.

**Q: How do you ensure the fan-in merged channel gets closed?**

Use a WaitGroup: each forwarder goroutine calls `wg.Done()` when its input channel is closed. A separate goroutine calls `wg.Wait()` then `close(out)`. The merged channel is only closed when ALL input channels have been drained.

## Key Takeaways

- Fan-out: one input channel, N workers all reading from it — automatic load balancing
- Fan-in: N input channels merged into one via N forwarder goroutines + WaitGroup-controlled close
- Combined: fan-out to parallelize, fan-in to collect — the backbone of concurrent pipelines
- Always pass context to workers and check `ctx.Done()` in every select
- Fan-in merger closes its output channel only when all inputs are exhausted (via WaitGroup)
- Multiple goroutines reading from the same channel is safe — each item goes to exactly one goroutine

---

# 20. Backpressure

## What problem does it solve?

When a producer generates work faster than consumers can process it, you have a flow control problem. Without backpressure, the queue grows unboundedly — eventually consuming all available memory and crashing the process.

Backpressure is the mechanism by which a slow consumer signals the producer to slow down. In Go, this is elegantly handled by buffered channels: when the buffer fills up, the sender blocks. The producer naturally throttles to the consumer's speed.

```
Without backpressure:
  Producer: 10,000 items/sec → Channel (unbounded queue, grows forever) → Consumer: 1,000 items/sec
  → Memory OOM after seconds

With backpressure:
  Producer: 10,000 items/sec → Channel (buffer=100, fills up) → blocks producer
  → Producer slows to 1,000 items/sec (consumer's speed)
```

## Backpressure via Buffered Channels

```go
// Buffer size determines how much "burst" you can absorb
queue := make(chan Job, 1000) // absorb up to 1000 jobs without blocking

// Producer blocks when queue is full — automatic backpressure
func produce(jobs chan<- Job, items []Item) {
    for _, item := range items {
        jobs <- Job{Item: item} // blocks when queue full (consumer is slow)
    }
}

// Consumer drains at its own pace
func consume(jobs <-chan Job) {
    for job := range jobs {
        process(job) // slow OK — producer will block
    }
}
```

## Backpressure with Context (Timeout/Rejection)

Sometimes blocking is not acceptable — you'd rather reject excess work than slow the producer:

```go
func submitJob(ctx context.Context, queue chan<- Job, job Job) error {
    select {
    case queue <- job:
        return nil     // accepted
    default:
        return ErrQueueFull  // reject immediately (non-blocking check)
    }
}

// Or: wait up to 100ms before rejecting
func submitJobWithTimeout(queue chan<- Job, job Job) error {
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    select {
    case queue <- job:
        return nil
    case <-ctx.Done():
        return ErrQueueFull
    }
}
```

## Rate Limiting as Backpressure

```go
// time.Ticker as a rate limiter — process at most N items/second
func rateLimitedWorker(ctx context.Context, jobs <-chan Job, ratePerSec int) {
    ticker := time.NewTicker(time.Second / time.Duration(ratePerSec))
    defer ticker.Stop()

    for job := range jobs {
        select {
        case <-ticker.C:    // wait for the next tick before processing
            process(job)
        case <-ctx.Done():
            return
        }
    }
}

// golang.org/x/time/rate for more sophisticated rate limiting
import "golang.org/x/time/rate"
limiter := rate.NewLimiter(rate.Limit(100), 10) // 100 req/s, burst of 10
if err := limiter.Wait(ctx); err != nil {
    return err
}
process(job)
```

## Shedding Load — Dropping Work vs Blocking

When the system is overwhelmed, you have three choices:

```
1. Block (backpressure):  producer waits until consumer catches up
   Pro: no work lost
   Con: producer stalls, requests queue up, latency increases

2. Reject (load shedding): return error immediately when queue full
   Pro: fast failure, no queue buildup
   Con: work lost, caller must retry

3. Drop silently (sampling): process a fraction of work
   Pro: system stays responsive
   Con: work lost without caller knowing
```

For user-facing APIs: reject with an HTTP 429 (Too Many Requests) is almost always correct. Blocking means all your goroutines stall, which means your server becomes unresponsive. Load shed early and fast.

```go
func handler(w http.ResponseWriter, r *http.Request) {
    select {
    case workQueue <- r:
        // accepted
    default:
        // Queue full — reject with 429
        http.Error(w, "server too busy", http.StatusTooManyRequests)
        metrics.Increment("requests.rejected")
        return
    }
}
```

## Key Takeaways

- Backpressure prevents memory explosion when producers outpace consumers
- Buffered channels are Go's built-in backpressure mechanism — sender blocks when buffer full
- Non-blocking send with `default` case implements load shedding (reject instead of block)
- For user-facing APIs: reject with 429 rather than queuing forever
- Rate limiters (`time.Ticker`, `golang.org/x/time/rate`) add explicit throttling

---

# 21. Cancellation Patterns

## What problem does it solve?

Cancellation is the mechanism for stopping work that is no longer needed. This happens constantly in production:
- Client disconnects mid-request
- Request deadline exceeded
- Service shutting down gracefully
- Upstream result already found (or error occurred), no need to continue

Without proper cancellation, goroutines continue working on abandoned tasks, wasting CPU, holding connections, and causing goroutine leaks.

## Pattern 1: Context Cancellation (The Standard)

The primary cancellation mechanism in Go — already covered in Section 9. The key reminder:

```go
func doWork(ctx context.Context) error {
    // Check cancellation before each expensive step
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    // Pass ctx to every downstream call
    result, err := callAPI(ctx)
    if err != nil {
        return err  // ctx.Err() if cancelled
    }

    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
    }

    return saveResult(ctx, result)
}
```

## Pattern 2: Done Channel

Before `context` was added in Go 1.7, the done channel pattern was standard. Still useful for non-context-aware code or when you need fine-grained control:

```go
type Worker struct {
    done chan struct{}
}

func NewWorker() *Worker {
    return &Worker{done: make(chan struct{})}
}

func (w *Worker) Stop() {
    close(w.done) // broadcast to all goroutines simultaneously
}

func (w *Worker) Run() {
    for {
        select {
        case <-w.done:
            return // cancelled
        case job := <-w.jobs:
            w.process(job)
        }
    }
}
```

Closing a channel is a broadcast: ALL goroutines waiting on `<-w.done` unblock simultaneously. This is better than sending N signals to N goroutines (which requires knowing N in advance).

## Pattern 3: First-Result Wins (Early Cancellation)

Run multiple workers searching for a result. Cancel all others as soon as the first one succeeds:

```go
func firstResult(ctx context.Context, candidates []string) (string, error) {
    ctx, cancel := context.WithCancel(ctx)
    defer cancel() // cancel all remaining searches when we return

    resultCh := make(chan string, len(candidates)) // buffered to prevent leaks

    for _, candidate := range candidates {
        candidate := candidate
        go func() {
            result, err := search(ctx, candidate)
            if err == nil {
                resultCh <- result // first success — others will be cancelled
            }
        }()
    }

    select {
    case result := <-resultCh:
        return result, nil // got first result, defer cancel() stops others
    case <-ctx.Done():
        return "", ctx.Err()
    }
}
```

The `defer cancel()` call cancels the derived context when `firstResult` returns — all in-flight goroutines see `ctx.Done()` close and stop their searches.

## Pattern 4: Graceful Shutdown

For long-running services, shutdown needs to:
1. Stop accepting new work
2. Finish in-flight work
3. Release resources

```go
type Server struct {
    httpServer *http.Server
    wg         sync.WaitGroup
}

func (s *Server) Shutdown(ctx context.Context) error {
    // Step 1: stop accepting new requests
    // http.Server.Shutdown already handles this
    if err := s.httpServer.Shutdown(ctx); err != nil {
        return err
    }
    // Step 2: wait for in-flight handlers to finish
    // WaitGroup tracks active handlers
    done := make(chan struct{})
    go func() {
        s.wg.Wait()
        close(done)
    }()
    select {
    case <-done:
        return nil // all handlers finished
    case <-ctx.Done():
        return ctx.Err() // timeout — forceful shutdown
    }
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    s.wg.Add(1)
    defer s.wg.Done()
    handle(w, r)
}
```

## Pattern 5: OS Signal Handling for Graceful Shutdown

```go
func main() {
    server := NewServer()
    go server.Start()

    // Wait for OS signal
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    log.Println("shutting down...")
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := server.Shutdown(ctx); err != nil {
        log.Printf("forced shutdown: %v", err)
    }
    log.Println("server stopped")
}
```

## Pattern 6: Tombstone / Poison Pill

For worker pools where you need to cleanly shut down workers, send a special "poison pill" value:

```go
type Job struct {
    Data    any
    IsDone  bool // poison pill
}

// Send poison pill to stop worker
jobs <- Job{IsDone: true}

// Worker checks for poison pill
func worker(jobs <-chan Job) {
    for job := range jobs {
        if job.IsDone {
            return
        }
        process(job)
    }
}
```

In modern Go, simply closing the channel (`close(jobs)`) is cleaner than poison pills. Reserve poison pills for situations where you need to stop specific workers selectively.

## Interview Questions

**Q: What's the difference between closing a done channel and sending on it?**

Closing a done channel is a broadcast — all goroutines waiting on `<-done` unblock simultaneously. Sending on a done channel signals only one goroutine (the one that receives). For shutdown signals that need to reach all goroutines simultaneously, always close the channel.

**Q: In the first-result-wins pattern, how do you prevent goroutine leaks?**

Use a buffered results channel (capacity = number of goroutines). Even if the first result is returned and `cancel()` is called, remaining goroutines can still send their results to the buffered channel without blocking. They'll see `ctx.Done()` on their next select iteration and exit cleanly.

**Q: How do you implement graceful shutdown of an HTTP server?**

Call `http.Server.Shutdown(ctx)` — it closes the listener (stops accepting new connections), waits for active connections to finish, and returns. The context deadline sets the maximum time to wait. Track in-flight handlers with a WaitGroup and wait for them after Shutdown returns.

## Key Takeaways

- Context is the standard cancellation mechanism — pass to every function, check `ctx.Done()`
- Closing a channel broadcasts to all goroutines simultaneously — use for shutdown signals
- First-result-wins: `defer cancel()` in caller + buffered results channel + goroutines check ctx
- Graceful shutdown: stop accepting new work → finish in-flight work → release resources
- OS signal handling: `signal.Notify` + `context.WithTimeout` for forced-shutdown deadline
- Always use buffered channels in cancellation patterns to prevent goroutine leaks

---

# 22. Concurrency Debugging

## What problem does it solve?

Concurrency bugs are in a different category from ordinary bugs. Ordinary bugs reproduce deterministically — same input, same output, same crash. Concurrency bugs depend on goroutine scheduling, CPU count, system load, and timing. They appear in production under load, vanish on your laptop, disappear when you add a print statement, and come back unpredictably.

You need a toolkit specifically designed for this. The good news: Go has excellent built-in tooling that no other language matches.

## Tool 1: The Race Detector (`-race`)

Already covered in Section 14, but worth restating as the first tool you should reach for.

```bash
go test -race ./...
go run -race main.go
go build -race -o app-debug .
```

Run it in CI. Always. The performance cost (~5-10x) is irrelevant in CI. The bugs it catches are catastrophic in production.

What it detects: data races — concurrent unsynchronized access to the same memory. What it does NOT detect: logic races, deadlocks, goroutine leaks.

```
==================
WARNING: DATA RACE
Write at 0x00c000018068 by goroutine 7:
  main.(*Counter).Inc()
      /tmp/main.go:15 +0x44

Previous write at 0x00c000018068 by goroutine 6:
  main.(*Counter).Inc()
      /tmp/main.go:15 +0x44
==================
```

The output tells you exactly which goroutines raced, at which lines, and where those goroutines were created. This is everything you need.

## Tool 2: Goroutine Dump (SIGQUIT / runtime.Stack)

When your program hangs, deadlocks, or has goroutine leaks, dump all goroutines:

### Method 1: SIGQUIT (Ctrl+\ on Linux/Mac)
```bash
kill -SIGQUIT <pid>
# or Ctrl+\ in terminal
```

Output on stderr:
```
goroutine 1 [chan receive]:
main.main()
    /tmp/main.go:23 +0x84

goroutine 6 [sleep]:
time.Sleep(0x3b9aca00)
    /usr/local/go/src/runtime/time.go:195 +0xd2
...
```

### Method 2: Programmatic goroutine dump

```go
func dumpGoroutines() {
    buf := make([]byte, 1<<20) // 1MB buffer
    n := runtime.Stack(buf, true) // true = ALL goroutines
    fmt.Printf("=== GOROUTINE DUMP ===\n%s\n", buf[:n])
}

// Or write to a file for large dumps:
func dumpGoroutinesToFile(path string) {
    f, _ := os.Create(path)
    defer f.Close()
    buf := make([]byte, 32<<20) // 32MB
    n := runtime.Stack(buf, true)
    f.Write(buf[:n])
}
```

### Reading a goroutine dump

```
goroutine 42 [chan receive, 30 minutes]:   ← state and how long it's been stuck
main.(*Server).processRequest(0xc0000b4000)
    /app/server.go:87 +0x1a4              ← exact location in code
created by main.(*Server).handleConn      ← who created this goroutine
    /app/server.go:55 +0x98
```

Key goroutine states to look for:
- `[chan receive]` — waiting to receive from channel (potential leak)
- `[chan send]` — waiting to send to channel (potential leak or backpressure)
- `[semacquire]` — waiting for a mutex (potential deadlock or contention)
- `[sleep]` — in `time.Sleep` (normal)
- `[IO wait]` — waiting on network/file IO (normal)
- `[running]` — actively executing
- `[syscall]` — in a system call
- `[GC assist mark]` — helping with garbage collection

If you see many goroutines in `[chan receive]` or `[semacquire]` and the duration is growing, you have a leak or deadlock.

## Tool 3: Delve — Go's Debugger

`dlv` (Delve) is the Go debugger. Unlike other languages, it understands goroutines natively:

```bash
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug a running process
dlv attach <pid>

# Run with debugger
dlv debug ./main.go
```

Inside Delve, goroutine-aware commands:

```
(dlv) goroutines                        # list all goroutines
  Goroutine 1 - User: /app/main.go:23 main.main (0x...)
  Goroutine 6 - User: /app/server.go:87 main.(*Server).processRequest (0x...)
  [30 goroutines]

(dlv) goroutine 6                       # switch to goroutine 6
Switched from 1 to 6 (thread 1234)

(dlv) bt                                # backtrace of goroutine 6
0  0x... in runtime.chanrecv1
1  0x... in main.(*Server).processRequest /app/server.go:87
2  0x... in main.(*Server).handleConn   /app/server.go:55

(dlv) frame 1                           # jump to stack frame 1
> main.(*Server).processRequest() /app/server.go:87 (hits)
    82:     ...
    87: →   result := <-s.resultCh      // goroutine stuck here
    88:     ...

(dlv) locals                            # inspect local variables
s = (*main.Server)(0xc0000a4000)
ctx = context.Background.WithCancel
```

Delve is invaluable for production debugging — you can attach to a live process, inspect goroutine states without stopping the process, and examine variables.

## Tool 4: escape analysis and compiler output

Sometimes concurrency problems stem from unexpected heap allocations. Check what the compiler decides to put on the heap:

```bash
go build -gcflags="-m" ./...
# or more verbose:
go build -gcflags="-m=2" ./...
```

Output:
```
./main.go:14:6: moved to heap: counter
./main.go:22:12: &sync.Mutex literal escapes to heap
./main.go:44:14: make(chan int) escapes to heap
```

Understanding escape analysis helps you minimize allocations in hot paths — fewer allocations means less GC pressure, which means fewer GC-induced goroutine pauses.

## Tool 5: GODEBUG and runtime environment variables

Go's runtime exposes debugging knobs via the `GODEBUG` environment variable:

```bash
# Print GC details on every collection
GODEBUG=gctrace=1 ./app

# Print scheduler traces (P utilization every N goroutine scheduling events)
GODEBUG=schedtrace=1000 ./app   # print every 1000ms

# Combined scheduler + GC
GODEBUG=schedtrace=1000,scheddetail=1 ./app

# Disable TLS session tickets (for debugging TLS)
GODEBUG=tlsrsakey=0

# Print mutex profile info
GODEBUG=mutexprofile=1 ./app
```

### Scheduler trace output explained

```
SCHED 1000ms: gomaxprocs=8 idleprocs=0 threads=10 spinningthreads=1
runqueue=0 [2 0 0 0 1 0 0 3]
```

- `gomaxprocs=8` — using 8 Ps
- `idleprocs=0` — all Ps are busy (good for CPU-bound, bad if I/O-bound)
- `threads=10` — 10 OS threads alive
- `spinningthreads=1` — 1 thread spinning (looking for work)
- `runqueue=0` — global run queue is empty
- `[2 0 0 0 1 0 0 3]` — per-P local run queue sizes (P0 has 2, P3 has 1, P7 has 3)

If you see consistently high `runqueue` values, you have more runnable goroutines than can execute — you're CPU-saturated or have goroutine congestion.

### GC trace output explained

```
gc 1 @0.012s 2%: 0.014+0.39+0.003 ms clock, 0.11+0.17/0.39/0+0.027 ms cpu, 4->4->2 MB, 5 MB goal, 8 P
```

- `gc 1` — first GC cycle
- `@0.012s` — 12ms into program lifetime
- `2%` — GC used 2% of total CPU
- `0.014+0.39+0.003 ms clock` — wall time: STW sweep + concurrent mark + STW finalize
- `4->4->2 MB` — heap before GC → after marking → after sweeping
- `8 P` — used 8 procs for GC assist

High `%` (>5-10%) means GC is thrashing — allocating too much. Increasing `GOGC` or using `sync.Pool` helps.

## Tool 6: Deadlock Detection via Stack Analysis

When you suspect a deadlock, get a goroutine dump and look for cycles:

```bash
# Step 1: get goroutine dump
kill -SIGQUIT <pid> 2>&1 | tee goroutine_dump.txt

# Step 2: grep for blocked goroutines
grep -A 5 "semacquire\|chan send\|chan receive" goroutine_dump.txt
```

Look for:
1. Goroutine A waiting for a lock
2. Goroutine B holding that lock, waiting for another lock
3. Goroutine C holding the second lock, waiting for goroutine A's lock → CYCLE

## Debugging Checklist

When you see a concurrency bug in production, work through this checklist:

```
1. Is it a data race?
   → Run with -race, look at the output

2. Is it a goroutine leak?
   → Check runtime.NumGoroutine() growth over time
   → Get goroutine dump, look for many goroutines in same blocked state

3. Is it a deadlock?
   → Does the program hang completely? → total deadlock, runtime panics
   → Do specific requests hang? → partial deadlock
   → Get goroutine dump, look for lock cycles

4. Is it a logic race?
   → Check-then-act patterns, multiple lock acquisitions for one operation
   → -race won't catch these, must code review

5. Is it a performance problem?
   → High mutex contention? → use pprof mutex profile
   → Too many GC pauses? → use GODEBUG=gctrace=1
   → Scheduler bottleneck? → use GODEBUG=schedtrace=1000
   → Use go tool trace for fine-grained analysis
```

## Interview Questions

**Q: What concurrency debugging tools does Go provide?**

Race detector (`-race`): detects data races. Goroutine dump (SIGQUIT / `runtime.Stack`): shows all goroutine states and stacks. Delve (`dlv`): goroutine-aware debugger, attach to live process. `GODEBUG=schedtrace`: scheduler utilization. `GODEBUG=gctrace`: GC stats. `pprof`: CPU, memory, mutex, goroutine profiles. `go tool trace`: microsecond-level scheduler and goroutine event traces.

**Q: How do you diagnose a goroutine stuck in `[chan receive, 45 minutes]`?**

It's blocked waiting to receive from a channel for 45 minutes. The sender either: never sent anything (channel is empty and never will be), already exited without closing the channel, or got stuck itself. Look at the goroutine's stack to find which channel it's waiting on. Find who owns that channel and trace back to why no send occurred. Fix: always close channels when done sending, or pass context to detect abandonment.

**Q: What does `idleprocs=0` in a schedtrace mean?**

All GOMAXPROCS processors are actively running goroutines (or spinning looking for work). For CPU-bound work, this means full utilization — good. For I/O-bound work, this could indicate too many runnable goroutines and scheduler pressure, or it could mean you're CPU-saturated.

## Key Takeaways

- Race detector (`-race`): always run in CI — catches data races with exact call stacks
- Goroutine dump (SIGQUIT / `runtime.Stack`): diagnose leaks, deadlocks, hung goroutines
- Delve: attach to live processes, inspect goroutine states and variables
- `GODEBUG=schedtrace`: see scheduler utilization per-P in real time
- `GODEBUG=gctrace`: see GC frequency, pause times, heap sizes
- Read goroutine states: `[chan receive]`, `[semacquire]` with long durations = problem
- Deadlock diagnosis: find lock cycles in goroutine dumps

---

# 23. pprof — Profiling Go Programs

## What problem does it solve?

Performance problems in concurrent Go programs are rarely obvious. "My service is slow" could mean: CPU saturated (goroutines doing too much computation), memory pressure (GC running too often), mutex contention (goroutines waiting for locks), or goroutine leaks (thousands of idle goroutines consuming resources).

`pprof` is Go's built-in profiler. It captures statistical samples of your program's behavior at runtime and presents them in interactive flame graphs, call graphs, and sorted tables. It tells you not just *that* something is slow, but *exactly which function* is responsible and *why*.

## Enabling pprof in Your Server

The easiest way — add one import to your `main.go`:

```go
import _ "net/http/pprof" // registers pprof handlers on DefaultServeMux
```

Then make sure the default mux is listening:

```go
go http.ListenAndServe(":6060", nil) // pprof served on :6060
```

**Security note**: Never expose pprof on a public port. Always bind to localhost or an internal port behind authentication.

Available endpoints:
```
http://localhost:6060/debug/pprof/           # index of all profiles
http://localhost:6060/debug/pprof/goroutine  # goroutine stacks
http://localhost:6060/debug/pprof/heap       # heap allocations
http://localhost:6060/debug/pprof/cpu        # CPU profile (30s sample)
http://localhost:6060/debug/pprof/mutex      # mutex contention
http://localhost:6060/debug/pprof/block      # goroutine blocking
http://localhost:6060/debug/pprof/threadcreate # thread creation
```

## Profile Types and When to Use Each

### CPU Profile — "My service uses too much CPU"

```bash
# Capture 30-second CPU profile from a running server:
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# From a test:
go test -cpuprofile=cpu.prof -bench=. ./...
go tool pprof cpu.prof
```

Inside pprof interactive mode:
```
(pprof) top10             # top 10 functions by CPU time
(pprof) top10 -cum        # top 10 by cumulative (includes callees)
(pprof) list FunctionName # annotated source with CPU time per line
(pprof) web               # open flame graph in browser (requires graphviz)
```

Example output:
```
      flat  flat%   sum%        cum   cum%
     520ms 26.00% 26.00%      520ms 26.00%  runtime.mallocgc
     200ms 10.00% 36.00%      200ms 10.00%  sync.(*Mutex).Lock
     180ms  9.00% 45.00%     1200ms 60.00%  main.(*Handler).processRequest
```

- `flat` — time spent IN this function (not callees)
- `cum` — time spent in this function + all functions it calls
- `processRequest` at 9% flat but 60% cumulative: it calls many things

### Heap Profile — "My service uses too much memory"

```bash
# Heap profile from running server:
go tool pprof http://localhost:6060/debug/pprof/heap

# Common heap profile options:
go tool pprof -alloc_space http://localhost:6060/debug/pprof/heap  # total bytes allocated (includes freed)
go tool pprof -inuse_space http://localhost:6060/debug/pprof/heap  # currently live bytes
go tool pprof -alloc_objects http://localhost:6060/debug/pprof/heap # allocation count
```

Heap profile shows you which functions are allocating the most memory. Highest `alloc_space` functions are your GC pressure sources.

### Mutex Profile — "My service has high latency under contention"

Mutex profiles show you which mutexes are being waited on the most — and which function is holding them when others are waiting.

```go
// Must explicitly enable mutex profiling at startup:
runtime.SetMutexProfileFraction(1) // sample every mutex contention event
// or:
runtime.SetMutexProfileFraction(5) // sample 1 in 5 events (lower overhead)
```

```bash
go tool pprof http://localhost:6060/debug/pprof/mutex
```

```
(pprof) top5
      flat  flat%   sum%        cum   cum%
     800ms 40.00% 40.00%      800ms 40.00%  sync.(*Mutex).Unlock
     300ms 15.00% 55.00%      300ms 15.00%  main.(*Cache).Get
```

This tells you: 800ms of goroutine wait time was caused by `sync.(*Mutex).Unlock` — meaning goroutines were waiting for this mutex to be released. The contended lock is in `main.(*Cache).Get`.

### Goroutine Profile — "My goroutine count is too high"

```bash
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

Shows a tree of goroutine counts grouped by creation site. If you see `1000 @ main.(*Server).handleConn`, you have 1000 goroutines all created from the same place — likely a goroutine leak.

### Block Profile — "My goroutines spend too much time blocked"

Block profiles show which channel sends/receives and mutex locks caused goroutines to block, and for how long.

```go
runtime.SetBlockProfileRate(1) // sample every blocking event
```

```bash
go tool pprof http://localhost:6060/debug/pprof/block
```

## The Flame Graph — Your Best Friend

```bash
# Generate interactive SVG flame graph
go tool pprof -http=:8080 cpu.prof
# Opens browser with interactive flame graph
```

The flame graph visualizes the call stack. The width of each box is proportional to the time spent in that function. Tall stacks with wide boxes at the top are hot paths:

```
 ┌──────────────────────────────────────────────────────┐
 │                    main.main (100%)                  │
 ├────────────────────┬─────────────────────────────────┤
 │ http.(*Server)     │  other (5%)                     │
 │ .Serve (95%)       │                                 │
 ├─────────┬──────────┤                                 │
 │ handle  │ accept   │                                 │
 │ (90%)   │ (5%)     │                                 │
 ├──┬──────┤          │                                 │
 │db│cache │          │                                 │
 │  │query │                                            │
 └──┴──────┴──────────────────────────────────────────-─┘
```

Wide boxes = expensive functions. Drill down by clicking to see what's inside.

## Real Production Workflow

When we saw p99 latency spike from 20ms to 500ms under load:

```bash
# 1. Capture CPU profile under load
go tool pprof http://prod-service:6060/debug/pprof/profile?seconds=30

# 2. Open interactive flame graph
(pprof) web

# 3. Found: 60% of CPU in json.Marshal
#    Solution: switch to a faster JSON library or cache serialized results

# 4. Check mutex contention
go tool pprof http://prod-service:6060/debug/pprof/mutex
(pprof) top10
#    Found: sync.(*RWMutex).Lock in config.Get called 50,000/sec
#    Solution: cache config reads in request context

# 5. After fixes, p99 back to 25ms
```

## Interview Questions

**Q: What is the difference between flat and cumulative time in a CPU profile?**

Flat time is time spent executing code directly inside that function, excluding time spent in functions it calls. Cumulative time includes all time from when the function is entered to when it returns — including all callees. A function with low flat but high cumulative is a bottleneck coordinator (calls expensive functions). A function with high flat is directly doing expensive work.

**Q: How do you enable mutex profiling?**

Call `runtime.SetMutexProfileFraction(n)` at startup (where n≥1 enables sampling). Then hit `/debug/pprof/mutex`. Without this call, the mutex profile is always empty.

**Q: What does a heap profile's `alloc_space` vs `inuse_space` difference tell you?**

`alloc_space` shows all bytes allocated since the program started (including GC'd objects). High `alloc_space` = GC pressure (lots of short-lived allocations). `inuse_space` shows currently live bytes. High `inuse_space` = memory leak (objects not being freed). If `alloc_space` is high but `inuse_space` is normal, you have allocation churn — add pooling.

## Key Takeaways

- Enable with `import _ "net/http/pprof"` + expose on internal port
- CPU profile: find which functions consume the most CPU time
- Heap profile (`-alloc_space`): find GC pressure sources; (`-inuse_space`): find memory leaks
- Mutex profile (`SetMutexProfileFraction(1)`): find lock contention hot spots
- Block profile (`SetBlockProfileRate(1)`): find channel and lock blocking hot spots
- Goroutine profile: diagnose goroutine leaks by creation site
- Flame graph (`-http=:8080`): visual drill-down of call stacks and hot paths

---

# 24. go tool trace — Scheduler and Latency Analysis

## What problem does it solve?

`pprof` gives you aggregate statistics: which functions consumed the most CPU overall. But it cannot tell you *why a specific request was slow* — which goroutines it created, how long each waited to be scheduled, whether GC paused everything for 1ms mid-request.

`go tool trace` captures a complete timeline of every goroutine event: creation, scheduling, blocking, unblocking, GC events, syscalls. It's microsecond-level visibility into the scheduler itself. When you need to understand latency — not throughput — this is the tool.

## Capturing a Trace

### From a test or benchmark:

```bash
go test -trace=trace.out ./...
go tool trace trace.out
```

### From a running server:

```bash
# Capture 5 seconds of trace
curl http://localhost:6060/debug/pprof/trace?seconds=5 > trace.out
go tool trace trace.out
```

### Programmatically:

```go
import "runtime/trace"

f, _ := os.Create("trace.out")
defer f.Close()

trace.Start(f)
defer trace.Stop()

// ... code to trace
```

## Reading the Trace Viewer

`go tool trace` opens a browser UI with multiple views:

### View 1: Timeline / Goroutine View

```
Time →
P0: [G1 running][G1 blocked on chan][──────][G3 running][G3 syscall][G3 run]
P1: [G2 running][──────────────────][G2 run][──────────────────────────────]
P2: [──────────][G4 running][G4 GC assist][G4 running][───────────────────]
GC: [────────────────────────────────][GC STW][GC mark][──────────────────]
```

Each row is a processor (P). Colored boxes are goroutines running on that P. Gaps are idle time. You can zoom in to microsecond level and click any box to see the goroutine's details.

### What to look for:

**Long gaps between goroutine runs**: A goroutine was runnable but not scheduled. This means the scheduler was busy with other goroutines — too much concurrency for the number of Ps, or one goroutine monopolizing a P.

**GC stop-the-world pauses**: Vertical red bars across all Ps simultaneously. If these are long (>1ms), your heap is too large or you're allocating too much.

**Goroutine creation bursts**: Thousands of goroutines created in a short window — look for unbounded `go` launches.

**Blocking events**: Goroutine blocked on channel/mutex, with visible wait time before unblocking.

### View 2: Analysis — Goroutine statistics

```
Goroutine analysis

goroutine 42 (main.(*Handler).ServeHTTP)
  Total time: 45.2ms
    Execution time:   12.1ms   (26.8%)   ← actually running on CPU
    Network wait:      8.4ms   (18.6%)   ← waiting for network
    Sync block:       18.3ms   (40.5%)   ← blocked on mutex/channel
    Scheduler delay:   4.2ms    (9.3%)   ← runnable but not scheduled
    GC sweeping:       2.2ms    (4.9%)   ← participating in GC
```

This breakdown tells you exactly where a goroutine spent its 45ms. If "Scheduler delay" is high, you have scheduling pressure. If "Sync block" is high, you have contention. If "GC sweeping" is high, you need to reduce allocations.

## Annotating Your Code for Trace Visibility

You can add custom regions and tasks to the trace, making your own code visible:

```go
import "runtime/trace"

func processRequest(ctx context.Context, r *Request) {
    // Create a task — spans across goroutines
    ctx, task := trace.NewTask(ctx, "processRequest")
    defer task.End()

    // Create a region — marks a code section within a goroutine
    trace.WithRegion(ctx, "validateRequest", func() {
        validate(r)
    })

    trace.WithRegion(ctx, "fetchData", func() {
        fetchFromDB(ctx, r)
    })

    trace.WithRegion(ctx, "renderResponse", func() {
        render(ctx, r)
    })
}
```

In the trace viewer, your custom regions appear color-coded in the goroutine timeline. You can filter by task to see the complete lifecycle of a single request across all goroutines it spawned.

## Practical Example — Diagnosing a Latency Spike

We had a service where p99 was 100ms but p50 was 2ms — a clear scheduling or GC issue (not a CPU issue, since median was fast).

```bash
# Capture trace during a load test
go test -trace=trace.out -bench=BenchmarkHandler ./...
go tool trace trace.out
```

In the trace viewer:
1. Zoomed into a slow request (100ms)
2. Found: the goroutine was runnable for 80ms before being scheduled
3. Root cause: we had 10,000 goroutines spawned during initialization (test setup bug)
4. The scheduler was round-robining through all 10,000 goroutines, giving each only ~10µs per round
5. A request goroutine had to wait for 10,000 × 10µs ≈ 100ms to get scheduled

Fix: reduce goroutine count. p99 dropped to 3ms.

## Trace vs pprof — When to Use Which

```
pprof:
  - "What is consuming the most CPU/memory over time?"
  - Statistical aggregate over many seconds
  - Best for throughput problems

go tool trace:
  - "Why did THIS specific request take 100ms?"
  - Complete event timeline, microsecond granularity
  - Best for latency tail problems (p99, p999)
  - Shows GC pauses, scheduler delays, blocking events
```

In practice: start with pprof, switch to trace when you need to understand why individual requests are slow.

## Interview Questions

**Q: What is the difference between pprof and go tool trace?**

pprof is a statistical profiler — it samples the call stack periodically and aggregates over time, giving you "which functions used the most CPU overall." `go tool trace` captures a complete chronological timeline of every goroutine event (scheduling, blocking, GC, syscalls) at microsecond resolution. pprof answers "what was hot," trace answers "why was this request slow."

**Q: What does scheduler delay tell you in a trace goroutine analysis?**

Time a goroutine was runnable (ready to run) but wasn't scheduled on a P. High scheduler delay means either: too many runnable goroutines competing for few Ps, or long-running goroutines preventing preemption. Fix: reduce goroutine count, or use `runtime.Gosched()` in long loops to allow preemption.

**Q: How do you add your own annotations to a trace?**

Use `trace.NewTask(ctx, "name")` for request-spanning tasks and `trace.WithRegion(ctx, "name", func)` for within-goroutine sections. These appear in the trace viewer and help correlate low-level scheduler events with your application's logical operations.

## Key Takeaways

- `go tool trace` captures every scheduler event — goroutine lifecycle, GC, syscalls, blocking
- Opens browser-based timeline: see exactly what happened, when, on which P
- Goroutine analysis view: execution time, network wait, sync block, scheduler delay, GC time broken down per goroutine
- Use for tail latency problems (p99) where pprof aggregate stats miss individual slow requests
- Annotate with `trace.NewTask` / `trace.WithRegion` for request-level visibility
- Common findings: scheduler delay from too many goroutines, GC STW pauses, lock contention

---

# 25. Production Tuning

## What problem does it solve?

Running Go concurrency correctly in production means more than just writing race-free code. It means configuring the runtime, tuning GC behavior, managing OS thread counts, and understanding how containerization affects your service. Default settings work fine in development but can be disastrous in production — especially in Kubernetes where CPU and memory limits create a mismatch between what Go thinks is available and what actually is.

## GOMAXPROCS — The Most Important Runtime Setting

`GOMAXPROCS` controls how many OS threads can execute Go code simultaneously. It defaults to the number of logical CPUs available to the process.

```go
import "runtime"

fmt.Println(runtime.GOMAXPROCS(0)) // query current value (0 = don't change)
runtime.GOMAXPROCS(4)               // set to 4
```

### The Container Problem

In Kubernetes (and Docker), your pod has a CPU limit — say, 2 CPUs. But `runtime.NumCPU()` reports the number of CPUs on the *host machine*, not the container's limit. If the host has 64 CPUs, Go sets GOMAXPROCS=64 — creating 64 OS threads, all fighting for 2 CPU cores.

```
Host: 64 CPUs
Container limit: 2 CPUs
runtime.GOMAXPROCS() = 64 (wrong!)

64 goroutine Ps fighting for 2 CPU cores → excessive context switching → degraded performance
```

**Fix: use `go.uber.org/automaxprocs`**

```go
import _ "go.uber.org/automaxprocs" // reads CPU quota from cgroups

// Just importing it sets GOMAXPROCS to the container's CPU quota at startup
// For a 2-CPU limited container: GOMAXPROCS = 2
```

This is the single most impactful change you can make to a containerized Go service. Always use it.

### Manual GOMAXPROCS tuning

```go
// CPU-bound services: GOMAXPROCS = numCPUs (default, usually correct)
// I/O-bound services: GOMAXPROCS can sometimes be slightly lower than numCPUs
//   because goroutines spend most of their time in syscalls (handled by extra threads)
//   and fewer Ps means less scheduler overhead

// Never set GOMAXPROCS lower than 2 in production — you lose the benefits of
// concurrent GC, background work, etc.
```

## GOGC — Garbage Collector Tuning

`GOGC` controls how aggressively the GC runs. It's the target heap growth percentage before triggering a GC:

```
GOGC=100 (default): GC runs when live heap doubles
  heap at 50MB → GC runs at 100MB

GOGC=200: GC runs when live heap triples
  heap at 50MB → GC runs at 150MB
  → Less frequent GC, more memory used

GOGC=50: GC runs more frequently (heap grows 50% before GC)
  heap at 50MB → GC runs at 75MB
  → More frequent GC, less memory used

GOGC=off: GC disabled entirely (only for benchmarks/tools, never production)
```

```bash
GOGC=200 ./app  # set via environment
```

Or programmatically:
```go
import "runtime/debug"
debug.SetGCPercent(200)
```

### When to increase GOGC

- Your service has high allocation rate and GC is consuming >5% CPU
- You have plenty of memory headroom in your container
- GC pauses are causing latency spikes

### When to decrease GOGC (or keep default)

- Your container has a tight memory limit
- Memory efficiency is more important than GC overhead

### GOMEMLIMIT — The Better Approach (Go 1.19+)

Instead of tuning GOGC, Go 1.19 introduced `GOMEMLIMIT` — a hard memory limit that tells the GC to run more aggressively if needed to stay under the limit:

```bash
GOMEMLIMIT=2GiB ./app
```

```go
import "runtime/debug"
debug.SetMemoryLimit(2 * 1024 * 1024 * 1024) // 2GB
```

With `GOMEMLIMIT`, you no longer need to tune `GOGC` — set the limit to (container memory - some headroom), and the GC adapts automatically:

```
Container memory: 4GB
GOMEMLIMIT: 3.5GB (leave 500MB headroom for non-Go allocations and OS)
GC: automatically adjusts frequency to keep heap under 3.5GB
```

This eliminates OOM kills from GC being too lazy. This is now the recommended approach in production.

## Connection Pool Sizing

```go
// database/sql connection pool
db, _ := sql.Open("postgres", connStr)
db.SetMaxOpenConns(25)          // max concurrent connections to DB
db.SetMaxIdleConns(5)           // keep up to 5 idle connections
db.SetConnMaxLifetime(5 * time.Minute) // recycle connections every 5 min
db.SetConnMaxIdleTime(1 * time.Minute) // close idle connections after 1 min
```

Rule of thumb for `MaxOpenConns`: `(GOMAXPROCS * 2)` to `(GOMAXPROCS * 4)`, up to the DB's connection limit. More than this provides no benefit and increases DB load.

## HTTP Client Tuning

```go
var httpClient = &http.Client{
    Timeout: 30 * time.Second,
    Transport: &http.Transport{
        MaxIdleConns:        100,
        MaxIdleConnsPerHost: 20,
        IdleConnTimeout:     90 * time.Second,
        TLSHandshakeTimeout: 10 * time.Second,
        ExpectContinueTimeout: 1 * time.Second,
        DialContext: (&net.Dialer{
            Timeout:   30 * time.Second,
            KeepAlive: 30 * time.Second,
        }).DialContext,
    },
}
```

The default `http.Client` has no timeout (dangerous) and the default transport has `MaxIdleConnsPerHost=2` (far too low for high-throughput services). Always configure a custom transport.

## Goroutine Budget

Establish a maximum goroutine count for your service and alert when it's exceeded:

```go
func init() {
    go func() {
        ticker := time.NewTicker(10 * time.Second)
        defer ticker.Stop()
        for range ticker.C {
            n := runtime.NumGoroutine()
            goroutineGauge.Set(float64(n))
            if n > 10_000 { // alert threshold
                log.Printf("WARNING: high goroutine count: %d", n)
            }
        }
    }()
}
```

## Production Configuration Checklist

```
□ automaxprocs imported for container-aware GOMAXPROCS
□ GOMEMLIMIT set to (container_memory - 500MB headroom)
□ database/sql pool: MaxOpenConns, MaxIdleConns, ConnMaxLifetime configured
□ http.Transport: MaxIdleConnsPerHost increased from default 2
□ http.Client: Timeout set (never use default client in production)
□ pprof endpoint registered on internal port (not public)
□ goroutine count exposed as metric with alert threshold
□ go test -race in CI pipeline
□ goleak in test suite
□ GODEBUG=gctrace in staging environment
```

## Real Production Example — The Kubernetes GOMAXPROCS Disaster

We deployed a Go service to Kubernetes with 2 CPU requests and 4 CPU limits. The host machines had 96 CPUs. Go set GOMAXPROCS=96. Our service used 3x more CPU than allocated, was throttled by the kernel's CFS scheduler, and had terrible p99 latency.

After adding `import _ "go.uber.org/automaxprocs"`, GOMAXPROCS correctly became 4 (matching the CPU limit), CPU usage dropped to normal, and p99 latency improved by 60%.

The lesson: always use automaxprocs in containerized deployments. It's a one-line fix with dramatic impact.

## Interview Questions

**Q: What is the container problem with GOMAXPROCS?**

`runtime.NumCPU()` reads from `/proc/cpuinfo`, which reports host CPU count in containers. In a pod with a 2-CPU limit on a 64-CPU host, Go sets GOMAXPROCS=64, creating 64 Ps and 64 OS threads that all compete for 2 CPUs. This causes massive context switching and CFS throttling. Fix: use `go.uber.org/automaxprocs` which reads CPU quota from cgroups.

**Q: What is GOMEMLIMIT and why is it preferred over GOGC tuning?**

`GOMEMLIMIT` (Go 1.19+) sets a hard memory limit on the Go heap. The GC automatically adapts its aggressiveness to stay under this limit. It's preferred over tuning `GOGC` because it's self-adapting — you set the target and the GC figures out the right frequency. Set it to container memory minus headroom to prevent OOM kills.

**Q: How do you size a database connection pool?**

`MaxOpenConns`: number of logical CPUs × 2-4, capped at the database's connection limit. `MaxIdleConns`: keep a fraction of MaxOpenConns warm (5-25%). `ConnMaxLifetime`: recycle connections periodically (5-30 minutes) to avoid stale connections after network changes.

## Key Takeaways

- Always use `go.uber.org/automaxprocs` in containers — default GOMAXPROCS=host CPUs is wrong
- GOMEMLIMIT (Go 1.19+): set to container memory - headroom, preferred over GOGC tuning
- GOGC=100 (default): GC when heap doubles. Increase for throughput, decrease for memory efficiency
- DB connection pool: always configure MaxOpenConns, MaxIdleConns, ConnMaxLifetime
- HTTP transport: increase MaxIdleConnsPerHost from default 2 for high-throughput services
- Monitor goroutine count as a metric; alert on unexpected growth

---

# 26. Runtime Internals

## What this section covers

This section goes deep into Go's runtime machinery — the pieces that make everything else work. Understanding the internals lets you reason about performance, make informed optimization decisions, and answer the deepest interview questions with confidence. We'll cover: the scheduler's actual implementation, the GC's concurrent marking algorithm, stack growth mechanics, and the netpoller.

## The Scheduler in Depth — Beyond the GMP Overview

We covered GMP in Section 3. Here's what happens inside the scheduler under specific conditions:

### The sysmon Goroutine

`sysmon` is the runtime's watchdog — a special goroutine that runs on its own OS thread (outside the GMP model) and never sleeps for more than 10ms:

```go
// From runtime/proc.go (simplified)
func sysmon() {
    for {
        // 1. Retake Ps from goroutines that have been running too long (preemption)
        retake(now)

        // 2. Poll network (netpoller) for goroutines with ready network I/O
        netpoll(delay)

        // 3. Trigger garbage collection if it's overdue
        forcegchelper()

        // 4. Run scavenging to return memory to OS
        scavenge()

        // 5. Print deadlock message if all goroutines are asleep
        checkdead()

        usleep(10_000) // sleep ~10ms (adaptive)
    }
}
```

Key things sysmon does:
1. **Preemption**: if a goroutine has been running on a P for >10ms without a scheduling point, sysmon sets a flag that causes the goroutine to be preempted at the next safe point (asynchronous preemption via signal on Linux/Mac)
2. **Netpoll**: moves goroutines with ready network I/O from the netpoller's wait list to runnable queues
3. **Forced GC**: if GC hasn't run in 2 minutes (to keep GC accounting accurate)

### Work Stealing In Detail

When a P's local run queue is empty:

```
Step 1: Check own local run queue → empty
Step 2: Check global run queue (1/61 of the time even when local queue has work)
Step 3: Check netpoller for ready I/O goroutines
Step 4: Try to steal from another P's local run queue
  → Pick a victim P randomly (actually cycles through all Ps)
  → Steal exactly half of the victim's goroutines
  → This balances load automatically

Step 5: If all queues empty and no network I/O ready → M goes to sleep
  → Thread pool: up to 10,000 OS threads can exist, most sleeping
```

The global run queue is checked 1/61 of the time to prevent starvation — if goroutines only ever moved between P local queues, a goroutine in the global queue could wait forever.

### The Netpoller — Why Go I/O Is So Efficient

Go's `net` package uses non-blocking I/O underneath, even though your code looks blocking:

```go
// This looks like a blocking call:
conn, err := net.Dial("tcp", "google.com:80")

// Internally:
// 1. Non-blocking connect() syscall
// 2. If EAGAIN/EWOULDBLOCK: goroutine is parked (suspended), P is released
// 3. File descriptor is registered with epoll/kqueue/IOCP (OS-level multiplexer)
// 4. When I/O is ready: netpoller wakes the goroutine, adds it to a run queue
// 5. Goroutine resumes on a P (possibly a different one than before)
```

```
Goroutine G1                    OS                    Netpoller
   │                             │                        │
   ├──── connect() ─────────────▶│                        │
   │     (non-blocking)          │                        │
   │◀──── EAGAIN ────────────────┤                        │
   │                             │                        │
   ├──── park G1 ───────────────────────────────────────▶│
   │     (release P, register fd)│                        │
   │                             │                        │
   │     (I/O ready) ────────────────────────────────────▶
   │                             │              wake G1   │
   │◀──────────────────────────────────────────────────────
   ├──── resume on P ────────────│                        │
```

This is why Go can handle millions of concurrent connections with thousands of goroutines — each blocked-on-I/O goroutine costs only ~2KB of stack (no OS thread needed).

## The Garbage Collector — Tri-Color Mark-and-Sweep

Go uses a **concurrent, tri-color, mark-and-sweep** GC. The "concurrent" part is key: most GC work happens while your goroutines are still running, with only microsecond-level stop-the-world pauses.

### Tri-Color Marking

Every heap object is one of three colors:

```
White: not yet visited (garbage candidate)
Gray:  visited, but children not yet scanned (in the "work queue")
Black: visited, children fully scanned (definitely alive)

Invariant: A black object may NEVER point to a white object.
(If it did, we'd miss a live object and incorrectly collect it.)
```

Algorithm:
```
1. STW (stop the world, ~100µs):
   - All goroutine stacks are roots → color them gray
   - Global variables → color gray
   
2. Concurrent mark:
   - Worker goroutines process the gray work queue
   - For each gray object: scan its pointers, color referenced whites gray
   - Mark the object black (done scanning)
   - Goroutines continue running in parallel

3. Write barrier (during concurrent mark):
   - If a running goroutine modifies a pointer (writes a new reference):
     the new target is colored gray (added to work queue)
   - This preserves the "no black→white" invariant even as goroutines mutate the heap

4. STW (stop the world, ~100µs):
   - Flush goroutine write barrier buffers
   - Rescan any stacks modified during marking
   - All remaining gray objects → black

5. Concurrent sweep:
   - Free white objects (garbage)
   - Running concurrently with goroutines
```

### GC Assist — Why Your Code Slows During GC

During the concurrent mark phase, goroutines that allocate memory must "assist" the GC — they spend a portion of their runtime doing GC marking work proportional to how much they're allocating. This is the "GC assist" time you see in traces.

If your goroutine is allocating 1MB/ms, it owes the GC a corresponding amount of marking work. If the GC can't keep up, allocation slows. This is the mechanism that prevents the heap from growing unboundedly during a GC cycle.

### STW Pauses and Why They're Short

Go's STW pauses are typically < 1ms (often ~100µs) because:
1. The initial STW only needs to scan goroutine stacks (not the whole heap)
2. Write barriers keep the invariant during concurrent marking (less work at STW2)
3. Stack scanning uses cooperative preemption — goroutines yield quickly
4. The GC team has spent years minimizing this

Pre-Go 1.5, GC was stop-the-world the entire time. Go 1.5 introduced concurrent marking. Go 1.14 introduced asynchronous preemption. Modern Go GC pauses are ~100µs — essentially invisible in p99 measurements.

## Stack Growth Mechanics

Every goroutine starts with a 2KB stack. When the stack needs to grow (function call would overflow), Go uses **stack copying** (since Go 1.4):

```
1. Allocate a new, larger stack (2x the current size)
2. Copy all stack frames to the new stack
3. Update all pointers that pointed into the old stack
4. Free the old stack
5. Resume the goroutine on the new stack

Stack sizes: 2KB → 4KB → 8KB → 16KB → ... → 1GB (max, configurable)
```

This is called **continuous stacks** or **copying stacks**. Before Go 1.4, Go used segmented stacks, which had a notorious "hot split" performance problem.

The stack copy operation requires updating every pointer into the old stack — this is why taking the address of a local variable can cause that variable to be "pinned" in certain low-level contexts, and why goroutine stacks are not directly addressable from C code via cgo.

```go
// Stack growth example: recursive function
func fib(n int) int {
    if n <= 1 { return n }
    return fib(n-1) + fib(n-2) // deep recursion grows the stack
}

// Each call pushes a new frame. When the stack is full,
// Go copies the entire stack to a larger allocation.
// This is transparent to user code.
```

## The sync primitives — Under the Hood

### How sync.Mutex Uses the Semaphore

```go
// runtime/sema.go
// semaphore implemented with a wait list per address

// sync.Mutex.Lock slow path:
runtime_SemacquireMutex(&m.sema, false, 1)
// → goroutine added to the semaphore's wait queue
// → goroutine parked (taken off the run queue)
// → P is released to run other goroutines

// sync.Mutex.Unlock:
runtime_Semrelease(&m.sema, false, 1)
// → pops first goroutine from semaphore wait queue
// → makes it runnable (adds to run queue)
```

This is the link between the sync package and the scheduler: `semacquire/semrelease` are the scheduler-level park/unpark operations.

## Interview Questions

**Q: How does Go's scheduler implement preemption?**

Two mechanisms: cooperative preemption (goroutine yields at function call preambles that check a preemption flag) and asynchronous preemption (sysmon sends SIGURG to the OS thread running a goroutine, the signal handler checks the preemption flag and parks the goroutine). Async preemption (Go 1.14+) ensures goroutines can be preempted even in tight loops with no function calls.

**Q: How does Go achieve efficient I/O with goroutines?**

Via the netpoller. `net` package uses non-blocking syscalls. If I/O would block (EAGAIN), the goroutine is parked and its file descriptor is registered with the OS's I/O multiplexer (epoll/kqueue). When I/O is ready, sysmon or the scheduler calls `netpoll()`, which finds ready goroutines and makes them runnable. The goroutine resumes on a P without ever blocking an OS thread.

**Q: What is the write barrier in Go's GC and why is it needed?**

During concurrent marking, goroutines continue running and modifying heap pointers. Without a write barrier, a goroutine could create a black→white pointer (black object pointing to unvisited white object), violating the tri-color invariant and causing live objects to be collected (memory corruption). The write barrier intercepts pointer writes and marks the new target gray, maintaining the invariant.

**Q: Why did Go move from segmented stacks to copying stacks?**

Segmented stacks had a "hot split" problem: if a function near a stack boundary was called in a tight loop, each call would split the stack (allocate a new segment) and the return would free it — causing constant stack grow/shrink thrashing. Copying stacks allocate a 2x larger stack on overflow, amortizing the cost. The tradeoff is that copying requires updating all pointers into the old stack (only possible because Go's GC knows all pointer locations).

## Key Takeaways

- sysmon: the runtime's watchdog — handles preemption, netpoll, forced GC, deadlock detection
- Work stealing: P with empty queue steals half of another P's goroutines — automatic load balancing
- Netpoller: non-blocking I/O + epoll/kqueue → goroutines park on I/O without blocking OS threads
- Tri-color concurrent GC: most marking happens while goroutines run; write barrier preserves invariant
- GC assist: goroutines that allocate must help mark during GC — prevents heap explosion
- STW pauses: ~100µs in modern Go (Go 1.5 concurrent marking + Go 1.14 async preemption)
- Copying stacks: goroutines grow stacks by copying to 2x allocation; transparent to user code

---

# 27. Interview Masterclass

## How to Actually Nail a Go Concurrency Interview

Here's what most candidates get wrong: they memorize API signatures and recite them back. "sync.Mutex has Lock() and Unlock()." That's worth nothing. Senior engineers want to see that you understand *why* things work the way they do, can reason about failure modes, and have seen these problems in production. This section is the consolidation of everything in this guide into a structured interview framework.

## The Mental Model They're Testing For

Every concurrency interview question is ultimately asking one of these:

```
1. Safety:   "Will this code produce a data race, deadlock, or incorrect result?"
2. Liveness: "Will every goroutine eventually make progress, or can something get stuck?"
3. Performance: "Is this efficient? Where are the bottlenecks?"
4. Internals: "How does Go actually implement this under the hood?"
```

When you get a question, first identify which category it falls into. Your answer structure changes based on that.

## The 27-Topic Quick-Reference

### Concurrency vs Parallelism
- Concurrency: structure — multiple things in progress simultaneously (interleaved)
- Parallelism: execution — multiple things running simultaneously (requires multiple CPUs)
- Go is designed for concurrency; parallelism is automatic when GOMAXPROCS > 1

### Goroutines
- 2KB initial stack, grows via copying stacks up to 1GB
- Multiplexed M:N onto OS threads by the GMP scheduler
- `go` keyword creates a goroutine — cheapest unit of concurrency in any language
- Stack is on the heap (allows growth); variables escape to heap when their address is taken

### GMP Scheduler
- G: goroutine, M: OS thread, P: logical processor (GOMAXPROCS controls P count)
- Each P has a local run queue (256 goroutines max), global run queue for overflow
- Work stealing: idle P steals half of a busy P's run queue
- sysmon preempts long-running goroutines every ~10ms via SIGURG

### Channels
- Unbuffered: synchronous rendezvous (sender blocks until receiver ready, and vice versa)
- Buffered: async up to capacity, then blocks
- Close signals "no more data" — receiver can detect via `val, ok := <-ch` (ok=false when closed)
- Nil channel: blocks forever on send and receive (useful for dynamic select disabling)

### Select
- Pseudorandom selection when multiple cases are ready — no priority
- `default` case makes the select non-blocking
- `nil` channel in select case is never selected — use to disable cases dynamically

### sync.Mutex
- Not reentrant — goroutine locking a mutex it holds deadlocks itself
- Starvation mode (Go 1.9+): after 1ms wait, mutex goes to FIFO mode — prevents starvation
- Always use `defer mu.Unlock()` — ensures unlock even on panic
- `sync.RWMutex`: concurrent reads, exclusive writes; writer priority prevents reader starvation

### sync.WaitGroup
- `Add()` before goroutine launch, not inside it (race condition)
- `defer wg.Done()` to handle panics
- Don't copy WaitGroup — pass pointer or embed in struct

### Context
- Propagates cancellation, deadlines, and request-scoped values across goroutines
- `WithCancel`, `WithTimeout`, `WithDeadline` create derived contexts
- Always call the cancel function (from defer) to prevent goroutine leak
- Pass `ctx` as the first argument to every function that does I/O or spawns goroutines

### sync.Once
- Executes a function exactly once, even under concurrent calls
- Correct lazy initialization — thread-safe singleton
- Panic in the function is remembered — subsequent callers see the same panic

### sync.Pool
- Per-P pools of reusable objects — reduces GC pressure in high-throughput paths
- Victim cache (Go 1.13+): objects survive one extra GC cycle before being freed
- Always reset objects before putting them back (data from previous use may be sensitive)
- Not a cache — objects can be freed at any GC

### Atomic Operations
- `sync/atomic`: lock-free, hardware-level operations on integers and pointers
- Compare-And-Swap: `atomic.CompareAndSwapInt64(&v, old, new)` — the foundation of lock-free algorithms
- Go 1.19+: typed atomics `atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]`
- Use for: counters, flags, pointers. Not for: maps, slices, complex structs

### Go Memory Model
- Happens-before: if A happens-before B, A's writes are visible to B
- Channel send happens-before channel receive
- Mutex unlock happens-before the next lock
- Without synchronization: no visibility guarantee between goroutines

### Race Conditions
- Data race: concurrent access, no synchronization, at least one write → undefined behavior
- Logic race: correct synchronization, wrong logic (check-then-act, TOCTOU)
- `go test -race`: TSan-based, ~5-10x slower, catches all data races at runtime
- Concurrent map writes: runtime panic (by design)

### Deadlocks
- Requires all four Coffman conditions: mutual exclusion + hold-and-wait + no preemption + circular wait
- Prevention: consistent lock ordering — always acquire multiple locks in same order
- Detection: total deadlock panics at runtime; partial deadlocks need goroutine dump + cycle detection
- Go mutexes are NOT reentrant

### Goroutine Leaks
- Most common cause: goroutine blocked on channel after caller exited
- Fix: buffer channel (capacity 1), pass context, close channels
- Detection: `go.uber.org/goleak` in tests, `runtime.NumGoroutine()` metric in production
- Never use `time.After` in a tight loop — use `time.NewTicker`

### Worker Pools
- Fixed N goroutines reading from a jobs channel — bounds concurrency
- CPU-bound: N = GOMAXPROCS. I/O-bound: N = downstream concurrency limit (Little's Law)
- Shutdown: close jobs channel → WaitGroup.Wait()
- Semaphore pattern: `make(chan struct{}, N)` as lightweight alternative

### Pipelines
- Chain of stages connected by channels — streaming, constant-memory processing
- Each stage: goroutine reading from input, writing to output, `defer close(out)`
- Every stage must check `ctx.Done()` to handle cancellation

### Fan-In / Fan-Out
- Fan-out: N workers reading from same channel — natural load balancing
- Fan-in: N channels merged into one via forwarder goroutines + WaitGroup close
- Used together: fan-out to parallelize, fan-in to collect

### Backpressure
- Buffered channel = built-in backpressure (sender blocks when buffer full)
- `default` case = load shedding (reject instead of block)
- For APIs: HTTP 429 on queue full — never block indefinitely

### Cancellation Patterns
- Context: standard mechanism — `defer cancel()` always, check `ctx.Done()`
- Closing a done channel: broadcast to all goroutines simultaneously
- First-result-wins: `defer cancel()` + buffered result channel + ctx in each goroutine
- Graceful shutdown: stop accepting → drain in-flight → signal.Notify + WithTimeout

### Debugging
- `-race`: always in CI
- Goroutine dump (SIGQUIT / `runtime.Stack`): leaks, deadlocks, hung goroutines
- Delve: attach to live processes, goroutine-aware debugger
- `GODEBUG=schedtrace`, `GODEBUG=gctrace`: scheduler and GC metrics
- pprof: aggregate profiling (CPU, heap, mutex, goroutine, block profiles)
- `go tool trace`: microsecond timeline of scheduler events

### Production Tuning
- `automaxprocs`: always in containers — GOMAXPROCS must match CPU quota
- `GOMEMLIMIT`: set to container memory - headroom (preferred over GOGC)
- DB pool: `MaxOpenConns`, `MaxIdleConns`, `ConnMaxLifetime` always configured
- HTTP client: always set `Timeout`, increase `MaxIdleConnsPerHost`

### Runtime Internals
- sysmon: preemption, netpoll, forced GC, deadlock detection
- Netpoller: non-blocking I/O + epoll/kqueue; goroutines park without blocking threads
- GC: tri-color concurrent mark-and-sweep; write barrier; GC assist; ~100µs STW
- Stacks: start at 2KB, grow by copying to 2x size; old segmented stacks caused hot-split

---

## Classic Interview Questions and Model Answers

### "What is a goroutine leak? How do you detect and fix it?"

A goroutine leak is a goroutine that will never terminate, accumulating over the process lifetime. The most common cause is a goroutine blocked on a channel send or receive after the other side has exited. 

Detect with `go.uber.org/goleak` in tests (fails if goroutines leak during test). In production, monitor `runtime.NumGoroutine()` as a Prometheus metric; alert on sustained growth.

Fix patterns: buffer the channel with capacity 1 so the goroutine can always send and exit; pass context and check `ctx.Done()` in channel operations; always `close()` channels when the sender is done.

### "How does the Go scheduler work? What is work stealing?"

The GMP model: G (goroutine), M (OS thread), P (logical processor). Each P has a local run queue of up to 256 goroutines. GOMAXPROCS controls how many Ps exist. Each P runs goroutines from its local queue on one M.

Work stealing: when a P's local queue is empty, it tries to steal half of another P's goroutines. This automatically load-balances without any explicit synchronization. The global run queue is checked 1/61 of the time to prevent starvation.

Preemption happens cooperatively (at function call sites) and asynchronously (sysmon sends SIGURG to force preemption of goroutines running tight loops with no function calls, added in Go 1.14).

### "Explain the difference between sync.Mutex and sync.RWMutex. When do you use each?"

`sync.Mutex` grants exclusive access — only one goroutine can hold it at a time, whether for reading or writing. Use it when operations both read and write shared state, or when you need simplicity.

`sync.RWMutex` allows multiple concurrent readers OR one exclusive writer, never both simultaneously. Use it when reads are far more frequent than writes and reads take long enough that concurrent execution provides meaningful speedup. A bad fit for frequently-written data — RWMutex has more overhead than plain Mutex, and frequent writers must wait for all current readers to finish.

### "Walk me through what happens when you send to a buffered channel."

At the hardware level, the channel `hchan` struct has a ring buffer, a mutex protecting it, and counts of buffered items. On send:
1. Acquire the channel lock
2. If there's a goroutine waiting to receive (`recvq` is non-empty): copy value directly to the receiver's stack and wake it (direct send optimization, skip the buffer)
3. If buffer has space: copy value into buffer, increment count, release lock
4. If buffer is full: create a `sudog` struct wrapping the current goroutine, enqueue it in `sendq`, call `gopark()` to suspend the goroutine and release the P for other work

The goroutine wakes when a receiver dequeues from `sendq` and copies the value.

### "What is the Go memory model and why does it matter?"

The Go memory model defines when writes in one goroutine are guaranteed to be visible to reads in another goroutine. The key concept is happens-before: if operation A happens-before operation B, then A's writes are visible when B executes.

Without synchronization, there's no visibility guarantee. The CPU and compiler can reorder operations for performance. What looks sequential to you may not be sequential in execution. The memory model defines synchronization primitives (channel operations, mutex lock/unlock, `atomic` operations) that establish happens-before relationships.

It matters because: a goroutine setting `ready = true` before `data = value` might have those stores reordered. If another goroutine checks `if ready` without synchronization, it might see `ready=true` but the old value of `data`. This is a data race. The fix is to synchronize both goroutines with a channel, mutex, or atomic.

### "How do you avoid deadlock when using multiple mutexes?"

Establish a global lock ordering and always acquire multiple locks in that order everywhere in the codebase. Deadlock requires circular wait — if goroutines always acquire locks in the same order, circular wait is impossible.

Document the order. Enforce in code review. If you can't establish a clear order, use `sync.TryLock` (Go 1.18+) as a fallback — try to acquire, back off and retry if failed. Another approach: use a single coarser lock instead of multiple fine-grained locks (simpler, but potential contention).

### "How would you implement a concurrent cache?"

```go
type Cache[K comparable, V any] struct {
    mu    sync.RWMutex
    items map[K]V
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    v, ok := c.items[key]
    return v, ok
}

func (c *Cache[K, V]) Set(key K, val V) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.items[key] = val
}
```

For higher throughput: shard the cache into N maps each with its own mutex, hash the key to determine the shard. Reduces contention N-fold. For read-heavy workloads with infrequent writes: consider `sync.Map` (uses a copy-on-write strategy for reads).

### "How does Go's GC work? How do you reduce GC pressure?"

Concurrent tri-color mark-and-sweep. Two short STW pauses (~100µs each): one to find roots, one to finish marking. Concurrent marking runs while goroutines execute (write barrier preserves correctness). Concurrent sweeping frees garbage.

Reduce GC pressure:
1. `sync.Pool`: reuse allocations (buffers, structs) — avoids creating new heap objects
2. Stack allocation: small objects that don't escape to heap are stack-allocated (free at return)
3. Avoid pointer-heavy data structures: flat arrays of values are better than linked lists of pointers (GC must scan every pointer)
4. `GOMEMLIMIT`: gives GC a memory budget to work within
5. Pre-allocate slices with `make([]T, 0, knownCapacity)` to avoid repeated growth allocations

---

## What Separates Good from Great Answers

**Good answer**: "sync.Mutex prevents concurrent access to shared data."

**Great answer**: "sync.Mutex has a fast path — if the mutex is unlocked and uncontended, Lock() is a single atomic CAS operation with no system call. When contended, the goroutine first spins for a few hundred nanoseconds hoping the lock releases (on a multicore system). If still contended, it calls `runtime_SemacquireMutex` which parks the goroutine (releases the P for other work) and adds it to a semaphore wait queue. Unlock calls `runtime_Semrelease` to wake the next waiter. In starvation mode (Go 1.9+), if a goroutine has waited > 1ms, the mutex goes FIFO to prevent starvation — the woken goroutine is handed the lock directly rather than competing."

The depth of that answer — knowing the CAS fast path, the spinning, the park/wake mechanism, and the starvation mode — is what earns senior-level respect in an interview.

## Final Advice

**On writing concurrent code:**
- Start with channels and context. Use mutexes when channels are awkward.
- Write goroutine-free first, add concurrency for real bottlenecks, measure.
- Every `go` statement is a new goroutine — make sure it can terminate.
- Every goroutine needs a cancellation path — context, done channel, or closed input channel.

**On debugging:**
- `-race` in CI. Not negotiable.
- Monitor goroutine count in production. First sign of a leak.
- Use pprof before optimizing anything. What you think is slow often isn't.

**On production:**
- `automaxprocs` in every containerized service. One import, dramatic impact.
- `GOMEMLIMIT` at container memory - headroom. Eliminates OOM from lazy GC.
- Buffer sizes aren't magic numbers — they're a tradeoff between memory and latency. Document why you chose the value you chose.

**On interviews:**
- Don't just state the API. Explain the WHY. What problem does it solve? What goes wrong without it?
- Mention failure modes proactively. "One thing to watch out for here is..."
- Bring in production experience. "I've seen this cause issues in production when..."
- Know the Coffman conditions. Know happens-before. Know the GMP model. These three come up in every serious Go interview.

---

*End of Go Concurrency Guide — 27 sections, from first principles to runtime internals.*
























