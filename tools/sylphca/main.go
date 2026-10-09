package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dir := fs.String("dir", "ca", "CA directory")
	var err error
	switch cmd {
	case "init":
		name := fs.String("name", "Sylph CA", "CA common name")
		err = fs.Parse(os.Args[2:])
		if err == nil {
			err = initCA(*dir, *name, time.Now())
		}
	case "issue":
		name := fs.String("name", "", "certificate name")
		certType := fs.String("type", "client", "client or server")
		ttl := fs.Duration("ttl", 8760*time.Hour, "certificate lifetime")
		err = fs.Parse(os.Args[2:])
		if err == nil {
			var usage mtls.LeafUsage
			usage, err = leafUsage(*certType)
			if err == nil && *name == "" {
				err = fmt.Errorf("--name is required")
			}
			if err == nil {
				err = issueCert(*dir, *name, usage, *ttl, time.Now())
			}
		}
	case "revoke":
		name := fs.String("name", "", "certificate name")
		err = fs.Parse(os.Args[2:])
		if err == nil {
			if *name == "" {
				err = fmt.Errorf("--name is required")
			} else {
				err = revokeCert(*dir, *name)
			}
		}
	case "list":
		err = fs.Parse(os.Args[2:])
		if err == nil {
			var infos []certInfo
			infos, err = listCerts(*dir)
			if err == nil {
				for _, ci := range infos {
					fmt.Printf("%-24s %-7s %-40s %s\n", ci.Name, ci.Type, ci.Serial, ci.NotAfter.Format(time.DateOnly))
				}
			}
		}
	default:
		printUsage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sylphca:", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: sylphca <init|issue|revoke|list> [flags]")
}

func leafUsage(s string) (mtls.LeafUsage, error) {
	switch s {
	case "client":
		return mtls.ClientUsage, nil
	case "server":
		return mtls.ServerUsage, nil
	}
	return 0, fmt.Errorf("unknown type %q, want client or server", s)
}
