package server

import "asty/asty/internal/core/netutil"

// connectNATS opens the NATS connection used by the server. Thin wrapper
// over core/netutil — agent does the same so they share startup options.
func (s *Server) connectNATS() error {
	nc, err := netutil.ConnectNATS(netutil.NATSCreds{
		Host: s.cfg.NATS.Host, Port: s.cfg.NATS.Port,
		User: s.cfg.NATS.User, Password: s.cfg.NATS.Password,
	}, "asty-server-"+s.nodeID)
	if err != nil {
		return err
	}
	s.nc = nc
	return nil
}
