/**
 * Definition for a binary tree node.
 * type TreeNode struct {
 *     Val int
 *     Left *TreeNode
 *     Right *TreeNode
 * }
 */
func isValidBST(root *TreeNode) bool {
	return checkBST(root, math.MinInt, math.MaxInt)
}

func checkBST(root *TreeNode, min, max int) bool {
	if root == nil {
		return true
	}
	if root.Val > min && root.Val < max {
		return checkBST(root.Left, min, root.Val) && checkBST(root.Right, root.Val, max)
	} else {
		return false
	}
}