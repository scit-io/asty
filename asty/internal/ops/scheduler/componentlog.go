package scheduler

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// clog returns the global logger tagged as the scheduler component.
// Fresh per call so writer/level reassignments (logs.AttachNATS,
// logs.SetLevel) take effect immediately — no init-time capture of
// the pre-configured global.
func clog() *zerolog.Logger { l := log.With().Str("component", "scheduler").Logger(); return &l }
