/**
 * Definition for a binary tree node.
 * type TreeNode struct {
 *     Val int
 *     Left *TreeNode
 *     Right *TreeNode
 * }
 */
func levelOrder(root *TreeNode) [][]int {
	if root == nil {
		return [][]int{}
	}
	res := make([][]int, 0)
	q := []*TreeNode{}
	q = append(q, root)
	for len(q) > 0 {
		len_size := len(q)
		temp_res := []int{}
		for len_size > 0 {
			top := q[0]
			q = q[1:]
			if top.Left != nil {
				q = append(q, top.Left)
			}
			if top.Right != nil {
				q = append(q, top.Right)
			}
			temp_res = append(temp_res, top.Val)
			len_size--
		}
		res = append(res, temp_res)
	}
	return res
}