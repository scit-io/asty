package agent

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/rs/zerolog/log"
)

// dropTarget is the resolved uid/gid the agent (and its spawned
// children) will run as after bootstrap. Zero value means "no drop";
// it's also the dev-mode default — RunAsUser empty in config.
type dropTarget struct {
	Enabled bool
	UID     int
	GID     int
	User    string // copied from cfg for log lines
	Group   string
}

// resolveDropTarget reads cfg.Agent.RunAsUser / RunAsGroup, looks up
// the corresponding uid/gid and returns the resolved tuple. Errors
// propagate so an operator who typed a wrong name learns about it at
// startup, not later when a chown fails. An empty RunAsUser disables
// the drop (returns Enabled=false, no error).
func (a *Agent) resolveDropTarget() (dropTarget, error) {
	cfg := a.cfg.Agent
	if cfg.RunAsUser == "" {
		return dropTarget{}, nil
	}

	u, err := user.Lookup(cfg.RunAsUser)
	if err != nil {
		return dropTarget{}, fmt.Errorf("lookup user %q: %w", cfg.RunAsUser, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return dropTarget{}, fmt.Errorf("parse uid for %q: %w", cfg.RunAsUser, err)
	}

	gidStr := u.Gid
	groupName := cfg.RunAsGroup
	if groupName != "" {
		g, err := user.LookupGroup(groupName)
		if err != nil {
			return dropTarget{}, fmt.Errorf("lookup group %q: %w", groupName, err)
		}
		gidStr = g.Gid
	}
	gid, err := strconv.Atoi(gidStr)
	if err != nil {
		return dropTarget{}, fmt.Errorf("parse gid: %w", err)
	}

	return dropTarget{
		Enabled: true,
		UID:     uid,
		GID:     gid,
		User:    cfg.RunAsUser,
		Group:   groupName,
	}, nil
}

// dropPrivileges chown's the directories the agent will need write
// access to after the drop, then calls setgid(2) followed by setuid(2).
// Modern Go (1.16+) implements syscall.Setgid and syscall.Setuid via
// AllThreadsSyscall on Linux, so the change applies to every OS thread
// in the runtime — not just the calling one. On macOS the call is a
// direct setuid(2) which is process-wide by definition.
//
// Idempotence: if the agent already runs as the target uid (operator
// started us under that account via systemd User=asty, say) we skip
// the syscalls — the chown is still useful when work_dir was
// pre-created by root during install.
//
// Order matters: setgid before setuid. Once euid != 0 we lose the
// privilege to call setgid(2), so the gid switch must come first.
func (a *Agent) dropPrivileges() error {
	if !a.drop.Enabled {
		return nil
	}

	dirs := []string{a.workDir}
	if storeDir := a.cfg.NATS.Server.JetStream.StoreDir; storeDir != "" {
		dirs = append(dirs, storeDir)
	}
	for _, d := range dirs {
		if err := chownTree(d, a.drop.UID, a.drop.GID); err != nil {
			return fmt.Errorf("chown %s: %w", d, err)
		}
	}

	if os.Getuid() == a.drop.UID && os.Getgid() == a.drop.GID {
		log.Info().Str("user", a.drop.User).Int("uid", a.drop.UID).Msg("agent already at target uid/gid, skipping setuid/setgid")
		return nil
	}

	if err := syscall.Setgid(a.drop.GID); err != nil {
		return fmt.Errorf("setgid %d: %w", a.drop.GID, err)
	}
	if err := syscall.Setuid(a.drop.UID); err != nil {
		return fmt.Errorf("setuid %d: %w", a.drop.UID, err)
	}

	log.Info().
		Str("user", a.drop.User).
		Str("group", a.drop.Group).
		Int("uid", a.drop.UID).
		Int("gid", a.drop.GID).
		Msg("agent privileges dropped")
	return nil
}

// chownTree walks path recursively and chown's every entry to uid/gid.
// Used to make work_dir and the nats-server store_dir writable after
// the drop. Silently no-ops if path doesn't exist — operator might
// not have configured a JetStream store_dir, or work_dir might be
// the install default that the systemd unit pre-creates.
func chownTree(path string, uid, gid int) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(p, uid, gid)
	})
}

// credentialForChildren returns the syscall.Credential to attach to
// a child process that's exec'd BEFORE the agent has dropped its own
// privileges — currently only nats-server in bootstrapNATS. User
// services start after the drop, so fork+exec inherits the agent's
// (already-dropped) uid naturally and Credential is not needed.
//
// Returns nil when drop is disabled OR when the agent is already
// running at the target uid/gid (e.g. systemd User=asty + a
// CAP_NET_BIND_SERVICE ambient cap to bind :80 without root). In
// that case the agent was never root, so asking exec to setuid would
// be a redundant syscall — and if it tried to change to a gid the
// process can't reach, it would fail with EPERM.
func (a *Agent) credentialForChildren() *syscall.Credential {
	if !a.drop.Enabled {
		return nil
	}
	if os.Getuid() == a.drop.UID && os.Getgid() == a.drop.GID {
		return nil
	}
	return &syscall.Credential{
		Uid: uint32(a.drop.UID),
		Gid: uint32(a.drop.GID),
	}
}

// withCredential merges a Credential into an existing SysProcAttr,
// allocating one when nil. Other SysProcAttr fields (Setpgid, etc.)
// stay as configured by the caller.
func withCredential(attr *syscall.SysProcAttr, cred *syscall.Credential) *syscall.SysProcAttr {
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.Credential = cred
	return attr
}
