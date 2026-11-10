package main

import "m/sys"

func keyed() sys.File { return sys.File{Name: "a"} }

func unkeyed() sys.File { return sys.File{"a", 3} }

func pointer() *sys.File { return &sys.File{Name: "b"} }
