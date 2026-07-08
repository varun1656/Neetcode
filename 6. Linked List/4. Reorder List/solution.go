package main

type Node struct {
	val  int
	next *Node
}

func NewNode(data int) *Node {
	return &Node{val: data, next: nil}
}

type LinkedList struct {
	head *Node
}

func (l *LinkedList) AddNode(data int) {
	if l.head == nil {
		l.head = NewNode(data)
		return
	}
	temp := l.head
	for temp.next != nil {
		temp = temp.next
	}
	temp.next = NewNode(data)
}

func (l *LinkedList) Reorder() {
	if l.head == nil || l.head.next == nil {
		return
	}
	fast, slow := l.head.next.next, l.head.next
	for fast!= nil && fast.next != nil {
		fast = fast.next.next
		slow = slow.next
	}
	head1 := l.head
	head2 := slow.next
	slow.next = nil

	head2 = reverse(head2)
	l.head = merge(head1, head2)
}

func reverse(head *Node) *Node {
	if head == nil || head.next == nil {
		return head
	}
	prev := (*Node)nil
	curr := head
	for curr != nil {
		next := curr.next
		curr.next = prev
		prev = curr
		curr = next
	}
	return prev
}

func merge(head1, head2 *Node) *Node {
	if head1 == nil {
		return head2
	}
	if head2 == nil {
		return head1
	}
	temp := head1
	var temp1 *Node
	var temp2 *Node
	for head1 != nil && head2 != nil {
		temp1 = head1.next
		head1.next = head2
		temp2 = head2.next
		head2.next = temp1
		head1 = temp1
		head2 = temp2
	}
	return temp
}
