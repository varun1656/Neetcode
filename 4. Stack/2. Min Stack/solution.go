type MinStack struct {
	mainStack []int
	minStack  []int
}

func Constructor() MinStack {
	return MinStack{mainStack: make([]int, 0), minStack: make([]int, 0)}
}

func (this *MinStack) Push(value int) {
	this.mainStack = append(this.mainStack, value)
	if len(this.minStack) == 0 || (len(this.minStack) >= 1 && this.minStack[len(this.minStack)-1] >= value) {
		this.minStack = append(this.minStack, value)
	}
}

func (this *MinStack) Pop() {
	if this.mainStack[len(this.mainStack)-1] == this.minStack[len(this.minStack)-1] {
		this.minStack = this.minStack[:len(this.minStack)-1]
	}
	this.mainStack = this.mainStack[:len(this.mainStack)-1]
}

func (this *MinStack) Top() int {
	return this.mainStack[len(this.mainStack)-1]
}

func (this *MinStack) GetMin() int {
	return this.minStack[len(this.minStack)-1]
}

/**
 * Your MinStack object will be instantiated and called as such:
 * obj := Constructor();
 * obj.Push(value);
 * obj.Pop();
 * param_3 := obj.Top();
 * param_4 := obj.GetMin();
 */