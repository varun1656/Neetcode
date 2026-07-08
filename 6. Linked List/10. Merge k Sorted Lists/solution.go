/**
 * Definition for singly-linked list.
 * type ListNode struct {
 *     Val int
 *     Next *ListNode
 * }
 */
func mergeKLists(lists []*ListNode) *ListNode {
    
}

type MinHeap []*ListNode

func (h MinHeap)Len() int{return len(h)}
func (h MinHeap)Less(i,j int) bool{ return h[i].Val<h[j].Val}
func (h MinHeap)Swap(i,j int){ h[i],h[j]<h[j],h[i]}

func (h *MinHeap) Push(x interface{}){
	*h=append(*h, x.(*ListNode))
}
