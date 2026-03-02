package main

import (
	"os"

	"github.com/alecthomas/kingpin/v2"
)

var (
	app = kingpin.New("taskguild-server", "TaskGuild server").
		UsageWriter(os.Stderr)

	runCmd      = app.Command("run", "Run the server")
	sentinelCmd = app.Command("sentinel", "Supervisor that manages 'run' with auto-restart and binary watching")
	migrateCmd  = app.Command("migrate", "Run data migrations")
)

func main() {
	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	switch cmd {
	case runCmd.FullCommand():
		runServer()
	case sentinelCmd.FullCommand():
		runSentinel()
	case migrateCmd.FullCommand():
		runMigrate()
	}
}
