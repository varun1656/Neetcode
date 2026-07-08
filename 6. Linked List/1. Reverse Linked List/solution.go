package main

import "fmt"

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

func (n *LinkedList) addNode(data int) {
	if n.head == nil {
		n.head = NewNode(data)
		return
	}
	temp := n.head
	for temp.next != nil {
		temp = temp.next
	}
	temp.next = NewNode(data)
}

func (n *LinkedList) print() {
	temp := n.head
	for temp != nil {
		fmt.Println(temp.data)
		temp=temp.next
	}
}

func (n *LinkedList) reverseIterative() {
	if n.head == nil || n.head.next == nil {
		return
	}
	curr := n.head
	prev := (*Node)nil
	for curr != nil {
		next:=curr.next
		curr.next = prev
		prev = curr
		curr = next
	}
	n.head=prev
}

func (n *LinkedList) reverseRecursively(){
	if n.head == nil || n.head.next == nil {
		return
	}
	
	var reverse func(temp *Node) *Node

	reverse=func(temp *Node) *Node{
		if temp.next==nil{
			n.head=temp
			return temp
		}

		nextNode:=reverse(temp.next)
		nextNode.next=temp
		return temp
	}
	reverse(n.head).next=nil
}
