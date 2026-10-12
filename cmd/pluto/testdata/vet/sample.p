package sample

import "example.com/audit/dependency"

func unused() { dependency.Raw() }

func Wrapper() { unused() }

func init() { panic("audit must never execute this") }
