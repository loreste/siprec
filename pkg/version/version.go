package version

// Version is the current version of the SIPREC server
const Version = "1.0.0"

// UserAgent returns the User-Agent string for HTTP requests
func UserAgent() string {
	return "izi-siprec/" + Version
}

// ServerHeader returns the Server header value for SIP responses
func ServerHeader() string {
	return "izi-siprec/" + Version
}
