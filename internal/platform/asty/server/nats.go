package server

import "asty/internal/platform/asty/core/netutil"

// connectNATS opens the NATS connection used by the server. Thin wrapper
// over core/netutil — agent does the same so they share startup options.
func (s *Server) connectNATS() error {
	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: s.cfg.NATSHost, Port: s.cfg.NATSPort,
		User: s.cfg.NATSUser, Password: s.cfg.NATSPassword,
	}, "asty-server-"+s.nodeID)
	if err != nil {
		return err
	}
	s.nc = nc
	return nil
}
