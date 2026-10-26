package main

import "m/dev"

func f(s dev.State) {
	switch s.(type) {
	case dev.State.hidden:
	default:
	}
}