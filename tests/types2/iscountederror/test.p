package main
type embedded interface { error; Context() string }
type structural interface { Error() string }
type unrelated interface { Error() int }
type owning owner interface { Error() string }
type concrete struct{}
func (*concrete) Error() string { return "error" }
