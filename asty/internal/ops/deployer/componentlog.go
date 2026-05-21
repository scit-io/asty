package deployer

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// clog returns the global logger tagged as the deployer component.
// Fresh per call so writer/level reassignments (logs.AttachNATS,
// logs.SetLevel) take effect immediately — no init-time capture of
// the pre-configured global.
func clog() *zerolog.Logger { l := log.With().Str("component", "deployer").Logger(); return &l }
