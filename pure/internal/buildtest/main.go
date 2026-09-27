// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2024 The Ebitengine Authors

//go:build !windows

package main

import (
	_ "github.com/terva-sh/tuohi/pure"
)

import "C"

// This file tests that build Cgo and pure at the same time succeeds to build (#189).
func main() {
}
