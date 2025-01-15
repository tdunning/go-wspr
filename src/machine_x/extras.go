package machine_x

//go:inline
func boolToBit(a bool) uint32 {
	if a {
		return 1
	}
	return 0
}

//go:inline
func u32max(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
