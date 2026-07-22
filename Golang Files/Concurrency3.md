# The Practical Golang Concurrency Handbook for Senior Engineers

> Written like a Staff Engineer mentoring a teammate — not like a textbook or compiler documentation.

---

# Part I — Foundations

---

## Chapter 1: Concurrency vs Parallelism

### What Problem Does This Solve?

Engineers constantly confuse these two ideas. In interviews, in design docs, in production debugging. Getting this wrong leads to bad architecture decisions and confused conversations.

### Why Engineers Care

If you think concurrency = parallelism, you'll try to speed up I/O-bound services by adding more CPU cores. That's like hiring more waiters when the kitchen is slow — it won't help.

### Mental Model

**Concurrency** is about *structure*. It's how you organize your program to deal with multiple things at once.

**Parallelism** is about *execution*. It's actually doing multiple things at the same time.

Think of it this way:

- **Concurrency**: A single chef juggling three dishes — switching between them, never letting anything burn.
- **Parallelism**: Three chefs each cooking one dish at the same time.

Go is designed for concurrency. It gives you tools to structure programs that handle many things. Whether they actually run in parallel depends on how many CPU cores you have and what `GOMAXPROCS` is set to.

### How It Works

```go
// This is concurrent — two goroutines are structured to run independently
go fetchFromDB()
go fetchFromCache()

// Whether they run in parallel depends on:
// 1. Number of CPU cores
// 2. GOMAXPROCS setting
// 3. Scheduler decisions
```

A single-core machine can run concurrent code but never in parallel. A multi-core machine with `GOMAXPROCS > 1` can run goroutines in parallel.

### What Happens Under The Hood

Go's scheduler multiplexes goroutines onto OS threads. Even with one OS thread, Go can run thousands of goroutines concurrently by switching between them at certain points (channel operations, system calls, function calls, etc.).

### Real Production Example

Your API server handles 10,000 requests per second. Each request is a goroutine. Most of the time, each goroutine is waiting for a database response. You don't need 10,000 CPU cores — you need concurrency. The goroutines are structured to handle multiple requests, but most are just waiting, not computing.

### Common Mistakes

- **Mistake**: "I set GOMAXPROCS to 64 and my API didn't get faster."
  - **Why**: If your bottleneck is I/O (database, network), adding more parallelism doesn't help. You need to fix the I/O.
- **Mistake**: Thinking concurrent code is automatically parallel.
  - **Reality**: Concurrency is a design pattern. Parallelism is a runtime behavior.

### Interview Questions

**Q: What's the difference between concurrency and parallelism?**
- **Answer**: Concurrency is about structuring a program to handle multiple tasks. Parallelism is about executing multiple tasks simultaneously. Go provides concurrency primitives; parallelism depends on hardware and runtime settings.
- **Why interviewers ask**: They want to know if you understand why goroutines are useful even on a single core.
- **Common wrong answer**: "They're the same thing."
- **Follow-up**: "Can you have concurrency without parallelism?" (Yes — single-core machine.)

### Key Takeaways

- Concurrency = structure. Parallelism = execution.
- Go is a concurrency language. Parallelism is a bonus.
- Most backend services benefit from concurrency, not parallelism.
- Don't throw cores at I/O-bound problems.

---

## Chapter 2: Processes vs Threads vs Goroutines

### What Problem Does This Solve?

To understand why goroutines exist, you need to understand what came before them and why those solutions were painful.

### Why Engineers Care

Every time you spawn a goroutine with `go func()`, you're making a choice that has real memory, scheduling, and performance implications. Understanding the alternatives helps you appreciate why Go's approach works so well for backend services.

### Mental Model

| Feature | Process | OS Thread | Goroutine |
|---------|---------|-----------|-----------|
| Memory | ~MB per process | ~1MB stack | ~2KB stack (grows dynamically) |
| Creation cost | Very expensive | Expensive | Very cheap |
| Scheduling | OS kernel | OS kernel | Go runtime (userspace) |
| Communication | IPC (pipes, sockets) | Shared memory + locks | Channels (or shared memory) |
| Context switch | Very slow | Slow (~1-10μs) | Fast (~200ns) |
| Limit on typical server | Hundreds | Thousands | Millions |

### How It Works

**Processes**: Each process gets its own memory space. Communication between processes requires serialization (pipes, sockets, shared memory mapped files). Forking is expensive. This is how Apache httpd worked — one process per request. It didn't scale.

**OS Threads**: Threads share memory within a process, which is better. But each thread needs ~1MB of stack space (allocated upfront by the OS), and the OS kernel must schedule them. Context switching between threads involves saving/restoring CPU registers, memory mappings, and going through the kernel. When you have 10,000 threads, the OS spends more time switching between them than doing actual work.

**Goroutines**: Go said, "What if we handle scheduling ourselves?" Goroutines start with a tiny ~2KB stack that grows as needed. The Go runtime schedules them in userspace — no kernel involvement for most switches. You can create millions of them.

### What Happens Under The Hood

The Go runtime maintains a small pool of OS threads (controlled by `GOMAXPROCS`). Goroutines are multiplexed onto these threads. When a goroutine blocks on I/O, the runtime parks it and runs another goroutine on the same thread. This is why 100,000 goroutines can run on 8 OS threads.

The key insight: goroutine context switches happen in userspace and only need to save/restore a few registers and the stack pointer. OS thread context switches go through the kernel and involve much more state.

### Real Production Example

A Java service handling 50,000 concurrent connections with one thread per connection needs 50,000 threads × 1MB stack = ~50GB just for stacks. That same service in Go needs 50,000 goroutines × 2KB = ~100MB. This is why Go dominates in network services and infrastructure tooling.

### Common Mistakes

- **Mistake**: "Goroutines are green threads." 
  - **Partially true**, but Go's goroutines have preemptive scheduling (since Go 1.14), growable stacks, and deep runtime integration that most green thread implementations lack.
- **Mistake**: Treating goroutines like threads and using very few of them.
  - **Reality**: Goroutines are cheap. Don't be afraid to use thousands. Be afraid of leaking them.

### Performance Considerations

- Creating a goroutine: ~1μs
- Creating an OS thread: ~10-100μs  
- Goroutine context switch: ~200ns
- OS thread context switch: ~1-10μs
- Goroutine initial stack: ~2KB (grows to MB if needed)
- OS thread stack: ~1MB (fixed)

### Key Takeaways

- Processes are too expensive for per-request concurrency.
- OS threads are expensive in memory and context-switch cost.
- Goroutines are cheap, userspace-scheduled, and grow their stack as needed.
- This is why Go can handle millions of concurrent operations.

---

## Chapter 3: Why Go Chose CSP

### What Problem Does This Solve?

There are two main approaches to concurrent programming:
1. **Shared memory with locks** (Java, C++, Python threading)
2. **Message passing** (Erlang, Go channels)

Go chose CSP (Communicating Sequential Processes) as its primary model. Understanding why helps you write idiomatic, correct Go code.

### Why Engineers Care

If you come from Java, your instinct is to reach for mutexes and shared variables. In Go, that works but it's not the preferred approach. Channels exist for a reason, and understanding that reason makes your code better.

### Mental Model

**Shared memory**: Imagine a whiteboard in an office. Multiple people can write on it, but they need to take turns (locks) or they'll overwrite each other's work.

**Message passing (CSP)**: Imagine people passing notes to each other. Each person has their own workspace. They communicate by sending and receiving messages. No shared whiteboard needed.

Go's philosophy:

> "Do not communicate by sharing memory; share memory by communicating."

This doesn't mean "never use mutexes." It means your default approach should be channels and message passing. Use mutexes when they're genuinely simpler (like protecting a simple counter or a cache).

### How It Works

```go
// Shared memory approach (works, but not idiomatic for complex coordination)
var counter int
var mu sync.Mutex

mu.Lock()
counter++
mu.Unlock()

// CSP approach (idiomatic Go for coordination)
results := make(chan int)
go func() { results <- computeExpensiveThing() }()
go func() { results <- computeAnotherThing() }()
total := <-results + <-results
```

### When To Use Which

| Use Channels When... | Use Mutexes When... |
|---|---|
| Transferring ownership of data | Protecting a simple shared variable |
| Coordinating multiple goroutines | Guarding a cache or map |
| Building pipelines | Simple counters |
| Signaling events | Internal state in a struct |
| Fan-out / fan-in patterns | When channel would be over-engineering |

### Real Production Example

A request handler fetches data from three microservices in parallel:

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    userCh := make(chan *User, 1)
    ordersCh := make(chan []Order, 1)
    recsCh := make(chan []Recommendation, 1)
    
    go func() { userCh <- fetchUser(ctx, userID) }()
    go func() { ordersCh <- fetchOrders(ctx, userID) }()
    go func() { recsCh <- fetchRecommendations(ctx, userID) }()
    
    user := <-userCh
    orders := <-ordersCh
    recs := <-recsCh
    
    // Combine and respond
}
```

This is CSP in action. Each goroutine does its work independently and sends the result through a channel. No shared state, no locks, no coordination bugs.

### Key Takeaways

- Go chose CSP because it makes concurrent programs easier to reason about.
- Channels transfer both data and ownership — once you send data on a channel, you shouldn't touch it anymore.
- Mutexes are still useful for simple shared state (caches, counters).
- Default to channels for goroutine coordination. Use mutexes for data protection.

---

# Part II — Core Primitives

---

## Chapter 4: Goroutines

### What Problem Does This Solve?

You need to do multiple things at the same time — handle thousands of network connections, process a batch of files, make parallel API calls. Goroutines let you do this without the complexity and cost of OS threads.

### Why Engineers Care

Goroutines are the fundamental unit of concurrency in Go. Every Go program you'll ever write or debug involves goroutines. Every HTTP handler runs in its own goroutine. Every concurrent pattern is built on them.

### Mental Model

Think of a goroutine as a lightweight task that the Go runtime manages for you. You say "go do this thing" and the runtime figures out when and where to run it.

```go
go doSomething()  // That's it. One keyword.
```

The `go` keyword creates a new goroutine that runs the function concurrently with the calling goroutine. The calling goroutine doesn't wait — it continues immediately.

### How It Works

```go
func main() {
    go sayHello()           // Launches goroutine
    fmt.Println("main")    // Runs immediately, doesn't wait
    time.Sleep(time.Second) // Crude way to wait (DON'T do this in production)
}

func sayHello() {
    fmt.Println("hello")
}
```

**Important**: When `main()` returns, the program exits — all goroutines are killed immediately. You must use proper synchronization (WaitGroup, channels, context) to coordinate goroutine lifecycles.

### What Happens Under The Hood

When you call `go func()`:

1. The runtime allocates a small goroutine descriptor (metadata about the goroutine).
2. It allocates a ~2KB initial stack.
3. The goroutine is placed on a run queue.
4. The scheduler picks it up when a thread is available.

The stack grows dynamically. If your goroutine needs more stack space, the runtime allocates a larger stack and copies the existing one over. This is why goroutines can start small but handle deep recursion.

### Goroutine Lifecycle

```
Created → Runnable → Running → Blocked → Runnable → Running → Dead
                                  ↑                      |
                                  └──────────────────────┘
```

A goroutine blocks when it:
- Reads from or writes to a channel
- Waits on a mutex
- Makes a system call (I/O, sleep)
- Calls `runtime.Gosched()`

When blocked, the runtime parks it and runs another goroutine on the same OS thread. This is why blocking in Go is cheap.

### Common Patterns

**Fire and forget** (be careful with this):
```go
go sendAnalytics(event)  // Don't care about the result
```

**Launch with result collection**:
```go
ch := make(chan Result, 10)
for _, item := range items {
    item := item // capture loop variable (pre-Go 1.22)
    go func() {
        ch <- process(item)
    }()
}
```

**Launch with WaitGroup**:
```go
var wg sync.WaitGroup
for _, item := range items {
    wg.Add(1)
    go func(item Item) {
        defer wg.Done()
        process(item)
    }(item)
}
wg.Wait()
```

### Common Mistakes

- **Mistake**: Forgetting that `main()` exiting kills all goroutines.
  ```go
  func main() {
      go doWork() // This might never run!
  }
  ```
  **Fix**: Use `sync.WaitGroup`, channels, or `select{}` to keep main alive.

- **Mistake**: Loop variable capture (pre-Go 1.22).
  ```go
  for _, v := range items {
      go func() {
          process(v) // BUG: v changes! All goroutines see the last value.
      }()
  }
  ```
  **Fix** (pre-Go 1.22): Pass as parameter: `go func(v Item) { process(v) }(v)`
  **Note**: Go 1.22+ fixes this — each iteration gets its own variable.

- **Mistake**: Launching goroutines without any way to stop them.
  ```go
  go func() {
      for {
          doWork() // Runs forever. How do you stop it?
      }
  }()
  ```
  **Fix**: Use `context.Context` for cancellation.

- **Mistake**: Assuming goroutine execution order.
  ```go
  go fmt.Println("first")
  go fmt.Println("second")
  // Output order is NOT guaranteed
  ```

### Production Patterns

**Goroutine with graceful shutdown**:
```go
func startWorker(ctx context.Context) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                log.Println("worker shutting down")
                return
            case job := <-jobQueue:
                process(job)
            }
        }
    }()
}
```

**Goroutine with error propagation**:
```go
errCh := make(chan error, 1)
go func() {
    if err := riskyOperation(); err != nil {
        errCh <- err
        return
    }
    errCh <- nil
}()

if err := <-errCh; err != nil {
    log.Printf("operation failed: %v", err)
}
```

### Performance Considerations

- Spawning a goroutine is very cheap (~1μs), but not free. Don't spawn a goroutine for a 10ns operation.
- Each goroutine consumes at least ~2KB of memory. 1 million goroutines = ~2GB minimum.
- Goroutine scheduling has overhead. For CPU-bound work, having more goroutines than `GOMAXPROCS` adds context-switch cost without benefit.
- The most expensive part of goroutines isn't creating them — it's forgetting to stop them (leaks).

### Interview Questions

**Q: What happens when you call `go func()`?**
- **Answer**: The runtime creates a goroutine with a small stack, places it on a run queue, and the scheduler runs it when a thread is available. The calling goroutine continues immediately.
- **Why asked**: Tests understanding of goroutine creation mechanics.
- **Wrong answer**: "It creates a new OS thread." (No — goroutines are multiplexed onto a pool of OS threads.)

**Q: How do you prevent goroutine leaks?**
- **Answer**: Always ensure goroutines have a way to exit — use `context.Context`, done channels, or `WaitGroup`. Every `go` statement should have a corresponding shutdown mechanism.
- **Follow-up**: "How would you detect a goroutine leak in production?"

### Key Takeaways

- Goroutines are cheap to create but must be managed carefully.
- Always have a strategy to stop every goroutine you start.
- Use `context.Context` for cancellation, not `time.Sleep`.
- Loop variable capture is a classic bug (fixed in Go 1.22+).
- The biggest goroutine problem isn't creation cost — it's leaks.

---

## Chapter 5: The Go Scheduler (G-M-P Model)

### What Problem Does This Solve?

If goroutines run on OS threads, someone has to decide which goroutine runs on which thread, when to switch between them, and what happens when a goroutine blocks. That's the scheduler's job.

### Why Engineers Care

You don't need to write scheduler code, but understanding the scheduler helps you:
- Explain why goroutines are cheap in interviews
- Debug performance problems (high CPU with low throughput)
- Understand `GOMAXPROCS` and runtime tuning
- Reason about fairness and latency

### Mental Model: The Restaurant

Imagine a restaurant:

- **G (Goroutines)** = Customers who want food (work to be done)
- **M (Machine/OS Threads)** = Chefs who cook food (execute goroutines)
- **P (Processors)** = Kitchen stations with equipment (execution contexts)

Rules:
- A chef (M) needs a kitchen station (P) to cook.
- The number of kitchen stations (P) = `GOMAXPROCS` (default = number of CPU cores).
- There can be more chefs than stations (extra chefs wait for a station).
- Each station has a queue of customers waiting (local run queue).
- If a station's queue is empty, the chef steals customers from another station's queue (work stealing).
- If a chef must leave the kitchen (syscall), they give up their station so another chef can use it.

### How It Works

```
                    ┌──────────────┐
                    │  Global Run  │
                    │    Queue     │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
        ┌─────┴─────┐ ┌───┴─────┐ ┌───┴─────┐
        │   P (0)   │ │  P (1)  │ │  P (2)  │
        │           │ │         │ │         │
        │ Local Q:  │ │ Local Q:│ │ Local Q:│
        │ [G1,G2]   │ │ [G3]   │ │ [G4,G5] │
        └─────┬─────┘ └───┬─────┘ └───┬─────┘
              │            │            │
        ┌─────┴─────┐ ┌───┴─────┐ ┌───┴─────┐
        │   M (0)   │ │  M (1)  │ │  M (2)  │
        │ OS Thread │ │OS Thread│ │OS Thread│
        └───────────┘ └─────────┘ └─────────┘
```

**Key points**:

1. **P count = GOMAXPROCS**: This limits how many goroutines truly run in parallel. Default is the number of CPU cores.

2. **Each P has a local run queue**: New goroutines are added to the local queue of the P that created them. This is fast because there's no global lock.

3. **Global run queue**: Overflow from local queues goes here. Checked periodically (every ~61 schedules) to prevent starvation.

4. **Work stealing**: When a P's local queue is empty, it steals half the goroutines from another P's queue. This balances load across processors.

### What Happens When a Goroutine Blocks

**Blocking on a channel or mutex** (userspace blocking):
- The goroutine is parked (taken off the run queue).
- The OS thread stays with the P and picks up the next goroutine.
- No OS thread is wasted.

**Blocking on a syscall** (kernel blocking, like file I/O):
- The OS thread goes into the kernel and can't run other goroutines.
- The P detaches from that thread and attaches to a new (or idle) OS thread.
- When the syscall completes, the goroutine tries to get a P back.

This is why Go can handle thousands of goroutines blocked on channels with only a handful of OS threads.

### Scheduling Points

The scheduler can switch goroutines at these points:
- Channel send/receive
- `go` statement (creating a new goroutine)
- Blocking syscalls
- Garbage collection
- `runtime.Gosched()` (voluntary yield)
- Function calls (since Go 1.14, the compiler inserts preemption checks)

**Since Go 1.14**: Goroutines can be preempted even in tight loops (asynchronous preemption). Before 1.14, a goroutine in a tight loop with no function calls could starve other goroutines.

### GOMAXPROCS

```go
runtime.GOMAXPROCS(n) // Set the number of Ps
```

- Default: number of CPU cores
- Setting it too low: underutilizes available CPU cores
- Setting it too high: more context switching, usually worse performance
- **Rule of thumb**: Leave it at the default for most applications

In containerized environments (Kubernetes), make sure `GOMAXPROCS` matches your CPU limits, not the host's core count. Use `go.uber.org/automaxprocs` for this.

### Real Production Example

**Problem**: A Go service in Kubernetes was allocated 2 CPU cores but ran on a 64-core host. `GOMAXPROCS` defaulted to 64, creating 64 Ps fighting over 2 cores. Result: massive context switching, high latency.

**Fix**: 
```go
import _ "go.uber.org/automaxprocs" // Automatically sets GOMAXPROCS from cgroup limits
```

### Common Mistakes

- **Mistake**: Setting `GOMAXPROCS(1)` to "avoid concurrency bugs."
  - **Problem**: You're not fixing bugs, you're hiding them. They'll resurface in production with more cores.
  
- **Mistake**: Thinking `GOMAXPROCS` controls the number of OS threads.
  - **Reality**: It controls the number of Ps (execution contexts). The runtime can create more OS threads than `GOMAXPROCS` for blocking syscalls.

### Performance Considerations

- Work stealing is efficient but has overhead. For best performance, keep related work on the same P (the runtime does this naturally).
- Network polling is integrated into the scheduler — goroutines waiting on network I/O don't consume OS threads.
- The scheduler checks the global queue every 61 scheduling cycles to prevent starvation.

### Interview Questions

**Q: Explain the G-M-P model.**
- **Answer**: G = goroutine (unit of work), M = OS thread (executes goroutines), P = processor (execution context with a local run queue). GOMAXPROCS controls the number of Ps. Each P can run one goroutine at a time on its attached M. Work stealing balances load between Ps.
- **Why asked**: Tests deep understanding of Go's scheduling model.
- **Wrong answer**: "Goroutines are green threads that run on a thread pool." (Misses the P concept and work stealing.)

**Q: What happens when a goroutine makes a blocking syscall?**
- **Answer**: The M (OS thread) enters the kernel with the goroutine. The P detaches from that M and finds or creates another M to continue running other goroutines. When the syscall returns, the goroutine tries to acquire a P to continue running.
- **Follow-up**: "Why is channel blocking different from syscall blocking?"

### Key Takeaways

- G = goroutine, M = OS thread, P = processor (execution context).
- GOMAXPROCS = number of Ps = max parallel goroutines.
- Work stealing balances load across Ps.
- Channel/mutex blocking is cheap (parks goroutine, reuses thread).
- Syscall blocking is more expensive (requires extra OS threads).
- In containers, ensure GOMAXPROCS matches your CPU limit.

---

## Chapter 6: Channels

### What Problem Does This Solve?

Goroutines need to communicate. Without channels, you'd use shared variables protected by mutexes — which is error-prone and hard to reason about. Channels give you a safe, structured way to pass data between goroutines.

### Why Engineers Care

Channels are Go's signature concurrency primitive. They show up everywhere: HTTP middleware, pipeline processing, worker pools, graceful shutdown, timeout handling. If you don't deeply understand channels, you can't write production Go.

### Mental Model

A channel is a **typed pipe** between goroutines:

```
Goroutine A ──── data ────→ [channel] ────→ Goroutine B
```

- **Unbuffered channel**: A direct handoff. The sender blocks until the receiver is ready, and vice versa. Like handing a package to someone face-to-face — you both have to be there.

- **Buffered channel**: A mailbox with limited slots. The sender can drop a message and leave (if there's space). The receiver picks it up later. The sender only blocks when the mailbox is full.

### Unbuffered Channels

```go
ch := make(chan int) // Unbuffered — capacity 0

// Sender blocks until receiver is ready
go func() { ch <- 42 }()

// Receiver blocks until sender sends
value := <-ch
```

**Key property**: Unbuffered channels synchronize goroutines. The send and receive happen at the same instant. This is a guarantee — if you received a value, you know the sender has passed that point in its execution.

**When to use**: When you need synchronization between goroutines, not just data transfer.

```go
// Using unbuffered channel as a synchronization signal
done := make(chan struct{})

go func() {
    doExpensiveWork()
    done <- struct{}{} // Signal completion
}()

<-done // Wait for completion
```

### Buffered Channels

```go
ch := make(chan int, 5) // Buffered — capacity 5

ch <- 1 // Doesn't block (buffer has space)
ch <- 2 // Doesn't block
ch <- 3 // Doesn't block
// ...
```

**Key property**: Buffered channels decouple sender and receiver timing. The sender only blocks when the buffer is full. The receiver only blocks when the buffer is empty.

**When to use**: When the sender and receiver work at different speeds and you want to smooth out bursts.

### Buffered vs Unbuffered — The Decision Framework

| Question | Unbuffered | Buffered |
|----------|-----------|----------|
| Do I need synchronization? | ✅ Yes | ❌ No guarantee |
| Can sender be faster than receiver? | ❌ Sender must wait | ✅ Buffer absorbs bursts |
| Do I want backpressure? | ✅ Natural backpressure | ⚠️ Backpressure only when full |
| Is it a signal (no data)? | ✅ Perfect | ❌ Overkill |
| Am I building a worker pool? | ❌ Too blocking | ✅ Job queue |

**Common buffer sizes and why**:
- `make(chan T, 0)` — Same as `make(chan T)`. Synchronization.
- `make(chan T, 1)` — Semaphore or "latest value" pattern. Very common.
- `make(chan T, N)` where N = number of workers — worker pool job queue.
- `make(chan T, largeN)` — Be careful. If you need a huge buffer, you might have a design problem.

### Channel Directions

You can restrict a channel to send-only or receive-only in function signatures. This is powerful for API safety.

```go
// Producer can only send
func produce(ch chan<- int) {
    ch <- 42
}

// Consumer can only receive
func consume(ch <-chan int) {
    val := <-ch
    fmt.Println(val)
}

func main() {
    ch := make(chan int)
    go produce(ch)
    consume(ch)
}
```

**Why this matters**: The compiler enforces the restriction. A function receiving `<-chan int` physically cannot close or send on that channel. This prevents entire classes of bugs.

### Closing Channels

```go
ch := make(chan int, 5)
ch <- 1
ch <- 2
close(ch)

// Reading from a closed channel returns the zero value immediately
val, ok := <-ch // val=1, ok=true
val, ok = <-ch  // val=2, ok=true
val, ok = <-ch  // val=0, ok=false (channel closed and empty)

// Writing to a closed channel PANICS
ch <- 3 // PANIC: send on closed channel
```

**Rules for closing channels**:

1. **Only the sender should close a channel.** Never close from the receiver side.
2. **Only close when you need to signal "no more data."** Not every channel needs to be closed.
3. **Closing is a broadcast signal.** All goroutines blocked on receive will unblock.
4. **Don't close a channel more than once.** Double close panics.

**The ownership principle**: The goroutine that creates and sends on a channel should close it. Think of it like a door — only the person who opened it should close it.

```go
func generator(nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out) // Generator owns the channel, so it closes it
        for _, n := range nums {
            out <- n
        }
    }()
    return out
}
```

### Range Over Channels

```go
ch := make(chan int)

go func() {
    for i := 0; i < 5; i++ {
        ch <- i
    }
    close(ch) // MUST close, or range will block forever
}()

for val := range ch {
    fmt.Println(val) // Prints 0, 1, 2, 3, 4
}
// Loop exits when channel is closed and drained
```

**Critical**: `range` over a channel blocks forever if the channel is never closed. This is a common source of goroutine leaks.

### Nil Channels

A nil channel blocks forever on both send and receive. This sounds useless but it's actually a powerful tool.

```go
var ch chan int // nil channel
// ch <- 1     // blocks forever
// <-ch        // blocks forever
// close(ch)   // PANICS
```

**Practical use — disabling a select case**:

```go
func merge(ch1, ch2 <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for ch1 != nil || ch2 != nil {
            select {
            case v, ok := <-ch1:
                if !ok {
                    ch1 = nil // Disable this case
                    continue
                }
                out <- v
            case v, ok := <-ch2:
                if !ok {
                    ch2 = nil // Disable this case
                    continue
                }
                out <- v
            }
        }
    }()
    return out
}
```

By setting a channel to nil, the `select` case is permanently disabled. This is the idiomatic way to handle dynamic select cases.

### Channel Ownership Pattern

One of the most important patterns in production Go:

```go
// The function that creates a channel should:
// 1. Create it
// 2. Launch the goroutine that writes to it
// 3. Return a read-only channel
// 4. Close it when done

func processItems(items []Item) <-chan Result {
    results := make(chan Result)
    go func() {
        defer close(results) // Owner closes
        for _, item := range items {
            results <- process(item)
        }
    }()
    return results // Return read-only view
}

// Caller only receives — cannot accidentally close or send
func main() {
    for result := range processItems(myItems) {
        fmt.Println(result)
    }
}
```

This pattern prevents:
- Sending on a closed channel (panic)
- Double closing (panic)
- Forgetting to close (goroutine leak)

### Common Mistakes

- **Mistake**: Writing to a closed channel.
  ```go
  close(ch)
  ch <- value // PANIC
  ```
  **Fix**: Only the sender closes. Use the ownership pattern.

- **Mistake**: Forgetting to close a channel when using `range`.
  ```go
  go func() {
      for _, v := range data {
          ch <- v
      }
      // Forgot to close(ch)! The range loop blocks forever.
  }()
  for v := range ch { /* blocks forever */ }
  ```

- **Mistake**: Using a massive buffer to "fix" a deadlock.
  ```go
  ch := make(chan int, 1000000) // This doesn't fix the problem, it hides it
  ```
  **Reality**: If you need a huge buffer, your goroutines have a coordination problem.

- **Mistake**: Not using directional channels in function signatures.
  ```go
  func bad(ch chan int) { ... }       // Can send, receive, AND close — too permissive
  func good(ch <-chan int) { ... }    // Read-only — safer
  ```

### Real Production Examples

**Result aggregation with timeout**:
```go
func fetchAll(ctx context.Context, urls []string) []Response {
    results := make(chan Response, len(urls))
    
    for _, url := range urls {
        url := url
        go func() {
            resp, err := fetch(ctx, url)
            results <- Response{URL: url, Data: resp, Err: err}
        }()
    }
    
    var responses []Response
    for i := 0; i < len(urls); i++ {
        select {
        case r := <-results:
            responses = append(responses, r)
        case <-ctx.Done():
            return responses // Return what we have
        }
    }
    return responses
}
```

**Semaphore using buffered channel**:
```go
// Limit to 10 concurrent operations
sem := make(chan struct{}, 10)

for _, task := range tasks {
    sem <- struct{}{} // Acquire (blocks when 10 are running)
    go func(t Task) {
        defer func() { <-sem }() // Release
        process(t)
    }(task)
}
```

### Performance Considerations

- Unbuffered channel operations involve goroutine scheduling — ~50-100ns per operation.
- Buffered channel operations when buffer isn't full/empty — ~20-50ns (just a memory copy).
- Channels use internal locks. Under very high contention, they can become bottlenecks.
- For simple counters or flags, `sync/atomic` is much faster than a channel.
- Channel of `struct{}` is zero-allocation for signaling.

### Interview Questions

**Q: When would you use a buffered channel vs unbuffered?**
- **Answer**: Unbuffered for synchronization (I need to know the receiver got the value). Buffered for decoupling speed differences between producer and consumer, or as a semaphore/job queue.
- **Why asked**: Tests practical understanding of channel semantics.
- **Wrong answer**: "Always use buffered for better performance." (Wrong — buffered channels can hide deadlocks and aren't always faster.)

**Q: What happens when you send on a closed channel?**
- **Answer**: It panics. Only the sender should close channels, and you should never send after closing.
- **Follow-up**: "What about receiving from a closed channel?" (Returns zero value immediately.)

**Q: What are nil channels useful for?**
- **Answer**: Disabling select cases dynamically. A nil channel blocks forever, so a select case on a nil channel is never selected.
- **Why asked**: Tests advanced channel knowledge.
- **Wrong answer**: "Nil channels are a bug." (They have legitimate uses.)

### Key Takeaways

- Unbuffered = synchronization. Buffered = decoupling.
- Only the sender closes a channel.
- Use directional channels in function signatures.
- Nil channels are useful for disabling select cases.
- The ownership pattern prevents most channel bugs.
- Don't use huge buffers to hide coordination problems.

---

## Chapter 7: Select Statement

### What Problem Does This Solve?

You have a goroutine that needs to wait on multiple channels simultaneously. Without `select`, you'd have to pick one channel to wait on and ignore the others. `select` lets you multiplex — wait on all of them and react to whichever is ready first.

### Why Engineers Care

`select` is how you build:
- Timeouts
- Cancellation
- Fan-in (merging multiple channels)
- Heartbeats
- Priority channels
- Non-blocking channel operations

It's the control flow primitive for concurrent Go programs.

### Mental Model

Think of `select` as a `switch` statement for channels. It waits until one of the channel operations can proceed, then executes that case. If multiple cases are ready, it picks one **randomly** (not in order).

```go
select {
case msg := <-ch1:
    // ch1 had data ready
case msg := <-ch2:
    // ch2 had data ready
case ch3 <- value:
    // ch3 was ready to receive
}
```

### How It Works

```go
select {
case v := <-ch1:
    fmt.Println("from ch1:", v)
case v := <-ch2:
    fmt.Println("from ch2:", v)
case <-time.After(5 * time.Second):
    fmt.Println("timeout!")
}
```

**Key behaviors**:
1. **Blocks** until at least one case is ready.
2. **Random selection** when multiple cases are ready (prevents starvation).
3. **Default case** makes it non-blocking.
4. **Nil channels** are never selected (disabled).

### Essential Patterns

**Pattern 1: Timeout**
```go
select {
case result := <-longOperation():
    process(result)
case <-time.After(3 * time.Second):
    log.Println("operation timed out")
}
```

**Pattern 2: Cancellation with Context**
```go
func worker(ctx context.Context, jobs <-chan Job) {
    for {
        select {
        case <-ctx.Done():
            log.Println("cancelled:", ctx.Err())
            return
        case job := <-jobs:
            process(job)
        }
    }
}
```

**Pattern 3: Non-blocking send/receive**
```go
select {
case ch <- value:
    // Sent successfully
default:
    // Channel is full or no receiver ready — skip
    log.Println("dropped message")
}

select {
case val := <-ch:
    // Received
default:
    // Nothing available — skip
}
```

**Pattern 4: Fan-in (merge multiple channels)**
```go
func fanIn(ch1, ch2 <-chan string) <-chan string {
    merged := make(chan string)
    go func() {
        defer close(merged)
        for ch1 != nil || ch2 != nil {
            select {
            case v, ok := <-ch1:
                if !ok { ch1 = nil; continue }
                merged <- v
            case v, ok := <-ch2:
                if !ok { ch2 = nil; continue }
                merged <- v
            }
        }
    }()
    return merged
}
```

**Pattern 5: Heartbeat / Ticker**
```go
func worker(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            sendHeartbeat()
        case job := <-jobQueue:
            process(job)
        }
    }
}
```

**Pattern 6: Priority select (with care)**

Go's `select` doesn't have native priority. But you can simulate it:
```go
for {
    // Check high priority first
    select {
    case <-ctx.Done():
        return
    default:
    }
    
    // Then handle both
    select {
    case <-ctx.Done():
        return
    case job := <-jobs:
        process(job)
    }
}
```

### Common Mistakes

- **Mistake**: Assuming select cases are evaluated in order.
  ```go
  select {
  case <-ch1: // NOT checked first!
  case <-ch2:
  }
  ```
  **Reality**: If both are ready, one is chosen randomly.

- **Mistake**: Leaking `time.After` in a loop.
  ```go
  for {
      select {
      case <-time.After(5 * time.Second): // BUG: creates new timer every iteration!
          // Each timer is never garbage collected until it fires
      }
  }
  ```
  **Fix**: Use `time.NewTimer` or `time.NewTicker` and reuse it.

- **Mistake**: Forgetting that `select{}` (empty select) blocks forever.
  ```go
  select{} // Blocks forever — useful to keep main alive, but dangerous if unintentional
  ```

### Interview Questions

**Q: What happens when multiple select cases are ready?**
- **Answer**: Go randomly picks one. This is by design to prevent starvation — no channel gets starved if multiple are always ready.
- **Why asked**: Tests understanding of select fairness semantics.
- **Wrong answer**: "The first case is selected." (Order doesn't matter.)

**Q: How do you implement a timeout with select?**
- **Answer**: Use `time.After` or `context.WithTimeout` with `ctx.Done()`.
- **Follow-up**: "What's the problem with `time.After` in a loop?"

### Key Takeaways

- `select` multiplexes channel operations.
- Random selection prevents starvation.
- Use `default` for non-blocking operations.
- Use `time.After` for timeouts (but not in loops — use `time.NewTimer`).
- Use `ctx.Done()` for cancellation.
- Nil channels disable select cases.

---

## Chapter 8: Context

### What Problem Does This Solve?

In a production service, a single user request might spawn dozens of goroutines — database queries, cache lookups, microservice calls, background processing. When the user cancels the request (or it times out), you need ALL of those goroutines to stop. Context solves this coordination problem.

### Why Engineers Care

Context is everywhere in production Go:
- Every HTTP handler gets a context
- Every database call takes a context
- Every gRPC call takes a context
- Every well-written library function takes a context as the first parameter

If you don't understand context, you can't write production Go services.

### Mental Model

Think of context as an **invisible thread** connecting a parent operation to all its children. When you pull the thread (cancel), every child operation feels it and can stop.

```
HTTP Request (ctx)
├── DB Query (ctx)         ← cancelled when request cancelled
├── Cache Lookup (ctx)     ← cancelled when request cancelled
├── gRPC Call (ctx)        ← cancelled when request cancelled
│   ├── Sub-query (ctx)    ← cancelled when parent cancelled
│   └── Sub-query (ctx)    ← cancelled when parent cancelled
└── Analytics (ctx)        ← cancelled when request cancelled
```

Context forms a **tree**. Cancelling a parent cancels all children. Cancelling a child does NOT affect the parent.

### How It Works

**Four ways to create a derived context**:

```go
// 1. With cancellation — cancel manually when done
ctx, cancel := context.WithCancel(parentCtx)
defer cancel() // ALWAYS defer cancel to release resources

// 2. With deadline — cancels at a specific time
ctx, cancel := context.WithDeadline(parentCtx, time.Now().Add(5*time.Second))
defer cancel()

// 3. With timeout — cancels after a duration (sugar for WithDeadline)
ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
defer cancel()

// 4. With value — attach request-scoped data (use sparingly)
ctx := context.WithValue(parentCtx, requestIDKey, "abc-123")
```

**Checking for cancellation**:
```go
select {
case <-ctx.Done():
    // Context was cancelled or timed out
    fmt.Println("reason:", ctx.Err()) // context.Canceled or context.DeadlineExceeded
    return
case result := <-doWork():
    // Work completed before cancellation
}
```

### The Golden Rules of Context

1. **First parameter, always**: `func DoSomething(ctx context.Context, ...) error`
2. **Never store context in a struct**: Pass it as a function parameter.
3. **Always call cancel**: Even if the context will expire on its own. `defer cancel()` releases resources immediately.
4. **Never pass nil context**: Use `context.Background()` or `context.TODO()`.
5. **Context values are for request-scoped data only**: Request IDs, trace IDs, auth tokens. NOT for passing function parameters.

### Production Patterns

**HTTP handler with timeout**:
```go
func handleSearch(w http.ResponseWriter, r *http.Request) {
    // r.Context() is already set by the HTTP server
    // Add a tighter timeout for this specific operation
    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()
    
    results, err := searchService.Search(ctx, query)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            http.Error(w, "search timed out", http.StatusGatewayTimeout)
            return
        }
        http.Error(w, "search failed", http.StatusInternalServerError)
        return
    }
    
    json.NewEncoder(w).Encode(results)
}
```

**Parallel operations with shared context**:
```go
func fetchUserProfile(ctx context.Context, userID string) (*Profile, error) {
    g, ctx := errgroup.WithContext(ctx)
    
    var user *User
    var orders []Order
    var prefs *Preferences
    
    g.Go(func() error {
        var err error
        user, err = userService.Get(ctx, userID)
        return err
    })
    
    g.Go(func() error {
        var err error
        orders, err = orderService.List(ctx, userID)
        return err
    })
    
    g.Go(func() error {
        var err error
        prefs, err = prefService.Get(ctx, userID)
        return err
    })
    
    if err := g.Wait(); err != nil {
        return nil, err // If any fail, ctx is cancelled, others stop too
    }
    
    return &Profile{User: user, Orders: orders, Prefs: prefs}, nil
}
```

**Graceful shutdown**:
```go
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    
    // Listen for OS signals
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    
    go func() {
        <-sigCh
        log.Println("shutting down...")
        cancel() // Cancel root context — all children stop
    }()
    
    startWorkers(ctx)
    startHTTPServer(ctx)
    
    <-ctx.Done()
    log.Println("shutdown complete")
}
```

### Context Values — Use With Care

```go
type contextKey string

const requestIDKey contextKey = "requestID"

// Setting a value
ctx := context.WithValue(parentCtx, requestIDKey, "req-abc-123")

// Getting a value
reqID, ok := ctx.Value(requestIDKey).(string)
if !ok {
    reqID = "unknown"
}
```

**Rules for context values**:
- Use a custom unexported type for keys (prevents collisions).
- Only for request-scoped data that crosses API boundaries.
- **Good uses**: request ID, trace ID, auth token, deadline info.
- **Bad uses**: database connections, loggers, configuration, function parameters.

### Common Mistakes

- **Mistake**: Not checking `ctx.Done()` in long-running operations.
  ```go
  func processItems(ctx context.Context, items []Item) {
      for _, item := range items {
          process(item) // If ctx is cancelled, this keeps running!
      }
  }
  ```
  **Fix**:
  ```go
  func processItems(ctx context.Context, items []Item) error {
      for _, item := range items {
          select {
          case <-ctx.Done():
              return ctx.Err()
          default:
          }
          process(item)
      }
      return nil
  }
  ```

- **Mistake**: Not calling cancel.
  ```go
  ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
  // cancel is never called — resources leak until timeout expires
  ```
  **Fix**: `defer cancel()` immediately after creation.

- **Mistake**: Using `context.Background()` everywhere instead of propagating parent context.
  ```go
  func handler(w http.ResponseWriter, r *http.Request) {
      // BAD: ignores the request's context (cancellation, deadline)
      result := fetchData(context.Background())
      
      // GOOD: propagates the request's context
      result := fetchData(r.Context())
  }
  ```

### Interview Questions

**Q: Why should you always defer cancel()?**
- **Answer**: Even if the context will expire on its own, calling cancel() releases associated resources immediately (timers, goroutines). Failing to call it causes resource leaks until the parent is cancelled.
- **Why asked**: Tests understanding of context resource management.
- **Wrong answer**: "Cancel is optional for timeout contexts."

**Q: Can you cancel a parent context from a child?**
- **Answer**: No. Cancellation flows downward. A child can only cancel its own children. If you cancel a child context, the parent and siblings are unaffected.
- **Follow-up**: "Why is this design important?" (Prevents accidental cancellation of unrelated operations.)

### Key Takeaways

- Context = cancellation + deadlines + request-scoped values.
- Always pass context as the first parameter.
- Always `defer cancel()`.
- Never store context in structs.
- Cancellation flows from parent to children, never upward.
- Use `errgroup.WithContext` for parallel operations.
- Context values are for request-scoped metadata only.

---

## Chapter 9: Synchronization Primitives

### What Problem Does This Solve?

Channels are great for communication, but sometimes you just need to protect a shared variable, wait for a group of goroutines, or ensure something runs exactly once. That's where sync primitives come in.

### Why Engineers Care

Choosing the right synchronization primitive is a Staff Engineer skill. Using a channel when a mutex is simpler makes code harder to read. Using a mutex when a channel is clearer leads to bugs. Knowing when to use each is critical.

---

### sync.Mutex

**What it does**: Provides mutual exclusion — only one goroutine can hold the lock at a time.

**When to use**: Protecting shared data that multiple goroutines read and write.

```go
type SafeCounter struct {
    mu sync.Mutex
    count map[string]int
}

func (c *SafeCounter) Inc(key string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count[key]++
}

func (c *SafeCounter) Get(key string) int {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.count[key]
}
```

**Rules**:
- Always use `defer mu.Unlock()` to prevent deadlocks from panics.
- Never copy a mutex (pass by pointer).
- Keep the critical section (code between Lock and Unlock) as small as possible.
- A locked mutex is NOT tied to a goroutine — any goroutine can unlock it (but don't do this).

**Common mistake — locking too broadly**:
```go
// BAD: holding lock during I/O
func (c *Cache) Get(key string) (Value, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if v, ok := c.data[key]; ok {
        return v, nil
    }
    
    v, err := fetchFromDB(key) // Slow! Holds lock while waiting for DB
    if err != nil {
        return Value{}, err
    }
    
    c.data[key] = v
    return v, nil
}

// GOOD: only lock for map access
func (c *Cache) Get(key string) (Value, error) {
    c.mu.Lock()
    if v, ok := c.data[key]; ok {
        c.mu.Unlock()
        return v, nil
    }
    c.mu.Unlock()
    
    v, err := fetchFromDB(key)
    if err != nil {
        return Value{}, err
    }
    
    c.mu.Lock()
    c.data[key] = v
    c.mu.Unlock()
    return v, nil
}
```

---

### sync.RWMutex

**What it does**: Allows multiple readers OR one writer. Readers don't block each other; only writers need exclusive access.

**When to use**: When reads vastly outnumber writes (caches, configuration, routing tables).

```go
type Config struct {
    mu   sync.RWMutex
    data map[string]string
}

func (c *Config) Get(key string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.data[key]
}

func (c *Config) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.data[key] = value
}
```

**When NOT to use**: If writes are frequent, RWMutex can be slower than Mutex due to more complex internal bookkeeping. Profile before choosing.

---

### sync.WaitGroup

**What it does**: Waits for a collection of goroutines to finish.

```go
var wg sync.WaitGroup

for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        process(id)
    }(i)
}

wg.Wait() // Blocks until all 10 goroutines call Done()
```

**Rules**:
- Call `Add()` BEFORE launching the goroutine, not inside it.
- `Done()` is just `Add(-1)`.
- Never copy a WaitGroup.

**Common mistake**:
```go
// BUG: Add called inside goroutine — race condition
for i := 0; i < 10; i++ {
    go func(id int) {
        wg.Add(1)  // BUG: might not be called before wg.Wait()
        defer wg.Done()
        process(id)
    }(i)
}
wg.Wait()
```

---

### sync.Once

**What it does**: Ensures a function is called exactly once, no matter how many goroutines call it.

**Classic use case**: Lazy initialization.

```go
type DBPool struct {
    once sync.Once
    pool *sql.DB
}

func (d *DBPool) GetPool() *sql.DB {
    d.once.Do(func() {
        d.pool = createConnection() // Called exactly once
    })
    return d.pool
}
```

**Key property**: If the function panics, `Once` considers it "done" — it won't retry. Be careful with initialization that can fail.

**Go 1.21+**: `sync.OnceFunc`, `sync.OnceValue`, `sync.OnceValues` provide cleaner APIs:
```go
getPool := sync.OnceValue(func() *sql.DB {
    return createConnection()
})

pool := getPool() // First call initializes, subsequent calls return cached value
```

---

### sync.Cond

**What it does**: Lets goroutines wait for a condition to be true. A condition variable.

**When to use**: When you need to notify one or all waiting goroutines that something changed. Rare in practice — channels usually work better.

```go
type Queue struct {
    mu    sync.Mutex
    cond  *sync.Cond
    items []int
}

func NewQueue() *Queue {
    q := &Queue{}
    q.cond = sync.NewCond(&q.mu)
    return q
}

func (q *Queue) Enqueue(item int) {
    q.mu.Lock()
    q.items = append(q.items, item)
    q.mu.Unlock()
    q.cond.Signal() // Wake one waiter
}

func (q *Queue) Dequeue() int {
    q.mu.Lock()
    for len(q.items) == 0 {
        q.cond.Wait() // Releases lock, waits, reacquires lock
    }
    item := q.items[0]
    q.items = q.items[1:]
    q.mu.Unlock()
    return item
}
```

**In practice**: Most Go engineers never use `sync.Cond`. Channels cover almost all the same use cases more clearly. Use `Cond` only when you need `Broadcast` (wake all waiters) and channels would be awkward.

---

### sync/atomic

**What it does**: Lock-free atomic operations on integers, pointers, and values.

**When to use**: Simple counters, flags, and statistics where a full mutex is overkill.

```go
var requestCount atomic.Int64

func handleRequest(w http.ResponseWriter, r *http.Request) {
    requestCount.Add(1)
    // handle request
}

func metricsHandler(w http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(w, "requests: %d", requestCount.Load())
}
```

**Go 1.19+ typed atomics**:
```go
var (
    counter  atomic.Int64
    flag     atomic.Bool
    pointer  atomic.Pointer[Config]
)

counter.Add(1)
flag.Store(true)
pointer.Store(&Config{MaxRetries: 3})
cfg := pointer.Load()
```

**When to use atomic vs mutex**:
- **Atomic**: Single variable, simple operations (increment, load, store, compare-and-swap).
- **Mutex**: Multiple variables that must be updated together, complex operations.

```go
// Atomic is perfect here
var hitCount atomic.Int64
hitCount.Add(1)

// Mutex is needed here (two variables must be consistent)
mu.Lock()
total += amount
count++
mu.Unlock()
```

---

### The Decision Matrix

| Need | Use |
|------|-----|
| Protect a map/struct from concurrent access | `sync.Mutex` |
| Read-heavy, write-rare shared data | `sync.RWMutex` |
| Wait for N goroutines to finish | `sync.WaitGroup` |
| Initialize something exactly once | `sync.Once` |
| Simple counter / flag | `sync/atomic` |
| Notify waiting goroutines of a condition | `sync.Cond` (rare) |
| Coordinate goroutine communication | Channels |
| Limit concurrent access | Buffered channel as semaphore |

### Key Takeaways

- Mutex for protecting shared data. RWMutex when reads >> writes.
- WaitGroup for "wait for all these goroutines to finish."
- Once for lazy initialization.
- Atomic for simple counters — much faster than mutex.
- Cond is rare — channels usually work better.
- Keep critical sections small. Never hold a lock during I/O.
- Never copy sync primitives.

---

## Chapter 10: Race Conditions

### What Problem Does This Solve?

A race condition happens when two goroutines access the same variable and at least one of them writes to it, without synchronization. The result depends on which goroutine runs first — it's non-deterministic. This is the #1 source of concurrent bugs.

### Why Engineers Care

Race conditions cause:
- Corrupted data
- Incorrect business logic
- Crashes that only happen under load
- Bugs that are impossible to reproduce locally but appear in production
- Security vulnerabilities

### Mental Model

Imagine two bank tellers processing withdrawals from the same account simultaneously:

```
Account balance: $100

Teller A reads balance: $100        Teller B reads balance: $100
Teller A subtracts $80: $20         Teller B subtracts $70: $30
Teller A writes balance: $20       Teller B writes balance: $30

Final balance: $30 (should be -$50 or rejected!)
```

Both tellers read the same value, computed independently, and one write clobbered the other.

### How It Happens in Go

```go
// RACE CONDITION
var counter int

func main() {
    for i := 0; i < 1000; i++ {
        go func() {
            counter++ // Read + modify + write — not atomic!
        }()
    }
    time.Sleep(time.Second)
    fmt.Println(counter) // Almost never 1000
}
```

`counter++` looks like one operation but it's three:
1. Read the current value of `counter`
2. Add 1
3. Write the new value back

Two goroutines can both read the same value, both add 1, and both write the same result — losing an increment.

### Detection: The Race Detector

Go has a built-in race detector. Use it. Always.

```bash
go test -race ./...          # Run tests with race detection
go run -race main.go         # Run program with race detection
go build -race -o myapp      # Build with race detection
```

**What it does**: Instruments memory accesses at compile time. At runtime, it detects when two goroutines access the same variable concurrently without synchronization.

**Output looks like this**:
```
WARNING: DATA RACE
Write at 0x00c0000a0010 by goroutine 7:
  main.main.func1()
      /app/main.go:15 +0x38

Previous read at 0x00c0000a0010 by goroutine 6:
  main.main.func1()
      /app/main.go:15 +0x2e
```

**Important**: The race detector only catches races that actually happen during the run. It doesn't prove the absence of races. Run it with good test coverage.

### Prevention Strategies

**Strategy 1: Mutex**
```go
var mu sync.Mutex
var counter int

mu.Lock()
counter++
mu.Unlock()
```

**Strategy 2: Atomic operations**
```go
var counter atomic.Int64
counter.Add(1)
```

**Strategy 3: Channel-based ownership**
```go
// Only one goroutine owns the data — no sharing
counterCh := make(chan int)
go func() {
    count := 0
    for range counterCh {
        count++
    }
}()
counterCh <- 1 // Send "increment" signal
```

**Strategy 4: Confinement**
```go
// Each goroutine gets its own data — no sharing needed
results := make([]int, len(items))
var wg sync.WaitGroup
for i, item := range items {
    wg.Add(1)
    go func(i int, item Item) {
        defer wg.Done()
        results[i] = process(item) // Each goroutine writes to its own index
    }(i, item)
}
wg.Wait()
```

### Real Production Example

```go
// BUG: Race condition in a web service's in-memory cache
type Cache struct {
    data map[string]string
}

func (c *Cache) Get(key string) string {
    return c.data[key] // Concurrent map read — RACE
}

func (c *Cache) Set(key, value string) {
    c.data[key] = value // Concurrent map write — CRASH (fatal error: concurrent map writes)
}

// FIX: Use sync.RWMutex
type Cache struct {
    mu   sync.RWMutex
    data map[string]string
}

func (c *Cache) Get(key string) string {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.data[key]
}

func (c *Cache) Set(key, value string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.data[key] = value
}
```

**Note**: Go's maps are NOT safe for concurrent use. Concurrent reads are fine, but concurrent read+write or write+write causes a **fatal crash** (not a data race — a hard crash with `fatal error: concurrent map writes`).

### Common Mistakes

- **Mistake**: "It works in my tests, so there's no race."
  - **Reality**: Race conditions are timing-dependent. They might only happen under load, on different hardware, or with different scheduling.
  
- **Mistake**: "I'll just use `time.Sleep` to avoid the race."
  - **Reality**: Sleep doesn't provide any guarantees. It might work 99% of the time, making the bug even harder to find.

- **Mistake**: Not running `-race` in CI.
  - **Fix**: Always include `go test -race ./...` in your CI pipeline.

### Interview Questions

**Q: What is a data race and how do you detect it in Go?**
- **Answer**: A data race occurs when two goroutines access the same variable concurrently and at least one is a write, without synchronization. Go has a built-in race detector activated with the `-race` flag.
- **Why asked**: Tests fundamental concurrent programming knowledge.
- **Wrong answer**: "A race condition is when code runs in the wrong order." (That's a symptom, not the definition.)
- **Follow-up**: "Is the race detector sufficient to guarantee no races?" (No — it only detects races that occur during the run.)

### Key Takeaways

- Race conditions = concurrent unsynchronized access with at least one write.
- Always run `go test -race` in CI.
- Go maps crash on concurrent writes — always protect with a mutex.
- Confinement (each goroutine gets its own data) is the safest strategy.
- The race detector catches races that happen, not races that could happen.

---

## Chapter 11: Deadlocks

### What Problem Does This Solve?

A deadlock is when two or more goroutines are waiting for each other, and none can proceed. The program freezes. In production, this means hung requests, unresponsive services, and pager alerts at 3 AM.

### Why Engineers Care

Deadlocks are one of the hardest concurrency bugs to debug because:
- The program doesn't crash — it just hangs.
- It might only happen under specific load patterns.
- The stack trace shows goroutines waiting, but understanding WHY requires understanding the full flow.

### Mental Model

Think of two people in a narrow hallway. Person A says, "I'll move when you move." Person B says, "I'll move when you move." Neither moves. That's a deadlock.

**Four conditions for deadlock** (all must be true):
1. **Mutual exclusion**: Resources can't be shared (locks are exclusive).
2. **Hold and wait**: A goroutine holds one resource while waiting for another.
3. **No preemption**: Resources can't be forcibly taken away.
4. **Circular wait**: A cycle of goroutines, each waiting for the next.

### Common Deadlock Scenarios

**Scenario 1: Unbuffered channel with no receiver**
```go
func main() {
    ch := make(chan int)
    ch <- 42 // DEADLOCK: no goroutine to receive
    fmt.Println(<-ch)
}
```
Go detects this: `fatal error: all goroutines are asleep - deadlock!`

**Scenario 2: Two goroutines waiting on each other**
```go
ch1 := make(chan int)
ch2 := make(chan int)

go func() {
    <-ch1    // Wait for ch1
    ch2 <- 1 // Then send on ch2
}()

go func() {
    <-ch2    // Wait for ch2
    ch1 <- 1 // Then send on ch1
}()
// Both goroutines wait forever
```

**Scenario 3: Lock ordering violation**
```go
var muA, muB sync.Mutex

// Goroutine 1
go func() {
    muA.Lock()
    time.Sleep(time.Millisecond)
    muB.Lock()  // Waits for muB — held by goroutine 2
    muB.Unlock()
    muA.Unlock()
}()

// Goroutine 2
go func() {
    muB.Lock()
    time.Sleep(time.Millisecond)
    muA.Lock()  // Waits for muA — held by goroutine 1
    muA.Unlock()
    muB.Unlock()
}()
// DEADLOCK: circular wait
```

**Scenario 4: Forgetting to release a lock**
```go
func (s *Service) Process() {
    s.mu.Lock()
    if someCondition {
        return // BUG: forgot to Unlock!
    }
    s.mu.Unlock()
}
```
**Fix**: Always use `defer s.mu.Unlock()`.

**Scenario 5: Self-deadlock (re-locking a non-reentrant mutex)**
```go
func (s *Service) A() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.B() // Calls B, which also tries to lock
}

func (s *Service) B() {
    s.mu.Lock()   // DEADLOCK: already locked by A
    defer s.mu.Unlock()
}
```
**Fix**: Separate the locked and unlocked parts:
```go
func (s *Service) A() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.bLocked() // Internal method that assumes lock is held
}

func (s *Service) B() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.bLocked()
}

func (s *Service) bLocked() {
    // Does the work, assumes caller holds the lock
}
```

### Detection

**Go runtime detection**: Go detects deadlocks where ALL goroutines are blocked. If even one goroutine is running (like an HTTP server), Go won't detect it.

**pprof goroutine dump**: In production, expose the pprof endpoint and inspect blocked goroutines:
```go
import _ "net/http/pprof"

go func() {
    log.Println(http.ListenAndServe(":6060", nil))
}()
```
Then: `curl http://localhost:6060/debug/pprof/goroutine?debug=2`

Look for goroutines stuck in `semacquire`, `chanrecv`, or `chansend`.

**Goroutine count monitoring**: If goroutine count keeps growing and never drops, you likely have deadlocks or leaks.

### Prevention

1. **Consistent lock ordering**: Always acquire locks in the same order everywhere.
2. **Avoid nested locks**: If you must, document the lock order.
3. **Use `defer Unlock()`**: Prevents forgotten unlocks.
4. **Use context with timeout**: Prevents waiting forever.
5. **Prefer channels over mutexes for coordination**: Channels with `select` and timeouts are less deadlock-prone.
6. **Use `go vet` and race detector**: They catch some deadlock scenarios.

### Interview Questions

**Q: How would you debug a deadlock in production?**
- **Answer**: Expose pprof endpoint, get a goroutine dump (`/debug/pprof/goroutine?debug=2`), look for goroutines stuck in lock acquisition or channel operations. Trace the dependency chain to find the cycle.
- **Why asked**: Tests practical debugging ability.
- **Wrong answer**: "Add print statements." (Insufficient for production deadlocks.)
- **Follow-up**: "How would you prevent deadlocks in a new system?" (Consistent lock ordering, timeouts, prefer channels.)

### Key Takeaways

- Deadlocks = circular wait where no goroutine can proceed.
- Go only detects deadlocks when ALL goroutines are blocked.
- Always use `defer mu.Unlock()`.
- Maintain consistent lock ordering.
- Use pprof to debug deadlocks in production.
- Context with timeout prevents indefinite waiting.
- Go mutexes are NOT reentrant — calling Lock() twice from the same goroutine deadlocks.

---

# Part III — Production Patterns

---

## Chapter 12: Goroutine Leaks

### What Problem Does This Solve?

A goroutine leak is when you launch a goroutine that never exits. It sits there forever, consuming memory, holding resources, and slowly killing your service. It's the memory leak of Go.

### Why Engineers Care

This is one of the top production issues in Go services. Your service starts fine, handles traffic for hours, then starts getting OOM-killed. You look at metrics and see goroutine count climbing linearly. That's a leak.

### Mental Model

Think of goroutines as restaurant orders. If you send an order to the kitchen but nobody ever picks it up, it just sits there. Send enough forgotten orders and the kitchen runs out of space.

Every `go` statement is a promise: "I will make sure this goroutine can finish." Breaking that promise is a leak.

### Common Causes

**Cause 1: Blocked channel with no sender**
```go
func leak() {
    ch := make(chan int)
    go func() {
        val := <-ch // Blocks forever — nobody sends
        fmt.Println(val)
    }()
    // Function returns, ch is garbage collected, but goroutine is stuck forever
}
```

**Cause 2: Blocked channel with no receiver**
```go
func leak() {
    ch := make(chan int, 0)
    go func() {
        ch <- 42 // Blocks forever — nobody receives
    }()
    // We never read from ch
}
```

**Cause 3: Missing context cancellation**
```go
func leak(ctx context.Context) {
    go func() {
        for {
            doWork()
            time.Sleep(time.Second)
            // Never checks ctx.Done() — runs forever even when ctx is cancelled
        }
    }()
}
```

**Cause 4: Infinite loop with no exit**
```go
func leak() {
    go func() {
        for {
            select {
            case job := <-jobCh:
                process(job)
            }
            // No ctx.Done() case — can never stop
        }
    }()
}
```

**Cause 5: HTTP request without timeout**
```go
func leak() {
    go func() {
        resp, err := http.Get("https://slow-service.com/api") // No timeout — can hang forever
        if err != nil { return }
        defer resp.Body.Close()
    }()
}
```

**Cause 6: Sending to a channel after nobody cares**
```go
func fetchWithTimeout(ctx context.Context) (Result, error) {
    ch := make(chan Result, 1)  // MUST be buffered!
    go func() {
        result := expensiveCall()
        ch <- result  // If unbuffered and ctx timed out, this blocks forever
    }()
    
    select {
    case result := <-ch:
        return result, nil
    case <-ctx.Done():
        return Result{}, ctx.Err()
        // If ch is unbuffered, the goroutine is leaked!
        // Buffer of 1 lets the goroutine send and exit even if nobody reads
    }
}
```

### Detection

**Runtime metric**:
```go
runtime.NumGoroutine() // Check this periodically
```

**pprof**:
```go
import _ "net/http/pprof"

// Then: curl http://localhost:6060/debug/pprof/goroutine?debug=1
// Shows counts by function

// debug=2 shows full stack traces of all goroutines
```

**Prometheus metric**:
```go
prometheus.NewGaugeFunc(prometheus.GaugeOpts{
    Name: "go_goroutines",
    Help: "Number of goroutines",
}, func() float64 {
    return float64(runtime.NumGoroutine())
})
```

**In tests — goleak**:
```go
import "go.uber.org/goleak"

func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}

func TestNoLeak(t *testing.T) {
    defer goleak.VerifyNone(t)
    // your test code
}
```

### Prevention Checklist

For every `go` statement, ask yourself:
1. **How does this goroutine exit?** (context cancellation, channel close, return condition)
2. **What if the receiver/sender is gone?** (Use buffered channels)
3. **What if the external call hangs?** (Use context with timeout)
4. **Who owns this goroutine's lifecycle?** (Parent should manage shutdown)

### Production-Ready Pattern

```go
func startWorker(ctx context.Context, jobs <-chan Job, results chan<- Result) {
    go func() {
        for {
            select {
            case <-ctx.Done():
                return // Clean exit
            case job, ok := <-jobs:
                if !ok {
                    return // Channel closed, clean exit
                }
                result := process(job)
                select {
                case results <- result:
                case <-ctx.Done():
                    return // Don't block on send if cancelled
                }
            }
        }
    }()
}
```

### Real Production Example

A microservice made parallel HTTP calls but didn't set timeouts:

```go
// LEAKED: goroutines hung on slow downstream services
func fetchFromAll(services []string) []Response {
    ch := make(chan Response)
    for _, svc := range services {
        go func(url string) {
            resp, _ := http.Get(url) // No timeout!
            ch <- Response{Data: resp}
        }(svc)
    }
    // Collected results with a 5s timeout
    // But goroutines calling slow services were stuck forever
}

// FIXED: context with timeout on every HTTP call
func fetchFromAll(ctx context.Context, services []string) []Response {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    
    ch := make(chan Response, len(services))
    for _, svc := range services {
        go func(url string) {
            req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
            resp, err := http.DefaultClient.Do(req)
            if err != nil {
                ch <- Response{Err: err}
                return
            }
            ch <- Response{Data: resp}
        }(svc)
    }
    
    var results []Response
    for i := 0; i < len(services); i++ {
        select {
        case r := <-ch:
            results = append(results, r)
        case <-ctx.Done():
            return results
        }
    }
    return results
}
```

### Interview Questions

**Q: What causes goroutine leaks and how do you prevent them?**
- **Answer**: Goroutines leak when they block forever on channel operations, missing context checks, or hanging external calls. Prevent by: always checking ctx.Done(), using buffered channels for fire-and-forget sends, setting timeouts on all I/O, and using goleak in tests.
- **Why asked**: This is a top production issue. Interviewers want to know you've dealt with it.
- **Wrong answer**: "Go's garbage collector handles goroutine cleanup." (It doesn't — goroutines are not garbage collected.)

### Key Takeaways

- Every `go` statement needs an exit strategy.
- Goroutines are NOT garbage collected — they run until they return.
- Use goleak in tests to catch leaks early.
- Monitor `runtime.NumGoroutine()` in production.
- Buffered channels prevent leaks in timeout patterns.
- Context with timeout prevents hanging on external calls.

---

## Chapter 13: Worker Pools

### What Problem Does This Solve?

You have 1 million jobs to process. Spawning 1 million goroutines would consume too much memory and overwhelm downstream systems. A worker pool limits concurrency to N workers that process jobs from a shared queue.

### Why Engineers Care

Worker pools are the backbone of:
- Background job processing
- Batch data processing
- Rate-limited API calls
- Image/video processing
- Database migrations
- Any "process N items with limited concurrency" scenario

### Mental Model

Think of a fast-food restaurant. You don't hire 1000 cooks for 1000 orders. You have 5 cooks and a queue of orders. Each cook grabs the next order when they finish the current one.

### Pattern 1: Simple Worker Pool

```go
func workerPool(ctx context.Context, numWorkers int, jobs <-chan Job) <-chan Result {
    results := make(chan Result)
    var wg sync.WaitGroup
    
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for {
                select {
                case <-ctx.Done():
                    return
                case job, ok := <-jobs:
                    if !ok {
                        return
                    }
                    results <- process(job)
                }
            }
        }()
    }
    
    go func() {
        wg.Wait()
        close(results)
    }()
    
    return results
}
```

### Pattern 2: Worker Pool with errgroup

```go
func processAll(ctx context.Context, items []Item) error {
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(10) // Max 10 concurrent goroutines
    
    for _, item := range items {
        item := item
        g.Go(func() error {
            return processItem(ctx, item)
        })
    }
    
    return g.Wait() // Returns first error, cancels remaining
}
```

This is the modern, idiomatic approach for most use cases. `errgroup` handles WaitGroup, error propagation, and concurrency limiting.

### Pattern 3: Semaphore-Based Pool

```go
func processWithSemaphore(ctx context.Context, items []Item) {
    sem := make(chan struct{}, 10) // Max 10 concurrent
    var wg sync.WaitGroup
    
    for _, item := range items {
        wg.Add(1)
        sem <- struct{}{} // Acquire — blocks when 10 are running
        
        go func(item Item) {
            defer wg.Done()
            defer func() { <-sem }() // Release
            process(item)
        }(item)
    }
    
    wg.Wait()
}
```

### Pattern 4: Production Worker Pool with Graceful Shutdown

```go
type Pool struct {
    jobs    chan Job
    results chan Result
    done    chan struct{}
    wg      sync.WaitGroup
}

func NewPool(numWorkers, jobBufferSize int) *Pool {
    p := &Pool{
        jobs:    make(chan Job, jobBufferSize),
        results: make(chan Result, jobBufferSize),
        done:    make(chan struct{}),
    }
    
    for i := 0; i < numWorkers; i++ {
        p.wg.Add(1)
        go p.worker(i)
    }
    
    go func() {
        p.wg.Wait()
        close(p.results)
        close(p.done)
    }()
    
    return p
}

func (p *Pool) worker(id int) {
    defer p.wg.Done()
    for job := range p.jobs {
        result := process(job)
        p.results <- result
    }
}

func (p *Pool) Submit(job Job) {
    p.jobs <- job
}

func (p *Pool) Shutdown() {
    close(p.jobs) // Signal workers to stop
    <-p.done       // Wait for all workers to finish
}

func (p *Pool) Results() <-chan Result {
    return p.results
}
```

### Choosing the Right Pattern

| Scenario | Pattern |
|----------|---------|
| Simple batch processing | errgroup with SetLimit |
| Long-running background workers | Channel-based worker pool |
| Need to control job submission rate | Semaphore |
| Complex lifecycle management | Struct-based pool |
| One-off parallel tasks | errgroup |

### Performance Considerations

- **Too few workers**: Underutilized resources, slow processing.
- **Too many workers**: Overwhelms downstream services, memory pressure.
- **Rule of thumb**: For I/O-bound work, workers = 10-100x CPU cores. For CPU-bound work, workers ≈ CPU cores.
- **Job channel buffer size**: Small buffer (0-10) for backpressure. Large buffer for burst absorption.

### Key Takeaways

- Worker pools limit concurrency to protect resources.
- `errgroup.SetLimit` is the modern, simple approach.
- Always handle graceful shutdown.
- Size workers based on whether work is CPU-bound or I/O-bound.

---

## Chapter 14: Fan-Out / Fan-In

### What Problem Does This Solve?

**Fan-out**: Distribute work from one source to multiple workers (parallelize).
**Fan-in**: Collect results from multiple sources into one channel (aggregate).

### Why Engineers Care

This is the fundamental pattern for parallel processing in Go. Every time you split work across goroutines and collect results, you're doing fan-out/fan-in.

### Mental Model

```
                    ┌── Worker 1 ──┐
                    │              │
Input ── Fan-Out ──├── Worker 2 ──├── Fan-In ── Output
                    │              │
                    └── Worker 3 ──┘
```

### Fan-Out

```go
func fanOut(ctx context.Context, input <-chan Job, numWorkers int) []<-chan Result {
    workers := make([]<-chan Result, numWorkers)
    for i := 0; i < numWorkers; i++ {
        workers[i] = worker(ctx, input)
    }
    return workers
}

func worker(ctx context.Context, input <-chan Job) <-chan Result {
    out := make(chan Result)
    go func() {
        defer close(out)
        for job := range input {
            select {
            case <-ctx.Done():
                return
            case out <- process(job):
            }
        }
    }()
    return out
}
```

### Fan-In

```go
func fanIn(ctx context.Context, channels ...<-chan Result) <-chan Result {
    merged := make(chan Result)
    var wg sync.WaitGroup
    
    for _, ch := range channels {
        wg.Add(1)
        go func(c <-chan Result) {
            defer wg.Done()
            for val := range c {
                select {
                case <-ctx.Done():
                    return
                case merged <- val:
                }
            }
        }(ch)
    }
    
    go func() {
        wg.Wait()
        close(merged)
    }()
    
    return merged
}
```

### Complete Example: Parallel Image Processing

```go
func processImages(ctx context.Context, paths []string) []ProcessedImage {
    // Fan-out: send paths to workers
    jobs := make(chan string, len(paths))
    go func() {
        defer close(jobs)
        for _, p := range paths {
            jobs <- p
        }
    }()
    
    // Fan-out: create 4 workers
    workers := make([]<-chan ProcessedImage, 4)
    for i := 0; i < 4; i++ {
        workers[i] = imageWorker(ctx, jobs)
    }
    
    // Fan-in: merge all results
    results := fanIn(ctx, workers...)
    
    var processed []ProcessedImage
    for img := range results {
        processed = append(processed, img)
    }
    return processed
}
```

### Key Takeaways

- Fan-out = distribute work. Fan-in = collect results.
- Multiple workers reading from the same channel is fan-out (Go handles this safely).
- Use WaitGroup to close the fan-in channel when all workers are done.
- Always respect context cancellation in both fan-out and fan-in.

---

## Chapter 15: Pipelines

### What Problem Does This Solve?

A pipeline is a series of stages where each stage takes input, processes it, and passes output to the next stage. Each stage runs concurrently.

### Why Engineers Care

Pipelines are how you build data processing systems in Go — ETL jobs, stream processing, log analysis, data transformation.

### Mental Model

```
Source → Stage 1 (Filter) → Stage 2 (Transform) → Stage 3 (Aggregate) → Sink
```

Each stage is a goroutine. Stages communicate through channels. Data flows through the pipeline like water through connected pipes.

### Building a Pipeline

```go
// Stage 1: Generate numbers
func generate(ctx context.Context, nums ...int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for _, n := range nums {
            select {
            case <-ctx.Done():
                return
            case out <- n:
            }
        }
    }()
    return out
}

// Stage 2: Square each number
func square(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            select {
            case <-ctx.Done():
                return
            case out <- n * n:
            }
        }
    }()
    return out
}

// Stage 3: Filter even numbers
func filterEven(ctx context.Context, in <-chan int) <-chan int {
    out := make(chan int)
    go func() {
        defer close(out)
        for n := range in {
            if n%2 == 0 {
                select {
                case <-ctx.Done():
                    return
                case out <- n:
                }
            }
        }
    }()
    return out
}

// Compose the pipeline
func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // Pipeline: generate → square → filter
    pipeline := filterEven(ctx, square(ctx, generate(ctx, 1, 2, 3, 4, 5)))
    
    for val := range pipeline {
        fmt.Println(val) // 4, 16
    }
}
```

### Pipeline Cancellation

The beauty of this pattern is that cancelling the context stops ALL stages:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// If timeout fires, all stages receive ctx.Done() and clean up
pipeline := stage3(ctx, stage2(ctx, stage1(ctx)))
```

### Real Production Pipeline: Log Processing

```go
func processLogs(ctx context.Context, reader io.Reader) <-chan Alert {
    lines := readLines(ctx, reader)           // Stage 1: Read log lines
    parsed := parseLogs(ctx, lines)            // Stage 2: Parse into structs
    errors := filterErrors(ctx, parsed)        // Stage 3: Filter error logs
    enriched := enrichWithMetadata(ctx, errors) // Stage 4: Add metadata
    alerts := detectAnomalies(ctx, enriched)    // Stage 5: Detect patterns
    return alerts
}
```

### Key Takeaways

- Each pipeline stage is a function that returns a channel.
- Stages connect by passing one stage's output channel as the next stage's input.
- Always use context for cancellation — it propagates through the entire pipeline.
- Close output channels in each stage (use `defer close(out)`) so downstream stages terminate.

---

## Chapter 16: Backpressure

### What Problem Does This Solve?

What happens when your producer is faster than your consumer? Without backpressure, work piles up in memory, your service OOMs, and you get paged at 2 AM.

Backpressure is the mechanism by which a consumer tells a producer to slow down.

### Why Engineers Care

Every production system has producers and consumers running at different speeds. If you don't handle this, you get:
- Out-of-memory crashes
- Unbounded queue growth
- Cascading failures
- Dropped messages at the worst possible time

### Mental Model

Think of a water pipe system. If water comes in faster than it drains, the pipe fills up. Backpressure is like the pipe pushing back against the source — slowing the water flow.

In Go, **unbuffered channels provide natural backpressure**. The producer can't send until the consumer is ready.

### Implementing Backpressure

**Strategy 1: Bounded channels (natural backpressure)**
```go
jobs := make(chan Job, 100) // Buffer of 100

// Producer blocks when buffer is full — natural backpressure
go func() {
    for _, item := range items {
        jobs <- item // Blocks when 100 jobs are queued
    }
    close(jobs)
}()
```

**Strategy 2: Drop oldest on overflow**
```go
func sendWithDrop(ch chan<- Event, event Event) {
    select {
    case ch <- event:
    default:
        // Channel full — drop the event
        log.Println("dropping event, consumer too slow")
    }
}
```

**Strategy 3: Rate limiting the producer**
```go
func producer(ctx context.Context, out chan<- Job) {
    limiter := rate.NewLimiter(rate.Every(time.Millisecond), 100) // 100 per second burst
    
    for _, job := range jobs {
        if err := limiter.Wait(ctx); err != nil {
            return
        }
        out <- job
    }
}
```

**Strategy 4: Dynamic worker scaling**
```go
func adaptivePool(ctx context.Context, jobs <-chan Job) {
    pending := len(jobs)
    workers := 1
    
    if pending > 1000 {
        workers = 10 // Scale up
    } else if pending > 100 {
        workers = 5
    }
    
    for i := 0; i < workers; i++ {
        go worker(ctx, jobs)
    }
}
```

### Key Takeaways

- Unbounded queues are a ticking time bomb.
- Unbuffered channels provide strongest backpressure.
- Bounded buffered channels are the most common approach.
- Choose: block, drop, or rate-limit. Never just buffer infinitely.
- Monitor queue depth as a key metric.

---

## Chapter 17: Rate Limiting

### What Problem Does This Solve?

You need to limit how fast your service processes requests — to protect downstream APIs, respect external rate limits, or prevent resource exhaustion.

### Why Engineers Care

Every service that calls external APIs needs rate limiting. Every API that serves clients needs rate limiting. It's a fundamental production concern.

### Token Bucket with `golang.org/x/time/rate`

```go
import "golang.org/x/time/rate"

// Allow 10 requests per second with a burst of 20
limiter := rate.NewLimiter(10, 20)

func handleRequest(w http.ResponseWriter, r *http.Request) {
    if !limiter.Allow() {
        http.Error(w, "rate limited", http.StatusTooManyRequests)
        return
    }
    // Process request
}

// Or wait for permission (blocking)
func processJob(ctx context.Context, job Job) error {
    if err := limiter.Wait(ctx); err != nil {
        return err // Context cancelled while waiting
    }
    return doWork(job)
}
```

### Per-Client Rate Limiting

```go
type ClientLimiter struct {
    mu       sync.Mutex
    limiters map[string]*rate.Limiter
}

func (cl *ClientLimiter) GetLimiter(clientID string) *rate.Limiter {
    cl.mu.Lock()
    defer cl.mu.Unlock()
    
    if limiter, ok := cl.limiters[clientID]; ok {
        return limiter
    }
    
    limiter := rate.NewLimiter(5, 10) // 5 req/s per client
    cl.limiters[clientID] = limiter
    return limiter
}

func (cl *ClientLimiter) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        clientID := r.Header.Get("X-Client-ID")
        if !cl.GetLimiter(clientID).Allow() {
            http.Error(w, "rate limited", http.StatusTooManyRequests)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Rate Limiting with Channel-Based Ticker

```go
// Simple rate limiter using time.Ticker
func rateLimitedWorker(ctx context.Context, jobs <-chan Job, rps int) {
    ticker := time.NewTicker(time.Second / time.Duration(rps))
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            select {
            case job := <-jobs:
                process(job)
            default:
            }
        }
    }
}
```

### Key Takeaways

- Use `golang.org/x/time/rate` for production rate limiting.
- Token bucket allows bursts while enforcing average rate.
- Per-client limiting prevents a single client from monopolizing resources.
- Rate limiting is both a client-side concern (calling APIs) and server-side concern (serving APIs).

---

# Part IV — Concurrency in Real-World Systems

---

## Chapter 18: Concurrency in APIs

### What Problem Does This Solve?

Your API handler needs to fetch data from multiple sources — database, cache, other services — to build a response. Doing this sequentially is slow. Doing it concurrently cuts latency dramatically.

### Production Pattern: Parallel Data Fetching

```go
func getUserDashboard(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
    defer cancel()
    
    userID := chi.URLParam(r, "userID")
    
    type result struct {
        user    *User
        orders  []Order
        recs    []Recommendation
        err     error
    }
    
    g, ctx := errgroup.WithContext(ctx)
    var res result
    
    g.Go(func() error {
        var err error
        res.user, err = userService.Get(ctx, userID)
        return fmt.Errorf("user fetch: %w", err)
    })
    
    g.Go(func() error {
        var err error
        res.orders, err = orderService.List(ctx, userID)
        return fmt.Errorf("orders fetch: %w", err)
    })
    
    g.Go(func() error {
        var err error
        res.recs, err = recService.Get(ctx, userID)
        if err != nil {
            // Recommendations are non-critical — log and continue
            log.Printf("recs failed: %v", err)
            return nil
        }
        return nil
    })
    
    if err := g.Wait(); err != nil {
        http.Error(w, "failed to build dashboard", http.StatusInternalServerError)
        return
    }
    
    json.NewEncoder(w).Encode(res)
}
```

### Pattern: Request Coalescing (Singleflight)

When many concurrent requests ask for the same data, don't hit the database 1000 times. Use `singleflight` to deduplicate:

```go
import "golang.org/x/sync/singleflight"

var sf singleflight.Group

func getUser(ctx context.Context, userID string) (*User, error) {
    result, err, _ := sf.Do(userID, func() (interface{}, error) {
        // Only ONE of the concurrent callers executes this
        return db.GetUser(ctx, userID)
    })
    if err != nil {
        return nil, err
    }
    return result.(*User), nil
}
```

If 100 goroutines all call `getUser("user-123")` at the same time, only ONE database query executes. The other 99 wait and share the result.

### Pattern: Graceful Degradation

```go
func fetchWithFallback(ctx context.Context, userID string) (*UserData, error) {
    // Try primary with 500ms timeout
    fastCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
    defer cancel()
    
    data, err := primaryService.Get(fastCtx, userID)
    if err == nil {
        return data, nil
    }
    
    // Fallback to cache
    return cache.Get(ctx, userID)
}
```

### Key Takeaways

- Use `errgroup` for parallel API data fetching.
- Use `singleflight` to deduplicate concurrent identical requests.
- Set tight timeouts per operation, not just per request.
- Design for graceful degradation — some data is optional.

---

## Chapter 19: Concurrency in Microservices

### What Problem Does This Solve?

Microservices communicate over the network. Network calls are slow and unreliable. Without proper concurrency patterns, one slow service brings down your entire system.

### The Cascade Failure Problem

```
Service A → Service B → Service C (slow)
                ↓
Service A goroutines pile up waiting for B waiting for C
                ↓
Service A runs out of goroutines/memory → OOM
```

### Prevention Patterns

**Pattern 1: Timeout at every boundary**
```go
func callServiceB(ctx context.Context, req *Request) (*Response, error) {
    ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
    defer cancel()
    
    return serviceB.Call(ctx, req)
}
```

**Pattern 2: Circuit Breaker**
```go
type CircuitBreaker struct {
    mu          sync.Mutex
    failures    int
    threshold   int
    state       string // "closed", "open", "half-open"
    lastFailure time.Time
    cooldown    time.Duration
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
    cb.mu.Lock()
    if cb.state == "open" {
        if time.Since(cb.lastFailure) > cb.cooldown {
            cb.state = "half-open"
        } else {
            cb.mu.Unlock()
            return errors.New("circuit breaker open")
        }
    }
    cb.mu.Unlock()
    
    err := fn()
    
    cb.mu.Lock()
    defer cb.mu.Unlock()
    if err != nil {
        cb.failures++
        cb.lastFailure = time.Now()
        if cb.failures >= cb.threshold {
            cb.state = "open"
        }
        return err
    }
    
    cb.failures = 0
    cb.state = "closed"
    return nil
}
```

**Pattern 3: Bulkhead (Concurrency Limiting per Service)**
```go
type ServiceClient struct {
    sem chan struct{}
}

func NewServiceClient(maxConcurrent int) *ServiceClient {
    return &ServiceClient{
        sem: make(chan struct{}, maxConcurrent),
    }
}

func (c *ServiceClient) Call(ctx context.Context, req *Request) (*Response, error) {
    select {
    case c.sem <- struct{}{}:
        defer func() { <-c.sem }()
    case <-ctx.Done():
        return nil, ctx.Err()
    }
    
    return c.doCall(ctx, req)
}
```

This limits how many concurrent calls go to a specific service. If Service C is slow, you don't exhaust all your goroutines calling it.

### Pattern 4: Retry with Backoff

```go
func retryWithBackoff(ctx context.Context, maxRetries int, fn func() error) error {
    var err error
    for i := 0; i < maxRetries; i++ {
        err = fn()
        if err == nil {
            return nil
        }
        
        backoff := time.Duration(1<<uint(i)) * 100 * time.Millisecond // 100ms, 200ms, 400ms...
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(backoff):
        }
    }
    return fmt.Errorf("after %d retries: %w", maxRetries, err)
}
```

### Key Takeaways

- Always set timeouts on service-to-service calls.
- Use circuit breakers to prevent cascade failures.
- Use bulkheads (semaphores) to limit per-service concurrency.
- Implement retry with exponential backoff and context awareness.

---

## Chapter 20: Concurrency in Event Processing

### What Problem Does This Solve?

You receive a stream of events (webhooks, queue messages, real-time data) that need to be processed concurrently. The challenge: maintain ordering guarantees where needed while maximizing throughput.

### Pattern: Ordered Processing by Partition Key

```go
func processEvents(ctx context.Context, events <-chan Event, numWorkers int) {
    // Create per-partition channels
    partitions := make([]chan Event, numWorkers)
    for i := range partitions {
        partitions[i] = make(chan Event, 100)
    }
    
    // Route events to partitions by key
    go func() {
        for event := range events {
            partition := hash(event.Key) % numWorkers
            partitions[partition] <- event
        }
        for _, ch := range partitions {
            close(ch)
        }
    }()
    
    // Each partition is processed in order
    var wg sync.WaitGroup
    for _, ch := range partitions {
        wg.Add(1)
        go func(events <-chan Event) {
            defer wg.Done()
            for event := range events {
                processEvent(event)
            }
        }(ch)
    }
    wg.Wait()
}
```

Events with the same key are always processed by the same worker, guaranteeing order per key while processing different keys in parallel.

### Pattern: Event Batching

```go
func batchProcessor(ctx context.Context, events <-chan Event, batchSize int, flushInterval time.Duration) {
    batch := make([]Event, 0, batchSize)
    ticker := time.NewTicker(flushInterval)
    defer ticker.Stop()
    
    flush := func() {
        if len(batch) == 0 {
            return
        }
        processBatch(batch)
        batch = batch[:0]
    }
    
    for {
        select {
        case <-ctx.Done():
            flush() // Flush remaining
            return
        case event, ok := <-events:
            if !ok {
                flush()
                return
            }
            batch = append(batch, event)
            if len(batch) >= batchSize {
                flush()
            }
        case <-ticker.C:
            flush() // Flush on time even if batch isn't full
        }
    }
}
```

### Key Takeaways

- Partition by key for ordered processing with parallelism.
- Batch events for better throughput on bulk operations.
- Always flush on context cancellation to avoid data loss.

---

## Chapter 21: Concurrency in Kafka Consumers

### What Problem Does This Solve?

Kafka consumers need to process messages concurrently while respecting partition ordering, managing offsets, and handling rebalancing.

### Pattern: Concurrent Consumer with Per-Partition Processing

```go
type KafkaConsumer struct {
    client  sarama.ConsumerGroup
    handler *ConsumerHandler
}

type ConsumerHandler struct {
    workerPool chan struct{}
}

func (h *ConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
    for msg := range claim.Messages() {
        // Process within the partition goroutine for ordering
        if err := processMessage(msg); err != nil {
            log.Printf("failed to process message: %v", err)
            continue
        }
        session.MarkMessage(msg, "") // Commit offset after successful processing
    }
    return nil
}
```

### Pattern: Parallel Processing Within Partitions (When Order Doesn't Matter)

```go
func (h *ConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
    g, ctx := errgroup.WithContext(session.Context())
    g.SetLimit(20) // Max 20 concurrent per partition
    
    for msg := range claim.Messages() {
        msg := msg
        g.Go(func() error {
            if err := processMessage(msg); err != nil {
                return err
            }
            session.MarkMessage(msg, "")
            return nil
        })
    }
    
    return g.Wait()
}
```

### Pattern: Dead Letter Queue

```go
func processWithDLQ(ctx context.Context, msg *sarama.ConsumerMessage, dlqProducer sarama.SyncProducer) error {
    err := processMessage(msg)
    if err != nil {
        // Send to dead letter queue after retries
        dlqMsg := &sarama.ProducerMessage{
            Topic: "my-topic.dlq",
            Key:   sarama.ByteEncoder(msg.Key),
            Value: sarama.ByteEncoder(msg.Value),
            Headers: []sarama.RecordHeader{
                {Key: []byte("original-topic"), Value: []byte(msg.Topic)},
                {Key: []byte("error"), Value: []byte(err.Error())},
            },
        }
        _, _, dlqErr := dlqProducer.SendMessage(dlqMsg)
        if dlqErr != nil {
            return fmt.Errorf("failed to send to DLQ: %w", dlqErr)
        }
    }
    return nil
}
```

### Key Takeaways

- One goroutine per partition preserves message ordering.
- Use errgroup with SetLimit for parallel processing when order doesn't matter.
- Always commit offsets after successful processing.
- Implement dead letter queues for messages that can't be processed.

---

## Chapter 22: Concurrency in Background Jobs

### What Problem Does This Solve?

Background jobs — cron tasks, queue workers, data migrations, cleanup jobs — need concurrency patterns that handle long-running operations, graceful shutdown, and error recovery.

### Pattern: Cron Job with Graceful Shutdown

```go
type Scheduler struct {
    jobs []ScheduledJob
    wg   sync.WaitGroup
}

type ScheduledJob struct {
    Name     string
    Interval time.Duration
    Fn       func(ctx context.Context) error
}

func (s *Scheduler) Start(ctx context.Context) {
    for _, job := range s.jobs {
        s.wg.Add(1)
        go func(job ScheduledJob) {
            defer s.wg.Done()
            ticker := time.NewTicker(job.Interval)
            defer ticker.Stop()
            
            for {
                select {
                case <-ctx.Done():
                    log.Printf("job %s shutting down", job.Name)
                    return
                case <-ticker.C:
                    if err := job.Fn(ctx); err != nil {
                        log.Printf("job %s failed: %v", job.Name, err)
                    }
                }
            }
        }(job)
    }
}

func (s *Scheduler) Wait() {
    s.wg.Wait()
}
```

### Pattern: Batch Processor with Progress Tracking

```go
func processBatch(ctx context.Context, items []Item, concurrency int) error {
    var (
        processed atomic.Int64
        failed    atomic.Int64
        total     = int64(len(items))
    )
    
    // Progress reporter
    go func() {
        ticker := time.NewTicker(5 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                p := processed.Load()
                f := failed.Load()
                log.Printf("Progress: %d/%d (%.1f%%) — %d failed",
                    p, total, float64(p)/float64(total)*100, f)
            }
        }
    }()
    
    g, ctx := errgroup.WithContext(ctx)
    g.SetLimit(concurrency)
    
    for _, item := range items {
        item := item
        g.Go(func() error {
            if err := processItem(ctx, item); err != nil {
                failed.Add(1)
                log.Printf("item %s failed: %v", item.ID, err)
                return nil // Don't stop batch for individual failures
            }
            processed.Add(1)
            return nil
        })
    }
    
    return g.Wait()
}
```

### Pattern: Queue Worker with Shutdown Drain

```go
func startQueueWorker(ctx context.Context, queue Queue, concurrency int) {
    sem := make(chan struct{}, concurrency)
    var wg sync.WaitGroup
    
    for {
        select {
        case <-ctx.Done():
            log.Println("waiting for in-flight jobs to complete...")
            wg.Wait()
            log.Println("all jobs completed, shutting down")
            return
        default:
        }
        
        msg, err := queue.Receive(ctx)
        if err != nil {
            if ctx.Err() != nil {
                wg.Wait()
                return
            }
            continue
        }
        
        sem <- struct{}{}
        wg.Add(1)
        go func(msg Message) {
            defer wg.Done()
            defer func() { <-sem }()
            
            if err := processMessage(msg); err != nil {
                log.Printf("failed: %v", err)
                msg.Nack()
                return
            }
            msg.Ack()
        }(msg)
    }
}
```

### Key Takeaways

- Always support graceful shutdown — drain in-flight work before exiting.
- Track progress for long-running batch jobs.
- Don't stop entire batches for individual item failures.
- Use semaphores to limit concurrent processing.
- Monitor background job health with metrics and heartbeats.

---

# Part V — Production Engineering

---

## Chapter 23: Debugging High CPU

### Symptoms

- CPU usage at 100% on one or more cores
- Service becomes slow but doesn't crash
- Latency increases across all endpoints

### Investigation Playbook

**Step 1: CPU Profile**
```bash
# Collect 30-second CPU profile
curl http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof
```

**Step 2: Analyze in pprof**
```
(pprof) top 20
(pprof) web         # Open flame graph in browser
(pprof) list funcName  # Line-by-line CPU usage for a function
```

**Step 3: Common concurrency-related CPU hogs**

1. **Busy-wait / spin loops**:
   ```go
   // BAD: CPU-burning busy wait
   for !ready {
       // Spins at 100% CPU
   }
   
   // FIX: Use channel or condition variable
   <-readyCh
   ```

2. **Excessive goroutine creation/destruction**:
   - Creating millions of short-lived goroutines causes scheduling overhead
   - Fix: Use worker pools

3. **Lock contention**:
   - Many goroutines fighting for the same mutex
   - CPU shows high time in `runtime.mutex*` functions
   - Fix: Reduce critical section size, use RWMutex, or shard data

4. **GC pressure from goroutines**:
   - Too many goroutines creating too much garbage
   - GC runs frequently, consuming CPU
   - Fix: Pool objects with `sync.Pool`, reduce allocations

### Key Commands

```bash
# Live CPU profile in terminal
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/profile?seconds=30

# Compare two profiles
go tool pprof -base before.prof after.prof
```

---

## Chapter 24: Debugging High Memory Usage

### Symptoms

- RSS keeps growing
- OOM kills in Kubernetes
- GC pauses increasing

### Investigation Playbook

**Step 1: Heap Profile**
```bash
curl http://localhost:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof
```

**Step 2: Check goroutine count**
```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

Each goroutine consumes at least ~2KB. 1 million leaked goroutines = ~2GB.

**Step 3: Common causes**

1. **Goroutine leaks** (most common): Goroutine count keeps climbing.
2. **Unbounded buffers**: Channel buffers or slices that grow without limit.
3. **Large per-goroutine allocations**: Each goroutine allocating large buffers.
4. **Retained references**: Goroutines holding references to large objects, preventing GC.

**Step 4: In pprof**
```
(pprof) top          # Biggest allocators
(pprof) inuse_space  # Currently held memory
(pprof) alloc_space  # Total allocated (includes freed)
```

### Goroutine Memory Quick Check

```go
func logMemStats() {
    var m runtime.MemStats
    runtime.ReadMemStats(&m)
    log.Printf("Goroutines: %d, HeapAlloc: %d MB, HeapSys: %d MB",
        runtime.NumGoroutine(),
        m.HeapAlloc/1024/1024,
        m.HeapSys/1024/1024)
}
```

---

## Chapter 25: Debugging Goroutine Leaks

### Investigation Playbook

**Step 1: Confirm goroutine growth**
```bash
# Check goroutine count over time
watch -n 5 'curl -s http://localhost:6060/debug/pprof/goroutine?debug=0 | head -1'
```

**Step 2: Identify leaked goroutines**
```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=1
```

This shows goroutine counts grouped by stack trace. Look for functions with unexpectedly high counts.

Example output:
```
50 @ runtime.gopark, runtime.chanrecv, main.processOrder
```
This means 50 goroutines are stuck in `chanrecv` inside `processOrder`. That's your leak.

**Step 3: Full stack traces**
```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=2
```

**Step 4: Common fixes**
- Add `ctx.Done()` checks
- Add timeouts on channel operations
- Use buffered channels for fire-and-forget patterns
- Set HTTP client timeouts

### Automated Detection in Tests

```go
func TestNoGoroutineLeaks(t *testing.T) {
    defer goleak.VerifyNone(t, goleak.IgnoreTopFunction("...")))
    
    // Your test code
}
```

---

## Chapter 26: Debugging Deadlocks

### Investigation Playbook

**Step 1: Get goroutine dump**
```bash
curl http://localhost:6060/debug/pprof/goroutine?debug=2 > goroutines.txt
```

**Step 2: Search for blocked goroutines**
Look for these states:
- `semacquire` — waiting for a mutex
- `chanrecv` — waiting for a channel receive
- `chansend` — waiting for a channel send
- `select` — blocked in select

**Step 3: Trace the dependency chain**
Find goroutine A waiting for a lock held by goroutine B, which is waiting for a lock held by goroutine A.

**Step 4: Send SIGQUIT for goroutine dump**
If pprof isn't available:
```bash
kill -QUIT <pid>  # Prints all goroutine stacks to stderr
```

### Prevention Strategies in Code Review

- Every Lock must have a matching Unlock (prefer `defer`)
- Document lock ordering when multiple locks exist
- Never hold a lock while doing I/O
- Use context timeouts on all blocking operations

---

## Chapter 27: Debugging Lock Contention

### Symptoms

- High CPU but low throughput
- Latency spikes under load
- pprof shows time spent in `sync.(*Mutex).Lock`

### Investigation Playbook

**Step 1: Mutex profile**
```go
runtime.SetMutexProfileFraction(5) // Enable mutex profiling (1 in 5 contention events)
```

```bash
curl http://localhost:6060/debug/pprof/mutex > mutex.prof
go tool pprof mutex.prof
```

**Step 2: Block profile**
```go
runtime.SetBlockProfileRate(1) // Enable block profiling
```

```bash
curl http://localhost:6060/debug/pprof/block > block.prof
go tool pprof block.prof
```

**Step 3: Common fixes**

1. **Reduce critical section size**: Do less work while holding the lock.
2. **Switch to RWMutex**: If reads >> writes.
3. **Shard the data**: Instead of one map with one lock, use N maps with N locks.
4. **Use atomic operations**: For simple counters.
5. **Use lock-free data structures**: For specific use cases.

### Sharding Example

```go
const numShards = 32

type ShardedMap struct {
    shards [numShards]struct {
        sync.RWMutex
        data map[string]string
    }
}

func (sm *ShardedMap) getShard(key string) *struct {
    sync.RWMutex
    data map[string]string
} {
    hash := fnv.New32a()
    hash.Write([]byte(key))
    return &sm.shards[hash.Sum32()%numShards]
}

func (sm *ShardedMap) Get(key string) string {
    shard := sm.getShard(key)
    shard.RLock()
    defer shard.RUnlock()
    return shard.data[key]
}

func (sm *ShardedMap) Set(key, value string) {
    shard := sm.getShard(key)
    shard.Lock()
    defer shard.Unlock()
    shard.data[key] = value
}
```

---

## Chapter 28: Debugging Slow APIs

### Investigation Playbook

**Step 1: Is it CPU-bound or I/O-bound?**
- High CPU → CPU profile
- Low CPU but slow → I/O waiting (DB, network, locks)

**Step 2: Trace a request**
```go
import "go.opentelemetry.io/otel/trace"

func handler(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.Start(r.Context(), "handler")
    defer span.End()
    
    ctx2, span2 := tracer.Start(ctx, "db-query")
    result := db.Query(ctx2, query)
    span2.End()
    
    ctx3, span3 := tracer.Start(ctx, "serialize")
    data := serialize(result)
    span3.End()
}
```

**Step 3: Common concurrency-related causes**
- **Lock contention**: Check mutex profile
- **Goroutine starvation**: Too many goroutines fighting for few CPUs
- **Inefficient fan-out**: Spawning too many goroutines for small tasks
- **Missing parallel fetching**: Sequential calls to independent services
- **Channel bottleneck**: Single channel with too many producers
- **Missing connection pooling**: Creating new DB/HTTP connections per request

### Quick Wins

```go
// BAD: Sequential service calls (600ms total)
user := fetchUser(ctx)       // 200ms
orders := fetchOrders(ctx)   // 200ms
prefs := fetchPrefs(ctx)     // 200ms

// GOOD: Parallel service calls (200ms total)
g, ctx := errgroup.WithContext(ctx)
var user *User; var orders []Order; var prefs *Prefs
g.Go(func() error { user, _ = fetchUser(ctx); return nil })
g.Go(func() error { orders, _ = fetchOrders(ctx); return nil })
g.Go(func() error { prefs, _ = fetchPrefs(ctx); return nil })
g.Wait()
```

---

## Chapter 29: Throughput Bottlenecks

### Common Bottlenecks and Fixes

| Bottleneck | Detection | Fix |
|------------|-----------|-----|
| Single-threaded processing | Low CPU usage, high latency | Add worker pool |
| Lock contention | High CPU in mutex functions | Shard data, use RWMutex |
| Connection pool exhaustion | Goroutines waiting for connections | Increase pool size, add timeouts |
| Channel bottleneck | Goroutines blocked on send | Increase buffer, add more consumers |
| GC pressure | Frequent GC pauses | Reduce allocations, use sync.Pool |
| GOMAXPROCS too low | Underutilized cores | Set to number of available cores |

### Connection Pool Sizing

```go
db, _ := sql.Open("postgres", connString)
db.SetMaxOpenConns(25)              // Max open connections
db.SetMaxIdleConns(25)              // Keep connections ready
db.SetConnMaxLifetime(5 * time.Minute) // Recycle connections
db.SetConnMaxIdleTime(1 * time.Minute)
```

**Rule of thumb**: `MaxOpenConns` ≈ number of concurrent queries you expect. Too few: goroutines wait for connections. Too many: overwhelm the database.

---

## Chapter 30: pprof

### Setup

```go
import _ "net/http/pprof"

func main() {
    go func() {
        log.Println(http.ListenAndServe(":6060", nil))
    }()
    // ... rest of your app
}
```

### Essential Profiles

| Profile | URL | What It Shows |
|---------|-----|---------------|
| CPU | `/debug/pprof/profile?seconds=30` | Where CPU time is spent |
| Heap | `/debug/pprof/heap` | Memory allocation |
| Goroutine | `/debug/pprof/goroutine?debug=1` | All goroutines and their state |
| Block | `/debug/pprof/block` | Where goroutines block |
| Mutex | `/debug/pprof/mutex` | Mutex contention |
| Trace | `/debug/pprof/trace?seconds=5` | Execution trace |

### Analyzing Profiles

```bash
# Interactive CLI
go tool pprof http://localhost:6060/debug/pprof/heap

# Web UI (recommended)
go tool pprof -http=:8080 http://localhost:6060/debug/pprof/heap

# Compare profiles (find regressions)
go tool pprof -base before.prof after.prof
```

### pprof Commands Cheat Sheet

```
top 20          # Top 20 functions by resource usage
list funcName   # Source-level breakdown
web             # Open flame graph
cum             # Sort by cumulative (includes callees)
flat            # Sort by self (excludes callees)
```

---

## Chapter 31: go tool trace

### What It Shows

`go tool trace` gives you a **timeline view** of goroutine execution — when they run, when they block, when they're scheduled. It's like an X-ray of your program's concurrent behavior.

### Collecting a Trace

```go
import "runtime/trace"

func main() {
    f, _ := os.Create("trace.out")
    trace.Start(f)
    defer trace.Stop()
    
    // ... your program
}
```

Or from pprof:
```bash
curl http://localhost:6060/debug/pprof/trace?seconds=5 > trace.out
go tool trace trace.out
```

### What to Look For

1. **Goroutine analysis**: Which goroutines are running vs waiting
2. **Network blocking**: Time spent waiting for network I/O
3. **Sync blocking**: Time spent waiting for locks
4. **Scheduler latency**: Time between a goroutine becoming runnable and actually running
5. **GC pauses**: Duration and frequency of garbage collection

### When to Use

- pprof tells you **where** time is spent
- trace tells you **when** things happen and **why** goroutines are blocked

Use trace when you need to understand timing and scheduling behavior, not just aggregate CPU/memory.

---

## Chapter 32: Metrics To Watch

### Essential Concurrency Metrics

| Metric | What to Watch | Danger Sign |
|--------|---------------|-------------|
| `runtime.NumGoroutine()` | Goroutine count | Steadily increasing = leak |
| `runtime.MemStats.HeapAlloc` | Heap memory | Growing without bound |
| `runtime.MemStats.NumGC` | GC runs | Too frequent = allocation pressure |
| `runtime.MemStats.PauseTotalNs` | GC pause time | High = stop-the-world delays |
| Channel buffer length | `len(ch)` | Consistently at capacity = backpressure issue |
| Connection pool wait time | DB/HTTP pool stats | High = pool exhaustion |
| Request latency p99 | HTTP metrics | Spikes = contention or blocking |

### Prometheus Setup

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    goroutineGauge = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
        Name: "app_goroutines_total",
        Help: "Current number of goroutines",
    }, func() float64 {
        return float64(runtime.NumGoroutine())
    })
    
    channelDepth = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "app_job_queue_depth",
        Help: "Number of jobs waiting in queue",
    })
    
    workerBusy = prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "app_workers_busy",
        Help: "Number of busy workers",
    })
)
```

### Alerting Rules

- **Goroutine count > 100K**: Possible leak, investigate immediately.
- **Goroutine count steadily increasing**: Definite leak, page on-call.
- **p99 latency > 5x p50**: Contention or blocking issue.
- **Queue depth consistently at capacity**: Consumer can't keep up.
- **GC pause > 50ms**: Consider reducing allocation rate.

---

# Part VI — Interview Masterclass

---

## 100 Junior-Level Questions

Each question includes: **Answer**, **Why interviewers ask it**, **Common wrong answer**, and **Follow-up question**.

---

**1. What is a goroutine?**
- **Answer**: A lightweight thread of execution managed by the Go runtime. Created with the `go` keyword.
- **Why asked**: Foundation check.
- **Wrong answer**: "It's a thread." (It's multiplexed onto OS threads, not a 1:1 mapping.)
- **Follow-up**: How much memory does a goroutine use initially?

**2. How do you create a goroutine?**
- **Answer**: `go functionName()` or `go func() { ... }()`
- **Why asked**: Basic syntax.
- **Wrong answer**: Using `thread.Start()` or similar.
- **Follow-up**: Does the calling function wait for the goroutine to finish?

**3. What happens when `main()` returns and goroutines are still running?**
- **Answer**: The program exits immediately. All running goroutines are terminated.
- **Why asked**: Tests understanding of goroutine lifecycle.
- **Wrong answer**: "The program waits for all goroutines."
- **Follow-up**: How do you prevent main from exiting too early?

**4. What is a channel?**
- **Answer**: A typed conduit for communication between goroutines. Channels allow goroutines to send and receive values safely.
- **Why asked**: Core primitive knowledge.
- **Wrong answer**: "It's like a variable shared between threads."
- **Follow-up**: What types can a channel carry?

**5. What is the difference between a buffered and unbuffered channel?**
- **Answer**: Unbuffered channels block the sender until a receiver is ready (and vice versa). Buffered channels only block the sender when the buffer is full.
- **Why asked**: Fundamental channel semantics.
- **Wrong answer**: "Buffered is always faster."
- **Follow-up**: When would you use each?

**6. How do you create a buffered channel?**
- **Answer**: `ch := make(chan int, 10)` — the second argument is the buffer size.
- **Why asked**: Syntax check.
- **Wrong answer**: `ch := make(chan int)` (this is unbuffered).
- **Follow-up**: What happens when a buffered channel is full?

**7. What does `<-ch` do?**
- **Answer**: Receives a value from channel `ch`. It blocks until a value is available.
- **Why asked**: Channel syntax.
- **Wrong answer**: "It sends to the channel."
- **Follow-up**: How do you check if the channel is closed while receiving?

**8. What does `ch <- value` do?**
- **Answer**: Sends `value` to channel `ch`. Blocks if the channel is unbuffered and no receiver is ready, or if the buffer is full.
- **Why asked**: Channel syntax.
- **Wrong answer**: Confusing send and receive operators.
- **Follow-up**: What happens if you send on a closed channel?

**9. What happens when you send on a closed channel?**
- **Answer**: It panics with `send on closed channel`.
- **Why asked**: Channel safety.
- **Wrong answer**: "It returns an error" or "It's a no-op."
- **Follow-up**: How do you prevent this?

**10. What happens when you receive from a closed channel?**
- **Answer**: It returns the zero value of the channel's type immediately, without blocking.
- **Why asked**: Channel lifecycle understanding.
- **Wrong answer**: "It panics."
- **Follow-up**: How can you tell if the channel was closed vs. a zero value was sent?

**11. How do you check if a channel is closed?**
- **Answer**: `val, ok := <-ch` — if `ok` is `false`, the channel is closed and empty.
- **Why asked**: Proper channel usage.
- **Wrong answer**: "Check `ch == nil`."
- **Follow-up**: Can you close a channel twice?

**12. What is `sync.WaitGroup`?**
- **Answer**: A synchronization primitive that waits for a collection of goroutines to finish. Uses `Add()`, `Done()`, and `Wait()`.
- **Why asked**: Basic sync knowledge.
- **Wrong answer**: "It limits concurrency."
- **Follow-up**: Where should `Add()` be called — inside or outside the goroutine?

**13. What is `sync.Mutex`?**
- **Answer**: A mutual exclusion lock. Only one goroutine can hold the lock at a time, preventing concurrent access to shared data.
- **Why asked**: Basic sync knowledge.
- **Wrong answer**: "It's like a channel."
- **Follow-up**: What happens if you call Lock() twice from the same goroutine?

**14. What is a race condition?**
- **Answer**: When two or more goroutines access shared data concurrently, and at least one modifies it, without synchronization.
- **Why asked**: Fundamental concurrency concept.
- **Wrong answer**: "When goroutines run in the wrong order."
- **Follow-up**: How do you detect race conditions in Go?

**15. How do you detect race conditions in Go?**
- **Answer**: Use the race detector: `go run -race main.go` or `go test -race ./...`
- **Why asked**: Practical tooling knowledge.
- **Wrong answer**: "Write more unit tests."
- **Follow-up**: Does the race detector catch all race conditions?

**16. What is a deadlock?**
- **Answer**: A situation where two or more goroutines are waiting for each other and none can proceed.
- **Why asked**: Core concurrency concept.
- **Wrong answer**: "When a goroutine crashes."
- **Follow-up**: How does Go detect deadlocks?

**17. What does `select` do?**
- **Answer**: Waits on multiple channel operations and executes the case for whichever channel is ready first.
- **Why asked**: Core primitive.
- **Wrong answer**: "It's like a switch statement for values."
- **Follow-up**: What happens when multiple cases are ready?

**18. What happens when multiple `select` cases are ready?**
- **Answer**: Go randomly selects one. This prevents starvation.
- **Why asked**: Select semantics.
- **Wrong answer**: "The first one is selected."
- **Follow-up**: Why is random selection important?

**19. What does the `default` case in `select` do?**
- **Answer**: Makes the select non-blocking. If no channel is ready, the default case executes immediately.
- **Why asked**: Non-blocking patterns.
- **Wrong answer**: "It catches errors."
- **Follow-up**: When is a non-blocking select useful?

**20. What is `context.Context`?**
- **Answer**: An interface that carries deadlines, cancellation signals, and request-scoped values across API boundaries and goroutines.
- **Why asked**: Essential for production Go.
- **Wrong answer**: "It's for passing function parameters."
- **Follow-up**: Where does context come from in an HTTP handler?

**21. What is `GOMAXPROCS`?**
- **Answer**: Controls the number of OS threads that can execute goroutines simultaneously. Default is the number of CPU cores.
- **Why asked**: Runtime configuration knowledge.
- **Wrong answer**: "It controls the number of goroutines."
- **Follow-up**: Should you change GOMAXPROCS in production?

**22. Can goroutines access the same variable safely?**
- **Answer**: No, not without synchronization. Concurrent access where at least one is a write causes a data race.
- **Why asked**: Race condition understanding.
- **Wrong answer**: "Yes, Go handles it automatically."
- **Follow-up**: What tools does Go provide for safe concurrent access?

**23. What does `defer` do?**
- **Answer**: Schedules a function call to execute when the surrounding function returns. Commonly used for cleanup (closing files, unlocking mutexes).
- **Why asked**: Essential Go idiom.
- **Wrong answer**: "It runs the function in a goroutine."
- **Follow-up**: In what order do multiple defers execute? (LIFO)

**24. Why is `defer mu.Unlock()` important?**
- **Answer**: It guarantees the mutex is unlocked even if the function panics, preventing deadlocks.
- **Why asked**: Best practices.
- **Wrong answer**: "It's just a style preference."
- **Follow-up**: What happens if you forget to unlock a mutex?

**25. What is a nil channel?**
- **Answer**: A channel with zero value (`var ch chan int`). Sending to or receiving from a nil channel blocks forever.
- **Why asked**: Edge case knowledge.
- **Wrong answer**: "It causes a panic."
- **Follow-up**: When would a nil channel be useful?

**26. How do you range over a channel?**
- **Answer**: `for val := range ch { ... }` — iterates until the channel is closed.
- **Why asked**: Channel iteration pattern.
- **Wrong answer**: "You use `len(ch)` to know when to stop."
- **Follow-up**: What happens if the channel is never closed?

**27. What is `sync.Once`?**
- **Answer**: Ensures a function is called exactly once, even from multiple goroutines.
- **Why asked**: Initialization patterns.
- **Wrong answer**: "It runs the function once per goroutine."
- **Follow-up**: What happens if the function passed to `Once.Do` panics?

**28. What are atomic operations?**
- **Answer**: Lock-free operations on single variables (integers, pointers) that are safe for concurrent use without mutexes.
- **Why asked**: Performance optimization knowledge.
- **Wrong answer**: "They're the same as mutex locks."
- **Follow-up**: When would you use atomic vs mutex?

**29. What is the difference between concurrency and parallelism?**
- **Answer**: Concurrency is about structuring a program to handle multiple things. Parallelism is about executing multiple things simultaneously.
- **Why asked**: Foundational concept.
- **Wrong answer**: "They're the same."
- **Follow-up**: Can you have concurrency without parallelism?

**30. What is `time.After` and how is it used with select?**
- **Answer**: Returns a channel that sends after a duration. Used in select for timeouts: `case <-time.After(5*time.Second):`.
- **Why asked**: Timeout patterns.
- **Wrong answer**: "It stops the goroutine."
- **Follow-up**: What's the problem with using `time.After` in a loop?

**31. What is `sync.RWMutex`?**
- **Answer**: A mutex that allows multiple concurrent readers or one exclusive writer.
- **Why asked**: Optimization knowledge.
- **Wrong answer**: "It's the same as Mutex but reentrant."
- **Follow-up**: When is RWMutex slower than Mutex?

**32. How do you stop a goroutine?**
- **Answer**: Using `context.Context` cancellation, closing a done channel, or closing the work channel.
- **Why asked**: Goroutine lifecycle management.
- **Wrong answer**: "Call `goroutine.Stop()`." (No such function exists.)
- **Follow-up**: What happens if you don't stop a goroutine?

**33. What is a goroutine leak?**
- **Answer**: A goroutine that blocks forever and never exits, consuming memory.
- **Why asked**: Production readiness.
- **Wrong answer**: "When too many goroutines run."
- **Follow-up**: How do you detect goroutine leaks?

**34. What does `close(ch)` do?**
- **Answer**: Closes the channel. No more values can be sent. Receivers get remaining values, then zero values with `ok=false`.
- **Why asked**: Channel lifecycle.
- **Wrong answer**: "It deletes the channel."
- **Follow-up**: Who should close a channel — sender or receiver?

**35. Why should only the sender close a channel?**
- **Answer**: Because sending on a closed channel panics. If the receiver closes it, the sender might still try to send.
- **Why asked**: Channel safety patterns.
- **Wrong answer**: "Either can close it."
- **Follow-up**: Is it always necessary to close a channel?

**36. What is `context.Background()`?**
- **Answer**: Returns a non-nil, empty context. Used as the top-level context (root of context tree).
- **Why asked**: Context usage.
- **Wrong answer**: "It creates a cancelable context."
- **Follow-up**: When do you use `context.TODO()` vs `context.Background()`?

**37. What is `context.WithCancel`?**
- **Answer**: Creates a derived context with a cancel function. Calling cancel stops all operations using that context.
- **Why asked**: Cancellation patterns.
- **Wrong answer**: "It creates a timeout."
- **Follow-up**: What happens if you don't call the cancel function?

**38. What is `context.WithTimeout`?**
- **Answer**: Creates a derived context that automatically cancels after a specified duration.
- **Why asked**: Timeout patterns.
- **Wrong answer**: "It makes the function sleep."
- **Follow-up**: Should you still call cancel() on a timeout context?

**39. What happens if you read from an empty open channel?**
- **Answer**: The goroutine blocks until a value is sent on the channel.
- **Why asked**: Channel blocking behavior.
- **Wrong answer**: "It returns zero value."
- **Follow-up**: How is this different from reading from a closed channel?

**40. What is the `go` keyword?**
- **Answer**: Starts a new goroutine that executes the specified function concurrently.
- **Why asked**: Basic syntax.
- **Wrong answer**: "It imports a package."
- **Follow-up**: Does the current goroutine wait for the new one?

**41. Can you return a value from a goroutine?**
- **Answer**: Not directly. You use channels, closures with shared variables (with sync), or callbacks to get results.
- **Why asked**: Goroutine communication.
- **Wrong answer**: "Yes, use `return`." (The return value is discarded.)
- **Follow-up**: What's the most idiomatic way?

**42. What is a channel direction?**
- **Answer**: Restricting a channel to send-only (`chan<- T`) or receive-only (`<-chan T`) in function signatures.
- **Why asked**: Type safety.
- **Wrong answer**: "It changes how the channel works."
- **Follow-up**: Why use directional channels?

**43. What does `len(ch)` return for a channel?**
- **Answer**: The number of elements currently in the channel's buffer.
- **Why asked**: Channel introspection.
- **Wrong answer**: "The capacity."
- **Follow-up**: What does `cap(ch)` return?

**44. What is `runtime.Gosched()`?**
- **Answer**: Yields the processor, allowing other goroutines to run. Rarely needed in modern Go.
- **Why asked**: Scheduler knowledge.
- **Wrong answer**: "It stops the goroutine."
- **Follow-up**: Why is it rarely needed since Go 1.14?

**45. Can maps be used concurrently in Go?**
- **Answer**: No. Concurrent reads are safe, but concurrent read+write or write+write causes a fatal crash.
- **Why asked**: Common bug source.
- **Wrong answer**: "Yes, maps are thread-safe."
- **Follow-up**: How do you make a map safe for concurrent use?

**46. What is `sync.Map`?**
- **Answer**: A concurrent-safe map from the sync package. Optimized for cases where keys are mostly read or each key is written by one goroutine.
- **Why asked**: Alternative to mutex-protected maps.
- **Wrong answer**: "It's always better than a regular map with mutex."
- **Follow-up**: When should you use `sync.Map` vs a regular map with `sync.Mutex`?

**47. What is `select{}` (empty select)?**
- **Answer**: Blocks the goroutine forever. Sometimes used to keep main alive.
- **Why asked**: Select edge case.
- **Wrong answer**: "It does nothing and continues."
- **Follow-up**: When would you use this?

**48. How do you pass data between goroutines?**
- **Answer**: Channels (preferred), shared variables with mutexes, or atomic operations.
- **Why asked**: Communication patterns.
- **Wrong answer**: "Global variables."
- **Follow-up**: What does Go's proverb say about this?

**49. What does `wg.Add(1)` do?**
- **Answer**: Increments the WaitGroup counter by 1. Each goroutine that needs to be waited for should have a corresponding Add.
- **Why asked**: WaitGroup usage.
- **Wrong answer**: "It creates a goroutine."
- **Follow-up**: What happens if Add is called after Wait?

**50. What is the zero value of a channel?**
- **Answer**: `nil`. A nil channel blocks forever on send and receive.
- **Why asked**: Zero value behavior.
- **Wrong answer**: "An empty channel with no buffer."
- **Follow-up**: What happens if you close a nil channel?

**51-100: Additional Junior Questions (Condensed)**

**51. What is `time.NewTicker`?** — Returns a ticker that sends on its channel at regular intervals. Must call `Stop()` to release resources.

**52. How do you create an unbuffered channel?** — `make(chan Type)` without a size argument.

**53. What is a data race?** — Unsynchronized concurrent access to shared memory where at least one access is a write.

**54. What does `runtime.NumGoroutine()` return?** — The number of currently active goroutines.

**55. Can you send a struct on a channel?** — Yes, channels can carry any type including structs, pointers, slices, maps, and functions.

**56. What is `chan struct{}`?** — A signal-only channel with zero-size elements. Used for signaling without data.

**57. What happens if you call `wg.Done()` more times than `wg.Add()`?** — Panic: negative WaitGroup counter.

**58. What is `context.WithValue`?** — Creates a context carrying a key-value pair for request-scoped data.

**59. Should you store context in a struct field?** — No. Pass it as the first parameter to functions.

**60. What is the difference between `Lock()` and `RLock()`?** — `Lock()` is exclusive (write). `RLock()` is shared (read) — multiple goroutines can hold RLock simultaneously.

**61. What is `context.TODO()`?** — A placeholder context for when you're unsure which context to use. Functionally identical to Background.

**62. Can multiple goroutines read from the same channel?** — Yes. Only one will receive each value. This is a fan-out pattern.

**63. Can multiple goroutines write to the same channel?** — Yes. This is safe. It's a fan-in pattern.

**64. What is `runtime.GOMAXPROCS(0)`?** — Returns the current GOMAXPROCS value without changing it.

**65. What is a panic in Go?** — A runtime error that unwinds the stack. Can be caught with `recover()` in a deferred function.

**66. Can a goroutine recover from a panic in another goroutine?** — No. Panics only propagate within the goroutine where they occur.

**67. What is `sync.Pool`?** — A pool of temporary objects for reuse, reducing garbage collection pressure.

**68. How do you implement a timeout for a function call?** — Use `context.WithTimeout` and check `ctx.Done()`.

**69. What is the difference between `context.WithTimeout` and `context.WithDeadline`?** — `WithTimeout(5s)` is sugar for `WithDeadline(now + 5s)`. They do the same thing.

**70. Can you close a receive-only channel (`<-chan T`)?** — No. The compiler won't allow it.

**71. What does `cap(ch)` return?** — The buffer capacity of the channel.

**72. What is fan-out?** — Distributing work from one source to multiple goroutines.

**73. What is fan-in?** — Collecting results from multiple goroutines into one channel.

**74. What is a worker pool?** — A fixed number of goroutines processing jobs from a shared channel.

**75. Why are goroutines cheap?** — Small initial stack (~2KB), userspace scheduling, and no kernel involvement for context switches.

**76. What is preemptive scheduling?** — The scheduler can pause a goroutine even if it hasn't voluntarily yielded. Available since Go 1.14.

**77. What is cooperative scheduling?** — Goroutines voluntarily yield at certain points (channel ops, function calls). Go used this before 1.14.

**78. What does `runtime.Goexit()` do?** — Terminates the calling goroutine (after running deferred functions) without affecting others.

**79. Can you resize a channel buffer after creation?** — No. Channel buffer size is fixed at creation.

**80. What is `errgroup`?** — A package (`golang.org/x/sync/errgroup`) for managing groups of goroutines with error propagation.

**81. What is `singleflight`?** — A package that deduplicates concurrent identical function calls so only one executes.

**82. How do goroutines communicate?** — Through channels (message passing) or shared memory with synchronization.

**83. What is the Go concurrency proverb?** — "Do not communicate by sharing memory; instead, share memory by communicating."

**84. What is `time.Sleep` and should you use it for synchronization?** — It pauses the goroutine. No — use proper synchronization (channels, WaitGroup).

**85. What is a semaphore in Go?** — Typically implemented as a buffered channel. Controls how many goroutines can access a resource.

**86. What does `make(chan int, 1)` create?** — A buffered channel with capacity 1. Useful for signaling or latest-value patterns.

**87. What happens if you write to a nil channel?** — The goroutine blocks forever.

**88. What is `context.Err()`?** — Returns `nil` if not cancelled, `context.Canceled` if cancelled, or `context.DeadlineExceeded` if timed out.

**89. Can you use a for loop without range on a channel?** — Yes: `for { val := <-ch }` but you won't know when the channel closes without checking `ok`.

**90. What is `runtime.LockOSThread()`?** — Pins the current goroutine to its current OS thread. Used for thread-local state (e.g., UI frameworks, CGo).

**91. What is a pipeline in Go?** — A series of stages where each stage is a goroutine reading from an input channel and writing to an output channel.

**92. What is `sync.WaitGroup.Add(-1)` equivalent to?** — `sync.WaitGroup.Done()`.

**93. Can channels be passed as function arguments?** — Yes. They're first-class values.

**94. What type is `<-chan int`?** — A receive-only channel of integers.

**95. What type is `chan<- int`?** — A send-only channel of integers.

**96. What is the difference between `go func(){}()` and `go func(){}`?** — The first immediately calls the anonymous function as a goroutine. The second is a syntax error (missing `()`).

**97. How does Go's scheduler differ from the OS scheduler?** — Go's scheduler runs in userspace, switches goroutines at known safe points, and manages thousands of goroutines on few OS threads.

**98. What is `runtime.ReadMemStats`?** — Reads memory allocator statistics into a MemStats struct for monitoring.

**99. Can you compare channels?** — Yes, channels support `==` and `!=` comparison (identity check).

**100. What is the maximum number of goroutines you can create?** — There's no hard limit beyond available memory. Millions are practical.

---

## 100 Mid-Level Questions

---

**1. Explain the G-M-P scheduling model.**
- **Answer**: G = goroutine (work), M = OS thread (executor), P = processor (execution context with local run queue). GOMAXPROCS controls the P count. Each P runs one G at a time on its M.
- **Why asked**: Tests deep scheduling knowledge.
- **Wrong answer**: "Goroutines run directly on OS threads."
- **Follow-up**: What is work stealing?

**2. What is work stealing and why does Go use it?**
- **Answer**: When a P's local queue is empty, it steals goroutines from another P's queue. This balances load across processors without central coordination.
- **Why asked**: Scheduling efficiency.
- **Wrong answer**: "The global queue distributes all work."
- **Follow-up**: What's the performance benefit of local queues?

**3. How would you implement a timeout pattern with channels?**
- **Answer**: Use `select` with `time.After` or `context.WithTimeout` with `ctx.Done()`.
- **Why asked**: Practical pattern.
- **Wrong answer**: Using `time.Sleep` before checking.
- **Follow-up**: What's wrong with `time.After` in a loop?

**4. What's the difference between `context.WithCancel` and `context.WithTimeout`?**
- **Answer**: WithCancel requires manual cancellation. WithTimeout cancels automatically after a duration (but should still have `defer cancel()`).
- **Why asked**: Context nuances.
- **Wrong answer**: "WithTimeout doesn't need cancel()."
- **Follow-up**: When would you use each?

**5. How do you implement a semaphore in Go?**
- **Answer**: Use a buffered channel: `sem := make(chan struct{}, N)`. Acquire with `sem <- struct{}{}`, release with `<-sem`.
- **Why asked**: Concurrency limiting.
- **Wrong answer**: Using a counter with a mutex (works but not idiomatic).
- **Follow-up**: How does this compare to `golang.org/x/sync/semaphore`?

**6. What happens when you close a channel that's being ranged over?**
- **Answer**: The range loop receives all remaining values and then exits.
- **Why asked**: Channel lifecycle understanding.
- **Wrong answer**: "It panics."
- **Follow-up**: What happens if you never close it?

**7. How do you prevent goroutine leaks?**
- **Answer**: Every goroutine needs an exit path: context cancellation, channel closure, or a done signal. Use goleak in tests.
- **Why asked**: Production readiness.
- **Wrong answer**: "Go's GC handles it."
- **Follow-up**: Give an example of a common leak pattern.

**8. When should you use a mutex vs a channel?**
- **Answer**: Mutex for protecting shared state (caches, counters). Channel for communication and coordination between goroutines.
- **Why asked**: Design decisions.
- **Wrong answer**: "Always use channels."
- **Follow-up**: Give an example where mutex is clearly better.

**9. What is `errgroup` and how does it differ from `sync.WaitGroup`?**
- **Answer**: errgroup provides WaitGroup functionality plus error propagation and context cancellation. If any goroutine returns an error, the context is cancelled.
- **Why asked**: Modern Go patterns.
- **Wrong answer**: "They're the same thing."
- **Follow-up**: What does `errgroup.SetLimit` do?

**10. Explain the channel ownership pattern.**
- **Answer**: The function that creates a channel should also own closing it. Return a read-only channel to callers. This prevents double-close and send-on-closed panics.
- **Why asked**: Code safety patterns.
- **Wrong answer**: "Anyone can close a channel."
- **Follow-up**: Show an example.

**11. What is `singleflight` and when would you use it?**
- **Answer**: Deduplicates concurrent identical function calls. Use for expensive operations (DB queries, API calls) that many goroutines request simultaneously.
- **Why asked**: Performance optimization.
- **Wrong answer**: "It's a caching library."
- **Follow-up**: How is it different from caching?

**12. How does the Go runtime handle a goroutine blocking on a syscall?**
- **Answer**: The M (OS thread) goes into the kernel. The P detaches and finds another M to continue running goroutines.
- **Why asked**: Scheduler mechanics.
- **Wrong answer**: "The goroutine is moved to a blocking queue."
- **Follow-up**: How is this different from blocking on a channel?

**13. What is `sync.Cond` and when would you use it?**
- **Answer**: A condition variable for waiting on a condition with `Wait()`, signaling with `Signal()` or `Broadcast()`. Use when channels would be awkward for broadcasting.
- **Why asked**: Advanced sync knowledge.
- **Wrong answer**: "Always use channels instead."
- **Follow-up**: Why is it rarely used in practice?

**14. How do you implement a fan-in pattern?**
- **Answer**: Multiple goroutines read from input channels and send to a single merged output channel. Use WaitGroup to close the output when all inputs are done.
- **Why asked**: Concurrency patterns.
- **Wrong answer**: "Just read from multiple channels sequentially."
- **Follow-up**: How do you ensure the merged channel closes properly?

**15. What is the difference between `time.After` and `time.NewTimer`?**
- **Answer**: `time.After` returns a channel and can't be stopped (leaks in loops). `time.NewTimer` can be stopped and reset.
- **Why asked**: Resource management.
- **Wrong answer**: "They're interchangeable."
- **Follow-up**: Show the memory leak with `time.After` in a loop.

**16. How do you detect goroutine leaks in production?**
- **Answer**: Monitor `runtime.NumGoroutine()`, use pprof goroutine endpoint, set up Prometheus alerts.
- **Why asked**: Production debugging.
- **Wrong answer**: "Check log files."
- **Follow-up**: How do you identify which goroutines are leaked?

**17. What is backpressure and how do you implement it in Go?**
- **Answer**: Mechanism for a consumer to slow down a producer. Implement with bounded channels (sender blocks when full), rate limiting, or dropping messages.
- **Why asked**: System design.
- **Wrong answer**: "Add a bigger buffer."
- **Follow-up**: What happens without backpressure?

**18. How does `context.WithValue` work and when should you use it?**
- **Answer**: Attaches a key-value pair to context. Use only for request-scoped data (request ID, trace ID, auth token). Never for function parameters.
- **Why asked**: Context best practices.
- **Wrong answer**: "Use it for any data you need in child functions."
- **Follow-up**: Why should context keys be unexported types?

**19. What is `runtime.SetMutexProfileFraction`?**
- **Answer**: Enables mutex contention profiling. The argument controls sampling rate (1/N contention events recorded).
- **Why asked**: Debugging tools knowledge.
- **Wrong answer**: "It limits the number of mutexes."
- **Follow-up**: How do you analyze the mutex profile?

**20. How would you build a simple rate limiter in Go?**
- **Answer**: Use `golang.org/x/time/rate` with `rate.NewLimiter(ratePerSecond, burst)`. Or use a ticker-based approach.
- **Why asked**: Production pattern.
- **Wrong answer**: "Use `time.Sleep` between operations."
- **Follow-up**: How do you implement per-client rate limiting?

**21. What is a circuit breaker pattern?**
- **Answer**: Prevents calling a failing service repeatedly. States: closed (normal), open (fail fast), half-open (test recovery).
- **Why asked**: Microservice resilience.
- **Wrong answer**: "It's the same as a timeout."
- **Follow-up**: How would you implement one in Go?

**22. How do you gracefully shutdown a Go HTTP server?**
- **Answer**: Use `server.Shutdown(ctx)` which stops accepting new connections and waits for in-flight requests to complete.
- **Why asked**: Production readiness.
- **Wrong answer**: "Call `os.Exit(0)`."
- **Follow-up**: How do you set a deadline for shutdown?

**23. What is `sync.Pool` and when should you use it?**
- **Answer**: A cache of temporary objects for reuse, reducing GC pressure. Use for frequently allocated and released objects (byte buffers, structs).
- **Why asked**: Performance optimization.
- **Wrong answer**: "It's a connection pool."
- **Follow-up**: Can you rely on objects staying in the pool?

**24. How do you test concurrent code in Go?**
- **Answer**: Use `go test -race`, write tests with `sync.WaitGroup` for coordination, use `t.Parallel()` for concurrent test cases, use goleak for leak detection.
- **Why asked**: Testing discipline.
- **Wrong answer**: "Use `time.Sleep` to synchronize."
- **Follow-up**: What is `-count=100` useful for with concurrent tests?

**25. What does `runtime.SetBlockProfileRate` do?**
- **Answer**: Enables profiling of blocking events (channel operations, mutex waits). Helps identify where goroutines spend time waiting.
- **Why asked**: Performance profiling.
- **Wrong answer**: "It sets the rate of context switches."
- **Follow-up**: How is it different from mutex profiling?

**26-50: Condensed**

**26. What is the bulkhead pattern?** — Limiting concurrency per downstream service with semaphores to prevent one slow service from exhausting all goroutines.

**27. How do you implement ordered processing with parallelism?** — Partition work by key, ensure same-key items go to the same worker goroutine.

**28. What is `golang.org/x/sync/semaphore`?** — A weighted semaphore supporting acquiring/releasing multiple permits at once.

**29. How do buffered channels provide backpressure?** — Sender blocks when buffer is full, naturally slowing the producer.

**30. What happens if a goroutine panics?** — The program crashes unless the panic is recovered within the same goroutine using `defer` and `recover()`.

**31. How do you share a slice between goroutines safely?** — Use mutex, or ensure each goroutine writes to distinct indices (confinement), or use channels.

**32. What is confinement?** — Ensuring each goroutine has exclusive access to its own data, eliminating the need for synchronization.

**33. What is `atomic.CompareAndSwap`?** — Atomically compares a value with an expected value and swaps if they match. Used for lock-free algorithms.

**34. How do you profile memory in Go?** — Use `pprof` heap profile: `go tool pprof http://localhost:6060/debug/pprof/heap`.

**35. What is a done channel pattern?** — Using `chan struct{}` closed to signal cancellation. Predecessor to `context.Context`.

**36. How do you implement a pipeline in Go?** — Chain functions where each returns a `<-chan T`. Each stage reads from input channel, processes, writes to output channel.

**37. What is `go vet`?** — A static analysis tool that catches suspicious constructs, including some concurrency issues (copied locks, etc.).

**38. What does closing a nil channel do?** — Panics.

**39. How do you implement retry with exponential backoff?** — Loop with increasing delay: `time.Duration(1<<i) * baseDelay`, respecting context cancellation.

**40. What is a ticker vs a timer?** — Ticker fires repeatedly at intervals. Timer fires once after a delay.

**41. What is `http.Client.Timeout`?** — Sets the total timeout for an HTTP request including connection, headers, and body reading.

**42. How do you limit concurrent HTTP requests?** — Semaphore (buffered channel), `errgroup.SetLimit()`, or custom transport with connection limits.

**43. What is connection pool exhaustion?** — When all connections in a pool are in use and new requests must wait. Causes latency spikes.

**44. How do you handle panic in a goroutine?** — Use `defer func() { if r := recover(); r != nil { ... } }()` at the top of the goroutine.

**45. What is `net/http` server's concurrency model?** — Each incoming request is handled in its own goroutine automatically.

**46. How do you implement graceful shutdown for worker goroutines?** — Cancel context, close job channel, wait with WaitGroup.

**47. What is `go tool trace`?** — Execution tracing tool showing goroutine scheduling, blocking, and GC events on a timeline.

**48. How do you prevent thundering herd with caching?** — Use `singleflight` to deduplicate concurrent cache miss requests.

**49. What is the happen-before relationship in Go?** — Guarantees on memory visibility: if event A happens before event B, then A's writes are visible to B.

**50. How does Go's garbage collector interact with goroutines?** — GC may pause goroutines briefly (stop-the-world for some phases). Many goroutines with heavy allocations increase GC pressure.

**51-100: Condensed**

**51. How do you implement a bounded worker pool?** — Fixed number of goroutines reading from a shared job channel.

**52. What is the `select` non-blocking pattern?** — `select` with a `default` case.

**53. How do you send a signal without data?** — `chan struct{}` — zero-size type means no allocation.

**54. What is `http.NewRequestWithContext`?** — Creates an HTTP request that respects context cancellation and timeouts.

**55. How do you test for race conditions?** — `go test -race -count=100 ./...` — run multiple times to increase likelihood of detecting races.

**56. What is `sync.Map` optimized for?** — Read-heavy workloads or when each key is written by one goroutine and read by many.

**57. How do you broadcast to multiple goroutines?** — Close a channel. All goroutines receiving from it unblock simultaneously.

**58. What is an unbounded channel?** — Doesn't exist natively in Go. You'd need a goroutine with a growing slice as buffer (avoid if possible).

**59. How do you limit goroutines in `errgroup`?** — `g.SetLimit(N)` — blocks `g.Go()` when N goroutines are already running.

**60. What is `context.Cause` (Go 1.20+)?** — Returns the underlying cause error that triggered cancellation.

**61. When does a channel send block?** — Unbuffered: always until receiver ready. Buffered: when buffer is full.

**62. When does a channel receive block?** — When channel is empty and not closed.

**63. What is goroutine starvation?** — When a goroutine can't get CPU time because other goroutines monopolize the scheduler.

**64. How does Go handle epoll/kqueue for network I/O?** — Integrated netpoller parks goroutines on network I/O without consuming OS threads.

**65. What is `runtime.Gosched()`?** — Voluntarily yields the processor. Rarely needed with preemptive scheduling.

**66. How do you debug high latency in a Go service?** — CPU profile, block profile, trace, distributed tracing, check for lock contention and sequential I/O.

**67. What is the purpose of `go.uber.org/automaxprocs`?** — Automatically sets GOMAXPROCS based on container CPU limits instead of host core count.

**68. How do you share configuration safely between goroutines?** — `atomic.Pointer[Config]` for lock-free reads, or `sync.RWMutex` for complex configs.

**69. What is the producer-consumer pattern?** — Producers send work to a channel, consumers read from the channel and process it.

**70. What happens with 1 million goroutines?** — Each uses ~2-8KB, so 2-8GB memory. Feasible but monitor memory and scheduling overhead.

**71. How do you implement a priority queue for goroutines?** — Use a heap with a mutex, or multiple channels with a priority select pattern.

**72. What is `context.AfterFunc` (Go 1.21+)?** — Registers a function to run when the context is cancelled.

**73. How do you implement a circuit breaker in Go?** — Track failures with mutex-protected counter, switch states (closed/open/half-open) based on threshold and cooldown.

**74. What is `sync.OnceFunc` (Go 1.21+)?** — Returns a function that calls the provided function exactly once.

**75. How do you profile goroutine blocking?** — `runtime.SetBlockProfileRate(1)` then analyze with pprof block profile.

**76. What is the difference between `t.Parallel()` and `go func()`?** — `t.Parallel()` runs subtests concurrently with proper test lifecycle management.

**77. How do you prevent data races on a map?** — `sync.Mutex`, `sync.RWMutex`, or `sync.Map`.

**78. What is sharding and when should you use it?** — Splitting a data structure into N independent parts with separate locks. Use when lock contention is a bottleneck.

**79. How do channels handle ordering?** — FIFO. First value sent is first value received.

**80. What is the network poller?** — Go runtime component that efficiently handles thousands of network connections using OS-specific mechanisms (epoll, kqueue).

**81. What happens when `GOMAXPROCS(1)` is set?** — Only one goroutine can run at a time (no parallelism), but concurrency still works.

**82. How do you implement a watchdog timer?** — Goroutine with a ticker that resets on activity. If no reset within timeout, trigger alert/action.

**83. What is `atomic.Value`?** — Stores and loads an arbitrary value atomically. Useful for read-heavy config swaps.

**84. How do you handle timeouts in gRPC?** — Context with deadline/timeout propagates through gRPC calls automatically.

**85. What is tail latency and how does concurrency affect it?** — p99/p999 latency. Lock contention, GC pauses, and goroutine scheduling delays increase tail latency.

**86. How do you implement heartbeats between goroutines?** — Periodic sends on a channel, monitored by a select with timeout.

**87. What is `http.Transport.MaxIdleConnsPerHost`?** — Limits idle connections kept per host. Default is 2 — often too low for high-throughput services.

**88. How do you drain a channel?** — `for range ch { }` — reads all remaining values until channel is closed.

**89. What is a leaky bucket vs token bucket?** — Leaky bucket: constant output rate. Token bucket: allows bursts up to bucket capacity.

**90. How do you implement graceful shutdown with OS signals?** — `signal.Notify` with SIGTERM/SIGINT, cancel root context, wait for goroutines.

**91. What is `context.WithoutCancel` (Go 1.21+)?** — Creates a child context that isn't cancelled when the parent is cancelled (for background operations).

**92. How do you handle goroutine panic without crashing?** — Each goroutine must have its own `defer recover()`. One goroutine can't catch another's panic.

**93. What is a poison pill pattern?** — Sending a special sentinel value on a channel to signal workers to stop.

**94. How do you benchmark concurrent code?** — Use `b.RunParallel` in Go benchmarks, monitor with `-benchmem`.

**95. What is false sharing?** — When variables on the same CPU cache line are modified by different goroutines, causing cache invalidation. Mitigate with padding.

**96. How do you implement a concurrent-safe counter?** — `atomic.Int64` or mutex-protected int.

**97. What is `sync/atomic.Pointer` (Go 1.19+)?** — Type-safe atomic pointer operations without type assertions.

**98. How do you implement request-scoped logging?** — Attach a request ID via `context.WithValue`, extract in logger middleware.

**99. What is the difference between `Signal()` and `Broadcast()` in `sync.Cond`?** — Signal wakes one waiter. Broadcast wakes all waiters.

**100. How do you prevent a goroutine from being scheduled on the same OS thread?** — You don't need to. But `runtime.LockOSThread()` pins a goroutine TO a specific thread.

---

## 100 Senior-Level Questions

---

**1. Design a worker pool that handles 1 million jobs with bounded concurrency, error handling, and graceful shutdown.**
- **Answer**: Use `errgroup.WithContext` with `SetLimit(N)`, or create N worker goroutines reading from a buffered job channel. Support context cancellation. Close the job channel to signal shutdown, use WaitGroup to wait for workers to drain.
- **Why asked**: Tests ability to design production-grade concurrent systems.
- **Wrong answer**: Spawning 1 million goroutines (memory explosion).
- **Follow-up**: How do you handle partial failures? How do you report progress?

**2. Your Go service is leaking goroutines in production. Walk me through your debugging process.**
- **Answer**: (1) Check `runtime.NumGoroutine()` metrics — confirm goroutine count is growing. (2) Get goroutine dump via pprof: `curl /debug/pprof/goroutine?debug=1` to see counts by stack trace. (3) Use `debug=2` for full stacks. (4) Identify which function is stuck (usually in `chanrecv`, `chansend`, or `select`). (5) Trace back to find the missing cancellation or close. (6) Fix and add goleak test.
- **Why asked**: Tests real debugging experience.
- **Wrong answer**: "Restart the service." (Doesn't fix the root cause.)
- **Follow-up**: How would you prevent this from happening again?

**3. Explain the tradeoffs between channels and mutexes. When would you choose each?**
- **Answer**: Channels for goroutine coordination, ownership transfer, and event-driven patterns. Mutexes for protecting shared data structures (caches, maps). Channels have higher overhead (~50-100ns) than atomic/mutex (~5-20ns) but provide cleaner concurrency semantics. Use channels when multiple goroutines need to coordinate. Use mutexes when you just need to protect a data structure.
- **Why asked**: Tests practical judgment.
- **Wrong answer**: "Always use channels — it's the Go way."
- **Follow-up**: Show a scenario where using a channel would be over-engineering.

**4. How would you design a system to process 10,000 events per second from Kafka with ordering guarantees per entity?**
- **Answer**: Consume messages from Kafka partitions. For per-entity ordering, route messages to worker goroutines by hashing the entity key. Each worker processes messages for its assigned keys sequentially. Use a bounded number of workers (e.g., 100). Commit offsets after successful processing.
- **Why asked**: Real-world system design.
- **Wrong answer**: "Process all messages sequentially." (Too slow.)
- **Follow-up**: What happens during a rebalance? How do you handle poison messages?

**5. What causes high tail latency (p99) in concurrent Go services and how do you fix it?**
- **Answer**: Lock contention (goroutines waiting for mutexes), GC pauses (stop-the-world phases), goroutine scheduling delays (too many runnable goroutines), connection pool exhaustion, unbounded queues, and non-preemptive tight loops (pre-Go 1.14). Fix by reducing allocations, sharding locks, setting proper pool sizes, using pprof and trace.
- **Why asked**: Performance engineering experience.
- **Wrong answer**: "Add more instances."
- **Follow-up**: How do you differentiate GC pauses from lock contention?

**6. How does Go's memory model affect concurrent programming?**
- **Answer**: Go's memory model defines when writes in one goroutine are visible to reads in another. Without synchronization (channels, mutexes, atomics), there are no guarantees about visibility. A write in goroutine A might never be seen by goroutine B without explicit synchronization. This is why `sync/atomic` and channels provide happens-before guarantees.
- **Why asked**: Deep language understanding.
- **Wrong answer**: "Go handles memory visibility automatically."
- **Follow-up**: Can the compiler reorder memory operations?

**7. How would you implement a concurrent-safe LRU cache in Go?**
- **Answer**: Use `sync.Mutex` or `sync.RWMutex` with a doubly-linked list and hash map. For high-throughput, consider sharding into N independent caches with separate locks, or using an eviction channel pattern where a single goroutine owns the cache data.
- **Why asked**: Data structure + concurrency.
- **Wrong answer**: Using `sync.Map` (doesn't support LRU eviction).
- **Follow-up**: How do you handle cache stampede (thundering herd)?

**8. Explain how `context.Context` cancellation propagates through a microservice call chain.**
- **Answer**: When an HTTP/gRPC request arrives, the server creates a context. This context is passed to service calls, DB queries, etc. If the client disconnects, the server's context is cancelled. This cancellation propagates to all child contexts (database queries, downstream service calls). Each operation checks `ctx.Done()` and returns early. `errgroup.WithContext` cancels sibling operations when one fails.
- **Why asked**: Tests understanding of distributed cancellation.
- **Wrong answer**: "The context automatically stops all downstream services."
- **Follow-up**: What happens if a downstream service doesn't respect context cancellation?

**9. How do you prevent cascade failures in a microservice architecture using Go concurrency primitives?**
- **Answer**: (1) Timeouts on all outbound calls (context.WithTimeout). (2) Circuit breakers (track failures, fail fast when threshold exceeded). (3) Bulkheads (semaphore per downstream service). (4) Retry with backoff and jitter. (5) Graceful degradation (return cached/partial data). (6) Rate limiting.
- **Why asked**: System resilience design.
- **Wrong answer**: "Add retries to all calls." (Can make cascade failures worse.)
- **Follow-up**: How do you implement the bulkhead pattern?

**10. What is the singleflight pattern and how does it help under load?**
- **Answer**: `singleflight.Group` deduplicates concurrent identical requests. If 100 goroutines all request the same user from the database simultaneously, only one query executes — the other 99 wait and share the result. Prevents database overload during cache misses.
- **Why asked**: Performance pattern knowledge.
- **Wrong answer**: "It caches results." (It doesn't — it deduplicates in-flight requests.)
- **Follow-up**: What's the difference between singleflight and caching?

**11-25: Detailed Senior Questions**

**11. How do you handle a goroutine that's blocked on a third-party library call that doesn't accept context?**
- **Answer**: Wrap in a goroutine with a timeout using `select` + `time.After`. Use a buffered channel (capacity 1) so the goroutine can send its result even if nobody's listening. Accept that the goroutine may leak until the library call returns.
- **Why asked**: Real-world pragmatism.
- **Wrong answer**: "Always modify the library to accept context."
- **Follow-up**: When is it acceptable to let a goroutine leak?

**12. How would you implement a pipeline with error handling and partial results?**
- **Answer**: Each stage returns both a result channel and an error channel (or a combined `Result{Data, Err}` type). The consumer can decide to continue with partial results or abort. Use `errgroup` for stages that should fail fast. Use context cancellation to propagate abort decisions.
- **Why asked**: Pipeline design maturity.
- **Wrong answer**: "Stop the entire pipeline on first error."
- **Follow-up**: How do you handle backpressure in a pipeline?

**13. What is the difference between `sync.Pool` and object pooling with channels?**
- **Answer**: `sync.Pool` is for temporary objects — items may be garbage collected at any time (between GC cycles). Channel-based pooling guarantees object retention. Use `sync.Pool` for short-lived allocation reduction (byte buffers in hot paths). Use channel-based pooling for expensive-to-create objects (DB connections).
- **Why asked**: Performance optimization depth.
- **Wrong answer**: "sync.Pool is always better."
- **Follow-up**: What happens to sync.Pool during GC?

**14. How do you debug a Go service with high CPU usage but low request throughput?**
- **Answer**: Get a CPU profile via pprof. Look for (1) lock contention — time in `sync.(*Mutex).Lock`, (2) GC overhead — time in `runtime.gc*`, (3) busy loops or spin waits, (4) inefficient algorithms. Use mutex profile and block profile to find contention. Use `go tool trace` for scheduling analysis.
- **Why asked**: Production debugging.
- **Wrong answer**: "Scale horizontally." (Doesn't address root cause.)
- **Follow-up**: How do you distinguish between CPU-bound and I/O-bound bottlenecks?

**15. How do you safely reload configuration in a running Go service?**
- **Answer**: Use `atomic.Pointer[Config]` for lock-free reads. Load new config, validate, then atomically swap the pointer. Readers always see a consistent config snapshot. Alternatively, use `sync.RWMutex` with read locks for config access.
- **Why asked**: Hot reload patterns.
- **Wrong answer**: "Restart the service."
- **Follow-up**: How do you handle config-dependent goroutines (e.g., worker count change)?

**16. Explain how the Go scheduler's preemption works since Go 1.14.**
- **Answer**: Before 1.14, goroutines could only be preempted at function call boundaries (cooperative). A tight loop with no function calls could starve other goroutines. Since 1.14, the runtime uses asynchronous preemption — it sends signals (SIGURG on Unix) to preempt goroutines even in tight loops. This ensures scheduling fairness.
- **Why asked**: Scheduler depth.
- **Wrong answer**: "Go always had preemptive scheduling."
- **Follow-up**: What problems did cooperative scheduling cause?

**17. How would you design a distributed rate limiter that works across multiple service instances?**
- **Answer**: Use Redis with a sliding window or token bucket algorithm. Each instance checks Redis before allowing a request. Use Lua scripts for atomic check-and-decrement. For lower latency, use local rate limiters with periodic sync to Redis. Accept some overrun at the boundary.
- **Why asked**: Distributed systems design.
- **Wrong answer**: "Use a local rate limiter." (Doesn't work across instances.)
- **Follow-up**: How do you handle Redis being unavailable?

**18. What are the performance implications of using channels vs atomic operations?**
- **Answer**: Atomic ops: ~5-10ns (no lock, hardware-level). Mutex lock/unlock: ~15-25ns (uncontended). Channel send/receive: ~50-200ns (involves scheduler, memory barriers). For simple counters, atomics are 10-40x faster than channels. Use channels for coordination, not for performance-critical data updates.
- **Why asked**: Performance knowledge.
- **Wrong answer**: "Channels are always fast enough."
- **Follow-up**: When does channel overhead actually matter?

**19. How do you implement request-scoped tracing across goroutines?**
- **Answer**: Propagate context with trace ID via `context.WithValue`. Pass context to all spawned goroutines. Use OpenTelemetry for spans — create child spans in goroutines. Ensure `errgroup` or manual coordination passes the right context.
- **Why asked**: Observability.
- **Wrong answer**: "Use thread-local storage." (Go doesn't have TLS in the traditional sense.)
- **Follow-up**: What happens to traces when using `errgroup.WithContext`?

**20. How do you handle graceful shutdown in a Go service with multiple concurrent subsystems?**
- **Answer**: Create a root context with cancel. Listen for SIGTERM/SIGINT, cancel root context. Each subsystem (HTTP server, workers, background jobs) receives the context and stops when cancelled. Use `server.Shutdown(ctx)` for HTTP. Wait for workers with WaitGroup. Set a hard deadline with `context.WithTimeout` on the shutdown context.
- **Why asked**: Production lifecycle management.
- **Wrong answer**: "Call `os.Exit(0)`." (Doesn't drain in-flight work.)
- **Follow-up**: What if a subsystem doesn't respond to cancellation in time?

**21. How do you profile and optimize GC pressure from concurrent operations?**
- **Answer**: Use `pprof` heap profile with `alloc_space` to find allocation hotspots. Use `sync.Pool` for frequently allocated buffers. Pre-allocate slices. Reduce pointer-heavy structures. Monitor GC metrics (GOGC, pause times). Use `GODEBUG=gctrace=1` for GC logging.
- **Why asked**: GC performance.
- **Wrong answer**: "Set GOGC to a very high value." (Delays but doesn't fix the problem.)
- **Follow-up**: How does `sync.Pool` interact with GC?

**22. What's the difference between `runtime.SetBlockProfileRate` and `runtime.SetMutexProfileFraction`?**
- **Answer**: Block profile captures all blocking events (channels, mutexes, select, I/O). Mutex profile specifically captures mutex contention (time spent waiting for a mutex). Mutex profile is more focused for diagnosing lock contention. Block profile is broader.
- **Why asked**: Profiling tool selection.
- **Wrong answer**: "They do the same thing."
- **Follow-up**: When would you use each?

**23. How do you handle idempotency in concurrent message processing?**
- **Answer**: Use a deduplication store (Redis/DB) keyed by message ID. Check before processing. Use database transactions with unique constraints. For at-least-once delivery systems (Kafka), idempotency is critical because messages can be redelivered.
- **Why asked**: Distributed systems.
- **Wrong answer**: "Use exactly-once delivery." (Impossible in most systems.)
- **Follow-up**: How do you handle deduplication store failures?

**24. Explain how network I/O is handled by the Go runtime without blocking OS threads.**
- **Answer**: Go integrates a network poller (epoll on Linux, kqueue on macOS) into the scheduler. When a goroutine does network I/O, it's parked (removed from the run queue) and registered with the netpoller. When data arrives, the netpoller wakes the goroutine. No OS thread is blocked during the wait.
- **Why asked**: I/O model understanding.
- **Wrong answer**: "Each network connection gets its own OS thread."
- **Follow-up**: How is file I/O different from network I/O in Go?

**25. How do you implement a concurrent rate-limited API client with retry?**
- **Answer**: Use `golang.org/x/time/rate.Limiter` for rate limiting. Wrap each request with retry logic using exponential backoff with jitter. Respect context cancellation. Use a semaphore to limit concurrent in-flight requests. Handle 429 (Too Many Requests) responses with Retry-After header.
- **Why asked**: Real-world API integration.
- **Wrong answer**: "Add `time.Sleep` between requests."
- **Follow-up**: How do you handle rate limit headers from the API?

**26-100: Condensed Senior Questions**

**26. How do you implement a pub/sub system in Go?** — Map of topics to subscriber channels, mutex for topic map, goroutine per subscriber for delivery.

**27. What is the cost of context cancellation propagation?** — O(n) where n is the number of child contexts. Each child checks parent's Done channel.

**28. How do you handle large file processing concurrently?** — Split file into chunks by byte offset, process chunks in parallel with worker pool, merge results.

**29. What happens to goroutines when the GC runs?** — Goroutines are briefly stopped during GC's mark phase (stop-the-world). Concurrent GC runs alongside goroutines for most of the work.

**30. How do you implement a lease-based distributed lock in Go?** — Use etcd/Redis with TTL. Goroutine holds the lock and refreshes TTL periodically. Other goroutines wait or retry.

**31. What is goroutine-local storage and why doesn't Go have it?** — Go deliberately avoids thread-local storage because goroutines are multiplexed across threads. Use context for request-scoped data instead.

**32. How do you test a race condition that only appears under load?** — `go test -race -count=1000`, stress tests, `-parallel` flag, fuzzing.

**33. What is a hazard pointer and does Go use it?** — No. Go uses GC instead of manual memory management techniques like hazard pointers.

**34. How do you handle connection draining during deployment?** — Kubernetes sends SIGTERM, stop accepting new connections, drain in-flight requests with timeout, then exit.

**35. How do you implement a concurrent-safe ring buffer?** — Fixed-size array with atomic read/write positions, or mutex-protected indices.

**36. What is GOGC and how does it affect concurrent performance?** — Sets the GC target percentage. Higher = less frequent GC but more memory. Lower = more frequent GC, more CPU.

**37. How do you implement a cancellable sleep?** — `select { case <-ctx.Done(): return; case <-time.After(duration): }`.

**38. How do you prevent goroutine leaks in HTTP middleware?** — Ensure all spawned goroutines respect the request context. Use `errgroup` for coordination.

**39. What is the impact of GOMAXPROCS on CPU-bound vs I/O-bound workloads?** — CPU-bound: GOMAXPROCS ≈ cores for best performance. I/O-bound: GOMAXPROCS matters less since goroutines spend time waiting.

**40. How do you implement a concurrent map with expiring entries?** — Map with TTL per entry. Background goroutine periodically scans and evicts. Or use lazy expiration (check on read).

**41. What is the thundering herd problem?** — Many goroutines wake up simultaneously when a resource becomes available. Only one succeeds, others wasted. Fix with `singleflight`.

**42. How do you safely close a channel with multiple senders?** — Don't. Use a `sync.Once` to close, or have senders signal done on separate channels, and a coordinator closes when all done.

**43. How do you implement a time-based sliding window counter?** — Array of buckets, each representing a time slot. Atomic increments. Rotate buckets based on time.

**44. What is the difference between `sync.Pool` and a channel-based pool?** — `sync.Pool` may lose objects during GC. Channel-based pool retains objects. Choose based on whether you can tolerate recreation cost.

**45. How do you implement circuit breaker with concurrent-safe state transitions?** — Atomic state variable or mutex-protected state. Use `atomic.CompareAndSwap` for lock-free transitions.

**46. How do you monitor goroutine scheduling latency?** — Use `go tool trace` scheduler latency view. Shows time between goroutine becoming runnable and actually running.

**47. What causes goroutine scheduling delays?** — Too many runnable goroutines, long-running goroutines without preemption points (pre-1.14), GC stop-the-world pauses, long syscalls.

**48. How do you implement a work-stealing scheduler pattern in application code?** — Multiple queues with workers. When queue empty, steal from random other queue's tail.

**49. How do you handle partial failures in parallel operations?** — `errgroup` for fail-fast. For partial results, use individual error channels or result structs with error fields.

**50. What is the cost of creating a goroutine?** — ~1μs for creation, ~2KB initial memory. Cheap but not free. Don't create goroutines for trivial operations.

**51. How does Go handle goroutine stack growth?** — Starts at ~2KB. When stack overflows, runtime allocates a larger stack (2x) and copies the old one. This is called "stack copying" or "contiguous stacks."

**52. What is the impact of many goroutines on GC?** — Each goroutine is a root for GC scanning. Millions of goroutines increase GC overhead. Goroutine stacks must be scanned.

**53. How do you implement backoff with jitter?** — `baseDelay * 2^attempt + random(0, baseDelay)`. Jitter prevents synchronized retries from all clients.

**54. What is a wait-free algorithm?** — Every operation completes in a bounded number of steps regardless of contention. Stronger than lock-free. Rare in practice.

**55. How do you handle database connection pool sizing with high concurrency?** — `MaxOpenConns` ≈ expected concurrent queries. Too high = overwhelms DB. Too low = goroutines wait for connections.

**56. What is the impact of defer on performance?** — ~35ns overhead (Go 1.14+). Negligible for most code. Don't avoid defer for micro-optimization.

**57. How do you implement a concurrent priority queue?** — Heap data structure protected by `sync.Mutex`. Or use multiple channels with priority select pattern.

**58. What is `runtime.KeepAlive` and when do you need it?** — Prevents an object from being GC'd before a specific point. Needed when passing pointers to CGo or unsafe code.

**59. How do you handle context propagation in async operations (fire-and-forget)?** — Create a new background context derived from the request context's values but not its cancellation. Use `context.WithoutCancel` (Go 1.21+).

**60. What is the memory ordering guarantee of Go's channel operations?** — A send on a channel happens before the corresponding receive completes. Closing a channel happens before a receive of the zero value.

**61. How do you implement a concurrent batch writer?** — Collect items in a buffer with mutex. Flush when buffer full or time interval expires. Use a goroutine with ticker and channel.

**62. What is lock-free programming and when should you use it in Go?** — Using atomic operations instead of locks. Use for simple counters and flags. For complex data structures, mutexes are simpler and usually fast enough.

**63. How do you handle memory leaks caused by goroutine-held references?** — Ensure goroutines can exit (context cancellation). Large objects referenced by goroutines aren't GC'd until the goroutine exits.

**64. What are the tradeoffs of sharding a mutex-protected map?** — Pros: reduces contention, improves throughput. Cons: more complex code, harder to iterate, doesn't help if one shard is hot.

**65. How do you implement a concurrent rate limiter with burst handling?** — Token bucket: `rate.NewLimiter(rate, burst)`. Burst allows N requests immediately, then enforces sustained rate.

**66. What is `runtime.SetFinalizer` and should you use it for cleanup?** — Registers a function to run when an object is GC'd. Unreliable timing. Use `defer`, `context`, or explicit cleanup instead.

**67. How do you handle long-running HTTP requests without blocking the server?** — Process in a background goroutine. Return 202 Accepted. Provide a status endpoint for polling. Or use Server-Sent Events/WebSockets.

**68. What is the impact of channel buffer size on performance?** — Larger buffer: less synchronization overhead, but more memory and delayed backpressure. Smaller buffer: tighter coupling, faster feedback.

**69. How do you implement a concurrent skip list?** — Use fine-grained locking (lock per level) or lock-free with CAS operations. Complex. Usually `sync.Map` or sharded maps are simpler.

**70. What is `GODEBUG=asyncpreemptoff=1` and when would you use it?** — Disables async preemption. Used for debugging preemption-related issues. Not for production.

**71. How do you handle slow consumers in a pipeline?** — Bounded channels (backpressure), drop oldest, batch processing, or dynamic worker scaling.

**72. What is the role of memory barriers in Go's sync primitives?** — Ensure memory visibility across goroutines. Mutex Lock/Unlock, channel send/receive, and atomic operations all include memory barriers.

**73. How do you implement a debouncer in Go?** — Timer-based: reset timer on each event. Only process when timer fires (no new event for N ms).

**74. What causes "fatal error: concurrent map read and map write"?** — Go runtime detects concurrent map access and crashes intentionally. Fix with `sync.Mutex` or `sync.Map`.

**75. How do you profile network latency in a Go service?** — Distributed tracing (OpenTelemetry), HTTP client trace hooks, `go tool trace` for network blocking.

**76. What is head-of-line blocking and how does it apply to channels?** — A slow consumer on a shared channel blocks all producers. Fix with per-consumer channels or non-blocking sends.

**77. How do you implement graceful degradation for non-critical features?** — Wrap in context with short timeout. On error/timeout, return default values. Log but don't fail the request.

**78. What is the difference between `t.Run` with `t.Parallel()` and `go func()` in tests?** — `t.Run`+`t.Parallel()` integrates with Go's test runner (proper cleanup, timeout, reporting). `go func()` doesn't.

**79. How do you handle goroutine panics in a worker pool?** — Each worker wraps its logic in `defer recover()`. Log the panic and continue processing other jobs.

**80. What is goroutine affinity and does Go support it?** — Keeping a goroutine on the same P/M for cache locality. Go's scheduler does this naturally (local queue) but doesn't guarantee it.

**81. How do you implement a health check that detects goroutine leaks?** — Expose `/healthz` that checks `runtime.NumGoroutine()` against a threshold.

**82. What is the cost of `defer`?** — ~35ns in Go 1.14+. Was ~90ns before. Optimized for common case (open-coded defers).

**83. How do you handle out-of-order completion in parallel operations?** — Use indexed result slices, or result channels with identifiers. Don't assume goroutines complete in launch order.

**84. What is `GOMAXPROCS` interaction with cgroup CPU limits?** — Default GOMAXPROCS uses host core count, ignoring container limits. Use `automaxprocs` to respect cgroup limits.

**85. How do you implement request coalescing for write operations?** — Batch writes: collect in a channel, flush periodically or when batch is full. Use mutex-protected buffer.

**86. What is the impact of goroutine stack shrinking?** — Runtime can shrink goroutine stacks during GC if they're oversized. Reduces memory but involves copying.

**87. How do you implement a concurrent trie or prefix tree?** — Fine-grained locking per node, or RWMutex at the root (simpler but less concurrent).

**88. What happens to pending timers when a goroutine exits?** — Timers are GC'd when no longer referenced. `time.After` timers in select may fire after the goroutine exits (safe, channel is GC'd too).

**89. How do you implement A/B testing with concurrent-safe feature flags?** — Atomic pointer to feature flag config. Hot reload with atomic swap. Check flag per request using context.

**90. What is `net.Dialer.Timeout` vs `http.Client.Timeout`?** — Dialer.Timeout: connection establishment timeout. Client.Timeout: entire request lifecycle (connect + TLS + headers + body).

**91. How do you handle dependency injection with concurrent services?** — Pass dependencies at creation time. Service struct holds clients/pools. Goroutines receive dependencies through the service methods, not globals.

**92. What is the impact of `runtime.Gosched()` on modern Go?** — Minimal. Preemptive scheduling (Go 1.14+) makes voluntary yielding unnecessary for most cases.

**93. How do you implement a concurrent iterator pattern?** — Return `<-chan Item`. Producer goroutine sends items, closes channel when done. Consumer ranges over channel.

**94. What is `sync.Map.CompareAndSwap`?** — Atomically compares and swaps a value in a `sync.Map`. Added in Go 1.20.

**95. How do you handle goroutine-safe lazy initialization with error handling?** — `sync.OnceValues` (Go 1.21+) or custom implementation: `sync.Once` with error stored in struct field.

**96. What is the impact of many `select` cases on performance?** — Linear scan of cases. More cases = slightly slower. Not an issue for typical use (< 10 cases).

**97. How do you implement a concurrent bloom filter?** — Sharded bit arrays with atomic operations per shard. Or single bit array with atomic bit operations.

**98. What causes goroutine scheduling unfairness?** — CPU-bound goroutines without preemption points (pre-1.14), uneven work distribution, priority inversion with locks.

**99. How do you implement leader election using Go concurrency primitives?** — Use etcd/consul for distributed leader election. Locally, use `sync.Once` or a single coordinating goroutine.

**100. What is the relationship between Go's scheduler and OS scheduler?** — Go scheduler runs in userspace, multiplexes goroutines onto OS threads. OS scheduler schedules those OS threads onto CPU cores. Two levels of scheduling.

---

## 50 Staff-Level Questions

---

**1. You're tasked with designing a Go service that handles 1 million concurrent WebSocket connections. What concurrency architecture would you use?**
- **Answer**: Use Go's net/http with custom WebSocket handler (gorilla/websocket or nhooyr/websocket). Each connection gets a goroutine pair (read + write). That's 2M goroutines — feasible (~4-16GB memory). Use per-connection buffered channels for outbound messages. Implement connection pooling for outbound messages (fan-out). Use epoll-based frameworks (gnet) if stdlib is a bottleneck. Shard connection state across multiple maps with separate locks. Implement graceful draining for deployments.
- **Why asked**: Tests large-scale systems thinking.
- **Wrong answer**: "Use a thread pool." (Goroutines are the pool.)
- **Follow-up**: How do you handle broadcasting to all connections efficiently?

**2. How would you architect a Go service to achieve sub-millisecond p99 latency?**
- **Answer**: Pre-allocate everything — use `sync.Pool` for request/response buffers. Avoid allocations in the hot path. Use `atomic` instead of mutexes where possible. Set `GOGC` higher to reduce GC frequency. Use connection pooling with pre-warmed connections. Avoid channel overhead in the critical path — use direct function calls. Pin to specific CPU cores if needed. Measure with `go tool trace` to identify scheduling delays. Consider using `fasthttp` instead of `net/http` for lower allocation overhead.
- **Why asked**: Performance engineering depth.
- **Wrong answer**: "Go can't achieve sub-millisecond latency." (It can.)
- **Follow-up**: How do you measure and monitor p99 latency continuously?

**3. How would you design a concurrent data pipeline that processes 100GB of data with exactly-once semantics?**
- **Answer**: Split input into chunks with deterministic boundaries. Use a checkpoint/offset store (database) to track processed chunks. Each chunk is processed idempotently (same input = same output). Use a transactional outbox pattern for downstream writes. Worker pool processes chunks with context cancellation. On restart, resume from last checkpoint. Use file locking or distributed locks to prevent concurrent processing of the same chunk.
- **Why asked**: Data engineering + concurrency.
- **Wrong answer**: "Process everything in memory." (Won't fit.)
- **Follow-up**: How do you handle a worker crashing mid-chunk?

**4. You discover that your Go service's performance degrades under sustained high load but recovers after load decreases. What's happening and how do you fix it?**
- **Answer**: Likely causes: (1) GC pressure — high allocation rate causes frequent GC, stealing CPU. Fix: reduce allocations with `sync.Pool`, pre-allocation. (2) Connection pool exhaustion — goroutines queue for connections. Fix: increase pool size, add timeouts. (3) Lock contention — grows with concurrency. Fix: shard data structures. (4) Goroutine explosion — each request spawns many goroutines. Fix: bound concurrency. Use pprof during degradation to identify the bottleneck.
- **Why asked**: Root cause analysis under pressure.
- **Wrong answer**: "Scale horizontally." (Hides the problem.)
- **Follow-up**: How do you prevent this from happening again?

**5. How would you design a distributed job scheduler in Go that ensures jobs run exactly once across multiple instances?**
- **Answer**: Use a coordination service (etcd, Redis, PostgreSQL advisory locks) for distributed locking. Each instance attempts to acquire a lock per job. The winner executes the job. Use lease-based locks with TTL to handle instance failures. Implement a job table with state machine (pending → running → completed/failed) using optimistic concurrency (version column). Background goroutines poll for jobs, acquire locks, process, update state.
- **Why asked**: Distributed systems design.
- **Wrong answer**: "Use in-memory state." (Lost on restart.)
- **Follow-up**: How do you handle long-running jobs and instance restarts?

**6. How would you refactor a Go monolith's concurrency model from shared-memory (mutexes everywhere) to message-passing?**
- **Answer**: (1) Identify data ownership — who reads/writes what? (2) Create service actors — goroutines that own data and communicate via channels. (3) Replace mutex-protected shared state with actor-owned state accessed via request/response channels. (4) Use the channel ownership pattern — each actor creates and closes its channels. (5) Migrate incrementally — start with the most contended locks. (6) Keep mutexes for truly simple shared state (counters, caches).
- **Why asked**: Architecture evolution.
- **Wrong answer**: "Replace all mutexes with channels." (Over-engineering.)
- **Follow-up**: When would you keep mutexes?

**7. A Go service processes events from 1000 Kafka partitions. Each event must update a shared database. How do you design the concurrency model?**
- **Answer**: One goroutine per partition (Kafka's consumer group model). For database writes, batch events and use a bounded worker pool for DB writes. Implement a write buffer per partition that flushes periodically or when full. Use connection pooling sized appropriately (not 1000 connections). Commit Kafka offsets only after successful DB writes. Handle partition rebalancing by draining in-flight work before releasing partitions.
- **Why asked**: Real-world high-throughput design.
- **Wrong answer**: "One DB connection per partition." (Overwhelms DB.)
- **Follow-up**: How do you handle DB failures without losing events?

**8. How do you design a Go service that can handle both real-time and batch workloads without one affecting the other?**
- **Answer**: Separate goroutine pools and resource allocation for each workload. Use CPU pinning or separate GOMAXPROCS-controlled runtimes (separate processes) for complete isolation. For partial isolation within a process: separate worker pools with independent semaphores, separate connection pools, and priority-based scheduling. Monitor each workload's latency independently. Use rate limiting to prevent batch work from starving real-time traffic.
- **Why asked**: Resource management.
- **Wrong answer**: "They can share the same goroutines." (One will affect the other.)
- **Follow-up**: How do you implement priority between the two?

**9. How would you implement an in-memory event sourcing system in Go with concurrent readers and a single writer?**
- **Answer**: Single writer goroutine owns the event log (slice of events). Readers snapshot the current state via atomic pointer to an immutable state snapshot. Writer appends events, computes new state, atomically publishes new snapshot. Readers never block the writer. Use `atomic.Pointer[State]` for the snapshot. For event persistence, the writer goroutine also writes to disk/DB asynchronously.
- **Why asked**: Event sourcing + concurrency.
- **Wrong answer**: "Use a mutex for all access." (Writer blocks readers.)
- **Follow-up**: How do you handle state snapshots for new readers?

**10. A critical Go service has intermittent latency spikes every 30 seconds. How do you investigate?**
- **Answer**: 30-second interval suggests GC (default `GOGC=100` triggers around this frequency under steady allocation). Check: (1) `GODEBUG=gctrace=1` for GC pause durations. (2) pprof heap profile for allocation hotspots. (3) `go tool trace` for GC-related goroutine pauses. (4) If not GC: check for periodic cron/ticker operations, cache expiration, connection pool rotation. Fix: reduce allocations, tune `GOGC`, use `sync.Pool`.
- **Why asked**: Deep debugging experience.
- **Wrong answer**: "It must be network issues." (Too vague.)
- **Follow-up**: How do you differentiate GC pauses from other causes?

**11-50: Condensed Staff Questions**

**11. How do you design a zero-downtime deployment strategy for a stateful Go service?** — Kubernetes rolling updates with preStop hooks, connection draining, health check delay, session migration.

**12. How would you implement a multi-tenant rate limiter with fairness guarantees?** — Per-tenant token buckets with a global coordinator. Use weighted fair queuing for shared resources.

**13. What's your approach to capacity planning for a concurrent Go service?** — Load test to find saturation point. Monitor goroutine count, heap, CPU, and latency at various loads. Plan for 3x peak.

**14. How do you design a Go service for observability at scale?** — Structured logging with context-propagated request IDs. OpenTelemetry traces. Prometheus metrics for goroutines, channels, and latency histograms.

**15. How would you implement a concurrent graph traversal with cycle detection?** — Worker pool with concurrent-safe visited set (sync.Map or sharded map). Each worker checks visited before processing.

**16. How do you handle goroutine leaks in third-party libraries?** — Wrap library calls with context+timeout. Monitor goroutine counts. File bugs. Consider alternative libraries. Isolate in separate process if critical.

**17. How would you design a Go service that survives dependent service outages?** — Circuit breakers, fallback caches, graceful degradation, bulkheads, retry with backoff, health checks with automatic failover.

**18. How do you evaluate whether to use Go's channels or a message queue (Kafka/NATS) for inter-service communication?** — Channels: in-process only. Message queues: cross-process, persistent, distributed. Use channels for intra-service concurrency. Use queues for inter-service communication.

**19. How do you design a Go worker that processes exactly once from an at-least-once queue?** — Idempotency key in a deduplication store. Check before processing. Transaction wrapping process+dedup insert.

**20. How would you implement a distributed tracing system in Go from scratch?** — Generate trace ID at entry. Propagate via context. Create spans with timing. Send to collector asynchronously via buffered channel + batch worker.

**21. How do you handle a Go service that needs to process both ordered and unordered streams simultaneously?** — Separate processing paths. Ordered: single goroutine per partition/key. Unordered: worker pool.

**22. How would you design a concurrent task scheduler with dependencies (DAG execution)?** — Topological sort. Track completed tasks with concurrent-safe set. Launch tasks when all dependencies are met. Use channels for completion signals.

**23. How do you prevent memory leaks in long-running Go services?** — Monitor heap profiles over time. Use goleak. Ensure all goroutines can exit. Avoid global caches without eviction. Profile periodically.

**24. How would you implement a load shedding mechanism in Go?** — Monitor queue depth and latency. When above threshold, reject new requests with 503. Use adaptive concurrency limits (Netflix's approach).

**25. How do you design a Go service for horizontal scalability?** — Stateless request handling. External state (Redis, DB). Distributed locking for coordination. Per-instance rate limiting with global coordination.

**26. How would you implement a concurrent-safe plugin system in Go?** — Plugin interface + goroutine per plugin. Communicate via channels. Use context for lifecycle. Recover from panics per plugin.

**27. How do you evaluate the tradeoff between in-process concurrency and distributed systems?** — In-process: lower latency, simpler, limited to one machine. Distributed: higher latency, complex, scales beyond one machine.

**28. How would you design a Go-based API gateway with concurrent request routing?** — Per-route handler goroutines. Circuit breakers per backend. Shared rate limiter. Connection pooling per upstream service.

**29. How do you handle schema evolution in concurrent event processing?** — Version events. Support reading old and new versions. Use protobuf or similar with backward compatibility.

**30. How would you implement a distributed cache with Go's concurrency primitives?** — Consistent hashing for key distribution. Per-shard goroutine for cache operations. gRPC for inter-node communication.

**31. What's your strategy for testing a Go service under concurrent load?** — `go test -race`, load testing with vegeta/k6, chaos testing with random delays/failures, soak testing for leaks.

**32. How do you handle database transaction isolation with concurrent goroutines?** — Each goroutine gets its own transaction. Never share a transaction across goroutines. Use connection pool properly.

**33. How would you implement a concurrent audit log system?** — Non-blocking log channel. Background goroutine batches and persists. Buffer with drop-on-full for non-critical, block-on-full for critical.

**34. How do you design for failure in concurrent Go systems?** — Every goroutine handles its own errors. Use errgroup for propagation. Circuit breakers for external calls. Supervision trees via context hierarchy.

**35. How would you migrate a callback-based async system to Go's channel model?** — Replace callbacks with channel sends. Replace callback registration with goroutine launch. Use select for multiplexing.

**36. How do you implement admission control in a Go service?** — Track in-flight requests with atomic counter. Reject when above threshold. Consider Little's Law: concurrency = throughput × latency.

**37. How do you handle clock skew in concurrent distributed systems?** — Use logical clocks (Lamport), vector clocks, or hybrid logical clocks. Don't rely on wall clock ordering across nodes.

**38. How would you implement a concurrent bloom filter with minimal lock contention?** — Partition the bit array into segments. Use atomic bit operations within segments. No global lock needed.

**39. How do you design a Go service that gracefully handles out-of-memory conditions?** — Set memory limits (GOMEMLIMIT in Go 1.19+). Monitor heap usage. Implement backpressure when memory is high. Drop non-critical work.

**40. How would you implement a work queue with fair scheduling across tenants?** — Per-tenant queue. Round-robin or weighted fair queuing across tenants. Worker pool pulls from the scheduler.

**41. How do you evaluate the performance impact of adding tracing to a concurrent Go service?** — Benchmark with and without tracing. Typical overhead: 1-5%. Use sampling for high-throughput services.

**42. How would you design a Go-based stream processing framework?** — Pipeline stages as goroutines. Channels between stages. Support fan-out, fan-in, filter, map, reduce. Context for lifecycle.

**43. How do you handle data consistency across concurrent microservice calls?** — Saga pattern for distributed transactions. Compensating actions for rollback. Eventual consistency with reconciliation.

**44. How would you implement a concurrent connection pool from scratch?** — Channel of connections. Get acquires from channel (or creates new). Put returns to channel. Background goroutine for health checks and eviction.

**45. How do you design a Go service for graceful capacity reduction (scale-down)?** — Kubernetes preStop hook. Drain connections. Finish in-flight work. Deregister from service discovery. Wait, then exit.

**46. How would you implement concurrent data validation with partial failure handling?** — Validate each item in parallel with errgroup. Collect both successes and failures. Return detailed validation report.

**47. How do you handle goroutine scheduling fairness in a multi-tenant system?** — Per-tenant concurrency limits (semaphores). Per-tenant resource quotas. Monitor and alert on per-tenant resource usage.

**48. How would you design a Go service that needs to handle both sync and async API patterns?** — Sync: handle in request goroutine. Async: submit to job queue, return job ID. Separate worker pool for async jobs.

**49. What's your approach to performance regression testing for concurrent Go code?** — Benchmark suite with `go test -bench`. Store results. Compare across commits. Alert on significant regressions. Include contention benchmarks.

**50. How do you mentor engineers on Go concurrency best practices?** — Code review checklist (context propagation, goroutine lifecycle, race detector CI, lock scope). Pattern library with production examples. Post-mortems from concurrency bugs. Pair programming on concurrent features.

---

# Final Summary

This document covered:

1. **Foundations**: Concurrency vs parallelism, processes vs threads vs goroutines, why Go chose CSP
2. **Core Primitives**: Goroutines, scheduler (G-M-P), channels (deep dive), select, context, sync primitives
3. **Common Bugs**: Race conditions, deadlocks, goroutine leaks
4. **Production Patterns**: Worker pools, fan-out/fan-in, pipelines, backpressure, rate limiting
5. **Real-World Systems**: APIs, microservices, event processing, Kafka, background jobs
6. **Production Engineering**: Debugging CPU, memory, goroutine leaks, deadlocks, lock contention, pprof, trace, metrics
7. **Interview Mastery**: 350 questions across junior, mid, senior, and staff levels

**Remember**: Concurrency is a design skill, not just a language feature. The best concurrent code is simple, explicit about ownership, and easy to reason about shutdown.

> "Make it work, make it right, make it concurrent." — Not Rob Pike, but should be.
