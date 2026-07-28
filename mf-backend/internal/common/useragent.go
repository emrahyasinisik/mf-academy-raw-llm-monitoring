package common

// UserAgent identifies this service to everything it calls over the internet.
//
// It lives here rather than beside one caller because it is not a property of
// the LLM client: every outbound request that leaves for the inference host
// crosses the same Cloudflare edge, and Go's default "Go-http-client/1.1" is
// precisely the anonymous client that bot protection challenges. That was
// diagnosed once already on the generation path — a challenge page the Go
// client cannot solve, arriving as an unexplained failure — and a second
// caller with its own header would be a second chance to forget.
//
// Deliberately names what we are rather than imitating a browser: whoever
// reads the inference host's access log should be able to tell which client
// called.
const UserAgent = "mf-backend/0.1.0 (+https://github.com/emrahyasinisik/mf-academy-raw-llm-monitoring)"
