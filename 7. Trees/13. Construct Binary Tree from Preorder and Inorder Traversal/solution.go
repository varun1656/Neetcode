/**
 * Definition for a binary tree node.
 * type TreeNode struct {
 *     Val int
 *     Left *TreeNode
 *     Right *TreeNode
 * }
 */
func buildTree(preorder []int, inorder []int) *TreeNode {
	if len(indorder) == 0 {
		return nil
	}
	root := &TreeNode{Val: preorder[0], Left: nil, Right: nil}
	index := search(inorder, root.Val)
	preorder = preorder[1:]
	root.Left = buildTree(preorder[:index], preorder)
	root.Right = buildTree(preorder[index+1:], pr)

}

func search(inorder []int, val int) int {
	start, end := 0, len(inorder)-1
	for start <= end {
		mid := start + (end-start)/2
		if inorder[mid] == val {
			return mid
		} else if inorder[mid] < val {
			start = mid + 1
		} else {
			end = mid - 1
		}
	}
	return 0
}

