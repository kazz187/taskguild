package main

import "github.com/kazz187/taskguild/pkg/sentinel"

// runSentinel starts the sentinel supervisor for the server.
func runSentinel() {
	var extraArgs []string
	if *sentinelProf {
		extraArgs = append(extraArgs, "--prof")
	}
	sentinel.Run(extraArgs...)
}
