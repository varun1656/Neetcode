package main

type Node struct {
	data int
	next *Node
}

func NewNode(data int) *Node {
	return &Node{data: data, next: nil}
}

type LinkedList struct {
	head *Node
}

func (l *LinkedList) addNode(data int) {
	newNode := NewNode(data)
	if l.head == nil {
		l.head = newNode
		return
	}

	temp := l.head
	for temp.next != nil {
		temp = temp.next
	}
	temp.next = newNode
}

func mergeTwoNodes(head1 *Node, head2 *Node) *Node {
	if head1 == nil {
		return head2
	} else if head2 == nil {
		return head1
	}

	var temp *Node
	var newHead *Node
	for head1 != nil && head2 != nil {
		if temp == nil {
			if head1.data < head2.data {
				newHead = head1
				head1 = head1.next
			} else {
				newHead = head2
				head2 = head2.next
			}
			temp = newHead
		} else {
			if head1.data < head2.data {
				temp.next = head1
				head1 = head1.next
			} else {
				temp.next = head2
				head2 = head2.next
			}
			temp = temp.next
		}

	}
	if head1 != nil {
		temp.next = head1
	} else if head2 != nil {
		temp.next = head2
	}
	return newHead
}
