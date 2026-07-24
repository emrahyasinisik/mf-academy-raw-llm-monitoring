package mcp

import (
	"net/http"
	"time"

	"github.com/emrah/mf-backend/internal/common"
	"github.com/go-chi/chi/v5"
)

// Routes mounts the MCP endpoint.
//
// One bound, and it is the generation bound rather than the short default:
// tools/call can run an analysis, which waits on a GPU across a tunnel. The
// cheap methods — initialize, ping, tools/list — return in microseconds and are
// not harmed by sharing a generous deadline, whereas splitting them would mean
// routing by JSON-RPC method inside the router, which chi cannot do and which
// would put protocol knowledge in the wrong layer.
func (s *Server) Routes(verify common.TokenVerifier, genTimeout time.Duration) http.Handler {
	r := chi.NewRouter()
	r.Use(common.RequireAuth(verify))
	r.Use(common.Timeout(genTimeout))

	r.Post("/", s.Handler)

	// GET is what a client uses to open a server-sent-events stream in the
	// Streamable HTTP transport. This server never initiates a message, so
	// there is nothing to stream — answered explicitly rather than left to
	// chi's 405, so the client is told why rather than left guessing whether
	// the endpoint exists.
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		common.Error(w, common.ErrBadRequest(
			"this server does not open a stream; POST JSON-RPC messages instead"))
	})

	return r
}
