package main

import (
	"os"

	"github.com/alecthomas/kingpin/v2"
)

var (
	app = kingpin.New("taskguild-agent", "TaskGuild agent manager").
		UsageWriter(os.Stderr)

	runCmd              = app.Command("run", "Run the agent manager (connects to server, executes tasks)")
	overrideAgentMDFlag = runCmd.Flag("override-agent-md", "Overwrite all local agent MD files from server on startup").Bool()
	sentinelCmd         = app.Command("sentinel", "Supervisor that manages 'run' with auto-restart and binary watching")
)

func main() {
	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))
	switch cmd {
	case runCmd.FullCommand():
		runAgent()
	case sentinelCmd.FullCommand():
		runSentinel()
	}
}
