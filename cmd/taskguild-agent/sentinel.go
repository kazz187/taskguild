package main

import "github.com/kazz187/taskguild/pkg/sentinel"

// runSentinel starts the sentinel supervisor for the agent.
func runSentinel() {
	sentinel.Run()
}
