package main

import "m/dev"

func f(s dev.State, r dev.R[int]) int {
	s = dev.State.On(1)
	s = dev.State.Off
	switch v := s.(type) {
	case On:
		return v
	case dev.State.Off:
	default:
	}
	if v, ok := r.(dev.R[int].Ok); ok {
		return v
	}
	_ = dev.R[string].Ok{}
	return 0
}
