package main

type Node struct {
	Val    int
	Next   *Node
	Random *Node
}

func NewNode(data int) *Node {
	return &Node{Val: data, Next: nil, Random: nil}
}

func copyRandomList(head *Node) *Node {
	if head == nil {
		return head
	}
	temp := head
	for temp != nil {
		next := temp.Next
		temp.Next = NewNode(temp.Val)
		temp.Next.Next = next
		temp = temp.Next.Next
	}
	temp = head
	for temp != nil {
		if temp.Random != nil {
			temp.Next.Random = temp.Random.Next
		}
		temp = temp.Next.Next
	}
	NewHead := head.Next
	for head != nil {
		temp = head.Next
		head.Next = head.Next.Next
		if temp.Next != nil {
			temp.Next = temp.Next.Next
		}
		head = head.Next
	}
	return NewHead
}
