// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2024 The Ebitengine Authors

//go:build android || ios

package pure

// Android and iOS are not supported by this vendored copy of pure: their code
// paths, build tags and the iOS CGO guard were removed (see the README
// "Supported Platforms").
//
// This undefined reference fails the build on those GOOSes on purpose. Go
// treats android as satisfying `linux` and ios as satisfying `darwin`, so
// without it the remaining Linux/Darwin files would compile there against the
// wrong dlfcn constants instead of reporting the missing support.
var _ = _PURE_DOES_NOT_SUPPORT_ANDROID_OR_IOS
