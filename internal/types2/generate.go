// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package types2

// Go's go/types carries a go:generate directive here that runs
// generate_test.go to copy files over from cmd/compile's types2. This
// package is a fork maintained by hand: none of its files are generated
// from another source, so it has no generator and no directive. The
// error code strings are generated in the errors subpackage.
