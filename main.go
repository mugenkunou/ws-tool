package main

import (
	"os"

	"github.com/mugenkunou/ws-tool/cmd"
)

func main() {
	os.Exit(cmd.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
