/**
 * Definition for a binary tree node.
 * type TreeNode struct {
 *     Val int
 *     Left *TreeNode
 *     Right *TreeNode
 * }
 */
func goodNodes(root *TreeNode) int {
	if root == nil {
		return 0
	}
	count := 0
	var calCount func(root *TreeNode, maxVal int)
	calCount = func(root *TreeNode, maxVal int) {
		if root == nil {
			return
		}
		if maxVal <= root.Val {
			count++
		}
		maxVal = max(maxVal, root.Val)
		calCount(root.Left, maxVal)
		calCount(root.Right, maxVal)
	}
	calCount(root, root.Val)
	return count
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}