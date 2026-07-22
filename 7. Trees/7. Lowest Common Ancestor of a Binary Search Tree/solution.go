/**
 * Definition for a binary tree node.
 * type TreeNode struct {
 *     Val   int
 *     Left  *TreeNode
 *     Right *TreeNode
 * }
 */

func lowestCommonAncestor(root, p, q *TreeNode) *TreeNode {
	if p.Val > q.Val {
		p, q = q, p
	}
	curr := root
	for curr != nil {
		if curr.Val == p.Val || curr.Val == q.Val {
			return curr
		} else if p.Val < curr.Val && q.Val > curr.Val {
			return curr
		} else if p.Val < curr.Val && q.Val < curr.Val {
			curr = curr.Left
		} else {
			curr = curr.Right
		}
	}
	return curr
}

