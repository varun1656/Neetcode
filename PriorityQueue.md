# The Definitive Guide to Priority Queues in Go

Welcome. You’re here because you want to move past the superficial “LeetCode” understanding of Priority Queues (PQs). You don't just want to know *how* to import `container/heap`; you want to know *why* it exists, *how* it lives in memory, and how a Senior/Staff Engineer wields it in production systems. 

Grab a coffee. This document is a deep, foundational, and permanent reference for Priority Queues in Go. We will build your mental models from the ground up, prove the math, inspect memory, and build production-grade implementations.

---

## Section 1: Motivation — Why Do We Need Priority Queues?

### The Problem
Imagine you are building the backend for a hospital triage system. Patients arrive continuously.
If you use a **FIFO Queue** (First-In, First-Out), the guy with a paper cut who arrived at 8:00 AM gets treated before the heart attack victim who arrived at 8:05 AM. That's a system failure.

If you use a **Standard Array/Slice** and sort it every time a new patient arrives, your system grinds to a halt. Sorting an array of $N$ patients takes $O(N \log N)$ time. If thousands of patients arrive per minute, sorting continuously destroys your CPU.

### Real-World Engineering Scenarios
PQs exist to solve the **"Dynamic Top-K"** or **"Highest Priority Next"** problem in environments where data is constantly changing.
1.  **CPU / OS Schedulers:** Linux doesn't run the "oldest" process next. It runs the process with the highest priority (e.g., completely fair scheduler concepts).
2.  **Network Bandwidth Management:** Routing VoIP packets before background file downloads.
3.  **Event-Driven Architectures / Timers:** Node.js, Go's runtime, and Redis all use PQs for delayed tasks. "Execute this task at timestamp X." The task with the lowest timestamp is always at the front.
4.  **Graph Algorithms:** Dijkstra's shortest path and A* search need to constantly explore the "closest" node next.

**The core requirement:** We need a data structure that instantly gives us the most important item ($O(1)$) and allows us to insert new items or remove the most important item extremely fast (better than $O(N)$).

---

## Section 2: What Is A Priority Queue?

### Formal Definition
A Priority Queue is an **Abstract Data Type (ADT)**. It is a *concept*, not a specific implementation. 
A PQ operates exactly like a regular queue, but with one difference: **every element has a "priority" associated with it.** 

*   In a Max-PQ, an element with high priority is served before an element with low priority.
*   In a Min-PQ, an element with low priority (like a low timestamp or cost) is served first.

### Mental Model
Think of a PQ as a VIP line at a nightclub. 
*   Regular people get in line. 
*   A celebrity shows up, they don't go to the back of the line; they bypass the queue based on their "priority status." 
*   When the bouncer lets the next person in, it is *always* the person with the highest priority currently standing outside.

---

## Section 3: Naive Implementations (And Why They Fail)

Before we introduce Heaps, let's prove why we need them by trying to implement a PQ using basic data structures.

### Approach 1: Unsorted Slice `[]int`
We just append new items to the end of a slice.
*   **Insert:** `append(slice, item)` -> **$O(1)$** (Amortized). Very fast!
*   **Remove Max:** We have to scan the *entire* slice to find the maximum element, remove it, and shift elements. -> **$O(N)$**.
*   **Tradeoff:** Writes are fast, reads are brutally slow. Unacceptable for high-throughput systems.

### Approach 2: Sorted Slice `[]int`
We keep the slice sorted at all times.
*   **Insert:** We use binary search to find the insertion point $O(\log N)$, but then we have to shift all elements to the right to make room! -> **$O(N)$**.
*   **Remove Max:** We just pop the last element. -> **$O(1)$**.
*   **Tradeoff:** Reads are fast, writes are brutally slow. Array shifting destroys performance.

### Approach 3: Linked List
We keep a sorted Linked List.
*   **Insert:** We scan the list to find the right spot and insert the node. Pointer manipulation is $O(1)$, but finding the spot takes **$O(N)$**.
*   **Remove Max:** Pop the head node. -> **$O(1)$**.
*   **Tradeoff:** Writes are still $O(N)$. Plus, linked lists have terrible cache locality (pointer chasing across the heap).

### The Bottleneck
We are trapped. We want fast inserts AND fast removals. $O(N)$ is a dealbreaker. We need a structure that gives us **$O(\log N)$ for both operations**.

---

## Section 4: Why Heaps Exist

To beat $O(N)$, we must use a Tree. Specifically, a **Binary Heap**.

A Binary Heap is a specialized tree that gives us exactly what we want by abandoning the idea of being "perfectly sorted." 

### The Insight
We don't actually need the *entire* collection to be perfectly sorted! If we only care about finding the *single maximum element*, sorting the rest of the elements relative to each other is a waste of CPU cycles. 

A Heap maintains a "loose" sorting order called the **Heap Property**. It guarantees the maximum element is always at the top, but the elements below it are only partially ordered. This relaxation of the rules allows us to achieve $O(\log N)$ insertions and deletions.

---

## Section 5: Binary Heap Deep Dive

A Binary Heap is a binary tree that satisfies two strict properties:

### 1. The Shape Property (Complete Binary Tree)
A binary heap must be a **Complete Binary Tree**. This means every level of the tree is completely filled, except possibly the last level, which is filled strictly from left to right.

```text
      VALID HEAP                INVALID HEAP
         [10]                       [10]
        /    \                     /    \
     [8]      [7]               [8]      [7]
    /  \      /                   \      /
  [5]  [4]  [2]                   [4]  [2]
 (Filled left-to-right)      (Missing left child on 8!)
```
*Why?* This guarantees the height of the tree is strictly $\approx \log_2(N)$. No matter what, it never becomes an unbalanced linked list.

### 2. The Heap Property
*   **Max-Heap:** Every parent node is $\ge$ its children. The largest element is the root.
*   **Min-Heap:** Every parent node is $\le$ its children. The smallest element is the root.
*(Note: There is no guaranteed order between siblings! The left child can be bigger or smaller than the right child. We only care about Parent vs. Child).*

---

## Section 6: Array Representation (The Genius of Heaps)

Here is where computer science gets beautiful. We don't use pointers (`*Node`) to build a heap. We use a flat Array/Slice.

Why? 
1. Pointers require memory allocations (Garbage Collection pressure).
2. Pointers fragment memory (Cache misses).
3. Slices are contiguous in memory (CPU Cache Prefetcher goes brrr!).

Because the tree is **Complete** (filled left to right), we can map the tree into an array without any empty gaps!

### Mathematical Derivation of Indices

Let's place the root at index `0`.

```text
Level 0:          [100]                  Index: 0
                 /     \
Level 1:      [50]      [40]             Index: 1, 2
             /   \      /   \
Level 2:   [30] [20]  [10]  [5]          Index: 3, 4, 5, 6

Array: [ 100,  50,  40,  30,  20,  10,   5 ]
Index:     0    1    2    3    4    5    6
```

**Deriving the Left Child Formula:**
*   Level $d$ has $2^d$ nodes.
*   The first node of Level $d$ is at index $2^d - 1$.
*   Let our parent node be the $k$-th node on Level $d$. Its array index is $i = (2^d - 1) + k$.
*   Its left child will be on Level $d+1$. Because every node has 2 children, the left child of the $k$-th node will be the $2k$-th node on Level $d+1$.
*   Index of left child = (First node of Level $d+1$) + $2k$
*   Left Child Index = $(2^{d+1} - 1) + 2k$
*   Left Child Index = $2(2^d) - 1 + 2k = 2(2^d - 1 + k) + 1$
*   Substitute $i$: **Left Child = $2i + 1$**.

Boom. By pure math, we know exactly where children and parents live without a single pointer.

**The Golden Formulas (0-indexed array):**
*   **Left Child:** `2*i + 1`
*   **Right Child:** `2*i + 2`
*   **Parent:** `(i - 1) / 2` (Integer division floors it automatically)

*Senior Takeaway:* This math is why heaps are insanely fast. Moving through the tree is just CPU bit-shifting and addition. Memory is loaded linearly into the L1 cache.

---

## Section 7: Heap Operations

Let's look at the mechanics of a **Max-Heap**.

### 1. Insert: `Push()`
**Goal:** Add a new element while maintaining the Shape and Heap properties.

**Algorithm:**
1. Append the new element to the end of the array (maintains Shape property).
2. **"Bubble Up" (or Swim):** Compare the new element to its parent. If it's larger than its parent, swap them. Repeat until it's smaller than its parent or it becomes the root.

**Diagram:** Insert `90` into the heap.
```text
1. Append to end:
        [100]
       /     \
    [50]     [40]
   /    \    
[30]   [90]   <-- Appended here (Index 4)

2. Bubble Up: Compare 90 with parent (50). 90 > 50, so SWAP.
        [100]
       /     \
    [90]     [40]
   /    \    
[30]   [50]   <-- 90 moved up!

3. Compare 90 with parent (100). 90 < 100. Stop.
```
**Complexity:** Tree height is $\log N$. At most, we swap $\log N$ times. -> **$O(\log N)$**.

### 2. Remove Max: `Pop()`
**Goal:** Remove the root (the maximum) and restore properties.

**Algorithm:**
1. You can't just delete the root, the tree would split in half!
2. Swap the root with the **last element** in the array.
3. Pop the last element off the array (this is the original max).
4. **"Bubble Down" (or Sink):** The new root is probably very small and violates the heap property. Compare it with its two children. Swap it with the *larger* of the two children. Repeat until it is larger than both children or it hits the bottom.

**Diagram:** Pop from the heap.
```text
Initial:
        [100]
       /     \
    [90]     [40]
   /    
[30]   

1. Swap root with last element (30):
        [30]
       /     \
    [90]     [40]
   /    
[100]  <-- Now at the end. Pop it off! Return 100.

2. Bubble Down 30: Compare with children (90 and 40). 90 is largest. Swap 30 and 90.
        [90]
       /     \
    [30]     [40]

3. 30 has no children. Stop.
```
**Complexity:** At most, we swap from root to leaf, which is $\log N$ levels. -> **$O(\log N)$**.

### 3. Peek
Return the element at index `0`. -> **$O(1)$**.

---

## Section 8: Heapify (Building a Heap)

Suppose you are given an unsorted array: `[30, 20, 10, 50, 60]`.
How do you turn it into a valid heap?

**Naive way:** Create an empty heap, and call `Push()` $N$ times. 
Cost: $N$ inserts $\times O(\log N)$ per insert = **$O(N \log N)$**.

**The Genius way (Heapify / Floyd's Method):**
We can do this in **$O(N)$** time!

**Algorithm:**
1. Treat the unsorted array as a complete binary tree (it won't satisfy the heap property yet).
2. The leaves of the tree have no children, so they are already valid "sub-heaps" of size 1.
3. Start at the last non-leaf node (`N/2 - 1`).
4. Call **Bubble Down** on that node.
5. Work your way backwards to index `0`, calling Bubble Down on each node.

**Why is this $O(N)$? (The Proof)**
Most nodes in a binary tree are at the bottom. 
*   Half the nodes ($N/2$) are leaves. They move $0$ steps.
*   A quarter of the nodes ($N/4$) are 1 level above leaves. They move at most $1$ step.
*   One node (the root) moves at most $\log N$ steps.
Mathematically, the total work is: $\sum_{h=0}^{\log N} \frac{N}{2^{h+1}} \times h$. 
This is a converging geometric series. The sum converges strictly to $O(N)$. 

*Senior Takeaway:* NEVER build a heap by calling `Push` repeatedly if you already have the dataset. Always use `heap.Init()`, which uses the $O(N)$ Heapify algorithm.

---

## Section 9: Complexity Analysis

| Operation | Time Complexity | Auxiliary Space |
| :--- | :--- | :--- |
| **Peek (Find Max/Min)** | $O(1)$ | $O(1)$ |
| **Push (Insert)** | $O(\log N)$ | $O(1)$ |
| **Pop (Remove Max/Min)** | $O(\log N)$ | $O(1)$ |
| **Build Heap (Heapify)** | $O(N)$ | $O(1)$ in-place |
| **Search (Find arbitrary)** | $O(N)$ | $O(1)$ |

*Note:* PQs are NOT meant for searching. Finding if a specific element exists takes $O(N)$ because the tree is not fully sorted.

---

## Section 10: Go's `container/heap` Package

Go does not have a `PriorityQueue` struct. Instead, Go takes an **interface-driven** approach.
The `container/heap` package provides functions (`heap.Init`, `heap.Push`, `heap.Pop`) that operate on *any* type that implements `heap.Interface`.

```go
type Interface interface {
	sort.Interface // Embedded interface! Requires Len(), Less(i, j int), Swap(i, j int)
	Push(x any)    // Add x as element Len()
	Pop() any      // Remove and return element Len() - 1.
}
```

### Why this design?
1. **Zero Allocations (Sometimes):** By operating directly on your slice, the package doesn't need to allocate wrapper nodes.
2. **Flexibility:** You can make a heap out of structs, integers, or custom objects. 
3. **Min vs Max:** The `Less(i, j)` function controls the priority.
   * `a[i] < a[j]` creates a **Min-Heap**.
   * `a[i] > a[j]` creates a **Max-Heap**.

### The Pointer Receiver Trap
Why do you write `Push(x any)` with a **Pointer Receiver** (`*MyHeap`) but `Less` with a **Value Receiver** (`MyHeap`)?

When you `Push`, you call `*h = append(*h, x)`. You are modifying the slice's length and potentially reallocating its backing array. If you don't use a pointer receiver, you will modify a *copy* of the slice header, and the caller will never see the appended item!
`Less`, `Len`, and `Swap` only read or modify existing slots in the backing array, so a value receiver is fine (slice headers point to the same backing array).

---

## Section 11: Reading Go Source Code (Mental Walkthrough)

What exactly happens when you call `heap.Push(h, x)`?
1. **`heap.Push`** calls YOUR `h.Push(x)` to append the element to the very end of your slice.
2. It calculates the index of the newly added item (`n - 1`).
3. It calls an internal function `up(h, n-1)`.
4. **`up`** runs a `for` loop. It calculates `parent = (child - 1) / 2`. 
5. It calls YOUR `h.Less(child, parent)`. If true, it calls YOUR `h.Swap(child, parent)` and continues up the tree.

What happens during `heap.Pop(h)`?
1. It calls YOUR `h.Swap(0, n-1)`. (Swaps root with the last element).
2. It calls internal `down(h, 0, n-1)`.
3. **`down`** compares the new root with its children using `h.Less`. It swaps it downwards until the heap property is restored.
4. Finally, it calls YOUR `h.Pop()` to slice off the last element and return it to you.

*Insight:* The `container/heap` package is literally just executing the Bubble Up and Bubble Down logic for you, using your interface methods to interact with your custom data structure!

---

## Section 12: Build Priority Queue From Scratch (Standard Go)

Let's build a standard Min-Heap of Integers.

```go
import "container/heap"

// 1. Define the type
type IntHeap []int

// 2. Implement sort.Interface
func (h IntHeap) Len() int           { return len(h) }
func (h IntHeap) Less(i, j int) bool { return h[i] < h[j] } // Min-Heap
func (h IntHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

// 3. Implement heap.Interface
// MUST use pointer receivers to modify slice length
func (h *IntHeap) Push(x any) {
	*h = append(*h, x.(int))
}

func (h *IntHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]     // Get the last item
	*h = old[0 : n-1] // Shrink the slice
	return x
}

// Usage:
func main() {
	h := &IntHeap{2, 1, 5}
	heap.Init(h)          // O(N) Heapify
	heap.Push(h, 3)       // O(log N) Insert
	fmt.Printf("minimum: %d\n", (*h)[0]) // O(1) Peek
	for h.Len() > 0 {
		fmt.Printf("%d ", heap.Pop(h)) // O(log N) Removal
	}
}
```

---

## Section 13: Generic Priority Queue (Go 1.18+)

The standard `container/heap` relies on `interface{}` (`any`). 
**The Problem:** Passing an `int` into `Push(x any)` forces the compiler to allocate that `int` on the Heap (Escape Analysis) so it can be wrapped in an interface. If you do this 10 million times, the Garbage Collector will destroy your performance.

Let's build a **Generic Priority Queue** that avoids `container/heap` completely, granting zero allocations and maximum type safety.

```go
type PriorityQueue[T any] struct {
	items []T
	less  func(a, b T) bool // Comparator function
}

func NewPriorityQueue[T any](less func(a, b T) bool) *PriorityQueue[T] {
	return &PriorityQueue[T]{less: less}
}

func (pq *PriorityQueue[T]) Push(item T) {
	pq.items = append(pq.items, item)
	pq.up(len(pq.items) - 1)
}

func (pq *PriorityQueue[T]) Pop() T {
	n := len(pq.items) - 1
	pq.items[0], pq.items[n] = pq.items[n], pq.items[0] // Swap root and last
	item := pq.items[n]
	pq.items = pq.items[:n] // Shrink
	pq.down(0)              // Restore heap property
	return item
}

// The internal Bubble Up logic
func (pq *PriorityQueue[T]) up(j int) {
	for {
		i := (j - 1) / 2 // parent
		if i == j || !pq.less(pq.items[j], pq.items[i]) {
			break
		}
		pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
		j = i
	}
}

// The internal Bubble Down logic
func (pq *PriorityQueue[T]) down(i int) {
	n := len(pq.items)
	for {
		left := 2*i + 1
		if left >= n || left < 0 { // < 0 catches int overflow
			break
		}
		j := left // j is the child we might swap with
		if right := left + 1; right < n && pq.less(pq.items[right], pq.items[left]) {
			j = right // right child is smaller, prefer swapping with it
		}
		if !pq.less(pq.items[j], pq.items[i]) {
			break // parent is smaller than both children, we are done
		}
		pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
		i = j
	}
}
```
*Senior Perspective:* We just eliminated all interface allocations. This generic PQ will run circles around `container/heap` in high-throughput microservices.

---

## Section 14: Mutable Priority Queue (Decrease Key)

Standard PQs don't let you change an item's priority once it's in the queue. But algorithms like **Dijkstra's Shortest Path** *require* you to update node distances!

To fix this, we need a **Mutable Priority Queue**. 
1. We must add an `index` field to our items so we know where they live in the array.
2. We must maintain that `index` during `Swap`.
3. We use `heap.Fix(h, index)` which efficiently calls `down` or `up` to restore the heap property.

```go
type Item struct {
	value    string
	priority int
	index    int // Required to use heap.Fix
}

type PriorityQueue []*Item

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i // Update indices upon swap!
	pq[j].index = j
}

// ... Implement Len, Less, Push, Pop ...

// How to mutate an item:
func (pq *PriorityQueue) Update(item *Item, value string, priority int) {
	item.value = value
	item.priority = priority
	heap.Fix(pq, item.index) // O(log N) restructuring!
}
```
*Mental Model:* `heap.Fix` looks at the item. If the priority increased, it tries to bubble it up. If it decreased, it tries to bubble it down. It is much faster than `Remove` + `Push`.

---

## Section 15: Memory Layout Deep Dive

Why do Staff Engineers love Heaps over Trees?

If you implement a standard Binary Search Tree using `*Node` pointers:
```text
Node A (Heap memory 0x100) -> Left Node B (Heap memory 0x8F0), Right Node C (Heap memory 0x2A0)
```
When the CPU tries to traverse this tree, it has to fetch memory from random locations. This causes **CPU Cache Misses**. Fetching from main memory takes ~100ns.

A Binary Heap uses a single Slice `[]int`.
```text
Slice Header: { DataPtr: 0xA00, Len: 7, Cap: 8 }
Memory at 0xA00: [ 100, 50, 40, 30, 20, 10, 5 ]
```
When the CPU reads `100`, the hardware prefetcher automatically pulls `50, 40, 30...` into the L1 Cache. Reading from L1 Cache takes ~1ns. 
**The Array-based heap is practically 100x faster purely due to hardware mechanical sympathy.**

---

## Section 16: Concurrency Considerations

**Are Heaps thread-safe?** NO.
If Goroutine A calls `heap.Push` while Goroutine B calls `heap.Pop`, they will trigger a race condition, corrupt the backing array, and crash your program.

### Solution 1: Mutex Lock (Simple)
Wrap your Push and Pop in a `sync.Mutex`.
*Tradeoff:* High contention. If you have 10,000 workers trying to push tasks, they will block each other.

### Solution 2: Channel-Driven Heap Manager (Production Standard)
Create a single dedicated Goroutine that "owns" the heap. All other goroutines send tasks to it via a channel.

```go
type Task struct { id int; priority int }

func HeapManager(incoming <-chan Task) {
    pq := make(PriorityQueue, 0)
    heap.Init(&pq)
    
    for task := range incoming {
        heap.Push(&pq, task)
        // Check if top task is ready to be executed, etc.
    }
}
```
This entirely eliminates locks, respects Go's "share memory by communicating" philosophy, and prevents race conditions.

---

## Section 17: Real Production Systems

How are PQs used in the real world?

**1. Kubernetes Pod Scheduling:**
K8s has an active scheduling queue. Pods are placed in a Priority Queue based on their priority class (e.g., `system-cluster-critical` vs `user-workload`). The scheduler pops the highest priority pod and tries to find a node for it.

**2. Go's `time` package (Timers):**
When you call `time.Sleep(1 * time.Second)`, Go doesn't block an OS thread. It creates a timer object and puts it into a Priority Queue (a 4-ary heap in the Go runtime), sorted by expiration time. A background goroutine checks the top of the heap, sleeps until that exact timestamp, pops it, and wakes your goroutine up!

**3. Kafka / Message Brokers:**
Delayed message delivery uses PQs to hold messages until their delivery timestamp is reached.

---

## Section 18: Algorithms That Depend On Priority Queues

1. **Dijkstra's Shortest Path:** Uses a Min-PQ to always process the nearest unvisited graph node first. Requires a Mutable PQ (`heap.Fix`) to update distances when shorter paths are found.
2. **A* Search Algorithm:** Used in game pathfinding. PQ sorted by `cost_so_far + heuristic_guess`.
3. **Merge K Sorted Lists:** Push the head of all K lists into a Min-PQ. Pop the minimum, add to result, and push the next element from that specific list into the PQ. Time: $O(N \log K)$.
4. **Top K Frequent Elements:** Maintain a Min-PQ of size K. Iterate over elements. If an element is larger than the root (the smallest of the top K), pop the root and push the new element. Memory: $O(K)$. Time: $O(N \log K)$.

---

## Section 19: Common Bugs & Pitfalls

1. **Forgetting pointer receivers:** Writing `func (h MyHeap) Push()` instead of `*MyHeap`. The slice length won't update in the caller.
2. **Mixing up `Less`:** Writing `h[i] > h[j]` for a Min-Heap. (It creates a Max-Heap!).
3. **Modifying items directly:** Changing an item's priority without calling `heap.Fix`. The heap invariants silently break, and subsequent `Pop`s return incorrect data.
4. **Aliasing during `Pop`:** 
   ```go
   x := old[n-1]
   *h = old[0:n-1] 
   // Memory Leak Warning! If x is a pointer, it stays in the backing array!
   ```
   *Fix:* Explicitly nil out the array slot before slicing to allow Garbage Collection:
   ```go
   old[n-1] = nil // Avoid memory leak!
   *h = old[0:n-1]
   ```

---

## Section 20: Senior Engineer Perspective

When I review code containing a Priority Queue, here is what I look for:

1. **Allocation Churn:** Are you pushing a massive struct by value? `heap.Push(h, BigStruct{})` creates a massive copy. Change your slice to hold pointers `[]*BigStruct`, OR use Generics.
2. **Capacity Pre-allocation:** Did you `make(pq, 0)`? If you know 10,000 items are coming, `make(pq, 0, 10000)` saves 14 array reallocations and copies!
3. **Backpressure:** If producers push to the PQ faster than consumers pop, your memory will OOM (Out of Memory). You must implement bounded capacity or rate-limiting on your PQ.

---

## Section 21: Interview Preparation

### The Classic Interview Question
*"Find the median in a stream of incoming numbers."*

**The Answer:** Use TWO Priority Queues!
1. A **Max-Heap** to store the smaller half of the numbers.
2. A **Min-Heap** to store the larger half of the numbers.
*Algorithm:* 
- Insert into Max-Heap. 
- Pop Max-Heap and Push to Min-Heap.
- Balance them: If Min-Heap size > Max-Heap size, pop Min and push to Max.
- *Median:* If sizes are equal, average the two roots. If Max-Heap is larger, the root of Max-Heap is the median. Both operations are $O(\log N)$ inserts and $O(1)$ reads!

### Follow-up Questions
*   *Why not a Binary Search Tree (BST)?* BSTs take $O(\log N)$ to find the max, but require overhead to maintain balance (Red-Black / AVL trees) and have poor cache locality. Heaps are perfectly flat arrays.

---

## Section 22: Practice Exercises

**Easy:**
1. Implement a Min-Heap from scratch using a slice.
2. Solve "Kth Largest Element in an Array" using `container/heap`.

**Medium:**
3. Solve "Merge K Sorted Lists".
4. Solve "Task Scheduler" (LeetCode 621) using a Max-Heap.

**Hard:**
5. Solve "Find Median from Data Stream" using Two Heaps.
6. Implement a thread-safe, generic Priority Queue with a maximum capacity. If it hits capacity, pushing blocks until space frees up.

---

## Section 23: Cheat Sheet

### Decision Tree
*   Do I need the absolute max/min constantly? -> **Priority Queue**
*   Do I need exact ordering of ALL elements? -> **Sorted Array / BST**
*   Do I need to check if an element exists fast? -> **Hash Map**
*   Do I need fast inserts, removals, AND fast existence checks? -> **Hash Map + Priority Queue (Mutable PQ)**

### Complexity Summary
*   **Insert:** $O(\log N)$
*   **Remove Max/Min:** $O(\log N)$
*   **Get Max/Min:** $O(1)$
*   **Heapify (Array to Heap):** $O(N)$
*   **Memory:** $O(1)$ overhead (in-place array)

### Go Implementation Checklist
1. `type MyHeap []Type`
2. `Len() int`
3. `Less(i, j int) bool` (Use `<` for Min, `>` for Max)
4. `Swap(i, j int)`
5. `Push(x any)` -> **Pointer receiver!** Append to `*h`.
6. `Pop() any` -> **Pointer receiver!** Read `n-1`, Nil it out, Reslice `*h`, Return.
7. Always initialize with `heap.Init(&h)`.

Keep this guide close. You now know more about Priority Queues than 95% of engineers. Build something fast.
