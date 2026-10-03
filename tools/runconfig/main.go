// runconfig starts the embedded sing-box with a config file (used by CI end-to-end tests).
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nshimanovskiy/tainavpn/core"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: runconfig config.json")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	logs := core.NewLogBuffer(10)
	logs.Echo = func(line string) { fmt.Println(line) }
	inst, err := core.Start(string(data), nil, logs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	fmt.Println("started sing-box", core.Version())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = inst.Close()
	fmt.Println("stopped")
}
