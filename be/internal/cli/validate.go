package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/anish-araz_cumulus/firecracker-manager-go/internal/config"
	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check that all manager prerequisites (config, binaries, paths, bridge) are satisfied",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			ok := runValidate(cfgPath)
			if !ok {
				return errors.New("validation failed")
			}
			return nil
		},
	}
}

// runValidate prints a checklist and returns true if every check passed.
func runValidate(cfgPath string) bool {
	checks := []check{}

	// 1. Config load
	cfg, cfgErr := config.Load(cfgPath)
	if cfgErr != nil {
		checks = append(checks, fail("config: load", cfgErr.Error()))
		report("Configuration", checks)
		return false
	}
	checks = append(checks, pass("config: load", cfgPathDesc(cfgPath)))

	// 2. Required binaries on PATH
	binChecks := []check{}
	for _, b := range []string{"firecracker"} {
		if p, err := exec.LookPath(b); err == nil {
			binChecks = append(binChecks, pass(b, p))
		} else {
			binChecks = append(binChecks, fail(b, "not found in PATH"))
		}
	}
	for _, b := range []string{"socat", "ip"} {
		if p, err := exec.LookPath(b); err == nil {
			binChecks = append(binChecks, pass(b+" (optional)", p))
		} else {
			binChecks = append(binChecks, warn(b+" (optional)", "not found in PATH"))
		}
	}

	// 3. File paths
	pathChecks := []check{
		statFile("kernel_path", cfg.KernelPath),
		statFile("rootfs_template", cfg.RootfsTemplate),
		statDir("state_dir", cfg.StateDir, true),
	}

	// 4. Bridge present on host
	netChecks := []check{checkBridge(cfg.BridgeName)}

	// 5. Listen address parses + free
	netChecks = append(netChecks, checkListenAddr(cfg.ListenAddr))

	// 6. Store config sane
	storeChecks := []check{checkStore(cfg)}

	// 7. Permissions: tap creation needs CAP_NET_ADMIN (root in practice)
	storeChecks = append(storeChecks, checkRoot())

	report("Configuration", checks)
	report("Binaries", binChecks)
	report("Paths", pathChecks)
	report("Network", netChecks)
	report("Runtime", storeChecks)

	all := append(append(append(append(append([]check{}, checks...), binChecks...), pathChecks...), netChecks...), storeChecks...)
	failed := 0
	for _, c := range all {
		if c.status == statusFail {
			failed++
		}
	}
	fmt.Println()
	if failed == 0 {
		fmt.Println("\033[32m✔ all required checks passed\033[0m")
		return true
	}
	fmt.Printf("\033[31m✘ %d required check(s) failed\033[0m\n", failed)
	return false
}

// ---- check primitives ---------------------------------------------------

type checkStatus int

const (
	statusPass checkStatus = iota
	statusWarn
	statusFail
)

type check struct {
	name   string
	detail string
	status checkStatus
}

func pass(n, d string) check { return check{n, d, statusPass} }
func warn(n, d string) check { return check{n, d, statusWarn} }
func fail(n, d string) check { return check{n, d, statusFail} }

func report(section string, cs []check) {
	fmt.Printf("\n\033[1m%s\033[0m\n", section)
	for _, c := range cs {
		var sym, color string
		switch c.status {
		case statusPass:
			sym, color = "✔", "\033[32m"
		case statusWarn:
			sym, color = "!", "\033[33m"
		case statusFail:
			sym, color = "✘", "\033[31m"
		}
		fmt.Printf("  %s%s\033[0m %-22s %s\n", color, sym, c.name, dim(c.detail))
	}
}

func dim(s string) string {
	if s == "" {
		return ""
	}
	return "\033[2m" + s + "\033[0m"
}

// ---- individual checks --------------------------------------------------

func cfgPathDesc(p string) string {
	if p == "" {
		return "(defaults + env only)"
	}
	return p
}

func statFile(name, p string) check {
	if p == "" {
		return fail(name, "not set")
	}
	st, err := os.Stat(p)
	if err != nil {
		return fail(name, err.Error())
	}
	if st.IsDir() {
		return fail(name, p+" is a directory, want file")
	}
	if st.Size() == 0 {
		return warn(name, p+" exists but is empty")
	}
	return pass(name, fmt.Sprintf("%s (%.1f MiB)", p, float64(st.Size())/1024/1024))
}

func statDir(name, p string, autoCreate bool) check {
	if p == "" {
		return fail(name, "not set")
	}
	st, err := os.Stat(p)
	if err == nil {
		if !st.IsDir() {
			return fail(name, p+" is not a directory")
		}
		// AF_UNIX path limit warning: state_dir + "/vms/<26-ulid>/console.sock" <= ~108
		const ulidPlusSock = len("/vms/") + 26 + len("/console.sock")
		if len(p)+ulidPlusSock > 100 {
			return warn(name, fmt.Sprintf("%s — long path may exceed AF_UNIX 108-byte limit (used+overhead=%d)", p, len(p)+ulidPlusSock))
		}
		return pass(name, p)
	}
	if !os.IsNotExist(err) {
		return fail(name, err.Error())
	}
	if autoCreate {
		return warn(name, p+" does not exist (will be created on start)")
	}
	return fail(name, p+" does not exist")
}

func checkBridge(name string) check {
	if name == "" {
		return fail("bridge", "bridge_name not set")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return fail("bridge "+name, "not found ("+err.Error()+")")
	}
	if iface.Flags&net.FlagUp == 0 {
		return warn("bridge "+name, "interface is DOWN")
	}
	// best-effort: check it's a bridge by reading /sys/class/net/<name>/bridge
	if _, err := os.Stat("/sys/class/net/" + name + "/bridge"); err != nil {
		return warn("bridge "+name, "exists but does not look like a Linux bridge")
	}
	return pass("bridge "+name, "up, is a bridge")
}

func checkListenAddr(addr string) check {
	if addr == "" {
		return fail("listen_addr", "not set")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fail("listen_addr", err.Error())
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return fail("listen_addr "+addr, "cannot bind: "+err.Error())
	}
	_ = l.Close()
	return pass("listen_addr", addr)
}

func checkStore(cfg config.Config) check {
	switch cfg.Store.Type {
	case "json":
		if cfg.Store.JSON.Path == "" {
			return warn("store=json", "path empty (will default to <state_dir>/vms.json)")
		}
		return pass("store=json", cfg.Store.JSON.Path)
	case "mongo":
		if cfg.Store.Mongo.URI == "" {
			return fail("store=mongo", "uri not set")
		}
		return warn("store=mongo", "selected but mongostore is not implemented yet")
	default:
		return fail("store.type", "unknown: "+cfg.Store.Type)
	}
}

func checkRoot() check {
	if os.Geteuid() == 0 {
		return pass("privileges", "running as root (CAP_NET_ADMIN OK)")
	}
	return warn("privileges", "not root — tap creation will fail unless CAP_NET_ADMIN is granted")
}

// (utility) used to keep imports minimal during refactors
var _ = strings.TrimSpace
