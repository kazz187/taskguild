package main

import "github.com/kazz187/taskguild/backend/pkg/sentinel"

// runSentinel starts the sentinel supervisor for the server.
func runSentinel() {
	sentinel.Run()
}
