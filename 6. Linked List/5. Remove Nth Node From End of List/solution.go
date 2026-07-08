/**
 * Definition for singly-linked list.
 * type ListNode struct {
 *     Val int
 *     Next *ListNode
 * }
 */
func removeNthFromEnd(head *ListNode, n int) *ListNode {
	dummy := &ListNode{Val: -1, Next: head}
	temp := head
	for n > 0 {
		temp = temp.Next
		n--
	}
	for temp != nil {
		dummy = dummy.Next
		temp = temp.Next
	}
	if dummy.Next == head {
		return head.Next
	}
	dummy.Next = dummy.Next.Next
	return head

}