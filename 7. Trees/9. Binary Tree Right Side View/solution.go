/**
 * Definition for a binary tree node.
 * type TreeNode struct {
 *     Val int
 *     Left *TreeNode
 *     Right *TreeNode
 * }
 */
func rightSideView(root *TreeNode) []int {
	if root == nil {
		return []int{}
	}
	res := []int{}
	q := []*TreeNode{}
	q = append(q, root)
	for len(q) > 0 {
		qLen := len(q)
		for qLen > 0 {
			top := q[0]
			q = q[1:]
			if top.Left != nil {
				q = append(q, top.Left)
			}
			if top.Right != nil {
				q = append(q, top.Right)
			}
			if qLen == 1 {
				res = append(res, top.Val)
			}
			qLen--
		}
	}
	return res
}