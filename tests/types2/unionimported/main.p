package main

import "m/dev"

func f(s dev.State, r dev.R[int]) int {
	s = dev.State.On(1)
	switch v := s.(type) {
	case On:
		return v
	case dev.State.Off:
	default:
	}
	if v, ok := r.(dev.R[int].Ok); ok {
		return v
	}
	return 0
}
