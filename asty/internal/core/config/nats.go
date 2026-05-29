package config

import "fmt"

// NATSConfig describes how Asty interacts with NATS on this node.
// The agent is the supervisor of the local nats-server process and
// renders its configuration at startup from the Server subsection.
//
// Three client-side credentials, all distinct:
//
//   - User/Password — main connection in the ASTY account. Server uses
//     it for everything. Agent uses it for everything except $SYS stats
//     (commands, cluster-state KV, log streams, gateway, ping).
//   - ObserverUser/ObserverPassword — agent-only connection in the SYS
//     account, restricted by permissions to STATSZ/JSZ request-reply.
//     Read in natsstats.go to feed asty_node_nats_* metrics.
//   - AppUser/AppPassword — credentials handed to spawned user-services
//     via A_NATS_USER / A_NATS_PASSWORD. MUST differ from the agent's
//     own credentials; otherwise apps inherit JetStream KV access to
//     the asty-cluster bucket and can rewrite cluster state.
type NATSConfig struct {
	User             string `yaml:"user"`
	Password         string `yaml:"password"`
	ObserverUser     string `yaml:"observer_user"`
	ObserverPassword string `yaml:"observer_password"`
	AppUser          string `yaml:"app_user"`
	AppPassword      string `yaml:"app_password"`

	Server NATSServerConfig `yaml:"server"`
}

// NATSServerConfig collects every field that ends up in the rendered
// nats.conf file. Per-node values (server_name, listen address,
// cluster.listen, cluster.routes) are not here — they're filled at
// render time from NodeID / NodeIP / DNS discovery.
type NATSServerConfig struct {
	Port          int                    `yaml:"port"`
	JetStream     NATSJetStreamConfig    `yaml:"jetstream"`
	Cluster       NATSClusterConfig      `yaml:"cluster"`
	Accounts      map[string]NATSAccount `yaml:"accounts"`
	SystemAccount string                 `yaml:"system_account"`
}

// NATSJetStreamConfig matches the `jetstream { ... }` block. Sizes
// follow the nats-server format (e.g. "256M", "10G").
type NATSJetStreamConfig struct {
	StoreDir  string `yaml:"store_dir"`
	MaxMemory string `yaml:"max_memory"`
	MaxFile   string `yaml:"max_file"`
}

// NATSClusterConfig matches the `cluster { ... }` block, minus the
// per-node routes which are discovered at runtime.
type NATSClusterConfig struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
}

// NATSAccount matches one entry in the `accounts { ... }` block.
// JetStream: true emits `jetstream: enabled` so the account can host
// streams and KV buckets. The system_account does not need it —
// Asty's KV lives in ASTY, never in SYS.
type NATSAccount struct {
	JetStream bool       `yaml:"jetstream"`
	Users     []NATSUser `yaml:"users"`
}

// NATSUser is one user inside an account. Permissions is optional —
// nil means the user gets full account-wide rights.
type NATSUser struct {
	User        string           `yaml:"user"`
	Password    string           `yaml:"password"`
	Permissions *NATSPermissions `yaml:"permissions,omitempty"`
}

// NATSPermissions matches `permissions { publish { allow: [...] }
// subscribe { allow: [...] } }`. Used to lock the observer user down
// to STATSZ/JSZ request-reply only.
type NATSPermissions struct {
	Publish   NATSSubjectACL `yaml:"publish"`
	Subscribe NATSSubjectACL `yaml:"subscribe"`
}

// NATSSubjectACL is the `{ allow: [...] }` body of a publish or
// subscribe permission. Deny lists could go here if needed later.
type NATSSubjectACL struct {
	Allow []string `yaml:"allow"`
}

// Validate rejects misconfiguration that would only surface as a NATS
// startup error or a silent auth failure at runtime.
func (n NATSConfig) Validate() error {
	if n.Server.Port <= 0 || n.Server.Port > 65535 {
		return fmt.Errorf("nats.server.port out of range: %d", n.Server.Port)
	}
	if n.Server.Cluster.Port != 0 {
		if n.Server.Cluster.Port < 0 || n.Server.Cluster.Port > 65535 {
			return fmt.Errorf("nats.server.cluster.port out of range: %d", n.Server.Cluster.Port)
		}
		if n.Server.Cluster.Port == n.Server.Port {
			return fmt.Errorf("nats.server.cluster.port must differ from server.port")
		}
	}
	if n.Server.SystemAccount != "" {
		if _, ok := n.Server.Accounts[n.Server.SystemAccount]; !ok {
			return fmt.Errorf("nats.server.system_account %q not defined in accounts", n.Server.SystemAccount)
		}
	}
	// The agent identity must not bleed into spawned services.
	if n.User != "" && n.AppUser != "" && n.User == n.AppUser {
		return fmt.Errorf("nats.user (%q) must differ from nats.app_user — apps would inherit cluster-state KV access", n.User)
	}
	for name, acc := range n.Server.Accounts {
		if len(acc.Users) == 0 {
			return fmt.Errorf("nats.server.accounts.%s has no users", name)
		}
		for i, u := range acc.Users {
			if u.User == "" {
				return fmt.Errorf("nats.server.accounts.%s.users[%d].user is empty", name, i)
			}
		}
	}
	return nil
}

// AppCredentials returns the credentials passed to spawned services via
// A_NATS_USER / A_NATS_PASSWORD. Empty strings if no app user is
// configured — in that case the agent MUST NOT export those env vars,
// because apps cannot fall back to the agent's identity (it has
// JetStream KV access to the asty-cluster bucket).
func (n NATSConfig) AppCredentials() (user, password string) {
	return n.AppUser, n.AppPassword
}

func natsDefaults() NATSConfig {
	return NATSConfig{
		Server: NATSServerConfig{
			Port: 4222,
			JetStream: NATSJetStreamConfig{
				StoreDir:  "/var/lib/asty/jetstream",
				MaxMemory: "256M",
				MaxFile:   "10G",
			},
			Cluster: NATSClusterConfig{
				Name: "asty",
				Port: 6222,
			},
		},
	}
}
