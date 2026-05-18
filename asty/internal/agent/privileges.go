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

// dropUserName is the dedicated unprivileged identity Asty drops to
// after bootstrap. A dedicated account (not `nobody`) matters for
// blast-radius reasons:
//
//   - `nobody` is shared with whatever else on the host happens to
//     drop to it. Same-uid processes can ptrace each other and read
//     each other's /proc/<pid>/mem; with `nobody` an unrelated
//     daemon's RCE would buy access to Asty's memory (and therefore
//     its NATS credentials → the whole cluster).
//   - `asty` is created at install (one `useradd --system asty`
//     line). Nothing else on the host runs as it, so the same-uid
//     attack vector closes.
//
// The name is hardcoded — no env wiring, no operator confusion. If
// the user is missing on the host the agent stays as root and logs
// a loud warning so the misconfiguration is visible.
const dropUserName = "asty"

// dropTarget is the resolved uid/gid the agent (and the nats-server
// it exec's pre-drop) will run as. Empty when:
//   - the agent didn't start as root (nothing to drop), or
//   - the host has no `nobody` user (e.g. distroless without it).
type dropTarget struct {
	Enabled bool
	UID     int
	GID     int
}

// resolveDropTarget decides whether to drop and, if yes, where to.
// Two checks:
//
//  1. If we didn't start as root, drop is a no-op — there is nothing
//     to give up. Many dev / container setups already run as some
//     non-root uid; in that case the function returns Enabled=false.
//
//  2. If we did start as root, look up the dedicated dropUserName
//     ("asty"). Missing user is logged loudly but NOT fatal — the
//     agent continues as root so the operator notices the misconfig
//     immediately in journalctl, instead of failing some downstream
//     KV write at 3 AM.
//
// Returns the target uid/gid pair; the rest of the drop sequence
// consumes it.
func (a *Agent) resolveDropTarget() (dropTarget, error) {
	if os.Geteuid() != 0 {
		return dropTarget{}, nil
	}

	u, err := user.Lookup(dropUserName)
	if err != nil {
		log.Warn().
			Err(err).
			Str("user", dropUserName).
			Msg("drop-root: no `asty` user on this host (run `useradd --system asty`); agent will continue as root")
		return dropTarget{}, nil
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return dropTarget{}, fmt.Errorf("parse %s uid %q: %w", dropUserName, u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return dropTarget{}, fmt.Errorf("parse %s gid %q: %w", dropUserName, u.Gid, err)
	}
	return dropTarget{Enabled: true, UID: uid, GID: gid}, nil
}

// dropPrivileges chown's the directories the agent will need write
// access to after the drop, then calls setgid(2) followed by setuid(2).
// Modern Go (1.16+) implements syscall.Setgid and syscall.Setuid via
// AllThreadsSyscall on Linux, so the change applies to every OS thread
// in the runtime — not just the calling one. On macOS the call is a
// direct setuid(2) which is process-wide by definition.
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

	if err := syscall.Setgid(a.drop.GID); err != nil {
		return fmt.Errorf("setgid %d: %w", a.drop.GID, err)
	}
	if err := syscall.Setuid(a.drop.UID); err != nil {
		return fmt.Errorf("setuid %d: %w", a.drop.UID, err)
	}

	log.Info().
		Str("user", dropUserName).
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
// privileges — currently only nats-server in bootstrapNATS. Without
// this, nats-server would inherit the agent's uid=0, and the post-
// drop agent (now nobody) wouldn't be able to signal it for SIGHUP /
// SIGTERM. Returns nil when drop is disabled (agent stays at the OS
// uid throughout, so the child inherits it correctly).
func (a *Agent) credentialForChildren() *syscall.Credential {
	if !a.drop.Enabled {
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
