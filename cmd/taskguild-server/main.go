package main

import (
	"os"

	"github.com/alecthomas/kingpin/v2"
)

var (
	app = kingpin.New("taskguild-server", "TaskGuild server").
		UsageWriter(os.Stderr)

	runCmd     = app.Command("run", "Run the server")
	runProf    = runCmd.Flag("prof", "Enable pprof server on :6060").Bool()
	sentinelCmd  = app.Command("sentinel", "Supervisor that manages 'run' with auto-restart and binary watching")
	sentinelProf = sentinelCmd.Flag("prof", "Enable pprof server in child 'run' process").Bool()

	seedUpsertCmd       = app.Command("seed-upsert", "Upsert default skills from the Seeder into an existing project (non-destructive — skills not in defaults are left alone).")
	seedUpsertProjectID = seedUpsertCmd.Flag("project-id", "Project ID to upsert skills into").Required().String()
)

func main() {
	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	switch cmd {
	case runCmd.FullCommand():
		runServer()
	case sentinelCmd.FullCommand():
		runSentinel()
	case seedUpsertCmd.FullCommand():
		runSeedUpsert(*seedUpsertProjectID)
	}
}
