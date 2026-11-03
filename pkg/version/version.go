package version

import "runtime"

// Version is the current version of the SIPREC server
var Version = "1.3.4"

// Commit and BuildTime are set by release builds using linker flags.
var Commit = "unknown"
var BuildTime = "unknown"

// BuildInfo reports the artifact identity and native codec capability.
type BuildInfo struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuildTime    string `json:"build_time"`
	GoVersion    string `json:"go_version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NativeCodecs bool   `json:"native_codecs"`
}

func Info() BuildInfo {
	return BuildInfo{Version: Version, Commit: Commit, BuildTime: BuildTime, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, NativeCodecs: nativeCodecs}
}

// UserAgent returns the User-Agent string for HTTP requests
func UserAgent() string {
	return "izi-siprec/" + Version
}

// ServerHeader returns the Server header value for SIP responses
func ServerHeader() string {
	return "izi-siprec/" + Version
}
