/**
 * Definition for a binary tree node.
 * type TreeNode struct {
 *     Val int
 *     Left *TreeNode
 *     Right *TreeNode
 * }
 */
func isBalanced(root *TreeNode) bool {
	if root == nil {
		return true
	}
	res := true
	var cal func(root *TreeNode) int
	cal = func(root *TreeNode) int {
		if root == nil || res == false {
			return 0
		}
		left := cal(root.Left)
		right := cal(root.Right)
		if left-right > 1 || left-right < -1 {
			res = false
		}
		return 1 + max(left, right)
	}
	cal(root)
	return res
}

func max(i, j int) int {
	if i < j {
		return j
	}
	return i
}