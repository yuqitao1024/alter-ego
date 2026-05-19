package orchestrator

import "time"

type AppServerState struct {
	ThreadID             string
	ActiveTurnID         string
	LastThreadStatus     string
	LastTurnStatus       string
	LastObservedItemID   string
	LastRemoteActivityAt *time.Time
}
