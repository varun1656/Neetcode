# The Go Concurrency Bible: Internals, Architecture, and Performance

Welcome to the definitive guide on Go Concurrency. This document bridges the gap between writing `go func()` and understanding the raw assembly, memory barriers, and runtime scheduling that make Go one of the most powerful concurrent languages in the world.

Whether you are a Senior Engineer debugging a distributed deadlock, a Performance Engineer tuning `GOMAXPROCS`, or a candidate preparing for a Staff-level systems interview, this document serves as your permanent reference.

---

## SECTION 1: CONCURRENCY FUNDAMENTALS

### Concurrency vs. Parallelism
Rob Pike famously said: *"Concurrency is about dealing with a lot of things at once. Parallelism is about doing a lot of things at once."*

*   **Concurrency** is a program structure. It is the composition of independently executing processes. 
*   **Parallelism** is a physical execution state. It is the simultaneous execution of (possibly related) computations.

A single-core machine can run highly concurrent Go programs without ever executing in parallel. The OS or Go runtime interleaves the execution.

### The CSP Model (Communicating Sequential Processes)
Go's concurrency is rooted in Tony Hoare's 1978 paper on CSP. 
Traditionally, threads communicate by sharing memory and protecting it with Mutexes. This leads to deadlocks, race conditions, and mental overhead.
CSP dictates: **"Do not communicate by sharing memory; instead, share memory by communicating."**

In Go, **Goroutines** are the *Sequential Processes*, and **Channels** are the *Communication* medium.

### Concurrency Models Compared
| Language | Model | Implementation | Thread Footprint | Context Switch Cost |
| :--- | :--- | :--- | :--- | :--- |
| **Java (Pre-21)** | Kernel Threads | 1:1 OS Thread mapping | 1MB - 2MB | High (~1-2µs) |
| **Node.js** | Event Loop | Single threaded, Async I/O | N/A | Low (Callback) |
| **Python** | Asyncio | Coroutines (Event Loop) | N/A | Low |
| **Erlang** | Actor Model | Green threads, Mailboxes | ~300 bytes | Very Low |
| **Go** | CSP | M:N Scheduling (Goroutines) | ~2KB | Very Low (~200ns) |

---

## SECTION 2: GOROUTINES

### What is a Goroutine?
A goroutine is a user-space thread (a "green thread") managed entirely by the Go runtime, not the OS kernel. 

### Internal Implementation
When you write `go func()`, the Go compiler translates this into a call to `runtime.newproc()`.

**The `g` struct (`src/runtime/runtime2.go`):**
Every goroutine is represented by a `g` struct.
```go
type g struct {
    stack       stack   // offset known to runtime/cgo
    stackguard0 uintptr // stack growth prologue checks
    stackguard1 uintptr // cgo stackguard
    _panic      *_panic // innermost panic
    _defer      *_defer // innermost defer
    m           *m      // current m; non-nil if running
    sched       gobuf   // goroutine context (SP, PC, BP)
    goid        int64   // unique ID
    status      uint32  // Gidle, Grunnable, Grunning, Gsyscall, Gwaiting
    // ...
}
```

### Goroutine Lifecycle & Stack Management
1.  **Initialization:** A goroutine starts with a tiny **2KB stack** allocated on the heap.
2.  **Execution:** The scheduler places the `g` onto a run queue.
3.  **Stack Growth (Continuous Stacks):**
    *   In C/C++, stacks are fixed size (e.g., 8MB). If you exceed it, you get a Stack Overflow.
    *   Go uses **Continuous Stacks**. Every function call includes a prologue that checks if `SP` (Stack Pointer) > `stackguard0`.
    *   If space is insufficient, Go calls `runtime.morestack()`.
    *   Go allocates a **new stack double the size**, copies the old stack to the new stack, updates all internal pointers, and frees the old stack.
4.  **Stack Shrinking:** During Garbage Collection, if a stack is less than 25% utilized, the runtime shrinks it by 50% to save memory.

---

## SECTION 3: GO SCHEDULER

The Go scheduler is an **M:N cooperative and preemptive scheduler**. It multiplexes `M` goroutines onto `N` OS threads.

### The GMP Model
*   **G (Goroutine):** The execution context (stack, instruction pointer).
*   **M (Machine):** An OS Kernel Thread. It is managed by the OS. It actually executes the code.
*   **P (Processor):** A logical CPU core. It holds the context and a local queue of Gs. `GOMAXPROCS` defines the number of Ps.

```text
Global Queue: [ G ] -> [ G ] -> [ G ]

[ P 0 ]  <-- M 0 (OS Thread)       [ P 1 ]  <-- M 1 (OS Thread)
  |                                  |
Local Queue                        Local Queue
 [ G 1 ]                            [ G 4 ]
 [ G 2 ]                            [ G 5 ]
 [ G 3 ] (Running)                  [ G 6 ] (Running)
```

### Work Stealing Algorithm
If `P 0` finishes all its `G`s, it doesn't stay idle. It performs **Work Stealing**:
1. Check its own local queue.
2. Check the Global Run Queue.
3. Check the Network Poller (for I/O bound Gs that are ready).
4. **Steal half the Gs from another randomly chosen P.**

### System Calls and Handoff
If `G 3` makes a blocking syscall (like reading a file), the OS thread `M 0` blocks. 
*   The Go runtime detects this. 
*   It **detaches** `P 0` from `M 0` and attaches `P 0` to a new or idle thread `M 2`.
*   `P 0` continues executing `G 1` and `G 2`.
*   When `M 0` finishes the syscall, `G 3` is placed back onto a run queue, and `M 0` goes to sleep (or is destroyed).

### Async Preemption
Before Go 1.14, a tight loop without function calls `for { /* heavy math */ }` would freeze a `P` forever (starvation) because scheduling was purely cooperative.
Go 1.14 introduced **Async Preemption** using OS signals (`SIGURG`). A background thread `sysmon` detects if a `G` has run for > 10ms, sends a `SIGURG` to the thread, forces an interrupt, saves the registers, and moves the `G` to the global queue.

---

## SECTION 4: CHANNELS

Channels are typed conduits. Under the hood, a channel is a strictly protected queue.

### Internals: `runtime.hchan` (`src/runtime/chan.go`)
```go
type hchan struct {
    qcount   uint           // total data in the queue
    dataqsiz uint           // size of the circular queue (capacity)
    buf      unsafe.Pointer // points to an array of dataqsiz elements
    elemsize uint16         // size of the element type
    closed   uint32         // 0 or 1
    sendx    uint           // send index (circular queue)
    recvx    uint           // receive index (circular queue)
    recvq    waitq          // list of recv waiters (sudogs)
    sendq    waitq          // list of send waiters (sudogs)
    lock     mutex          // protects ALL fields in hchan
}
```

### Memory Layout
A buffered channel `make(chan int, 3)` allocates:
1.  The `hchan` struct (~96 bytes).
2.  A contiguous memory block for `buf` (3 * 8 = 24 bytes).

### Blocking and Unblocking (The `sudog`)
What happens when you read from an empty channel `<-ch`?
1.  The goroutine acquires `hchan.lock`.
2.  It sees `qcount == 0`.
3.  It creates a `sudog` struct (representing the waiting goroutine and the memory address where it wants the data).
4.  It pushes the `sudog` into the `hchan.recvq` (receive queue).
5.  It calls `runtime.park()`, taking the goroutine OFF the OS thread.
6.  It releases `hchan.lock`.

When another goroutine sends `ch <- 5`:
1.  It acquires `hchan.lock`.
2.  It sees a waiting `sudog` in `recvq`.
3.  **Optimization:** It copies the value `5` *directly* into the memory address of the receiving goroutine (bypassing the buffer entirely).
4.  It calls `runtime.goready()` to put the receiving goroutine back onto a `P`'s run queue.

---

## SECTION 5: SELECT STATEMENT

The `select` statement lets a goroutine wait on multiple communication operations.

### Implementation: `runtime.selectgo()`
When the compiler sees a `select`, it translates it to `selectgo()`.
1.  **Locking Order:** To prevent deadlocks, `selectgo` sorts all the channels involved by their memory addresses. It locks them in that exact order.
2.  **Randomization:** It creates a randomly ordered list of the cases. It polls the channels in this random order to ensure fairness (so the first case doesn't starve the others).
3.  **Fast Path:** If any channel is ready (buffer has data, or a waiting `sudog`), it executes that case.
4.  **Slow Path (Parking):** If NO channels are ready:
    *   It creates a `sudog` for *every* channel in the select statement.
    *   It parks the goroutine.
    *   When *one* of the channels wakes it up, the goroutine must iterate through all the other channels and remove its `sudog` from their wait queues to prevent memory leaks.

---

## SECTION 6: MEMORY MODEL & DATA RACES

### The Go Memory Model
The memory model specifies the conditions under which reads of a variable in one goroutine can be guaranteed to observe values produced by writes to the same variable in a different goroutine.

At the hardware level, CPUs use **Out-of-Order Execution** and **L1/L2 Caches**. Without explicit synchronization, Thread A might write `x = 1`, but Thread B might see `x = 0` because the write hasn't propagated through the MESI cache coherence protocol, or the CPU reordered the instructions.

### Happens-Before Guarantees
Go defines "happens-before" rules. If Event $A$ happens before Event $B$, $B$ is guaranteed to see the memory written by $A$.
*   **Initialization:** `init()` happens before `main.main()`.
*   **Goroutine Creation:** The `go` statement happens before the goroutine's execution begins.
*   **Channels:** A send on a channel happens before the corresponding receive from that channel completes.
*   **Unbuffered Channels:** A receive from an unbuffered channel happens before the send on that channel completes.
*   **Mutexes:** `Unlock()` happens before any subsequent `Lock()` returns.

### The Race Detector (`-race`)
Go's race detector uses **Vector Clocks** and shadow memory. 
For every 8 bytes of application memory, the race detector allocates 4 bytes of shadow memory to track thread IDs, access timestamps, and read/write flags.
If two different thread IDs access the same memory location, at least one is a write, and there is no happens-before edge (tracked via Vector Clocks), Go throws a Data Race panic.

---

## SECTION 7: SYNCHRONIZATION PRIMITIVES

### 1. `sync.Mutex`
```go
type Mutex struct {
    state int32
    sema  uint32
}
```
The `state` is a bitmask:
*   `mutexLocked` (1 bit)
*   `mutexWoken` (1 bit)
*   `mutexStarving` (1 bit)
*   `waiterShift` (29 bits) - Number of waiting goroutines.

**Fast Path:** Uses atomic Compare-And-Swap (CAS) on `state`. If `state == 0`, it atomically sets it to `1`. This takes ~1ns.
**Slow Path:** If locked, the goroutine enters **Active Spinning**. It wastes CPU cycles for a few microseconds hoping the lock gets released. If it doesn't, it increments the waiter count and yields to the OS (parks on the `sema`).
**Starvation Mode:** If a goroutine waits for > 1ms, the mutex enters starvation mode. New goroutines trying to acquire the lock will not spin; they will immediately queue up at the tail. The lock is handed off directly from the unlocking goroutine to the first waiter.

### 2. `sync.RWMutex`
Optimized for Read-Heavy workloads.
*   **RLock:** Atomically increments `readerCount`. If `readerCount < 0`, a writer is waiting, so the reader parks.
*   **Lock:** Atomically subtracts `1 << 30` from `readerCount`. This makes it negative (signaling new readers to wait). It then waits for existing readers to finish.
*   *Warning:* RWMutex has massive cache-contention overhead on the `readerCount` variable across multiple CPU cores. For tiny critical sections, `sync.Mutex` is often faster!

### 3. `sync/atomic`
Bypasses the Go scheduler entirely. Maps directly to CPU hardware instructions (e.g., `LOCK CMPXCHG` on x86).
*   No context switches.
*   Use for lock-free queues, global counters, and state flags.

---

## SECTION 8: ADVANCED PATTERNS

### 1. The Worker Pool
Limits concurrency to prevent resource exhaustion (e.g., max DB connections).

```go
func WorkerPool(numWorkers int, jobs <-chan Job, results chan<- Result) {
    var wg sync.WaitGroup
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := range jobs {
                results <- process(j)
            }
        }()
    }
    wg.Wait()
    close(results)
}
```

### 2. The Semaphore Pattern
Using a buffered channel to limit active goroutines natively.

```go
sem := make(chan struct{}, 10) // Max 10 concurrent tasks
for _, task := range tasks {
    sem <- struct{}{} // Block if 10 are running
    go func(t Task) {
        defer func() { <-sem }() // Release slot
        process(t)
    }(task)
}
```

### 3. Graceful Shutdown (Context Cancellation Tree)
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

go func() {
    select {
    case <-ctx.Done():
        fmt.Println("Shutting down cleanly:", ctx.Err())
        return
    case res := <-doWork():
        fmt.Println("Done:", res)
    }
}()
```

---

## SECTION 9: PITFALLS & DEBUGGING

### 1. Goroutine Leaks
A goroutine blocked on a channel that will never be written to/read from stays in memory forever. The 2KB stack + the `hchan` references mean the Garbage Collector cannot clean it up.
**Fix:** Always use `context.Context` to signal cancellation.

### 2. The Loop Variable Trap (Pre Go 1.22)
```go
for _, val := range values {
    go func() { fmt.Println(val) }() // Prints the last element repeatedly!
}
```
**Fix:** Pass `val` as an argument `go func(v int) { ... }(val)` or upgrade to Go 1.22+ where loop variables are rescoped per iteration.

### 3. Profiling (`pprof`)
When production CPU hits 100%:
1. Import `_ "net/http/pprof"`
2. Access `http://localhost:8080/debug/pprof/goroutine?debug=2` to see a full stack trace of every running goroutine.
3. Access `/debug/pprof/profile` to get a CPU flame graph.
4. Access `/debug/pprof/block` to see where goroutines are waiting on Mutexes/Channels.

---

## SECTION 10: INTERVIEW MASTERCLASS

### Q1: Can a Go program with `GOMAXPROCS=1` run concurrently? Can it run in parallel?
**Answer:** It can run concurrently, but NEVER in parallel. The single `P` will context switch between multiple `G`s, simulating concurrency, but physical parallelism is impossible on 1 logical core.

### Q2: What happens if you close an already closed channel? Write to a closed channel? Read from a closed channel?
**Answer:**
*   Close a closed chan -> **Panic**
*   Write to closed chan -> **Panic**
*   Read from closed chan -> Returns the zero value of the type immediately and a `false` boolean (`val, ok := <-ch`).

### Q3: How does the Go runtime handle a goroutine that does an infinite `for{}` loop of math?
**Answer:** In older versions, it starved the core. In Go 1.14+, the `sysmon` background thread detects a goroutine running > 10ms and sends a `SIGURG` signal to the OS thread. The runtime intercepts the signal, halts the goroutine, saves its registers, and schedules the next one.

### Q4: Why is `sync.RWMutex` sometimes slower than `sync.Mutex`?
**Answer:** Cache Line Bouncing. When 16 CPU cores all try to acquire an `RLock`, they all must atomically increment the `readerCount` integer. This causes the hardware cache line containing `readerCount` to bounce between CPU L1 caches, stalling the CPU memory bus. A standard `Mutex` just spins or parks.

---

## CONCLUSION

You have reached the end of the Go Concurrency Bible. 

To achieve Staff-level mastery, your next steps are to read the actual Go runtime source code:
1. `src/runtime/proc.go` (The Scheduler)
2. `src/runtime/chan.go` (Channels)
3. `src/runtime/mgc.go` (The Garbage Collector)

Never guess about performance. Always benchmark. Always trace. Use `go tool trace` to see the scheduler making decisions in microseconds. Write robust, CSP-compliant systems, and protect shared state flawlessly.
