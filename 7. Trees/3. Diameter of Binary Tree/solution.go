package main

type TreeNode struct {
	Val   int
	Left  *TreeNode
	Right *TreeNode
}

func diameterOfBinaryTree(root *TreeNode) int {
	result := 0
	var cal func(*TreeNode) int
	cal = func(root *TreeNode) int {
		if root == nil {
			return 0
		}
		left := cal(root.Left)
		right := cal(root.Right)
		result = max(result, left+right+1)
		return 1 + max(left, right)
	}
	cal(root)
	if result != 0 {
		return result - 1
	}
	return result
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}
