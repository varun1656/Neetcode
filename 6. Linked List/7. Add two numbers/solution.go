/**
 * Definition for singly-linked list.
 * type ListNode struct {
 *     Val int
 *     Next *ListNode
 * }
 */

func NewNode(data int) *ListNode {
	return &ListNode{Val: data, Next: nil}
}

func addTwoNumbers(l1 *ListNode, l2 *ListNode) *ListNode {
	var result *ListNode
	var res *ListNode
	carry := 0
	for l1 != nil || l2 != nil {
		sum := carry
		if l1 != nil {
			sum = sum + l1.Val
			l1 = l1.Next
		}
		if l2 != nil {
			sum = sum + l2.Val
			l2 = l2.Next
		}
		carry = sum / 10
		sum = sum % 10
		if result == nil {
			result = NewNode(sum)
			res = result
		} else {
			result.Next = NewNode(sum)
			result = result.Next
		}
	}
	if carry != 0 {
		result.Next = NewNode(carry)
	}
	return res
}